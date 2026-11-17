package moduledb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// ErrMigrationChanged reports a migration whose content differs from the one
// the database applied. Put the change in a new migration instead.
var ErrMigrationChanged = errors.New("moduledb: an applied migration has changed")

// ErrMigrationMissing reports a migration the database applied that this
// release does not ship, although the release ships a migration that sorts
// after it: a file or a step renamed or deleted.
var ErrMigrationMissing = errors.New("moduledb: an applied migration is not in this release")

// ErrSchemaTooNew reports a database whose compat floor this release does not
// reach: a floor above the highest version it ships, or a floor migration it
// does not ship.
var ErrSchemaTooNew = errors.New("moduledb: the database was migrated by a newer release")

// ErrMigrationName reports a migration file or a step whose name is neither
// dated nor numbered, a number or a date two migrations of one kind share, or
// a Schema.Floor this release does not ship.
var ErrMigrationName = errors.New("moduledb: a migration is not named by its date or version")

// ErrMigrationCarriageReturn reports a migration with a carriage return that
// is not part of a CRLF. Such a file would run differently on SQLite and on
// Postgres.
var ErrMigrationCarriageReturn = errors.New("moduledb: a migration holds a bare carriage return")

// ErrOutsideTransaction reports a migration with a statement that cannot run
// inside the upgrade's transaction, such as CREATE INDEX CONCURRENTLY, VACUUM,
// or a COMMIT of its own.
var ErrOutsideTransaction = errors.New("moduledb: a migration holds a statement that cannot run in a transaction")

// ErrMigrationFailed reports a migration that failed. Nothing of the upgrade
// was kept.
var ErrMigrationFailed = errors.New("moduledb: a migration failed, and the database is as it was before")

// migrationLockKey is the Postgres advisory lock taken while migrating. It
// must never change, because two releases may migrate one database at once.
const migrationLockKey int64 = 0x70636C5F6D6F6475

// Tx is the transaction a migration step runs in. *sql.Tx and *sql.Conn
// satisfy it.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Step is a migration written in Go, for a change SQL cannot express. It runs
// in the same transaction as every other migration of the upgrade, after the
// SQL file of the same date or version. A step sets Stamp or Version, never
// both. A new step sets Stamp.
type Step struct {
	// Stamp orders the step among dated files, in StampLayout, such as
	// 20261027T093015. The step is recorded in the ledger as
	// <stamp>_<name>.go.
	Stamp string
	// Version orders the step among numbered files, whose version is the
	// number their name starts with. The step is recorded in the ledger as
	// <version>_<name>.go, the version written with four digits.
	Version int
	// Name is lower case letters, digits and underscores. Renaming a step
	// that was applied is refused, as renaming a file is.
	Name string
	// Up changes the data. engine says which SQL dialect tx speaks. An error
	// rolls back the whole upgrade.
	Up func(ctx context.Context, tx Tx, engine wirerpc.DatabaseEngine) error
}

// Report is what one Open or Migrate found and did.
type Report struct {
	// Version is the schema version of the database afterwards: the highest
	// numbered version applied to it. A dated migration does not change it.
	Version int
	// Compat is the compat floor the database records afterwards, as a
	// numbered version.
	Compat int
	// Floor is the floor migration the database records afterwards, and
	// empty for none. See Schema.Floor.
	Floor string
	// Applied is every migration this call applied, in order, by its ledger
	// name. It is empty when the database was up to date.
	Applied []string
}

// State is the report in the shape plugin.configure answers with.
func (r Report) State() *wirerpc.DatabaseState {
	return &wirerpc.DatabaseState{Version: r.Version, Compat: r.Compat, Applied: r.Applied}
}

// statements is one engine's spelling of the queries this file runs.
type statements struct {
	dir, ledger, meta, ledgerRows, insert, floor, raise, raiseName, hasFloorName, addFloorName, lock string
}

// The floor migration is a column added after the numbered floor, so a
// database a release before it made gains the column on its next upgrade.
const (
	selectFloorSQL    = `SELECT min_schema_version, min_migration FROM module_schema WHERE id = 1`
	addFloorNameSQL   = `ALTER TABLE module_schema ADD COLUMN min_migration TEXT NOT NULL DEFAULT ''`
	sqliteHasFloorSQL = `SELECT COUNT(*) FROM pragma_table_info('module_schema') WHERE name = 'min_migration'`
	postgresFloorSQL  = `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'module_schema' AND column_name = 'min_migration'`
)

