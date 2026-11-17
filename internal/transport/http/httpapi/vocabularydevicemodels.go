package httpapi

// The device-model vocabulary: the Prusa models a capability profile covers, the
// models the drivers in this build declared, and how a page names and draws
// each one.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// deviceModelsOutput is the models this build can name.
type deviceModelsOutput struct {
	Body struct {
		Vocabulary string `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string `json:"version"    doc:"The content version of the set."`
		Format     string `json:"format"     doc:"The shape of an entry."`
		//nolint:lll // the huma tag is one field's whole contract and does not wrap.
		Items []deviceModelEntry `json:"items" doc:"One row per model this build can name: the Prusa models a capability profile covers and the models a driver in it declared, ordered by the machine's own spelling. A declared model the server refused has no row; refused lists it."`
		//nolint:lll // the huma tag is one field's whole contract and does not wrap.
		Connectors []connectorEntry `json:"connectors" doc:"The dialects the rows above are reached through, and what each one needs from a person before a machine of that kind can arrive. It rides with the models because the two are read together: a row above names its dialect, and this says what that dialect is."`
		//nolint:lll // the huma tag is one field's whole contract and does not wrap.
		Refused []refusedModelEntry `json:"refused" nullable:"false" doc:"The models a driver in this build declared and the server left out, because the spelling names a Prusa machine or another driver declared it too. A refused model has no row in items. It is empty when every declared model was placed."`
	}
}

// refusedModelEntry is one declared model the host left out, as an operator or
// a test of a composed build reads it.
type refusedModelEntry struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Model string `json:"model" doc:"The spelling the driver declared."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Connector string `json:"connector" doc:"The bare name of the dialect that declared it, as the connector member of a model row names a dialect."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Qualified string `json:"qualified" doc:"The namespace-qualified name of the dialect that declared it, as the qualified member of a connectors entry carries it. It tells apart two modules registering one bare name."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	CollidesWith string `json:"collides_with" doc:"What already answers to the spelling: a Prusa model, the device type code of one, or the other drivers that declared it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Remedy string `json:"remedy" doc:"What the person who composed the build does about the refusal."`
}

// deviceModelEntry is one model a page renders a device row under.
//
// The machine's own spelling is the key and everything else is about showing
// it: a device record carries that spelling in `model`, and a page joins this
// row onto it to find a name a person reads and a drawing to put beside it.
type deviceModelEntry struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Model string `json:"model" doc:"The machine's own spelling, exactly as a device reports it and exactly as the model member of a device record carries it. It is what the listing's model filter takes."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	DisplayName string `json:"display_name" doc:"What the machine is sold as. Put it on a card and never parse it."`
	Vendor      string `json:"vendor"       doc:"Who makes it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Kind string `json:"kind" doc:"What type of device a machine of this model is. It is printer on every row this build serves, from the same closed set the kind member of a device record is drawn from."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Icon string `json:"icon" doc:"The key to map to a drawing. Models that look alike share one, so a page ships a handful of pictures rather than one per model. Read it as an open value: a newer core may name a key this page has no drawing for, and the answer to that is a generic device."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Connector string `json:"connector" doc:"The dialect a machine of this model is reached through, by the dialect's bare name. The connectors member of this answer says what that dialect is and what it needs from a person. It is not the driver member of a device record, which names the instance of the dialect one machine actually arrived through."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	DeviceTypeID string `json:"device_type_id,omitempty" doc:"The code some firmware reports where a model name is expected. A client that meets one resolves it to this row rather than showing the number."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	ToolChanger bool `json:"tool_changer,omitempty" doc:"True when the numbered slots of a machine of this model are separate toolheads, as on the Prusa INDX tool changer and the XL, so a page draws a row per tool. It is absent for a model whose slots are feeders on one head, such as an MMU."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Artwork string `json:"artwork,omitempty" doc:"Where to fetch a drawing of the machine, present when the driver that reaches it declared one. It is meant for a model a page has no drawing of its own for, and a page that has one keeps its own."`
}

func registerDeviceModelVocabulary(api huma.API, models profile.Catalogue) {
	// Built once. The catalogue was placed at start and the registry holds what
	// the binary was built with, so every request would build the same answer,
	// and building it probes every driver and copies its declaration.
	items, connectors, refused := deviceModelEntries(models), connectorEntries(models), refusedModelEntries(models)

	register(api, huma.Operation{
		OperationID: "get-device-model-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyDeviceModelsPath,
		Summary:     "The device models this build can name",
		Description: "Every model this build knows, each with the machine's own spelling, the name " +
			"the product is sold under, who makes it, and a key to map to an icon. A device record " +
			"carries the machine's spelling in `model` and nothing else, so this is what turns " +
			"`CoreOneL_INDX` into a name a person reads and a picture to put beside it." +
			paragraphBreak +
			"The icon key names a family rather than a model. Machines in one family look alike, so " +
			"a page maps a handful of keys onto a handful of drawings instead of carrying one per " +
			"model, and a key it has no drawing for is answered with a generic device." +
			paragraphBreak +
			"It is the set this binary carries and not a catalogue of everything Prusa makes: the " +
			"Prusa models a capability profile covers and the models a driver composed into the " +
			"build declared, in one list, each under its own dialect. A machine reporting a model " +
			"with no row here still connects and is still listed: the capability profile is what a " +
			"model decides, and a device that matched none runs on the driver's baseline. The model " +
			"member of `list-devices` filters on the same spelling.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*deviceModelsOutput, error) {
		out := &deviceModelsOutput{}
		out.Body.Vocabulary = profile.VocabularyName
		out.Body.Version = profile.VocabularyVersion
		out.Body.Format = profile.VocabularyFormat
		out.Body.Items = items
		out.Body.Connectors = connectors
		out.Body.Refused = refused

		return out, nil
	})
}

// artworkOf is the path a row's drawing is served at, or empty for a row with
// none.
func artworkOf(doc profile.ModelDoc) string {
	if doc.Artwork.IsZero() {
		return ""
	}

	return artworkPathOf(doc.Model)
}

// deviceModelEntries is the projection onto the wire. The kind is written here
// and not in the profile package, because the closed set of kinds is the
// registry's and internal/printer may not reach it.
func deviceModelEntries(models profile.Catalogue) []deviceModelEntry {
	docs := models.Docs()

	out := make([]deviceModelEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, deviceModelEntry{
			Model:        doc.Model,
			DisplayName:  doc.DisplayName,
			Vendor:       doc.Vendor,
			Kind:         ref.KindPrinter.String(),
			Icon:         doc.Icon,
			Connector:    doc.Connector,
			DeviceTypeID: doc.DeviceTypeID,
			ToolChanger:  doc.ToolChanger,
			Deprecated:   doc.Deprecated,
			Artwork:      artworkOf(doc),
		})
	}

	return out
}

// refusedModelEntries is the projection of the refusals onto the wire.
func refusedModelEntries(models profile.Catalogue) []refusedModelEntry {
	refusals := models.Refused()

	out := make([]refusedModelEntry, 0, len(refusals))
	for _, refusal := range refusals {
		out = append(out, refusedModelEntry{
			Model:        refusal.Model,
			Connector:    refusal.Connector,
			Qualified:    refusal.DeclaredBy,
			CollidesWith: refusal.CollidesWith(),
			Remedy:       refusal.Remedy(),
		})
	}

	return out
}
