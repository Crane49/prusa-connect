// Package catalog says which PrusaSlicer builds this module may download for
// this host.
//
// There are two lists. One is read from the GitHub Releases API of
// prusa3d/PrusaSlicer: every release in the range of versions this module
// supports, with the asset this host can unpack. The other is a manifest an
// operator hosts: a JSON document naming each build's version, platform,
// address, SHA-256 and executable. A build in the operator's manifest takes the
// place of the GitHub one of the same version. ADR-20261026-ydf holds the
// rules.
package catalog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

// Schema is the manifest format this module reads.
const Schema = 1

// MaxManifestBytes bounds a manifest. A list of every release ever made fits
// in a small part of it.
const MaxManifestBytes = 1 << 20

// MaxDownloadBytes bounds one download. The largest public PrusaSlicer
// download is a few hundred megabytes.
const MaxDownloadBytes int64 = 2 << 30

// Archive is how a build is packed.
type Archive string

// The ways a build may be packed.
const (
	// ArchiveZip is a zip file. The Windows builds are packed this way.
	ArchiveZip Archive = "zip"
	// ArchiveTarGz is a gzip-compressed tar file, the usual way to pack a
	// build for Linux.
	ArchiveTarGz Archive = "tar.gz"
	// ArchiveDMG is a macOS disk image. Only a macOS host opens one.
	ArchiveDMG Archive = "dmg"
	// ArchiveTarBz2 is a bzip2-compressed tar file. The Linux arm64 builds
	// of PrusaSlicer 2.7 are packed this way.
	ArchiveTarBz2 Archive = "tar.bz2"
	// ArchiveFile is the executable itself, such as an AppImage or a single
	// static binary.
	ArchiveFile Archive = "file"
)

// Channel says whether a release is final.
type Channel string

// The channels of a release.
const (
	Stable     Channel = "stable"
	Prerelease Channel = "prerelease"
)

// Build is one download of one release, for one operating system and one
// processor.
type Build struct {
	// OS and Arch are Go's names: linux, darwin or windows, and amd64 or
	// arm64.
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// URL is where the download is. It is an https address.
	URL string `json:"url"`
	// SHA256 is the digest of the download, lowercase hex. A download that
	// does not match it is thrown away.
	SHA256 string `json:"sha256"`
	// Size is the length of the download in bytes, or zero when the manifest
	// does not say.
	Size    int64   `json:"size,omitempty"`
	Archive Archive `json:"archive"`
	// Binary is the path of the executable inside the archive, with "/" as
	// the separator. An ArchiveFile build leaves it empty.
	Binary string `json:"binary,omitempty"`
	// Args are put before every other argument when the executable runs, such
	// as --appimage-extract-and-run for an AppImage on a host with no FUSE.
	Args []string `json:"args,omitempty"`
}

// Release is one version of PrusaSlicer and its builds.
type Release struct {
	Version string  `json:"version"`
	Channel Channel `json:"channel"`
	// Released is the day the release was published, YYYY-MM-DD, and empty
	// when the manifest does not say.
	Released string `json:"released,omitempty"`
	// Notes is an https address of the release's notes, and empty when the
	// manifest names none.
	Notes  string  `json:"notes,omitempty"`
	Builds []Build `json:"builds"`
}

// Manifest is one list of releases.
type Manifest struct {
	Schema   int       `json:"schema"`
	Releases []Release `json:"releases"`
}

// ErrManifest reports a manifest this module refuses. Nothing in it is used.
var ErrManifest = errors.New("the manifest is not one this module reads")

// Parse reads one manifest and refuses it whole when one entry is wrong, so a
// list with a typing error never offers half of what its author meant.
func Parse(data []byte) (Manifest, error) {
	if len(data) > MaxManifestBytes {
		return Manifest{}, fmt.Errorf(
			"%w: it is %d bytes and the limit is %d",
			ErrManifest,
			len(data),
			MaxManifestBytes,
		)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var held Manifest
	if err := decoder.Decode(&held); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrManifest, err)
	}

	if held.Schema != Schema {
		return Manifest{}, fmt.Errorf("%w: schema %d, and this module reads %d", ErrManifest, held.Schema, Schema)
	}

	seen := map[string]bool{}

	for _, release := range held.Releases {
		if err := checkRelease(release, seen); err != nil {
			return Manifest{}, fmt.Errorf("%w: %w", ErrManifest, err)
		}
	}

	return held, nil
}

