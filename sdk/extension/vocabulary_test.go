package extension_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// namespaceCase is one module path and the exact namespace the host derives for
// it. The expected value is written out rather than computed, because a test
// that asks the derivation what it produces passes against every wrong answer.
type namespaceCase struct {
	module string
	want   extension.Namespace
}

// communityModules covers the shapes a third-party driver actually has: a
// GitHub path, a GitLab path with a nested group, a hyphenated repository name,
// a label that starts with a digit, a major-version suffix, and a bare host.
var communityModules = []namespaceCase{
	{module: "github.com/acme/mk4", want: "com.github.aux.acme.mk4"},
	{module: "gitlab.com/team/printers/mk4", want: "com.gitlab.aux.team.printers.mk4"},
	{module: "example.com/prusa-driver-acme", want: "com.example.aux.prusa-driver-acme"},
	{module: "github.com/99designs/mk4", want: "com.github.aux.99designs.mk4"},
	{module: "gitlab.example.com/team/dialect/v2", want: "com.example.gitlab.aux.team.dialect"},
	{module: "acme.dev", want: "dev.acme"},
}

// TestANamespaceIsLegalInEveryVocabularyThatCarriesIt is ADR-20260826-6x5 rule 1. The
// host derives one namespace per module path (ADR-20260825-rre rule 16) and three
// vocabularies carry it on a wire: a capability key, an error domain and an
// event type. A rendering one of them refuses leaves a community author with no
// identifier of their own in that vocabulary, which is what happened: the label
// "-" ADR-20260825-rre rule 14 put between the reversed host and the rest of the path
// was refused by all three.
func TestANamespaceIsLegalInEveryVocabularyThatCarriesIt(t *testing.T) {
	t.Parallel()

	for _, testCase := range communityModules {
		t.Run(testCase.module, func(t *testing.T) {
			t.Parallel()

			namespace, err := extension.NamespaceOf(testCase.module)
			if err != nil {
				t.Fatalf("NamespaceOf(%q): %v", testCase.module, err)
			}

			if namespace != testCase.want {
				t.Fatalf("NamespaceOf(%q) = %q, want %q", testCase.module, namespace, testCase.want)
			}

			key := connectorapi.Capability(namespace.String() + ".command.wipe_eeprom")
			if !key.Valid() {
				t.Errorf("capability key %q is refused by connectorapi.Capability.Valid", key)
			}

			if _, domainErr := fault.NewDomain(namespace.String()); domainErr != nil {
				t.Errorf("error domain %q is refused: %v", namespace, domainErr)
			}

			eventType := namespace.String() + ".printer.arrived"
			if typeErr := event.ValidateType(eventType); typeErr != nil {
				t.Errorf("event type %q is refused: %v", eventType, typeErr)
			}
		})
	}
}

// TestEveryLabelANamespaceCanCarryIsLegalInEveryVocabulary is the same rule
// stated over the grammar rather than over six examples. A namespace label is
// whatever extension.ValidateName accepts, so every vocabulary that carries a
// namespace has to accept every one of them: the boundary label included,
// because the derivation puts it in every namespace with a path element.
func TestEveryLabelANamespaceCanCarryIsLegalInEveryVocabulary(t *testing.T) {
	t.Parallel()

	const (
		alphabet = "az09-_.A"

		// The boundary label, written out rather than read from the package
		// under test: a gate that asks the code what it produces agrees with
		// every answer, including the wrong one.
		boundary = "aux"
	)

	labels := []string{boundary, strings.Repeat("a", 63)}

	for _, first := range alphabet {
		labels = append(labels, string(first))

		for _, second := range alphabet {
			labels = append(labels, string(first)+string(second))
			labels = append(labels, string(first)+string(second)+"z")
		}
	}

	checked := 0

	for _, label := range labels {
		if label != boundary && extension.ValidateName(label) != nil {
			continue
		}

		checked++

		// A namespace of one label is not a legal error domain, so each label is
		// checked in the position the derivation puts it in: inside a namespace
		// that has a host label in front of it.
		namespace := "com." + label

		if key := connectorapi.Capability(namespace + ".command.wipe"); !key.Valid() {
			t.Errorf("label %q: capability key %q is refused", label, key)
		}

		if _, err := fault.NewDomain(namespace); err != nil {
			t.Errorf("label %q: error domain %q is refused: %v", label, namespace, err)
		}

		if err := event.ValidateTypeSyntax(namespace + ".printer.arrived"); err != nil {
			t.Errorf("label %q: event type under %q is refused: %v", label, namespace, err)
		}
	}

	// The generator makes 138 labels and ValidateName accepts 42 of them. A
	// count that moves means the corpus changed, and a corpus that has shrunk
	// to nothing is a test that passes because it checks nothing.
	if want := 42; checked != want {
		t.Errorf("%d of %d generated labels were checked, want %d", checked, len(labels), want)
	}
}

