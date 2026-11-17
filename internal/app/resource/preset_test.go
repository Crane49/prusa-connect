package resource_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// purge is the preset every case below writes unless it needs another shape.
func purge() httpapi.GCodePresetSpec {
	return httpapi.GCodePresetSpec{
		Summary: "Home, heat and purge before a long print.",
		Models:  []string{"MK4", "MK4S"},
		Steps:   []string{"G28", "M104 S215", "G1 E10 F100"},
	}
}

// grantTo writes one role on the preset kind for a named principal, which is
// how the two halves of ADR-20260825-z65 rule 16 are told apart.
func (h *harness) grantTo(principal string, relation authz.Relation) {
	h.t.Helper()

	require.NoError(h.t, h.graph.Write(h.t.Context(),
		h.tuple(ref.KindGCodePreset, relation, principal)))
}

func (h *harness) createPreset(name string, spec httpapi.GCodePresetSpec) (httpapi.GCodePresetResult, error) {
	h.t.Helper()

	return h.store.ApplyPreset(h.t.Context(), owner, name, spec,
		httpapi.Precondition{MustNotExist: true})
}

// TestAPresetIsWrittenIntoTheOverlayUnderTheSharedVersionToken is ADR-20260825-z65 rule
// 14: the preset lands in the machine-owned overlay, beside the instances,
// under the token that fences a write to either.
func TestAPresetIsWrittenIntoTheOverlayUnderTheSharedVersionToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	before := h.version()

	written, err := h.createPreset("purge", purge())
	require.NoError(t, err)
	require.True(t, written.Created)
	require.Equal(t, "purge", written.Preset.Name)
	require.Equal(t, []string{"G28", "M104 S215", "G1 E10 F100"}, written.Preset.Steps)
	require.NotEqual(t, before, written.Preset.Version, "a write that changed the overlay moved its token")

	var overlay struct {
		Presets []struct {
			Name   string   `json:"name"`
			Steps  []string `json:"steps"`
			Models []string `json:"models"`
		} `json:"presets"`
	}

	require.NoError(t, json.Unmarshal(h.overlayBytes(), &overlay))
	require.Len(t, overlay.Presets, 1)
	require.Equal(t, "purge", overlay.Presets[0].Name)
	require.Equal(t, []string{"MK4", "MK4S"}, overlay.Presets[0].Models)

	require.Empty(t, h.stray(), "a staged write was left behind")
}

// TestAPresetIsNotAnInstanceAndNeverReachesTheSupervisionPlane is the reason a
// preset is not written through the resource surface: nothing constructs one,
// so a plane asked to start it would report an instance that can never become
// ready.
func TestAPresetIsNotAnInstanceAndNeverReachesTheSupervisionPlane(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))
	h.plane.reset()

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	require.Empty(t, h.plane.outcomes(), "the plane was handed a preset to supervise")
	require.Empty(t, h.store.Document().Instances, "a preset was merged in as a declared instance")
}

// TestAPresetSurvivesAReloadAndIsListed covers the round trip through the file,
// which is the half a write-only test never reaches.
func TestAPresetSurvivesAReloadAndIsListed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	_, err = h.createPreset("park", httpapi.GCodePresetSpec{Steps: []string{"G27"}})
	require.NoError(t, err)

	require.NoError(t, h.store.Reload(t.Context()))

	listing, err := h.store.ListPresets(t.Context(), owner)
	require.NoError(t, err)
	require.Len(t, listing.Items, 2)
	require.Equal(t, "park", listing.Items[0].Name, "presets list by name")
	require.Equal(t, "purge", listing.Items[1].Name)
	require.Equal(t, listing.Version, listing.Items[0].Version)

	one, err := h.store.GetPreset(t.Context(), owner, "purge")
	require.NoError(t, err)
	require.Equal(t, purge().Steps, one.Steps)
	require.Equal(t, purge().Models, one.Models)
}

// TestAPresetWriteIsFencedByTheOverlayToken holds the preset surface to the
// same compare-and-swap the instance surface has. Without it a client that read
// the file a minute ago overwrites whatever was written since.
func TestAPresetWriteIsFencedByTheOverlayToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	first, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	_, err = h.store.ApplyPreset(t.Context(), owner, "purge", purge(),
		httpapi.Precondition{IfMatch: []string{"a-token-from-somewhere-else"}})
	require.True(t, fault.HasCode(err, httpfault.CodeVersionConflict()),
		"a stale token was accepted: %v", err)

	updated := purge()
	updated.Summary = "Changed."

	replaced, err := h.store.ApplyPreset(t.Context(), owner, "purge", updated,
		httpapi.Precondition{IfMatch: []string{first.Preset.Version}})
	require.NoError(t, err)
	require.False(t, replaced.Created)
	require.Equal(t, "Changed.", replaced.Preset.Summary)
}

