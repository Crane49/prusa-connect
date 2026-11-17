// Package eventsapi is the contract of an event sink: a plugin that receives
// the host's events and forwards them wherever it wants.
//
// A plugin implements Processor and calls Register from an init function.
// The host builds the Processor that the configuration names.
package eventsapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/internal/registration"
)

// ErrUnknownProvider reports a processor type no registration answers to.
var ErrUnknownProvider = extension.ErrUnknown

// PrinterState reports that a printer changed its connection or print state.
type PrinterState struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// PrinterLabels reports that the labels of a printer changed.
type PrinterLabels struct {
	ID     string   `json:"id"`
	Mode   string   `json:"mode"`
	Labels []string `json:"labels"`
}

// JobInfo reports that a job was created or changed.
type JobInfo struct {
	ID string `json:"id"`
	// ExternalID is the job id the printer reported. It is 0 when unset.
	ExternalID int `json:"external_id"`
	// PrinterID is the machine the job runs on. Its JSON name is device_id.
	PrinterID       string   `json:"device_id"`
	State           string   `json:"state"`
	Source          string   `json:"source"`
	ExternalContext string   `json:"external_context"`
	PrintHeight     *float32 `json:"print_height"`
}

// Fact is one durable event of the host, with the fields and the JSON names
// of its envelope (sdk/event.Envelope). Payload is the event's data,
// unparsed. sdk/historyapi.Fact has the same content.
type Fact struct {
	// Origin is what the event came from, such as "device:printer-01".
	Origin event.Origin `json:"origin"`
	// Type is the event type a consumer routes on, such as
	// com.prusa.connect.device.command.requested.
	Type string `json:"type"`
	// Class is the class of the event. Only durable classes reach a sink.
	// Telemetry does not.
	Class event.Class `json:"evclass"`
	// At is the time of the event.
	At time.Time `json:"time"`
	// Sequence and Epoch number the event within its origin. Together with
	// Origin they are the dedup key.
	Sequence uint64 `json:"sequence"`
	Epoch    string `json:"epoch,omitempty"`
	// CausationID is the id of the command that caused this event. Empty when
	// no command caused it. The events of one command share it.
	CausationID string `json:"causationid,omitempty"`
	// Payload is the event's data, unparsed.
	Payload json.RawMessage `json:"data,omitempty"`
}

// Sample is one node of a device's state tree, such as /tools/0/nozzle_temp,
// and its JSON value. A null value means the device stopped reporting the
// node.
type Sample struct {
	// Path is the path of the node in the state tree.
	Path string `json:"path"`
	// Value is the node's JSON value.
	Value json.RawMessage `json:"value"`
}

// Samples is the newest value of each node of one device that changed in one
// delivery window. It is not every sample the device sent. The first delivery
// for a device, and one after the host lost track, has the whole tree and
// Whole set.
type Samples struct {
	// DeviceID is the device the nodes belong to.
	DeviceID string `json:"device_id"`
	// Whole is true when Nodes is the device's whole tree. The device does
	// not report a node that is missing from it.
	Whole bool `json:"whole"`
	// Nodes is one entry per node, in the order of their paths.
	Nodes []Sample `json:"nodes"`
}

// SampleObserver is an optional interface of a Processor that also receives
// the live state of devices. SamplesObserved must not block and must not
// panic.
type SampleObserver interface {
	SamplesObserved(Samples)
}

// Processor receives the host's events. A plugin implements it. No method may
// block or panic. Buffer internally.
type Processor interface {
	PrinterStateChanged(PrinterState)
	PrinterLabelsChanged(PrinterLabels)
	PrinterDeleted(id string)
	JobUpdated(JobInfo)
	LabelDeleted(id string)
	// FactObserved receives every durable event the host publishes, including
	// an event that one of the typed methods above also reports.
	FactObserved(Fact)
	Close() error
}

// ProviderSpec describes the processor to build. Config is the processor's
// own settings. The host does not read them.
type ProviderSpec struct {
	Type   string
	Config map[string]any
}

