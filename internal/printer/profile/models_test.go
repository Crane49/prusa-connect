package profile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryProfiledModelIsInTheCatalogue is the gate the catalogue's comment
// names, both ways round.
//
// profiles.json is generated from the recorded firmware matrix and the
// catalogue is written by hand, so the two drift the moment a firmware release
// adds a machine. A model with no row leaves a page with nothing to draw, and a
// row naming a model no profile carries tells a page about a machine this build
// cannot serve. A machine outside the matrix has its row declared by its driver
// (ADR-20261016-qrp), so every row here has a profile.
func TestEveryProfiledModelIsInTheCatalogue(t *testing.T) {
	t.Parallel()

	registry, err := Builtin()
	require.NoError(t, err)
	require.NotEmpty(t, registry.Profiles, "no profile is shipped, so this gate would compare two empty sets")

	seen := map[string]bool{}
	for _, doc := range ModelDocs() {
		require.Falsef(t, seen[doc.Model], "%s has two rows in the catalogue", doc.Model)

		seen[doc.Model] = true
	}

	catalogued := map[string]bool{}

	for _, doc := range ModelDocs() {
		catalogued[doc.Model] = true
	}

	profiled := map[string]bool{}

	for _, entry := range registry.Profiles {
		profiled[entry.Model] = true

		require.Truef(t, catalogued[entry.Model],
			"%s has a capability profile and no catalogue row, so a page has no name and no icon for it",
			entry.Model)
	}

	for model := range catalogued {
		require.Truef(t, profiled[model],
			"the catalogue names %s and no profile carries it", model)
	}
}

// TestEveryCatalogueRowIsComplete refuses a row with an empty cell. A row
// carrying no display name or no icon key is a row a page cannot render, which
// is the whole reason the catalogue exists.
//
// Every row carries a device type code too. It is the code the Prusa firmware
// reports where a model name is expected, and every row here has a profile
// that names one.
func TestEveryCatalogueRowIsComplete(t *testing.T) {
	t.Parallel()

	rows := ModelDocs()
	require.NotEmpty(t, rows)

	for _, doc := range rows {
		require.NotEmpty(t, doc.Model, "a row carries no machine spelling")
		require.NotEmptyf(t, doc.DisplayName, "%s carries no name a person reads", doc.Model)
		require.NotEmptyf(t, doc.Vendor, "%s says nobody makes it", doc.Model)
		require.NotEmptyf(t, doc.Icon, "%s carries no icon key", doc.Model)
		require.NotEmptyf(t, doc.DeviceTypeID,
			"%s carries no device type code, and its profile has one", doc.Model)
	}
}

// TestAReportedSpellingReachesItsCatalogueRow is the join a page makes. A
// machine reports a model name or a device type code, and both have to reach the
// same row, because a page keys an icon on whichever the record carries.
func TestAReportedSpellingReachesItsCatalogueRow(t *testing.T) {
	t.Parallel()

	for _, reported := range []string{"CoreOne", "core one", "COREONE", "7.1.0"} {
		doc, ok := LookupModel(reported)
		require.Truef(t, ok, "a printer reporting %q reached no catalogue row", reported)
		require.Equal(t, "CoreOne", doc.Model)
		require.Equal(t, "Prusa CORE One", doc.DisplayName)
		require.Equal(t, IconCoreOne, doc.Icon)
	}

	_, ok := LookupModel("SomeoneElsesPrinter")
	require.False(t, ok, "a model this build carries no profile for reached a row")
}

// TestOnlyToolChangerModelsAreFlagged holds the flag to the machines whose
// numbered slots are separate toolheads; an MMU model must not carry it.
func TestOnlyToolChangerModelsAreFlagged(t *testing.T) {
	t.Parallel()

	flagged := map[string]bool{}

	for _, doc := range ModelDocs() {
		if doc.ToolChanger {
			flagged[doc.Model] = true
		}
	}

	require.Equal(t, map[string]bool{"XL": true, "CoreOne_INDX": true, "CoreOneL_INDX": true}, flagged)
}
