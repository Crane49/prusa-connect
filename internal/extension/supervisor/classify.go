package supervisor

import (
	"context"
	"errors"
	"io/fs"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Class is the host's verdict on one failure. One place decides it, so no
// resource author has to answer it and none can get it wrong.
type Class uint8

const (
	// ClassSuccess is no failure.
	ClassSuccess Class = iota
	// ClassCancelled is the caller going away. It is not the resource's failure
	// and it changes no state.
	ClassCancelled
	// ClassClient is the remote working correctly on a request that was wrong,
	// such as a missing key or a conflict. It changes no state, counts against
	// no breaker and is not retried.
	ClassClient
	// ClassRecoverable is a failure worth retrying. The instance goes retrying
	// and climbs the backoff ladder.
	ClassRecoverable
	// ClassPermanent is a failure retrying cannot fix. The instance goes failed
	// and stays there until an operator acts.
	ClassPermanent
)

// Client and server status boundaries. A fault carries an advisory HTTP status
// whether or not it ever reaches HTTP, and that status is the only thing in the
// value that says who was wrong.
const (
	statusClientFloor = 400
	statusServerFloor = 500
	statusUnauthMin   = 401
	statusUnauthMax   = 403
)

// String returns the class as it is rendered in logs and metric labels.
func (c Class) String() string {
	switch c {
	case ClassSuccess:
		return "success"
	case ClassCancelled:
		return "cancelled"
	case ClassClient:
		return "client"
	case ClassRecoverable:
		return "recoverable"
	case ClassPermanent:
		return "permanent"
	}

	return "unknown"
}

// Failure reports whether the outcome counts against the circuit breaker. A
// client error and a cancellation do not. Counting a 404 would trip the breaker
// on user input, and counting a cancellation would trip it when a browser tab
// closes.
func (c Class) Failure() bool { return c == ClassRecoverable || c == ClassPermanent }

// Classify returns the host's verdict on err.
//
// Anything it does not recognise counts as recoverable, because the two
// mistakes cost different amounts. Retrying a permanent failure wastes a
// socket. Giving up on a recoverable one loses the resource until somebody
// notices.
//
// A resource states its own verdict by returning a fault whose code is
// registered with fault.RetryNever or fault.RetryAllowed. That overrides the
// guess, and it is the same classification the client sees.
func Classify(err error) Class {
	if err == nil {
		return ClassSuccess
	}

	if errors.Is(err, context.Canceled) {
		return ClassCancelled
	}

	if carried, ok := errors.AsType[*fault.Fault](err); ok {
		return classifyFault(carried)
	}

	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrExist) {
		return ClassClient
	}

	return ClassRecoverable
}

// classifyFault reads the registered documentation of a code and does not guess
// from its text. Retryability is declared at registration, and a status in the
// 4xx range that is not a credential failure means the remote answered
// correctly.
func classifyFault(f *fault.Fault) Class {
	if f.Retryable() {
		return ClassRecoverable
	}

	status := f.Status()

	switch {
	case status >= statusUnauthMin && status <= statusUnauthMax:
		return ClassPermanent
	case status >= statusClientFloor && status < statusServerFloor:
		return ClassClient
	}

	return ClassPermanent
}
