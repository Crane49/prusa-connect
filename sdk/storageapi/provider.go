// Package storageapi is the contract a storage provider implements. A separate
// Go module implements [Provider] and registers it with [Register].
//
// A path is relative to the provider's root, uses "/" as the separator and
// never starts with "/". The empty path is the root.
//
// A write is [Provider.Create] followed by [Writer.Commit]. Until Commit
// returns nil, the name keeps its earlier content.
//
// [Renamer], [Presigner], [RangeReader] and [LocalFiles] are optional. The
// host finds them with a type assertion. A provider that cannot serve one
// must not implement it.
package storageapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/internal/registration"
)

// FileInfo describes a single entry (file or directory) in a storage provider.
type FileInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // relative to the provider root, "/"-separated
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"m_timestamp"` // Unix seconds
	// SHA256 is the digest the provider computed when the object was written,
	// lowercase hex. It is empty for a directory and when the provider has no
	// digest for the object.
	SHA256 string `json:"sha256,omitempty"`
	// Irregular is true for a link or a special file, which a provider may
	// list but which is not an object of the store.
	Irregular bool `json:"irregular,omitempty"`

	// Print is what a slicer wrote into the file. The host fills it in. A
	// provider must leave it nil.
	Print *PrintInfo `json:"print,omitempty"`
}

// PrintInfo is what a slicer wrote about the print inside one file. Every
// field is optional. A zero means the file did not say.
type PrintInfo struct {
	// EstimatedSeconds is the print time the slicer estimated for its normal
	// mode, in whole seconds.
	EstimatedSeconds int64 `json:"estimated_seconds,omitempty"`
	// FilamentMM is how much filament the print uses, in millimetres.
	FilamentMM float64 `json:"filament_mm,omitempty"`
	// FilamentG is how much filament the print uses, in grams.
	FilamentG float64 `json:"filament_g,omitempty"`
	// LayerHeightMM is the layer height the file was sliced at, in
	// millimetres.
	LayerHeightMM float64 `json:"layer_height_mm,omitempty"`
	// NozzleMM is the nozzle diameter the file was sliced for, in millimetres.
	// A file sliced for several tools reports the first.
	NozzleMM float64 `json:"nozzle_mm,omitempty"`
	// Material is the filament the file was sliced for, such as PLA. A file
	// sliced for several tools reports the first.
	Material string `json:"material,omitempty"`
	// PrinterModel is the printer the file was sliced for.
	PrinterModel string `json:"printer_model,omitempty"`
	// PrintProfile, PrinterProfile and FilamentProfile name the slicer
	// profiles the file was sliced with. A file sliced for several tools
	// reports the first filament profile.
	PrintProfile    string `json:"print_profile,omitempty"`
	PrinterProfile  string `json:"printer_profile,omitempty"`
	FilamentProfile string `json:"filament_profile,omitempty"`
	// HighFlow says the file was sliced for a high flow nozzle.
	HighFlow bool `json:"high_flow,omitempty"`
	// ThumbnailWidth and ThumbnailHeight are the size in pixels of the picture
	// stored in the file, and zero when the file has none.
	ThumbnailWidth  int `json:"thumbnail_width,omitempty"`
	ThumbnailHeight int `json:"thumbnail_height,omitempty"`

	// Filaments is what the print needs of each filament, T0 first. The
	// single-filament fields above repeat the first entry.
	Filaments []PrintFilament `json:"filaments,omitempty"`
	// MaxLayerZMM is the height of the top layer, in millimetres.
	MaxLayerZMM float64 `json:"max_layer_z_mm,omitempty"`
	// Bounds is the area the objects cover on the bed, and nil when the file
	// does not say.
	Bounds *PrintBounds `json:"bounds,omitempty"`
	// Bed is the printable volume of the printer profile the file was sliced
	// with, and nil when the file does not say.
	Bed *PrintBed `json:"bed,omitempty"`
	// Checks is what the start of the G-code asks the printer to verify. It
	// is nil when the G-code could not be read, and empty when it asks for
	// nothing.
	Checks *PrintChecks `json:"checks,omitempty"`
	// Objects are the objects the slicer labelled, in the order of its
	// objects_info, with the outline of each on the bed. A printer cancels an
	// object by its ID.
	Objects []PrintObject `json:"objects,omitempty"`
}

// PrintObject is one object a sliced file labels.
type PrintObject struct {
	// ID is the object's number in objects_info, which M486 S and a
	// printer's cancel list use.
	ID int `json:"id"`
	// Name is the label the slicer gave the object. It is not unique.
	Name string `json:"name"`
	// Outline is the object's outline on the bed it was sliced on, in
	// millimetres from the bed's origin.
	Outline []PrintPoint `json:"outline"`
}

// PrintPoint is one point on the bed, in millimetres from its origin.
type PrintPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PrintFilament is what a print needs of one of its filaments. A file names
// each filament by its G-code tool number, T0 first. Map that number to a
// printer's tool with connectorapi.ToolTree.Resolve. Every field is optional.
// A zero or a nil means the file did not say.
type PrintFilament struct {
	// Index is n of the Tn that selects the filament, counted from 0.
	Index int `json:"index"`
	// Used says whether the print extrudes this filament. It is nil when the
	// file does not say how much of each filament it uses.
	Used *bool `json:"used,omitempty"`
	// Material is the filament type, such as PLA.
	Material string `json:"material,omitempty"`
	// NozzleMM is the nozzle diameter the filament was sliced for.
	NozzleMM float64 `json:"nozzle_mm,omitempty"`
	// HighFlow says whether the filament was sliced for a high-flow nozzle.
	HighFlow *bool `json:"high_flow,omitempty"`
	// Abrasive says whether the filament wears a nozzle that is not hardened.
	Abrasive *bool `json:"abrasive,omitempty"`
	// NozzleTempC and FirstLayerNozzleTempC are the nozzle temperatures, in
	// degrees Celsius.
	NozzleTempC           float64 `json:"nozzle_temp_c,omitempty"`
	FirstLayerNozzleTempC float64 `json:"first_layer_nozzle_temp_c,omitempty"`
	// BedTempC and FirstLayerBedTempC are the bed temperatures the filament
	// asks for, in degrees Celsius.
	BedTempC           float64 `json:"bed_temp_c,omitempty"`
	FirstLayerBedTempC float64 `json:"first_layer_bed_temp_c,omitempty"`
	// FilamentG and FilamentMM are how much of this filament the print uses.
	FilamentG  float64 `json:"filament_g,omitempty"`
	FilamentMM float64 `json:"filament_mm,omitempty"`
	// Colour is the colour the slicer shows the filament in, such as #FF8000.
	Colour string `json:"colour,omitempty"`
}

// PrintBounds is a rectangle on the bed, in millimetres from its origin.
type PrintBounds struct {
	MinXMM float64 `json:"min_x_mm"`
	MinYMM float64 `json:"min_y_mm"`
	MaxXMM float64 `json:"max_x_mm"`
	MaxYMM float64 `json:"max_y_mm"`
}

// PrintBed is a printable volume, in millimetres. A zero means the file did
// not say.
type PrintBed struct {
	WidthMM  float64 `json:"width_mm,omitempty"`
	DepthMM  float64 `json:"depth_mm,omitempty"`
	HeightMM float64 `json:"height_mm,omitempty"`
}

// PrintChecks is what the M862 and M115 lines at the start of the G-code ask
// the printer to verify.
type PrintChecks struct {
	// PrinterModel is the model an M862.3 line names.
	PrinterModel string `json:"printer_model,omitempty"`
	// PrinterCode is the numeric model code an M862.2 line names.
	PrinterCode int `json:"printer_code,omitempty"`
	// Nozzles is what each M862.1 line asks of a nozzle.
	Nozzles []PrintNozzleCheck `json:"nozzles,omitempty"`
	// MinFirmware is the lowest firmware an M862.4 line accepts.
	MinFirmware string `json:"min_firmware,omitempty"`
	// GCodeLevel is the G-code level an M862.5 line names.
	GCodeLevel int `json:"gcode_level,omitempty"`
	// Features are the firmware features M862.6 lines name, such as
	// "Input shaper".
	Features []string `json:"features,omitempty"`
	// Firmware is the version an M115 U line names: the newest firmware the
	// slicer's profile knows.
	Firmware string `json:"firmware,omitempty"`
}

// PrintNozzleCheck is one M862.1 line.
type PrintNozzleCheck struct {
	// Index is n of the Tn the line names, counted from 0. A line that names
	// no T is about T0.
	Index int `json:"index"`
	// DiameterMM is the nozzle diameter the line asks for.
	DiameterMM float64 `json:"diameter_mm,omitempty"`
	// Hardened and HighFlow say whether the line asks for a hardened or a
	// high-flow nozzle, and are nil when the line does not say.
	Hardened *bool `json:"hardened,omitempty"`
	HighFlow *bool `json:"high_flow,omitempty"`
}

// WriteResult is the outcome of one committed write: the path, the length and
// the SHA-256 in lowercase hex. The provider computes the digest from the
// bytes it wrote. A provider that cannot compute it leaves SHA256 empty.
type WriteResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

// Writer is one object being written into a store. The name keeps its earlier
// content until Commit returns nil. A write that fails part way must leave
// that content untouched.
type Writer interface {
	io.Writer

	// Commit publishes the object and reports what was written. Always check
	// its error: out of space and a failed remote upload are reported here.
	// Call it once. A second call reports ErrWriteClosed.
	Commit(ctx context.Context) (WriteResult, error)

	// Abort discards the write and removes what it staged. It does nothing
	// after Commit, so a caller can defer it.
	Abort(ctx context.Context) error
}

// Caps says which operations a provider serves. A provider sets only Browse
// and Write. [Probe] sets the other four from the optional interfaces the
// provider implements. A caller reads Caps from Probe, not from Capabilities.
type Caps struct {
	// Browse: List, Stat and Open return real data.
	Browse bool
	// Write: Create, Delete, DeleteMany and Mkdir mutate storage.
	Write bool
	// DownloadURL: the provider implements Presigner.
	DownloadURL bool
	// LocalFile: the provider implements LocalFiles.
	LocalFile bool
	// Rename: the provider implements Renamer.
	Rename bool
	// Range: the provider implements RangeReader.
	Range bool
}

// Probe reports what a provider serves. Browse and Write come from
// Capabilities. The rest come from the optional interfaces it implements.
func Probe(p Provider) Caps {
	caps := p.Capabilities()

	_, caps.DownloadURL = p.(Presigner)
	_, caps.LocalFile = p.(LocalFiles)
	_, caps.Rename = p.(Renamer)
	_, caps.Range = p.(RangeReader)

	return caps
}

// LocalFile is an open, read-only handle to one object in a filesystem-backed
// store. The provider resolves the name inside its root and opens it in one
// step, so a link planted at the name afterwards has no effect.
type LocalFile interface {
	io.ReadSeekCloser
	// Stat describes the open object, not the name it came from.
	Stat() (fs.FileInfo, error)
}

// LocalWriter is an open handle to an object a filesystem-backed provider
// created inside its root. Always check the error of Close: on some media
// out of space is reported only there.
type LocalWriter interface {
	io.WriteCloser
	// Name is the absolute on-disk path of the open object. It is for log
	// lines and for a later delete. Do not open it again.
	Name() string
}

// Provider is a storage backend. An implementation must treat every relative
// path as untrusted input and reject one that would leave its root. A provider
// that reaches a remote must honour the context.
type Provider interface {
	// Name is the provider's unique identifier / storage id (e.g. "local").
	Name() string
	// DisplayName is the human-friendly label shown in the UI.
	DisplayName() string
	// List returns the entries directly inside relDir ("" = root).
	List(ctx context.Context, relDir string) ([]FileInfo, error)
	// Stat returns metadata for a single entry.
	Stat(ctx context.Context, relPath string) (FileInfo, error)
	// Open opens a file for reading (used for download / streaming to printers).
	Open(ctx context.Context, relPath string) (io.ReadCloser, error)
	// Create starts a write at relPath, creating parent directories as needed.
	// Only Writer.Commit publishes the object. Create applies the upload
	// allowlist, because the name comes from a caller.
	Create(ctx context.Context, relPath string) (Writer, error)
	// Delete removes a file or (recursively) a directory.
	Delete(ctx context.Context, relPath string) error
	// DeleteMany removes several files/folders in one call. Implementations
	// should attempt every path and report if any failed.
	DeleteMany(ctx context.Context, relPaths []string) error
	// Mkdir creates a directory (and parents) at relPath.
	Mkdir(ctx context.Context, relPath string) error
	// Capabilities sets Browse and Write of Caps. It must be cheap and do no
	// I/O. A caller reads Probe, not this.
	Capabilities() Caps
}

// Renamer is the optional ability to move a name inside one store in a single
// operation.
type Renamer interface {
	// Rename moves from to to within the provider.
	Rename(ctx context.Context, from, to string) error
}

// ExtensionPolicy is the optional ability to report the file extensions that
// Create accepts: lowercase, with the leading dot. An empty list means any
// name is accepted.
type ExtensionPolicy interface {
	AllowedExtensions() []string
}

// Presigner is the optional ability to give a printer a URL to fetch an
// object from directly, so the host does not relay the bytes.
type Presigner interface {
	// DownloadURL returns a URL a printer can fetch relPath from. ok is false
	// for an object this store will not presign.
	DownloadURL(ctx context.Context, relPath string) (url string, ok bool)
}

// RangeReader is the optional ability to serve part of an object without a
// seekable handle.
type RangeReader interface {
	// OpenRange returns length bytes of relPath from offset. A length of -1
	// means to the end. An offset at or past the object's length returns a
	// reader at end of file, not an error.
	OpenRange(ctx context.Context, relPath string, offset, length int64) (io.ReadCloser, error)
}

// LocalFiles is the optional ability to hand the host an open file handle on
// the store's own disk. A remote store never implements it.
type LocalFiles interface {
	// OpenLocal opens relPath for reading. It must resolve and open in one
	// step inside the store's root. It reports ErrInvalidPath for a path that
	// names nothing, names a directory, or leaves the store.
	OpenLocal(ctx context.Context, relPath string) (LocalFile, error)
	// CreateLocal creates or truncates relPath, creating parent directories
	// as needed. It does not apply the upload allowlist, so use it only for a
	// name the host chose. For a name from a caller use Create. It reports
	// the errors of OpenLocal, and ErrInvalidPath for the store root.
	CreateLocal(ctx context.Context, relPath string) (LocalWriter, error)
}

// NewDigest returns the hash every provider computes over a write, so the
// digests of two providers are comparable.
func NewDigest() hash.Hash { return sha256.New() }

// Sum returns a digest as lowercase hex.
func Sum(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

// Save writes r into p under relPath and reports what was written. It calls
// Create, copies, calls Commit, and calls Abort on any failure.
func Save(ctx context.Context, p Provider, relPath string, r io.Reader) (WriteResult, error) {
	writer, err := p.Create(ctx, relPath)
	if err != nil {
		return WriteResult{}, err
	}

	if _, err = io.Copy(writer, r); err != nil {
		// Return the copy error, not the error of the cleanup.
		_ = writer.Abort(ctx)

		return WriteResult{}, err
	}

	result, err := writer.Commit(ctx)
	if err != nil {
		_ = writer.Abort(ctx)

		return WriteResult{}, err
	}

	return result, nil
}

// ProviderSpec describes one storage instance to build (mapped from config).
type ProviderSpec struct {
	ID                string
	Type              string
	Name              string
	Path              string
	AllowedExtensions []string
}

// ProviderFactory builds a Provider from its spec.
type ProviderFactory func(spec ProviderSpec) (Provider, error)

// Contract is the version of the storage contract this build serves. An
// implementation declares the version it compiled against and is refused at
// registration when the two are incompatible.
const Contract = extension.Version("1.0.0")

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
	ErrNoFactory = errors.New("storageapi: type has no factory")
	// ErrUnknownProvider reports a storage type no registration answers to.
	ErrUnknownProvider = extension.ErrUnknown
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
func (r Registration) Build(spec ProviderSpec) (Provider, error) {
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

	descriptor, err := extension.Declare(extension.KindStorage, Contract, name, doc)
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

// Registry is the table of storage types of one host. It is immutable and
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

	inner, err := extension.NewRegistry(extension.KindStorage, entries...)
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
func (r *Registry) BuildProvider(spec ProviderSpec) (Provider, error) {
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
