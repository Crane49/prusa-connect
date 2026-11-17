package echo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/connector/echo"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestTheDeclaredStateSetIsOneAHostWillPlace runs the check the host runs at
// install. An adapter author reads this as the answer to "will my declaration
// be accepted", and it is the same function the host calls (ADR-20260825-bhe rule 11).
func TestTheDeclaredStateSetIsOneAHostWillPlace(t *testing.T) {
	t.Parallel()

	problems := echo.States().Problems()
	require.Empty(t, problems, "the reference declaration is one the host refuses: %v", problems)
}

// TestEveryVerbTheStateSetNamesIsOneTheDriverDeclares is the check that keeps
// the two declarations from disagreeing. A transition naming a verb the
// Baseline does not carry describes a move nothing can ask for, and ADR-20260831-c7b
// rule 7 keeps the capability set and the state model separate exactly so that
// each can be read against the other.
func TestEveryVerbTheStateSetNamesIsOneTheDriverDeclares(t *testing.T) {
	t.Parallel()

	baseline := echo.NewDriver(0, 0).Baseline()

	named := 0

	for _, transition := range echo.States().Transitions {
		if !transition.Caused() {
			continue
		}

		named++

		require.Truef(t, baseline.Has(transition.Verb),
			"the state set says %s moves a device from %s to %s, and the driver does not declare it",
			transition.Verb, transition.From, transition.To)
	}

	require.Positive(t, named, "no transition names a verb, so this case compared nothing")
}

// TestEveryStateTheDeviceReachesAloneNamesNoVerb is the shape of a printer's
// state model, which connectorapi.Transition.Verb states: most of the moves
// have no caller.
func TestEveryStateTheDeviceReachesAloneNamesNoVerb(t *testing.T) {
	t.Parallel()

	set := echo.States()

	entered := map[connectorapi.StateName]bool{}
	for _, transition := range set.Transitions {
		if !transition.Caused() {
			entered[transition.To] = true
		}
	}

	for _, state := range []connectorapi.StateName{echo.StateFinished, echo.StateAttention, echo.StateError} {
		require.Truef(t, entered[state], "%s is reachable only by a verb, and no caller finishes a print", state)
	}
}

// TestTheRegistrationCarriesTheStateSet is the path a host reads the set by.
// A set the package declared and the registration left out is a vocabulary
// with no entry for this dialect.
func TestTheRegistrationCarriesTheStateSet(t *testing.T) {
	t.Parallel()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registration, err := types.Lookup(echo.Type)
	require.NoError(t, err)

	require.Equal(t, echo.States(), registration.Dialect().States)
}

// TestEveryAcceptedVerbIsOneTheDriverDeclares is the guard read against the
// baseline, for the reason the transition case above gives.
func TestEveryAcceptedVerbIsOneTheDriverDeclares(t *testing.T) {
	t.Parallel()

	baseline := echo.NewDriver(0, 0).Baseline()

	for _, state := range echo.States().States {
		require.NotNil(t, state.Accepts, "%s says nothing about the verbs it accepts", state.Name)

		for _, verb := range state.Accepts {
			require.Truef(t, baseline.Has(verb), "%s accepts %s, which the driver does not declare", state.Name, verb)
		}
	}
}
