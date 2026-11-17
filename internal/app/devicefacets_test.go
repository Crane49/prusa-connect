package app

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

func filterNamed(t *testing.T, page httpapi.DevicePage, param string) httpapi.DeviceFilter {
	t.Helper()

	for _, filter := range page.Filters {
		if filter.Param == param {
			return filter
		}
	}

	t.Fatalf("the page advertises no %s filter", param)

	return httpapi.DeviceFilter{}
}

func countOf(filter httpapi.DeviceFilter, value string) (int, bool) {
	for _, held := range filter.Values {
		if held.Value == value {
			return held.Count, true
		}
	}

	return 0, false
}

// TestAFacetCountsWhatChoosingItWouldList is the chip a reader clicks: its
// number is how many printers it would bring in, so choosing one state does
// not zero the other states' chips.
func TestAFacetCountsWhatChoosingItWouldList(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	for index, state := range []string{"PRINTING", "PRINTING", "IDLE"} {
		session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-FACET-%d", index))
		id := plane.idOf(session.identity)
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
		session.report(t, plane, "/state", `"`+state+`"`)
	}

	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{States: []string{"IDLE"}})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)

	states := filterNamed(t, page, "state")
	require.Equal(t, "multi", states.Kind)

	printing, _ := countOf(states, "PRINTING")
	idle, _ := countOf(states, "IDLE")
	require.Equal(t, 2, printing, "the state chip counted under its own selection")
	require.Equal(t, 1, idle)

	models := filterNamed(t, page, "model")
	require.Len(t, models.Values, 1)
	require.Equal(t, 1, models.Values[0].Count, "the model chip ignored the state selection")

	search := filterNamed(t, page, "search")
	require.Equal(t, "text", search.Kind)
	require.Equal(t, 96, search.MaxLength)
}

// TestAChosenValueThatMatchesNothingStaysOnTheChip is so a reader can see what
// they chose and take it off.
func TestAChosenValueThatMatchesNothingStaysOnTheChip(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	arriveSpeaking(t, plane, "SIM-NONE-1")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{States: []string{"ERROR"}})
	require.NoError(t, err)
	require.Zero(t, page.Total)

	count, listed := countOf(filterNamed(t, page, "state"), "ERROR")
	require.True(t, listed, "the chosen state vanished from the filter")
	require.Zero(t, count)
}

// TestTheListingSaysHowItMayBeOrdered is what lets a client draw a sort
// control without a list of its own.
func TestTheListingSaysHowItMayBeOrdered(t *testing.T) {
	t.Parallel()

	page, err := newPlane(
		t,
	).List(t.Context(), fleetOwner, httpapi.DeviceQuery{Sort: sortByName, Order: orderDescending})
	require.NoError(t, err)

	fields := make([]string, 0, len(page.Sorts))
	for _, offered := range page.Sorts {
		fields = append(fields, offered.Field)
	}

	require.Equal(t, []string{sortByUrgency, sortByName, sortByModel, sortByState, sortBySeen, sortByID}, fields)
	require.Equal(t, sortByName, page.Sort)
	require.Equal(t, orderDescending, page.Order)
}

// TestAValueOnlyAnExcludedPrinterCarriesStaysOnItsChip is the chip that must
// not vanish when a search is typed: it reads zero and stays.
func TestAValueOnlyAnExcludedPrinterCarriesStaysOnItsChip(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	for index, state := range []string{"PRINTING", "IDLE"} {
		session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-EXCL-%d", index))
		id := plane.idOf(session.identity)
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
		session.report(t, plane, "/state", `"`+state+`"`)
	}

	page, err := plane.List(t.Context(), manager, httpapi.DeviceQuery{Search: "SIM-EXCL-1"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)

	states := filterNamed(t, page, "state")
	printing, listed := countOf(states, "PRINTING")
	require.True(t, listed, "a state only an excluded printer reports vanished from the filter")
	require.Zero(t, printing)

	idle, _ := countOf(states, "IDLE")
	require.Equal(t, 1, idle)
}
