package stateapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The methods of this contract. A module calls them and the host answers.
// None of them has a namespace parameter: the host sets the namespace of each
// module's session.
const (
	// MethodGet reads one record.
	MethodGet wirerpc.Method = "state.get"
	// MethodPut writes one record, whatever is there.
	MethodPut wirerpc.Method = "state.put"
	// MethodCompareAndPut writes one record only at an expected version.
	MethodCompareAndPut wirerpc.Method = "state.compare_and_put"
	// MethodDelete removes one record.
	MethodDelete wirerpc.Method = "state.delete"
	// MethodList returns the keys under a prefix.
	MethodList wirerpc.Method = "state.list"
	// MethodVersion returns the version of one record, without its bytes.
	MethodVersion wirerpc.Method = "state.version"
	// MethodUsage returns how much the namespace stores, and its limit.
	MethodUsage wirerpc.Method = "state.usage"
)

// The wire codes of this contract's errors. They are positive, outside the
// range from -32768 to -32000 that JSON-RPC 2.0 reserves.
const (
	// CodeNoRecord is the wire code of ErrNoRecord.
	CodeNoRecord = 1000
	// CodeVersion is the wire code of ErrVersion.
	CodeVersion = 1001
	// CodeKey is the wire code of ErrKey.
	CodeKey = 1002
	// CodeValueTooLarge is the wire code of ErrValueTooLarge.
	CodeValueTooLarge = 1003
	// CodeNamespaceFull is the wire code of ErrNamespaceFull.
	CodeNamespaceFull = 1004
	// CodeNotReady is the wire code of ErrNotReady.
	CodeNotReady = 1005
	// CodeBusy is the wire code of ErrBusy.
	CodeBusy = 1006
)

// A value travels inside the control frame, as base64 in the JSON.

// KeyParams is the parameters of MethodGet, MethodDelete and MethodVersion.
type KeyParams struct {
	Key Key `json:"key"`
}

// PutParams is the parameters of MethodPut.
type PutParams struct {
	Key   Key    `json:"key"`
	Value []byte `json:"value,omitempty"`
}

// CompareAndPutParams is the parameters of MethodCompareAndPut. Expected is
// the version the caller read. AbsentVersion expects no record.
type CompareAndPutParams struct {
	Key      Key    `json:"key"`
	Expected uint64 `json:"expected"`
	Value    []byte `json:"value,omitempty"`
}

// ListParams is the parameters of MethodList.
type ListParams struct {
	Prefix Key `json:"prefix,omitempty"`
}

// ValueResult is the answer of MethodGet.
type ValueResult struct {
	Value   []byte `json:"value,omitempty"`
	Version uint64 `json:"version"`
}

// VersionResult is the answer of MethodPut, MethodCompareAndPut and
// MethodVersion: the record's version after the call.
type VersionResult struct {
	Version uint64 `json:"version"`
}

// ListResult is the answer of MethodList. The keys are sorted.
type ListResult struct {
	Keys []Key `json:"keys"`
}

// Handlers returns the methods a host installs on its session with one
// module. records must be that module's own Records, because every call on
// the session reads and writes it.
func Handlers(records Records) wirerpc.Methods {
	return wirerpc.Methods{
		MethodGet:           getHandler(records),
		MethodPut:           putHandler(records),
		MethodCompareAndPut: compareAndPutHandler(records),
		MethodDelete:        deleteHandler(records),
		MethodList:          listHandler(records),
		MethodVersion:       versionHandler(records),
		MethodUsage:         usageHandler(records),
	}
}

// getHandler answers MethodGet.
func getHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[KeyParams](call)
		if err != nil {
			return nil, err
		}

		held, err := records.Get(ctx, params.Key)
		if err != nil {
			return nil, refusal(err)
		}

		return ValueResult{Value: held.Bytes, Version: held.Version}, nil
	}
}

// putHandler answers MethodPut.
func putHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[PutParams](call)
		if err != nil {
			return nil, err
		}

		version, err := records.Put(ctx, params.Key, params.Value)
		if err != nil {
			return nil, refusal(err)
		}

		return VersionResult{Version: version}, nil
	}
}

