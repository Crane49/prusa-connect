package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The errors staging reports. Callers inspect them with errors.Is.
var (
	// ErrNoSuchStorage reports a storage resource this deployment does not
	// declare, or one that is not running.
	ErrNoSuchStorage = errors.New("app: no storage resource of that name is running")
	// ErrStorageCannotRead reports a storage resource whose type holds files it
	// will not hand out. That is not a mistake in the operator's declaration,
	// so it has an error of its own, apart from an absent name.
	ErrStorageCannotRead = errors.New("app: that storage type cannot open a file for reading")
)

// staging is how a file held by one of this deployment's storage resources
// reaches a printer.
//
// Every call goes through the supervisor, because a resource is third-party
// code and the host must supervise every call into one. So a transfer opens
// the file again for every range the printer asks for and holds no handle
// between them. A handle kept across supervised calls would outlive the call
// that produced it, and a resource restarted mid-transfer would go on serving
// through it.
type staging struct {
	host *resourcehost.Host
}

// content returns the bytes of one file, for a transfer to read as the printer
// asks for them.
//
// name is relative to the storage's own root. The file's length is read once,
// here, because the printer is told how much to expect before the first byte
// leaves.
func (s staging) content(ctx context.Context, instance, name string) (connectorapi.Content, error) {
	// The same refusal the storage routes give, because it is the same
	// namespace. Without it the provider would silently rewrite the name to a
	// different file.
	if err := refuseClimb(instance, name); err != nil {
		return nil, err
	}

	if instance == deviceapi.ModuleDataStorage {
		dir, held := ctx.Value(moduleDataKey{}).(string)
		if !held {
			return nil, noSuchStorage(instance, ErrNoSuchStorage)
		}

		return printFolderContent(ctx, dir, name)
	}

	target, err := ref.New(ref.KindStorage, instance)
	if err != nil {
		return nil, noSuchStorage(instance, err)
	}

	running, found := s.host.Instance(target)
	if !found {
		return nil, noSuchStorage(instance, ErrNoSuchStorage)
	}

	info, err := s.measure(ctx, running, name)
	if err != nil {
		return nil, stagingFault(instance, name, "stat", err)
	}

	return stagedFile{
		size:     info.Size,
		identity: identityOf(info),
		open: func(callCtx context.Context, offset int64) (io.ReadCloser, error) {
			return openStaged(callCtx, running, name, offset)
		},
	}, nil
}

// printFolder is the folder in a module's data directory that the core prints
// from under deviceapi.ModuleDataStorage.
const printFolder = "print"

// moduleDataKey carries the data directory of the module that asked for a
// print. The calling module is who is asking, as the principal is. Only
// moduleFleet.Print sets it, so a print over HTTP finds no folder under
// deviceapi.ModuleDataStorage.
type moduleDataKey struct{}

// withModuleData is ctx carrying the calling module's data directory.
func withModuleData(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, moduleDataKey{}, dir)
}

// printFolderFiles is the print folder of one module's data directory, read
// and never written. It creates nothing, so a print before the module made the
// folder leaves the data directory as it was.
type printFolderFiles string

// OpenLocal opens one file of the print folder. The folder and the file are
// each resolved under a directory handle, so a link that leaves either is
// refused. The refusal is the one a missing file gets.
func (data printFolderFiles) OpenLocal(_ context.Context, name string) (storageapi.LocalFile, error) {
	dataRoot, err := os.OpenRoot(string(data))
	if err != nil {
		return nil, outOfPrintFolder(err)
	}

	defer func() { _ = dataRoot.Close() }()

	folder, err := dataRoot.OpenRoot(printFolder)
	if err != nil {
		return nil, outOfPrintFolder(err)
	}

	defer func() { _ = folder.Close() }()

	local := filepath.FromSlash(storageapi.CleanRelPath(name))

	// Opening a FIFO for reading waits for a writer, and would hold the print
	// until one came. The open does not wait, and the check is made on the
	// handle it gives. So an entry swapped for a FIFO after a check is also
	// refused.
	file, err := folder.OpenFile(local, os.O_RDONLY|openNoWait, 0)
	if err != nil {
		return nil, outOfPrintFolder(err)
	}

	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%w: %q is not a regular file", ErrStorageCannotRead, name)
	}

	if err != nil {
		_ = file.Close() // a read-only handle; the error above is the one to report

		return nil, err
	}

	return file, nil
}

// outOfPrintFolder folds a name that resolves to nothing in the print folder
// onto fs.ErrNotExist. A missing folder, a component that is not a folder and
// a link escaping the root (an error with no errno) are all a file that is
// not there.
func outOfPrintFolder(err error) error {
	var errno syscall.Errno
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || !errors.As(err, &errno) {
		return fmt.Errorf("%w: %w", fs.ErrNotExist, err)
	}

	return err
}

