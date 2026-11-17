// Package authapitest provides a shared conformance suite that every identity
// provider, the built-in local provider and each plugin, must pass. A new
// plugin adds a TestXContract(t) that calls RunIdentityProviderContract.
package authapitest

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// Harness describes a provider under test plus the inputs the suite needs to
// drive a successful authentication for whatever flow(s) the provider supports.
type Harness struct {
	Provider authapi.IdentityProvider
	// ValidUsername / ValidPassword are required when the provider advertises
	// Capabilities.Credentials.
	ValidUsername string
	ValidPassword string
	// ValidCallbackParams, for a Redirect provider, are callback parameters that
	// make CompleteLogin succeed (e.g. url.Values{"code": {"..."}} for OIDC, or
	// {"SAMLResponse": {"..."}} for SAML). When set, the suite drives a full
	// begin→complete happy path and (for revalidating providers) re-checks the
	// resulting subject.
	//
	// A provider that binds one login to values it generated inside BeginLogin,
	// which every OIDC and SAML provider does, cannot be driven from a fixed
	// set. Those set Callback instead.
	ValidCallbackParams url.Values
	// Callback drives the identity provider the way a browser would: it takes
	// the authorize URL BeginLogin just produced, and returns the parameters the
	// provider would send back to the callback for it.
	//
	// It is what a redirect provider that is doing its job needs. A nonce and a
	// PKCE challenge are made inside BeginLogin and travel in the authorize
	// request, so the answer to one login is not the answer to another, and a
	// fixed set of parameters can only be minted by an issuer that ignores both.
	//
	// When set, the suite drives the whole redirect flow through it, and holds
	// the provider to refusing the answer to a different login.
	Callback func(t *testing.T, authorizeURL string) url.Values
	// ValidToken, for a Bearer provider, mints a token the provider's issuer
	// signed and the provider accepts. When set, the suite holds the provider
	// to yielding a subject and an expiry for it.
	ValidToken func(t *testing.T) string
}

// callbackFor drives one login end to end and returns what CompleteLogin
// should be given, or reports that the harness supplies nothing to drive it
// with.
func (h Harness) callbackFor(t *testing.T, authorizeURL string) (url.Values, bool) {
	t.Helper()
	if h.Callback != nil {
		return h.Callback(t, authorizeURL), true
	}
	if h.ValidCallbackParams != nil {
		return h.ValidCallbackParams, true
	}
	return nil, false
}

// authenticate runs whichever supported flow the harness has inputs for and
// returns the resulting subject, or "" when the harness supplies nothing to
// authenticate with. It lets the suite obtain a real subject to exercise the
// revalidation round-trip without knowing the provider's flow.
func (h Harness) authenticate(ctx context.Context, t *testing.T) (string, bool) {
	t.Helper()
	caps := h.Provider.Capabilities()
	if caps.Credentials && h.ValidUsername != "" {
		if id, err := h.Provider.VerifyCredentials(ctx, h.ValidUsername, h.ValidPassword); err == nil {
			return id.Subject, true
		}
	}
	if !caps.Redirect {
		return "", false
	}
	authorizeURL, providerState, err := h.Provider.BeginLogin(ctx, callbackURI, "s")
	if err != nil {
		return "", false
	}
	params, ok := h.callbackFor(t, authorizeURL)
	if !ok {
		return "", false
	}
	if id, cErr := h.Provider.CompleteLogin(ctx, params, providerState); cErr == nil {
		return id.Subject, true
	}
	return "", false
}

// callbackURI is the redirect the suite claims to be listening on. It is one
// value throughout, because a provider that sends the token endpoint a redirect
// derived from the callback rather than the one it asked with is a provider an
// issuer will refuse.
const callbackURI = "https://host/auth/sso/callback"

// MakeProvider builds a fresh Harness for each subtest.
type MakeProvider func(t *testing.T) Harness

