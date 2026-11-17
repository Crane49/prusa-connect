package profile

// The catalogue of device models this build carries a profile for.
//
// A device reports its model in its own spelling, and that spelling is what
// every other surface here carries: `Device.model`, the profile lookup and the
// listing filter all use it. It is not what a page shows. "CoreOneL_INDX" is a
// firmware string, and a person reading a fleet screen wants "Prusa CORE One L
// INDX" and a picture of the machine.
//
// So each row holds three things the capability data cannot: the name a product
// is sold under, the vendor, and a key a page maps to an icon. The icon key is
// a family and not a model, because the machines in one family look the same.
//
// This table is written by hand, and profiles.json is generated. The two are
// held together by TestEveryProfiledModelIsInTheCatalogue, which fails on a
// model with no row and on a row naming a model no profile carries, so a
// firmware release that adds a machine cannot leave a page with nothing to
// draw.

import (
	"sort"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// What the published device-model reference declares about itself. The content
// version moves when a model is added, retired or redefined; the format version
// moves when the shape of a row changes, so a consumer can tell a reshaped
// artefact from a reworded one.
//
// The set is published like every other closed set here: it is generated into
// an artefact and a page, served under /api/v1/vocabularies, and held to its
// version by the compatibility gate. A machine reporting a model with no row
// still connects, so a row that leaves this table takes a name off a page and
// never takes a printer off the fleet.
const (
	VocabularyName    = "device-models"
	VocabularyVersion = "2.0.0"
	VocabularyFormat  = "1.2"
)

// The icon keys. A key names a family of machines that look alike, so a page
// needs one drawing per key and not one per model.
//
// The set is open to a client. A federated peer running a newer core may serve
// a key this build does not know. A page that meets one falls back to a
// generic printer and does not refuse the record.
const (
	IconCoreOne = "printer-core-one"
	IconIX      = "printer-ix"
	IconMini    = "printer-mini"
	IconMK3     = "printer-mk3"
	IconMK4     = "printer-mk4"
	IconXL      = "printer-xl"
	// IconGeneric is the drawing for a machine this project has no picture of.
	// A page that meets it draws the outline of a printer and puts the model the
	// machine reported beside it, as it does for a key it does not know.
	IconGeneric = "printer-generic"
)

// vendorPrusa is the vendor of every row here. A vendor is a brand name, so it
// is the same word in every language and no page translates it.
const vendorPrusa = "Prusa Research"

// ConnectorXBuddy is the dialect every row here is reached through, by its own
// bare name. The machine dials this server. It is a plain string because
// internal/printer may not import a connector package. A machine another
// dialect reaches has its row declared by that dialect's driver.
const ConnectorXBuddy = "xbuddy"

// ModelDoc is one device model as a page reads it.
type ModelDoc struct {
	// Model is the machine's own spelling, exactly as it reports it in the
	// handshake and exactly as `Device.model` carries it. It is the value to
	// filter a listing by and the key to join a device row onto this row.
	Model string `json:"model"`
	// DisplayName is what the machine is sold as. It is what a page puts on a
	// card, and it is never parsed.
	DisplayName string `json:"display_name"`
	// Vendor is who makes it.
	Vendor string `json:"vendor"`
	// Icon is the key a page maps to a drawing. Models that look alike share
	// one.
	Icon string `json:"icon"`
	// Connector is the dialect a machine of this model is reached through, by
	// the dialect's own bare name. A page offering a person a choice of brand
	// groups the catalogue by it, and the device record's driver member names
	// the instance of that dialect the machine actually arrived through.
	Connector string `json:"connector"`
	// DeclaredBy is the qualified name of the driver that declared the row,
	// and empty on a core row. Two modules can share the bare name Connector
	// carries, so a refusal names the driver by this one. A row never serves
	// it; a refusal serves it as qualified.
	DeclaredBy string `json:"-"`
	// DeviceTypeID is the code the machine reports where a model name is
	// expected, which some firmware sends instead of the name. It is here so a
	// client meeting one can resolve it to this row without a second table.
	//
	// It is filled in from the profile this model is filed under and never
	// written on the row below, so the two cannot disagree.
	DeviceTypeID string `json:"device_type_id,omitempty"`
	// ToolChanger marks a model whose numbered slots are separate toolheads
	// (the Prusa INDX tool changer, the XL), so a page draws a row per tool. An
	// MMU's slots are feeders on one head and are not flagged.
	ToolChanger bool `json:"tool_changer,omitempty"`
	// Deprecated marks a model row that is going away, and is nil for one that is
	// staying. From the server's 1.0.0 release on, a row is removed only after a
	// release published this marker for it, and the compatibility gate refuses
	// any other removal. Before that release a major version of the set may drop
	// a row without it.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
	// Artwork is the drawing a driver declared for the model. It is served at
	// a path of its own and never written into the published artefact, which
	// carries the core's rows alone.
	Artwork connectorapi.Artwork `json:"-"`
}

// modelDocs is the catalogue, in the order the published set reads: by the
// machine's own spelling, so two builds list the same models in the same order.
var modelDocs = []ModelDoc{
	{Model: "CoreOne", DisplayName: "Prusa CORE One", Icon: IconCoreOne},
	{Model: "CoreOneL", DisplayName: "Prusa CORE One L", Icon: IconCoreOne},
	{Model: "CoreOneL_INDX", DisplayName: "Prusa CORE One L INDX", Icon: IconCoreOne, ToolChanger: true},
	{Model: "CoreOne_INDX", DisplayName: "Prusa CORE One INDX", Icon: IconCoreOne, ToolChanger: true},
	{Model: "IX", DisplayName: "Prusa iX", Icon: IconIX},
	{Model: "MINI", DisplayName: "Prusa MINI", Icon: IconMini},
	{Model: "MK3.5", DisplayName: "Prusa MK3.5", Icon: IconMK3},
	{Model: "MK3.5S", DisplayName: "Prusa MK3.5S", Icon: IconMK3},
	{Model: "MK3.9", DisplayName: "Prusa MK3.9", Icon: IconMK3},
	{Model: "MK3.9S", DisplayName: "Prusa MK3.9S", Icon: IconMK3},
	{Model: "MK4", DisplayName: "Prusa MK4", Icon: IconMK4},
	{Model: "MK4S", DisplayName: "Prusa MK4S", Icon: IconMK4},
	{Model: "XL", DisplayName: "Prusa XL", Icon: IconXL, ToolChanger: true},
}

// ModelDocs returns the core's catalogue, ordered by the machine's own spelling
// so two builds list the same models in the same order.
//
// The vendor and the connector are filled in here and not repeated on every
// row: every machine in the firmware matrix is a Prusa reached over xBuddy.
// The device type code is read from the profile each model is filed under, so
// this file cannot disagree with the generated data. A model whose profiles
// this build cannot read carries no code.
//
// The generated artefact and page read it, and the served vocabulary reads it
// through Catalogue.Docs, so the core's rows are the same in both. The rows a
// driver declares are served and never published, because they belong to the
// build and not to the core.
func ModelDocs() []ModelDoc {
	codes := map[string]string{}

	if registry, err := Builtin(); err == nil {
		for _, entry := range registry.Profiles {
			codes[entry.Model] = entry.DeviceTypeID
		}
	}

	out := make([]ModelDoc, 0, len(modelDocs))
	out = append(out, modelDocs...)

	for i := range out {
		out[i].Vendor = vendorPrusa
		out[i].Connector = ConnectorXBuddy
		out[i].DeviceTypeID = codes[out[i].Model]
	}

	sort.Slice(out, func(a, b int) bool { return out[a].Model < out[b].Model })

	return out
}

// LookupModel returns the catalogue row for one machine spelling, reporting
// false for a spelling the catalogue does not carry. The comparison is the same
// one the profile lookup makes, so a printer reporting "core one" or its device
// type code reaches the same row a profile does.
func LookupModel(reported string) (ModelDoc, bool) {
	registry, err := Builtin()
	if err != nil {
		return ModelDoc{}, false
	}

	model, ok := registry.Canonical(reported)
	if !ok {
		return ModelDoc{}, false
	}

	for _, doc := range ModelDocs() {
		if doc.Model == model {
			return doc, true
		}
	}

	return ModelDoc{}, false
}
