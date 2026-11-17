package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/oidc"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// errNoIssuer is the cause behind a refusal from a deployment with no login
// provider. It reaches a log line and never a client.
var errNoIssuer = errors.New("app: no identity provider is configured")

// humanSubjectType is what an authenticated person is in the authorization
// graph. The wiring picks the type, not the credential. An OIDC login names a
// person, and a machine credential or a federated peer would be keyed under a
// different type from the same bare id.
const humanSubjectType = authz.TypeUser

// identity is everything the HTTP surface needs to know who is calling and how
// somebody becomes a caller.
type identity struct {
	principals  httpapi.Principals
	sessions    httpapi.Sessions
	accounts    httpapi.Accounts
	profiles    httpapi.Profiles
	credentials httpapi.Credentials
	pairings    httpapi.Pairings
	// appPairings is how an app asks a person for a credential. Nil on a
	// deployment that mints no credential.
	appPairings httpapi.AppPairings
	// people is the list of people a module may ask about while it is not
	// serving them, and nil on a deployment that keeps no accounts.
	people *accountPeople
	// secureCookies says whether a browser reaches this server over https, so
	// an https deployment marks the cookie Secure and a plain-http one does not
	// mark a cookie the browser would then never send back.
	secureCookies bool
}

// buildIdentity constructs the identity layer this process runs with.
//
// The branch is over which implementation is wired, never over whether a check
// runs. session.Require has no bypass and neither does auth.Verifier. A
// deployment with no issuer resolves nobody through a credential and refuses to
// begin a login. One with an issuer runs the whole login ceremony. The
// configuration picks who answers, not whether anybody is asked.
//
// plane is the supervision plane the identity instances run on. A bearer token
// another issuer signed, a Prusa Account token, is verified through whichever
// running instance advertises the flow and mapped onto a local principal
// through a pairing an administrator wrote. It is an addition to the login
// this deployment serves and never replaces it: a deployment that
// authenticates nobody admits no bearer token either.
func buildIdentity(
	ctx context.Context, core config.Core, host hostStore,
	plane instanceReader, mode *setupMode, logger *slog.Logger,
) (identity, error) {
	if !core.Auth.Enabled() {
		return buildLocalOrOpenIdentity(ctx, core, host, plane, mode, logger)
	}

	client, err := oidc.New(ctx, oidcConfig(core.Auth))
	if err != nil {
		return identity{}, fmt.Errorf("reach the identity provider at %s: %w", core.Auth.Issuer, err)
	}

	store, err := openCredentials(ctx, host, logger)
	if err != nil {
		return identity{}, err
	}

	verifier := auth.NewVerifier(store, nil)

	pairings, err := openPairings(ctx, host, plane, logger)
	if err != nil {
		return identity{}, err
	}

	subjects, err := dbstore.NewOIDCSubjects(host.db, host.engine)
	if err != nil {
		return identity{}, fmt.Errorf("open the store of people signed in at the provider: %w", err)
	}

	logger.InfoContext(ctx, "identity provider ready",
		"issuer", client.Issuer(), "client_id", core.Auth.ClientID, "redirect_uri", core.Auth.RedirectURI,
		"id_token_algorithms", client.Algorithms())

	if algErr := client.CheckAlgorithms(); algErr != nil {
		logger.ErrorContext(ctx, "every sign-in at the identity provider will be refused", "cause", algErr)
	}

	apps, err := openAppPairings(host, store, logger)
	if err != nil {
		return identity{}, err
	}

	inner := bearerPrincipals(verifier, pairings, plane, logger)

	return identity{
		principals: credentialPrincipals{inner: inner},
		sessions: sessions{
			login:    session.NewLogin(client, store, nil).WithSubjects(subjects),
			verifier: verifier,
			logger:   logger,
		},
		accounts: noAccounts{info: httpapi.AuthInfo{
			Provider: oidcProvider, ProviderName: providerName(core.Auth), Redirect: true,
		}},
		profiles:      oidcProfiles{store: subjects},
		credentials:   apiCredentials{store: store, verifier: verifier, now: time.Now, resolve: inner.Resolve},
		pairings:      subjectPairings{store: pairings, generations: store.Generation, now: time.Now},
		appPairings:   apps,
		secureCookies: core.SecureCookies(),
	}, nil
}

