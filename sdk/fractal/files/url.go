package files

import (
	"fmt"
	"net/url"
	"strings"
)

// Scheme is the scheme of a URL that names an object.
const Scheme = "fractal"

// nameParam is the one query parameter this version defines.
const nameParam = "name"

// URL names an object as fractal:///<identity>, with an optional display
// name. A node obtains the object by ID alone. Name never addresses, caches
// or verifies it.
type URL struct {
	// ID is the identity of the object.
	ID Identity
	// Name is the value of the name parameter, or "" when the URL has none or
	// ParseURL ignored one that breaks the rule for names.
	Name string
}

// ParseURL reads a fractal URL. It matches the scheme in any case, and
// refuses an authority, a fragment and a percent-encoded octet in the path.
// It ignores a query parameter it does not know and a name that breaks the
// rule for names, and it refuses a URL with two names.
func ParseURL(s string) (URL, error) {
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok || !strings.EqualFold(scheme, Scheme) {
		return URL{}, fmt.Errorf("%w: %q is not a %s URL", ErrMalformed, s, Scheme)
	}
	path, ok := strings.CutPrefix(rest, "///")
	if !ok {
		return URL{}, fmt.Errorf("%w: %q has an authority or no path", ErrMalformed, s)
	}
	if strings.Contains(path, "#") {
		return URL{}, fmt.Errorf("%w: %q has a fragment", ErrMalformed, s)
	}
	path, query, _ := strings.Cut(path, "?")
	if strings.Contains(path, "%") {
		return URL{}, fmt.Errorf("%w: the path of %q holds a percent-encoded octet", ErrMalformed, s)
	}
	id, err := ParseText(path)
	if err != nil {
		return URL{}, err
	}
	u := URL{ID: id}
	seen := false
	for param := range strings.SplitSeq(query, "&") {
		key, value, _ := strings.Cut(param, "=")
		if key != nameParam {
			continue
		}
		if seen {
			return URL{}, fmt.Errorf("%w: %q has two names", ErrMalformed, s)
		}
		seen = true
		if u.Name, err = url.PathUnescape(value); err != nil {
			return URL{}, fmt.Errorf("%w: the name in %q: %w", ErrMalformed, s, err)
		}
		if CheckName(u.Name) != nil {
			u.Name = ""
		}
	}
	return u, nil
}

// String returns the URL with the scheme in lowercase and the name, when
// there is one, with every byte outside the unreserved set of RFC 3986
// escaped.
func (u URL) String() string {
	s := Scheme + ":///" + u.ID.Text()
	if u.Name == "" {
		return s
	}
	return s + "?" + nameParam + "=" + escape(u.Name)
}

// escape percent-encodes every byte of s outside the unreserved set of RFC
// 3986, section 2.3. url.QueryEscape and url.PathEscape encode other sets.
func escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if unreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// unreserved reports whether c is in the unreserved set of RFC 3986.
func unreserved(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}
