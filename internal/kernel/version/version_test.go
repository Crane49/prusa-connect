package version

import (
	"strings"
	"testing"
)

// TestTheStampWinsOverTheFile covers the two builds that exist: a release,
// which the linker stamps, and a plain `go build`, which reports the VERSION
// file. The cases share the package variable, so they do not run in parallel.
func TestTheStampWinsOverTheFile(t *testing.T) {
	file := strings.TrimSpace(raw)
	commit := "0.1.0+3835f5f6e0a1"

	cases := []struct {
		name    string
		stamp   string
		version string
		stamped string
	}{
		{name: "no stamp", stamp: "", version: file, stamped: ""},
		{name: "a tag with its v", stamp: "v0.2.0", version: "0.2.0", stamped: "0.2.0"},
		{name: "a commit build", stamp: commit, version: commit, stamped: commit},
		{name: "whitespace only", stamp: "  \n", version: file, stamped: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := stamped
			stamped = tc.stamp

			t.Cleanup(func() { stamped = previous })

			if got := Version(); got != tc.version {
				t.Errorf("Version() = %q, want %q", got, tc.version)
			}

			if got := Stamped(); got != tc.stamped {
				t.Errorf("Stamped() = %q, want %q", got, tc.stamped)
			}
		})
	}
}

func TestTheDocumentationNamesTheVersionItWasBuiltFor(t *testing.T) {
	previousDocs, previousStamp := docsURL, stamped
	t.Cleanup(func() { docsURL, stamped = previousDocs, previousStamp })

	stamped = "v1.2.3"

	docsURL = "https://docs.example.org/{version}"
	if got := Documentation(); got != "https://docs.example.org/1.2.3/" {
		t.Errorf("Documentation() = %q", got)
	}

	docsURL = "https://beta.example.org/prusa-connect/docs/"
	if got := Documentation(); got != "https://beta.example.org/prusa-connect/docs/" {
		t.Errorf("Documentation() = %q", got)
	}
}
