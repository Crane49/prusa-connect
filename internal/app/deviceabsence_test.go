package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// absenceIdentity is the machine every case in this file drives.
var absenceIdentity = connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-ABSENT"}

// TestAnAbsenceReachesTheStreamAndIsNotADeparture is the bench story: somebody
// pulls the cable and watches a page that lives on the event stream.
//
// The two facts are separate and both have to be on the bus. A printer that is
// unreachable keeps its session, its queue and its place in the fleet, so
// reporting it as departed would take a commandable printer off a reader's
// screen; and a session that ends is a departure, which an absence is not.
func TestAnAbsenceReachesTheStreamAndIsNotADeparture(t *testing.T) {
	t.Parallel()

	// In a bubble: the quiet periods and the gaps between flaps are the bubble's
	// clock, so the case costs no wall clock and the host still sees every edge.
	synctest.Test(t, func(t *testing.T) {
		bench := watchedPlane(t)

		require.Eventually(t, func() bool {
			return bench.frames.count(event.TypeDeviceArrived) == 1
		}, settle, 10*time.Millisecond, "the printer never announced itself")

		bench.session.setConnected(false)

		require.Eventually(t, func() bool {
			return len(bench.frames.presence()) == 1
		}, settle, 10*time.Millisecond,
			"a printer was taken off the wire and the event stream said nothing")

		require.Equal(t, []bool{false}, bench.frames.presence(),
			"the frame reporting an absence does not say the printer is away")
		require.Zero(t, bench.frames.count(event.TypeDeviceDeparted),
			"an absence was published as a departure, which takes a printer that still queues commands "+
				"off a reader's fleet")

		_, live := bench.plane.entry(bench.plane.idOf(absenceIdentity))
		require.True(t, live, "an absence retired the entry, so the queue it exists for is gone")

		// A reader that saw the arrival, at this origin's first offset, and nothing
		// since, resumes and is given the absence. That is what makes it a state
		// event and not telemetry: telemetry is not retained, and a printer that is
		// away sends none, so there would be no later frame to learn it from.
		resumed := bench.replayFrom(t, 1)

		require.Eventually(t, func() bool {
			return len(resumed.presence()) == 1
		}, settle, 10*time.Millisecond,
			"a reader resuming from before the absence was not replayed it, so it holds a printer that "+
				"is not there")
		require.Equal(t, []bool{false}, resumed.presence())

		bench.session.setConnected(true)

		require.Eventually(t, func() bool {
			return len(bench.frames.presence()) == 2
		}, settle, 10*time.Millisecond, "the printer came back and the event stream said nothing")

		require.Equal(t, []bool{false, true}, bench.frames.presence(),
			"coming back is not visible on the stream")
		require.Zero(t, bench.frames.count(event.TypeDeviceArrived)-1,
			"a printer that came back on the same session was announced as a new arrival")

		// The session really ending is still a departure, and it is a different
		// frame from the absence above.
		require.NoError(t, bench.session.Close())

		require.Eventually(t, func() bool {
			return bench.frames.count(event.TypeDeviceDeparted) == 1
		}, settle, 10*time.Millisecond, "a session that ended never published a departure")
	})
}

// TestAFlappingLinkIsOneFramePerQuietPeriodAndNotOnePerFlap holds the rate.
//
// A cable in a socket it does not quite fit bounces at whatever rate the
// contact does, and every bounce is a state change. What a reader owes is the
// state the link settled on, not a frame per bounce, so the first change after
// a quiet stretch leaves immediately and everything the flap does afterwards is
// folded into the state the period ends on.
func TestAFlappingLinkIsOneFramePerQuietPeriodAndNotOnePerFlap(t *testing.T) {
	t.Parallel()

	// In a bubble: the quiet periods and the gaps between flaps are the bubble's
	// clock, so the case costs no wall clock and the host still sees every edge.
	synctest.Test(t, func(t *testing.T) {
		bench := watchedPlane(t)

		require.Eventually(t, func() bool {
			return bench.frames.count(event.TypeDeviceArrived) == 1
		}, settle, 10*time.Millisecond, "the printer never announced itself")

		// A link bouncing for a second, slowly enough that the host really does
		// observe every edge of it. Unrated this is a frame per edge.
		const (
			flaps = 70
			gap   = 15 * time.Millisecond
		)

		for i := range flaps {
			bench.session.setConnected(i%2 == 1)
			time.Sleep(gap)
		}

		// The state the link settles on, and the one the reader has to end up
		// holding. It is the opposite of the first edge, so a reporter that only
		// ever published the first change would fail here rather than pass on it.
		bench.session.setConnected(true)

		require.Eventually(t, func() bool {
			return len(bench.frames.presence()) >= 1
		}, settle, 10*time.Millisecond, "a link that bounced put nothing at all on the stream")

		// Long enough for every quiet period the flap could have armed to have run
		// out, so what is counted is everything the flap produced.
		time.Sleep(3 * presenceQuiet)

		seen := bench.frames.presence()

		require.LessOrEqualf(t, len(seen), int(flaps*gap/presenceQuiet)+3,
			"a flapping link put %d frames on the bus, which is a frame per bounce and not a rate", len(seen))
		require.True(t, seen[len(seen)-1],
			"the stream never carried the state the link settled on, so a reader is left believing the "+
				"printer is away")
	})
}

// benchPrinter is a device plane with one printer on it, a session the test takes on
// and off the wire, and a subscriber reading everything that printer publishes.
type benchPrinter struct {
	plane   *devices
	hub     *stream.Hub
	origin  event.Origin
	session *presenceSession
	frames  *frameLog
}

