package catalog

import (
	"fmt"
	"slices"
	"strings"
)

// The range of PrusaSlicer versions this module slices with.
//
// The floor is 2.7.0, the first release that writes binary G-code. The module
// asks for one format or the other with --binary-gcode, which an older release
// refuses as an unknown option, and it reads the print details out of the
// printer metadata of a binary file, which only 2.7 and newer write. Every
// release in the range also writes objects_info and saves its projects in a
// 3MF that the threemf reader reads: the 2.x format, with beds laid out on a grid from
// 2.9, and the 3.0 format.
//
// The ceiling is the next major version. A PrusaSlicer 4 may change its command
// line or its project format, and this module has not seen either.
//
// The floor's numbers, and the first major version that is not offered.
const (
	minMajor   = 2
	minMinor   = 7
	BelowMajor = 4
)

// MinVersion is the oldest release offered.
//
//nolint:gochecknoglobals // a fixed version, read and never written.
var MinVersion = Version{Major: minMajor, Minor: minMinor}

// Supported reports a version inside the range. A prerelease of the floor,
// such as 2.7.0-alpha1, is inside it.
func Supported(version Version) bool {
	floor := version
	floor.Pre = ""

	return floor.Compare(MinVersion) >= 0 && version.Major < BelowMajor
}

// GitHubRelease is one release as the GitHub Releases API answers it, with the
// fields this module reads.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset is one file of a release.
type GitHubAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Digest is "sha256:<hex>", which GitHub computes for every asset
	// uploaded since mid-2025, and empty for an older one.
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

// Skipped is a release in the supported range that this host cannot install,
// and why.
type Skipped struct {
	Version  Version
	Channel  Channel
	Released string
	Notes    string
	Reason   string
}

// ReadGitHub reads the GitHub releases into the builds this host can install,
// newest first. A release in the supported range with no asset for this host
// is answered in skipped, so a page can say why it is not offered. A draft and
// a release outside the range are left out.
func ReadGitHub(releases []GitHubRelease, goos, goarch string) ([]Candidate, []Skipped) {
	var (
		offered []Candidate
		skipped []Skipped
	)

	for _, release := range releases {
		version, err := ParseVersion(release.TagName)
		if err != nil || release.Draft || !Supported(version) {
			continue
		}

		channel := Stable
		if release.Prerelease || !version.Stable() {
			channel = Prerelease
		}

		released, _, _ := strings.Cut(release.PublishedAt, "T")
		notes := ""

		if strings.HasPrefix(release.HTMLURL, "https://github.com/") {
			notes = release.HTMLURL
		}

		build, found := assetFor(release.Assets, goos, goarch)
		if !found {
			skipped = append(skipped, Skipped{
				Version: version, Channel: channel, Released: released, Notes: notes,
				Reason: fmt.Sprintf("no build for %s/%s", goos, goarch),
			})

			continue
		}

		build.OS, build.Arch = goos, goarch
		offered = append(offered, Candidate{
			Version: version, Channel: channel, Released: released, Notes: notes, Source: FromGitHub, Build: build,
		})
	}

	slices.SortFunc(offered, func(a, b Candidate) int { return b.Version.Compare(a.Version) })
	slices.SortFunc(skipped, func(a, b Skipped) int { return b.Version.Compare(a.Version) })

	return offered, skipped
}

// assetFor picks the asset this host can unpack. The executable's path inside
// the archive is not in the API, so the build names none and the install
// finds it.
//
// Every macOS disk image of the range is universal, so one serves both
// processors. On Linux the module takes a tar.bz2 before an AppImage, because
// an AppImage needs FUSE or unpacks itself on every run, and it takes GTK3
// before GTK2 and a build for newer distributions before one for older ones.
func assetFor(assets []GitHubAsset, goos, goarch string) (Build, bool) {
	var (
		best     GitHubAsset
		archive  Archive
		bestRank = -1
	)

	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if !strings.HasPrefix(name, "prusaslicer-") || strings.Contains(name, "luaapireference") ||
			!strings.HasPrefix(asset.URL, "https://github.com/") {
			continue
		}

		kind, rank, ok := classify(name, goos, goarch)
		if ok && rank > bestRank {
			best, archive, bestRank = asset, kind, rank
		}
	}

	if bestRank < 0 {
		return Build{}, false
	}

	build := Build{URL: best.URL, Size: best.Size, Archive: archive}

	if digest, ok := strings.CutPrefix(best.Digest, "sha256:"); ok && len(digest) == sha256HexBytes {
		build.SHA256 = strings.ToLower(digest)
	}

	if archive == ArchiveFile {
		build.Args = []string{"--appimage-extract-and-run"}
	}

	return build, true
}

// sha256HexBytes is the length of a SHA-256 written in hex.
const sha256HexBytes = 64

// The weights an asset name earns on Linux. A tar.bz2 outweighs every
// AppImage.
const (
	rankTarBz2       = 8
	rankGTK3         = 4
	rankNewerDistros = 2
)

// classify says whether one asset name fits a host, as which archive, and how
// much it is preferred.
func classify(name, goos, goarch string) (Archive, int, bool) {
	switch goos {
	case "darwin":
		return ArchiveDMG, 0, strings.HasSuffix(name, ".dmg")
	case "windows":
		return ArchiveZip, 0, goarch == "amd64" && strings.HasSuffix(name, ".zip")
	case "linux":
		markers := map[string]string{"amd64": "linux-x64", "arm64": "linux-arm64"}

		marker, known := markers[goarch]
		if !known || !strings.Contains(name, marker) {
			return "", 0, false
		}

		rank := 0
		if strings.Contains(name, "gtk3") {
			rank += rankGTK3
		}

		if strings.Contains(name, "newer-distros") {
			rank += rankNewerDistros
		}

		switch {
		case strings.HasSuffix(name, ".tar.bz2"):
			return ArchiveTarBz2, rank + rankTarBz2, true
		case strings.HasSuffix(name, ".appimage"):
			return ArchiveFile, rank, true
		}
	}

	return "", 0, false
}
