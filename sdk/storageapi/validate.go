package storageapi

import (
	"errors"
	"path"
	"strings"
)

var (
	// ErrInvalidPath is returned for a path a provider will not resolve: empty
	// where a name is required, naming nothing in the store, or leaving the
	// provider root. A missing name and an escape must return the same error,
	// so a caller cannot learn which names point out of the store.
	ErrInvalidPath = errors.New("invalid storage path")
	// ErrExtensionNotAllowed is returned when an uploaded file's extension is
	// not in the configured allowlist.
	ErrExtensionNotAllowed = errors.New("file extension not allowed")
	// ErrDeletePartial is returned by DeleteMany when some entries could not be
	// deleted. The rest are removed.
	ErrDeletePartial = errors.New("some entries could not be deleted")
	// ErrNoLocalFile is returned by OpenLocal and CreateLocal of a provider
	// that cannot give a file handle. The caller falls back to Open or
	// DownloadURL. Any other error is a real failure.
	ErrNoLocalFile = errors.New("provider serves no local file handles")
	// ErrWriteClosed is returned by a Writer whose Commit or Abort has already
	// run.
	ErrWriteClosed = errors.New("the write is already finished")
	// ErrNotSupported is returned by a host that asked a provider for an
	// optional ability the provider does not implement. A provider never
	// returns it.
	ErrNotSupported = errors.New("the storage does not serve that operation")
)

// DefaultAllowedExtensions is the built-in allowlist of upload extensions:
// printable formats, 3D model formats, firmware files and common images.
// Lowercase, with the leading dot.
var DefaultAllowedExtensions = []string{
	".gcode", ".bgcode", ".gco", ".g",
	".stl", ".3mf", ".obj",
	".bbf",
	".png", ".jpg", ".jpeg", ".webp", ".gif",
}

// CleanRelPath returns the canonical relative form of rel. It trims
// surrounding space, turns backslashes into "/", resolves "." and ".."
// segments, and strips a leading "/", so the result cannot leave the provider
// root. The empty path is the root. Providers derive FileInfo.Path with it.
func CleanRelPath(rel string) string {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	clean := path.Clean("/" + strings.TrimPrefix(rel, "/"))
	return strings.TrimPrefix(clean, "/")
}

// ValidateExtension reports whether name has an allowed extension. The allowed
// list is matched case-insensitively. An empty allowed list permits anything.
func ValidateExtension(name string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	ext := strings.ToLower(path.Ext(name))
	for _, a := range allowed {
		if ext == strings.ToLower(a) {
			return nil
		}
	}
	return ErrExtensionNotAllowed
}

// IsPrintableExtension reports whether name is a G-code file a printer can
// print.
func IsPrintableExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".gcode", ".bgcode", ".gco", ".g":
		return true
	default:
		return false
	}
}

// IsSliceableExtension reports whether name is a 3D model that a slicer can
// open.
func IsSliceableExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".stl", ".3mf", ".obj":
		return true
	default:
		return false
	}
}
