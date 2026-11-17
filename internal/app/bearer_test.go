package app

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The tokens the fake identity plugin knows, shaped like a JWS so the
// principals resolver sends them down the bearer path.
const (
	pairedBearer   = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJwYWlyZWQifQ.c2ln"
	unpairedBearer = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1bnBhaXJlZCJ9.c2ln"
	expiredBearer  = "eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjF9.c2ln"
	bearerSubject  = "8f1e1b2c-0000-4000-8000-abcdefabcdef"
)

// fakeIdentityPlugin is an identity provider with the cryptography taken out.
// It advertises the bearer flow, as the prusa-account adapter does, so the
// wiring from the supervision plane to the pairing table is what is under test.
type fakeIdentityPlugin struct{}

var (
	_ authapi.IdentityProvider = fakeIdentityPlugin{}
	_ authapi.TokenVerifier    = fakeIdentityPlugin{}
)

func (fakeIdentityPlugin) Name() string { return "prusa-account" }
func (fakeIdentityPlugin) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Bearer: true}
}

func (fakeIdentityPlugin) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

func (fakeIdentityPlugin) BeginLogin(context.Context, string, string) (string, string, error) {
	return "", "", authapi.ErrUnsupportedFlow
}

func (fakeIdentityPlugin) CompleteLogin(context.Context, url.Values, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

func (fakeIdentityPlugin) RefreshIdentity(context.Context, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

func (fakeIdentityPlugin) VerifyToken(_ context.Context, token string) (authapi.Identity, error) {
	switch token {
	case pairedBearer:
		return authapi.Identity{Subject: bearerSubject, ExpiresAt: credentialClock.Add(time.Hour).Unix()}, nil
	case unpairedBearer:
		return authapi.Identity{Subject: "nobody-paired", ExpiresAt: credentialClock.Add(time.Hour).Unix()}, nil
	case expiredBearer:
		return authapi.Identity{}, authapi.ErrTokenExpired
	default:
		return authapi.Identity{}, authapi.ErrTokenSignature
	}
}

// identityPlane is the one method of the supervision plane the bearer path reads,
// over instances a test started by hand.
type identityPlane struct {
	running map[ref.Ref]*supervisor.Instance[any]
}

func (p identityPlane) Supervising(kind ref.Kind) map[ref.Ref]*supervisor.Instance[any] {
	out := make(map[ref.Ref]*supervisor.Instance[any])

	for instance, runner := range p.running {
		if instance.Kind() == kind {
			out[instance] = runner
		}
	}

	return out
}

// runIdentity starts one supervised identity instance whose target is the
// fake plugin, the way the resource host builds every runtime, and returns
// once it is serving.
func runIdentity(t *testing.T, name string, target any) *supervisor.Instance[any] {
	t.Helper()

	instance, err := ref.New(ref.KindAuth, name)
	require.NoError(t, err)

	policy := supervisor.DefaultPolicy()
	policy.Backoff.Spread = 0

	running, err := supervisor.New(supervisor.Config[any]{
		Ref:    instance,
		Type:   "core/prusa-account",
		Policy: policy,
		Start:  func(context.Context, supervisor.Handle) (any, error) { return target, nil },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = running.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, running.Serving, 5*time.Second, 10*time.Millisecond, "the identity instance never served")

	return running
}

// bearerFixture is the resolver the composition root wires, over a plane
// running the fake plugin and a pairing table holding one row.
func bearerFixture(t *testing.T, plane instanceReader) (credentialPrincipals, *auth.MemoryPairings) {
	t.Helper()

	pairings := auth.NewMemoryPairings()
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: "prusa-account", Subject: bearerSubject, Principal: "service_account:wall-dashboard",
		Label: "the dashboard", CreatedAt: credentialClock,
	}))

	verifier := auth.NewVerifier(auth.NewMemoryStore(), func() time.Time { return credentialClock })
	inner := session.NewPrincipals(verifier, nil, httpapi.StreamPaths()...).
		WithBearer(auth.NewBearer(bearerProviders{host: plane}, pairings, func() time.Time { return credentialClock }))

	return credentialPrincipals{inner: inner}, pairings
}

