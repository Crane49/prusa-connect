package app

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// stores is the server's own storage: what an operator declared under
// resources, reached through the supervisor.
//
// Every operation resolves the instance by name and calls into it through
// supervisor.Call, or supervisor.Stream for a write, because a storage type is
// third-party code and the host must supervise every call into one. The
// provider contract carries a context. A supervised call bounds the caller's
// own context, so a provider that reaches a remote is cancelled with the
// request that wanted it.
type stores struct {
	host *resourcehost.Host
}

// provider resolves one declared storage to the contract it serves.
func (s stores) provider(instance string) (*supervisor.Instance[any], error) {
	target, err := ref.New(ref.KindStorage, instance)
	if err != nil {
		return nil, noSuchStorage(instance, err)
	}

	running, found := s.host.Instance(target)
	if !found {
		return nil, noSuchStorage(instance, ErrNoSuchStorage)
	}

	return running, nil
}

// call runs one provider operation under supervision, mapping a failure onto a
// code the caller can act on.
//
// The function it takes receives the supervised context and must not use the
// caller's, so a provider that honours a deadline sees the budget the
// supervisor set.
func call[R any](
	ctx context.Context, s stores, instance, name, op string,
	fn func(context.Context, storageapi.Provider) (R, error),
) (R, error) {
	var zero R

	running, err := s.provider(instance)
	if err != nil {
		return zero, err
	}

	out, err := supervisor.Call(ctx, running, op,
		func(callCtx context.Context, target any) (R, error) {
			provider, ok := target.(storageapi.Provider)
			if !ok {
				return zero, ErrStorageCannotRead
			}

			return fn(callCtx, provider)
		})
	if err != nil {
		return zero, stagingFault(instance, name, op, err)
	}

	return out, nil
}

// List returns the entries directly inside one directory of a storage. The
// empty path is the storage's own root.
func (s stores) List(ctx context.Context, instance, path string) ([]storageapi.FileInfo, error) {
	return call(ctx, s, instance, path, "list",
		func(callCtx context.Context, provider storageapi.Provider) ([]storageapi.FileInfo, error) {
			return provider.List(callCtx, path)
		})
}

// Stat describes one entry.
func (s stores) Stat(ctx context.Context, instance, path string) (storageapi.FileInfo, error) {
	return call(ctx, s, instance, path, "stat",
		func(callCtx context.Context, provider storageapi.Provider) (storageapi.FileInfo, error) {
			return provider.Stat(callCtx, path)
		})
}

// Open returns an open handle to one object, for a caller that will serve its
// bytes. The caller closes it.
//
// The handle outlives the supervised call that produced it. The provider
// contract allows that, and serving a range needs it, because a download
// seeks. Keeping the supervised call open would hold it for the length of a
// transfer over the client's network.
//
// A provider with no local file is served through its range reader, so a
// store that keeps its objects somewhere other than a disk still answers a
// Range request. A provider with neither cannot be downloaded from: the call
// fails, and the file is not read whole into memory.
func (s stores) Open(ctx context.Context, instance, path string) (io.ReadSeekCloser, error) {
	size, err := s.Stat(ctx, instance, path)
	if err != nil {
		return nil, err
	}

	return call(ctx, s, instance, path, "open",
		func(callCtx context.Context, provider storageapi.Provider) (io.ReadSeekCloser, error) {
			if local, ok := provider.(storageapi.LocalFiles); ok {
				return local.OpenLocal(callCtx, path)
			}

			if ranged, ok := provider.(storageapi.RangeReader); ok {
				return &rangeFile{
					open: func(openCtx context.Context, offset int64) (io.ReadCloser, error) {
						return ranged.OpenRange(openCtx, path, offset, -1)
					},
					size: size.Size,
				}, nil
			}

			return nil, ErrStorageCannotRead
		})
}

// Save writes a stream into a storage under the given name and reports what
// landed, including the digest the provider took of the bytes it wrote.
//
// The provider applies the extension allowlist and publishes the object only
// on Commit, so a transfer that fails part way leaves no half file under the
// caller's name.
//
// Only Create is under the supervisor's call budget. A large file takes longer
// than that budget, so the copy and Commit run under the caller's ctx, which
// carries the route's stream budget and ends when the client goes away. The
// bulkhead slot stays taken until release is called after the copy.
func (s stores) Save(
	ctx context.Context, instance, path string, body io.Reader,
) (storageapi.WriteResult, error) {
	running, err := s.provider(instance)
	if err != nil {
		return storageapi.WriteResult{}, err
	}

	writer, release, err := supervisor.Stream(ctx, running, "save",
		func(callCtx context.Context, target any) (storageapi.Writer, error) {
			provider, ok := target.(storageapi.Provider)
			if !ok {
				return nil, ErrStorageCannotRead
			}

			return provider.Create(callCtx, path)
		})
	if err != nil {
		return storageapi.WriteResult{}, stagingFault(instance, path, "save", err)
	}

	result, blamed, err := transfer(ctx, running, writer, body)
	release(blamed)

	if err != nil {
		return storageapi.WriteResult{}, stagingFault(instance, path, "save", err)
	}

	return result, nil
}

