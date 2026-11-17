package vocabulary_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

func TestAnEntryWithNoMarkerIsNotDeprecated(t *testing.T) {
	t.Parallel()

	var none *vocabulary.Deprecation

	if none.Deprecated() {
		t.Fatal("a missing marker reported a deprecation")
	}

	if none.Sentence() != "" {
		t.Fatalf("a missing marker printed %q", none.Sentence())
	}

	if (&vocabulary.Deprecation{Since: "  "}).Deprecated() {
		t.Fatal("a marker naming no release reported a deprecation")
	}
}

func TestAMarkerNamesTheReleaseAndTheReplacement(t *testing.T) {
	t.Parallel()

	marked := vocabulary.Deprecate("0.2.0", "printer.arrived")

	if !marked.Deprecated() {
		t.Fatal("a marker that names a release reported no deprecation")
	}

	sentence := marked.Sentence()
	for _, want := range []string{"0.2.0", "printer.arrived"} {
		if !strings.Contains(sentence, want) {
			t.Errorf("the sentence %q does not name %q", sentence, want)
		}
	}

	if replaces := vocabulary.Deprecate("0.2.0", "").Sentence(); strings.Contains(replaces, "Use") {
		t.Errorf("a marker with no replacement still offered one: %q", replaces)
	}
}

// Every vocabulary encodes the marker as the same JSON object, so a client
// needs one decoder. An entry that is not deprecated has no "deprecated" member.
func TestTheMarkerIsOmittedWhenNothingIsDeprecated(t *testing.T) {
	t.Parallel()

	type entry struct {
		Name       string                  `json:"name"`
		Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
	}

	staying, err := json.Marshal(entry{Name: "a"})
	if err != nil {
		t.Fatalf("encode the entry that is staying: %v", err)
	}

	if strings.Contains(string(staying), "deprecated") {
		t.Errorf("an entry that is not deprecated has the deprecated member: %s", staying)
	}

	going, err := json.Marshal(entry{Name: "a", Deprecated: vocabulary.Deprecate("0.2.0", "b")})
	if err != nil {
		t.Fatalf("encode the deprecated entry: %v", err)
	}

	if want := `"deprecated":{"since":"0.2.0","replaced_by":"b"}`; !strings.Contains(string(going), want) {
		t.Errorf("the encoded entry reads %s and not %s", going, want)
	}
}
