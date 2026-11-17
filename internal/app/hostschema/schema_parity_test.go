package hostschema

import (
	"database/sql"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The aspects a snapshot is compared on. Each one names a property both engines
// can report, so a difference in it is a difference in the schema rather than
// in the dialect that spelled it.
const (
	aspectTable        = "table"
	aspectColumns      = "column names, in order"
	aspectClass        = "type class"
	aspectNotNull      = "not null"
	aspectDefault      = "default"
	aspectCollation    = "collation"
	aspectPrimaryKey   = "primary key"
	aspectIndexes      = "index names"
	aspectIndexColumns = "index columns, in order"
	aspectIndexUnique  = "index uniqueness"
	aspectIndexPartial = "index partiality"
	aspectForeignKeys  = "foreign keys"
	aspectChecks       = "check constraint count"
	aspectTriggers     = "trigger names"
	aspectViews        = "view names"
)

// schemaWide is the Table value for an aspect that belongs to the database
// rather than to one table. A view is not owned by a table, so it has no other
// name to be reported under.
const schemaWide = "(schema)"

// expectedTables is what a migrated database has on either engine. It is here
// so that a gate reading nothing fails: two empty snapshots agree perfectly,
// and an introspection query that silently returned no rows would otherwise be
// the greenest result this file can produce.
var expectedTables = []string{
	"authz_action",
	"authz_bootstrap",
	"authz_declared_grant",
	"authz_declared_retired",
	"authz_device",
	"authz_epoch",
	"schema_migration",
	"authz_relation_shape",
	"authz_resource",
	"authz_tuple",
	"authz_type",
	"camera",
	"dashboard",
	"dashboard_sequence",
	"device_access",
	"device_record",
	"printer_job",
	"resource_access",
	"schema_meta",
}

// schemaDifference is one property the two engines report differently.
type schemaDifference struct {
	Table    string
	Column   string
	Aspect   string
	SQLite   string
	Postgres string
}

// knownDifference is a schemaDifference the review accepted, with the reason it
// cannot be removed. The value on each side is part of the match, so an entry
// excuses the difference that was reviewed and not the aspect in general.
type knownDifference struct {
	Table    string
	Column   string
	Aspect   string
	SQLite   string
	Postgres string
	Why      string
}

// knownDifferences is the reviewed list. Two rules keep it honest, and both are
// enforced below: a difference with no entry fails, and an entry that no longer
// matches a difference fails. The second is what stops an entry outliving the
// schema it was written for, which is how an exception list turns into a place
// to put things.
var knownDifferences = []knownDifference{
	{
		Table:    "dashboard",
		Column:   "cards",
		Aspect:   aspectClass,
		SQLite:   "text",
		Postgres: "json",
		Why: "the cards of a dashboard are one JSON array, which SQLite keeps as TEXT and " +
			"Postgres as JSONB, for the reason device_record.tags gives. The CHECK on each " +
			"side refuses anything but an array",
	},
	{
		Table:    "camera",
		Column:   "config",
		Aspect:   aspectClass,
		SQLite:   "text",
		Postgres: "json",
		Why: "a camera's settings are one JSON object, kept as TEXT and JSONB for the reason " +
			"device_record.state_document gives. The CHECK on each side refuses anything but an object",
	},
	{
		Table:    "device_record",
		Column:   "tags",
		Aspect:   aspectClass,
		SQLite:   "text",
		Postgres: "json",
		Why: "SQLite has no JSON type, so the tag list is TEXT and the shape is held by the " +
			"reader. Postgres has JSONB, and taking it buys the CHECK that refuses anything " +
			"but an array at the door rather than at the next read. Both store the same " +
			"document and both default to '[]', so the difference is which engine can refuse " +
			"a malformed one, and refusing earlier is worth the divergence",
	},
	{
		Table:    "authz_device",
		Column:   "sort_key",
		Aspect:   aspectCollation,
		SQLite:   "binary",
		Postgres: "C",
		Why: "SQLite compares TEXT byte-wise and has no other collation to ask for; " +
			"Postgres compares in the database collation, so COLLATE \"C\" is what makes the " +
			"device list return the same page on both engines. Both sides spell the collation " +
			"out, so an unread collation is a difference rather than agreement",
	},
	{
		Table:    "authz_resource",
		Column:   "kind",
		Aspect:   aspectCollation,
		SQLite:   "binary",
		Postgres: "C",
		Why: "SQLite compares TEXT byte-wise and has no other collation to ask for; " +
			"Postgres compares in the database collation, so COLLATE \"C\" is what makes the " +
			"resource list return the same rows in the same order on both engines. It is also " +
			"what lets this column join the other table's: two columns whose collations are " +
			"both implicit and different have no resolvable collation for the equality operator",
	},
	{
		Table:    "authz_resource",
		Column:   "resource_id",
		Aspect:   aspectCollation,
		SQLite:   "binary",
		Postgres: "C",
		Why: "SQLite compares TEXT byte-wise and has no other collation to ask for; " +
			"Postgres compares in the database collation, so COLLATE \"C\" is what makes the " +
			"resource list return the same rows in the same order on both engines. It is also " +
			"what lets this column join the other table's: two columns whose collations are " +
			"both implicit and different have no resolvable collation for the equality operator",
	},
	{
		Table:    "device_record",
		Column:   "state_document",
		Aspect:   aspectClass,
		SQLite:   "text",
		Postgres: "json",
		Why: "the state tree is one JSON document, and the engines have no shared encoding for " +
			"one. SQLite's JSONB is an engine-private binary form with no cross-engine meaning, so a " +
			"column written as JSONB on one and jsonb on the other holds two byte strings for one " +
			"value and a dump moved between them becomes a conversion; earlier research measured what it " +
			"would buy at 8 per cent of a 311-byte value. Postgres jsonb is the type a GIN index " +
			"accepts and is what the same note recommends there. The CHECK on each side is what makes " +
			"the two agree about the value: both refuse anything that is not a JSON object",
	},
	{
		Table:    "resource_access",
		Column:   "kind",
		Aspect:   aspectCollation,
		SQLite:   "binary",
		Postgres: "C",
		Why: "SQLite compares TEXT byte-wise and has no other collation to ask for; " +
			"Postgres compares in the database collation, so COLLATE \"C\" is what makes the " +
			"resource list return the same rows in the same order on both engines. It is also " +
			"what lets this column join the other table's: two columns whose collations are " +
			"both implicit and different have no resolvable collation for the equality operator",
	},
}

// TestSchemaParity is the structural half of the dual-target gate (requirement
// 78, DB7): migrate both engines, read both schemas back, class the types, and
// hold the two to each other. The behavioural half is the forEachEngine suite,
// which covers what introspection cannot see: collation at runtime, NULL
// ordering, and the width of the bitmask in device_access.
func TestSchemaParity(t *testing.T) {
	t.Parallel()

	snapshots := migratedSnapshots(t)

	sqlite, hasSQLite := snapshots["sqlite"]
	postgres, hasPostgres := snapshots["postgres"]

	if !hasSQLite || !hasPostgres {
		t.Skipf("the parity gate compares two engines and this build reached %v; "+
			"build with -tags \"sqlite postgres\", set %s, and set %s=sqlite,postgres so that a "+
			"missing engine fails instead of reaching this line",
			slices.Sorted(maps.Keys(snapshots)), authztest.PostgresDSNEnv, authztest.RequiredEnginesEnv)
	}

	unmatched := make([]bool, len(knownDifferences))

	for _, difference := range diffSchemas(sqlite, postgres) {
		index, excused := matchKnown(difference)
		if !excused {
			t.Errorf("schema difference with no entry in knownDifferences:\n%s", render(difference))

			continue
		}

		unmatched[index] = true
	}

	for index, matched := range unmatched {
		if matched {
			continue
		}

		known := knownDifferences[index]
		t.Errorf("knownDifferences[%d] (%s.%s %s) no longer describes a difference. The engines "+
			"changed under the entry: restore the schema it was written for, or delete it",
			index, known.Table, known.Column, known.Aspect)
	}
}

// wideActionMask is above what int4 holds. actionSet is uint32 and no assigned
// bit comes near bit 40, so nothing writes this today; the column is declared
// wide on purpose and this is what holds it to that. The count of actions is
// deliberately not stated here, because a comment carrying one goes stale the
// next time the vocabulary grows.
const wideActionMask = int64(1) << 40

// TestActionBitmaskColumnIsWide is the behavioural half of the device_access
// width, and the only half there is. The parity gate classes int4 and int8 as
// one type by design, so a Postgres column narrowed to INTEGER is a difference
// no snapshot reports, and SQLite stores an INTEGER as 64 bits either way: the
// engines diverge only when a value crosses 2^31, which is what this writes.
func TestActionBitmaskColumnIsWide(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		_, err := db.ExecContext(t.Context(),
			e.Engine.Q(`INSERT INTO device_access (subject_type, subject_id, device_id, actions)
				VALUES ('user', 'wide', 'p1', ?)`),
			wideActionMask)
		require.NoError(t, err)

		var stored int64

		require.NoError(t, db.QueryRowContext(t.Context(),
			e.Engine.Q(`SELECT actions FROM device_access WHERE subject_id = ?`), "wide").Scan(&stored))
		require.Equal(t, wideActionMask, stored)
	})
}

