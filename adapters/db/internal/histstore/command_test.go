package histstore_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/histstore"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// commandBase is the instant the rows below carry.
var commandBase = time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

// commandOrigin is the printer every case here writes about.
func commandOrigin(t *testing.T) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, "p-commanded")
	require.NoError(t, err)

	return origin
}

// requestedFact and settledFact are the two transitions the projection folds.
func requestedFact(t *testing.T, sequence uint64, commandID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       commandBase,
		Ref:      commandOrigin(t),
		Type:     event.TypeDeviceCommandRequested,
		Sequence: sequence,
		Epoch:    "epoch-a",
		Class:    event.ClassCommand,
		Payload: json.RawMessage(`{"command_id":"` + commandID + `",` +
			`"verb":"core.command.set_nozzle_temperature","key":"k-1",` +
			`"args":{"nozzle_temperature":"215"}}`),
	}
}

func settledFact(t *testing.T, sequence uint64, commandID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       commandBase.Add(time.Second),
		Ref:      commandOrigin(t),
		Type:     event.TypeDeviceCommandSettled,
		Sequence: sequence,
		Epoch:    "epoch-a",
		Class:    event.ClassState,
		Payload: json.RawMessage(`{"command_id":"` + commandID + `",` +
			`"verb":"core.command.set_nozzle_temperature","status":"succeeded",` +
			`"ok":true,"elapsed_ms":42}`),
	}
}

func authorizedFact(t *testing.T, sequence uint64, commandID string) historyapi.Fact {
	t.Helper()

	return historyapi.Fact{
		At:       commandBase,
		Ref:      commandOrigin(t),
		Type:     event.TypeDeviceCommandAuthorized,
		Sequence: sequence,
		Epoch:    "epoch-a",
		Class:    event.ClassAudit,
		Payload: json.RawMessage(`{"command_id":"` + commandID + `",` +
			`"verb":"core.command.set_nozzle_temperature",` +
			`"auth_type":"user","auth_id":"operator"}`),
	}
}

// TestTheProjectionFoldsTheTransitionsInAnyOrder is ADR-20260901-atx rule 14. The core
// publishes append-only transitions and folds nothing; a store that wants a
// mutable command row derives it, keyed by command_id and driven by event type.
// The fold has to converge whatever order the frames arrive in, because a
// replay out of the hub's window is not ordered against the live path.
func TestTheProjectionFoldsTheTransitionsInAnyOrder(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		// Settled first, then requested, then both again: out of order and
		// replayed. One row must come out of it.
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			settledFact(t, 2, "cmd-1"),
		}))

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			requestedFact(t, 1, "cmd-1"),
			authorizedFact(t, 3, "cmd-1"),
		}))
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			settledFact(t, 2, "cmd-1"), requestedFact(t, 1, "cmd-1"),
		}))

		var (
			rows     int
			verb     string
			key      string
			status   string
			elapsed  int64
			args     string
			settleAt int64
		)

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_command`).Scan(&rows))
		require.Equal(t, 1, rows, "the fold produced more than one row for one command")

		// The id is this file's own literal and no caller value reaches the
		// statement, which is how the schema case beside this one is written
		// too: the two engines disagree about placeholders and the point here
		// is the row, not the binding.
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT verb, key, status, elapsed_ms, args, settled_us
				FROM history_command WHERE command_id = 'cmd-1'`).
			Scan(&verb, &key, &status, &elapsed, &args, &settleAt))

		require.Equal(t, "core.command.set_nozzle_temperature", verb)
		require.Equal(t, "k-1", key)
		require.Equal(t, "succeeded", status)
		require.Equal(t, int64(42), elapsed)
		require.Contains(t, args, "215")
		require.Equal(t, commandBase.Add(time.Second).UnixMicro(), settleAt)

		// The facts themselves are still there, all three of them, which is
		// what makes the projection a derivation and not a replacement.
		var facts int
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_fact`).Scan(&facts))
		require.Equal(t, 3, facts)

		t.Logf("observed: %s folded three shuffled transitions into one row, %d facts kept",
			e.Name, facts)
	})
}

// TestAFactQueryHonoursTheClassRestriction is the provider's half of ADR-20260901-atx
// rule 13. The host sets Classes and never a caller, and a provider that
// answered a class the query excluded is a provider the reader then has to drop
// rows from: this is the case that says the reference adapter does not make it
// do that.
func TestAFactQueryHonoursTheClassRestriction(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			requestedFact(t, 1, "cmd-1"),
			settledFact(t, 2, "cmd-1"),
			authorizedFact(t, 3, "cmd-1"),
		}))

		whole, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs:    []event.Origin{commandOrigin(t)},
			Classes: []event.Class{event.ClassState, event.ClassCommand, event.ClassAudit},
		})
		require.NoError(t, err)
		require.Len(t, whole.Facts, 3)

		for _, fact := range whole.Facts {
			require.NotEmpty(t, fact.Class, "a stored fact came back with no class")
		}

		narrowed, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs:    []event.Origin{commandOrigin(t)},
			Classes: []event.Class{event.ClassState, event.ClassCommand},
		})
		require.NoError(t, err)
		require.Len(t, narrowed.Facts, 2, "the excluded class was answered anyway")

		for _, fact := range narrowed.Facts {
			require.NotEqual(t, event.ClassAudit, fact.Class)
			require.NotEqual(t, event.TypeDeviceCommandAuthorized, fact.Type)
		}

		t.Logf("observed: %s answered %d rows unrestricted and %d with audit excluded",
			e.Name, len(whole.Facts), len(narrowed.Facts))
	})
}
