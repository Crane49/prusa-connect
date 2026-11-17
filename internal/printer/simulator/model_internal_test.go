package simulator

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A capability the wire has no spelling for stops the projection.
//
// The shipping profiles carry only commands and features, so this branch is
// unreachable from the embedded data and would rot untested. It is the branch
// that matters most on the day a fourth namespace is added: projecting a key
// this package does not understand would make the simulator quietly answer for
// a printer nobody described, which is the one thing it must not do.
func TestACapabilityWithNoWireSpellingIsRefused(t *testing.T) {
	t.Parallel()

	_, err := catalogFor(profile.Registry{Schema: profile.CurrentSchema, Profiles: []profile.Profile{{
		Model:        "SIM",
		DeviceTypeID: "9.9.9",
		Firmware:     profile.Range{From: "6.4.0"},
		Capabilities: connectorapi.Set{"core.telemetry.slot": {Flag: true}},
	}}})
	if err == nil {
		t.Fatal("the projection accepted a capability that is neither a command nor a feature")
	}

	if !strings.Contains(err.Error(), "core.telemetry.slot") {
		t.Errorf("the refusal does not name the key it could not project: %v", err)
	}
}

// A model with more than one firmware band is the band the file leads with.
//
// The simulator cannot be a firmware version (earlier research, "the firmware axis
// is flat"), so a second band is not a second model it could offer: it is two
// answers to one name. Taking the first in file order is what
// profile.Registry.Models does, and it is what stops map order deciding which
// printer a test gets.
func TestAModelWithTwoFirmwareBandsIsTheFirstInFileOrder(t *testing.T) {
	t.Parallel()

	loaded, err := catalogFor(profile.Registry{Schema: profile.CurrentSchema, Profiles: []profile.Profile{
		{
			Model:        "SIM",
			DeviceTypeID: "9.9.9",
			Firmware:     profile.Range{From: "6.4.0", To: "7.0.0"},
			Capabilities: connectorapi.Set{"core.command.beep": {Flag: true}},
		},
		{
			Model:        "SIM",
			DeviceTypeID: "9.9.9",
			Firmware:     profile.Range{From: "7.0.0"},
			Capabilities: connectorapi.Set{"core.command.reset": {Flag: true}},
		},
	}})
	if err != nil {
		t.Fatalf("project two bands of one model: %v", err)
	}

	if len(loaded.names) != 1 {
		t.Fatalf("the catalog lists %v, and two bands of one model are one model", loaded.names)
	}

	model := loaded.byName["sim"]

	if model.Firmware.To != "7.0.0" {
		t.Errorf("the catalog took the band [%s, %s), and the file leads with [6.4.0, 7.0.0)",
			model.Firmware.From, model.Firmware.To)
	}

	if _, ok := model.Command("BEEP"); !ok {
		t.Error("the model does not answer BEEP, which the band the file leads with declares")
	}
}
