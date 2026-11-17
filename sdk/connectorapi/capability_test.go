package connectorapi_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func TestCapabilitySyntax(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  connectorapi.Capability
		want bool
	}{
		{key: "core.command.start_print", want: true},
		{key: "core.command.start_print#path", want: true},
		{key: "core.command.set_chamber_fan_target#chamber.fan_pwm_target", want: true},
		{key: "io.github.acme.laser.power", want: true},

		// A DNS-1123 label may start with a digit and carry a hyphen, and a
		// module namespace renders in that grammar, so a key has to accept one
		// or a driver cannot mint a key under the namespace the host derived
		// for it (ADR-20260826-6x5 rule 2).
		{key: "com.github.aux.acme.prusa-driver-mk4.command.purge_chamber", want: true},
		{key: "1core.command", want: true},

		{key: "", want: false},
		{key: "core.command.START_PRINT", want: false},
		{key: "core..command", want: false},
		{key: "core.command.", want: false},
		{key: "core.command.-start_print", want: false},
		{key: "core.command.start_print-", want: false},
		{key: "core.command.start_print#", want: false},
		{key: "core.command.start_print#path#extra", want: false},
		{key: "core.command start_print", want: false},
	} {
		t.Run(tc.key.String(), func(t *testing.T) {
			t.Parallel()

			if got := tc.key.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Argument names carry dots, so the separator between a verb and its argument
// cannot be one. A key that splits wrongly here silently addresses a different
// capability.
func TestArgumentKeySplitsBackToItsHalves(t *testing.T) {
	t.Parallel()

	verb := connectorapi.Capability("core.command.set_chamber_fan_target")
	key := connectorapi.ArgumentKey(verb, "chamber.fan_pwm_target")

	if got := key.Verb(); got != verb {
		t.Errorf("Verb() = %q, want %q", got, verb)
	}

	name, ok := key.Argument()
	if !ok || name != "chamber.fan_pwm_target" {
		t.Errorf("Argument() = %q, %v, want the full dotted name", name, ok)
	}

	if _, isArgument := verb.Argument(); isArgument {
		t.Error("a verb key reports an argument half")
	}
}

func TestCoreNamespaceIsRecognised(t *testing.T) {
	t.Parallel()

	if !connectorapi.Capability("core.command.home").IsCore() {
		t.Error("a core key is not recognised as one")
	}

	if connectorapi.Capability("io.github.acme.home").IsCore() {
		t.Error("a driver key is treated as host-minted")
	}
}

// A kind that does not round-trip through its name turns a typo in a profile
// into KindFlag, which validates nothing.
func TestKindRoundTripsThroughItsName(t *testing.T) {
	t.Parallel()

	for kind := connectorapi.KindFlag; kind <= connectorapi.KindOpaque; kind++ {
		parsed, ok := connectorapi.ParseKind(kind.String())
		if !ok || parsed != kind {
			t.Errorf("%v round-tripped to %v, %v", kind, parsed, ok)
		}
	}

	if _, ok := connectorapi.ParseKind("int"); ok {
		t.Error("an undefined kind name was accepted")
	}
}

func TestUnmarshalRefusesAnUndefinedKind(t *testing.T) {
	t.Parallel()

	var value connectorapi.Value
	if err := json.Unmarshal([]byte(`{"flag":true,"kind":"int"}`), &value); err == nil {
		t.Fatal("an undefined kind name decoded without complaint")
	}

	if err := json.Unmarshal([]byte(`{"flag":true,"kind":"integer"}`), &value); err != nil {
		t.Fatalf("a defined kind name failed to decode: %v", err)
	}

	if value.Kind != connectorapi.KindInteger {
		t.Errorf("kind = %v, want integer", value.Kind)
	}
}
