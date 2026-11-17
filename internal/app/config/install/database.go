package install

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The database a declaration grants a module.

// Database is the database an operator granted a module.
type Database struct {
	// Engine is sqlite or postgres.
	Engine wirerpc.DatabaseEngine
	// DSN is the postgres:// URL of a Postgres database, with the module's own
	// role and password. It is empty for sqlite, whose file the host places in
	// the module's data directory.
	DSN string
	// Provisioned says this server made the database and its role on its own
	// Postgres server.
	Provisioned bool
}

// DatabaseEntry is the database block as an operator writes it.
type DatabaseEntry struct {
	Engine      string `json:"engine"                yaml:"engine"`
	DSN         string `json:"dsn,omitempty"         yaml:"dsn"`
	Provisioned bool   `json:"provisioned,omitempty" yaml:"provisioned"`
}

// Entry is the database in the spelling the declaration writes, and nil for
// none.
func (d *Database) Entry() *DatabaseEntry {
	if d == nil {
		return nil
	}

	return &DatabaseEntry{Engine: string(d.Engine), DSN: d.DSN, Provisioned: d.Provisioned}
}

// Server is where a Postgres database is, as host:port/name with no
// credential, and empty for sqlite or a URL that does not parse.
func (d *Database) Server() string {
	if d == nil || d.Engine != wirerpc.DatabasePostgres {
		return ""
	}

	parsed, err := url.Parse(d.DSN)
	if err != nil {
		return ""
	}

	return parsed.Host + parsed.Path
}

// Password is the password a Postgres database's URL carries, and empty for
// none. The secret sweep of a diagnostics bundle reads it.
func (d *Database) Password() string {
	if d == nil || d.Engine != wirerpc.DatabasePostgres {
		return ""
	}

	parsed, err := url.Parse(d.DSN)
	if err != nil {
		return ""
	}

	password, _ := parsed.User.Password()

	return password
}

// errDatabaseURL reports a Postgres connection string that is not a
// postgres:// URL.
var errDatabaseURL = errors.New("expected a postgres:// or postgresql:// URL")

// CheckPostgresURL refuses a connection string that is not a postgres:// or
// postgresql:// URL naming a host and a database. The URL form is the one
// the server can read its password out of, so a log line and a diagnostics
// bundle can remove it.
func CheckPostgresURL(dsn string) error {
	parsed, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil {
		return errDatabaseURL
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errDatabaseURL
	}

	if parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return fmt.Errorf("%w with a host and a database name", errDatabaseURL)
	}

	return nil
}

// databaseOf reads the database a declaration grants, and nil for none.
func databaseOf(declared *DatabaseEntry) (*Database, error) {
	if declared == nil {
		return nil, nil //nolint:nilnil // no grant is no database, and nothing went wrong.
	}

	switch wirerpc.DatabaseEngine(strings.TrimSpace(declared.Engine)) {
	case wirerpc.DatabaseSQLite:
		if declared.DSN != "" || declared.Provisioned {
			return nil, errors.New("service.database is sqlite and names a dsn or provisioned. The file is " +
				"in the module's data directory, so sqlite takes neither")
		}

		return &Database{Engine: wirerpc.DatabaseSQLite}, nil
	case wirerpc.DatabasePostgres:
		if err := CheckPostgresURL(declared.DSN); err != nil {
			return nil, fmt.Errorf("service.database.dsn: %w", err)
		}

		return &Database{
			Engine:      wirerpc.DatabasePostgres,
			DSN:         strings.TrimSpace(declared.DSN),
			Provisioned: declared.Provisioned,
		}, nil
	}

	return nil, fmt.Errorf("service.database.engine is %q, expected %q or %q",
		declared.Engine, wirerpc.DatabaseSQLite, wirerpc.DatabasePostgres)
}
