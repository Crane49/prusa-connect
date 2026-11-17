package manifest

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Range is a set of versions in npm's range syntax. Alternatives are separated
// by `||`. A version is in the range when it satisfies every comparator of
// one alternative:
//
//	1.2.3            that version only
//	=1.2.3           the same
//	>1.2.3 <2.0.0    both comparators
//	>=1.2.3          at least that version
//	<=1.2.3          that version or below
//	^1.2.3           >=1.2.3 <2.0.0, and <0.3.0 for ^0.2.3, and <0.0.4 for ^0.0.3
//	~1.2.3           >=1.2.3 <1.3.0
//	1.2              any 1.2 patch, which is >=1.2.0 <1.3.0
//	1.x              any 1 minor, which is >=1.0.0 <2.0.0
//	*                any version
//	1.2.3 - 2.3.4    >=1.2.3 <=2.3.4
//	>=1.0, <2.0      a comma joins two bounds, as Composer writes them
//	^1 || ^2         either alternative
//
// A prerelease such as 1.2.3-rc.1 is in a range only when a comparator of the
// matching alternative names the same three numbers with a prerelease. So
// `>=1.0.0` does not match `2.0.0-rc.1`.
type Range struct {
	// alternatives are the `||` branches, each a list of comparators every one
	// of which has to hold.
	alternatives [][]comparator
	// text is the range as the author wrote it, for String and for a refusal
	// to quote.
	text string
}

// comparator is one bound: an operator and the version it is against.
type comparator struct {
	operator string
	version  Semver
}

// The operators a comparator is written with, longest first so that a scan
// reads `>=` before `>`.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var operators = []string{">=", "<=", ">", "<", "="}

// versionFields is how many numbers a version is: MAJOR, MINOR and PATCH.
const versionFields = 3

// anyVersion is the alternative a `*`, an `x` or an empty range reads as.
const anyVersion = ">=0.0.0"

// ParseRange reads one range. It refuses anything outside the grammar of
// Range.
func ParseRange(text string) (Range, error) {
	held := Range{text: strings.TrimSpace(text)}

	for alternative := range strings.SplitSeq(text, "||") {
		// A comma joins two bounds that both hold, as Composer writes them:
		// `>=1.0, <2.0` is `>=1.0 <2.0`.
		parsed, err := parseAlternative(text, strings.ReplaceAll(alternative, ",", " "))
		if err != nil {
			return Range{}, err
		}

		if err = refuseEmpty(text, parsed); err != nil {
			return Range{}, err
		}

		held.alternatives = append(held.alternatives, parsed)
	}

	if len(held.alternatives) == 0 {
		return Range{}, fmt.Errorf("%w: %q holds no version", ErrInvalid, text)
	}

	return held, nil
}

// refuseEmpty refuses an alternative no version can satisfy, such as two
// bounds written the wrong way round.
func refuseEmpty(whole string, alternative []comparator) error {
	var floor, ceiling *comparator

	for index, held := range alternative {
		switch held.operator {
		case ">", ">=", "=":
			if floor == nil || held.version.Compare(floor.version) > 0 {
				floor = &alternative[index]
			}
		}

		switch held.operator {
		case "<", "<=", "=":
			if ceiling == nil || held.version.Compare(ceiling.version) < 0 {
				ceiling = &alternative[index]
			}
		}
	}

	if floor == nil || ceiling == nil {
		return nil
	}

	order := floor.version.Compare(ceiling.version)
	closed := floor.operator != ">" && ceiling.operator != "<"

	if order > 0 || (order == 0 && !closed) {
		return fmt.Errorf("%w: %q holds no version: %s%s and %s%s cannot both hold",
			ErrInvalid, whole, floor.operator, floor.version, ceiling.operator, ceiling.version)
	}

	return nil
}

// parseAlternative reads one `||` branch into the comparators it means.
func parseAlternative(whole, text string) ([]comparator, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return parseSimple(whole, "*")
	}

	// A hyphen range is three fields and is read whole.
	if len(fields) == versionFields && fields[1] == "-" {
		return parseHyphen(whole, fields[0], fields[2])
	}

	var out []comparator

	for _, field := range fields {
		if field == "-" {
			return nil, fmt.Errorf("%w: %q writes a hyphen range as `A - B`, with one version on each side",
				ErrInvalid, whole)
		}

		parsed, err := parseSimple(whole, field)
		if err != nil {
			return nil, err
		}

		out = append(out, parsed...)
	}

	return out, nil
}

// parseSimple reads one comparator, one caret, one tilde or one partial
// version into the comparators it means.
func parseSimple(whole, text string) ([]comparator, error) {
	switch {
	case strings.HasPrefix(text, "^"):
		return parseCaret(whole, text[1:])
	case strings.HasPrefix(text, "~>"):
		return parseTilde(whole, text[2:])
	case strings.HasPrefix(text, "~"):
		return parseTilde(whole, text[1:])
	}

	for _, operator := range operators {
		rest, found := strings.CutPrefix(text, operator)
		if !found {
			continue
		}

		return parseOperator(whole, operator, rest)
	}

	return parsePartialRange(whole, text)
}

