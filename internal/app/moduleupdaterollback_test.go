package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// An update over HTTP is kept only when its release starts (ADR-20261021-bqc). The
// releases below are examples/database-module with one migration added
// through go build -overlay: version 4 adds a column and a step fills it, and
// the broken release's step fails after the column was added.

// notesAt is a release of the database module carrying migration 4 with the
// given step body.
func notesAt(variant, stepBody string) *artefact {
	const column = "ALTER TABLE note ADD COLUMN title TEXT NOT NULL DEFAULT '';\n"

	return &artefact{
		pkg: "./examples/database-module", name: "example-database", variant: variant,
		ldflags: "-X main.version=1.1.0",
		overlay: map[string]string{
			"migrations/sqlite/0004_note_title.sql":   column,
			"migrations/postgres/0004_note_title.sql": column,
			"step4_test_overlay.go": `package main

import (
	"context"
	"errors"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

var _ = errors.New

func init() {
	schema.Steps = append(schema.Steps, moduledb.Step{Version: 4, Name: "fill_titles", Up: func(
		ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine,
	) error {
` + stepBody + `
	}})
}
`,
		},
	}
}

//nolint:gochecknoglobals // one build per test binary, shared by every case that launches it.
var (
	notesModule = &artefact{pkg: "./examples/database-module", name: "example-database"}
	notesBroken = notesAt("-broken", brokenStep)
	notesFixed  = notesAt("-fixed", `		_, err := tx.ExecContext(ctx, "UPDATE note SET title = text")

		return err`)
)

// brokenStep fills the titles and then fails, so the column the file added
// and the rows the step wrote are both undone.
const brokenStep = `		if _, err := tx.ExecContext(ctx, "UPDATE note SET title = text"); err != nil {
			return err
		}

		return errors.New("the titles cannot be filled")`

// notesManifest is the database module's manifest at one version.
func notesManifest(version string) string {
	return strings.Join([]string{
		"name: example-database",
		"kind: sink",
		"license: MIT",
		"version: " + version,
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"needs:",
		"  database: true",
		"",
	}, "\n")
}

// aReleaseOfNotes is a release directory carrying one build of the module.
func aReleaseOfNotes(t *testing.T, built *artefact, version string) string {
	t.Helper()

	from, _ := built.install(t)

	dir := filepath.Join(t.TempDir(), "release")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(notesManifest(version)), 0o600))

	binary := "example-database_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	body, err := os.ReadFile(filepath.Join(from, binary))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, binary), body, 0o700))

	return dir
}

const notesPath = "github.com/acme/notes"

func TestAnUpdateWhoseMigrationFailsIsTakenBack(t *testing.T) {
	t.Parallel()

	deployment := aLiveDeployment(t)
	deployment.installer.settle = settle

	install := func(built *artefact, version string) (httpapi.ModuleInstallation, error) {
		return deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
			Module: notesPath, Version: version, From: aReleaseOfNotes(t, built, version), Approve: true,
		})
	}

	first, err := install(notesModule, "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "ready", first.State, first.Reason)
	require.Equal(t, 3, first.SchemaVersion)
	require.Equal(t, []string{
		"0001_notes.sql", "0002_note_written.sql", "0003_note_words.sql", "0003_count_words.go",
	}, first.Migrations)

	_, err = install(notesBroken, "1.1.0")
	require.Error(t, err)

	var raised *fault.Fault
	require.ErrorAs(t, err, &raised)
	require.Equal(t, httpfault.CodeModuleUpdateRolledBack(), raised.Code())
	require.Contains(t, err.Error(), "a migration failed, and the database is as it was before: 0004_fill_titles.go")
	require.Contains(t, err.Error(), "1.0.0 runs again")

	// The old release is declared, running, and its database is where it
	// was: version 3, nothing applied by the start that took it back.
	rows, err := resource.Installed(resource.Paths{File: deployment.installer.configPath,
		Overlay: deployment.installer.core.Overlay})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "1.0.0", rows[0].Spec.Module.Version)

	require.Eventually(t, func() bool {
		state, reported := deployment.host.Database(rows[0].Ref)
		status, _ := deployment.host.Status(rows[0].Ref)

		return reported && state.Version == 3 && len(state.Applied) == 0 && status.State == "ready"
	}, settle, 20*time.Millisecond, "the old release does not run again on the database it had")

	_, err = os.Stat(previousRelease(deployment.installer.core.Modules, "example-database"))
	require.ErrorIs(t, err, os.ErrNotExist, "the release kept aside is still there")

	fixed, err := install(notesFixed, "1.1.0")
	require.NoError(t, err)
	require.Equal(t, "ready", fixed.State, fixed.Reason)
	require.Equal(t, 4, fixed.SchemaVersion)
	require.Equal(t, []string{"0004_note_title.sql", "0004_fill_titles.go"}, fixed.Migrations)

	_, err = os.Stat(previousRelease(deployment.installer.core.Modules, "example-database"))
	require.ErrorIs(t, err, os.ErrNotExist, "an update that started kept the old release")
}
