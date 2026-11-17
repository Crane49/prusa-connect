package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A printer that registered on its own on an open server belongs to the one
// principal that server serves, so that person can flash it, update it and
// rename it. Before this nobody owned it, and no screen could change that.
func TestAnOpenServerOwnsThePrinterNobodyApproved(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	plane.openOwner = openOwnerOf(config.Default(), newSetupMode(resource.SetupOpen))

	id := arriveUnder(t, plane, connectorapi.Identity{Vendor: "Prusa", Model: "MK4S", UnitUID: "FP-OPEN"})

	require.Equal(t, []string{"user:operator"}, owners(t, graph, id),
		"an open server left the printer nobody approved without an owner")
}

// Only a server that serves every request as core.principal names it as the
// owner. Anywhere else a printer nobody approved stays unowned, because there
// the principal is one person among others.
func TestOnlyAnOpenServerNamesTheOwnerOfAPrinterNobodyApproved(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode string
		auth config.Auth
		want string
	}{
		{name: "open", mode: resource.SetupOpen, want: "user:operator"},
		{name: "a login required", mode: resource.SetupRequired},
		{name: "no decision yet", mode: ""},
		{name: "local accounts", mode: resource.SetupOpen, auth: config.Auth{Local: true}},
		{name: "an issuer", mode: resource.SetupOpen, auth: config.Auth{Issuer: "https://id.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			core := config.Default()
			core.Auth = tc.auth

			require.Equal(t, tc.want, openOwnerOf(core, newSetupMode(tc.mode))())
		})
	}
}

// The first run records its decision while the server runs, and a printer
// that arrives after it is owned without a restart.
func TestAPrinterArrivingAfterTheFirstRunChoseOpenIsOwned(t *testing.T) {
	t.Parallel()

	mode := newSetupMode("")
	owner := openOwnerOf(config.Default(), mode)

	require.Empty(t, owner(), "a server that has not decided yet named an owner")

	mode.set(resource.SetupOpen)

	require.Equal(t, "user:operator", owner())
}
