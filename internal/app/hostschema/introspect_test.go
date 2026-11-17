package hostschema

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The schema as an engine reports it back, reduced to what both engines can
// answer for. Names are lowercased and types are classed, so the comparison in
// schema_parity_test.go is between two schemas rather than between two
// dialects; anything that survives the classing is a real difference and needs
// an entry in knownDifferences.
type schemaSnapshot struct {
	Tables map[string]tableShape
	// View names, lowercased and sorted. A view is a table to every reader of
	// the projection, so one that exists on one engine and not the other is a
	// schema difference that the table list alone cannot report.
	Views []string
}

type tableShape struct {
	// Ordered by the position the engine declares, so a reordered column is a
	// difference rather than a shrug.
	Columns    []columnShape
	PrimaryKey []string
	Indexes    map[string]indexShape
	// Canonical "cols -> table(cols)" strings, sorted. Constraint names are not
	// compared: SQLite leaves an inline foreign key unnamed and Postgres invents
	// one, which is a naming difference and not a schema difference.
	ForeignKeys []string
	// Counted, not compared as text. SQLite exposes a CHECK only as the DDL
	// string it was declared with, so the two engines have no common rendering
	// to hold each other to. What each CHECK does is held instead by
	// TestEveryCheckConstraintRefusesTheRowItWasWrittenFor.
	Checks int
	// Trigger names, lowercased and sorted. A trigger rewrites a row on its way
	// into the table, so one on an engine that does not have it on the other is
	// two databases that answer differently from the same statement. Nothing
	// else in the snapshot can show it: every column, index and constraint of
	// the two tables agrees.
	Triggers []string
}

type columnShape struct {
	Name    string
	Class   string
	NotNull bool
	// Lowercased, with a Postgres cast suffix removed, so '' and ''::text are
	// the same default.
	Default string
	// The declared collation, lowercased, empty when the column takes the
	// database default. SQLite exposes it only in the stored DDL, so it is read
	// from there: a collation the gate cannot see is one that can be changed on
	// the SQLite side without the diff moving, and NOCASE or RTRIM on the
	// pagination key reorders a page.
	Collation string
}

type indexShape struct {
	// One entry per key column, in index order, with " desc" appended to a
	// descending one. An expression is "(expression)" on both engines: SQLite
	// reports no text for one, so comparing Postgres's reconstruction against it
	// would be a difference in what the engines can say rather than in the
	// schema.
	Columns []string
	Unique  bool
	// A partial index answers a different question from a full one with the
	// same name and columns, and nothing else in the snapshot would show it.
	Partial bool
}

// typeClasses maps every declared type either engine uses onto one class. A
// type outside this table fails the gate rather than defaulting, because two
// unrecognised types would otherwise compare equal to each other.
var typeClasses = map[string]string{
	"text":                        "text",
	"character varying":           "text",
	"varchar":                     "text",
	"int":                         "int",
	"integer":                     "int",
	"bigint":                      "int",
	"smallint":                    "int",
	"boolean":                     "bool",
	"jsonb":                       "json",
	"blob":                        "blob",
	"bytea":                       "blob",
	"real":                        "real",
	"double precision":            "real",
	"numeric":                     "real",
	"timestamp":                   "time",
	"timestamptz":                 "time",
	"timestamp with time zone":    "time",
	"timestamp without time zone": "time",
	"datetime":                    "time",
}

// checkKeyword finds a CHECK constraint in SQLite's stored DDL. Comments are
// stripped before it runs, so the only false positive left would be an object
// literally named check, which this schema does not have.
var checkKeyword = regexp.MustCompile(`(?i)\bcheck\s*\(`)

// sqlComment matches a line comment or a block comment in the stored DDL.
var sqlComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)

// postgresCast strips the ::type a Postgres default carries.
var postgresCast = regexp.MustCompile(`::[a-z ]+$`)

// The stored DDL is the only place SQLite records a table column's collation:
// no pragma reports it, and a collation the gate cannot read is one that can be
// changed on the SQLite side without the diff moving.
var (
	// sqliteDefinitionName matches the identifier a column definition opens with.
	// A table-level PRIMARY KEY, FOREIGN KEY or CHECK clause matches its keyword,
	// which is not a column name and is therefore never looked up.
	sqliteDefinitionName = regexp.MustCompile(`(?is)^\s*(?:"([^"]+)"|\[([^\]]+)\]|([a-z_][a-z0-9_$]*))`)

	sqliteCollateClause = regexp.MustCompile(`(?is)\bcollate\s+(?:"([^"]+)"|([a-z_][a-z0-9_$]*))`)
)

