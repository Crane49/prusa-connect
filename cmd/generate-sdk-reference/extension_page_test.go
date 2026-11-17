package main

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// placesManifest is a module.yaml with an entry limited to the printer the
// server runs on, one limited to a file type, and one limited to neither.
const placesManifest = `
name: display
kind: asset
license: MIT
ui:
  entry: ui/index.js
  library: ">=0.1.0 <1.0.0"
  contributes:
    device_tab:
      - id: display
        title: Display
        devices:
          resident: true
      - id: log
        title: Log
    device_action:
      - id: wake
        title: Wake
        devices:
          resident: true
    file_tab:
      - id: plates
        title: Plates
        file_types: [.3mf]
`

// An extension page says where each entry of the ui block shows, with the
// files it is limited to and whether it is limited to the printer the server
// runs on (ADR-20261110-5np).
func TestAppPlacesSayWhatAnEntryIsLimitedTo(t *testing.T) {
	t.Parallel()

	declared, err := manifest.Parse("module.yaml", []byte(placesManifest), yaml.Unmarshal)
	if err != nil {
		t.Fatalf("the manifest was refused: %v", err)
	}

	got := appPlaces(declared.UI)
	want := []string{
		"**Display**: a tab of its own on each printer's page, but only on the printer this server runs on",
		"**Log**: a tab of its own on each printer's page",
		"**Wake**: an item in the action menu of each printer's page, but only on the printer this server runs on",
		"**Plates**: a tab of its own on a file's page, for `.3mf`",
	}

	for _, line := range want {
		if !slices.Contains(got, line) {
			t.Errorf("the places do not hold %q; they are %q", line, got)
		}
	}
}
