package storageapi

// The module wire: how a module reads and writes the files of a storage over
// its session with the host. Paths have the same form as in Provider.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The calls a module makes to read a file of a storage and to list a folder.
const (
	// MethodStat describes one file: its length, its modification time and
	// its digest if the storage has one. For a file a slicer wrote, the answer
	// also has FileInfo.Print.
	MethodStat wirerpc.Method = "storage.stat"
	// MethodRead returns one range of one file, at most MaxReadBytes.
	MethodRead wirerpc.Method = "storage.read"
	// MethodList returns the entries of one folder, one page at a time, in
	// the order of their names.
	MethodList wirerpc.Method = "storage.list"
	// MethodFetch sends one range of one file as data frames on the stream
	// the call arrived on, and answers once the bytes are sent. It is the
	// read for a large file, up to MaxFetchBytes a call.
	MethodFetch wirerpc.Method = "storage.fetch"
)

const (
	// MaxReadBytes is the largest range one read returns. A larger length is
	// refused, not cut.
	MaxReadBytes = 512 << 10

	// DefaultReadBytes is the length of a read that names a length of zero.
	DefaultReadBytes = 64 << 10

	// MaxListEntries is the most entries in one page of a listing.
	MaxListEntries = 1000

	// MaxStorageBytes is the longest name of a storage.
	MaxStorageBytes = 128

	// MaxPathBytes is the longest path inside a storage.
	MaxPathBytes = 1024

	// MaxPrincipalBytes is the longest principal.
	MaxPrincipalBytes = 256

	// MaxFetchBytes is the largest range one fetch sends. A larger length is
	// refused, not cut.
	MaxFetchBytes = 16 << 20

	// MaxHandleBytes is the longest read handle.
	MaxHandleBytes = 64
)

// The wire codes of this contract's errors. They are outside the range
// JSON-RPC 2.0 reserves and outside the ranges of the other contracts.
const (
	// CodeNoFile is the wire code of ErrNoFile.
	CodeNoFile = 5000
	// CodeInvalidPath is the wire code of ErrInvalidPath. On the module wire
	// it reports a path or a cursor over MaxPathBytes.
	CodeInvalidPath = 5001
	// CodeInvalidRange is the wire code of ErrInvalidRange.
	CodeInvalidRange = 5002
	// CodeUnreadable is the wire code of ErrUnreadable.
	CodeUnreadable = 5003
	// CodeDenied is the wire code of ErrDenied.
	CodeDenied = 5004
)

// The errors of the module wire. A module inspects them with errors.Is.
var (
	// ErrNoFile reports an unknown storage, a storage that is not running, a
	// missing file, a path that names a directory and a path that leaves the
	// storage. They share one error so that a module cannot learn which
	// storages and files exist.
	ErrNoFile = errors.New("storageapi: no such storage or file")
	// ErrInvalidRange reports an offset below zero, or a length or a size
	// over the limit of the method.
	ErrInvalidRange = errors.New("storageapi: the range is outside the published bounds")
	// ErrUnreadable reports a storage that is not serving, or one whose type
	// cannot give a file. A later call may succeed.
	ErrUnreadable = errors.New("storageapi: that path cannot be read")
	// ErrDenied reports a call the host refused: a principal the host did not
	// give this module in a request it is still serving, or work outside the
	// permissions of the module's instance.
	ErrDenied = errors.New("storageapi: the instance may not do that")
)

// StatParams is the parameters of MethodStat.
type StatParams struct {
	// Principal is who the call is made for, as the host gave it to the
	// module in a request. Empty for the module's own work.
	Principal string `json:"principal,omitempty"`
	// Handle is a read handle the host gave the module for one file. It
	// replaces Principal, Storage and Path.
	Handle string `json:"handle,omitempty"`

	// Storage is the name of the storage.
	Storage string `json:"storage,omitempty"`
	// Path is the file inside that storage, relative to its root.
	Path string `json:"path,omitempty"`
}

// Validate checks one stat against the limits of this contract. The client
// and the host both call it.
func (p StatParams) Validate() error {
	return validTarget(p.Principal, p.Handle, p.Storage, p.Path)
}

