package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestUpgradePreflightDisablesAModuleWhenTheCandidateDropsACapability(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	if err := os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	holder := maintenance{configPath: configPath, core: config.Core{Overlay: overlay}}
	impacts := holder.moduleImpacts(&upgrade.BinaryMetadata{
		Contract: "1.0.0", Capabilities: []string{"device.read"},
	}, modulePlan{})
	if len(impacts) != 1 {
		t.Fatalf("module impacts are %#v", impacts)
	}
	if impacts[0].Outcome != "disabled" ||
		!strings.Contains(impacts[0].Reason, "state.read") {
		t.Fatalf("module impact is %#v", impacts[0])
	}
}

func TestUpgradePreflightKeepsAModuleWhenTheCandidateServesItsCapability(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	if err := os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	holder := maintenance{configPath: configPath, core: config.Core{Overlay: overlay}}
	impacts := holder.moduleImpacts(&upgrade.BinaryMetadata{
		Contract: "1.0.0", Capabilities: []string{"state.read"},
	}, modulePlan{})
	if len(impacts) != 1 || impacts[0].Outcome != "ok" {
		t.Fatalf("module impacts are %#v", impacts)
	}
}

func TestUpgradePreflightDisablesAModuleWhenTheCandidateChangesTheContractMajor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	if err := os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	holder := maintenance{configPath: configPath, core: config.Core{Overlay: overlay}}
	impacts := holder.moduleImpacts(&upgrade.BinaryMetadata{
		Contract: "2.0.0", Capabilities: []string{"state.read"},
	}, modulePlan{})
	if len(impacts) != 1 || impacts[0].Outcome != upgrade.ImpactDisabled ||
		!strings.Contains(impacts[0].Reason, "wire contract major") {
		t.Fatalf("module impacts are %#v", impacts)
	}
}

// TestPrepareRunsAgainAStepItsRequestStopped stops the verify, then the
// rehearsal, with the request that ran it. Each journal stays where the step
// began, and the next prepare runs the step and goes on.
func TestPrepareRunsAgainAStepItsRequestStopped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	require.NoError(t, os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600))

	// The rehearsal ends the request it runs under, as a browser closed then
	// would. Only the first prepare runs under that request.
	rehearsing, stopRehearsal := context.WithCancel(t.Context())
	verifier, err := upgrade.BuildVerifier(nil, true)
	require.NoError(t, err)
	service, err := upgrade.NewService(upgrade.ServiceOptions{
		Root: filepath.Join(root, "upgrade"), CurrentVersion: "1.0.0", Contract: "1.1.0", Verifier: verifier,
		Metadata: func(ctx context.Context, _ string) (upgrade.BinaryMetadata, error) {
			return upgrade.BinaryMetadata{
				Version: "1.1.0", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
				Contract: "1.1.0", Capabilities: []string{"state.read"},
			}, ctx.Err()
		},
		Rehearse: func(ctx context.Context, _ string) (upgrade.RehearsalReport, error) {
			stopRehearsal()

			return upgrade.RehearsalReport{}, ctx.Err()
		},
	})
	require.NoError(t, err)
	holder := &maintenance{configPath: configPath, core: config.Core{Overlay: overlay}, service: service}

	stopped, stop := context.WithCancel(t.Context())
	stop()
	binary := []byte("candidate")
	sum := sha256.Sum256(binary)
	_, err = holder.stageAndVerify(stopped, upgrade.StageRequest{
		Name: "core", Version: "1.1.0", Binary: bytes.NewReader(binary),
		Sums: []byte(hex.EncodeToString(sum[:]) + "  core\n"),
	})
	require.ErrorIs(t, err, context.Canceled)
	status, err := service.Status()
	require.NoError(t, err)
	require.Equal(t, upgrade.StateStaged, status.State)

	_, err = holder.Prepare(rehearsing)
	require.ErrorIs(t, err, context.Canceled)
	status, err = service.Status()
	require.NoError(t, err)
	require.Equal(t, upgrade.StatePreflighted, status.State)

	prepared, err := holder.Prepare(t.Context())
	require.NoError(t, err)
	require.Equal(t, upgrade.StateRehearsed, prepared.State)
}

