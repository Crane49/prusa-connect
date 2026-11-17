package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// gatedSession is a driver whose transfer runs until the test lets it end, and
// which then reports whatever the context it was given says.
type gatedSession struct {
	identity  connectorapi.Identity
	patches   chan connectorapi.StatePatch
	started   chan struct{}
	release   chan struct{}
	commanded chan error
	// sent carries what each transfer returned, and deadline the deadline its
	// context carried.
	sent     chan error
	deadline chan time.Time
}

func (s *gatedSession) Identity() connectorapi.Identity { return s.identity }

func (s *gatedSession) Capabilities() (connectorapi.Set, uint64) {
	return connectorapi.Set{
		connectorapi.VerbFileUpload:                     {Flag: true},
		connectorapi.Capability(httpapi.StartPrintVerb): {Flag: true},
		connectorapi.ArgumentKey(connectorapi.Capability(httpapi.StartPrintVerb), httpapi.StartPrintPathArgument): {
			Flag: true, Kind: connectorapi.KindString, Required: true,
		},
	}, 1
}

func (s *gatedSession) Do(ctx context.Context, _ connectorapi.Command) (connectorapi.Result, error) {
	if s.commanded != nil {
		s.commanded <- ctx.Err()
	}

	return connectorapi.Result{OK: true}, ctx.Err()
}

func (s *gatedSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *gatedSession) Close() error { return nil }

func (s *gatedSession) Send(ctx context.Context, _ connectorapi.Upload) error {
	if s.deadline != nil {
		deadline, _ := ctx.Deadline()
		s.deadline <- deadline
	}

	s.started <- struct{}{}

	select {
	case <-s.release:
	case <-ctx.Done():
	}

	if s.sent != nil {
		s.sent <- ctx.Err()
	}

	return ctx.Err()
}

// gatedPlane is a plane with one gated printer on it that the default
// principal operates, and a storage holding one file.
func gatedPlane(t *testing.T, session *gatedSession) (*devices, string, string) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	graph := newGraph(t)
	plane := planeReading(t, graph, stageOne(t, root))

	host := plane.Arrivals("test",
		connectorapi.Set{
			connectorapi.VerbFileUpload:                     {Flag: true},
			connectorapi.Capability(httpapi.StartPrintVerb): {Flag: true},
			connectorapi.ArgumentKey(connectorapi.Capability(httpapi.StartPrintVerb), httpapi.StartPrintPathArgument): {
				Flag: true, Kind: connectorapi.KindString, Required: true,
			},
		},
		capability.NewResolver(profile.Registry{}, discard()), "")

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	id := plane.idOf(session.identity)
	who := subjectOf(config.Default().Principal)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, who)))

	return plane, id, who
}

// A page that is reloaded or closed while a file is on its way ends the request
// that carries the transfer. The caller is answered at once, so the request
// ends and a server can stop, and the relay goes on to the end of the file.
// The stop route is still what ends a transfer.
func TestATransferOutlivesTheCallerThatStartedIt(t *testing.T) {
	t.Parallel()

	session := &gatedSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-SEND-DETACH"},
		patches:  make(chan connectorapi.StatePatch),
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}, 1),
		sent:     make(chan error, 1),
	}

	plane, id, who := gatedPlane(t, session)
	upload := httpapi.DeviceUpload{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	type answer struct {
		transfer httpapi.DeviceTransfer
		err      error
	}

	send := func(ctx context.Context) <-chan answer {
		out := make(chan answer, 1)

		go func() {
			transfer, sendErr := plane.Send(ctx, who, id, upload)
			out <- answer{transfer: transfer, err: sendErr}
		}()

		return out
	}

	caller, leave := context.WithCancel(t.Context())
	sent := send(caller)

	<-session.started

	// The page goes away while the bytes are moving. It is answered while the
	// printer is still taking them.
	leave()

	select {
	case got := <-sent:
		require.ErrorIs(t, got.err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("a caller that left was kept waiting for the transfer")
	}

	require.Len(t, plane.transfers.running("device:"+id), 1, "the transfer ended with its caller")

	session.release <- struct{}{}

	select {
	case got := <-session.sent:
		require.NoError(t, got, "the transfer ran on the context of a caller that had left")
	case <-time.After(5 * time.Second):
		t.Fatal("the transfer never ended")
	}

	require.Eventually(t, func() bool { return len(plane.transfers.running("device:"+id)) == 0 },
		5*time.Second, 10*time.Millisecond, "the finished transfer is still filed against the printer")

	// A person pressing stop still ends a transfer, and the caller still
	// waiting is told so.
	stopping := send(t.Context())

	<-session.started

	running := plane.transfers.running("device:" + id)
	require.Len(t, running, 1)
	require.NoError(t, plane.StopTransfer(t.Context(), who, id, running[0].TransferID))

	select {
	case got := <-stopping:
		require.Error(t, got.err, "a stopped transfer was reported as finished")
	case <-time.After(5 * time.Second):
		t.Fatal("the stopped transfer never ended")
	}

	require.ErrorIs(t, <-session.sent, context.Canceled)
}

// The relay keeps the caller's deadline, which over HTTP is the route's
// stream budget, and only a caller with none gets detachedPrintBudget.
func TestARelayKeepsTheDeadlineOfItsCaller(t *testing.T) {
	t.Parallel()

	session := &gatedSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-SEND-DEADLINE"},
		patches:  make(chan connectorapi.StatePatch),
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}, 1),
		deadline: make(chan time.Time, 1),
	}

	plane, id, who := gatedPlane(t, session)
	upload := httpapi.DeviceUpload{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	budget := time.Now().Add(42 * time.Minute)
	caller, leave := context.WithDeadline(t.Context(), budget)

	done := make(chan error, 1)

	go func() {
		_, err := plane.Send(caller, who, id, upload)
		done <- err
	}()

	got := <-session.deadline
	<-session.started

	leave()
	session.release <- struct{}{}

	require.ErrorIs(t, <-done, context.Canceled)
	require.WithinDuration(t, budget, got, time.Second,
		"the relay ran under a deadline that is not its caller's")

	now := time.Now()
	require.Equal(t, now.Add(detachedPrintBudget), relayDeadline(context.Background(), now))
	require.Equal(t, budget, relayDeadline(caller, now))
}

// A page that asked for a print and went away while the file moved leaves a
// whole file on the printer. The start command is sent all the same: a file
// that arrived and was never started is a print the person asked for and did
// not get.
func TestAPrintStartsAfterTheCallerThatAskedForItLeft(t *testing.T) {
	t.Parallel()

	session := &gatedSession{
		identity:  connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-PRINT-DETACH"},
		patches:   make(chan connectorapi.StatePatch),
		started:   make(chan struct{}, 1),
		release:   make(chan struct{}, 1),
		commanded: make(chan error, 1),
	}

	plane, id, who := gatedPlane(t, session)

	caller, leave := context.WithCancel(t.Context())

	go func() {
		_, _ = plane.Print(caller, who, id, httpapi.DevicePrint{
			Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode",
		})
	}()

	<-session.started

	leave()
	session.release <- struct{}{}

	select {
	case got := <-session.commanded:
		require.NoError(t, got, "the start command went out on the context of a caller that had left")
	case <-time.After(5 * time.Second):
		t.Fatal("the start command was never sent")
	}
}
