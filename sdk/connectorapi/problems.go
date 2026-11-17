package connectorapi

import (
	"fmt"
	"maps"
	"slices"
)

// Problems reports every key the set cannot use: an invalid key, a constraint
// that applies to nothing, a default the key's own rules refuse. It applies to
// a merged set as well as to a Baseline. It does not compare two sets.
func (s Set) Problems() []error {
	var problems []error

	for _, key := range s.Keys() {
		problems = append(problems, s.valueProblems(key)...)
	}

	return problems
}

// Prune returns the set with every key Problems refuses removed, the keys it
// removed, and the problem that removed each one. The result has no problems.
//
// It also removes a verb whose required argument was removed, and an argument
// whose verb was removed.
func (s Set) Prune() (sound Set, removed []Capability, problems []error) {
	sound = make(Set, len(s))
	maps.Copy(sound, s)

	for {
		round := sound.unusableKeys()
		if len(round) == 0 {
			break
		}

		for _, key := range round {
			problems = append(problems, sound.valueProblems(key)...)
			delete(sound, key)
		}

		dropVerbsMissingARequiredArgument(sound, s, s)
		dropOrphanArguments(sound)
	}

	// Report every key that was removed, including a verb dropped because its
	// required argument had a problem.
	for _, key := range s.Keys() {
		if _, kept := sound[key]; !kept {
			removed = append(removed, key)
		}
	}

	return sound, removed, problems
}

// unusableKeys returns every key of the set the descriptor rules refuse, sorted.
func (s Set) unusableKeys() []Capability {
	var keys []Capability

	for _, key := range s.Keys() {
		if len(s.valueProblems(key)) > 0 {
			keys = append(keys, key)
		}
	}

	return keys
}

func (s Set) valueProblems(key Capability) []error {
	var problems []error

	value := s[key]

	if !key.Valid() {
		problems = append(problems, fmt.Errorf("%q is not a valid capability key", key))
	}

	if !value.Kind.Valid() {
		problems = append(problems, fmt.Errorf("%s declares kind %d, which the vocabulary does not define",
			key, value.Kind))
	}

	if value.Default != "" && value.Required {
		problems = append(problems, fmt.Errorf(
			"%s is required and also declares the default %q. The default is always filled in, "+
				"so the argument can never be missing and nothing can ever refuse the command for it",
			key, value.Default))
	}

	if value.Default != "" && len(value.Supported) > 0 && !slices.Contains(value.Supported, value.Default) {
		problems = append(problems, fmt.Errorf("%s defaults to %q, which is not in its supported set",
			key, value.Default))
	}

	if value.Kind == KindFlag && (len(value.Supported) > 0 || value.HasLimit || value.HasMin) {
		problems = append(problems, fmt.Errorf(
			"%s is a flag but constrains values, so nothing will ever read those constraints", key))
	}

	if value.HasMin && !value.Kind.IsNumeric() {
		problems = append(problems, fmt.Errorf(
			"%s declares a lower bound of %v but is a %s, and a lower bound applies to a number only",
			key, value.Min, value.Kind))
	}

	if value.HasMin && value.HasLimit && value.Min > value.Limit {
		problems = append(problems, fmt.Errorf(
			"%s accepts from %v up to a magnitude of %v, which permits nothing", key, value.Min, value.Limit))
	}

	problems = append(problems, boundProblems(key, value)...)

	if value.Unit != "" && !value.Unit.Valid() {
		problems = append(problems, fmt.Errorf("%s counts in %q, and the units are %v",
			key, value.Unit, Units()))
	}

	if value.Unit != "" && !value.Kind.IsNumeric() {
		problems = append(problems, fmt.Errorf(
			"%s declares the unit %q but is a %s, and a unit applies to a number only",
			key, value.Unit, value.Kind))
	}

	problems = append(problems, s.defaultProblems(key, value)...)

	if name, isArgument := key.Argument(); isArgument && !s.Has(key.Verb()) {
		problems = append(problems, fmt.Errorf(
			"%s describes argument %q of a verb the set does not declare", key, name))
	}

	return problems
}

// boundProblems reports a bound that is NaN or infinite, and a negative Limit.
// A NaN bound would accept every value, and a negative Limit would accept none.
func boundProblems(key Capability, value Value) []error {
	var problems []error

	if value.HasLimit {
		switch {
		case !isFinite(value.Limit):
			problems = append(problems, fmt.Errorf(
				"%s bounds its magnitude at %v, which no value can be compared against", key, value.Limit))
		case value.Limit < 0:
			problems = append(problems, fmt.Errorf(
				"%s bounds its magnitude at %v, and a magnitude is never negative, so it permits nothing",
				key, value.Limit))
		}
	}

	if value.HasMin && !isFinite(value.Min) {
		problems = append(problems, fmt.Errorf(
			"%s declares a lower bound of %v, which no value can be compared against", key, value.Min))
	}

	return problems
}

// defaultProblems checks a declared default the same way as a supplied value.
// The host fills in defaults, so a bad default would reach a driver unchecked.
func (s Set) defaultProblems(key Capability, value Value) []error {
	if value.Default == "" {
		return nil
	}

	number, ok := readAs(value.Kind, value.Default)
	if !ok {
		return []error{fmt.Errorf("%s defaults to %q, which is not readable as a %s",
			key, value.Default, value.Kind)}
	}

	if err := rangeError(key.Verb(), key.String(), value, number); err != nil {
		return []error{fmt.Errorf("%s defaults to %q, which its own descriptor refuses: %w",
			key, value.Default, err)}
	}

	return nil
}
