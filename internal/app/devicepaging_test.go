package app

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// fleetOwner is the principal every printer that arrives is reachable by,
// because an arrival is placed in the bootstrap org and the bootstrap grant
// makes this subject that org's owner.
const fleetOwner = "user:operator"

// TestTheFleetListPagesPastTheFirstFifty is earlier research. The product
// claims four hundred printers and the graph's page defaults to fifty, so a
// list that takes no page parameter cannot return the fifty-first printer by
// any request a client can make.
//
// The fleet here is larger than one default page on purpose: a test that asks
// for the second page of a small fleet passes against a handler that ignores
// the parameter and answers with the first page every time.
func TestTheFleetListPagesPastTheFirstFifty(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const fleet = 120

	for i := range fleet {
		arrive(t, plane, fmt.Sprintf("SIM-%04d", i))
	}

	first, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 50}})
	require.NoError(t, err)
	require.Len(t, first.Items, 50)
	require.Equal(t, fleet, first.Total, "the answer says how many the caller may see over every page")

	second, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 50, Offset: 50}})
	require.NoError(t, err)
	require.Len(t, second.Items, 50)

	third, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 50, Offset: 100}})
	require.NoError(t, err)
	require.Len(t, third.Items, 20, "the last page is the remainder, and it is reachable")

	// Every printer is reachable exactly once over the three pages, which is
	// what a page parameter that is actually read means. A handler that ignored
	// the offset would return the same fifty ids three times.
	seen := map[string]int{}

	for _, page := range []httpapi.DevicePage{first, second, third} {
		for _, item := range page.Items {
			seen[item.ID]++
		}
	}

	require.Len(t, seen, fleet, "the three pages did not cover the fleet exactly once")

	for id, count := range seen {
		require.Equal(t, 1, count, "printer %s was returned on more than one page", id)
	}
}

// TestAPageIsNotShrunkByPrintersThisProcessHasNotMet is the second half of
// earlier research: the join ran after the page was cut, so a page of fifty
// authorized rows of which thirty had no entry returned twenty items and the
// caller could not tell that from a fleet of twenty.
//
// A directory row with no entry behind it is what a restart leaves: the
// directory is on disk and the sessions are not, so every printer that has not
// dialled in again is authorized and unknown.
func TestAPageIsNotShrunkByPrintersThisProcessHasNotMet(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const (
		known   = 30
		unknown = 30
	)

	for i := range known {
		arrive(t, plane, fmt.Sprintf("SIM-%04d", i))
	}

	for i := range unknown {
		identity := connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: fmt.Sprintf("OLD-%04d", i)}
		require.NoError(t, plane.register(t.Context(), plane.idOf(identity), identity, ""))
	}

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{Limit: 20}})
	require.NoError(t, err)
	require.Len(t, page.Items, 20, "the page was cut before the rows with no entry were dropped")
	require.Equal(t, known, page.Total)
}

// TestEveryDeviceRecordNamesItsKind is ADR-20260826-5yj rules 8 and 9 on the wire.
//
// The value is read from `ref` rather than written out here, so renaming the
// kind in the core moves the wire with it. A literal on both sides would let
// the API keep describing a type this build no longer has.
func TestEveryDeviceRecordNamesItsKind(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	arrive(t, plane, "SIM-KIND")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{Page: httpapi.Page{}})
	require.NoError(t, err)
	require.NotEmpty(t, page.Items, "the listing is empty, so this reads no record at all")

	for _, row := range page.Items {
		require.Equal(t, ref.DeviceKindPrinter.String(), row.Kind, "printer "+row.ID)

		one, getErr := plane.Get(t.Context(), fleetOwner, row.ID)
		require.NoError(t, getErr)
		require.Equal(t, row.Kind, one.Kind, "the listing and the single record disagree")
	}
}
