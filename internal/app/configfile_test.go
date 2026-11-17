package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAMisplacedConfigurationIsNamed covers a person who unpacked config.yaml
// beside the binary instead of into config/. The server starts on its
// defaults either way, so the warning is the only place that says where the
// file was read from and where it is.
func TestAMisplacedConfigurationIsNamed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binaryDir := filepath.Join(root, "bin")
	beside := filepath.Join(binaryDir, configurationName)
	inConfig := filepath.Join(binaryDir, "config", configurationName)
	wanted := filepath.Join(root, "config", configurationName)

	cases := []struct {
		name    string
		present []string
		missing bool
		found   []string
	}{
		{name: "the file is where it is read", present: []string{wanted}, missing: false},
		{name: "nothing anywhere", missing: true},
		{name: "beside the binary", present: []string{beside}, missing: true, found: []string{beside}},
		{name: "in config beside the binary", present: []string{inConfig}, missing: true, found: []string{inConfig}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			there := map[string]bool{}
			for _, path := range tc.present {
				there[path] = true
			}

			exists := func(path string) bool { return there[path] }

			missing, looked, found := configurationHint(wanted, exists, binaryDir)

			if missing != tc.missing {
				t.Fatalf("missing = %v, want %v", missing, tc.missing)
			}

			if looked != wanted {
				t.Errorf("looked at %q, want %q", looked, wanted)
			}

			if len(found) != len(tc.found) || (len(found) > 0 && found[0] != tc.found[0]) {
				t.Errorf("found %v, want %v", found, tc.found)
			}
		})
	}
}

// TestFileExistsSeparatesAFileFromAFolder holds the helper to the real disk.
func TestFileExistsSeparatesAFileFromAFolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, configurationName)

	if err := os.WriteFile(file, []byte("core: {}\n"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}

	if !fileExists(file) {
		t.Errorf("%s exists and was reported missing", file)
	}

	if fileExists(dir) {
		t.Errorf("the folder %s was reported as a file", dir)
	}

	if fileExists(filepath.Join(dir, "absent.yaml")) {
		t.Errorf("an absent file was reported as there")
	}
}
