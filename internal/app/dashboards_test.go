//go:build !nodashboards

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The people a dashboard test signs in as, one for each org role and one
// holding none. The administrator is the principal the host bootstraps.
const (
	boardAdmin    = "user:operator"
	boardOperator = "user:olga"
	boardViewer   = "user:vera"
	boardStranger = "user:sam"
)

// heardBoards records what the dashboards announced.
type heardBoards struct {
	mu    sync.Mutex
	heard []stream.Publication
}

func (h *heardBoards) Publish(pub stream.Publication) (event.Position, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.heard = append(h.heard, pub)

	return event.Position{}, nil
}

func (h *heardBoards) all() []stream.Publication {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]stream.Publication(nil), h.heard...)
}

func boardsFor(t *testing.T, host hostStore) (*dashboardStore, *heardBoards) {
	t.Helper()

	root, err := authz.ParseSubject(boardAdmin)
	require.NoError(t, err)

	for principal, role := range map[string]authz.Relation{
		boardOperator: authz.RelOperator, boardViewer: authz.RelViewer,
	} {
		who, parseErr := authz.ParseSubject(principal)
		require.NoError(t, parseErr)
		require.NoError(t, host.graph.SetOrgRole(t.Context(), root, who, role))
	}

	heard := &heardBoards{}

	store, err := newDashboardStore(host, heard, discard())
	require.NoError(t, err)

	return store, heard
}

func boardDraft(name, visibility string) httpapi.DashboardDraft {
	cards := []httpapi.DashboardCard{{
		ID: "p1", Type: "core/printer", W: 4, H: 3, Config: map[string]any{"device": "mk4"},
	}}

	return httpapi.DashboardDraft{Name: name, Visibility: &visibility, Cards: &cards}
}

func requireFault(t *testing.T, err error, code fault.Code) {
	t.Helper()

	raised, ok := errors.AsType[*fault.Fault](err)
	require.Truef(t, ok, "answered %v, not a fault", err)
	require.Equal(t, code, raised.Code())
}

func TestEachRoleSeesTheDashboardsOfItsLevel(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		wall, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Wall", "everyone"))
		require.NoError(t, err)
		require.Equal(t, map[string]any{"device": "mk4"}, wall.Cards[0].Config)

		ops, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Ops", "operators"))
		require.NoError(t, err)

		secret, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Admin", "admins"))
		require.NoError(t, err)

		for principal, names := range map[string][]string{
			boardAdmin:    {"Admin", "Ops", "Wall"},
			boardOperator: {"Ops", "Wall"},
			boardViewer:   {"Wall"},
			boardStranger: {},
		} {
			listing, listErr := store.ListDashboards(t.Context(), principal)
			require.NoError(t, listErr)

			seen := []string{}
			for _, row := range listing.Items {
				seen = append(seen, row.Name)
			}

			require.Equalf(t, names, seen, "%s", principal)
			require.Equalf(t, len(names), listing.Total, "%s counts what it may not see", principal)
		}

		// One the caller may not see is the same answer as one not there.
		_, err = store.GetDashboard(t.Context(), boardViewer, ops.ID)
		requireFault(t, err, httpfault.CodeDashboardNotFound())

		_, err = store.GetDashboard(t.Context(), boardOperator, secret.ID)
		requireFault(t, err, httpfault.CodeDashboardNotFound())

		_, err = store.GetDashboard(t.Context(), boardStranger, wall.ID)
		requireFault(t, err, httpfault.CodeDashboardNotFound())

		_, err = store.GetDashboard(t.Context(), boardViewer, wall.ID)
		require.NoError(t, err)
	})
}

func TestAWriteTakesTheRoleItsVisibilityNeeds(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		// A viewer reads and never writes.
		_, err := store.CreateDashboard(t.Context(), boardViewer, boardDraft("Mine", "everyone"))
		requireFault(t, err, authz.CodeAccessDenied())

		_, err = store.CreateDashboard(t.Context(), boardStranger, boardDraft("Mine", "everyone"))
		requireFault(t, err, authz.CodeAccessDenied())

		// An operator writes what operators see, and not past it.
		ops, err := store.CreateDashboard(t.Context(), boardOperator, boardDraft("Ops", "operators"))
		require.NoError(t, err)

		_, err = store.CreateDashboard(t.Context(), boardOperator, boardDraft("Hidden", "admins"))
		requireFault(t, err, authz.CodeAccessDenied())

		admins := "admins"
		_, err = store.ReplaceDashboard(t.Context(), boardOperator, ops.ID,
			httpapi.DashboardDraft{Name: "Ops", Visibility: &admins, Revision: &ops.Revision})
		requireFault(t, err, authz.CodeAccessDenied())

		// The default changes what everyone sees first, so it takes an admin.
		wall, err := store.CreateDashboard(t.Context(), boardOperator, boardDraft("Wall", "everyone"))
		require.NoError(t, err)

		_, err = store.SetDefaultDashboard(t.Context(), boardOperator, &wall.ID)
		requireFault(t, err, authz.CodeAccessDenied())

		held, err := store.SetDefaultDashboard(t.Context(), boardAdmin, &wall.ID)
		require.NoError(t, err)
		require.Equal(t, wall.ID, *held)

		_, err = store.SetDefaultDashboard(t.Context(), boardAdmin, &ops.ID)
		requireFault(t, err, httpfault.CodeDashboardInvalid())

		// A viewer cannot delete; an operator can.
		requireFault(t, store.DeleteDashboard(t.Context(), boardViewer, wall.ID), authz.CodeAccessDenied())
		require.NoError(t, store.DeleteDashboard(t.Context(), boardOperator, wall.ID))
	})
}