// TestDeletingAPresetTakesItOutOfTheOverlay covers the third write.
func TestDeletingAPresetTakesItOutOfTheOverlay(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	written, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	require.NoError(t, h.store.DeletePreset(t.Context(), owner, "purge",
		httpapi.Precondition{IfMatch: []string{written.Preset.Version}}))

	_, err = h.store.GetPreset(t.Context(), owner, "purge")
	require.True(t, fault.HasCode(err, httpfault.CodeResourceNotFound()), "the preset survived a delete: %v", err)

	require.NotContains(t, string(h.overlayBytes()), "G1 E10 F100")
}

// TestReadingAPresetTakesViewResourceAndWritingTakesConfigureResource is
// ADR-20260825-z65 rule 16, in both directions. A caller who may read a macro is not
// thereby a caller who may rewrite what it sends to a machine.
func TestReadingAPresetTakesViewResourceAndWritingTakesConfigureResource(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	const reader = "user:reader"

	h.grantTo(reader, authz.RelViewer)

	got, err := h.store.GetPreset(t.Context(), reader, "purge")
	require.NoError(t, err, "a viewer on the preset kind may read a preset")
	require.Equal(t, "purge", got.Name)

	_, err = h.store.ApplyPreset(t.Context(), reader, "purge", purge(),
		httpapi.Precondition{IfMatch: []string{got.Version}})
	require.Error(t, err, "a viewer rewrote a preset")
	require.True(t, fault.HasCode(err, authz.CodeAccessDenied()),
		"a viewer who may see the preset is told which grant is missing, got %v", err)

	require.Error(t, h.store.DeletePreset(t.Context(), reader, "purge",
		httpapi.Precondition{IfMatch: []string{got.Version}}), "a viewer deleted a preset")
}

// TestAPrincipalWithNoGrantOnTheKindLearnsNothing is the absence rule. A caller
// holding nothing must not be able to tell a preset that exists from one that
// does not.
func TestAPrincipalWithNoGrantOnTheKindLearnsNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	_, present := h.store.GetPreset(t.Context(), stranger, "purge")
	require.Error(t, present)

	_, absent := h.store.GetPreset(t.Context(), stranger, "nothing-here")
	require.Error(t, absent)

	var forPresent, forAbsent *fault.Fault

	require.ErrorAs(t, present, &forPresent)
	require.ErrorAs(t, absent, &forAbsent)
	require.Equal(t, httpfault.CodeResourceNotFound(), forPresent.Code())
	require.Equal(t, forAbsent.Code(), forPresent.Code())
	require.Equal(t, forAbsent.Message(), forPresent.Message(),
		"a caller can tell a preset that exists from one that does not")

	listing, err := h.store.ListPresets(t.Context(), stranger)
	require.NoError(t, err)
	require.Empty(t, listing.Items, "a caller holding no grant was shown a preset")
}

// TestAnEmptyPresetListingCarriesNoVersion is the resource listing's rule on
// the preset listing. The version is the ETag of the whole overlay, so a caller
// with no grant was handed a token that moved whenever anybody wrote any preset
// or any instance: a poll on this route reported that the configuration had
// changed, to somebody the graph grants nothing.
//
// The viewer grant is the arming half. Without it the case would pass on a
// listing that never carries a version at all, which is the same defect written
// the other way round.
func TestAnEmptyPresetListingCarriesNoVersion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	blind, err := h.store.ListPresets(t.Context(), stranger)
	require.NoError(t, err)
	require.Empty(t, blind.Items)
	require.Empty(t, blind.Version, "a caller who may read no preset was given the overlay's token")

	h.grantTo(stranger, authz.RelViewer)

	seen, err := h.store.ListPresets(t.Context(), stranger)
	require.NoError(t, err)
	require.Len(t, seen.Items, 1)
	require.NotEmpty(t, seen.Version, "a caller who may read a preset needs the token to write one")
	require.Equal(t, seen.Version, seen.Items[0].Version,
		"the listing and its items report one version, because one file is the concurrency unit")
}

// TestTheRunPathReadsAPresetWithoutAGrantOnTheKind is the other half of rule
// 16: running a preset takes send_gcode on the printer and nothing on the
// preset kind. The transport authorizes the printer before it calls this.
func TestTheRunPathReadsAPresetWithoutAGrantOnTheKind(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	_, err := h.createPreset("purge", purge())
	require.NoError(t, err)

	found, err := h.store.LookupPreset(t.Context(), "purge")
	require.NoError(t, err)
	require.Equal(t, purge().Steps, found.Steps)

	_, err = h.store.LookupPreset(t.Context(), "nothing-here")
	require.True(t, fault.HasCode(err, httpfault.CodeResourceNotFound()))
}

