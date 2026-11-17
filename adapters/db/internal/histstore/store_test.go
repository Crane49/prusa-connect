package histstore_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sqlengine "github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/histstore"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// base is the instant every case below places its rows around. A fixed instant
// rather than time.Now, because the window a query asks for is the thing under
// test and a moving one hides an off-by-one at either end.
var base = time.Date(2026, time.August, 28, 14, 0, 0, 0, time.UTC)

// printer is one origin the cases store against.
func printer(t *testing.T, id string) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, id)
	require.NoError(t, err)

	return origin
}

// TestASampleSurvivesTheStore is the append and read back of the sample half.
func TestASampleSurvivesTheStore(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
			{At: base.Add(time.Second), Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 215.5},
			{At: base, Ref: mk4, Metric: "/temperature/bed", Value: 60},
		}))

		answered, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs:    []event.Origin{mk4},
			Metrics: []string{"/tools/0/nozzle_temp"},
			From:    base,
			To:      base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.False(t, answered.Incomplete)
		require.Len(t, answered.Samples, 2, "the metric filter did not hold")

		t.Logf("observed: %s answered %d samples for %s",
			e.Name, len(answered.Samples), mk4.String())

		require.Equal(t, mk4, answered.Samples[0].Ref)
		require.Equal(t, "/tools/0/nozzle_temp", answered.Samples[0].Metric)
		require.InDelta(t, 214.5, answered.Samples[0].Value, 0)
		require.True(t, answered.Samples[0].At.Equal(base))
		require.InDelta(t, 215.5, answered.Samples[1].Value, 0)
	})
}

// TestATextSampleSurvivesTheStore is BUG-008: the paths a printer reports as
// words rather than as numbers.
//
// /state is the series an operator asks the store about first, and until the
// sample row grew a text column beside its float there was nowhere for it to
// land: the feed counted every frame of it as untyped and dropped it. The
// numeric half is stored in the same batch here, because what has to hold is
// that one table answers both and keeps them apart.
func TestATextSampleSurvivesTheStore(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: base, Ref: mk4, Metric: "/state", Kind: historyapi.SampleText, Text: "IDLE"},
			{
				At: base.Add(time.Second), Ref: mk4, Metric: "/state",
				Kind: historyapi.SampleText, Text: "PRINTING",
			},
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
		}))

		answered, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs:    []event.Origin{mk4},
			Metrics: []string{"/state"},
			From:    base,
			To:      base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, answered.Samples, 2, "the metric filter did not hold")

		t.Logf("observed: %s answered %d readings of /state for %s, the first of them %q",
			e.Name, len(answered.Samples), mk4.String(), answered.Samples[0].Text)

		require.Equal(t, historyapi.SampleText, answered.Samples[0].Kind)
		require.Equal(t, "IDLE", answered.Samples[0].Text)
		require.InDelta(t, 0, answered.Samples[0].Value, 0, "a word came back carrying a number")
		require.Equal(t, "PRINTING", answered.Samples[1].Text)

		// The numeric half of the same batch is unchanged, which is what makes
		// the column an addition rather than a replacement.
		numbers, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs:    []event.Origin{mk4},
			Metrics: []string{"/tools/0/nozzle_temp"},
			From:    base,
			To:      base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, numbers.Samples, 1)
		require.Equal(t, historyapi.SampleNumber, numbers.Samples[0].Kind)
		require.InDelta(t, 214.5, numbers.Samples[0].Value, 0)
		require.Empty(t, numbers.Samples[0].Text, "a measurement came back carrying text")
	})
}

// TestASampleCarryingBothValuesIsRefused is the other half of the shape rule. A
// row holding a number and a word at once is two answers to what the series
// read, and a store that took it would decide silently which one it kept.
func TestASampleCarryingBothValuesIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		err := store.AppendSamples(t.Context(), []historyapi.Sample{
			{
				At: base, Ref: mk4, Metric: "/state",
				Kind: historyapi.SampleText, Text: "IDLE", Value: 1,
			},
		})
		require.ErrorIs(t, err, historyapi.ErrSampleShape)

		t.Logf("observed: %s refused a sample carrying both values with %v", e.Name, err)

		// And nothing of the refused batch is in the table: the shape is read
		// before the transaction opens.
		answered, queryErr := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, queryErr)
		require.Empty(t, answered.Samples)
	})
}

