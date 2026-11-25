package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// An executable an installer put down is replaced by the installer, never by
// the server, so the Windows service keeps the build the installer recorded.
func TestTheInstallerMarkerHandsTheExecutableToTheInstaller(t *testing.T) {
	t.Parallel()

	if buildprofile.Name == "printer" {
		t.Skip("the printer operating system owns the executable before any marker is read")
	}

	cases := []struct {
		name   string
		marker bool
	}{
		{name: "with the marker the installer owns it", marker: true},
		{name: "without the marker the installer does not", marker: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			folder := t.TempDir()
			binary := filepath.Join(folder, "prusa-connect.exe")
			if tc.marker {
				require.NoError(t, os.WriteFile(filepath.Join(folder, installerMarker), nil, 0o600))
			}

			external, reason := externallyManaged(binary)

			if tc.marker {
				require.True(t, external)
				require.Equal(t, installerReason, reason)

				return
			}

			require.NotEqual(t, installerReason, reason)
		})
	}
}