// RunIdentityProviderContract runs the conformance suite against a provider.
func RunIdentityProviderContract(t *testing.T, makeHarness MakeProvider) {
	t.Helper()

	t.Run("NameAndCapabilities", func(t *testing.T) {
		h := makeHarness(t)
		if h.Provider.Name() == "" {
			t.Fatal("Name() must not be empty")
		}
		caps := h.Provider.Capabilities()
		if !caps.Credentials && !caps.Redirect && !caps.Bearer {
			t.Fatal("provider must advertise at least one capability")
		}
	})

	t.Run("Bearer", func(t *testing.T) { bearerContract(t, makeHarness(t)) })

	t.Run("Credentials", func(t *testing.T) {
		h := makeHarness(t)
		caps := h.Provider.Capabilities()
		ctx := context.Background()
		if !caps.Credentials {
			// Unsupported flow must be reported, not silently succeed.
			if _, err := h.Provider.VerifyCredentials(ctx, "x", "y"); !errors.Is(err, authapi.ErrUnsupportedFlow) {
				t.Fatalf("VerifyCredentials on non-credential provider: want ErrUnsupportedFlow, got %v", err)
			}
			return
		}
		id, err := h.Provider.VerifyCredentials(ctx, h.ValidUsername, h.ValidPassword)
		if err != nil {
			t.Fatalf("VerifyCredentials with valid pair: %v", err)
		}
		if id.Subject == "" {
			t.Fatal("authenticated identity must have a non-empty Subject")
		}
		if _, err = h.Provider.VerifyCredentials(ctx, h.ValidUsername, h.ValidPassword+"-wrong"); !errors.Is(
			err,
			authapi.ErrInvalidCredentials,
		) {
			t.Fatalf("VerifyCredentials with bad password: want ErrInvalidCredentials, got %v", err)
		}
		// Enumeration resistance: an unknown username must fail with the SAME
		// error as a wrong password, a provider that returns a distinct error
		// (or success) for unknown users leaks which accounts exist.
		if _, err = h.Provider.VerifyCredentials(ctx, "no-such-user@nonexistent.invalid", "whatever"); !errors.Is(
			err,
			authapi.ErrInvalidCredentials,
		) {
			t.Fatalf("VerifyCredentials with unknown user: want ErrInvalidCredentials, got %v", err)
		}
	})

	t.Run("Redirect", func(t *testing.T) {
		h := makeHarness(t)
		caps := h.Provider.Capabilities()
		ctx := context.Background()
		const state = "host-owned-state"
		if !caps.Redirect {
			if _, _, err := h.Provider.BeginLogin(ctx, "https://host/cb", state); !errors.Is(
				err,
				authapi.ErrUnsupportedFlow,
			) {
				t.Fatalf("BeginLogin on non-redirect provider: want ErrUnsupportedFlow, got %v", err)
			}
			return
		}
		authorizeURL, providerState, err := h.Provider.BeginLogin(ctx, callbackURI, state)
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		if authorizeURL == "" {
			t.Fatal("BeginLogin must return a non-empty authorize URL")
		}
		// CompleteLogin with no callback params (no code/assertion) must fail, not
		// mint an identity.
		if _, err = h.Provider.CompleteLogin(ctx, url.Values{}, providerState); err == nil {
			t.Fatal("CompleteLogin with empty params should fail")
		}
		// Happy path: valid callback params yield an authenticated identity with a
		// non-empty (stable) Subject.
		params, ok := h.callbackFor(t, authorizeURL)
		if !ok {
			return
		}
		id, cErr := h.Provider.CompleteLogin(ctx, params, providerState)
		if cErr != nil {
			t.Fatalf("CompleteLogin with valid params: %v", cErr)
		}
		if id.Subject == "" {
			t.Fatal("a completed redirect login must have a non-empty Subject")
		}
	})

	t.Run("RedirectCarriesTheHostState", func(t *testing.T) {
		h := makeHarness(t)
		if !h.Provider.Capabilities().Redirect {
			return
		}
		const state = "host-owned-state"
		authorizeURL, _, err := h.Provider.BeginLogin(context.Background(), callbackURI, state)
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		parsed, err := url.Parse(authorizeURL)
		if err != nil {
			t.Fatalf("BeginLogin returned an unparseable authorize URL %q: %v", authorizeURL, err)
		}
		// The host owns the CSRF value so its strength never depends on what a
		// provider generated. A provider that substitutes its own leaves the host
		// unable to match the callback to the login it started.
		if got := parsed.Query().Get("state"); got != state {
			t.Errorf("the authorize URL carries state=%q, want the host's %q", got, state)
		}
	})

	t.Run("RedirectLoginsAreNotInterchangeable", func(t *testing.T) {
		h := makeHarness(t)
		if !h.Provider.Capabilities().Redirect || h.Callback == nil {
			return
		}
		ctx := context.Background()

		firstURL, firstState, err := h.Provider.BeginLogin(ctx, callbackURI, "state-one")
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		_, secondState, err := h.Provider.BeginLogin(ctx, callbackURI, "state-two")
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}

		// Two logins must not share their provider state. A fixed verifier or a
		// fixed nonce makes every login answerable by the same reply.
		if firstState == secondState {
			t.Error("two logins produced the same providerState, so nothing distinguishes one from the other")
		}

		// The answer to the first login, replayed against the second, must be
		// refused. A provider that accepts it is not checking the nonce or the
		// PKCE verifier it sent, which is what stops a captured callback from
		// being replayed into somebody else's session.
		answer := h.Callback(t, firstURL)
		if id, cErr := h.Provider.CompleteLogin(ctx, answer, secondState); cErr == nil {
			t.Errorf("CompleteLogin accepted the answer to one login against another and returned %q.\n"+
				"BeginLogin sends a value the provider has to require back (a nonce, a PKCE verifier, "+
				"a request id), or a captured callback completes any login.", id.Subject)
		}
	})

	t.Run("Revalidation", func(t *testing.T) {
		h := makeHarness(t)
		caps := h.Provider.Capabilities()
		ctx := context.Background()
		if !caps.Revalidation {
			// Providers that cannot re-check a subject must say so explicitly, so
			// the host keeps sessions for their normal lifetime instead of guessing.
			if _, err := h.Provider.RefreshIdentity(ctx, "any-subject"); !errors.Is(
				err,
				authapi.ErrUnsupportedFlow,
			) {
				t.Fatalf("RefreshIdentity on non-revalidating provider: want ErrUnsupportedFlow, got %v", err)
			}
			return
		}
		// A subject the provider has never heard of must be reported revoked, so
		// the host terminates its sessions rather than leaving it logged in.
		if _, err := h.Provider.RefreshIdentity(ctx, "definitely-not-a-real-subject"); !errors.Is(
			err,
			authapi.ErrIdentityRevoked,
		) {
			t.Fatalf("RefreshIdentity of unknown subject: want ErrIdentityRevoked, got %v", err)
		}
		// Round-trip: a subject that just authenticated must re-validate to the
		// SAME subject. The host keys the user row on this subject, so an
		// inconsistent value here would mis-sync roles or fail to deprovision.
		if subject, ok := h.authenticate(ctx, t); ok {
			id, err := h.Provider.RefreshIdentity(ctx, subject)
			if err != nil {
				t.Fatalf("RefreshIdentity of a just-authenticated subject %q: %v", subject, err)
			}
			if id.Subject != subject {
				t.Fatalf("RefreshIdentity must return the same subject; got %q for %q", id.Subject, subject)
			}
		}
	})
}

