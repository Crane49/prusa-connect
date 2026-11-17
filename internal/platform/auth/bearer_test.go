package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The subject the fake issuer vouches for, and the principal it is paired to.
const (
	pairedSubject   = "8f1e1b2c-0000-4000-8000-abcdefabcdef"
	unpairedSubject = "c2a4d6e8-0000-4000-8000-fedcbafedcba"
	pairedPrincipal = "service_account:wall-dashboard"
	providerName    = "prusa-account"
)

// bearerClock is when the tokens below are checked.
var bearerClock = time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)

// fakeVerifier is an identity plugin with the cryptography removed. The token
// text says which answer it gives, so each refusal is reached by name.
type fakeVerifier struct {
	calls int
}

func (f *fakeVerifier) VerifyToken(_ context.Context, token string) (authapi.Identity, string, error) {
	f.calls++

	switch token {
	case "good.token.paired":
		return authapi.Identity{Subject: pairedSubject, ExpiresAt: bearerClock.Add(time.Hour).Unix()}, providerName, nil
	case "good.token.unpaired":
		return authapi.Identity{
			Subject:   unpairedSubject,
			ExpiresAt: bearerClock.Add(time.Hour).Unix(),
		}, providerName, nil
	case "good.token.nosubject":
		return authapi.Identity{ExpiresAt: bearerClock.Add(time.Hour).Unix()}, providerName, nil
	case "bad.signature.x":
		return authapi.Identity{}, "", authapi.ErrTokenSignature
	case "bad.expired.x":
		return authapi.Identity{}, "", authapi.ErrTokenExpired
	case "bad.issuer.x":
		return authapi.Identity{}, "", authapi.ErrTokenIssuer
	case "bad.audience.x":
		return authapi.Identity{}, "", authapi.ErrTokenAudience
	case "bad.keys.x":
		return authapi.Identity{}, "", authapi.ErrTokenKeys
	case "bad.malformed.x":
		return authapi.Identity{}, "", authapi.ErrTokenMalformed
	default:
		return authapi.Identity{}, "", errors.New("the plugin fell over")
	}
}

func pairedStore(t *testing.T) *auth.MemoryPairings {
	t.Helper()

	pairings := auth.NewMemoryPairings()
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: providerName, Subject: pairedSubject, Principal: pairedPrincipal,
		Label: "the dashboard", CreatedAt: bearerClock.Add(-24 * time.Hour),
	}))

	return pairings
}

// The plugin names a subject, the pairing names the principal, and the
// credential carries the principal, the kind and the token's own expiry. The
// issuer's subject never becomes the caller's principal.
func TestAPairedSubjectResolvesToItsPrincipal(t *testing.T) {
	t.Parallel()

	bearer := auth.NewBearer(&fakeVerifier{}, pairedStore(t), func() time.Time { return bearerClock })

	for _, on := range []auth.Transport{auth.TransportHTTP, auth.TransportSSE} {
		cred, err := bearer.Resolve(t.Context(), "good.token.paired", on)
		require.NoError(t, err, "on %s", on)

		require.Equal(t, pairedPrincipal, cred.Subject)
		require.Equal(t, auth.KindPrusaAccount, cred.Kind)
		require.Equal(t, "the dashboard", cred.Name)
		require.Equal(t, bearerClock.Add(time.Hour), cred.ExpiresAt)
		require.Equal(t, bearerClock, cred.LastSeen)
		require.Empty(t, cred.PublicID, "a bearer credential has no row and no public id")
		require.True(t, cred.Live(bearerClock))
	}
}

// The cross-transport rule applies to this kind too, in the same words as every
// other transport refusal.
func TestABearerCredentialIsRefusedOnTheBroker(t *testing.T) {
	t.Parallel()

	bearer := auth.NewBearer(&fakeVerifier{}, pairedStore(t), nil)

	_, err := bearer.Resolve(t.Context(), "good.token.paired", auth.TransportMQTT)
	require.True(t, auth.Refused(err), "got %v", err)
}

