package connectorapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The errors a registration or a lookup reports. Callers inspect them with
// errors.Is. Most are the same values as in sdk/extension.
var (
	// ErrInvalidTypeName reports a type name that is not a DNS-1123 label.
	ErrInvalidTypeName = extension.ErrInvalidName
	// ErrDuplicateType reports one qualified name registered twice. A second
	// registration never replaces the first.
	ErrDuplicateType = extension.ErrDuplicate
	// ErrUnknownType reports a name no registered driver answers to.
	ErrUnknownType = extension.ErrUnknown
	// ErrAmbiguousType reports a bare name that two namespaces both answer to.
	ErrAmbiguousType = extension.ErrAmbiguous
	// ErrNoFactory reports a registration with no factory.
	ErrNoFactory = errors.New("connectorapi: driver type has no factory")
	// ErrSealed reports a registration that arrived after the host drained the
	// list.
	ErrSealed = extension.ErrSealed
	// ErrUndocumented reports a registration missing a mandatory descriptor
	// field.
	ErrUndocumented = extension.ErrUndocumented
	// ErrNoNamespace reports a registration from a module that is not in the
	// build graph.
	ErrNoNamespace = extension.ErrNoNamespace
)

const qualifiedSeparator = "/"

// TypeName is a driver type as configuration names it: the bare name a driver
// registered, or "namespace/name" when two modules registered the same bare
// name.
type TypeName string

// String returns the name as text.
func (t TypeName) String() string { return string(t) }

// IsQualified reports whether the name includes a namespace.
func (t TypeName) IsQualified() bool { return strings.Contains(string(t), qualifiedSeparator) }

// Ref is the operator-chosen name of one driver instance. It is the name in
// metric labels, log fields, API paths and event subjects.
type Ref string

// String returns the ref as text.
func (r Ref) String() string { return string(r) }

// Spec is one driver instance as configuration names it.
type Spec struct {
	// Ref is the operator-chosen instance name.
	Ref Ref `json:"ref"`
	// Type is the driver type, bare or qualified.
	Type TypeName `json:"type"`
	// Config is the driver's own settings, still encoded. The host passes them
	// through unread. Decode them with DecodeConfig.
	Config json.RawMessage `json:"config,omitempty"`
	// Logger is the driver's logger. It may be nil, and the driver then logs
	// nothing. A driver must not create a logger of its own.
	Logger *slog.Logger `json:"-"`
}

// DecodeConfig unmarshals Config into v. An empty Config leaves v unchanged,
// so the defaults already in v stay.
func (s Spec) DecodeConfig(v any) error {
	if len(s.Config) == 0 {
		return nil
	}

	if err := json.Unmarshal(s.Config, v); err != nil {
		return fmt.Errorf("decode config for %s: %w", s.Ref, err)
	}

	return nil
}

// Factory builds one driver instance from its spec. It must not talk to a
// device. The host is not ready when it runs.
type Factory func(spec Spec) (Driver, error)

// Contract is the version of the connector contract this build serves. A
// driver declares the version it compiled against and is refused at
// registration when the two are incompatible.
const Contract = extension.Version("1.0.0")

// Namespace is the owner of a driver type name. The host derives it from the
// registering module.
type Namespace = extension.Namespace

// CoreNamespace is the namespace of every driver registered from the
// application's own module.
const CoreNamespace = extension.CoreNamespace

// Registration is one registered driver type: its descriptor, its factory and
// its Dialect. The zero value is refused everywhere.
type Registration struct {
	descriptor extension.Descriptor
	driver     declaredDriver
}

// declaredDriver is the factory and the dialect of one registered type.
type declaredDriver struct {
	factory Factory
	dialect Dialect
}

// Name returns the bare name, as an operator writes it in configuration.
func (r Registration) Name() TypeName { return TypeName(r.descriptor.Name()) }

// Namespace returns the owner of the name, as the host derived it.
func (r Registration) Namespace() Namespace { return r.descriptor.Namespace() }

// Descriptor returns the identity and documentation this type registered.
func (r Registration) Descriptor() extension.Descriptor { return r.descriptor }

// Qualified returns "namespace/name", which is unique.
func (r Registration) Qualified() TypeName { return TypeName(r.descriptor.Qualified()) }

// Dialect returns a copy of what this type declared about its machines. It is
// the zero value for a type that declared nothing.
func (r Registration) Dialect() Dialect { return r.driver.dialect.clone() }

// IsZero reports whether the registration was never declared.
func (r Registration) IsZero() bool { return r.driver.factory == nil }

// Build makes one driver instance.
func (r Registration) Build(spec Spec) (Driver, error) {
	if r.IsZero() {
		return nil, fmt.Errorf("%w: %s", ErrNoFactory, spec.Type)
	}

	driver, err := r.driver.factory(spec)
	if err != nil {
		return nil, fmt.Errorf("build %s as %s: %w", spec.Ref, r.Qualified(), err)
	}

	if driver == nil {
		return nil, fmt.Errorf("%w: %s built nothing", ErrNoFactory, r.Qualified())
	}

	return driver, nil
}

// Declarations is a list of driver types waiting to become a Registry. Init
// functions add to it and the host drains it once. The zero value is ready to
// use and safe for concurrent use.
type Declarations struct {
	set extension.Set[declaredDriver]
}

// Add appends one declaration. It reports ErrSealed once the list is drained
// and ErrDuplicateType for a qualified name already present.
func (d *Declarations) Add(r Registration) error {
	if r.IsZero() {
		return fmt.Errorf("%w: %s", ErrNoFactory, r.descriptor.Name())
	}

	return d.set.Add(extension.Entry[declaredDriver]{Descriptor: r.descriptor, Value: r.driver})
}

