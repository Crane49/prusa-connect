package stateapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/internal/registration"
)

// Contract is the version of the record backend contract this build serves. An
// implementation declares the version it compiled against and is refused at
// registration when the two are incompatible.
const Contract = extension.Version("1.0.0")

// Backend is what a registered record store builds. It creates the Records of
// each namespace. Every backend must behave the same to a module.
type Backend interface {
	// Records returns the store of one namespace, limited to limit bytes. The
	// host chooses both. A write that would take the namespace past the limit
	// must fail with ErrNamespaceFull.
	Records(namespace string, limit int64) (Records, error)
	// Namespaces returns the key count and the byte total of every namespace,
	// never a value. A backend must not filter the list. The host decides who
	// may see a row.
	Namespaces(ctx context.Context) ([]Accounting, error)
	// Close releases what the backend opened. The host calls it once.
	Close() error
}

// Accounting is how much one namespace stores.
type Accounting struct {
	// Namespace is the name of the namespace.
	Namespace string
	// Keys is the number of records.
	Keys int64
	// Bytes is what those records count toward the namespace's limit.
	Bytes int64
}

// ProviderSpec describes the backend to build.
type ProviderSpec struct {
	// ID is the instance's own name, as an operator declared it.
	ID string
	// Type is the type name the configuration asked for, bare or qualified.
	Type string
	// Config is the instance's settings as JSON. The host does not read them.
	Config json.RawMessage
	// Logger is the instance's logger, with its kind and name already set. It
	// may be nil. Use Log.
	Logger *slog.Logger
}

// Log returns the instance's logger, or one that discards when Logger is nil.
func (s ProviderSpec) Log() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return s.Logger
}

// ProviderFactory builds a Backend from its spec.
type ProviderFactory func(spec ProviderSpec) (Backend, error)

// The errors a registration or a lookup reports. Callers inspect them with
// errors.Is.
var (
	// ErrUnknownProvider reports a backend type no registration answers to.
	ErrUnknownProvider = extension.ErrUnknown
	// ErrDuplicateType reports one qualified name registered twice.
	ErrDuplicateType = extension.ErrDuplicate
	// ErrSealed reports a registration that arrived after the host built its
	// registry.
	ErrSealed = extension.ErrSealed
	// ErrUndocumented reports a registration missing a mandatory descriptor
	// field.
	ErrUndocumented = extension.ErrUndocumented
	// ErrAmbiguousType reports a bare name that two namespaces both answer to.
	ErrAmbiguousType = extension.ErrAmbiguous
	// ErrNoFactory reports a registration with no factory.
	ErrNoFactory = errors.New("stateapi: type has no factory")
)

// Registration is one registered type: its descriptor and its factory.
type Registration struct {
	descriptor extension.Descriptor
	factory    ProviderFactory
}

// Name returns the bare name, as an operator writes it in configuration.
func (r Registration) Name() string { return r.descriptor.Name() }

// Namespace returns the owner of the name, as the host derived it.
func (r Registration) Namespace() extension.Namespace { return r.descriptor.Namespace() }

// Descriptor returns the identity and documentation this type registered.
func (r Registration) Descriptor() extension.Descriptor { return r.descriptor }

// Qualified returns "namespace/name", which is unique.
func (r Registration) Qualified() string { return r.descriptor.Qualified() }

// IsZero reports whether the registration was never declared.
func (r Registration) IsZero() bool { return r.factory == nil }

// Build makes one instance from its spec.
func (r Registration) Build(spec ProviderSpec) (Backend, error) {
	return registration.Build(
		registration.Target{Requested: spec.Type, Qualified: r.Qualified(), NoFactory: ErrNoFactory},
		r.factory, spec)
}

// Declarations is a list of types waiting to become a Registry. The zero
// value is ready to use and safe for concurrent use.
type Declarations struct {
	set extension.Set[ProviderFactory]
}

