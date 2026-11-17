package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func restartingMaintenance(t *testing.T) (*maintenance, string, *int) {
	t.Helper()

	overlay := filepath.Join(t.TempDir(), "overlay.yaml")
	restarts := 0

	return &maintenance{
		core:           config.Core{Overlay: overlay},
		requestRestart: func() { restarts++ },
		logger:         discard(),
	}, safeModeMarker(overlay), &restarts
}

func TestARestartInSafeModeWritesTheMarkerAndANormalOneRemovesIt(t *testing.T) {
	t.Parallel()

	m, marker, restarts := restartingMaintenance(t)

	require.NoError(
		t,
		m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe, Principal: "u1"}),
	)
	require.Equal(t, 1, *restarts)

	taken := takeSafeMode(context.Background(), discard(), m.core.Overlay, false, time.Now())
	require.Equal(t, "restart", taken.Reason)

	require.NoFileExists(t, marker)

	require.NoError(t, m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe}))
	require.FileExists(t, marker)
	require.NotContains(t, markerText(t, marker), "u1")

	require.NoError(t, m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartNormal}))
	require.NoFileExists(t, marker)
	require.Equal(t, 3, *restarts)
}

func TestANormalRestartRefusesWhileTheMarkerStays(t *testing.T) {
	t.Parallel()

	m, marker, restarts := restartingMaintenance(t)
	require.NoError(t, os.MkdirAll(filepath.Join(marker, "held"), 0o700))

	require.Error(t, m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartNormal}))
	require.Zero(t, *restarts)
}

func TestAppSettingsReportSafeMode(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	require.Nil(t, withSafeMode(httpapi.AppSettings{}, safeMode{}).SafeMode)

	got := withSafeMode(httpapi.AppSettings{}, safeMode{Reason: "restart", Since: since}).SafeMode
	require.Equal(t, &httpapi.SafeModeState{Reason: "restart", Since: since}, got)
}

func markerText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}

func TestARestartIsRefusedWhileAnUpgradeIsScheduledOrRunning(t *testing.T) {
	t.Parallel()

	for _, state := range []upgrade.State{upgrade.StateScheduled, upgrade.StateTrial} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			m, marker, restarts := restartingMaintenance(t)
			m.store = upgrade.NewStore(filepath.Join(t.TempDir(), "journal.json"))
			require.NoError(t, m.store.Save(upgrade.Journal{
				Schema:        1,
				ID:            "upgrade-1",
				State:         state,
				FromVersion:   "1.0.0",
				ToVersion:     "1.1.0",
				StagedBinary:  "/s",
				CurrentBinary: "/c",
				Revision:      1,
				UpdatedAt:     time.Now(),
			}))

			err := m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe})
			require.ErrorIs(t, err, upgrade.ErrUpgradeBusy)
			require.NoFileExists(t, marker)
			require.Zero(t, *restarts)
		})
	}
}

func TestARestartWithoutAHookLeavesNoMarker(t *testing.T) {
	t.Parallel()

	m, marker, _ := restartingMaintenance(t)
	m.requestRestart = nil

	require.Error(t, m.Restart(context.Background(), httpapi.MaintenanceRestart{Mode: httpapi.RestartSafe}))
	require.NoFileExists(t, marker)
}
