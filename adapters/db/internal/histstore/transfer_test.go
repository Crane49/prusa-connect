package histstore_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// transferBase is the instant the rows below carry.
var transferBase = time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)

// transferOrigin names one printer. The cases that page and filter write about
// more than one, so the name is a parameter rather than a constant.
func transferOrigin(t *testing.T, name string) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, name)
	require.NoError(t, err)

	return origin
}

// startedTransferFact and settledTransferFact are the two durable transitions
// the projection folds. progressedTransferFact is the telemetry frame it does
// not.
func startedTransferFact(t *testing.T, device string, sequence uint64, transferID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       transferBase,
		Ref:      transferOrigin(t, device),
		Type:     event.TypeDeviceTransferStarted,
		Sequence: sequence,
		Epoch:    "epoch-a",
		Class:    event.ClassState,
		Payload: json.RawMessage(`{"transfer_id":"` + transferID + `",` +
			`"storage":"library","source":"/models/cube.bgcode",` +
			`"destination":"usb/cube.bgcode","bytes_total":4096}`),
	}
}

// settledSequence is where the settled frame sits in its origin's numbering.
// Every case here settles the transfer the frame before it started, so the
// number is this helper's own rather than an argument that never varies.
const settledSequence = 2

// settledTransferFact is the frame that closes a transfer's row. Every case
// settles a transfer of the same device, so the device is this helper's own
// rather than an argument that never varies.
func settledTransferFact(t *testing.T, transferID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       transferBase.Add(3 * time.Second),
		Ref:      transferOrigin(t, "p-one"),
		Type:     event.TypeDeviceTransferSettled,
		Sequence: settledSequence,
		Epoch:    "epoch-a",
		Class:    event.ClassState,
		Payload: json.RawMessage(`{"transfer_id":"` + transferID + `",` +
			`"state":"failed","bytes_sent":1024,"bytes_total":4096,` +
			`"reason":"the device closed the connection","fault_code":"transfer.aborted",` +
			`"elapsed_ms":3000}`),
	}
}

func progressedTransferFact(t *testing.T, device string, sequence uint64, transferID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       transferBase.Add(time.Second),
		Ref:      transferOrigin(t, device),
		Type:     event.TypeDeviceTransferProgressed,
		Sequence: sequence,
		Epoch:    "epoch-a",
		Class:    event.ClassTelemetry,
		Payload: json.RawMessage(`{"transfer_id":"` + transferID + `",` +
			`"bytes_total":4096,"bytes_sent":1024}`),
	}
}

// TestATransfersTwoFramesFoldIntoOneRow is the whole projection: the core
// publishes two append-only transitions and folds nothing, so a store that
// wants one row per transfer derives it here, keyed by the host-minted id.
func TestATransfersTwoFramesFoldIntoOneRow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			startedTransferFact(t, "p-one", 1, "tr-1"),
			settledTransferFact(t, "tr-1"),
		}))

		records, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)
		require.Len(t, records, 1)

		require.Equal(t, transferapi.Record{
			TransferID:  "tr-1",
			Device:      transferOrigin(t, "p-one").String(),
			Storage:     "library",
			Source:      "/models/cube.bgcode",
			Destination: "usb/cube.bgcode",
			BytesTotal:  4096,
			BytesSent:   1024,
			State:       transferapi.StateFailed,
			Reason:      "the device closed the connection",
			FaultCode:   "transfer.aborted",
			StartedMS:   transferBase.UnixMilli(),
			EndedMS:     transferBase.Add(3 * time.Second).UnixMilli(),
		}, records[0])
	})
}

