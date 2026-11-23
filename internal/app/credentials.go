package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// errNotAPrincipal is the cause behind refusing an api credential row that
// names a position in the graph and not a holder. It reaches a log line and
// never a client.
var errNotAPrincipal = errors.New("app: the credential names no principal")

// apiCredentials is the api credential surface as the HTTP layer consumes it,
// over the same store and verifier the sessions run on. It is in this package
// for the reason sessions is: the transport may not import the platform group
// and the platform may not name the transport's types.
//
// The subject a row carries is the graph subject in full, user:<id> or
// service_account:<id>, which is what credentialPrincipals reads back. A
// session row carries a bare id instead, and the kind of the row says which
// spelling to expect.
type apiCredentials struct {
	store    auth.Store
	verifier *auth.Verifier
	now      func() time.Time
	// resolve is the resolver every request goes through, so a sign-in with a
	// token another issuer signed is read the way the request was admitted.
	// Nil reads this server's own credentials only.
	resolve func(r *http.Request) (auth.Credential, error)
}

var _ httpapi.Credentials = apiCredentials{}

// Issue mints a named api credential and hands back the token, once. It is
// saved at the generations the caller read before its check, when it says.
func (c apiCredentials) Issue(ctx context.Context, spec httpapi.NewCredential) (httpapi.IssuedCredential, error) {
	var fence *auth.Fence
	if spec.Fence != nil {
		fence = &auth.Fence{Subject: spec.Fence.Subject, Minter: spec.Fence.Minter, Root: spec.Fence.Root}
	}

	token, cred, err := auth.IssueAPI(ctx, c.store, auth.APISpec{
		Subject:     spec.Subject,
		Name:        spec.Name,
		MintedBy:    spec.MintedBy,
		RootCreator: spec.RootCreator,
		ExpiresAt:   spec.ExpiresAt,
		Fence:       fence,
		Scopes:      spec.Scopes,
		Origin:      spec.Origin,
	}, c.now())
	if err != nil {
		return httpapi.IssuedCredential{}, credentialRefusal(err, spec)
	}

	return httpapi.IssuedCredential{
		APICredential: apiCredentialOf(cred),
		Token:         string(token),
	}, nil
}

// SignedIn reports whether the request carries a session, the credential a
// person receives by signing in. It reads the token the way the principal was
// resolved, so a request with a bearer header is judged by that header. A
// token another issuer signed is not a session either.
//
// A verified Prusa Account token that a pairing names a person with counts too.
// It is how a person with no password on this server signs in, and they must
// still be able to administer it. An api credential never counts.
func (c apiCredentials) SignedIn(r *http.Request) bool {
	if r == nil {
		return false
	}

	var (
		cred auth.Credential
		err  error
	)

	if c.resolve != nil {
		cred, err = c.resolve(r)
	} else {
		token := session.TokenFrom(r)
		if token == "" {
			return false
		}

		cred, err = c.verifier.Verify(r.Context(), token, auth.TransportHTTP)
	}

	if err != nil {
		return false
	}

	switch cred.Kind {
	case auth.KindSession:
		return true
	case auth.KindPrusaAccount:
		return pairedToAPerson(cred.Subject)
	case auth.KindUnknown, auth.KindAPI, auth.KindMQTT:
		return false
	default:
		return false
	}
}

// pairedToAPerson reports whether a Prusa Account token acts as a person. A
// token paired to a service account is never a sign-in. With a stolen
// session an attacker could pair their own account to a service account and
// use it to write more pairings and mint tokens in that account's name, which
// the person's password change could not find.
func pairedToAPerson(principal string) bool {
	subject, err := authz.ParseSubject(principal)

	return err == nil && !subject.IsUserset() && subject.Type() == humanSubjectType
}

// Root names the person at the top of the chain the request's credential came
// through. A session is its own person's root. A Prusa Account token carries
// its pairing's root, and an api credential its own, falling back to its
// minter and then its subject for a row an older build wrote.
func (c apiCredentials) Root(r *http.Request) string {
	cred, ok := c.credentialOf(r)
	if !ok {
		return ""
	}

	switch cred.Kind {
	case auth.KindSession:
		return subjectOf(cred.Subject)
	case auth.KindPrusaAccount:
		return cred.RootCreator
	case auth.KindAPI:
		switch {
		case cred.RootCreator != "":
			return cred.RootCreator
		case cred.MintedBy != "":
			return cred.MintedBy
		default:
			return cred.Subject
		}
	case auth.KindUnknown, auth.KindMQTT:
		return ""
	default:
		return ""
	}
}

