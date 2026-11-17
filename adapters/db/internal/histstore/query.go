package histstore

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// ErrQuery reports a query this store will not answer. It wraps
// historyapi.ErrQueryRefused, so the host answers 422 and not 500.
var ErrQuery = fmt.Errorf("%w: histstore: the query cannot be answered", historyapi.ErrQueryRefused)

// Paging bounds. A query with no limit gets defaultLimit rows. A limit above
// maxLimit is cut to maxLimit.
const (
	defaultLimit = 1000
	maxLimit     = 10000
)

// QuerySamples answers a range over the series the caller may read.
//
// Refs is the permitted set. It is applied inside the statement, before the
// limit, so pagination and counts cannot leak other rows. An empty set gives
// an empty page, never an unfiltered query. A query with Step or Agg is
// refused, because this provider has no CapAggregate.
func (s *Store) QuerySamples(
	ctx context.Context, query historyapi.SampleQuery,
) (historyapi.SampleResult, error) {
	if query.Step != 0 || query.Agg != historyapi.AggNone {
		return historyapi.SampleResult{}, fmt.Errorf("%w: %w: this provider stores raw samples",
			ErrQuery, historyapi.ErrUnsupported)
	}

	if len(query.Refs) == 0 {
		return historyapi.SampleResult{}, nil
	}

	from, clamped, err := s.refuseExpiredWindow(query.From)
	if err != nil {
		return historyapi.SampleResult{}, err
	}

	query.From = from

	statement, args, err := sampleStatement(query)
	if err != nil {
		return historyapi.SampleResult{}, err
	}

	samples, cursor, err := s.readSamples(ctx, statement, args, limitOf(query.Limit))
	if err != nil {
		return historyapi.SampleResult{}, err
	}

	incomplete, err := s.windowIsIncomplete(ctx, query.Refs, query.From, query.To)
	if err != nil {
		return historyapi.SampleResult{}, err
	}

	return historyapi.SampleResult{
		Samples: samples, Cursor: cursor, Incomplete: incomplete || clamped,
	}, nil
}

// QueryFacts answers a window of events the caller may read. Refs is the
// permitted set, with the same rules as in QuerySamples.
func (s *Store) QueryFacts(ctx context.Context, query historyapi.FactQuery) (historyapi.FactResult, error) {
	if len(query.Refs) == 0 {
		return historyapi.FactResult{}, nil
	}

	from, clamped, err := s.refuseExpiredWindow(query.From)
	if err != nil {
		return historyapi.FactResult{}, err
	}

	query.From = from

	statement, args, err := factStatement(query)
	if err != nil {
		return historyapi.FactResult{}, err
	}

	facts, cursor, err := s.readFacts(ctx, statement, args, limitOf(query.Limit))
	if err != nil {
		return historyapi.FactResult{}, err
	}

	incomplete, err := s.windowIsIncomplete(ctx, query.Refs, query.From, query.To)
	if err != nil {
		return historyapi.FactResult{}, err
	}

	return historyapi.FactResult{
		Facts: facts, Cursor: cursor, Incomplete: incomplete || clamped,
	}, nil
}

// readSamples runs one page of the sample query.
func (s *Store) readSamples(
	ctx context.Context, statement string, args []any, limit int,
) ([]historyapi.Sample, string, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.render(statement), args...)
	if err != nil {
		return nil, "", fmt.Errorf("histstore: read samples: %w", err)
	}

	defer func() { _ = rows.Close() }()

	samples := make([]historyapi.Sample, 0, limit)

	var (
		lastAt     int64
		lastSeries int64
	)

	for rows.Next() {
		var (
			reference string
			metric    string
			atUS      int64
			seriesID  int64
			value     sql.NullFloat64
			text      sql.NullString
		)

		if err = rows.Scan(&reference, &metric, &atUS, &seriesID, &value, &text); err != nil {
			return nil, "", fmt.Errorf("histstore: read a sample row: %w", err)
		}

		origin, parseErr := event.ParseOrigin(reference)
		if parseErr != nil {
			return nil, "", fmt.Errorf("histstore: a stored sample names %q, which is no origin: %w",
				reference, parseErr)
		}

		sample := historyapi.Sample{At: time.UnixMicro(atUS).UTC(), Ref: origin, Metric: metric}

		// A row is a number or a text. A row with both or neither was not
		// written by this store, and the read fails.
		switch {
		case value.Valid && !text.Valid:
			sample.Value = value.Float64
		case text.Valid && !value.Valid:
			sample.Kind, sample.Text = historyapi.SampleText, text.String
		default:
			held := "carries neither a number nor a word"
			if value.Valid {
				held = "carries a number and a word at once"
			}

			return nil, "", fmt.Errorf("histstore: the stored sample of %s %s at %d %s",
				reference, metric, atUS, held)
		}

		samples = append(samples, sample)

		lastAt, lastSeries = atUS, seriesID
	}

	if err = rows.Err(); err != nil {
		return nil, "", fmt.Errorf("histstore: read samples: %w", err)
	}

	return samples, cursorAfter(len(samples), limit, lastAt, strconv.FormatInt(lastSeries, 10)), nil
}

