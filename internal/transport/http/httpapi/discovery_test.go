package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fakeDiscovery is the listener and the announcer as the routes see them.
type fakeDiscovery struct {
	mu     sync.Mutex
	nearby []NearbyPrinter
	hidden bool
	sets   int
	// described is what the announcer says about this server besides the
	// setting and the state.
	described Visibility
}

func (f *fakeDiscovery) Nearby(context.Context) []NearbyPrinter {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.nearby
}

func (f *fakeDiscovery) Visibility(context.Context) (Visibility, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.visibility(), nil
}

func (f *fakeDiscovery) SetHidden(_ context.Context, hidden bool) (Visibility, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.hidden = hidden
	f.sets++

	return f.visibility(), nil
}

func (f *fakeDiscovery) visibility() Visibility {
	out := f.described
	out.Hidden, out.Announcement = f.hidden, "announcing"

	if f.hidden {
		out.Announcement = "hidden"
	}

	return out
}

// orgRoleGrants answers the graph's question the way one org role does: a viewer
// holds view_resource on every resource kind, and an administrator holds
// configure_resource as well. It records the objects it was asked about.
type orgRoleGrants struct {
	fakeGrantStore

	admin bool

	mu      sync.Mutex
	objects []authz.Object
}

func (g *orgRoleGrants) Can(
	_ context.Context, _ authz.Subject, action authz.Action, object authz.Object,
) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.objects = append(g.objects, object)

	if action == authz.ActionViewResource {
		return true, nil
	}

	return action == authz.ActionConfigureResource && g.admin, nil
}

func discoveryServer(t *testing.T, grants GrantStore, principal string, discovery Discovery) http.Handler {
	t.Helper()

	return discoveryServerWith(t, grants, principal, discovery, &fakeReadiness{report: readyReport()})
}

func discoveryServerWith(
	t *testing.T, grants GrantStore, principal string, discovery Discovery, readiness Readiness,
) http.Handler {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(principal),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  readiness,
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
		Discovery:  discovery,
	})
	require.NoError(t, err)

	return server.Handler()
}

func TestAViewerReadsThePrintersNearby(t *testing.T) {
	t.Parallel()

	discovery := &fakeDiscovery{nearby: []NearbyPrinter{{
		Name: "Desk MK4", Address: "192.168.1.20", Port: 80,
		Services: []string{"_prusalink._tcp"}, URL: "http://192.168.1.20:80/",
	}}}
	grants := &orgRoleGrants{}

	rec := call(t, discoveryServer(t, grants, "user:viewer", discovery), http.MethodGet, nearbyPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []NearbyPrinter `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, discovery.nearby, body.Items)

	require.Equal(t, []authz.Object{linkKindObject(t)}, grants.objects,
		"the list is decided on the link resource kind")
}

func TestAServerHearingNobodyAnswersAnEmptyList(t *testing.T) {
	t.Parallel()

	for name, discovery := range map[string]Discovery{
		"a listener that heard nothing": &fakeDiscovery{},
		"a build with no listener":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := call(
				t,
				discoveryServer(t, &orgRoleGrants{}, "user:viewer", discovery),
				http.MethodGet,
				nearbyPath,
				"",
			)
			require.Equal(t, http.StatusOK, rec.Code)
			require.JSONEq(t, `{"items": []}`, rec.Body.String())
		})
	}
}

// observingGrants is an org viewer who also observes the printers it names.
type observingGrants struct {
	orgRoleGrants

	observes map[string]bool
}

func (g *observingGrants) Can(
	ctx context.Context, who authz.Subject, action authz.Action, object authz.Object,
) (bool, error) {
	if action == authz.ActionObserve {
		return object.Type() == authz.TypeDevice && g.observes[object.ID()], nil
	}

	return g.orgRoleGrants.Can(ctx, who, action, object)
}

// TestAnEntryNamesItsPrinterOnlyToSomebodyWhoObservesIt is both directions of
// the device id on the list. A viewer who observes the printer reads which one
// it is; a viewer who does not still reads the entry, and nothing names the
// printer.
func TestAnEntryNamesItsPrinterOnlyToSomebodyWhoObservesIt(t *testing.T) {
	t.Parallel()

	discovery := &fakeDiscovery{nearby: []NearbyPrinter{
		{Name: "Desk MK4", Address: "192.168.1.20", Port: 80, DeviceID: "dev-seen"},
		{Name: "Lab XL", Address: "192.168.1.21", Port: 80, DeviceID: "dev-unseen"},
		{Name: "New MINI", Address: "192.168.1.22", Port: 80},
	}}
	grants := &observingGrants{observes: map[string]bool{"dev-seen": true}}

	rec := call(t, discoveryServer(t, grants, "user:viewer", discovery), http.MethodGet, nearbyPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []NearbyPrinter `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 3, "an entry was dropped, and the count says what was withheld")
	require.Equal(t, "dev-seen", body.Items[0].DeviceID, "the observed printer is not named")
	require.Empty(t, body.Items[1].DeviceID, "a printer the caller does not observe is named")
	require.Empty(t, body.Items[2].DeviceID)
	require.NotContains(t, rec.Body.String(), "dev-unseen")
}