// transfer copies body into writer and commits it, aborting on any failure so
// nothing is published under the name. The writer is third-party code running
// outside the supervised call, so a panic out of it is recovered here.
//
// It returns two errors. err goes to the caller. blamed is what the supervisor
// records against the storage: only a failure of the writer itself, or the
// caller's ctx ending. A body that fails to read is the client's failure.
// Counting it would let a client that keeps dropping open the storage's
// breaker.
func transfer(
	ctx context.Context, running *supervisor.Instance[any], writer storageapi.Writer, body io.Reader,
) (result storageapi.WriteResult, blamed, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = supervisor.Recovered(running, "save", rec)
			blamed = err
		}
	}()

	sink := &blamingWriter{writer: writer}

	if _, err = io.Copy(sink, body); err != nil {
		// The abort is best effort. The caller gets the copy's failure, and a
		// cleanup failure must not hide it.
		aborted := abort(ctx, running, writer)

		return storageapi.WriteResult{}, cmp.Or(sink.failed, aborted, ctx.Err()), err
	}

	if result, err = writer.Commit(ctx); err != nil {
		_ = abort(ctx, running, writer)

		return storageapi.WriteResult{}, err, err
	}

	return result, nil, nil
}

// abort discards a write whose caller may already have gone. It runs on a
// context that outlives the caller's, bounded by the call budget, so a remote
// store still removes what it staged after a client drops.
func abort(ctx context.Context, running *supervisor.Instance[any], writer storageapi.Writer) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), running.Policy().CallTimeout)
	defer cancel()

	return writer.Abort(cleanup)
}

// blamingWriter records the first error the storage's writer returns, so a
// failed copy can tell the writer's failure from the body's.
type blamingWriter struct {
	writer storageapi.Writer
	failed error
}

func (w *blamingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err != nil && w.failed == nil {
		w.failed = err
	}

	return n, err
}

// Delete removes one entry, and everything under it when it is a directory.
func (s stores) Delete(ctx context.Context, instance, path string) error {
	_, err := call(ctx, s, instance, path, "delete",
		func(callCtx context.Context, provider storageapi.Provider) (struct{}, error) {
			return struct{}{}, provider.Delete(callCtx, path)
		})

	return err
}

// DeleteMany removes several entries in one call. The provider attempts every
// path, so one name that is already gone does not keep the others in place,
// and a batch where any of them stayed reports ErrDeletePartial.
//
// The whole batch is one supervised call and takes one call budget. A caller
// with more paths than fit in it sends them in several batches. The provider
// gets its own copy of the slice, because a call that runs past its budget
// still holds it after the caller has moved on.
func (s stores) DeleteMany(ctx context.Context, instance string, paths []string) error {
	_, err := call(ctx, s, instance, "", "delete_many",
		func(callCtx context.Context, provider storageapi.Provider) (struct{}, error) {
			return struct{}{}, provider.DeleteMany(callCtx, slices.Clone(paths))
		})

	return err
}

// downloadURL asks a storage for a URL a client can fetch one object from
// without this server relaying the bytes. ok is false for a storage that
// presigns nothing and for an object it will not presign, and the caller then
// serves the bytes itself.
//
// The provider writes the URL, so a value that is not an absolute http or
// https URL is treated as no URL. A redirect to any other scheme would send
// the caller's browser wherever the storage's type chose.
func (s stores) downloadURL(ctx context.Context, instance, path string) (string, bool, error) {
	type presigned struct {
		url string
		ok  bool
	}

	out, err := call(ctx, s, instance, path, "download_url",
		func(callCtx context.Context, provider storageapi.Provider) (presigned, error) {
			signer, has := provider.(storageapi.Presigner)
			if !has {
				return presigned{}, nil
			}

			location, ok := signer.DownloadURL(callCtx, path)

			return presigned{url: location, ok: ok}, nil
		})
	if err != nil {
		return "", false, err
	}

	if !out.ok || !fetchable(out.url) {
		return "", false, nil
	}

	return out.url, true, nil
}

