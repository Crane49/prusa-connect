package busapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodSubscribe is the call a module makes to read the host's event stream.
// It opens one stream, and the host writes windows on it until the module
// stops reading or the session ends.
//
// A subscription sees what its principal may observe: the person whose
// request opened it, or the module's own instance when Principal is empty.
const MethodSubscribe wirerpc.Method = "bus.subscribe"

const (
	// MaxOrigins bounds how many origins one subscription may narrow to.
	MaxOrigins = 512

	// MaxTypes bounds how many event types one subscription may narrow to.
	MaxTypes = 64

	// MinIntervalMS is the shortest coalescing window a module may ask for.
	MinIntervalMS = 100

	// MaxIntervalMS is the longest coalescing window a module may ask for.
	MaxIntervalMS = 10_000

	// MaxCursorBytes bounds a resume cursor.
	MaxCursorBytes = 8 << 10

	// MaxPrincipalBytes bounds a principal.
	MaxPrincipalBytes = 256
)

// The error codes of bus.subscribe.
const (
	// CodeInvalidSubscription is ErrInvalidSubscription.
	CodeInvalidSubscription = 2005
	// CodeNoStream is ErrNoStream.
	CodeNoStream = 2006
	// CodeDenied is ErrDenied.
	CodeDenied = 2007
)

var (
	// ErrInvalidSubscription reports a subscription outside the bounds this
	// contract publishes.
	ErrInvalidSubscription = errors.New("busapi: the subscription is outside the published bounds")
	// ErrNoStream reports bus.subscribe made as a plain call. Make it on a
	// stream the module opened.
	ErrNoStream = errors.New("busapi: bus.subscribe is made on a stream")
	// ErrDenied reports a subscription the host refused to open as the
	// principal it named: a person whose request the host is not serving, or
	// the module itself outside its grants.
	ErrDenied = errors.New("busapi: the instance may not subscribe as that")
)

// SubscribeParams is the params of MethodSubscribe. Every field narrows. An
// empty one follows every origin the module may observe, at the host's
// default window.
type SubscribeParams struct {
	// Principal is who the subscription is opened for, and empty for the
	// module's own work. See MethodSubscribe.
	Principal string `json:"principal,omitempty"`

	// Origins narrows to named origins, each `kind:id`. An origin the module
	// may not observe refuses the whole subscription.
	Origins []string `json:"origins,omitempty"`

	// Types narrows to named event types.
	Types []string `json:"types,omitempty"`

	// Resume is a cursor a previous subscription ended on. An empty cursor
	// starts at the head.
	Resume string `json:"resume,omitempty"`

	// IntervalMS is the coalescing window in milliseconds. Zero is the host's
	// default.
	IntervalMS int `json:"interval_ms,omitempty"`
}

// Validate checks one subscription against the bounds of this contract. The
// host checks them again.
func (p SubscribeParams) Validate() error {
	switch {
	case len(p.Principal) > MaxPrincipalBytes:
		return fmt.Errorf("%w: the principal is %d bytes and the limit is %d",
			ErrDenied, len(p.Principal), MaxPrincipalBytes)
	case len(p.Origins) > MaxOrigins:
		return fmt.Errorf("%w: %d origins and the limit is %d",
			ErrInvalidSubscription, len(p.Origins), MaxOrigins)
	case len(p.Types) > MaxTypes:
		return fmt.Errorf("%w: %d types and the limit is %d",
			ErrInvalidSubscription, len(p.Types), MaxTypes)
	case len(p.Resume) > MaxCursorBytes:
		return fmt.Errorf("%w: the cursor is %d bytes and the limit is %d",
			ErrInvalidSubscription, len(p.Resume), MaxCursorBytes)
	case p.IntervalMS != 0 && p.IntervalMS < MinIntervalMS:
		return fmt.Errorf("%w: the window is %dms and the floor is %dms",
			ErrInvalidSubscription, p.IntervalMS, MinIntervalMS)
	case p.IntervalMS > MaxIntervalMS:
		return fmt.Errorf("%w: the window is %dms and the ceiling is %dms",
			ErrInvalidSubscription, p.IntervalMS, MaxIntervalMS)
	}

	return nil
}

// Window is one coalescing window as a module reads it. The envelopes stay
// encoded.
type Window struct {
	// Telemetry is the newest value of each key that changed during the
	// window.
	Telemetry []json.RawMessage `json:"telemetry,omitempty"`

	// Events is the events of the durable classes, in publication order.
	Events []json.RawMessage `json:"events,omitempty"`

	// Resync reports that continuity is gone: the queue overflowed, or a resume
	// could not be honoured. Reload state from the API and continue from this
	// window's cursor.
	Resync bool `json:"resync,omitempty"`

	// Cursor is the place to resume from after this window.
	Cursor string `json:"cursor,omitempty"`
}

