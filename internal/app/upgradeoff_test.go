package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// TestUpgradesOffLeavesNoPairingAndNoUpgradeFolder is a device whose operating
// system installs every server update. With core.upgrade.enabled off the server writes
// no pairing and nothing below the upgrade folder, and serves no upgrade, no
// backup and no pairing. Its version and its restart are still served.
func TestUpgradesOffLeavesNoPairingAndNoUpgradeFolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, lines(
		"core:",
		"  log:",
		"    level: error",
		"  upgrade:",
		"    enabled: false",
	))

	for _, path := range []string{
		"/api/v1/server/upgrade/history",
		"/api/v1/server/backups",
		"/api/v1/server/beta-portal-pairing",
	} {
		read := running.get(t, path)
		require.Equal(t, http.StatusNotFound, read.status, "%s: %s", path, read.body)
	}

	version := running.get(t, "/api/v1/server/version")
	require.Equal(t, http.StatusOK, version.status, version.body)

	var settings httpapi.AppSettings
	require.NoError(t, json.Unmarshal([]byte(running.get(t, "/api/v1/ui").body), &settings))
	require.False(t, settings.Upgrades)

	overlay := config.DefaultOverlay(filepath.Join(dir, "config.yaml"))
	_, err := os.Stat(betaportal.Path(overlay))
	require.ErrorIs(t, err, os.ErrNotExist, "the server wrote a Beta Portal pairing")
	_, err = os.Stat(filepath.Join(filepath.Dir(overlay), "upgrade"))
	require.ErrorIs(t, err, os.ErrNotExist, "the server wrote below the upgrade folder")

	running.stopAndWait(t)
}

// TestUpgradesOffBuildsNoUpgradeService checks what an HTTP request cannot
// see: the maintenance holder has no service, so no scheduler reads the
// journal every second.
func TestUpgradesOffBuildsNoUpgradeService(t *testing.T) {
	t.Parallel()

	off := false
	core := config.Core{Overlay: filepath.Join(t.TempDir(), "resources.json")}
	core.Upgrade.Enabled = &off

	holder, err := newMaintenance(
		t.Context(), core, filepath.Join(t.TempDir(), "config.yaml"), "test", nil, nil, nil, "local",
		nil, nil, nil,
	)
	require.NoError(t, err)
	require.Nil(t, holder.service)
	require.Nil(t, holder.releases)
	require.Zero(t, holder.backups)

	version, err := holder.ServerVersion(t.Context())
	require.NoError(t, err)
	require.Equal(t, "test", version.Version)
}

// TestUpgradesOffRunsNoScheduledUpgradeAndKeepsTheRestart is a server switched
// off while an upgrade waits for a time that has passed. No scheduler runs, so
// the upgrade is not applied, and the restart is not refused for it.
func TestUpgradesOffRunsNoScheduledUpgradeAndKeepsTheRestart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		core := upgradesOff(t)
		waitingUpgrade(t, core)

		applied, restarted := 0, 0
		holder, err := newMaintenance(
			t.Context(), core, filepath.Join(t.TempDir(), "config.yaml"), "test", nil, nil, nil, "local",
			nil, func() { applied++ }, nil,
		)
		require.NoError(t, err)
		holder.requestRestart, holder.logger = func() { restarted++ }, discard()

		require.NoError(t, holder.Restart(t.Context(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe}))
		require.Equal(t, 1, restarted)

		// A scheduler ticks once a second, so a minute covers many ticks.
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Zero(t, applied, "a scheduler applied an upgrade while upgrades are off")
	})
}

// TestUpgradesOffSaysSoAndNamesAPairingThatHoldsAToken is the start of a
// server that had upgrades on before. Disconnect is on the Updates tab, which
// is not drawn, so the log names a pairing that still holds an access token,
// beside the overlay or where release 0.0.1-alpha.7 kept it. A pairing without
// a token, such as one a disconnect cleared, is not named, so the warning
// stops once the operator acted on it.
func TestUpgradesOffSaysSoAndNamesAPairingThatHoldsAToken(t *testing.T) {
	t.Parallel()

	for name, earlierHeld := range map[string]bool{"beside the overlay": false, "in config.d": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			namesAPairingThatHoldsAToken(t, earlierHeld)
		})
	}
}

