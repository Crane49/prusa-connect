package machinetext_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// sdk/connectorapi tells a driver author how much of a value survives, and this
// package decides how much really does. A host that keeps less than the SDK
// promises breaks a published contract, and nothing else compares the two
// numbers.
func TestTheHostKeepsAtLeastTheFloorTheDriverContractPromises(t *testing.T) {
	t.Parallel()

	t.Logf("observed: the SDK promises %d bytes and the host keeps %d",
		connectorapi.RetainedTextFloor, machinetext.MaxBytes)

	require.GreaterOrEqual(t, machinetext.MaxBytes, connectorapi.RetainedTextFloor,
		"the host retains less of a value than sdk/connectorapi promises a driver author")
}

// A string as wide as anything the Buddy firmware can display passes through
// unchanged. Clipping is what a defect or a hostile driver meets, not a
// printer.
func TestTextInsideTheBoundIsTheDevicesOwnWord(t *testing.T) {
	t.Parallel()

	// 444 bytes: wider than any string in the Buddy translation catalogues.
	widest := strings.Repeat("あ", 148)
	require.Len(t, widest, 444)

	require.Equal(t, widest, machinetext.Clip(widest, machinetext.MaxBytes),
		"a string the firmware really displays was cut")
}

// The vendor, model, serial and firmware a driver reports are cut like any
// other text a device chose. The identifier the host files the machine under is
// not: a cut identifier names a different machine.
func TestAnIdentityIsCutFieldByFieldAndKeepsItsIdentifier(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("ř", machinetext.IdentityMaxBytes)

	bounded := machinetext.ClipIdentity(connectorapi.Identity{
		Vendor:       long,
		Model:        long,
		Serial:       long,
		Firmware:     connectorapi.NewFirmwareLabel(long),
		UnitUID:      long,
		UnitUIDAlias: long,
	})

	firmware, err := bounded.Firmware.MarshalText()
	require.NoError(t, err)

	for name, field := range map[string]string{
		"vendor": bounded.Vendor, "model": bounded.Model,
		"serial": bounded.Serial, "firmware": string(firmware),
	} {
		require.True(t, utf8.ValidString(field), name+" was cut through a character")
		require.True(t, strings.HasSuffix(field, machinetext.Marker),
			name+" was cut and a reader cannot tell it was cut")
		require.LessOrEqual(t, len(field), machinetext.IdentityMaxBytes+len(machinetext.Marker))
	}

	require.Equal(t, long, bounded.UnitUID, "the identifier the host files a machine under was cut")
	require.Equal(t, long, bounded.UnitUIDAlias, "the second spelling of that identifier was cut")
}

// A cut value is still valid UTF-8 and carries the marker. A cut through a
// multi-byte character would reach a JSON encoder as the replacement character,
// and most of what a firmware writes is not in English.
func TestAClippedValueIsStillTextAndSaysItWasCut(t *testing.T) {
	t.Parallel()

	// The four-byte case is written as a code point, so the source stays
	// readable in any editor.
	for _, character := range []string{"a", "ř", "€", "\U0001F600"} {
		t.Run(character, func(t *testing.T) {
			t.Parallel()

			// One character per byte of the bound, plus one, so the input is
			// over the bound even when a character costs one byte.
			over := strings.Repeat(character, machinetext.MaxBytes+1)
			cut := machinetext.Clip(over, machinetext.MaxBytes)

			t.Logf("observed: a %d-byte character clipped to %d bytes", len(character), len(cut))

			require.True(t, utf8.ValidString(cut), "the bound cut a character in half")
			require.True(t, strings.HasSuffix(cut, machinetext.Marker),
				"the value was cut and a reader cannot tell it was cut")
			require.LessOrEqual(t, len(cut), machinetext.MaxBytes+len(machinetext.Marker))
			require.Contains(t, cut, character)
		})
	}
}