// TestTheDiscoveryRoutesRefuseACallerWithNoCredential is the stranger on the
// network: every discovery route answers 401 and says nothing about the
// printers nearby.
func TestTheDiscoveryRoutesRefuseACallerWithNoCredential(t *testing.T) {
	t.Parallel()

	discovery := &fakeDiscovery{nearby: []NearbyPrinter{{Name: "Secret"}}}

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, nearbyPath, ""},
		{http.MethodGet, visibilityPath, ""},
		{http.MethodPut, visibilityPath, `{"hidden": true}`},
	} {
		rec := call(
			t,
			discoveryServer(t, &orgRoleGrants{admin: true}, "", discovery),
			route.method,
			route.path,
			route.body,
		)

		body := requireProblem(t, rec, http.StatusUnauthorized)
		require.Equal(t, fault.AuthTokenInvalid.Code(), body.Code, route.method+" "+route.path)
		require.NotContains(t, rec.Body.String(), "Secret")
	}

	require.Zero(t, discovery.sets)
}

// TestACallerWithNoRoleIsRefusedTheList is a signed-in person the graph grants
// nothing.
func TestACallerWithNoRoleIsRefusedTheList(t *testing.T) {
	t.Parallel()

	discovery := &fakeDiscovery{nearby: []NearbyPrinter{{Name: "Secret"}}}

	for _, path := range []string{nearbyPath, visibilityPath} {
		rec := call(t, discoveryServer(t, &fakeGrantStore{}, "user:nobody", discovery), http.MethodGet, path, "")

		body := requireProblem(t, rec, http.StatusForbidden)
		require.Equal(t, authz.CodeAccessDenied(), body.Code)
		require.NotContains(t, rec.Body.String(), "Secret")
	}
}

func TestAViewerReadsTheHiddenSetting(t *testing.T) {
	t.Parallel()

	rec := call(t, discoveryServer(t, &orgRoleGrants{}, "user:viewer", &fakeDiscovery{hidden: true}),
		http.MethodGet, visibilityPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"hidden": true, "announcement": "hidden", "bridged": false}`, rec.Body.String())
}

// TestOnlyAnAdministratorHidesTheServer is the setting in both directions: a
// viewer is refused and nothing changes, and an administrator's change is
// applied and answered.
func TestOnlyAnAdministratorHidesTheServer(t *testing.T) {
	t.Parallel()

	t.Run("a viewer", func(t *testing.T) {
		t.Parallel()

		discovery := &fakeDiscovery{}

		rec := call(t, discoveryServer(t, &orgRoleGrants{}, "user:viewer", discovery),
			http.MethodPut, visibilityPath, `{"hidden": true}`)

		body := requireProblem(t, rec, http.StatusForbidden)
		require.Equal(t, authz.CodeAccessDenied(), body.Code)
		require.Zero(t, discovery.sets)
		require.False(t, discovery.hidden)
	})

	t.Run("an administrator", func(t *testing.T) {
		t.Parallel()

		discovery := &fakeDiscovery{}
		grants := &orgRoleGrants{admin: true}

		rec := call(t, discoveryServer(t, grants, "user:admin", discovery),
			http.MethodPut, visibilityPath, `{"hidden": true}`)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.JSONEq(t, `{"hidden": true, "announcement": "hidden", "bridged": false}`, rec.Body.String())
		require.True(t, discovery.hidden)
		require.Equal(t, []authz.Object{linkKindObject(t)}, grants.objects)
	})
}

func TestTheHiddenSettingRefusesAnUnknownField(t *testing.T) {
	t.Parallel()

	discovery := &fakeDiscovery{}

	rec := call(t, discoveryServer(t, &orgRoleGrants{admin: true}, "user:admin", discovery),
		http.MethodPut, visibilityPath, `{"hidden": true, "everywhere": true}`)

	require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
	require.Zero(t, discovery.sets)
}

// linkKindObject is the object every discovery route is decided on. It is
// written here because the helper beside the developer tools is left out of
// the printer build.
func linkKindObject(t *testing.T) authz.Object {
	t.Helper()

	object, err := authz.NewObject(authz.TypeResourceKind, "link")
	require.NoError(t, err)

	return object
}

// TestTheReadinessAnswerNamesTheAnnouncerOnlyToAViewerOfTheLinkKind holds the
// readiness answer to the rule of GET /api/v1/discovery/visibility. The
// announcer's state says whether this server hides itself, so a caller who
// may not read the hidden setting does not read it there either.
func TestTheReadinessAnswerNamesTheAnnouncerOnlyToAViewerOfTheLinkKind(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		grants    GrantStore
		principal string
		want      string
	}{
		"an org viewer":                   {&orgRoleGrants{}, "user:viewer", "hidden"},
		"a signed-in caller with no role": {&fakeGrantStore{}, "user:nobody", ""},
		"a caller with no credential":     {&orgRoleGrants{}, "", ""},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			report := readyReport()
			report.Discovery = "hidden"

			handler := discoveryServerWith(t, one.grants, one.principal, &fakeDiscovery{},
				&fakeReadiness{report: report})

			rec := call(t, handler, http.MethodGet, readinessPath, "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body ReadinessReport
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, one.want, body.Discovery)

			if one.want == "" {
				require.NotContains(t, rec.Body.String(), `"discovery"`)
			}
		})
	}
}
