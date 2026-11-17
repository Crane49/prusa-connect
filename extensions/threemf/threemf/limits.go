package threemf

import (
	"errors"
	"fmt"
)

// Limits bounds what one Read or Rewrite may spend on one file. A zero field
// takes the value of DefaultLimits, so Limits{} is a usable value.
type Limits struct {
	// MaxEntries is the most zip entries an archive may have.
	MaxEntries int
	// MaxModelBytes is the most bytes of model XML read from one file,
	// counting every model part together, after decompression.
	MaxModelBytes int64
	// MaxMetadataBytes is the most bytes one settings or metadata file may
	// have after decompression.
	MaxMetadataBytes int64
	// MaxThumbnailBytes is the largest picture kept.
	MaxThumbnailBytes int64
	// MaxThumbnails is the most pictures kept.
	MaxThumbnails int
	// MaxResources is the most objects 3D/3dmodel.model may declare.
	MaxResources int
	// MaxItems is the most build items a file may have.
	MaxItems int
	// MaxVertices is the most vertices and triangles kept in memory, counted
	// together, to draw outlines and to judge where an instance stands. A mesh
	// beyond it is drawn from its box and the read goes on.
	MaxVertices int
	// MaxComponentDepth is how deep components may nest.
	MaxComponentDepth int
	// MaxMeshUses is the most meshes the objects of a file may be built of
	// together, counting a mesh once for every component that leads to it.
	// Components that point twice at the next object, thirty levels deep,
	// would build an object of a thousand million meshes from a file of a few
	// kilobytes.
	MaxMeshUses int
}

// DefaultLimits are the limits a zero Limits field takes. They hold memory to
// a few hundred megabytes for the largest file they let through.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:        defaultMaxEntries,
		MaxModelBytes:     defaultMaxModelBytes,
		MaxMetadataBytes:  defaultMaxMetadataBytes,
		MaxThumbnailBytes: defaultMaxThumbnailBytes,
		MaxThumbnails:     defaultMaxThumbnails,
		MaxResources:      defaultMaxResources,
		MaxItems:          defaultMaxItems,
		MaxVertices:       defaultMaxVertices,
		MaxComponentDepth: defaultMaxComponentDepth,
		MaxMeshUses:       defaultMaxMeshUses,
	}
}

// The values of DefaultLimits. MaxVertices costs twelve bytes a vertex or a
// triangle, so its default keeps them to 144 MB.
const (
	defaultMaxEntries        = 4096
	defaultMaxModelBytes     = 1 << 30
	defaultMaxMetadataBytes  = 64 << 20
	defaultMaxThumbnailBytes = 4 << 20
	defaultMaxThumbnails     = 16
	defaultMaxResources      = 100_000
	defaultMaxItems          = 100_000
	defaultMaxVertices       = 12_000_000
	defaultMaxComponentDepth = 32
	defaultMaxMeshUses       = 1_000_000
)

// withDefaults fills every zero field from DefaultLimits.
func (l Limits) withDefaults() Limits {
	d := DefaultLimits()

	pick := func(value, fallback int) int {
		if value > 0 {
			return value
		}

		return fallback
	}
	pick64 := func(value, fallback int64) int64 {
		if value > 0 {
			return value
		}

		return fallback
	}

	return Limits{
		MaxEntries:        pick(l.MaxEntries, d.MaxEntries),
		MaxModelBytes:     pick64(l.MaxModelBytes, d.MaxModelBytes),
		MaxMetadataBytes:  pick64(l.MaxMetadataBytes, d.MaxMetadataBytes),
		MaxThumbnailBytes: pick64(l.MaxThumbnailBytes, d.MaxThumbnailBytes),
		MaxThumbnails:     pick(l.MaxThumbnails, d.MaxThumbnails),
		MaxResources:      pick(l.MaxResources, d.MaxResources),
		MaxItems:          pick(l.MaxItems, d.MaxItems),
		MaxVertices:       pick(l.MaxVertices, d.MaxVertices),
		MaxComponentDepth: pick(l.MaxComponentDepth, d.MaxComponentDepth),
		MaxMeshUses:       pick(l.MaxMeshUses, d.MaxMeshUses),
	}
}

// The errors Read and Rewrite return. Each is wrapped with what was being
// read, so a caller compares with errors.Is.
var (
	// ErrFormat is a file that is not a 3MF this package can read: not a zip,
	// no model, a model that is not XML, or a reference the format forbids.
	ErrFormat = errors.New("threemf: not a readable 3MF file")
	// ErrLimit is a file larger than the Limits allow. A LimitError says which
	// limit.
	ErrLimit = errors.New("threemf: limit exceeded")
	// ErrSelection is a Selection the project cannot satisfy.
	ErrSelection = errors.New("threemf: selection does not fit the project")
	// ErrMismatch is a Rewrite given a project that was not read from the
	// same file.
	ErrMismatch = errors.New("threemf: project does not match the file")
	// ErrUnsupported is a Rewrite of a project that is not a PrusaSlicer
	// project. Only a PrusaSlicer file is rewritten for PrusaSlicer to slice.
	ErrUnsupported = errors.New("threemf: not supported for this producer")
)

// LimitError is a file that went past one of the Limits.
type LimitError struct {
	// Limit is the name of the Limits field, such as MaxEntries.
	Limit string
	// Max is the value the limit had.
	Max int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("threemf: the file exceeds %s (%d)", e.Limit, e.Max)
}

// Is makes errors.Is(err, ErrLimit) true for every LimitError.
func (e *LimitError) Is(target error) bool { return target == ErrLimit }

func limitError(name string, value int64) error {
	return &LimitError{Limit: name, Max: value}
}

func formatError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFormat, fmt.Sprintf(format, args...))
}
