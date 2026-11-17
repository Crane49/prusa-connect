package moduledb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The driver names the two common drivers register under.
const (
	// DefaultSQLiteDriver is the name modernc.org/sqlite registers.
	DefaultSQLiteDriver = "sqlite"
	// DefaultPostgresDriver is the name github.com/lib/pq registers.
	DefaultPostgresDriver = "postgres"
)

// ErrEngine reports a grant whose engine this package does not know.
var ErrEngine = errors.New("moduledb: unknown database engine")

// ErrSQLitePath reports a SQLite grant that is not an absolute file path.
var ErrSQLitePath = errors.New("moduledb: the sqlite database is not an absolute file path")

// Schema is a module's migrations, its compat floor and the drivers it
// registered.
type Schema struct {
	// Migrations is migrations/sqlite/*.sql and migrations/postgres/*.sql,
	// usually an embed.FS. See MigrationName for the names and the order
	// they are applied in.
	Migrations fs.FS
	// Steps are the migrations written in Go, ordered among the files by
	// their date or version. A step runs after the file of its own date or
	// version, in the same transaction.
	Steps []Step
	// Compat is the oldest numbered schema version that may open a database
	// this release migrated. Zero sets no floor. A schema of dated
	// migrations sets Floor instead.
	Compat int
	// Floor names the newest migration an older release cannot work with,
	// such as one that drops a column. A release that does not ship it
	// refuses the database. It must be a migration this release ships.
	Floor string
	// SQLiteDriver is the database/sql driver name for SQLite. Empty is
	// DefaultSQLiteDriver.
	SQLiteDriver string
	// PostgresDriver is the database/sql driver name for Postgres. Empty is
	// DefaultPostgresDriver.
	PostgresDriver string
}

// sqlitePragmas are set on every SQLite connection.
const sqlitePragmas = "PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000; PRAGMA synchronous = NORMAL"

// Open opens the granted database, migrates it and returns the handle with a
// report of what the migration did. The caller closes the handle. On a
// failure the database stays as it was. A SQLite handle has one connection,
// and a module that reads a lot may raise that with SetMaxOpenConns.
func Open(ctx context.Context, grant wirerpc.DatabaseGrant, schema Schema) (*sql.DB, Report, error) {
	var (
		db  *sql.DB
		err error
	)

	switch grant.Engine {
	case wirerpc.DatabaseSQLite:
		db, err = openSQLite(ctx, grant.DSN, orDefault(schema.SQLiteDriver, DefaultSQLiteDriver))
	case wirerpc.DatabasePostgres:
		db, err = openPostgres(ctx, grant.DSN, orDefault(schema.PostgresDriver, DefaultPostgresDriver))
	default:
		return nil, Report{}, fmt.Errorf("%w: %q, expected %q or %q",
			ErrEngine, grant.Engine, wirerpc.DatabaseSQLite, wirerpc.DatabasePostgres)
	}

	if err != nil {
		return nil, Report{}, err
	}

	report, err := Migrate(ctx, db, grant.Engine, schema)
	if err != nil {
		_ = db.Close() // the migration failure is the one worth reporting

		return nil, Report{}, err
	}

	return db, report, nil
}

func orDefault(name, fallback string) string {
	if name == "" {
		return fallback
	}

	return name
}

func openSQLite(ctx context.Context, path, driverName string) (*sql.DB, error) {
	if !filepath.IsAbs(path) || strings.Contains(path, "?") {
		return nil, fmt.Errorf("%w: %q", ErrSQLitePath, path)
	}

	if err := ownFile(path); err != nil {
		return nil, fmt.Errorf("moduledb: open the sqlite database %s: %w", path, err)
	}

	probe, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("moduledb: open the sqlite driver %q: %w", driverName, err)
	}

	base := probe.Driver()
	_ = probe.Close() // it opened no connection, it was only asked for its driver

	var inner driver.Connector = dsnConnector{driver: base, dsn: path}

	if withContext, ok := base.(driver.DriverContext); ok {
		if inner, err = withContext.OpenConnector(path); err != nil {
			return nil, fmt.Errorf("moduledb: open the sqlite database %s: %w", path, err)
		}
	}

	db := sql.OpenDB(pragmaConnector{inner: inner})
	db.SetMaxOpenConns(1)

	if err = walMode(ctx, db); err != nil {
		_ = db.Close() // the failure being returned is the one worth reporting

		return nil, fmt.Errorf("moduledb: open the sqlite database %s: %w", path, err)
	}

	return db, nil
}

