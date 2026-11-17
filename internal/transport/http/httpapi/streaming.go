package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"time"
)

// The server's read and write deadlines bound a request that stalls, and a file
// is the one body that may rightly take longer than either. A deadline moved
// on every chunk still cuts off a peer that has stopped and lets a slow one
// finish. The event stream does the same when it re-arms its write deadline on
// every flush (events.go).
//
// The window is per chunk and not per request, so a transfer of any length is
// served and a peer that says nothing for this long is still cut off.
const transferIdleWindow = 60 * time.Second

// ErrNoConnectionControl reports a handler that cannot reach the connection's
// deadlines, which is a server this package did not build.
var ErrNoConnectionControl = errors.New("httpapi: the handler cannot reach the connection")

// uploadBody is the request body, bounded and with both connection deadlines
// moved as the bytes arrive.
//
// The bound is still the middleware's: this reads what MaxBytesReader already
// wrapped, so a body over the limit is refused with the limit in the refusal
// wherever it is read. What this adds is time, which the middleware cannot
// give.
//
// Both deadlines are moved, and not the read one alone. The server's
// WriteTimeout is armed when the request arrives, so an upload longer than it
// would land on disk and then fail to answer. The client would read a reset
// where the upload succeeded.
func uploadBody(ctx context.Context) (io.Reader, error) {
	request := requestFrom(ctx)
	if request == nil {
		return nil, ErrNoConnectionControl
	}

	writer := writerFrom(ctx)
	if writer == nil {
		return request.Body, nil
	}

	control := http.NewResponseController(writer)

	// Cleared, not lengthened: the per-chunk deadlines below replace them, and
	// a deadline left in place would still cut the request at whatever the
	// server configured.
	if err := control.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, fmt.Errorf("lift the read deadline: %w", err)
	}

	if err := control.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, fmt.Errorf("lift the write deadline: %w", err)
	}

	return &progressReader{inner: request.Body, control: control}, nil
}

// progressReader moves both connection deadlines forward on every chunk that
// arrives.
//
// The write deadline moves with the read one because the response is written
// after the last chunk. An upload of any real size takes longer than a window
// measured from the start of the request.
type progressReader struct {
	inner   io.Reader
	control *http.ResponseController
}

func (r *progressReader) Read(out []byte) (int, error) {
	// Armed before the read and not after it, so the window bounds waiting for
	// the next chunk and not the gap between two that already arrived.
	window := time.Now().Add(transferIdleWindow)

	if err := r.control.SetReadDeadline(window); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, fmt.Errorf("arm the read deadline: %w", err)
	}

	if err := r.control.SetWriteDeadline(window); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, fmt.Errorf("arm the write deadline: %w", err)
	}

	read, err := r.inner.Read(out)
	if err != nil && !errors.Is(err, io.EOF) {
		return read, fmt.Errorf("read the request body: %w", err)
	}

	return read, err //nolint:wrapcheck // io.EOF reaches the caller as itself.
}

// liftWriteDeadline clears the connection's write deadline for a handler whose
// response is written only once the work it started has finished.
//
// A printer transfer runs for as long as the file and the machine take, and the
// server's WriteTimeout is armed when the request arrives. There is nothing to
// re-arm per chunk here: the bytes leave over the printer's own connection, so
// this handler makes no progress an idle window could measure, and the caller's
// context is what bounds the wait.
func liftWriteDeadline(ctx context.Context) error {
	writer := writerFrom(ctx)
	if writer == nil {
		return nil
	}

	err := http.NewResponseController(writer).SetWriteDeadline(time.Time{})
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("lift the write deadline: %w", err)
	}

	return nil
}

// serveDownload writes one object to the client, with a length, a modification
// time and range support, and moves the write deadline as the bytes leave.
//
// http.ServeContent answers a Range header, so the standard library serves a
// client that resumes an interrupted download.
func serveDownload(
	writer http.ResponseWriter, request *http.Request,
	name string, modified time.Time, content io.ReadSeeker,
) {
	control := http.NewResponseController(writer)

	if err := control.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		// Nothing to do about it and nothing to say to the client: the write
		// still works, and the server's own deadline is what then bounds it.
		_ = err
	}

	// headMiddleware handed this handler a GET. ServeContent answers a HEAD with
	// the headers alone, but only when it is told it is one, and it sniffs the
	// type from the first bytes when none is set.
	if headOf(request.Context()) {
		request = request.Clone(request.Context())
		request.Method = http.MethodHead

		if writer.Header().Get("Content-Type") == "" {
			kind := mime.TypeByExtension(path.Ext(name))
			if kind == "" {
				kind = "application/octet-stream"
			}

			writer.Header().Set("Content-Type", kind)
		}
	}

	http.ServeContent(writer, request, name, modified, &progressSeeker{
		inner:   content,
		writer:  writer,
		control: control,
	})
}

// progressSeeker moves the write deadline forward on every chunk ServeContent
// reads out of it.
//
// The deadline is moved here and not around the writer, because ServeContent
// owns the writing. This type sees only the reads, and ServeContent makes one
// read per chunk it writes.
type progressSeeker struct {
	inner   io.ReadSeeker
	writer  http.ResponseWriter
	control *http.ResponseController
}

func (s *progressSeeker) Read(out []byte) (int, error) {
	if err := s.control.SetWriteDeadline(time.Now().Add(transferIdleWindow)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return 0, fmt.Errorf("arm the write deadline: %w", err)
	}

	read, err := s.inner.Read(out)
	if err != nil && !errors.Is(err, io.EOF) {
		return read, fmt.Errorf("read the object: %w", err)
	}

	return read, err //nolint:wrapcheck // io.EOF reaches ServeContent as itself.
}

func (s *progressSeeker) Seek(offset int64, whence int) (int64, error) {
	at, err := s.inner.Seek(offset, whence)
	if err != nil {
		return at, fmt.Errorf("seek the object: %w", err)
	}

	return at, nil
}
