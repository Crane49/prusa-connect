package assign

import (
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"

	"github.com/prusa3d/materials/internal/stock"
)

// Outcome is what a printer's report of a tool does to the assignment stored
// on it. Exactly one field is true.
type Outcome struct {
	Keep    bool // the stored assignment stands, maybe with Reported refreshed
	Replace bool // the printer's material replaces it (By "printer", spool matched by UUID if known)
	Drop    bool // nothing loaded on a reporting tool: the assignment goes
}

// Merge decides what the report of one tool does to the assignment stored on
// it. It looks nothing up. stored is nil when the tool has no assignment. stored.Reported is what
// the printer last sent for the tool, as stock.EncodeReport writes it, and
// holds the instance values the assignment has no field for.
//
// The first rule that applies decides:
//   - a tool whose material is not reported keeps its assignment;
//   - a tool reported empty drops it;
//   - a tool with no assignment, or one the reported material does not
//     match, takes the printer's material;
//   - a report that is another instance than the stored one replaces it;
//   - anything else keeps it. The reported values then fill only what the
//     assignment lacks.
//
// tagged is the spool Materials holds under the instance uuid the tool
// reports, nil for none. It overrules the rules above: a tag read off the
// spool already on the tool is that spool, and a known tag on a tool that
// holds no spool links its spool, which names the material.
func Merge(stored *stock.Assignment, reported connectorapi.Tool, tagged *int64) Outcome {
	out := merge(stored, reported)
	if tagged == nil || stored == nil {
		return out
	}

	switch {
	case stored.SpoolID == nil && out.Keep:
		return Outcome{Replace: true}
	case stored.SpoolID != nil && *stored.SpoolID == *tagged:
		return Outcome{Keep: true}
	}

	return out
}

func merge(stored *stock.Assignment, reported connectorapi.Tool) Outcome {
	switch {
	case reported.Material == nil:
		return Outcome{Keep: true}
	case *reported.Material == connectorapi.NoMaterial:
		return Outcome{Drop: true}
	case stored == nil, !Matches(*reported.Material, Candidate{Abbreviation: stored.Abbreviation, Type: stored.Type}):
		return Outcome{Replace: true}
	case another(stored, *reported.Material, reported.Reported):
		return Outcome{Replace: true}
	}

	return Outcome{Keep: true}
}

// another is true when the report is another instance than the stored one: a
// value both have differs. A value only one of them has is no difference.
//
// A report without an instance uuid comes from a printer's profile. Over an
// assignment a person made, only the values outside the material count, and
// the person's material values stay. A report with the uuid comes from an NFC
// tag, and every value counts. So does every value of an assignment the
// printer made.
func another(stored *stock.Assignment, abbreviation string, r *connectorapi.ReportedMaterial) bool {
	if r == nil {
		r = &connectorapi.ReportedMaterial{}
	}

	// A stored report that does not decode tells nothing.
	last, err := stock.DecodeReport(stored.Reported)
	if err != nil {
		last = connectorapi.ReportedMaterial{}
	}

	if differs(last.InstanceUUID, r.InstanceUUID) || differs(last.PackageUUID, r.PackageUUID) ||
		last.FullWeight != nil && r.FullWeight != nil && *last.FullWeight != *r.FullWeight {
		return true
	}

	if r.InstanceUUID == "" && stored.By != stock.ByPrinter {
		return false
	}

	// Merge does not look the spool up, so a tag read over a spool whose
	// uuid no report gave is taken as another. The follower finds the spool
	// by that uuid.
	if r.InstanceUUID != "" && last.InstanceUUID == "" && stored.SpoolID != nil {
		return true
	}

	return differs(stored.Abbreviation, abbreviation) || differs(stored.Type, r.Type) ||
		differs(stored.MaterialUUID, r.MaterialUUID) || differs(stored.ColorRGBA, r.ColorRGBA) ||
		propertiesDiffer(last.Properties, r.Properties)
}

func differs(stored, reported string) bool {
	return stored != "" && reported != "" && stored != reported
}

func propertiesDiffer(stored, reported map[string]float64) bool {
	for name, value := range reported {
		if old, ok := stored[name]; ok && old != value {
			return true
		}
	}

	return false
}
