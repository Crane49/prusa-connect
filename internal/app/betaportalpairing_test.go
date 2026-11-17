package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// TestAnUpgradedServiceKeepsItsBetaPortalPairing is the systemd service, which
// reads its file from /etc and keeps its overlay below its state directory.
// Release 0.0.1-alpha.7 kept the pairing in config.d beside the file, and the
// server keeps using it there rather than pairing again.
func TestAnUpgradedServiceKeepsItsBetaPortalPairing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	overlay := filepath.Join(root, "state", "resources.json")

	write(t, filepath.Join(etc, "config.yaml"), lines(
		"core:",
		"  log:",
		"    level: error",
		"  overlay: "+slashed(overlay),
	))
	recordOpenSetup(t, etc)

	earlier := filepath.Join(etc, "config.d", "beta-portal-pairing.json")
	paired := pairingFile(t, earlier)

	running := launch(t, etc)
	waitUntilReady(t, running)

	require.Equal(t, paired, readFile(t, earlier), "the server made a new pairing instead of keeping the earlier one")
	_, err := os.Stat(betaportal.Path(overlay))
	require.ErrorIs(t, err, os.ErrNotExist, "the server made a second pairing beside the overlay")

	running.stopAndWait(t)
}

// TestAnUnusableEarlierPairingIsLeftAndNamed is a pairing in config.d the
// server cannot open. The start goes on with a pairing beside the overlay and
// leaves the earlier file as it is. Once that file is usable again, the server
// uses it and names the one beside the overlay it no longer uses.
func TestAnUnusableEarlierPairingIsLeftAndNamed(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a file mode does not stop this process here")
	}

	etc, state := t.TempDir(), t.TempDir()
	core := config.Core{Overlay: filepath.Join(state, "resources.json")}
	configPath := filepath.Join(etc, "config.yaml")
	earlier := filepath.Join(etc, "config.d", "beta-portal-pairing.json")
	paired := pairingFile(t, earlier)
	require.NoError(t, os.Chmod(earlier, 0))

	var logged bytes.Buffer
	_, err := openPairing(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, configPath)
	require.NoError(t, err)
	require.Contains(t, logged.String(), "beside the configuration file cannot be read")
	beside := readFile(t, betaportal.Path(core.Overlay))

	require.NoError(t, os.Chmod(earlier, 0o600))
	require.Equal(t, paired, readFile(t, earlier), "the earlier pairing was changed")

	logged.Reset()
	_, err = openPairing(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, configPath)
	require.NoError(t, err)
	require.Contains(t, logged.String(), "a Beta Portal pairing beside the overlay is not used")
	require.Equal(t, paired, readFile(t, earlier))
	require.Equal(t, beside, readFile(t, betaportal.Path(core.Overlay)), "the unused pairing was changed")
}

// pairingFile writes a new Beta Portal pairing to path.
func pairingFile(t *testing.T, path string) []byte {
	t.Helper()

	_, err := betaportal.Open(path)
	require.NoError(t, err)

	return readFile(t, path)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return content
}

// TestTheServerAndTheBackupNameOnePairing is an earlier pairing in config.d
// that the server can or cannot use. Whichever file the server opens, the
// backup protects that file and no other.
func TestTheServerAndTheBackupNameOnePairing(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a file mode does not stop this process here")
	}

	cases := map[string]struct {
		prepare     func(t *testing.T, earlier string) []byte
		usesEarlier bool
	}{
		"an unreadable earlier pairing": {prepare: func(t *testing.T, earlier string) []byte {
			t.Helper()
			content := pairingFile(t, earlier)
			require.NoError(t, os.Chmod(earlier, 0))

			return content
		}},
		"a damaged earlier pairing": {prepare: func(t *testing.T, earlier string) []byte {
			t.Helper()
			write(t, earlier, "not a pairing")

			return []byte("not a pairing")
		}},
		"a damaged earlier pairing in a read-only folder": {prepare: func(t *testing.T, earlier string) []byte {
			t.Helper()
			write(t, earlier, "not a pairing")
			readOnly(t, filepath.Dir(earlier))

			return []byte("not a pairing")
		}},
		"a usable earlier pairing in a read-only folder": {
			usesEarlier: true,
			prepare: func(t *testing.T, earlier string) []byte {
				t.Helper()
				content := pairingFile(t, earlier)
				readOnly(t, filepath.Dir(earlier))

				return content
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			configPath := filepath.Join(root, "etc", "config.yaml")
			overlay := filepath.Join(root, "state", "resources.json")
			write(t, configPath, lines("core:", "  overlay: "+slashed(overlay)))
			core, err := config.Load(configPath)
			require.NoError(t, err)
			earlier, beside := betaportal.EarlierPath(configPath), betaportal.Path(overlay)
			before := tc.prepare(t, earlier)

			_, err = openPairing(t.Context(), slog.New(slog.DiscardHandler), core, configPath)
			require.NoError(t, err)

			used := beside
			if tc.usesEarlier {
				used = earlier
			}
			plan, err := backupPlan(configPath, core, "2.0.0", upgrade.PostgresNone, true)
			require.NoError(t, err)
			for _, source := range plan.Files {
				if source.Name == "beta-portal-pairing" {
					require.Equal(t, used, source.Path, "the backup protects a pairing the server does not use")
				}
			}

			if tc.usesEarlier {
				_, err = os.Stat(beside)
				require.ErrorIs(t, err, os.ErrNotExist, "the server opened a pairing beside the overlay")
			} else {
				_, err = os.Stat(beside)
				require.NoError(t, err, "the server opened no pairing beside the overlay")
				require.NoError(t, os.Chmod(earlier, 0o600))
				require.Equal(t, before, readFile(t, earlier), "the earlier pairing was changed")
			}
		})
	}
}