// Add appends one declaration.
func (d *Declarations) Add(r Registration) error {
	if r.IsZero() {
		return fmt.Errorf("%w: %s", ErrNoFactory, r.descriptor.Name())
	}

	return d.set.Add(extension.Entry[ProviderFactory]{Descriptor: r.descriptor, Value: r.factory})
}

// Drain seals the list and returns its registrations.
func (d *Declarations) Drain() []Registration { return registrationsOf(d.set.Drain()) }

// Descriptors returns the documentation of everything declared, without sealing
// the list, for the reference generator.
func (d *Declarations) Descriptors() []extension.Descriptor { return d.set.Descriptors() }

// Sealed reports whether the list has been drained.
func (d *Declarations) Sealed() bool { return d.set.Sealed() }

func registrationsOf(entries []extension.Entry[ProviderFactory]) []Registration {
	out := make([]Registration, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Registration{descriptor: entry.Descriptor, factory: entry.Value})
	}

	return out
}

// declared is the list init functions write to.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var declared Declarations

// Register declares a type for NewRegistry. Call it from an init function. It
// panics on a rejected registration.
func Register(name string, doc extension.Doc, factory ProviderFactory) {
	if err := tryRegister(name, doc, factory); err != nil {
		panic(err.Error())
	}
}

// TryRegister declares a type and returns an error where Register panics.
func TryRegister(name string, doc extension.Doc, factory ProviderFactory) error {
	return tryRegister(name, doc, factory)
}

// Declare returns a registration without adding it to the package list.
func Declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	return declare(name, doc, factory)
}

// Drain seals the package list and returns what init functions put in it.
func Drain() []Registration { return declared.Drain() }

// Descriptors returns what init functions declared, without sealing the list.
func Descriptors() []extension.Descriptor { return declared.Descriptors() }

// Sealed reports whether the package list has been drained.
func Sealed() bool { return declared.Sealed() }

func declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	if factory == nil {
		return Registration{}, fmt.Errorf("%w: %s", ErrNoFactory, name)
	}

	descriptor, err := extension.Declare(extension.KindDatabase, Contract, name, doc)
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: descriptor, factory: factory}, nil
}

func tryRegister(name string, doc extension.Doc, factory ProviderFactory) error {
	declaration, err := declare(name, doc, factory)
	if err != nil {
		return err
	}

	return declared.Add(declaration)
}

// Registry is the table of record backend types of one host. It is immutable.
type Registry struct {
	inner *extension.Registry[ProviderFactory]
}

// NewRegistry drains the declarations made with Register into a Registry.
func NewRegistry() (*Registry, error) { return NewRegistryFrom(Drain()...) }

// NewRegistryFrom builds a Registry from registrations declared explicitly.
func NewRegistryFrom(registrations ...Registration) (*Registry, error) {
	entries := make([]extension.Entry[ProviderFactory], 0, len(registrations))

	for _, declaration := range registrations {
		if declaration.IsZero() {
			return nil, fmt.Errorf("%w: %s", ErrNoFactory, declaration.Name())
		}

		entries = append(entries, extension.Entry[ProviderFactory]{
			Descriptor: declaration.descriptor,
			Value:      declaration.factory,
		})
	}

	inner, err := extension.NewRegistry(extension.KindDatabase, entries...)
	if err != nil {
		return nil, err
	}

	return &Registry{inner: inner}, nil
}

// Lookup resolves a name written in configuration.
func (r *Registry) Lookup(name string) (Registration, error) {
	entry, err := r.inner.Lookup(name)
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: entry.Descriptor, factory: entry.Value}, nil
}

// Registered reports whether a name written in configuration resolves to a
// registered type.
func (r *Registry) Registered(name string) bool { return r.inner.Registered(name) }

// BuildProvider resolves the spec's type and makes one instance from it.
func (r *Registry) BuildProvider(spec ProviderSpec) (Backend, error) {
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