//nolint:gochecknoglobals // two closed tables read at run time, never written.
var (
	sqliteStatements = statements{
		dir: "migrations/sqlite",
		ledger: `CREATE TABLE IF NOT EXISTS module_migration (
			name       TEXT NOT NULL PRIMARY KEY,
			version    INTEGER NOT NULL,
			checksum   TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL)`,
		meta: `CREATE TABLE IF NOT EXISTS module_schema (
			id                 INTEGER NOT NULL PRIMARY KEY,
			min_schema_version INTEGER NOT NULL,
			min_migration      TEXT NOT NULL DEFAULT '')`,
		ledgerRows: `SELECT name, version, checksum FROM module_migration`,
		insert:     `INSERT INTO module_migration (name, version, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		floor:      selectFloorSQL,
		raise: `INSERT INTO module_schema (id, min_schema_version) VALUES (1, ?)
			ON CONFLICT (id) DO UPDATE SET min_schema_version = excluded.min_schema_version
			WHERE module_schema.min_schema_version < excluded.min_schema_version`,
		raiseName:    `UPDATE module_schema SET min_migration = ? WHERE id = 1`,
		hasFloorName: sqliteHasFloorSQL,
		addFloorName: addFloorNameSQL,
	}

	postgresStatements = statements{
		dir: "migrations/postgres",
		ledger: `CREATE TABLE IF NOT EXISTS module_migration (
			name       TEXT PRIMARY KEY,
			version    INTEGER NOT NULL,
			checksum   TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL)`,
		meta: `CREATE TABLE IF NOT EXISTS module_schema (
			id                 INTEGER PRIMARY KEY,
			min_schema_version INTEGER NOT NULL,
			min_migration      TEXT NOT NULL DEFAULT '')`,
		ledgerRows: `SELECT name, version, checksum FROM module_migration`,
		insert:     `INSERT INTO module_migration (name, version, checksum, applied_at) VALUES ($1, $2, $3, $4)`,
		floor:      selectFloorSQL,
		raise: `INSERT INTO module_schema (id, min_schema_version) VALUES (1, $1)
			ON CONFLICT (id) DO UPDATE SET min_schema_version = excluded.min_schema_version
			WHERE module_schema.min_schema_version < excluded.min_schema_version`,
		raiseName:    `UPDATE module_schema SET min_migration = $1 WHERE id = 1`,
		hasFloorName: postgresFloorSQL,
		addFloorName: addFloorNameSQL,
		lock:         `SELECT pg_advisory_xact_lock($1)`,
	}
)

// migration is one shipped migration: a SQL file or a Go step.
type migration struct {
	order    MigrationName
	name     string
	checksum string
	content  string
	step     *Step
}

// applied is one row of the ledger.
type applied struct {
	version  int
	checksum string
}

// floors is what the database records about which releases may open it.
type floors struct {
	// version is the numbered floor.
	version int
	// migration is the floor migration, and empty for none.
	migration string
}

// Migrate applies every migration of schema the database has not recorded,
// in one transaction, and raises its compat floor. On a failure the database
// stays as it was. An unrecorded migration is applied even when it sorts
// before a recorded one. A database lock keeps two processes from migrating
// at once.
func Migrate(ctx context.Context, db *sql.DB, engine wirerpc.DatabaseEngine, schema Schema) (Report, error) {
	spelled, err := statementsOf(engine)
	if err != nil {
		return Report{}, err
	}

	shipped, err := shippedMigrations(schema, spelled.dir)
	if err != nil {
		return Report{}, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("moduledb: open a connection to migrate on: %w", err)
	}

	defer func() { _ = conn.Close() }() // returning a connection to the pool reports nothing worth acting on

	tx, finish, err := begin(ctx, conn, engine, spelled)
	if err != nil {
		return Report{}, err
	}

	report, err := migrateIn(ctx, tx, engine, spelled, schema, shipped)
	if err != nil {
		return Report{}, errors.Join(err, finish(false))
	}

	if err = finish(true); err != nil {
		return Report{}, fmt.Errorf("%w: commit: %w", ErrMigrationFailed, err)
	}

	return report, nil
}

// begin opens the one transaction an upgrade runs in, with the lock that
// keeps a second migration out, and returns what ends it.
func begin(
	ctx context.Context, conn *sql.Conn, engine wirerpc.DatabaseEngine, spelled statements,
) (Tx, func(commit bool) error, error) {
	if engine == wirerpc.DatabaseSQLite {
		// BEGIN IMMEDIATE takes the write lock at once, so a second migration
		// waits for this one.
		if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return nil, nil, fmt.Errorf("moduledb: begin the migration: %w", err)
		}

		return conn, func(commit bool) error {
			statement := "ROLLBACK"
			if commit {
				statement = "COMMIT"
			}

			if _, err := conn.ExecContext(context.WithoutCancel(ctx), statement); err != nil {
				return fmt.Errorf("moduledb: %s the migration: %w", strings.ToLower(statement), err)
			}

			return nil
		}, nil
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("moduledb: begin the migration: %w", err)
	}

	if _, err = tx.ExecContext(ctx, spelled.lock, migrationLockKey); err != nil {
		_ = tx.Rollback() // the lock failure is the one worth reporting

		return nil, nil, fmt.Errorf("moduledb: take the migration lock: %w", err)
	}

	return tx, func(commit bool) error {
		if commit {
			return tx.Commit() //nolint:wrapcheck // the caller wraps it.
		}

		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return fmt.Errorf("moduledb: roll the migration back: %w", rollbackErr)
		}

		return nil
	}, nil
}

// migrateIn is Migrate's work inside the transaction.
func migrateIn(
	ctx context.Context, tx Tx, engine wirerpc.DatabaseEngine, spelled statements, schema Schema,
	shipped []migration,
) (Report, error) {
	if err := createTables(ctx, tx, spelled); err != nil {
		return Report{}, err
	}

	floor, err := readFloor(ctx, tx, spelled)
	if err != nil {
		return Report{}, err
	}

	if err = requireFloor(floor, shipped); err != nil {
		return Report{}, err
	}

	ledger, err := readLedger(ctx, tx, spelled)
	if err != nil {
		return Report{}, err
	}

	held, err := checkLedger(ledger, shipped)
	if err != nil {
		return Report{}, err
	}

	report := Report{Version: held, Compat: max(floor.version, schema.Compat), Floor: floor.migration}

	for _, one := range shipped {
		if _, done := ledger[one.name]; done {
			continue
		}

		if err = run(ctx, tx, engine, one); err != nil {
			return Report{}, err
		}

		if _, err = tx.ExecContext(ctx, spelled.insert, one.name, one.order.Number, one.checksum,
			time.Now().UTC()); err != nil {
			return Report{}, fmt.Errorf("%w: record %s: %w", ErrMigrationFailed, one.name, err)
		}

		report.Applied = append(report.Applied, one.name)
		report.Version = max(report.Version, one.order.Number)
	}

	if _, err = tx.ExecContext(ctx, spelled.raise, schema.Compat); err != nil {
		return Report{}, fmt.Errorf("%w: record the compat floor: %w", ErrMigrationFailed, err)
	}

	if raised := raisedFloor(floor.migration, schema.Floor, shipped); raised != floor.migration {
		if _, err = tx.ExecContext(ctx, spelled.raiseName, raised); err != nil {
			return Report{}, fmt.Errorf("%w: record the floor migration: %w", ErrMigrationFailed, err)
		}

		report.Floor = raised
	}

	return report, nil
}

// createTables creates the ledger and the floor, and gives a floor a release
// before the floor migration made the column it lacks.
func createTables(ctx context.Context, tx Tx, spelled statements) error {
	for _, create := range []string{spelled.meta, spelled.ledger} {
		if _, err := tx.ExecContext(ctx, create); err != nil {
			return fmt.Errorf("moduledb: create the migration tables: %w", err)
		}
	}

	var columns int
	if err := tx.QueryRowContext(ctx, spelled.hasFloorName).Scan(&columns); err != nil {
		return fmt.Errorf("moduledb: inspect the compat floor: %w", err)
	}

	if columns == 0 {
		if _, err := tx.ExecContext(ctx, spelled.addFloorName); err != nil {
			return fmt.Errorf("moduledb: add the floor migration to the compat floor: %w", err)
		}
	}

	return nil
}

// requireFloor refuses a database whose floor this release does not reach.
// A recorded floor migration decides alone, without the numbered floor.
func requireFloor(floor floors, shipped []migration) error {
	if floor.migration != "" {
		if slices.ContainsFunc(shipped, func(one migration) bool { return one.name == floor.migration }) {
			return nil
		}

		return fmt.Errorf("%w: it needs the migration %s, which this release does not ship. Install the "+
			"release that migrated it, or restore the backup taken before that upgrade",
			ErrSchemaTooNew, floor.migration)
	}

	if highest := highestNumber(shipped); floor.version > highest {
		return fmt.Errorf("%w: it needs schema version %d and this release ships up to version %d. "+
			"Install the release that migrated it, or restore the backup taken before that upgrade",
			ErrSchemaTooNew, floor.version, highest)
	}

	return nil
}

// raisedFloor is the floor migration after this release: its own when the
// database records none or an earlier one, and the recorded one otherwise.
func raisedFloor(recorded, own string, shipped []migration) string {
	if own == "" || recorded == own {
		return recorded
	}

	if recorded == "" {
		return own
	}

	at := func(name string) int {
		return slices.IndexFunc(shipped, func(one migration) bool { return one.name == name })
	}

	if at(recorded) < at(own) {
		return own
	}

	return recorded
}

// highestNumber is the highest numbered version among shipped, and zero for
// none.
func highestNumber(shipped []migration) int {
	highest := 0
	for _, one := range shipped {
		highest = max(highest, one.order.Number)
	}

	return highest
}

// run applies one migration on the transaction.
func run(ctx context.Context, tx Tx, engine wirerpc.DatabaseEngine, one migration) error {
	if one.step != nil {
		if err := one.step.Up(ctx, tx, engine); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrMigrationFailed, one.name, err)
		}

		return nil
	}

	if _, err := tx.ExecContext(ctx, one.content); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMigrationFailed, one.name, err)
	}

	return nil
}

// checkLedger checks the ledger against the release and returns the highest
// numbered version in the database. An unshipped row that sorts before a
// shipped migration is an error. One that sorts after all of them is allowed.
func checkLedger(ledger map[string]applied, shipped []migration) (int, error) {
	held := 0

	var missing []string

	for name, row := range ledger {
		held = max(held, row.version)

		index := slices.IndexFunc(shipped, func(one migration) bool { return one.name == name })
		if index < 0 {
			if !afterEvery(name, shipped) {
				missing = append(missing, name)
			}

			continue
		}

		if shipped[index].checksum != row.checksum {
			return 0, fmt.Errorf("%w: %s. Restore it as it was applied and put the change in a new migration",
				ErrMigrationChanged, name)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		return 0, fmt.Errorf("%w: %s. Restore it under that name and put the change in a new migration",
			ErrMigrationMissing, strings.Join(missing, ", "))
	}

	return held, nil
}

// afterEvery reports whether a ledger name sorts after every shipped
// migration. A name that does not parse sorts nowhere and is not.
func afterEvery(name string, shipped []migration) bool {
	order, err := ParseMigrationName(name)
	if err != nil {
		return false
	}

	return len(shipped) == 0 || order.Compare(shipped[len(shipped)-1].order) > 0
}

func statementsOf(engine wirerpc.DatabaseEngine) (statements, error) {
	switch engine {
	case wirerpc.DatabaseSQLite:
		return sqliteStatements, nil
	case wirerpc.DatabasePostgres:
		return postgresStatements, nil
	}

	return statements{}, fmt.Errorf("%w: %q", ErrEngine, engine)
}

// stepName is what a step's name may be.
var stepName = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)

// shippedMigrations is one engine's files and the steps, in apply order, with
// the schema's floor migration held to one of them.
func shippedMigrations(schema Schema, dir string) ([]migration, error) {
	files, err := readMigrations(schema.Migrations, dir)
	if err != nil {
		return nil, err
	}

	stepped := map[string]bool{}

	for _, step := range schema.Steps {
		name, stepErr := stepLedgerName(step)
		if stepErr != nil {
			return nil, stepErr
		}

		order, stepErr := ParseMigrationName(name)
		if stepErr != nil {
			return nil, stepErr
		}

		at := order.Stamp + strconv.Itoa(order.Number)
		if stepped[at] {
			return nil, fmt.Errorf("%w: two steps share the date or version of %s", ErrMigrationName, name)
		}

		stepped[at] = true
		sum := sha256.Sum256([]byte("step:" + name))

		files = append(files, migration{order: order, name: name, checksum: hex.EncodeToString(sum[:]), step: &step})
	}

	slices.SortStableFunc(files, func(a, b migration) int { return a.order.Compare(b.order) })

	if schema.Floor != "" && !slices.ContainsFunc(files, func(one migration) bool { return one.name == schema.Floor }) {
		return nil, fmt.Errorf("%w: the floor %s is not a migration this release ships", ErrMigrationName,
			schema.Floor)
	}

	return files, nil
}

// stepLedgerName is the name a step is recorded under.
func stepLedgerName(step Step) (string, error) {
	if !stepName.MatchString(step.Name) || step.Up == nil || (step.Stamp == "") == (step.Version < 1) {
		return "", fmt.Errorf("%w: the step %q. A step has a Stamp or a Version from 1, not both, a name of "+
			"lower case letters, digits and underscores, and an Up function", ErrMigrationName, step.Name)
	}

	if step.Stamp != "" {
		return step.Stamp + "_" + step.Name + ".go", nil
	}

	return fmt.Sprintf("%04d_%s.go", step.Version, step.Name), nil
}

// readMigrations reads one engine's files.
func readMigrations(files fs.FS, dir string) ([]migration, error) {
	if files == nil {
		return nil, fmt.Errorf("moduledb: the schema names no migrations, expected %s/*.sql", dir)
	}

	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, fmt.Errorf("moduledb: read %s: %w", dir, err)
	}

	var out []migration

	versions := map[int]string{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		one, readErr := readMigration(files, dir, entry.Name())
		if readErr != nil {
			return nil, readErr
		}

		// A date is to the second, and the words after it tell two of one
		// second apart. A number has no such second part.
		if number := one.order.Number; number > 0 {
			if other, taken := versions[number]; taken {
				return nil, fmt.Errorf("%w: %s and %s are both version %d", ErrMigrationName, other, one.name, number)
			}

			versions[number] = one.name
		}

		out = append(out, one)
	}

	return out, nil
}

// readMigration reads one file and holds it to a dated or numbered name and
// to statements that run in a transaction.
func readMigration(files fs.FS, dir, name string) (migration, error) {
	order, err := ParseMigrationName(name)
	if err != nil {
		return migration{}, fmt.Errorf("%s: %w", dir, err)
	}

	content, err := fs.ReadFile(files, path.Join(dir, name))
	if err != nil {
		return migration{}, fmt.Errorf("moduledb: read %s: %w", name, err)
	}

	sum, err := Checksum(content)
	if err != nil {
		return migration{}, fmt.Errorf("moduledb: %s: %w", name, err)
	}

	if err = RequireTransactional(content); err != nil {
		return migration{}, fmt.Errorf("moduledb: %s: %w", name, err)
	}

	return migration{order: order, name: name, content: string(content), checksum: sum}, nil
}

// Checksum is the sha256 of a migration with CRLF read as LF, so Windows line
// endings give the same checksum. A bare carriage return is refused with
// ErrMigrationCarriageReturn.
func Checksum(content []byte) (string, error) {
	for at, b := range content {
		if b == '\r' && (at+1 == len(content) || content[at+1] != '\n') {
			return "", fmt.Errorf("%w at byte %d", ErrMigrationCarriageReturn, at)
		}
	}

	sum := sha256.Sum256(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")))

	return hex.EncodeToString(sum[:]), nil
}

func readFloor(ctx context.Context, tx Tx, spelled statements) (floors, error) {
	var floor floors

	err := tx.QueryRowContext(ctx, spelled.floor).Scan(&floor.version, &floor.migration)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return floors{}, nil
	case err != nil:
		return floors{}, fmt.Errorf("moduledb: read the compat floor: %w", err)
	}

	return floor, nil
}

func readLedger(ctx context.Context, tx Tx, spelled statements) (map[string]applied, error) {
	rows, err := tx.QueryContext(ctx, spelled.ledgerRows)
	if err != nil {
		return nil, fmt.Errorf("moduledb: read the migration ledger: %w", err)
	}

	defer func() { _ = rows.Close() }() // rows.Err below reports what this would

	out := map[string]applied{}

	for rows.Next() {
		var (
			name string
			row  applied
		)

		if err = rows.Scan(&name, &row.version, &row.checksum); err != nil {
			return nil, fmt.Errorf("moduledb: read the migration ledger: %w", err)
		}

		out[name] = row
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("moduledb: read the migration ledger: %w", err)
	}

	return out, nil
}