// indexExpression stands in for an index key that is an expression rather than
// a column. SQLite reports no text for one, so the two engines are held to the
// shape they can both answer for and the expression itself is not compared.
const indexExpression = "(expression)"

// indexKey renders one index key column the way both engines can report it.
func indexKey(name string, descending bool) string {
	if descending {
		return name + " desc"
	}

	return name
}

// sqliteCollations maps column name to declared collation, from a table's
// stored DDL with comments already stripped.
func sqliteCollations(ddl string) map[string]string {
	out := map[string]string{}

	first := strings.Index(ddl, "(")
	last := strings.LastIndex(ddl, ")")

	if first < 0 || last <= first {
		return out
	}

	for _, definition := range splitTopLevel(ddl[first+1 : last]) {
		name := sqliteDefinitionName.FindStringSubmatch(definition)
		collate := sqliteCollateClause.FindStringSubmatch(definition)

		if name == nil || collate == nil {
			continue
		}

		out[strings.ToLower(firstNonEmpty(name[1:]))] = strings.ToLower(firstNonEmpty(collate[1:]))
	}

	return out
}

// splitTopLevel cuts a comma-separated DDL body at the commas outside every
// parenthesis, so a composite key or a CHECK stays one item.
func splitTopLevel(body string) []string {
	var (
		out   []string
		depth int
		start int
	)

	for i := range len(body) {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}

	return append(out, body[start:])
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// introspect reads the schema back out of a migrated database.
func introspect(tb testing.TB, db *sql.DB, engine sqlstore.Engine) schemaSnapshot {
	tb.Helper()

	if engine == sqlstore.EnginePostgres {
		return introspectPostgres(tb, db)
	}

	return introspectSQLite(tb, db)
}

const (
	sqliteTablesSQL = `SELECT name, sql FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`

	sqliteColumnsSQL = `SELECT name, type, "notnull", dflt_value, pk
		FROM pragma_table_info(?) ORDER BY cid`

	// key = 1 drops the trailing rowid and primary key columns SQLite appends to
	// every index, which Postgres does not have and does not report.
	sqliteIndexColumnsSQL = `SELECT cid, name, "desc" FROM pragma_index_xinfo(?)
		WHERE key = 1 ORDER BY seqno`

	// origin is 'pk' for the index SQLite builds for a primary key, which
	// Postgres also builds and names differently. The primary key itself is
	// compared as a column list, so both engines' implicit indexes drop out.
	sqliteIndexesSQL = `SELECT name, "unique", partial FROM pragma_index_list(?) WHERE origin <> 'pk'`

	sqliteForeignKeysSQL = `SELECT id, "table", "from", "to", on_delete, on_update
		FROM pragma_foreign_key_list(?) ORDER BY id, seq`

	sqliteTriggersSQL = `SELECT name FROM sqlite_master
		WHERE type = 'trigger' AND tbl_name = ? ORDER BY name`

	sqliteViewsSQL = `SELECT name FROM sqlite_master
		WHERE type = 'view' AND name NOT LIKE 'sqlite_%' ORDER BY name`
)

func introspectSQLite(tb testing.TB, db *sql.DB) schemaSnapshot {
	tb.Helper()

	snapshot := schemaSnapshot{Tables: map[string]tableShape{}}

	type storedTable struct {
		name string
		ddl  string
	}

	var tables []storedTable

	eachRow(tb, db, sqliteTablesSQL, nil, func(rows *sql.Rows) error {
		var (
			name string
			ddl  sql.NullString
		)

		if err := rows.Scan(&name, &ddl); err != nil {
			return err
		}

		tables = append(tables, storedTable{name: strings.ToLower(name), ddl: ddl.String})

		return nil
	})

	for _, table := range tables {
		ddl := sqlComment.ReplaceAllString(table.ddl, " ")

		shape := tableShape{Indexes: map[string]indexShape{}}
		shape.Columns, shape.PrimaryKey = sqliteColumns(tb, db, table.name, sqliteCollations(ddl))
		shape.Indexes = sqliteIndexes(tb, db, table.name)
		shape.ForeignKeys = sqliteForeignKeys(tb, db, table.name)
		shape.Checks = len(checkKeyword.FindAllString(ddl, -1))
		shape.Triggers = queriedNames(tb, db, sqliteTriggersSQL, []any{table.name})

		snapshot.Tables[table.name] = shape
	}

	snapshot.Views = queriedNames(tb, db, sqliteViewsSQL, nil)

	return snapshot
}

// queriedNames runs a query whose one column is a name and returns them
// lowercased, in the order the query asked for.
func queriedNames(tb testing.TB, db *sql.DB, query string, args []any) []string {
	tb.Helper()

	out := []string{}

	eachRow(tb, db, query, args, func(rows *sql.Rows) error {
		var name string

		if err := rows.Scan(&name); err != nil {
			return err
		}

		out = append(out, strings.ToLower(name))

		return nil
	})

	return out
}

func sqliteColumns(
	tb testing.TB, db *sql.DB, table string, collations map[string]string,
) ([]columnShape, []string) {
	tb.Helper()

	var (
		columns []columnShape
		keyed   []keyColumn
	)

	eachRow(tb, db, sqliteColumnsSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			name, declared string
			notNull, pk    int
			dflt           sql.NullString
		)

		if err := rows.Scan(&name, &declared, &notNull, &dflt, &pk); err != nil {
			return err
		}

		name = strings.ToLower(name)

		columns = append(columns, columnShape{
			Name:      name,
			Class:     typeClass(tb, declared),
			NotNull:   notNull != 0,
			Default:   normaliseDefault(dflt.String),
			Collation: collations[name],
		})

		if pk != 0 {
			keyed = append(keyed, keyColumn{position: pk, name: name})
		}

		return nil
	})

	return columns, orderKey(keyed)
}

