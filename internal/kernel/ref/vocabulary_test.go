package ref

import (
	"strings"
	"testing"
)

// TestEveryKindHasADescriptor is the first direction of ADR-20260825-bhe rule 8, held
// at the source rather than at the artefact: a kind added to the closed set and
// not described here would publish a row with two empty cells.
func TestEveryKindHasADescriptor(t *testing.T) {
	t.Parallel()

	for _, kind := range Kinds() {
		doc, ok := LookupKind(kind)
		if !ok {
			t.Errorf("%s is in the closed set and has no descriptor", kind)

			continue
		}

		if strings.TrimSpace(doc.Summary) == "" {
			t.Errorf("%s has no summary", kind)
		}

		if strings.TrimSpace(doc.Description) == "" {
			t.Errorf("%s has no description", kind)
		}

		if doc.Version != VocabularyVersion {
			t.Errorf("%s carries version %q, want %q", kind, doc.Version, VocabularyVersion)
		}
	}
}

// TestTheDescriptorTableNamesNoKindOutsideTheClosedSet is the other direction:
// a descriptor left behind by a retired kind would publish a kind the code
// refuses to construct.
func TestTheDescriptorTableNamesNoKindOutsideTheClosedSet(t *testing.T) {
	t.Parallel()

	for _, doc := range KindDocs() {
		if !doc.Kind.Valid() {
			t.Errorf("the descriptor table names %q, which is not in the closed set", doc.Kind)
		}
	}
}

// TestTheDescriptorTableIsInKindsOrder holds the artefact stable. The page and
// the endpoint render in table order, so a reordering that nothing catches is a
// diff in a generated file with no change behind it.
func TestTheDescriptorTableIsInKindsOrder(t *testing.T) {
	t.Parallel()

	kinds := Kinds()
	docs := KindDocs()

	if len(docs) != len(kinds) {
		t.Fatalf("the descriptor table holds %d entries, the closed set holds %d", len(docs), len(kinds))
	}

	for i, kind := range kinds {
		if docs[i].Kind != kind {
			t.Errorf("entry %d is %s, want %s", i, docs[i].Kind, kind)
		}
	}
}

// TestTheDescriptorAgreesWithConfigurable stops the published half of the
// vocabulary from drifting from the half the resource API enforces. A kind
// documented as configurable that the lifecycle plane does not own would send
// an operator to a PUT that answers 404.
func TestTheDescriptorAgreesWithConfigurable(t *testing.T) {
	t.Parallel()

	for _, doc := range KindDocs() {
		if doc.Configurable != doc.Kind.Configurable() {
			t.Errorf("%s is documented configurable=%v and reports %v",
				doc.Kind, doc.Configurable, doc.Kind.Configurable())
		}
	}
}

// TestLookupRefusesAnUnknownKind covers the refusal half. A lookup that
// answered a zero descriptor instead of false would publish an empty row.
func TestLookupRefusesAnUnknownKind(t *testing.T) {
	t.Parallel()

	if doc, ok := LookupKind(Kind("nonsense")); ok {
		t.Errorf("an unknown kind resolved to %+v", doc)
	}
}

// TestTheDescriptorsAreDistinct catches a copied entry whose Kind was not
// changed, which would publish one kind twice and lose another.
func TestTheDescriptorsAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[Kind]struct{}, len(kindDocs))

	for _, doc := range KindDocs() {
		if _, taken := seen[doc.Kind]; taken {
			t.Errorf("%s is described twice", doc.Kind)
		}

		seen[doc.Kind] = struct{}{}
	}
}
