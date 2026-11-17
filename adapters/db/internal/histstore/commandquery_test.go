package histstore_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The read side of the fold, on both engines.
//
// Every case runs under forEachEngine, so a query answered on SQLite and
// refused on PostgreSQL is a failure here rather than a surprise in a
// deployment: the two adapters are one store on two engines and the statements
// are rendered from one text.

// TestTheFoldedCommandIsReadableByItsID is the read surface the projection
// never had. The row was written from the first day and queried by nothing
// outside the store's own tests, so a command id a caller was handed at 200 was
// not a thing that could be asked about.
func TestTheFoldedCommandIsReadableByItsID(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			requestedFact(t, 1, "cmd-read"),
			authorizedFact(t, 2, "cmd-read"),
			settledFact(t, 3, "cmd-read"),
		}))

		answer, err := store.QueryCommands(t.Context(),
			historyapi.CommandQuery{IDs: []string{"cmd-read"}})
		require.NoError(t, err)
		require.Len(t, answer.Commands, 1)

		one := answer.Commands[0]

		require.Equal(t, "cmd-read", one.ID)
		require.Equal(t, commandOrigin(t), one.Ref)
		require.Equal(t, "core.command.set_nozzle_temperature", one.Verb)
		require.Equal(t, "k-1", one.Key)
		require.Equal(t, "succeeded", one.Status)
		require.True(t, one.OK)
		require.Equal(t, int64(42), one.ElapsedMS)
		require.False(t, one.Open(), "a command with both halves is not open")

		// The audit transition is folded now, so the row carries who authorized
		// it. Withholding it from the caller who may not see it is the reader's
		// job and not the store's: a store that did not keep it would withhold
		// it from the caller who holds view_audit as well.
		require.Equal(t, "user", one.AuthType)
		require.Equal(t, "operator", one.AuthID)
		require.Equal(t, commandBase, one.Authorized)
	})
}

// TestAnUnsettledCommandIsFoundByTheOpenQuery is what a restart asks. The row a
// process left behind is the only trace of a command that was on the wire when
// it died, and finding it takes a query keyed on the absence of the settled
// transition rather than a scan of every fact this store ever caught.
func TestAnUnsettledCommandIsFoundByTheOpenQuery(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			requestedFact(t, 1, "cmd-open"),
			authorizedFact(t, 2, "cmd-open"),
			requestedFact(t, 3, "cmd-closed"),
			settledFact(t, 4, "cmd-closed"),
		}))

		answer, err := store.QueryCommands(t.Context(), historyapi.CommandQuery{OpenOnly: true})
		require.NoError(t, err)
		require.Len(t, answer.Commands, 1,
			"the open query answered %d rows", len(answer.Commands))
		require.Equal(t, "cmd-open", answer.Commands[0].ID)
		require.True(t, answer.Commands[0].Open())

		// And the unfiltered query still answers both, newest requested first.
		every, err := store.QueryCommands(t.Context(), historyapi.CommandQuery{})
		require.NoError(t, err)
		require.Len(t, every.Commands, 2)
	})
}

// TestACommandQueryAnswersOnlyTheOriginsItWasGiven is the narrowing every query
// on this contract makes. Refs reaches the statement, so a page counts rows the
// caller may read rather than filtering a page that was already fetched.
func TestACommandQueryAnswersOnlyTheOriginsItWasGiven(t *testing.T) {
	t.Parallel()

	other, made := event.NewOrigin(event.KindDevice, "p-elsewhere")
	require.NoError(t, made)

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		elsewhere := requestedFact(t, 1, "cmd-elsewhere")
		elsewhere.Ref = other

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{
			requestedFact(t, 2, "cmd-here"), elsewhere,
		}))

		answer, err := store.QueryCommands(t.Context(),
			historyapi.CommandQuery{Refs: []event.Origin{commandOrigin(t)}})
		require.NoError(t, err)
		require.Len(t, answer.Commands, 1)
		require.Equal(t, "cmd-here", answer.Commands[0].ID)
	})
}

