package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// declaringModels registers one dialect that declares the given models, for
// the tests below. The factory is never called.
func declaringModels(
	t *testing.T, name connectorapi.TypeName, models ...connectorapi.DeviceModel,
) connectorapi.Registration {
	t.Helper()

	registration, err := connectorapi.Declare(name, outboundDoc(),
		func(connectorapi.Spec) (connectorapi.Driver, error) { return baselineDriver{}, nil },
		connectorapi.Dialect{Models: models})
	require.NoError(t, err)

	return registration
}

func benchOne(spelling string) connectorapi.DeviceModel {
	return connectorapi.DeviceModel{
		Model:       spelling,
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		ToolChanger: true,
	}
}

// TestADeclaredModelTakesItsDialectsName is ADR-20261016-qrp rule 4. The driver writes
// no connector, so it cannot name a dialect other than its own, and a page
// groups the model under the dialect that declared it. It writes no icon key
// either, and the host writes the generic one.
func TestADeclaredModelTakesItsDialectsName(t *testing.T) {
	t.Parallel()

	registration := declaringModels(t, "bench", benchOne("Bench_One"))

	catalogue, err := placeModels(t.Context(), slog.New(slog.DiscardHandler), registration)
	require.NoError(t, err)

	require.Contains(t, catalogue.Docs(), profile.ModelDoc{
		Model:       "Bench_One",
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		Icon:        profile.IconGeneric,
		Connector:   "bench",
		DeclaredBy:  registration.Qualified().String(),
		ToolChanger: true,
	})
}

// TestTwoDriversClaimingOneMachineAreRefusedAndTheStartGoesOn is ADR-20261016-qrp rule
// 5 as the host meets it. Each driver passed its own validation, so only the
// host sees the collision. It leaves both rows out, keeps the start going and
// says so in one line per row, naming both drivers and what to do.
func TestTwoDriversClaimingOneMachineAreRefusedAndTheStartGoesOn(t *testing.T) {
	t.Parallel()

	var said bytes.Buffer

	bench := declaringModels(t, "bench", benchOne("Bench_One"))
	other := declaringModels(t, "other-bench", benchOne("bench one"))

	catalogue, err := placeModels(t.Context(), slog.New(slog.NewTextHandler(&said, nil)), bench, other)
	require.NoError(t, err, "a collision refused the start")

	for _, doc := range catalogue.Docs() {
		require.NotEqual(t, "Bench Works", doc.Vendor, "%s was served", doc.Model)
	}

	lines := strings.Split(strings.TrimSpace(said.String()), "\n")
	require.Len(t, lines, 2, "one line per refused row")

	for _, line := range lines {
		require.Contains(t, line, "level=ERROR")
		require.Contains(t, line, `msg="a declared device model was refused"`)
		require.Contains(t, line, bench.Qualified().String())
		require.Contains(t, line, other.Qualified().String())
		require.Contains(t, line, "remedy=")
	}
}

// TestADriverClaimingAPrusaMachineIsRefusedAndTheStartGoesOn is the other
// collision. The Prusa row is the one served.
func TestADriverClaimingAPrusaMachineIsRefusedAndTheStartGoesOn(t *testing.T) {
	t.Parallel()

	var said bytes.Buffer

	bench := declaringModels(t, "bench", benchOne("MK4S"))

	catalogue, err := placeModels(t.Context(), slog.New(slog.NewTextHandler(&said, nil)), bench)
	require.NoError(t, err, "a collision refused the start")
	require.Equal(t, profile.ModelDocs(), catalogue.Docs())

	require.Contains(t, said.String(), "level=ERROR")
	require.Contains(t, said.String(), `model=MK4S type=`+bench.Qualified().String())
	require.Contains(t, said.String(), `the Prusa model`)
}