// Feed is the host's side of the subscription. The host supplies it and a
// module never constructs one. Subscribe runs until ctx ends, the module stops
// reading, or the host has nothing more to send. It calls write once per
// window, and a write error ends the subscription.
type Feed interface {
	Subscribe(ctx context.Context, params SubscribeParams, write func(Window) error) error
}

// SubscribeHandler answers MethodSubscribe. The host adds it to its method
// set. The set is empty for a nil feed.
func SubscribeHandler(feed Feed) wirerpc.Methods {
	if feed == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodSubscribe: subscribeHandler(feed)}
}

func subscribeHandler(feed Feed) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		if call.Stream == nil {
			return nil, &wirerpc.Error{Code: CodeNoStream, Message: ErrNoStream.Error()}
		}

		var params SubscribeParams

		// An absent params object follows everything the module may observe.
		if len(call.Params) > 0 {
			if err := json.Unmarshal(call.Params, &params); err != nil {
				return nil, &wirerpc.Error{
					Code:    wirerpc.CodeInvalidParams,
					Message: string(MethodSubscribe) + ": the params object could not be read",
				}
			}
		}

		if err := params.Validate(); err != nil {
			return nil, subscribeRefusal(err)
		}

		// One window per line.
		write := func(window Window) error {
			body, err := json.Marshal(window)
			if err != nil {
				return fmt.Errorf("%s: encode the window: %w", MethodSubscribe, err)
			}

			if _, err = call.Stream.Write(append(body, '\n')); err != nil {
				return fmt.Errorf("%s: write the window: %w", MethodSubscribe, err)
			}

			return nil
		}

		if err := feed.Subscribe(ctx, params, write); err != nil {
			return nil, subscribeRefusal(err)
		}

		return nil, call.Stream.CloseWrite()
	}
}

// subscribeRefusal renders a host-side error as a code of this contract.
func subscribeRefusal(err error) error {
	if errors.Is(err, ErrInvalidSubscription) {
		return &wirerpc.Error{Code: CodeInvalidSubscription, Message: err.Error()}
	}

	if errors.Is(err, ErrNotReady) {
		return &wirerpc.Error{Code: CodeNotReady, Message: err.Error()}
	}

	if errors.Is(err, ErrDenied) {
		return &wirerpc.Error{Code: CodeDenied, Message: err.Error()}
	}

	return &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
}

// Opener is the part of a session a subscription needs: one stream of its own.
type Opener interface {
	Open() (*wirerpc.Stream, error)
}

// Subscribe opens one stream and reads windows until ctx ends, the host stops
// writing, or onWindow reports an error. A slow onWindow slows the host down:
// nothing is queued without bound.
func Subscribe(
	ctx context.Context, conn Opener, params SubscribeParams, onWindow func(Window) error,
) error {
	if err := params.Validate(); err != nil {
		return fmt.Errorf("busapi: %s: %w", MethodSubscribe, err)
	}

	stream, err := conn.Open()
	if err != nil {
		return fmt.Errorf("busapi: %s: open a stream: %w", MethodSubscribe, err)
	}

	// The call answers when the host has finished writing, so the windows
	// are read on another goroutine while it runs.
	windows := make(chan error, 1)

	go func() { windows <- readWindows(stream, onWindow) }()

	_, called := stream.Call(ctx, MethodSubscribe, params)

	// Nothing more arrives after the call answered. Closing lets a reader
	// that is still waiting finish.
	_ = stream.Close()

	if read := <-windows; read != nil && !errors.Is(read, wirerpc.ErrStreamClosed) {
		return read
	}

	if called != nil && !errors.Is(called, wirerpc.ErrStreamClosed) {
		return answered(MethodSubscribe, called)
	}

	return nil
}

// readWindows decodes one window per line until the stream ends.
func readWindows(stream *wirerpc.Stream, onWindow func(Window) error) error {
	lines := bufio.NewScanner(stream)
	lines.Buffer(make([]byte, 0, initialWindowBuffer), MaxWindowBytes)

	for lines.Scan() {
		var window Window
		if err := json.Unmarshal(lines.Bytes(), &window); err != nil {
			return fmt.Errorf("busapi: %s: read a window: %w", MethodSubscribe, err)
		}

		if err := onWindow(window); err != nil {
			return err
		}
	}

	return lines.Err()
}

// initialWindowBuffer is the buffer a reader starts with. It grows to
// MaxWindowBytes as a window needs it.
const initialWindowBuffer = 64 << 10

// MaxWindowBytes bounds one encoded window a module will read.
const MaxWindowBytes = 1 << 20