// bearerPrincipals is the resolver every deployment with a login runs: this
// server's own credentials through the verifier, and a token another issuer
// signed through the running identity instance and the pairing table. The
// shape of the token picks the path, and a caller cannot pick it for them.
func bearerPrincipals(
	verifier *auth.Verifier, pairings auth.Pairings, plane instanceReader, logger *slog.Logger,
) *session.Principals {
	return session.NewPrincipals(verifier, logger, httpapi.StreamPaths()...).
		WithBearer(auth.NewBearer(bearerProviders{host: plane}, pairings, nil))
}

// oidcConfig is the OpenID Connect client the configuration names. A client
// secret is passed on when there is one, so a confidential client
// authenticates its code exchange.
func oidcConfig(auth config.Auth) oidc.Config {
	return oidc.Config{
		Issuer:       auth.Issuer,
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientSecret,
		RedirectURI:  auth.RedirectURI,
	}
}

// principalID is the user id core.principal names in the authorization graph.
// With an issuer it is the administrator's subject at that issuer, mapped the
// way a login maps it, so the first grant is written for the id the
// administrator signs in as. Without one it is the id itself.
func principalID(core config.Core) string {
	if core.Auth.Enabled() {
		return oidc.PrincipalID(core.Auth.Issuer, core.Principal)
	}

	return core.Principal
}

// providerName is what the sign-in button calls the issuer: the configured
// name, else the issuer's host name, which a person recognises from the
// address bar they are sent to.
func providerName(cfg config.Auth) string {
	if cfg.ProviderName != "" {
		return cfg.ProviderName
	}

	if parsed, err := url.Parse(cfg.Issuer); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}

	return oidcProvider
}

// oidcProfiles is httpapi.Profiles over the people who signed in at the
// issuer.
type oidcProfiles struct {
	store auth.OIDCSubjects
}

func (p oidcProfiles) Profile(ctx context.Context, id string) (httpapi.Profile, bool, error) {
	found, err := p.store.Get(ctx, id)
	if errors.Is(err, auth.ErrNoOIDCSubject) {
		return httpapi.Profile{}, false, nil
	}

	if err != nil {
		return httpapi.Profile{}, false, err
	}

	return httpapi.Profile{DisplayName: found.DisplayName, Email: found.Email}, true, nil
}

// oidcProvider is what /api/v1/auth/info names on a deployment with an issuer.
// It is the adapter's type as an operator writes it, so a login screen chooses
// its form from the same word the configuration uses.
const oidcProvider = "oidc"

// buildLocalOrOpenIdentity is the deployment with no issuer. The recorded
// first-run decision picks between the three shapes it can take.
//
// An operator who set core.auth.local has already decided, and so has an
// install whose overlay records a mode. An install that has recorded none takes
// the account store anyway, because the first run creates the administrator
// through it. Its resolver follows the decision as it is taken, so recording
// open needs no restart.
func buildLocalOrOpenIdentity(
	ctx context.Context, core config.Core, host hostStore,
	plane instanceReader, mode *setupMode, logger *slog.Logger,
) (identity, error) {
	if core.Auth.Local || mode.get() == resource.SetupRequired {
		return buildLocalIdentity(ctx, core, host, plane, nil, logger)
	}

	if mode.get() == resource.SetupOpen {
		// Without a provider every request is served as one subject. The
		// subject comes from one configuration key, and this line names it at
		// startup so an operator sees it.
		logger.WarnContext(ctx, "no identity provider is configured; every request is served as one subject",
			"principal", subjectOf(core.Principal))

		return identity{
			principals: graphPrincipals{inner: unauthenticated{subject: core.Principal}},
			sessions:   noIssuer{},
			accounts:   noAccounts{},
		}, nil
	}

	return buildLocalIdentity(ctx, core, host, plane, mode, logger)
}

