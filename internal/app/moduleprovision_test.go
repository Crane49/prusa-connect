package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// The server makes a module's database on a real Postgres server, connecting
// as a role that may create roles and databases and is not a superuser
// (ADR-20261021-bqc rules 11 to 14). It runs where the fixture is: make test-engines.
func TestTheServerMakesAModuleDatabaseOnItsPostgresServer(t *testing.T) {
	t.Parallel()

	server := provisioningServer(t)
	admin := limitedAdmin(t, server)
	postgres := config.PostgresEngine{DSN: "postgres://nobody@127.0.0.1:1/none", ModuleAdminDSN: admin}

	suffix := strings.ToLower(rand.Text())[:8]
	instance := mustModuleInstance(t, "notes-"+suffix)
	name, err := databaseNameOf(postgres, instance)
	require.NoError(t, err)

	t.Cleanup(func() { dropModuleDatabase(server, name) })

	made, err := provisionDatabase(t.Context(), postgres, instance, "com.github.acme.notes/notes")
	require.NoError(t, err)
	require.True(t, made.Provisioned)
	require.Equal(t, "module_none_notes_"+suffix, name, "the core's database is in the name")
	require.Equal(t, name, strings.TrimPrefix(mustURL(t, made.DSN).Path, "/"))

	// The module logs in with its own role, owns its database, and is no
	// superuser.
	module := openAs(t, made.DSN)
	_, err = module.ExecContext(t.Context(), "CREATE TABLE job (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	_, err = module.ExecContext(t.Context(), "INSERT INTO job (id) VALUES (1)")
	require.NoError(t, err)

	var super bool
	require.NoError(t, module.QueryRowContext(t.Context(),
		"SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&super))
	require.False(t, super)
	require.NoError(t, module.Close())

	// The same module installed again finds its data under a new password, and
	// the old password no longer logs in.
	again, err := provisionDatabase(t.Context(), postgres, instance, "com.github.acme.notes/notes")
	require.NoError(t, err)
	require.NotEqual(t, made.DSN, again.DSN)

	var rows int
	require.NoError(t, openAs(t, again.DSN).QueryRowContext(t.Context(), "SELECT COUNT(*) FROM job").Scan(&rows))
	require.Equal(t, 1, rows)
	require.Error(t, openAs(t, made.DSN).PingContext(t.Context()))

	// Another module under the same instance name is refused.
	_, err = provisionDatabase(t.Context(), postgres, instance, "com.github.other.notes/notes")
	require.ErrorIs(t, err, errDatabaseTaken)
}

// A database of the module's name that somebody else owns is refused before
// the server makes a role.
func TestTheServerTakesNoDatabaseItDidNotMake(t *testing.T) {
	t.Parallel()

	server := provisioningServer(t)
	suffix := strings.ToLower(rand.Text())[:8]
	postgres := config.PostgresEngine{DSN: server}
	instance := mustModuleInstance(t, "crm-"+suffix)

	name, err := databaseNameOf(postgres, instance)
	require.NoError(t, err)

	t.Cleanup(func() { dropModuleDatabase(server, name) })

	_, err = openAs(t, server).ExecContext(t.Context(), "CREATE DATABASE "+quotedName(name))
	require.NoError(t, err)

	_, err = provisionDatabase(t.Context(), postgres, instance, "com.github.acme.notes/crm")
	require.ErrorIs(t, err, errDatabaseTaken)

	var roles int
	require.NoError(t, openAs(t, server).QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM pg_roles WHERE rolname = $1", name).Scan(&roles))
	require.Zero(t, roles, "a role was made beside a database the server refused")
}

// provisioningServer is the fixture's connection string, and a skip where
// there is none, unless AUTHZ_REQUIRED_ENGINES names postgres.
func provisioningServer(t *testing.T) string {
	t.Helper()

	required := slices.Contains(requiredEngineNames(), "postgres")

	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		if required {
			t.Fatalf("AUTHZ_REQUIRED_ENGINES names postgres and %s is not set", postgresDSNEnv)
		}

		t.Skipf("%s is not set, so there is no Postgres server to make a database on", postgresDSNEnv)
	}

	if !postgresDriverLinked(t, dsn) {
		if required {
			t.Fatal("AUTHZ_REQUIRED_ENGINES names postgres and this build carries no postgres driver")
		}

		t.Skip("this build carries no postgres driver")
	}

	return dsn
}

// limitedAdmin makes a role that may create roles and databases and nothing
// more, and answers its connection string.
func limitedAdmin(t *testing.T, server string) string {
	t.Helper()

	name := "admin_" + strings.ToLower(rand.Text())[:8]
	password := rand.Text()

	admin := openAs(t, server)

	_, err := admin.ExecContext(t.Context(), "CREATE ROLE "+quotedName(name)+
		" WITH LOGIN CREATEDB CREATEROLE NOSUPERUSER PASSWORD "+quotedText(password))
	require.NoError(t, err)

	// A server that takes CONNECT away from PUBLIC, as the development
	// Postgres does, still lets this role into the database it connects to.
	var database string
	require.NoError(t, admin.QueryRowContext(t.Context(), "SELECT current_database()").Scan(&database))

	// Cases running at once grant on one database, and Postgres answers a
	// grant that races another with "tuple concurrently updated".
	require.Eventually(t, func() bool {
		_, err = admin.ExecContext(t.Context(), "GRANT CONNECT ON DATABASE "+pqQuoted(database)+" TO "+quotedName(name))

		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "grant CONNECT: %v", err)

	t.Cleanup(func() {
		db, openErr := authz.OpenPostgres(server)
		if openErr != nil {
			return
		}

		defer func() { _ = db.Close() }()

		// The test's own context is cancelled by the time cleanup runs.
		_, _ = db.ExecContext(context.Background(), "DROP OWNED BY "+quotedName(name))
		_, _ = db.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+quotedName(name))
	})

	parsed := mustURL(t, server)
	parsed.User = url.UserPassword(name, password)

	return parsed.String()
}

// openAs opens a handle the test closes.
func openAs(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := authz.OpenPostgres(dsn)
	require.NoError(t, err)

	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// dropModuleDatabase removes what a case made, as the fixture's own user.
func dropModuleDatabase(server, name string) {
	db, err := authz.OpenPostgres(server)
	if err != nil {
		return
	}

	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quotedName(name)+" WITH (FORCE)")
	_, _ = db.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+quotedName(name))
}

// pqQuoted quotes an identifier the server named, doubling any quote in it.
func pqQuoted(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