// migratedSnapshots migrates every engine this build can reach, reads its schema
// back and holds the result to the landmarks before anything is compared.
// engines fails rather than skips when AUTHZ_REQUIRED_ENGINES names an engine
// that is not there, which is the only thing between this gate and a green run
// that compared one engine with itself.
func migratedSnapshots(t *testing.T) map[string]schemaSnapshot {
	t.Helper()

	out := map[string]schemaSnapshot{}

	for _, e := range engines(t) {
		db := e.Open(t)

		snapshot := introspect(t, db, e.Engine)

		requireLandmarks(t, e.Name, snapshot)
		requireProbedShapesReadable(t, e)

		out[e.Name] = snapshot
	}

	return out
}

func matchKnown(difference schemaDifference) (int, bool) {
	for index, known := range knownDifferences {
		if known.Table == difference.Table &&
			known.Column == difference.Column &&
			known.Aspect == difference.Aspect &&
			known.SQLite == difference.SQLite &&
			known.Postgres == difference.Postgres {
			return index, true
		}
	}

	return 0, false
}

func render(difference schemaDifference) string {
	return "  table    " + difference.Table +
		"\n  column   " + difference.Column +
		"\n  aspect   " + difference.Aspect +
		"\n  sqlite   " + difference.SQLite +
		"\n  postgres " + difference.Postgres
}

