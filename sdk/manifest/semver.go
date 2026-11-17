package manifest

import (
	"fmt"
	"strconv"
	"strings"
)

// Semantic versions, as https://semver.org/spec/v2.0.0.html defines them.

// Semver is one version: the three numbers, the prerelease identifiers and
// the build metadata. Build takes no part in ordering (semver rule 10).
type Semver struct {
	// Major, Minor and Patch are the three numbers.
	Major, Minor, Patch int
	// Pre are the dot-separated prerelease identifiers, in the order they were
	// written, and empty for a release.
	Pre []string
	// Build is the build metadata after the plus sign, and empty when there is
	// none.
	Build string
}

// ParseVersion reads one version. It refuses a leading v, a zero-padded field
// and an empty identifier, all of which make two spellings of one version
// compare unequal as text.
func ParseVersion(text string) (Semver, error) {
	rest := strings.TrimSpace(text)
	if rest == "" {
		return Semver{}, fmt.Errorf("%w: a version was expected and the value is empty", ErrInvalid)
	}

	var held Semver

	if plus := strings.IndexByte(rest, '+'); plus >= 0 {
		held.Build = rest[plus+1:]
		rest = rest[:plus]

		if err := identifiers("the build metadata of "+text, held.Build); err != nil {
			return Semver{}, err
		}
	}

	if dash := strings.IndexByte(rest, '-'); dash >= 0 {
		held.Pre = strings.Split(rest[dash+1:], ".")
		rest = rest[:dash]

		if err := identifiers("the prerelease of "+text, strings.Join(held.Pre, ".")); err != nil {
			return Semver{}, err
		}

		for _, identifier := range held.Pre {
			if numeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return Semver{}, fmt.Errorf("%w: %q has a zero-padded prerelease identifier %q",
					ErrInvalid, text, identifier)
			}
		}
	}

	fields := strings.Split(rest, ".")
	if len(fields) != versionFields {
		return Semver{}, fmt.Errorf("%w: %q is not MAJOR.MINOR.PATCH", ErrInvalid, text)
	}

	numbers := make([]int, versionFields)

	for index, field := range fields {
		number, err := versionNumber(text, field)
		if err != nil {
			return Semver{}, err
		}

		numbers[index] = number
	}

	held.Major, held.Minor, held.Patch = numbers[0], numbers[1], numbers[2]

	return held, nil
}

// versionNumber reads one of the three fields, refusing a zero-padded one so
// that "1.02.0" and "1.2.0" cannot both name one version.
func versionNumber(text, field string) (int, error) {
	if field == "" || (len(field) > 1 && field[0] == '0') || !numeric(field) {
		return 0, fmt.Errorf("%w: %q has an empty, zero-padded or non-numeric field %q",
			ErrInvalid, text, field)
	}

	number, err := strconv.Atoi(field)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not MAJOR.MINOR.PATCH", ErrInvalid, text)
	}

	return number, nil
}

// identifiers checks a prerelease or a build: dot-separated identifiers of
// letters, digits and hyphens, none empty.
func identifiers(where, text string) error {
	if text == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalid, where)
	}

	for identifier := range strings.SplitSeq(text, ".") {
		if identifier == "" {
			return fmt.Errorf("%w: %s has an empty identifier", ErrInvalid, where)
		}

		for index := range len(identifier) {
			char := identifier[index]
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
				(char < '0' || char > '9') && char != '-' {
				return fmt.Errorf("%w: %s holds %q, and an identifier is letters, digits and hyphens",
					ErrInvalid, where, identifier)
			}
		}
	}

	return nil
}

// numeric reports whether every character is a digit.
func numeric(text string) bool {
	if text == "" {
		return false
	}

	for index := range len(text) {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}

	return true
}

// String writes the version back in the form ParseVersion reads.
func (s Semver) String() string {
	out := strconv.Itoa(s.Major) + "." + strconv.Itoa(s.Minor) + "." + strconv.Itoa(s.Patch)

	if len(s.Pre) > 0 {
		out += "-" + strings.Join(s.Pre, ".")
	}

	if s.Build != "" {
		out += "+" + s.Build
	}

	return out
}

// Release reports whether the version has no prerelease.
func (s Semver) Release() bool { return len(s.Pre) == 0 }

// Compare orders two versions and returns a negative number, zero or a
// positive one. Build metadata is ignored (semver rule 10). A prerelease is
// below the same three numbers without one (rule 9).
func (s Semver) Compare(other Semver) int {
	for _, pair := range [][2]int{
		{s.Major, other.Major}, {s.Minor, other.Minor}, {s.Patch, other.Patch},
	} {
		if pair[0] != pair[1] {
			return pair[0] - pair[1]
		}
	}

	switch {
	case len(s.Pre) == 0 && len(other.Pre) == 0:
		return 0
	case len(s.Pre) == 0:
		return 1
	case len(other.Pre) == 0:
		return -1
	}

	return comparePre(s.Pre, other.Pre)
}

// comparePre orders two prerelease lists as semver rule 11 says: numbers below
// text, and a shorter list below a longer one that starts the same way.
func comparePre(left, right []string) int {
	for index := range min(len(left), len(right)) {
		if order := compareIdentifier(left[index], right[index]); order != 0 {
			return order
		}
	}

	return len(left) - len(right)
}

// compareIdentifier orders one pair of prerelease identifiers.
func compareIdentifier(left, right string) int {
	leftNumber, rightNumber := numeric(left), numeric(right)

	switch {
	case leftNumber && rightNumber:
		return atoi(left) - atoi(right)
	case leftNumber:
		return -1
	case rightNumber:
		return 1
	}

	return strings.Compare(left, right)
}

// atoi reads an identifier already known to be digits. A value too large for
// an int is clamped.
func atoi(text string) int {
	number, err := strconv.Atoi(text)
	if err != nil {
		return len(text)
	}

	return number
}
