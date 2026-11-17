package engine

import (
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
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
)

// ErrMigrationChanged reports a file whose bytes are not the bytes that were
// applied to this database. The file is not applied again. Never edit a
// migration after it shipped.
var ErrMigrationChanged = errors.New("a migration file differs from the one this database applied")

// ErrMigrationMissing reports a ledger row naming a migration this build does
// not ship: a file renamed or deleted after it was applied, or a database a
// newer build migrated.
var ErrMigrationMissing = errors.New("an applied migration is not in this build")

// ErrSchemaTooNew reports a database whose compatibility floor this build
// does not reach: a version above the highest numbered migration it ships, or
// a floor migration it does not ship.
var ErrSchemaTooNew = errors.New("the database was migrated by a newer build")

// ErrMigrationName reports a migration whose name is neither dated nor
// numbered, two numbered migrations of one version, or a floor this build does
// not ship.
var ErrMigrationName = errors.New("a migration is not named by its date or version")

// ErrOutsideTransaction reports a migration with a statement that cannot run
// inside the store's one migration transaction, or that would end it.
var ErrOutsideTransaction = errors.New("a migration holds a statement that cannot run in a transaction")

// ErrUnrecordedTable reports a table a migration would create that already
// exists while the ledger does not record that migration: a database a
// different build made, or one that is not this store's.
var ErrUnrecordedTable = errors.New("the database holds a table its migration ledger does not record")

// Ledger is one store's migrations and the table it records them in. Each
// store has its own ledger, because on PostgreSQL several stores may share one
// database with the host.
type Ledger struct {
	// Store names the store in every error, such as "histstore".
	Store string
	// Table is the ledger table.
	Table string
	// Files has a sqlite and a postgres directory with the same file names.
	// They are applied in moduledb.MigrationName.Compare order: numbered
	// files first, then dated ones by date.
	Files fs.FS
	// Postgres is whether the database is PostgreSQL.
	Postgres bool
	// Compat is the oldest numbered migration version that may open the
	// schema after this build has migrated it.
	Compat int
	// Floor names the newest migration an older build cannot survive. A build
	// that does not ship it refuses the schema. Empty sets none.
	Floor string
}

// createdTable finds the tables a migration file creates.
var createdTable = regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

// Migrate brings the store's schema up to what this binary ships, in one
// transaction. There is no down migration. To go back, restore a backup and
// start the older binary.
func Migrate(ctx context.Context, db *sql.DB, ledger Ledger) error {
	return migrate(ctx, db, ledger, true)
}

// Rehearse runs the same migration transaction and rolls it back.
func Rehearse(ctx context.Context, db *sql.DB, ledger Ledger) error {
	return migrate(ctx, db, ledger, false)
}

func migrate(ctx context.Context, db *sql.DB, ledger Ledger, commit bool) (err error) {
	files, err := ledger.files()
	if err != nil {
		return err
	}

	tx, finish, err := ledger.begin(ctx, db)
	if err != nil {
		return err
	}

	finished := false

	defer func() {
		if !finished {
			err = errors.Join(err, finish(false))
		}
	}()

	applied, err := ledger.applied(ctx, tx)
	if err != nil {
		return err
	}

	if err = ledger.requireShipped(applied, files); err != nil {
		return err
	}

	if err = ledger.requireRecordedTables(ctx, tx, applied, files); err != nil {
		return err
	}

	columns := `(name TEXT NOT NULL PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMP NOT NULL)`
	if ledger.Postgres {
		columns = `(name TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL)`
	}

	// The table name is the store's own constant and never a caller's value.
	create := `CREATE TABLE IF NOT EXISTS ` + ledger.Table + ` ` + columns

	if _, err = tx.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("%s: create the migration ledger: %w", ledger.Store, err)
	}

	recorded, err := ledger.prepareFloor(ctx, tx, files)
	if err != nil {
		return err
	}

	for _, one := range files {
		if err = ledger.apply(ctx, tx, applied, one.Name); err != nil {
			return err
		}
	}

	if err = ledger.raiseFloor(ctx, tx, recorded, files); err != nil {
		return err
	}

	if err = finish(commit); err != nil {
		action := "roll back the migration rehearsal"
		if commit {
			action = "commit the migrations"
		}

		return fmt.Errorf("%s: %s: %w", ledger.Store, action, err)
	}

	finished = true

	return nil
}

