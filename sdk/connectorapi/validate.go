package connectorapi

import (
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The two values a KindBoolean may have. Any other spelling, such as "1" or
// "TRUE", is refused.
const (
	boolTrue  = "true"
	boolFalse = "false"
)

// Validate checks a command against the set and returns the command a driver
// receives, with declared defaults filled in. The host calls it before Do, so
// a driver does not check its declared arguments again. An argument the set
// does not declare for the verb is refused.
func (s Set) Validate(cmd Command) (checked Command, err error) {
	if _, isArgument := cmd.Verb.Argument(); isArgument {
		// An argument key is in the set but is not a verb. Refuse it, or the
		// verb would run with no argument checks.
		return Command{}, s.refuseVerb(cmd.Verb)
	}

	if !s.Has(cmd.Verb) {
		return Command{}, s.refuseVerb(cmd.Verb)
	}

	for _, name := range slices.Sorted(maps.Keys(cmd.Args)) {
		if !s.Has(ArgumentKey(cmd.Verb, name)) {
			return Command{}, ArgumentUnknown.New("argument not accepted").
				With(fault.StringParam("verb", cmd.Verb.String()),
					fault.StringParam("argument", name))
		}
	}

	args := maps.Clone(cmd.Args)
	if args == nil {
		args = make(map[string]string)
	}

	for _, key := range s.argumentsOf(cmd.Verb) {
		name, _ := key.Argument()

		value, supplied := args[name]
		if !supplied {
			filled, fillErr := s.fill(cmd.Verb, key, name)
			if fillErr != nil {
				return Command{}, fillErr
			}

			if filled == "" {
				continue
			}

			args[name] = filled

			continue
		}

		if checkErr := s.check(cmd.Verb, key, name, value); checkErr != nil {
			return Command{}, checkErr
		}
	}

	return Command{Verb: cmd.Verb, Args: args, Key: cmd.Key}, nil
}

// maxOfferedVerbs is how many verbs one refusal lists at most, so a driver
// with many capabilities cannot make a refusal arbitrarily large.
const maxOfferedVerbs = 128

// refuseVerb refuses a verb the printer does not have and lists the verbs it
// has. verb_count is the total, so a caller can tell when the list is cut.
func (s Set) refuseVerb(verb Capability) error {
	offered, count := s.offeredVerbs()

	return CapabilityNotSupported.New("capability not supported").
		With(fault.StringParam("verb", verb.String()),
			fault.StringParam("verbs", offered),
			fault.IntParam("verb_count", int64(count)))
}

// offeredVerbs renders the first maxOfferedVerbs verbs as a JSON array, and how
// many the printer has in total.
func (s Set) offeredVerbs() (encoded string, count int) {
	verbs := s.Verbs()

	names := make([]string, 0, min(len(verbs), maxOfferedVerbs))
	for _, verb := range verbs[:min(len(verbs), maxOfferedVerbs)] {
		names = append(names, verb.String())
	}

	return encodeList(names), len(verbs)
}

// argumentsOf returns the argument keys of one verb, sorted, so a command
// with two bad arguments always reports the same one.
func (s Set) argumentsOf(verb Capability) []Capability {
	var keys []Capability

	for key, v := range s {
		if !v.Flag || key.Verb() != verb {
			continue
		}

		if _, isArgument := key.Argument(); isArgument {
			keys = append(keys, key)
		}
	}

	slices.Sort(keys)

	return keys
}

// fill returns the default for an argument the caller did not supply, or the
// empty string for an optional argument. The default is checked like a
// supplied value.
func (s Set) fill(verb, key Capability, name string) (string, error) {
	v := s[key]

	if v.Default != "" {
		if err := s.check(verb, key, name, v.Default); err != nil {
			return "", err
		}

		return v.Default, nil
	}

	if v.Required {
		return "", ArgumentMissing.New("required argument missing").
			With(fault.StringParam("verb", verb.String()),
				fault.StringParam("argument", name))
	}

	return "", nil
}

// check refuses a supplied value the descriptor does not permit.
func (s Set) check(verb, key Capability, name, value string) error {
	v := s[key]

	if len(v.Supported) > 0 && !slices.Contains(v.Supported, value) {
		return ArgumentNotSupported.New("argument value not supported").
			With(fault.StringParam("verb", verb.String()),
				fault.StringParam("argument", name),
				fault.StringParam("supported", encodeList(v.Supported)))
	}

	number, ok := readAs(v.Kind, value)
	if !ok {
		return ArgumentMalformed.New("argument not readable as its kind").
			With(fault.StringParam("verb", verb.String()),
				fault.StringParam("argument", name),
				fault.StringParam("kind", v.Kind.String()))
	}

	return rangeError(verb, name, v, number)
}

// rangeError refuses a value outside the declared bounds. Limit bounds the
// absolute value and Min bounds a numeric value. A bound that is NaN or
// infinite refuses every value.
func rangeError(verb Capability, name string, v Value, number float64) error {
	if v.HasLimit && (!isFinite(v.Limit) || math.Abs(number) > v.Limit) {
		return ArgumentOutOfRange.New("argument outside the declared range").
			With(fault.StringParam("verb", verb.String()),
				fault.StringParam("argument", name),
				fault.FloatParam("max", v.Limit))
	}

	if v.HasMin && v.Kind.IsNumeric() && (!isFinite(v.Min) || number < v.Min) {
		return ArgumentOutOfRange.New("argument outside the declared range").
			With(fault.StringParam("verb", verb.String()),
				fault.StringParam("argument", name),
				fault.FloatParam("min", v.Min))
	}

	return nil
}

// encodeList renders a list as a JSON array, so a value that contains a comma
// reads back whole.
func encodeList(values []string) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		// json.Marshal of a []string cannot fail.
		return ""
	}

	return string(encoded)
}

// readAs reads a value as its declared kind and returns the number the bounds
// apply to: the number itself, or the length of a string. ok is false when the
// text is not readable as that kind. NaN and infinity are refused, because
// NaN would pass every bound check.
func readAs(kind Kind, value string) (number float64, ok bool) {
	switch kind {
	case KindInteger:
		n, err := strconv.ParseInt(value, 10, 64)

		return float64(n), err == nil
	case KindNumber:
		if !decimalOnly(value) {
			return 0, false
		}

		n, err := strconv.ParseFloat(value, 64)
		if err != nil || !isFinite(n) {
			return 0, false
		}

		return n, true
	case KindBoolean:
		return 0, value == boolTrue || value == boolFalse
	case KindString:
		return float64(len(value)), true
	case KindFlag, KindOpaque:
		return 0, true
	default:
		return 0, false
	}
}

// decimalOnly refuses the hex, binary-exponent and underscore spellings that
// strconv.ParseFloat accepts, such as "0x1p3". A driver receives the text, and
// its own parser may read such a spelling differently.
func decimalOnly(value string) bool {
	return !strings.ContainsAny(value, "xXpP_")
}

// isFinite reports whether f is neither NaN nor infinite.
func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
