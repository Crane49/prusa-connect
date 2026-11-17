package profile

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Coverage is what the profiles in this registry describe between them: a key
// no profile carries is absent from it, and a key some profile carries is
// present in the loosest form any of them gives it.
//
// It is the tier a printer no profile covers is held to. A nil profile tier
// narrows nothing, so without Coverage an identity nothing recognises would
// keep the driver's whole baseline and be offered more than a recognised
// printer.
//
// The union is per registry, so a resolver is built over the profiles of the
// driver whose baseline it resolves against. One registry holding two dialects'
// profiles would hold each dialect's unknown devices to the other's data.
//
// It is nil for a registry with no profiles. A nil tier is absent and narrows
// nothing. An empty tier would narrow everything away and leave a printer that
// accepts no command.
//
// This is the only place that widens. connectorapi has no union and must not
// get one. Coverage builds a tier out of data, and the merge that applies it
// is still an intersection bounded by the baseline.
func (r Registry) Coverage() connectorapi.Set {
	if len(r.Profiles) == 0 {
		return nil
	}

	out := make(connectorapi.Set)
	refused := map[connectorapi.Capability]bool{}

	for _, candidate := range r.Profiles {
		for _, key := range candidate.Capabilities.Keys() {
			if refused[key] {
				continue
			}

			value := candidate.Capabilities[key]

			held, seen := out[key]
			if !seen {
				value.Supported = slices.Clone(value.Supported)
				out[key] = value

				continue
			}

			widened, keep := widen(held, value)
			if !keep {
				refused[key] = true

				delete(out, key)

				continue
			}

			out[key] = widened
		}
	}

	return out
}

// widen returns the looser of two descriptions of one key. keep is false when
// the two differ in kind, because they have no single looser form. The key is
// then dropped. That holds an unknown device to less, and keeping one
// profile's reading would let file order decide.
func widen(a, b connectorapi.Value) (widened connectorapi.Value, keep bool) {
	if a.Kind != b.Kind {
		return connectorapi.Value{}, false
	}

	out := connectorapi.Value{
		Flag:      a.Flag || b.Flag,
		Kind:      a.Kind,
		Supported: widenSupported(a.Supported, b.Supported),
		Required:  a.Required && b.Required,
	}

	out.Default = widenDefault(a.Default, b.Default, out.Supported)
	out.Limit, out.HasLimit = widenLimit(a, b)
	out.Min, out.HasMin = widenMin(a, b)

	return out, true
}

// widenSupported unions two value sets. An empty set is unconstrained, so the
// union with it is unconstrained too.
func widenSupported(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}

	out := slices.Clone(a)

	for _, value := range b {
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}

	slices.Sort(out)

	return out
}

// widenDefault keeps a default only when both sides name the same one and it is
// still a legal value. When two profiles disagree about the default, the
// caller must name the value itself.
func widenDefault(a, b string, supported []string) string {
	if a != b || a == "" {
		return ""
	}

	if len(supported) > 0 && !slices.Contains(supported, a) {
		return ""
	}

	return a
}

// widenLimit takes the higher of two upper bounds, and no bound at all when
// either side declares none, because an undeclared bound is unbounded.
func widenLimit(a, b connectorapi.Value) (limit float64, has bool) {
	if !a.HasLimit || !b.HasLimit {
		return 0, false
	}

	return max(a.Limit, b.Limit), true
}

// widenMin takes the lower of two lower bounds, and no bound at all when either
// side declares none.
func widenMin(a, b connectorapi.Value) (lowest float64, has bool) {
	if !a.HasMin || !b.HasMin {
		return 0, false
	}

	return min(a.Min, b.Min), true
}
