package storageapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module reads a large file through MethodFetch. The bytes travel as data
// frames on a stream of their own.

// ErrNoStream reports a client built over a session that cannot open a
// stream.
var ErrNoStream = errors.New("storageapi: the session opens no stream")

// Opener opens the stream that a fetch or a write sends its bytes on.
// *wirerpc.Conn implements it.
type Opener interface {
	Open() (*wirerpc.Stream, error)
}

// fetchWindow is the credit a fetch grants the host on its stream. It bounds
// what one fetch buffers.
const fetchWindow = 1 << 20

// Fetch reads one range of a file into p and returns how many bytes it read.
// The range is len(p) bytes from params.Offset, at most MaxFetchBytes.
// params.Length is ignored. A range past the end of the file reads what is
// there and returns io.EOF, as io.ReaderAt does.
func (c *Client) Fetch(ctx context.Context, params FetchParams, p []byte) (int, error) {
	params.Length = len(p)
	if err := params.Validate(); err != nil {
		return 0, fmt.Errorf("storageapi: %s: %w", MethodFetch, err)
	}

	opener, ok := c.conn.(Opener)
	if !ok {
		return 0, fmt.Errorf("storageapi: %s: %w", MethodFetch, ErrNoStream)
	}

	stream, err := opener.Open()
	if err != nil {
		return 0, fmt.Errorf("storageapi: %s: open a stream: %w", MethodFetch, err)
	}

	if err = stream.Grant(fetchWindow); err != nil {
		_ = stream.Close()

		return 0, fmt.Errorf("storageapi: %s: grant the window: %w", MethodFetch, err)
	}

	// The bytes arrive before the answer, on the same stream, so read them
	// in a goroutine while the call runs.
	type read struct {
		n   int
		err error
	}

	done := make(chan read, 1)

	go func() {
		n, readErr := io.ReadFull(stream, p)
		done <- read{n: n, err: readErr}
	}()

	raw, called := stream.Call(ctx, MethodFetch, params)

	// Every byte the host sent is buffered before its answer arrives. The
	// close only ends a read that waits for bytes that will not come.
	_ = stream.Close()
	got := <-done

	if called != nil {
		return got.n, c.mapped(MethodFetch, called)
	}

	var fetched Fetched
	if err = json.Unmarshal(raw, &fetched); err != nil {
		return got.n, fmt.Errorf("storageapi: %s: read the answer: %w", MethodFetch, err)
	}

	if got.n != fetched.Length {
		return got.n, fmt.Errorf("storageapi: %s: the host sent %d bytes and read %d arrived",
			MethodFetch, fetched.Length, got.n)
	}

	if got.n < len(p) {
		return got.n, io.EOF
	}

	return got.n, nil
}

// mapped turns a coded refusal back into the matching error of this package.
func (c *Client) mapped(method wirerpc.Method, err error) error {
	if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
		if sentinel := sentinelFor(coded.Code); sentinel != nil {
			return fmt.Errorf("%s: %w", method, &remote{sentinel: sentinel, message: coded.Message})
		}
	}

	return fmt.Errorf("storageapi: %s: %w", method, err)
}

// Target names one file: by a read handle, or by a storage and a path read as
// a principal, or as the module itself when Principal is empty.
type Target struct {
	Principal string
	Handle    string
	Storage   string
	Path      string
}

// File is one file of a storage, read at any offset. It is an io.ReaderAt.
// Each ReadAt is one or more fetches, bounded by the context given to Open.
type File struct {
	client *Client
	// ctx is the context of Open. It bounds every fetch.
	ctx    context.Context //nolint:containedctx // io.ReaderAt has no context, and a File lives for one call.
	target Target
	size   int64
}

// Open returns a reader over one file. It reads the size once. A file that
// changes afterwards is read as it is then, or reports ErrNoFile through a
// handle.
func (c *Client) Open(ctx context.Context, target Target) (*File, error) {
	info, err := c.Stat(ctx, StatParams(target))
	if err != nil {
		return nil, err
	}

	if info.IsDir {
		return nil, fmt.Errorf("storageapi: open: %w: the path names a directory", ErrNoFile)
	}

	return &File{client: c, ctx: ctx, target: target, size: info.Size}, nil
}

// Size is the length of the file when it was opened.
func (f *File) Size() int64 { return f.size }

// ReadAt reads len(p) bytes from off, in fetches of at most MaxFetchBytes.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("storageapi: read at %d: %w", off, ErrInvalidRange)
	}

	if off >= f.size {
		return 0, io.EOF
	}

	want := p
	if left := f.size - off; int64(len(want)) > left {
		want = want[:left]
	}

	read := 0

	for read < len(want) {
		piece := want[read:min(len(want), read+MaxFetchBytes)]

		n, err := f.client.Fetch(f.ctx, FetchParams{
			Principal: f.target.Principal, Handle: f.target.Handle,
			Storage: f.target.Storage, Path: f.target.Path, Offset: off + int64(read),
		}, piece)
		read += n

		if err != nil {
			return read, err
		}
	}

	if read < len(p) {
		return read, io.EOF
	}

	return read, nil
}