// An operator debugging a client reads which check failed. Malformed text and a
// plugin that failed get the plain refusal, because naming those would let a
// caller probe the plugin.
func TestEachTokenRefusalHasItsOwnCode(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		token string
		code  fault.Code
	}{
		"signature":   {"bad.signature.x", fault.AuthBearerSignature.Code()},
		"expired":     {"bad.expired.x", fault.AuthBearerExpired.Code()},
		"issuer":      {"bad.issuer.x", fault.AuthBearerIssuer.Code()},
		"audience":    {"bad.audience.x", fault.AuthBearerAudience.Code()},
		"keys":        {"bad.keys.x", fault.AuthBearerKeysUnavailable.Code()},
		"unpaired":    {"good.token.unpaired", fault.AuthBearerUnpaired.Code()},
		"malformed":   {"bad.malformed.x", fault.AuthTokenInvalid.Code()},
		"plugin down": {"what.ever.x", fault.AuthTokenInvalid.Code()},
		"no subject":  {"good.token.nosubject", fault.AuthTokenInvalid.Code()},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bearer := auth.NewBearer(&fakeVerifier{}, pairedStore(t), nil)

			_, err := bearer.Resolve(t.Context(), auth.Token(tc.token), auth.TransportHTTP)
			require.Error(t, err)
			require.True(t, fault.HasCode(err, tc.code), "want %s, got %v", tc.code, err)
		})
	}
}

// A compatibility client meets this refusal first. It names the provider and
// the subject, so the administrator who reads it can write the pairing. The
// status is 403 and not 401: the token verified, and the person is not known
// here.
func TestAnUnpairedSubjectIsToldHowToPair(t *testing.T) {
	t.Parallel()

	bearer := auth.NewBearer(&fakeVerifier{}, pairedStore(t), nil)

	_, err := bearer.Resolve(t.Context(), "good.token.unpaired", auth.TransportHTTP)

	var refused *fault.Fault

	require.ErrorAs(t, err, &refused)
	require.Equal(t, fault.AuthBearerUnpaired.Code(), refused.Code())
	require.Equal(t, 403, refused.Status())

	provider, ok := refused.Params().Get("provider")
	require.True(t, ok)
	require.Equal(t, providerName, provider.String())

	subject, ok := refused.Params().Get("subject")
	require.True(t, ok)
	require.Equal(t, unpairedSubject, subject.String())
}

// No verifier, no pairing store, and a pairing store that cannot answer all
// refuse. None of them admits.
func TestTheBearerPathIsFailClosed(t *testing.T) {
	t.Parallel()

	var none *auth.Bearer

	_, err := none.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a nil resolver admitted: %v", err)

	_, err = auth.NewBearer(nil, pairedStore(t), nil).Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "no verifier admitted: %v", err)

	_, err = auth.NewBearer(&fakeVerifier{}, nil, nil).Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "no pairing store admitted: %v", err)

	broken := pairedStore(t)
	broken.Fail(errors.New("disk on fire"))

	_, err = auth.NewBearer(&fakeVerifier{}, broken, nil).Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a store that cannot answer admitted: %v", err)
	require.False(t, fault.HasCode(err, fault.AuthBearerUnpaired.Code()),
		"a store outage was reported as an unpaired subject")
}

// A minted token is never a bearer token, whatever it contains, and a bearer
// token is three segments.
func TestIsBearerTokenReadsTheShape(t *testing.T) {
	t.Parallel()

	minted, _, _, err := auth.Mint()
	require.NoError(t, err)

	require.False(t, auth.IsBearerToken(minted))
	require.False(t, auth.IsBearerToken(""))
	require.False(t, auth.IsBearerToken("a.b"))
	require.False(t, auth.IsBearerToken("a.b.c.d"))
	require.False(t, auth.IsBearerToken(auth.Prefix+"a.b.c"))
	require.True(t, auth.IsBearerToken("eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"))
}

