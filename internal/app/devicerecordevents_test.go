package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// A record change on the bus, which is what a history provider waits on.
//
// The core stores a row per device and keeps no history of it. Something has to
// say what changed and when, or nothing outside this process can hold a past.
// The frame is com.prusa.connect.device.record.changed and every case below is
// about one of its three properties: it fires once per device per write, it
// carries the row and names what moved, and it reaches exactly the subscribers
// who may see that device.

// TestARecordChangeReachesASubscriberWatchingTheDevice drives the whole thing
// over the real composition root: the shipping binary's routes, its graph, its
// record store and its event stream, with a simulated printer on the wire.
//
// It is written as a subscriber rather than as a read of the store, because a
// history provider is a subscriber and what it can act on is the frame.
func TestARecordChangeReachesASubscriberWatchingTheDevice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, grantsConfig(dir))

	// Opened before the printer dials in, which is what makes the arrival a
	// frame this reader was given rather than one it went looking for.
	frames := watch(t, running)

	printer := runSimulator(t, running, simulator.Config{
		Model:  "MK4",
		Serial: "SIM-RECORDED",
	}, "")

	// Two frames, and the second is the point. The record is written the moment
	// the machine is registered, before its socket is up, so the row is stored
	// with the machine down and corrected at the next flush. Both writes are on
	// the bus, which is what a store that keeps no history of itself owes a
	// consumer that has to.
	require.Eventually(t, func() bool {
		return frames.count(event.TypeDeviceRecordChanged) == 2
	}, settle, 25*time.Millisecond,
		"a device was written to the record store and nothing on the bus said so")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	first := recordFrames(t, frames)[0]

	t.Logf("observed: the first record frame carried %s", first.raw)

	require.Equal(t, listed[0].ID, first.DeviceID, "the frame is about a device that is not in the listing")
	require.Equal(t, "xbuddy", first.Driver)
	require.Equal(t, "Prusa", first.Vendor)
	require.Equal(t, "MK4", first.Model)
	require.Equal(t, "SIM-RECORDED", first.Serial)
	require.NotEmpty(t, first.Firmware, "the frame carries no firmware, which is the field a record moves")
	require.Empty(t, first.Departure, "a device whose session is open reported a departure")
	require.False(t, first.LastSeen.IsZero(), "the frame carries no time the machine was last heard from")

	// The first frame about a device names every field it carries: a consumer
	// that has never heard of it holds none of them.
	require.Equal(t,
		[]string{"driver", "vendor", "model", "serial", "unit_uid", "firmware",
			"departure", "connected", "tags", "retired", "name"},
		first.Changed)

	up := recordFrames(t, frames)[1]

	t.Logf("observed: the frame that corrected the stored presence carried %s", up.raw)

	require.True(t, up.Connected, "the store was never corrected once the machine's socket was up")
	require.Equal(t, []string{"connected"}, up.Changed,
		"the frame named fields that did not move, so a consumer acts on a change that never happened")

	// The granularity, and the reason this frame is not the flush. The simulator
	// reports at 4 Hz and every frame marks the device for the store, so two
	// whole flush intervals is forty frames' worth of writes with nothing in the
	// record's own columns moving.
	telemetryBefore := frames.count(event.TypeDeviceTelemetrySampled)

	time.Sleep(2*recordFlushInterval + 500*time.Millisecond)

	grew := frames.count(event.TypeDeviceTelemetrySampled) - telemetryBefore

	t.Logf("observed: over two flush intervals the stream carried %d telemetry frames and %d record frames",
		grew, frames.count(event.TypeDeviceRecordChanged)-2)

	require.Greater(t, grew, 10,
		"the printer stopped reporting, so nothing here says anything about the record's rate")
	require.Equal(t, 2, frames.count(event.TypeDeviceRecordChanged),
		"a record frame per flush: the state tree moves on every telemetry frame, and a frame per "+
			"movement is one durable event per device every two seconds into a 32-event replay window")

	// A cable out. The session stays open, so this is not a departure, and the
	// stored row's presence column is what moved.
	takeOffTheWire(t, running, printer, listed[0].ID)

	require.Eventually(t, func() bool {
		return frames.count(event.TypeDeviceRecordChanged) == 3
	}, settle, 25*time.Millisecond,
		"the stored presence moved and the bus never carried the record it was written into")

	gone := recordFrames(t, frames)[2]

	t.Logf("observed: the frame after the cable came out carried %s", gone.raw)

	require.Equal(t, listed[0].ID, gone.DeviceID)
	require.False(t, gone.Connected)
	require.Contains(t, gone.Changed, "connected", "the frame does not name the field that moved")
	require.NotContains(t, gone.Changed, "model",
		"the frame named a field that did not move, so a consumer acts on a change that never happened")
	require.NotContains(t, gone.Changed, "serial")
	require.NotContains(t, gone.Changed, "driver")

	// The whole row is on the second frame too, which is what makes one frame
	// the whole answer rather than a delta a reader has to have a base for.
	require.Equal(t, "MK4", gone.Model)
	require.Equal(t, "SIM-RECORDED", gone.Serial)
}

