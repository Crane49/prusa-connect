//go:build !nodashboards

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// recordedBoards answers every dashboard call with the sample dashboard and
// records which method the route reached, with what.
type recordedBoards struct {
	mu    sync.Mutex
	calls []string
	ids   []int64
	draft DashboardDraft
}

func (r *recordedBoards) note(call string, id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, call)
	r.ids = append(r.ids, id)
}

func (r *recordedBoards) ListDashboards(_ context.Context, principal string) (DashboardListing, error) {
	r.note("list "+principal, 0)

	return DashboardListing{Items: []DashboardRow{exampleDashboardRow()}, Total: 1}, nil
}

func (r *recordedBoards) GetDashboard(_ context.Context, _ string, id int64) (Dashboard, error) {
	r.note("get", id)

	return exampleDashboard(), nil
}

func (r *recordedBoards) CreateDashboard(_ context.Context, _ string, draft DashboardDraft) (Dashboard, error) {
	r.note("create", 0)
	r.mu.Lock()
	r.draft = draft
	r.mu.Unlock()

	return exampleDashboard(), nil
}

func (r *recordedBoards) ReplaceDashboard(
	_ context.Context,
	_ string,
	id int64,
	draft DashboardDraft,
) (Dashboard, error) {
	r.note("replace", id)
	r.mu.Lock()
	r.draft = draft
	r.mu.Unlock()

	return exampleDashboard(), nil
}

func (r *recordedBoards) DeleteDashboard(_ context.Context, _ string, id int64) error {
	r.note("delete", id)

	return nil
}

func (r *recordedBoards) SetDefaultDashboard(_ context.Context, _ string, id *int64) (*int64, error) {
	var at int64
	if id != nil {
		at = *id
	}

	r.note("default", at)

	return id, nil
}

func dashboardServer(t *testing.T, boards DashboardStore) http.Handler {
	t.Helper()

	cfg := exampleConfig(&gcodeFleetStub{printer: devPrinter()}, seededStore(), seededPresets())
	cfg.Principals = fixedPrincipal("user:operator")
	cfg.Dashboards = boards

	server, err := New(cfg)
	require.NoError(t, err)

	return server.Handler()
}

func TestEveryDashboardRouteReachesItsOwnMethod(t *testing.T) {
	t.Parallel()

	boards := &recordedBoards{}
	handler := dashboardServer(t, boards)

	rec := call(t, handler, http.MethodGet, "/api/v1/dashboards", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = call(t, handler, http.MethodPost, "/api/v1/dashboards",
		`{"name":"Wall","cards":[{"id":"n1","type":"core/note","x":0,"y":0,"w":3,"h":2,`+
			`"config":{"text":"hi","size":2,"big":9007199254740993}},{"id":"c1","type":"core/clock","x":3,"y":0,"w":2,"h":2}]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "/api/v1/dashboards/3", rec.Header().Get("Location"))
	require.Equal(t, "9007199254740993", fmt.Sprint((*boards.draft.Cards)[0].Config["big"]),
		"an integer past 2^53 was rounded on its way to the store")

	rec = call(t, handler, http.MethodGet, "/api/v1/dashboards/3", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Get("Location"), "a read named a place to find the dashboard")
	require.JSONEq(t, mustJSON(t, exampleDashboard()), rec.Body.String())

	rec = call(t, handler, http.MethodPut, "/api/v1/dashboards/3", `{"name":"Wall","revision":4}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The default is a path of its own, and never read as an id.
	rec = call(t, handler, http.MethodPut, "/api/v1/dashboards/default", `{"id":3}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"default_id":3}`, rec.Body.String())

	rec = call(t, handler, http.MethodPut, "/api/v1/dashboards/default", `{"id":null}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"default_id":null}`, rec.Body.String())

	rec = call(t, handler, http.MethodDelete, "/api/v1/dashboards/3", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())

	require.Equal(t,
		[]string{"list user:operator", "create", "get", "replace", "default", "default", "delete"}, boards.calls)
	require.Equal(t, []int64{0, 0, 3, 3, 3, 0, 3}, boards.ids)

	// The draft reached the store as it was written, settings included.
	require.Equal(t, "Wall", boards.draft.Name)
	require.Equal(t, int64(4), *boards.draft.Revision)
	require.Nil(t, boards.draft.Cards, "a change that left the cards out handed the store an empty list")
}

func TestADashboardRequestTheContractRefusesNeverReachesTheStore(t *testing.T) {
	t.Parallel()

	boards := &recordedBoards{}
	handler := dashboardServer(t, boards)

	for _, step := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/dashboards/0", ""},
		{http.MethodGet, "/api/v1/dashboards/abc", ""},
		{http.MethodGet, "/api/v1/dashboards/-1", ""},
		{http.MethodGet, "/api/v1/dashboards/1234567890123456789", ""},
		{http.MethodPost, "/api/v1/dashboards", `{"name":"x","owner":"user:sam"}`},
		{http.MethodPost, "/api/v1/dashboards", `{"name":"x","visibility":"friends"}`},
		{http.MethodPost, "/api/v1/dashboards", `{"name":"x","columns":30}`},
		{http.MethodPost, "/api/v1/dashboards", `{"name":"x","cards":[{"id":"a","type":"core/note",` +
			`"x":0,"y":0,"w":1,"h":1,"title":"","config":[1]}]}`},
		{http.MethodPut, "/api/v1/dashboards/default", `{"id":"three"}`},
	} {
		rec := call(t, handler, step.method, step.path, step.body)
		require.GreaterOrEqualf(t, rec.Code, 400, "%s %s %s", step.method, step.path, step.body)
		require.Lessf(t, rec.Code, 500, "%s %s %s: %s", step.method, step.path, step.body, rec.Body.String())
	}

	require.Empty(t, boards.calls)
}

func TestAServerWithNoDashboardStoreAnswersNone(t *testing.T) {
	t.Parallel()

	handler := dashboardServer(t, nil)

	rec := call(t, handler, http.MethodGet, "/api/v1/dashboards", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var listing DashboardListing
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Empty(t, listing.Items)
	require.Nil(t, listing.DefaultID)

	rec = call(t, handler, http.MethodGet, "/api/v1/dashboards/3", "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), httpfault.CodeDashboardNotFound().String())
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	out, err := json.Marshal(v)
	require.NoError(t, err)

	return string(out)
}

func TestDashboardsSwitchedOffServeNothingAndSaySo(t *testing.T) {
	t.Parallel()

	boards := &recordedBoards{}

	cfg := exampleConfig(&gcodeFleetStub{printer: devPrinter()}, seededStore(), seededPresets())
	cfg.Principals = fixedPrincipal("user:operator")
	cfg.Dashboards = boards
	cfg.DashboardsOff = true

	server, err := New(cfg)
	require.NoError(t, err)

	for _, path := range []string{"/api/v1/dashboards", "/api/v1/dashboards/3"} {
		rec := call(t, server.Handler(), http.MethodGet, path, "")
		require.Equalf(t, http.StatusNotFound, rec.Code, "%s: %s", path, rec.Body.String())
	}

	require.Empty(t, boards.calls)
	require.False(t, uiSettings(t, server.Handler()).Dashboards)
}

func TestTheUISettingsSayTheDashboardsAreServed(t *testing.T) {
	t.Parallel()

	require.True(t, uiSettings(t, dashboardServer(t, &recordedBoards{})).Dashboards)
}