func namesAPairingThatHoldsAToken(t *testing.T, earlierHeld bool) {
	t.Helper()

	core := upgradesOff(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	paired, unpaired := betaportal.Path(core.Overlay), betaportal.EarlierPath(configPath)
	if earlierHeld {
		paired, unpaired = unpaired, paired
	}
	holdToken(t, paired)
	_, err := betaportal.Open(unpaired)
	require.NoError(t, err)

	var logged bytes.Buffer
	entitlement, portal, err := openPortal(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core,
		configPath, "instance")
	require.NoError(t, err)
	require.Nil(t, entitlement)
	require.Nil(t, portal)

	require.Contains(t, logged.String(), `msg="upgrades are off" key=core.upgrade.enabled`)
	require.Contains(t, logged.String(), "a Beta Portal pairing still holds an access token")
	require.Contains(t, logged.String(), "file="+paired)
	require.NotContains(t, logged.String(), unpaired, "a pairing without a token was named")

	store, err := betaportal.Open(paired)
	require.NoError(t, err)
	require.NoError(t, store.ClearLocal())
	logged.Reset()
	_, _, err = openPortal(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, configPath, "instance")
	require.NoError(t, err)
	require.NotContains(t, logged.String(), "Beta Portal pairing", "a disconnected pairing was named")
}

// TestUpgradesOffCancelsAScheduledUpgradeAtStart is a server switched off
// while an upgrade waits. Nobody can see or cancel it while upgrades are off,
// and a scheduler would start it the moment they are on again, so the start
// cancels it as Cancel on the Updates tab does, and says so.
func TestUpgradesOffCancelsAScheduledUpgradeAtStart(t *testing.T) {
	t.Parallel()

	core := upgradesOff(t)
	staged := waitingUpgrade(t, core)

	var logged bytes.Buffer
	_, _, err := openPortal(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core,
		filepath.Join(t.TempDir(), "config.yaml"), "instance")
	require.NoError(t, err)

	const cancelled = `msg="an upgrade scheduled before upgrades were switched off is cancelled; ` +
		`prepare the release again once upgrades are on" transaction=upgrade-1 to_version=1.1.0 scheduled_for=`
	require.Contains(t, logged.String(), cancelled)
	_, err = upgrade.NewStore(upgradeJournalPath(core)).Load()
	require.ErrorIs(t, err, upgrade.ErrNoJournal, "the scheduled upgrade stayed")
	require.NoDirExists(t, staged, "the staged release stayed")
}

// TestUpgradesOffNamesAScheduledUpgradeItCannotCancel is an upgrade folder the
// server cannot write. The journal reads, and the log names the transaction
// it could not remove rather than a journal it could not read.
func TestUpgradesOffNamesAScheduledUpgradeItCannotCancel(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder mode does not stop this process here")
	}

	core := upgradesOff(t)
	staged := waitingUpgrade(t, core)
	folder := filepath.Dir(upgradeJournalPath(core))
	require.NoError(t, os.Chmod(folder, 0o500))
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })

	var logged bytes.Buffer
	_, _, err := openPortal(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core,
		filepath.Join(t.TempDir(), "config.yaml"), "instance")
	require.NoError(t, err)

	require.Contains(t, logged.String(), "cannot be cancelled, because its journal cannot be removed")
	require.Contains(t, logged.String(), "transaction=upgrade-1")
	require.NotContains(t, logged.String(), "the upgrade journal cannot be read")
	// The release goes before the journal, so the next start that can remove
	// the journal finds nothing left to remove.
	require.NoDirExists(t, staged, "the staged release stayed while the journal could not be removed")
}

// TestTheRestartWithUpgradesOffReadsNoJournal is the guard in Restart. A
// server with upgrades off runs no upgrade, so a journal on disk does not
// refuse the restart, as it does with upgrades on.
func TestTheRestartWithUpgradesOffReadsNoJournal(t *testing.T) {
	t.Parallel()

	m, marker, restarts := restartingMaintenance(t)
	m.off = true
	m.store = upgrade.NewStore(filepath.Join(t.TempDir(), "journal.json"))
	require.NoError(t, m.store.Save(upgrade.Journal{
		Schema: 1, ID: "upgrade-1", State: upgrade.StateScheduled,
		FromVersion: "1.0.0", ToVersion: "1.1.0", StagedBinary: "/s", CurrentBinary: "/c",
		Revision: 1, UpdatedAt: time.Now(),
	}))

	require.NoError(t, m.Restart(t.Context(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe}))
	require.FileExists(t, marker)
	require.Equal(t, 1, *restarts)
	_, err := m.store.Load()
	require.NoError(t, err, "the restart changed the journal")
}

// TestUpgradesOffStillRefusesAWrongTrustedKey is a configuration whose trusted
// key does not match its id. Upgrades off still read the keys, so the mistake
// stops this start and not the one that switches upgrades on.
func TestUpgradesOffStillRefusesAWrongTrustedKey(t *testing.T) {
	t.Parallel()

	core := upgradesOff(t)
	core.Upgrade.TrustedKeys = []config.UpgradeKey{
		{ID: "not-its-id", Key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	}

	_, err := newMaintenance(
		t.Context(), core, filepath.Join(t.TempDir(), "config.yaml"), "test", nil, nil, nil, "local",
		nil, nil, nil,
	)
	require.ErrorIs(t, err, upgrade.ErrSignature)
}

func upgradesOff(t *testing.T) config.Core {
	t.Helper()

	off := false
	core := config.Core{Overlay: filepath.Join(t.TempDir(), "resources.json")}
	core.Upgrade.Enabled = &off

	return core
}

// waitingUpgrade records an upgrade scheduled for a day ago, which a running
// scheduler applies at its next tick, with its release staged where the
// service stages one. It returns the staging folder.
func waitingUpgrade(t *testing.T, core config.Core) string {
	t.Helper()

	root := filepath.Dir(upgradeJournalPath(core))
	staged := filepath.Join(root, "staged", "upgrade-1")
	require.NoError(t, os.MkdirAll(staged, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "core"), []byte("candidate"), 0o600))

	due := time.Now().Add(-24 * time.Hour)
	require.NoError(t, upgrade.NewStore(upgradeJournalPath(core)).Save(upgrade.Journal{
		Schema:        1,
		ID:            "upgrade-1",
		State:         upgrade.StateScheduled,
		FromVersion:   "1.0.0",
		ToVersion:     "1.1.0",
		StagedBinary:  filepath.Join(staged, "core"),
		CurrentBinary: "/c",
		Schedule:      "at",
		ScheduledFor:  &due,
		Revision:      1,
		UpdatedAt:     due,
	}))

	return staged
}

// holdToken writes a paired Beta Portal pairing at path, holding a token.
func holdToken(t *testing.T, path string) {
	t.Helper()

	_, err := betaportal.Open(path)
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(content, &doc))
	doc["state"], doc["access_token"] = "paired", "opaque-access-token"
	content, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o600))
}
