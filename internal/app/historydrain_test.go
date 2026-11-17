package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// What a shutdown does about the frames published in the moment before it.
//
// Measured on 2026-09-02, BUG-002: three of eighteen transfers that the API
// answered "state":"done" for, and that the server itself logged as ended, had
// no settled fact at all and read back as `sending 0/312057` for ever. Every
// one of them ended within 30 ms of a graceful shutdown; every transfer
// followed by a few seconds of uptime was stored. The same shape closed a
// command's record: a command posted 130 ms before the process stopped left no
// row at all, not even the instruction.
//
// Two things were wrong and each is gated here.
//
// The first is the hub. A subscription coalesces on a 200 ms window, and a
// pump that returns the instant it is told to close discards whatever the
// window holds. Everything published in the last window before a shutdown is
// then thrown away between the publisher and the store, which is how
// "history provider detached ... failed_batches=0" appears in the bench log
// beside a fact that is not in the file: there was nothing left to fail on.
//
// The second is the order of the shutdown. The history feed's last write used
// to race the supervisor closing the very instance it writes into, and the
// bench log carries the losing side of that race as
// "a history provider refused a batch: history:past is stopping".
//
// Both are the composition root's, so both run on both engines.

// TestAFactPublishedJustBeforeAShutdownIsStored is the gate on the graceful
// case.
//
// The command settles in microseconds against a simulated printer, so the
// shutdown that follows it is inside the coalescing window by construction
// rather than by a sleep tuned to a machine. Nothing here waits for a flush:
// waiting for one would be the case proving that a frame the feed had already
// written is written, which is not the failure.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestAFactPublishedJustBeforeAShutdownIsStored(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		running := storingPastOn(t, dir, e)

		device := dialInImmediately(t, running, "SIM-DRAIN")
		commandID := commandNow(t, running, device, "core.command.beep")

		// Straight out, with no wait of any kind between the command settling
		// and the stop. This is the 30 ms window the bench measured.
		running.stopAndWait(t)

		store := e.Open(t, dir)

		settled := `SELECT count(*) FROM history_fact WHERE type = '` +
			event.TypeDeviceCommandSettled + `' AND ` +
			e.JSONText("payload", "command_id") + ` = '` + commandID + `'`

		requested := `SELECT count(*) FROM history_fact WHERE type = '` +
			event.TypeDeviceCommandRequested + `' AND ` +
			e.JSONText("payload", "command_id") + ` = '` + commandID + `'`

		t.Logf("observed: on %s, after a stop within the coalescing window of command %s, "+
			"the store holds %d requested and %d settled facts",
			e.Name, commandID, e.count(t, store, requested), e.count(t, store, settled))

		require.Equal(t, 1, e.count(t, store, requested),
			"the instruction published just before the shutdown reached no row")
		require.Equal(t, 1, e.count(t, store, settled),
			"the outcome published just before the shutdown reached no row")

		// And the folded row is closed, which is what a caller reads. A fact
		// that arrived and a fold that did not see it would be the same bug one
		// layer down.
		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_command WHERE command_id = '`+commandID+
				`' AND settled_us IS NOT NULL`),
			"the folded command row is still open after a clean shutdown")
	})
}

// TestACommandIsStoredBeforeTheFeedFlushes is the gate on the hard case.
//
// A SIGKILL cannot be staged against a core this suite runs in its own process,
// and it does not have to be: what a SIGKILL costs is everything the feed is
// holding, so the question is whether the record of a command exists in the
// store at the instant the command has been answered. If it does, the process
// can die at any moment after that and the row is there.
//
// The feed writes on a 1,000 row threshold or a two second timer, so before
// this fix a command's three frames were in memory for up to two seconds and a
// process that stopped inside that window left no row at all. The frames a
// caller's own action produces are therefore written through to the store as
// they are published, beside the feed rather than instead of it: a fact is
// idempotent on (ref, epoch, sequence), so the feed writing the same frame a
// moment later keeps one row.
//
// The bound is deliberately far below historyFlushInterval. A case that allowed
// two seconds would pass on the buffered feed alone and prove nothing.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestACommandIsStoredBeforeTheFeedFlushes(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		running := storingPastOn(t, dir, e)

		device := dialInImmediately(t, running, "SIM-THROUGH")

		store := e.Open(t, dir)

		// The write-through respects the provider's own grant, and the hub
		// resolves a grant made while a subscription was idle on its authorize
		// tick, once a second. So the case waits until the store holds
		// something about this printer, which is the store saying it can see
		// it, and only then sends the command. Skipping that wait would gate
		// the arrival window rather than the flush interval, and the arrival
		// window has a file of its own.
		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE ref = 'device:`+device+`'`) > 0
		}, settle, 50*time.Millisecond,
			"the provider never saw printer %s at all", device)

		commandID := commandNow(t, running, device, "core.command.beep")

		requested := `SELECT count(*) FROM history_fact WHERE type = '` +
			event.TypeDeviceCommandRequested + `' AND ` +
			e.JSONText("payload", "command_id") + ` = '` + commandID + `'`

		// A quarter of the feed's interval. Long enough that a write-through
		// which itself takes a lock is not raced; short enough that a run which
		// only passes because the feed flushed cannot reach it.
		within := historyFlushInterval / 4

		require.Eventually(t, func() bool {
			return e.count(t, store, requested) == 1
		}, within, 10*time.Millisecond,
			"on %s the instruction of command %s was not in the store within %s of the "+
				"caller being answered, so a process killed here would lose it",
			e.Name, commandID, within)

		t.Logf("observed: on %s the instruction of command %s was in the store within %s of "+
			"the caller being answered", e.Name, commandID, within)

		// The outcome too. It is published on a context detached from the
		// caller's, and it is the half a restart sweep has to invent when it is
		// missing.
		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE type = '`+
					event.TypeDeviceCommandSettled+`' AND `+
					e.JSONText("payload", "command_id")+` = '`+commandID+`'`) == 1
		}, within, 10*time.Millisecond,
			"the outcome of command %s was not in the store within %s", commandID, within)

		// And exactly one row of each, whatever the feed did afterwards. The
		// write-through and the feed carry the same frame with the same
		// position, so a store that kept two would be storing the same fact
		// twice.
		time.Sleep(2 * historyFlushInterval)

		require.Equal(t, 1, e.count(t, store, requested),
			"the instruction was stored twice: the write-through and the feed are not idempotent")

		require.NoError(t, running.wait())
	})
}
