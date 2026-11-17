package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
)

// Migrations is one owner's schema: the package whose Go code reads and writes
// the tables, and the files that create them. Files holds a sqlite and a
// postgres directory with the same file names. The two engines have separate
// files, so a schema difference between them shows in a review.
//
// A new file is named by the UTC date and time it was written and a few words,
// such as 20261027T093015_device_notes.sql. The older files are numbered, such
// as 0002_job_objects.sql, and keep their names. A file is never edited or
// renamed once it has shipped. A change to the schema is a new file in the
// owner's directories.
type Migrations struct {
	// Owner names the owner in the ledger, where a file is recorded as
	// "<owner>/<file>".
	Owner string
	Files fs.FS
}

// Schema is every owner's migrations over one database. An owner whose tables
// reference another owner's comes after it.
//
// The numbered files are applied first, owner by owner in this order and by
// name within an owner. Every dated file comes after them, by its date across
// all owners, and at one date in the order of the owners. A file the database
// has not recorded is applied even
// when it sorts before one it has: two pull requests that each added one merge
// into a build that ships both.
type Schema struct {
	Owners []Migrations

	// Compat is the floor as a count of migrations, which is how builds before
	// the dated names read it: a build that ships fewer migrations refuses the
	// database. It stays where the numbered files left it. A new migration an
	// older build cannot survive names itself in Floor.
	Compat int

	// Floor names the newest migration an older build cannot survive, as the
	// ledger records it: "<owner>/<file>". A dropped column it still selects,
	// a narrowed constraint it still writes through, a table it still reads.
	// A build that does not ship that migration refuses a database this
	// schema has migrated. A migration that only adds something leaves it
	// alone. Empty sets none.
	Floor string
}

// Version is the number of migrations this schema ships for engine. It only
// grows from one build to the next, because a shipped file is never removed.
func (s Schema) Version(engine Engine) (int, error) {
	migrator := Migrator{engine: engine, schema: s}

	return migrator.Version()
}

// The ledger, the compatibility floor and the queries over them. NOT NULL beside
// PRIMARY KEY is needed on SQLite, which accepts NULL in a primary key column
// that does not say so.
const (
	// A database an older build migrated may hold one more column, version.
	// It is nullable, nothing reads it, and a row this build writes leaves it
	// empty.
	createLedgerSQLite = `CREATE TABLE IF NOT EXISTS schema_migration (
		name           TEXT NOT NULL PRIMARY KEY,
		checksum       TEXT NOT NULL,
		binary_version TEXT NOT NULL DEFAULT '',
		applied_at     TIMESTAMP NOT NULL
	)`

	createLedgerPostgres = `CREATE TABLE IF NOT EXISTS schema_migration (
		name           TEXT PRIMARY KEY,
		checksum       TEXT NOT NULL,
		binary_version TEXT NOT NULL DEFAULT '',
		applied_at     TIMESTAMPTZ NOT NULL
	)`

	selectMigrationNamesSQL = `SELECT name FROM schema_migration`

	selectMigrationsSQL = `SELECT name, checksum FROM schema_migration`

	insertMigrationSQL = `INSERT INTO schema_migration (name, checksum, binary_version, applied_at)
		VALUES (?, ?, ?, ?)`

	// The floor is a schema number and a migration name, not a version of the
	// binary. A comparison over a version string needs a build that stamps
	// one. An unstamped build reports 0.0.0-dev, which is below every floor,
	// so it would refuse a database it wrote itself. Which build applied a
	// migration is recorded in schema_migration.binary_version, for a person
	// to read.
	//
	// A database made without min_migration gains the column on its next
	// migration. It is added at the end, where the CREATE below puts it too.
	createSchemaMetaSQLite = `CREATE TABLE IF NOT EXISTS schema_meta (
		id                 INTEGER NOT NULL PRIMARY KEY,
		min_schema_version INTEGER NOT NULL,
		min_migration      TEXT NOT NULL DEFAULT ''
	)`

	createSchemaMetaPostgres = `CREATE TABLE IF NOT EXISTS schema_meta (
		id                 INTEGER PRIMARY KEY,
		min_schema_version INTEGER NOT NULL,
		min_migration      TEXT NOT NULL DEFAULT ''
	)`

	selectCompatFloorSQL = `SELECT min_schema_version, min_migration FROM schema_meta WHERE id = 1`

	// The floor only rises. A build that lowered it would tell the next older
	// one that a database it cannot read is safe to open.
	raiseCompatFloorSQL = `INSERT INTO schema_meta (id, min_schema_version) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET min_schema_version = ?
		WHERE schema_meta.min_schema_version < ?`

	raiseFloorMigrationSQL = `UPDATE schema_meta SET min_migration = ? WHERE id = 1`

	sqliteFloorMigrationColumnSQL = `SELECT COUNT(*) FROM pragma_table_info('schema_meta')
		WHERE name = 'min_migration'`

	postgresFloorMigrationColumnSQL = `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'schema_meta' AND column_name = 'min_migration'`

	addFloorMigrationColumnSQL = `ALTER TABLE schema_meta ADD COLUMN min_migration TEXT NOT NULL DEFAULT ''`

	sqliteTableExistsSQL = `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`

	postgresTableExistsSQL = `SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = ?`

	foreignKeysPragma = `PRAGMA foreign_keys`
)

