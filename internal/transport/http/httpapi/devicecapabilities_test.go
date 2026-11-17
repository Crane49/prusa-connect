package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// resolvedSet is a descriptor of the shapes a driver declares: a bounded
// number with a unit, a magnitude bound with no floor, a bounded string, a
// closed set, a default, a verb with no arguments and a feature.
func resolvedSet() connectorapi.Set {
	return connectorapi.Set{
		"core.command.set_nozzle_temperature": {Flag: true},
		"core.command.set_nozzle_temperature#celsius": {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: 300, HasLimit: true, Unit: connectorapi.UnitCelsius,
		},
		"core.command.move_z": {Flag: true},
		"core.command.move_z#distance": {
			Flag: true, Kind: connectorapi.KindNumber, Required: true,
			Limit: 50, HasLimit: true, Unit: connectorapi.UnitMillimeter,
		},
		"core.command.start_print": {Flag: true},
		"core.command.start_print#path": {
			Flag:     true,
			Kind:     connectorapi.KindString,
			Required: true,
			Limit:    255,
			HasLimit: true,
		},
		"core.command.home": {Flag: true},
		"core.command.home#axis": {
			Flag: true, Kind: connectorapi.KindString, Supported: []string{"X", "XY"}, Default: "XY",
		},
		"core.command.beep":      {Flag: true},
		"core.feature.dialogs":   {Flag: true},
		"core.command.withdrawn": {Flag: false},
	}
}

func TestTheProjectionWritesEveryArgumentAsARange(t *testing.T) {
	t.Parallel()

	got := deviceCapabilitiesOf("printer-1", DeviceCapabilitySet{Set: resolvedSet(), Epoch: 7, ProfileMatched: true})

	require.Equal(t, "printer-1", got.ID)
	require.Equal(t, uint64(7), got.CapabilityEpoch)
	require.True(t, got.ProfileMatched)

	byKey := map[string]DeviceCapability{}
	for _, capability := range got.Capabilities {
		byKey[capability.Key] = capability
	}

	require.NotContains(t, byKey, "core.command.withdrawn", "a key that is not present was served")
	require.Contains(t, byKey, "core.feature.dialogs")
	require.Empty(t, byKey["core.command.beep"].Arguments)

	require.Equal(t, []DeviceArgument{{
		Name: "celsius", Key: "core.command.set_nozzle_temperature#celsius", Type: "integer", Required: true,
		Minimum: new(0.0), Maximum: new(300.0), Unit: "celsius",
	}}, byKey["core.command.set_nozzle_temperature"].Arguments)

	require.Equal(t, []DeviceArgument{{
		Name: "distance", Key: "core.command.move_z#distance", Type: "number", Required: true,
		Minimum: new(-50.0), Maximum: new(50.0), Unit: "mm",
	}}, byKey["core.command.move_z"].Arguments, "a magnitude bound with no floor is a range on both sides of zero")

	require.Equal(t, []DeviceArgument{{
		Name: "path", Key: "core.command.start_print#path", Type: "string", Required: true, MaxLength: new(255),
	}}, byKey["core.command.start_print"].Arguments)

	require.Equal(t, []DeviceArgument{{
		Name: "axis", Key: "core.command.home#axis", Type: "string", Default: "XY", Enum: []string{"X", "XY"},
	}}, byKey["core.command.home"].Arguments)
}

// TestAFloorBelowTheMagnitudeBoundIsNarrowed holds the one case where the
// descriptor's two bounds overlap: a floor of -1000 under a magnitude of 300
// still refuses -301, so the range a client reads starts at -300.
func TestAFloorBelowTheMagnitudeBoundIsNarrowed(t *testing.T) {
	t.Parallel()

	argument := deviceArgumentOf("core.command.move#x", "x", connectorapi.Value{
		Flag: true, Kind: connectorapi.KindNumber, Min: -1000, HasMin: true, Limit: 300, HasLimit: true,
	})

	require.Equal(t, new(-300.0), argument.Minimum)
}

// TestEveryServedRangeIsOneTheHostAccepts is the claim the endpoint makes: a
// value inside the published range passes the check a command meets, and one
// just outside it is refused.
func TestEveryServedRangeIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	set := resolvedSet()

	for _, capability := range deviceCapabilitiesOf("p", DeviceCapabilitySet{Set: set}).Capabilities {
		for _, argument := range capability.Arguments {
			if argument.Minimum == nil || argument.Maximum == nil {
				continue
			}

			for value, accepted := range map[float64]bool{
				*argument.Minimum: true, *argument.Maximum: true,
				*argument.Minimum - 1: false, *argument.Maximum + 1: false,
			} {
				_, err := set.Validate(connectorapi.Command{
					Verb: connectorapi.Capability(capability.Key),
					Args: map[string]string{argument.Name: jsonNumber(t, value)},
				})
				require.Equalf(t, accepted, err == nil, "%s=%v: %v", argument.Key, value, err)
			}
		}
	}
}

func jsonNumber(t *testing.T, value float64) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	require.NoError(t, err)

	return string(encoded)
}

func TestTheCapabilitiesEndpointServesTheProjection(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{
		printer:  Device{ID: "printer-1"},
		resolved: DeviceCapabilitySet{Set: resolvedSet(), Epoch: 3},
	}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/devices/printer-1/capabilities", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body DeviceCapabilities
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, deviceCapabilitiesOf("printer-1", fleet.resolved), body)

	rec = call(t, handler, http.MethodGet, "/api/v1/devices/nobody/capabilities", "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestADeviceWithNoSessionServesAnEmptyList(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(deviceCapabilitiesOf("printer-1", DeviceCapabilitySet{}))
	require.NoError(t, err)

	require.JSONEq(t,
		`{"device_id":"printer-1","capability_epoch":0,"profile_matched":false,"capabilities":[]}`,
		string(encoded))
}
