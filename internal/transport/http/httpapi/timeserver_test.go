package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// fakeTimeServer is the switch as the routes see it.
type fakeTimeServer struct {
	mu      sync.Mutex
	enabled bool
	sets    int
}

func (f *fakeTimeServer) TimeServer(context.Context) TimeServerState {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.state()
}

func (f *fakeTimeServer) SetTimeServer(_ context.Context, enabled bool) (TimeServerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.enabled = enabled
	f.sets++

	return f.state(), nil
}

func (f *fakeTimeServer) state() TimeServerState {
	if f.enabled {
		return TimeServerState{Enabled: true, State: "serving", Port: timeServerPort}
	}

	return TimeServerState{State: "off", Port: timeServerPort}
}

func timeServerHandler(t *testing.T, grants GrantStore, principal string, server TimeServer) http.Handler {
	t.Helper()

	built, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(principal),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
		TimeServer: server,
	})
	require.NoError(t, err)

	return built.Handler()
}

func TestAViewerReadsTheTimeServer(t *testing.T) {
	t.Parallel()

	grants := &orgRoleGrants{}
	rec := call(t, timeServerHandler(t, grants, "user:viewer", &fakeTimeServer{enabled: true}),
		http.MethodGet, timeServerPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body TimeServerState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, TimeServerState{Enabled: true, State: "serving", Port: 123}, body)
	require.Equal(t, []authz.Object{linkKindObject(t)}, grants.objects,
		"the read is decided on the link resource kind")
}

func TestAViewerMayNotSwitchTheTimeServer(t *testing.T) {
	t.Parallel()

	server := &fakeTimeServer{}
	rec := call(t, timeServerHandler(t, &orgRoleGrants{}, "user:viewer", server),
		http.MethodPut, timeServerPath, `{"enabled": true}`)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Zero(t, server.sets, "a refused caller changed the switch")
}

func TestAnAdministratorSwitchesTheTimeServer(t *testing.T) {
	t.Parallel()

	server := &fakeTimeServer{}
	handler := timeServerHandler(t, &orgRoleGrants{admin: true}, "user:admin", server)

	rec := call(t, handler, http.MethodPut, timeServerPath, `{"enabled": true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body TimeServerState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Enabled)
	require.Equal(t, "serving", body.State)

	rec = call(t, handler, http.MethodPut, timeServerPath, `{"enabled": false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Enabled)
	require.Equal(t, 2, server.sets)
}

func TestAServerWithNoTimeServerReportsOffAndRefusesAChange(t *testing.T) {
	t.Parallel()

	handler := timeServerHandler(t, &orgRoleGrants{admin: true}, "user:admin", nil)

	rec := call(t, handler, http.MethodGet, timeServerPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"enabled": false, "state": "off", "port": 123}`, rec.Body.String())

	rec = call(t, handler, http.MethodPut, timeServerPath, `{"enabled": true}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
