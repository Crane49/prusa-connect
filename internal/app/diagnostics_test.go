//go:build diagnostics

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
)

func TestTailFileKeepsTheNewestBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	require.Nil(t, tailFile(filepath.Join(dir, "missing"), 10))

	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	require.Empty(t, tailFile(empty, 10))

	full := filepath.Join(dir, "full")
	content := append(bytes.Repeat([]byte("a"), 90), []byte("0123456789")...)
	require.NoError(t, os.WriteFile(full, content, 0o600))
	require.Equal(t, content, tailFile(full, 1000))

	lines := filepath.Join(dir, "lines")
	require.NoError(t, os.WriteFile(lines, []byte("token=abcdef\nsecond\nthird\n"), 0o600))
	require.Equal(t, []byte("third\n"), tailFile(lines, 10), "the cut line is left out")
}

//nolint:paralleltest // debug.SetCrashOutput is process-wide.
func TestRecordCrashesCutsALongFileBackToItsTail(t *testing.T) {
	t.Cleanup(func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) })

	path := filepath.Join(t.TempDir(), "crash.log")
	old := bytes.Repeat([]byte("old crash line\n"), crashTail/8)
	require.NoError(t, os.WriteFile(path, append(old, "newest crash\n"...), 0o600))

	require.NoError(t, recordCrashes(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.LessOrEqual(t, info.Size(), int64(crashTail))

	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(kept, []byte("old crash line\nnewest crash\n")), "the newest crash is kept whole")
}

//nolint:paralleltest // debug.SetCrashOutput is process-wide.
func TestRecordCrashesOpensAWritableFile(t *testing.T) {
	t.Cleanup(func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) })

	path := filepath.Join(t.TempDir(), "nested", "crash.log")
	require.NoError(t, recordCrashes(path))
	require.FileExists(t, path)
}

// TestAnOpenInstallServesDiagnosticsOnlyWhenAsked: open access makes everyone
// on the network an admin, so the bundle there takes an explicit switch.
func TestAnOpenInstallServesDiagnosticsOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	on := true

	for name, tc := range map[string]struct {
		mode  string
		local bool
		on    *bool
		want  bool
	}{
		"open":                         {mode: resource.SetupOpen},
		"open, switched on":            {mode: resource.SetupOpen, on: &on, want: true},
		"sign-in required":             {mode: resource.SetupRequired, want: true},
		"first run not taken":          {want: true},
		"local accounts in the config": {mode: resource.SetupOpen, local: true, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core := config.Core{}
			core.Auth.Local, core.Diagnostics.Enabled = tc.local, tc.on

			d := diagnosticsBundles{core: core, mode: newSetupMode(tc.mode)}
			require.Equal(t, tc.want, d.Served())
		})
	}
}
