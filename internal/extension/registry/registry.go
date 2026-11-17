// Package registry is compile-time registration of resource types: connectors,
// storage backends, identity providers, databases and their siblings that are
// compiled into the binary.
//
// A type registers itself from an init function, which a build reaches through
// a blank import. Registration happens before main, so the declarations
// gathered at init are drained once, by New, into a Registry the application
// owns and passes around. Nothing changes a Registry after that.
//
// The registration rule, the namespace and the descriptor every entry carries
// come from sdk/extension. A type compiled in here and a plugin registered
// through a published contract follow the same rule.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Contract is the version of the resource contract this build serves. A type
// declares the version it compiled against and is refused at registration when
// the two are incompatible.
const Contract = extension.Version("1.0.0")

// Errors reported when a type is declared or resolved. Callers test them with
// errors.Is. Several are sdk/extension's own values, so a caller that checks
// either package sees the same error.
var (
	// ErrInvalidType reports a type name that is not a DNS-1123 label.
	ErrInvalidType = errors.New("registry: invalid type name")
	// ErrDuplicateType reports one qualified name registered twice. The second
	// registration is refused and does not replace the first, so a plugin
	// cannot take over a built-in name.
	ErrDuplicateType = extension.ErrDuplicate
	// ErrUnknownType reports a name no registered type answers to.
	ErrUnknownType = extension.ErrUnknown
	// ErrAmbiguousType reports a bare name that two namespaces both answer to.
	ErrAmbiguousType = extension.ErrAmbiguous
	// ErrTypeMismatch reports a constructor whose value is not the contract the
	// caller asked for.
	ErrTypeMismatch = errors.New("registry: constructed value has another type")
	// ErrNoConstructor reports a declaration with no constructor.
	ErrNoConstructor = errors.New("registry: type has no constructor")
	// ErrUnregisterableKind reports a real kind that has no type to register:
	// an intrinsic kind nobody constructs, or stored configuration with nothing
	// behind it. ref.ErrUnknownKind is the other case, a kind outside the
	// closed set, and the two need different corrections.
	ErrUnregisterableKind = errors.New("registry: kind registers no type")
	// ErrSealed reports a registration after the application drained the
	// gathering list.
	ErrSealed = extension.ErrSealed
	// ErrUndocumented reports a registration missing a mandatory descriptor
	// field.
	ErrUndocumented = extension.ErrUndocumented
	// ErrNoNamespace reports a registration whose module the build graph does
	// not account for.
	ErrNoNamespace = extension.ErrNoNamespace
)

// CoreNamespace is the namespace of every type registered from the main module.
// The namespace comes from the build's module graph and not from anything an
// author writes, so a type from another module cannot claim it.
const CoreNamespace = extension.CoreNamespace

// Namespace is the collision domain of a resource type. The host derives it
// from the module that registers the type.
type Namespace = extension.Namespace

// constructor is the type-erased builder a declaration carries, so one
// gathering list serves every contract.
type constructor func(context.Context, host.Handle, Spec) (any, error)

// pointOf maps a resource kind onto the extension point it serves, so a
// filesystem registered here and one registered through sdk/storageapi appear
// under the same heading in the published reference.
//
//nolint:gochecknoglobals,exhaustive // a lookup table of the configurable kinds, never written to.
var pointOf = map[ref.Kind]extension.Kind{
	ref.KindStorage: extension.KindStorage,
	ref.KindAuth:    extension.KindIdentity,
	ref.KindSink:    extension.KindEventSink,
	ref.KindDriver:  extension.KindConnector,
	ref.KindDB:      extension.KindDatabase,
	ref.KindCamera:  extension.KindCamera,
	ref.KindHistory: extension.KindHistory,
	ref.KindLink:    extension.KindLink,
}

// kindOf reverses pointOf. A descriptor carries the extension point; the
// application still speaks in resource kinds.
func kindOf(d extension.Descriptor) ref.Kind {
	for kind, point := range pointOf {
		if point == d.Kind() {
			return kind
		}
	}

	return ""
}

// declared is the gathering list init functions write to, and the only package
// state here. The behaviour lives on Declarations, so a test works on a value
// and not on process-wide state.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var declared Declarations

