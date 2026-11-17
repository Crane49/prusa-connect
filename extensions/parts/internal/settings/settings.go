// Package settings holds what an operator can change about this module, with
// the default of each.
//
// The defaults here are the defaults module.yaml publishes, and
// manifest_test.go fails when the two differ. What the manifest tells an
// operator is therefore what this code does when nobody writes a config block.
package settings

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/prusa3d/parts/internal/catalog"
)

// The default of every setting.
const (
	// DefaultStorage is the storage the catalogue's files go into. It is the
	// name the server's own configuration gives its durable storage.
	DefaultStorage = "local"

	// DefaultRoot is the folder inside that storage the files go under, so
	// the catalogue's files are kept apart from what else the storage holds.
	DefaultRoot = "parts"

	// MaxStorageBytes bounds the storage name, as the storage door does.
	MaxStorageBytes = 63
)

// Values is one set of settings, as the host merged them.
type Values struct {
	Storage string
	Root    string
}

// Defaults is what the module does when the operator writes nothing.
func Defaults() Values {
	return Values{Storage: DefaultStorage, Root: DefaultRoot}
}

// ErrInvalid reports a setting the operator cannot have meant. The host
// shows it on the instance, which then does not start.
var ErrInvalid = errors.New("parts: a setting is not one this module can use")

// Checked answers the settings with the spaces around each value trimmed,
// and refuses a value the operator cannot have meant.
//
// The host checks the type of each key and this checks the shape. A storage
// name that is empty, or a root that climbs out of the storage, is refused
// rather than replaced with the default. A default chosen in silence would
// put files somewhere the operator did not say, and nobody would find out.
func (v Values) Checked() (Values, error) {
	storage := strings.TrimSpace(v.Storage)
	if storage == "" || len(storage) > MaxStorageBytes ||
		strings.ContainsFunc(storage, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) ||
		strings.ContainsAny(storage, "/\\") {
		return Values{}, fmt.Errorf(
			"%w: storage %q is not the name of a storage, which is one word of at most %d bytes",
			ErrInvalid,
			v.Storage,
			MaxStorageBytes,
		)
	}

	root := strings.Trim(strings.TrimSpace(v.Root), "/")
	if err := catalog.ValidRoot(root); err != nil {
		return Values{}, fmt.Errorf("%w: root %q is not a relative folder inside the storage", ErrInvalid, v.Root)
	}

	return Values{Storage: storage, Root: root}, nil
}

// Place is where new files go, as the catalogue reads it.
func (v Values) Place() catalog.Place {
	return catalog.Place{Storage: v.Storage, Root: v.Root}
}

// Live is the settings as they stand, readable from every request while the
// host sends a new set.
//
// The zero value answers the defaults.
type Live struct {
	held atomic.Pointer[Values]
}

// Set takes one merged set of settings from the host, and refuses a set
// Checked refuses. A refused set leaves the settings as they stood.
func (l *Live) Set(values Values) error {
	checked, err := values.Checked()
	if err != nil {
		return err
	}

	l.held.Store(&checked)

	return nil
}

// Get answers the settings as they stand.
func (l *Live) Get() Values {
	held := l.held.Load()
	if held == nil {
		return Defaults()
	}

	return *held
}
