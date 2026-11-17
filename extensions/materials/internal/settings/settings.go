// Package settings holds what an operator can change about this module, with
// the default of each.
//
// The defaults here are the defaults module.yaml publishes, and the module
// does what they say when nobody writes a config block.
package settings

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// DefaultMaxSpools is how many spools the module holds when the operator
// writes nothing. It is the default of max_spools in module.yaml.
const DefaultMaxSpools = 2000

// Values is one set of settings, as the host merged them.
type Values struct {
	MaxSpools int
}

// Defaults is what the module does when the operator writes nothing.
func Defaults() Values {
	return Values{MaxSpools: DefaultMaxSpools}
}

// ErrInvalid reports a setting the operator cannot have meant. The host shows
// it on the instance, which then does not start.
var ErrInvalid = errors.New("materials: a setting is not one this module can use")

// Checked refuses a value the operator cannot have meant. The host checks the
// type of each key and this checks the range.
func (v Values) Checked() (Values, error) {
	if v.MaxSpools < 1 {
		return Values{}, fmt.Errorf("%w: max_spools is %d, which must be at least 1", ErrInvalid, v.MaxSpools)
	}

	return v, nil
}

// Live is the settings as they stand, readable from every request while the
// host sends a new set. The zero value answers the defaults.
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
