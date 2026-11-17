package profile

import (
	"strconv"
	"strings"
)

// versionParts is how many components of a firmware version are compared.
const versionParts = 3

// version is a firmware version reduced to what a profile range is expressed
// in: the first three components as numbers, and the prerelease tag that orders
// below every one of them.
type version struct {
	components [versionParts]int
	// prerelease holds the identifiers of a "-" tag, and is empty for a released
	// build. A build that carries one is below the release of the same numbers.
	prerelease []string
}

// parseVersion reads a reported firmware label as a version. ok is false for a
// label whose first three components are not all numeric. This is the only
// firmware version comparison in the tree. Nothing after the resolver sees a
// version string.
//
// An unreadable label is refused and never ordered. Profile ranges are
// open-ended, so a text compare would put every label that starts with a
// letter above every numeric floor. It would select a profile for "MK4" or
// "unknown" and grant capabilities the device does not have.
//
// Build metadata is dropped first, from the first "+", because semver rule 10
// excludes it from precedence and firmware does report it ("4.7.10+8628").
// The prerelease tag after the first "-" is kept and ordered by semver rule 11,
// so "6.4.0-RC" is below "6.4.0". A band that ends at 6.5.0 therefore covers
// 6.5.0-RC, which must not be offered what the release added.
//
// Buddy firmware also spells a prerelease with a dot: both "6.2.0.alpha" and
// "6.4.0-alpha" occur. A component past the third that is not a number
// therefore opens the prerelease tag, so both spellings order below the
// release. A fourth number counts builds of a release and names no candidate,
// so it is dropped.
//
// Surrounding whitespace is not trimmed: " 6.4.0" and "6.4.0 " take the
// unmatched path. The suffixes above are read because a real device reports
// them. Other spellings, such as "v6.4.0", "6.4.0 (build 8628)" or "6,4,0",
// would each need a guess. TestAnUnreadableFirmwareLabelMatchesNothing holds
// every spelling.
func parseVersion(label string) (parsed version, ok bool) {
	base, _, _ := strings.Cut(label, "+")

	base, tag, tagged := strings.Cut(base, "-")

	components := strings.Split(base, ".")
	if len(components) > versionParts {
		dotted := components[versionParts:]
		components = components[:versionParts]

		if !allDigits(dotted[0]) {
			tag, tagged = joinTag(dotted, tag, tagged), true
		}
	}

	for i, component := range components {
		if !allDigits(component) {
			return version{}, false
		}

		number, err := strconv.Atoi(component)
		if err != nil {
			// A run of digits longer than an int is not a version anyone ships.
			return version{}, false
		}

		parsed.components[i] = number
	}

	if tagged {
		parsed.prerelease, ok = prereleaseIdentifiers(tag)
		if !ok {
			return version{}, false
		}
	}

	// A shorter version is padded with zeroes, so "6.4" is 6.4.0 and compares
	// against a three-part bound.
	return parsed, true
}

// joinTag puts the dotted identifiers in front of a hyphenated tag, so a label
// spelling both reads as one prerelease. The grammar is checked once, by
// prereleaseIdentifiers, over what this returns.
func joinTag(dotted []string, tag string, tagged bool) string {
	joined := strings.Join(dotted, ".")
	if tagged {
		joined += "." + tag
	}

	return joined
}

// prereleaseIdentifiers splits a prerelease tag on ".". ok is false for a tag
// semver's grammar refuses, which is an empty identifier or one carrying
// anything but a letter, a digit or a hyphen. A label that would need a guess
// takes the unmatched path.
func prereleaseIdentifiers(tag string) (identifiers []string, ok bool) {
	identifiers = strings.Split(tag, ".")

	for _, identifier := range identifiers {
		if identifier == "" {
			return nil, false
		}

		for i := range len(identifier) {
			if !isIdentifierByte(identifier[i]) {
				return nil, false
			}
		}
	}

	return identifiers, true
}

func isIdentifierByte(c byte) bool {
	return c == '-' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// allDigits reports whether s is a non-empty run of ASCII digits. A sign or a
// space makes it not a version component. strconv.Atoi accepts "+5" and this
// does not, so such a label takes the unmatched path.
func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// compare orders two versions, returning -1, 0 or 1.
func (v version) compare(w version) int {
	for i := range versionParts {
		if v.components[i] != w.components[i] {
			if v.components[i] < w.components[i] {
				return -1
			}

			return 1
		}
	}

	return comparePrerelease(v.prerelease, w.prerelease)
}

// comparePrerelease orders two prerelease tags by semver rule 11.4: a build
// carrying one is below the release of the same numbers, identifiers are
// compared one by one, and a tag that extends another is above it.
//
// "-0" is therefore the lowest build of its version. A range uses it to start
// at the first candidate for a release.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}

	for i := range min(len(a), len(b)) {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}

	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}

	return 0
}

// compareIdentifier orders one prerelease identifier against another. A numeric
// identifier is below an alphanumeric one. Two numeric identifiers compare by
// digit count first and then as text. That equals a numeric compare only for
// identifiers with no leading zero, which semver requires and this code does
// not check.
func compareIdentifier(a, b string) int {
	numericA, numericB := allDigits(a), allDigits(b)

	switch {
	case numericA && numericB:
		if len(a) != len(b) {
			if len(a) < len(b) {
				return -1
			}

			return 1
		}
	case numericA:
		return -1
	case numericB:
		return 1
	}

	return strings.Compare(a, b)
}

// isVersion reports whether a profile's own bound is readable as a version.
func isVersion(bound string) bool {
	_, ok := parseVersion(bound)

	return ok
}

// compareBounds orders two of a profile's own bounds, which validation has
// already established are readable. ok is false when either is not.
func compareBounds(a, b string) (order int, ok bool) {
	left, leftOK := parseVersion(a)
	right, rightOK := parseVersion(b)

	if !leftOK || !rightOK {
		return 0, false
	}

	return left.compare(right), true
}