func diffSchemas(sqlite, postgres schemaSnapshot) []schemaDifference {
	var out []schemaDifference

	if !slices.Equal(sqlite.Views, postgres.Views) {
		out = append(out, schemaDifference{
			Table:    schemaWide,
			Aspect:   aspectViews,
			SQLite:   strings.Join(sqlite.Views, ","),
			Postgres: strings.Join(postgres.Views, ","),
		})
	}

	for _, table := range unionKeys(sqlite.Tables, postgres.Tables) {
		left, inSQLite := sqlite.Tables[table]
		right, inPostgres := postgres.Tables[table]

		if !inSQLite || !inPostgres {
			out = append(out, schemaDifference{
				Table:    table,
				Aspect:   aspectTable,
				SQLite:   presence(inSQLite),
				Postgres: presence(inPostgres),
			})

			continue
		}

		out = append(out, diffTable(table, left, right)...)
	}

	return out
}

func diffTable(table string, left, right tableShape) []schemaDifference {
	out := diffColumns(table, left.Columns, right.Columns)

	out = append(out, diffIndexes(table, left.Indexes, right.Indexes)...)

	scalars := []schemaDifference{
		{Table: table, Aspect: aspectPrimaryKey,
			SQLite: strings.Join(left.PrimaryKey, ","), Postgres: strings.Join(right.PrimaryKey, ",")},
		{Table: table, Aspect: aspectForeignKeys,
			SQLite: strings.Join(left.ForeignKeys, "; "), Postgres: strings.Join(right.ForeignKeys, "; ")},
		{Table: table, Aspect: aspectChecks,
			SQLite: strconv.Itoa(left.Checks), Postgres: strconv.Itoa(right.Checks)},
		{Table: table, Aspect: aspectTriggers,
			SQLite: strings.Join(left.Triggers, ","), Postgres: strings.Join(right.Triggers, ",")},
	}

	for _, scalar := range scalars {
		if scalar.SQLite != scalar.Postgres {
			out = append(out, scalar)
		}
	}

	return out
}

func diffColumns(table string, left, right []columnShape) []schemaDifference {
	var out []schemaDifference

	leftNames := columnNames(left)
	rightNames := columnNames(right)

	if !slices.Equal(leftNames, rightNames) {
		out = append(out, schemaDifference{
			Table:    table,
			Aspect:   aspectColumns,
			SQLite:   strings.Join(leftNames, ","),
			Postgres: strings.Join(rightNames, ","),
		})
	}

	byName := make(map[string]columnShape, len(right))
	for _, column := range right {
		byName[column.Name] = column
	}

	for _, column := range left {
		other, shared := byName[column.Name]
		if !shared {
			continue
		}

		out = append(out, diffColumn(table, column, other)...)
	}

	return out
}

