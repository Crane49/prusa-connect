package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// What a restart does about a command the last process was holding.
//
// Measured on 2026-09-01: a command in flight across a restart left requested
// and authorized in the store and never a settled, and nothing closed the row.
// The invocation was over, the driver session it was waiting on died with the
// process, and the record said it was still running, for ever.
//
// Both engines, because the sweep is the composition root's and not the store's:
// what runs it is the history feed attaching a provider, and a deployment on
// PostgreSQL reaches that through a different adapter, a different migration
// and a different set of statements.

// TestARestartClosesTheCommandsTheLastRunLeftOpen is the gate.
//
// The open row is made rather than raced for: the first core runs a real
// command through the real plane, and then the settled half is removed from the
// store, which is exactly the state a process killed between dispatch and the
// printer's answer leaves behind. Racing a SIGKILL against a simulator that
// answers in microseconds would be a case that passes on a slow machine.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestARestartClosesTheCommandsTheLastRunLeftOpen(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		first := storingPastOn(t, dir, e)

		device := dialInImmediately(t, first, "SIM-RESTART")
		commandID := commandNow(t, first, device, "core.command.beep")

		store := e.Open(t, dir)
		settled := `SELECT count(*) FROM history_command WHERE command_id = '` + commandID + `'`

		require.Eventually(t, func() bool {
			return e.count(t, store, settled+` AND settled_us IS NOT NULL`) == 1
		}, settle, 100*time.Millisecond, "the command never settled while the first core ran")

		first.stopAndWait(t)

		// The state a process killed mid-command leaves: the instruction and
		// the authorization are stored, the outcome never arrived.
		e.exec(t, store,
			`UPDATE history_command SET settled_us = NULL, status = NULL, ok = NULL,
				outcome_unknown = NULL, machine_reason = NULL, fault_code = NULL,
				elapsed_ms = NULL WHERE command_id = '`+commandID+`'`)
		e.exec(t, store,
			`DELETE FROM history_fact WHERE type = '`+event.TypeDeviceCommandSettled+
				`' AND `+e.JSONText("payload", "command_id")+` = '`+commandID+`'`)

		require.Equal(t, 1, e.count(t, store, settled+` AND settled_us IS NULL`),
			"the case did not manage to leave an open command behind")

		// The second run. Nothing is dialled in: closing the record of the last
		// run must not depend on the printer coming back, because it may never.
		second := start(t, dir, e.Config(t, dir))

		var status string

		require.Eventually(t, func() bool {
			status = e.text(t, store,
				`SELECT coalesce(status, '') FROM history_command WHERE command_id = '`+
					commandID+`'`)

			return status != ""
		}, settle, 100*time.Millisecond, "a restart left command %s open for ever", commandID)

		require.Equal(t, event.CommandServerRestarted, status)

		require.Equal(t, 1, e.count(t, store,
			settled+` AND settled_us IS NOT NULL AND outcome_unknown = `+e.True),
			"the closing row does not say the outcome is unknown")

		// And the closing frame is a fact like any other, so a reader following
		// the event trail sees the invocation end rather than only the folded
		// row.
		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE type = '`+event.TypeDeviceCommandSettled+
					`' AND `+e.JSONText("payload", "command_id")+` = '`+commandID+`'`) == 1
		}, settle, 100*time.Millisecond,
			"the terminal frame of a restarted command reached no fact row")

		t.Logf("observed: on %s, after a restart command %s reads %s with the outcome unknown",
			e.Name, commandID, status)

		require.NoError(t, second.wait())
	})
}

// TestASecondRestartWritesNothingMore is the idempotence of the sweep. The
// closing frame is a fact with a position of its own, so a run that swept a
// store and a run that found nothing to sweep must leave the same rows.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestASecondRestartWritesNothingMore(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		first := storingPastOn(t, dir, e)

		device := dialInImmediately(t, first, "SIM-RESWEPT")
		commandID := commandNow(t, first, device, "core.command.beep")

		store := e.Open(t, dir)

		require.Eventually(t, func() bool {
			return e.count(t, store,
				`SELECT count(*) FROM history_command WHERE command_id = '`+commandID+`'`) == 1
		}, settle, 100*time.Millisecond)

		first.stopAndWait(t)

		before := e.count(t, store, `SELECT count(*) FROM history_fact`)

		second := start(t, dir, e.Config(t, dir))

		// Two flush intervals, so this is the absence of a write rather than a
		// race with one.
		time.Sleep(2 * historyFlushInterval)

		require.Equal(t, before, e.count(t, store, `SELECT count(*) FROM history_fact`),
			"a restart with nothing open still wrote a terminal frame")

		t.Logf("observed: on %s a restart over a store with no open command left %d facts as it "+
			"found them", e.Name, before)

		require.NoError(t, second.wait())
	})
}

// storingPastOn starts the core with a history instance on one engine and waits
// until that instance is serving.
//
// The wait is the property and not a convenience: a provider is a supervised
// resource, so it is attached a moment after the listener binds, and it stores
// what it sees from the moment it is attached.
func storingPastOn(t *testing.T, dir string, e historyEngine) *process {
	t.Helper()

	running := start(t, dir, e.Config(t, dir))

	require.Eventually(t, func() bool {
		return running.instanceState(t, "history/past") == "ready"
	}, settle, 100*time.Millisecond, "the declared history provider never reached ready")

	return running
}