// databaseMode is the mode of a module's SQLite file: only the module's
// process reads and writes it.
const databaseMode = 0o600

// ownFile makes the database file the module's alone, creating it empty when
// it is not there yet, so it never exists with a mode other users may read.
func ownFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, databaseMode)
	if err != nil {
		return err //nolint:wrapcheck // the caller wraps it.
	}

	if err = file.Close(); err != nil {
		return err //nolint:wrapcheck // the caller wraps it.
	}

	return os.Chmod(path, databaseMode) //nolint:wrapcheck // the caller wraps it.
}

// The bound on retrying the switch to WAL while another process does the
// same. SQLite answers busy at once there, without waiting for busy_timeout.
const (
	walAttempts = 50
	walPause    = 100 * time.Millisecond
)

// walMode switches the file to write-ahead logging, which lets the module
// read while it writes. A file already in that mode is left alone.
func walMode(ctx context.Context, db *sql.DB) error {
	var err error

	for range walAttempts {
		var mode string
		if err = db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err == nil {
			return nil
		}

		if !busy(err) {
			return err //nolint:wrapcheck // the caller wraps it.
		}

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // the caller wraps it.
		case <-time.After(walPause):
		}
	}

	return err
}

// busy reports a SQLite error that waiting can clear. The driver is the
// module's choice, so its error is read by its words and not by its type.
func busy(err error) bool {
	text := strings.ToLower(err.Error())

	return strings.Contains(text, "locked") || strings.Contains(text, "busy")
}

func openPostgres(ctx context.Context, dsn, driverName string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("moduledb: open the postgres driver %q: %w", driverName, err)
	}

	if err = db.PingContext(ctx); err != nil {
		_ = db.Close() // the failure being returned is the one worth reporting

		grant := wirerpc.DatabaseGrant{Engine: wirerpc.DatabasePostgres, DSN: dsn}

		return nil, fmt.Errorf("moduledb: reach %s: %w", grant, err)
	}

	return db, nil
}

// dsnConnector is a driver.Connector for a driver that has no OpenConnector.
type dsnConnector struct {
	driver driver.Driver
	dsn    string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn) //nolint:wrapcheck // database/sql reports it with its own context.
}

func (c dsnConnector) Driver() driver.Driver { return c.driver }

// pragmaConnector sets sqlitePragmas on every connection it opens, whichever
// driver is underneath.
type pragmaConnector struct {
	inner driver.Connector
}

func (c pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err //nolint:wrapcheck // database/sql reports it with its own context.
	}

	if err = execOn(ctx, conn, sqlitePragmas); err != nil {
		_ = conn.Close() // the pragma failure is the one worth reporting

		return nil, fmt.Errorf("moduledb: set the sqlite pragmas: %w", err)
	}

	return conn, nil
}

func (c pragmaConnector) Driver() driver.Driver { return c.inner.Driver() }

// execOn runs a statement with no arguments on a raw driver connection.
func execOn(ctx context.Context, conn driver.Conn, query string) error {
	if execer, ok := conn.(driver.ExecerContext); ok {
		_, err := execer.ExecContext(ctx, query, nil)
		if !errors.Is(err, driver.ErrSkip) {
			return err //nolint:wrapcheck // the caller wraps it.
		}
	}

	// A driver may take one statement per Prepare.
	for statement := range strings.SplitSeq(query, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}

		prepared, err := conn.Prepare(statement)
		if err != nil {
			return err //nolint:wrapcheck // the caller wraps it.
		}

		//nolint:staticcheck // the fallback for a driver without ExecerContext.
		_, err = prepared.Exec(nil)
		if closeErr := prepared.Close(); err == nil {
			err = closeErr
		}

		if err != nil {
			return err //nolint:wrapcheck // the caller wraps it.
		}
	}

	return nil
}