// readFacts runs one page of the fact query.
func (s *Store) readFacts(
	ctx context.Context, statement string, args []any, limit int,
) ([]historyapi.Fact, string, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.render(statement), args...)
	if err != nil {
		return nil, "", fmt.Errorf("histstore: read facts: %w", err)
	}

	defer func() { _ = rows.Close() }()

	facts := make([]historyapi.Fact, 0, limit)

	var (
		lastAt  int64
		lastKey string
	)

	for rows.Next() {
		var (
			reference string
			epoch     string
			sequence  int64
			atUS      int64
			factType  string
			class     string
			payload   string
		)

		if err = rows.Scan(
			&reference, &epoch, &sequence, &atUS, &factType, &class, &payload,
		); err != nil {
			return nil, "", fmt.Errorf("histstore: read a fact row: %w", err)
		}

		origin, parseErr := event.ParseOrigin(reference)
		if parseErr != nil {
			return nil, "", fmt.Errorf("histstore: a stored fact names %q, which is no origin: %w",
				reference, parseErr)
		}

		if sequence < 0 {
			return nil, "", fmt.Errorf("histstore: a stored fact of %s carries sequence %d, "+
				"which is no place in an origin's numbering", reference, sequence)
		}

		facts = append(facts, historyapi.Fact{
			At: time.UnixMicro(atUS).UTC(), Ref: origin, Type: factType,
			Sequence: uint64(sequence), Epoch: epoch, Payload: []byte(payload),
			Class: event.Class(class),
		})

		lastAt, lastKey = atUS, reference+"\x00"+strconv.FormatInt(sequence, 10)
	}

	if err = rows.Err(); err != nil {
		return nil, "", fmt.Errorf("histstore: read facts: %w", err)
	}

	return facts, cursorAfter(len(facts), limit, lastAt, lastKey), nil
}

// windowIsIncomplete reports whether continuity was lost for any of these
// origins inside the window.
func (s *Store) windowIsIncomplete(
	ctx context.Context, refs []event.Origin, from, to time.Time,
) (bool, error) {
	if len(refs) == 0 {
		return false, nil
	}

	statement := `SELECT 1 FROM history_gap WHERE ref IN (` + placeholders(len(refs)) +
		`) AND at_us >= ? AND at_us < ? LIMIT 1`

	args := make([]any, 0, len(refs)+windowBounds)
	for _, ref := range refs {
		args = append(args, ref.String())
	}

	args = append(args, boundOf(from, minMicro), boundOf(to, maxMicro))

	var marker int

	err := s.db.QueryRowContext(ctx, s.dialect.render(statement), args...).Scan(&marker)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("histstore: read the gap rows: %w", err)
	}
}

// sampleStatement builds the sample query and its arguments.
func sampleStatement(query historyapi.SampleQuery) (string, []any, error) {
	var (
		text strings.Builder
		args []any
	)

	text.WriteString(`SELECT se.ref, se.metric, s.at_us, s.series_id, s.value, s.text_value
		FROM history_sample AS s
		JOIN history_series AS se ON se.series_id = s.series_id
		WHERE se.ref IN (` + placeholders(len(query.Refs)) + `)`)

	for _, ref := range query.Refs {
		args = append(args, ref.String())
	}

	if len(query.Metrics) > 0 {
		text.WriteString(` AND se.metric IN (` + placeholders(len(query.Metrics)) + `)`)

		for _, metric := range query.Metrics {
			args = append(args, metric)
		}
	}

	text.WriteString(` AND s.at_us >= ? AND s.at_us < ?`)
	args = append(args, boundOf(query.From, minMicro), boundOf(query.To, maxMicro))

	after, key, err := readCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}

	if key != "" {
		// The series id breaks a tie on the instant. It must be compared as
		// a number: as text, 10 sorts before 9.
		series, parseErr := strconv.ParseInt(key, 10, 64)
		if parseErr != nil {
			return "", nil, fmt.Errorf("%w: the cursor is not one this provider minted", ErrQuery)
		}

		text.WriteString(` AND (s.at_us > ? OR (s.at_us = ? AND s.series_id > ?))`)
		args = append(args, after, after, series)
	}

	text.WriteString(` ORDER BY s.at_us, s.series_id LIMIT ?`)
	args = append(args, limitOf(query.Limit))

	return text.String(), args, nil
}

