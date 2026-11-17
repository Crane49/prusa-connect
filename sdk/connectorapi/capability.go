// Package connectorapi is the contract a printer driver implements and a host
// drives it through. A driver declares what its code can do with Baseline, and
// takes commands through the single Do entry point on a Session.
//
// A driver implements Outbound when the host dials the device, Inbound when
// the device dials the host, or both. Sessions behave the same either way.
//
// A profile or a probe may remove a capability that Baseline declared. It may
// never add one.
//
// A capability key or an argument name never changes its meaning. A changed
// meaning gets a new name and the old name is retired.
package connectorapi

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

const (
	// CorePrefix starts every key the host defines. A driver's own keys start
	// with its module path, so two drivers cannot collide.
	CorePrefix = "core."

	// ArgumentSeparator joins a verb key to one of its argument names. It is
	// not "." because argument names contain dots (chamber.fan_pwm_target).
	ArgumentSeparator = extension.CapabilityArgumentSeparator
)

// Capability is one key in the closed vocabulary: a dotted lowercase name,
// optionally followed by ArgumentSeparator and the name of one argument of that
// verb. Each dotted segment is a DNS-1123 label that may also contain "_".
//
//	core.command.start_print
//	core.command.start_print#path
//	core.feature.dialogs
type Capability string

// ArgumentKey returns the descriptor key for one argument of a verb. It does
// not validate the result. Check it with Valid before putting it in a
// descriptor.
func ArgumentKey(verb Capability, argument string) Capability {
	return verb + ArgumentSeparator + Capability(argument)
}

// Verb returns the verb half of an argument key, and the key itself when it has
// no argument half.
func (c Capability) Verb() Capability {
	if verb, _, found := strings.Cut(string(c), ArgumentSeparator); found {
		return Capability(verb)
	}

	return c
}

// Argument returns the argument half of an argument key. ok is false for a key
// that names a verb or a feature.
func (c Capability) Argument() (name string, ok bool) {
	_, argument, found := strings.Cut(string(c), ArgumentSeparator)

	return argument, found
}

// IsCore reports whether the host defines this key.
func (c Capability) IsCore() bool { return strings.HasPrefix(string(c), CorePrefix) }

// Valid reports whether the key satisfies the vocabulary's syntax: dotted
// segments of lowercase alphanumerics with '-' and '_' between, optionally an
// argument half. It says nothing about whether anything declares the key.
func (c Capability) Valid() bool { return extension.IsCapabilityKey(string(c)) }

// String returns the key as text.
func (c Capability) String() string { return string(c) }

// Kind is the type of a capability's argument. Arguments travel as text, and
// the host reads each one by its kind before a driver sees it.
type Kind uint8

// The kinds of an argument. KindFlag is the zero value, for a key with no
// value such as a verb or a firmware feature.
const (
	// KindFlag has no argument value.
	KindFlag Kind = iota
	// KindString is free text, bounded by Limit when one is set.
	KindString
	// KindInteger is a whole number.
	KindInteger
	// KindNumber is a fractional number.
	KindNumber
	// KindBoolean is "true" or "false".
	KindBoolean
	// KindOpaque is a value the host passes through untouched, such as an
	// embedded JSON document. Supported and Limit do not apply to it.
	KindOpaque
)

// Valid reports whether k is one of the defined kinds.
func (k Kind) Valid() bool { return k <= KindOpaque }

// IsNumeric reports whether the kind is a number. Value.Min applies only to
// numeric kinds.
func (k Kind) IsNumeric() bool { return k == KindInteger || k == KindNumber }

// String returns the kind's name as a profile file writes it.
func (k Kind) String() string {
	switch k {
	case KindFlag:
		return "flag"
	case KindString:
		return "string"
	case KindInteger:
		return "integer"
	case KindNumber:
		return "number"
	case KindBoolean:
		return "boolean"
	case KindOpaque:
		return "opaque"
	default:
		return "invalid"
	}
}

// ParseKind returns the kind a profile file names. ok is false for an unknown
// name.
func ParseKind(s string) (kind Kind, ok bool) {
	for candidate := KindFlag; candidate <= KindOpaque; candidate++ {
		if candidate.String() == s {
			return candidate, true
		}
	}

	return KindFlag, false
}

// MarshalText encodes the kind as its name.
func (k Kind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// UnmarshalText decodes a kind name. It reports ErrUnknownKind for an unknown
// name.
func (k *Kind) UnmarshalText(text []byte) error {
	parsed, ok := ParseKind(string(text))
	if !ok {
		return errUnknownKind(string(text))
	}

	*k = parsed

	return nil
}

// Unit is what a numeric argument counts in. The set is closed, so a client
// can label and convert every value.
type Unit string

// The units an argument may declare. The zero value declares none, as for a
// count or an index.
const (
	// UnitCelsius is a temperature in degrees Celsius.
	UnitCelsius Unit = "celsius"
	// UnitPercent is a share of a whole, or of normal, in percent.
	UnitPercent Unit = "percent"
	// UnitMillimeter is a length or a displacement in millimetres.
	UnitMillimeter Unit = "mm"
	// UnitMillimeterPerMinute is a speed in millimetres per minute, the unit
	// of a G-code feedrate.
	UnitMillimeterPerMinute Unit = "mm/min"
	// UnitSecond is a duration in seconds.
	UnitSecond Unit = "s"
	// UnitByte is a size in bytes.
	UnitByte Unit = "byte"
)

// Units returns every unit an argument may declare, in the order the reference
// lists them.
func Units() []Unit {
	return []Unit{UnitCelsius, UnitPercent, UnitMillimeter, UnitMillimeterPerMinute, UnitSecond, UnitByte}
}

// Valid reports whether u is a member of the closed set. The zero value is not.
func (u Unit) Valid() bool { return slices.Contains(Units(), u) }

// String returns the unit as a descriptor writes it.
func (u Unit) String() string { return string(u) }

// Value is what one capability permits: whether it is present (Flag), which
// values it takes (Supported, Min, Limit), and what happens when the caller
// sends none (Default, Required).
//
// Each argument of a verb is its own key, built with ArgumentKey. A verb
// accepts only the arguments that have a key.
type Value struct {
	// Flag is whether the capability is present at all.
	Flag bool `json:"flag"`
	// Kind is how the argument's text is read. KindFlag for a verb or feature.
	Kind Kind `json:"kind,omitempty"`
	// Supported is the closed set of legal values. Empty means unconstrained:
	// anything the Kind can read is accepted.
	Supported []string `json:"supported,omitempty"`
	// Default is what the host sends when the caller is silent. Empty means
	// there is none.
	Default string `json:"default,omitempty"`
	// Required is whether the caller must supply a value. A descriptor that
	// sets both Required and Default is refused.
	Required bool `json:"required,omitempty"`
	// Limit is the inclusive upper bound on the absolute value of a number, or
	// on the length in bytes of a string. Limit 300 refuses both 301 and -301.
	Limit float64 `json:"limit,omitempty"`
	// Deprecated marks a key that is going away. Nil otherwise.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
	// HasLimit distinguishes a limit of zero from no limit at all.
	HasLimit bool `json:"has_limit,omitempty"`
	// Min is the inclusive lower bound on a numeric value. An argument that
	// must not go negative declares Min 0. It does not apply to a string.
	Min float64 `json:"min,omitempty"`
	// HasMin distinguishes a lower bound of zero from no lower bound at all.
	HasMin bool `json:"has_min,omitempty"`
	// Unit is what a numeric value counts in. Empty for a value with no unit.
	// The host does not check it. A client labels and converts the value by it.
	Unit Unit `json:"unit,omitempty"`
}
