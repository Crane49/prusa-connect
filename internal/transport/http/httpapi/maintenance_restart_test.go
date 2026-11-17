package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

type recordingRestarter struct {
	exampleMaintenance

	asked []MaintenanceRestart
}

func (r *recordingRestarter) Restart(_ context.Context, input MaintenanceRestart) error {
	r.asked = append(r.asked, input)

	return nil
}

func serverWithRestarter(t *testing.T, restarter Maintenance, grants GrantStore) http.Handler {
	t.Helper()

	cfg := exampleConfig(&fakeFleet{}, seededStore(), seededPresets())
	cfg.Maintenance = restarter
	cfg.Grants = grants

	server, err := New(cfg)
	require.NoError(t, err)

	return server.Handler()
}

func TestOnlyAnOrganizationAdminRestartsTheServer(t *testing.T) {
	t.Parallel()

	refused := fault.New(authz.CodeAccessDenied(), "the subject holds no admin role on this org")

	denied := &recordingRestarter{}
	rec := call(t, serverWithRestarter(t, denied, permittingGrants{&fakeGrantStore{refuse: refused}}),
		http.MethodPost, "/api/v1/server/restart", `{"mode":"safe"}`)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "org:local")
	require.Empty(t, denied.asked)

	allowed := &recordingRestarter{}
	rec = call(t, serverWithRestarter(t, allowed, permittingGrants{&fakeGrantStore{}}),
		http.MethodPost, "/api/v1/server/restart", `{"mode":"safe"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"mode":"safe"}`, rec.Body.String())
	require.Len(t, allowed.asked, 1)
	require.Equal(t, RestartSafe, allowed.asked[0].Mode)
	require.NotEmpty(t, allowed.asked[0].Principal)
}

func TestRestartServerRefusesAnUnknownMode(t *testing.T) {
	t.Parallel()

	restarter := &recordingRestarter{}
	rec := call(t, serverWithRestarter(t, restarter, permittingGrants{&fakeGrantStore{}}),
		http.MethodPost, "/api/v1/server/restart", `{"mode":"later"}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Empty(t, restarter.asked)
}
