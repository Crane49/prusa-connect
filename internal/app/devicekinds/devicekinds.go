// Package devicekinds turns the device-kind declarations compiled into this
// binary into the per-build half of the authorization verb table.
//
// An adapter writes its declaration with sdk/connectorapi beside its
// Baseline(). The host derives the namespace from the declaring module, holds
// the declaration to that namespace, computes the opaque_payload marking from
// the capability descriptor, and hands the result to internal/authz.
//
// A declaration is a request the host validates. The host computes the
// opaque_payload marking itself: a verb with an unconstrained argument is
// marked whatever the declaration says, and nobody can clear the host's
// marking. This catches a declaration that claims too little. It does not
// catch a destructive verb whose arguments are all constrained. An operator
// marks such a verb by hand.
package devicekinds

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Compiled is everything one drain of the gathering list produced: the
// declarations the host accepted, the ones it refused with the reason, and
// which device kind each declaring driver type's devices are registered under.
//
// It is one value because the gathering list drains once. A second drain
// returns nothing, so a caller that wanted the declarations and the driver map
// would get one of the two.
type Compiled struct {
	// Declarations is what internal/authz installs as the per-build half.
	Declarations []authz.KindDeclaration
	// Refused is the declarations the host would not derive, keyed by the
	// qualified name the extension catalogue lists the kind under, with the
	// reason. A refusal reaches the kind's health condition and never the boot.
	//
	// The key is the qualified name so that the refusal can be joined to the
	// extension catalogue. The catalogue lists what registered, and this table
	// holds what the host accepted.
	Refused map[string]string
	// Drivers is the qualified driver type name to the device kind its devices
	// are registered under. A driver type no declaration names is absent, and
	// its devices are printers.
	Drivers map[string]authz.DeviceKind
}

// Declarations drains the gathering list and converts what it holds.
func Declarations() Compiled { return Convert(connectorapi.DrainKinds()...) }

// Convert is Declarations over registrations a caller already holds, for a host
// that wires its own kinds and for a test.
func Convert(registrations ...connectorapi.KindRegistration) Compiled {
	out := Compiled{Refused: map[string]string{}, Drivers: map[string]authz.DeviceKind{}}

	for _, registration := range registrations {
		declaration, err := convert(registration)
		if err != nil {
			out.Refused[registration.Descriptor().Qualified()] = err.Error()

			continue
		}

		out.Declarations = append(out.Declarations, declaration)

		namespace := registration.Descriptor().Namespace().String()
		for _, driver := range registration.Declaration().DriverTypes {
			out.Drivers[namespace+"/"+driver] = declaration.Kind
		}
	}

	return out
}

// convert is one registration: the namespace check and the marking computation.
func convert(registration connectorapi.KindRegistration) (authz.KindDeclaration, error) {
	descriptor := registration.Descriptor()
	declared := registration.Declaration()

	namespace := descriptor.Namespace().String()
	if namespace == "" || namespace == extension.CoreNamespace.String() {
		return authz.KindDeclaration{}, fmt.Errorf(
			"%w: %s is declared under the core namespace, which is this project's own",
			ErrNamespace, declared.Kind)
	}

	if err := connectorapi.ValidateKindDeclaration(declared); err != nil {
		return authz.KindDeclaration{}, err
	}

	// The kind is qualified by the namespace the host derived, and never by one
	// the module wrote. The namespace is read from the build's module graph, so
	// a module cannot claim another's name by spelling it.
	kind := authz.DeviceKind(namespace + "/" + declared.Kind)

	verbs := make(map[string]authz.DeclaredVerb, len(declared.Verbs))

	for _, verb := range slices.Sorted(maps.Keys(declared.Verbs)) {
		entry := declared.Verbs[verb]

		if !inNamespace(verb, namespace) {
			return authz.KindDeclaration{}, fmt.Errorf(
				"%w: %s declares the verb %s, which is not under %s. A kind declares verbs in its own "+
					"module's namespace and in no other",
				ErrNamespace, declared.Kind, verb, namespace)
		}

		markings := map[authz.Marking]authz.Marker{}

		// The adapter's own markings come first: an adapter may always mark its
		// own verb dangerous. The host's marking is written after, so it wins
		// the key if the adapter claimed the same one. The marker then reads
		// "host" for a marking the host makes anyway.
		for _, marking := range entry.Dangerous {
			markings[authz.Marking(marking)] = authz.MarkerAdapter
		}

		if Unconstrained(declared.Capabilities, verb) {
			markings[authz.MarkingOpaquePayload] = authz.MarkerHost
		}

		verbs[verb] = authz.DeclaredVerb{Class: authz.Action(entry.Class), Markings: markings}
	}

	return authz.KindDeclaration{
		Module:      descriptor.Module(),
		Kind:        kind,
		DisplayName: declared.DisplayName,
		Verbs:       verbs,
	}, nil
}

// ErrNamespace reports a declaration outside the namespace the host derived for
// the declaring module.
var ErrNamespace = errors.New("devicekinds: declaration is outside its module's namespace")

// Unconstrained reports whether any argument of one verb is unconstrained.
//
// An argument is unconstrained when any of these holds:
//
//   - its kind is KindOpaque, a value the host passes through untouched.
//     Supported and Limit do not apply to it, so it is always unconstrained;
//   - it is a string with no Supported set;
//   - it is a numeric kind with no declared magnitude bound. HasLimit bounds a
//     number, because the Limit is a magnitude and bounds both sides. A Min
//     alone leaves the value unbounded upward and does not count.
//
// A verb with no arguments reports false, and it can still be harmful:
// factory_reset takes no argument. An operator marks such a verb by hand.
func Unconstrained(set connectorapi.Set, verb string) bool {
	for key, value := range set {
		if key.Verb() != connectorapi.Capability(verb) {
			continue
		}

		if _, isArgument := key.Argument(); !isArgument {
			continue
		}

		if unconstrainedValue(value) {
			return true
		}
	}

	return false
}

// unconstrainedValue is the per-argument half of the rule.
func unconstrainedValue(value connectorapi.Value) bool {
	switch {
	case value.Kind == connectorapi.KindOpaque:
		return true
	case value.Kind == connectorapi.KindString:
		return len(value.Supported) == 0
	case value.Kind.IsNumeric():
		return !value.HasLimit
	default:
		// A flag carries no value and a boolean carries one of two, so neither
		// can be unconstrained.
		return false
	}
}

// inNamespace reports whether a verb is spelled under one namespace. The prefix
// must end on a dot: com.example.aux.shelfish must not pass as
// com.example.aux.shelf.
func inNamespace(verb, namespace string) bool {
	return strings.HasPrefix(verb, namespace+".")
}