func diffColumn(table string, left, right columnShape) []schemaDifference {
	candidates := []schemaDifference{
		{Aspect: aspectClass, SQLite: left.Class, Postgres: right.Class},
		{Aspect: aspectNotNull, SQLite: strconv.FormatBool(left.NotNull), Postgres: strconv.FormatBool(right.NotNull)},
		{Aspect: aspectDefault, SQLite: left.Default, Postgres: right.Default},
		{Aspect: aspectCollation, SQLite: left.Collation, Postgres: right.Collation},
	}

	var out []schemaDifference

	for _, candidate := range candidates {
		if candidate.SQLite == candidate.Postgres {
			continue
		}

		candidate.Table = table
		candidate.Column = left.Name
		out = append(out, candidate)
	}

	return out
}

func diffIndexes(table string, left, right map[string]indexShape) []schemaDifference {
	var out []schemaDifference

	leftNames := slices.Sorted(maps.Keys(left))
	rightNames := slices.Sorted(maps.Keys(right))

	if !slices.Equal(leftNames, rightNames) {
		out = append(out, schemaDifference{
			Table:    table,
			Aspect:   aspectIndexes,
			SQLite:   strings.Join(leftNames, ","),
			Postgres: strings.Join(rightNames, ","),
		})
	}

	for _, name := range leftNames {
		other, shared := right[name]
		if !shared {
			continue
		}

		index := left[name]

		if !slices.Equal(index.Columns, other.Columns) {
			out = append(out, schemaDifference{
				Table: table, Column: name, Aspect: aspectIndexColumns,
				SQLite: strings.Join(index.Columns, ","), Postgres: strings.Join(other.Columns, ","),
			})
		}

		if index.Unique != other.Unique {
			out = append(out, schemaDifference{
				Table: table, Column: name, Aspect: aspectIndexUnique,
				SQLite: strconv.FormatBool(index.Unique), Postgres: strconv.FormatBool(other.Unique),
			})
		}

		if index.Partial != other.Partial {
			out = append(out, schemaDifference{
				Table: table, Column: name, Aspect: aspectIndexPartial,
				SQLite: strconv.FormatBool(index.Partial), Postgres: strconv.FormatBool(other.Partial),
			})
		}
	}

	return out
}

func columnNames(columns []columnShape) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, column.Name)
	}

	return out
}

func unionKeys(left, right map[string]tableShape) []string {
	seen := make(map[string]struct{}, len(left)+len(right))

	for name := range left {
		seen[name] = struct{}{}
	}

	for name := range right {
		seen[name] = struct{}{}
	}

	return slices.Sorted(maps.Keys(seen))
}

func presence(present bool) string {
	if present {
		return "present"
	}

	return "absent"
}

// schemaLandmark is one positive assertion about a snapshot, for one aspect the
// diff reports.
type schemaLandmark struct {
	Aspect string
	Assert func(tb testing.TB, engine string, snapshot schemaSnapshot)
}