// Declarations is a gathering list of types waiting to become a Registry.
// Registration happens in an init function, before the application exists, so
// the declarations are collected and drained once into a value the application
// owns.
//
// The zero value is an empty list, ready to use, and safe for concurrent use.
type Declarations struct {
	set extension.Set[constructor]
}

// Add appends one declaration. It reports ErrSealed once the list has been
// drained, and ErrDuplicateType for a qualified name this kind already has. A
// registration that arrives after the registry was built would otherwise do
// nothing, with no sign of it anywhere.
func (d *Declarations) Add(t Type) error {
	if t.IsZero() {
		return fmt.Errorf("%w: %s/%s", ErrNoConstructor, t.kind, t.Name())
	}

	return d.set.Add(extension.Entry[constructor]{Descriptor: t.descriptor, Value: t.construct})
}

// Drain seals the list and returns its types. Draining twice returns the same
// types, so two callers cannot get two different registries.
func (d *Declarations) Drain() []Type { return typesOf(d.set.Drain()) }

// Descriptors returns the documentation of everything declared, without sealing
// the list, for the reference generator.
func (d *Declarations) Descriptors() []extension.Descriptor { return d.set.Descriptors() }

// Sealed reports whether the list has been drained.
func (d *Declarations) Sealed() bool { return d.set.Sealed() }

// Descriptors returns what init functions declared, without sealing the package
// gathering list.
func Descriptors() []extension.Descriptor { return declared.Descriptors() }

// Sealed reports whether the package gathering list has been drained.
func Sealed() bool { return declared.Sealed() }

// Spec is one operator config entry, resolved to an instance identity. The
// config block stays encoded, because its fields belong to the type and the
// host does not read them.
type Spec struct {
	// Ref is the operator-chosen name, the only name that appears in metric
	// labels, log fields, API paths and event subjects.
	Ref ref.Ref
	// UID is the host-minted identity data rows reference.
	UID ref.UID
	// Type is the name as written in config, bare or qualified.
	Type string
	// Config is the type-private block, untouched by the host.
	Config json.RawMessage
}

// DecodeConfig unmarshals the type-private block into v.
func (s Spec) DecodeConfig(v any) error {
	if len(s.Config) == 0 {
		return nil
	}

	if err := json.Unmarshal(s.Config, v); err != nil {
		return fmt.Errorf("decode config for %s: %w", s.Ref, err)
	}

	return nil
}

// Type is one registered resource type: what kind it serves, its descriptor,
// and how an instance of it is built.
type Type struct {
	kind       ref.Kind
	descriptor extension.Descriptor
	construct  constructor
}

// Kind returns the closed-set kind the type serves.
func (t Type) Kind() ref.Kind { return t.kind }

// Name returns the bare name, as an operator writes it in config.
func (t Type) Name() string { return t.descriptor.Name() }

// Namespace returns the host-derived owner of the name.
func (t Type) Namespace() Namespace { return t.descriptor.Namespace() }

// Descriptor returns the identity and documentation this type registered.
func (t Type) Descriptor() extension.Descriptor { return t.descriptor }

// Qualified renders "namespace/name", which is unique across a build.
func (t Type) Qualified() string { return t.descriptor.Qualified() }

// IsZero reports whether the type was never declared.
func (t Type) IsZero() bool { return t.construct == nil }

// Declare returns a type whose constructor yields T, without registering it. The
// namespace is read from the module of the calling package, so a caller cannot
// choose one.
func Declare[T any](
	kind ref.Kind, name string, doc extension.Doc, construct func(context.Context, host.Handle, Spec) (T, error),
) (Type, error) {
	return declare(kind, name, doc, construct)
}

// ErrForeignDescriptor reports a descriptor that cannot be adopted for the kind
// the caller named: an extension point that is not the kind's, or a descriptor
// with nothing in it.
var ErrForeignDescriptor = errors.New("registry: descriptor does not belong to this kind")