// TestAStoredInstantKeepsItsMicroseconds is the contract's declared resolution
// read back off the disk.
//
// historyapi.Precision is a microsecond, and it is stated because it was not:
// both shipped adapters truncated to a millisecond, the contract named no
// resolution, and a third-party adapter keeping nanoseconds was equally
// conformant and answered the same query differently.
func TestAStoredInstantKeepsItsMicroseconds(t *testing.T) {
	t.Parallel()

	// 123456 microseconds and 789 nanoseconds: the microseconds survive and the
	// nanoseconds are truncated towards the epoch, which is what the contract
	// says and never rounded, because rounding would give two adjacent samples
	// one key.
	at := time.Date(2026, time.September, 2, 9, 30, 0, 123456789, time.UTC)

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: at, Ref: commandOrigin(t), Metric: "/tools/0/nozzle_temp", Value: 215},
		}))

		fact := requestedFact(t, 1, "cmd-precise")
		fact.At = at
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{fact}))

		samples, err := store.QuerySamples(t.Context(),
			historyapi.SampleQuery{Refs: []event.Origin{commandOrigin(t)}})
		require.NoError(t, err)
		require.Len(t, samples.Samples, 1)
		require.Equal(t, historyapi.Truncate(at), samples.Samples[0].At,
			"the sample's instant did not survive at the contract's resolution")

		facts, err := store.QueryFacts(t.Context(),
			historyapi.FactQuery{Refs: []event.Origin{commandOrigin(t)}})
		require.NoError(t, err)
		require.Len(t, facts.Facts, 1)
		require.Equal(t, historyapi.Truncate(at), facts.Facts[0].At,
			"the fact's instant did not survive at the contract's resolution")

		// The reading that proves it is a microsecond and not a millisecond: a
		// store that truncated would answer the same instant for a sample a
		// tenth of a millisecond later.
		require.Equal(t, 123456, facts.Facts[0].At.Nanosecond()/1000)
	})
}

// TestTwoReadingsInOneMillisecondAreTwoRows is the same property from the side
// that costs data. A printer reporting at 20 Hz is inside a millisecond of
// itself, and a store keyed on milliseconds would silently keep one of every
// pair: the unique index on (series_id, at) is the thing that would drop it.
func TestTwoReadingsInOneMillisecondAreTwoRows(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, time.September, 2, 9, 30, 0, 1_000_000, time.UTC)
	second := first.Add(200 * time.Microsecond)

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: first, Ref: commandOrigin(t), Metric: "/tools/0/nozzle_temp", Value: 215},
			{At: second, Ref: commandOrigin(t), Metric: "/tools/0/nozzle_temp", Value: 216},
		}))

		answer, err := store.QuerySamples(t.Context(),
			historyapi.SampleQuery{Refs: []event.Origin{commandOrigin(t)}})
		require.NoError(t, err)
		require.Len(t, answer.Samples, 2,
			"two readings 200 microseconds apart were stored as %d rows", len(answer.Samples))
		require.Equal(t, 215.0, answer.Samples[0].Value)
		require.Equal(t, 216.0, answer.Samples[1].Value)
	})
}

// TestARefusedCommandIsAFoldedRowToo is item 3 of this session read off the
// store: a command minted before authorization and refused publishes a
// requested transition and a settled one carrying the refusal, so it folds into
// a row like any other invocation.
func TestARefusedCommandIsAFoldedRowToo(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		refused := settledFact(t, 2, "cmd-refused")
		refused.Payload = json.RawMessage(`{"command_id":"cmd-refused",` +
			`"verb":"core.command.force_gcode","status":"` + event.CommandUnauthorized + `",` +
			`"fault_code":"core.auth/ACCESS_DENIED","elapsed_ms":0}`)

		request := requestedFact(t, 1, "cmd-refused")
		require.NoError(t, store.AppendFacts(t.Context(),
			[]historyapi.Fact{request, refused}))

		answer, err := store.QueryCommands(t.Context(),
			historyapi.CommandQuery{IDs: []string{"cmd-refused"}})
		require.NoError(t, err)
		require.Len(t, answer.Commands, 1)
		require.Equal(t, event.CommandUnauthorized, answer.Commands[0].Status)
		require.Equal(t, "core.auth/ACCESS_DENIED", answer.Commands[0].FaultCode)
		require.False(t, answer.Commands[0].Open(),
			"a refused command is settled, not open")
	})
}