// ProviderFactory builds a Processor from its spec.
type ProviderFactory func(spec ProviderSpec) (Processor, error)

// Contract is the version of the event sink contract this build serves. An
// implementation declares the version it compiled against and is refused at
// registration when the two are incompatible.
const Contract = extension.Version("1.1.0")

// The errors a registration or a lookup reports. Callers inspect them with
// errors.Is. Most are the same values as in sdk/extension.
var (
	// ErrDuplicateType reports one qualified name registered twice. A second
	// registration never replaces the first.
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
	ErrNoFactory = errors.New("eventsapi: type has no factory")
)

// Registration is one registered type: its descriptor and its factory. The
// zero value is refused everywhere.
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
func (r Registration) Build(spec ProviderSpec) (Processor, error) {
	return registration.Build(
		registration.Target{Requested: spec.Type, Qualified: r.Qualified(), NoFactory: ErrNoFactory},
		r.factory, spec)
}

// Declarations is a list of types waiting to become a Registry. Init
// functions add to it and the host drains it once. The zero value is ready to
// use and safe for concurrent use.
type Declarations struct {
	set extension.Set[ProviderFactory]
}

// Add appends one declaration. It reports ErrSealed once the list is drained
// and ErrDuplicateType for a qualified name already present.
func (d *Declarations) Add(r Registration) error {
	if r.IsZero() {
		return fmt.Errorf("%w: %s", ErrNoFactory, r.descriptor.Name())
	}

	return d.set.Add(extension.Entry[ProviderFactory]{Descriptor: r.descriptor, Value: r.factory})
}

// Drain seals the list and returns its registrations. A second call returns
// the same registrations.
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

// Declare returns a registration without adding it to the package list, for a
// host that wires its own built-ins and for tests.
func Declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	return declare(name, doc, factory)
}

// Drain seals the package list and returns what init functions put in it. The
// host calls it once, after every init function has run.
func Drain() []Registration { return declared.Drain() }

// Descriptors returns what init functions declared, without sealing the list.
func Descriptors() []extension.Descriptor { return declared.Descriptors() }

// Sealed reports whether the package list has been drained.
func Sealed() bool { return declared.Sealed() }

func declare(name string, doc extension.Doc, factory ProviderFactory) (Registration, error) {
	if factory == nil {
		return Registration{}, fmt.Errorf("%w: %s", ErrNoFactory, name)
	}

	descriptor, err := extension.Declare(extension.KindEventSink, Contract, name, doc)
	if err != nil {
		return Registration{}, err
	}

	return Registration{descriptor: descriptor, factory: factory}, nil
}

func tryRegister(name string, doc extension.Doc, factory ProviderFactory) error {
	registration, err := declare(name, doc, factory)
	if err != nil {
		return err
	}

	return declared.Add(registration)
}

// Registry is the table of event sink types of one host. It is immutable and
// safe for concurrent use.
type Registry struct {
	inner *extension.Registry[ProviderFactory]
}

// NewRegistry drains the declarations made with Register into a Registry. The
// host calls it once, after every init function has run.
func NewRegistry() (*Registry, error) { return NewRegistryFrom(Drain()...) }

// NewRegistryFrom builds a Registry from registrations declared explicitly, for
// a host that wires its own built-ins and for tests.
func NewRegistryFrom(registrations ...Registration) (*Registry, error) {
	entries := make([]extension.Entry[ProviderFactory], 0, len(registrations))

	for _, registration := range registrations {
		if registration.IsZero() {
			return nil, fmt.Errorf("%w: %s", ErrNoFactory, registration.Name())
		}

		entries = append(entries, extension.Entry[ProviderFactory]{
			Descriptor: registration.descriptor,
			Value:      registration.factory,
		})
	}

	inner, err := extension.NewRegistry(extension.KindEventSink, entries...)
	if err != nil {
		return nil, err
	}

	return &Registry{inner: inner}, nil
}

// Lookup resolves a name written in configuration. A qualified name must match
// in full. A bare name matches when one namespace has it.
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
func (r *Registry) BuildProvider(spec ProviderSpec) (Processor, error) {
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
