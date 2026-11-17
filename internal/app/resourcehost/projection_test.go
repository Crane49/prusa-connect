package resourcehost

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// carriedOnTheStatus maps every supervisor.Status field that reaches a client
// on the wire status to the httpapi.ResourceStatus field carrying it.
var carriedOnTheStatus = map[string]string{
	"State":       "State",
	"Result":      "Result",
	"Conditions":  "Conditions",
	"Attempt":     "Attempt",
	"NextRetryAt": "NextRetryAt",
	"Dropped":     "Dropped",
	// The failure window: how many stops it holds and the latest one. The
	// host adds the memory it reads, and the store adds the budget.
	"RecentStops": "Memory",
}

// carriedElsewhere is every supervisor.Status field the wire status
// deliberately does not carry, and where a client reads it instead. A decision
// recorded here rather than in a reviewer's memory is the difference between a
// field nobody published and a field nobody noticed.
var carriedElsewhere = map[string]string{
	"Ref":  "ResourceInstance.Kind and .Name, which is how an instance is addressed",
	"UID":  "ResourceInstance.UID",
	"Type": "ResourceInstance.Spec.Type",
	"Generation": "nowhere, on purpose: the supervisor's construction counter and the declaration " +
		"generation count different things, and observed_generation on the wire is the second one",
	"HangEvidenceAt": "nowhere, on purpose: it is the host's input to the hang watch, and a hang shows to " +
		"clients as the result not-responding",
}

// derivedOnTheWire is every httpapi.ResourceStatus field computed rather than
// copied, so no supervisor.Status field shares its name.
var derivedOnTheWire = map[string]string{
	"Serving":            "supervisor.Status.Serving(), which is a method over State",
	"ObservedGeneration": "the declaration generation the host last acted on, handed to projectStatus beside the status",
}

// TestTheStatusProjectionCoversTheSupervisorType is the drift gate the state
// enum's gate does not give: TestTheStateEnumMatchesTheDerivedStates compares
// one enum against condition.State, and nothing else compares the two field
// sets. A field added to supervisor.Status and not projected is invisible to
// every other gate, because no other test reads either type's fields.
//
// Every supervisor field is accounted for exactly once, as projected or as
// deliberately absent, and every wire field is a projection target or derived.
// A new field on either type lands in none of the three maps and fails here.
func TestTheStatusProjectionCoversTheSupervisorType(t *testing.T) {
	t.Parallel()

	wire := map[string]struct{}{}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[httpapi.ResourceStatus]()) {
		wire[field.Name] = struct{}{}
	}

	for _, field := range reflect.VisibleFields(reflect.TypeFor[supervisor.Status]()) {
		_, projected := carriedOnTheStatus[field.Name]
		_, absent := carriedElsewhere[field.Name]

		require.NotEqualf(t, projected, absent,
			"supervisor.Status.%s is in neither map or in both: say where a client reads it, "+
				"or say that it is not published and why", field.Name)

		if projected {
			require.Containsf(t, wire, carriedOnTheStatus[field.Name],
				"supervisor.Status.%s is mapped to a ResourceStatus field that does not exist", field.Name)
		}
	}

	targets := map[string]struct{}{}
	for _, target := range carriedOnTheStatus {
		targets[target] = struct{}{}
	}

	for name := range wire {
		_, copied := targets[name]
		_, derived := derivedOnTheWire[name]

		require.Truef(t, copied || derived,
			"httpapi.ResourceStatus.%s is filled from nothing this gate knows about", name)
	}
}

// TestEveryProjectedFieldIsActuallyFilled is the half a name map cannot hold. A
// field listed above that projectStatus forgets to assign is a status field
// that is always the zero value, and the map would still agree with itself.
//
// Every input below is non-zero and distinguishable, so a projection that
// dropped one, or filled one from the wrong source, leaves a zero value here.
func TestEveryProjectedFieldIsActuallyFilled(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, "webhook")
	require.NoError(t, err)

	const declared = int64(7)

	uid, err := (&ref.Minter{}).Mint(time.Now())
	require.NoError(t, err)

	full := supervisor.Status{
		Ref:         instance,
		UID:         uid,
		Type:        "http",
		State:       condition.StateDegraded(),
		Result:      supervisor.ResultRuntimeError,
		Generation:  3,
		Attempt:     4,
		NextRetryAt: time.Now().Add(time.Minute),
		Dropped:     11,
		Conditions: []condition.Condition{{
			Type:               condition.TypeStarted,
			Status:             condition.StatusFalse,
			Reason:             "StartFailed",
			Message:            "the endpoint refused the connection",
			LastTransitionTime: time.Now(),
			ObservedGeneration: 3,
		}},
		RecentStops: []supervisor.Stop{{Result: supervisor.ResultSignal, At: time.Now(), Signal: "SIGSEGV"}},
	}

	projected := reflect.ValueOf(projectStatus(full, declared))

	for _, field := range reflect.VisibleFields(reflect.TypeFor[httpapi.ResourceStatus]()) {
		require.Falsef(t, projected.FieldByIndex(field.Index).IsZero(),
			"httpapi.ResourceStatus.%s is the zero value after projecting a status where nothing is zero",
			field.Name)
	}

	// Degraded serves, so the derived field is true rather than merely non-zero,
	// and observed_generation is the declaration's and not the supervisor's.
	status := projectStatus(full, declared)
	require.True(t, status.Serving)
	require.Equal(t, declared, status.ObservedGeneration)
}
