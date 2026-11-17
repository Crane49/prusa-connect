package manifest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A constraint names the releases of a module, or of the core, in Composer's
// grammar (https://getcomposer.org/doc/articles/versions.md):
//
//	1.2.3            that release only
//	>=1.2 <2.0       both bounds, also written with a comma
//	!=1.2.5          any release but that one
//	1.2.*  1.*  *    a wildcard
//	1.0 - 2.0        a hyphen range: >=1.0.0 <2.1.0, and <=2.0.0 for 1.0.0 - 2.0.0
//	~1.2             >=1.2.0 <2.0.0, and >=1.2.3 <1.3.0 for ~1.2.3
//	^1.2.3           >=1.2.3 <2.0.0, and <0.4.0 for ^0.3, and <0.0.4 for ^0.0.3
//	^1 || ^2         either alternative
//	^1.2@beta        a stability flag: beta releases of this module are taken
//
// A bound derived from a partial version includes that version's prereleases:
// `^1.2` matches 1.2.0-beta1 and not 2.0.0-beta1. Stability decides whether a
// prerelease is taken at all.
//
// ui.library is an npm range and is read with ParseRange.

// Stability is how far along a release is, from its version: dev, alpha,
// beta, RC or stable, in that order.
type Stability int

// The stabilities, lowest first.
const (
	StabilityDev Stability = iota
	StabilityAlpha
	StabilityBeta
	StabilityRC
	StabilityStable
)

// String is the stability as Composer writes it.
func (s Stability) String() string {
	switch s {
	case StabilityDev:
		return "dev"
	case StabilityAlpha:
		return "alpha"
	case StabilityBeta:
		return "beta"
	case StabilityRC:
		return "RC"
	case StabilityStable:
		return "stable"
	}

	return "stable"
}

// ParseStability reads a stability name, as minimum-stability and a stability
// flag write it.
func ParseStability(text string) (Stability, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "dev":
		return StabilityDev, nil
	case "alpha", "a":
		return StabilityAlpha, nil
	case "beta", "b":
		return StabilityBeta, nil
	case "rc":
		return StabilityRC, nil
	case "stable", "":
		return StabilityStable, nil
	}

	return StabilityStable, fmt.Errorf("%w: %q is not a stability, expected dev, alpha, beta, RC or stable",
		ErrInvalid, text)
}

// Release is one release version as Composer reads it: three numbers, a
// stability and the number after the stability, such as 1.2.0-beta2.
type Release struct {
	Major, Minor, Patch int
	Stability           Stability
	// Number is the number after a prerelease word, 0 for none.
	Number int
}

// releasePattern reads a version: an optional v, one to three numbers and an
// optional prerelease. Build metadata after a plus is dropped.
var releasePattern = regexp.MustCompile(
	`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[-.]?(dev|alpha|a|beta|b|rc|RC|stable)(?:[.-]?(\d+))?)?(?:\+[0-9A-Za-z.-]+)?$`)

// ParseRelease reads one release version.
func ParseRelease(text string) (Release, error) {
	held, _, err := parseRelease(text)

	return held, err
}

// parseRelease reads a version and answers how many numbers it wrote.
func parseRelease(text string) (Release, int, error) {
	parts := releasePattern.FindStringSubmatch(strings.TrimSpace(text))
	if parts == nil {
		return Release{}, 0, fmt.Errorf("%w: %q is not a version, expected one such as 1.2.3 or 1.2.0-beta2",
			ErrInvalid, text)
	}

	numbers := []int{0, 0, 0}
	written := 0

	for index := range 3 {
		if parts[index+1] == "" {
			continue
		}

		value, err := strconv.Atoi(parts[index+1])
		if err != nil {
			return Release{}, 0, fmt.Errorf("%w: %q: %w", ErrInvalid, text, err)
		}

		numbers[index] = value
		written++
	}

	held := Release{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Stability: StabilityStable}

	if parts[4] != "" {
		stability, err := ParseStability(parts[4])
		if err != nil {
			return Release{}, 0, err
		}

		held.Stability = stability
	}

	if parts[5] != "" {
		held.Number, _ = strconv.Atoi(parts[5])
	}

	return held, written, nil
}