// TestASubscriberGrantedNothingAboutADeviceIsNotToldItsRecordChanged is the
// negative, and it runs against the real authorization adapter over a real
// graph rather than a stub that answers yes.
//
// A record frame is about a device, so it is published to that device's origin
// and the visibility answer that already covers an arrival covers this too. The
// case exists because "already covered" is a claim, and a frame published to
// the wrong origin, or to none, would still reach the reader who may see
// everything and pass every other test in this file.
func TestASubscriberGrantedNothingAboutADeviceIsNotToldItsRecordChanged(t *testing.T) {
	t.Parallel()

	fleet := newRecordFleet(t, newGraph(t))

	granted := readFrames(t, fleet.hub, "user:"+config.DefaultPrincipal)
	stranger := readFrames(t, fleet.hub, "user:nobody")

	fleet.dialIn(t)

	require.Eventually(t, func() bool {
		return granted.count(event.TypeDeviceRecordChanged) == 1
	}, settle, 10*time.Millisecond,
		"the principal who may see the device was not told its record changed")

	fleet.session.setConnected(false)

	require.Eventually(t, func() bool {
		return granted.count(event.TypeDeviceRecordChanged) == 2
	}, settle, 10*time.Millisecond, "the presence write reached no subscriber")

	// Long enough that a frame published to an origin this reader can see would
	// have arrived, rather than long enough for nothing to have happened yet.
	time.Sleep(recordFlushInterval)

	require.Zero(t, stranger.count(event.TypeDeviceRecordChanged),
		"a subscriber holding no grant on the device was told its record changed, which is the "+
			"device's model, its serial and when it was last seen")
	require.Zero(t, stranger.total(),
		"a subscriber holding no grant on the device was given frames about it at all")
}

