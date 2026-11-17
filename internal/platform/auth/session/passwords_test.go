package session_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

const (
	owner        = "owner@example.com"
	ownerID      = "operator"
	ownerSecret  = "correct horse battery"
	changedToNew = "a new password entirely"
)

func newCredentialLogin(t *testing.T) (*session.Passwords, *local.Accounts, *auth.MemoryStore) {
	t.Helper()

	accounts := local.New(local.NewMemoryStore(), nil)

	_, err := accounts.CreateFirst(t.Context(), ownerID, local.NewAccount{
		Username: owner,
		Password: []byte(ownerSecret),
	})
	require.NoError(t, err)

	store := auth.NewMemoryStore()

	return session.NewPasswords(accounts, store, nil), accounts, store
}

// TestACredentialLoginEndsInASessionThisServerIssued is the path a self-hoster
// takes. The provider says who the caller is and issues nothing; the credential
// is this server's, of the kind a browser carries.
func TestACredentialLoginEndsInASessionThisServerIssued(t *testing.T) {
	t.Parallel()

	login, _, store := newCredentialLogin(t)

	result, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)
	require.Equal(t, ownerID, result.Credential.Subject)
	require.Equal(t, auth.KindSession, result.Credential.Kind)
	require.NotEmpty(t, result.Token)

	verified, err := auth.NewVerifier(store, nil).Verify(t.Context(), result.Token, auth.TransportHTTP)
	require.NoError(t, err)
	require.Equal(t, ownerID, verified.Subject)
}

// The cross-transport rule covers the credential login too. A session
// credential belongs to a browser, and the rule belongs to the kind, not to the
// login that minted it.
func TestAnIssuedSessionIsRefusedOnTheBrokerTransport(t *testing.T) {
	t.Parallel()

	login, _, store := newCredentialLogin(t)

	result, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)

	_, err = auth.NewVerifier(store, nil).Verify(t.Context(), result.Token, auth.TransportMQTT)
	require.True(t, auth.Refused(err), "a session credential opened the broker")
}

// TestARefusedCredentialLoginIssuesNothing is the fail-closed half. A refused
// login must leave no row: a credential store that grew on a wrong password
// would be a way to fill it.
func TestARefusedCredentialLoginIssuesNothing(t *testing.T) {
	t.Parallel()

	login, _, store := newCredentialLogin(t)

	cases := map[string]struct{ username, password string }{
		"wrong password": {username: owner, password: "not the password"},
		"unknown user":   {username: "nobody@example.com", password: ownerSecret},
		"empty pair":     {username: "", password: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := login.Login(t.Context(), tc.username, tc.password)
			require.True(t, auth.Refused(err), "the refusal is not this package's: %v", err)
		})
	}

	require.Empty(t, store.Rows(), "a refused login left a credential behind")
}

// TestAChangedPasswordEndsEverySessionUnderTheOldOne is the attack. A person
// who changes their password because somebody else has it must not leave that
// somebody a live cookie, and the cookie's own window is a week.
func TestAChangedPasswordEndsEverySessionUnderTheOldOne(t *testing.T) {
	t.Parallel()

	login, accounts, store := newCredentialLogin(t)
	verifier := auth.NewVerifier(store, nil)

	// Two sessions, because a change that ended only the caller's own would
	// leave the browser the password was taken from still logged in.
	first, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)

	second, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)

	require.NoError(t, accounts.ChangePassword(t.Context(), ownerID,
		[]byte(ownerSecret), []byte(changedToNew)))

	removed, err := verifier.RevokeSubject(t.Context(), ownerID)
	require.NoError(t, err)
	require.Equal(t, 2, removed, "the change ended %d of two sessions", removed)

	for name, token := range map[string]auth.Token{"first": first.Token, "second": second.Token} {
		_, verifyErr := verifier.Verify(t.Context(), token, auth.TransportHTTP)
		require.True(t, auth.Refused(verifyErr), "the %s session survived the password change", name)
	}

	// The new password logs in, so the change did not lock the person out of
	// their own account along with everybody else.
	_, err = login.Login(t.Context(), owner, changedToNew)
	require.NoError(t, err)
}

// TestRevokingOneSubjectLeavesAnotherAlone is the other half of that rule. A
// password change must not log the rest of the workshop out.
func TestRevokingOneSubjectLeavesAnotherAlone(t *testing.T) {
	t.Parallel()

	login, accounts, store := newCredentialLogin(t)
	verifier := auth.NewVerifier(store, nil)

	colleague, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "colleague@example.com",
		Password: []byte("another good password"),
	})
	require.NoError(t, err)

	theirs, err := login.Login(t.Context(), "colleague@example.com", "another good password")
	require.NoError(t, err)

	mine, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)

	removed, err := verifier.RevokeSubject(t.Context(), ownerID)
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	_, err = verifier.Verify(t.Context(), mine.Token, auth.TransportHTTP)
	require.True(t, auth.Refused(err), "the revoked subject's session survived")

	kept, err := verifier.Verify(t.Context(), theirs.Token, auth.TransportHTTP)
	require.NoError(t, err, "revoking one subject ended another subject's session")
	require.Equal(t, colleague.ID, kept.Subject)
}

