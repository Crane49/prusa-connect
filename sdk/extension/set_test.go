package extension_test

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// declared counts the names this package has taken. See freshName.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var declared atomic.Uint64

// freshName is a name nothing has declared yet.
//
// extension.Declare writes into the process's own name table and nothing takes
// an entry out again, so a fixed name is declared once and every run after that
// is refused. Under -count=2 the cases here failed on their first registration,
// which is the trap somebody chasing a flake with -count falls into first. A
// case that needs one name twice holds on to what this returns.
func freshName(stem string) string {
	return stem + "-" + strconv.FormatUint(declared.Add(1), 10)
}

func declare(t *testing.T, kind extension.Kind, name string) extension.Descriptor {
	t.Helper()

	descriptor, err := extension.Declare(kind, hostContract, name, complete())
	if err != nil {
		t.Fatalf("Declare(%q): %v", name, err)
	}

	return descriptor
}

func TestASecondRegistrationOfOneNameIsRefused(t *testing.T) {
	t.Parallel()

	var set extension.Set[int]

	name := freshName("twice")

	first := extension.Entry[int]{Descriptor: declare(t, extension.KindStorage, name), Value: 1}
	if err := set.Add(first); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	err := set.Add(extension.Entry[int]{Descriptor: declare(t, extension.KindStorage, name), Value: 2})
	if !errors.Is(err, extension.ErrDuplicate) {
		t.Fatalf("a second registration of one qualified name was accepted: %v", err)
	}

	if got := len(set.Drain()); got != 1 {
		t.Errorf("the gathering list holds %d entries after one was refused", got)
	}
}

// One name may be taken once per extension point. A storage backend and an
// event sink both called "local" are two different things an operator selects
// in two different config blocks.
func TestOneNameMayBeTakenOncePerExtensionPoint(t *testing.T) {
	t.Parallel()

	var set extension.Set[int]

	shared := freshName("local")

	if err := set.Add(extension.Entry[int]{Descriptor: declare(t, extension.KindStorage, shared)}); err != nil {
		t.Fatalf("registering the storage backend: %v", err)
	}

	if err := set.Add(extension.Entry[int]{Descriptor: declare(t, extension.KindEventSink, shared)}); err != nil {
		t.Errorf("an event sink was refused a name a storage backend holds: %v", err)
	}
}

func TestARegistrationAfterTheDrainIsRefused(t *testing.T) {
	t.Parallel()

	var set extension.Set[int]

	early := declare(t, extension.KindStorage, freshName("before"))
	if err := set.Add(extension.Entry[int]{Descriptor: early}); err != nil {
		t.Fatalf("registering before the drain: %v", err)
	}

	drained := set.Drain()

	if !set.Sealed() {
		t.Error("the gathering list is not sealed after a drain")
	}

	late := freshName("after")

	err := set.Add(extension.Entry[int]{Descriptor: declare(t, extension.KindStorage, late)})
	if !errors.Is(err, extension.ErrSealed) {
		t.Fatalf("a registration after the drain was accepted: %v", err)
	}

	if !strings.Contains(err.Error(), late) {
		t.Errorf("the refusal does not name the late registrant: %v", err)
	}

	// Draining twice answers the same, so a second registry is the same
	// registry rather than a smaller one.
	if second := set.Drain(); len(second) != len(drained) {
		t.Errorf("a second drain returned %d entries and the first returned %d", len(second), len(drained))
	}
}

// A name is unique per extension point, and an extension point is served by more
// than one gathering list: sdk/storageapi and the application's resource registry
// both mint KindStorage descriptors. Two lists that never consult each other take
// one qualified name twice and publish it twice.
func TestOneNameIsTakenOncePerExtensionPointAcrossSets(t *testing.T) {
	t.Parallel()

	var first, second extension.Set[int]

	shared := freshName("two-lists")

	if err := first.Add(extension.Entry[int]{
		Descriptor: declare(t, extension.KindStorage, shared), Value: 1,
	}); err != nil {
		t.Fatalf("registering in the first list: %v", err)
	}

	err := second.Add(extension.Entry[int]{
		Descriptor: declare(t, extension.KindStorage, shared), Value: 2,
	})
	if !errors.Is(err, extension.ErrDuplicate) {
		t.Fatalf("a second gathering list took a qualified name the first holds: %v", err)
	}

	held := 0

	for _, entry := range extension.Global().All() {
		if entry.Name() == shared {
			held++
		}
	}

	if held != 1 {
		t.Errorf("the catalogue holds %d entries called %q, so the published page carries it twice",
			held, shared)
	}
}

func TestAnUndeclaredDescriptorCannotBeRegistered(t *testing.T) {
	t.Parallel()

	var set extension.Set[int]

	if err := set.Add(extension.Entry[int]{Value: 1}); !errors.Is(err, extension.ErrNoValue) {
		t.Errorf("a zero descriptor was registered: %v", err)
	}
}

func TestDescriptorsDoesNotSealTheList(t *testing.T) {
	t.Parallel()

	var set extension.Set[int]

	only := declare(t, extension.KindStorage, freshName("read-only"))
	if err := set.Add(extension.Entry[int]{Descriptor: only}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	if got := len(set.Descriptors()); got != 1 {
		t.Fatalf("Descriptors() returned %d entries", got)
	}

	if set.Sealed() {
		t.Error("reading the descriptors sealed the gathering list, so the generator would break the host")
	}
}
