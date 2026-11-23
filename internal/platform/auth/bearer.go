package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// jwsSegments is how many dot-separated segments a compact JWS carries.
const jwsSegments = 3

// errNoBearerVerifier is the cause behind refusing a bearer token on a server
// wired without a verifier for it. It reaches a log line and never a client.
var errNoBearerVerifier = errors.New("auth: no bearer verifier is wired")

// IsBearerToken reports whether presented text is shaped like a token another
// issuer signed, instead of one this server minted. A minted token carries
// Prefix. A JWS is three base64url segments. A JWS whose third segment carries
// Prefix is a credential this server shaped as a JWT, and the verifier checks
// it (IsMintedJWT). The shape only decides which path checks the token:
// a forged JWS is refused by the bearer path, and rubbish is refused by both.
func IsBearerToken(token Token) bool {
	text := string(token)
	if strings.HasPrefix(text, Prefix) || IsMintedJWT(token) {
		return false
	}

	return strings.Count(text, ".") == jwsSegments-1
}

// BearerVerifier is the identity plugin side of a bearer credential: it checks
// a token offline and says which provider vouched for it. The composition root
// satisfies it over the running identity instances, and a test over one plugin.
type BearerVerifier interface {
	// VerifyToken checks one token and returns the identity it names and the
	// provider that verified it. It reports the token errors sdk/authapi
	// declares, and any other error is treated as a server that could not
	// answer.
	VerifyToken(ctx context.Context, token string) (identity authapi.Identity, provider string, err error)
}

// Bearer resolves a token another issuer signed to a credential, the way
// Verifier resolves one this server minted. The plugin verifies the token, and
// the subject is then mapped through a pairing an administrator wrote. The
// plugin never sees the pairing table, and the caller's principal is never the
// subject the issuer named.
//
// It has no bypass. A server wired without a verifier, a verifier that cannot
// answer, and a subject nobody paired all refuse.
type Bearer struct {
	verifier BearerVerifier
	pairings Pairings
	now      func() time.Time
}

// NewBearer returns a resolver over a verifier and a pairing store. A nil
// clock means time.Now.
func NewBearer(verifier BearerVerifier, pairings Pairings, now func() time.Time) *Bearer {
	if now == nil {
		now = time.Now
	}

	return &Bearer{verifier: verifier, pairings: pairings, now: now}
}

// Resolve verifies a bearer token presented on a transport and maps its
// subject to the principal it was paired to, or refuses.
//
// A refusal a caller can act on gets its own fault code, unlike the single
// wording Verifier gives. The caller holds the token and can read its claims,
// so naming the failed check tells an attacker nothing the token did not. A
// verifier or a pairing store that could not answer gives the plain refusal.
func (b *Bearer) Resolve(ctx context.Context, token Token, on Transport) (Credential, error) {
	if b == nil || b.verifier == nil || b.pairings == nil {
		return Credential{}, refuse(errNoBearerVerifier)
	}

	identity, provider, err := b.verifier.VerifyToken(ctx, string(token))
	if err != nil {
		return Credential{}, bearerRefusal(err)
	}

	if identity.Subject == "" {
		return Credential{}, refuse(fmt.Errorf("%w: the %s verifier named no subject", ErrMalformedToken, provider))
	}

	pairing, err := b.pairings.Lookup(ctx, provider, identity.Subject)

	switch {
	case errors.Is(err, ErrNoPairing):
		return Credential{}, Unpaired(provider, identity.Subject).Wrap(err)
	case err != nil:
		return Credential{}, refuse(
			fmt.Errorf("look up the pairing of %s subject %q: %w", provider, identity.Subject, err),
		)
	}

	if pairing.Generation < pairing.CurrentGeneration {
		return Credential{}, refuse(fmt.Errorf(
			"the pairing of %s subject %q is from generation %d of %q, which is now %d",
			provider, identity.Subject, pairing.Generation, pairing.Principal, pairing.CurrentGeneration))
	}

	if pairing.RootCreator != "" && pairing.RootGeneration < pairing.RootCurrentGeneration {
		return Credential{}, refuse(fmt.Errorf(
			"the pairing of %s subject %q was written under root %q at generation %d, which is now %d",
			provider, identity.Subject, pairing.RootCreator, pairing.RootGeneration, pairing.RootCurrentGeneration))
	}

	if pairing.CreatedBy != "" && pairing.CreatorGeneration < pairing.CreatorCurrentGeneration {
		return Credential{}, refuse(fmt.Errorf(
			"the pairing of %s subject %q was written at generation %d of %q, which is now %d",
			provider, identity.Subject, pairing.CreatorGeneration, pairing.CreatedBy, pairing.CreatorCurrentGeneration))
	}

	cred := Credential{
		RootCreator: pairing.Root(),
		Subject:     pairing.Principal,
		Kind:        KindPrusaAccount,
		Name:        pairing.Label,
		IssuedAt:    pairing.CreatedAt,
		ExpiresAt:   time.Unix(identity.ExpiresAt, 0).UTC(),
		LastSeen:    b.now(),
	}

	if !cred.Kind.AcceptedOn(on) {
		return Credential{}, refuse(fmt.Errorf("%s credential presented on %s", cred.Kind, on))
	}

	return cred, nil
}

// Unpaired is the refusal for a subject the token vouched for and nobody
// paired. It is exported so the composition root and the pairing routes can
// answer in the same words.
func Unpaired(provider, subject string) *fault.Fault {
	return fault.AuthBearerUnpaired.New(
		"the token verified, and no administrator has paired its subject to a principal on this server").With(
		fault.StringParam("provider", provider),
		fault.StringParam("subject", subject))
}

// bearerRefusal turns what the verifier reported into the fault the caller is
// told. Every token error sdk/authapi declares has its own code. Anything else
// is a verifier that could not answer, and gets the same wording as a store
// that could not answer.
func bearerRefusal(err error) error {
	switch {
	case errors.Is(err, authapi.ErrTokenSignature):
		return fault.AuthBearerSignature.New(
			"the token's signature does not verify against a key the issuer publishes, or its algorithm is not accepted",
		).
			Wrap(err)
	case errors.Is(err, authapi.ErrTokenExpired):
		return fault.AuthBearerExpired.New("the token has expired or is not valid yet").Wrap(err)
	case errors.Is(err, authapi.ErrTokenIssuer):
		return fault.AuthBearerIssuer.New("the token names an issuer this server is not configured for").Wrap(err)
	case errors.Is(err, authapi.ErrTokenAudience):
		return fault.AuthBearerAudience.New("the token was issued for another audience").Wrap(err)
	case errors.Is(err, authapi.ErrTokenKeys):
		return fault.AuthBearerKeysUnavailable.New(
			"the issuer's signing keys could not be fetched, so the token cannot be checked").Wrap(err)
	default:
		// Malformed text and a verifier that failed for its own reasons are
		// both the plain refusal. Naming the difference would let a caller
		// probe the verifier with rubbish.
		return refuse(err)
	}
}