// schemaLandmarks holds each engine's snapshot to structure the migration is
// known to create, before the two are compared. A diff is a comparison, so an
// introspection query blinded on both engines agrees with itself perfectly and
// the gate reports ok: every aspect needs an assertion a blinded query cannot
// satisfy, and this is that list. Index uniqueness, index partiality, a
// descending key, a trigger and a view have no instance in this schema, so
// requireProbedShapesReadable builds one of each instead.
var schemaLandmarks = []schemaLandmark{
	{Aspect: aspectTable, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		for _, table := range expectedTables {
			require.Containsf(tb, snapshot.Tables, table, "%s introspection returned no %s", engine, table)
		}
	}},

	{Aspect: aspectColumns, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, tupleColumns, columnNames(landmarkTable(tb, engine, snapshot, "authz_tuple").Columns))
	}},

	{Aspect: aspectClass, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, "int", landmarkColumn(tb, engine, snapshot, "device_access", "actions").Class)
		require.Equal(tb, "text", landmarkColumn(tb, engine, snapshot, "authz_device", "name").Class)
	}},

	{Aspect: aspectNotNull, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.True(tb, landmarkColumn(tb, engine, snapshot, "authz_tuple", "object_id").NotNull)
	}},

	{Aspect: aspectDefault, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, "''", landmarkColumn(tb, engine, snapshot, "authz_tuple", "subject_relation").Default)
	}},

	{Aspect: aspectCollation, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		// Both files spell the collation of the pagination key out, so an empty
		// value here is a reader that is not reading rather than a column that
		// takes the database default.
		require.NotEmpty(tb, landmarkColumn(tb, engine, snapshot, "authz_device", "sort_key").Collation)
	}},

	{Aspect: aspectPrimaryKey, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, []string{"subject_type", "subject_id", "device_id"},
			landmarkTable(tb, engine, snapshot, "device_access").PrimaryKey)
	}},

	{Aspect: aspectIndexes, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, []string{"authz_tuple_by_shape", "authz_tuple_by_subject"},
			slices.Sorted(maps.Keys(landmarkTable(tb, engine, snapshot, "authz_tuple").Indexes)))
	}},

	{Aspect: aspectIndexColumns, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, []string{"sort_key", "device_id"},
			landmarkIndex(tb, engine, snapshot, "authz_device", "authz_device_by_sort_key").Columns)
	}},

	{Aspect: aspectForeignKeys, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, []string{tupleForeignKey},
			landmarkTable(tb, engine, snapshot, "authz_tuple").ForeignKeys)
	}},

	{Aspect: aspectChecks, Assert: func(tb testing.TB, engine string, snapshot schemaSnapshot) {
		require.Equal(tb, 1, landmarkTable(tb, engine, snapshot, "authz_epoch").Checks)
	}},
}

// tupleColumns is authz_tuple in declaration order, which is the order both
// engines report and therefore the order the diff compares.
var tupleColumns = []string{
	"object_type", "object_id", "relation", "subject_type", "subject_id", "subject_relation",
}

// tupleForeignKey is the composite constraint that makes the vocabulary real,
// rendered the way both engines' snapshots render it.
const tupleForeignKey = "object_type,relation,subject_type,subject_relation -> " +
	"authz_relation_shape(object_type,relation,subject_type,subject_relation) " +
	"on delete no action on update no action"

func requireLandmarks(t *testing.T, engine string, snapshot schemaSnapshot) {
	t.Helper()

	for _, landmark := range schemaLandmarks {
		t.Run(engine+"/"+landmark.Aspect, func(t *testing.T) {
			landmark.Assert(t, engine, snapshot)
		})
	}
}

// probeIndexes is what the probe DDL below builds, named here rather than read
// from the snapshot. A gate written over what came back passes over a shrinking
// input set: a reader that returned two of the four indexes would agree with
// itself, and the missing two would take their aspect out of the comparison
// without a word. This names what must be present.
var probeIndexes = map[string]indexShape{
	"authz_index_probe_plain":      {Columns: []string{"a"}, Unique: false, Partial: false},
	"authz_index_probe_unique":     {Columns: []string{"b"}, Unique: true, Partial: false},
	"authz_index_probe_partial":    {Columns: []string{"a"}, Unique: false, Partial: true},
	"authz_index_probe_descending": {Columns: []string{"a desc"}, Unique: false, Partial: false},
}

