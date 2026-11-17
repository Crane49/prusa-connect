package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// declaringStates registers one dialect that declares a state set, for the
// tests below. The factory is never called.
func declaringStates(t *testing.T, name connectorapi.TypeName, set connectorapi.StateSet) connectorapi.Registration {
	t.Helper()

	registration, err := connectorapi.Declare(name, outboundDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return baselineDriver{}, nil },
		connectorapi.Dialect{States: set})
	require.NoError(t, err)

	return registration
}

func benchStates() connectorapi.StateSet {
	return connectorapi.StateSet{
		States: []connectorapi.DeviceState{
			{Name: "IDLE", DisplayName: "Idle", Accepts: []connectorapi.Capability{"core.command.start_print"}},
			{Name: "PRINTING", DisplayName: "Printing", Accepts: []connectorapi.Capability{}},
		},
		Transitions: []connectorapi.Transition{
			{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
			{From: "PRINTING", To: "IDLE"},
		},
	}
}

// TestEveryMountedDialectsStatesArePlaced is the install path ADR-20260831-c7b owed: a
// declaration reaches the vocabulary from what the host mounted. Two instances
// of one type share one entry, so a client looking up either device's driver
// finds the same set.
func TestEveryMountedDialectsStatesArePlaced(t *testing.T) {
	t.Parallel()

	bench := declaringStates(t, "bench", benchStates())
	shop := declaringStates(t, "shop", benchStates())

	installed, err := placedStates([]mountedStates{
		{instance: "bench-b", registration: bench},
		{instance: "shop-a", registration: shop, kind: authz.DeviceKindPrinter},
		{instance: "bench-a", registration: bench},
	})
	require.NoError(t, err)

	placed := installed.Declarations()
	require.Len(t, placed, 2)

	found, ok := installed.Lookup(ref.KindPrinter, bench.Qualified())
	require.True(t, ok, "the bench dialect's set was not placed under its qualified type")
	require.Equal(t, []string{"bench-a", "bench-b"}, found.Instances)
	require.Equal(t, bench.Namespace(), found.Namespace)
	require.Equal(t, benchStates(), found.States)

	_, ok = installed.Lookup(ref.KindPrinter, shop.Qualified())
	require.True(t, ok, "the second dialect's set was dropped")
}

func TestNoMountedStatesPlaceAnEmptySet(t *testing.T) {
	t.Parallel()

	installed, err := placedStates(nil)
	require.NoError(t, err)
	require.Empty(t, installed.Declarations())
}
