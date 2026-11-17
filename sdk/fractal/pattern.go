package fractal

// Pattern selects, by Interface Name, the interfaces a node keeps in its
// shadow. It is made of Identifier characters and the wildcard "*", which
// matches zero or more characters anywhere in the pattern. A pattern must
// match the whole name, case-sensitively. It selects every instance of an
// interface whose name matches.
type Pattern string

// Validate returns an error for an empty pattern, and for a pattern with a
// character that is neither an Identifier character nor the wildcard.
func (p Pattern) Validate() error {
	s := string(p)
	if s == "" {
		return nameErr("pattern", s, ErrEmpty)
	}
	for i := range len(s) {
		if c := s[i]; !identifierByte(c) && c != '*' {
			return nameErr("pattern", s, ErrBadCharacter)
		}
	}
	return nil
}

// String returns the pattern unchanged.
func (p Pattern) String() string { return string(p) }

// ParsePattern validates s and returns it as a Pattern.
func ParsePattern(s string) (Pattern, error) {
	p := Pattern(s)
	return p, p.Validate()
}

// Match reports whether the pattern selects name. The pattern must match the
// whole name, and the comparison is case-sensitive. The time is at most the
// length of the name times the length of the pattern.
func (p Pattern) Match(name InterfaceName) bool {
	pat, s := string(p), string(name)
	var pi, si int
	star, mark := -1, 0
	for si < len(s) {
		switch {
		case pi < len(pat) && pat[pi] == '*':
			star = pi
			mark = si
			pi++
		case pi < len(pat) && pat[pi] == s[si]:
			pi++
			si++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}
