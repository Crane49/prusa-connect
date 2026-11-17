package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

func TestTheMarkerStartsSafeModeOnce(t *testing.T) {
	t.Parallel()

	overlay := filepath.Join(t.TempDir(), "config.d", "resources.json")
	write(t, safeModeMarker(overlay), `{"reason":"restart","at":"2026-10-30T10:00:00Z"}`)
	now := time.Now()

	first := takeSafeMode(t.Context(), discard(), overlay, false, now)
	require.True(t, first.on())
	require.Equal(t, "restart", first.Reason)
	require.Equal(t, now, first.Since)
	require.NoFileExists(t, safeModeMarker(overlay))

	require.False(t, takeSafeMode(t.Context(), discard(), overlay, false, now).on())
}

func TestTheFlagStartsSafeModeWithoutAMarker(t *testing.T) {
	t.Parallel()

	overlay := filepath.Join(t.TempDir(), "resources.json")

	mode := takeSafeMode(t.Context(), discard(), overlay, true, time.Now())
	require.True(t, mode.on())
	require.Equal(t, "flag", mode.Reason)
}

func TestNoMarkerAndNoFlagIsNormal(t *testing.T) {
	t.Parallel()

	overlay := filepath.Join(t.TempDir(), "resources.json")
	require.False(t, takeSafeMode(t.Context(), discard(), overlay, false, time.Now()).on())
}

// TestAMalformedMarkerStillStartsSafeMode: the operator asked for safe mode,
// and a server that ignored a damaged request could be the one they cannot
// reach.
func TestAMalformedMarkerStillStartsSafeMode(t *testing.T) {
	t.Parallel()

	overlay := filepath.Join(t.TempDir(), "resources.json")
	write(t, safeModeMarker(overlay), `{`)

	mode := takeSafeMode(t.Context(), discard(), overlay, false, time.Now())
	require.True(t, mode.on())
	require.Equal(t, "restart", mode.Reason)
	require.NoFileExists(t, safeModeMarker(overlay))
}

// TestARestartRequestedDuringAWedgeStillRestarts holds ADR-20261030-7e2 rules
// 23 and 24: a module that hangs on stop does not hold an asked-for restart,
// and an upgrade or a restore still needs the clean stop.
func TestARestartRequestedDuringAWedgeStillRestarts(t *testing.T) {
	t.Parallel()

	wedged := errors.Join(nil, ErrPlaneWedged)
	broken := errors.New("listener failed")

	for _, step := range []struct {
		cause, err, want error
	}{
		{ErrRestart, ErrPlaneWedged, ErrRestart},
		{ErrRestart, wedged, ErrRestart},
		{ErrRestart, nil, ErrRestart},
		{ErrRestart, broken, broken},
		{ErrRestart, fmt.Errorf("drain the api listener: %w", context.DeadlineExceeded), ErrRestart},
		// A wedge with a serve error beside it still restarts: the error is
		// in the WARN line, and the operator asked for the restart.
		{ErrRestart, errors.Join(broken, ErrPlaneWedged), ErrRestart},
		{nil, ErrPlaneWedged, ErrPlaneWedged},
		{nil, nil, nil},
		{context.Canceled, nil, nil},
		{ErrUpgradeApply, ErrPlaneWedged, ErrPlaneWedged},
		{ErrUpgradeApply, nil, ErrUpgradeApply},
		{ErrBackupRestore, ErrPlaneWedged, ErrPlaneWedged},
		{ErrBackupRestore, nil, ErrBackupRestore},
	} {
		require.ErrorIs(t, afterServe(step.cause, step.err), step.want, "%v, %v", step.cause, step.err)

		if step.want == nil {
			require.NoError(t, afterServe(step.cause, step.err))
		}
	}
}

// TestSafeModeHoldsEveryModuleOffAndRefusesTheCatalogue runs a server in safe
// mode, entered by the flag and by the marker: the marker is gone, the store
// lists the installed module under SafeMode, and the catalogue answers
// SAFE_MODE.
func TestSafeModeHoldsEveryModuleOffAndRefusesTheCatalogue(t *testing.T) {
	t.Parallel()

	for _, flag := range []bool{true, false} {
		t.Run(map[bool]string{true: "flag", false: "marker"}[flag], func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "config.d", "resources.json"), `{
  "version": 1,
  "instances": [
    {"kind": "asset", "name": "spools", "type": "com.github.aux.acme.spools/spools", "runtime": "asset",
     "enabled": true, "required": false,
     "asset": {"path": "`+filepath.Join(dir, "spools")+`", "checksum": "sha256:`+strings.Repeat("0", 64)+`"}}
  ]
}
`)
			if !flag {
				write(t, filepath.Join(dir, "config.d", safeModeFile), `{"reason":"restart"}`)
			}

			write(t, filepath.Join(dir, "config.yaml"), lines("core:", "  log:", "    level: error"))
			recordOpenSetup(t, dir)

			running := launchOptions(t, Options{
				ConfigPath:   filepath.Join(dir, "config.yaml"),
				APIAddr:      freeAddr(t),
				Version:      "test",
				LoopbackOnly: true,
				SafeMode:     flag,
			})
			waitUntilReady(t, running)

			require.NoFileExists(t, filepath.Join(dir, "config.d", safeModeFile))

			read := running.get(t, "/api/v1/resources/asset/spools")
			require.Equal(t, http.StatusOK, read.status, read.body)

			var listed httpapi.ResourceInstance
			require.NoError(t, json.Unmarshal([]byte(read.body), &listed), read.body)
			require.Equal(t, "disabled", listed.Status.State, read.body)
			require.Len(t, listed.Status.Conditions, 1, read.body)
			require.Equal(t, "SafeMode", listed.Status.Conditions[0].Reason)

			market := running.get(t, "/api/v1/modules/marketplace")
			require.Equal(t, http.StatusConflict, market.status, market.body)
			require.Contains(t, market.body, "SAFE_MODE")

			running.stopAndWait(t)
		})
	}
}