// TestAPrusaAccountTokenIsResolvedThroughThePairingOnHTTPAndTheStream is the
// kind over the resolver the composition root wires: the running identity
// instance verifies the token, the pairing names the principal, and the same
// principal comes out on a call and on the stream. The token's own subject
// never becomes the caller.
func TestAPrusaAccountTokenIsResolvedThroughThePairingOnHTTPAndTheStream(t *testing.T) {
	t.Parallel()

	idp := runIdentity(t, "prusa", fakeIdentityPlugin{})
	principals, pairings := bearerFixture(
		t,
		identityPlane{running: map[ref.Ref]*supervisor.Instance[any]{idp.Ref(): idp}},
	)

	overHTTP, err := principals.Principal(requestBearing(pairedBearer))
	require.NoError(t, err)
	require.Equal(t, "service_account:wall-dashboard", overHTTP)

	onStream, err := principals.Principal(requestStreaming(pairedBearer))
	require.NoError(t, err, "a prusa-account token must open the stream")
	require.Equal(t, overHTTP, onStream)

	_, err = principals.Principal(requestStreaming(unpairedBearer))
	require.True(t, fault.HasCode(err, fault.AuthBearerUnpaired.Code()), "an unpaired subject was admitted: %v", err)

	_, err = principals.Principal(requestBearing(expiredBearer))
	require.True(t, fault.HasCode(err, fault.AuthBearerExpired.Code()), "got %v", err)

	_, err = principals.Principal(requestBearing("eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJmb3JnZWQifQ.c2ln"))
	require.True(t, fault.HasCode(err, fault.AuthBearerSignature.Code()), "got %v", err)

	// Unpairing ends the admission on the next request, which is what the
	// stream's re-check runs into.
	require.NoError(t, pairings.Delete(t.Context(), "prusa-account", bearerSubject))

	_, err = principals.Principal(requestStreaming(pairedBearer))
	require.True(t, fault.HasCode(err, fault.AuthBearerUnpaired.Code()), "an unpaired subject kept the stream: %v", err)
}

// TestAPlaneWithNoBearerProviderRefusesTheToken is the deployment that declares
// no identity instance, or one that advertises no bearer flow: the token is
// refused in the plain wording, and nothing was consulted.
func TestAPlaneWithNoBearerProviderRefusesTheToken(t *testing.T) {
	t.Parallel()

	principals, _ := bearerFixture(t, identityPlane{})

	_, err := principals.Principal(requestBearing(pairedBearer))
	require.True(t, auth.Refused(err), "got %v", err)
	require.True(t, fault.HasCode(err, fault.AuthTokenInvalid.Code()))

	_, _, verifyErr := bearerProviders{host: identityPlane{}}.VerifyToken(t.Context(), pairedBearer)
	require.ErrorIs(t, verifyErr, errNoBearerProvider)
}

// TestTheProviderWhoseRefusalIsAboutTheTokenWins: with two providers, the one
// that says "not my issuer" is describing itself, so the other one's answer is
// the one reported.
func TestTheProviderWhoseRefusalIsAboutTheTokenWins(t *testing.T) {
	t.Parallel()

	other := runIdentity(t, "other", otherIssuerPlugin{fakeIdentityPlugin{}})
	ours := runIdentity(t, "prusa", fakeIdentityPlugin{})
	plane := identityPlane{running: map[ref.Ref]*supervisor.Instance[any]{other.Ref(): other, ours.Ref(): ours}}

	_, _, err := bearerProviders{host: plane}.VerifyToken(t.Context(), expiredBearer)
	require.ErrorIs(t, err, authapi.ErrTokenExpired, "the issuer refusal hid the one about the token")

	found, provider, err := bearerProviders{host: plane}.VerifyToken(t.Context(), pairedBearer)
	require.NoError(t, err)
	require.Equal(t, bearerSubject, found.Subject)
	require.Equal(t, "prusa-account", provider)
}

