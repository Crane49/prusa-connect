package moduledb_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
)

// A database/sql driver that knows the statements Migrate sends and nothing
// else. This module imports no real driver (ADR-20260824-gfr), so the cases here prove
// how Migrate orders and bounds its work: one transaction per upgrade, kept
// whole or not at all. examples/database-module runs the same questions on
// real SQLite and Postgres.

const fakeDriverName = "moduledb-fake"

//nolint:gochecknoglobals // the driver registry is process-wide, and so are its databases.
var (
	registerFake sync.Once
	fakeMu       sync.Mutex
	fakes        = map[string]*fakeDatabase{}
)

// fakeState is what a database holds: the ledger, the floor and every
// statement that was not one of Migrate's own, in the order it ran.
type fakeState struct {
	ledger    map[string]fakeRow
	floor     int64
	floorName string
	ran       []string
}

// fakeRow is one ledger row.
type fakeRow struct {
	version  int64
	checksum string
}

func (s fakeState) clone() fakeState {
	return fakeState{
		ledger: maps.Clone(s.ledger), floor: s.floor, floorName: s.floorName, ran: slices.Clone(s.ran),
	}
}

// fakeDatabase is one database, shared by every connection opened on its
// name. Its lock is held from BEGIN to COMMIT, as an exclusive write lock is.
type fakeDatabase struct {
	writer    sync.Mutex
	mu        sync.Mutex
	committed fakeState
	begun     int
}

// newFake registers a fresh database and opens a handle on it.
func newFake(t *testing.T) (*sql.DB, *fakeDatabase) {
	t.Helper()

	registerFake.Do(func() { sql.Register(fakeDriverName, fakeDriver{}) })

	held := &fakeDatabase{committed: fakeState{ledger: map[string]fakeRow{}}}

	fakeMu.Lock()
	fakes[t.Name()] = held
	fakeMu.Unlock()

	db, err := sql.Open(fakeDriverName, t.Name())
	if err != nil {
		t.Fatalf("open the fake: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db, held
}

func (d *fakeDatabase) state() fakeState {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.committed.clone()
}

type fakeDriver struct{}

func (fakeDriver) Open(name string) (driver.Conn, error) {
	fakeMu.Lock()
	defer fakeMu.Unlock()

	held, ok := fakes[name]
	if !ok {
		return nil, fmt.Errorf("no fake database %q", name)
	}

	return &fakeConn{db: held}, nil
}

// fakeConn is one connection, with the state a transaction has written and
// not committed.
type fakeConn struct {
	db      *fakeDatabase
	pending *fakeState
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("the fake prepares nothing")
}
func (c *fakeConn) Close() error { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.begin()

	return fakeTx{conn: c}, nil
}

func (c *fakeConn) begin() {
	c.db.writer.Lock()
	c.db.mu.Lock()
	c.db.begun++
	state := c.db.committed.clone()
	c.db.mu.Unlock()
	c.pending = &state
}

func (c *fakeConn) end(commit bool) {
	if c.pending == nil {
		return
	}

	if commit {
		c.db.mu.Lock()
		c.db.committed = *c.pending
		c.db.mu.Unlock()
	}

	c.pending = nil
	c.db.writer.Unlock()
}

type fakeTx struct{ conn *fakeConn }

func (t fakeTx) Commit() error   { t.conn.end(true); return nil }
func (t fakeTx) Rollback() error { t.conn.end(false); return nil }

// write is the state a statement changes: the transaction's, or the
// committed one for a statement outside a transaction.
func (c *fakeConn) write(change func(*fakeState)) {
	if c.pending != nil {
		change(c.pending)

		return
	}

	c.db.mu.Lock()
	defer c.db.mu.Unlock()

	change(&c.db.committed)
}

func (c *fakeConn) read() fakeState {
	if c.pending != nil {
		return c.pending.clone()
	}

	return c.db.state()
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	trimmed := strings.TrimSpace(query)

	switch {
	case trimmed == "BEGIN IMMEDIATE":
		c.begin()
	case trimmed == "COMMIT":
		c.end(true)
	case trimmed == "ROLLBACK":
		c.end(false)
	case strings.HasPrefix(trimmed, "CREATE TABLE IF NOT EXISTS module_"),
		strings.Contains(trimmed, "pg_advisory_xact_lock"):
	case strings.HasPrefix(trimmed, "INSERT INTO module_migration"):
		name, _ := args[0].Value.(string)
		version, _ := args[1].Value.(int64)
		checksum, _ := args[2].Value.(string)
		c.write(func(s *fakeState) { s.ledger[name] = fakeRow{version: version, checksum: checksum} })
	case strings.HasPrefix(trimmed, "INSERT INTO module_schema"):
		floor, _ := args[0].Value.(int64)
		c.write(func(s *fakeState) { s.floor = max(s.floor, floor) })
	case strings.HasPrefix(trimmed, "UPDATE module_schema SET min_migration"):
		name, _ := args[0].Value.(string)
		c.write(func(s *fakeState) { s.floorName = name })
	case strings.Contains(trimmed, "FAIL"):
		return nil, errors.New("the statement asked to fail")
	default:
		c.write(func(s *fakeState) { s.ran = append(s.ran, trimmed) })
	}

	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	state := c.read()

	switch {
	case strings.HasPrefix(query, "SELECT COUNT(*)") && strings.Contains(query, "min_migration"):
		// The fake's floor has had the floor migration column from the start.
		return &fakeRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(1)}}}, nil
	case strings.HasPrefix(query, "SELECT min_schema_version, min_migration"):
		columns := []string{"min_schema_version", "min_migration"}
		if state.floor == 0 && state.floorName == "" {
			return &fakeRows{columns: columns}, nil
		}

		return &fakeRows{columns: columns, rows: [][]driver.Value{{state.floor, state.floorName}}}, nil
	case strings.HasPrefix(query, "SELECT name, version, checksum"):
		out := &fakeRows{columns: []string{"name", "version", "checksum"}}

		for _, name := range slices.Sorted(maps.Keys(state.ledger)) {
			row := state.ledger[name]
			out.rows = append(out.rows, []driver.Value{name, row.version, row.checksum})
		}

		return out, nil
	}

	return nil, fmt.Errorf("the fake reads nothing for %q", query)
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	at      int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.at >= len(r.rows) {
		return io.EOF
	}

	copy(dest, r.rows[r.at])
	r.at++

	return nil
}
