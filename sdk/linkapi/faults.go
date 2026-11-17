package linkapi

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// CodeFaulted is the error code a link module answers with when it has a
// registered fault for the failure. The whole fault is in the error's data
// member, so a page can translate the refusal by its code.
const CodeFaulted = 4004

// ErrFaulted reports a refusal with a registered fault. A caller matches it
// with errors.Is and then reads the fault with FaultOf.
var ErrFaulted = errors.New("linkapi: the link refused with a registered code")

// Faulted turns a registered fault into the error a link module's handler
// returns. The fault stays the cause. To keep an error variable of this
// contract matchable, such as ErrNoNode, wrap it into the fault first. It
// returns nil for a nil fault.
func Faulted(raised *fault.Fault) error {
	if raised == nil {
		return nil
	}

	return &faulted{payload: raised.Payload(), cause: raised}
}

// FaultOf returns the fault a link module sent with its refusal, and false for
// an error without one.
func FaultOf(err error) (fault.Payload, bool) {
	var carried *faulted
	if !errors.As(err, &carried) {
		return fault.Payload{}, false
	}

	return carried.payload, true
}

// faulted is a refusal with a registered fault. cause is the fault itself in
// the process that raised it, and this contract's error variable, or nil, in
// the process that received it.
type faulted struct {
	payload fault.Payload
	cause   error
}

// Error is the operator-facing line: the code, then the sentence for this
// occurrence.
func (f *faulted) Error() string {
	if f.payload.Detail == "" {
		return f.payload.Code.String()
	}

	return f.payload.Code.String() + ": " + f.payload.Detail
}

// Unwrap returns the error this refusal was built from, so errors.Is reaches
// it.
func (f *faulted) Unwrap() error { return f.cause }

// Is matches ErrFaulted.
func (f *faulted) Is(target error) bool { return target == ErrFaulted }

// data renders the fault as the data member of a wirerpc error. It returns
// nil for a payload that does not encode.
func (f *faulted) data() json.RawMessage {
	encoded, err := json.Marshal(f.payload)
	if err != nil {
		return nil
	}

	return encoded
}

// wireError renders the fault as the wirerpc error the other side receives,
// for a refusal that matches no error variable of this contract.
func (f *faulted) wireError() *wirerpc.Error {
	return &wirerpc.Error{Code: CodeFaulted, Message: f.Error(), Data: f.data()}
}

// payloadFrom reads the fault in the data member of the other side's error.
// ok is false when there is none or it cannot be read.
func payloadFrom(coded *wirerpc.Error) (payload fault.Payload, ok bool) {
	if len(coded.Data) == 0 {
		return fault.Payload{}, false
	}

	if err := json.Unmarshal(coded.Data, &payload); err != nil {
		return fault.Payload{}, false
	}

	if payload.Code.IsZero() {
		return fault.Payload{}, false
	}

	return payload, true
}

// refusalOf turns the error the other side sent into the error this side
// returns, and nil for an error with neither a code of this contract nor a
// fault.
func refusalOf(method wirerpc.Method, coded *wirerpc.Error) error {
	payload, held := payloadFrom(coded)
	sentinel := sentinelFor(coded.Code)

	switch {
	case held && sentinel != nil:
		return wrapMethod(method, &faulted{
			payload: payload,
			cause:   &remote{sentinel: sentinel, message: coded.Message},
		})
	case held:
		return wrapMethod(method, &faulted{payload: payload})
	case sentinel != nil:
		return fmt.Errorf("%s: %w", method, &remote{sentinel: sentinel, message: coded.Message})
	}

	return nil
}

// wrapMethod names the method in an error of this contract.
func wrapMethod(method wirerpc.Method, err error) error {
	return fmt.Errorf("linkapi: %s: %w", method, err)
}