// seedOneSeries stores one well-formed sample so that the series dictionary
// holds a row, and answers the id the raw statements below hang their rows on.
//
// The raw path is the point of the cases that use it: AppendSamples reads
// Sample.Check before it opens a transaction, so nothing that goes through the
// store can ever reach the table's own CHECK, and a constraint no test writes
// against is a constraint that could be dropped from one engine's migration
// without a single case turning red.
func seedOneSeries(t *testing.T, db *sql.DB, store *histstore.Store, metric string) int64 {
	t.Helper()

	require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
		{At: base, Ref: printer(t, "mk4s-01"), Metric: metric, Value: 1},
	}))

	var id int64

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT series_id FROM history_series`).Scan(&id))

	return id
}

// insertRawSample writes one sample row with no help from the store: the two
// value columns are rendered as SQL literals, which is what lets a case write a
// pair the store's own path would have refused first.
func insertRawSample(t *testing.T, db *sql.DB, series int64, at time.Time, columns string) error {
	t.Helper()

	_, err := db.ExecContext(t.Context(),
		fmt.Sprintf(`INSERT INTO history_sample (series_id, at_us, value, text_value) VALUES (%d, %d, %s)`,
			series, at.UnixMicro(), columns))

	return err
}

// TestTheTableItselfRefusesARowCarryingBothValuesOrNeither is the CHECK on
// history_sample, on each engine, written against directly.
//
// AppendSamples refuses a malformed sample before the transaction opens, so the
// constraint is unreachable through the store and every case that goes through
// it proves Sample.Check and nothing about the schema. This one writes the row
// the append path cannot: whoever else ever holds a connection to this database
// is held to the same rule, which is the reason the CHECK exists rather than a
// comment saying the writer keeps it.
func TestTheTableItselfRefusesARowCarryingBothValuesOrNeither(t *testing.T) {
	t.Parallel()

	malformed := []struct {
		name    string
		columns string
	}{
		{name: "both", columns: `1.0, 'PRINTING'`},
		{name: "neither", columns: `NULL, NULL`},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		series := seedOneSeries(t, db, store, "/state")

		for row, bad := range malformed {
			// A distinct instant per case, so a refusal is the CHECK and never
			// the unique index the seeded row already occupies.
			at := base.Add(time.Duration(row+1) * time.Second)

			insertErr := insertRawSample(t, db, series, at, bad.columns)
			require.Errorf(t, insertErr,
				"%s stored a sample row carrying %s of a number and a word", e.Name, bad.name)

			t.Logf("observed: %s refused a row carrying %s value with %v", e.Name, bad.name, insertErr)
		}

		// And the seeded row is still the only one there: a refused insert
		// leaves the table as it found it.
		var rows int

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_sample`).Scan(&rows))
		require.Equal(t, 1, rows)
	})
}

// dropTheOneValueCheck takes the CHECK off history_sample so that a case can
// plant the row the read side refuses.
//
// It is per engine because dropping a constraint is: PostgreSQL alters the table
// in place and SQLite cannot, so the table is rebuilt there the way migration
// 0004 rebuilt it. Nothing but the two cases below uses this, and each of them
// opens a database of its own.
func dropTheOneValueCheck(t *testing.T, db *sql.DB, e engine) {
	t.Helper()

	var statements []string

	switch e.Name {
	case "sqlite":
		statements = []string{
			`CREATE TABLE history_sample_unchecked (
			   series_id  INTEGER NOT NULL REFERENCES history_series(series_id),
			   at_us      INTEGER NOT NULL,
			   value      REAL,
			   text_value TEXT
			 )`,
			`INSERT INTO history_sample_unchecked
			   SELECT series_id, at_us, value, text_value FROM history_sample`,
			`DROP TABLE history_sample`,
			`ALTER TABLE history_sample_unchecked RENAME TO history_sample`,
			`CREATE UNIQUE INDEX history_sample_series_at ON history_sample (series_id, at_us)`,
		}
	case "postgres":
		statements = []string{`ALTER TABLE history_sample DROP CONSTRAINT history_sample_one_value`}
	default:
		t.Fatalf("no way to unconstrain the sample table on the %s engine", e.Name)
	}

	for _, statement := range statements {
		_, err := db.ExecContext(t.Context(), statement)
		require.NoErrorf(t, err, "%s: %s", e.Name, statement)
	}
}