// printFolderContent is one file of a module's print folder. Its version is
// the module's data directory, its time of change and its length. So a second
// print of it can start the copy this process already sent, and a file of
// another module's folder is never taken for it.
func printFolderContent(ctx context.Context, data, name string) (connectorapi.Content, error) {
	folder := printFolderFiles(data)

	file, err := folder.OpenLocal(ctx, name)
	if err != nil {
		return nil, stagingFault(deviceapi.ModuleDataStorage, name, "stat", err)
	}

	info, err := file.Stat()
	_ = file.Close() // a read-only handle opened only for the Stat

	if err != nil {
		return nil, stagingFault(deviceapi.ModuleDataStorage, name, "stat", err)
	}

	identity := identityOf(storageapi.FileInfo{Size: info.Size(), MTime: info.ModTime().Unix()})
	if identity != "" {
		identity = data + ":" + identity
	}

	return stagedFile{
		size:     info.Size(),
		identity: identity,
		open: func(callCtx context.Context, offset int64) (io.ReadCloser, error) {
			return openLocalStaged(callCtx, folder, name, offset)
		},
	}, nil
}

// measure reads the file's length and what tells this version of it from
// another. It is a separate call from the opens a transfer makes. A file that
// changed length between the two gives a transfer the printer ends short.
func (s staging) measure(
	ctx context.Context, running *supervisor.Instance[any], name string,
) (storageapi.FileInfo, error) {
	return supervisor.Call(ctx, running, "stat",
		func(callCtx context.Context, target any) (storageapi.FileInfo, error) {
			provider, ok := target.(storageapi.Provider)
			if !ok {
				return storageapi.FileInfo{}, ErrStorageCannotRead
			}

			info, err := provider.Stat(callCtx, name)
			if err != nil {
				return storageapi.FileInfo{}, fmt.Errorf("measure the staged file: %w", err)
			}

			if info.IsDir {
				return storageapi.FileInfo{}, fmt.Errorf("%w: %q names a directory", ErrStorageCannotRead, name)
			}

			return info, nil
		})
}

// identityOf is what tells one version of a stored file from another: the
// digest the provider kept, or else its modification time and length. Empty
// when the provider reports neither, and then no version is ever taken for
// another.
func identityOf(info storageapi.FileInfo) string {
	if info.SHA256 != "" {
		return "sha256:" + info.SHA256
	}

	if info.MTime != 0 {
		return fmt.Sprintf("mtime:%d:%d", info.MTime, info.Size)
	}

	return ""
}

// openStaged opens the file through the supervisor, positioned at offset.
//
// Two kinds of provider are served, the cheaper first. A store with a file on
// a disk hands over the handle, and the seek costs nothing. A store that
// answers a byte range opens only the range that is wanted, as an object store
// does with a Range header, so a transfer does not read a file from its start
// every time the printer asks for the middle of it. A store with neither
// cannot stage a file for a printer, and the call fails with
// ErrStorageCannotRead.
func openStaged(
	ctx context.Context, running *supervisor.Instance[any], name string, offset int64,
) (io.ReadCloser, error) {
	return supervisor.Call(ctx, running, "open",
		func(callCtx context.Context, target any) (io.ReadCloser, error) {
			provider, ok := target.(storageapi.Provider)
			if !ok {
				return nil, ErrStorageCannotRead
			}

			if local, has := provider.(storageapi.LocalFiles); has {
				return openLocalStaged(callCtx, local, name, offset)
			}

			if ranged, has := provider.(storageapi.RangeReader); has {
				body, err := ranged.OpenRange(callCtx, name, offset, -1)
				if err != nil {
					return nil, fmt.Errorf("open the staged file at %d: %w", offset, err)
				}

				return body, nil
			}

			return nil, ErrStorageCannotRead
		})
}

// localOpener is the half of storageapi.LocalFiles a staged read needs.
type localOpener interface {
	OpenLocal(ctx context.Context, relPath string) (storageapi.LocalFile, error)
}

// openLocalStaged opens the file handle and seeks it.
func openLocalStaged(
	ctx context.Context, local localOpener, name string, offset int64,
) (io.ReadCloser, error) {
	file, err := local.OpenLocal(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("open the staged file: %w", err)
	}

	if offset == 0 {
		return file, nil
	}

	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		// The caller gets the seek error. A close error would hide the cause.
		_ = file.Close()

		return nil, fmt.Errorf("seek the staged file to %d: %w", offset, err)
	}

	return file, nil
}

// noSuchStorage is the refusal a name nothing is serving under gets.
func noSuchStorage(instance string, cause error) error {
	return fault.StorageNotFound.
		New("no storage of that name is serving").
		With(fault.StringParam("storage_id", instance)).
		Wrap(cause)
}

