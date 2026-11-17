package authz

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fleet fixture reproduces the shape the research measured: 400 printers in
// 20 printer groups, ten of which nest under the other ten, one org, ten teams
// of twenty users nested into one staff group, a user with fifty direct grants,
// an org viewer with three printers blocked, an org admin, and one device
// credential per printer.
const (
	fleetDevices      = 400
	fleetGroups       = 20
	fleetTopGroups    = fleetGroups / 2
	fleetTeams        = 10
	fleetUsersPerTeam = 20
	fleetDirectGrants = 50
	fleetBlocked      = 3
	// fleetTeamPrinters is what one team's grant reaches: its own printer group
	// and the one nested under it.
	fleetTeamPrinters = 2 * (fleetDevices / fleetGroups)
)

// The principals the measurements and the visibility assertions are about.
const (
	fleetTeamUser  = "user:u000"
	fleetWideUser  = "user:wide"
	fleetOrgViewer = "user:orgviewer"
	fleetOrgAdmin  = "user:root"
	fleetAgent     = "device_credential:d000"
)

func fleetDeviceID(i int) string { return fmt.Sprintf("p%03d", i) }

func fleetGroupID(i int) string { return fmt.Sprintf("g%02d", i) }

// fleetDirectory names printers in the reverse of their id order, so the list
// query has a sort to do rather than a pre-ordered scan to walk.
func fleetDirectory() []DeviceRow {
	out := make([]DeviceRow, 0, fleetDevices)
	for i := range fleetDevices {
		out = append(out, DeviceRow{
			ID:   fleetDeviceID(i),
			Name: fmt.Sprintf("printer-%03d", fleetDevices-1-i),
		})
	}

	return out
}

func fleetTuples() []string {
	out := make([]string, 0, fleetDevices*3)

	for i := range fleetDevices {
		id := fleetDeviceID(i)
		out = append(out,
			fmt.Sprintf("device:%s#org@org:acme", id),
			fmt.Sprintf("device:%s#group@device_group:%s", id, fleetGroupID(i%fleetGroups)),
			fmt.Sprintf("device:%s#agent@device_credential:d%03d", id, i),
		)
	}

	// The upper half of the groups nests under the lower half, so a grant on a
	// floor reaches the cells inside it.
	for i := range fleetTopGroups {
		out = append(out, fmt.Sprintf(
			"device_group:%s#parent@device_group:%s",
			fleetGroupID(i+fleetTopGroups), fleetGroupID(i),
		))
	}

	for team := range fleetTeams {
		for member := range fleetUsersPerTeam {
			out = append(out, fmt.Sprintf(
				"group:t%d#member@user:u%03d", team, team*fleetUsersPerTeam+member,
			))
		}

		out = append(out,
			fmt.Sprintf("group:all-staff#member@group:t%d#member", team),
			fmt.Sprintf("device_group:%s#operator@group:t%d#member", fleetGroupID(team), team),
		)
	}

	for i := range fleetDirectGrants {
		out = append(out, fmt.Sprintf("device:%s#viewer@%s", fleetDeviceID(i), fleetWideUser))
	}

	out = append(out, "org:acme#viewer@"+fleetOrgViewer, "org:acme#admin@"+fleetOrgAdmin)

	for i := range fleetBlocked {
		out = append(out, fmt.Sprintf("device:%s#blocked@%s", fleetDeviceID(i), fleetOrgViewer))
	}

	return out
}

// seedFleet builds the measured fixture on this engine.
func seedFleet(tb testing.TB, e engine) *Store {
	tb.Helper()

	store := newStore(tb, e)
	require.NoError(tb, store.UpsertDevices(tb.Context(), fleetDirectory()...))
	require.NoError(tb, store.Write(tb.Context(), mustTuples(tb, fleetTuples()...)...))

	return store
}

// TestFleetVisibility pins the visibility set of every principal in the
// measured fixture. These are the counts the research reported for the same
// graph, and they are what makes the benchmark numbers below comparable.
func TestFleetVisibility(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFleet(t, e)

		cases := []struct {
			name    string
			subject string
			action  Action
			want    int
		}{
			{
				name:    "a team user sees its floor and the cells under it",
				subject: fleetTeamUser,
				action:  ActionObserve,
				want:    fleetTeamPrinters,
			},
			{name: "and commands them", subject: fleetTeamUser, action: ActionOperate, want: fleetTeamPrinters},
			{name: "and manages none of them", subject: fleetTeamUser, action: ActionAdminister, want: 0},
			{
				name:    "fifty direct grants are fifty printers",
				subject: fleetWideUser,
				action:  ActionObserve,
				want:    fleetDirectGrants,
			},
			{name: "and none of them are commandable", subject: fleetWideUser, action: ActionOperate, want: 0},
			{
				name:    "an org viewer sees the fleet less the blocked printers",
				subject: fleetOrgViewer,
				action:  ActionObserve,
				want:    fleetDevices - fleetBlocked,
			},
			{
				name:    "an org admin sees all of it",
				subject: fleetOrgAdmin,
				action:  ActionObserve,
				want:    fleetDevices,
			},
			{name: "and manages all of it", subject: fleetOrgAdmin, action: ActionAdminister, want: fleetDevices},
			{name: "an agent sees one printer", subject: fleetAgent, action: ActionObserve, want: 1},
			{name: "and commands none", subject: fleetAgent, action: ActionOperate, want: 0},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, tc.subject)

				total, err := store.CountDevices(t.Context(), who, tc.action)
				require.NoError(t, err)
				require.Equal(t, tc.want, total)

				rows, err := store.ListDevices(t.Context(), who, tc.action, Page{Limit: maxPageLimit})
				require.NoError(t, err)
				require.Len(t, rows, tc.want, "the list has to agree with the count")
			})
		}
	})
}

// TestFleetListMatchesThePointCheck runs the invariant over the whole fleet,
// where a projection bug has four hundred chances to show.
func TestFleetListMatchesThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFleet(t, e)

		for _, name := range []string{fleetTeamUser, fleetWideUser, fleetOrgViewer, fleetOrgAdmin, fleetAgent} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				who := subjectOf(t, name)

				for _, action := range []Action{ActionObserve, ActionOperate, ActionAdminister} {
					listed := listedIDs(t, store, who, action)

					for i := range fleetDevices {
						id := fleetDeviceID(i)

						allowed, err := store.Can(t.Context(), who, action, deviceOf(t, id))
						require.NoError(t, err)
						require.Equalf(t, allowed, listed[id], "%s %s on device:%s", name, action, id)
					}
				}
			})
		}
	})
}

// TestFleetListIsSorted checks the order the page is built in, on a directory
// whose names deliberately disagree with its ids.
func TestFleetListIsSorted(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFleet(t, e)

		rows, err := store.ListDevices(
			t.Context(), subjectOf(t, fleetOrgAdmin), ActionObserve, Page{Limit: defaultPageLimit},
		)
		require.NoError(t, err)
		require.Len(t, rows, defaultPageLimit)

		for i := 1; i < len(rows); i++ {
			require.Lessf(t, rows[i-1].Name, rows[i].Name, "row %d is out of order", i)
		}

		require.Equal(t, "printer-000", rows[0].Name)
		require.Equal(t, fleetDeviceID(fleetDevices-1), rows[0].ID)
	})
}

// listedIDs is every printer the list returns for this principal and action.
func listedIDs(tb testing.TB, store *Store, who Subject, action Action) map[string]bool {
	tb.Helper()

	rows, err := store.ListDevices(tb.Context(), who, action, Page{Limit: maxPageLimit})
	require.NoError(tb, err)

	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.ID] = true
	}

	return out
}
