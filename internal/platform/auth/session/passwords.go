package session

import (
	"context"
	"errors"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// errNoCredentialFlow is the cause behind a refusal from a provider that
// authenticates by redirect. It reaches a log line and never a client.
var errNoCredentialFlow = errors.New("session: the identity provider serves no credential login")

// Passwords turns a username and password into a session credential this server
// issued.
//
// It is to a credential provider what Login is to a redirect issuer, and it
// ends in the same place: auth.Issue, KindSession, and a cookie. The provider
// is reached through sdk/authapi, so the local account store and a third-party
// LDAP provider arrive here the same way, and neither one issues a token.
type Passwords struct {
	provider authapi.IdentityProvider
	store    auth.Store
	now      func() time.Time
}

// NewPasswords returns a credential login over a provider and a credential
// store. A nil clock means time.Now.
func NewPasswords(provider authapi.IdentityProvider, store auth.Store, now func() time.Time) *Passwords {
	if now == nil {
		now = time.Now
	}

	return &Passwords{provider: provider, store: store, now: now}
}

// Login authenticates a pair and issues a session credential.
//
// Every refusal is auth's one refusal, worded the same way. A provider that
// does not serve credentials, a wrong password and an unknown user all give one
// answer, because a caller can act on none of those differences.
func (p *Passwords) Login(ctx context.Context, username, password string) (Result, error) {
	if p.provider == nil {
		return Result{}, auth.Refuse(errNoCredentialFlow)
	}

	if !p.provider.Capabilities().Credentials {
		return Result{}, auth.Refuse(errNoCredentialFlow)
	}

	identity, generation, err := verifyAtGeneration(ctx, p.provider, p.store, username, password)
	if err != nil {
		return Result{}, auth.Refuse(err)
	}

	// auth.Issue refuses an empty subject, so a provider that returned no error
	// and named nobody mints nothing. There is no second check here, because
	// no test could reach it.

	token, cred, err := auth.IssueAtGeneration(ctx, p.store, identity.Subject, auth.KindSession, p.now(), generation)
	if err != nil {
		return Result{}, auth.Refuse(err)
	}

	return Result{Token: token, Credential: cred}, nil
}

// generationVerifier is a provider that reads the generation of the account's
// credentials in the same read as the password it checks. The local account
// store is one.
type generationVerifier interface {
	VerifyCredentialsAtGeneration(ctx context.Context, username, password string) (authapi.Identity, int64, error)
}

// verifyAtGeneration checks a password and returns the generation the session
// is minted at. A provider that cannot say reads the generation after the
// check. Such a provider keeps no passwords here, so no password change here
// can race it.
func verifyAtGeneration(
	ctx context.Context, provider authapi.IdentityProvider, store auth.Store, username, password string,
) (authapi.Identity, int64, error) {
	if fenced, ok := provider.(generationVerifier); ok {
		return fenced.VerifyCredentialsAtGeneration(ctx, username, password)
	}

	identity, err := provider.VerifyCredentials(ctx, username, password)
	if err != nil {
		return authapi.Identity{}, 0, err
	}

	generation, err := store.Generation(ctx, identity.Subject)
	if err != nil {
		return authapi.Identity{}, 0, err
	}

	return identity, generation, nil
}