// readsAFile says whether op asked the storage for a file that is already
// there. Only those two do. A save, a delete, a rename or a new directory
// carries a name the caller wants the storage to take, so a name it will never
// resolve is the caller's own mistake and not a missing file.
func readsAFile(op string) bool { return op == "stat" || op == "open" }

// stagingFault gives a failure to read a staged file a code the caller can act
// on. op is the provider call that failed, as `call` in storagefiles.go names
// it.
//
// A storage that is not ready, a name it holds nothing under, and a type that
// cannot serve a file mean three different things to the caller, and all
// three arrive here as an error from a supervised call. A failure that is
// already a fault keeps its own code, such as the supervisor's "not ready".
func stagingFault(instance, name, op string, err error) error {
	if _, ok := errors.AsType[*fault.Fault](err); ok {
		return err
	}

	named := []fault.Param{
		fault.StringParam("storage_id", instance),
		fault.StringParam("path", name),
	}

	absent := func() error {
		return fault.StorageFileNotFound.
			New("the storage holds no file of that name").
			With(named...).
			Wrap(err)
	}

	var tooLarge *http.MaxBytesError

	switch {
	case errors.As(err, &tooLarge):
		// The transport sets the bound, and the error surfaces here because
		// the provider copies from the reader the transport wrapped. Without
		// this arm the caller would get a retryable server error for a file
		// that is over the published limit.
		return httpfault.BodyTooLarge(tooLarge.Limit).Wrap(err)
	case errors.Is(err, syscall.ENOSPC):
		// On FAT and USB this arrives from Close and not from a write, so it
		// is mapped here and not where the copy runs. EDQUOT is the same
		// condition and has no portable spelling, so it lands in the default
		// branch.
		return fault.StorageNoSpace.
			New("the storage ran out of room").
			With(named...).
			Wrap(err)
	case errors.Is(err, ErrStorageCannotRead), errors.Is(err, storageapi.ErrNoLocalFile):
		return fault.StorageNotReadable.
			New("that storage cannot serve a file that way").
			With(fault.StringParam("storage_id", instance)).
			Wrap(err)
	case errors.Is(err, storageapi.ErrExtensionNotAllowed):
		return fault.StorageExtensionNotAllowed.
			New("that storage does not accept a file with this extension").
			With(append(named, fault.StringParam("extension", path.Ext(name)))...).
			Wrap(err)
	// A store answers a name it holds nothing under with both errors. The
	// provider contract publishes the path error for a name that does not
	// resolve. The absence error is there because a caller reading a file
	// should get "there is no such file" and a 404, and not "your path is
	// wrong" and a 400. The call decides which one wins, through the next
	// three arms in this order.
	case readsAFile(op) && errors.Is(err, os.ErrNotExist):
		return absent()
	case errors.Is(err, storageapi.ErrInvalidPath):
		return fault.StorageInvalidPath.
			New("the storage will not accept that name").
			With(named...).
			Wrap(err)
	case errors.Is(err, os.ErrNotExist):
		return absent()
	case errors.Is(err, storageapi.ErrDeletePartial):
		return fault.StorageDeleteIncomplete.
			New("some of the entries are still there").
			With(named...).
			Wrap(err)
	case errors.Is(err, os.ErrPermission):
		return fault.StorageNotReadable.
			New("the server may not read or write there").
			With(fault.StringParam("storage_id", instance)).
			Wrap(err)
	default:
		// Not one of the codes above. A failure nothing here recognises is the
		// server's and not the caller's, and reporting it as "no such file"
		// would send a caller looking for a name that is fine.
		return fault.StorageUnavailable.
			New("the storage could not carry that out").
			With(named...).
			Wrap(err)
	}
}

// stagedFile is one file on a storage resource, as a transfer reads it.
type stagedFile struct {
	size int64
	// identity is identityOf the file when it was measured.
	identity string
	open     func(ctx context.Context, offset int64) (io.ReadCloser, error)
}

func (f stagedFile) Size() int64 { return f.size }

// Identity is the version of the stored file this transfer reads.
func (f stagedFile) Identity() string { return f.identity }

// Open returns the file's bytes from offset. It opens a new handle each time,
// so every read starts inside a supervised call. An offset at or past the end
// returns a reader at end of file and no error.
func (f stagedFile) Open(ctx context.Context, offset int64) (io.ReadCloser, error) {
	if offset >= f.size {
		// Nothing is opened, which saves a round trip to a store reached over
		// a network.
		return io.NopCloser(emptyReader{}), nil
	}

	return f.open(ctx, offset)
}

// emptyReader is end of file, for an offset past the last byte.
type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