// TestARedirectProviderServesNoCredentialLogin is what a deployment with an
// OIDC issuer answers here. The refusal is the same one a wrong password gets,
// so a caller cannot read the deployment's shape out of it.
func TestARedirectProviderServesNoCredentialLogin(t *testing.T) {
	t.Parallel()

	login := session.NewPasswords(redirectOnly{}, auth.NewMemoryStore(), nil)

	_, err := login.Login(t.Context(), owner, ownerSecret)
	require.True(t, auth.Refused(err))
}

// TestAProviderThatDidNotAdvertiseCredentialsIsNotAskedForOne holds the
// capability check. A provider whose Capabilities say redirect and whose
// VerifyCredentials answers anyway is one this server must not log anybody in
// through: the advertised flow is what the login screen and the host agree on,
// and a provider that answers a flow it did not advertise is answering a
// question nobody asked it.
func TestAProviderThatDidNotAdvertiseCredentialsIsNotAskedForOne(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	login := session.NewPasswords(undeclared{}, store, nil)

	_, err := login.Login(t.Context(), owner, ownerSecret)
	require.True(t, auth.Refused(err), "a provider that advertises no credential flow logged somebody in")
	require.Empty(t, store.Rows(), "a credential was minted through an unadvertised flow")
}

// A server built without a credential provider refuses. It does not panic and
// it does not admit.
func TestNoProviderRefusesEveryCredentialLogin(t *testing.T) {
	t.Parallel()

	login := session.NewPasswords(nil, auth.NewMemoryStore(), nil)

	_, err := login.Login(t.Context(), owner, ownerSecret)
	require.True(t, auth.Refused(err))
}

// TestAProviderNamingNobodyIssuesNothing is the forged-identity door. A
// provider that returned no error and no subject would otherwise mint a
// credential for the empty subject, which the graph would then read as a
// principal.
func TestAProviderNamingNobodyIssuesNothing(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	login := session.NewPasswords(anonymous{}, store, nil)

	_, err := login.Login(t.Context(), owner, ownerSecret)
	require.True(t, auth.Refused(err))
	require.Empty(t, store.Rows(), "a provider that named nobody was issued a credential")
}

// redirectOnly is a provider that authenticates by redirect, the way an OIDC
// adapter does.
type redirectOnly struct{}

var _ authapi.IdentityProvider = redirectOnly{}

func (redirectOnly) Name() string { return "redirect-only" }

func (redirectOnly) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Redirect: true}
}

func (redirectOnly) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

func (redirectOnly) BeginLogin(context.Context, string, string) (string, string, error) {
	return "https://issuer/authorize", "", nil
}

func (redirectOnly) CompleteLogin(context.Context, url.Values, string) (authapi.Identity, error) {
	return authapi.Identity{Subject: ownerID}, nil
}

func (redirectOnly) RefreshIdentity(context.Context, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// undeclared advertises the redirect flow and answers a credential login
// anyway.
type undeclared struct{ redirectOnly }

func (undeclared) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{Subject: ownerID}, nil
}

// anonymous is a provider that succeeds and names nobody.
type anonymous struct{ redirectOnly }

func (anonymous) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Credentials: true}
}

func (anonymous) VerifyCredentials(context.Context, string, string) (authapi.Identity, error) {
	return authapi.Identity{}, nil
}

// TestASessionCarriesTheGenerationOfThePasswordItChecked closes the race with a
// password change. The change lands during the check, and the session is minted
// at the generation read with the old password, so it is refused.
func TestASessionCarriesTheGenerationOfThePasswordItChecked(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	provider := &changedDuringCheck{store: store}

	login := session.NewPasswords(provider, store, nil)

	result, err := login.Login(t.Context(), owner, ownerSecret)
	require.NoError(t, err)
	require.Zero(t, result.Credential.Generation)

	_, err = auth.NewVerifier(store, nil).Verify(t.Context(), result.Token, auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a session checked against the old password was accepted")
}

// changedDuringCheck is a provider whose password changes while it checks the
// old one. It reports the generation it read with the old password.
type changedDuringCheck struct {
	redirectOnly

	store *auth.MemoryStore
}

func (*changedDuringCheck) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Credentials: true}
}

func (c *changedDuringCheck) VerifyCredentialsAtGeneration(
	context.Context, string, string,
) (authapi.Identity, int64, error) {
	c.store.Bump(ownerID)

	return authapi.Identity{Subject: ownerID}, 0, nil
}
