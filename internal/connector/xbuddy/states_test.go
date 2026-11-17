package xbuddy_test

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// declaredStates is the state set as a host reads it: through the registration
// and not through the package.
func declaredStates(t *testing.T) connectorapi.StateSet {
	t.Helper()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registration, err := types.Lookup(xbuddy.Type)
	require.NoError(t, err)

	return registration.Dialect().States
}

func TestTheDeclaredStatesAreSound(t *testing.T) {
	t.Parallel()

	set := declaredStates(t)

	require.Empty(t, set.Problems())
	require.True(t, set.Guarded(), "every Buddy state says which verbs it accepts")
}

// TestTheGuardsAreTheFirmwares holds the rows of states.go to the firmware
// rules its comment cites. A row that drifted would tell a client to grey out
// a control the machine accepts, or to offer one it rejects.
func TestTheGuardsAreTheFirmwares(t *testing.T) {
	t.Parallel()

	set := declaredStates(t)

	cases := map[connectorapi.Capability][]connectorapi.StateName{
		xbuddy.VerbStartPrint:      {"IDLE", "READY", "STOPPED", "FINISHED"},
		xbuddy.VerbSetPrinterReady: {"IDLE", "READY", "STOPPED", "FINISHED"},
		xbuddy.VerbPausePrint:      {"PRINTING"},
		xbuddy.VerbResumePrint:     {"PAUSED"},
		xbuddy.VerbStopPrint:       {"PRINTING", "PAUSED", "ATTENTION"},
		xbuddy.VerbSetIdle:         {"FINISHED", "STOPPED"},
		xbuddy.VerbFlash:           {"IDLE", "READY", "STOPPED", "FINISHED"},
		xbuddy.VerbUpgrade:         {"IDLE", "READY", "STOPPED", "FINISHED"},
	}

	for verb, want := range cases {
		var got []connectorapi.StateName

		for _, state := range set.States {
			if set.Accepted(state.Name, verb) {
				got = append(got, state.Name)
			}
		}

		require.ElementsMatch(t, want, got, "the states %s is accepted in", verb)
	}
}

func TestErrorAcceptsOnlyTheFirmwaresFourCommands(t *testing.T) {
	t.Parallel()

	set := declaredStates(t)

	index := slices.IndexFunc(set.States, func(s connectorapi.DeviceState) bool { return s.Name == "ERROR" })
	require.GreaterOrEqual(t, index, 0)

	require.ElementsMatch(t, []connectorapi.Capability{
		xbuddy.VerbSendInfo, xbuddy.VerbSendStateInfo, xbuddy.VerbSetToken, xbuddy.VerbResetPrinter,
	}, set.States[index].Accepts)
}

// TestEveryAcceptedVerbIsOneACallerCanSend holds the guard to the published
// baseline. A verb only the driver sends for itself would otherwise be offered
// to a client as a control.
func TestEveryAcceptedVerbIsOneACallerCanSend(t *testing.T) {
	t.Parallel()

	baseline := xbuddy.NewDriver(xbuddy.ModeToken, 1, 0, "", slog.New(slog.DiscardHandler)).Baseline()

	for _, state := range declaredStates(t).States {
		for _, verb := range state.Accepts {
			require.True(t, baseline.Has(verb), "%s accepts %s, which a caller cannot send", state.Name, verb)
		}
	}
}
