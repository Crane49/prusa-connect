package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module's own database outlives its grant until the operator purges it
// (ADR-20261021-bqc rules 21 to 25).

// runModule runs one module command and answers what it printed.
func runModule(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := Module(t.Context(), &out, append([]string{args[0], "-config", configPath}, args[1:]...))

	return out.String(), err
}

// sqliteFileOf is where the host puts the SQLite database of the instance the
// tests install.
func sqliteFileOf(core config.Core) string {
	return filepath.Join(moduleDataRoot(core), "spools", sqliteDatabaseName)
}

// A removal leaves the database and records it. `module list` shows it, and
// installing the same module again takes it back.
func TestARemovalLeavesTheDatabaseAndAReinstallTakesItBack(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, databaseManifest(), true)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	said, err := runModule(t, configPath, removeVerb, "spools")
	require.NoError(t, err)
	require.Contains(t, said, "left behind sqlite")

	left, err := resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Len(t, left, 1)
	require.Equal(t, resource.LeftRemoved, left[0].Why)
	require.Equal(t, "sink:spools", left[0].Instance)

	listed, err := runModule(t, configPath, listVerb)
	require.NoError(t, err)
	require.Contains(t, listed, "Databases no module uses any more")
	require.Contains(t, listed, "sink:spools")

	again := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, again, "with the data an earlier install left there")

	left, err = resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Empty(t, left, "the database taken back is still listed as left behind")
}

// An update that moves the database records the old one as moved, and the
// update's output says where it is.
func TestAnUpdateThatMovesTheDatabaseLeavesTheOldOne(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, databaseManifest(), true)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release,
		"-approve", "-database", "postgres", "-database-dsn", theOperatorsDSN)
	require.Contains(t, said, "left behind sqlite")
	require.NotContains(t, said, "s3cret")

	left, err := resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Len(t, left, 1)
	require.Equal(t, resource.LeftMoved, left[0].Why)
	require.Equal(t, string(wirerpc.DatabaseSQLite), left[0].Database.Engine)

	// Back to SQLite takes the file back, and leaves the operator's database.
	back := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve", "-database", "sqlite")
	require.Contains(t, back, "with the data an earlier install left there")
	require.Contains(t, back, "left behind postgres db.lan:5432/spools")

	left, err = resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Len(t, left, 1)
	require.Equal(t, string(wirerpc.DatabasePostgres), left[0].Database.Engine)
}

// A purge without -yes says what it would do and does nothing. With -yes it
// deletes the SQLite file and the two files beside it, and the record goes.
func TestAPurgeDeletesTheSQLiteFileOnlyWithYes(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, databaseManifest(), true), "-approve")

	file := sqliteFileOf(core)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))

	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.WriteFile(file+suffix, []byte("notes"), 0o600))
	}

	_, err := runModule(t, configPath, removeVerb, "spools")
	require.NoError(t, err)

	said, err := runModule(t, configPath, purgeVerb, "spools")
	require.NoError(t, err)
	require.Contains(t, said, "would delete")
	require.FileExists(t, file)

	said, err = runModule(t, configPath, purgeVerb, "spools", "-yes")
	require.NoError(t, err)
	require.Contains(t, said, purgeDeleted)

	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoFileExists(t, file+suffix)
	}

	left, err := resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Empty(t, left)

	_, err = runModule(t, configPath, purgeVerb, "spools", "-yes")
	require.ErrorIs(t, err, errNothingLeft)
}

// A database the operator named is theirs: a purge forgets the record and
// touches nothing on their server.
func TestAPurgeOnlyForgetsTheOperatorsDatabase(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, databaseManifest(), true),
		"-approve", "-database", "postgres", "-database-dsn", theOperatorsDSN)

	_, err := runModule(t, configPath, removeVerb, "spools")
	require.NoError(t, err)

	said, err := runModule(t, configPath, purgeVerb, "sink:spools", "-yes")
	require.NoError(t, err)
	require.Contains(t, said, purgeForgotten+" postgres db.lan:5432/spools")
	require.NotContains(t, said, "s3cret")

	left, err := resource.LeftDatabases(core.Overlay)
	require.NoError(t, err)
	require.Empty(t, left)
}

// A record whose database a declaration grants again is not purged.
func TestAPurgeRefusesADatabaseGrantedAgain(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, databaseManifest(), true)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	instance := mustModuleInstance(t, "spools")
	left := resource.LeftBehind(instance, theNamespace+"/spools",
		listenOfDeclared(t, configPath, core).Database, resource.LeftRemoved, time.Now())

	_, err := purgeLeft(t.Context(), core, configPath, []resource.LeftDatabase{left}, false)
	require.ErrorIs(t, err, ErrUsage)
	require.Contains(t, err.Error(), "uses this database again")
}

// A database this server made is dropped with its role on the server it was
// made on, and one whose role names another module is refused. It runs where
// the fixture is: make test-engines.
func TestAPurgeDropsTheDatabaseAndTheRoleTheServerMade(t *testing.T) {
	t.Parallel()

	server := provisioningServer(t)
	admin := limitedAdmin(t, server)
	postgres := config.PostgresEngine{DSN: admin}
	core := config.Core{Persistence: config.Persistence{Type: config.EnginePostgres, Postgres: postgres}}

	suffix := strings.ToLower(rand.Text())[:8]
	instance := mustModuleInstance(t, "notes-"+suffix)
	name, err := databaseNameOf(postgres, instance)
	require.NoError(t, err)

	t.Cleanup(func() { dropModuleDatabase(server, name) })

	made, err := provisionDatabase(t.Context(), postgres, instance, "com.github.acme.notes/notes")
	require.NoError(t, err)

	left := resource.LeftBehind(instance, "com.github.acme.notes/notes", made, resource.LeftRemoved, time.Now())

	// The record says another module: the role's comment names this one, so
	// nothing is dropped.
	stranger := left
	stranger.Type = "com.github.other.notes/notes"
	err = dropLeftDatabase(t.Context(), postgres, stranger)
	require.ErrorIs(t, err, errDatabaseTaken)
	require.True(t, roleExists(t, server, name))

	require.NoError(t, dropLeftDatabase(t.Context(), core.Persistence.Postgres, left))
	require.False(t, roleExists(t, server, name), "the role is still there")
	require.False(t, databaseExists(t, server, name), "the database is still there")

	// Another server's database is never dropped from this one.
	elsewhere := left
	elsewhere.Database.DSN = strings.Replace(made.DSN, mustURL(t, made.DSN).Host, "db.elsewhere:5432", 1)
	err = dropLeftDatabase(t.Context(), postgres, elsewhere)
	require.ErrorIs(t, err, ErrUsage)
	require.Contains(t, err.Error(), "db.elsewhere:5432")
}

// roleExists and databaseExists ask the server about a name.
func roleExists(t *testing.T, server, name string) bool {
	t.Helper()

	return namedCount(t, server, "SELECT COUNT(*) FROM pg_roles WHERE rolname = $1", name) == 1
}

func databaseExists(t *testing.T, server, name string) bool {
	t.Helper()

	return namedCount(t, server, "SELECT COUNT(*) FROM pg_database WHERE datname = $1", name) == 1
}

func namedCount(t *testing.T, server, query, name string) int {
	t.Helper()

	db := openAs(t, server)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(context.WithoutCancel(t.Context()), query, name).Scan(&count))

	return count
}