func writeMaintenanceModule(t *testing.T, path string) {
	t.Helper()

	body := `resources:
  sink:
    spools:
      type: com.github.aux.acme.spools/spools
      enabled: true
      required: false
      runtime: service
      service:
        path: /opt/example/spools
        checksum: sha256:` + strings.Repeat("a", 64) + `
        contract_major: 1
      module:
        version: 1.0.0
        requires:
          - capability: state.read
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A SQLite host whose module keeps its data in PostgreSQL is not covered by a
// rollback unless pg_dump protects it, so applying asks for the same
// acknowledgement a PostgreSQL host does.
func TestUpgradeApplyAsksToAcknowledgeAModulePostgresDatabase(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "        contract_major: 1\n",
		"        contract_major: 1\n        database:\n          engine: postgres\n"+
			"          dsn: postgres://spools:secret@db/spools\n", 1))
	if err = os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		mode string
		want bool
	}{
		{string(upgrade.PostgresExternal), true},
		{string(upgrade.PostgresNone), true},
		{string(upgrade.PostgresDump), false},
	} {
		core := config.Core{Overlay: overlay}
		core.Persistence.Type = config.EngineSQLite
		core.Upgrade.PostgresBackup = tc.mode
		holder := maintenance{configPath: configPath, core: core}
		got, checkErr := holder.postgresOutsideBackups()
		if checkErr != nil {
			t.Fatal(checkErr)
		}
		if got != tc.want {
			t.Fatalf("mode %s: a module PostgreSQL database outside backups is %t, want %t", tc.mode, got, tc.want)
		}
	}
}

// A module the upgrade's plan moves to another release is updated, not
// disabled, and one the plan cannot resolve stops serving with the
// resolver's reason (ADR-20261027-e6k rule 3a). A module may not be required
// for readiness, so it never blocks the upgrade on its own.
func TestUpgradeImpactsFollowTheModulePlan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	overlay := filepath.Join(root, "resources.json")
	writeMaintenanceModule(t, configPath)
	if err := os.WriteFile(overlay, []byte(`{"version":1,"instances":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	holder := maintenance{configPath: configPath, core: config.Core{Overlay: overlay}}
	metadata := &upgrade.BinaryMetadata{Contract: "1.0.0", Capabilities: []string{"state.read"}}

	updated := holder.moduleImpacts(metadata, modulePlan{steps: []moduledeps.Step{{
		Release: moduledeps.Release{Namespace: "com.github.aux.acme.spools", Version: "2.0.0"},
	}}})
	require.Len(t, updated, 1)
	require.Equal(t, upgrade.ImpactUpdated, updated[0].Outcome, updated[0].Reason)

	refused := holder.moduleImpacts(metadata, modulePlan{
		refused: errors.New("no release of spools runs on core 2.0.0"),
		broken:  []string{"com.github.aux.acme.spools"},
	})
	require.Len(t, refused, 1)
	require.Equal(t, upgrade.ImpactDisabled, refused[0].Outcome)
	require.Contains(t, refused[0].Reason, "no release of spools")
}

// The Updates page asks for the PostgreSQL acknowledgement only where applying
// would refuse without it, so the version names that.
func TestServerVersionSaysWhenApplyNeedsThePostgresAcknowledgement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode string
		want bool
	}{
		{string(upgrade.PostgresExternal), true},
		{string(upgrade.PostgresDump), false},
	} {
		core := config.Core{}
		core.Persistence.Type = config.EnginePostgres
		core.Upgrade.PostgresBackup = tc.mode
		holder := maintenance{core: core}
		version, err := holder.ServerVersion(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if version.PostgresAckRequired != tc.want {
			t.Fatalf("mode %s: acknowledgement required is %t, want %t", tc.mode, version.PostgresAckRequired, tc.want)
		}
	}
}

// A cancel on Updates whose journal names a folder outside the staging folder
// still ends the transaction, and leaves that folder as it is.
func TestCancelOnUpdatesEndsATransactionWhoseReleaseItLeaves(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	kept := filepath.Join(t.TempDir(), "core")
	require.NoError(t, os.WriteFile(kept, []byte("x"), 0o600))
	store := upgrade.NewStore(filepath.Join(root, "journal.json"))
	journal, err := upgrade.NewJournal("upgrade-1", "1.0.0", "1.1.0", kept, time.Now())
	require.NoError(t, err)
	require.NoError(t, store.Save(journal))

	service, err := upgrade.NewService(upgrade.ServiceOptions{Root: root, CurrentVersion: "1.0.0"})
	require.NoError(t, err)
	holder := maintenance{service: service, logger: discard()}

	require.NoError(t, holder.Cancel(t.Context()))
	_, err = store.Load()
	require.ErrorIs(t, err, upgrade.ErrNoJournal, "the transaction stayed")
	require.FileExists(t, kept, "the cancel removed a file outside the staging folder")
}