func TestAChangeCarriesTheRevisionItWasMadeAgainst(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		board, err := store.CreateDashboard(t.Context(), boardOperator, boardDraft("Farm", "everyone"))
		require.NoError(t, err)

		_, err = store.ReplaceDashboard(t.Context(), boardOperator, board.ID, httpapi.DashboardDraft{Name: "Farm"})
		requireFault(t, err, httpfault.CodeDashboardInvalid())

		saved, err := store.ReplaceDashboard(t.Context(), boardOperator, board.ID,
			httpapi.DashboardDraft{Name: "Farm, first", Revision: &board.Revision})
		require.NoError(t, err)
		require.Len(t, saved.Cards, 1, "a change that left the cards out removed them")

		_, err = store.ReplaceDashboard(t.Context(), boardAdmin, board.ID,
			httpapi.DashboardDraft{Name: "Farm, late", Revision: &board.Revision})
		requireFault(t, err, httpfault.CodeDashboardChanged())

		wide := 30
		_, err = store.ReplaceDashboard(t.Context(), boardAdmin, board.ID,
			httpapi.DashboardDraft{Name: "Farm", Columns: &wide, Revision: &saved.Revision})
		requireFault(t, err, httpfault.CodeDashboardInvalid())
	})
}

func TestEveryWriteIsAnnouncedToWhoSeesIt(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, heard := boardsFor(t, host)

		board, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Wall", "everyone"))
		require.NoError(t, err)

		admins := "admins"
		narrowed, err := store.ReplaceDashboard(t.Context(), boardAdmin, board.ID,
			httpapi.DashboardDraft{Name: "Wall", Visibility: &admins, Revision: &board.Revision})
		require.NoError(t, err)
		require.NoError(t, store.DeleteDashboard(t.Context(), boardAdmin, narrowed.ID))

		type said struct {
			origin string
			data   dashboardChange
		}

		var got []said

		for _, pub := range heard.all() {
			require.Equal(t, event.TypeDashboardChanged, pub.Type)

			var data dashboardChange
			require.NoError(t, json.Unmarshal(pub.Data, &data))

			got = append(got, said{origin: pub.Origin.String(), data: data})
		}

		require.Equal(t, []said{
			{"dashboard:everyone", dashboardChange{ID: board.ID, Action: "created"}},
			// Narrowed past everyone, and told to everyone who saw it.
			{"dashboard:everyone", dashboardChange{ID: board.ID, Action: "changed"}},
			{"dashboard:admins", dashboardChange{ID: board.ID, Action: "deleted"}},
		}, got)
	})
}

func TestASubscriberHearsTheDashboardsOfItsLevel(t *testing.T) {
	t.Parallel()

	host := testHost(t)
	boardsFor(t, host)

	for principal, origins := range map[string][]string{
		boardAdmin:    {"dashboard:everyone", "dashboard:operators", "dashboard:admins"},
		boardOperator: {"dashboard:everyone", "dashboard:operators"},
		boardViewer:   {"dashboard:everyone"},
		boardStranger: {},
	} {
		who, err := authz.ParseSubject(principal)
		require.NoError(t, err)

		grants, err := dashboardGrants(t.Context(), host.graph, who)
		require.NoError(t, err)

		heard := []string{}
		for _, grant := range grants {
			heard = append(heard, grant.Origin.String())
		}

		require.Equalf(t, origins, heard, "%s", principal)
	}
}