// fetchable reports whether a URL a storage handed out is one a client may be
// redirected to.
func fetchable(location string) bool {
	parsed, err := url.Parse(location)
	if err != nil {
		return false
	}

	return (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

// CreateFolder makes a directory and its parents.
func (s stores) CreateFolder(ctx context.Context, instance, path string) error {
	_, err := call(ctx, s, instance, path, "mkdir",
		func(callCtx context.Context, provider storageapi.Provider) (struct{}, error) {
			return struct{}{}, provider.Mkdir(callCtx, path)
		})

	return err
}

// Move renames an entry inside one storage. It does not cross storages: two
// providers have no shared namespace and no atomic step between them.
//
// A provider that serves no rename is refused here. A copy and a delete are
// not the same operation as a rename, so the move is not emulated with them.
func (s stores) Move(ctx context.Context, instance, from, to string) error {
	_, err := call(ctx, s, instance, from, "rename",
		func(callCtx context.Context, provider storageapi.Provider) (struct{}, error) {
			renamer, ok := provider.(storageapi.Renamer)
			if !ok {
				return struct{}{}, fmt.Errorf("%w: rename", storageapi.ErrNotSupported)
			}

			return struct{}{}, renamer.Rename(callCtx, from, to)
		})

	return err
}

// Serving is the declared storages that are running, by name.
//
// It reads the supervision plane and not the declaration. A caller cannot
// browse a storage that was declared and failed to start, and every operation
// on its name would be refused.
func (s stores) Serving() []string {
	running := s.host.Supervising(ref.KindStorage)

	names := make([]string, 0, len(running))

	for instance, live := range running {
		if !live.State().Serving() {
			continue
		}

		names = append(names, instance.ID())
	}

	return names
}

// capabilitiesOf reports what one storage's type can do, so a caller is not
// offered an operation that cannot work.
//
// It probes the provider's interfaces and reads no declaration, because a
// declaration could disagree with the implementation.
func (s stores) capabilitiesOf(ctx context.Context, instance string) (storageapi.Caps, error) {
	return call(ctx, s, instance, "", "capabilities",
		func(_ context.Context, provider storageapi.Provider) (storageapi.Caps, error) {
			return storageapi.Probe(provider), nil
		})
}

// allowedExtensionsOf is the upload allowlist a storage enforces, and an empty
// list for one that accepts any name or does not say.
func (s stores) allowedExtensionsOf(ctx context.Context, instance string) ([]string, error) {
	return call(ctx, s, instance, "", "allowed_extensions",
		func(_ context.Context, provider storageapi.Provider) ([]string, error) {
			policy, ok := provider.(storageapi.ExtensionPolicy)
			if !ok {
				return []string{}, nil
			}

			return append([]string{}, policy.AllowedExtensions()...), nil
		})
}

// displayNameOf is the label a storage's type gives itself.
func (s stores) displayNameOf(ctx context.Context, instance string) (string, error) {
	name, err := call(ctx, s, instance, "", "display_name",
		func(_ context.Context, provider storageapi.Provider) (string, error) {
			return provider.DisplayName(), nil
		})
	if err != nil {
		return "", fmt.Errorf("read the storage's name: %w", err)
	}

	return name, nil
}

// rangeFile serves a provider that has no file handle to give but can answer a
// byte range. With it an HTTP Range request works against a store that keeps
// its objects somewhere other than a disk.
//
// Seek changes the offset and opens nothing. The range is opened on the next
// read, so a caller that seeks twice before reading costs one open.
// http.ServeContent seeks that way when it measures a file before serving it.
type rangeFile struct {
	open   func(ctx context.Context, offset int64) (io.ReadCloser, error)
	size   int64
	offset int64
	body   io.ReadCloser
}

// Read serves from the current offset, opening the range when there is nothing
// open at it.
func (f *rangeFile) Read(p []byte) (int, error) {
	if f.offset >= f.size {
		return 0, io.EOF
	}

	if f.body == nil {
		// The context is the background one. This handle outlives the
		// supervised call that made it, as a local file handle does, and the
		// caller that serves the bytes ends the read by closing it.
		body, err := f.open(context.Background(), f.offset)
		if err != nil {
			return 0, err
		}

		f.body = body
	}

	read, err := f.body.Read(p)
	f.offset += int64(read)

	return read, err //nolint:wrapcheck // the provider's read error is what the caller is owed.
}

// Seek moves the offset and drops whatever range was open at the old one.
func (f *rangeFile) Seek(offset int64, whence int) (int64, error) {
	var next int64

	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = f.offset + offset
	case io.SeekEnd:
		next = f.size + offset
	default:
		return 0, fmt.Errorf("%w: seek whence %d", storageapi.ErrNotSupported, whence)
	}

	if next < 0 {
		return 0, fmt.Errorf("%w: seek to %d", storageapi.ErrInvalidPath, next)
	}

	if next != f.offset {
		f.closeBody()
		f.offset = next
	}

	return next, nil
}

// Close releases whatever range is open.
func (f *rangeFile) Close() error {
	f.closeBody()

	return nil
}

// closeBody drops the open range. A close failure on a read handle is
// ignored, because the caller cannot act on it.
func (f *rangeFile) closeBody() {
	if f.body == nil {
		return
	}

	_ = f.body.Close()
	f.body = nil
}