// buildLocalIdentity is the deployment a self-hoster runs: no issuer, and this
// server's own account store behind the login.
//
// The redirect ceremony is wired to refuse, the same way the credential store
// is on a deployment with an issuer. Exactly one provider answers a login, and
// the configuration picks which one. A logout still revokes, because the
// credential it ends is this server's.
//
// undecided is the first-run cell on an install that has recorded no decision,
// and nil on every install that has. With it the resolver follows a decision
// taken while this process runs.
func buildLocalIdentity(
	ctx context.Context, core config.Core, host hostStore,
	plane instanceReader, undecided *setupMode, logger *slog.Logger,
) (identity, error) {
	store, err := openCredentials(ctx, host, logger)
	if err != nil {
		return identity{}, err
	}

	verifier := auth.NewVerifier(store, nil)

	pairings, err := openPairings(ctx, host, plane, logger)
	if err != nil {
		return identity{}, err
	}

	local, err := openAccounts(ctx, core, host, store, verifier, logger)
	if err != nil {
		return identity{}, err
	}

	local.mode = undecided

	apps, err := openAppPairings(host, store, logger)
	if err != nil {
		return identity{}, err
	}

	inner := bearerPrincipals(verifier, pairings, plane, logger)

	var principals httpapi.Principals = credentialPrincipals{inner: inner}

	if undecided != nil {
		principals = modePrincipals{
			mode:     undecided,
			open:     graphPrincipals{inner: unauthenticated{subject: core.Principal}},
			required: principals,
		}
	}

	return identity{
		principals:    principals,
		sessions:      localSessions{verifier: verifier},
		accounts:      local,
		people:        &accountPeople{accounts: local.store, mode: undecided},
		credentials:   apiCredentials{store: store, verifier: verifier, now: time.Now, resolve: inner.Resolve},
		pairings:      subjectPairings{store: pairings, generations: store.Generation, now: time.Now},
		appPairings:   apps,
		secureCookies: core.SecureCookies(),
	}, nil
}

// graphPrincipals renders whatever authenticated a caller as the subject the
// authorization graph is keyed by.
//
// The prefix is applied here, once, and not by the package that verified the
// credential. An issuer names a subject in its own namespace and knows nothing
// of this graph's types; the graph refuses a bare id because two objects of
// different types may share one. Only the composition root knows which
// implementation produced the id, so only it can say which type the id belongs
// to, and doing it here means every surface below sees the same string.
//
// It wraps the deployment that authenticates nobody. A deployment that verifies
// credentials goes through credentialPrincipals, which reads the kind of the
// row to know whether the id is bare or already typed.
type graphPrincipals struct {
	inner httpapi.Principals
}

var _ httpapi.Principals = graphPrincipals{}

// Principal resolves the caller and renders them as a graph subject. An id the
// vocabulary will not take is refused in the same words as a bad credential:
// the caller cannot act on the difference, and an issuer configured to hand out
// ids this server cannot key on is the operator's problem, in the log line.
func (g graphPrincipals) Principal(r *http.Request) (string, error) {
	caller, err := g.Caller(r)

	return caller.Principal, err
}

// Caller resolves the caller, renders them as a graph subject and keeps the
// scopes the inner resolver reported.
func (g graphPrincipals) Caller(r *http.Request) (httpapi.Caller, error) {
	inner, err := g.inner.Caller(r)
	if err != nil {
		return httpapi.Caller{}, err
	}

	subject, err := g.subject(inner.Principal)
	if err != nil {
		return httpapi.Caller{}, err
	}

	return httpapi.Caller{Principal: subject, Scopes: inner.Scopes}, nil
}

// subject renders an id the inner resolver named as the graph subject.
func (g graphPrincipals) subject(id string) (string, error) {
	subject, err := authz.NewSubject(humanSubjectType, id)
	if err != nil {
		return "", auth.Refuse(fmt.Errorf("the identity provider named a subject the "+
			"authorization graph cannot key on: %w", err))
	}

	return subject.String(), nil
}