// TestAMalformedStoredRowIsRefusedOnTheWayOut is the read half of the same
// rule, and the only case that reaches it.
//
// A row that is both or neither is a row this store did not write, and the
// reader refuses it rather than picking one of the two: showing an operator
// that /state was 0 while it was PRINTING is worse than answering nothing. The
// row has to be planted behind the CHECK's back, because with the constraint in
// place the reader's refusal is unreachable, which is exactly the argument for
// keeping both, and for testing both.
func TestAMalformedStoredRowIsRefusedOnTheWayOut(t *testing.T) {
	t.Parallel()

	planted := []struct {
		name    string
		columns string
		says    string
	}{
		{
			name:    "a number and a word at once",
			columns: `1.0, 'PRINTING'`,
			says:    "carries a number and a word at once",
		},
		{
			name:    "neither of the two",
			columns: `NULL, NULL`,
			says:    "carries neither a number nor a word",
		},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		for _, bad := range planted {
			db := e.Open(t)

			store, err := histstore.New(db, e.Dialect)
			require.NoError(t, err)
			require.NoError(t, store.Migrate(t.Context()))

			mk4 := printer(t, "mk4s-01")
			series := seedOneSeries(t, db, store, "/state")

			dropTheOneValueCheck(t, db, e)
			require.NoError(t,
				insertRawSample(t, db, series, base.Add(time.Second), bad.columns))

			answered, queryErr := store.QuerySamples(t.Context(), historyapi.SampleQuery{
				Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
			})
			require.Error(t, queryErr, "%s read a row carrying %s as a measurement", e.Name, bad.name)
			require.Contains(t, queryErr.Error(), bad.says,
				"the refusal does not say which of the two malformations it read")
			require.Empty(t, answered.Samples, "a refused page carried rows anyway")

			t.Logf("observed: %s refused a stored row carrying %s with %v", e.Name, bad.name, queryErr)
		}
	})
}

// TestTheSweepExpiresAWordTheSameWayItExpiresANumber is retention over the new
// column. The sweep is blind to what a row holds by design, and this is what
// says so: a text row older than the window is deleted like a numeric one, and
// a text row inside it survives like a numeric one.
//
// The instants are relative to time.Now rather than to base, because the
// horizon is computed from the clock and a fixed instant would have every row
// expire the day this file is a year old.
func TestTheSweepExpiresAWordTheSameWayItExpiresANumber(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect, histstore.WithRawRetention(time.Hour))
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		mk4 := printer(t, "mk4s-01")
		now := historyapi.Truncate(time.Now())
		stale := now.Add(-2 * time.Hour)

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: stale, Ref: mk4, Metric: "/state", Kind: historyapi.SampleText, Text: "PRINTING"},
			{At: stale, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
			{At: now, Ref: mk4, Metric: "/state", Kind: historyapi.SampleText, Text: "IDLE"},
			{At: now, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 215.5},
		}))

		require.NoError(t, store.Prune(t.Context()))

		var words, numbers int

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_sample WHERE text_value IS NOT NULL`).Scan(&words))
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_sample WHERE value IS NOT NULL`).Scan(&numbers))

		t.Logf("observed: %s kept %d word rows and %d numeric rows of two each after the sweep",
			e.Name, words, numbers)

		require.Equal(t, 1, words, "the sweep treated a word differently from a number")
		require.Equal(t, 1, numbers)

		// The word that survived is the one inside the window, read back
		// through the query rather than counted in the table.
		answered, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs:    []event.Origin{mk4},
			Metrics: []string{"/state"},
			From:    now.Add(-time.Minute),
			To:      now.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, answered.Samples, 1)
		require.Equal(t, historyapi.SampleText, answered.Samples[0].Kind)
		require.Equal(t, "IDLE", answered.Samples[0].Text)
	})
}

// TestTheSeriesDictionaryHoldsOneRowPerPair is an earlier design decision: the reference is
// interned rather than repeated on every sample row, which measured 38.4 bytes
// per row against 92.1.
func TestTheSeriesDictionaryHoldsOneRowPerPair(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		mk4 := printer(t, "mk4s-01")

		samples := make([]historyapi.Sample, 0, 200)
		for i := range 200 {
			samples = append(samples, historyapi.Sample{
				At:     base.Add(time.Duration(i) * time.Second),
				Ref:    mk4,
				Metric: "/tools/0/nozzle_temp",
				Value:  float64(i),
			})
		}

		require.NoError(t, store.AppendSamples(t.Context(), samples))

		var series, rows int

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_series`).Scan(&series))
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT count(*) FROM history_sample`).Scan(&rows))

		t.Logf("observed: %s holds %d series rows behind %d sample rows", e.Name, series, rows)

		require.Equal(t, 1, series, "the pair was interned once per sample")
		require.Equal(t, 200, rows)
	})
}

