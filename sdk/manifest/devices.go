package manifest

import (
	"fmt"
	"reflect"
)

// The devices selector names the devices an entry of a device slot is shown
// for, as file_types does for files.

// DeviceSelector is the condition a device's record has to meet for an entry
// to be shown on it. The zero value is no condition: the entry is shown on
// every device.
type DeviceSelector struct {
	// Resident shows the entry only on a device whose record has
	// resident: true, which is a device the server runs on.
	Resident bool

	// The selector cannot be compared with ==, so a later condition held in a
	// list adds a field without breaking code that compares two selectors.
	_ [0]func()
}

// IsZero reports a selector that names no condition. A selector cannot be
// compared with ==, so test it with IsZero.
func (s DeviceSelector) IsZero() bool {
	return reflect.ValueOf(s).IsZero()
}

// deviceSelectorKeys is the keys a devices block may carry.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var deviceSelectorKeys = []string{"resident"}

// readDevices reads an entry's optional devices block. It refuses a block
// that names no condition, and resident: false.
func readDevices(entry map[string]any, where string) (DeviceSelector, error) {
	block, declared, err := node(entry, where, "devices")
	if err != nil || !declared {
		return DeviceSelector{}, err
	}

	place := where + ".devices"

	if err = refuseUnknown(block, place, deviceSelectorKeys); err != nil {
		return DeviceSelector{}, err
	}

	if held, present := block["resident"]; !present || held == nil {
		return DeviceSelector{}, fmt.Errorf("%w: %s names no condition, expected resident: true; "+
			"drop devices to show the entry on every device", ErrInvalid, place)
	}

	var selector DeviceSelector

	if selector.Resident, err = truth(block, place, "resident"); err != nil {
		return DeviceSelector{}, err
	}

	if !selector.Resident {
		return DeviceSelector{}, fmt.Errorf("%w: %s.resident is false, and only true is read; "+
			"drop devices to show the entry on every device", ErrInvalid, place)
	}

	return selector, nil
}
