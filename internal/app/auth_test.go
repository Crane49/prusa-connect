package app

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// fixedID is an identity layer that resolves every request to one bare id, the
// way an issuer's `sub` claim arrives.
type fixedID struct {
	id  string
	err error
}

func (f fixedID) Principal(*http.Request) (string, error) { return f.id, f.err }

// TestTheGraphPrefixIsAppliedInTheCompositionRoot is the subject mapping. An
// issuer names `operator` in its own namespace; the authorization graph refuses
// a bare id because two objects of different types may share one, so the type
// is chosen by whoever wired the resolver and applied once, here.
func TestTheGraphPrefixIsAppliedInTheCompositionRoot(t *testing.T) {
	t.Parallel()

	resolved, err := graphPrincipals{inner: fixedID{id: "operator"}}.
		Principal(httptest.NewRequest(http.MethodGet, "/api/v1/principal", nil))

	require.NoError(t, err)
	require.Equal(t, "user:operator", resolved,
		"a bare issuer subject matches no grant, and every surface has to see the same string")
}

// TestASubjectTheGraphCannotKeyOnIsRefused is the other half. An issuer handing
// out `auth0|1234` would otherwise be rendered into a subject that silently
// matches nothing, which reads to an operator as a permission problem.
func TestASubjectTheGraphCannotKeyOnIsRefused(t *testing.T) {
	t.Parallel()

	_, err := graphPrincipals{inner: fixedID{id: "auth0|1234"}}.
		Principal(httptest.NewRequest(http.MethodGet, "/api/v1/principal", nil))

	require.Error(t, err)
	require.True(t, auth.Refused(err), "it reads as any other refusal: %v", err)
}

// TestARefusalFromTheInnerResolverIsPassedThrough keeps the mapping from
// turning a credential refusal into anything else.
func TestARefusalFromTheInnerResolverIsPassedThrough(t *testing.T) {
	t.Parallel()

	refusal := auth.Refuse(errors.New("no credential"))

	_, err := graphPrincipals{inner: fixedID{err: refusal}}.
		Principal(httptest.NewRequest(http.MethodGet, "/api/v1/principal", nil))

	require.ErrorIs(t, err, refusal)
}

// TestAnUnauthenticatedDeploymentStillProducesGraphSubjects is what makes the
// prefix a property of the graph rather than of the login. Both wirings key the
// same way, so a grant written against user:operator holds whichever one this
// process was started with.
func TestAnUnauthenticatedDeploymentStillProducesGraphSubjects(t *testing.T) {
	t.Parallel()

	core := config.Default()

	who, err := buildIdentity(t.Context(), core, testHost(t), identityPlane{},
		newSetupMode(resource.SetupOpen), discard())
	require.NoError(t, err)

	resolved, err := who.principals.Principal(httptest.NewRequest(http.MethodGet, "/", nil))
	require.NoError(t, err)
	require.Equal(t, "user:operator", resolved)
}

// TestADeploymentWithNoIssuerRefusesToBeginALogin is the shape that replaces an
// enabled flag: no provider means a resolver and a ceremony that refuse, never
// a check that does not run.
func TestADeploymentWithNoIssuerRefusesToBeginALogin(t *testing.T) {
	t.Parallel()

	who, err := buildIdentity(t.Context(), config.Default(), testHost(t), identityPlane{},
		newSetupMode(resource.SetupOpen), discard())
	require.NoError(t, err)

	_, err = who.sessions.Begin(t.Context())
	require.True(t, auth.Refused(err), "a login with no issuer is refused, not started: %v", err)

	_, err = who.sessions.Complete(t.Context(), "state", "code")
	require.True(t, auth.Refused(err), "a callback with no issuer is refused: %v", err)

	require.NoError(t, who.sessions.Revoke(t.Context(),
		httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)),
		"there is no session to end, and the browser is still told to drop its cookie")
}

// TestTheCookieNameComesFromThePackageThatMintsCredentials keeps the name the
// callback sets and the name identity is read from as one fact.
func TestTheCookieNameComesFromThePackageThatMintsCredentials(t *testing.T) {
	t.Parallel()

	who, err := buildIdentity(t.Context(), config.Default(), testHost(t), identityPlane{},
		newSetupMode(resource.SetupOpen), discard())
	require.NoError(t, err)

	require.Equal(t, session.CookieName, who.sessions.Cookie())
}

// TestAConfiguredIssuerMustBeReachableAtStartup is why discovery happens here
// and not at the first login: a server that cannot reach its issuer refuses to
// start, rather than answering every login with a refusal an operator reads as
// a credential problem.
func TestAConfiguredIssuerMustBeReachableAtStartup(t *testing.T) {
	t.Parallel()

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unreachable.Close()

	core := config.Default()
	core.Auth = config.Auth{
		Issuer:      unreachable.URL,
		ClientID:    "prusa-connect",
		RedirectURI: "http://127.0.0.1:8080/api/v1/auth/callback",
	}

	_, err := buildIdentity(t.Context(), core, testHost(t), identityPlane{},
		newSetupMode(resource.SetupRequired), discard())
	require.ErrorContains(t, err, "reach the identity provider")
}

// TestTheCookieSecureFlagFollowsTheRedirectScheme pins the one thing that
// decides it. There is no separate switch: the scheme a browser reaches this
// server on is already written down in the redirect URI the issuer is told.
func TestTheCookieSecureFlagFollowsTheRedirectScheme(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		redirect string
		secure   bool
	}{
		"https marks the cookie Secure":       {redirect: "https://pcl.example/api/v1/auth/callback", secure: true},
		"plain http would never send it back": {redirect: "http://127.0.0.1:8080/api/v1/auth/callback"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.secure, config.Auth{RedirectURI: tc.redirect}.Secure())
		})
	}
}

// TestTheApiRefusesToBuildWithoutTheIdentitySeams is the gate under all of it:
// a server that cannot say who is calling, how somebody logs in, or whether it
// is ready does not start.
func TestTheApiRefusesToBuildWithoutTheIdentitySeams(t *testing.T) {
	t.Parallel()

	_, err := httpapi.New(httpapi.Config{})
	require.ErrorIs(t, err, httpapi.ErrNoPrincipals)
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// Caller is Principal with no scope.
func (f fixedID) Caller(r *http.Request) (httpapi.Caller, error) {
	return httpapi.Unscoped(f.Principal(r))
}