// ledgerTable is the table every applied migration is recorded in.
const ledgerTable = "schema_migration"

// foreignKeyProbes is how many pooled connections the foreign key check pins at
// once. The pragma is per connection, so one connection reporting it on says
// nothing about the next one the pool hands out; holding several open forces
// distinct connections and catches a database opened without the DSN parameter
// and then patched with a single Exec, which lands on one connection only.
const foreignKeyProbes = 4

// createdTable finds the tables a migration file creates.
var createdTable = regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

// ErrForeignKeysDisabled reports a SQLite handle that does not enforce foreign
// keys. SQLite ignores them unless the connection asked for them, and a silently
// unenforced constraint is worse than none, because the tests and the schema
// both claim it holds. Open the database with _pragma=foreign_keys(1).
var ErrForeignKeysDisabled = errors.New("sqlstore: the sqlite connection does not enforce foreign keys")

// ErrMigrationChanged reports a migration whose content no longer matches the
// checksum recorded when it was applied. Without it an edited migration is
// skipped in silence, and two installations report one version over two
// schemas.
var ErrMigrationChanged = errors.New("sqlstore: an applied migration has changed")

// ErrMigrationMissing reports a ledger row naming a migration this build does
// not ship and no newer build can have written: a file renamed or deleted after
// it was applied. A newer build's row is dated and sorts after every file this
// build ships of its owner.
var ErrMigrationMissing = errors.New("sqlstore: an applied migration is not in this build")

// ErrMigrationName reports a migration file whose name is neither dated nor
// numbered, or a Schema.Floor this build does not ship.
var ErrMigrationName = errors.New("sqlstore: a migration is not named by its date")

// ErrOutsideTransaction reports a migration holding a statement that cannot run
// inside the one migration transaction, or that would end it early: a COMMIT
// of its own, CREATE INDEX CONCURRENTLY. What ran before such a COMMIT would
// survive a failed or rehearsed migration.
var ErrOutsideTransaction = errors.New("sqlstore: a migration holds a statement that cannot run in a transaction")

// ErrMigrationCarriageReturn reports a migration holding a carriage return that
// is not part of a CRLF. A bare CR does not end a -- comment on SQLite and does
// end one on Postgres, so the statement after it would run on one engine only.
var ErrMigrationCarriageReturn = errors.New("sqlstore: a migration holds a bare carriage return")

// ErrUnrecordedTable reports a table a migration would create that the database
// already holds while the ledger does not record that migration. The database
// was made by a different build, or it is not this server's.
var ErrUnrecordedTable = errors.New("sqlstore: the database holds a table its migration ledger does not record")

