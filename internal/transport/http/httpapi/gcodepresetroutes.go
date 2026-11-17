package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The preset paths. A preset is addressed by name alone: the kind is in the
// path segment, and a name is unique across all presets.
const (
	gcodePresetsPath = "/api/v1/gcode-presets"
	gcodePresetPath  = gcodePresetsPath + "/{name}"

	// runPresetPath sits under the printer's gcode path, because running a
	// preset sends a sequence to that printer, under that printer's send_gcode
	// grant, through the path a raw sequence takes.
	runPresetPath = "/api/v1/devices/{device_id}/gcode/presets/{preset}"
)

type listGCodePresetsOutput struct {
	ETag string `header:"ETag" doc:"The version of the configuration this listing was read from."`
	Body struct {
		Items []GCodePreset `json:"items"`
	}
}

// GCodePresetRef is the preset a path names. It is exported because Huma binds
// parameters by reflection and cannot write into an unexported embedded field.
type GCodePresetRef struct {
	Name string `path:"name" maxLength:"63" doc:"The operator-chosen name." example:"preheat-pla"`
}

type getGCodePresetInput struct {
	GCodePresetRef
}

// getGCodePresetOutput is separate from writeGCodePresetOutput for the reason
// getResourceOutput is separate from resourceOutput: Huma derives a response's
// headers from the struct a handler returns, and a read sends no Location.
type getGCodePresetOutput struct {
	Status int
	ETag   string `header:"ETag" doc:"The version to quote in If-Match on the next write."`
	Body   GCodePreset
}

type writeGCodePresetOutput struct {
	Status   int
	ETag     string `header:"ETag"     doc:"The version to quote in If-Match on the next write. Absent on a dry run, which wrote nothing to quote."`
	Location string `header:"Location" doc:"Where the created preset lives. Sent only with 201."`
	Body     GCodePreset
}

// gcodePresetBody is what a PUT accepts: the declaration, plus the read-only
// members a GET returns, so the body of a read can be sent straight back.
type gcodePresetBody struct {
	GCodePresetSpec

	Name    string `json:"name,omitempty"    doc:"Ignored unless it disagrees with the path, which is refused."`
	Version string `json:"version,omitempty" doc:"Read-only. The precondition rides in If-Match, not here."`
}

type writeGCodePresetInput struct {
	GCodePresetRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted; a weak validator is not. * alone does not fence a write."`
	IfNoneMatch string `header:"If-None-Match" doc:"Send * to create only, failing if the preset already exists. A tag list is compared weakly and alone does not fence a write."`
	DryRun      bool   `                       doc:"Validate and report the result without writing the file."                                                                     query:"dry_run" example:"true"`

	Body gcodePresetBody
}

type deleteGCodePresetInput struct {
	GCodePresetRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted. * alone does not fence a delete."`
	IfNoneMatch string `header:"If-None-Match" doc:"A tag list, compared weakly: the delete applies only if none is current. It alone does not fence a delete."`
	DryRun      bool   `                       doc:"Validate without writing the file."                                                                         query:"dry_run" example:"true"`
}

type deleteGCodePresetOutput struct {
	Status int
}

// runGCodePresetInput names the printer and the preset. A run takes no body,
// because a preset a caller can change at the call is not the preset that was
// reviewed.
type runGCodePresetInput struct {
	DeviceRef

	Preset string `path:"preset" maxLength:"63" doc:"The preset to run, by name." example:"preheat-pla"`
}

// presetRunErrors adds the model refusal to what the raw gcode surface already
// answers with. 422 is a preset pinned to models the target printer is not:
// the printer and the preset both exist, and this server will not run the
// pair.
var presetRunErrors = append([]int{http.StatusUnprocessableEntity}, gcodeErrors...)

// registerGCodePresets publishes the stored gcode sequences: the listing, one
// preset, and the two writes.
func registerGCodePresets(api huma.API, store GCodePresetStore) {
	registerListGCodePresets(api, store)
	registerGetGCodePreset(api, store)
	registerPutGCodePreset(api, store)
	registerDeleteGCodePreset(api, store)
}

