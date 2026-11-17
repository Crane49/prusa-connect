package main

import (
	"strings"
	"testing"
)

func TestNextVersionIsAboveStableAndPrereleaseSources(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"v1.2.3", "1.2.3-beta.4"} {
		got, err := nextVersion(source, "71")
		if err != nil {
			t.Fatal(err)
		}
		if got != "1.2.4-upgrade-ci.71" {
			t.Fatalf("nextVersion(%q) = %q", source, got)
		}
	}
}

func TestNextVersionRefusesANonNumericRun(t *testing.T) {
	t.Parallel()

	_, err := nextVersion("1.2.3", "nightly")
	if err == nil || !strings.Contains(err.Error(), "not numeric") {
		t.Fatalf("error = %v", err)
	}
}
