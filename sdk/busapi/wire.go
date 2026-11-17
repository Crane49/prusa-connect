// Package busapi is how a module tells the rest of the deployment that
// something happened.
//
// The module calls bus.emit and the host answers. A module names an endpoint
// and a payload. It never names the event type or the origin. The origin is
// the module's instance. The type is the module's namespace joined to the name
// the operator declared for that endpoint. So a module cannot publish a core
// type or a type in another namespace.
package busapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodEmit is the call a module makes to publish one event on the host's own
// event stream. It has no field for an event type or an origin.
const MethodEmit wirerpc.Method = "bus.emit"

const (
	// MaxDataBytes bounds one emission's payload. Put a larger value in a
	// record and emit its key.
	MaxDataBytes = 8 << 10

	// MaxEndpointBytes bounds an endpoint name.
	MaxEndpointBytes = 64

	// MaxKeyBytes bounds the routing key, which is the conflation key of the
	// publication.
	MaxKeyBytes = 128
)

// The error codes of this contract. They are outside the range JSON-RPC 2.0
// reserves and outside the ranges of the other contracts.
const (
	// CodeNoEndpoint is ErrNoEndpoint.
	CodeNoEndpoint = 2000
	// CodeDataTooLarge is ErrDataTooLarge.
	CodeDataTooLarge = 2001
	// CodeInvalidData is ErrInvalidData.
	CodeInvalidData = 2002
	// CodeNotReady is ErrNotReady.
	CodeNotReady = 2003
	// CodeBusy is ErrBusy.
	CodeBusy = 2004
)

// The refusals of this contract. A module branches on them with errors.Is.
// The message names the limit or the declared endpoints.
var (
	// ErrNoEndpoint reports an endpoint that is not in the instance's
	// declaration. The operator names every endpoint a module may publish on.
	ErrNoEndpoint = errors.New("busapi: the instance declares no such endpoint")
	// ErrDataTooLarge reports a payload over MaxDataBytes.
	ErrDataTooLarge = errors.New("busapi: the payload is over the limit")
	// ErrInvalidData reports a payload that is not JSON.
	ErrInvalidData = errors.New("busapi: the payload is not json")
	// ErrNotReady reports an emission from a module the host has not marked
	// ready. Retry later.
	ErrNotReady = errors.New("busapi: the host has not marked this module ready")
	// ErrBusy reports an instance already publishing as many events at once as
	// the host allows. The excess is refused, not queued.
	ErrBusy = errors.New("busapi: the instance is at its concurrent emit limit")
)

