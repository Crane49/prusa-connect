package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestConnectedFollowsThePrinterAndNotTheSession is what the field on the wire
// says it is: whether a session is open right now. A printer keeps its host
// entry across an absence, because that is what lets a command queue for a
// machine that is switched off, so the entry existing is not the answer.
//
// At the bench this is the field somebody watches while they pull the cable out.
func TestConnectedFollowsThePrinterAndNotTheSession(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")
	printer := dialSimulator(t, running, "SIM-PRESENCE")

	listed := printers(t, running)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Connected, "a printer with a live socket reads as away")

	id := listed[0].ID

	printer.GoOffline()

	require.Eventually(t, func() bool {
		return !fetch(t, running, id).Connected
	}, settle, 25*time.Millisecond, "the printer went away and the listing still reads as connected")

	// The entry survives the absence, which is what the queue depends on.
	require.Len(t, printers(t, running), 1, "an absence removed the printer from the listing")

	printer.GoOnline()

	require.Eventually(t, func() bool {
		return fetch(t, running, id).Connected
	}, settle, 25*time.Millisecond, "the printer came back and the listing still reads as away")
}

// TestTheCableComingOutReachesAStreamThatWasAlreadyOpen is the bench, driven
// through the running binary on a real socket.
//
// The stream is opened before the printer dials in, against a configuration
// declaring no resource at all, which is what a fresh deployment is. Then the
// printer is taken off the wire and put back, and every frame a page watching
// the stream would see is asserted rather than inferred.
func TestTheCableComingOutReachesAStreamThatWasAlreadyOpen(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	// Open before anything has arrived, so nothing the caller may observe exists
	// yet. A refusal here would put the page on its own reconnect ladder for the
	// arrival it opened the stream to watch.
	frames := watch(t, running)

	printer := dialSimulator(t, running, "SIM-CABLE")

	require.Eventually(t, func() bool {
		return frames.saw(event.TypeDeviceArrived)
	}, settle, 25*time.Millisecond,
		"the printer that dialled in never reached the stream that was open before it")

	printer.GoOffline()

	require.Eventually(t, func() bool {
		return len(frames.presence()) == 1
	}, settle, 25*time.Millisecond,
		"the printer was taken off the wire and the event stream carried nothing")

	away := frames.presence()[0]
	require.Contains(t, away.data, `"connected":false`, "the frame does not say the printer is away")

	require.Zero(t, frames.count(event.TypeDeviceDeparted),
		"an absence was published as a departure, so a page would drop a printer that still queues "+
			"commands")

	printer.GoOnline()

	require.Eventually(t, func() bool {
		return len(frames.presence()) == 2
	}, settle, 25*time.Millisecond, "the printer came back and the event stream carried nothing")

	back := frames.presence()[1]
	require.Contains(t, back.data, `"connected":true`, "the frame does not say the printer is back")

	require.Equal(t, 1, frames.count(event.TypeDeviceArrived),
		"a printer that came back on the session it never lost was announced as a new arrival")
}

// TestABouncingCableIsNotAFleetOfArrivals is the rate on the running binary.
//
// A printer bouncing reconnects, and every reconnection is admitted and
// resolved again, so this is where a flap turns into durable frames if nothing
// bounds it. What a reader owes is the state the cable settled on: one arrival
// for the machine, and reachability at a rate rather than per bounce.
func TestABouncingCableIsNotAFleetOfArrivals(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")
	frames := watch(t, running)
	printer := dialSimulator(t, running, "SIM-FLAP")

	require.Eventually(t, func() bool {
		return frames.saw(event.TypeDeviceArrived)
	}, settle, 25*time.Millisecond, "the printer never announced itself")

	const bounces = 8

	for range bounces {
		printer.GoOffline()
		time.Sleep(120 * time.Millisecond)
		printer.GoOnline()
		time.Sleep(120 * time.Millisecond)
	}

	// Long enough for the last quiet period the flap armed to run out.
	time.Sleep(3 * presenceQuiet)

	require.Equal(t, 1, frames.count(event.TypeDeviceArrived),
		"a bouncing cable put an arrival on the bus per bounce, so a page reloads its whole fleet "+
			"every time a connector wobbles")

	seen := frames.presence()
	require.NotEmpty(t, seen, "a bouncing cable put nothing at all on the stream")
	require.LessOrEqualf(t, len(seen), bounces,
		"a bouncing cable put %d reachability frames on the bus for %d bounces", len(seen), bounces)
	require.Contains(t, seen[len(seen)-1].data, `"connected":true`,
		"the stream never carried the state the cable settled on")

	require.Zero(t, frames.count(event.TypeDeviceDeparted),
		"a bouncing cable read as the printer departing")
}

// fetch reads one printer through the published API.
func fetch(t *testing.T, running *process, id string) httpapi.Device {
	t.Helper()

	read := running.get(t, "/api/v1/devices/"+id)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var found httpapi.Device
	require.NoError(t, json.Unmarshal([]byte(read.body), &found))

	return found
}

// dialSimulator runs a simulated printer against the core and hands the test the
// handle, so it can take the printer off the wire and put it back.
func dialSimulator(t *testing.T, running *process, serial string) *simulator.Printer {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:   "MK4",
		Serial:  serial,
		BaseURL: "http://" + running.addr,
		// Short enough that a test does not spend its budget waiting for the
		// printer to come back after GoOnline.
		ReconnectDelay: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		stop()
		<-done
	})

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && telemetered(listed[0].State)
	}, settle, 25*time.Millisecond, "the simulated printer never appeared in the listing")

	return printer
}
