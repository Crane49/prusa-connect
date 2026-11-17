package app

// The module wire onto a storage's contents: one file described, one range of
// one file read, one file written, and one folder listed. Each goes through
// the storage plane, so a module and a person take the same permission and the
// same confinement.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// moduleStorage answers sdk/storageapi's module wire for one module instance.
//
// A call naming a person runs as that person, whom the supervision plane has
// already vouched for. A call naming nobody is the module's own work and runs
// as the instance inside the ceiling the operator wrote, which is the rule
// moduleStanding states. Either way the storage plane decides read_storage on
// the storage kind, so nothing here is a second door onto a file.
type moduleStorage struct {
	stores   httpapi.Storages
	standing moduleStanding
	log      *refusalLog
	// raw is the storages behind the permission check, read only through a
	// handle the host made for this instance.
	raw     stores
	handles *fileHandles
}

var _ storageapi.Files = moduleStorage{}

// moduleStorages binds an instance to the storages it may read.
//
// It is a late binding for the reason moduleFleets is: the storage plane needs
// the supervision plane, and the supervision plane is built first. The holder
// is filled once, before the first reconcile starts a declared instance. A
// plane run with no storages, which the tests do, reads CodeMethodNotFound
// rather than a nil.
type moduleStorages struct {
	stores  httpapi.Storages
	log     *refusalLog
	raw     stores
	handles *fileHandles
}

// bind fills the holder. It runs once, before any module starts.
func (m *moduleStorages) bind(stores httpapi.Storages) { m.stores = stores }

// bindHandles hands the holder the storages a read handle reads, and the
// handles the host makes. It runs once, beside bind.
func (m *moduleStorages) bindHandles(raw stores, handles *fileHandles) {
	m.raw = raw
	m.handles = handles
}

// forModule is what resourcehost.Config.Storage takes.
func (m *moduleStorages) forModule(module resourcehost.Module) storageapi.Files {
	if m.stores == nil {
		return nil
	}

	return moduleStorage{
		stores: m.stores, standing: standingOf(module), log: m.log, raw: m.raw, handles: m.handles,
	}
}

// as is who a read runs as: the person it names, or the instance itself when
// read_storage is inside its ceiling.
func (m moduleStorage) as(principal string) (string, error) {
	return m.asFor(principal, authz.ActionReadStorage)
}

// asFor is who a call taking action runs as.
func (m moduleStorage) asFor(principal string, action authz.Action) (string, error) {
	who, err := m.standing.as(principal, action)
	if err != nil {
		return "", fmt.Errorf("%w: %s takes %s, and %w",
			storageapi.ErrDenied, m.standing.principal, action, err)
	}

	return who, nil
}

// Write publishes one file. The storage plane takes write_storage and writes
// under a temporary name, so a body that fails partway publishes nothing.
func (m moduleStorage) Write(
	ctx context.Context, params storageapi.WriteParams, body io.Reader,
) (storageapi.Written, error) {
	who, err := m.asFor(params.Principal, authz.ActionWriteStorage)
	if err != nil {
		return storageapi.Written{}, err
	}

	saved, err := m.stores.Save(ctx, who, params.Storage, params.Path, body)
	if err != nil {
		// A body the module cut short is the module's own failure, and it
		// reads as one rather than as a storage that cannot be written.
		if errors.Is(err, storageapi.ErrShortWrite) {
			return storageapi.Written{}, err
		}

		return storageapi.Written{}, m.refusal(ctx, err)
	}

	return storageapi.Written{Path: saved.Path, Size: saved.Size, SHA256: saved.SHA256}, nil
}

// Stat describes one file without reading it.
func (m moduleStorage) Stat(
	ctx context.Context, params storageapi.StatParams,
) (storageapi.FileInfo, error) {
	if params.Handle != "" {
		_, info, err := m.byHandle(ctx, params.Handle)

		return info, err
	}

	who, err := m.as(params.Principal)
	if err != nil {
		return storageapi.FileInfo{}, err
	}

	// A module describing a file for the core may read that same file by
	// its path, so its own stat never waits for a module.
	entry, err := m.stores.Stat(withoutAnalysisWait(ctx), who, params.Storage, params.Path)
	if err != nil {
		return storageapi.FileInfo{}, m.refusal(ctx, err)
	}

	return storageapi.FileInfo{
		Name:   entry.Name,
		Path:   entry.Path,
		IsDir:  entry.Folder,
		Size:   entry.Size,
		MTime:  entry.Modified.Unix(),
		SHA256: entry.SHA256,
		Print:  printInfoOf(entry.Print),
	}, nil
}