// TestOneBatchMayHoldTwoValuesForOneInstant is the shape a multi-row insert has
// to survive. PostgreSQL refuses a DO UPDATE whose statement holds two rows with
// one conflict key, so the conflict clause is DO NOTHING and a batch that
// carries a duplicate is stored rather than refused whole.
func TestOneBatchMayHoldTwoValuesForOneInstant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 215.5},
			{At: base.Add(time.Second), Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 216.5},
		}))

		answered, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, answered.Samples, 2, "the duplicated instant was stored twice")

		t.Logf("observed: %s stored %d rows from a batch of 3 holding one duplicated instant",
			e.Name, len(answered.Samples))

		// The same batch again changes nothing, which is what makes a replay of
		// a window safe on this half as well as on the fact half.
		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
		}))

		again, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, again.Samples, 2)
	})
}

// TestAFactIsStoredOnceHoweverOftenItArrives is the idempotence the envelope's
// own dedup key gives, and it is what makes a replay safe.
func TestAFactIsStoredOnceHoweverOftenItArrives(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		fact := historyapi.Fact{
			At:       base,
			Ref:      mk4,
			Type:     event.TypeDeviceRecordChanged,
			Sequence: 41,
			Epoch:    "epoch-a",
			Payload:  json.RawMessage(`{"moved":["model"]}`),
		}

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{fact}))
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{fact}))

		answered, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, answered.Facts, 1, "a replayed event was stored twice")

		t.Logf("observed: %s stored %s once at sequence %d",
			e.Name, answered.Facts[0].Type, answered.Facts[0].Sequence)

		require.Equal(t, event.TypeDeviceRecordChanged, answered.Facts[0].Type)
		require.Equal(t, uint64(41), answered.Facts[0].Sequence)
		require.Equal(t, "epoch-a", answered.Facts[0].Epoch)
		require.JSONEq(t, `{"moved":["model"]}`, string(answered.Facts[0].Payload))

		// The same sequence under a new epoch is a different event, because the
		// origin's numbering generation changed.
		fact.Epoch = "epoch-b"
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{fact}))

		answered, err = store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, answered.Facts, 2)
	})
}

// TestALostWindowIsReportedIncomplete is an earlier design decision. A store that wrote
// nothing down when continuity was lost would answer the same query with a
// quiet hole, and a caller cannot tell that from a printer that reported
// nothing.
func TestALostWindowIsReportedIncomplete(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")
		mini := printer(t, "mini-02")

		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{{
			At: base.Add(30 * time.Second), Ref: mk4,
			Type: event.TypeStreamContinuityLost, Sequence: 97, Epoch: "epoch-a",
		}}))

		lost, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.True(t, lost.Incomplete, "the window holding the loss was reported whole")
		require.Empty(t, lost.Facts, "the gap marker was stored as an ordinary fact")

		t.Logf("observed: %s reported the window of %s incomplete and the window of %s whole",
			e.Name, mk4.String(), mini.String())

		// Another printer's window is untouched, and so is a window before the
		// loss: an incomplete answer that was always true would say nothing.
		whole, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs: []event.Origin{mini}, From: base, To: base.Add(time.Minute),
		})
		require.NoError(t, err)
		require.False(t, whole.Incomplete)

		earlier, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{mk4}, From: base.Add(-time.Minute), To: base,
		})
		require.NoError(t, err)
		require.False(t, earlier.Incomplete)
	})
}