// TestTheRecordFrameFollowsTheWriteOnEveryEngine holds the ordering the whole
// design rests on: the frame says the record store holds this row, so it leaves
// after the transaction commits and never before it.
//
// It runs against every engine the build reached, because the write it follows
// is SQL and the two engines commit differently: SQLite has one writer and
// Postgres has a round trip per statement. AUTHZ_REQUIRED_ENGINES turns a
// missing engine from a skip into a failure, the way the graph's own suite does.
func TestTheRecordFrameFollowsTheWriteOnEveryEngine(t *testing.T) {
	t.Parallel()

	for _, engine := range recordEngines(t) {
		t.Run(engine.name, func(t *testing.T) {
			t.Parallel()

			fleet := newRecordFleet(t, engine.open(t))
			frames := readFrames(t, fleet.hub, "user:"+config.DefaultPrincipal)

			fleet.dialIn(t)

			require.Eventually(t, func() bool {
				return frames.count(event.TypeDeviceRecordChanged) == 1
			}, settle, 10*time.Millisecond, "no record frame reached a subscriber on %s", engine.name)

			// The store, read through the same handle the plane wrote with. A
			// frame whose row is not there is a history provider holding a
			// record no second reader of that table can find.
			stored, err := fleet.plane.records.DeviceRecords(t.Context())
			require.NoError(t, err)
			require.Len(t, stored, 1, "the frame was published and the row was not written")

			frame := recordFrames(t, frames)[0]

			t.Logf("observed: on %s the store held %s and the frame carried %s",
				engine.name, stored[0].DeviceID, frame.raw)

			require.Equal(t, stored[0].DeviceID, frame.DeviceID)
			require.Equal(t, stored[0].Model, frame.Model)
			require.Equal(t, stored[0].Serial, frame.Serial)

			// Presence is the one member with no column behind it. Earlier research
			// refused a presence column, so the frame is the only place a
			// subscriber reads it and there is no row to compare it against.
			require.True(t, frame.Connected, "the frame said a machine that had just dialled in was away")

			require.WithinDuration(t, stored[0].LastSeen, frame.LastSeen, time.Millisecond,
				"the frame reports a different last-seen from the row it says was written")
		})
	}
}

// TestAWriteThatFailedPublishesNothing keeps the frame honest about the store.
// A flush that could not commit leaves the record store behind, and a frame
// leaving anyway would tell a history provider about a row nothing holds.
func TestAWriteThatFailedPublishesNothing(t *testing.T) {
	t.Parallel()

	fleet := newRecordFleet(t, newGraph(t))
	frames := readFrames(t, fleet.hub, "user:"+config.DefaultPrincipal)

	fleet.plane.records = refusingRecords{records: fleet.plane.records}

	fleet.dialIn(t)

	// Every write this plane attempts is refused, so the arrival's own write is
	// too. Two flush intervals is every chance the timer gets.
	time.Sleep(2*recordFlushInterval + 500*time.Millisecond)

	require.Zero(t, frames.count(event.TypeDeviceRecordChanged),
		"a record store that refused every write still put a record on the bus")
	require.Positive(t, frames.count(event.TypeDeviceArrived),
		"nothing was published at all, so this case proves nothing about the record frame")
}

// recordFrames is every record frame a reader was given, decoded. It takes the
// two logs this package already has by their common shape, because one case
// reads the published SSE stream and the others read a subscription directly.
func recordFrames(t *testing.T, frames interface{ bodies(string) []string }) []decodedRecord {
	t.Helper()

	raw := frames.bodies(event.TypeDeviceRecordChanged)
	out := make([]decodedRecord, 0, len(raw))

	for _, body := range raw {
		var envelope struct {
			Data recordChange `json:"data"`
		}

		require.NoError(t, json.Unmarshal([]byte(body), &envelope), "a record frame is not an envelope")

		out = append(out, decodedRecord{recordChange: envelope.Data, raw: body})
	}

	return out
}

// bodies is every frame of one type, as the JSON envelope behind it.
func (l *frameLog) bodies(kind string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := []string{}

	for _, frame := range l.frames {
		if frame.Type == kind {
			out = append(out, string(frame.Bytes))
		}
	}

	return out
}

// total is how many frames of every type have arrived.
func (l *frameLog) total() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.frames)
}

// bodies is every frame of one type off the published event stream.
func (s *eventLog) bodies(kind string) []string {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	out := []string{}

	for _, frame := range s.frames {
		if frame.kind == kind {
			out = append(out, frame.data)
		}
	}

	return out
}

// decodedRecord is one record frame's payload with the bytes it came from, so a
// failure prints what was on the wire rather than a struct.
type decodedRecord struct {
	recordChange

	raw string
}

// recordFleet is a device plane over a real graph and a real hub, with the
// authorization adapter the shipping core uses, and one session to drive.
type recordFleet struct {
	graph   *authz.Store
	hub     *stream.Hub
	plane   *devices
	session *presenceSession
}

