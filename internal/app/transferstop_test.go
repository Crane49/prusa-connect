package app

import (
	"database/sql"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestACallerCanStopATransferOnlyTheStoreRemembers is BUG-009's gate.
//
// The register is this process's memory, so a transfer another run began is not
// in it. Stopping one answered 404 TRANSFER_NOT_FOUND while the row went on
// reading `sending 0/312057`, and the printer went on holding the transfer
// slot: on a CORE One L a file our record called done could not be deleted,
// with machine_reason "File is being transferred", and the firmware retried its
// own cleanup every 0.5 s for 47 minutes.
//
// Two answers are gated here, because the sweep and this fallback divide the
// work. A row the start-up sweep has closed is over, and stopping it answers
// 404 the way stopping any settled transfer does. A row the sweep did not close
// is stopped: the printer is told to release the slot, and the row is closed as
// cancelled.
//
// The stranded row is written after the sweep has been seen closing another
// one, which is what makes the second half about the fallback rather than a
// race with the sweep. It is also the state a live server reaches: the sweep
// runs once, when the store reaches ready, and a row it could not read then
// stays open for the rest of the run.
//
//nolint:tparallel // the subtests are one core and two simulated printers each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestACallerCanStopATransferOnlyTheStoreRemembers(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		first := storingPastOn(t, dir, e)

		device := dialInImmediately(t, first, "SIM-TSTOP")
		store := e.Open(t, dir)

		// The printer has to be in the record store, so that the next core
		// restores it and dials it back in under the same id.
		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE ref = 'device:`+device+`'`) > 0
		}, settle, 50*time.Millisecond, "the provider never saw printer %s", device)

		first.stopAndWait(t)

		const (
			swept    = "01TRANSFERTHESWEEPCLOSED1"
			stranded = "01TRANSFERONLYTHESTOREHAS"
		)

		// Five seconds of transfer before the process died, so what the closed
		// row reports for elapsed can be read as a duration.
		startedMS := time.Now().UnixMilli() - 5_000

		openTransferRow(t, e, store, openRow{
			id: swept, device: device, path: "usb/swept.bgcode", startedMS: startedMS,
		})

		// The second run, and the printer comes back to it. The fallback tells
		// the machine to let go of the slot, so the machine has to be there.
		second := start(t, dir, e.Config(t, dir))

		require.Eventually(t, func() bool {
			return e.text(t, store, `SELECT coalesce(state, '') FROM history_transfer WHERE `+
				`transfer_id = '`+swept+`' AND ended_ms IS NOT NULL`) == event.TransferInterrupted
		}, settle, 100*time.Millisecond, "the restart never closed transfer %s", swept)

		back := dialInImmediately(t, second, "SIM-TSTOP")
		require.Equal(t, device, back, "the printer came back under another id")

		// And its socket is up, which the listing says and the restored record
		// does not: a device read back from the record store is listed before
		// the machine it names has dialled in.
		waitUntilConnected(t, second, device)

		// Half one: the sweep has already ended this transfer, so there is
		// nothing left to stop and the caller is told so.
		refused := second.call(t, http.MethodPost,
			"/api/v1/devices/"+device+"/transfers/"+swept+"/stop", "")
		require.Equal(t, http.StatusNotFound, refused.status, refused.body)

		// Half two: a row this run has never held and the sweep has been past.
		openTransferRow(t, e, store, openRow{
			id: stranded, device: device, path: "usb/stranded.bgcode", startedMS: startedMS,
		})

		// It belongs to one device. A caller who may operate another printer
		// cannot end it through that printer, and the row stays open.
		other := dialInImmediately(t, second, "SIM-TSTOP-OTHER")

		elsewhere := second.call(t, http.MethodPost,
			"/api/v1/devices/"+other+"/transfers/"+stranded+"/stop", "")
		require.Equal(t, http.StatusNotFound, elsewhere.status, elsewhere.body)
		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_transfer WHERE transfer_id = '`+stranded+
				`' AND ended_ms IS NULL`),
			"a stop through another device closed the row anyway")

		stopped := second.call(t, http.MethodPost,
			"/api/v1/devices/"+device+"/transfers/"+stranded+"/stop", "")
		require.Equal(t, http.StatusNoContent, stopped.status, stopped.body)

		var state string

		require.Eventually(t, func() bool {
			state = e.text(t, store, `SELECT coalesce(state, '') FROM history_transfer WHERE `+
				`transfer_id = '`+stranded+`' AND ended_ms IS NOT NULL`)

			return state != ""
		}, settle, 100*time.Millisecond,
			"the caller was answered and transfer %s stayed open", stranded)

		require.Equal(t, event.TransferCancelled, state)

		// How far it got is what the last run measured. A row closed here is
		// not a row rewritten here.
		require.Equal(t, "1024", e.text(t, store,
			`SELECT cast(bytes_sent as text) FROM history_transfer WHERE transfer_id = '`+
				stranded+`'`), "closing the row rewrote how far the transfer got")

		// The elapsed time is a duration and not the age of the epoch, which is
		// what a row closed without an end stamp reports.
		elapsed := transferElapsed(t, e, store, stranded)
		require.Greater(t, elapsed, 4*time.Second,
			"the closed row reports %s between the transfer starting and ending", elapsed)
		require.Less(t, elapsed, time.Hour,
			"the closed row reports %s, so the end stamp is not the transfer's", elapsed)

		// The terminal frame is a fact like any other, so a reader following
		// the trail sees the transfer end rather than only the row changing
		// under it.
		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_fact WHERE type = '`+
				event.TypeDeviceTransferSettled+`' AND `+
				e.JSONText("payload", "transfer_id")+` = '`+stranded+`'`),
			"the terminal frame of a stopped transfer reached no fact row")

		// And the printer was told, which is the half the firmware needs. The
		// command goes down the ordinary path, so the machine answering it is a
		// stored command row of its own.
		told := `SELECT count(*) FROM history_command WHERE ref = 'device:` + device +
			`' AND verb = '` + verbStopTransfer + `' AND status = '` + event.CommandSucceeded + `'`

		require.Eventually(t, func() bool {
			return e.count(t, store, told) > 0
		}, settle, 100*time.Millisecond,
			"printer %s was never told to release the slot of transfer %s", device, stranded)

		t.Logf("observed: on %s, transfer %s reads %s at 1024 bytes after %s, and printer %s "+
			"answered %s", e.Name, stranded, state, elapsed, device, verbStopTransfer)

		require.NoError(t, second.wait())
	})
}