// factStatement builds the fact query and its arguments.
func factStatement(query historyapi.FactQuery) (string, []any, error) {
	var (
		text strings.Builder
		args []any
	)

	text.WriteString(`SELECT ref, epoch, sequence, at_us, type, class, payload
		FROM history_fact
		WHERE ref IN (` + placeholders(len(query.Refs)) + `)`)

	for _, ref := range query.Refs {
		args = append(args, ref.String())
	}

	// The host sets the class restriction. Like the permitted set, it is
	// applied inside the statement, so the count and the cursor cannot leak.
	if len(query.Classes) > 0 {
		text.WriteString(` AND class IN (` + placeholders(len(query.Classes)) + `)`)

		for _, class := range query.Classes {
			args = append(args, string(class))
		}
	}

	if len(query.Types) > 0 {
		text.WriteString(` AND type IN (` + placeholders(len(query.Types)) + `)`)

		for _, factType := range query.Types {
			args = append(args, factType)
		}
	}

	text.WriteString(` AND at_us >= ? AND at_us < ?`)
	args = append(args, boundOf(query.From, minMicro), boundOf(query.To, maxMicro))

	after, key, err := readCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}

	if key != "" {
		// Page on the whole ordering tuple. Two origins can have a fact at the
		// same instant.
		reference, sequence, found := strings.Cut(key, "\x00")

		number, parseErr := strconv.ParseInt(sequence, 10, 64)
		if !found || parseErr != nil {
			return "", nil, fmt.Errorf("%w: the cursor is not one this provider minted", ErrQuery)
		}

		text.WriteString(` AND (at_us > ? OR (at_us = ? AND (ref > ? OR (ref = ? AND sequence > ?))))`)
		args = append(args, after, after, reference, reference, number)
	}

	text.WriteString(` ORDER BY at_us, ref, sequence LIMIT ?`)
	args = append(args, limitOf(query.Limit))

	return text.String(), args, nil
}

// windowBounds is how many arguments the two ends of a window take.
const windowBounds = 2

// The window of a query with no bound, in Unix microseconds: year one to the
// end of year 9999, the range RFC 3339 can express. Every query has both
// bounds, so there is one statement shape.
const (
	minMicro = -62135596800000000
	maxMicro = 253402300799999999
)

// boundOf returns one end of the window, or open for a zero time.
func boundOf(at time.Time, open int64) int64 {
	if at.IsZero() {
		return open
	}

	return at.UnixMicro()
}

// limitOf clamps a caller's page size to the paging bounds.
func limitOf(asked int) int {
	switch {
	case asked <= 0:
		return defaultLimit
	case asked > maxLimit:
		return maxLimit
	default:
		return asked
	}
}

// placeholders returns n comma-separated '?' markers, or NULL for none.
func placeholders(n int) string {
	if n <= 0 {
		return `NULL`
	}

	return strings.TrimSuffix(strings.Repeat(`?,`, n), `,`)
}

// cursorAfter returns the opaque cursor for the next page, or "" when this
// page is the last one.
func cursorAfter(read, limit int, at int64, key string) string {
	if read < limit {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at, 10) + "\x00" + key))
}

// readCursor decodes a cursor from cursorAfter.
func readCursor(cursor string) (int64, string, error) {
	if cursor == "" {
		return 0, "", nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", fmt.Errorf("%w: the cursor is not one this provider minted", ErrQuery)
	}

	at, key, found := strings.Cut(string(raw), "\x00")
	if !found {
		return 0, "", fmt.Errorf("%w: the cursor is not one this provider minted", ErrQuery)
	}

	microseconds, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("%w: the cursor is not one this provider minted", ErrQuery)
	}

	return microseconds, key, nil
}