func sqliteIndexes(tb testing.TB, db *sql.DB, table string) map[string]indexShape {
	tb.Helper()

	indexes := map[string]indexShape{}

	var names []string

	eachRow(tb, db, sqliteIndexesSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			name            string
			unique, partial int
		)

		if err := rows.Scan(&name, &unique, &partial); err != nil {
			return err
		}

		name = strings.ToLower(name)
		names = append(names, name)
		indexes[name] = indexShape{Unique: unique != 0, Partial: partial != 0}

		return nil
	})

	for _, name := range names {
		shape := indexes[name]

		eachRow(tb, db, sqliteIndexColumnsSQL, []any{name}, func(rows *sql.Rows) error {
			var (
				cid, descending int
				column          sql.NullString
			)

			if err := rows.Scan(&cid, &column, &descending); err != nil {
				return err
			}

			// A negative cid is an expression or the rowid, and the name comes
			// back NULL for both.
			key := indexExpression
			if cid >= 0 && column.Valid {
				key = strings.ToLower(column.String)
			}

			shape.Columns = append(shape.Columns, indexKey(key, descending != 0))

			return nil
		})

		indexes[name] = shape
	}

	return indexes
}

func sqliteForeignKeys(tb testing.TB, db *sql.DB, table string) []string {
	tb.Helper()

	parts := map[int]*foreignKeyParts{}

	var order []int

	eachRow(tb, db, sqliteForeignKeysSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			id                                       int
			parent, from, target, onDelete, onUpdate string
		)

		if err := rows.Scan(&id, &parent, &from, &target, &onDelete, &onUpdate); err != nil {
			return err
		}

		if _, seen := parts[id]; !seen {
			parts[id] = &foreignKeyParts{
				parent:   strings.ToLower(parent),
				onDelete: strings.ToLower(onDelete),
				onUpdate: strings.ToLower(onUpdate),
			}
			order = append(order, id)
		}

		parts[id].columns = append(parts[id].columns, strings.ToLower(from))
		parts[id].targets = append(parts[id].targets, strings.ToLower(target))

		return nil
	})

	out := make([]string, 0, len(order))
	for _, id := range order {
		out = append(out, parts[id].String())
	}

	sort.Strings(out)

	return out
}