// dir is this engine's directory.
func (l Ledger) dir() string {
	if l.Postgres {
		return "postgres"
	}

	return "sqlite"
}

// render rewrites a statement's '?' placeholders for the engine.
func (l Ledger) render(query string) string {
	if l.Postgres {
		return Numbered(query)
	}

	return query
}

// files is this engine's migrations, in the order they are applied. It also
// validates their names, their statements and the floor.
func (l Ledger) files() ([]moduledb.MigrationName, error) {
	entries, err := fs.ReadDir(l.Files, l.dir())
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", l.Store, l.dir(), err)
	}

	var names []moduledb.MigrationName

	versions := map[int]string{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name, nameErr := moduledb.ParseMigrationName(entry.Name())
		if nameErr != nil || name.Step {
			return nil, fmt.Errorf("%s: %w: %s, expected <date>T<time>_<words>.sql", l.Store, ErrMigrationName,
				entry.Name())
		}

		if other, found := versions[name.Number]; found && name.Number > 0 {
			return nil, fmt.Errorf("%s: %w: %s and %s are both version %d",
				l.Store, ErrMigrationName, other, name.Name, name.Number)
		}

		versions[name.Number] = name.Name

		if err = l.requireTransactional(name.Name); err != nil {
			return nil, err
		}

		names = append(names, name)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("%s: %s holds no migration", l.Store, l.dir())
	}

	slices.SortFunc(names, moduledb.MigrationName.Compare)

	if l.Floor != "" && !slices.ContainsFunc(names, func(one moduledb.MigrationName) bool {
		return one.Name == l.Floor
	}) {
		return nil, fmt.Errorf("%s: %w: the floor %s is not a migration this build ships",
			l.Store, ErrMigrationName, l.Floor)
	}

	return names, nil
}

// requireTransactional refuses a file with a statement that would leave the
// one transaction, such as a COMMIT.
func (l Ledger) requireTransactional(name string) error {
	body, err := fs.ReadFile(l.Files, path.Join(l.dir(), name))
	if err != nil {
		return fmt.Errorf("%s: read the migration %s: %w", l.Store, name, err)
	}

	if err = moduledb.RequireTransactional(body); err != nil {
		return fmt.Errorf("%s: %w: %s: %w", l.Store, ErrOutsideTransaction, name, err)
	}

	return nil
}

// applied is every file the ledger records, by name, with its checksum.
func (l Ledger) applied(ctx context.Context, db migrationTx) (map[string]string, error) {
	out := map[string]string{}

	exists, err := l.tableExists(ctx, db, l.Table)
	if err != nil || !exists {
		return out, err
	}

	// The table name is the store's own constant and never a caller's value.
	rows, err := db.QueryContext(ctx, `SELECT name, checksum FROM `+l.Table)
	if err != nil {
		return nil, fmt.Errorf("%s: read the migration ledger: %w", l.Store, err)
	}

	defer func() { _ = rows.Close() }() // the scan loop below reports what this would

	for rows.Next() {
		var name, checksum string
		if err = rows.Scan(&name, &checksum); err != nil {
			return nil, fmt.Errorf("%s: read the migration ledger: %w", l.Store, err)
		}

		out[name] = checksum
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: read the migration ledger: %w", l.Store, err)
	}

	return out, nil
}