func checkRelease(release Release, seen map[string]bool) error {
	version, err := ParseVersion(release.Version)
	if err != nil {
		return err
	}

	switch release.Channel {
	case Stable:
		if !version.Stable() {
			return fmt.Errorf("%s is a prerelease and the manifest calls it stable", release.Version)
		}
	case Prerelease:
	default:
		return fmt.Errorf("%s names the channel %q, and the channels are stable and prerelease",
			release.Version, release.Channel)
	}

	if release.Released != "" {
		if _, err = time.Parse(time.DateOnly, release.Released); err != nil {
			return fmt.Errorf(
				"%s was released on %q, which is not a day of the form 2006-01-02",
				release.Version,
				release.Released,
			)
		}
	}

	if release.Notes != "" {
		if address, parseErr := url.Parse(
			release.Notes,
		); parseErr != nil || address.Scheme != "https" ||
			address.Host == "" {
			return fmt.Errorf(
				"the notes of %s are at %q, which is not an https address",
				release.Version,
				release.Notes,
			)
		}
	}

	for _, build := range release.Builds {
		key := version.String() + " " + build.OS + "/" + build.Arch
		if seen[key] {
			return fmt.Errorf("%s is listed twice", key)
		}

		seen[key] = true

		if err = checkBuild(build); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}

	return nil
}

func checkBuild(build Build) error {
	if !slices.Contains([]string{"linux", "darwin", "windows"}, build.OS) {
		return fmt.Errorf("the operating system %q is not linux, darwin or windows", build.OS)
	}

	if !slices.Contains([]string{"amd64", "arm64"}, build.Arch) {
		return fmt.Errorf("the processor %q is not amd64 or arm64", build.Arch)
	}

	address, err := url.Parse(build.URL)
	if err != nil || address.Scheme != "https" || address.Host == "" {
		return fmt.Errorf("the address %q is not an https address", build.URL)
	}

	if digest, decodeErr := hex.DecodeString(build.SHA256); decodeErr != nil || len(digest) != 32 ||
		strings.ToLower(build.SHA256) != build.SHA256 {
		return errors.New("the digest is not 64 lowercase hex characters")
	}

	if build.Size < 0 || build.Size > MaxDownloadBytes {
		return fmt.Errorf("the size %d is outside 0 to %d bytes", build.Size, MaxDownloadBytes)
	}

	switch build.Archive {
	case ArchiveFile:
		if build.Binary != "" {
			return errors.New("a build that is the executable itself names no binary inside it")
		}
	case ArchiveZip, ArchiveTarGz, ArchiveTarBz2, ArchiveDMG:
		if !InsidePath(build.Binary) {
			return fmt.Errorf("the binary %q is not a path inside the archive", build.Binary)
		}
	default:
		return fmt.Errorf("the archive %q is not zip, tar.gz, tar.bz2, dmg or file", build.Archive)
	}

	if build.Archive == ArchiveDMG && build.OS != "darwin" {
		return errors.New("a dmg opens only on darwin")
	}

	for _, arg := range build.Args {
		if !strings.HasPrefix(arg, "--") || strings.ContainsAny(arg, " \t\n\x00") {
			return fmt.Errorf("the argument %q is not one --flag", arg)
		}
	}

	return nil
}

// InsidePath reports a relative path that stays inside the directory it is
// read from: no leading slash, no drive, no "..", no empty segment.
func InsidePath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") {
		return false
	}

	return path.Clean(name) == name && name != "." && !strings.HasPrefix(name, "../") && name != ".."
}

// Source says which manifest a build came from.
type Source string

// The sources of a build.
const (
	// FromGitHub is the manifest this module carries, of the public GitHub
	// releases.
	FromGitHub Source = "github"
	// FromOperator is the manifest the operator named in manifest_url.
	FromOperator Source = "operator"
)

// Candidate is one build this host can run.
type Candidate struct {
	Version  Version
	Channel  Channel
	Released string
	Notes    string
	Source   Source
	Build    Build
}

// Verified reports a build whose download is checked against a SHA-256 its
// source published. A GitHub release older than mid-2025 publishes none.
func (c Candidate) Verified() bool { return c.Build.SHA256 != "" }

// Candidates returns the GitHub builds and the builds of the operator's
// manifest that run on goos and goarch, newest first. A build the operator's
// manifest lists replaces the GitHub one of the same version, so an operator
// can swap a download for one they built.
func Candidates(goos, goarch string, github []Candidate, operator Manifest) []Candidate {
	byVersion := map[string]Candidate{}

	for _, candidate := range github {
		byVersion[candidate.Version.String()] = candidate
	}

	for _, release := range operator.Releases {
		version, err := ParseVersion(release.Version)
		if err != nil {
			continue
		}

		for _, build := range release.Builds {
			if build.OS != goos || build.Arch != goarch {
				continue
			}

			byVersion[version.String()] = Candidate{
				Version: version, Channel: release.Channel, Released: release.Released, Notes: release.Notes,
				Source: FromOperator, Build: build,
			}
		}
	}

	out := make([]Candidate, 0, len(byVersion))
	for _, candidate := range byVersion {
		out = append(out, candidate)
	}

	slices.SortFunc(out, func(a, b Candidate) int { return b.Version.Compare(a.Version) })

	return out
}