// TestATransferFoldConvergesWhateverOrderTheFramesArriveIn holds the fold
// against the delivery it will actually see. A replay out of the hub's window
// is not ordered against the live path, so a started frame can land after the
// settled one and must not reopen a transfer that has ended.
func TestATransferFoldConvergesWhateverOrderTheFramesArriveIn(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			settledTransferFact(t, "tr-1"),
		}))
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			startedTransferFact(t, "p-one", 1, "tr-1"),
		}))

		records, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)
		require.Len(t, records, 1)

		require.Equal(t, transferapi.StateFailed, records[0].State,
			"the late started frame reopened a transfer that had ended")
		require.Equal(t, transferBase.Add(3*time.Second).UnixMilli(), records[0].EndedMS)
		require.Equal(t, "library", records[0].Storage)
		require.Equal(t, "/models/cube.bgcode", records[0].Source)
		require.Equal(t, "usb/cube.bgcode", records[0].Destination)
	})
}

// TestReplayingATransferFrameChangesNothing is what makes the fold safe to run
// over a window a reader has already seen.
func TestReplayingATransferFrameChangesNothing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		pair := []historyapi.Fact{
			startedTransferFact(t, "p-one", 1, "tr-1"),
			settledTransferFact(t, "tr-1"),
		}

		require.NoError(t, store.AppendFacts(t.Context(), pair))

		first, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)

		require.NoError(t, store.AppendFacts(t.Context(), pair))

		second, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)

		require.Equal(t, first, second)
		require.Len(t, second, 1)
	})
}

// TestAProgressFrameIsNeverStoredAsATransferRow is the deliberate omission. A
// row per range of every file would grow with bytes moved rather than with
// things that happened, so the frame is stored as an ordinary fact and folded
// into nothing.
func TestAProgressFrameIsNeverStoredAsATransferRow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			progressedTransferFact(t, "p-one", 1, "tr-1"),
		}))

		records, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

// TestOnlyTheRunningTransfersAnswerTheRestartSweep is what the sweep asks for
// on the next start: the transfers that were open when this server stopped,
// which are the ones it has to mark interrupted.
func TestOnlyTheRunningTransfersAnswerTheRestartSweep(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			startedTransferFact(t, "p-one", 1, "tr-settled"),
			settledTransferFact(t, "tr-settled"),
			startedTransferFact(t, "p-one", 3, "tr-running"),
		}))

		records, err := store.Transfers(t.Context(), transferapi.Query{RunningOnly: true})
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "tr-running", records[0].TransferID)
		require.Equal(t, transferapi.StateSending, records[0].State)
		require.True(t, records[0].Running())
	})
}

// TestTransfersAreReadNewestFirstAndOnePageAtATime holds the order and the
// bound. The order is total: the id breaks a tie inside one millisecond, so a
// page boundary cannot fall between two rows and drop one of them.
func TestTransfersAreReadNewestFirstAndOnePageAtATime(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		facts := make([]historyapi.Fact, 0, 3)
		for i, id := range []string{"tr-a", "tr-b", "tr-c"} {
			fact := startedTransferFact(t, "p-one", uint64(i+1), id)
			fact.At = transferBase.Add(time.Duration(i) * time.Minute)
			facts = append(facts, fact)
		}

		require.NoError(t, store.AppendFacts(t.Context(), facts))

		records, err := store.Transfers(t.Context(), transferapi.Query{Limit: 2})
		require.NoError(t, err)
		require.Len(t, records, 2)
		require.Equal(t, "tr-c", records[0].TransferID)
		require.Equal(t, "tr-b", records[1].TransferID)

		all, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)
		require.Len(t, all, 3)
	})
}

// TestATransferQueryNamingADeviceReadsThatDeviceAlone is the filter the device
// page asks for. An empty Device reads the whole fleet, which is what the
// restart sweep asks for instead.
func TestATransferQueryNamingADeviceReadsThatDeviceAlone(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			startedTransferFact(t, "p-one", 1, "tr-one"),
			startedTransferFact(t, "p-two", 1, "tr-two"),
		}))

		records, err := store.Transfers(t.Context(), transferapi.Query{
			Device: transferOrigin(t, "p-two").String(),
		})
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "tr-two", records[0].TransferID)

		fleet, err := store.Transfers(t.Context(), transferapi.Query{})
		require.NoError(t, err)
		require.Len(t, fleet, 2)
	})
}
