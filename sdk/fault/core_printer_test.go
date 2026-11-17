package fault_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestTheStateRefusalIsRegisteredAsItsOwn holds ADR-20260831-c7b rule 31 against the
// registry rather than against the source: the code is in the global registry,
// so it reaches the published reference and the vocabulary endpoint that read
// the same accessor.
//
// The status is asserted because it is the whole HTTP mapping. A fault renders
// with the status its registration carries, so a wrong number here is a wrong
// number on the wire and in the document.
func TestTheStateRefusalIsRegisteredAsItsOwn(t *testing.T) {
	t.Parallel()

	entry, ok := fault.Global().Lookup(fault.PrinterVerbNotAllowedInState.Code())
	if !ok {
		t.Fatalf("%s is not in the global registry, so no page and no endpoint carries it",
			fault.PrinterVerbNotAllowedInState.Code())
	}

	if got := entry.Code.String(); got != "core.printer/VERB_NOT_ALLOWED_IN_STATE" {
		t.Errorf("the code reads %q", got)
	}

	if entry.Status != http.StatusConflict {
		t.Errorf("the code publishes status %d, want %d: the state a device is in is a conflict "+
			"with the request and not a fault in it", entry.Status, http.StatusConflict)
	}

	if entry.Retry != fault.RetryAllowed {
		t.Error("the code publishes no retry, and a device leaves the state that refused the verb")
	}

	if entry.Redaction != fault.ShowDetail {
		t.Error("the occurrence detail is redacted, so the state the refusal names never reaches the caller")
	}

	for _, name := range []string{"device_id", "verb", "state"} {
		if !slices.Contains(entry.Params, name) {
			t.Errorf("the code does not declare the %q param, so With drops it", name)
		}
	}
}

// TestTheStateRefusalIsNotTheOtherThreeRefusals is ADR-20260831-c7b rule 30. Four
// refusals stay apart, and this holds the three of them this package owns
// against the fourth.
//
// A client that cannot tell them apart retries the wrong one: a verb this build
// does not name never succeeds, and the same verb in a state that refuses it
// succeeds in the next state. The permission refusal is core.printer/NOT_FOUND
// on this surface, because rule 32 keeps a device's existence a
// permission-gated fact.
func TestTheStateRefusalIsNotTheOtherThreeRefusals(t *testing.T) {
	t.Parallel()

	state := fault.PrinterVerbNotAllowedInState.Code()

	others := map[string]fault.Code{
		"the verb does not exist":       fault.PrinterVerbUnknown.Code(),
		"the caller may not do it":      fault.PrinterNotFound.Code(),
		"the machine refused it itself": fault.PrinterCommandRejected.Code(),
		"the device is not there":       fault.PrinterSessionEnded.Code(),
	}

	for refusal, code := range others {
		if code == state {
			t.Errorf("%q and the state refusal are one code, %s", refusal, code)
		}
	}

	unknown, _ := fault.Global().Lookup(fault.PrinterVerbUnknown.Code())
	refused, _ := fault.Global().Lookup(state)

	if unknown.Status == refused.Status {
		t.Errorf("both verb refusals publish status %d, so a client branching on the status alone "+
			"cannot tell a verb that does not exist from one a state refuses", unknown.Status)
	}
}

// TestTheStateRefusalCarriesTheStateItWasDecidedOn proves the three params
// reach a caller. A param a code does not declare is dropped from the
// rendering, so the assertion is over the payload and not over the builder.
func TestTheStateRefusalCarriesTheStateItWasDecidedOn(t *testing.T) {
	t.Parallel()

	raised := fault.PrinterVerbNotAllowedInState.
		New("the printer is printing, so the verb was not sent").
		With(fault.StringParam("device_id", "9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"),
			fault.StringParam("verb", "core.command.set_nozzle_temperature"),
			fault.StringParam("state", "PRINTING"))

	payload := raised.ProblemJSON("/api/v1/devices/9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93/command")

	if payload.Status != http.StatusConflict {
		t.Errorf("the problem framing carries status %d, want %d", payload.Status, http.StatusConflict)
	}

	for name, want := range map[string]string{
		"device_id": "9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93",
		"verb":      "core.command.set_nozzle_temperature",
		"state":     "PRINTING",
	} {
		value, ok := payload.Params.Get(name)
		if !ok {
			t.Errorf("the payload carries no %q", name)

			continue
		}

		if value.String() != want {
			t.Errorf("%q reads %q, want %q", name, value.String(), want)
		}
	}
}
