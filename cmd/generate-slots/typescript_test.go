package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// TestTheTypeScriptContractNamesEverySlotAndIcon pins generated.ts to the
// table: a slot or an icon missing there is one the library cannot register
// or draw.
func TestTheTypeScriptContractNamesEverySlotAndIcon(t *testing.T) {
	t.Parallel()

	source := string(renderTypeScript(manifest.Slots(), manifest.Icons()))
	require.Contains(t, source, "'sidebar'")

	for _, slot := range manifest.Slots() {
		require.Contains(t, source, "'"+string(slot.Name)+"'")
		require.Contains(t, source, "  "+slot.Name.Register()+": (")
	}

	for _, icon := range manifest.Icons() {
		if component, own := libraryIcons[icon]; own {
			require.Contains(t, source, "import { "+component+" as ")
			require.NotContains(t, source, "@lucide/svelte/icons/"+string(icon)+"'")

			continue
		}

		require.Contains(t, source, "@lucide/svelte/icons/"+string(icon)+"'")
	}
}

// TestEveryActionSlotNamesItsSubjectInTheActionTarget holds the action menu
// to the id each action slot is handed: a run function given a device id
// where it expects a file reference would run against the wrong thing.
func TestEveryActionSlotNamesItsSubjectInTheActionTarget(t *testing.T) {
	t.Parallel()

	source := string(renderTypeScript(manifest.Slots(), manifest.Icons()))

	for _, slot := range manifest.Slots() {
		if slot.Form != manifest.FormAction {
			continue
		}

		require.Contains(t, source,
			"{ name: '"+string(slot.Name)+"'; "+slot.Subject.Prop()+": string }")
	}
}
