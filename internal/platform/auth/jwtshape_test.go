package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// pairedClock is when the app pairing credentials below are minted.
var pairedClock = time.Date(2026, time.November, 13, 9, 0, 0, 0, time.UTC)

// issuePaired mints an api credential the way a collected app pairing does.
func issuePaired(t *testing.T, store auth.Store, origin string) (auth.Token, auth.Credential) {
	t.Helper()

	token, cred, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
		Subject: "user:ada", Name: "PrusaSlicer", MintedBy: "user:ada",
		Scopes: []string{"operate"}, Origin: origin,
	}, pairedClock)
	require.NoError(t, err)

	return token, cred
}

// An app pairing credential travels as a JWT whose third segment is the
// minted token. The verifier reads that segment and holds the payload to the
// row it names (ADR-20261112-nay rules 2 to 4).
func TestAnAppPairingCredentialIsReadInItsJWTShape(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	token, cred := issuePaired(t, store, auth.OriginAppPairing)

	verifier := auth.NewVerifier(store, func() time.Time { return pairedClock.Add(time.Minute) })
	shaped := auth.WrapJWT(token, cred)

	segments := strings.Split(string(shaped), ".")
	require.Len(t, segments, 3)
	require.Equal(t, string(token), segments[2], "the third segment is not the minted token")

	header, err := base64.RawURLEncoding.DecodeString(segments[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"alg":"none","kid":"pcl","typ":"JWT"}`, string(header))

	claims := decodeClaims(t, segments[1])
	require.Equal(t, auth.MintedClaims{
		Subject: "user:ada", IssuedAt: pairedClock.Unix(), ExpiresAt: cred.ExpiresAt.Unix(),
		Issuer: auth.JWTIssuer, ID: cred.PublicID,
	}, claims)

	require.True(t, auth.IsMintedJWT(shaped))
	require.False(t, auth.IsBearerToken(shaped), "the shaped token went to the bearer path")
	require.Equal(t, token, auth.Inner(shaped))

	got, err := verifier.Verify(t.Context(), shaped, auth.TransportHTTP)
	require.NoError(t, err)
	require.Equal(t, cred.PublicID, got.PublicID)

	got, err = verifier.Verify(t.Context(), token, auth.TransportHTTP)
	require.NoError(t, err, "the bare token stopped working")
	require.Equal(t, cred.PublicID, got.PublicID)

	t.Run("a payload that differs from the row is refused", func(t *testing.T) {
		t.Parallel()

		for name, edit := range map[string]func(*auth.MintedClaims){
			"another jti":      func(c *auth.MintedClaims) { c.ID = "AAAAAAAAAAAAAAAAAAAAAA" },
			"another sub":      func(c *auth.MintedClaims) { c.Subject = "user:admin" },
			"a later exp":      func(c *auth.MintedClaims) { c.ExpiresAt += 3600 },
			"an exp one short": func(c *auth.MintedClaims) { c.ExpiresAt-- },
		} {
			edited := claims
			edit(&edited)

			_, refused := verifier.Verify(t.Context(), withClaims(t, shaped, edited), auth.TransportHTTP)
			require.True(t, auth.Refused(refused), "%s was accepted", name)
		}

		for name, payload := range map[string]string{
			"not base64": "!!!",
			"not json":   base64.RawURLEncoding.EncodeToString([]byte("[1,2]")),
			"too long":   strings.Repeat("A", 4096),
		} {
			presented := auth.Token(segments[0] + "." + payload + "." + segments[2])

			_, refused := verifier.Verify(t.Context(), presented, auth.TransportHTTP)
			require.True(t, auth.Refused(refused), "a payload that is %s was accepted", name)
		}
	})

	t.Run("a session or a minted credential wrapped by hand is refused", func(t *testing.T) {
		t.Parallel()

		session, row, issueErr := auth.Issue(t.Context(), store, "ada", auth.KindSession, pairedClock)
		require.NoError(t, issueErr)

		_, refused := verifier.Verify(t.Context(), auth.WrapJWT(session, row), auth.TransportHTTP)
		require.True(t, auth.Refused(refused), "a session wrapped by hand was accepted")

		minted, mintedRow := issuePaired(t, store, auth.OriginMint)

		_, refused = verifier.Verify(t.Context(), auth.WrapJWT(minted, mintedRow), auth.TransportHTTP)
		require.True(t, auth.Refused(refused), "a minted credential wrapped by hand was accepted")

		_, refused = verifier.Verify(t.Context(), minted, auth.TransportHTTP)
		require.NoError(t, refused, "the bare minted credential stopped working")
	})

	t.Run("a Prusa Account token keeps the bearer path", func(t *testing.T) {
		t.Parallel()

		prusaAccount := auth.Token("eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln")
		require.True(t, auth.IsBearerToken(prusaAccount))
		require.False(t, auth.IsMintedJWT(prusaAccount))
		require.Equal(t, prusaAccount, auth.Inner(prusaAccount))
	})

	t.Run("a revoked credential is refused in the JWT shape", func(t *testing.T) {
		t.Parallel()

		other, row := issuePaired(t, store, auth.OriginAppPairing)
		require.NoError(t, verifier.Revoke(t.Context(), row.PublicID))

		_, refused := verifier.Verify(t.Context(), auth.WrapJWT(other, row), auth.TransportHTTP)
		require.True(t, auth.Refused(refused))
	})

	t.Run("an expired credential is refused in the JWT shape", func(t *testing.T) {
		t.Parallel()

		late := auth.NewVerifier(store, func() time.Time { return cred.ExpiresAt })

		_, refused := late.Verify(t.Context(), shaped, auth.TransportHTTP)
		require.True(t, auth.Refused(refused))
	})
}

func decodeClaims(t *testing.T, segment string) auth.MintedClaims {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	require.NoError(t, err)

	var claims auth.MintedClaims
	require.NoError(t, json.Unmarshal(decoded, &claims))

	return claims
}

func withClaims(t *testing.T, shaped auth.Token, claims auth.MintedClaims) auth.Token {
	t.Helper()

	encoded, err := json.Marshal(claims)
	require.NoError(t, err)

	segments := strings.Split(string(shaped), ".")

	return auth.Token(segments[0] + "." + base64.RawURLEncoding.EncodeToString(encoded) + "." + segments[2])
}
