package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The parts of the HTTP surface this file never reaches. Each embeds its
// interface and implements nothing, so a call to one would fail loudly.
type (
	unusedResources struct{ httpapi.ResourceStore }
	unusedPresets   struct{ httpapi.GCodePresetStore }
	unusedEndpoints struct{ httpapi.EndpointStore }
	unusedReadiness struct{ httpapi.Readiness }
	unusedEvents    struct{ httpapi.EventStream }
	unusedDevices   struct{ httpapi.DeviceFleet }
	unusedStores    struct{ httpapi.Storages }
)

// TestAChainThroughTheHandlersEndsWithItsRootsPasswordChange runs the chain
// over the real handlers, the real credential and pairing stores and the real
// bearer resolver. The owner's session pairs an outside account to bob. With
// bob's token that account mints for a service account and writes a second
// pairing. The owner's password change then refuses both, because both
// recorded the owner as their root, at the owner's generation.
//
// The owner changes their password once first, so the owner's generation and
// bob's differ. A handler that saved bob's generation as the root's, or a store
// that saved any other value, leaves a row this test catches.
func TestAChainThroughTheHandlersEndsWithItsRootsPasswordChange(t *testing.T) {
	t.Parallel()

	host := testHost(t)

	store, err := dbstore.NewCredentials(host.db, host.engine)
	require.NoError(t, err)

	pairingRows, err := dbstore.NewPairings(host.db, host.engine)
	require.NoError(t, err)

	verifier := auth.NewVerifier(store, nil)

	idp := runIdentity(t, "prusa", fakeIdentityPlugin{})
	plane := identityPlane{running: map[ref.Ref]*supervisor.Instance[any]{idp.Ref(): idp}}
	inner := session.NewPrincipals(verifier, discard(), httpapi.StreamPaths()...).
		WithBearer(auth.NewBearer(bearerProviders{host: plane}, pairingRows, func() time.Time { return credentialClock }))

	local, err := openAccounts(t.Context(), localCore(t), host, store, verifier, discard())
	require.NoError(t, err)

	_, err = local.Setup(t.Context(), httpapi.NewAccount{Username: testOwner, Password: testPassword})
	require.NoError(t, err)

	_, err = local.ChangePassword(t.Context(), "operator", testPassword, "a second password")
	require.NoError(t, err)

	owner, err := local.Login(t.Context(), testOwner, "a second password")
	require.NoError(t, err)

	bob, err := local.Create(t.Context(), httpapi.NewAccount{Username: "bob@example.com", Password: "bob's password"})
	require.NoError(t, err)

	operator, err := authz.ParseSubject("user:operator")
	require.NoError(t, err)

	bobSubject, err := authz.ParseSubject("user:" + bob.ID)
	require.NoError(t, err)

	require.NoError(t, host.graph.SetOrgRole(t.Context(), operator, bobSubject, authz.Relation("admin")))

	api, err := httpapi.New(httpapi.Config{
		Version:     "test",
		Logger:      discard(),
		Principals:  credentialPrincipals{inner: inner},
		Sessions:    noIssuer{},
		Accounts:    local,
		Credentials: apiCredentials{store: store, verifier: verifier, now: time.Now, resolve: inner.Resolve},
		Pairings:    subjectPairings{store: pairingRows, generations: store.Generation, now: time.Now},
		Grants:      host.graph,
		Resources:   unusedResources{},
		Presets:     unusedPresets{},
		Endpoints:   unusedEndpoints{},
		Readiness:   unusedReadiness{},
		Events:      unusedEvents{},
		Devices:     unusedDevices{},
		Stores:      unusedStores{},
	})
	require.NoError(t, err)

	served := httptest.NewServer(api.Handler())
	t.Cleanup(served.Close)

	post := func(token, path, body string) (int, string) {
		t.Helper()

		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodPost, served.URL+path,
			strings.NewReader(body))
		require.NoError(t, requestErr)

		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)

		response, doErr := http.DefaultClient.Do(request)
		require.NoError(t, doErr)

		defer func() { _ = response.Body.Close() }()

		read, readErr := io.ReadAll(response.Body)
		require.NoError(t, readErr)

		return response.StatusCode, string(read)
	}

	// The owner's session pairs the outside account to bob.
	status, body := post(owner.Token, "/api/v1/auth/pairings",
		`{"provider":"prusa-account","subject":"`+bearerSubject+`","principal":"user:`+bob.ID+`"}`)
	require.Equal(t, http.StatusCreated, status, body)

	// Bob's paired token mints for a service account and writes a second pairing.
	status, body = post(pairedBearer, "/api/v1/auth/credentials", `{"name":"ci","subject":"service_account:ci"}`)
	require.Equal(t, http.StatusCreated, status, body)

	var issued struct {
		Token string `json:"token"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &issued))

	minted := issued.Token

	status, body = post(pairedBearer, "/api/v1/auth/pairings",
		`{"provider":"prusa-account","subject":"nobody-paired","principal":"service_account:ci"}`)
	require.Equal(t, http.StatusCreated, status, body)

	_, err = verifier.Verify(t.Context(), auth.Token(minted), auth.TransportHTTP)
	require.NoError(t, err, "the token minted down the chain did not work before the change")

	_, err = inner.Resolve(requestBearing(unpairedBearer))
	require.NoError(t, err, "the second pairing did not work before the change")

	_, err = local.ChangePassword(t.Context(), "operator", "a second password", "a third password")
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), auth.Token(minted), auth.TransportHTTP)
	require.True(t, auth.Refused(err), "the token minted down the chain survived the root's change")

	_, err = inner.Resolve(requestBearing(unpairedBearer))
	require.Error(t, err, "the second pairing survived the root's change")
}
