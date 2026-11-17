package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// keptTransfers is a history adapter's transfer projection, as this plane reads
// it. It is a fake and not a real store because what is under test here is what
// the plane does with the rows, and the fold that produces them is tested on
// both engines in adapters/db.
type keptTransfers struct {
	rows []transferapi.Record
	// asked is the last query, so a case can prove the sweep asks for the
	// running transfers rather than filtering a whole table in memory.
	asked transferapi.Query
}

func (k *keptTransfers) Transfers(
	_ context.Context, query transferapi.Query,
) ([]transferapi.Record, error) {
	k.asked = query

	out := make([]transferapi.Record, 0, len(k.rows))

	for _, row := range k.rows {
		if query.RunningOnly && !row.Running() {
			continue
		}

		if query.Device != "" && row.Device != query.Device {
			continue
		}

		out = append(out, row)
	}

	return out, nil
}

// transferFrames is the transfer lifecycle frames the bus carried, in arrival
// order.
func (r *busReader) transferFrames() []event.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]event.Envelope, 0, len(r.frames))

	for _, envelope := range r.frames {
		if strings.HasPrefix(envelope.Type, event.CoreNamespace+"device.transfer.") {
			out = append(out, envelope)
		}
	}

	return out
}

// awaitTransferFrames waits for the first transfer frame and returns every
// frame that had arrived with it.
func (r *busReader) awaitTransferFrames(t *testing.T) []event.Envelope {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		got := r.transferFrames()
		if len(got) > 0 {
			return got
		}

		if time.Now().After(deadline) {
			t.Fatal("no transfer frame arrived")
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// A transfer that was in flight when the process stopped is over, and the next
// start is what says so. The device has forgotten it and the bytes it holds are
// a prefix nobody can name, so nothing is resumed: the row is closed with the
// reason, and it is closed by publishing the frame the fold already knows how
// to read rather than by writing the row a second way.
func TestATransferLeftOpenByTheLastRunIsRecordedInterrupted(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		// A real printer, because the bus only carries an origin the subscriber may
		// see and a made-up id is invisible to it. The transfer the last run left
		// open is one of this printer's.
		id := arrive(t, plane, "SIM-SWEEP")
		origin := "device:" + id

		kept := &keptTransfers{rows: []transferapi.Record{
			{
				TransferID: "01TRANSFERLEFTRUNNING0001", Device: origin,
				Storage: "scratch", Source: "part.bgcode", Destination: "usb/part.bgcode",
				BytesTotal: 4096, BytesSent: 1024,
				State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
			},
			{
				TransferID: "01TRANSFERTHATFINISHED001", Device: origin,
				State: transferapi.StateDone, StartedMS: time.Now().UnixMilli(),
				EndedMS: time.Now().UnixMilli(),
			},
		}}

		plane.past = func() (transferapi.Records, bool) { return kept, true }

		plane.sweepInterruptedTransfers(t.Context())

		require.True(t, kept.asked.RunningOnly,
			"the sweep read the whole table rather than the transfers that were still running")

		frames := bus.awaitTransferFrames(t)
		require.Len(t, frames, 1, "the transfer that had already finished was settled a second time")

		require.Equal(t, event.TypeDeviceTransferSettled, frames[0].Type)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, "01TRANSFERLEFTRUNNING0001", settled.TransferID)
		require.Equal(t, event.TransferInterrupted, settled.State)
		require.Equal(t, "server restarted", settled.Reason)
		require.Equal(t, int64(1024), settled.BytesSent,
			"the record has to say how far the interrupted transfer got")

		t.Logf("observed: %s closed as %s, %q", settled.TransferID, settled.State, settled.Reason)
	})
}

// A build whose history adapter keeps no transfer rows sweeps nothing and does
// not fail. The projection is an ability an adapter has or has not, so a
// deployment on an adapter without one is a deployment with less recorded
// rather than one that will not start.
func TestAStoreThatKeepsNoTransferRowsIsSweptWithoutComplaint(t *testing.T) {
	t.Parallel()

	plane, bus, _ := planeBusAndGraph(t)

	plane.past = func() (transferapi.Records, bool) { return nil, false }
	plane.sweepInterruptedTransfers(t.Context())

	// A plane with no resolver at all is the same answer, which is what a test
	// constructing the plane directly leaves behind.
	plane.past = nil
	plane.sweepInterruptedTransfers(t.Context())

	require.Empty(t, bus.transferFrames(), "a build with no projection published a transfer frame")
}

// The register is what a live byte count exists in, and it holds what is
// running and nothing else. A transfer that settled has to leave it, because
// from that moment the stored row is the one that says how it ended and two
// answers to one id is the shape where a listing shows a transfer twice.
func TestTheRegisterHoldsWhatIsRunningAndDropsWhatSettled(t *testing.T) {
	t.Parallel()

	register := newTransferRegister()

	record := transferapi.Record{
		TransferID: "01RUNNING", Device: "device:pone", BytesTotal: 4096,
		State: transferapi.StateQueued, StartedMS: time.Now().UnixMilli(),
	}

	entry, err := register.begin(record, "", nil)
	require.NoError(t, err)

	other, err := register.begin(transferapi.Record{
		TransferID: "01ELSEWHERE", Device: "device:ptwo",
		State: transferapi.StateQueued, StartedMS: time.Now().UnixMilli(),
	}, "", nil)
	require.NoError(t, err)

	require.NotNil(t, other)

	running := register.running("device:pone")
	require.Len(t, running, 1, "the register answered for a device that was not asked about")
	require.Equal(t, "01RUNNING", running[0].TransferID)

	entry.advance(connectorapi.Progress{Total: 4096, Sent: 1024})

	running = register.running("device:pone")
	require.Equal(t, int64(1024), running[0].BytesSent)
	require.Equal(t, transferapi.StateSending, running[0].State)

	_, found := register.find("device:pone", "01RUNNING")
	require.True(t, found)

	// A transfer of another device is not found under this one, so an id
	// guessed by a caller who may read one printer cannot reach another's.
	_, found = register.find("device:pone", "01ELSEWHERE")
	require.False(t, found, "one device's transfer was reachable through another's id")

	register.end("01RUNNING")
	require.Empty(t, register.running("device:pone"))
}

// Stopping a transfer says so on the record, so the frame that closes it reads
// "a person pressed stop" and not "the server broke". The two are the same
// cancelled context to the driver, and only the register knows which it was.
func TestAStoppedTransferIsRecordedCancelledAndNotFailed(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-STOPPED")
		origin := "device:" + id

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01STOPPED", Device: origin, BytesTotal: 4096, BytesSent: 512,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", cancel)
		require.NoError(t, beginErr)

		entry.stop("a caller stopped the transfer")

		require.Error(t, ctx.Err(), "stopping the transfer did not cancel what it was running under")

		// The failure the driver reports for a stopped transfer is the cancelled
		// context, which is exactly the case this has to read as a cancellation.
		plane.settleTransferRecord(t.Context(), entry, ctx.Err(), time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferCancelled, settled.State)
		require.Equal(t, "a caller stopped the transfer", settled.Reason)
		require.Equal(t, int64(512), settled.BytesSent)

		require.Empty(t, plane.transfers.running(origin),
			"a settled transfer is still on the register")
	})
}
