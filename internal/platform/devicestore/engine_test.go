package devicestore

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The records and the prints reference the device directory, so every test
// here opens the host database with the graph's schema and this package's, and
// registers a device through the graph before it writes about one.

// engine is one backend a test runs against.
type engine = authztest.Engine

// forEachEngine runs body as a parallel subtest per engine this run reaches.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()
	authztest.ForEachEngine(t, body, Migrations())
}

// benchEngines is every engine a benchmark can reach.
func benchEngines(b *testing.B) []engine {
	b.Helper()

	return authztest.Engines(b, Migrations())
}

// fixture is this package's store and the graph over one database. The record
// and print methods are the store's; the directory and the grants are the
// graph's.
type fixture struct {
	*Store

	graph *authz.Store
	db    *sql.DB
}

func (f fixture) UpsertDevices(ctx context.Context, devices ...authz.DeviceRow) error {
	return f.graph.UpsertDevices(ctx, devices...)
}

func (f fixture) RemoveDevice(ctx context.Context, id string) error {
	return f.graph.RemoveDevice(ctx, id)
}

func (f fixture) Write(ctx context.Context, tuples ...authz.Tuple) error {
	return f.graph.Write(ctx, tuples...)
}

func (f fixture) Epoch() uint64 { return f.graph.Epoch() }

// newStore is a fixture over an empty, migrated database of the test's own.
func newStore(tb testing.TB, e engine) fixture {
	tb.Helper()

	return openFixture(tb, e, e.Open(tb))
}

// openFixture is a fixture over db, which is migrated: what a process that
// starts against an existing database builds.
func openFixture(tb testing.TB, e engine, db *sql.DB) fixture {
	tb.Helper()

	graph, err := authz.Open(db, e.Dialect)
	require.NoError(tb, err)
	require.NoError(tb, graph.Reconcile(tb.Context()))

	store, err := New(db, e.Engine)
	require.NoError(tb, err)

	return fixture{Store: store, graph: graph, db: db}
}

// migrated applies the graph's schema and this package's to db, as the host
// does at a start.
func migrated(tb testing.TB, e engine, db *sql.DB) *sql.DB {
	tb.Helper()

	schema := sqlstore.Schema{Owners: []sqlstore.Migrations{authz.Migrations(), Migrations()}}

	migrator, err := sqlstore.NewMigrator(db, e.Engine, schema, "")
	require.NoError(tb, err)
	require.NoError(tb, migrator.Migrate(tb.Context()))

	return db
}

// readEpoch reads the graph's generation from its row.
func readEpoch(tb testing.TB, db *sql.DB) uint64 {
	tb.Helper()

	var epoch uint64
	require.NoError(tb, db.QueryRowContext(tb.Context(), `SELECT epoch FROM authz_epoch WHERE id = 1`).Scan(&epoch))

	return epoch
}

// mustTuple reads "object#relation@subject", the notation of the Zanzibar paper.
func mustTuple(tb testing.TB, text string) authz.Tuple {
	tb.Helper()

	objectText, rest, ok := strings.Cut(text, "#")
	require.Truef(tb, ok, "tuple %q has no #relation", text)

	relation, subjectText, ok := strings.Cut(rest, "@")
	require.Truef(tb, ok, "tuple %q has no @subject", text)

	object, err := authz.ParseObject(objectText)
	require.NoErrorf(tb, err, "tuple %q", text)

	subject, err := authz.ParseSubject(subjectText)
	require.NoErrorf(tb, err, "tuple %q", text)

	out, err := authz.NewTuple(object, authz.Relation(relation), subject)
	require.NoErrorf(tb, err, "tuple %q", text)

	return out
}

// subjectOf parses a subject, failing the test when it cannot.
func subjectOf(tb testing.TB, text string) authz.Subject {
	tb.Helper()

	out, err := authz.ParseSubject(text)
	require.NoErrorf(tb, err, "subject %q", text)

	return out
}