// TestDashboardsSwitchedOffServeNoStoreAndNoOrigin is core.dashboards.enabled
// false in a build that carries the code: no store, no origin on the stream,
// and one line at start saying so.
func TestDashboardsSwitchedOffServeNoStoreAndNoOrigin(t *testing.T) {
	t.Parallel()

	host := testHost(t)

	var said bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&said, nil))

	core := config.Default()
	off := false
	core.Dashboards.Enabled = &off

	store, isOff, err := servedDashboards(t.Context(), core, host, &heardBoards{}, logger)
	require.NoError(t, err)
	require.Nil(t, store)
	require.True(t, isOff)
	require.Contains(t, said.String(), "level=INFO msg=\"dashboards are off\"")

	store, isOff, err = servedDashboards(t.Context(), config.Default(), host, &heardBoards{}, logger)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.False(t, isOff)

	dashboardOrigins := func(seen visibility) []string {
		answer, visibleErr := seen.VisibleOrigins(t.Context(), boardAdmin)
		require.NoError(t, visibleErr)

		var out []string

		for _, name := range originNames(answer.Origins) {
			if strings.HasPrefix(name, "dashboard:") {
				out = append(out, name)
			}
		}

		return out
	}

	require.NotEmpty(t, dashboardOrigins(visibility{graph: host.graph}))
	require.Empty(t, dashboardOrigins(visibility{graph: host.graph, dashboardsOff: true}))
}

func TestAViewerChangesNoDashboardAndChoosesNoDefault(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		wall, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Wall", "everyone"))
		require.NoError(t, err)

		_, err = store.ReplaceDashboard(t.Context(), boardViewer, wall.ID,
			httpapi.DashboardDraft{Name: "Mine", Revision: &wall.Revision})
		requireFault(t, err, authz.CodeAccessDenied())

		_, err = store.SetDefaultDashboard(t.Context(), boardViewer, &wall.ID)
		requireFault(t, err, authz.CodeAccessDenied())

		read, err := store.GetDashboard(t.Context(), boardAdmin, wall.ID)
		require.NoError(t, err)
		require.Equal(t, "Wall", read.Name)
		require.False(t, read.IsDefault)
	})
}

func TestAnOperatorFindsNoDashboardOnlyAdminsSee(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		secret, err := store.CreateDashboard(t.Context(), boardAdmin, boardDraft("Admin", "admins"))
		require.NoError(t, err)

		_, err = store.ReplaceDashboard(t.Context(), boardOperator, secret.ID,
			httpapi.DashboardDraft{Name: "Taken", Revision: &secret.Revision})
		requireFault(t, err, httpfault.CodeDashboardNotFound())

		requireFault(t, store.DeleteDashboard(t.Context(), boardOperator, secret.ID),
			httpfault.CodeDashboardNotFound())

		_, err = store.GetDashboard(t.Context(), boardAdmin, secret.ID)
		require.NoError(t, err, "a refused write changed the dashboard")
	})
}

// flipGraph answers no to Can and then yes to Require, which is a grant
// arriving between the two questions.
type flipGraph struct{}

func (flipGraph) Can(context.Context, authz.Subject, authz.Action, authz.Object) (bool, error) {
	return false, nil
}

func (flipGraph) Require(context.Context, authz.Subject, authz.Action, authz.Object) error {
	return nil
}

// TestACallDecidedOnARefusalStaysRefused is the second question answering yes.
// The call was decided on the first answer, so it is refused, and no write
// runs with the empty subject the refusal would otherwise have handed on.
func TestACallDecidedOnARefusalStaysRefused(t *testing.T) {
	t.Parallel()

	store := &dashboardStore{graph: flipGraph{}, log: discard()}

	_, err := store.CreateDashboard(t.Context(), boardOperator, boardDraft("Wall", "everyone"))
	requireFault(t, err, authz.CodeAccessDenied())

	requireFault(t, store.DeleteDashboard(t.Context(), boardOperator, 1), authz.CodeAccessDenied())

	id := int64(1)
	_, err = store.SetDefaultDashboard(t.Context(), boardOperator, &id)
	requireFault(t, err, authz.CodeAccessDenied())
}

func TestACardKeepsTheNumbersOfItsSettingsAsWritten(t *testing.T) {
	t.Parallel()

	forEachHostEngine(t, func(t *testing.T, host hostStore) {
		store, _ := boardsFor(t, host)

		draft := boardDraft("Farm", "everyone")
		(*draft.Cards)[0].Config = map[string]any{"count": json.Number("9007199254740993")}

		made, err := store.CreateDashboard(t.Context(), boardOperator, draft)
		require.NoError(t, err)

		read, err := store.GetDashboard(t.Context(), boardOperator, made.ID)
		require.NoError(t, err)

		written, err := json.Marshal(read.Cards[0].Config)
		require.NoError(t, err)
		require.JSONEq(t, `{"count":9007199254740993}`, string(written))
		require.Contains(t, string(written), "9007199254740993")
	})
}