// TestAnEmptyPermittedSetAnswersAnEmptyPage is the rule earlier research
// states and AGENTS.md names as one that breaks quietly: a list endpoint that
// fetches and then filters leaks through pagination and counts.
func TestAnEmptyPermittedSetAnswersAnEmptyPage(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		require.NoError(t, store.AppendSamples(t.Context(), []historyapi.Sample{
			{At: base, Ref: mk4, Metric: "/tools/0/nozzle_temp", Value: 214.5},
		}))
		require.NoError(t, store.AppendFacts(t.Context(), []historyapi.Fact{{
			At: base, Ref: mk4, Type: event.TypeDeviceRecordChanged, Sequence: 1, Epoch: "epoch-a",
		}}))

		samples, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{From: base, To: base.Add(time.Hour)})
		require.NoError(t, err)
		require.Empty(t, samples.Samples, "a caller who may read nothing was answered rows")

		facts, err := store.QueryFacts(t.Context(), historyapi.FactQuery{From: base, To: base.Add(time.Hour)})
		require.NoError(t, err)
		require.Empty(t, facts.Facts, "a caller who may read nothing was answered rows")

		// And an origin the caller may read does not carry another one's rows.
		other, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{printer(t, "mini-02")}, From: base, To: base.Add(time.Hour),
		})
		require.NoError(t, err)
		require.Empty(t, other.Samples)

		t.Logf("observed: %s answered no row to a caller holding no origin", e.Name)
	})
}

// TestThePageWalksTheWholeWindow holds the cursor. A page that repeated a row
// or skipped one would be a history that reads differently depending on the
// page size it was read at.
func TestThePageWalksTheWholeWindow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mk4 := printer(t, "mk4s-01")

		const total = 25

		samples := make([]historyapi.Sample, 0, total)
		for i := range total {
			// Two metrics report at every instant, so a page of three has to
			// resume inside one millisecond rather than between two.
			metric := "/tools/0/nozzle_temp"
			if i%2 == 1 {
				metric = "/temperature/bed"
			}

			samples = append(samples, historyapi.Sample{
				At:     base.Add(time.Duration(i/2) * time.Second),
				Ref:    mk4,
				Metric: metric,
				Value:  float64(i),
			})
		}

		require.NoError(t, store.AppendSamples(t.Context(), samples))

		var (
			seen   int
			cursor string
			pages  int
		)

		for {
			page, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
				Refs: []event.Origin{mk4}, From: base, To: base.Add(time.Hour),
				Limit: 3, Cursor: cursor,
			})
			require.NoError(t, err)

			seen += len(page.Samples)
			pages++
			cursor = page.Cursor

			if cursor == "" {
				break
			}

			require.Less(t, pages, total, "the cursor is not advancing")
		}

		t.Logf("observed: %s walked %d samples in %d pages of 3", e.Name, seen, pages)

		require.Equal(t, total, seen, "the walk did not cover the window")
	})
}

// TestAnAggregateQueryIsRefusedRatherThanAnswered holds the capability rule: the
// bits are the authority, and a provider without CapAggregate that answered raw
// rows to a bucketed query would be giving a wrong answer dressed as a right
// one.
func TestAnAggregateQueryIsRefusedRatherThanAnswered(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.True(t, store.Caps().Has(historyapi.CapSamples|historyapi.CapFacts))
		require.False(t, store.Caps().Has(historyapi.CapAggregate))

		_, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
			Refs: []event.Origin{printer(t, "mk4s-01")},
			From: base, To: base.Add(time.Hour),
			Step: 5 * time.Minute, Agg: historyapi.AggMean,
		})
		require.ErrorIs(t, err, historyapi.ErrUnsupported)

		t.Logf("observed: %s refused a five minute mean with %v", e.Name, err)
	})
}

// TestAnEditedMigrationIsRefused holds the checksum gate on this store's own
// ledger. Without it an edited migration is skipped in silence and two
// installations report the same version over different schemas.
func TestAnEditedMigrationIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		// Running it again on the same database does nothing and refuses
		// nothing, which is the half a checksum gate has to keep working.
		require.NoError(t, store.Migrate(t.Context()))

		_, err = db.ExecContext(t.Context(),
			`UPDATE history_migration SET checksum = 'not the bytes that ran'`)
		require.NoError(t, err)

		err = store.Migrate(t.Context())
		require.ErrorIs(t, err, histstore.ErrMigrationChanged)

		t.Logf("observed: %s refused a changed migration with %v", e.Name, err)
	})
}

