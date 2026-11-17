package httpapi

import (
	"net/http"
	"strings"
)

// anyTag is the "*" a client sends when it means "whatever is there now",
// RFC 9110 §13.1.1 and §13.1.2.
const anyTag = "*"

// entityTag is one RFC 9110 §8.8.3 entity-tag. The opaque value is held
// unquoted, because the quoting is wire syntax and the comparison functions are
// defined over the opaque part.
type entityTag struct {
	value string
	weak  bool
}

// etagOf renders an opaque version as the entity-tag a client quotes back.
// RFC 9110 §8.8.3 defines entity-tag as a quoted string. A cache, a proxy and
// every conforming client refuse to echo a bare token.
func etagOf(version string) string {
	return `"` + version + `"`
}

// etagSafe reports whether a version can be an entity-tag at all. etagc is
// %x21 / %x23-7E / obs-text, which is every printable byte except the double
// quote, and there is no escape inside the quoted form.
func etagSafe(version string) bool {
	if version == "" {
		return false
	}

	for i := range len(version) {
		c := version[i]
		if c == '"' || c < 0x21 || c == 0x7F {
			return false
		}
	}

	return true
}

// parseTagList parses an If-Match or If-None-Match field value: either "*" or a
// comma-separated list of entity-tags. Callers distinguish the three outcomes,
// because RFC 9110 gives each a different answer: absent means the condition
// does not apply, "*" means any current representation, and a list is compared
// tag by tag.
func parseTagList(header string) (tags []entityTag, wildcard, ok bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, false, true
	}

	if header == anyTag {
		return nil, true, true
	}

	for field := range strings.SplitSeq(header, ",") {
		field = strings.TrimSpace(field)

		// An empty list element. RFC 9110 §5.6.1.2 requires a recipient to
		// parse and ignore one, so `"v1",` and `, "v1"` are both the list
		// holding "v1". A sender that emits one is doing something the grammar
		// permits, and a 422 leaves it nothing to correct.
		if field == "" {
			continue
		}

		tag, parsed := parseTag(field)
		if !parsed {
			return nil, false, false
		}

		tags = append(tags, tag)
	}

	// Every element was empty, so the field carried separators and no member.
	// §5.6.1.2 permits empty elements inside a list and still requires the list
	// to have one, so the field is malformed and not absent.
	if len(tags) == 0 {
		return nil, false, false
	}

	return tags, false, true
}

func parseTag(field string) (entityTag, bool) {
	weak := false

	if rest, found := strings.CutPrefix(field, "W/"); found {
		weak = true
		field = rest
	}

	if len(field) < 2 || field[0] != '"' || field[len(field)-1] != '"' {
		return entityTag{}, false
	}

	value := field[1 : len(field)-1]
	if !etagSafe(value) {
		return entityTag{}, false
	}

	return entityTag{value: value, weak: weak}, true
}

// strongMatch is RFC 9110 §8.8.3.2's strong comparison: both tags must be
// strong and their opaque values octet-equal. If-Match uses it, so a weak
// validator can never satisfy an If-Match.
func strongMatch(tags []entityTag, version string) bool {
	for _, tag := range tags {
		if !tag.weak && tag.value == version {
			return true
		}
	}

	return false
}

// weakMatch is the weak comparison: opaque values octet-equal, whether or not
// either side is marked weak. If-None-Match is required to use it.
func weakMatch(tags []entityTag, version string) bool {
	for _, tag := range tags {
		if tag.value == version {
			return true
		}
	}

	return false
}

// anyWeak reports whether the list carries a weak validator. In If-Match that
// is a client error and not a condition that evaluates to false. RFC 9110
// §8.8.3.2 says a weak entity-tag "can only be used for weak comparison", so a
// 412 would refuse a request the client cannot correct by re-reading.
func anyWeak(tags []entityTag) bool {
	for _, tag := range tags {
		if tag.weak {
			return true
		}
	}

	return false
}

// notModified evaluates If-None-Match the way RFC 9110 §13.1.2 requires: "*"
// matches any current representation, a list is compared tag by tag, and the
// comparison is weak. Matching only a byte-exact echo of the header would make
// every cache that sends W/, a list or * fetch a body it already has.
func notModified(r *http.Request, etag string) bool {
	header, ok := r.Header["If-None-Match"]
	if !ok || etag == "" {
		return false
	}

	tags, wildcard, parsed := parseTagList(strings.Join(header, ", "))
	if !parsed {
		return false
	}

	if wildcard {
		return true
	}

	current, _ := parseTag(etag)

	return weakMatch(tags, current.value)
}
