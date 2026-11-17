package historyapi_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// TestVerbOfIsTelemetryIsASampleAndEverythingElseIsAFact is ADR-20260901-atx rule 2.
//
// A command is not a class a provider has to special-case: the class picks the
// verb, and the whole table is one sentence. Without this the requested
// frame, the one carrying the verb, the arguments and the deadline, would be
// the one frame of the three a provider silently drops, and a store would hold
// outcomes for instructions it has no record of.
func TestVerbOfIsTelemetryIsASampleAndEverythingElseIsAFact(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		class event.Class
		want  historyapi.Verb
	}{
		{event.ClassTelemetry, historyapi.VerbSample},
		{event.ClassState, historyapi.VerbFact},
		{event.ClassAudit, historyapi.VerbFact},
		{event.ClassCommand, historyapi.VerbFact},
		{event.Class("a class no build has heard of"), historyapi.VerbNone},
	} {
		if got := historyapi.VerbOf(tc.class); got != tc.want {
			t.Errorf("VerbOf(%q) is %v, want %v", tc.class, got, tc.want)
		}
	}

	t.Log("observed: telemetry is a sample, every declared class beside it is a fact")
}

// TestAFactCarriesItsClass is the field ADR-20260901-atx amends HI2 in. A store that
// holds audit rows has to answer a query that excludes them, and the class is
// not recoverable from a stored type string for a type this build has never
// heard of.
func TestAFactCarriesItsClass(t *testing.T) {
	t.Parallel()

	fact := historyapi.Fact{Type: "com.example.thing.happened", Class: event.ClassAudit}
	if fact.Type != "com.example.thing.happened" {
		t.Fatalf("a fact did not carry the type it was written with")
	}

	if fact.Class != event.ClassAudit {
		t.Fatalf("a fact did not carry the class it was written with")
	}

	query := historyapi.FactQuery{Classes: []event.Class{event.ClassState}}
	if len(query.Classes) != 1 {
		t.Fatalf("a fact query did not carry the class restriction")
	}
}

// TestASampleCarriesExactlyOneOfANumberAndAWord is Sample.Check, both ways
// round and off the end of the enumeration.
//
// The rule is a method on the contract and not one each adapter writes for
// itself, so this is the only place it is stated: a store that coerced quietly
// and a store that refused would be equally conformant otherwise, and the two
// would answer one operator's query differently a year later.
//
// The unknown kind is here because a sample arrives from a driver compiled
// against a different build of this module. A kind this build has never heard
// of is refused rather than read as the zero one, which would store somebody
// else's reading as the number 0.
func TestASampleCarriesExactlyOneOfANumberAndAWord(t *testing.T) {
	t.Parallel()

	printer, err := event.NewOrigin(event.KindDevice, "mk4s-01")
	if err != nil {
		t.Fatalf("NewOrigin: %v", err)
	}

	for _, tc := range []struct {
		name   string
		sample historyapi.Sample
		refuse bool
	}{
		{
			name:   "a number",
			sample: historyapi.Sample{Ref: printer, Metric: "/tools/0/nozzle_temp", Value: 214.5},
		},
		{
			name: "a number carrying a word",
			sample: historyapi.Sample{
				Ref: printer, Metric: "/tools/0/nozzle_temp", Value: 214.5, Text: "PRINTING",
			},
			refuse: true,
		},
		{
			name: "a word",
			sample: historyapi.Sample{
				Ref: printer, Metric: "/state", Kind: historyapi.SampleText, Text: "PRINTING",
			},
		},
		{
			name: "a word carrying a number",
			sample: historyapi.Sample{
				Ref: printer, Metric: "/state",
				Kind: historyapi.SampleText, Text: "PRINTING", Value: 1,
			},
			refuse: true,
		},
		{
			// A printer with nothing loaded has reported its material, and the
			// empty string is that reading rather than the absence of one.
			name: "a word that is empty",
			sample: historyapi.Sample{
				Ref: printer, Metric: "/tools/0/material_package_instance/package/material/abbreviation", Kind: historyapi.SampleText,
			},
		},
		{
			name: "a kind this build has never heard of",
			sample: historyapi.Sample{
				Ref: printer, Metric: "/state", Kind: historyapi.SampleKind(9), Text: "PRINTING",
			},
			refuse: true,
		},
	} {
		checked := tc.sample.Check()

		switch {
		case tc.refuse && !errors.Is(checked, historyapi.ErrSampleShape):
			t.Errorf("Check of %s is %v, want ErrSampleShape", tc.name, checked)
		case !tc.refuse && checked != nil:
			t.Errorf("Check of %s refused a well-formed sample with %v", tc.name, checked)
		}
	}

	t.Log("observed: a sample carrying both values, neither of them, or no known kind is refused")
}