// printInfoOf carries what the slicer said onto the module wire, so a module
// reads the print time and the filament without parsing the file itself.
func printInfoOf(details *httpapi.PrintDetails) *storageapi.PrintInfo {
	if details == nil {
		return nil
	}

	out := &storageapi.PrintInfo{
		EstimatedSeconds: details.EstimatedSeconds,
		FilamentMM:       details.FilamentMM,
		FilamentG:        details.FilamentG,
		LayerHeightMM:    details.LayerHeightMM,
		NozzleMM:         details.NozzleMM,
		Material:         details.Material,
		PrinterModel:     details.PrinterModel,
		PrintProfile:     details.PrintProfile,
		PrinterProfile:   details.PrinterProfile,
		FilamentProfile:  details.FilamentProfile,
		HighFlow:         details.HighFlow,
		ThumbnailWidth:   details.ThumbnailWidth,
		ThumbnailHeight:  details.ThumbnailHeight,
	}

	moduleNeeds(out, details)

	return out
}

// List answers one page of one folder, in the order of the entries' names,
// leaving out a link and a special file.
//
// The storage plane lists the folder under read_storage and refuses a path
// that leaves the storage, as it does for Stat. A page ends after
// storageapi.MaxListEntries entries, and Next is the name of its last entry.
// The next page starts after that name, so an entry added or removed between
// two calls moves no other entry onto a page the module already read.
func (m moduleStorage) List(
	ctx context.Context, params storageapi.ListParams,
) (storageapi.Listing, error) {
	who, err := m.as(params.Principal)
	if err != nil {
		return storageapi.Listing{}, err
	}

	// shortcut: every page reads the whole folder; give the provider a cursor
	// when folders of many thousands of entries are common.
	entries, err := m.stores.Files(ctx, who, params.Storage, params.Path)
	if err != nil {
		return storageapi.Listing{}, m.refusal(ctx, err)
	}

	// A link and a special file are not objects of the store. They leave
	// before paging, so a page holds MaxListEntries real entries and Next
	// always names one.
	entries = slices.DeleteFunc(entries, func(entry httpapi.StorageEntry) bool { return entry.Irregular })

	slices.SortFunc(entries, func(a, b httpapi.StorageEntry) int { return strings.Compare(a.Name, b.Name) })

	start := sort.Search(len(entries), func(i int) bool { return entries[i].Name > params.Cursor })
	end := min(start+storageapi.MaxListEntries, len(entries))

	page := storageapi.Listing{Entries: make([]storageapi.ListEntry, 0, end-start)}
	for _, entry := range entries[start:end] {
		page.Entries = append(page.Entries, listEntryOf(entry))
	}

	if end < len(entries) {
		page.Next = entries[end-1].Name
	}

	return page, nil
}

// listEntryOf is one entry as the module wire carries it. A folder carries no
// size: the bytes a directory takes on a disk say nothing about what it holds.
func listEntryOf(entry httpapi.StorageEntry) storageapi.ListEntry {
	listed := storageapi.ListEntry{
		Name:   entry.Name,
		Dir:    entry.Folder,
		MTime:  entry.Modified.Unix(),
		SHA256: entry.SHA256,
	}

	if !entry.Folder {
		listed.Size = entry.Size
	}

	return listed
}

// Read answers one range of one file.
//
// The length is read first, so the range is held against a length this server
// read rather than against one the module claimed, and a directory is refused
// before anything is opened. It asks for the length alone and not for Stat: a
// module walks a file piece by piece, and Stat parses the head of a print file
// to describe it, which this caller would pay for on every piece.
//
// The buffer is the shorter of what was asked for and what is left of the file,
// so nothing here allocates in proportion to a file somebody else put in a
// storage.
func (m moduleStorage) Read(
	ctx context.Context, params storageapi.ReadParams,
) (storageapi.Chunk, error) {
	size, content, err := m.openFile(ctx, params.Principal, params.Handle, params.Storage, params.Path)
	if err != nil {
		return storageapi.Chunk{}, err
	}

	defer func() { _ = content.Close() }() // the read error below is the one worth reporting

	wanted := params.Length
	if wanted == 0 {
		wanted = storageapi.DefaultReadBytes
	}

	left := size - params.Offset
	if left <= 0 {
		return storageapi.Chunk{Size: size, EOF: true}, nil
	}

	if int64(wanted) > left {
		wanted = int(left)
	}

	read, err := readRange(content, params.Offset, wanted)
	if err != nil {
		return storageapi.Chunk{}, err
	}

	return storageapi.Chunk{
		Data: read,
		Size: size,
		EOF:  params.Offset+int64(len(read)) >= size,
	}, nil
}

