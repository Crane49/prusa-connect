// Package settings holds what an operator can change about this module, with
// the default of each.
//
// The defaults here are the defaults module.yaml publishes, and
// manifest_test.go fails when the two differ.
package settings

import (
	"errors"
	"fmt"

	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// The default of every setting, and the bounds a value is held to.
const (
	// DefaultRetentionHours is a day, which covers a fault a person noticed
	// yesterday and reports today.
	DefaultRetentionHours = 24
	// DefaultMaxMiB fits a Raspberry Pi's card many times over.
	DefaultMaxMiB = 128

	MinRetentionHours = 1
	MaxRetentionHours = 24 * 30
	MinMiB            = 4
	MaxMiB            = 4096
)

// mebibyte is one MiB in bytes.
const mebibyte = 1 << 20

// logShare is the part of the room the log lines keep for themselves. A
// printer sends far more metric points than log lines, and the lines are what
// a person reads first, so the points never push them out.
const logShare = 4

// ErrInvalid reports a setting the operator cannot have meant. The host shows
// it on the instance, which then does not start.
var ErrInvalid = errors.New("buddy-debug-logs: a setting is out of range")

// Values is one set of settings, as the host merged them.
type Values struct {
	RetentionHours int `json:"retention_hours"`
	MaxMiB         int `json:"max_mib"`
}

// Defaults is what the module does when the operator writes nothing.
func Defaults() Values {
	return Values{RetentionHours: DefaultRetentionHours, MaxMiB: DefaultMaxMiB}
}

// Checked refuses a value outside its range rather than replacing it, so an
// operator who wrote one finds out at start.
func (v Values) Checked() (Values, error) {
	if v.RetentionHours < MinRetentionHours || v.RetentionHours > MaxRetentionHours {
		return Values{}, fmt.Errorf("%w: retention_hours is %d, expected %d to %d",
			ErrInvalid, v.RetentionHours, MinRetentionHours, MaxRetentionHours)
	}

	if v.MaxMiB < MinMiB || v.MaxMiB > MaxMiB {
		return Values{}, fmt.Errorf("%w: max_mib is %d, expected %d to %d", ErrInvalid, v.MaxMiB, MinMiB, MaxMiB)
	}

	return v, nil
}

// Limits is what the store keeps under these settings: a quarter of the room
// for the log lines and the rest for the metrics.
func (v Values) Limits() store.Limits {
	total := int64(v.MaxMiB) * mebibyte
	logs := total / logShare

	return store.Limits{
		Hours: v.RetentionHours,
		Bytes: map[store.Stream]int64{store.Syslog: logs, store.Metrics: total - logs},
	}
}