// probeDDL builds one instance of every shape the migrated schema has none of:
// a unique index, a partial one, a descending key, a trigger and a view.
//
// Without it those aspects compare nothing. A diff is a comparison, so a reader
// that answers "no triggers" on both engines agrees perfectly, and the gate
// reports the schemas match while seeing none of the five.
//
// The trigger is the one shape the two engines do not declare alike: Postgres
// runs a function and SQLite runs a statement body.
func probeDDL(engine sqlstore.Engine) []string {
	common := []string{
		`CREATE TABLE authz_index_probe (a TEXT NOT NULL, b TEXT NOT NULL)`,
		`CREATE INDEX authz_index_probe_plain ON authz_index_probe (a)`,
		`CREATE UNIQUE INDEX authz_index_probe_unique ON authz_index_probe (b)`,
		`CREATE INDEX authz_index_probe_partial ON authz_index_probe (a) WHERE b <> ''`,
		`CREATE INDEX authz_index_probe_descending ON authz_index_probe (a DESC)`,
		`CREATE VIEW authz_view_probe AS SELECT a FROM authz_index_probe`,
	}

	if engine == sqlstore.EnginePostgres {
		return append(common,
			`CREATE FUNCTION authz_trigger_probe_body() RETURNS trigger
				LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
			`CREATE TRIGGER authz_trigger_probe BEFORE UPDATE ON authz_index_probe
				FOR EACH ROW EXECUTE FUNCTION authz_trigger_probe_body()`)
	}

	return append(common,
		`CREATE TRIGGER authz_trigger_probe BEFORE UPDATE ON authz_index_probe
			BEGIN SELECT 1; END`)
}

func requireProbedShapesReadable(tb testing.TB, e engine) {
	tb.Helper()

	db := e.Open(tb)

	for _, statement := range probeDDL(e.Engine) {
		_, err := db.ExecContext(tb.Context(), statement)
		require.NoErrorf(tb, err, "%s could not build the probe: %s", e.Name, statement)
	}

	snapshot := introspect(tb, db, e.Engine)

	probe, present := snapshot.Tables["authz_index_probe"]
	require.Truef(tb, present, "%s reported no probe table", e.Name)
	require.Lenf(tb, probe.Indexes, len(probeIndexes), "%s reported %d of the %d probe indexes",
		e.Name, len(probe.Indexes), len(probeIndexes))

	for name, want := range probeIndexes {
		got, indexed := probe.Indexes[name]

		require.Truef(tb, indexed, "%s reported no %s", e.Name, name)
		require.Equalf(tb, want.Columns, got.Columns, "%s read the wrong key for %s", e.Name, name)
		require.Equalf(tb, want.Unique, got.Unique, "%s read the wrong uniqueness for %s", e.Name, name)
		require.Equalf(tb, want.Partial, got.Partial, "%s read the wrong partiality for %s", e.Name, name)
	}

	require.Equalf(tb, []string{"authz_trigger_probe"}, probe.Triggers,
		"%s does not read a trigger, so a trigger on either engine is agreement", e.Name)
	require.Equalf(tb, []string{"authz_view_probe"}, snapshot.Views,
		"%s does not read a view, so a view on either engine is agreement", e.Name)
}

func landmarkTable(tb testing.TB, engine string, snapshot schemaSnapshot, table string) tableShape {
	tb.Helper()

	shape, present := snapshot.Tables[table]
	require.Truef(tb, present, "%s introspection returned no %s", engine, table)

	return shape
}

func landmarkColumn(
	tb testing.TB, engine string, snapshot schemaSnapshot, table, column string,
) columnShape {
	tb.Helper()

	for _, shape := range landmarkTable(tb, engine, snapshot, table).Columns {
		if shape.Name == column {
			return shape
		}
	}

	require.FailNowf(tb, "missing column", "%s introspection returned no %s.%s", engine, table, column)

	return columnShape{}
}

func landmarkIndex(
	tb testing.TB, engine string, snapshot schemaSnapshot, table, index string,
) indexShape {
	tb.Helper()

	shape, present := landmarkTable(tb, engine, snapshot, table).Indexes[index]
	require.Truef(tb, present, "%s introspection returned no %s on %s", engine, index, table)

	return shape
}

// checkedConstraints is every CHECK this schema declares, with whatever has to
// exist first and a statement the constraint has to refuse.
//
// The snapshot counts CHECK constraints and does not read them: SQLite exposes
// one only as the DDL text it was declared with, so the two engines have no
// rendering to hold each other to. Two databases can therefore agree on the
// count and disagree on what the constraint does. What it does is behavioural,
// and this is the behaviour.
//
// The first two are the singleton-row guard. Each table holds one row that is
// the whole of its state, and a second row would make the read that follows it
// ambiguous. The third is the device record's document, which is TEXT on one
// engine and jsonb on the other: on neither does the column type alone say that
// the value is an object, and everything that reads it unmarshals one.
var checkedConstraints = []struct {
	Table string
	// Setup is what has to be in the database before the refusal can be about
	// the CHECK. Without the directory row the record write below would be
	// refused by the foreign key, and the case would pass having proved
	// nothing about the constraint it names.
	Setup     []string
	Statement string
}{
	{Table: "authz_epoch", Statement: `INSERT INTO authz_epoch (id, epoch) VALUES (2, 1)`},
	{Table: "authz_bootstrap", Statement: `INSERT INTO authz_bootstrap (id, owner) VALUES (2, 'user:root')`},
	{
		Table: "device_record",
		Setup: []string{
			`INSERT INTO authz_device (device_id, name, sort_key, kind)
				VALUES ('p-check', 'Check', 'check', 'printer')`,
		},
		// A JSON string is valid JSON and is not an object, which is the case
		// json_valid alone would take. Both engines refuse it here: SQLite
		// because json_type is text, Postgres because jsonb_typeof is string.
		Statement: `INSERT INTO device_record
			(device_id, last_seen, state_document)
			VALUES ('p-check', '2026-01-01T00:00:00Z', '"not an object"')`,
	},
	{
		Table: "device_record",
		Setup: []string{
			`INSERT INTO authz_device (device_id, name, sort_key, kind)
				VALUES ('p-tags', 'Tags', 'tags', 'printer')`,
		},
		// The tag column carries its own CHECK beside the state document's. A
		// row may hold a valid state tree and a tag value that is not an array.
		Statement: `INSERT INTO device_record
			(device_id, last_seen, tags)
			VALUES ('p-tags', '2026-01-01T00:00:00Z', '{}')`,
	},
	{
		Table: "camera",
		Setup: []string{
			`INSERT INTO authz_device (device_id, name, sort_key, kind)
				VALUES ('c-check', 'Check', 'check', 'camera')`,
		},
		// The settings are an object. A list is valid JSON and is not one.
		Statement: `INSERT INTO camera (device_id, type, config) VALUES ('c-check', 'generic', '[]')`,
	},
	{
		Table: "dashboard",
		// A visibility past administrators is no level the listing compares.
		Statement: `INSERT INTO dashboard
			(id, name, audience, revision, created_at, created_by, changed_at, changed_by)
			VALUES (90, 'Wall', 3, 1, '2026-01-01T00:00:00Z', 'user:a', '2026-01-01T00:00:00Z', 'user:a')`,
	},
	{
		Table: "dashboard",
		// The cards are a list. An object is valid JSON and is not one.
		Statement: `INSERT INTO dashboard
			(id, name, cards, revision, created_at, created_by, changed_at, changed_by)
			VALUES (91, 'Wall', '{}', 1, '2026-01-01T00:00:00Z', 'user:a', '2026-01-01T00:00:00Z', 'user:a')`,
	},
	{
		Table: "dashboard_sequence",
		// The sequence is one row, and a second one would give out an id twice.
		Statement: `INSERT INTO dashboard_sequence (slot, next_id) VALUES (2, 1)`,
	},
	{
		Table: "server_instance",
		// The server answers to one id, and a second row would be a second one.
		Statement: `INSERT INTO server_instance (slot, instance_id, created_at) VALUES (2, 'x', '2026-10-24 08:00:00')`,
	},
}

// TestEveryCheckConstraintRefusesTheRowItWasWrittenFor is the behavioural half
// of the check-constraint aspect, and the only half there is.
func TestEveryCheckConstraintRefusesTheRowItWasWrittenFor(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		// The arming half. A CHECK dropped from a migration would otherwise
		// leave the list above naming a constraint the schema no longer has,
		// and the case for it would pass because the insert failed for some
		// other reason or because nothing refused it at all.
		declared := 0
		for _, table := range introspect(t, db, e.Engine).Tables {
			declared += table.Checks
		}

		require.Equalf(t, len(checkedConstraints), declared,
			"%s declares %d CHECK constraints and this test names %d",
			e.Name, declared, len(checkedConstraints))

		for _, constraint := range checkedConstraints {
			for _, statement := range constraint.Setup {
				_, err := db.ExecContext(t.Context(), e.Engine.Q(statement))
				require.NoErrorf(t, err, "%s could not build the row the %s case needs",
					e.Name, constraint.Table)
			}

			_, err := db.ExecContext(t.Context(), e.Engine.Q(constraint.Statement))
			require.Errorf(t, err, "%s took the row %s's CHECK is written to refuse",
				e.Name, constraint.Table)
		}
	})
}

// orderedColumns is every column name a shipped statement in orderedSources
// orders by. It is compared against the source rather than trusted, so a
// statement that orders by a new column fails here until the name is added and
// the column is held to NOT NULL below.
var orderedColumns = []string{
	"device_id", "kind", "module", "object_id", "printer_job_id", "relation", "resource_id",
	"seq", "sort_key", "started", "subject_id", "subject_relation", "subject_type", "verb",
}

// orderByClause captures what follows ORDER BY on one line. Every ORDER BY in
// this package is written on a line of its own, and the clause ends at the
// closing backtick or at the end of the line.
var orderByClause = regexp.MustCompile(`(?i)ORDER BY\s+(.*)$`)

// TestNothingIsOrderedByAColumnThatCanBeNull is the ordering rule the engines
// make necessary. Postgres sorts a NULL last in an ascending order and SQLite
// sorts it first, so one nullable column in an ORDER BY is a page that holds
// the same rows in a different order on the two engines. Neither default is
// wrong and neither is the schema's to choose, so no ordered column may be
// null.
//
// The alternative is spelling NULLS LAST in every statement, which SQLite has
// taken only since 3.30 and which the projection does not need: every column
// ordered by today is part of a key or a directory row that cannot be absent.
func TestNothingIsOrderedByAColumnThatCanBeNull(t *testing.T) {
	t.Parallel()

	require.Equal(t, orderedColumns, orderedColumnsInSource(t),
		"a statement orders by a column this test does not name")

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		found := 0

		for name, table := range introspect(t, db, e.Engine).Tables {
			for _, column := range table.Columns {
				if !slices.Contains(orderedColumns, column.Name) {
					continue
				}

				found++

				require.Truef(t, column.NotNull, "%s.%s is ordered by and takes a null",
					name, column.Name)
			}
		}

		require.NotZerof(t, found, "%s reported no ordered column, so this compared nothing", e.Name)
	})
}

// TestTheEnginesDisagreeAboutWhereANullSorts is what makes the rule above worth
// having. It is a property of the engines rather than of this schema, so it is
// written down here: the day either of them changes its default, the rule can
// be reconsidered rather than rediscovered.
func TestTheEnginesDisagreeAboutWhereANullSorts(t *testing.T) {
	t.Parallel()

	wantFirst := map[sqlstore.Engine]bool{sqlstore.EngineSQLite: true, sqlstore.EnginePostgres: false}

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		for _, statement := range []string{
			`CREATE TABLE authz_null_probe (c TEXT)`,
			`INSERT INTO authz_null_probe (c) VALUES ('a')`,
			`INSERT INTO authz_null_probe (c) VALUES (NULL)`,
		} {
			_, err := db.ExecContext(t.Context(), statement)
			require.NoError(t, err)
		}

		var ordered []sql.NullString

		eachRow(t, db, `SELECT c FROM authz_null_probe ORDER BY c`, nil, func(rows *sql.Rows) error {
			var value sql.NullString

			if err := rows.Scan(&value); err != nil {
				return err
			}

			ordered = append(ordered, value)

			return nil
		})

		require.Len(t, ordered, 2)
		require.Equalf(t, wantFirst[e.Engine], !ordered[0].Valid,
			"%s no longer sorts a null where this rule assumes it does", e.Name)
	})
}

// orderedSources is the code whose statements are read for an ORDER BY,
// relative to this directory.
var orderedSources = []string{"../../authz/*.go", "../../platform/devicestore/*.go"}

// orderedColumnsInSource reads the statements of orderedSources and returns
// every column name an ORDER BY carries, lowercased and sorted. A comment line
// is skipped: prose there uses the words in their English sense.
func orderedColumnsInSource(tb testing.TB) []string {
	tb.Helper()

	var paths []string

	for _, pattern := range orderedSources {
		matched, err := filepath.Glob(pattern)
		require.NoError(tb, err)

		paths = append(paths, matched...)
	}

	seen := map[string]struct{}{}

	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		body, readErr := os.ReadFile(path)
		require.NoError(tb, readErr)

		for line := range strings.SplitSeq(string(body), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}

			match := orderByClause.FindStringSubmatch(line)
			if match == nil {
				continue
			}

			for _, name := range orderKeyNames(match[1]) {
				seen[name] = struct{}{}
			}
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

// orderKeyNames splits one ORDER BY clause into the column names it carries,
// dropping the table alias, the sort direction and anything past the closing
// backtick of the statement literal.
func orderKeyNames(clause string) []string {
	if cut := strings.IndexByte(clause, '`'); cut >= 0 {
		clause = clause[:cut]
	}

	var out []string

	for term := range strings.SplitSeq(clause, ",") {
		name := strings.TrimSpace(term)

		for _, direction := range []string{" desc", " asc"} {
			name = strings.TrimSuffix(strings.ToLower(name), direction)
		}

		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}

		if name != "" {
			out = append(out, name)
		}
	}

	return out
}
