package connectorapi

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// A driver declares its own verbs, each with a Class. The host checks that the
// caller's grant includes that class.

// Class is the permission a caller needs to send a verb. The set is closed
// and the host owns it. There is no read class: every verb sends a command to
// the device, so the lowest class is ClassOperate.
type Class string

// The classes a verb may have, lowest first.
const (
	// ClassOperate is the lowest class: act on the machine in its normal duty.
	ClassOperate Class = "operate"
	// ClassConfigure changes what the machine is, not the work on it.
	ClassConfigure Class = "configure"
)

// Classes returns the classes a verb may have, lowest first.
func Classes() []Class { return []Class{ClassOperate, ClassConfigure} }

// Valid reports whether c is a class a verb may have.
func (c Class) Valid() bool { return c == ClassOperate || c == ClassConfigure }

// String returns the class as it is written in a declaration.
func (c Class) String() string { return string(c) }

// Marking is a reason a verb is dangerous. No class or role grants a dangerous
// verb. Only a grant that names the subject, the device and the verb does.
type Marking string

// The markings. A verb may have both.
const (
	// MarkingIrreversible is an act this server cannot undo: firmware that
	// cannot be put back, a token that hands the machine to another account.
	MarkingIrreversible Marking = "irreversible"
	// MarkingOpaquePayload is a payload the host cannot inspect. The host
	// computes it from the capability descriptor. A declaration cannot clear it.
	MarkingOpaquePayload Marking = "opaque_payload"
)

// Valid reports whether m is one of the markings.
func (m Marking) Valid() bool {
	return m == MarkingIrreversible || m == MarkingOpaquePayload
}

// String returns the marking as it is written in a declaration.
func (m Marking) String() string { return string(m) }

// VerbEntry is one verb of a kind declaration: its class and the markings the
// driver puts on it. A driver may add a marking to its own verb. It cannot
// clear one the host computed.
type VerbEntry struct {
	// Class is the permission the verb requires. Required.
	Class Class `json:"class"`
	// Dangerous is the markings the driver puts on this verb. Empty for none.
	Dangerous []Marking `json:"dangerous,omitempty"`
}

// KindDeclaration is one device kind and its verbs. The host derives the
// namespace from the declaring module.
type KindDeclaration struct {
	// Kind is the bare device-kind name, a DNS-1123 label.
	Kind string `json:"kind"`
	// DisplayName is what an operator reads in a listing.
	DisplayName string `json:"display_name"`
	// Verbs is every verb of this kind, keyed by the full verb name. Each name
	// must be in the declaring module's own namespace.
	Verbs map[string]VerbEntry `json:"verbs"`
	// DriverTypes is the bare names of the driver types whose devices are of
	// this kind, as registered with Register. A type the declaring module did
	// not register is refused.
	DriverTypes []string `json:"driver_types,omitempty"`
	// Capabilities is the verb keys and their argument keys, as Baseline
	// returns them. The host computes the opaque_payload marking from it.
	Capabilities Set `json:"-"`
}

// Errors a kind declaration reports.
var (
	// ErrNoVerbs reports a kind declaration with no verbs.
	ErrNoVerbs = errors.New("connectorapi: kind declaration has no verbs")
	// ErrInvalidClass reports a verb whose class is outside the closed set.
	ErrInvalidClass = errors.New("connectorapi: verb class is not one a verb may carry")
	// ErrInvalidMarking reports a marking outside the closed set.
	ErrInvalidMarking = errors.New("connectorapi: marking is not one the host knows")
)

// KindRegistration is one declared kind, with the descriptor the host derived
// for it.
type KindRegistration struct {
	descriptor  extension.Descriptor
	declaration KindDeclaration
}

// Descriptor returns what the host derived: the namespace, the module path and
// the documented name.
func (r KindRegistration) Descriptor() extension.Descriptor { return r.descriptor }

// Declaration returns the kind and its verbs as the driver wrote them.
func (r KindRegistration) Declaration() KindDeclaration { return r.declaration }

// IsZero reports whether r was never constructed.
func (r KindRegistration) IsZero() bool { return r.descriptor.IsZero() }

// declaredKinds is the list init functions write to. The host drains it once.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var declaredKinds extension.Set[KindDeclaration]

// RegisterKind declares one device kind and its verbs. It panics on a
// registration the host would refuse. Call it from an init function.
func RegisterKind(declaration KindDeclaration, doc extension.Doc) {
	if err := TryRegisterKind(declaration, doc); err != nil {
		panic(err.Error())
	}
}

// TryRegisterKind declares one device kind and returns an error where
// RegisterKind panics.
func TryRegisterKind(declaration KindDeclaration, doc extension.Doc) error {
	registration, err := DeclareKind(declaration, doc)
	if err != nil {
		return err
	}

	return declaredKinds.Add(extension.Entry[KindDeclaration]{
		Descriptor: registration.descriptor, Value: registration.declaration,
	})
}

// DeclareKind validates one declaration and derives its namespace, without
// registering it. It is for a host that wires its own kinds and for tests.
func DeclareKind(declaration KindDeclaration, doc extension.Doc) (KindRegistration, error) {
	descriptor, err := extension.Declare(extension.KindDeviceKind, Contract, declaration.Kind, doc)
	if err != nil {
		return KindRegistration{}, err
	}

	if err = ValidateKindDeclaration(declaration); err != nil {
		return KindRegistration{}, err
	}

	return KindRegistration{descriptor: descriptor, declaration: declaration.clone()}, nil
}

// DrainKinds seals the list of declared kinds and returns it. The host calls
// it once, after every init function has run.
func DrainKinds() []KindRegistration {
	entries := declaredKinds.Drain()

	out := make([]KindRegistration, 0, len(entries))
	for _, entry := range entries {
		out = append(out, KindRegistration{descriptor: entry.Descriptor, declaration: entry.Value})
	}

	return out
}

// ValidateKindDeclaration checks the verb syntax, the classes and the
// markings of a declaration. It does not check the namespace of a verb. The
// host does that.
func ValidateKindDeclaration(declaration KindDeclaration) error {
	if len(declaration.Verbs) == 0 {
		return fmt.Errorf("%w: %s", ErrNoVerbs, declaration.Kind)
	}

	for _, verb := range slices.Sorted(maps.Keys(declaration.Verbs)) {
		entry := declaration.Verbs[verb]

		if !Capability(verb).Valid() {
			return fmt.Errorf("%w: %q is not a capability key", extension.ErrInvalidName, verb)
		}

		if !entry.Class.Valid() {
			return fmt.Errorf("%w: %q carries %q, and a verb carries one of %v",
				ErrInvalidClass, verb, entry.Class, Classes())
		}

		for _, marking := range entry.Dangerous {
			if !marking.Valid() {
				return fmt.Errorf("%w: %q carries %q", ErrInvalidMarking, verb, marking)
			}
		}
	}

	return nil
}

// clone copies every map and slice, so the registry keeps nothing the
// registering module can still write to.
func (d KindDeclaration) clone() KindDeclaration {
	out := d
	out.Verbs = make(map[string]VerbEntry, len(d.Verbs))

	for verb, entry := range d.Verbs {
		entry.Dangerous = slices.Clone(entry.Dangerous)
		out.Verbs[verb] = entry
	}

	out.DriverTypes = slices.Clone(d.DriverTypes)
	out.Capabilities = d.Capabilities.Clone()

	return out
}