// openRow is the row a process killed mid-transfer leaves behind.
type openRow struct {
	id        string
	device    string
	path      string
	startedMS int64
}

// openTransferRow writes one started transfer with no ended_ms, which nothing
// in a running core can be asked to produce on purpose.
func openTransferRow(t *testing.T, e historyEngine, store *sql.DB, row openRow) {
	t.Helper()

	e.exec(t, store,
		`INSERT INTO history_transfer
			(transfer_id, ref, storage, source, destination, bytes_total, bytes_sent,
			 started_ms, state)
		 VALUES ('`+row.id+`', 'device:`+row.device+`', 'local', 'part.bgcode',
			 '`+row.path+`', 312057, 1024, `+strconv.FormatInt(row.startedMS, 10)+
			`, 'sending')`)

	require.Equal(t, 1, e.count(t, store,
		`SELECT count(*) FROM history_transfer WHERE transfer_id = '`+row.id+
			`' AND ended_ms IS NULL`),
		"the case did not manage to leave transfer %s open", row.id)
}

// transferElapsed reads what the closed row says one transfer took.
func transferElapsed(t *testing.T, e historyEngine, store *sql.DB, id string) time.Duration {
	t.Helper()

	read := e.text(t, store,
		`SELECT cast(coalesce(elapsed_ms, 0) as text) FROM history_transfer WHERE `+
			`transfer_id = '`+id+`'`)

	milliseconds, err := strconv.ParseInt(read, 10, 64)
	require.NoError(t, err, "the row reports %q for elapsed_ms", read)

	return time.Duration(milliseconds) * time.Millisecond
}

// waitUntilConnected waits for one device's own socket to be up, which is the
// earliest moment a command reaches the machine rather than its record.
func waitUntilConnected(t *testing.T, running *process, device string) {
	t.Helper()

	require.Eventually(t, func() bool {
		for _, one := range printers(t, running) {
			if one.ID == device {
				return one.Connected
			}
		}

		return false
	}, settle, 25*time.Millisecond, "printer %s never dialled back in", device)
}