// credentialOf resolves the credential a request carries, the way the request
// was admitted.
func (c apiCredentials) credentialOf(r *http.Request) (auth.Credential, bool) {
	if r == nil {
		return auth.Credential{}, false
	}

	if c.resolve != nil {
		cred, err := c.resolve(r)

		return cred, err == nil
	}

	token := session.TokenFrom(r)
	if token == "" {
		return auth.Credential{}, false
	}

	cred, err := c.verifier.Verify(r.Context(), token, auth.TransportHTTP)

	return cred, err == nil
}

// Presented names the api credential the request carries, empty for a session
// or any other kind.
func (c apiCredentials) Presented(r *http.Request) string {
	cred, ok := c.credentialOf(r)
	if !ok || cred.Kind != auth.KindAPI {
		return ""
	}

	return cred.PublicID
}

// Fence reads the current generations of the subject, the minter and the root.
func (c apiCredentials) Fence(
	ctx context.Context, subject, minter, root string,
) (httpapi.CredentialFence, error) {
	subjectGeneration, err := c.store.Generation(ctx, subject)
	if err != nil {
		return httpapi.CredentialFence{}, fmt.Errorf("read the generation of %q: %w", subject, err)
	}

	minterGeneration, err := c.store.Generation(ctx, minter)
	if err != nil {
		return httpapi.CredentialFence{}, fmt.Errorf("read the generation of %q: %w", minter, err)
	}

	rootGeneration, err := c.store.Generation(ctx, root)
	if err != nil {
		return httpapi.CredentialFence{}, fmt.Errorf("read the generation of %q: %w", root, err)
	}

	return httpapi.CredentialFence{Subject: subjectGeneration, Minter: minterGeneration, Root: rootGeneration}, nil
}

// List returns the api credentials one subject holds. The store lists every
// kind and the sessions are dropped here: a session is ended by logging out,
// and a listing that offered it for revocation would be a second logout with a
// different name.
func (c apiCredentials) List(ctx context.Context, subject string) ([]httpapi.APICredential, error) {
	held, err := c.store.ListSubject(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("list the credentials of %q: %w", subject, err)
	}

	items := make([]httpapi.APICredential, 0, len(held))

	for _, cred := range held {
		// A page session stands in for an app pairing credential the listing
		// already shows (ADR-20261112-nay).
		if cred.Kind == auth.KindAPI && cred.Parent == "" {
			items = append(items, apiCredentialOf(cred))
		}
	}

	return items, nil
}

// Get returns one api credential. A row of another kind is unknown here for
// the reason List drops it.
func (c apiCredentials) Get(ctx context.Context, id string) (httpapi.APICredential, error) {
	cred, err := c.store.Lookup(ctx, id)
	if errors.Is(err, auth.ErrNoCredential) {
		return httpapi.APICredential{}, httpapi.UnknownCredential(id)
	}

	if err != nil {
		return httpapi.APICredential{}, fmt.Errorf("find the credential %q: %w", id, err)
	}

	if cred.Kind != auth.KindAPI || cred.Parent != "" {
		return httpapi.APICredential{}, httpapi.UnknownCredential(id)
	}

	return apiCredentialOf(cred), nil
}

// Revoke deletes one api credential. The surface has already asked Get whose
// it is, so an id that has gone since is the same answer: the caller wanted it
// gone and it is.
func (c apiCredentials) Revoke(ctx context.Context, id string) error {
	return c.verifier.Revoke(ctx, id)
}