// ReadParams is the parameters of MethodRead. Offset counts from the start of
// the file. A Length of zero means DefaultReadBytes. A Length over
// MaxReadBytes is refused.
type ReadParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`
	// Handle is a read handle, in place of Principal, Storage and Path.
	Handle string `json:"handle,omitempty"`

	Storage string `json:"storage,omitempty"`
	Path    string `json:"path,omitempty"`

	Offset int64 `json:"offset,omitempty"`
	Length int   `json:"length,omitempty"`
}

// Validate checks one read against the limits of this contract.
func (p ReadParams) Validate() error {
	if err := validTarget(p.Principal, p.Handle, p.Storage, p.Path); err != nil {
		return err
	}

	return validRange(p.Offset, p.Length, MaxReadBytes)
}

// FetchParams is the parameters of MethodFetch: the fields of ReadParams,
// with a Length of up to MaxFetchBytes. A Length of zero means MaxFetchBytes.
type FetchParams ReadParams

// Validate checks one fetch against the limits of this contract.
func (p FetchParams) Validate() error {
	if err := validTarget(p.Principal, p.Handle, p.Storage, p.Path); err != nil {
		return err
	}

	return validRange(p.Offset, p.Length, MaxFetchBytes)
}

// Fetched is the answer of MethodFetch: the length of the whole file, and how
// many bytes were sent.
type Fetched struct {
	Size   int64 `json:"size"`
	Length int   `json:"length"`
}

// validRange checks an offset and a length against the limit of one method.
func validRange(offset int64, length, limit int) error {
	switch {
	case offset < 0:
		return fmt.Errorf("%w: the offset is %d", ErrInvalidRange, offset)
	case length < 0:
		return fmt.Errorf("%w: the length is %d", ErrInvalidRange, length)
	case length > limit:
		return fmt.Errorf("%w: the length is %d and the maximum is %d", ErrInvalidRange, length, limit)
	}

	return nil
}

// Chunk is the answer of MethodRead: the bytes of the range, and the length
// of the whole file. Data is shorter than the requested length at the end of
// the file, and empty for an offset at or past the end.
type Chunk struct {
	Data []byte `json:"data"`
	Size int64  `json:"size"`
	// EOF is true when this range reached the end of the file.
	EOF bool `json:"eof"`
}

// ListParams is the parameters of MethodList. An empty Path is the storage's
// root. Cursor is empty for the first page, and Listing.Next of the previous
// page for each later one.
type ListParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`

	Storage string `json:"storage"`
	Path    string `json:"path"`
	Cursor  string `json:"cursor,omitempty"`
}

// Validate checks one listing against the limits of this contract.
func (p ListParams) Validate() error {
	if err := validStorage(p.Principal, p.Storage); err != nil {
		return err
	}

	switch {
	case len(p.Path) > MaxPathBytes:
		return fmt.Errorf("%w: the path is %d bytes and the limit is %d",
			ErrInvalidPath, len(p.Path), MaxPathBytes)
	case len(p.Cursor) > MaxPathBytes:
		return fmt.Errorf("%w: the cursor is %d bytes and the limit is %d",
			ErrInvalidPath, len(p.Cursor), MaxPathBytes)
	}

	return nil
}

// ListEntry is one entry of a folder.
type ListEntry struct {
	Name string `json:"name"`
	// Dir is true for a folder.
	Dir bool `json:"dir,omitempty"`
	// Size is a file's length in bytes, and zero for a folder.
	Size int64 `json:"size"`
	// MTime is when the entry last changed, in Unix seconds.
	MTime int64 `json:"mtime"`
	// SHA256 is the digest of a file, lowercase hex. It is empty for a
	// folder, for a file with no stored digest, and for a file that changed
	// after the digest was computed.
	SHA256 string `json:"sha256,omitempty"`
}

// Listing is the answer of MethodList: at most MaxListEntries entries in the
// order of their names, and the cursor of the next page. Next is empty on the
// last page.
type Listing struct {
	Entries []ListEntry `json:"entries"`
	Next    string      `json:"next,omitempty"`
}

