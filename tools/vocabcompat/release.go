package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	vocab "github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// freezeFloor is the release at which a published set stops being allowed to
// lose an entry. ADR-20260825-zkq rule 8 sets the same floor for the HTTP API, and
// ADR-20260827-qvk rule 1 reads it from the same file so one release freezes every
// published surface at once.
const freezeFloor = "1.0.0"

// deprecationWindow is how many minor releases of the application an entry
// stays published after the release that deprecated it. It is the number
// sdk/vocabulary publishes with the marker, so the gate and the sets it reads
// cannot disagree about the count.
const deprecationWindow = vocab.Window

// defaultVersion is the file carrying the release this build is, relative to
// the repository root. scripts/release.sh writes it.
const defaultVersion = "VERSION"

// version is MAJOR.MINOR.PATCH with an optional prerelease label. It is the
// shape semver 2.0.0 defines and the shape extension.Version.Parse enforces.
type version struct {
	major, minor, patch int
	prerelease          bool
	text                string
}

// parseVersion reads a semver 2.0.0 version string. Build metadata is dropped,
// because semver section 10 says it takes no part in precedence.
func parseVersion(where, text string) (version, error) {
	if text == "" {
		return version{}, fmt.Errorf(
			"%s is empty, and a published set carries a version",
			where,
		)
	}

	rest, _, _ := strings.Cut(text, "+")
	rest, label, hasLabel := strings.Cut(rest, "-")

	majorText, rest, hasMinor := strings.Cut(rest, ".")
	minorText, patchText, hasPatch := strings.Cut(rest, ".")

	if !hasMinor || !hasPatch || strings.Contains(patchText, ".") {
		return version{}, fmt.Errorf("%s reads %q, and a version is three numbers such as 1.0.0", where, text)
	}

	parsed := version{prerelease: hasLabel && label != "", text: text}

	for _, part := range []struct {
		number string
		into   *int
	}{
		{majorText, &parsed.major},
		{minorText, &parsed.minor},
		{patchText, &parsed.patch},
	} {
		value, err := strconv.Atoi(part.number)
		if err != nil || value < 0 {
			return version{}, fmt.Errorf("%s reads %q, and %q is not a version number", where, text, part.number)
		}

		*part.into = value
	}

	return parsed, nil
}

// readRelease reads VERSION. A file this gate cannot read is a usage error and
// never a pass: the release decides whether a removal refuses, so a gate that
// could not find the release has no verdict to give.
func readRelease(path string) (version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return version{}, fmt.Errorf("read the release %s: %w", path, err)
	}

	return parseVersion(path, strings.TrimSpace(string(data)))
}

// compare orders two versions, ignoring the prerelease label. It returns a
// negative number when v precedes other, zero when the triples are equal, and a
// positive number otherwise.
func (v version) compare(other version) int {
	if v.major != other.major {
		return v.major - other.major
	}

	if v.minor != other.minor {
		return v.minor - other.minor
	}

	return v.patch - other.patch
}

// frozen reports whether this release freezes every published set.
//
// A prerelease of the floor is below it, which is semver 2.0.0 section 11:
// 1.0.0-rc.1 precedes 1.0.0. So a release candidate for the first 1.0.0 still
// reports a removal, and the tag that ships refuses it.
func (v version) frozen() bool {
	floor, err := parseVersion("freezeFloor", freezeFloor)
	if err != nil {
		panic("vocabcompat: freezeFloor is not a version: " + err.Error())
	}

	if cmp := v.compare(floor); cmp != 0 {
		return cmp > 0
	}

	return !v.prerelease
}
