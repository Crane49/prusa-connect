package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// The merge that landed openGraph resolved the engine selection and the binary
// version stamp by hand across two branches, and no test in this package has
// ever run openGraph with persistence.type set to postgres. These two are that
// path: the first needs no driver and holds the selection, the second needs one
// and holds the whole start-up.

// postgresDSNEnv points this package's Postgres case at a server. It is the
// same variable internal/authz reads, spelled again here because that package
// declares it in a test file, which no other package can see.
const postgresDSNEnv = "AUTHZ_TEST_POSTGRES_DSN"

// postgresDriverLinked reports whether this build carries the Postgres driver.
// Every case that runs on both engines asks this before it starts a deployment
// on the server engine.
//
// The probe closes the handle it opened. database/sql starts a goroutine for
// every pool and stops it only on Close. A discarded probe therefore outlives
// its case, and the leak check in TestMain fails a run that otherwise passed.
func postgresDriverLinked(t *testing.T, dsn string) bool {
	t.Helper()

	probe, err := authz.OpenPostgres(dsn)
	if errors.Is(err, authz.ErrPostgresUnlinked) {
		return false
	}

	require.NoError(t, err)
	require.NoError(t, probe.Close())

	return true
}

// TestADeploymentNamingPostgresDoesNotOpenTheSqliteFile is the selection half.
// core.authz.database is still validated and still set on a Postgres
// deployment, so the failure to guard against is a build that reports Postgres
// and quietly serves its grants out of a SQLite file nobody meant to create.
//
// It also holds the message. openDatabase returns one printable name and every
// refusal in openGraph is wrapped with it, so a Postgres failure must name the
// DSN and never the file path. It needs no driver and no server: a build with
// neither refuses at OpenPostgres, and one with the driver refuses at the first
// query, which is Migrate, and both are wrapped with the same name.
func TestADeploymentNamingPostgresDoesNotOpenTheSqliteFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "authz.db")

	core := config.Default()
	core.Overlay = "overlay.json"
	core.Modules = "modules"
	core.Authz.Database = path
	core.Persistence.Type = config.EnginePostgres
	core.Persistence.Postgres.DSN = "postgres://someone:secret@127.0.0.1:1/connect?sslmode=disable"

	require.NoError(t, core.Validate(), "the configuration under test is not one a deployment could write")

	_, _, err := openGraph(t.Context(), core, "0.0.0-test", discard())
	require.Error(t, err, "a build with no postgres driver opened the graph anyway")

	require.NotContains(t, err.Error(), path, "a postgres failure named the sqlite file")
	require.NotContains(t, err.Error(), "secret", "the refusal printed the database password")
	require.Contains(t, err.Error(), "127.0.0.1:1", "the refusal does not say which database it is about")

	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"a deployment naming postgres created the sqlite graph beside it")
}

// TestADeploymentNamingPostgresMigratesAndStampsTheBuild is the whole start-up
// against a real server: the engine the configuration names is opened, the
// schema is brought up, the vocabulary is written in, the first grant is
// written, and the ledger records which build applied each migration.
//
// It runs only in a build carrying the driver, and only with a DSN, which is
// the invocation `make test-engines` uses.
func TestADeploymentNamingPostgresMigratesAndStampsTheBuild(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set", postgresDSNEnv)
	}

	if !postgresDriverLinked(t, dsn) {
		t.Skip("this build carries no postgres driver")
	}

	const version = "9.9.9-review"

	core := config.Default()
	core.Overlay = "overlay.json"
	core.Modules = "modules"
	core.Authz.Database = filepath.Join(t.TempDir(), "unused", "authz.db")
	core.Persistence.Type = config.EnginePostgres
	core.Persistence.Postgres.DSN = scratchDatabase(t, dsn)

	require.NoError(t, core.Validate())

	host, closeGraph, err := openGraph(t.Context(), core, version, discard())
	require.NoError(t, err)

	t.Cleanup(closeGraph)

	// The graph is usable, which is migrate and reconcile and bootstrap having
	// all run against the engine the configuration named.
	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	held, err := holdsAResourceKind(t.Context(), host.graph, owner)
	require.NoError(t, err)
	require.True(t, held, "the first grant was not written on postgres")

	// The stamp, read from the ledger with a handle of this test's own so that
	// nothing under test reports on itself.
	db, err := sql.Open("postgres", core.Persistence.Postgres.DSN)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	rows, err := db.QueryContext(t.Context(),
		"SELECT name, binary_version FROM schema_migration ORDER BY name")
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	var applied int

	for rows.Next() {
		var name, stamped string

		require.NoError(t, rows.Scan(&name, &stamped))
		require.Equalf(t, version, stamped, "%s records no build", name)

		applied++
	}

	require.NoError(t, rows.Err())
	require.Positive(t, applied, "no migration ran, so the stamp proves nothing")

	// The sqlite path the configuration still carries was never touched.
	_, statErr := os.Stat(core.Authz.Database)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// scratchDatabase creates an empty database on the server the DSN names and
// returns a DSN pointing at it, dropped with the test.
func scratchDatabase(t *testing.T, dsn string) string {
	t.Helper()

	name := "app_open_" + strings.ToLower(rand.Text())[:12]

	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)

	// The identifier is generated here and carries no caller value, and CREATE
	// DATABASE takes no parameter.
	_, err = admin.ExecContext(t.Context(), `CREATE DATABASE "`+name+`"`)
	require.NoError(t, err)

	t.Cleanup(func() {
		// The test's context is already cancelled by cleanup time.
		_, dropErr := admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close())
	})

	parsed, err := url.Parse(dsn)
	require.NoErrorf(t, err, "%s must be a postgres:// URL", postgresDSNEnv)

	parsed.Path = "/" + name

	return parsed.String()
}