// parseOperator reads one primitive comparator. A partial version is filled
// in as npm does: `>1.2` means everything above the whole 1.2 line.
func parseOperator(whole, operator, text string) ([]comparator, error) {
	held, err := parsePartial(whole, text)
	if err != nil {
		return nil, err
	}

	if held.wild == versionFields {
		return nil, fmt.Errorf("%w: %q writes %s against a wildcard. A wildcard on its own is the "+
			"whole range, so write * and drop the operator", ErrInvalid, whole, operator)
	}

	if held.wild == 0 {
		return []comparator{{operator: operator, version: held.version}}, nil
	}

	switch operator {
	case ">":
		return []comparator{{operator: ">=", version: held.ceiling()}}, nil
	case "<=":
		return []comparator{{operator: "<", version: held.ceiling()}}, nil
	case "=":
		return parsePartialRange(whole, text)
	}

	// >= and < both take the floor: >=1.2 is >=1.2.0 and <1.2 is <1.2.0.
	return []comparator{{operator: operator, version: held.version}}, nil
}

// parseCaret reads a caret range: every version up to the next change that may
// break, which is the next value of the leftmost field that is not zero.
func parseCaret(whole, text string) ([]comparator, error) {
	held, err := parsePartial(whole, text)
	if err != nil {
		return nil, err
	}

	if held.wild == versionFields {
		return parseSimple(whole, anyVersion)
	}

	floor := held.version

	var ceiling Semver

	switch {
	case floor.Major > 0, held.wild >= versionFields-1:
		ceiling = Semver{Major: floor.Major + 1}
	case floor.Minor > 0, held.wild >= 1:
		ceiling = Semver{Minor: floor.Minor + 1}
	default:
		ceiling = Semver{Patch: floor.Patch + 1}
	}

	return between(floor, ceiling), nil
}

// parseTilde reads a tilde range: patches of the minor that was named, and
// minors of the major when no minor was.
func parseTilde(whole, text string) ([]comparator, error) {
	held, err := parsePartial(whole, text)
	if err != nil {
		return nil, err
	}

	if held.wild == versionFields {
		return parseSimple(whole, anyVersion)
	}

	floor := held.version

	ceiling := Semver{Major: floor.Major, Minor: floor.Minor + 1}
	if held.wild >= versionFields-1 {
		ceiling = Semver{Major: floor.Major + 1}
	}

	return between(floor, ceiling), nil
}

// parsePartialRange reads a bare version. A whole one is an exact pin. A
// partial one is every version the missing fields allow.
func parsePartialRange(whole, text string) ([]comparator, error) {
	held, err := parsePartial(whole, text)
	if err != nil {
		return nil, err
	}

	if held.wild == 0 {
		return []comparator{{operator: "=", version: held.version}}, nil
	}

	if held.wild == versionFields {
		return parseSimple(whole, anyVersion)
	}

	return between(held.version, held.ceiling()), nil
}

// between is the two comparators a range with both ends written out means.
func between(floor, ceiling Semver) []comparator {
	return []comparator{{operator: ">=", version: floor}, {operator: "<", version: ceiling}}
}

// partial is one version as written. wild counts the fields that were a
// wildcard or absent: 3 for `*`, 2 for `1`, 1 for `1.2`, 0 for a whole version.
type partial struct {
	version Semver
	wild    int
}

// ceiling is the first version above everything the partial could have named:
// 1.2 answers 1.3.0, and 1 answers 2.0.0.
func (p partial) ceiling() Semver {
	switch p.wild {
	case versionFields:
		return Semver{}
	case versionFields - 1:
		return Semver{Major: p.version.Major + 1}
	default:
		return Semver{Major: p.version.Major, Minor: p.version.Minor + 1}
	}
}

// parsePartial reads a version that may leave fields out or write them as a
// wildcard.
func parsePartial(whole, text string) (partial, error) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "v")

	if trimmed == "" || wildcard(trimmed) {
		return partial{wild: versionFields}, nil
	}

	numbers, rest, wild, err := partialFields(whole, trimmed)
	if err != nil {
		return partial{}, err
	}

	held := Semver{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}

	if rest != "" {
		if wild > 0 {
			return partial{}, fmt.Errorf("%w: %q writes a prerelease on a version with a wildcard in it",
				ErrInvalid, whole)
		}

		full, parseErr := ParseVersion(held.String() + rest)
		if parseErr != nil {
			return partial{}, fmt.Errorf("%w: in %q: %w", ErrInvalid, whole, parseErr)
		}

		held = full
	}

	return partial{version: held, wild: wild}, nil
}

