package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A printer's row says per verb what the caller may send, because its classes
// cannot say it for a dangerous verb. An administrator holds every class and
// still may not flash a printer they do not own, so a page that read only the
// classes offered a flash the server then refused.
func TestAPrinterRowListsAFlashOnlyForACallerWhoReachesIt(t *testing.T) {
	t.Parallel()

	const flash = "core.command.flash"

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arriveUnder(t, plane, connectorapi.Identity{Vendor: "Prusa", Model: "MK4S", UnitUID: "FP-VERBS"})

	row, err := plane.Get(t.Context(), fleetOwner, id)
	require.NoError(t, err)
	require.Contains(t, row.Verbs, "core.command.pause_print", "the row leaves out a verb the caller's role gives")
	require.NotContains(t, row.Verbs, flash, "the row offers a flash to an administrator who does not own the printer")

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelOwner, fleetOwner)))

	row, err = plane.Get(t.Context(), fleetOwner, id)
	require.NoError(t, err)
	require.Contains(t, row.Verbs, flash, "the row leaves out a flash on a printer the caller owns")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, row.Verbs, page.Items[0].Verbs, "the listing and the printer's own read disagree")
}
