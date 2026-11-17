package connectorapi

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Set is the descriptor of one printer: every capability it has, and what
// each one accepts. A nil Set is empty and safe to read.
type Set map[Capability]Value

// Has reports whether the capability is present. A key whose Flag is false is
// absent.
func (s Set) Has(c Capability) bool {
	v, ok := s[c]

	return ok && v.Flag
}

// Allows reports whether the capability is present and permits the value. An
// empty Supported list permits every value.
func (s Set) Allows(c Capability, value string) bool {
	v, ok := s[c]
	if !ok || !v.Flag {
		return false
	}

	if len(v.Supported) == 0 {
		return true
	}

	return slices.Contains(v.Supported, value)
}

// Default returns the value the host sends when the caller sends none. ok is
// false when the capability is absent or declares no default.
func (s Set) Default(c Capability) (value string, ok bool) {
	v, present := s[c]
	if !present || !v.Flag || v.Default == "" {
		return "", false
	}

	return v.Default, true
}

// Limit returns the inclusive upper bound on the capability's magnitude. ok is
// false when the capability is absent or declares no limit.
func (s Set) Limit(c Capability) (limit float64, ok bool) {
	v, present := s[c]
	if !present || !v.Flag || !v.HasLimit {
		return 0, false
	}

	return v.Limit, true
}

// Keys returns every key in the set, present or not, in sorted order.
func (s Set) Keys() []Capability {
	return slices.Sorted(maps.Keys(s))
}

// Verbs returns every present key that is not an argument key, sorted.
// Validate accepts these as command verbs.
func (s Set) Verbs() []Capability {
	verbs := make([]Capability, 0, len(s))

	for key, value := range s {
		if !value.Flag {
			continue
		}

		if _, isArgument := key.Argument(); isArgument {
			continue
		}

		verbs = append(verbs, key)
	}

	slices.Sort(verbs)

	return verbs
}

// Clone returns an independent copy.
func (s Set) Clone() Set {
	if s == nil {
		return nil
	}

	out := make(Set, len(s))
	for k, v := range s {
		v.Supported = slices.Clone(v.Supported)
		out[k] = v
	}

	return out
}

// Intersect keeps only what both sets permit: a key absent from either is
// absent from the result, Supported sets are narrowed to their common values,
// the lower Limit and the higher Min win, and a required argument stays
// required. An argument whose verb is dropped is dropped too, and so is a verb
// that lost a required argument.
func (s Set) Intersect(t Set) Set {
	out := make(Set, min(len(s), len(t)))

	for key, mine := range s {
		theirs, ok := t[key]
		if !ok {
			continue
		}

		merged, keep := meet(mine, theirs)
		if !keep {
			continue
		}

		out[key] = merged
	}

	dropVerbsMissingARequiredArgument(out, s, t)
	dropOrphanArguments(out)

	return out
}

// dropVerbsMissingARequiredArgument removes a verb whose required argument is
// not in out. Otherwise a caller could send the verb without that argument.
func dropVerbsMissingARequiredArgument(out, left, right Set) {
	for _, side := range []Set{left, right} {
		for key, v := range side {
			if !v.Flag || !v.Required {
				continue
			}

			if _, isArgument := key.Argument(); !isArgument {
				continue
			}

			if out.Has(key) {
				continue
			}

			delete(out, key.Verb())
		}
	}
}

// dropOrphanArguments removes every argument key whose verb is not in the set.
func dropOrphanArguments(set Set) {
	for key := range set {
		if _, isArgument := key.Argument(); !isArgument {
			continue
		}

		if !set.Has(key.Verb()) {
			delete(set, key)
		}
	}
}

