package extension

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// declared counts the names these cases have taken. See freshName.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var declared atomic.Uint64

// freshName is a name nothing has registered yet.
//
// A gathering list writes into the process's own name table and nothing takes
// an entry out again, so a fixed name is registered once and every run after
// that is refused. Under -count=2 the cases here failed on their first
// registration, which is the trap somebody chasing a flake with -count falls
// into first. A case that needs one name twice holds on to what this returns.
func freshName(stem string) string {
	return stem + "-" + strconv.FormatUint(declared.Add(1), 10)
}

// Two namespaces answering to one bare name is the case an exported test cannot
// build: a namespace is derived from the module that registers a name, so a test
// can only ever produce its own. Constructing the descriptors here is the only
// way to exercise the branch that refuses to guess.
func descriptorIn(namespace Namespace, module, name string) Descriptor {
	return Descriptor{
		kind:      KindConnector,
		name:      name,
		namespace: namespace,
		module:    module,
		doc:       Doc{Summary: "s", Description: "d", Version: "1.0.0", Contract: "1.0.0", Stability: StabilityStable},
	}
}

func TestABareNameTwoNamespacesAnswerToIsAmbiguous(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(
		KindConnector,
		Entry[int]{
			Descriptor: descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", "mk4"),
			Value:      1,
		},
		Entry[int]{Descriptor: descriptorIn("com.github.acme.mk4", "github.com/acme/mk4", "mk4"), Value: 2},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if _, ambiguous := registry.Lookup("mk4"); !errors.Is(ambiguous, ErrAmbiguous) {
		t.Errorf("a bare name two namespaces answer to resolved to one of them: %v", ambiguous)
	}

	found, err := registry.Lookup("com.github.acme.mk4/mk4")
	if err != nil {
		t.Fatalf("the qualified name did not resolve: %v", err)
	}

	if found.Descriptor.Namespace() != "com.github.acme.mk4" {
		t.Errorf("the qualified name resolved to namespace %q", found.Descriptor.Namespace())
	}
}

// A third party cannot forge a namespace through Declare, so the case that
// matters is what happens when one somehow holds a core-namespaced descriptor
// for a name we already registered: the second one is refused, and the message
// names the module each came from so an operator can tell which is which.
func TestADuplicateNamesTheModuleEachRegistrantCameFrom(t *testing.T) {
	t.Parallel()

	var set Set[int]

	shared := freshName("filesystem")

	ours := descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", shared)
	theirs := descriptorIn(CoreNamespace, "github.com/acme/hijack", shared)

	if err := set.Add(Entry[int]{Descriptor: ours, Value: 1}); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	err := set.Add(Entry[int]{Descriptor: theirs, Value: 2})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("a second registration of one qualified name was accepted: %v", err)
	}

	for _, want := range []string{
		"github.com/prusa3d/prusa-connect",
		"github.com/acme/hijack",
		"connector core/" + shared,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

func TestARegistryRefusesAnEntryOfAnotherKind(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(KindStorage,
		Entry[int]{Descriptor: descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", "mk4")},
	)
	if !errors.Is(err, ErrInvalidKind) {
		t.Errorf("a connector was accepted into the storage registry: %v", err)
	}
}

func TestAnUnknownNameSaysWhatTheBuildHas(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(KindConnector,
		Entry[int]{Descriptor: descriptorIn(CoreNamespace, "github.com/prusa3d/prusa-connect", "mk4")},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	_, err = registry.Lookup("absent")
	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("an unregistered name resolved: %v", err)
	}

	if !strings.Contains(err.Error(), "core/mk4") {
		t.Errorf("the refusal does not say what this build has: %v", err)
	}
}
