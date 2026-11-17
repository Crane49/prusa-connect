package app

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// databaseManifest is the two-halves module asking for a database of its own
// (ADR-20261021-bqc).
func databaseManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", "needs:\n  database: true\npermissions:\n", 1)
}

// theOperatorsDSN is a Postgres database an operator made for the module.
const theOperatorsDSN = "postgres://spools:s3cret@db.lan:5432/spools?sslmode=require"

// An approved install with no choice gives the module a SQLite file, and the
// install says so (ADR-20261021-bqc rules 4 and 6).
func TestAnApprovedInstallGivesTheModuleASQLiteDatabase(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, databaseManifest(), true), "-approve")

	service := listenOfDeclared(t, configPath, core)
	require.Equal(t, &install.Database{Engine: wirerpc.DatabaseSQLite}, service.Database)
	require.Contains(t, said, "database    sqlite, database.sqlite in the module's data directory")
}

// Without approve the database is a request, and the declaration grants none.
func TestAnInstallWithoutApproveListsTheDatabaseAsARequest(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, databaseManifest(), true))

	require.Nil(t, listenOfDeclared(t, configPath, core).Database)
	require.Contains(t, said, databaseRequest)
}

// The operator names a Postgres database, and an update with no choice keeps
// it, password and all (ADR-20261021-bqc rule 6).
func TestAnUpdateKeepsTheDatabaseTheOperatorChose(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, databaseManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release,
		"-approve", "-database", "postgres", "-database-dsn", theOperatorsDSN)

	want := &install.Database{Engine: wirerpc.DatabasePostgres, DSN: theOperatorsDSN}
	require.Equal(t, want, listenOfDeclared(t, configPath, core).Database)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Equal(t, want, listenOfDeclared(t, configPath, core).Database)
	require.Contains(t, said, "database    postgres db.lan:5432/spools")
	require.NotContains(t, said, "s3cret")
}

// A choice the install cannot honour stops it before anything is written
// (ADR-20261021-bqc rule 7).
func TestADatabaseChoiceTheInstallCannotHonourIsRefused(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		manifest string
		extra    []string
	}{
		"a choice without approve":          {databaseManifest(), []string{"-database", "sqlite"}},
		"a choice for a module asking none": {bothHalvesManifest(), []string{"-approve", "-database", "sqlite"}},
		"an unknown mode":                   {databaseManifest(), []string{"-approve", "-database", "mysql"}},
		"postgres with no dsn":              {databaseManifest(), []string{"-approve", "-database", "postgres"}},
		"postgres in key=value":             {databaseManifest(), []string{"-approve", "-database", "postgres", "-database-dsn", "host=db dbname=x"}},
		"a dsn for sqlite":                  {databaseManifest(), []string{"-approve", "-database", "sqlite", "-database-dsn", theOperatorsDSN}},
		"provision on a sqlite core":        {databaseManifest(), []string{"-approve", "-database", "provision"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath, core := installConfig(t)

			var out bytes.Buffer

			args := append([]string{addVerb, "-config", configPath, "-from", aRelease(t, c.manifest, true)}, c.extra...)
			err := Module(t.Context(), &out, append(args, theModulePath+"@1.0.0"))
			require.ErrorIs(t, err, ErrUsage)

			doc, loadErr := installedDocument(configPath, core)
			require.NoError(t, loadErr)
			require.Empty(t, doc.Instances, "a refused install wrote a declaration")
		})
	}
}

// Over HTTP the database is among what was given, and a page that did not
// show it cannot approve it (ADR-20261021-bqc rules 4 and 8).
func TestAnInstallOverHTTPGrantsTheDatabaseItShowed(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, databaseManifest(), true), Approve: true,
		Database: &httpapi.ModuleDatabaseChoice{Mode: "postgres", DSN: theOperatorsDSN},
		Expect:   &httpapi.ModuleRequests{Permissions: []string{"observe"}, Database: true},
	})
	require.NoError(t, err)
	require.Contains(t, held.Approved, databaseRequest)

	installer, _ = theInstaller(t, true)

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, databaseManifest(), true), Approve: true,
		Expect: &httpapi.ModuleRequests{Permissions: []string{"observe"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "MODULE_REQUESTS_CHANGED")
	require.Contains(t, err.Error(), databaseRequest)
}