// EmitParams is the params of MethodEmit. Endpoint selects one of the names
// the operator declared for this instance. Key is the conflation key, and
// empty means the resolved type. Data is the payload, which must be JSON.
type EmitParams struct {
	Endpoint string          `json:"endpoint"`
	Key      string          `json:"key,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// Validate checks one emission against the bounds of this contract. The host
// checks them again.
func (p EmitParams) Validate() error {
	switch {
	case p.Endpoint == "":
		return fmt.Errorf("%w: the emission names no endpoint", ErrNoEndpoint)
	case len(p.Endpoint) > MaxEndpointBytes:
		return fmt.Errorf("%w: the endpoint name is %d bytes and the limit is %d",
			ErrNoEndpoint, len(p.Endpoint), MaxEndpointBytes)
	case !printable(p.Endpoint):
		return fmt.Errorf("%w: the endpoint name carries a control character", ErrNoEndpoint)
	case len(p.Key) > MaxKeyBytes:
		return fmt.Errorf("%w: the key is %d bytes and the limit is %d",
			ErrDataTooLarge, len(p.Key), MaxKeyBytes)
	case !printable(p.Key):
		return fmt.Errorf("%w: the key carries a control character", ErrInvalidData)
	case len(p.Data) > MaxDataBytes:
		return fmt.Errorf("%w: the payload is %d bytes and the limit is %d",
			ErrDataTooLarge, len(p.Data), MaxDataBytes)
	case len(p.Data) > 0 && !json.Valid(p.Data):
		return fmt.Errorf("%w: the payload is not json", ErrInvalidData)
	}

	return nil
}

// printable reports whether every byte of s is above the control range and is
// not DEL. A control character in a quoted name could forge a log line.
func printable(s string) bool {
	const del = 0x7f

	for i := range len(s) {
		if s[i] <= ' ' || s[i] == del {
			return false
		}
	}

	return true
}

// Publisher is the host's side. It resolves the endpoint against the
// declaration of one instance and publishes under that instance's origin.
type Publisher interface {
	Emit(ctx context.Context, endpoint, key string, data json.RawMessage) error
}

// Handlers is the method set a host installs on the session it opened to one
// child, bound to the Publisher it built for that child. An instance with no
// declared endpoint gets no handlers, and bus.emit answers CodeMethodNotFound.
func Handlers(publisher Publisher) wirerpc.Methods {
	return wirerpc.Methods{MethodEmit: emitHandler(publisher)}
}

// emitHandler answers MethodEmit.
func emitHandler(publisher Publisher) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params EmitParams

		if len(call.Params) == 0 {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: string(MethodEmit) + " carries a params object",
			}
		}

		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: string(MethodEmit) + ": the params object could not be read",
			}
		}

		if err := params.Validate(); err != nil {
			return nil, refusal(err)
		}

		if err := publisher.Emit(ctx, params.Endpoint, params.Key, params.Data); err != nil {
			return nil, refusal(err)
		}

		return struct{}{}, nil
	}
}

// HostFailed is the message a module gets when the emission failed inside the
// host. The detail is in the host's log.
const HostFailed = "busapi: the host could not publish"

// refusal renders a host-side error as a code of this contract, with its
// message. Any other error becomes CodeInternal with HostFailed.
func refusal(err error) error {
	coded := []struct {
		code     int
		sentinel error
	}{
		{CodeNoEndpoint, ErrNoEndpoint},
		{CodeDataTooLarge, ErrDataTooLarge},
		{CodeInvalidData, ErrInvalidData},
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

// sentinelFor is the reverse of refusal: the error of this package for a code
// the host answered with.
func sentinelFor(code int) error {
	switch code {
	case CodeNoEndpoint:
		return ErrNoEndpoint
	case CodeDataTooLarge:
		return ErrDataTooLarge
	case CodeInvalidData:
		return ErrInvalidData
	case CodeNotReady:
		return ErrNotReady
	case CodeBusy:
		return ErrBusy
	case CodeInvalidSubscription:
		return ErrInvalidSubscription
	case CodeNoStream:
		return ErrNoStream
	case CodeDenied:
		return ErrDenied
	}

	return nil
}

// remote is a refusal the host raised, as a client returns it: the host's
// message and this package's sentinel.
type remote struct {
	sentinel error
	message  string
}

func (r *remote) Error() string { return r.message }

// Unwrap returns the sentinel, so errors.Is works on an error from another
// process.
func (r *remote) Unwrap() error { return r.sentinel }

// Caller is the part of a session a Client needs: one control call.
// *wirerpc.Conn satisfies it.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client publishes a module's events on the host's event stream.
type Client struct {
	conn Caller
}

// NewClient returns the bus a module publishes through. It makes no call. An
// instance with no declared endpoint gets CodeMethodNotFound on the first
// Emit.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

// Emit publishes one event on the endpoint the operator named. It marshals
// data to JSON.
func (c *Client) Emit(ctx context.Context, endpoint, key string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("busapi: %s: encode the payload: %w", MethodEmit, err)
	}

	return c.EmitRaw(ctx, endpoint, key, body)
}

// EmitRaw publishes a payload the caller has already encoded.
func (c *Client) EmitRaw(ctx context.Context, endpoint, key string, data json.RawMessage) error {
	params := EmitParams{Endpoint: endpoint, Key: key, Data: data}
	if err := params.Validate(); err != nil {
		return fmt.Errorf("busapi: %s: %w", MethodEmit, err)
	}

	if c.conn == nil {
		return fmt.Errorf("busapi: %s: the client holds no session", MethodEmit)
	}

	if _, err := c.conn.Call(ctx, MethodEmit, params); err != nil {
		return answered(MethodEmit, err)
	}

	return nil
}

// answered maps the error of a call onto the sentinel of its code. An error
// with no code of this contract is returned unchanged.
func answered(method wirerpc.Method, err error) error {
	if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
		if sentinel := sentinelFor(coded.Code); sentinel != nil {
			return fmt.Errorf("%s: %w", method, &remote{sentinel: sentinel, message: coded.Message})
		}
	}

	return fmt.Errorf("busapi: %s: %w", method, err)
}