const (
	postgresTablesSQL = `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`

	postgresColumnsSQL = `SELECT column_name, data_type, is_nullable, column_default, collation_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`

	postgresPrimaryKeySQL = `SELECT kcu.column_name, kcu.ordinal_position
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name
		 AND kcu.constraint_schema = tc.constraint_schema
		WHERE tc.table_schema = 'public' AND tc.table_name = $1
		  AND tc.constraint_type = 'PRIMARY KEY'
		ORDER BY kcu.ordinal_position`

	// information_schema has no view over indexes, so this is the one place the
	// gate reads pg_catalog. The primary key's index is excluded for the same
	// reason as on SQLite.
	// pg_get_indexdef per key column, not a join on pg_attribute: an expression
	// index holds 0 in indkey, which matches no attribute, so such a join drops
	// every row of that index and the index with it. indoption bit 0 is DESC,
	// which nothing else in the snapshot reads.
	postgresIndexesSQL = `SELECT ic.relname, i.indisunique, i.indpred IS NOT NULL,
			k.attnum = 0, pg_get_indexdef(i.indexrelid, k.ord::int, true), (k.opt & 1) = 1
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN unnest(i.indkey::int2[], i.indoption::int2[]) WITH ORDINALITY AS k(attnum, opt, ord) ON true
		WHERE n.nspname = 'public' AND c.relname = $1 AND NOT i.indisprimary
		ORDER BY ic.relname, k.ord`

	postgresForeignKeysSQL = `SELECT con.oid, rel.relname, att.attname, fatt.attname,
			con.confdeltype, con.confupdtype
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class rel ON rel.oid = con.confrelid
		JOIN unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(src, tgt, ord) ON true
		JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = k.src
		JOIN pg_attribute fatt ON fatt.attrelid = con.confrelid AND fatt.attnum = k.tgt
		WHERE n.nspname = 'public' AND c.relname = $1 AND con.contype = 'f'
		ORDER BY con.oid, k.ord`

	// contype 'c' is a CHECK. A NOT NULL is contype 'n' since Postgres 17 and
	// is compared column by column instead.
	postgresChecksSQL = `SELECT count(*)
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1 AND con.contype = 'c'`

	// tgisinternal is the trigger Postgres builds for a foreign key or a
	// deferred constraint. Those are already compared as constraints, and
	// SQLite builds none of them, so counting them here would be a difference
	// in what the engines do with one declaration.
	postgresTriggersSQL = `SELECT t.tgname
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1 AND NOT t.tgisinternal
		ORDER BY t.tgname`

	postgresViewsSQL = `SELECT table_name FROM information_schema.views
		WHERE table_schema = 'public' ORDER BY table_name`
)

func introspectPostgres(tb testing.TB, db *sql.DB) schemaSnapshot {
	tb.Helper()

	snapshot := schemaSnapshot{Tables: map[string]tableShape{}}

	var tables []string

	eachRow(tb, db, postgresTablesSQL, nil, func(rows *sql.Rows) error {
		var name string

		if err := rows.Scan(&name); err != nil {
			return err
		}

		tables = append(tables, strings.ToLower(name))

		return nil
	})

	for _, table := range tables {
		shape := tableShape{
			Columns:     postgresColumns(tb, db, table),
			PrimaryKey:  postgresPrimaryKey(tb, db, table),
			Indexes:     postgresIndexes(tb, db, table),
			ForeignKeys: postgresForeignKeys(tb, db, table),
			Triggers:    queriedNames(tb, db, postgresTriggersSQL, []any{table}),
		}

		require.NoError(tb, db.QueryRowContext(tb.Context(), postgresChecksSQL, table).Scan(&shape.Checks))

		snapshot.Tables[table] = shape
	}

	snapshot.Views = queriedNames(tb, db, postgresViewsSQL, nil)

	return snapshot
}

func postgresColumns(tb testing.TB, db *sql.DB, table string) []columnShape {
	tb.Helper()

	var columns []columnShape

	eachRow(tb, db, postgresColumnsSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			name, declared, nullable string
			dflt, collation          sql.NullString
		)

		if err := rows.Scan(&name, &declared, &nullable, &dflt, &collation); err != nil {
			return err
		}

		columns = append(columns, columnShape{
			Name:      strings.ToLower(name),
			Class:     typeClass(tb, declared),
			NotNull:   nullable == "NO",
			Default:   normaliseDefault(dflt.String),
			Collation: collation.String,
		})

		return nil
	})

	return columns
}

func postgresPrimaryKey(tb testing.TB, db *sql.DB, table string) []string {
	tb.Helper()

	var keyed []keyColumn

	eachRow(tb, db, postgresPrimaryKeySQL, []any{table}, func(rows *sql.Rows) error {
		var (
			name     string
			position int
		)

		if err := rows.Scan(&name, &position); err != nil {
			return err
		}

		keyed = append(keyed, keyColumn{position: position, name: strings.ToLower(name)})

		return nil
	})

	return orderKey(keyed)
}

