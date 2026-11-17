package profile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Bound is the range one numeric argument may take on the models it names.
//
// It is hand-written data, like aliases.json and unlike profiles.json. The
// shipping firmware matrix records an argument's kind and not how large it may
// be, so without a bound a nozzle target of 9999 or of -4000 would reach the
// printer. The driver's baseline holds the widest range. A row here holds a
// model's own narrower range when somebody has measured one. Until then a row
// repeats the baseline and says so.
//
// A row can only narrow. It is folded into the profile tier, which the merge
// intersects with the driver's baseline, so a row wider than the baseline
// changes nothing.
type Bound struct {
	// Argument is the descriptor key this range applies to: an argument key,
	// not a verb, and one of a numeric kind.
	Argument connectorapi.Capability `json:"argument"`
	// Models are the models the range applies to, spelled as the profile file
	// spells them. A model this file does not carry is refused, because a typo
	// here would be a bound nothing enforces.
	Models []string `json:"models"`
	// Min is the inclusive lower bound on the value.
	Min float64 `json:"min"`
	// Max is the inclusive upper bound.
	Max float64 `json:"max"`
	// Source says where the numbers came from. A row nobody can trace back to a
	// published figure is a guess, and a guess here refuses a command somebody's
	// machine can carry out.
	Source string `json:"source"`
}

// covers reports whether the row names a model, by the same case rule the rest
// of the file matches models under.
func (b Bound) covers(model string) bool {
	for _, named := range b.Models {
		if strings.EqualFold(named, model) {
			return true
		}
	}

	return false
}

// clone copies the model list, so a caller cannot write through the rows it was
// handed into the registry somebody else holds.
func (b Bound) clone() Bound {
	b.Models = slices.Clone(b.Models)

	return b
}

// applyBounds folds every row into the profiles it names.
//
// It narrows and never widens: an argument that already carries a bound keeps
// the tighter of the two, so a row cannot loosen what the profile data says,
// and the merge with the driver's baseline narrows again on top of that. An
// argument a row names that a profile does not carry is left alone here and
// refused by Validate, which is where a stale row is reported.
func (r Registry) applyBounds() {
	for _, bound := range r.Bounds {
		for i := range r.Profiles {
			if !bound.covers(r.Profiles[i].Model) {
				continue
			}

			apply(r.Profiles[i].Capabilities, bound)
		}
	}
}

// apply narrows one argument of one profile.
func apply(capabilities connectorapi.Set, bound Bound) {
	value, ok := capabilities[bound.Argument]
	if !ok || !value.Flag || !value.Kind.IsNumeric() {
		return
	}

	if value.HasMin {
		value.Min = max(value.Min, bound.Min)
	} else {
		value.Min, value.HasMin = bound.Min, true
	}

	// Limit bounds the magnitude and Max bounds the value. The two are the same
	// number for a range whose ends are both positive, and for the one
	// negative floor this vocabulary carries. The driver spells a bound the
	// same way, in boundedInteger.
	if value.HasLimit {
		value.Limit = min(value.Limit, bound.Max)
	} else {
		value.Limit, value.HasLimit = bound.Max, true
	}

	capabilities[bound.Argument] = value
}

// boundProblems refuses a bound row that cannot be acted on: one naming
// something that is not an argument, one naming a model or an argument the file
// does not carry, one with its ends the wrong way round, and two rows bounding
// one argument of one model, which would make the answer depend on file order.
func boundProblems(registry Registry) []error {
	var problems []error

	claimed := map[string]bool{}

	for _, bound := range registry.Bounds {
		problems = append(problems, oneBoundProblems(registry, bound)...)

		for _, model := range bound.Models {
			key := strings.ToLower(model) + " " + bound.Argument.String()
			if claimed[key] {
				problems = append(problems, fmt.Errorf("%w: %s is bounded twice for %s",
					ErrInvalidBound, bound.Argument, model))
			}

			claimed[key] = true
		}
	}

	return problems
}

// oneBoundProblems is boundProblems for a single row.
func oneBoundProblems(registry Registry, bound Bound) []error {
	var problems []error

	if _, isArgument := bound.Argument.Argument(); !isArgument {
		problems = append(problems, fmt.Errorf("%w: %q is not an argument key",
			ErrInvalidBound, bound.Argument))

		return problems
	}

	if bound.Source == "" {
		problems = append(problems, fmt.Errorf(
			"%w: the bound on %s names no source. A number nobody can trace is a guess",
			ErrInvalidBound, bound.Argument))
	}

	if bound.Min > bound.Max {
		problems = append(problems, fmt.Errorf("%w: %s runs from %v to %v",
			ErrInvalidBound, bound.Argument, bound.Min, bound.Max))
	}

	if len(bound.Models) == 0 {
		problems = append(problems, fmt.Errorf("%w: the bound on %s names no model",
			ErrInvalidBound, bound.Argument))
	}

	for _, model := range bound.Models {
		problems = append(problems, modelBoundProblems(registry, bound, model)...)
	}

	return problems
}

// modelBoundProblems refuses a row against one of the models it names. A row
// for a model the file does not carry, or for an argument that model's profile
// does not carry, is a bound nothing enforces. It is refused, so the argument
// is not left unbounded without notice.
func modelBoundProblems(registry Registry, bound Bound, model string) []error {
	if !carries(registry, model) {
		return []error{fmt.Errorf("%w: %s bounds %s", ErrUnknownBoundModel, bound.Argument, model)}
	}

	for _, candidate := range registry.Profiles {
		if !strings.EqualFold(candidate.Model, model) {
			continue
		}

		value, ok := candidate.Capabilities[bound.Argument]
		if !ok || !value.Flag {
			return []error{fmt.Errorf("%w: %s does not carry %s",
				ErrInvalidBound, model, bound.Argument)}
		}

		if !value.Kind.IsNumeric() {
			return []error{fmt.Errorf("%w: %s carries %s as a %s, which has no range",
				ErrInvalidBound, model, bound.Argument, value.Kind)}
		}
	}

	return nil
}
