package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The order the three instants of a command are stamped in.
//
// BUG-001: on every stored command authorized_at fell 35 to 50 microseconds
// before requested_at. Nothing was wrong with the clock, and the stamps are
// UTC and consistent: the two frames were published the wrong way round, so the
// record said the command was allowed before anybody asked for it.

// TestTheStoredCommandInstantsRunForwards is the gate, on both engines because
// the instants come off the stored fold rather than off the frames.
//
//nolint:tparallel // the subtests are one printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestTheStoredCommandInstantsRunForwards(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		instantOrderCase(t, e)
	})
}

// instantOrderCase is the body above, per engine.
func instantOrderCase(t *testing.T, e historyEngine) {
	t.Helper()

	dir := t.TempDir()
	running := storingPastOn(t, dir, e)

	device := dialInImmediately(t, running, "SIM-INSTANTS")
	commandID := commandNow(t, running, device, "core.command.beep")

	var one httpapi.HistoryCommand

	require.Eventually(t, func() bool {
		page := commandsOf(t, running, device)
		if len(page.Items) != 1 || page.Items[0].Status == "" {
			return false
		}

		one = page.Items[0]

		return !one.Authorized.IsZero()
	}, settle, 100*time.Millisecond,
		"the command record of %s never carried a settled invocation", device)

	require.Equal(t, commandID, one.ID)

	// The caller asked, then this server allowed it, then the printer answered.
	// A record that puts them in any other order is a record of something that
	// did not happen.
	require.False(t, one.Authorized.Before(one.Requested),
		"authorized_at %s is before requested_at %s, by %s",
		one.Authorized.Format(time.RFC3339Nano), one.Requested.Format(time.RFC3339Nano),
		one.Requested.Sub(one.Authorized))

	require.False(t, one.Settled.Before(one.Authorized),
		"settled_at %s is before authorized_at %s",
		one.Settled.Format(time.RFC3339Nano), one.Authorized.Format(time.RFC3339Nano))

	t.Logf("observed: on %s, %s ran requested %s, authorized %s, settled %s",
		e.Name, commandID,
		one.Requested.Format(time.RFC3339Nano),
		one.Authorized.Format(time.RFC3339Nano),
		one.Settled.Format(time.RFC3339Nano))
}
