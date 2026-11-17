package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

func TestVersionsAreOrderedNewestLast(t *testing.T) {
	t.Parallel()

	ordered := []string{"2.9.5", "2.9.6-beta1", "2.9.6-rc1", "2.9.6", "3.0.0-alpha2", "3.0.0-alpha12", "3.0.0"}

	for index := 1; index < len(ordered); index++ {
		older, err := catalog.ParseVersion(ordered[index-1])
		if err != nil {
			t.Fatal(err)
		}

		newer, err := catalog.ParseVersion(ordered[index])
		if err != nil {
			t.Fatal(err)
		}

		if older.Compare(newer) >= 0 || newer.Compare(older) <= 0 {
			t.Errorf("%s is not older than %s", older, newer)
		}
	}
}

func TestAVersionThatIsNotOneIsRefused(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "2.9", "2.9.x", "2.9.6-al pha", "1.2.3.4", "-1.0.0"} {
		if _, err := catalog.ParseVersion(text); !errors.Is(err, catalog.ErrVersion) {
			t.Errorf("%q was read as a version", text)
		}
	}

	held, err := catalog.ParseVersion("version_2.9.6")
	if err != nil || held.String() != "2.9.6" {
		t.Fatalf("the release tag read as %v, %v", held, err)
	}
}

// A build the operator lists replaces the GitHub one of the same version, and
// adds a platform GitHub does not cover.
func TestTheOperatorsBuildReplacesTheGitHubOne(t *testing.T) {
	t.Parallel()

	operator, err := catalog.Parse([]byte(`{"schema":1,"releases":[{"version":"2.9.6","channel":"stable","builds":[
		{"os":"linux","arch":"arm64","url":"https://builds.example.com/ps-2.9.6-linux-arm64.tar.gz",
		 "sha256":"` + strings.Repeat("ab", 32) + `","archive":"tar.gz","binary":"prusa-slicer/bin/prusa-slicer"},
		{"os":"darwin","arch":"arm64","url":"https://builds.example.com/ps-2.9.6-cli.tar.gz",
		 "sha256":"` + strings.Repeat("cd", 32) + `","archive":"tar.gz","binary":"bin/prusa-slicer"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}

	// GitHub has no 2.9.6 for linux/arm64, and the operator's build adds it.
	github, _ := catalog.ReadGitHub(recorded(t), "linux", "arm64")

	linux := catalog.Candidates("linux", "arm64", github, operator)
	if len(linux) != len(github)+1 || linux[0].Version.String() != "2.9.6" || linux[0].Source != catalog.FromOperator {
		t.Fatalf("linux/arm64 offers %+v", linux)
	}

	github, _ = catalog.ReadGitHub(recorded(t), "darwin", "arm64")

	mac := catalog.Candidates("darwin", "arm64", github, operator)
	for _, candidate := range mac {
		if candidate.Version.String() == "2.9.6" && candidate.Source != catalog.FromOperator {
			t.Fatalf("2.9.6 on darwin/arm64 still comes from %s", candidate.Source)
		}
	}
}

func TestAManifestWithOneWrongEntryIsRefusedWhole(t *testing.T) {
	t.Parallel()

	good := `{"os":"linux","arch":"amd64","url":"https://x.example/a.tar.gz","sha256":"` +
		strings.Repeat("0", 64) + `","archive":"tar.gz","binary":"bin/prusa-slicer"}`

	cases := map[string]string{
		"plain http":         strings.Replace(good, "https://", "http://", 1),
		"a short digest":     strings.Replace(good, strings.Repeat("0", 64), "00", 1),
		"an upper digest":    strings.Replace(good, strings.Repeat("0", 64), strings.Repeat("A", 64), 1),
		"a binary outside":   strings.Replace(good, "bin/prusa-slicer", "../bin/prusa-slicer", 1),
		"an absolute binary": strings.Replace(good, "bin/prusa-slicer", "/usr/bin/prusa-slicer", 1),
		"a dmg on linux":     strings.Replace(good, `"tar.gz"`, `"dmg"`, 1),
		"an unknown archive": strings.Replace(good, `"tar.gz"`, `"rar"`, 1),
		"an unknown os":      strings.Replace(good, `"linux"`, `"plan9"`, 1),
		"an argument":        strings.Replace(good, `"binary"`, `"args":["; rm -rf /"],"binary"`, 1),
		"an unknown field":   strings.Replace(good, `"binary"`, `"script":"x","binary"`, 1),
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := `{"schema":1,"releases":[{"version":"2.9.6","channel":"stable","builds":[` + good + `]},` +
				`{"version":"2.9.5","channel":"stable","builds":[` + build + `]}]}`

			if _, err := catalog.Parse([]byte(document)); !errors.Is(err, catalog.ErrManifest) {
				t.Fatalf("the manifest was accepted: %v", err)
			}
		})
	}

	if _, err := catalog.Parse(
		[]byte(`{"schema":1,"releases":[{"version":"3.0.0-alpha12","channel":"stable","builds":[]}]}`),
	); err == nil {
		t.Fatal("a prerelease was accepted as stable")
	}

	if _, err := catalog.Parse([]byte(`{"schema":2,"releases":[]}`)); err == nil {
		t.Fatal("a schema this module does not read was accepted")
	}
}

func TestTheReleaseDayAndNotesAreHeldToTheirShape(t *testing.T) {
	t.Parallel()

	for _, release := range []string{`"released":"26.6.2026"`, `"notes":"http://example.com/notes"`} {
		document := `{"schema":1,"releases":[{"version":"2.9.6","channel":"stable",` + release + `,"builds":[]}]}`
		if _, err := catalog.Parse([]byte(document)); !errors.Is(err, catalog.ErrManifest) {
			t.Errorf("%s was accepted", release)
		}
	}

	github, _ := catalog.ReadGitHub(recorded(t), "darwin", "arm64")

	for _, candidate := range catalog.Candidates("darwin", "arm64", github, catalog.Manifest{}) {
		if candidate.Released == "" ||
			!strings.HasPrefix(candidate.Notes, "https://github.com/prusa3d/PrusaSlicer/releases/tag/") {
			t.Errorf("%s carries no day or notes: %+v", candidate.Version, candidate)
		}
	}
}