// requireShipped refuses a ledger row this build does not ship. A dated row
// that sorts after every shipped file is allowed: a newer build applied it,
// and the floor decides whether this build may open the schema.
func (l Ledger) requireShipped(applied map[string]string, files []moduledb.MigrationName) error {
	last := files[len(files)-1]

	var missing []string

	for name := range applied {
		if slices.ContainsFunc(files, func(one moduledb.MigrationName) bool { return one.Name == name }) {
			continue
		}

		order, err := moduledb.ParseMigrationName(name)
		if err != nil || !order.Dated() || order.Compare(last) <= 0 {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	slices.Sort(missing)

	return fmt.Errorf("%s: %w: %s. Restore the migration under that name",
		l.Store, ErrMigrationMissing, strings.Join(missing, ", "))
}

// requireRecordedTables refuses a table an unrecorded migration would create.
// It checks only this store's tables, because on PostgreSQL the database may
// be shared with the host.
func (l Ledger) requireRecordedTables(
	ctx context.Context, db migrationTx, applied map[string]string, files []moduledb.MigrationName,
) error {
	for _, one := range files {
		name := one.Name
		if _, recorded := applied[name]; recorded {
			continue
		}

		body, err := fs.ReadFile(l.Files, path.Join(l.dir(), name))
		if err != nil {
			return fmt.Errorf("%s: read the migration %s: %w", l.Store, name, err)
		}

		for _, match := range createdTable.FindAllSubmatch(body, -1) {
			table := strings.ToLower(string(match[1]))

			exists, existsErr := l.tableExists(ctx, db, table)
			if existsErr != nil {
				return existsErr
			}

			if exists {
				return fmt.Errorf(
					"%s: %w: the table %s, which %s creates. The database was made by a different build, "+
						"or it is not this store's database. Start with an empty one, or start the build that "+
						"made it", l.Store, ErrUnrecordedTable, table, name)
			}
		}
	}

	return nil
}

// apply runs one migration and writes its ledger row in the same transaction.
func (l Ledger) apply(ctx context.Context, db migrationTx, applied map[string]string, name string) error {
	body, err := fs.ReadFile(l.Files, path.Join(l.dir(), name))
	if err != nil {
		return fmt.Errorf("%s: read the migration %s: %w", l.Store, name, err)
	}

	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])

	switch recorded, ok := applied[name]; {
	case ok && recorded == checksum:
		return nil
	case ok:
		return fmt.Errorf("%s: %w: %s was applied as %s and this build ships %s",
			l.Store, ErrMigrationChanged, name, recorded, checksum)
	}

	if _, err = db.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("%s: apply the migration %s: %w", l.Store, name, err)
	}

	insert := `INSERT INTO ` + l.Table + ` (name, checksum, applied_at) VALUES (?, ?, ?)`
	if _, err = db.ExecContext(ctx, l.render(insert), name, checksum, time.Now().UTC()); err != nil {
		return fmt.Errorf("%s: record the migration %s: %w", l.Store, name, err)
	}

	return nil
}