// validTarget checks the lengths of a principal, a storage name and a path,
// or of a handle that replaces all three.
func validTarget(principal, handle, storage, path string) error {
	if handle != "" {
		switch {
		case len(handle) > MaxHandleBytes:
			return fmt.Errorf("%w: the handle is %d bytes and the limit is %d", ErrNoFile, len(handle), MaxHandleBytes)
		case principal != "" || storage != "" || path != "":
			return fmt.Errorf("%w: a call names a handle or a storage and a path, never both", ErrDenied)
		}

		return nil
	}

	if err := validStorage(principal, storage); err != nil {
		return err
	}

	switch {
	case path == "":
		return fmt.Errorf("%w: the call names no file", ErrNoFile)
	case len(path) > MaxPathBytes:
		return fmt.Errorf("%w: the path is %d bytes and the limit is %d",
			ErrInvalidPath, len(path), MaxPathBytes)
	}

	return nil
}

// validStorage checks the lengths of a principal and a storage name.
func validStorage(principal, storage string) error {
	switch {
	case len(principal) > MaxPrincipalBytes:
		return fmt.Errorf("%w: the principal is %d bytes and the limit is %d",
			ErrDenied, len(principal), MaxPrincipalBytes)
	case storage == "":
		return fmt.Errorf("%w: the call names no storage", ErrNoFile)
	case len(storage) > MaxStorageBytes:
		return fmt.Errorf("%w: the storage name is %d bytes and the limit is %d",
			ErrNoFile, len(storage), MaxStorageBytes)
	}

	return nil
}

// Files is the host's side of the module wire. The host implements it.
type Files interface {
	Stat(ctx context.Context, params StatParams) (FileInfo, error)
	Read(ctx context.Context, params ReadParams) (Chunk, error)
	List(ctx context.Context, params ListParams) (Listing, error)
	// Fetch writes the range to w and returns what it wrote. w is the stream
	// the call arrived on.
	Fetch(ctx context.Context, params FetchParams, w io.Writer) (Fetched, error)
	// Write publishes body under params.Path. body ends after params.Size
	// bytes and fails when the stream had fewer or more. Publish only when
	// body ends without an error.
	Write(ctx context.Context, params WriteParams, body io.Reader) (Written, error)
}

// Caller is the part of a wirerpc session this contract needs.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client is how a module reads and writes the files of a storage.
type Client struct {
	conn Caller
}

// NewClient returns a client over a module's session with the host. A host
// that serves no storage answers the first call with CodeMethodNotFound.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

// Stat describes one file. For a file a slicer wrote, the answer has
// FileInfo.Print. It needs the read_storage permission. An unknown storage, a
// missing file and a path that leaves the storage all report ErrNoFile.
func (c *Client) Stat(ctx context.Context, params StatParams) (FileInfo, error) {
	if err := params.Validate(); err != nil {
		return FileInfo{}, fmt.Errorf("storageapi: %s: %w", MethodStat, err)
	}

	var info FileInfo
	if err := c.call(ctx, MethodStat, params, &info); err != nil {
		return FileInfo{}, err
	}

	return info, nil
}

// Read returns one range of one file, at most MaxReadBytes. It needs the same
// permission as Stat. A directory reports ErrNoFile. A storage whose type
// cannot give a file reports ErrUnreadable.
func (c *Client) Read(ctx context.Context, params ReadParams) (Chunk, error) {
	if err := params.Validate(); err != nil {
		return Chunk{}, fmt.Errorf("storageapi: %s: %w", MethodRead, err)
	}

	var chunk Chunk
	if err := c.call(ctx, MethodRead, params, &chunk); err != nil {
		return Chunk{}, err
	}

	return chunk, nil
}

