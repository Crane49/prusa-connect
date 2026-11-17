package histstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// A fact is written once and never edited.
//
// Every device event the dialect surfaces, a state change, a dialog, an error,
// a transfer, a job, reaches this store as a fact, and what makes the store
// worth reading is that a row means what it meant when it was written. A
// projection may be folded and refolded, because it is derived; the row it is
// derived from may not.
//
// Expiry is the one thing that removes a fact, and it removes rows rather than
// changing them: retention is the adapter's own configuration (ADR-20260901-rf2 rule
// 14, and the owner's decision of 2026-09-02 that retention is not a core
// rule), so a store that keeps 48 hours answers a query about last week with a
// refusal rather than with a quietly shortened page.

// TestAStoredFactIsNeverRewritten is the behaviour. A second append of the same
// (ref, epoch, sequence) carrying a different payload changes nothing, which is
// what makes a replay idempotent and an edit impossible through the one door
// this contract has.
func TestAStoredFactIsNeverRewritten(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		original := requestedFact(t, 1, "cmd-immutable")
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{original}))

		// The same place in the same origin's numbering, carrying something
		// else. This is the shape a rewrite would take if one were possible.
		forged := original
		forged.Payload = json.RawMessage(`{"command_id":"cmd-immutable","verb":"core.command.stop_print"}`)
		forged.Type = event.TypeDeviceCommandSettled

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{forged}))

		answer, err := store.QueryFacts(t.Context(),
			historyapi.FactQuery{Refs: []event.Origin{commandOrigin(t)}})
		require.NoError(t, err)
		require.Len(t, answer.Facts, 1, "one dedup key produced two rows")

		require.Equal(t, event.TypeDeviceCommandRequested, answer.Facts[0].Type,
			"a second append rewrote the stored fact")
		require.JSONEq(t, string(original.Payload), string(answer.Facts[0].Payload),
			"a second append rewrote the stored payload")
	})
}

// TestNothingInThisStoreUpdatesAFactRow reads the statements rather than the
// rows.
//
// The case above proves the door is shut; this one proves there is no second
// door. A store that gained an UPDATE against history_fact would still pass the
// case above, because that one only exercises the append path.
//
// DELETE is allowed in retention.go and nowhere else, and it is named here so
// that a delete written somewhere else has to move this list to pass.
func TestNothingInThisStoreUpdatesAFactRow(t *testing.T) {
	t.Parallel()

	// The file expiry is implemented in. Every other source file in the package
	// must hold no statement that removes a fact.
	const expiry = "retention.go"

	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources)

	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}

		text, readErr := os.ReadFile(source)
		require.NoError(t, readErr)

		lowered := strings.ToLower(string(text))

		require.NotContains(t, lowered, "update history_fact",
			"%s updates a stored fact, and a fact is written once", source)
		require.NotContains(t, lowered, "update history_gap",
			"%s updates a gap marker, and a gap marker is written once", source)

		if source == expiry {
			continue
		}

		require.NotContains(t, lowered, "delete from history_fact",
			"%s deletes a stored fact outside %s", source, expiry)
	}

	// And the migrations create no trigger that could do it behind the
	// statements above.
	files, err := filepath.Glob(filepath.Join("migrations", "*", "*.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		text, readErr := os.ReadFile(file)
		require.NoError(t, readErr)

		require.NotContains(t, strings.ToLower(string(text)), "create trigger",
			"%s installs a trigger, which is a writer this package cannot see", file)
	}

	t.Logf("observed: %d source files and %d migrations hold no statement that "+
		"rewrites a fact", len(sources), len(files))
}