// subjectOf renders an id the way graphPrincipals will, for the startup line
// that says who an unauthenticated deployment serves every request as.
func subjectOf(id string) string {
	subject, err := authz.NewSubject(humanSubjectType, id)
	if err != nil {
		return id
	}

	return subject.String()
}

// sessions is the login ceremony as the HTTP surface consumes it. It is in this
// package because the transport may not import the platform group and the
// platform group may not name the transport's types. This package is the
// composition root and maps between the two.
type sessions struct {
	login    *session.Login
	verifier *auth.Verifier
	logger   *slog.Logger
}

var _ httpapi.Sessions = sessions{}

// Begin starts a login.
func (s sessions) Begin(ctx context.Context) (httpapi.Redirect, error) {
	redirect, err := s.login.Begin(ctx)
	if err != nil {
		return httpapi.Redirect{}, err
	}

	return httpapi.Redirect{URL: redirect.URL}, nil
}

// Complete finishes a login and hands back the credential it minted. A login
// this server started that fails comes back as httpapi.SignInError with its
// reason, so the browser returns to the sign-in page and the log names the
// cause. A callback for a login it did not start stays the one refusal.
func (s sessions) Complete(ctx context.Context, state, code string) (httpapi.Session, error) {
	result, err := s.login.Complete(ctx, state, code)
	if err != nil {
		if reason, failed := signInReason(err); failed {
			return httpapi.Session{}, &httpapi.SignInError{Reason: reason, Cause: auth.UnwrapCause(err)}
		}

		return httpapi.Session{}, err
	}

	// The subject is logged because core.principal takes it, and this line is
	// where an operator who added an issuer by hand can read it.
	if s.logger != nil {
		s.logger.InfoContext(ctx, "a person signed in at the identity provider",
			"principal", result.Principal, "subject", result.Claims.Subject,
			"id_token_alg", result.Claims.Algorithm,
			"name", result.Profile.DisplayName())

		if result.ProfileErr != nil {
			s.logger.WarnContext(ctx, "the identity provider's userinfo could not be read, so the person "+
				"may be shown by their id", "principal", result.Principal, "cause", result.ProfileErr)
		}
	}

	return httpapi.Session{
		Token:     string(result.Token),
		ExpiresAt: result.Credential.ExpiresAt,
	}, nil
}

// signInReason sorts why a login failed. A state this server did not start,
// or already spent, is not a failed sign-in: it is a forged or replayed
// callback, and it reports false.
func signInReason(err error) (string, bool) {
	switch {
	case errors.Is(err, session.ErrUnknownState):
		return "", false
	case errors.Is(err, session.ErrLoginExpired):
		return httpapi.SignInReasonExpired, true
	case errors.Is(err, oidc.ErrExchangeRefused), errors.Is(err, oidc.ErrNoIDToken):
		return httpapi.SignInReasonProvider, true
	case errors.Is(err, oidc.ErrMalformedJWT), errors.Is(err, oidc.ErrUnsupportedAlgorithm),
		errors.Is(err, oidc.ErrUnknownKey), errors.Is(err, oidc.ErrBadSignature),
		errors.Is(err, oidc.ErrClaimRejected):
		return httpapi.SignInReasonIDToken, true
	case errors.Is(err, oidc.ErrSubject):
		return httpapi.SignInReasonSubject, true
	default:
		return httpapi.SignInReasonFailed, true
	}
}

// Revoke ends the session the request carries.
func (s sessions) Revoke(ctx context.Context, r *http.Request) error {
	return revokeCarried(ctx, s.verifier, r)
}

// revokeCarried ends the credential the request carries. A request carrying
// nothing this server issued is not an error: the caller wanted the session
// gone and there is none, which is the same answer the store gives for a row
// that is not there.
func revokeCarried(ctx context.Context, verifier *auth.Verifier, r *http.Request) error {
	publicID, _, err := auth.Split(session.TokenFrom(r))
	if err != nil {
		//nolint:nilerr // text that is not a token this server issued names no
		// credential to revoke, and reporting that is a refusal for a caller
		// who asked for the one thing already true.
		return nil
	}

	return verifier.Revoke(ctx, publicID)
}

