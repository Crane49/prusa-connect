package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The freeze follows the release, so nobody has to remember it.
//
// ADR-20260825-zkq rule 8 ties the flip from reporting to frozen to the first 1.0.0.
// A rule that asks a person to edit a JSON file on the day of a release is a
// rule that is remembered until the one release where it is not, and the cost
// of forgetting is a gate that prints a breaking change and exits 0.
//
// So the release decides the verdict and policy.json records it. `frozenBy`
// reads VERSION and freezes every major at or above the floor whatever the
// file says, and `policyDisagrees` refuses a file that still reads reporting
// at such a release, so the file a person opens cannot say the opposite of
// what the gate did.

// freezeFloor is the release at which a published major version stops
// reporting a break and starts refusing it. ADR-20260825-zkq rule 8.
const freezeFloor = "1.0.0"

// defaultVersion is the file that carries the release this build is, relative
// to the repository root. scripts/release.sh writes it and it is the only copy
// this gate reads.
const defaultVersion = "VERSION"

// release is a version read from VERSION: the three numbers, and whether a
// prerelease label follows them.
type release struct {
	major, minor, patch int
	prerelease          bool
	text                string
}

// readRelease reads VERSION and parses it. A file this gate cannot read is a
// usage error and never a pass: the release decides whether a break refuses, so
// a gate that could not find the release has no verdict to give.
func readRelease(path string) (release, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return release{}, fmt.Errorf("read the release %s: %w", path, err)
	}

	return parseRelease(path, strings.TrimSpace(string(data)))
}

// parseRelease reads a semver 2.0.0 version string. Build metadata is dropped,
// because semver section 10 says it takes no part in precedence.
func parseRelease(path, text string) (release, error) {
	if text == "" {
		return release{}, fmt.Errorf("%s is empty, so this build has no release to judge the API against", path)
	}

	rest, _, _ := strings.Cut(text, "+")

	rest, label, hasLabel := strings.Cut(rest, "-")

	majorText, rest, hasMinor := strings.Cut(rest, ".")
	minorText, patchText, hasPatch := strings.Cut(rest, ".")

	if !hasMinor || !hasPatch || strings.Contains(patchText, ".") {
		return release{}, fmt.Errorf("%s reads %q, and a release is three numbers such as 1.0.0", path, text)
	}

	parsed := release{prerelease: hasLabel && label != "", text: text}

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
			return release{}, fmt.Errorf("%s reads %q, and %q is not a version number", path, text, part.number)
		}

		*part.into = value
	}

	return parsed, nil
}

// frozenBy reports whether this release freezes every published major version.
//
// A prerelease of the floor is below it, which is semver 2.0.0 section 11:
// 1.0.0-rc.1 precedes 1.0.0. So a release candidate for the first 1.0.0 still
// reports, and the tag that ships refuses.
func (r release) frozenBy(floor release) bool {
	if r.major != floor.major {
		return r.major > floor.major
	}

	if r.minor != floor.minor {
		return r.minor > floor.minor
	}

	if r.patch != floor.patch {
		return r.patch > floor.patch
	}

	return !r.prerelease
}

// mustFloor parses freezeFloor, which is a constant this file owns.
func mustFloor() release {
	floor, err := parseRelease("freezeFloor", freezeFloor)
	if err != nil {
		panic("apicompat: freezeFloor is not a version: " + err.Error())
	}

	return floor
}

// freeze returns the policy the gate applies, which is the committed one until
// the release reaches the floor and every major frozen after it.
func freeze(rules policy, current release) policy {
	if !current.frozenBy(mustFloor()) {
		return rules
	}

	applied := policy{Schema: rules.Schema, Majors: make(map[string]majorStability, len(rules.Majors))}

	for name, major := range rules.Majors {
		major.Stability = stabilityFrozen
		applied.Majors[name] = major
	}

	return applied
}

// policyDisagrees names every major whose committed stability contradicts the
// release, or returns nil. It is the second half of the flip: `freeze` makes the
// verdict right, and this makes the file wrong out loud, so `policy.json` cannot
// promise `reporting` about a surface the gate is refusing breaks on.
func policyDisagrees(rules policy, current release) error {
	if !current.frozenBy(mustFloor()) {
		return nil
	}

	var stale []string

	for name, major := range rules.Majors {
		if major.Stability != stabilityFrozen {
			stale = append(stale, name)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	sort.Strings(stale)

	return fmt.Errorf(
		"VERSION reads %s, which is at or above %s, and %s still reads %q in the policy.\n"+
			"A major version is frozen at the first %s. The gate has already "+
			"applied it. Set the stability to %q and say so in the reason beside it",
		current.text, freezeFloor, strings.Join(stale, ", "), stabilityReporting,
		freezeFloor, stabilityFrozen)
}