// compareAndPutHandler answers MethodCompareAndPut.
func compareAndPutHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[CompareAndPutParams](call)
		if err != nil {
			return nil, err
		}

		version, err := records.CompareAndPut(ctx, params.Key, params.Expected, params.Value)
		if err != nil {
			return nil, refusal(err)
		}

		return VersionResult{Version: version}, nil
	}
}

// deleteHandler answers MethodDelete.
func deleteHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[KeyParams](call)
		if err != nil {
			return nil, err
		}

		if err = records.Delete(ctx, params.Key); err != nil {
			return nil, refusal(err)
		}

		return struct{}{}, nil
	}
}

// listHandler answers MethodList. An empty namespace answers an empty array,
// not null.
func listHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[ListParams](call)
		if err != nil {
			return nil, err
		}

		keys, err := records.List(ctx, params.Prefix)
		if err != nil {
			return nil, refusal(err)
		}

		if keys == nil {
			keys = []Key{}
		}

		return ListResult{Keys: keys}, nil
	}
}

// versionHandler answers MethodVersion.
func versionHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[KeyParams](call)
		if err != nil {
			return nil, err
		}

		version, err := records.Version(ctx, params.Key)
		if err != nil {
			return nil, refusal(err)
		}

		return VersionResult{Version: version}, nil
	}
}

// usageHandler answers MethodUsage, which has no params.
func usageHandler(records Records) wirerpc.Handler {
	return func(ctx context.Context, _ wirerpc.Call) (any, error) {
		usage, err := records.Usage(ctx)
		if err != nil {
			return nil, refusal(err)
		}

		return usage, nil
	}
}

// read decodes one call's params. It reports params it cannot read as
// CodeInvalidParams.
func read[T any](call wirerpc.Call) (T, error) {
	var params T

	if len(call.Params) == 0 {
		return params, &wirerpc.Error{
			Code:    wirerpc.CodeInvalidParams,
			Message: fmt.Sprintf("%s carries a params object", call.Method),
		}
	}

	if err := json.Unmarshal(call.Params, &params); err != nil {
		return params, &wirerpc.Error{
			Code:    wirerpc.CodeInvalidParams,
			Message: fmt.Sprintf("%s: the params object could not be read", call.Method),
		}
	}

	return params, nil
}

