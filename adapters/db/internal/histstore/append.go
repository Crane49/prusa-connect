package histstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The append statements. Each is written with '?' and rendered for PostgreSQL
// by Dialect.render. Each is a multi-row insert, so a batch is one round trip.
const (
	insertSeriesSQL = `INSERT INTO history_series (ref, metric) VALUES `
	selectSeriesSQL = `SELECT series_id, ref, metric FROM history_series WHERE (ref, metric) IN `
	insertSampleSQL = `INSERT INTO history_sample (series_id, at_us, value, text_value) VALUES `
	insertFactSQL   = `INSERT INTO history_fact (ref, epoch, sequence, at_us, type, class, payload) VALUES `
	insertGapSQL    = `INSERT INTO history_gap (ref, epoch, resumed_at, at_us) VALUES `
)

// The conflict clauses make an append idempotent. They are DO NOTHING, because
// PostgreSQL refuses a DO UPDATE when one statement has two rows with the same
// conflict key, and one batch can have two values for one instant.
const (
	seriesConflict = ` ON CONFLICT (ref, metric) DO NOTHING`
	sampleConflict = ` ON CONFLICT (series_id, at_us) DO NOTHING`
	factConflict   = ` ON CONFLICT (ref, epoch, sequence) DO NOTHING`
	gapConflict    = ` ON CONFLICT (ref, epoch, resumed_at) DO NOTHING`
)

// The column counts of the four inserts, which decide how many rows fit in one
// statement.
const (
	seriesColumns = 2
	sampleColumns = 4
	gapColumns    = 4
	factColumns   = 7
)

// maxStatementParams bounds one statement's placeholder count. 999 is the
// limit of SQLite before 3.32 (https://www.sqlite.org/limits.html), the lowest
// of any engine this runs on.
const maxStatementParams = 999

// emptyPayload is stored for a fact with no data. The column must be valid
// JSON on both engines, and an empty string is not.
const emptyPayload = `null`

// AppendSamples stores a batch of measurements in one transaction. Every
// sample is checked before the transaction opens, so a malformed one is
// reported by Sample.Check and not by a database constraint.
func (s *Store) AppendSamples(ctx context.Context, samples []historyapi.Sample) error {
	if len(samples) == 0 {
		return nil
	}

	for _, sample := range samples {
		if err := sample.Check(); err != nil {
			return fmt.Errorf("histstore: store %d samples: %w", len(samples), err)
		}
	}

	ids, err := s.seriesIDs(ctx, samples)
	if err != nil {
		return err
	}

	return s.inTransaction(ctx, strconv.Itoa(len(samples))+" samples", func(tx *sql.Tx) error {
		return s.insertChunks(ctx, tx, len(samples), sampleColumns, insertSampleSQL, sampleConflict,
			func(row int, args []any) []any {
				value, text := sampleColumnsOf(samples[row])

				return append(args, ids[row], samples[row].At.UnixMicro(), value, text)
			})
	})
}

// sampleColumnsOf returns the value and text columns of one sample. One of
// them is always NULL.
func sampleColumnsOf(sample historyapi.Sample) (value, text any) {
	if sample.Kind == historyapi.SampleText {
		return nil, sample.Text
	}

	return sample.Value, nil
}

// AppendFacts stores a batch of events in one transaction.
//
// A fact of type com.prusa.connect.stream.continuity.lost is a gap marker. It
// goes to history_gap, so a later query over that window reports it as
// incomplete.
func (s *Store) AppendFacts(ctx context.Context, facts []historyapi.Fact) error {
	if len(facts) == 0 {
		return nil
	}

	events, gaps := splitGaps(facts)

	return s.inTransaction(ctx, strconv.Itoa(len(facts))+" facts", func(tx *sql.Tx) error {
		err := s.insertChunks(ctx, tx, len(events), factColumns, insertFactSQL, factConflict,
			func(row int, args []any) []any {
				return append(args, events[row].Ref.String(), events[row].Epoch,
					events[row].Sequence, events[row].At.UnixMicro(), events[row].Type,
					string(events[row].Class), payloadOf(events[row]))
			})
		if err != nil {
			return err
		}

		if err = s.projectCommands(ctx, tx, events); err != nil {
			return err
		}

		if err = s.projectTransfers(ctx, tx, events); err != nil {
			return err
		}

		return s.insertChunks(ctx, tx, len(gaps), gapColumns, insertGapSQL, gapConflict,
			func(row int, args []any) []any {
				return append(args, gaps[row].Ref.String(), gaps[row].Epoch,
					gaps[row].Sequence, gaps[row].At.UnixMicro())
			})
	})
}

// splitGaps separates the gap markers from the events, in the caller's order.
func splitGaps(facts []historyapi.Fact) (events, gaps []historyapi.Fact) {
	for _, fact := range facts {
		if fact.Type == event.TypeStreamContinuityLost {
			gaps = append(gaps, fact)

			continue
		}

		events = append(events, fact)
	}

	return events, gaps
}

// payloadOf returns one fact's data as stored in the payload column.
func payloadOf(fact historyapi.Fact) string {
	if len(fact.Payload) == 0 {
		return emptyPayload
	}

	return string(fact.Payload)
}

