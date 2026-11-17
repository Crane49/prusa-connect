package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
)

// examples/database-module, which keeps notes in a database of its own and
// migrates it in its plugin.configure (ADR-20261021-bqc rules 16 and 17).
//
//nolint:gochecknoglobals // one build per test binary, shared by every case that launches it.
var databaseModule = &artefact{pkg: "./examples/database-module", name: "database-module"}

const (
	databaseModuleType  = moduleNamespace + "/database-module"
	databaseModuleGroup = moduleGroup + "/notes"
	databaseInstance    = "sink/notes"
)

// databaseModuleConfig declares the example module with the database lines
// given under its service block, and none when there are no lines.
func databaseModuleConfig(t *testing.T, database ...string) string {
	t.Helper()

	dir, checksum := databaseModule.install(t)

	var declared strings.Builder

	declared.WriteString(lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		"  sink:",
		"    notes:",
		"      type: "+databaseModuleType,
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: "+slashed(dir),
		"        checksum: sha256:"+checksum,
		"        contract_major: 1",
	))

	if len(database) > 0 {
		declared.WriteString(lines("        database:"))

		for _, line := range database {
			declared.WriteString(lines("          " + line))
		}
	}

	return declared.String()
}

// The module is granted a SQLite file, migrates it before it reports ready,
// keeps a note in it, and finds the note again after the server restarts.
func TestAModuleKeepsItsDataInASQLiteDatabaseOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	running := start(t, dir, databaseModuleConfig(t, "engine: sqlite"))

	waitForNotes(t, running, condition.StateReady().String())

	written := running.call(t, http.MethodPost, databaseModuleGroup, `{"text":"a spool of PETG"}`)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	file := filepath.Join(config.DefaultModules(filepath.Join(dir, "config.yaml")), "..", "module-data", "notes",
		sqliteDatabaseName)
	_, err := os.Stat(filepath.Clean(file))
	require.NoError(t, err, "the module's database is not where the host said it is")

	running.stopAndWait(t)

	again := start(t, dir, databaseModuleConfig(t, "engine: sqlite"))
	waitForNotes(t, again, condition.StateReady().String())

	listed := again.get(t, databaseModuleGroup)
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.Contains(t, listed.body, "a spool of PETG")

	again.stopAndWait(t)
}

// A module that needs a database and was granted none refuses its settings,
// and the instance says why instead of serving.
func TestAModuleGrantedNoDatabaseDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	running := start(t, dir, databaseModuleConfig(t))

	waitForNotes(t, running, condition.StateFailed().String())

	read := running.get(t, "/api/v1/resources/"+databaseInstance)
	require.Contains(t, read.body, "database")

	running.stopAndWait(t)
}

// The server makes the module's database on Postgres, and the module keeps
// its notes there with its own login (ADR-20261021-bqc rules 11 to 17).
func TestAModuleKeepsItsDataInAPostgresDatabaseTheServerMade(t *testing.T) {
	server := provisioningServer(t)
	suffix := strings.ToLower(time.Now().Format("150405.000000"))
	suffix = strings.ReplaceAll(suffix, ".", "")
	instance := mustModuleInstance(t, "notes-"+suffix)
	postgres := config.PostgresEngine{DSN: server}

	name, err := databaseNameOf(postgres, instance)
	require.NoError(t, err)

	t.Cleanup(func() { dropModuleDatabase(server, name) })

	made, err := provisionDatabase(t.Context(), postgres, instance, databaseModuleType)
	require.NoError(t, err)

	dir := t.TempDir()
	running := start(t, dir, databaseModuleConfig(t, "engine: postgres", "dsn: '"+made.DSN+"'", "provisioned: true"))

	waitForNotes(t, running, condition.StateReady().String())

	written := running.call(t, http.MethodPost, databaseModuleGroup, `{"text":"a spool of ASA"}`)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	listed := running.get(t, databaseModuleGroup)
	require.Contains(t, listed.body, "a spool of ASA")

	password, _ := mustURL(t, made.DSN).User.Password()
	resources := running.get(t, "/api/v1/resources/"+databaseInstance)
	require.NotContains(t, resources.body, password, "a resource read answered the module's credential")

	running.stopAndWait(t)
}

// waitForNotes waits for the example module to reach a state.
func waitForNotes(t *testing.T, running *process, state string) {
	t.Helper()

	require.Eventually(t, func() bool {
		return running.instanceState(t, databaseInstance) == state
	}, settle, 20*time.Millisecond, "%s never reached %s", databaseInstance, state)
}