// The decision itself, over every place and every held grant.
func TestTheDatabaseAnInstallGrants(t *testing.T) {
	t.Parallel()

	onPostgres := config.Core{Persistence: config.Persistence{Type: config.EnginePostgres}}
	made := &install.Database{Engine: wirerpc.DatabasePostgres, DSN: theOperatorsDSN, Provisioned: true}
	sqlite := &install.Database{Engine: wirerpc.DatabaseSQLite}

	for name, c := range map[string]struct {
		core    config.Core
		request moduleRequest
		asked   bool
		held    *install.Database
		want    databaseDecision
	}{
		"not asked":                {request: moduleRequest{approve: true}},
		"not approved":             {asked: true},
		"a first install":          {request: moduleRequest{approve: true}, asked: true, want: databaseDecision{granted: sqlite}},
		"an update keeps":          {request: moduleRequest{approve: true}, asked: true, held: made, want: databaseDecision{granted: made}},
		"provision makes":          {core: onPostgres, request: moduleRequest{approve: true, database: &databaseChoice{mode: "provision"}}, asked: true, want: databaseDecision{provision: true}},
		"provision makes it again": {core: onPostgres, request: moduleRequest{approve: true, database: &databaseChoice{mode: "provision"}}, asked: true, held: made, want: databaseDecision{provision: true}},
		"sqlite replaces":          {request: moduleRequest{approve: true, database: &databaseChoice{mode: "sqlite"}}, asked: true, held: made, want: databaseDecision{granted: sqlite}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decideDatabase(c.core, c.request, c.asked, c.held, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

// The server names the role and the database after the instance, and builds
// the module's connection string from the admin's server alone (ADR-20261021-bqc
// rules 12 and 13).
func TestTheModuleDatabaseIsNamedAndReachedWithItsOwnLogin(t *testing.T) {
	t.Parallel()

	instance := mustModuleInstance(t, "notes-crm")

	// The core's own database is in the name, from either form of its
	// connection string, so two servers on one Postgres server never share it.
	for dsn, want := range map[string]string{
		"postgres://connect:pw@db.lan:5432/connect":        "module_connect_notes_crm",
		"host=db.lan user=connect dbname='Connect-B'":      "module_connect_b_notes_crm",
		"host=db.lan user=connect":                         "module_notes_crm",
		"postgresql://connect@db.lan/site.one?sslmode=off": "module_site_one_notes_crm",
	} {
		named, err := databaseNameOf(config.PostgresEngine{DSN: dsn}, instance)
		require.NoError(t, err)
		require.Equal(t, want, named, dsn)
	}

	name, err := databaseNameOf(config.PostgresEngine{}, instance)
	require.NoError(t, err)

	_, err = databaseNameOf(config.PostgresEngine{}, mustModuleInstance(t, strings.Repeat("a", 60)))
	require.ErrorIs(t, err, ErrUsage)

	base := mustURL(t, "postgres://admin:adminpw@db.lan:5432/connect?sslmode=verify-full&sslrootcert=/ca.pem"+
		"&sslcert=/admin.crt&sslkey=/admin.key&password=adminpw")
	dsn := moduleDSN(base, name, "modulepw")

	require.Equal(t, "postgres://module_notes_crm:modulepw@db.lan:5432/module_notes_crm?"+
		"sslmode=verify-full&sslrootcert=%2Fca.pem", dsn)
	require.NotContains(t, dsn, "admin")

	require.Equal(t, "'it''s'", quotedText("it's"))
}

// A database the server could not make is answered with its own code, which
// names the database and never the admin's credential.
func TestADatabaseTheServerCouldNotMakeHasItsOwnCode(t *testing.T) {
	t.Parallel()

	err := installFault(theModulePath, databaseNotMadeError{name: "module_spools", reason: errDatabaseTaken})
	require.Contains(t, err.Error(), "MODULE_DATABASE_NOT_MADE")
	require.Contains(t, err.Error(), "module_spools")
	require.ErrorIs(t, databaseNotMadeError{reason: errDatabaseTaken}, errDatabaseTaken)
}

// installedDocument reads back every declaration the install wrote.
func installedDocument(configPath string, core config.Core) (resource.Document, error) {
	return resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
}

func mustModuleInstance(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, name)
	require.NoError(t, err)

	return instance
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoError(t, err)

	return parsed
}
