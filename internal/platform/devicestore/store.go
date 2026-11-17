package devicestore

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// maxPageLimit bounds one page of prints. A caller asking for more gets this,
// not an error, so a fleet-wide export pages instead of allocating.
const maxPageLimit = 1000

// selectDeviceSQL asks the device directory whether a device exists. A refused
// write is explained with the answer.
const selectDeviceSQL = `SELECT kind FROM authz_device WHERE device_id = ?`

// Store reads and writes the device records and the prints. It owns no
// connection lifecycle: the host opens the database and keeps the handle.
type Store struct {
	db     *sql.DB
	engine sqlstore.Engine
}

// New returns the store over the host's database, whose schema the host has
// migrated.
func New(db *sql.DB, engine sqlstore.Engine) (*Store, error) {
	if db == nil {
		return nil, errors.New("devicestore: the store needs an open database handle")
	}

	if err := engine.Check(); err != nil {
		return nil, err
	}

	return &Store{db: db, engine: engine}, nil
}

// q renders a statement for the engine this deployment runs on.
func (s *Store) q(query string) string { return s.engine.Q(query) }

// failure names what the store was doing when the database failed.
func failure(what string, err error) error { return fmt.Errorf("devicestore: %s: %w", what, err) }
