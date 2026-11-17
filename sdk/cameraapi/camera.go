// Package cameraapi is the contract a camera driver implements.
//
// A driver declares the formats its device can emit, returns one still image,
// and returns a reader of frames it has already framed. The host relays the
// bytes and never decodes them.
//
// A driver that cannot emit a format returns ErrFormatNotSupported. The host
// does not convert between formats.
package cameraapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/internal/registration"
)

// Contract is the version of the camera contract this build serves. A driver
// declares the version it compiled against and is refused at registration when
// the two are incompatible.
const Contract = extension.Version("1.0.0")

// Format is a format a camera can emit. The set is closed, and a driver
// cannot add to it.
type Format string

// The formats. The host can relay each one without decoding it.
const (
	// FormatJPEG is one still image, as image/jpeg.
	FormatJPEG Format = "jpeg"
	// FormatMJPEG is a sequence of JPEG frames the driver has already framed
	// as multipart/x-mixed-replace.
	FormatMJPEG Format = "mjpeg"
	// FormatH264AnnexB is an H.264 elementary stream with start-code
	// delimiters, as from an RTSP source.
	FormatH264AnnexB Format = "h264/annexb"
	// FormatH264AVCC is an H.264 elementary stream with length-prefixed NAL
	// units, as in an MP4 sample entry.
	FormatH264AVCC Format = "h264/avcc"
	// FormatYUYV is raw 4:2:2 pixels. A driver may declare it. A host that
	// cannot encode does not serve it.
	FormatYUYV Format = "yuyv"
)

// Formats returns the closed set, in declaration order.
func Formats() []Format {
	return []Format{FormatJPEG, FormatMJPEG, FormatH264AnnexB, FormatH264AVCC, FormatYUYV}
}

// Known reports whether f is in the closed set.
func (f Format) Known() bool { return slices.Contains(Formats(), f) }

// String returns the format as an operator writes it.
func (f Format) String() string { return string(f) }

// MediaType is the HTTP content type a host serves this format as. It is
// empty for a format no browser reads directly.
func (f Format) MediaType() string {
	switch f {
	case FormatJPEG:
		return "image/jpeg"
	case FormatMJPEG:
		return "multipart/x-mixed-replace; boundary=" + MJPEGBoundary
	case FormatH264AnnexB, FormatH264AVCC, FormatYUYV:
		return ""
	}

	return ""
}

// MJPEGBoundary is the multipart boundary a driver must frame FormatMJPEG
// with.
const MJPEGBoundary = "prusaframe"

// The errors a driver reports and a host inspects with errors.Is.
var (
	// ErrFormatNotSupported reports a format this camera cannot emit.
	ErrFormatNotSupported = errors.New("cameraapi: the camera cannot emit this format")
	// ErrNoFrame reports a camera that has not produced an image yet. It is
	// not a failure.
	ErrNoFrame = errors.New("cameraapi: the camera has produced no frame yet")
	// ErrUnknownProvider reports a camera type no registration answers to.
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
	ErrNoFactory = errors.New("cameraapi: type has no factory")
	// ErrInvalidSnapshot reports a still image a host cannot serve: no bytes,
	// or a media type other than that of FormatJPEG.
	ErrInvalidSnapshot = errors.New("cameraapi: the snapshot is not servable")
	// ErrPushNotSupported reports a camera that does not implement Receiver.
	ErrPushNotSupported = errors.New("cameraapi: the camera does not take pushed frames")
)

// Snapshot is one still image and when it was taken.
type Snapshot struct {
	// Bytes is the encoded image, ready to serve.
	Bytes []byte
	// MediaType is the type of the bytes. It must be the media type of
	// FormatJPEG.
	MediaType string
	// Taken is when the device produced the frame, not when the call
	// returned. The host uses it for conditional requests.
	Taken time.Time
}

// Validate reports whether the host can serve this snapshot. The host calls
// it before it sends the bytes.
func (s Snapshot) Validate() error {
	if len(s.Bytes) == 0 {
		return fmt.Errorf("%w: it carries no bytes", ErrInvalidSnapshot)
	}

	if s.MediaType != FormatJPEG.MediaType() {
		return fmt.Errorf("%w: the media type is %q and a still image is %s",
			ErrInvalidSnapshot, echo(s.MediaType), FormatJPEG.MediaType())
	}

	return nil
}