// Drain seals the list and returns its registrations. A second call returns
// the same registrations.
func (d *Declarations) Drain() []Registration { return registrationsOf(d.set.Drain()) }

// Descriptors returns the documentation of everything declared, without sealing
// the list, for the reference generator.
func (d *Declarations) Descriptors() []extension.Descriptor { return d.set.Descriptors() }

// Sealed reports whether the list has been drained.
func (d *Declarations) Sealed() bool { return d.set.Sealed() }

func registrationsOf(entries []extension.Entry[declaredDriver]) []Registration {
	out := make([]Registration, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Registration{descriptor: entry.Descriptor, driver: entry.Value})
	}

	return out
}

// declared is the list init functions write to.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var declared Declarations

// Register declares a driver type for NewRegistry. Call it from an init
// function. It panics on a rejected registration. about is the optional
// Dialect. Pass one or none.
func Register(name TypeName, doc extension.Doc, factory Factory, about ...Dialect) {
	if err := tryRegister(name, doc, factory, about...); err != nil {
		panic(err.Error())
	}
}

// TryRegister declares a driver type and returns an error where Register
// panics.
func TryRegister(name TypeName, doc extension.Doc, factory Factory, about ...Dialect) error {
	return tryRegister(name, doc, factory, about...)
}

// Declare returns a registration without adding it to the package list, for a
// host that wires its own built-ins and for tests.
func Declare(
	name TypeName, doc extension.Doc, factory Factory, about ...Dialect,
) (Registration, error) {
	return declare(name, doc, factory, about...)
}

// Drain seals the package list and returns what init functions put in it. The
// host calls it once, after every init function has run.
func Drain() []Registration { return declared.Drain() }

// Descriptors returns what init functions declared, without sealing the list.
func Descriptors() []extension.Descriptor { return declared.Descriptors() }

// Sealed reports whether the package list has been drained.
func Sealed() bool { return declared.Sealed() }

func declare(
	name TypeName, doc extension.Doc, factory Factory, about ...Dialect,
) (Registration, error) {
	if factory == nil {
		return Registration{}, fmt.Errorf("%w: %s", ErrNoFactory, name)
	}

	if len(about) > 1 {
		return Registration{}, fmt.Errorf("%w: %s declares %d dialects, and a type declares one or none",
			ErrInvalidDialect, name, len(about))
	}

	var dialect Dialect
	if len(about) == 1 {
		dialect = about[0]
	}

	if err := ValidateDialect(dialect); err != nil {
		return Registration{}, fmt.Errorf("%s: %w", name, err)
	}

	descriptor, err := extension.Declare(extension.KindConnector, Contract, name.String(), doc)
	if err != nil {
		return Registration{}, err
	}

	return Registration{
		descriptor: descriptor,
		driver:     declaredDriver{factory: factory, dialect: dialect.clone()},
	}, nil
}

func tryRegister(name TypeName, doc extension.Doc, factory Factory, about ...Dialect) error {
	registration, err := declare(name, doc, factory, about...)
	if err != nil {
		return err
	}

	return declared.Add(registration)
}

// ValidateTypeName reports whether name is a DNS-1123 label: lowercase
// alphanumerics and '-', starting and ending with an alphanumeric, at most 63
// bytes. Such a name is legal as a config key, a metric label value, a URL
// path segment and a messaging subject token.
func ValidateTypeName(name TypeName) error { return extension.ValidateName(name.String()) }

// Registry is the table of driver types of one host. It is immutable and safe
// for concurrent use.
type Registry struct {
	inner *extension.Registry[declaredDriver]
}

// NewRegistry drains the declarations made with Register into a Registry. The
// host calls it once, after every init function has run.
func NewRegistry() (*Registry, error) { return NewRegistryFrom(Drain()...) }

// NewRegistryFrom builds a Registry from registrations declared explicitly, for
// a host that wires its own built-ins and for tests.
func NewRegistryFrom(registrations ...Registration) (*Registry, error) {
	entries := make([]extension.Entry[declaredDriver], 0, len(registrations))

	for _, registration := range registrations {
		if registration.IsZero() {
			return nil, fmt.Errorf("%w: %s", ErrNoFactory, registration.Name())
		}

		entries = append(entries, extension.Entry[declaredDriver]{
			Descriptor: registration.descriptor,
			Value:      registration.driver,
		})
	}

	inner, err := extension.NewRegistry(extension.KindConnector, entries...)
	if err != nil {
		return nil, err
	}

	return &Registry{inner: inner}, nil
}

// Lookup resolves a name written in configuration. A qualified name must match
// in full. A bare name matches when one namespace has it. When several do, the
// error is ErrAmbiguousType and lists every candidate.
func (r *Registry) Lookup(name TypeName) (Registration, error) {
	entry, err := r.inner.Lookup(name.String())
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: entry.Descriptor, driver: entry.Value}, nil
}

// Build resolves the spec's type and makes one driver instance from it.
func (r *Registry) Build(spec Spec) (Driver, error) {
	found, err := r.Lookup(spec.Type)
	if err != nil {
		return nil, err
	}

	return found.Build(spec)
}

// Types returns every registered type, sorted by qualified name.
func (r *Registry) Types() []Registration { return registrationsOf(r.inner.Entries()) }

// Descriptors returns the documentation of every registered type.
func (r *Registry) Descriptors() []extension.Descriptor { return r.inner.Descriptors() }