// insertChunks runs one multi-row insert per chunk of rows that fits inside the
// parameter bound. bind appends one row's values to the argument slice.
func (s *Store) insertChunks(
	ctx context.Context, tx *sql.Tx, rows, columns int, statement, conflict string,
	bind func(row int, args []any) []any,
) error {
	if rows == 0 {
		return nil
	}

	perStatement := maxStatementParams / columns
	args := make([]any, 0, perStatement*columns)

	for start := 0; start < rows; start += perStatement {
		end := min(start+perStatement, rows)

		args = args[:0]
		for row := start; row < end; row++ {
			args = bind(row, args)
		}

		text := statement + valueTuples(end-start, columns) + conflict
		if _, err := tx.ExecContext(ctx, s.dialect.render(text), args...); err != nil {
			return err
		}
	}

	return nil
}

// valueTuples returns "(?,?,?),(?,?,?)" for rows of the given width.
func valueTuples(rows, columns int) string {
	tuple := "(" + strings.TrimSuffix(strings.Repeat("?,", columns), ",") + ")"

	var out strings.Builder

	out.Grow(rows * (len(tuple) + 1))

	for row := range rows {
		if row > 0 {
			out.WriteString(",")
		}

		out.WriteString(tuple)
	}

	return out.String()
}

// seriesKey is one (ref, metric) pair, the key of the series dictionary.
type seriesKey struct {
	ref    string
	metric string
}

// seriesIDs returns one interned id per sample, in the caller's order. It
// reads the cache first and interns the missing pairs together.
func (s *Store) seriesIDs(ctx context.Context, samples []historyapi.Sample) ([]int64, error) {
	ids := make([]int64, len(samples))
	keys := make([]seriesKey, len(samples))

	var missing []seriesKey

	for i, sample := range samples {
		keys[i] = seriesKey{ref: sample.Ref.String(), metric: sample.Metric}

		id, ok := s.cachedSeries(keys[i])
		if ok {
			ids[i] = id

			continue
		}

		missing = append(missing, keys[i])
	}

	if len(missing) > 0 {
		if err := s.internSeries(ctx, missing); err != nil {
			return nil, err
		}
	}

	for i, key := range keys {
		if ids[i] != 0 {
			continue
		}

		id, ok := s.cachedSeries(key)
		if !ok {
			return nil, fmt.Errorf("histstore: the series %s %s was interned and cannot be read back",
				key.ref, key.metric)
		}

		ids[i] = id
	}

	return ids, nil
}

// cachedSeries reads one interned id out of the cache.
func (s *Store) cachedSeries(key seriesKey) (int64, bool) {
	held, ok := s.series.Load(key)
	if !ok {
		return 0, false
	}

	id, valid := held.(int64)

	return id, valid
}

// internSeries writes the missing pairs and reads their ids back. It needs two
// statements per chunk: an insert that skips existing pairs cannot return
// their ids.
func (s *Store) internSeries(ctx context.Context, missing []seriesKey) error {
	perStatement := maxStatementParams / seriesColumns

	for start := 0; start < len(missing); start += perStatement {
		end := min(start+perStatement, len(missing))
		chunk := missing[start:end]

		args := make([]any, 0, len(chunk)*seriesColumns)
		for _, key := range chunk {
			args = append(args, key.ref, key.metric)
		}

		insert := insertSeriesSQL + valueTuples(len(chunk), seriesColumns) + seriesConflict
		if _, err := s.db.ExecContext(ctx, s.dialect.render(insert), args...); err != nil {
			return fmt.Errorf("histstore: intern %d series: %w", len(chunk), err)
		}

		if err := s.readSeries(ctx, chunk, args); err != nil {
			return err
		}
	}

	return nil
}

// readSeries reads the ids of one chunk of pairs back into the cache.
func (s *Store) readSeries(ctx context.Context, chunk []seriesKey, args []any) error {
	statement := selectSeriesSQL + "(" + valueTuples(len(chunk), seriesColumns) + ")"

	rows, err := s.db.QueryContext(ctx, s.dialect.render(statement), args...)
	if err != nil {
		return fmt.Errorf("histstore: read %d interned series: %w", len(chunk), err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id     int64
			ref    string
			metric string
		)

		if err = rows.Scan(&id, &ref, &metric); err != nil {
			return fmt.Errorf("histstore: read an interned series: %w", err)
		}

		if s.interned.Load() >= seriesCacheLimit {
			continue
		}

		if _, loaded := s.series.LoadOrStore(seriesKey{ref: ref, metric: metric}, id); !loaded {
			s.interned.Add(1)
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("histstore: read %d interned series: %w", len(chunk), err)
	}

	return nil
}

// inTransaction runs body in one transaction. what names the batch in errors.
func (s *Store) inTransaction(ctx context.Context, what string, body func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("histstore: begin to store %s: %w", what, err)
	}

	defer func() { _ = tx.Rollback() }()

	if err = body(tx); err != nil {
		return fmt.Errorf("histstore: store %s: %w", what, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("histstore: commit %s: %w", what, err)
	}

	return nil
}
