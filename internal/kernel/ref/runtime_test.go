package ref

import (
	"slices"
	"strings"
	"testing"
)

// TestEveryRuntimeHasADescriptor is the first direction of ADR-20260825-bhe rule 8 for
// the runtime set, held at the source: a runtime added to the closed set and
// not described here would publish a row with two empty cells.
func TestEveryRuntimeHasADescriptor(t *testing.T) {
	t.Parallel()

	for _, runtime := range Runtimes() {
		doc, ok := LookupRuntime(runtime)
		if !ok {
			t.Errorf("%s is in the closed set and has no descriptor", runtime)

			continue
		}

		if strings.TrimSpace(doc.Summary) == "" {
			t.Errorf("%s has no summary", runtime)
		}

		if strings.TrimSpace(doc.Description) == "" {
			t.Errorf("%s has no description", runtime)
		}

		if doc.Version != VocabularyVersion {
			t.Errorf("%s carries version %q, want %q", runtime, doc.Version, VocabularyVersion)
		}
	}
}

// TestTheRuntimeTableNamesNoRuntimeOutsideTheClosedSet is the other direction: a
// descriptor left behind by a retired runtime would publish a value the loader
// refuses.
func TestTheRuntimeTableNamesNoRuntimeOutsideTheClosedSet(t *testing.T) {
	t.Parallel()

	for _, doc := range RuntimeDocs() {
		if !doc.Runtime.Valid() {
			t.Errorf("the descriptor table names %q, which is not in the closed set", doc.Runtime)
		}
	}
}

// TestTheRuntimeTableIsInRuntimesOrder keeps the published artefact stable
// across builds. A generated file whose row order moves for no reason is a diff
// nobody can review.
func TestTheRuntimeTableIsInRuntimesOrder(t *testing.T) {
	t.Parallel()

	declared := Runtimes()
	described := RuntimeDocs()

	if len(described) != len(declared) {
		t.Fatalf("the table has %d entries and the closed set has %d", len(described), len(declared))
	}

	for i, doc := range described {
		if doc.Runtime != declared[i] {
			t.Errorf("entry %d is %s, and the closed set has %s there", i, doc.Runtime, declared[i])
		}
	}
}

// TestEveryKindOffersRuntimesInsideTheClosedSet holds the two tables to each
// other. A kind offering a value no descriptor names would tell an operator to
// write something the loader refuses.
func TestEveryKindOffersRuntimesInsideTheClosedSet(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		offered := KindRuntimes(kind)

		if !kind.Supervised() {
			if len(offered) != 0 {
				t.Errorf("%s constructs nothing and offers %v", kind, offered)
			}

			continue
		}

		if len(offered) == 0 {
			t.Errorf("%s declares instances and offers no runtime, so nothing of that kind can load", kind)
		}

		// An asset is files and never code, so it is the one supervised kind
		// with nothing to compile in (ADR-20260911-4p6 rule 30).
		if !slices.Contains(offered, RuntimeBuiltin) && kind != KindAsset {
			t.Errorf("%s cannot be compiled in", kind)
		}

		for _, runtime := range offered {
			if !runtime.Valid() {
				t.Errorf("%s offers %q, which is not in the closed set", kind, runtime)
			}
		}
	}
}

// TestOnlyASinkOffersTheScriptRuntime is ADR-20260826-w3y's tier boundary in the table
// the published page is generated from. A script maps one event onto messages
// and performs no effect; every other kind holds a connection, answers a
// request or keeps state, and a language with no assignment expresses none of
// those.
func TestOnlyASinkOffersTheScriptRuntime(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		offers := slices.Contains(KindRuntimes(kind), RuntimeScript)

		if kind == KindSink {
			if !offers {
				t.Error("a sink cannot run a script")
			}

			continue
		}

		if offers {
			t.Errorf("%s offers the script runtime, and only a sink may", kind)
		}
	}
}

// TestTheDescriptorCarriesTheRuntimesTheTableHolds is what keeps the published
// answer and the enforced answer one answer. The descriptor fills the field
// from the same table the loader refuses against, so a page that disagreed with
// a refusal would have to come from a second table.
func TestTheDescriptorCarriesTheRuntimesTheTableHolds(t *testing.T) {
	t.Parallel()

	for _, doc := range KindDocs() {
		if !slices.Equal(doc.Runtimes, KindRuntimes(doc.Kind)) {
			t.Errorf("%s publishes %v and offers %v", doc.Kind, doc.Runtimes, KindRuntimes(doc.Kind))
		}
	}
}
