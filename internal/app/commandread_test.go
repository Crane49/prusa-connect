package app

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// Reading the command record back through the published API.
//
// The projection has been written since the store was built and queried by
// nothing outside the store's own tests, so an id a caller was handed at 200
// was not a thing that could be asked about. These two paths are that surface,
// and the class parameter below is the filter that was accepted and ignored.

// commandsOf reads one device's folded command record through the API.
func commandsOf(t *testing.T, running *process, device string) httpapi.HistoryCommandPage {
	t.Helper()

	read := running.get(t, "/api/v1/devices/"+device+"/commands")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page httpapi.HistoryCommandPage
	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page
}

// TestTheFoldedCommandRecordIsReadableThroughTheAPI is the read surface the
// projection never had.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestTheFoldedCommandRecordIsReadableThroughTheAPI(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		readbackCase(t, e)
	})
}

// readbackCase is the body above, per engine.
func readbackCase(t *testing.T, e historyEngine) {
	t.Helper()

	dir := t.TempDir()
	running := storingPastOn(t, dir, e)

	device := dialInImmediately(t, running, "SIM-READBACK")
	commandID := commandNow(t, running, device, "core.command.beep")

	var page httpapi.HistoryCommandPage

	// The requested and settled frames are written through on the caller's
	// goroutine and the audit frame reaches the store off the feed, on its
	// flush, so a row that carries a status does not yet carry all three
	// transitions. The wait is on the whole fold.
	require.Eventually(t, func() bool {
		page = commandsOf(t, running, device)

		return len(page.Items) == 1 && page.Items[0].Status != "" && page.Items[0].AuthType != ""
	}, settle, 100*time.Millisecond,
		"the command record of %s never carried a settled and authorized invocation", device)

	one := page.Items[0]

	require.Equal(t, commandID, one.ID)
	require.Equal(t, "core.command.beep", one.Verb)
	require.Equal(t, event.CommandSucceeded, one.Status)
	require.Equal(t, "device:"+device, one.Origin)
	require.False(t, one.Requested.IsZero(), "the folded row carries no requested instant")
	require.False(t, one.Settled.IsZero(), "the folded row carries no settled instant")

	// The deployment's own principal holds audit sight of the fleet, so the
	// audit half is here. A caller who does not hold it reads the same row with
	// these three members absent, which is the narrowing historyread.go makes.
	require.NotEmpty(t, one.AuthType, "the audit half is missing for a caller who may read it")
	require.NotEmpty(t, one.AuthID)
	require.False(t, one.Authorized.IsZero())

	// And the same row by its id alone, which is the question a caller holding
	// a command_id and no device id asks.
	read := running.get(t, "/api/v1/commands/"+commandID)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var byID httpapi.HistoryCommand
	require.NoError(t, json.Unmarshal([]byte(read.body), &byID))
	require.Equal(t, one, byID)

	// An id nothing was stored under is a 404 and not an empty row.
	missing := running.get(t, "/api/v1/commands/01M0000000000000000000000")
	require.Equal(t, http.StatusNotFound, missing.status, missing.body)

	t.Logf("observed: on %s, %s reads back as %s on %s, authorized by %s:%s",
		e.Name, commandID, one.Status, one.Origin, one.AuthType, one.AuthID)
}

// TestTheClassParameterNarrowsTheStoredEvents is the filter that was silently
// ignored. Every value answered the same rows, which a client cannot tell from
// a server that filtered and found everything.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestTheClassParameterNarrowsTheStoredEvents(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		classFilterCase(t, e)
	})
}

// classFilterCase is the body above, per engine.
func classFilterCase(t *testing.T, e historyEngine) {
	t.Helper()

	dir := t.TempDir()
	running := storingPastOn(t, dir, e)

	device := dialInImmediately(t, running, "SIM-CLASSES")
	commandID := commandNow(t, running, device, "core.command.beep")

	// Wait for the whole invocation to be stored, so the counts below are about
	// the filter rather than about the flush.
	store := e.Open(t, dir)

	require.Eventually(t, func() bool {
		return e.count(t, store,
			`SELECT count(*) FROM history_fact WHERE `+
				e.JSONText("payload", "command_id")+` = '`+commandID+`'`) >= 3
	}, settle, 100*time.Millisecond)

	// The origin is named, because a fleet-wide query fails closed on the audit
	// class: it is answered only where the caller holds audit sight of every
	// origin in it, and a running deployment has origins that are not devices.
	// That is ADR-20260901-atx rule 13 and not this parameter's doing.
	where := "/api/v1/history/facts?limit=500&origins=device:" + device

	every := storedFacts(t, running, where)
	command := storedFacts(t, running, where+"&classes=command")
	audit := storedFacts(t, running, where+"&classes=audit")
	state := storedFacts(t, running, where+"&classes=state")

	t.Logf("observed: on %s, every=%d command=%d audit=%d state=%d",
		e.Name, len(every), len(command), len(audit), len(state))

	require.NotEmpty(t, command)
	require.NotEmpty(t, audit)
	require.NotEmpty(t, state)

	require.Less(t, len(command), len(every),
		"the class filter answered every row it was asked to exclude")

	// Each answer holds only the types that class carries.
	for _, one := range command {
		require.Equal(t, event.TypeDeviceCommandRequested, one.Type,
			"a class=command query answered a %s", one.Type)
	}

	for _, one := range audit {
		require.Equal(t, event.TypeDeviceCommandAuthorized, one.Type,
			"a class=audit query answered a %s", one.Type)
	}

	require.Equal(t, len(every), len(command)+len(audit)+len(state),
		"the three classes do not partition what an unfiltered query answers")

	// A word this build has no class for is refused rather than ignored.
	refused := running.get(t, "/api/v1/history/facts?classes=weather")
	require.Equal(t, http.StatusUnprocessableEntity, refused.status, refused.body)
}

// storedFacts reads one page of stored events through the API.
func storedFacts(t *testing.T, running *process, path string) []httpapi.HistoryFact {
	t.Helper()

	read := running.get(t, path)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page httpapi.HistoryFactPage
	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page.Items
}