// bearerContract holds a provider to the bearer half of the contract: one
// that advertises the flow verifies a token the issuer signed and refuses
// rubbish before any key is consulted, and one that does not advertise it
// refuses to verify at all.
func bearerContract(t *testing.T, h Harness) {
	t.Helper()

	ctx := context.Background()
	verifier, implemented := h.Provider.(authapi.TokenVerifier)

	if !h.Provider.Capabilities().Bearer {
		if implemented {
			// A verifier nobody advertised is a door the host does not know
			// it opened, so it has to refuse rather than verify.
			if _, err := verifier.VerifyToken(ctx, "x.y.z"); !errors.Is(err, authapi.ErrUnsupportedFlow) {
				t.Fatalf("VerifyToken on a provider not advertising Bearer: want ErrUnsupportedFlow, got %v", err)
			}
		}

		return
	}

	if !implemented {
		t.Fatal("a provider advertising Bearer must implement authapi.TokenVerifier")
	}

	refusesRubbish(ctx, t, verifier)

	if h.ValidToken == nil {
		return
	}

	id, err := verifier.VerifyToken(ctx, h.ValidToken(t))
	if err != nil {
		t.Fatalf("VerifyToken with a token the issuer signed: %v", err)
	}

	if id.Subject == "" {
		t.Fatal("a verified token must yield a non-empty Subject")
	}

	if id.ExpiresAt == 0 {
		t.Fatal("a verified token must carry its expiry in ExpiresAt")
	}
}

// refusesRubbish: nothing, and text that is not a token, are refused before
// any key is consulted. A verifier that reached the network for rubbish is one
// a caller can make dial the issuer.
func refusesRubbish(ctx context.Context, t *testing.T, verifier authapi.TokenVerifier) {
	t.Helper()

	for name, token := range map[string]string{"empty": "", "not a token": "not-a-token", "two parts": "a.b"} {
		if _, err := verifier.VerifyToken(ctx, token); !errors.Is(err, authapi.ErrTokenMalformed) {
			t.Fatalf("VerifyToken(%s): want ErrTokenMalformed, got %v", name, err)
		}
	}
}