// fetchBuffer is what one fetch copies through at a time. The stream splits
// it into data frames and waits for credit, so this bounds memory and not the
// size of the range.
const fetchBuffer = 256 << 10

// Fetch writes one range of one file to the stream the call arrived on. It
// takes what Read takes, and allocates one buffer whatever the range.
func (m moduleStorage) Fetch(
	ctx context.Context, params storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	size, content, err := m.openFile(ctx, params.Principal, params.Handle, params.Storage, params.Path)
	if err != nil {
		return storageapi.Fetched{}, err
	}

	defer func() { _ = content.Close() }() // the copy error below is the one worth reporting

	wanted := int64(params.Length)
	if wanted == 0 {
		wanted = storageapi.MaxFetchBytes
	}

	wanted = max(0, min(wanted, size-params.Offset))
	if wanted == 0 {
		return storageapi.Fetched{Size: size}, nil
	}

	if _, err = content.Seek(params.Offset, io.SeekStart); err != nil {
		return storageapi.Fetched{}, fmt.Errorf("%w: reach offset %d: %w", storageapi.ErrUnreadable, params.Offset, err)
	}

	sent, err := io.CopyBuffer(w, io.LimitReader(content, wanted), make([]byte, fetchBuffer))
	if err != nil {
		return storageapi.Fetched{}, fmt.Errorf("%w: send %d bytes at %d: %w",
			storageapi.ErrUnreadable, wanted, params.Offset, err)
	}

	return storageapi.Fetched{Size: size, Length: int(sent)}, nil
}

// openFile opens the file a call names, through a handle or as the
// principal, and answers its length. A directory reads as the file that is
// not there: it will hand out no bytes on the next call either, so telling a
// module to come back leaves it asking for the same path for ever.
func (m moduleStorage) openFile(
	ctx context.Context, principal, handle, storage, path string,
) (int64, io.ReadSeekCloser, error) {
	if handle != "" {
		held, info, err := m.byHandle(ctx, handle)
		if err != nil {
			return 0, nil, err
		}

		content, err := m.raw.Open(ctx, held.storage, held.path)
		if err != nil {
			return 0, nil, m.refusal(ctx, err)
		}

		return info.Size, content, nil
	}

	who, err := m.as(principal)
	if err != nil {
		return 0, nil, err
	}

	// The length is read first, so the range is held against a length this
	// server read rather than one the module claimed. It asks for the length
	// alone and not for Stat, which parses the head of a print file.
	extent, err := m.stores.FileExtent(ctx, who, storage, path)
	if err != nil {
		return 0, nil, m.refusal(ctx, err)
	}

	if extent.Folder {
		return 0, nil, fmt.Errorf("%w: %s", storageapi.ErrNoFile, absentFileMessage)
	}

	object, err := m.stores.Open(ctx, who, storage, path)
	if err != nil {
		return 0, nil, m.refusal(ctx, err)
	}

	return extent.Size, object.Content, nil
}

// byHandle resolves a handle to the file it names, as the storage describes
// it at the time of the call. A handle this instance was not given, one that
// ended, and a file whose size or time differs from the version the handle
// was made for all read as the file that is not there. The description is the
// plain one: a handle is how the core asks a module to describe a file, so
// this never asks a module in turn.
func (m moduleStorage) byHandle(ctx context.Context, token string) (fileHandle, storageapi.FileInfo, error) {
	held, ok := m.handles.resolve(m.standing.principal, token)
	if !ok {
		return fileHandle{}, storageapi.FileInfo{}, fmt.Errorf("%w: %s", storageapi.ErrNoFile, absentFileMessage)
	}

	info, err := m.raw.Stat(ctx, held.storage, held.path)
	if err != nil {
		return fileHandle{}, storageapi.FileInfo{}, m.refusal(ctx, err)
	}

	if info.IsDir || info.Size != held.size || info.MTime != held.modified {
		return fileHandle{}, storageapi.FileInfo{}, fmt.Errorf("%w: %s", storageapi.ErrNoFile, absentFileMessage)
	}

	return held, info, nil
}

