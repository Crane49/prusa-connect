package manifest_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// devices is the selector of ADR-20261110-5np: a device slot entry that sets
// devices: {resident: true} is shown only for the device the server runs on.

func TestADeviceSlotEntryTakesADevicesSelector(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
	  "contributes":{
	    "device_tab":[{"id":"display","title":"Display","devices":{"resident":true}},{"id":"log","title":"Log"}],
	    "device_card":[{"id":"line","title":"Line","devices":{"resident":true}}],
	    "device_section":[{"id":"panel","title":"Panel"}],
	    "device_action":[{"id":"wake","title":"Wake","devices":{"resident":true}}]}}`)), decode)
	if err != nil {
		t.Fatalf("devices on a device slot was refused: %v", err)
	}

	sections := held.UI.Contributes.Sections
	if tabs := sections[manifest.SlotDeviceTab]; len(tabs) != 2 || !tabs[0].Devices.Resident ||
		!tabs[1].Devices.IsZero() {
		t.Errorf("the device tabs read as %+v", tabs)
	}

	if cards := sections[manifest.SlotDeviceCard]; len(cards) != 1 || !cards[0].Devices.Resident {
		t.Errorf("the device card line read as %+v", cards)
	}

	if panels := sections[manifest.SlotDeviceSection]; len(panels) != 1 || !panels[0].Devices.IsZero() {
		t.Errorf("an entry with no devices read as %+v", panels)
	}

	if actions := held.UI.Contributes.Actions[manifest.SlotDeviceAction]; len(actions) != 1 ||
		!actions[0].Devices.Resident {
		t.Errorf("the device action read as %+v", actions)
	}
}

// The block with no value counts as a missing block, as it does for every
// block in a manifest (ADR-20261110-5np rule 5), so the entry is shown for
// every device.
func TestAnEmptyDevicesIsAMissingKey(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
	  "contributes":{
	    "device_tab":[{"id":"display","title":"Display","devices":null}],
	    "device_action":[{"id":"wake","title":"Wake","devices":null}]}}`)), decode)
	if err != nil {
		t.Fatalf("devices with no value was refused: %v", err)
	}

	if tabs := held.UI.Contributes.Sections[manifest.SlotDeviceTab]; len(tabs) != 1 || !tabs[0].Devices.IsZero() {
		t.Errorf("the device tab read as %+v", tabs)
	}

	if actions := held.UI.Contributes.Actions[manifest.SlotDeviceAction]; len(actions) != 1 ||
		!actions[0].Devices.IsZero() {
		t.Errorf("the device action read as %+v", actions)
	}
}

func TestOnlyADeviceSlotEntryTakesADevicesSelector(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ contributes, refuses string }{
		"a file action": {
			`{"file_action":[{"id":"x","title":"X","devices":{"resident":true}}]}`, "carries devices",
		},
		"a print section": {
			`{"print_section":[{"id":"x","title":"X","devices":{"resident":true}}]}`, "carries devices",
		},
		"a settings pane": {
			`{"settings":[{"id":"x","title":"X","devices":{"resident":true}}]}`, "carries devices",
		},
		"the earlier boolean key": {
			`{"device_tab":[{"id":"x","title":"X","resident_only":true}]}`, "carries resident_only",
		},
		"not a mapping": {
			`{"device_tab":[{"id":"x","title":"X","devices":true}]}`, "devices is true or false, expected a mapping",
		},
		"a list": {
			`{"device_tab":[{"id":"x","title":"X","devices":["resident"]}]}`, "expected a mapping",
		},
		"no condition": {
			`{"device_tab":[{"id":"x","title":"X","devices":{}}]}`, "devices names no condition",
		},
		"resident with no value": {
			`{"device_tab":[{"id":"x","title":"X","devices":{"resident":null}}]}`, "devices names no condition",
		},
		"resident false": {
			`{"device_action":[{"id":"x","title":"X","devices":{"resident":false}}]}`,
			"devices.resident is false, and only true is read",
		},
		"resident not a truth": {
			`{"device_tab":[{"id":"x","title":"X","devices":{"resident":"yes"}}]}`, "expected true or false",
		},
		"a key this version does not read": {
			`{"device_card":[{"id":"x","title":"X","devices":{"resident":true,"kind":"printer"}}]}`,
			"devices carries kind",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(withUI(`{"entry":"ui/index.js","library":">=0.0.1",
			  "contributes":`+tc.contributes+`}`)), decode)
			if err == nil || !strings.Contains(err.Error(), tc.refuses) {
				t.Fatalf("the refusal is %v and does not name %q", err, tc.refuses)
			}
		})
	}
}