func postgresIndexes(tb testing.TB, db *sql.DB, table string) map[string]indexShape {
	tb.Helper()

	indexes := map[string]indexShape{}

	eachRow(tb, db, postgresIndexesSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			name, column                            string
			unique, partial, expression, descending bool
		)

		if err := rows.Scan(&name, &unique, &partial, &expression, &column, &descending); err != nil {
			return err
		}

		if expression {
			column = indexExpression
		}

		name = strings.ToLower(name)

		shape := indexes[name]
		shape.Unique = unique
		shape.Partial = partial
		shape.Columns = append(shape.Columns, indexKey(strings.ToLower(column), descending))
		indexes[name] = shape

		return nil
	})

	return indexes
}

func postgresForeignKeys(tb testing.TB, db *sql.DB, table string) []string {
	tb.Helper()

	parts := map[int64]*foreignKeyParts{}

	var order []int64

	eachRow(tb, db, postgresForeignKeysSQL, []any{table}, func(rows *sql.Rows) error {
		var (
			oid                                      int64
			parent, from, target, onDelete, onUpdate string
		)

		if err := rows.Scan(&oid, &parent, &from, &target, &onDelete, &onUpdate); err != nil {
			return err
		}

		if _, seen := parts[oid]; !seen {
			parts[oid] = &foreignKeyParts{
				parent:   strings.ToLower(parent),
				onDelete: referentialAction(tb, onDelete),
				onUpdate: referentialAction(tb, onUpdate),
			}
			order = append(order, oid)
		}

		parts[oid].columns = append(parts[oid].columns, strings.ToLower(from))
		parts[oid].targets = append(parts[oid].targets, strings.ToLower(target))

		return nil
	})

	out := make([]string, 0, len(order))
	for _, oid := range order {
		out = append(out, parts[oid].String())
	}

	sort.Strings(out)

	return out
}

// foreignKeyParts accumulates one constraint's columns in declaration order.
type foreignKeyParts struct {
	parent   string
	columns  []string
	targets  []string
	onDelete string
	onUpdate string
}

func (f *foreignKeyParts) String() string {
	return strings.Join(f.columns, ",") + " -> " + f.parent + "(" + strings.Join(f.targets, ",") + ")" +
		" on delete " + f.onDelete + " on update " + f.onUpdate
}

// referentialActions spells a Postgres confdeltype/confupdtype code the way
// SQLite's pragma spells the same action, so a cascade on one engine and a
// restrict on the other is a difference rather than two unrelated encodings.
var referentialActions = map[string]string{
	"a": "no action",
	"r": "restrict",
	"c": "cascade",
	"n": "set null",
	"d": "set default",
}

func referentialAction(tb testing.TB, code string) string {
	tb.Helper()

	action, known := referentialActions[code]
	require.Truef(tb, known, "no referential action for the Postgres code %q", code)

	return action
}

// keyColumn is one primary key member and its position in the key.
type keyColumn struct {
	position int
	name     string
}

func orderKey(keyed []keyColumn) []string {
	sort.Slice(keyed, func(i, j int) bool { return keyed[i].position < keyed[j].position })

	out := make([]string, 0, len(keyed))
	for _, column := range keyed {
		out = append(out, column.name)
	}

	return out
}

func typeClass(tb testing.TB, declared string) string {
	tb.Helper()

	class, known := typeClasses[strings.ToLower(strings.TrimSpace(declared))]
	require.Truef(tb, known,
		"no type class for %q: add it to typeClasses rather than leaving it to compare equal to the next unknown type",
		declared)

	return class
}

func normaliseDefault(value string) string {
	return postgresCast.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "")
}

// eachRow runs a query and hands every row to scan, so the close and the
// post-loop error check are written once rather than at each of the callers.
func eachRow(tb testing.TB, db *sql.DB, query string, args []any, scan func(*sql.Rows) error) {
	tb.Helper()

	rows, err := db.QueryContext(tb.Context(), query, args...)
	require.NoError(tb, err)

	defer func() { require.NoError(tb, rows.Close()) }()

	for rows.Next() {
		require.NoError(tb, scan(rows))
	}

	require.NoError(tb, rows.Err())
}
