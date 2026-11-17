// Package histstore is the history provider: the optional store that keeps
// metrics and events. It is the reference implementation of sdk/historyapi on
// SQLite and PostgreSQL.
//
// It stores samples and facts. Telemetry becomes samples, with the state path
// as the metric name. Record changes become facts.
//
// On SQLite the store has its own file, because SQLite has one writer per
// database and sample inserts would block the host's writes. On PostgreSQL the
// tables live in the deployment's database with their own migration ledger.
package histstore

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"

	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// Dialect is the engine this store renders its SQL for.
type Dialect uint8

// The engines the store runs on. Every statement runs on both.
const (
	// DialectSQLite is the default.
	DialectSQLite Dialect = iota
	// DialectPostgres is PostgreSQL.
	DialectPostgres
)

// render rewrites a statement's '?' placeholders for PostgreSQL. A statement
// must contain no literal '?'.
func (d Dialect) render(query string) string {
	if d != DialectPostgres {
		return query
	}

	return engine.Numbered(query)
}

// seriesCacheLimit bounds the interning cache. Past it a new series costs a
// round trip per append, not memory.
const seriesCacheLimit = 8192

// Store is the history provider over one database handle. Close closes the
// handle.
type Store struct {
	db      *sql.DB
	dialect Dialect

	// series caches the id of each (ref, metric) pair.
	series sync.Map
	// keepRaw is how long a raw row is kept. Zero keeps everything.
	keepRaw time.Duration

	// interned counts the entries of series. It is an estimate: two
	// goroutines may both pass the limit check and go one entry over.
	interned atomic.Int64
}

var _ historyapi.Provider = (*Store)(nil)

// Option is one setting given to New.
type Option func(*Store)

// WithRawRetention sets how long a raw row is kept. Zero, the default, keeps
// everything.
func WithRawRetention(keep time.Duration) Option {
	return func(s *Store) { s.keepRaw = keep }
}

// New returns the history store over an open handle. It runs no statement: call
// Migrate before anything else.
func New(db *sql.DB, dialect Dialect, options ...Option) (*Store, error) {
	if db == nil {
		return nil, errors.New("histstore: the store is opened on a database handle")
	}

	if dialect != DialectSQLite && dialect != DialectPostgres {
		return nil, fmt.Errorf("histstore: %d is not an engine this store renders SQL for", dialect)
	}

	store := &Store{db: db, dialect: dialect}

	for _, option := range options {
		option(store)
	}

	return store, nil
}

// Caps is what this provider serves: samples, facts and commands, and
// retention when a raw retention is configured. It does not downsample or
// aggregate. QuerySamples returns raw rows only.
func (s *Store) Caps() historyapi.Caps {
	caps := historyapi.CapSamples | historyapi.CapFacts | historyapi.CapCommands

	// Declare CapRetention only when the store expires rows.
	if s.keepRaw > 0 {
		caps |= historyapi.CapRetention
	}

	return caps
}

// Close closes the handle this store opened.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("histstore: close the store: %w", err)
	}

	return nil
}
