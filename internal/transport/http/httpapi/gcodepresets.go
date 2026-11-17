package httpapi

import (
	"context"
)

// GCodePresetStore is the stored gcode presets: named sequences an operator
// runs against a printer, kept in the same machine-owned overlay the resource
// instances are kept in and fenced by the same version token. It is declared
// here because this is where it is consumed.
//
// Four of the five methods take the caller and answer what that caller may see
// or change. LookupPreset takes no caller, because running a preset takes
// send_gcode on the printer and nothing on the preset kind. The run handler
// authorizes the printer before it calls LookupPreset, so a caller who holds
// nothing on the printer never learns whether a preset name is taken.
type GCodePresetStore interface {
	// ListPresets returns every preset the caller may read. A caller holding no
	// view_resource on the preset kind is answered an empty listing and not a
	// refusal. The resource listing does the same for an instance the caller
	// holds nothing on.
	ListPresets(ctx context.Context, principal string) (GCodePresetListing, error)

	// GetPreset returns one preset. A caller who may not read presets and a
	// name nothing carries are the same refusal, in the same bytes.
	GetPreset(ctx context.Context, principal string, name string) (GCodePreset, error)

	// ApplyPreset writes the preset into the overlay. It validates before it
	// writes: a rejected spec leaves the file untouched.
	ApplyPreset(
		ctx context.Context,
		principal string,
		name string,
		desired GCodePresetSpec,
		pre Precondition,
	) (GCodePresetResult, error)

	// DeletePreset removes one from the overlay.
	DeletePreset(ctx context.Context, principal string, name string, pre Precondition) error

	// LookupPreset reads a preset for the run path, with no grant on the preset
	// kind consulted. That is intended, for the reason the type comment gives,
	// and the caller of it authorizes the printer first.
	LookupPreset(ctx context.Context, name string) (GCodePreset, error)
}

// GCodePresetSpec is what an author declares: the ordered commands, the models
// the sequence was written for, and how it is sent.
type GCodePresetSpec struct {
	// Summary is what the preset is for, shown beside its name.
	Summary string `json:"summary,omitempty" maxLength:"256" doc:"What this sequence does, in one line."`

	// Models pins the preset to the printers it was written for. Empty runs on
	// any model; a non-empty list runs only on a printer that reported one of
	// these, compared without regard to case.
	Models []string `json:"models,omitempty" maxItems:"16" doc:"The printer models this sequence was written for, as the printer reports them. Empty runs on any model."`

	// Steps are the commands, in order, one gcode command each.
	Steps []string `json:"steps" maxItems:"64" minItems:"1" doc:"The commands, in order, one gcode command each."`

	// Force sends every step out of band, ahead of the print queue. An
	// emergency sequence needs it and a maintenance macro does not.
	Force bool `json:"force,omitempty" doc:"Run every step out of band, ahead of the print queue."`
}

// GCodePreset is one stored preset as the API returns it.
type GCodePreset struct {
	GCodePresetSpec

	// Name is the operator-chosen name, unique across presets. It is the path,
	// and it is in the body too so a listing is usable without reparsing links.
	Name string `json:"name" doc:"The operator-chosen name."`

	// Version is the fencing token, which covers the whole overlay rather than
	// this preset alone: a write rewrites that file whole, so a write to any
	// preset or any resource instance moves it.
	Version string `json:"version" doc:"Fencing token, covering the whole overlay. Quote the ETag in If-Match on a write."`
}

// GCodePresetListing is the collection as one artefact, with the token every
// entry under it carries.
type GCodePresetListing struct {
	Items   []GCodePreset
	Version string
}

// GCodePresetResult is what a write produced. Created separates the PUT that
// brought a preset into existence, which answers 201 with a Location, from the
// one that replaced it.
type GCodePresetResult struct {
	Preset  GCodePreset
	Created bool
}