// Cookie is the name the platform package owns, so the name set at the callback
// and the name read when identity is resolved cannot drift apart.
func (s sessions) Cookie() string { return session.CookieName }

// noIssuer is the login ceremony of a deployment that has none. It refuses to
// begin a login and admits nobody, as session.Require does when it is wired
// without a verifier.
type noIssuer struct{}

var _ httpapi.Sessions = noIssuer{}

func (noIssuer) Begin(context.Context) (httpapi.Redirect, error) {
	return httpapi.Redirect{}, auth.Refuse(errNoIssuer)
}

func (noIssuer) Complete(context.Context, string, string) (httpapi.Session, error) {
	return httpapi.Session{}, auth.Refuse(errNoIssuer)
}

// Revoke has nothing to end and says so by succeeding, so a browser holding a
// stale cookie is still told to drop it.
func (noIssuer) Revoke(context.Context, *http.Request) error { return nil }

func (noIssuer) Cookie() string { return session.CookieName }

// localSessions is the login ceremony of a deployment whose own account store
// answers the login. It has no redirect to begin or complete, so those refuse
// as noIssuer does. The credential a login here issued is still this server's
// to end, so a logout revokes it instead of only expiring the cookie.
type localSessions struct {
	noIssuer

	verifier *auth.Verifier
}

var _ httpapi.Sessions = localSessions{}

func (s localSessions) Revoke(ctx context.Context, r *http.Request) error {
	return revokeCarried(ctx, s.verifier, r)
}

// openCredentials is the credential store this process runs with: the rows in
// the host's own database, so a session and an api token outlive the process
// that issued them. With credentials kept in memory, a restart would make
// every operator log in again and would end every script's token.
//
// The expired rows are swept once, here, and not by a goroutine. The verifier
// refuses an expired credential whether or not the row is there. The sweep
// only stops the table from growing by one row per login, and a restart is
// often enough for that.
func openCredentials(ctx context.Context, host hostStore, logger *slog.Logger) (auth.Store, error) {
	store, err := dbstore.NewCredentials(host.db, host.engine)
	if err != nil {
		return nil, fmt.Errorf("open the credential store: %w", err)
	}

	swept, err := store.DeleteExpired(ctx, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("sweep the expired credentials: %w", err)
	}

	if swept > 0 {
		logger.InfoContext(ctx, "expired credentials were swept at startup", "credentials", swept)
	}

	return store, nil
}

// openPairings is the pairing store this process runs with: the rows in the
// host's own database, beside the credentials, so a pairing an administrator
// wrote survives a restart and travels with the graph it names principals in.
// It also says at startup which identity instances will be consulted.
func openPairings(
	ctx context.Context, host hostStore, plane instanceReader, logger *slog.Logger,
) (auth.Pairings, error) {
	pairings, err := dbstore.NewPairings(host.db, host.engine)
	if err != nil {
		return nil, fmt.Errorf("open the pairing store: %w", err)
	}

	announceBearerProviders(ctx, plane, logger)

	return pairings, nil
}

// checkPrincipal refuses a core.principal the first grant cannot be written
// for. With an issuer it is the administrator's subject at the issuer, which
// takes any subject the specification allows and is mapped to an id. Without
// one it is the id itself.
func checkPrincipal(core config.Core) error {
	if core.Auth.Enabled() {
		if err := oidc.CheckSubject(core.Principal); err != nil {
			return httpfault.RequestInvalid("body.configuration.core.principal",
				"the administrator's subject at the provider, at most 255 characters with no spaces", err.Error())
		}

		return nil
	}

	if err := ref.ValidateID(core.Principal); err != nil {
		return httpfault.RequestInvalid("body.configuration.core.principal",
			"a user id of lowercase letters, digits and dashes", err.Error())
	}

	return nil
}
