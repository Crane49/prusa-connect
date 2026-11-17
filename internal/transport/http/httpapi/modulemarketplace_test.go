package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func TestSwitchingTheMarketplaceOffReachesTheInstaller(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{market: &ModuleMarketplace{
		URL: "https://plugins.example", BrowseURL: "https://plugins.example/connect", Enabled: true,
	}}

	rec := call(t, serverWithInstaller(t, installer, permittingGrants{&fakeGrantStore{}}).Handler(),
		http.MethodPut, "/api/v1/modules/marketplace", `{"enabled":false}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []bool{false}, installer.switches)
	require.Equal(t, []*bool{nil}, installer.communities, "a body with no community changed the community switch")

	var body ModuleMarketplace

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Enabled)
	require.Equal(t, "https://plugins.example", body.URL)
}

func TestTheCommunitySwitchReachesTheInstaller(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{market: &ModuleMarketplace{URL: "https://plugins.example", Enabled: true}}

	rec := call(t, serverWithInstaller(t, installer, permittingGrants{&fakeGrantStore{}}).Handler(),
		http.MethodPut, "/api/v1/modules/marketplace", `{"enabled":true,"community":true}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, installer.communities, 1)
	require.NotNil(t, installer.communities[0])
	require.True(t, *installer.communities[0])

	var body ModuleMarketplace

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Community)
}

func TestTheMarketplaceIsReadBackWithItsSwitch(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{market: &ModuleMarketplace{URL: "https://plugins.example", Enabled: false}}

	rec := call(t, serverWithInstaller(t, installer, permittingGrants{&fakeGrantStore{}}).Handler(),
		http.MethodGet, "/api/v1/modules/marketplace", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(
		t,
		`{"url":"https://plugins.example","browse_url":"","enabled":false,"community":false,"reachable":false,"stale":false}`,
		rec.Body.String(),
	)
}

// A deployment that names no marketplace has nothing to switch, and says so
// rather than storing a switch that would turn nothing on.
func TestADeploymentWithNoMarketplaceHasNoSwitch(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{}
	handler := serverWithInstaller(t, installer, permittingGrants{&fakeGrantStore{}}).Handler()

	read := call(t, handler, http.MethodGet, "/api/v1/modules/marketplace", "")
	require.Equal(t, http.StatusNotFound, read.Code, read.Body.String())

	written := call(t, handler, http.MethodPut, "/api/v1/modules/marketplace", `{"enabled":true}`)
	require.Equal(t, http.StatusNotFound, written.Code, written.Body.String())
}

// Both directions of the decision: the same request the permitted caller made
// above changes nothing for a caller who administers no resource kind.
func TestACallerWhoAdministersNoKindCannotSwitchTheMarketplace(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{market: &ModuleMarketplace{URL: "https://plugins.example", Enabled: true}}
	handler := serverWithInstaller(t, installer, &fakeGrantStore{}).Handler()

	read := call(t, handler, http.MethodGet, "/api/v1/modules/marketplace", "")
	require.Equal(t, http.StatusForbidden, read.Code, read.Body.String())

	written := call(t, handler, http.MethodPut, "/api/v1/modules/marketplace", `{"enabled":false}`)
	require.Equal(t, http.StatusForbidden, written.Code, written.Body.String())
	require.Empty(t, installer.switches, "the switch was stored for a caller the graph refused")
	require.True(t, installer.market.Enabled)
}

// The switch decides where every administrator's installs come from, so it
// takes the admin role on the organization. A caller who configures every
// resource kind and holds no such role reads the marketplace and cannot switch
// it.
func TestOnlyAnOrganizationAdminSwitchesTheMarketplace(t *testing.T) {
	t.Parallel()

	installer := &recordingInstaller{market: &ModuleMarketplace{URL: "https://plugins.example", Enabled: true}}
	refused := fault.New(authz.CodeAccessDenied(), "the subject holds no admin role on this org")
	handler := serverWithInstaller(t, installer, permittingGrants{&fakeGrantStore{refuse: refused}}).Handler()

	read := call(t, handler, http.MethodGet, "/api/v1/modules/marketplace", "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())

	written := call(t, handler, http.MethodPut, "/api/v1/modules/marketplace", `{"enabled":false}`)
	require.Equal(t, http.StatusForbidden, written.Code, written.Body.String())
	require.Contains(t, written.Body.String(), "org:local")
	require.Empty(t, installer.switches, "the switch was stored for a caller who is no organization admin")
}

func TestACallerWhoAdministersNoKindCannotReadTheModuleMemory(t *testing.T) {
	t.Parallel()

	refused := serverWithInstaller(t, &recordingInstaller{}, &fakeGrantStore{}).Handler()
	require.Equal(t, http.StatusForbidden, call(t, refused, http.MethodGet, "/api/v1/modules/memory", "").Code)

	permitted := serverWithInstaller(t, &recordingInstaller{}, permittingGrants{&fakeGrantStore{}}).Handler()
	read := call(t, permitted, http.MethodGet, "/api/v1/modules/memory", "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.JSONEq(t, `{"pool_bytes":1073741824,"committed_bytes":402653184,"source":"configured"}`, read.Body.String())
}