// apiCredentialOf renders a row without its digest. LastUsedAt is absent for a
// credential nobody has presented, which IssueAPI marks with a zero LastSeen.
func apiCredentialOf(cred auth.Credential) httpapi.APICredential {
	out := httpapi.APICredential{
		ID:        cred.PublicID,
		Name:      cred.Name,
		Subject:   cred.Subject,
		CreatedAt: cred.IssuedAt,
		ExpiresAt: cred.ExpiresAt,
		Scopes:    append([]string{}, cred.Scopes...),
		Origin:    cred.Origin,
	}

	if !cred.LastSeen.IsZero() {
		seen := cred.LastSeen
		out.LastUsedAt = &seen
	}

	return out
}

// credentialRefusal turns what the platform refused into the fault the caller
// is told. An expiry in the past is the one refusal a caller can act on; a
// store that could not save is this server's failure and stays an error.
func credentialRefusal(err error, spec httpapi.NewCredential) error {
	if errors.Is(err, auth.ErrExpiryTooFar) {
		return httpfault.RequestInvalid("body.expires_at",
			fmt.Sprintf("at most %d days from now", auth.APIMaxLifetimeDays),
			"a leaked token that nobody revokes has to stop working within a year")
	}

	if errors.Is(err, auth.ErrExpiryPassed) {
		return fault.AuthCredentialExpired.New("the requested expiry is not in the future").With(
			fault.StringParam("expires_at", spec.ExpiresAt.UTC().Format(time.RFC3339)))
	}

	return fmt.Errorf("mint an api credential for %q: %w", spec.Subject, err)
}

// credentialPrincipals renders whatever credential authenticated a caller as
// the subject the authorization graph is keyed by, by the kind of the row.
//
// A session names a bare id the issuer or the account store minted, and the
// human type is stamped on it here, as graphPrincipals does. An api credential
// names the graph subject it was minted for, in full, because it may speak for
// a service account, and a bearer token another issuer signed names the graph
// subject its pairing wrote, for the same reason. The kind is a property of the
// stored row, or of the pairing, and never of the token text, so a holder
// cannot choose which reading they get.
type credentialPrincipals struct {
	inner *session.Principals
}

var _ httpapi.Principals = credentialPrincipals{}

// Principal resolves the caller and renders them as a graph subject. Every
// refusal is auth's one refusal, worded the same way.
func (p credentialPrincipals) Principal(r *http.Request) (string, error) {
	caller, err := p.Caller(r)

	return caller.Principal, err
}

// Caller is Principal with the scopes an api credential is limited to. A
// session and a token another issuer signed carry none. A stored scope this
// build does not know refuses the credential, so a row can never read as
// allowing more than it says.
func (p credentialPrincipals) Caller(r *http.Request) (httpapi.Caller, error) {
	cred, err := p.inner.Resolve(r)
	if errors.Is(err, auth.ErrNoToken) {
		return httpapi.Caller{}, fmt.Errorf("%w: %w", httpapi.ErrNoCredential, err)
	}

	if err != nil {
		return httpapi.Caller{}, err
	}

	var scopes []authz.Scope

	if cred.Kind == auth.KindAPI {
		scopes, err = authz.ParseScopes(cred.Scopes)
		if err != nil {
			return httpapi.Caller{}, auth.Refuse(fmt.Errorf("the api credential %q names scopes "+
				"this server does not read: %w", cred.PublicID, err))
		}
	}

	if cred.Kind.NamesGraphSubject() {
		subject, parseErr := authz.ParseSubject(cred.Subject)
		if parseErr != nil {
			return httpapi.Caller{}, auth.Refuse(fmt.Errorf("the %s credential %q names a subject the "+
				"authorization graph cannot key on: %w", cred.Kind, cred.PublicID, parseErr))
		}

		if subject.IsUserset() {
			return httpapi.Caller{}, auth.Refuse(fmt.Errorf("%w: the %s credential %q names a userset",
				errNotAPrincipal, cred.Kind, cred.PublicID))
		}

		return httpapi.Caller{Principal: subject.String(), Scopes: scopes}, nil
	}

	subject, err := authz.NewSubject(humanSubjectType, cred.Subject)
	if err != nil {
		return httpapi.Caller{}, auth.Refuse(fmt.Errorf("the identity provider named a subject the "+
			"authorization graph cannot key on: %w", err))
	}

	return httpapi.Caller{Principal: subject.String(), Scopes: scopes}, nil
}