// Adopt returns a type carrying a descriptor somebody else already minted.
// Declare mints a new one for the calling package.
//
// A registration that arrived through a published contract, such as
// sdk/eventsapi, already carries the namespace derived from the registrant's
// module. Declaring it again here would give it this package's namespace.
//
// The descriptor is safe to trust. sdk/extension.Declare builds it from the
// live call stack and the module graph, so its author cannot choose the
// namespace inside it.
func Adopt[T any](
	kind ref.Kind, descriptor extension.Descriptor,
	construct func(context.Context, host.Handle, Spec) (T, error),
) (Type, error) {
	class, err := kind.Classify()
	if err != nil {
		return Type{}, err
	}

	if class != ref.ClassSupervised && class != ref.ClassDevice {
		return Type{}, fmt.Errorf("%w: %q is %s, and only a supervised kind has a type to register",
			ErrUnregisterableKind, kind, class)
	}

	point, known := pointOf[kind]
	if !known {
		return Type{}, fmt.Errorf("%w: %q has no extension point", ErrUnregisterableKind, kind)
	}

	if descriptor.IsZero() {
		return Type{}, fmt.Errorf("%w: %s carries no descriptor", ErrForeignDescriptor, kind)
	}

	if descriptor.Kind() != point {
		return Type{}, fmt.Errorf("%w: %s is a %s and %s serves %s",
			ErrForeignDescriptor, descriptor.Qualified(), descriptor.Kind(), kind, point)
	}

	if construct == nil {
		return Type{}, fmt.Errorf("%w: %s/%s", ErrNoConstructor, kind, descriptor.Name())
	}

	return Type{
		kind:       kind,
		descriptor: descriptor,
		construct: func(ctx context.Context, h host.Handle, spec Spec) (any, error) {
			return construct(ctx, h, spec)
		},
	}, nil
}

// Register declares a type in the gathering list New drains. It panics when the
// registration is refused, so it belongs in an init function: a binary with a
// broken type table must not reach its first request.
func Register[T any](
	kind ref.Kind, name string, doc extension.Doc, construct func(context.Context, host.Handle, Spec) (T, error),
) {
	if err := tryRegister(kind, name, doc, construct); err != nil {
		panic(err.Error())
	}
}

// TryRegister declares a type in the gathering list, reporting a rejected
// registration instead of stopping the process.
func TryRegister[T any](
	kind ref.Kind, name string, doc extension.Doc, construct func(context.Context, host.Handle, Spec) (T, error),
) error {
	return tryRegister(kind, name, doc, construct)
}

// Construct builds one instance from the type resolved for name, and reports
// ErrTypeMismatch when that type does not serve the contract the caller wants.
func Construct[T any](ctx context.Context, r *Registry, kind ref.Kind, h host.Handle, spec Spec) (T, error) {
	var zero T

	found, err := r.Lookup(kind, spec.Type)
	if err != nil {
		return zero, err
	}

	built, err := found.construct(ctx, h, spec)
	if err != nil {
		return zero, fmt.Errorf("construct %s as %s: %w", spec.Ref, found.Qualified(), err)
	}

	target, ok := built.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s built %T, want %T", ErrTypeMismatch, found.Qualified(), built, zero)
	}

	return target, nil
}

func declare[T any](
	kind ref.Kind, name string, doc extension.Doc,
	construct func(context.Context, host.Handle, Spec) (T, error),
) (Type, error) {
	class, err := kind.Classify()
	if err != nil {
		return Type{}, err
	}

	if class != ref.ClassSupervised && class != ref.ClassDevice {
		return Type{}, fmt.Errorf("%w: %q is %s, and only a supervised kind has a type to register",
			ErrUnregisterableKind, kind, class)
	}

	if construct == nil {
		return Type{}, fmt.Errorf("%w: %s/%s", ErrNoConstructor, kind, name)
	}

	point, known := pointOf[kind]
	if !known {
		return Type{}, fmt.Errorf("%w: %q has no extension point", ErrUnregisterableKind, kind)
	}

	descriptor, err := extension.Declare(point, Contract, name, doc)
	if err != nil {
		if errors.Is(err, extension.ErrInvalidName) {
			return Type{}, fmt.Errorf("%w: %w", ErrInvalidType, err)
		}

		return Type{}, err
	}

	return Type{
		kind:       kind,
		descriptor: descriptor,
		construct: func(ctx context.Context, h host.Handle, spec Spec) (any, error) {
			return construct(ctx, h, spec)
		},
	}, nil
}