// List returns one page of one folder, at most MaxListEntries entries in the
// order of their names. Pass Listing.Next back as ListParams.Cursor until
// Next is empty. It needs the same permission as Stat. An unknown storage, a
// missing folder and a path that leaves the storage all report ErrNoFile.
func (c *Client) List(ctx context.Context, params ListParams) (Listing, error) {
	if err := params.Validate(); err != nil {
		return Listing{}, fmt.Errorf("storageapi: %s: %w", MethodList, err)
	}

	var listing Listing
	if err := c.call(ctx, MethodList, params, &listing); err != nil {
		return Listing{}, err
	}

	return listing, nil
}

// call makes one round trip and decodes the answer. It maps a coded refusal
// back to the matching error of this package.
func (c *Client) call(ctx context.Context, method wirerpc.Method, params, into any) error {
	if c.conn == nil {
		return fmt.Errorf("storageapi: %s: the client holds no session", method)
	}

	body, err := c.conn.Call(ctx, method, params)
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
			if sentinel := sentinelFor(coded.Code); sentinel != nil {
				return fmt.Errorf("%s: %w", method, &remote{sentinel: sentinel, message: coded.Message})
			}
		}

		return fmt.Errorf("storageapi: %s: %w", method, err)
	}

	if len(body) == 0 {
		return nil
	}

	if err = json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("storageapi: %s: read the answer: %w", method, err)
	}

	return nil
}

// remote is the host's message wrapped around the matching error of this
// package.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }
func (r *remote) Unwrap() error { return r.sentinel }

func sentinelFor(code int) error {
	switch code {
	case CodeNoFile:
		return ErrNoFile
	case CodeInvalidPath:
		return ErrInvalidPath
	case CodeInvalidRange:
		return ErrInvalidRange
	case CodeUnreadable:
		return ErrUnreadable
	case CodeDenied:
		return ErrDenied
	case CodeShortWrite:
		return ErrShortWrite
	}

	return nil
}

// HostFailed is the message a module gets when a call failed inside the host.
// The text is fixed so that host internals do not reach a module.
const HostFailed = "storageapi: the host could not answer"

// refusal turns a host-side error into its wire code, so the module can map
// it back to the same error.
func refusal(err error) error {
	coded := []struct {
		code     int
		sentinel error
	}{
		{CodeDenied, ErrDenied},
		{CodeShortWrite, ErrShortWrite},
		{CodeInvalidPath, ErrInvalidPath},
		{CodeInvalidRange, ErrInvalidRange},
		{CodeUnreadable, ErrUnreadable},
		{CodeNoFile, ErrNoFile},
	}

	for _, known := range coded {
		if errors.Is(err, known.sentinel) {
			return &wirerpc.Error{Code: known.code, Message: err.Error()}
		}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
}

// Handlers returns the host's methods of the module wire for one module. A
// nil files returns an empty set, and every call then answers
// CodeMethodNotFound.
func Handlers(files Files) wirerpc.Methods {
	if files == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{
		MethodStat:  handle(MethodStat, files.Stat),
		MethodRead:  handle(MethodRead, files.Read),
		MethodList:  handle(MethodList, files.List),
		MethodFetch: fetchHandler(files),
		MethodWrite: writeHandler(files),
	}
}

func fetchHandler(files Files) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params FetchParams

		if len(call.Params) == 0 || json.Unmarshal(call.Params, &params) != nil {
			return nil, badParams(MethodFetch)
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		// A call with no stream gets the answer and no bytes.
		sink := io.Discard
		if call.Stream != nil {
			sink = call.Stream
		}

		fetched, err := files.Fetch(ctx, params, sink)
		if err != nil {
			return nil, refusal(err)
		}

		return fetched, nil
	}
}

// handle decodes one call, validates it again on the host side, and turns an
// error into its wire code.
func handle[P interface{ Validate() error }, R any](
	method wirerpc.Method, answer func(context.Context, P) (R, error),
) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params P

		if len(call.Params) == 0 || json.Unmarshal(call.Params, &params) != nil {
			return nil, badParams(method)
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		answered, err := answer(ctx, params)
		if err != nil {
			return nil, refusal(err)
		}

		return answered, nil
	}
}

func badParams(method wirerpc.Method) error {
	return &wirerpc.Error{
		Code:    wirerpc.CodeInvalidParams,
		Message: string(method) + " carries a params object",
	}
}
