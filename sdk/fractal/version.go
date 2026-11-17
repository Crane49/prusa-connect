package fractal

import (
	"errors"
	"fmt"
)

// Version is the protocol version this package implements.
const Version uint = 1

// ErrUnsupportedVersion reports that the version field of a message names a
// protocol version this package does not implement. A node drops such a
// message, except a join request, which it denies.
var ErrUnsupportedVersion = errors.New("unsupported protocol version")

// supported lists every protocol version this package implements.
var supported = [...]uint{Version}

// SupportedVersion reports whether this package implements v.
func SupportedVersion(v uint) bool {
	for _, s := range supported {
		if s == v {
			return true
		}
	}
	return false
}

// SupportedVersions returns every protocol version this package implements, in
// the order they were defined. The caller owns the returned slice.
func SupportedVersions() []uint {
	out := make([]uint, len(supported))
	copy(out, supported[:])
	return out
}

// checkVersion returns ErrUnsupportedVersion for a version this package does
// not implement.
func checkVersion(v uint) error {
	if !SupportedVersion(v) {
		return fmt.Errorf("version %d: %w", v, ErrUnsupportedVersion)
	}
	return nil
}