// otherIssuerPlugin refuses every token as another issuer's.
type otherIssuerPlugin struct{ fakeIdentityPlugin }

func (otherIssuerPlugin) VerifyToken(context.Context, string) (authapi.Identity, error) {
	return authapi.Identity{}, errors.Join(authapi.ErrTokenIssuer, errors.New("issued by somebody else"))
}

// TestAPrusaAccountSignInCountsAsASignIn is the administrator who signs in with
// a Prusa Account and holds no password here. A verified token that a pairing
// names a person with is their sign-in, so they can still administer this
// server. An api credential is not a sign-in.
//
// A token paired to a service account is not a sign-in either. With a stolen
// session an attacker could pair their own account to a service account, then
// use that token to write more pairings and mint tokens in the service
// account's name. The person's password change could not find those.
func TestAPrusaAccountSignInCountsAsASignIn(t *testing.T) {
	t.Parallel()

	idp := runIdentity(t, "prusa", fakeIdentityPlugin{})
	principals, pairings := bearerFixture(
		t,
		identityPlane{running: map[ref.Ref]*supervisor.Instance[any]{idp.Ref(): idp}},
	)

	store := auth.NewMemoryStore()
	verifier := auth.NewVerifier(store, func() time.Time { return credentialClock })
	credentials := apiCredentials{
		store: store, verifier: verifier, now: func() time.Time { return credentialClock },
		resolve: principals.inner.Resolve,
	}

	require.False(t, credentials.SignedIn(requestBearing(pairedBearer)),
		"a Prusa Account paired to a service account was a sign-in")

	require.NoError(t, pairings.Delete(t.Context(), "prusa-account", bearerSubject))
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: "prusa-account", Subject: bearerSubject, Principal: "user:operator", CreatedAt: credentialClock,
	}))

	require.True(t, credentials.SignedIn(requestBearing(pairedBearer)),
		"a Prusa Account paired to a person was not a sign-in")
	require.False(t, credentials.SignedIn(requestBearing(unpairedBearer)), "an unpaired token was a sign-in")

	minted, err := credentials.Issue(
		t.Context(),
		httpapi.NewCredential{Scopes: []string{"administer"}, Subject: "user:operator", Name: "script"},
	)
	require.NoError(t, err)
	require.False(t, credentials.SignedIn(requestBearing(auth.Token(minted.Token))), "an api credential was a sign-in")
}

// TestTheRootOfACredentialIsTheTopOfItsChain holds what a mint or a pairing
// records as its root. A session is its own person. A Prusa Account token is
// the root its pairing recorded, which is the person whose session wrote the
// first link, not the person the token acts as.
func TestTheRootOfACredentialIsTheTopOfItsChain(t *testing.T) {
	t.Parallel()

	idp := runIdentity(t, "prusa", fakeIdentityPlugin{})
	principals, pairings := bearerFixture(
		t,
		identityPlane{running: map[ref.Ref]*supervisor.Instance[any]{idp.Ref(): idp}},
	)

	require.NoError(t, pairings.Delete(t.Context(), "prusa-account", bearerSubject))
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: "prusa-account", Subject: bearerSubject, Principal: "user:bob",
		CreatedBy: "user:operator", RootCreator: "user:operator", CreatedAt: credentialClock,
	}))

	store := auth.NewMemoryStore()
	verifier := auth.NewVerifier(store, func() time.Time { return credentialClock })
	credentials := apiCredentials{
		store: store, verifier: verifier, now: func() time.Time { return credentialClock },
		resolve: principals.inner.Resolve,
	}

	require.Equal(t, "user:operator", credentials.Root(requestBearing(pairedBearer)),
		"a paired token did not carry its pairing's root")
	require.Empty(t, credentials.Root(requestBearing(unpairedBearer)))

	session, _, err := auth.Issue(t.Context(), store, "operator", auth.KindSession, credentialClock)
	require.NoError(t, err)

	own := apiCredentials{store: store, verifier: verifier, now: func() time.Time { return credentialClock }}
	require.Equal(t, "user:operator", own.Root(requestBearing(session)), "a session is not its own root")
}
