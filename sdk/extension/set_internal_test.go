package extension

import (
	"errors"
	"strings"
	"testing"
)

// The gathering list is what the deprecated entry points read, and an
// out-of-tree caller may still reach them, so it stays a path a configured type
// can be resolved on. It has to refuse a bare name two namespaces answer to for
// the same reason the sealed registry does: matched by position,
// the answer goes to whichever namespace sorts first, and a plugin author picks
// their own module path.
func TestTheGatheringListRefusesABareNameTwoNamespacesAnswerTo(t *testing.T) {
	t.Parallel()

	set := &Set[int]{}

	shared := freshName("local")

	// Registered in the order that hands the plugin the win: "com" sorts before
	// "core", so a first-match scan never reaches the built-in.
	for _, entry := range []Entry[int]{
		{Descriptor: descriptorIn("com.github.evil.plugin", "github.com/evil/plugin", shared), Value: 2},
		{Descriptor: descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", shared), Value: 1},
	} {
		if err := set.Add(entry); err != nil {
			t.Fatalf("Add %s: %v", entry.Descriptor.Qualified(), err)
		}
	}

	_, ambiguous := set.Resolve(shared)
	if !errors.Is(ambiguous, ErrAmbiguous) {
		t.Fatalf("a bare name a plugin and a built-in both answer to resolved to one of them: %v", ambiguous)
	}

	// The refusal names both, so an operator can tell which module collided.
	for _, want := range []string{"com.github.evil.plugin/" + shared, "core/" + shared} {
		if !strings.Contains(ambiguous.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, ambiguous)
		}
	}

	// A qualified name is still exact, which is how an operator resolves the
	// collision without either registrant changing.
	found, err := set.Resolve("core/" + shared)
	if err != nil {
		t.Fatalf("the qualified name did not resolve: %v", err)
	}

	if found.Value != 1 {
		t.Errorf("the qualified name resolved to the wrong entry: %d", found.Value)
	}
}

// One name, one registrant, which is every deployment that has installed no
// plugin. The ambiguity refusal must not cost them anything.
func TestASingleRegistrantStillResolvesByBareName(t *testing.T) {
	t.Parallel()

	set := &Set[int]{}
	sole := freshName("sole")

	if err := set.Add(Entry[int]{
		Descriptor: descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", sole),
		Value:      1,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	found, err := set.Resolve(sole)
	if err != nil {
		t.Fatalf("a bare name with one registrant did not resolve: %v", err)
	}

	if found.Value != 1 {
		t.Errorf("resolved to the wrong entry: %d", found.Value)
	}
}