// Compare orders two releases: by the numbers, then the stability, then the
// number after the stability.
func (r Release) Compare(other Release) int {
	for _, pair := range [][2]int{
		{r.Major, other.Major}, {r.Minor, other.Minor}, {r.Patch, other.Patch},
		{int(r.Stability), int(other.Stability)}, {r.Number, other.Number},
	} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}

			return 1
		}
	}

	return 0
}

// String writes the release.
func (r Release) String() string {
	base := fmt.Sprintf("%d.%d.%d", r.Major, r.Minor, r.Patch)
	if r.Stability == StabilityStable {
		return base
	}

	if r.Number == 0 {
		return base + "-" + r.Stability.String()
	}

	return fmt.Sprintf("%s-%s%d", base, r.Stability, r.Number)
}

// lowest is the first release of a version's numbers, its dev release, which
// a bound derived from a partial version starts or stops at.
func lowest(major, minor, patch int) Release {
	return Release{Major: major, Minor: minor, Patch: patch, Stability: StabilityDev}
}

// Constraint is one constraint, read.
type Constraint struct {
	text string
	// alternatives are the || branches, each a list of bounds every one of
	// which has to hold.
	alternatives [][]bound
	// flag is the stability the constraint names for its module: a @flag, or
	// the lowest stability of a version it writes out. StabilityStable when it
	// names none.
	flag Stability
	// flagged is true when the constraint names a stability.
	flagged bool
}

// bound is one comparison against one release.
type bound struct {
	operator string
	release  Release
	// written is true for a bound on a prerelease the constraint wrote out,
	// and false for the dev release a range derives.
	written bool
}

func (b bound) holds(release Release) bool {
	order := release.Compare(b.release)

	switch b.operator {
	case ">":
		return order > 0
	case ">=":
		return order >= 0
	case "<":
		return order < 0
	case "<=":
		return order <= 0
	case "!=":
		return order != 0
	}

	return order == 0
}

// String is the constraint as written.
func (c Constraint) String() string { return c.text }

// Holds reports whether a release is in the constraint. Stability is not
// checked here. See Stability.
func (c Constraint) Holds(release Release) bool {
	for _, alternative := range c.alternatives {
		held := true

		for _, one := range alternative {
			if !one.holds(release) {
				held = false

				break
			}
		}

		if held {
			return true
		}
	}

	return false
}

// HoldsText reads a release and reports whether it is in the constraint. A
// release that cannot be read is not.
func (c Constraint) HoldsText(release string) bool {
	held, err := ParseRelease(release)

	return err == nil && c.Holds(held)
}

// HoldsCore reports whether a core version is in the constraint. A prerelease
// of the core counts as its release unless the constraint writes a prerelease
// of the same numbers, so a beta of 0.1.0 runs a module that requires 0.1.0.
func (c Constraint) HoldsCore(core Release) bool {
	if c.Holds(core) || c.NamesPrereleaseOf(core) {
		return c.Holds(core)
	}

	release := core
	release.Stability, release.Number = StabilityStable, 0

	return c.Holds(release)
}

// Stability is the stability the constraint names for its module, and false
// when it names none.
func (c Constraint) Stability() (Stability, bool) { return c.flag, c.flagged }

// hyphenPattern is a hyphen range, whose spaces around the hyphen are part of
// the syntax.
var hyphenPattern = regexp.MustCompile(`^\s*(\S+)\s+-\s+(\S+)\s*$`)

// operatorPattern is a bound written with an operator, with or without a
// space after it.
var operatorPattern = regexp.MustCompile(`^(>=|<=|<>|!=|==|=|>|<)\s*(\S+)$`)