func registerListGCodePresets(api huma.API, store GCodePresetStore) {
	register(api, huma.Operation{
		OperationID: "list-gcode-presets",
		Method:      http.MethodGet,
		Path:        gcodePresetsPath,
		Summary:     "Stored gcode presets",
		Description: "Every preset the caller may read, which takes view_resource on the gcode-preset " +
			"kind. A preset is a named, ordered gcode sequence and the models it was written for. " +
			"The ETag is the version of the configuration overlay the presets are stored in, and it " +
			"moves whenever any preset or any resource instance is written.",
		Tags:   []string{"devices"},
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, _ *struct{}) (*listGCodePresetsOutput, error) {
		listing, err := store.ListPresets(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &listGCodePresetsOutput{}
		out.Body.Items = listing.Items

		if listing.Version != "" {
			if out.ETag, err = validatedETag(listing.Version); err != nil {
				return nil, err
			}
		}

		return out, nil
	})
}

func registerGetGCodePreset(api huma.API, store GCodePresetStore) {
	register(api, huma.Operation{
		OperationID: "get-gcode-preset",
		Method:      http.MethodGet,
		Path:        gcodePresetPath,
		Summary:     "One stored gcode preset",
		Description: "The sequence, the models it is pinned to and the token a write must quote. " +
			"A caller who may not read presets and a name nothing carries are the same refusal.",
		Tags:   []string{"devices"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *getGCodePresetInput) (*getGCodePresetOutput, error) {
		preset, err := store.GetPreset(ctx, principalFrom(ctx), in.Name)
		if err != nil {
			return nil, err
		}

		etag, err := validatedETag(preset.Version)
		if err != nil {
			return nil, err
		}

		return &getGCodePresetOutput{Status: http.StatusOK, ETag: etag, Body: preset}, nil
	})
}

func registerPutGCodePreset(api huma.API, store GCodePresetStore) {
	register(api, huma.Operation{
		OperationID: "put-gcode-preset",
		Method:      http.MethodPut,
		Path:        gcodePresetPath,
		Summary:     "Declare a gcode preset",
		Description: "Writes the preset into the machine-owned configuration overlay. It takes " +
			"configure_resource on the gcode-preset kind, which is not what running one takes: a " +
			"caller trusted with a maintenance macro is not thereby trusted to rewrite what it " +
			"sends to a machine.\n\n" +
			"An If-Match tag list or If-None-Match: * is required, because the overlay has another " +
			"writer. A write that creates answers 201 with a Location; one that replaces answers 200.\n\n" +
			"models pins the preset to the printers it was written for, as those printers report " +
			"themselves, compared without regard to case. A preset naming no model runs on any " +
			"printer; a preset naming models is refused on anything else, because gcode that is safe " +
			"on one machine is not thereby safe on another.\n\n" +
			"What a printer will accept is not checked here. The machine a preset runs on may not " +
			"have arrived yet, so every step is checked against that printer's capability set when " +
			"the preset is run.",
		Tags:          []string{"devices"},
		DefaultStatus: http.StatusOK,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *writeGCodePresetInput) (*writeGCodePresetOutput, error) {
		if err := in.Body.checkName(in.Name); err != nil {
			return nil, err
		}

		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		applied, err := store.ApplyPreset(ctx, principalFrom(ctx), in.Name, in.Body.GCodePresetSpec, pre)
		if err != nil {
			return nil, err
		}

		return presetOutput(applied, pre.DryRun)
	})

	documentCreatedResponse(api, gcodePresetPath)
}

func registerDeleteGCodePreset(api huma.API, store GCodePresetStore) {
	register(api, huma.Operation{
		OperationID:   "delete-gcode-preset",
		Method:        http.MethodDelete,
		Path:          gcodePresetPath,
		Summary:       "Remove a gcode preset",
		Description:   "Removes the preset from the machine-owned configuration overlay.",
		Tags:          []string{"devices"},
		DefaultStatus: http.StatusNoContent,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *deleteGCodePresetInput) (*deleteGCodePresetOutput, error) {
		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		if err = store.DeletePreset(ctx, principalFrom(ctx), in.Name, pre); err != nil {
			return nil, err
		}

		return &deleteGCodePresetOutput{Status: http.StatusNoContent}, nil
	})
}

// registerRunGCodePreset mounts the one operation that reads a preset without
// a grant on the preset kind. It is registered over the same fleet the raw
// gcode operation is, so a preset cannot reach a verb a raw sequence could not.
func registerRunGCodePreset(api huma.API, fleet gcodeFleet, store GCodePresetStore) {
	register(api, huma.Operation{
		OperationID: "run-printer-gcode-preset",
		Method:      http.MethodPost,
		Path:        runPresetPath,
		Summary:     "Run a stored gcode preset on a printer",
		Description: "Expands the preset and sends its steps to the printer, in order, reporting what " +
			"became of each. It takes send_gcode on the printer and nothing at all on the preset: a " +
			"caller may be trusted to run a reviewed macro without being trusted to edit it.\n\n" +
			"A preset pinned to models the printer is not is refused with 422 and nothing is sent. " +
			"The comparison ignores case and uses the model the printer reported in its handshake.\n\n" +
			"The run is ordered. It is not atomic and it is not exclusive: it stops at the first step " +
			"the server or the printer did not accept, nothing undoes the steps that already ran, and " +
			"another caller's command may land between two steps. The status is 200 whenever the run " +
			"was started, and a step after the one that stopped it is reported as not_attempted.\n\n" +
			"Every step is validated against the printer that is there now. A preset stored before " +
			"this printer arrived was never checked against it.",
		Tags:   []string{"devices"},
		Errors: presetRunErrors,
	}, func(ctx context.Context, in *runGCodePresetInput) (*sendGCodeOutput, error) {
		outcome, err := fleet.runPreset(ctx, principalFrom(ctx), in.DeviceID, in.Preset, store)
		if err != nil {
			return nil, err
		}

		return &sendGCodeOutput{Body: outcome}, nil
	})
}

// runPreset expands one stored preset against one printer.
//
// The order of the checks stops this endpoint from leaking. The printer grant
// is checked first, so a caller holding nothing never learns whether a preset
// name is taken. The printer is resolved second, so a printer the caller
// cannot see answers as absent before a preset is read. The preset is read
// last, and no grant on the preset kind is consulted: running a preset takes
// send_gcode on the printer and nothing on the preset.
func (f gcodeFleet) runPreset(
	ctx context.Context, principal string, id string, name string, store GCodePresetStore,
) (PrinterGCodeOutcome, error) {
	// A preset is a stored sequence of raw gcode, so the verb it reaches for is
	// the plain one. A forced preset does not exist: force is a flag on a run.
	if err := f.authorize(ctx, principal, id, authz.RawGCodeVerbs()[0]); err != nil {
		return PrinterGCodeOutcome{}, err
	}

	printer, err := f.DeviceFleet.Get(ctx, principal, id)
	if err != nil {
		return PrinterGCodeOutcome{}, err
	}

	preset, err := store.LookupPreset(ctx, name)
	if err != nil {
		return PrinterGCodeOutcome{}, err
	}

	if err = presetRunsOn(preset, printer.Model); err != nil {
		return PrinterGCodeOutcome{}, err
	}

	return f.run(ctx, principal, id, PrinterGCodeRequest{Lines: preset.Steps, Force: preset.Force})
}

// presetRunsOn checks the models a preset is pinned to. A preset naming no
// model runs on any printer. A preset naming models is refused on a printer
// that is not one of them. It is a refusal and not a warning, because gcode
// written for one machine can damage another, and a warning would arrive
// after the gcode ran.
//
// The refusal carries the printer's model and the preset's models as
// parameters, so a client does not have to parse a sentence.
func presetRunsOn(preset GCodePreset, model string) error {
	if len(preset.Models) == 0 {
		return nil
	}

	for _, pinned := range preset.Models {
		if strings.EqualFold(strings.TrimSpace(pinned), strings.TrimSpace(model)) {
			return nil
		}
	}

	return fault.PrinterPresetModelMismatch.
		New("this preset names printer models and this printer is not one of them").
		With(
			fault.StringParam("printer_model", model),
			fault.StringParam("preset_models", strings.Join(preset.Models, ", ")),
		)
}

// presetOutput frames what the store wrote. A dry run carries no ETag and no
// version, for the reason applyOutput drops them: it wrote nothing, so a token
// handed back would name a state that never existed.
func presetOutput(applied GCodePresetResult, dryRun bool) (*writeGCodePresetOutput, error) {
	if dryRun {
		applied.Preset.Version = ""

		return &writeGCodePresetOutput{Status: http.StatusOK, Body: applied.Preset}, nil
	}

	etag, err := validatedETag(applied.Preset.Version)
	if err != nil {
		return nil, err
	}

	out := &writeGCodePresetOutput{Status: http.StatusOK, ETag: etag, Body: applied.Preset}
	if applied.Created {
		out.Status = http.StatusCreated
		out.Location = gcodePresetsPath + "/" + applied.Preset.Name
	}

	return out, nil
}

// checkName refuses a body that names a different preset from the path, for the
// reason resourceSpecBody.checkNames does: the body is echoed back from a read
// and a disagreement is a client bug worth reporting.
func (b gcodePresetBody) checkName(name string) error {
	if b.Name != "" && b.Name != name {
		return httpfault.RequestInvalid("body.name",
			"expected "+name, "the body names a different preset from the path")
	}

	return nil
}
