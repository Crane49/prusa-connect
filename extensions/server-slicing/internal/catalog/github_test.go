package catalog_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// recorded is a few releases as the GitHub Releases API answered them, with the
// fields this module reads.
func recorded(t *testing.T) []catalog.GitHubRelease {
	t.Helper()

	data, err := os.ReadFile("../../testdata/github-releases.json")
	if err != nil {
		t.Fatal(err)
	}

	var releases []catalog.GitHubRelease
	if err = json.Unmarshal(data, &releases); err != nil {
		t.Fatal(err)
	}

	return releases
}

func versionsOf(candidates []catalog.Candidate) string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Version.String())
	}

	return strings.Join(out, " ")
}

func TestTheReleasesAreReadForEachHost(t *testing.T) {
	t.Parallel()

	releases := recorded(t)

	cases := map[string]struct {
		offered, skipped string
		archive          catalog.Archive
	}{
		"darwin/arm64":  {"3.0.0-alpha12 2.9.6 2.9.6-rc1 2.9.5 2.9.2 2.8.1 2.7.4 2.7.0-alpha1", "", catalog.ArchiveDMG},
		"windows/amd64": {"3.0.0-alpha12 2.9.6 2.9.6-rc1 2.9.5 2.9.2 2.8.1 2.7.4 2.7.0-alpha1", "", catalog.ArchiveZip},
		"linux/amd64":   {"2.8.1 2.7.4 2.7.0-alpha1", "3.0.0-alpha12 2.9.6 2.9.6-rc1 2.9.5 2.9.2", ""},
		"linux/arm64": {
			"2.7.4 2.7.0-alpha1",
			"3.0.0-alpha12 2.9.6 2.9.6-rc1 2.9.5 2.9.2 2.8.1",
			catalog.ArchiveTarBz2,
		},
	}

	for platform, want := range cases {
		t.Run(platform, func(t *testing.T) {
			t.Parallel()

			goos, goarch, _ := strings.Cut(platform, "/")
			offered, skipped := catalog.ReadGitHub(releases, goos, goarch)

			if got := versionsOf(offered); got != want.offered {
				t.Errorf("offered %s, want %s", got, want.offered)
			}

			var names []string
			for _, held := range skipped {
				names = append(names, held.Version.String())

				if !strings.Contains(held.Reason, platform) {
					t.Errorf("%s is skipped for %q", held.Version, held.Reason)
				}
			}

			if got := strings.Join(names, " "); got != want.skipped {
				t.Errorf("skipped %s, want %s", got, want.skipped)
			}

			if want.archive != "" && offered[0].Build.Archive != want.archive {
				t.Errorf("the newest build is a %s, want a %s", offered[0].Build.Archive, want.archive)
			}
		})
	}
}

// A release since mid-2025 carries the digest GitHub computed, and an older
// one carries none, so its install needs an administrator to say yes.
func TestTheDigestGitHubPublishesIsTheDigestChecked(t *testing.T) {
	t.Parallel()

	offered, _ := catalog.ReadGitHub(recorded(t), "darwin", "arm64")

	for _, candidate := range offered {
		switch candidate.Version.String() {
		case "2.9.6":
			if candidate.Build.SHA256 != "94fd7b8a9f87c9631e1c71739b15b184fc5f4c0ceabd69072f1c78f229a4fe40" ||
				!candidate.Verified() || candidate.Channel != catalog.Stable || candidate.Released != "2026-06-25" ||
				!strings.HasSuffix(candidate.Notes, "/tag/version_2.9.6") {
				t.Errorf("2.9.6 reads as %+v", candidate)
			}
		case "2.9.2":
			if candidate.Verified() {
				t.Error("2.9.2 has no digest on GitHub and reads as verified")
			}
		case "3.0.0-alpha12", "2.9.6-rc1":
			if candidate.Channel != catalog.Prerelease {
				t.Errorf("%s reads as %s", candidate.Version, candidate.Channel)
			}
		}
	}
}

// On Linux a tar.bz2 is taken before an AppImage, and an AppImage runs with
// --appimage-extract-and-run, so a host with no FUSE runs it.
func TestALinuxBuildPrefersAnArchiveToAnAppImage(t *testing.T) {
	t.Parallel()

	offered, _ := catalog.ReadGitHub(recorded(t), "linux", "amd64")

	for _, candidate := range offered {
		name := candidate.Build.URL[strings.LastIndex(candidate.Build.URL, "/")+1:]

		switch candidate.Version.String() {
		case "2.7.4":
			if candidate.Build.Archive != catalog.ArchiveTarBz2 || !strings.Contains(name, "GTK3") {
				t.Errorf("2.7.4 takes %s", name)
			}
		case "2.8.1":
			if candidate.Build.Archive != catalog.ArchiveFile || !strings.Contains(name, "newer-distros") ||
				len(candidate.Build.Args) != 1 {
				t.Errorf("2.8.1 takes %s with %v", name, candidate.Build.Args)
			}
		}
	}
}

func TestTheSupportedRange(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"2.6.1": false, "2.7.0-alpha1": true, "2.7.0": true, "2.9.6": true, "3.0.0-alpha12": true, "3.9.9": true, "4.0.0-alpha1": false,
	} {
		version, err := catalog.ParseVersion(text)
		if err != nil {
			t.Fatal(err)
		}

		if catalog.Supported(version) != want {
			t.Errorf("%s is supported: %v, want %v", text, !want, want)
		}
	}
}

// A draft and a release outside the range are not offered.
func TestADraftAndAReleaseOutsideTheRangeAreLeftOut(t *testing.T) {
	t.Parallel()

	asset := []catalog.GitHubAsset{
		{
			Name: "PrusaSlicer-x.dmg",
			Size: 1,
			URL:  "https://github.com/prusa3d/PrusaSlicer/releases/download/x/PrusaSlicer-x.dmg",
		},
	}

	offered, skipped := catalog.ReadGitHub([]catalog.GitHubRelease{
		{TagName: "version_4.0.0", Assets: asset},
		{TagName: "version_2.9.9", Draft: true, Assets: asset},
		{
			TagName: "version_2.9.8",
			Assets:  []catalog.GitHubAsset{{Name: "PrusaSlicer-x.dmg", URL: "https://example.com/x.dmg"}},
		},
	}, "darwin", "arm64")

	if len(offered) != 0 || len(skipped) != 1 {
		t.Fatalf("offered %v and skipped %v", offered, skipped)
	}
}