// TestACommunityAuthorMintsEveryIdentifierFromOneNamespace is the whole path a
// driver author walks, in the order they walk it: derive the namespace from the
// module path, then mint the three identifiers the host will carry for them.
// Each one is asserted as the exact string, because a key that is merely "valid"
// is valid for every wrong namespace too.
func TestACommunityAuthorMintsEveryIdentifierFromOneNamespace(t *testing.T) {
	t.Parallel()

	const module = "github.com/acme/prusa-driver-mk4"

	namespace, err := extension.NamespaceOf(module)
	if err != nil {
		t.Fatalf("NamespaceOf(%q): %v", module, err)
	}

	if want := extension.Namespace("com.github.aux.acme.prusa-driver-mk4"); namespace != want {
		t.Fatalf("NamespaceOf(%q) = %q, want %q", module, namespace, want)
	}

	key := connectorapi.Capability(namespace.String() + ".command.purge_chamber")
	if want := "com.github.aux.acme.prusa-driver-mk4.command.purge_chamber"; key.String() != want {
		t.Fatalf("capability key = %q, want %q", key, want)
	}

	if !key.Valid() {
		t.Fatalf("capability key %q is refused", key)
	}

	if key.IsCore() {
		t.Errorf("capability key %q reads as one the host minted", key)
	}

	domain, err := fault.NewDomain(namespace.String())
	if err != nil {
		t.Fatalf("NewDomain(%q): %v", namespace, err)
	}

	code, err := fault.NewCode(domain, "CHAMBER_BUSY")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	if want := "com.github.aux.acme.prusa-driver-mk4/CHAMBER_BUSY"; code.String() != want {
		t.Fatalf("code = %q, want %q", code, want)
	}

	eventType := namespace.String() + ".printer.chamber.purged"
	if want := "com.github.aux.acme.prusa-driver-mk4.printer.chamber.purged"; eventType != want {
		t.Fatalf("event type = %q, want %q", eventType, want)
	}

	// ValidateType is what internal/transport/stream's Publication.validate
	// calls, so an event type this accepts is one the wire path accepts.
	if typeErr := event.ValidateType(eventType); typeErr != nil {
		t.Fatalf("event type %q is refused on the publish path: %v", eventType, typeErr)
	}
}

// TestARequiredCapabilityIsCheckedAgainstTheVocabularyThatOwnsIt is ADR-20260826-6x5
// rule 7. Requires[].Capability is a key in connectorapi's vocabulary, and a
// value the descriptor accepts and that vocabulary refuses reaches the published
// reference as a host requirement no host can serve. ADR-20260825-rre rule 10 refuses an
// empty cell on that page, and a garbage cell says more and means less.
func TestARequiredCapabilityIsCheckedAgainstTheVocabularyThatOwnsIt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		capability string
		wantErr    bool
	}{
		{name: "a key the host publishes", capability: "core.command.start_print"},
		{name: "an argument key", capability: "core.command.start_print#path"},
		{name: "a key under a module namespace", capability: "com.github.aux.acme.mk4.command.wipe"},
		{name: "prose", capability: "this is not a capability key", wantErr: true},
		{name: "uppercase", capability: "core.command.START_PRINT", wantErr: true},
		{name: "two argument separators", capability: "core.command.start#path#extra", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			doc.Requires = []extension.Requirement{{Capability: testCase.capability, Since: "1.0.0"}}

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)

			// The vocabulary's own answer, which is the one Declare has to give.
			owner := connectorapi.Capability(testCase.capability).Valid()
			if owner == testCase.wantErr {
				t.Fatalf("connectorapi.Capability(%q).Valid() = %v, and this case expects the opposite",
					testCase.capability, owner)
			}

			if testCase.wantErr {
				if !errors.Is(err, extension.ErrInvalidText) {
					t.Fatalf("Declare with capability %q = %v, want ErrInvalidText",
						testCase.capability, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Declare with capability %q: %v", testCase.capability, err)
			}
		})
	}
}

// TestAnEmittedEventTypeIsCheckedAgainstTheVocabularyThatOwnsIt is ADR-20260826-6x5
// rule 7 on the descriptor's other cross-reference. Emits[].Type is a type in
// sdk/event's vocabulary, and a value that vocabulary refuses reaches the
// published reference as an event no host will ever carry: stream's publish
// path runs ValidateType and drops it there, so the page advertises an event
// that cannot arrive.
//
// The accepted rows are half the gate. A check that refuses a bad type is worth
// nothing if it also refuses a good one, and that is the shape a Declare which
// refused everything would take.
func TestAnEmittedEventTypeIsCheckedAgainstTheVocabularyThatOwnsIt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		eventType string
		wantErr   bool
	}{
		{name: "a core type the vocabulary carries", eventType: event.TypeDeviceArrived},
		{name: "a type under a module namespace", eventType: "com.github.aux.acme.mk4.printer.chamber.purged"},
		{name: "a two-segment type", eventType: "job.updated"},
		{name: "prose", eventType: "NOT even reverse dns", wantErr: true},
		{name: "one segment", eventType: "arrived", wantErr: true},
		{name: "an mqtt topic pattern", eventType: "v1/devices/printers/{printerId}/data/state", wantErr: true},
		{
			name:      "a core type the vocabulary does not carry",
			eventType: "com.prusa.connect.device.exploded",
			wantErr:   true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := complete()
			doc.Emits = []extension.Emission{
				{Type: testCase.eventType, Version: "1.0.0", Summary: "something happened."},
			}

			_, err := extension.Declare(extension.KindEventSink, hostContract, "broker", doc)

			// The vocabulary's own answer, which is the one Declare has to give.
			owner := event.ValidateType(testCase.eventType)
			if (owner != nil) != testCase.wantErr {
				t.Fatalf("event.ValidateType(%q) = %v, and this case expects the opposite",
					testCase.eventType, owner)
			}

			if testCase.wantErr {
				if !errors.Is(err, extension.ErrInvalidText) {
					t.Fatalf("Declare emitting %q = %v, want ErrInvalidText", testCase.eventType, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Declare emitting %q: %v", testCase.eventType, err)
			}
		})
	}
}