// Digest is a stable hash of the whole set. Two sets have the same digest if
// and only if they permit the same things. The order and the repetition of
// Supported values do not change it.
func (s Set) Digest() string {
	var b strings.Builder

	for _, key := range s.Keys() {
		v := s[key]
		supported := canonicalSupported(v.Supported)

		writeField(&b, key.String())
		writeField(&b, strconv.FormatBool(v.Flag))
		writeField(&b, v.Kind.String())

		// Write the count first. Without it a supported value could be read as
		// one of the fields that follow, and two sets could hash alike.
		writeField(&b, strconv.Itoa(len(supported)))

		for _, value := range supported {
			writeField(&b, value)
		}

		writeField(&b, v.Default)
		writeField(&b, strconv.FormatBool(v.Required))
		writeField(&b, optionalNumber(v.Limit, v.HasLimit))
		writeField(&b, optionalNumber(v.Min, v.HasMin))
		writeField(&b, v.Unit.String())
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}

// canonicalSupported returns the supported values sorted and deduplicated.
func canonicalSupported(supported []string) []string {
	if len(supported) == 0 {
		return nil
	}

	out := slices.Clone(supported)
	slices.Sort(out)

	return slices.Compact(out)
}

// optionalNumber renders a bound, or the empty string when there is none, so
// a bound of zero and no bound hash differently.
func optionalNumber(value float64, has bool) string {
	if !has {
		return ""
	}

	return strconv.FormatFloat(value, 'g', -1, 64)
}

// writeField writes one field with its length first, so a field that contains
// a separator cannot collide with two fields.
func writeField(b *strings.Builder, field string) {
	b.WriteString(strconv.Itoa(len(field)))
	b.WriteByte(':')
	b.WriteString(field)
}

// meet narrows one value against another. keep is false when the kinds or the
// units differ, or no supported value is common to both.
func meet(a, b Value) (merged Value, keep bool) {
	if a.Kind != b.Kind {
		return Value{}, false
	}

	supported, ok := meetSupported(a.Supported, b.Supported)
	if !ok {
		return Value{}, false
	}

	unit, ok := meetUnit(a.Unit, b.Unit)
	if !ok {
		return Value{}, false
	}

	out := Value{
		Flag:      a.Flag && b.Flag,
		Kind:      a.Kind,
		Supported: supported,
		Default:   meetDefault(a.Default, b.Default, supported),
		Required:  a.Required || b.Required,
		Unit:      unit,
	}

	out.Limit, out.HasLimit = meetLimit(a, b)
	out.Min, out.HasMin = meetMin(a, b)

	return out, true
}

// meetSupported intersects two value sets, where an empty set means
// unconstrained. ok is false when both constrain and nothing is common.
func meetSupported(a, b []string) (supported []string, ok bool) {
	switch {
	case len(a) == 0:
		return slices.Clone(b), true
	case len(b) == 0:
		return slices.Clone(a), true
	}

	common := make([]string, 0, min(len(a), len(b)))

	for _, value := range a {
		if slices.Contains(b, value) && !slices.Contains(common, value) {
			common = append(common, value)
		}
	}

	if len(common) == 0 {
		return nil, false
	}

	slices.Sort(common)

	return common, true
}

// meetDefault keeps a default only when both sides name the same one and it
// is still a supported value.
func meetDefault(a, b string, supported []string) string {
	if a != b || a == "" {
		return ""
	}

	if len(supported) > 0 && !slices.Contains(supported, a) {
		return ""
	}

	return a
}

// meetUnit keeps the unit either side declared. ok is false when the two
// sides declare different units.
func meetUnit(a, b Unit) (unit Unit, ok bool) {
	switch {
	case a == "":
		return b, true
	case b == "", a == b:
		return a, true
	}

	return "", false
}

// meetLimit takes the lower of two bounds, and the only bound when one side
// declares none.
func meetLimit(a, b Value) (limit float64, has bool) {
	switch {
	case a.HasLimit && b.HasLimit:
		return min(a.Limit, b.Limit), true
	case a.HasLimit:
		return a.Limit, true
	case b.HasLimit:
		return b.Limit, true
	}

	return 0, false
}

// meetMin takes the higher of two lower bounds, and the only bound when one
// side declares none.
func meetMin(a, b Value) (lowest float64, has bool) {
	switch {
	case a.HasMin && b.HasMin:
		return max(a.Min, b.Min), true
	case a.HasMin:
		return a.Min, true
	case b.HasMin:
		return b.Min, true
	}

	return 0, false
}