// ErrSchemaTooNew reports a database whose compatibility floor this build does
// not reach: a floor migration it does not ship, or a count of migrations above
// its own. There are no down migrations, so the way back from an
// upgrade is the backup taken before it.
var ErrSchemaTooNew = errors.New("sqlstore: the database was migrated by a newer build")

// ErrSchemaMismatch reports a database whose applied migrations are not the ones
// this build ships. A tool that runs beside a server checks for it before it
// migrates, so it never changes the schema under that server.
var ErrSchemaMismatch = errors.New("sqlstore: the database schema is not the one this build ships")

// ErrSchemaOlder reports a database that has not yet applied every migration
// this build ships, which is the state after an upgrade and before the new
// server has started once. It comes with ErrSchemaMismatch.
var ErrSchemaOlder = errors.New("sqlstore: the database schema is older than this build")

// ErrSchemaNewer reports a database that has applied a migration this build
// does not ship. It comes with ErrSchemaMismatch.
var ErrSchemaNewer = errors.New("sqlstore: the database schema is newer than this build")

// migration is one file of one owner, read from its owner's files.
type migration struct {
	name     string // "<owner>/<file>", as the ledger records it
	owner    string
	at       int // the owner's place in the schema
	file     moduledb.MigrationName
	content  []byte
	checksum string
}

// before reports whether a is applied before b.
func before(a, b migration) int {
	if a.file.Dated() != b.file.Dated() {
		return a.file.Compare(b.file)
	}

	// The numbered files run owner by owner.
	if !a.file.Dated() && a.at != b.at {
		return a.at - b.at
	}

	if order := a.file.Compare(b.file); order != 0 {
		return order
	}

	return a.at - b.at
}

// Migrator applies a Schema to one database.
type Migrator struct {
	db            *sql.DB
	engine        Engine
	schema        Schema
	binaryVersion string
}

// NewMigrator builds the migrator for one database. binaryVersion is recorded
// against every migration it applies, and an empty one records nothing.
func NewMigrator(db *sql.DB, engine Engine, schema Schema, binaryVersion string) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("sqlstore: the migrator needs an open database handle")
	}

	if err := engine.Check(); err != nil {
		return nil, err
	}

	return &Migrator{db: db, engine: engine, schema: schema, binaryVersion: binaryVersion}, nil
}

// Version is the number of migrations this build ships for the engine, which is
// the schema version the compatibility floor is compared against.
func (m *Migrator) Version() (int, error) {
	all, err := m.migrations()
	if err != nil {
		return 0, err
	}

	return len(all), nil
}

// Migrate applies and records every missing migration in one transaction.
// Running it on an up-to-date database changes nothing.
//
// It refuses before writing anything when the database cannot be this build's:
// a SQLite handle without foreign keys, a table the ledger does not account for,
// a floor above this build, or a ledger row this build does not ship.
func (m *Migrator) Migrate(ctx context.Context) (err error) {
	return m.migrate(ctx, true)
}

// Rehearse runs the same migration transaction and rolls it back.
func (m *Migrator) Rehearse(ctx context.Context) error {
	return m.migrate(ctx, false)
}

func (m *Migrator) migrate(ctx context.Context, commit bool) (err error) {
	all, err := m.migrations()
	if err != nil {
		return err
	}

	if err = m.requireForeignKeys(ctx); err != nil {
		return err
	}

	release, err := m.lock(ctx)
	if err != nil {
		return err
	}

	// A lock that is not given back blocks every later run, so its failure is
	// joined onto the error the run itself reported.
	defer func() { err = errors.Join(err, release()) }()

	tx, finish, err := m.begin(ctx)
	if err != nil {
		return err
	}

	finished := false
	defer func() {
		if !finished {
			err = errors.Join(err, finish(false))
		}
	}()

	if _, err = tx.ExecContext(ctx, m.pick(createLedgerSQLite, createLedgerPostgres)); err != nil {
		return fmt.Errorf("create the migration ledger: %w", err)
	}

	applied, err := m.applied(ctx, tx)
	if err != nil {
		return err
	}

	if err = m.requireShipped(applied, all); err != nil {
		return err
	}

	if err = m.requireRecordedTables(ctx, tx, applied, all); err != nil {
		return err
	}

	recorded, err := m.requireCompatFloor(ctx, tx, all)
	if err != nil {
		return err
	}

	for _, one := range all {
		if err = m.applyIfNeeded(ctx, tx, applied, one); err != nil {
			return err
		}
	}

	if err = m.raiseCompatFloor(ctx, tx, recorded, all); err != nil {
		return err
	}

	if err = finish(commit); err != nil {
		action := "roll back the migration rehearsal"
		if commit {
			action = "commit the migrations"
		}

		return fmt.Errorf("%s: %w", action, err)
	}

	finished = true

	return nil
}