// The memory store follows the same pairing rules as the database store.
func TestAPairingIsWrittenOnceAndListedInOrder(t *testing.T) {
	t.Parallel()

	pairings := pairedStore(t)

	err := pairings.Save(t.Context(), auth.Pairing{
		Provider: providerName, Subject: pairedSubject, Principal: "user:somebody-else", CreatedAt: bearerClock,
	})
	require.ErrorIs(t, err, auth.ErrPairingExists)

	kept, err := pairings.Lookup(t.Context(), providerName, pairedSubject)
	require.NoError(t, err)
	require.Equal(t, pairedPrincipal, kept.Principal, "a refused write changed the row")

	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: "another", Subject: "z", Principal: "user:z", CreatedAt: bearerClock,
	}))
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: providerName, Subject: "0-first", Principal: "user:a", CreatedAt: bearerClock,
	}))

	all, err := pairings.List(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, "another", all[0].Provider)
	require.Equal(t, "0-first", all[1].Subject)
	require.Equal(t, pairedSubject, all[2].Subject)

	one, err := pairings.List(t.Context(), providerName)
	require.NoError(t, err)
	require.Len(t, one, 2)

	require.NoError(t, pairings.Delete(t.Context(), providerName, pairedSubject))
	require.ErrorIs(t, pairings.Delete(t.Context(), providerName, pairedSubject), auth.ErrNoPairing)

	_, err = pairings.Lookup(t.Context(), providerName, pairedSubject)
	require.ErrorIs(t, err, auth.ErrNoPairing)

	require.ErrorIs(t, pairings.Save(t.Context(), auth.Pairing{Provider: providerName}), auth.ErrInvalidPairing)
}

// Issue refuses the kind. Another issuer minted the token, so there is no row
// to write.
func TestTheBearerKindIsNeverMinted(t *testing.T) {
	t.Parallel()

	_, _, err := auth.Issue(t.Context(), auth.NewMemoryStore(), "user:x", auth.KindPrusaAccount, bearerClock)
	require.ErrorIs(t, err, auth.ErrMalformedToken)

	require.Equal(t, "prusa-account", auth.KindPrusaAccount.String())
	require.True(t, auth.KindPrusaAccount.NamesGraphSubject())
	require.True(t, auth.KindAPI.NamesGraphSubject())
	require.False(t, auth.KindSession.NamesGraphSubject())
}

// TestAPairingFromAnEarlierGenerationIsRefused is the pairing half of the
// password fence. A session a password change ended could still save a
// pairing it had checked for. The pairing carries the principal's older
// generation, so its token is refused.
func TestAPairingFromAnEarlierGenerationIsRefused(t *testing.T) {
	t.Parallel()

	pairings := pairedStore(t)
	bearer := auth.NewBearer(&fakeVerifier{}, pairings, func() time.Time { return bearerClock })

	_, err := bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.NoError(t, err)

	pairings.Bump(pairedPrincipal)

	_, err = bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a pairing from before the principal's password change was accepted")
}

// TestAPairingItsCreatorsChangeEndedIsRefused is a pairing the person made for
// somebody else. With a stolen session an attacker pairs their own account to a
// service account. The pairing records who made it and at which generation, so
// the person's password change refuses it.
func TestAPairingItsCreatorsChangeEndedIsRefused(t *testing.T) {
	t.Parallel()

	pairings := auth.NewMemoryPairings()
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: providerName, Subject: pairedSubject, Principal: "service_account:ci",
		CreatedBy: "user:ada", CreatedAt: bearerClock,
	}))

	bearer := auth.NewBearer(&fakeVerifier{}, pairings, func() time.Time { return bearerClock })

	_, err := bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.NoError(t, err)

	pairings.Bump("user:ada")

	_, err = bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a pairing its creator's password change ended was accepted")
}

// TestAPairingWrittenDownAChainEndsWithItsRoot is a pairing written through a
// second person. It names bob as its creator and ada as its root, and ada's
// password change refuses it.
func TestAPairingWrittenDownAChainEndsWithItsRoot(t *testing.T) {
	t.Parallel()

	pairings := auth.NewMemoryPairings()
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: providerName, Subject: pairedSubject, Principal: "service_account:ci",
		CreatedBy: "user:bob", RootCreator: "user:ada", CreatedAt: bearerClock,
	}))

	bearer := auth.NewBearer(&fakeVerifier{}, pairings, func() time.Time { return bearerClock })

	cred, err := bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.NoError(t, err)
	require.Equal(t, "user:ada", cred.RootCreator, "the token does not carry its pairing's root")

	pairings.Bump("user:ada")

	_, err = bearer.Resolve(t.Context(), "good.token.paired", auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a pairing whose root changed their password was accepted")

	removed, err := pairings.DeleteCreatedBy(t.Context(), "user:ada")
	require.NoError(t, err)
	require.Equal(t, 1, removed, "the end did not remove the pairing by its root")
}
