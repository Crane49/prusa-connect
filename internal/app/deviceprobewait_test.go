package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// stuckProbeSession is a driver session whose Probe does not return until the
// test releases it. It says when Probe was called.
type stuckProbeSession struct {
	talkingSession

	called  chan struct{}
	release chan struct{}
	closing sync.Once
}

func (s *stuckProbeSession) Capabilities() (connectorapi.Set, uint64) { return twoVerbBaseline(), 1 }

// Close ends the state stream, as a driver does when its session closes. A
// host that took the session waits for the stream to end when the plane
// closes, so a case that fails after the host took it still ends.
func (s *stuckProbeSession) Close() error {
	s.closing.Do(func() { close(s.patches) })

	return nil
}

func (s *stuckProbeSession) Probe() connectorapi.Set {
	close(s.called)
	<-s.release

	return connectorapi.Set{}
}

func newStuckProbeSession(serial string) *stuckProbeSession {
	return &stuckProbeSession{
		talkingSession: talkingSession{
			countedSession: countedSession{
				identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
				patches:  make(chan connectorapi.StatePatch),
			},
			answer: connectorapi.Result{OK: true},
		},
		called:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

// panickingProbeSession is a driver session whose Probe panics.
type panickingProbeSession struct {
	talkingSession
}

func (s *panickingProbeSession) Capabilities() (connectorapi.Set, uint64) {
	return twoVerbBaseline(), 1
}

func (s *panickingProbeSession) Probe() connectorapi.Set { panic("the driver's probe failed") }

// slowProbeSession is a driver session whose Probe answers after longer than
// the host gives a driver to start.
type slowProbeSession struct {
	talkingSession

	probe connectorapi.Set
}

func (s *slowProbeSession) Capabilities() (connectorapi.Set, uint64) { return twoVerbBaseline(), 1 }

func (s *slowProbeSession) Probe() connectorapi.Set {
	time.Sleep(supervisor.DefaultStartTimeout + time.Second)

	return s.probe
}

// The contract lets Probe be slow. A slow answer still narrows the set, so the
// API does not offer a command the machine said it does not have.
func TestASlowProbeStillNarrowsWhatThePrinterIsOffered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, _ := planeAndBus(t)
		host := plane.Arrivals("test", twoVerbBaseline(),
			capability.NewResolver(profile.Registry{}, discard()), "")

		session := &slowProbeSession{
			talkingSession: talkingSession{
				countedSession: countedSession{
					identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-SLOW-PROBE"},
					patches:  make(chan connectorapi.StatePatch),
				},
				answer: connectorapi.Result{OK: true},
			},
			probe: connectorapi.Set{connectorapi.Capability(nozzleVerb): {Flag: true}},
		}

		t.Cleanup(func() { close(session.patches) })

		_, err := host.Admit(t.Context(), session.identity)
		require.NoError(t, err)
		require.NoError(t, host.Arrived(t.Context(), session))

		device, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, plane.idOf(session.identity))
		require.NoError(t, err)
		require.NotContains(t, device.Capabilities, bedVerb,
			"the machine said late that it has no heated bed and the API offers a bed verb anyway")
	})
}

// An arrival whose context ends while the driver's Probe has not answered
// returns, and the host does not take the session. The context of a request
// that is not hijacked ends when the client goes.
func TestAnArrivalReturnsWhenItsContextEndsDuringProbe(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)
	host := plane.Arrivals("test", twoVerbBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := newStuckProbeSession("SIM-STUCK-PROBE")

	t.Cleanup(func() { close(session.release) })

	_, err := host.Admit(t.Context(), session.identity)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	arrived := make(chan error, 1)

	go func() { arrived <- host.Arrived(ctx, session) }()

	<-session.called
	cancel()

	select {
	case err = <-arrived:
		require.ErrorIs(t, err, connectorapi.ErrSessionNotTaken,
			"the host took a session whose arrival ended before Probe answered")
		require.ErrorContains(t, err, "the arrival ended before the host took the session")
	case <-time.After(5 * time.Second):
		t.Fatal("the arrival still waits for a Probe after its context ended")
	}
}

// A driver whose Probe panics does not end the process. The printer arrives
// with the set its admission resolved.
func TestAPrinterWhoseDriverPanicsWhileReportingWhatItHasArrivesAnyway(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)
	host := plane.Arrivals("test", twoVerbBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &panickingProbeSession{talkingSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-PANIC-PROBE"},
			patches:  make(chan connectorapi.StatePatch),
		},
		answer: connectorapi.Result{OK: true},
	}}

	_, err := host.Admit(t.Context(), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(t.Context(), session))

	t.Cleanup(func() { close(session.patches) })

	device, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, plane.idOf(session.identity))
	require.NoError(t, err)
	require.Len(t, device.Capabilities, len(twoVerbBaseline()))
}

// dialsStuckProbe is a dialect for a printer the server dials. Probe on the
// session it returns does not return.
type dialsStuckProbe struct {
	baselineDriver

	session *stuckProbeSession
}

func (d dialsStuckProbe) Connect(
	context.Context, connectorapi.Endpoint, connectorapi.Set,
) (connectorapi.Session, error) {
	return d.session, nil
}

// The server stops while a dialled printer's driver has not answered Probe.
// The dial loop is the goroutine that waits for the answer, and stopping it
// must not wait for a driver that never answers.
func TestTheDialLoopStopsWhileADriverHasNotReportedWhatItsPrinterHas(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := newStuckProbeSession("DIAL-STUCK-PROBE")

	connector := dialledConnector{
		ref:      "near",
		driver:   dialsStuckProbe{baselineDriver: baselineDriver{baseline: twoVerbBaseline()}, session: session},
		baseline: twoVerbBaseline(),
		arrivals: plane.Arrivals("near", twoVerbBaseline(), capability.NewResolver(profile.Registry{}, discard()), ""),
	}

	said := &spokenTo{}

	loops := newDials(slog.New(slog.NewTextHandler(said, &slog.HandlerOptions{Level: slog.LevelDebug})),
		[]dialledConnector{connector}, fastLadder())
	loops.start(t.Context())

	// Probe is released before the loop is stopped. A loop that still waits for
	// Probe cannot stop, so stopping it first would never return.
	t.Cleanup(func() {
		close(session.release)
		loops.stop()
	})

	loops.SyncEndpoints(t.Context(), []resource.Endpoint{{
		Name: "bench", Connector: "near", Address: "printer.local:7125", Enabled: true,
	}})

	select {
	case <-session.called:
	case <-time.After(settle):
		t.Fatal("the dialled printer's Probe was never called")
	}

	stopped := make(chan struct{})

	go func() {
		loops.stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the dial loop did not stop while a driver's Probe had not answered")
	}

	require.False(t, said.contains("level=WARN msg=\"the host did not take"),
		"a dial loop that was stopping warned that the host refused the printer")
	require.True(t, said.contains("while its loop stopped"),
		"the refusal of an arrival whose loop stopped was not logged at all")
}