func tryRegister[T any](
	kind ref.Kind, name string, doc extension.Doc,
	construct func(context.Context, host.Handle, Spec) (T, error),
) error {
	declaration, err := declare(kind, name, doc, construct)
	if err != nil {
		return err
	}

	return declared.Add(declaration)
}

func typesOf(entries []extension.Entry[constructor]) []Type {
	out := make([]Type, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Type{kind: kindOf(entry.Descriptor), descriptor: entry.Descriptor, construct: entry.Value})
	}

	return out
}

// Registry is the type table one application owns. It never changes once built,
// so it is safe to share and a lookup takes no lock.
type Registry struct {
	byKind map[ref.Kind]map[string]Type
}

// New drains the declarations gathered by Register into a Registry. The
// composition root calls it once, after the blank imports have run.
func New() (*Registry, error) { return NewFrom(declared.Drain()...) }

// NewFrom builds a Registry from types declared explicitly, for a host that
// wires its own built-ins and for tests.
func NewFrom(types ...Type) (*Registry, error) {
	r := &Registry{byKind: make(map[ref.Kind]map[string]Type, len(ref.Kinds()))}

	for _, t := range types {
		if t.IsZero() {
			return nil, fmt.Errorf("%w: %s/%s", ErrNoConstructor, t.kind, t.Name())
		}

		if existing, exists := r.byKind[t.kind][t.Qualified()]; exists {
			return nil, fmt.Errorf("%w: %s %s is registered by %q and again by %q",
				ErrDuplicateType, t.kind, t.Qualified(), existing.descriptor.Module(), t.descriptor.Module())
		}

		if r.byKind[t.kind] == nil {
			r.byKind[t.kind] = map[string]Type{}
		}

		r.byKind[t.kind][t.Qualified()] = t
	}

	return r, nil
}

// Lookup resolves a name written in config. A qualified name matches exactly. A
// bare name matches when one namespace answers to it, and the error names every
// candidate when more than one does. Guessing which author was meant would let
// the wrong code serve the operator's data.
func (r *Registry) Lookup(kind ref.Kind, name string) (Type, error) {
	if extension.IsQualified(name) {
		found, ok := r.byKind[kind][name]
		if !ok {
			return Type{}, fmt.Errorf("%w: %s %q, this build has %s", ErrUnknownType, kind, name, r.list(kind))
		}

		return found, nil
	}

	var candidates []Type

	for _, t := range r.byKind[kind] {
		if t.Name() == name {
			candidates = append(candidates, t)
		}
	}

	switch len(candidates) {
	case 0:
		return Type{}, fmt.Errorf("%w: %s %q, this build has %s", ErrUnknownType, kind, name, r.list(kind))
	case 1:
		return candidates[0], nil
	}

	return Type{}, fmt.Errorf("%w: %s %q is served by %s", ErrAmbiguousType, kind, name, qualifiedNames(candidates))
}

// Types returns the types registered for one kind, sorted by qualified name.
func (r *Registry) Types(kind ref.Kind) []Type {
	out := slices.Collect(maps.Values(r.byKind[kind]))
	slices.SortFunc(out, func(a, b Type) int { return strings.Compare(a.Qualified(), b.Qualified()) })

	return out
}

// All returns every registered type, sorted by kind then qualified name.
func (r *Registry) All() []Type {
	var out []Type
	for _, kind := range ref.Kinds() {
		out = append(out, r.Types(kind)...)
	}

	return out
}

// Descriptors returns the documentation of every registered type, in the order
// All returns them.
func (r *Registry) Descriptors() []extension.Descriptor {
	types := r.All()

	out := make([]extension.Descriptor, 0, len(types))
	for _, t := range types {
		out = append(out, t.descriptor)
	}

	return out
}

// list renders the types this build has, so the error for an unknown type also
// names the alternatives.
func (r *Registry) list(kind ref.Kind) string {
	types := r.Types(kind)
	if len(types) == 0 {
		return "none"
	}

	return qualifiedNames(types)
}

func qualifiedNames(types []Type) string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, t.Qualified())
	}

	slices.Sort(names)

	return strings.Join(names, ", ")
}