// ParseConstraint reads one constraint, and refuses anything Composer would
// not read.
func ParseConstraint(text string) (Constraint, error) {
	held := Constraint{text: strings.TrimSpace(text), flag: StabilityStable}

	body := held.text

	if at := strings.LastIndex(body, "@"); at >= 0 {
		flag, err := ParseStability(body[at+1:])
		if err != nil {
			return Constraint{}, fmt.Errorf("%w: %q: %w", ErrInvalid, text, err)
		}

		held.flag, held.flagged = flag, true
		body = strings.TrimSpace(body[:at])
	}

	if body == "" {
		body = "*"
	}

	for alternative := range strings.SplitSeq(strings.ReplaceAll(body, "||", "|"), "|") {
		bounds, err := constraintAlternative(&held, strings.TrimSpace(alternative))
		if err != nil {
			return Constraint{}, fmt.Errorf("%w: %q: %w", ErrInvalid, text, err)
		}

		held.alternatives = append(held.alternatives, bounds)
	}

	return held, nil
}

// constraintAlternative reads the bounds of one || branch.
func constraintAlternative(c *Constraint, alternative string) ([]bound, error) {
	if alternative == "" {
		return nil, fmt.Errorf("%w: an empty alternative", ErrInvalid)
	}

	if parts := hyphenPattern.FindStringSubmatch(alternative); parts != nil {
		return constraintHyphen(c, parts[1], parts[2])
	}

	var out []bound

	for _, word := range andWords(alternative) {
		bounds, err := constraintSingle(c, word)
		if err != nil {
			return nil, err
		}

		out = append(out, bounds...)
	}

	return out, nil
}

// andWords splits a branch at its commas and spaces, keeping an operator
// with the version written after it.
func andWords(alternative string) []string {
	fields := strings.Fields(strings.ReplaceAll(alternative, ",", " "))

	var out []string

	for index := 0; index < len(fields); index++ {
		word := fields[index]
		if operatorOnly(word) && index+1 < len(fields) {
			word += fields[index+1]
			index++
		}

		out = append(out, word)
	}

	return out
}

func operatorOnly(word string) bool {
	switch word {
	case ">=", "<=", "<>", "!=", "==", "=", ">", "<", "^", "~":
		return true
	}

	return false
}

// noteStability records the stability a version written in the constraint names.
func noteStability(c *Constraint, release Release) {
	if release.Stability < StabilityStable && (!c.flagged || release.Stability < c.flag) {
		c.flag, c.flagged = release.Stability, true
	}
}

// constraintSingle reads one bound or one range written as one word.
func constraintSingle(c *Constraint, word string) ([]bound, error) {
	switch {
	case word == "*" || word == "x" || word == "X" || word == "*.*" || word == "*.*.*":
		return []bound{{operator: ">=", release: lowest(0, 0, 0)}}, nil
	case strings.HasPrefix(word, "^"):
		return constraintCaret(c, word[1:])
	case strings.HasPrefix(word, "~") && !strings.HasPrefix(word, "~>"):
		return constraintTilde(c, word[1:])
	case strings.ContainsAny(word, "*xX") && !strings.ContainsAny(word, "<>=!"):
		return constraintWildcard(c, word)
	}

	operator, version := "=", word

	if parts := operatorPattern.FindStringSubmatch(word); parts != nil {
		operator, version = parts[1], parts[2]
	}

	release, _, err := parseRelease(version)
	if err != nil {
		return nil, err
	}

	noteStability(c, release)

	switch operator {
	case "==":
		operator = "="
	case "<>":
		operator = "!="
	}

	// Composer starts >= and stops < at the dev release of a stable
	// version, so >=1.2 matches 1.2.0-beta1 and <2.0 does not match 2.0.0-beta1.
	if (operator == ">=" || operator == "<") && release.Stability == StabilityStable {
		release.Stability = StabilityDev
	}

	return []bound{{operator: operator, release: release, written: writesPrerelease(version)}}, nil
}

// constraintCaret reads ^v: the releases that do not change the first number that is
// not zero.
func constraintCaret(c *Constraint, version string) ([]bound, error) {
	release, written, err := parseRelease(version)
	if err != nil {
		return nil, err
	}

	noteStability(c, release)

	var upper Release

	switch {
	case release.Major > 0 || written == 1:
		upper = lowest(release.Major+1, 0, 0)
	case release.Minor > 0 || written == 2:
		upper = lowest(0, release.Minor+1, 0)
	default:
		upper = lowest(0, 0, release.Patch+1)
	}

	return []bound{
		{operator: ">=", release: floor(release), written: writesPrerelease(version)},
		{operator: "<", release: upper},
	}, nil
}

