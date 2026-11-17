package catalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Version is a PrusaSlicer release number, such as 2.9.6 or 3.0.0-alpha12.
type Version struct {
	Major, Minor, Patch int
	// Pre is the prerelease label after the hyphen, such as alpha12 or rc1,
	// and empty for a final release.
	Pre string
}

// ErrVersion reports a version this module cannot read.
var ErrVersion = errors.New("not a version of the form 2.9.6 or 3.0.0-alpha12")

// versionParts is the major, the minor and the patch number.
const versionParts = 3

// ParseVersion reads a version. A leading "version_", the prefix of the
// PrusaSlicer release tags, is accepted and dropped.
func ParseVersion(text string) (Version, error) {
	text = strings.TrimPrefix(strings.TrimSpace(text), "version_")

	core, pre, _ := strings.Cut(text, "-")

	parts := strings.Split(core, ".")
	if len(parts) != versionParts {
		return Version{}, fmt.Errorf("%w: %q", ErrVersion, text)
	}

	numbers := [versionParts]int{}

	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || number > 999 {
			return Version{}, fmt.Errorf("%w: %q", ErrVersion, text)
		}

		numbers[index] = number
	}

	if pre != "" && !validLabel(pre) {
		return Version{}, fmt.Errorf("%w: %q", ErrVersion, text)
	}

	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Pre: pre}, nil
}

// maxLabelBytes bounds a prerelease label, such as alpha12.
const maxLabelBytes = 32

// validLabel holds a prerelease label to letters, digits and dots.
func validLabel(label string) bool {
	if len(label) > maxLabelBytes {
		return false
	}

	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' {
			return false
		}
	}

	return true
}

// String writes the version the way PrusaSlicer names its releases.
func (v Version) String() string {
	out := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		out += "-" + v.Pre
	}

	return out
}

// Stable reports a final release.
func (v Version) Stable() bool { return v.Pre == "" }

// Compare orders two versions: negative when v is older than other, zero when
// they are the same release, positive when v is newer. A prerelease is older
// than the final release of the same number, and alpha12 is newer than alpha2.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		if pair[0] != pair[1] {
			return pair[0] - pair[1]
		}
	}

	switch {
	case v.Pre == other.Pre:
		return 0
	case v.Pre == "":
		return 1
	case other.Pre == "":
		return -1
	}

	return compareLabels(v.Pre, other.Pre)
}

// compareLabels orders two prerelease labels by their word and then by the
// number that follows it, so rc1 is newer than beta2 and alpha12 newer than
// alpha2.
func compareLabels(a, b string) int {
	wordA, numberA := splitLabel(a)
	wordB, numberB := splitLabel(b)

	if wordA != wordB {
		return strings.Compare(wordA, wordB)
	}

	return numberA - numberB
}

func splitLabel(label string) (string, int) {
	cut := len(label)
	for cut > 0 && label[cut-1] >= '0' && label[cut-1] <= '9' {
		cut--
	}

	number, err := strconv.Atoi(label[cut:])
	if err != nil {
		number = 0
	}

	return strings.TrimSuffix(label[:cut], "."), number
}
