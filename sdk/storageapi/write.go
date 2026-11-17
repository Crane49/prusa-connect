package storageapi

// Writing a file into a storage over the module wire. The call and the bytes
// of the file travel on a stream of their own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodWrite puts one file into a storage and replaces any file of that name.
//
// The module makes the call on a new stream. The host writes one byte on the
// stream when it is ready. The module then writes WriteParams.Size bytes and
// ends its half of the stream. The host publishes the file only when every
// byte arrived. A failed write leaves the old file, or no file.
const MethodWrite wirerpc.Method = "storage.write"

// MaxWriteBytes is the largest file one write puts into a storage.
const MaxWriteBytes int64 = 4 << 30

// WriteParams is the parameters of MethodWrite.
type WriteParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`

	Storage string `json:"storage"`
	// Path is the file to write, relative to the storage's root. Missing
	// parent folders are created.
	Path string `json:"path"`
	// Size is how many bytes follow the call, at least one. The host refuses
	// a stream with fewer or more bytes.
	Size int64 `json:"size"`
}

// Validate checks one write against the limits of this contract.
func (p WriteParams) Validate() error {
	// A write names a storage and a path, never a read handle.
	if err := validTarget(p.Principal, "", p.Storage, p.Path); err != nil {
		return err
	}

	if p.Size < 1 || p.Size > MaxWriteBytes {
		return fmt.Errorf("%w: the size is %d, and a write carries 1 to %d bytes",
			ErrInvalidRange, p.Size, MaxWriteBytes)
	}

	return nil
}

// Written is the answer of MethodWrite: the file as the storage has it.
type Written struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// SHA256 is the digest the storage computed while it wrote, lowercase hex.
	// It is empty for a storage that computes none.
	SHA256 string `json:"sha256,omitempty"`
}

// ErrShortWrite reports a stream with fewer or more bytes than the call
// named. The host published nothing.
var ErrShortWrite = errors.New("storageapi: the stream did not carry the size the call named")

// CodeShortWrite is the wire code of ErrShortWrite.
const CodeShortWrite = 5005

// Write sends one file to a storage. It reads params.Size bytes from body and
// returns when the storage has published the file. It needs the
// write_storage permission. The client's session must open streams, or Write
// returns ErrNoStream.
func (c *Client) Write(ctx context.Context, params WriteParams, body io.Reader) (Written, error) {
	if err := params.Validate(); err != nil {
		return Written{}, fmt.Errorf("storageapi: %s: %w", MethodWrite, err)
	}

	opener, ok := c.conn.(Opener)
	if !ok {
		return Written{}, fmt.Errorf("storageapi: %s: %w", MethodWrite, ErrNoStream)
	}

	stream, err := opener.Open()
	if err != nil {
		return Written{}, fmt.Errorf("storageapi: %s: open a stream: %w", MethodWrite, err)
	}

	// Send the bytes in a goroutine, and only after the host's ready byte.
	// An end of stream sent earlier could reach the host before the call.
	sent := make(chan error, 1)

	go func() { sent <- send(stream, body, params.Size) }()

	answer, called := stream.Call(ctx, MethodWrite, params)

	// Close the stream so that a copy still waiting for credit returns.
	_ = stream.Close()
	copied := <-sent

	if errors.Is(copied, ErrShortWrite) {
		return Written{}, fmt.Errorf("storageapi: %s: %w", MethodWrite, copied)
	}

	if called != nil {
		return Written{}, c.mapped(MethodWrite, called)
	}

	if copied != nil && !errors.Is(copied, wirerpc.ErrStreamClosed) {
		return Written{}, fmt.Errorf("storageapi: %s: send the bytes: %w", MethodWrite, copied)
	}

	var written Written
	if err = json.Unmarshal(answer, &written); err != nil {
		return Written{}, fmt.Errorf("storageapi: %s: read the answer: %w", MethodWrite, err)
	}

	return written, nil
}

// ready is the byte the host writes on the stream when it has taken a write.
const ready = 1

// send waits for the host's ready byte, writes size bytes of body and ends
// this side of the stream. A body that ends early resets the stream.
func send(stream *wirerpc.Stream, body io.Reader, size int64) error {
	var held [1]byte
	if _, err := io.ReadFull(stream, held[:]); err != nil {
		return err
	}

	if _, err := io.CopyN(stream, body, size); err != nil {
		// The call ended first and closed the stream. Its answer says why.
		if errors.Is(err, wirerpc.ErrStreamClosed) {
			return err
		}

		_ = stream.Reset("the module could not read the whole file")

		return fmt.Errorf("%w: %w", ErrShortWrite, err)
	}

	return stream.CloseWrite()
}

func writeHandler(files Files) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params WriteParams

		if len(call.Params) == 0 {
			return nil, badParams(MethodWrite)
		}

		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, badParams(MethodWrite)
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		// A write made as a plain call has no bytes behind it.
		if call.Stream == nil {
			return nil, refusal(fmt.Errorf("%w: %s is made on a stream", ErrShortWrite, MethodWrite))
		}

		// The module sends the file only after it reads this byte, so the end
		// of its stream cannot arrive before the call.
		if _, err := call.Stream.Write([]byte{ready}); err != nil {
			return nil, refusal(fmt.Errorf("%w: %w", ErrShortWrite, err))
		}

		written, err := files.Write(ctx, params, &exactReader{from: call.Stream, left: params.Size})
		if err != nil {
			return nil, refusal(err)
		}

		return written, nil
	}
}

// exactReader reads left bytes and then reports the end. A stream with fewer
// or more bytes fails the read, so the storage publishes nothing.
type exactReader struct {
	from io.Reader
	left int64
}

func (e *exactReader) Read(p []byte) (int, error) {
	if e.left == 0 {
		var probe [1]byte

		extra, err := io.ReadFull(e.from, probe[:])
		if extra > 0 {
			return 0, fmt.Errorf("%w: more bytes followed", ErrShortWrite)
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("%w: %w", ErrShortWrite, err)
		}

		return 0, io.EOF
	}

	if int64(len(p)) > e.left {
		p = p[:e.left]
	}

	read, err := e.from.Read(p)
	e.left -= int64(read)

	if errors.Is(err, io.EOF) && e.left > 0 {
		return read, fmt.Errorf("%w: %d bytes are missing", ErrShortWrite, e.left)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return read, fmt.Errorf("%w: %w", ErrShortWrite, err)
	}

	return read, nil
}
