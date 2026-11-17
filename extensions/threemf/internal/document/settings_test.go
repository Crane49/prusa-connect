package document

import (
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

func setupWith(name string, values map[string]string, profiles ...string) threemf.Setup {
	return threemf.Setup{
		MaterialProfiles: profiles,
		Settings:         []threemf.SettingsGroup{{Name: name, Values: values}},
	}
}

// PrusaSlicer 2 writes plain text, a list joined by semicolons and support as
// two switches.
func TestTheSummaryReadsPrusaSlicer2Text(t *testing.T) {
	t.Parallel()

	got := summariseSettings(setupWith("config", map[string]string{
		"layer_height":          "0.2",
		"first_layer_height":    "150%",
		"fill_density":          "15%",
		"fill_pattern":          "gyroid",
		"perimeters":            "3",
		"support_material":      "1",
		"support_material_auto": "0",
		"brim_width":            "4",
		"wipe_tower":            "1",
		"filament_type":         "PLA;PETG",
		"filament_colour":       "#ff8000;#000000",
		"extruder_colour":       ";#00FF00",
	}, "PLA profile", "PETG profile"))

	near(t, 0.2, got.LayerHeightMM, "got.LayerHeightMM")
	near(t, 0.3, got.FirstLayerHeightMM, "got.FirstLayerHeightMM")
	near(t, 15, *got.InfillPercent, "got.InfillPercent")
	same(t, "gyroid", got.InfillPattern)
	same(t, 3, got.Perimeters)
	same(t, SupportsEnforcersOnly, got.Supports)
	near(t, 4, *got.BrimWidthMM, "got.BrimWidthMM")
	same(t, true, *got.WipeTower, "got.WipeTower")
	same(t, []summaryFilament{
		{Type: "PLA", Colour: "#FF8000"},
		{Type: "PETG", Colour: "#00FF00"},
	}, got.Filaments)
}

// PrusaSlicer 3 writes JSON, with a percentage as an object.
func TestTheSummaryReadsPrusaSlicer3JSON(t *testing.T) {
	t.Parallel()

	got := summariseSettings(setupWith("print_settings", map[string]string{
		"layer_height":       "0.15",
		"first_layer_height": `{"is_percent":false,"value":0.2}`,
		"fill_density":       `{"is_percent":true,"value":20.0}`,
		"fill_pattern":       `"grid"`,
		"support_material":   `"everywhere"`,
		"wipe_tower":         "false",
		"filament_type":      `["PLA"]`,
	}))

	near(t, 0.15, got.LayerHeightMM, "got.LayerHeightMM")
	near(t, 0.2, got.FirstLayerHeightMM, "got.FirstLayerHeightMM")
	near(t, 20, *got.InfillPercent, "got.InfillPercent")
	same(t, "grid", got.InfillPattern)
	same(t, SupportsEverywhere, got.Supports)
	same(t, false, *got.WipeTower, "got.WipeTower")
	same(t, []summaryFilament{{Type: "PLA"}}, got.Filaments)
}

// OrcaSlicer names the same settings differently and turns support and the
// brim on and off with words of its own.
func TestTheSummaryReadsOrcaSlicerNames(t *testing.T) {
	t.Parallel()

	got := summariseSettings(setupWith("project_settings", map[string]string{
		"layer_height":                "\"0.2\"",
		"initial_layer_print_height":  "\"0.25\"",
		"sparse_infill_density":       "\"15%\"",
		"sparse_infill_pattern":       "\"crosshatch\"",
		"wall_loops":                  "\"2\"",
		"enable_support":              "\"1\"",
		"support_type":                "\"tree(auto)\"",
		"support_on_build_plate_only": "\"1\"",
		"brim_type":                   "\"no_brim\"",
		"brim_width":                  "\"5\"",
		"enable_prime_tower":          "\"0\"",
		"filament_type":               `["PLA","PETG"]`,
		"filament_colour":             `["#26A69AFF","not a colour"]`,
	}))

	near(t, 0.25, got.FirstLayerHeightMM, "got.FirstLayerHeightMM")
	same(t, "crosshatch", got.InfillPattern)
	same(t, 2, got.Perimeters)
	same(t, SupportsBuildPlateOnly, got.Supports)
	near(t, 0, *got.BrimWidthMM, "got.BrimWidthMM")
	same(t, false, *got.WipeTower, "got.WipeTower")
	same(t, []summaryFilament{{Type: "PLA", Colour: "#26A69A"}, {Type: "PETG"}}, got.Filaments)

	manual := summariseSettings(setupWith("project_settings", map[string]string{
		"enable_support": "\"1\"", "support_type": "\"normal(manual)\"", "brim_type": "\"auto_brim\"",
		"brim_width": "\"0\"",
	}))
	same(t, SupportsEnforcersOnly, manual.Supports)
	if manual.BrimWidthMM != nil {
		t.Fatalf("manual.BrimWidthMM: got %v, want nil", manual.BrimWidthMM)
	}
}

// A setup with no settings, which is what a file from any other program has,
// says nothing.
func TestTheSummaryOfNoSettingsIsEmpty(t *testing.T) {
	t.Parallel()

	got := summariseSettings(threemf.Setup{})

	near(t, 0, got.LayerHeightMM, "got.LayerHeightMM")
	if got.InfillPercent != nil {
		t.Fatalf("got.InfillPercent: got %v, want nil", got.InfillPercent)
	}
	if len(got.Supports) != 0 {
		t.Fatalf("got.Supports: got %v, want none", got.Supports)
	}
	if got.WipeTower != nil {
		t.Fatalf("got.WipeTower: got %v, want nil", got.WipeTower)
	}
	if len(got.Filaments) != 0 {
		t.Fatalf("got.Filaments: got %v, want none", got.Filaments)
	}
}
