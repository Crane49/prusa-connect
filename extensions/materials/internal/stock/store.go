// Package stock is the module's own database: the catalogue, the custom
// materials and favourites, and the spools in stock.
package stock

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Store reads and writes the tables migrations/ makes, on the engine the
// operator granted. The statements are the same on both engines: both read
// $1. Only a time differs: SQLite keeps it as text, Postgres as a timestamp.
type Store struct {
	db     *sql.DB
	engine wirerpc.DatabaseEngine

	// betweenBatches runs after each staging batch of an import; tests hold
	// the import there.
	betweenBatches func()
	// changes hears every committed change, nil for nobody.
	changes Changes
}

// New wraps an open, migrated database.
func New(db *sql.DB, engine wirerpc.DatabaseEngine) *Store {
	return &Store{db: db, engine: engine}
}

// textTime keeps every fraction digit, so SQLite orders the text as time.
// RFC3339Nano drops trailing zeros, and "…05Z" would sort after "…05.5Z".
const textTime = "2006-01-02T15:04:05.000000000Z07:00"

// at renders a time for one statement.
func (s *Store) at(when time.Time) any {
	if s.engine == wirerpc.DatabasePostgres {
		return when.UTC()
	}

	return when.UTC().Format(textTime)
}

// atOrNil renders a time that may be missing, as at does, and nil as NULL.
func (s *Store) atOrNil(when *time.Time) any {
	if when == nil {
		return nil
	}

	return s.at(*when)
}

// SQLITE_CONSTRAINT_UNIQUE, and Postgres's unique_violation.
const (
	sqliteUnique   = 2067
	postgresUnique = "23505"
)

// unique is true for a write a unique index refused, on either engine.
func unique(err error) bool {
	if lite, ok := errors.AsType[*sqlite.Error](err); ok {
		return lite.Code() == sqliteUnique
	}

	pg, ok := errors.AsType[*pq.Error](err)

	return ok && pg.Code == postgresUnique
}

// SQLite's primary code for a row a constraint refused.
const sqliteConstraint = 19

// Refused is true for a write the engine refused for what it was sent: a
// number its column cannot hold, which only Postgres's 32-bit integer
// meets, or a row a constraint refuses, such as a spool at a location
// deleted meanwhile. That is SQLSTATE class 22 or 23 on Postgres.
func Refused(err error) bool {
	if lite, ok := errors.AsType[*sqlite.Error](err); ok {
		return lite.Code()&0xff == sqliteConstraint
	}

	pg, ok := errors.AsType[*pq.Error](err)

	return ok && (pg.Code.Class() == "22" || pg.Code.Class() == "23")
}

// parseTime reads a time column: a time.Time on Postgres, text on SQLite.
func parseTime(v any) (time.Time, error) {
	switch v := v.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return time.Parse(time.RFC3339Nano, v)
	case []byte:
		return time.Parse(time.RFC3339Nano, string(v))
	}

	return time.Time{}, fmt.Errorf("unexpected time column %T", v)
}