// recordedIdentity is the machine the plane-level cases drive.
var recordedIdentity = connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-RECORD-EVENT"}

// recordFleet stands one up over a graph the caller opened, which is what lets
// one case run against two engines.
func newRecordFleet(t *testing.T, graph *authz.Store) *recordFleet {
	t.Helper()

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Authorizer: visibility{graph: graph},
		Logger:     discard(),
		Interval:   stream.MinInterval,
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	return &recordFleet{
		graph: graph,
		hub:   hub,
		plane: newPlaneOn(t, graph, hub),
		session: &presenceSession{
			identity: recordedIdentity,
			patches:  make(chan connectorapi.StatePatch),
			changed:  make(chan struct{}, 1),
		},
	}
}

// dialIn admits the printer and hands the plane its session, which is what
// registers the device and writes its first record.
func (f *recordFleet) dialIn(t *testing.T) {
	t.Helper()

	f.session.setConnected(true)

	host := f.plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	_, err := host.Admit(t.Context(), recordedIdentity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(t.Context(), f.session))
}

// readFrames attaches one subscriber and collects everything it is handed.
func readFrames(t *testing.T, hub *stream.Hub, principal string) *frameLog {
	t.Helper()

	sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: principal})
	require.NoError(t, err)

	frames := &frameLog{}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for batch := range sub.Batches() {
			frames.take(batch)
		}
	}()

	// Registered before the subscription is closed so it runs after it: the
	// reader ends when the batch channel does.
	t.Cleanup(func() { <-done })
	t.Cleanup(sub.Close)

	return frames
}

// refusingRecords is a record store that will not write, which is a database
// that has stopped answering as far as the flush is concerned.
type refusingRecords struct {
	records
}

func (refusingRecords) SaveDeviceRecords(context.Context, ...devicestore.DeviceRecord) error {
	return errors.New("the record store is not answering")
}

// recordEngine is one SQL backend the record frame is proved against.
type recordEngine struct {
	name string
	open func(t *testing.T) *authz.Store
}

// recordEngines is what this run exercises. A missing engine is a skip unless
// AUTHZ_REQUIRED_ENGINES names it, in which case it is a failure: a run that
// reports ok while the engine it was invoked for never ran is a run that proved
// nothing.
func recordEngines(t *testing.T) []recordEngine {
	t.Helper()

	engines := []recordEngine{{name: "sqlite", open: newGraph}}
	required := slices.Contains(requiredEngineNames(), "postgres")

	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		if required {
			t.Fatalf("AUTHZ_REQUIRED_ENGINES names postgres and %s is not set", postgresDSNEnv)
		}

		return engines
	}

	if !postgresDriverLinked(t, dsn) {
		if required {
			t.Fatal("AUTHZ_REQUIRED_ENGINES names postgres and this build carries no postgres driver")
		}

		return engines
	}

	return append(engines, recordEngine{name: "postgres", open: func(t *testing.T) *authz.Store {
		t.Helper()

		return graphOnPostgres(t, dsn)
	}})
}

// requiredEngineNames is what the environment says this run has to exercise.
// The variable is internal/authz's and is spelled again here, because that
// package declares it in a test file no other package can see.
func requiredEngineNames() []string {
	var out []string

	for name := range strings.SplitSeq(os.Getenv("AUTHZ_REQUIRED_ENGINES"), ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// graphOnPostgres opens the graph on a scratch database of its own, through the
// same openGraph the composition root calls.
func graphOnPostgres(t *testing.T, dsn string) *authz.Store {
	t.Helper()

	core := config.Default()
	core.Overlay = "overlay.json"
	core.Modules = "modules"
	core.Authz.Database = filepath.Join(t.TempDir(), "unused", "authz.db")
	core.Persistence.Type = config.EnginePostgres
	core.Persistence.Postgres.DSN = scratchDatabase(t, dsn)

	require.NoError(t, core.Validate())

	host := openTestHost(t, core)

	return host.graph
}