// readRange seeks to offset and reads wanted bytes, or what is left of the
// file. A file that shrank between the stat and this read answers what is
// there rather than failing.
func readRange(content io.ReadSeeker, offset int64, wanted int) ([]byte, error) {
	if _, err := content.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: reach offset %d: %w", storageapi.ErrUnreadable, offset, err)
	}

	buffer := make([]byte, wanted)

	read, err := io.ReadFull(content, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("%w: read %d bytes at %d: %w",
			storageapi.ErrUnreadable, wanted, offset, err)
	}

	return buffer[:read], nil
}

// refusal is what a module is told when the storage plane would not answer.
//
// There are three answers and no others. A caller the authorization graph
// refused is told so, and that refusal names the action and no storage. A file
// that is settled as out of reach reads absentFileMessage: the storage is not
// declared, it holds no such file, it will not read the file, or the path
// leaves it. Anything that may work on the next call reads
// unreadableNowMessage: the storage is not serving, the call was cancelled or
// ran out of time, or this server failed in a way it has no answer for.
//
// The split matters because a module acts on it. A module told a file is not
// there takes it off its list; one told to come back keeps it. The plane's own
// error is in neither answer, because its text carries the storage name and
// this server's own file paths.
func (m moduleStorage) refusal(ctx context.Context, err error) error {
	var raised *fault.Fault
	if errors.As(err, &raised) && raised.Code() == authz.CodeAccessDenied() {
		return fmt.Errorf("%w: %w", storageapi.ErrDenied, err)
	}

	m.logs().refused(ctx, "a module was not given a file", m.standing.principal, err)

	if absentInAStorage(err) {
		return fmt.Errorf("%w: %s", storageapi.ErrNoFile, absentFileMessage)
	}

	return fmt.Errorf("%w: %s", storageapi.ErrUnreadable, unreadableNowMessage)
}

// The two sentences a module reads. Neither names a storage, a path or a
// reason: a sentence that did would let a module send one name after another
// and collect the storages this deployment declares. The reason is in this
// server's log.
const (
	absentFileMessage    = "no file of that name is in that storage"
	unreadableNowMessage = "that storage cannot be read right now"
)

// absentInAStorage reports the failures that are settled rather than passing.
// Every other failure may work on the next call.
func absentInAStorage(err error) bool {
	for _, absent := range []*fault.Fault{
		fault.StorageNotFound.New(""),
		fault.StorageFileNotFound.New(""),
		fault.StorageInvalidPath.New(""),
		fault.StorageNotReadable.New(""),
	} {
		if errors.Is(err, absent) {
			return true
		}
	}

	return errors.Is(err, ErrNoSuchStorage) || errors.Is(err, ErrStorageCannotRead)
}

// logs is the shared writer, or one that discards for a moduleStorage a test
// built by hand.
func (m moduleStorage) logs() *refusalLog {
	if m.log == nil {
		return newRefusalLog(nil)
	}

	return m.log
}

// refusalLogInterval is how often one module's refusals reach the log.
const refusalLogInterval = time.Minute

// refusalLog writes why a module was refused, at most one line per module in
// each refusalLogInterval.
//
// The answer a module reads names nothing, so this log is the only place the
// reason exists and an operator needs it. A module can also ask for one name
// after another, and a line per ask would fill an operator's disk. So the line
// that opens an interval carries how many asks went unwritten before it.
//
// The map is keyed by the instance's own principal, so it grows with the
// modules an operator installed and not with what any of them sends.
type refusalLog struct {
	log *slog.Logger

	mu      sync.Mutex
	written map[string]time.Time
	skipped map[string]int
}

// newRefusalLog returns a writer ready to use. A nil logger discards.
func newRefusalLog(logger *slog.Logger) *refusalLog {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &refusalLog{
		log:     logger,
		written: map[string]time.Time{},
		skipped: map[string]int{},
	}
}

// refused writes one line, or counts this refusal against the line that opens
// the next interval.
func (r *refusalLog) refused(ctx context.Context, message, principal string, err error) {
	r.mu.Lock()

	now := time.Now()

	if written, seen := r.written[principal]; seen && now.Sub(written) < refusalLogInterval {
		r.skipped[principal]++
		r.mu.Unlock()

		return
	}

	r.written[principal] = now
	unwritten := r.skipped[principal]
	delete(r.skipped, principal)

	r.mu.Unlock()

	r.log.InfoContext(ctx, message,
		"principal", principal, "error", err.Error(), "unwritten_refusals", unwritten)
}
