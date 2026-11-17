package ref

import (
	"slices"
	"testing"
)

// TestTheAssetKindIsSupervisedAndServedNeverRun pins the shape ADR-20260911-4p6 rule 30
// gives the kind: the plane constructs and reports an instance, so it is
// enabled, disabled and listed like any other, and the one runtime it offers is
// the one that runs nothing.
func TestTheAssetKindIsSupervisedAndServedNeverRun(t *testing.T) {
	t.Parallel()

	if !slices.Contains(SupervisedKinds(), KindAsset) {
		t.Fatalf("SupervisedKinds() = %v, which does not name %s", SupervisedKinds(), KindAsset)
	}

	if !KindAsset.Configurable() {
		t.Errorf("Kind(%q).Configurable() = false, want true: an operator is granted view_resource on one", KindAsset)
	}

	if !KindAsset.Read() {
		t.Errorf("Kind(%q).Read() = false: the module plane reads every bundle back to serve it", KindAsset)
	}

	if got := KindRuntimes(KindAsset); !slices.Equal(got, []Runtime{RuntimeAsset}) {
		t.Errorf("KindRuntimes(%q) = %v, want [%s]: a runtime that executes anything is a process module",
			KindAsset, got, RuntimeAsset)
	}
}

// TestOnlyTheAssetKindOffersTheAssetRuntime is the other direction. A sink or a
// driver declared as a directory of files would be an instance that reports
// ready and delivers nothing.
func TestOnlyTheAssetKindOffersTheAssetRuntime(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		offers := slices.Contains(KindRuntimes(kind), RuntimeAsset)

		if kind == KindAsset {
			if !offers {
				t.Error("the asset kind does not offer the asset runtime")
			}

			continue
		}

		if offers {
			t.Errorf("%s offers the asset runtime, and only the asset kind may", kind)
		}
	}

	if _, ok := LookupRuntime(RuntimeAsset); !ok {
		t.Error("the asset runtime has no descriptor, so the published page cannot name it")
	}
}
