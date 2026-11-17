package ref

import (
	"slices"
	"testing"
)

// TestTheGCodePresetKindIsConfigurableAndNotSupervised pins the third position
// in the kind set that ADR-20260825-z65 rule 14 asks for: a preset is declared through
// the API and stored in the machine-owned overlay, so it carries a resource
// kind grant, and nothing supervises one, so the lifecycle plane never builds
// an instance of it.
func TestTheGCodePresetKindIsConfigurableAndNotSupervised(t *testing.T) {
	t.Parallel()

	if !slices.Contains(Kinds(), KindGCodePreset) {
		t.Fatalf("Kinds() = %v, which does not name %s", Kinds(), KindGCodePreset)
	}

	if !slices.Contains(ConfigurableKinds(), KindGCodePreset) {
		t.Errorf("ConfigurableKinds() = %v, which does not name %s: the authorization graph "+
			"compiles resource_kind grants over exactly that set", ConfigurableKinds(), KindGCodePreset)
	}

	if !KindGCodePreset.Configurable() {
		t.Errorf("Kind(%q).Configurable() = false, want true", KindGCodePreset)
	}

	if KindGCodePreset.Supervised() {
		t.Errorf("Kind(%q).Supervised() = true: nothing constructs a preset, so the supervision "+
			"plane would report an instance that can never become ready", KindGCodePreset)
	}
}

// TestEverySupervisedKindIsConfigurable holds the two sets in the one
// containment that matters. A kind the plane builds instances of that carries
// no resource-kind grant would be a running instance nobody can be granted.
func TestEverySupervisedKindIsConfigurable(t *testing.T) {
	t.Parallel()

	for _, kind := range SupervisedKinds() {
		if !kind.Configurable() {
			t.Errorf("Kind(%q) is supervised and not configurable", kind)
		}

		if !slices.Contains(ConfigurableKinds(), kind) {
			t.Errorf("ConfigurableKinds() does not name the supervised kind %q", kind)
		}
	}

	for _, kind := range Kinds() {
		if got := kind.Supervised(); got != slices.Contains(SupervisedKinds(), kind) {
			t.Errorf("Kind(%q).Supervised() = %v, which disagrees with SupervisedKinds()", kind, got)
		}
	}
}

// TestSupervisedKindsAreInDeclarationOrder keeps the generated artefacts and
// the path enums stable across builds.
func TestSupervisedKindsAreInDeclarationOrder(t *testing.T) {
	t.Parallel()

	supervised := SupervisedKinds()

	positions := make([]int, 0, len(supervised))
	for _, kind := range supervised {
		positions = append(positions, slices.Index(Kinds(), kind))
	}

	if !slices.IsSorted(positions) {
		t.Fatalf("SupervisedKinds() = %v, which is not in Kinds() order", supervised)
	}
}