// Stream is a sequence of frames the driver has already framed. The host
// relays the bytes unread. Closing Body ends the stream, and a blocked Read
// must return when Body is closed.
type Stream struct {
	// Body is the framed bytes. The host closes it once.
	Body io.ReadCloser
	// MediaType is how the bytes are framed. It must be the media type of the
	// format the stream was opened for.
	MediaType string
}

// Camera is what a driver implements. The host may call every method
// concurrently with every other. A driver that cannot handle that must
// serialise the calls itself.
type Camera interface {
	// Formats returns what this camera can emit, in the driver's order of
	// preference. The host refuses a camera that returns none.
	Formats() []Format

	// Snapshot returns the most recent still image. It returns ErrNoFrame
	// when the camera works and has produced nothing yet.
	Snapshot(ctx context.Context) (Snapshot, error)

	// Open starts a stream in one format. It returns ErrFormatNotSupported
	// for a format this camera does not emit. The caller closes the returned
	// Body. Open what the stream needs here and release it when Body is
	// closed. Do not keep a device connection open between streams.
	Open(ctx context.Context, format Format) (Stream, error)

	// Close releases the device. The host calls it once, when the instance
	// stops.
	Close() error
}

// Paced is an optional interface of a Camera that knows how often it can have
// a new frame. The host tells clients not to ask more often.
type Paced interface {
	Camera

	// MinInterval is the shortest gap between two new frames. Return zero
	// when it is unknown. Do not guess.
	MinInterval() time.Duration
}

// Receiver is an optional interface of a Camera that the host pushes frames
// to, for a device that sends its frames to the server. The host reports
// ErrPushNotSupported for a camera without it.
type Receiver interface {
	Camera

	// Accept stores one still image as the camera's most recent frame. The
	// host has already checked that the bytes are a JPEG within its size
	// limit. It may be called concurrently with Snapshot and Open.
	Accept(ctx context.Context, shot Snapshot) error
}

// ProviderSpec describes the camera to build.
type ProviderSpec struct {
	// ID is the operator-chosen instance name.
	ID string
	// Type is the name as written in configuration.
	Type string
	// Printer is the printer this camera watches. The host sets it from the
	// operator's configuration. Whoever may read that printer may see the
	// camera.
	Printer string
	// Config is the driver's own settings. The host does not read them.
	Config map[string]any
}

// ProviderFactory builds a Camera from its spec.
type ProviderFactory func(spec ProviderSpec) (Camera, error)

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

// Build makes one instance from its spec. It refuses a camera whose Formats
// fail ValidateFormats.
func (r Registration) Build(spec ProviderSpec) (Camera, error) {
	built, err := registration.Build(
		registration.Target{Requested: spec.Type, Qualified: r.Qualified(), NoFactory: ErrNoFactory},
		r.factory, spec)
	if err != nil {
		return nil, err
	}

	if err = ValidateFormats(built.Formats()); err != nil {
		return nil, fmt.Errorf("build %s: %w", r.Qualified(), err)
	}

	return built, nil
}

// ValidateFormats refuses an empty set of formats and a set with an unknown
// format.
func ValidateFormats(declared []Format) error {
	if len(declared) == 0 {
		return fmt.Errorf("%w: the camera declares no format", ErrFormatNotSupported)
	}

	for _, format := range declared {
		if !format.Known() {
			return fmt.Errorf("%w: %q is not one of %s",
				ErrFormatNotSupported, echo(format.String()), renderFormats(Formats()))
		}
	}

	return nil
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

	descriptor, err := extension.Declare(extension.KindCamera, Contract, name, doc)
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

// Registry is the table of camera types of one host. It is immutable and safe
// for concurrent use.
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

	inner, err := extension.NewRegistry(extension.KindCamera, entries...)
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
func (r *Registry) BuildProvider(spec ProviderSpec) (Camera, error) {
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

// renderFormats lists a format set for an error message.
func renderFormats(formats []Format) string {
	names := make([]string, 0, len(formats))
	for _, format := range formats {
		names = append(names, format.String())
	}

	return strings.Join(names, ", ")
}

// echo cuts a rejected value to 32 bytes before an error quotes it.
func echo(value string) string {
	const limit = 32

	if len(value) <= limit {
		return value
	}

	return value[:limit] + "..."
}
