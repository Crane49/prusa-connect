package event_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestEveryCoreTypeIsDescribed is the first direction of the drift rule held at
// the source: a type added to the closed set and not described would publish a
// row with two empty cells.
func TestEveryCoreTypeIsDescribed(t *testing.T) {
	t.Parallel()

	docs := event.TypeDocs()
	if len(docs) == 0 {
		t.Fatal("the vocabulary is empty, so every gate over it would pass on nothing")
	}

	for _, doc := range docs {
		if strings.TrimSpace(doc.Summary) == "" {
			t.Errorf("%s has no summary", doc.Type)
		}

		if strings.TrimSpace(doc.Description) == "" {
			t.Errorf("%s has no description", doc.Type)
		}

		if !doc.Class.Valid() {
			t.Errorf("%s carries class %q, which is not in the closed set", doc.Type, doc.Class)
		}

		if doc.Version != event.VocabularyVersion {
			t.Errorf("%s carries version %q, want %q", doc.Type, doc.Version, event.VocabularyVersion)
		}
	}
}

// TestEveryCoreTypeIsInTheCoreNamespace holds the property the hub's refusal
// rests on. A declared type outside the namespace would be published by the
// vocabulary and unenforceable by it.
func TestEveryCoreTypeIsInTheCoreNamespace(t *testing.T) {
	t.Parallel()

	for _, name := range event.Types() {
		if !strings.HasPrefix(name, event.CoreNamespace) {
			t.Errorf("%s is declared as a core type and is outside %s", name, event.CoreNamespace)
		}

		if err := event.ValidateTypeSyntax(name); err != nil {
			t.Errorf("the vocabulary carries a malformed type: %v", err)
		}

		if _, ok := event.LookupType(name); !ok {
			t.Errorf("%s is in Types() and LookupType does not find it", name)
		}
	}
}

// TestTheCoreTypesAreDistinct catches a copied entry whose type was not
// changed, which would publish one type twice and lose another.
func TestTheCoreTypesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})

	for _, name := range event.Types() {
		if _, taken := seen[name]; taken {
			t.Errorf("%s is described twice", name)
		}

		seen[name] = struct{}{}
	}
}

// TestValidateTypeSyntaxRefusesAFrameInjection is the security half. An event
// type is written verbatim into an SSE event field, so a name carrying a
// newline injects a frame of its own into every reader's stream.
func TestValidateTypeSyntaxRefusesAFrameInjection(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":            "",
		"newline":          "com.prusa.connect.x\nevent: forged",
		"carriage return":  "com.prusa.connect.x\rdata: forged",
		"colon":            "com.prusa.connect.x: forged",
		"space":            "com.prusa.connect printer",
		"one segment":      "printer",
		"empty segment":    "com..printer",
		"trailing dot":     "com.prusa.",
		"uppercase":        "com.Prusa.connect.printer.arrived",
		"null byte":        "com.prusa.connect.x\x00",
		"leading hyphen":   "com.-prusa.connect",
		"trailing hyphen":  "com.prusa-.connect",
		"too long":         strings.Repeat("a.", 100),
		"unicode":          "com.prusa.connect.device.arrivé",
		"tab":              "com.prusa.connect.x\tforged",
		"percent encoding": "com.prusa.connect.x%0aevent",
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := event.ValidateTypeSyntax(value); !errors.Is(err, event.ErrInvalidEnvelope) {
				t.Errorf("%q was accepted, want ErrInvalidEnvelope, got %v", value, err)
			}
		})
	}
}

// TestValidateTypeSyntaxAcceptsAWellFormedName covers the other side, so the
// rule is not simply refusing everything.
func TestValidateTypeSyntaxAcceptsAWellFormedName(t *testing.T) {
	t.Parallel()

	accepted := []string{
		event.TypeDeviceArrived,
		event.TypeDeviceTelemetrySampled,
		"com.example.plugin.thing.happened",
		"prusa-connect.printer.initial_state",
		"a.b",
	}

	for _, name := range accepted {
		if err := event.ValidateTypeSyntax(name); err != nil {
			t.Errorf("%q was refused: %v", name, err)
		}
	}
}

// TestValidateTypeClosesTheCoreNamespace is the membership half. Syntax alone
// admits an unregistered value under the namespace this project owns, and such
// a value flows end to end with nothing to notice it.
func TestValidateTypeClosesTheCoreNamespace(t *testing.T) {
	t.Parallel()

	for _, name := range event.Types() {
		if err := event.ValidateType(name); err != nil {
			t.Errorf("the declared type %q was refused: %v", name, err)
		}
	}

	unregistered := event.CoreNamespace + "printer.invented"
	if err := event.ValidateType(unregistered); !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Errorf("%q was accepted, want ErrInvalidEnvelope, got %v", unregistered, err)
	}

	if _, ok := event.LookupType(unregistered); ok {
		t.Errorf("%q resolved to a descriptor", unregistered)
	}
}

// TestValidateTypeLeavesAnotherNamespaceAlone keeps the closure narrow. A
// plugin's event types stay in its own descriptor, and a peer's stay with the
// peer.
func TestValidateTypeLeavesAnotherNamespaceAlone(t *testing.T) {
	t.Parallel()

	if err := event.ValidateType("com.example.plugin.thing.happened"); err != nil {
		t.Errorf("a type outside the core namespace was refused: %v", err)
	}
}

// TestAnEnvelopeCarryingAForgedTypeIsRefused proves the check is reached
// through the envelope, which is what every transport validates.
func TestAnEnvelopeCarryingAForgedTypeIsRefused(t *testing.T) {
	t.Parallel()

	env := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01",
		Source:      "urn:prusa:site:home/device/p1",
		Type:        "com.prusa.connect.x\nevent: forged",
		Class:       event.ClassState,
	}

	if err := env.Validate(); !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Errorf("an envelope with a forged type validated, got %v", err)
	}
}

// BenchmarkValidateType prices the check every publication runs before an event
// takes a sequence number. It is on the frame path, so an allocation here is one
// per event on every printer in the fleet.
func BenchmarkValidateType(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		if err := event.ValidateType(event.TypeDeviceTelemetrySampled); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateTypeSyntax is the half a type outside the core namespace
// pays, which is what an extension's own event type runs.
func BenchmarkValidateTypeSyntax(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		if err := event.ValidateTypeSyntax("com.example.plugin.thing.happened"); err != nil {
			b.Fatal(err)
		}
	}
}
