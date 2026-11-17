package install

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// What the host does when a module's process goes over its memory budget.
const (
	// OnOverMemoryStop kills the process tree and counts towards the
	// switch-off.
	OnOverMemoryStop = "stop"
	// OnOverMemoryWarn keeps the process running and marks its status.
	OnOverMemoryWarn = "warn"
)

// limitsOf refuses a service block whose record room, memory budget,
// on_over_memory or switch_off is out of bounds, and returns its switch_off.
func limitsOf(declared *ServiceEntry) (SwitchOff, error) {
	switchOff, err := switchOffOf(declared.SwitchOff)

	return switchOff, errors.Join(
		checkRecordsMiB("service.records_mib", declared.RecordsMiB),
		CheckMemoryMiB("service.memory_mib", declared.MemoryMiB),
		CheckOnOverMemory("service.on_over_memory", declared.OnOverMemory),
		err)
}

// CheckMemoryMiB refuses a budget outside the bounds a manifest may
// recommend. Zero sets none and passes.
func CheckMemoryMiB(key string, mib int) error {
	if mib != 0 && (mib < manifest.MinMemoryMiB || mib > manifest.MaxMemoryMiB) {
		return fmt.Errorf("%s is %d, expected 0 to follow the module, or %d to %d",
			key, mib, manifest.MinMemoryMiB, manifest.MaxMemoryMiB)
	}

	return nil
}

// CheckOnOverMemory refuses a value other than stop, warn or empty.
func CheckOnOverMemory(key, value string) error {
	switch value {
	case "", OnOverMemoryStop, OnOverMemoryWarn:
		return nil
	default:
		return fmt.Errorf("%s is %q, expected %s or %s", key, value, OnOverMemoryStop, OnOverMemoryWarn)
	}
}

// The automatic switch-off of a service module that keeps failing. The
// defaults apply when neither the server nor the operator sets a value.
const (
	DefaultSwitchOffFailures = 3
	DefaultSwitchOffWindow   = time.Hour
	MaxSwitchOffFailures     = 100
	MinSwitchOffWindow       = time.Minute
	MaxSwitchOffWindow       = 24 * time.Hour
)

// SwitchOff is when the host switches a failing service module off: Failures
// failed stops or starts inside Window. Zero failures never switches it off. A
// nil Failures or a zero Window is not set, and the level below applies: the
// server's core.modules_switch_off under an instance, the defaults under the
// server.
type SwitchOff struct {
	Failures *int          `yaml:"failures"`
	Window   time.Duration `yaml:"window"`
}

// IsZero reports whether nothing is set.
func (o SwitchOff) IsZero() bool { return o.Failures == nil && o.Window == 0 }

// Equal reports whether both set the same values.
func (o SwitchOff) Equal(other SwitchOff) bool {
	return o.Window == other.Window &&
		(o.Failures == nil) == (other.Failures == nil) &&
		(o.Failures == nil || *o.Failures == *other.Failures)
}

// Over is o with every value it does not set taken from under.
func (o SwitchOff) Over(under SwitchOff) SwitchOff {
	if o.Failures == nil {
		o.Failures = under.Failures
	}

	if o.Window == 0 {
		o.Window = under.Window
	}

	return o
}

// Entry is o as the service block writes it, and nil when nothing is set.
func (o SwitchOff) Entry() *SwitchOffEntry {
	if o.IsZero() {
		return nil
	}

	entry := &SwitchOffEntry{Failures: o.Failures}
	if o.Window != 0 {
		entry.Window = shortDuration(o.Window)
	}

	return entry
}

// shortDuration writes a duration without its zero units, 6h and not 6h0m0s.
func shortDuration(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}

	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}

	return text
}

// SwitchOffEntry is the switch_off block as an operator writes it. The window
// is a Go duration such as 1h, kept as text in its shortest form so the overlay
// reads the way the operator's file does.
type SwitchOffEntry struct {
	Failures *int   `json:"failures,omitempty" yaml:"failures"`
	Window   string `json:"window,omitempty"   yaml:"window"`
}

// switchOffOf parses and checks a service block's switch_off. Nil sets
// nothing.
func switchOffOf(entry *SwitchOffEntry) (SwitchOff, error) {
	if entry == nil {
		return SwitchOff{}, nil
	}

	out := SwitchOff{Failures: entry.Failures}

	if entry.Window != "" {
		window, err := time.ParseDuration(entry.Window)
		if err != nil {
			return SwitchOff{}, fmt.Errorf(
				"service.switch_off.window is %q, expected a duration such as 1h",
				entry.Window,
			)
		}

		out.Window = window
	}

	return out, CheckSwitchOff("service.switch_off", out)
}

// CheckSwitchOff refuses a failure count or a window out of bounds. A value
// that is not set passes.
func CheckSwitchOff(key string, o SwitchOff) error {
	if o.Failures != nil && (*o.Failures < 0 || *o.Failures > MaxSwitchOffFailures) {
		return fmt.Errorf("%s.failures is %d, expected 0 to never switch off, or 1 to %d",
			key, *o.Failures, MaxSwitchOffFailures)
	}

	if o.Window != 0 && (o.Window < MinSwitchOffWindow || o.Window > MaxSwitchOffWindow) {
		return fmt.Errorf("%s.window is %s, expected %s to %s", key, shortDuration(o.Window),
			shortDuration(MinSwitchOffWindow), shortDuration(MaxSwitchOffWindow))
	}

	return nil
}
