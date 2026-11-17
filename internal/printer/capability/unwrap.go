package capability

import (
	"reflect"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// unwrapLimit is how many sessions As asks: the one it is given and the ones
// inside it. A session whose Unwrap returns itself, or a chain that loops,
// would otherwise keep As running for ever on the goroutine that asked.
const unwrapLimit = 16

// unwrapper is a session that contains another one. Every type the host puts
// around a driver's session and gives the rest of the host as a
// connectorapi.Session has an Unwrap method. Without it, a type assertion on
// the outer type finds none of the driver's optional interfaces, because
// embedding connectorapi.Session copies only its methods.
//
// A driver's own session that declares Unwrap() connectorapi.Session is asked
// through it too. The SDK's conformance suite does not do that, so such a
// driver can pass the suite and still be read differently by the host.
type unwrapper interface {
	Unwrap() connectorapi.Session
}

// As returns the first session in the chain from s inwards that implements T,
// and reports false when none does.
//
// It is how the host asks a session for an optional interface, such as
// connectorapi.Filesystem or Presence. When a wrapper implements T, As returns
// the wrapper and does not ask the session inside it. When it does not, As
// asks the session that Unwrap returns. A nil session, or a nil pointer that
// Unwrap returns, ends the chain.
func As[T any](s connectorapi.Session) (T, bool) {
	for range unwrapLimit {
		if absent(s) {
			break
		}

		if found, ok := s.(T); ok {
			return found, true
		}

		inner, ok := s.(unwrapper)
		if !ok {
			break
		}

		s = inner.Unwrap()
	}

	var none T

	return none, false
}

// absent reports whether s is no session: nil, or a nil pointer. A type
// assertion on a nil pointer succeeds, and the caller would then call a method
// on nothing.
func absent(s connectorapi.Session) bool {
	if s == nil {
		return true
	}

	value := reflect.ValueOf(s)

	return value.Kind() == reflect.Pointer && value.IsNil()
}
