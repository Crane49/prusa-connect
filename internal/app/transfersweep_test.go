package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestARestartClosesTheTransfersTheLastRunLeftOpen is BUG-003's gate.
//
// The sweep that closes an interrupted transfer was called from the boot
// sequence, at the point the fleet is read back, which is about 1.8 seconds
// before a declared history instance reaches ready. It asked a provider that
// did not exist yet, was told this build keeps no transfer rows, and returned
// having read nothing, silently, because a build that keeps no rows is a
// legitimate deployment and not a failure. On the bench three rows left open by
// BUG-002 survived five restarts and the log never once carried the line the
// sweep writes.
//
// The row is made rather than raced for, the way the command sweep's gate makes
// one: a process killed mid-transfer leaves a started row with no ended_ms, and
// writing that state directly is what makes the case about the sweep instead of
// about how fast a simulator answers.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestARestartClosesTheTransfersTheLastRunLeftOpen(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		first := storingPastOn(t, dir, e)

		device := dialInImmediately(t, first, "SIM-TSWEEP")
		store := e.Open(t, dir)

		// The printer has to be in the record store, so that the next core
		// restores it and the origin the sweep publishes on exists.
		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE ref = 'device:`+device+`'`) > 0
		}, settle, 50*time.Millisecond, "the provider never saw printer %s", device)

		first.stopAndWait(t)

		const transferID = "01TRANSFERSWEPTONRESTART1"

		e.exec(t, store,
			`INSERT INTO history_transfer
				(transfer_id, ref, storage, source, destination, bytes_total, bytes_sent,
				 started_ms, state)
			 VALUES ('`+transferID+`', 'device:`+device+`', 'local', 'part.bgcode',
				 'usb/part.bgcode', 4096, 1024, 1, 'sending')`)

		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_transfer WHERE transfer_id = '`+transferID+
				`' AND ended_ms IS NULL`),
			"the case did not manage to leave an open transfer behind")

		// The second run. Nothing is dialled in: closing the record of the last
		// run must not depend on the printer coming back.
		second := start(t, dir, e.Config(t, dir))

		var state string

		require.Eventually(t, func() bool {
			state = e.text(t, store,
				`SELECT coalesce(state, '') FROM history_transfer WHERE transfer_id = '`+
					transferID+`' AND ended_ms IS NOT NULL`)

			return state != ""
		}, settle, 100*time.Millisecond,
			"a restart left transfer %s open for ever", transferID)

		require.Equal(t, event.TransferInterrupted, state)

		t.Logf("observed: on %s, after a restart transfer %s reads %s and how far it got is "+
			"still %s bytes", e.Name, transferID, state,
			e.text(t, store, `SELECT cast(bytes_sent as text) FROM history_transfer WHERE `+
				`transfer_id = '`+transferID+`'`))

		// And the terminal frame is a fact like any other, so a reader
		// following the trail sees the transfer end rather than only the folded
		// row changing under it.
		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_fact WHERE type = '`+
				event.TypeDeviceTransferSettled+`' AND `+
				e.JSONText("payload", "transfer_id")+` = '`+transferID+`'`),
			"the terminal frame of a swept transfer reached no fact row")

		require.NoError(t, second.wait())
	})
}