// partialFields reads the three numbers, the prerelease and build suffix, and
// how many fields were left to the wildcard.
func partialFields(whole, text string) ([versionFields]int, string, int, error) {
	var numbers [versionFields]int

	body, rest := text, ""
	if cut := strings.IndexAny(text, "-+"); cut > 0 {
		body, rest = text[:cut], text[cut:]
	}

	fields := strings.Split(body, ".")
	if len(fields) > versionFields {
		return numbers, "", 0, fmt.Errorf("%w: %q has %d fields and a version has at most %d",
			ErrInvalid, whole, len(fields), versionFields)
	}

	wild := versionFields - len(fields)
	seen := false

	for index, field := range fields {
		if wildcard(field) {
			// Every field after a wildcard must be a wildcard too. npm reads
			// 1.x.3 as 1.x, and this package refuses it.
			for _, after := range fields[index+1:] {
				if !wildcard(after) {
					return numbers, "", 0, fmt.Errorf(
						"%w: %q writes %q after a wildcard, and every field after one is a wildcard too",
						ErrInvalid, whole, after)
				}
			}

			wild = versionFields - index
			seen = true

			break
		}

		number, err := versionNumber(whole, field)
		if err != nil {
			return numbers, "", 0, err
		}

		numbers[index] = number
	}

	if seen && rest != "" {
		return numbers, "", 0, fmt.Errorf("%w: %q writes a prerelease on a version with a wildcard in it",
			ErrInvalid, whole)
	}

	return numbers, rest, wild, nil
}

// wildcard reports whether a field stands for every value it could have.
func wildcard(field string) bool {
	return field == "*" || field == "x" || field == "X"
}

// parseHyphen reads `A - B`: at least A, and at most everything B could name.
func parseHyphen(whole, left, right string) ([]comparator, error) {
	lower, err := parsePartial(whole, left)
	if err != nil {
		return nil, err
	}

	upper, err := parsePartial(whole, right)
	if err != nil {
		return nil, err
	}

	out := []comparator{{operator: ">=", version: lower.version}}

	if upper.wild == 0 {
		return append(out, comparator{operator: "<=", version: upper.version}), nil
	}

	if upper.wild == versionFields {
		return out, nil
	}

	return append(out, comparator{operator: "<", version: upper.ceiling()}), nil
}

// String writes the range back as the author wrote it.
func (r Range) String() string { return r.text }

// Includes reports whether a MAJOR.MINOR.PATCH version is in the range. It
// returns an error for a version this package cannot read.
func (r Range) Includes(version extension.Version) (bool, error) {
	held, err := ParseVersion(string(version))
	if err != nil {
		return false, err
	}

	return r.Holds(held), nil
}

// Holds reports whether one parsed version is in the range.
func (r Range) Holds(version Semver) bool {
	for _, alternative := range r.alternatives {
		if satisfies(version, alternative) {
			return true
		}
	}

	return false
}

// HoldsCore reports whether a running core is in the range. A prerelease of
// the core, such as 0.1.0-beta.2, counts as its three numbers. When the range
// names a prerelease of those numbers, the whole version is compared.
func (r Range) HoldsCore(running Semver) bool {
	if running.Release() || r.namesPrereleaseOf(running) {
		return r.Holds(running)
	}

	return r.Holds(Semver{Major: running.Major, Minor: running.Minor, Patch: running.Patch})
}

// namesPrereleaseOf reports whether a comparator names a prerelease of the
// version's three numbers.
func (r Range) namesPrereleaseOf(version Semver) bool {
	for _, alternative := range r.alternatives {
		for _, held := range alternative {
			if !held.version.Release() && held.version.Major == version.Major &&
				held.version.Minor == version.Minor && held.version.Patch == version.Patch {
				return true
			}
		}
	}

	return false
}

// satisfies reports whether a version meets every comparator of one
// alternative, under npm's prerelease rule.
func satisfies(version Semver, alternative []comparator) bool {
	for _, held := range alternative {
		if !meets(version, held) {
			return false
		}
	}

	if version.Release() {
		return true
	}

	// A prerelease is in the range only where the author named a prerelease
	// of the same three numbers.
	for _, held := range alternative {
		if !held.version.Release() &&
			held.version.Major == version.Major &&
			held.version.Minor == version.Minor &&
			held.version.Patch == version.Patch {
			return true
		}
	}

	return false
}

// meets reports whether a version satisfies one comparator.
func meets(version Semver, held comparator) bool {
	order := version.Compare(held.version)

	switch held.operator {
	case ">":
		return order > 0
	case ">=":
		return order >= 0
	case "<":
		return order < 0
	case "<=":
		return order <= 0
	default:
		return order == 0
	}
}

// Exact is the one version a range pins, and false for a range that names a
// set.
func (r Range) Exact() (Semver, bool) {
	if len(r.alternatives) != 1 || len(r.alternatives[0]) != 1 {
		return Semver{}, false
	}

	held := r.alternatives[0][0]
	if held.operator != "=" {
		return Semver{}, false
	}

	return held.version, true
}

// Describe writes what the range was read as, in comparators, so a refusal
// can show an author how their range was understood.
func (r Range) Describe() string {
	rendered := make([]string, 0, len(r.alternatives))

	for _, alternative := range r.alternatives {
		parts := make([]string, 0, len(alternative))
		for _, held := range alternative {
			parts = append(parts, held.operator+held.version.String())
		}

		rendered = append(rendered, strings.Join(parts, " "))
	}

	return strings.Join(rendered, " || ")
}