type appliedMigration struct {
	checksum string
}

func (m *Migrator) applied(ctx context.Context, q queryer) (map[string]appliedMigration, error) {
	rows, err := q.QueryContext(ctx, selectMigrationsSQL)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := map[string]appliedMigration{}
	for rows.Next() {
		var (
			name     string
			recorded appliedMigration
		)

		if err = rows.Scan(&name, &recorded.checksum); err != nil {
			return nil, fmt.Errorf("read the migration ledger: %w", err)
		}

		out[name] = recorded
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}

	return out, nil
}

// requireShipped refuses a ledger row this build does not ship, unless a newer
// build wrote it. A newer build's row is dated and sorts after every file this
// build ships of the same owner, or names an owner this build does not have.
// Any other row is a file renamed or deleted after it was applied, and a
// numbered row is always one, because no build adds a numbered file.
func (m *Migrator) requireShipped(applied map[string]appliedMigration, all []migration) error {
	var missing []string

	for name := range applied {
		if slices.ContainsFunc(all, func(one migration) bool { return one.name == name }) {
			continue
		}

		if !newerBuilds(name, all) {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	slices.Sort(missing)

	return fmt.Errorf(
		"%w: %s is applied, this build ships no such file, and no newer build can have written it. "+
			"Restore the file under the name the ledger holds and put the change in a new migration",
		ErrMigrationMissing, strings.Join(missing, ", "))
}

// newerBuilds reports whether a ledger row this build does not ship can be a
// newer build's.
func newerBuilds(name string, all []migration) bool {
	owner, file, found := strings.Cut(name, "/")
	if !found {
		return false
	}

	order, err := moduledb.ParseMigrationName(file)
	if err != nil || !order.Dated() || order.Step {
		return false
	}

	return !slices.ContainsFunc(all, func(one migration) bool {
		return one.owner == owner && one.file.Compare(order) >= 0
	})
}

// RequireSchemaOfThisBuild reports ErrSchemaMismatch when the database has been
// migrated and its ledger does not name exactly the migrations this build ships,
// with ErrSchemaNewer when it names one this build does not ship and
// ErrSchemaOlder when it lacks some. A database no build has migrated passes. It
// writes nothing.
func (m *Migrator) RequireSchemaOfThisBuild(ctx context.Context) error {
	exists, err := m.tableExists(ctx, m.db, ledgerTable)
	if err != nil || !exists {
		return err
	}

	all, err := m.migrations()
	if err != nil {
		return err
	}

	applied, err := m.appliedNames(ctx, m.db)
	if err != nil {
		return err
	}

	if missing := unshipped(applied, all); len(missing) > 0 {
		return fmt.Errorf("%w: %w: the database has applied %s, which this build does not ship",
			ErrSchemaMismatch, ErrSchemaNewer, missing[0])
	}

	var pending []string

	for _, one := range all {
		if !slices.Contains(applied, one.name) {
			pending = append(pending, one.name)
		}
	}

	if len(pending) > 0 {
		return fmt.Errorf("%w: %w: the database has not applied %d of the %d migrations this build ships, "+
			"the first %s", ErrSchemaMismatch, ErrSchemaOlder, len(pending), len(all), pending[0])
	}

	return nil
}

// migrations reads every owner's files for this engine, in apply order. It
// refuses a file whose name is neither dated nor numbered, a file holding a
// statement that would escape the migration transaction, and a floor this
// build does not ship.
func (m *Migrator) migrations() ([]migration, error) {
	dir := m.pick("sqlite", "postgres")

	var out []migration

	for at, owner := range m.schema.Owners {
		entries, err := fs.ReadDir(owner.Files, dir)
		if err != nil {
			return nil, fmt.Errorf("read the %s migrations of %s: %w", dir, owner.Owner, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			one, readErr := readMigration(owner, at, dir, entry.Name())
			if readErr != nil {
				return nil, readErr
			}

			out = append(out, one)
		}
	}

	slices.SortFunc(out, before)

	if floor := m.schema.Floor; floor != "" && !slices.ContainsFunc(out, func(one migration) bool {
		return one.name == floor
	}) {
		return nil, fmt.Errorf("%w: the floor %s is not a migration this build ships", ErrMigrationName, floor)
	}

	return out, nil
}

// readMigration reads one owner's file and holds it to a name and to
// statements the migration transaction can run.
func readMigration(owner Migrations, at int, dir, name string) (migration, error) {
	file, err := moduledb.ParseMigrationName(name)
	if err != nil || file.Step {
		return migration{}, fmt.Errorf("%w: %s/%s. A new migration is named <date>T<time>_<words>.sql in UTC, "+
			"such as 20261027T093015_device_notes.sql", ErrMigrationName, owner.Owner, name)
	}

	content, err := fs.ReadFile(owner.Files, path.Join(dir, name))
	if err != nil {
		return migration{}, fmt.Errorf("read migration %s/%s: %w", owner.Owner, name, err)
	}

	checksum, err := Checksum(content)
	if err != nil {
		return migration{}, fmt.Errorf("migration %s/%s: %w", owner.Owner, name, err)
	}

	if err = moduledb.RequireTransactional(content); err != nil {
		return migration{}, fmt.Errorf("%w: %s/%s: %w", ErrOutsideTransaction, owner.Owner, name, err)
	}

	return migration{
		name: owner.Owner + "/" + name, owner: owner.Owner, at: at, file: file, content: content,
		checksum: checksum,
	}, nil
}

// requireRecordedTables refuses a database that already holds a table one of the
// unrecorded migrations would create. It reads the ledger and the catalogue in
// one transaction, and a migration commits its tables and its ledger row
// together, so a run that is migrating beside this one is seen as both or
// neither.
//
// It checks only the tables each migration creates, because another store may
// keep its own tables in the same database.
func (m *Migrator) requireRecordedTables(
	ctx context.Context, tx migrationTx, recorded map[string]appliedMigration, all []migration,
) error {
	for _, one := range all {
		if _, ok := recorded[one.name]; ok {
			continue
		}

		for _, table := range CreatedTables(one.content) {
			exists, existsErr := m.tableExists(ctx, tx, table)
			if existsErr != nil {
				return existsErr
			}

			if exists {
				return fmt.Errorf(
					"%w: the table %s, which %s creates. The database was made by a different build, or "+
						"it is not this server's database. Start with an empty database, or start the "+
						"build that made it",
					ErrUnrecordedTable, table, one.name)
			}
		}
	}

	return nil
}

// requireCompatFloor refuses a database whose floor this build does not reach,
// and answers the floor migration the database records. It runs before any
// migration, so a build that must not touch the database has not.
//
// A recorded floor migration decides alone. The count a build raises beside it
// is for the builds before the dated names, and an older build that ships the
// floor migration and fewer files than the build that raised the count is
// still one that can read the database.
func (m *Migrator) requireCompatFloor(ctx context.Context, tx migrationTx, all []migration) (string, error) {
	if _, err := tx.ExecContext(ctx, m.pick(createSchemaMetaSQLite, createSchemaMetaPostgres)); err != nil {
		return "", fmt.Errorf("create the schema metadata table: %w", err)
	}

	if err := m.addFloorMigrationColumn(ctx, tx); err != nil {
		return "", err
	}

	var (
		floor    int
		recorded string
	)

	err := tx.QueryRowContext(ctx, selectCompatFloorSQL).Scan(&floor, &recorded)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read the schema compatibility floor: %w", err)
	case recorded != "":
		if slices.ContainsFunc(all, func(one migration) bool { return one.name == recorded }) {
			return recorded, nil
		}

		return "", fmt.Errorf(
			"%w: it needs the migration %s, which this build does not ship. Start the build that migrated "+
				"it, or restore the backup taken before that upgrade, because this schema has no down migrations",
			ErrSchemaTooNew, recorded)
	case floor <= len(all):
		return "", nil
	}

	return "", fmt.Errorf(
		"%w: it needs schema version %d and this build understands %d. Start the build that "+
			"migrated it, or restore the backup taken before that upgrade, because this schema has no "+
			"down migrations",
		ErrSchemaTooNew, floor, len(all))
}

// addFloorMigrationColumn adds the min_migration column to a schema_meta table
// that was made without it.
func (m *Migrator) addFloorMigrationColumn(ctx context.Context, tx migrationTx) error {
	var columns int

	query := m.pick(sqliteFloorMigrationColumnSQL, postgresFloorMigrationColumnSQL)
	if err := tx.QueryRowContext(ctx, query).Scan(&columns); err != nil {
		return fmt.Errorf("inspect the schema compatibility floor: %w", err)
	}

	if columns > 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, addFloorMigrationColumnSQL); err != nil {
		return fmt.Errorf("add the floor migration to the schema compatibility floor: %w", err)
	}

	return nil
}

// raiseCompatFloor records what this schema needs once it has been applied. It
// never lowers the count it finds, and moves the floor migration only to one
// applied after the recorded one.
//
// A build that sets a floor migration also raises the count to every file it
// ships. A build from before the dated names reads only the count, ships fewer
// files than that, and so refuses the database as it must.
func (m *Migrator) raiseCompatFloor(ctx context.Context, tx migrationTx, recorded string, all []migration) error {
	floor := m.schema.Compat
	if m.raisesFloorMigration(recorded, all) {
		floor = max(floor, len(all))
	}

	if _, err := tx.ExecContext(ctx, m.engine.Q(raiseCompatFloorSQL), floor, floor, floor); err != nil {
		return fmt.Errorf("record the schema compatibility floor: %w", err)
	}

	if !m.raisesFloorMigration(recorded, all) {
		return nil
	}

	if _, err := tx.ExecContext(ctx, m.engine.Q(raiseFloorMigrationSQL), m.schema.Floor); err != nil {
		return fmt.Errorf("record the floor migration: %w", err)
	}

	return nil
}

// raisesFloorMigration reports whether this schema's floor migration replaces
// the recorded one: there is none, or it is applied before this one.
func (m *Migrator) raisesFloorMigration(recorded string, all []migration) bool {
	own := m.schema.Floor
	if own == "" || own == recorded {
		return false
	}

	if recorded == "" {
		return true
	}

	at := func(name string) int {
		return slices.IndexFunc(all, func(one migration) bool { return one.name == name })
	}

	return at(recorded) < at(own)
}

// applyIfNeeded applies a migration the ledger has not recorded and refuses one
// whose content has moved since it was applied.
//
// The ledger is read inside the transaction that writes it. Reading it outside
// is a check-then-act across two transactions: two processes starting together
// would both read a migration as unapplied, and the second would run it against
// the schema the first one built.
func (m *Migrator) applyIfNeeded(
	ctx context.Context, tx migrationTx, applied map[string]appliedMigration, one migration,
) error {
	if recorded, ok := applied[one.name]; ok {
		if recorded.checksum == one.checksum {
			return nil
		}

		return fmt.Errorf(
			"%w: %s (applied %s, shipped %s). Restore the file to the applied version and put the "+
				"change in a new migration, because a database that has run this one will not run it again",
			ErrMigrationChanged, one.name, recorded.checksum, one.checksum)
	}

	// The file carries several statements and no parameters, which is the one
	// shape both drivers execute in a single call.
	if _, err := tx.ExecContext(ctx, string(one.content)); err != nil {
		return fmt.Errorf("apply migration %s: %w", one.name, err)
	}

	if _, err := tx.ExecContext(ctx, m.engine.Q(insertMigrationSQL),
		one.name, one.checksum, m.binaryVersion, time.Now().UTC()); err != nil {
		return fmt.Errorf("record migration %s: %w", one.name, err)
	}

	return nil
}

// requireForeignKeys refuses a SQLite handle that would not enforce foreign
// keys. Postgres enforces them unconditionally. This proves the pragma on the
// connections it can reach; only the DSN makes sure of every connection the pool
// will ever open.
func (m *Migrator) requireForeignKeys(ctx context.Context) error {
	if m.engine != EngineSQLite {
		return nil
	}

	probes := foreignKeyProbes
	if limit := m.db.Stats().MaxOpenConnections; limit > 0 && limit < probes {
		probes = limit
	}

	conns := make([]*sql.Conn, 0, probes)

	// Held to the end of the check, not returned one at a time: the pool would
	// hand the same connection back on every probe and prove nothing.
	defer func() {
		for _, conn := range conns {
			_ = conn.Close() // returning a connection to the pool reports nothing worth acting on
		}
	}()

	for range probes {
		conn, err := m.db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("open a connection: %w", err)
		}

		conns = append(conns, conn)

		var enabled int
		if err = conn.QueryRowContext(ctx, foreignKeysPragma).Scan(&enabled); err != nil {
			return fmt.Errorf("read the foreign_keys pragma: %w", err)
		}

		if enabled == 0 {
			return ErrForeignKeysDisabled
		}
	}

	return nil
}