// prepareFloor creates the floor table and refuses a schema whose floor this
// build does not reach. It returns the floor migration the schema records.
func (l Ledger) prepareFloor(ctx context.Context, db migrationTx, files []moduledb.MigrationName) (string, error) {
	highest := 0
	for _, one := range files {
		highest = max(highest, one.Number)
	}

	if l.Compat < 0 || l.Compat > highest {
		return "", fmt.Errorf("%s: compatibility floor %d is outside the shipped schema 0..%d",
			l.Store, l.Compat, highest)
	}

	columns := `(id INTEGER NOT NULL PRIMARY KEY, min_schema_version INTEGER NOT NULL,
		min_migration TEXT NOT NULL DEFAULT '')`
	if l.Postgres {
		columns = `(id INTEGER PRIMARY KEY, min_schema_version INTEGER NOT NULL,
			min_migration TEXT NOT NULL DEFAULT '')`
	}

	create := `CREATE TABLE IF NOT EXISTS ` + l.metaTable() + ` ` + columns
	if _, err := db.ExecContext(ctx, create); err != nil {
		return "", fmt.Errorf("%s: create the schema metadata: %w", l.Store, err)
	}

	if err := l.addFloorMigration(ctx, db); err != nil {
		return "", err
	}

	var (
		floor    int
		recorded string
	)

	query := `SELECT min_schema_version, min_migration FROM ` + l.metaTable() + ` WHERE id = 1`
	err := db.QueryRowContext(ctx, query).Scan(&floor, &recorded)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("%s: read the compatibility floor: %w", l.Store, err)
	case recorded != "":
		// A recorded floor migration overrides the numbered floor.
		if slices.ContainsFunc(files, func(one moduledb.MigrationName) bool { return one.Name == recorded }) {
			return recorded, nil
		}

		return "", fmt.Errorf("%s: %w: it needs the migration %s, which this build does not ship",
			l.Store, ErrSchemaTooNew, recorded)
	case floor <= highest:
		return "", nil
	}

	return "", fmt.Errorf("%s: %w: it needs schema version %d and this build ships through %d",
		l.Store, ErrSchemaTooNew, floor, highest)
}

// addFloorMigration adds the min_migration column to a floor table that does
// not have it.
func (l Ledger) addFloorMigration(ctx context.Context, db migrationTx) error {
	query := `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'min_migration'`
	if l.Postgres {
		query = `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'min_migration'`
	}

	var found int
	if err := db.QueryRowContext(ctx, l.render(query), l.metaTable()).Scan(&found); err != nil {
		return fmt.Errorf("%s: inspect the compatibility floor: %w", l.Store, err)
	}

	if found > 0 {
		return nil
	}

	// The table name is the store's own constant and never a caller's value.
	add := `ALTER TABLE ` + l.metaTable() + ` ADD COLUMN min_migration TEXT NOT NULL DEFAULT ''`
	if _, err := db.ExecContext(ctx, add); err != nil {
		return fmt.Errorf("%s: add the floor migration column: %w", l.Store, err)
	}

	return nil
}

// raiseFloor records this build's floor. The numbered floor only rises, and
// the floor migration only moves to one that sorts after the recorded one.
func (l Ledger) raiseFloor(ctx context.Context, db migrationTx, recorded string, files []moduledb.MigrationName) error {
	query := `INSERT INTO ` + l.metaTable() + ` (id, min_schema_version) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET min_schema_version = ?
		WHERE ` + l.metaTable() + `.min_schema_version < ?`

	if _, err := db.ExecContext(ctx, l.render(query), l.Compat, l.Compat, l.Compat); err != nil {
		return fmt.Errorf("%s: record the compatibility floor: %w", l.Store, err)
	}

	if l.Floor == "" || l.Floor == recorded {
		return nil
	}

	at := func(name string) int {
		return slices.IndexFunc(files, func(one moduledb.MigrationName) bool { return one.Name == name })
	}

	if recorded != "" && at(recorded) > at(l.Floor) {
		return nil
	}

	update := `UPDATE ` + l.metaTable() + ` SET min_migration = ? WHERE id = 1`
	if _, err := db.ExecContext(ctx, l.render(update), l.Floor); err != nil {
		return fmt.Errorf("%s: record the floor migration: %w", l.Store, err)
	}

	return nil
}

func (l Ledger) metaTable() string {
	return strings.TrimSuffix(l.Table, "_migration") + "_schema"
}

func (l Ledger) tableExists(ctx context.Context, db migrationTx, table string) (bool, error) {
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
	if l.Postgres {
		query = `SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = ?`
	}

	var count int
	if err := db.QueryRowContext(ctx, l.render(query), table).Scan(&count); err != nil {
		return false, fmt.Errorf("%s: look for the table %s: %w", l.Store, table, err)
	}

	return count > 0, nil
}