func TestMigrationRehearsalLeavesTheDatabaseUnchanged(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.RehearseMigrations(t.Context()))

		var ledger int
		require.NoError(t, db.QueryRowContext(t.Context(), e.tableCount("history_migration")).Scan(&ledger))
		require.Zero(t, ledger, "the rehearsal kept its migration ledger")

		require.NoError(t, store.Migrate(t.Context()))

		var applied int
		require.NoError(t, db.QueryRowContext(t.Context(),
			"SELECT count(*) FROM history_migration").Scan(&applied))
		require.Positive(t, applied)
	})
}

func TestTwoSQLiteHandlesSerializeOneMigration(t *testing.T) {
	t.Parallel()

	path := filepath.ToSlash(filepath.Join(t.TempDir(), "history.db"))

	firstDB, err := sqlengine.OpenSQLite(t.Context(), path, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, firstDB.Close()) })

	secondDB, err := sqlengine.OpenSQLite(t.Context(), path, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, secondDB.Close()) })

	first, err := histstore.New(firstDB, histstore.DialectSQLite)
	require.NoError(t, err)
	second, err := histstore.New(secondDB, histstore.DialectSQLite)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)

	for _, store := range []*histstore.Store{first, second} {
		go func() {
			<-start
			results <- store.Migrate(t.Context())
		}()
	}

	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)

	var applied int
	require.NoError(t, firstDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM history_migration").Scan(&applied))
	require.Equal(t, 1, applied)
}

// TestTheTwoEnginesCarryOneSchema is the equivalence gate for this store. The
// two migration files are hand-written per engine under DB1, so nothing but a
// test holds them to the same tables and the same columns.
func TestTheTwoEnginesCarryOneSchema(t *testing.T) {
	t.Parallel()

	tables := map[string][]string{
		"history_series": {"series_id", "ref", "metric"},
		"history_sample": {"series_id", "at_us", "value", "text_value"},
		"history_fact":   {"ref", "epoch", "sequence", "at_us", "type", "class", "payload"},
		"history_gap":    {"ref", "epoch", "resumed_at", "at_us"},
		"history_command": {
			"command_id", "ref", "verb", "key", "requested_us", "args", "args_digest",
			"settled_us", "status", "ok", "outcome_unknown", "machine_reason",
			"fault_code", "elapsed_ms", "authorized_us", "auth_type", "auth_id",
		},
	}

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := histstore.New(db, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		for table, columns := range tables {
			requireColumns(t, db, table, columns, e.Name)
		}

		t.Logf("observed: %s carries all %d history tables with the same columns", e.Name, len(tables))
	})
}

// requireColumns asks one engine for a table's columns and reports the table
// that answered differently. The names are this file's own literals and no
// caller value reaches the statement.
func requireColumns(t *testing.T, db *sql.DB, table string, columns []string, engineName string) {
	t.Helper()

	rows, err := db.QueryContext(t.Context(),
		`SELECT `+strings.Join(columns, ", ")+` FROM `+table+` WHERE 1 = 0`)
	require.NoErrorf(t, err, "%s on %s", table, engineName)

	defer func() { require.NoError(t, rows.Close()) }()

	require.NoError(t, rows.Err())
}