// watchedPlane stands one up.
func watchedPlane(t *testing.T) benchPrinter {
	t.Helper()

	// A device id is minted rather than derived from what the machine said it
	// is, and the authorizer has to name the origin before the
	// subscription is opened. So the id is minted here and bound to the
	// machine below, which is what an approved registration does.
	id := mintDeviceID()

	origin, err := event.NewOrigin(event.KindDevice, id)
	require.NoError(t, err)

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Authorizer: onePrinter{origin: origin},
		Logger:     discard(),
		Interval:   stream.MinInterval,
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	plane := newPlaneOn(t, newGraph(t), hub)
	plane.identities.bindIdentity(id, absenceIdentity)

	sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: "user:reader"})
	require.NoError(t, err)

	frames := &frameLog{}

	done := make(chan struct{})
	go func() {
		defer close(done)

		for batch := range sub.Batches() {
			frames.take(batch)
		}
	}()

	// Registered after the reader so it runs before it: the reader ends when the
	// subscription closes its batch channel, and waiting for it first would wait
	// for something nothing had asked to stop.
	t.Cleanup(func() { <-done })
	t.Cleanup(sub.Close)

	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &presenceSession{
		identity: absenceIdentity,
		patches:  make(chan connectorapi.StatePatch),
		changed:  make(chan struct{}, 1),
	}
	session.setConnected(true)

	_, err = host.Admit(t.Context(), absenceIdentity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(t.Context(), session))

	require.Equal(t, id, plane.idOf(absenceIdentity),
		"the printer arrived under an id nothing bound it to")

	return benchPrinter{plane: plane, hub: hub, origin: origin, session: session, frames: frames}
}

// replayFrom opens a second reader that has seen this printer's log up to
// offset and returns what it is handed to catch up.
func (b benchPrinter) replayFrom(t *testing.T, offset uint64) *frameLog {
	t.Helper()

	sub, err := b.hub.Subscribe(t.Context(), stream.SubscribeRequest{
		Principal: "user:latecomer",
		Resume: event.NewCursor(event.NewGeneration(b.hub.Epoch(),
			[]event.Position{{Origin: b.origin, Offset: offset}})),
	})
	require.NoError(t, err)

	replayed := &frameLog{}

	done := make(chan struct{})
	go func() {
		defer close(done)

		for batch := range sub.Batches() {
			replayed.take(batch)
		}
	}()

	t.Cleanup(func() { <-done })
	t.Cleanup(sub.Close)

	return replayed
}

// onePrinter is a stream.Authorizer that permits exactly one origin, so a test
// reads what a printer published without standing up a grant for it.
type onePrinter struct {
	origin event.Origin
}

func (o onePrinter) VisibleOrigins(context.Context, string) (stream.Visibility, error) {
	return stream.Visibility{Origins: []stream.Grant{{Origin: o.origin}}, Epoch: 1}, nil
}

func (o onePrinter) Epoch() uint64 { return 1 }

// presenceSession is a driver session that outlives the connection underneath
// it, which is the only kind of session an absence is a question about.
type presenceSession struct {
	identity connectorapi.Identity
	patches  chan connectorapi.StatePatch
	changed  chan struct{}
	shut     sync.Once

	mu        sync.Mutex
	connected bool
}

func (s *presenceSession) Identity() connectorapi.Identity { return s.identity }

func (s *presenceSession) Capabilities() (connectorapi.Set, uint64) { return connectorapi.Set{}, 1 }

func (s *presenceSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{OK: true}, nil
}

func (s *presenceSession) State() <-chan connectorapi.StatePatch { return s.patches }

// Close ends the session the way a driver does, by closing the state channel
// the host is reading.
func (s *presenceSession) Close() error {
	s.shut.Do(func() { close(s.patches) })

	return nil
}

func (s *presenceSession) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.connected
}

func (s *presenceSession) PresenceChanged() <-chan struct{} { return s.changed }

// setConnected moves the printer on or off the wire and wakes whoever is
// watching, dropping the wake-up when one is already waiting: the reader takes
// the answer from Connected, so a second wake-up carries nothing.
func (s *presenceSession) setConnected(connected bool) {
	s.mu.Lock()
	s.connected = connected
	s.mu.Unlock()

	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// frameLog is every frame one subscriber has been given, on both lanes. The
// telemetry lane is read too so that a case asserting a frame is durable fails
// when the frame arrives on the other one, rather than when it arrives nowhere.
type frameLog struct {
	mu     sync.Mutex
	frames []stream.Frame
}

func (l *frameLog) take(batch stream.Batch) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.frames = append(l.frames, batch.Telemetry...)
	l.frames = append(l.frames, batch.Events...)
}

// count is how many frames of one type have arrived.
func (l *frameLog) count(kind string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	total := 0

	for _, frame := range l.frames {
		if frame.Type == kind {
			total++
		}
	}

	return total
}

// presence is what every presence frame said, in arrival order.
func (l *frameLog) presence() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := []bool{}

	for _, frame := range l.frames {
		if frame.Type != event.TypeDevicePresenceChanged {
			continue
		}

		var envelope struct {
			Data struct {
				Connected bool `json:"connected"`
			} `json:"data"`
		}

		if json.Unmarshal(frame.Bytes, &envelope) != nil {
			continue
		}

		out = append(out, envelope.Data.Connected)
	}

	return out
}
