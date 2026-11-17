package authz

import (
	"context"
	"database/sql"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// The projection is the two tables every enforcement surface reads, recompiled
// inside the transaction that moved the graph. Writing them is what is here.
// Who reaches what is project's answer, and which rows one mutation moved is
// scope's.

const (
	truncateAccessSQL = `DELETE FROM device_access`

	insertAccessPrefix = `INSERT INTO device_access ` +
		`(subject_type, subject_id, device_id, actions, since) VALUES `

	truncateResourceAccessSQL = `DELETE FROM resource_access`

	insertResourceAccessPrefix = `INSERT INTO resource_access ` +
		`(subject_type, subject_id, kind, actions, since) VALUES `

	// The generation each sight began at, read before the rows that carry it are
	// rewritten. Only a row that already holds the sight is read: a row that
	// holds some other action and gains the sight now begins it now, and one the
	// mask does not match has no generation to carry.
	selectDeviceSinceSQL = `SELECT subject_type, subject_id, device_id, since
		FROM device_access WHERE (actions & ?) <> 0`

	selectDeviceSinceOnSQL = selectDeviceSinceSQL + ` AND device_id = ?`

	selectResourceSinceSQL = `SELECT subject_type, subject_id, kind, since
		FROM resource_access WHERE (actions & ?) <> 0`

	selectResourceSinceOnSQL = selectResourceSinceSQL + ` AND kind = ?`

	truncateDeviceVerbSQL = `DELETE FROM device_verb`

	deleteDeviceVerbOnSQL = `DELETE FROM device_verb WHERE device_id = ?`

	insertDeviceVerbPrefix = `INSERT INTO device_verb ` +
		`(subject_type, subject_id, device_id, verb) VALUES `
)

const (
	// accessBatchRows keeps one statement well inside both engines' parameter
	// limits while still amortising the round trip.
	accessBatchRows = 100
	accessColumns   = 5
	verbColumns     = 4
)

// accessRowValues is one projected row's placeholder group, built once from
// accessColumns so the column list and the VALUES list cannot fall out of step.
var accessRowValues = "(" + placeholders(accessColumns) + ")"

// verbRowValues is the same for the verb projection's four columns.
var verbRowValues = "(" + placeholders(verbColumns) + ")"

// reproject recompiles both projections from the whole tuple table. It is what
// a mutation reaching more of the graph than scopedCeiling falls back to, what
// Rebuild runs, and what every scoped recompile is compared against.
//
// It is not free. Measured on 400 devices and 9,247 projected rows, on the
// pure-Go SQLite driver: 52 ms on SQLite and 170 ms on Postgres, of which the
// compiler itself is 7.0 ms and reading the tuples 1.2 ms, so it is the row
// writing rather than the graph walk (BenchmarkRebuild, BenchmarkProject). On
// Postgres it also leaves the previous 9,247 rows behind for autovacuum. That
// cost is why a write that moves a few objects goes through reprojectScope
// instead.
//
// epoch is the generation this mutation produced, which is where a sight this
// recompile creates begins.
func (s *Store) reproject(ctx context.Context, tx *sql.Tx, epoch uint64) error {
	tuples, err := loadTuples(ctx, tx, s.dialect)
	if err != nil {
		return err
	}

	devices, err := loadDevices(ctx, tx, s.dialect)
	if err != nil {
		return err
	}

	verbs, err := s.loadVerbIndex(ctx, tx, kindsOf(devices), nil)
	if err != nil {
		return err
	}

	compiled := project(tuples, devices, configurableKinds(), verbs)

	// Read before the rows go, because the answer is in the rows that are about
	// to be replaced. Anything the recompile leaves holding the same sight keeps
	// the generation that sight began at.
	held, err := s.readSince(ctx, tx, selectDeviceSinceSQL, int64(ActionObserve.mask()))
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, s.q(truncateAccessSQL)); err != nil {
		return storeFailure("clear projection", err)
	}

	if err = s.insertAccess(ctx, tx, compiled.Devices, held, epoch); err != nil {
		return err
	}

	held, err = s.readSince(ctx, tx, selectResourceSinceSQL, int64(ActionViewResource.mask()))
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, s.q(truncateResourceAccessSQL)); err != nil {
		return storeFailure("clear resource projection", err)
	}

	if err = s.insertResourceAccess(ctx, tx, compiled.Resources, held, epoch); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, s.q(truncateDeviceVerbSQL)); err != nil {
		return storeFailure("clear the verb projection", err)
	}

	return s.insertVerbs(ctx, tx, compiled.Verbs)
}

