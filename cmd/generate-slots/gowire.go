package main

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// formType is the wire entry type of each form. renderWire fails on a form
// that is missing here.
var formType = map[manifest.Form]string{ //nolint:gochecknoglobals // a closed table, read and never written.
	manifest.FormPage:    "ResourcePage",
	manifest.FormSection: "ResourceSection",
	manifest.FormAction:  "ResourceAction",
	manifest.FormCard:    "ResourceCard",
}

// renderWire writes the wire struct: one field per slot, named and tagged
// from the slot's name.
func renderWire(slots []manifest.Slot) ([]byte, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n\npackage httpapi\n\n", generatedNotice)
	b.WriteString("// ResourceContributes holds the lists a bundle registers into, one per slot.\n" +
		"// Each is absent when the manifest declared none.\n")
	b.WriteString("type ResourceContributes struct {\n")

	for _, slot := range slots {
		kind, known := formType[slot.Form]
		if !known {
			return nil, fmt.Errorf("slot %s has a form the generator does not know", slot.Name)
		}

		fmt.Fprintf(&b, "\t// %s is the %s slot.\n\t%s []%s `json:\"%s,omitempty\" doc:%q`\n",
			slot.Name.Field(), slot.Name, slot.Name.Field(), kind, slot.Name.Key(), slot.Where)
	}

	b.WriteString("}\n")

	return format.Source([]byte(b.String()))
}

// formMapper is the hand-written function in internal/app/resourcehost/asset.go
// that maps one form's declared entries onto the wire.
var formMapper = map[manifest.Form]string{ //nolint:gochecknoglobals // a closed table, read and never written.
	manifest.FormPage:    "pagesOf",
	manifest.FormSection: "sectionsOf",
	manifest.FormAction:  "actionsOf",
	manifest.FormCard:    "cardsOf",
}

// renderMapping writes contributesOf, which copies every slot from the
// manifest's declaration to the wire struct.
func renderMapping(slots []manifest.Slot) ([]byte, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n\npackage resourcehost\n\n", generatedNotice)
	b.WriteString("import (\n" +
		"\t\"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi\"\n" +
		"\t\"github.com/prusa3d/prusa-connect/sdk/manifest\"\n)\n\n")
	b.WriteString("// contributesOf maps what the manifest declared onto the wire. The two shapes\n" +
		"// are kept apart because one is a file an author writes and the other is a\n" +
		"// contract a client generator reads.\n")
	b.WriteString("func contributesOf(declared manifest.Contributes) httpapi.ResourceContributes {\n" +
		"\treturn httpapi.ResourceContributes{\n")

	for _, slot := range slots {
		var from string

		switch slot.Form {
		case manifest.FormPage:
			from = "declared.Pages"
		case manifest.FormSection:
			from = fmt.Sprintf("declared.Sections[manifest.%s]", constName(slot.Name))
		case manifest.FormAction:
			from = fmt.Sprintf("declared.Actions[manifest.%s]", constName(slot.Name))
		case manifest.FormCard:
			from = "declared.Cards"
		}

		fmt.Fprintf(&b, "\t\t%s: %s(%s),\n", slot.Name.Field(), formMapper[slot.Form], from)
	}

	b.WriteString("\t}\n}\n")

	return format.Source([]byte(b.String()))
}

// constName is the sdk/manifest constant of a slot: device-card gives
// SlotDeviceCard.
func constName(name manifest.SlotName) string {
	register := name.Register()

	return "Slot" + strings.ToUpper(register[:1]) + register[1:]
}