// TestAStoreOpensItsOwnFile is an earlier design decision from the file's side: the history
// database is not the graph's, so nothing here can take the graph's write lock.
func TestAStoreOpensItsOwnFile(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/nested/history.db"

	db, err := sqlengine.OpenSQLite(t.Context(), path, 0)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.Close()) }()

	var mode string
	require.NoError(t, db.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&mode))
	require.Equal(t, "wal", mode, "a store outside write-ahead logging blocks every read behind a write")

	var attached int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM pragma_database_list WHERE name <> 'main'`).Scan(&attached))
	require.Zero(t, attached, "the history store attached another database")

	t.Logf("observed: the store opened %s in %s with %d attached databases", path, mode, attached)
}

// TestAPathWithAQueryStringIsRefused holds the one way a path could silently
// become a shorter path carrying settings nobody asked for.
func TestAPathWithAQueryStringIsRefused(t *testing.T) {
	t.Parallel()

	_, err := sqlengine.OpenSQLite(t.Context(), t.TempDir()+"/history.db?_pragma=synchronous(OFF)", 0)
	require.ErrorIs(t, err, sqlengine.ErrDatabasePath)

	_, err = sqlengine.OpenSQLite(t.Context(), "", 0)
	require.ErrorIs(t, err, sqlengine.ErrDatabasePath)
}

// TestTheStoreRefusesAnEngineItCannotRender is the constructor's own guard.
func TestTheStoreRefusesAnEngineItCannotRender(t *testing.T) {
	t.Parallel()

	_, err := histstore.New(nil, histstore.DialectSQLite)
	require.Error(t, err)

	_, err = histstore.New(new(sql.DB), histstore.Dialect(9))
	require.Error(t, err)
}

// TestThePermittedSetIsAppliedBeforeTheLimit is the rule AGENTS.md names as one
// that breaks quietly here, held over a range query: a listing that fetched and
// then filtered leaks through its own paging even where every returned row is
// one the caller may read.
//
// Two printers report at the same instants and only one of them is permitted.
// A page of five has to be five rows of the permitted machine, and the walk has
// to reach every one of its rows. Under fetch-then-filter the first page is
// three rows and the walk stops early, because the limit was spent on rows that
// were thrown away afterwards.
func TestThePermittedSetIsAppliedBeforeTheLimit(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		mine, theirs := printer(t, "mk4s-01"), printer(t, "mini-02")

		const each = 10

		samples := make([]historyapi.Sample, 0, each*2)
		facts := make([]historyapi.Fact, 0, each*2)

		for i := range each {
			at := base.Add(time.Duration(i) * time.Second)

			for _, ref := range []event.Origin{mine, theirs} {
				samples = append(samples, historyapi.Sample{
					At: at, Ref: ref, Metric: "/tools/0/nozzle_temp", Value: float64(i),
				})
				facts = append(facts, historyapi.Fact{
					At: at, Ref: ref, Type: event.TypeDeviceRecordChanged,
					Sequence: uint64(i + 1), Epoch: "epoch-a", Payload: json.RawMessage(`{}`),
				})
			}
		}

		require.NoError(t, store.AppendSamples(t.Context(), samples))
		require.NoError(t, store.AppendFacts(t.Context(), facts))

		var (
			seen   int
			pages  int
			cursor string
		)

		for {
			page, err := store.QuerySamples(t.Context(), historyapi.SampleQuery{
				Refs: []event.Origin{mine}, From: base, To: base.Add(time.Hour), Limit: 5, Cursor: cursor,
			})
			require.NoError(t, err)

			for _, sample := range page.Samples {
				require.Equal(t, mine, sample.Ref, "the page carried a machine the caller may not read")
			}

			if page.Cursor != "" {
				require.Len(t, page.Samples, 5,
					"a full page held fewer rows than the limit, so rows were dropped after the limit was applied")
			}

			seen += len(page.Samples)
			pages++
			cursor = page.Cursor

			if cursor == "" {
				break
			}

			require.Less(t, pages, each, "the cursor is not advancing")
		}

		require.Equal(t, each, seen, "the walk did not reach every row the caller may read")

		one, err := store.QueryFacts(t.Context(), historyapi.FactQuery{
			Refs: []event.Origin{mine}, From: base, To: base.Add(time.Hour), Limit: 5,
		})
		require.NoError(t, err)
		require.Len(t, one.Facts, 5)

		for _, stored := range one.Facts {
			require.Equal(t, mine, stored.Ref, "the page carried a machine the caller may not read")
		}

		t.Logf("observed: %s walked %d permitted samples in %d pages of 5, beside %d it withheld",
			e.Name, seen, pages, each)
	})
}

// TestADatabaseAnotherBuildMadeIsRefused is the two refusals a database this
// build did not migrate gets: a ledger row naming a file this build does not
// ship, and a table of this store that exists with no ledger row creating it.
// Neither is written to.
func TestADatabaseAnotherBuildMadeIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		migrated := e.Open(t)

		store, err := histstore.New(migrated, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		_, err = migrated.ExecContext(t.Context(),
			"UPDATE history_migration SET name = '0001_renamed_after_shipping.sql'")
		require.NoError(t, err)
		require.ErrorIs(t, store.Migrate(t.Context()), histstore.ErrMigrationMissing)

		foreign := e.Open(t)

		_, err = foreign.ExecContext(t.Context(), "CREATE TABLE history_fact (ref TEXT)")
		require.NoError(t, err)

		store, err = histstore.New(foreign, e.Dialect)
		require.NoError(t, err)

		err = store.Migrate(t.Context())
		require.ErrorIs(t, err, histstore.ErrUnrecordedTable)
		require.ErrorContains(t, err, "history_fact")
	})
}