// insertVerbs writes the verb projection in batches. It carries no `since`
// column: a dangerous verb is not a sight, nothing subscribes to one, and there
// is no event stream bound to replay from.
func (s *Store) insertVerbs(ctx context.Context, tx *sql.Tx, rows []verbRow) error {
	for start := 0; start < len(rows); start += accessBatchRows {
		end := min(start+accessBatchRows, len(rows))

		batch := rows[start:end]
		args := make([]any, 0, len(batch)*verbColumns)

		for _, row := range batch {
			args = append(args, row.Subject.typ.String(), row.Subject.id, row.Device, row.Verb)
		}

		query := valuesOf(insertDeviceVerbPrefix, verbRowValues, len(batch))
		if _, err := tx.ExecContext(ctx, s.q(query), args...); err != nil {
			return storeFailure("insert the verb projection", err)
		}
	}

	return nil
}

// configurableKinds is the resource id space the projection is compiled over,
// taken from the registry rather than from a table: the kinds are a closed set
// in the code, so a grant naming anything else compiles to no row and reaches
// nothing, exactly as a grant on an unregistered device does.
func configurableKinds() []string {
	kinds := ref.ConfigurableKinds()

	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, kind.String())
	}

	return out
}

// sightKey addresses one projected row: who, and what they are looking at.
type sightKey struct {
	subject principal
	object  string
}

// since is the generation each projected sight began at, keyed by row. A key it
// does not hold is a sight that begins with the write reading it.
type since map[sightKey]uint64

// begin reports the generation to store for one row: the one already recorded
// when this sight is not new, and the generation of this write when it is.
func (s since) begin(who principal, object string, now uint64) uint64 {
	if began, held := s[sightKey{subject: who, object: object}]; held {
		return began
	}

	return now
}

// readSince reads when each sight in one of the two projections began. Both
// tables answer the same four columns in the same order, so one scan serves the
// whole recompile and the scoped one, and neither can read the other's answer
// differently.
func (s *Store) readSince(ctx context.Context, q queryer, query string, args ...any) (since, error) {
	rows, err := q.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, storeFailure("read when a sight began", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	out := since{}

	for rows.Next() {
		var (
			subjectType, subjectID, object string
			began                          uint64
		)

		if err = rows.Scan(&subjectType, &subjectID, &object, &began); err != nil {
			return nil, storeFailure("scan when a sight began", err)
		}

		out[sightKey{subject: principal{typ: Type(subjectType), id: subjectID}, object: object}] = began
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read when a sight began", err)
	}

	return out, nil
}

// projected is one row of either projection: who, the object and the mask it
// carries. The two tables are written by one function, so a column added to one
// cannot be left out of the other.
type projected struct {
	subject principal
	object  string
	actions actionSet
}

func (s *Store) insertAccess(
	ctx context.Context, tx *sql.Tx, rows []accessRow, held since, epoch uint64,
) error {
	out := make([]projected, 0, len(rows))
	for _, row := range rows {
		out = append(out, projected{
			subject: row.Subject, object: row.Device, actions: row.Actions,
		})
	}

	return s.insertProjection(ctx, tx, insertAccessPrefix, out, held, epoch)
}

func (s *Store) insertResourceAccess(
	ctx context.Context, tx *sql.Tx, rows []resourceAccessRow, held since, epoch uint64,
) error {
	out := make([]projected, 0, len(rows))
	for _, row := range rows {
		out = append(out, projected{
			subject: row.Subject, object: row.Kind, actions: row.Actions,
		})
	}

	return s.insertProjection(ctx, tx, insertResourceAccessPrefix, out, held, epoch)
}

// insertProjection writes one projection in batches, filling each row's since
// from the generation it already had or from this write's own.
func (s *Store) insertProjection(
	ctx context.Context, tx *sql.Tx, prefix string, rows []projected, held since, epoch uint64,
) error {
	for start := 0; start < len(rows); start += accessBatchRows {
		end := min(start+accessBatchRows, len(rows))

		batch := rows[start:end]
		args := make([]any, 0, len(batch)*accessColumns)

		for _, row := range batch {
			args = append(args, row.subject.typ.String(), row.subject.id, row.object,
				int64(row.actions), held.begin(row.subject, row.object, epoch))
		}

		query := valuesOf(prefix, accessRowValues, len(batch))
		if _, err := tx.ExecContext(ctx, s.q(query), args...); err != nil {
			return storeFailure("insert projection", err)
		}
	}

	return nil
}

// valuesOf builds the multi-row VALUES list for n rows of one shape. Only
// placeholders are concatenated; no value ever reaches the query text.
func valuesOf(prefix, row string, n int) string {
	var b strings.Builder

	b.Grow(len(prefix) + n*(len(row)+len(", ")))
	b.WriteString(prefix)

	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(row)
	}

	return b.String()
}
