package arch_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestEveryModuleManifestInTheTreeParses holds every module.yaml the repo
// ships against the parser, so a check made stricter cannot strand an
// example or a fixture that is already installed somewhere.
func TestEveryModuleManifestInTheTreeParses(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "*", "*", "module.yaml"))
	if err != nil {
		t.Fatalf("glob the packages and examples: %v", err)
	}

	more, err := filepath.Glob(filepath.Join(repoRoot(t), "test", "modules", "*", "module.yaml"))
	if err != nil {
		t.Fatalf("glob the test fixtures: %v", err)
	}

	for _, path := range append(paths, more...) {
		document, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}

		if _, parseErr := manifest.Parse("module.yaml", document, yaml.Unmarshal); parseErr != nil {
			t.Errorf("%s: %v", path, parseErr)
		}
	}
}
