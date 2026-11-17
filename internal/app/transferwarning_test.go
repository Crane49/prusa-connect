package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// The note a driver leaves on a transfer it could not confirm.
//
// BUG-014: a printer's own TRANSFER_FINISHED is not a file on its storage, so a
// driver reads the destination directory back. A firmware that cannot be asked
// for a listing leaves the transfer done and unconfirmed, and the caller learns
// which from this field rather than from a failure it can do nothing with.

// TestATransferThatCouldNotBeConfirmedCarriesTheDriversWarning is the gate on
// the field reaching the record and the answer the caller reads.
func TestATransferThatCouldNotBeConfirmedCarriesTheDriversWarning(t *testing.T) {
	t.Parallel()

	entry := &liveTransfer{record: transferapi.Record{
		TransferID: "01UNCONFIRMED", BytesTotal: 8490,
		State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
	}}

	const note = "the file was sent and this printer could not be asked whether it is there"

	entry.advance(connectorapi.Progress{Total: 8490, Sent: 8490, Warning: note})

	// A later report carries no warning, and the one already recorded stands: it
	// says the transfer finished unconfirmed, which a silent report does not
	// undo.
	entry.advance(connectorapi.Progress{Total: 8490, Sent: 8490})

	record := entry.settle(transferapi.StateDone, "", time.Now().UnixMilli())

	require.Equal(t, note, record.Warning, "the warning the driver reported is not on the record")
	require.Equal(t, note, transferOf(record).Warning,
		"the caller's answer carries no warning for a transfer that was never confirmed")
}

// TestAConfirmedTransferCarriesNoWarning is the other direction, and it is the
// case the field must stay absent on: a transfer whose file the driver saw in
// the printer's own listing is an ordinary success.
func TestAConfirmedTransferCarriesNoWarning(t *testing.T) {
	t.Parallel()

	entry := &liveTransfer{record: transferapi.Record{
		TransferID: "01CONFIRMED", BytesTotal: 8490,
		State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
	}}

	entry.advance(connectorapi.Progress{Total: 8490, Sent: 8490})

	record := entry.settle(transferapi.StateDone, "", time.Now().UnixMilli())

	require.Empty(t, record.Warning, "a confirmed transfer carries a warning")
	require.Empty(t, transferOf(record).Warning, "a confirmed transfer is answered with a warning")
}

// A driver reports from goroutines of its own, and the one serving the last
// range can report after Send has returned. The caller is answered from the
// record after it is settled, so a late report must leave the record settled.
func TestAReportAfterTheTransferSettledLeavesItSettled(t *testing.T) {
	t.Parallel()

	entry := &liveTransfer{record: transferapi.Record{
		TransferID: "01LATEREPORT", BytesTotal: 8490,
		State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
	}}

	entry.settle(transferapi.StateDone, "", time.Now().UnixMilli())

	_, running := entry.advance(connectorapi.Progress{Total: 8490, Sent: 8490})

	require.False(t, running, "a report after the settle counted as progress")
	require.Equal(t, transferapi.StateDone, entry.snapshot().State,
		"a report after the settle turned a done transfer back into a running one")
}

// A stop pressed after the last byte went out, while the driver was reading the
// destination directory back, is a cancelled transfer and not a confirmed one.
//
// The readback runs inside Send, after the printer has reported the transfer
// finished. A stop there cancels the listing, and a driver that read that as a
// printer it could not ask would answer the caller done with a warning about
// the printer: the row would record a success nobody asked for and blame the
// machine for it. The driver reports the cancellation instead, and this is what
// the row does with it.
func TestATransferStoppedInsideTheReadbackIsCancelledAndNotConfirmed(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)

		id := arrive(t, plane, "SIM-READBACK-STOP")

		entry, beginErr := plane.transfers.begin(transferapi.Record{
			TransferID: "01READBACK", Device: "device:" + id, BytesTotal: 8490, BytesSent: 8490,
			State: transferapi.StateSending, StartedMS: time.Now().UnixMilli(),
		}, "", func() {})
		require.NoError(t, beginErr)

		entry.stop("a person stopped it")

		plane.settleTransferRecord(t.Context(), entry,
			fmt.Errorf("xbuddy: confirming usb/part.bgcode reached the storage: %w", context.Canceled),
			time.Millisecond)

		frames := bus.awaitTransferFrames(t)

		var settled event.TransferSettled
		require.NoError(t, json.Unmarshal(frames[0].Data, &settled))

		require.Equal(t, event.TransferCancelled, settled.State,
			"a transfer stopped during its readback is not recorded as cancelled")
		require.Empty(t, settled.FaultCode, "a transfer a person stopped carries a fault code")

		t.Logf("observed: state=%s reason=%q", settled.State, settled.Reason)
	})
}