// readOnly makes folder read-only until the test ends.
func readOnly(t *testing.T, folder string) {
	t.Helper()

	require.NoError(t, os.Chmod(folder, 0o500))
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
}

// TestAnUnusedPairingWithATokenIsEndedInThePortalFirst is a pairing beside the
// overlay that still holds an access token while the server uses the earlier
// one. Disconnect acts on the pairing in use, so the log says to end the
// unused one in the Beta Portal before the file is deleted.
func TestAnUnusedPairingWithATokenIsEndedInThePortalFirst(t *testing.T) {
	t.Parallel()

	etc, state := t.TempDir(), t.TempDir()
	core := config.Core{Overlay: filepath.Join(state, "resources.json")}
	configPath := filepath.Join(etc, "config.yaml")
	pairingFile(t, betaportal.EarlierPath(configPath))
	unused := betaportal.Path(core.Overlay)
	holdToken(t, unused)

	var logged bytes.Buffer
	_, err := openPairing(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, configPath)
	require.NoError(t, err)
	require.Contains(t, logged.String(), "it still holds an access token; remove it in the Beta Portal")
	require.Contains(t, logged.String(), "file="+unused)
}

// TestAnUnusedPairingThatCannotBeReadIsNamedSo is a damaged pairing beside
// the overlay while the server uses the earlier one. It may still hold a
// token, and the log says that rather than that it holds one.
func TestAnUnusedPairingThatCannotBeReadIsNamedSo(t *testing.T) {
	t.Parallel()

	etc, state := t.TempDir(), t.TempDir()
	core := config.Core{Overlay: filepath.Join(state, "resources.json")}
	configPath := filepath.Join(etc, "config.yaml")
	pairingFile(t, betaportal.EarlierPath(configPath))
	write(t, betaportal.Path(core.Overlay), "not a pairing")

	var logged bytes.Buffer
	_, err := openPairing(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)), core, configPath)
	require.NoError(t, err)
	require.Contains(t, logged.String(), "it cannot be read and may still hold an access token")
	require.NotContains(t, logged.String(), "it still holds an access token")
}

// TestAPassedOverPairingWithATokenIsNamedSo is the message for an earlier
// pairing the server passes over: one it cannot protect, as on a read-only
// file system, and one that is damaged. A file that still holds an access
// token says so.
func TestAPassedOverPairingWithATokenIsNamedSo(t *testing.T) {
	t.Parallel()

	readOnly := passedOverMessage(fmt.Errorf("%w: %w", betaportal.ErrNotProtected, syscall.EROFS), true)
	require.Contains(t, readOnly, "cannot be set to mode 0600")
	require.Contains(t, readOnly, "the file still holds an access token")

	unreadable := passedOverMessage(fmt.Errorf("read: %w", fs.ErrPermission), false)
	require.Contains(t, unreadable, "make the file readable by the server")

	damaged := passedOverMessage(errors.New("decode the Beta Portal pairing: unexpected end of JSON input"), false)
	require.Contains(t, damaged, "is damaged and not used")
	require.NotContains(t, damaged, "access token")
}
