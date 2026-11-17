package resourcehost

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestEveryConditionOfASelectorReachesTheWire is ADR-20261110-5np rules 7 and
// 10 for a condition added later. A field of the selector that devicesOf does
// not copy would go out as a block that names no condition.
func TestEveryConditionOfASelectorReachesTheWire(t *testing.T) {
	t.Parallel()

	fields := reflect.TypeFor[manifest.DeviceSelector]()

	for i := range fields.NumField() {
		field := fields.Field(i)
		if !field.IsExported() {
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()

			var selector manifest.DeviceSelector

			condition := reflect.ValueOf(&selector).Elem().Field(i)
			require.Equal(t, reflect.Bool, condition.Kind(),
				"this test sets only a boolean condition, so teach it to set a %s", condition.Kind())
			condition.SetBool(true)

			wire := devicesOf(selector)
			require.NotNil(t, wire, "a selector that names %s is left off the wire", field.Name)

			carried := reflect.ValueOf(*wire).FieldByName(field.Name)
			require.True(t, carried.IsValid(), "the wire selector has no field %s", field.Name)
			require.False(t, carried.IsZero(), "devicesOf does not carry %s to the wire", field.Name)
		})
	}
}
