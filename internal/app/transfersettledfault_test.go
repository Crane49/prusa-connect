package app

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// The fault code on a frame that has no fault.
//
// BUG-007: every settled fact for a transfer that ended done carried
// "fault_code":"unregistered", which is the registry's code of last resort and
// what an empty fault renders as. The field is omitempty, so the value was not
// empty: the host was asking an error that was nil what its code is, and being
// answered with the fallback.

// TestASuccessfulTransferCarriesNoFaultCode is the gate. A reader that branches
// on the presence of a fault code was told every transfer had one.
func TestASuccessfulTransferCarriesNoFaultCode(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-DONE")
		origin := "device:" + id

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01DONE", Device: origin, BytesTotal: 2557, BytesSent: 2557,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		// Nothing went wrong, which is the whole of the case: the driver reports no
		// failure, so there is no fault and nothing to name one.
		plane.settleTransferRecord(t.Context(), entry, nil, time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferDone, settled.State)
		require.Empty(t, settled.FaultCode,
			"a transfer that ended done carries a fault code")

		// And the field is absent on the wire rather than present and empty, which
		// is what omitempty on the schema promises a reader.
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(frames[0].Data, &raw))
		require.NotContains(t, raw, "fault_code",
			"the settled frame publishes fault_code for a transfer that succeeded")
	})
}

// TestNoErrorHasNoCode is the same rule one level down, where the trap is. Every
// frame that carries a fault code reads it through codeOf, so a nil error
// answering with the fallback code is a trap for each of them and not only for
// the transfer frame.
func TestNoErrorHasNoCode(t *testing.T) {
	t.Parallel()

	require.Empty(t, codeOf(nil), "an error that is not there was given a code")
}

// TestAStoppedTransferCarriesNoFaultCode is the other end of the same rule, and
// it is the case the first fix left behind: the code was read off the failure
// for every state, and the failure of a stopped transfer is the cancelled
// context the stop itself caused. A person pressing stop rendered
// fault_code:"unregistered", which reads as a fault this server could not name.
func TestAStoppedTransferCarriesNoFaultCode(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-STOPPED")
		origin := "device:" + id

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01STOPPED", Device: origin, BytesTotal: 2557, BytesSent: 1200,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		entry.stop("a person stopped it")

		// What the driver reports for a stopped transfer is the cancelled context,
		// which carries no registered fault of its own.
		plane.settleTransferRecord(t.Context(), entry, context.Canceled, time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferCancelled, settled.State)
		require.Empty(t, settled.FaultCode,
			"a transfer a person stopped carries a fault code")

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(frames[0].Data, &raw))
		require.NotContains(t, raw, "fault_code",
			"the settled frame publishes fault_code for a transfer a person stopped")

		t.Logf("observed: the stopped transfer reads state=%s fault_code=%q reason=%q",
			settled.State, settled.FaultCode, settled.Reason)
	})
}

// A driver can report a cancelled context that nobody asked for. The file may
// well be on the machine by then, so calling it a failure sends a person to send
// it again.
func TestACallerThatWentAwayCancelsRatherThanFails(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-GONE")
		origin := "device:" + id

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01GONE", Device: origin, BytesTotal: 2557, BytesSent: 2557,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		// Nobody pressed stop: the context ended under the transfer.
		plane.settleTransferRecord(t.Context(), entry, context.Canceled, time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferCancelled, settled.State)
		require.Empty(t, settled.FaultCode)
	})
}

// A refusal's own words are what a person acts on. The chain around them says
// only that the machine refused.
func TestARefusedTransferReadsTheMachinesOwnReason(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-REFUSED")
		origin := "device:" + id

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01REFUSED", Device: origin, BytesTotal: 2557,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		plane.settleTransferRecord(t.Context(), entry,
			connectorapi.TransferRefused.
				New("the printer would not take the file").
				With(
					fault.StringParam("machine_reason", "FILE_EXISTS"),
					fault.StringParam("reason", "File already exists"),
				),
			time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferFailed, settled.State)
		require.Equal(t, "File already exists", settled.Reason)
	})
}

// TestAFailedTransferStillCarriesItsCode keeps the fix from being a blanket
// silence. A transfer that failed on its own is the case the field exists for.
func TestAFailedTransferStillCarriesItsCode(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-FAILED")
		origin := "device:" + id

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01FAILED", Device: origin, BytesTotal: 2557, BytesSent: 1200,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		plane.settleTransferRecord(t.Context(), entry,
			connectorapi.TransferFailed.New("the transfer did not finish"), time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferFailed, settled.State)
		require.Equal(t, connectorapi.TransferFailed.Code().String(), settled.FaultCode,
			"a transfer that failed on its own carries no code")
	})
}
