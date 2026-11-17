package assign

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"

	"github.com/prusa3d/materials/internal/stock"
)

// The assignment holds the material values of a tool, and Reported holds
// what the printer last sent of the material package instance on it.

// lastReport is the JSON of a reported instance, as the follower stores it.
func lastReport(t *testing.T, r connectorapi.ReportedMaterial) json.RawMessage {
	t.Helper()

	out, err := stock.EncodeReport(r)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// report is a tool reporting abbreviation, with the rest of the instance in r.
func report(abbreviation string, r *connectorapi.ReportedMaterial) connectorapi.Tool {
	return connectorapi.Tool{Material: &abbreviation, Reported: r}
}

func TestMerge(t *testing.T) {
	const (
		instance = "784e29b5-175f-52e6-93ec-abb14fe539fa"
		pkg      = "5d73a040-77c9-55ee-95bc-f4ef0fc2efda"
		mat      = "2e6e999d-09cd-5acc-8240-e12ecbc1e07d"
		nfcSlot  = "9c1d0e2f-1a2b-4c3d-8e5f-0000000000a0"
		nfcPkg   = "5f02a1b3-c4d5-46e7-88f9-0a1b2c3d4e5f"
	)

	spool := int64(7)
	weight := 1000.0

	// assigned: a person put a spool on the tool, and
	// the printer reported its instance before.
	assigned := &stock.Assignment{
		By: stock.ByPerson, MaterialUUID: mat, MaterialName: "PLA Blend Viva La Bronze",
		Abbreviation: "PLA", ColorRGBA: "#b58c39", SpoolID: &spool,
		Reported: lastReport(t, connectorapi.ReportedMaterial{
			InstanceUUID: instance, PackageUUID: pkg, MaterialUUID: mat, ColorRGBA: "#b58c39",
			Properties: map[string]float64{"max_print_temperature": 215},
		}),
	}
	// enriched: a person assigned a catalogue material, and no report of
	// its instance is stored.
	enriched := func(abbreviation string) *stock.Assignment {
		return &stock.Assignment{By: stock.ByPerson, MaterialUUID: mat, Abbreviation: abbreviation}
	}
	// slot is the first MMU slot, as the printer reported it.
	slotReport := connectorapi.ReportedMaterial{
		InstanceUUID: nfcSlot, PackageUUID: nfcPkg, Type: "PLA", FullWeight: &weight,
	}
	slot := &stock.Assignment{
		By: stock.ByPrinter, Abbreviation: "PLA", Type: "PLA", Reported: lastReport(t, slotReport),
	}
	// printerPETG is a material the printer reported, with its profile.
	profile := map[string]float64{
		"min_print_temperature": 230, "max_print_temperature": 230, "chamber_temperature": 30,
	}
	printerPETG := &stock.Assignment{
		By: stock.ByPrinter, Abbreviation: "PETG", ColorRGBA: "#ffffff",
		Reported: lastReport(t, connectorapi.ReportedMaterial{ColorRGBA: "#ffffff", Properties: profile}),
	}

	keep, replace, drop := Outcome{Keep: true}, Outcome{Replace: true}, Outcome{Drop: true}

	for _, c := range []struct {
		name     string
		stored   *stock.Assignment
		reported connectorapi.Tool
		want     Outcome
	}{
		{"nfc same values", assigned, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: instance, PackageUUID: pkg, ColorRGBA: "#b58c39",
			Properties: map[string]float64{"max_print_temperature": 215},
		}), keep},
		{"nfc new value", assigned, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: instance, PackageUUID: pkg, FullWeight: new(1050.0),
			Properties: map[string]float64{"max_print_temperature": 215, "min_print_temperature": 210},
		}), keep},
		{"nfc another package", assigned, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: instance, PackageUUID: "6e84b151-88da-46ff-a6fd-05f010d3f0eb",
		}), replace},
		{"profile over a person's material", assigned, report("PLA", &connectorapi.ReportedMaterial{
			ColorRGBA: "#ffffff", Properties: map[string]float64{"max_print_temperature": 230},
		}), keep},
		{"nfc another colour", assigned, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: instance, ColorRGBA: "#b58c40",
		}), replace},
		{"nfc over a material without instance", &stock.Assignment{
			By: stock.ByPerson, MaterialName: "Woodfill Birch White", Abbreviation: "PLA", ColorRGBA: "#b58c39",
		}, report("PLA", &connectorapi.ReportedMaterial{InstanceUUID: instance, ColorRGBA: "#b58c39"}), keep},
		{"another colour over the printer's own", printerPETG, report("PETG", &connectorapi.ReportedMaterial{
			ColorRGBA: "#000000", Properties: profile,
		}), replace},
		{"same profile over the printer's own", printerPETG, report("PETG", &connectorapi.ReportedMaterial{
			ColorRGBA: "#ffffff", Properties: profile,
		}), keep},
		{"another profile over the printer's own", printerPETG, report("PETG", &connectorapi.ReportedMaterial{
			ColorRGBA: "#ffffff", Properties: map[string]float64{"min_print_temperature": 240},
		}), replace},
		{"nfc another full weight", slot, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: nfcSlot, FullWeight: new(750.0),
		}), replace},
		{"custom name", &stock.Assignment{
			By: stock.ByPerson, MaterialName: "Woodfill Birch White", Abbreviation: "CUSTOM", ColorRGBA: "#d9cdadff",
		}, report("CUSTOM", &connectorapi.ReportedMaterial{ColorRGBA: "#ffa500"}), keep},
		{"another material", assigned, report("PETG", nil), replace},
		{"nfc another instance", slot, report("PLA", &connectorapi.ReportedMaterial{
			InstanceUUID: "11111111-1a2b-4c3d-8e5f-0000000000a0",
		}), replace},
		{"same report again", slot, report("PLA", &slotReport), keep},
		{"material uuid not reported", &stock.Assignment{
			By: stock.ByPrinter, MaterialUUID: mat, Abbreviation: "PLA", Type: "PLA", Reported: lastReport(t, slotReport),
		}, report("PLA", &slotReport), keep},
		{"name added to a flat report", &stock.Assignment{
			By: stock.ByPerson, MaterialName: "Prusament PETG", Abbreviation: "PETG",
		}, report("PETG", nil), keep},

		{"same material", enriched("PLA"), report("PLA", nil), keep},
		{"material changes", enriched("PLA"), report("PETG", nil), replace},
		{"nothing loaded", enriched("PLA"), report(connectorapi.NoMaterial, nil), drop},
		{"material not reported", enriched("PLA"), connectorapi.Tool{}, keep},
		{"new tool", nil, report("PETG", nil), replace},
		{"nothing loaded on an unknown tool", nil, report(connectorapi.NoMaterial, nil), drop},

		{"flat same material", enriched("PLA"), report("PLA", nil), keep},
		{"flat another material", enriched("PLA"), report("PETG", nil), replace},
		{"type only", &stock.Assignment{By: stock.ByPerson, Type: "PLA", ColorRGBA: "#8601afff"},
			report("PLA", nil), keep},
		{"alias TPU", enriched("TPU"), report("FLEX", nil), keep},
		{"custom name assigned", enriched("CUSTOM1"), report("CUSTOM1", nil), keep},
		{"alias TPE", enriched("TPE"), report("FLEX", nil), keep},
		{"alias PCCF", enriched("PCCF"), report("PC", nil), keep},
		{"alias in the catalogue's case", enriched("Wood"), report("PLA", nil), keep},
		{"type matches", &stock.Assignment{By: stock.ByPerson, MaterialUUID: mat, Type: "PLA", Abbreviation: "PLA+"},
			report("PLA", nil), keep},
		{"alias does not cover", enriched("TPU"), report("PLA", nil), replace},

		// Merge does not look the spool up, so a tag read over a person's
		// spool with no report stored gives the printer's spool.
		{"nfc over a person's spool", &stock.Assignment{By: stock.ByPerson, Abbreviation: "PLA", SpoolID: &spool},
			report("PLA", &connectorapi.ReportedMaterial{InstanceUUID: instance}), replace},
		// A stored report that does not decode is no report.
		{"unreadable last report", &stock.Assignment{
			By: stock.ByPrinter, Abbreviation: "PLA", Reported: json.RawMessage(`{`),
		}, report("PLA", nil), keep},
	} {
		if got := Merge(c.stored, c.reported, nil); got != c.want {
			t.Errorf("%s: Merge = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestMergeTakesASpoolTheTagNames(t *testing.T) {
	const instance = "784e29b5-175f-52e6-93ec-abb14fe539fa"

	onTool, other := int64(7), int64(8)
	tag := report("PLA", &connectorapi.ReportedMaterial{InstanceUUID: instance})
	person := func(spool *int64) *stock.Assignment {
		return &stock.Assignment{By: stock.ByPerson, Abbreviation: "PLA", SpoolID: spool}
	}

	for _, c := range []struct {
		name   string
		stored *stock.Assignment
		tagged *int64
		want   Outcome
	}{
		{"the tag names the spool on the tool", person(&onTool), &onTool, Outcome{Keep: true}},
		{"the tag names another spool", person(&onTool), &other, Outcome{Replace: true}},
		{"a known tag on a tool with no spool", person(nil), &other, Outcome{Replace: true}},
		{"an unknown tag on a tool with no spool", person(nil), nil, Outcome{Keep: true}},
		{"a known tag on a bare tool", nil, &other, Outcome{Replace: true}},
	} {
		if got := Merge(c.stored, tag, c.tagged); got != c.want {
			t.Errorf("%s: Merge = %+v, want %+v", c.name, got, c.want)
		}
	}
}