// constraintTilde reads ~v: the releases up to the next change of the number before
// the last one written, or of the major when only one is written.
func constraintTilde(c *Constraint, version string) ([]bound, error) {
	release, written, err := parseRelease(version)
	if err != nil {
		return nil, err
	}

	noteStability(c, release)

	var upper Release

	switch written {
	case 1, 2: //nolint:mnd // a major, or a major and a minor.
		upper = lowest(release.Major+1, 0, 0)
	default:
		upper = lowest(release.Major, release.Minor+1, 0)
	}

	return []bound{
		{operator: ">=", release: floor(release), written: writesPrerelease(version)},
		{operator: "<", release: upper},
	}, nil
}

// constraintWildcard reads 1.2.*, 1.* and 1.x.
func constraintWildcard(_ *Constraint, word string) ([]bound, error) {
	trimmed := strings.TrimRight(strings.NewReplacer("*", "", "x", "", "X", "").Replace(word), ".")

	release, written, err := parseRelease(trimmed)
	if err != nil || release.Stability != StabilityStable {
		return nil, fmt.Errorf("%w: %q is not a wildcard such as 1.2.*", ErrInvalid, word)
	}

	switch written {
	case 1:
		return []bound{
			{operator: ">=", release: lowest(release.Major, 0, 0)},
			{operator: "<", release: lowest(release.Major+1, 0, 0)},
		}, nil
	case 2: //nolint:mnd // a major and a minor.
		return []bound{
			{operator: ">=", release: lowest(release.Major, release.Minor, 0)},
			{operator: "<", release: lowest(release.Major, release.Minor+1, 0)},
		}, nil
	}

	return nil, fmt.Errorf("%w: %q is not a wildcard such as 1.2.*", ErrInvalid, word)
}

// constraintHyphen reads A - B. A partial upper bound takes every release of the part
// it wrote, as Composer reads it.
func constraintHyphen(c *Constraint, from, to string) ([]bound, error) {
	low, _, err := parseRelease(from)
	if err != nil {
		return nil, err
	}

	high, written, err := parseRelease(to)
	if err != nil {
		return nil, err
	}

	noteStability(c, low)
	noteStability(c, high)

	upper := bound{operator: "<=", release: high}

	switch written {
	case 1:
		upper = bound{operator: "<", release: lowest(high.Major+1, 0, 0)}
	case 2: //nolint:mnd // a major and a minor.
		upper = bound{operator: "<", release: lowest(high.Major, high.Minor+1, 0)}
	}

	return []bound{{operator: ">=", release: floor(low)}, upper}, nil
}

// writesPrerelease reports whether a version writes a prerelease out.
func writesPrerelease(version string) bool {
	held, err := ParseRelease(version)

	return err == nil && held.Stability != StabilityStable
}

// NamesPrereleaseOf reports whether the constraint writes out a prerelease
// of the release's three numbers, such as >=0.1.0-beta2 for 0.1.0-beta1.
func (c Constraint) NamesPrereleaseOf(release Release) bool {
	for _, alternative := range c.alternatives {
		for _, one := range alternative {
			if one.written && one.release.Major == release.Major && one.release.Minor == release.Minor &&
				one.release.Patch == release.Patch {
				return true
			}
		}
	}

	return false
}

// floor is the lowest release of a range that starts at a version: the dev
// release of a stable version, or the prerelease itself.
func floor(release Release) Release {
	if release.Stability == StabilityStable {
		release.Stability = StabilityDev
	}

	return release
}

// constraintKey holds a key to the constraint grammar. An empty value is
// absent.
func constraintKey(key, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	if _, err := ParseConstraint(value); err != nil {
		return fmt.Errorf("%s is %q: %w", key, value, err)
	}

	return nil
}