// refusal turns a store's error into its wire code, with its message. Any
// other error becomes CodeInternal with the fixed text HostFailed, so host
// internals do not reach a module.
func refusal(err error) error {
	coded := []struct {
		code     int
		sentinel error
	}{
		{CodeNoRecord, ErrNoRecord},
		{CodeVersion, ErrVersion},
		{CodeKey, ErrKey},
		{CodeValueTooLarge, ErrValueTooLarge},
		{CodeNamespaceFull, ErrNamespaceFull},
		{CodeNotReady, ErrNotReady},
		{CodeBusy, ErrBusy},
	}

	for _, known := range coded {
		if errors.Is(err, known.sentinel) {
			return &wirerpc.Error{Code: known.code, Message: err.Error()}
		}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
}

// HostFailed is the message a module gets when the host's store failed. The
// text is fixed so that host internals do not reach a module.
const HostFailed = "stateapi: the host could not answer"

// sentinelFor returns the error of this package for a wire code, and nil for
// an unknown code.
func sentinelFor(code int) error {
	switch code {
	case CodeNoRecord:
		return ErrNoRecord
	case CodeVersion:
		return ErrVersion
	case CodeKey:
		return ErrKey
	case CodeValueTooLarge:
		return ErrValueTooLarge
	case CodeNamespaceFull:
		return ErrNamespaceFull
	case CodeNotReady:
		return ErrNotReady
	case CodeBusy:
		return ErrBusy
	}

	return nil
}

// remote is a refusal from the host: the host's message, wrapped around the
// matching error of this package.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }

// Unwrap returns the matching error of this package, so errors.Is works.
func (r *remote) Unwrap() error { return r.sentinel }

// Caller is the part of a session a Client needs. *wirerpc.Conn implements
// it.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client is the Records of a module, served by the host over the module's
// session.
type Client struct {
	conn Caller
}

// NewClient returns the Records of a module. A host that serves no state
// answers the first call with CodeMethodNotFound.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

var _ Records = (*Client)(nil)

// Get returns the record under key.
func (c *Client) Get(ctx context.Context, key Key) (Value, error) {
	if err := key.Validate(); err != nil {
		return Value{}, err
	}

	answer, err := call[ValueResult](ctx, c.conn, MethodGet, KeyParams{Key: key})
	if err != nil {
		return Value{}, err
	}

	return Value{Bytes: answer.Value, Version: answer.Version}, nil
}

// Put writes value under key, whatever is there.
func (c *Client) Put(ctx context.Context, key Key, value []byte) (uint64, error) {
	if err := validateWrite(key, value); err != nil {
		return 0, err
	}

	answer, err := call[VersionResult](ctx, c.conn, MethodPut, PutParams{Key: key, Value: value})
	if err != nil {
		return 0, err
	}

	return answer.Version, nil
}

// CompareAndPut writes value under key only if the record is at expected.
func (c *Client) CompareAndPut(ctx context.Context, key Key, expected uint64, value []byte) (uint64, error) {
	if err := validateWrite(key, value); err != nil {
		return 0, err
	}

	answer, err := call[VersionResult](ctx, c.conn, MethodCompareAndPut,
		CompareAndPutParams{Key: key, Expected: expected, Value: value})
	if err != nil {
		return 0, err
	}

	return answer.Version, nil
}

// Delete removes the record under key.
func (c *Client) Delete(ctx context.Context, key Key) error {
	if err := key.Validate(); err != nil {
		return err
	}

	_, err := call[struct{}](ctx, c.conn, MethodDelete, KeyParams{Key: key})

	return err
}

// List returns every key under prefix, sorted.
func (c *Client) List(ctx context.Context, prefix Key) ([]Key, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}

	answer, err := call[ListResult](ctx, c.conn, MethodList, ListParams{Prefix: prefix})
	if err != nil {
		return nil, err
	}

	return answer.Keys, nil
}

// Version returns the version of the record under key.
func (c *Client) Version(ctx context.Context, key Key) (uint64, error) {
	if err := key.Validate(); err != nil {
		return 0, err
	}

	answer, err := call[VersionResult](ctx, c.conn, MethodVersion, KeyParams{Key: key})
	if err != nil {
		return 0, err
	}

	return answer.Version, nil
}

// Usage returns how much the namespace stores, and its limit. A host that
// does not serve this call answers CodeMethodNotFound, and the limit is then
// DefaultNamespaceBytes.
func (c *Client) Usage(ctx context.Context) (Usage, error) {
	return call[Usage](ctx, c.conn, MethodUsage, struct{}{})
}

// validateWrite checks the key and the value before a write is sent. The host
// checks both again.
func validateWrite(key Key, value []byte) error {
	if err := key.Validate(); err != nil {
		return err
	}

	return ValidateValue(value)
}

// call makes one control call and reads the answer. It maps a coded refusal
// back to the matching error of this package.
func call[T any](ctx context.Context, conn Caller, method wirerpc.Method, params any) (T, error) {
	var answer T

	if conn == nil {
		return answer, fmt.Errorf("stateapi: %s: the client holds no session", method)
	}

	raw, err := conn.Call(ctx, method, params)
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
			if sentinel := sentinelFor(coded.Code); sentinel != nil {
				return answer, fmt.Errorf("%s: %w",
					method, &remote{sentinel: sentinel, message: coded.Message})
			}
		}

		return answer, fmt.Errorf("stateapi: %s: %w", method, err)
	}

	// A method such as Delete answers nothing. json.Unmarshal would refuse
	// the empty document.
	if len(raw) == 0 {
		return answer, nil
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return answer, fmt.Errorf("stateapi: %s: read the answer: %w", method, err)
	}

	return answer, nil
}
