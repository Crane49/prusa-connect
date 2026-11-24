package main

import (
	"os"
	"strings"
	"testing"
)

// The version the process reports, the one module.yaml declares and the
// newest heading of CHANGELOG.md are one version.
func TestTheVersionIsOneVersion(t *testing.T) {
	t.Parallel()

	manifest, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(manifest), "\nversion: "+string(release)+"\n") {
		t.Errorf("module.yaml does not declare version %s", release)
	}

	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}

	for line := range strings.SplitSeq(string(changelog), "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			if version, _, _ := strings.Cut(heading, " - "); version != string(release) {
				t.Errorf("the newest heading of CHANGELOG.md is %s, and the release is %s", heading, release)
			}

			return
		}
	}

	t.Error("CHANGELOG.md has no version heading")
}