// queryer is what a *sql.DB and a *sql.Tx share.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (m *Migrator) appliedNames(ctx context.Context, q queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, selectMigrationNamesSQL)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}

	defer func() { _ = rows.Close() }() // the scan loop below reports what this would

	var names []string

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read the migration ledger: %w", err)
		}

		names = append(names, name)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}

	return names, nil
}

func (m *Migrator) tableExists(ctx context.Context, q queryer, table string) (bool, error) {
	var count int

	query := m.engine.Q(m.pick(sqliteTableExistsSQL, postgresTableExistsSQL))
	if err := q.QueryRowContext(ctx, query, table).Scan(&count); err != nil {
		return false, fmt.Errorf("look for the table %s: %w", table, err)
	}

	return count > 0, nil
}

// pick is the SQLite or the Postgres spelling of one thing.
func (m *Migrator) pick(sqlite, postgres string) string {
	if m.engine == EnginePostgres {
		return postgres
	}

	return sqlite
}

// unshipped is the applied names no shipped migration carries.
func unshipped(applied []string, shipped []migration) []string {
	var out []string

	for _, name := range applied {
		if !slices.ContainsFunc(shipped, func(one migration) bool { return one.name == name }) {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

// Checksum is sha256 over a migration with CRLF folded to LF, and it refuses a
// bare carriage return. go:embed hashes the bytes of the working copy, and a
// copy saved with CRLF holds the same migration, so hashing the raw bytes would
// make the ledger disagree with itself across two checkouts of one commit.
func Checksum(content []byte) (string, error) {
	// One rule for the host's migrations and a module's own, so the two
	// ledgers agree on what a file is.
	sum, err := moduledb.Checksum(content)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrMigrationCarriageReturn, err)
	}

	return sum, nil
}

// CreatedTables is the names of the tables a migration file creates, in the
// order it creates them.
func CreatedTables(content []byte) []string {
	var out []string

	for _, match := range createdTable.FindAllSubmatch(content, -1) {
		out = append(out, strings.ToLower(string(match[1])))
	}

	return out
}
