package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app"
)

// A typo in the configuration file fails the first recovery step, which reads
// the file. The error must name the key and its line, and no restore.
func TestATypoInTheConfigurationNamesNoRecovery(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("core:\n  api:\n    request_timout: 30s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := app.RecoverBackupRestore(t.Context(), path)
	if err == nil {
		t.Fatal("RecoverBackupRestore accepted a key the build does not read")
	}

	got := recoveryFailed("recover interrupted backup restore", err).Error()
	if strings.Contains(got, "recover interrupted") {
		t.Errorf("the error names a recovery for a typo: %s", got)
	}

	if !strings.Contains(got, "line 3") || !strings.Contains(got, "core.api.request_timout") {
		t.Errorf("the error does not name the key and its line: %s", got)
	}
}

func TestAFailedRecoveryKeepsItsStep(t *testing.T) {
	t.Parallel()

	cause := errors.New("disk full")

	got := recoveryFailed("recover interrupted upgrade", fmt.Errorf("apply: %w", cause))
	if !errors.Is(got, cause) || !strings.HasPrefix(got.Error(), "recover interrupted upgrade: ") {
		t.Errorf("recoveryFailed = %v", got)
	}
}
