package capability_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// wrappedSession is a host's own type around a driver's session. It embeds
// only connectorapi.Session, so a type assertion on it finds none of the
// driver's optional interfaces.
type wrappedSession struct {
	connectorapi.Session

	inner connectorapi.Session
}

func wrap(inner connectorapi.Session) wrappedSession {
	return wrappedSession{Session: inner, inner: inner}
}

func (w wrappedSession) Unwrap() connectorapi.Session { return w.inner }

// opaqueSession is a wrapper with no Unwrap. The host cannot find the driver's
// optional interfaces through it.
type opaqueSession struct {
	connectorapi.Session
}

// everyInterfaceSession is a driver's session with every optional interface
// the host asks for.
type everyInterfaceSession struct {
	connectorapi.Session
}

func (everyInterfaceSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	return nil, nil
}

func (everyInterfaceSession) List(context.Context, string) (connectorapi.Listing, error) {
	return connectorapi.Listing{}, nil
}

func (everyInterfaceSession) CreateFolder(context.Context, string) error { return nil }

func (everyInterfaceSession) DeleteFile(context.Context, string) error { return nil }

func (everyInterfaceSession) DeleteFolder(context.Context, string) error { return nil }

func (everyInterfaceSession) Send(context.Context, connectorapi.Upload) error { return nil }

func (everyInterfaceSession) Probe() connectorapi.Set { return connectorapi.Set{} }

func (everyInterfaceSession) Dropped() uint64 { return 0 }

func (everyInterfaceSession) Refused() uint64 { return 0 }

func (everyInterfaceSession) Connected() bool { return false }

func (everyInterfaceSession) PresenceChanged() <-chan struct{} { return nil }

// pointerSession is a driver's session with a pointer receiver, so a nil
// pointer of it still implements the optional interfaces.
type pointerSession struct {
	everyInterfaceSession
}

func (*pointerSession) Probe() connectorapi.Set { return connectorapi.Set{} }

// plainSession is a driver's session with no optional interface.
type plainSession struct {
	connectorapi.Session
}

func TestAsFindsADriversOptionalInterfacesInsideWrappers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		session connectorapi.Session
		found   bool
	}{
		{"the driver's own session", everyInterfaceSession{}, true},
		{"inside one wrapper", wrap(everyInterfaceSession{}), true},
		{"inside two wrappers", wrap(wrap(everyInterfaceSession{})), true},
		{"inside a wrapper with no Unwrap", opaqueSession{everyInterfaceSession{}}, false},
		{"a driver with none, inside a wrapper", wrap(plainSession{}), false},
		{"a wrapper around nothing", wrap(nil), false},
		{"a wrapper around a nil pointer", wrap((*pointerSession)(nil)), false},
		{"no session", nil, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			found := map[string]bool{}
			_, found["Filesystem"] = capability.As[connectorapi.Filesystem](testCase.session)
			_, found["FileManager"] = capability.As[connectorapi.FileManager](testCase.session)
			_, found["Transfers"] = capability.As[connectorapi.Transfers](testCase.session)
			_, found["ReportsProbe"] = capability.As[connectorapi.ReportsProbe](testCase.session)
			_, found["CountsLink"] = capability.As[connectorapi.CountsLink](testCase.session)
			_, found["Presence"] = capability.As[capability.Presence](testCase.session)

			for name, ok := range found {
				require.Equal(t, testCase.found, ok, "whether As finds %s", name)
			}
		})
	}
}

// probingWrapper is a wrapper that reports a probe of its own.
type probingWrapper struct {
	wrappedSession

	probe connectorapi.Set
}

func (p probingWrapper) Probe() connectorapi.Set { return p.probe }

// When a wrapper implements the interface itself, As returns the wrapper, so a
// host can replace what the driver reports.
func TestAsReturnsTheWrapperWhenTheWrapperImplementsTheInterface(t *testing.T) {
	t.Parallel()

	replaced := connectorapi.Set{"core.command.pause_print": {Flag: true}}
	outer := probingWrapper{wrappedSession: wrap(everyInterfaceSession{}), probe: replaced}

	reporter, ok := capability.As[connectorapi.ReportsProbe](outer)
	require.True(t, ok, "a wrapper that reports a probe itself was not found")
	require.True(t, reporter.Probe().Has("core.command.pause_print"),
		"the probe came from the session inside the wrapper")
}

// selfSession is a session whose Unwrap returns itself.
type selfSession struct {
	plainSession
}

func (s selfSession) Unwrap() connectorapi.Session { return s }

// A session whose Unwrap returns itself must not keep As running. As runs on
// the goroutine that takes a printer's session, and that goroutine must
// return, or the printer never arrives.
func TestAsStopsOnASessionThatUnwrapsToItself(t *testing.T) {
	t.Parallel()

	done := make(chan bool, 1)

	go func() {
		_, ok := capability.As[connectorapi.Filesystem](selfSession{})
		done <- ok
	}()

	select {
	case ok := <-done:
		require.False(t, ok, "a session with no filesystem was reported as having one")
	case <-time.After(5 * time.Second):
		t.Fatal("As did not return for a session whose Unwrap returns itself")
	}
}
