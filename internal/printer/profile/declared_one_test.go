//go:build noothermodels

package profile_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// TestThePrinterBuildRefusesADeclaredCoreRow holds ADR-20261016-qrp rule 5 in a build
// that loads one profile and still serves every core row. A row whose profile
// this build did not load is still the core's, and a declared copy of it would
// be a second row for one spelling.
func TestThePrinterBuildRefusesADeclaredCoreRow(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err)

	var unloaded string

	for _, doc := range profile.ModelDocs() {
		if !slices.Contains(registry.Models(), doc.Model) {
			unloaded = doc.Model

			break
		}
	}

	require.NotEmpty(t, unloaded, "this build loaded a profile for every row")

	catalogue, err := profile.Place(profile.ModelDoc{
		Model: unloaded, DisplayName: "Bench Works", Vendor: "Bench Works", Icon: profile.IconGeneric,
		Connector: "bench", DeclaredBy: "com.example.aux.bench/bench",
	})
	require.NoError(t, err)

	require.Equal(t, []profile.Refusal{{
		Model: unloaded, Connector: "bench", DeclaredBy: "com.example.aux.bench/bench", Profile: unloaded,
	}}, catalogue.Refused())
}