// TestAPresetIsRefusedBeforeAnythingIsWritten covers every bound the stored
// shape carries. A refusal must leave the overlay exactly as it was.
func TestAPresetIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	longStep := strings.Repeat("G", 257)
	manySteps := make([]string, 65)

	for i := range manySteps {
		manySteps[i] = "G28"
	}

	cases := map[string]struct {
		name string
		spec httpapi.GCodePresetSpec
		at   string
	}{
		"no steps":          {"purge", httpapi.GCodePresetSpec{}, "body.steps"},
		"too many steps":    {"purge", httpapi.GCodePresetSpec{Steps: manySteps}, "body.steps"},
		"an oversized step": {"purge", httpapi.GCodePresetSpec{Steps: []string{longStep}}, "body.steps"},
		"an empty step":     {"purge", httpapi.GCodePresetSpec{Steps: []string{"  "}}, "body.steps"},
		"a smuggled second": {"purge", httpapi.GCodePresetSpec{Steps: []string{"G28\nM502"}}, "body.steps"},
		"a tab":             {"purge", httpapi.GCodePresetSpec{Steps: []string{"G28\tM502"}}, "body.steps"},
		"a non-ascii step":  {"purge", httpapi.GCodePresetSpec{Steps: []string{"G28 ; café"}}, "body.steps"},
		"an empty model": {
			"purge",
			httpapi.GCodePresetSpec{Steps: []string{"G28"}, Models: []string{" "}},
			"body.models",
		},
		"a repeated model": {
			"purge",
			httpapi.GCodePresetSpec{Steps: []string{"G28"}, Models: []string{"MK4", "mk4"}},
			"body.models",
		},
		"an oversized model": {
			"purge",
			httpapi.GCodePresetSpec{Steps: []string{"G28"}, Models: []string{strings.Repeat("m", 64)}},
			"body.models",
		},
		"an oversized summary": {
			"purge",
			httpapi.GCodePresetSpec{Steps: []string{"G28"}, Summary: strings.Repeat("s", 257)},
			"body.summary",
		},
		"a name that is not a label": {
			"Purge Sequence", httpapi.GCodePresetSpec{Steps: []string{"G28"}}, "path.name",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			require.NoError(t, h.store.Reload(t.Context()))

			before := h.overlayBytes()

			_, err := h.createPreset(tc.name, tc.spec)
			require.Error(t, err)

			var carried *fault.Fault
			require.ErrorAs(t, err, &carried)
			require.Equal(t, httpfault.CodeRequestInvalid(), carried.Code())

			location, ok := carried.Params().Get("location")
			require.True(t, ok, "the refusal names no member")
			require.Equal(t, tc.at, location.String())

			require.Equal(t, string(before), string(h.overlayBytes()),
				"a refused preset changed the overlay")
		})
	}
}

// TestAPresetAndAnInstanceShareOneOverlayAndOneToken is the property that made
// the preset a section of this file rather than a file of its own: one writer,
// one lock, one token, so neither write can lose the other.
func TestAPresetAndAnInstanceShareOneOverlayAndOneToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	instance := mustRef(t, ref.KindStorage, "local")

	created, err := h.create(instance, spec("filesystem", `{"root":"/tmp/x"}`))
	require.NoError(t, err)

	written, err := h.store.ApplyPreset(t.Context(), owner, "purge", purge(),
		httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)
	require.NotEqual(t, created.Instance.Version, written.Preset.Version)

	// The preset write moved the instance's token too, because the file is the
	// concurrency unit and a write rewrites it whole.
	after, err := h.get(instance)
	require.NoError(t, err)
	require.Equal(t, written.Preset.Version, after.Version)

	listing, err := h.store.ListPresets(t.Context(), owner)
	require.NoError(t, err)
	require.Equal(t, after.Version, listing.Version)
}

// TestTheGenericResourceSurfaceRefusesAPresetKind stops the second door. A
// preset written as an instance would name a type nothing constructs and would
// be handed to the supervision plane.
func TestTheGenericResourceSurfaceRefusesAPresetKind(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.store.Reload(t.Context()))

	instance := mustRef(t, ref.KindGCodePreset, "purge")

	_, err := h.create(instance, spec("core/anything", `{}`))
	require.Error(t, err, "a preset was declared through the instance surface")
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "got %v", err)
}
