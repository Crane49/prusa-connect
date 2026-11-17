package oidctoken

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// signers is one key of each kind an issuer publishes, served as a JWKS.
type signers struct {
	rsa  *rsa.PrivateKey
	ec   map[string]*ecdsa.PrivateKey
	ed   ed25519.PrivateKey
	keys *KeySet
}

func newSigners(t *testing.T) *signers {
	t.Helper()

	s := &signers{ec: make(map[string]*ecdsa.PrivateKey)}

	var err error

	if s.rsa, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}

	for name, curve := range map[string]elliptic.Curve{
		"P-256": elliptic.P256(), "P-384": elliptic.P384(), "P-521": elliptic.P521(),
	} {
		if s.ec[name], err = ecdsa.GenerateKey(curve, rand.Reader); err != nil {
			t.Fatal(err)
		}
	}

	if _, s.ed, err = ed25519.GenerateKey(rand.Reader); err != nil {
		t.Fatal(err)
	}

	b64 := base64.RawURLEncoding.EncodeToString
	fixed := func(n *big.Int, curve elliptic.Curve) string {
		return b64(n.FillBytes(make([]byte, (curve.Params().BitSize+7)/8)))
	}

	entries := []map[string]string{
		{"kty": "RSA", "kid": "rsa", "use": "sig", "n": b64(s.rsa.N.Bytes()), "e": "AQAB"},
		{"kty": "OKP", "kid": "ed", "crv": "Ed25519", "x": b64(s.ed.Public().(ed25519.PublicKey))},
	}

	for name, key := range s.ec {
		entries = append(entries, map[string]string{
			"kty": "EC", "kid": name, "crv": name,
			"x": fixed(key.X, key.Curve), "y": fixed(key.Y, key.Curve),
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": entries})
	}))
	t.Cleanup(server.Close)

	s.keys = NewKeySet(server.URL, server.Client(), nil)

	return s
}

// token signs a payload with alg, naming kid, the way an issuer does.
func (s *signers) token(t *testing.T, alg, kid string, secret []byte) string {
	t.Helper()

	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	b64 := base64.RawURLEncoding.EncodeToString
	signed := b64(header) + "." + b64([]byte(`{"sub":"ada"}`))

	digest := func(hash crypto.Hash) []byte {
		h := hash.New()
		h.Write([]byte(signed))

		return h.Sum(nil)
	}

	var (
		signature []byte
		err       error
	)

	switch alg {
	case "RS256":
		signature, err = rsa.SignPKCS1v15(rand.Reader, s.rsa, crypto.SHA256, digest(crypto.SHA256))
	case "PS256":
		signature, err = rsa.SignPSS(rand.Reader, s.rsa, crypto.SHA256, digest(crypto.SHA256),
			&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	case "ES256", "ES384", "ES512":
		curve := map[string]string{"ES256": "P-256", "ES384": "P-384", "ES512": "P-521"}[alg]
		hash := map[string]crypto.Hash{"ES256": crypto.SHA256, "ES384": crypto.SHA384, "ES512": crypto.SHA512}[alg]
		key := s.ec[curve]

		r, sig, signErr := ecdsa.Sign(rand.Reader, key, digest(hash))
		err = signErr
		size := (key.Curve.Params().BitSize + 7) / 8
		signature = append(r.FillBytes(make([]byte, size)), sig.FillBytes(make([]byte, size))...)
	case "EdDSA", "Ed25519":
		signature = ed25519.Sign(s.ed, []byte(signed))
	case "HS256":
		mac := hmac.New(crypto.SHA256.New, secret)
		mac.Write([]byte(signed))
		signature = mac.Sum(nil)
	default:
		signature = []byte("x")
	}

	if err != nil {
		t.Fatal(err)
	}

	return signed + "." + b64(signature)
}

// Every algorithm a real issuer signs an ID token with verifies when the
// policy accepts it: authentik with no signing key signs HS256 with the client
// secret, Authelia and Keycloak can sign ES256, and RS256 is the default.
func TestEveryAcceptedAlgorithmVerifies(t *testing.T) {
	t.Parallel()

	s := newSigners(t)
	secret := []byte("pcl-authentik-fixture-secret")

	for alg, kid := range map[string]string{
		"RS256": "rsa", "PS256": "rsa",
		"ES256": "P-256", "ES384": "P-384", "ES512": "P-521",
		"EdDSA": "ed", "Ed25519": "ed",
		"HS256": "",
	} {
		t.Run(alg, func(t *testing.T) {
			t.Parallel()

			payload, err := Verify(t.Context(), s.token(t, alg, kid, secret),
				Policy{Algorithms: []string{alg}, Keys: s.keys, Secret: secret})
			if err != nil {
				t.Fatalf("%s did not verify: %v", alg, err)
			}

			if string(payload) != `{"sub":"ada"}` {
				t.Fatalf("payload %s", payload)
			}
		})
	}
}

// Each case is a token that must not verify, and the refusal it earns.
func TestATokenOutsideThePolicyIsRefused(t *testing.T) {
	t.Parallel()

	s := newSigners(t)
	secret := []byte("the-client-secret")

	cases := []struct {
		name   string
		token  string
		policy Policy
		want   error
	}{
		{
			name:   "an algorithm the issuer did not advertise",
			token:  s.token(t, "ES256", "P-256", nil),
			policy: Policy{Algorithms: []string{"RS256"}, Keys: s.keys},
			want:   ErrAlgorithm,
		},
		{
			name:   "alg none, even when a list names it",
			token:  s.token(t, "none", "", nil),
			policy: Policy{Algorithms: []string{"none"}, Keys: s.keys},
			want:   ErrAlgorithm,
		},
		{
			name:   "HMAC with no client secret",
			token:  s.token(t, "HS256", "", secret),
			policy: Policy{Algorithms: []string{"HS256"}, Keys: s.keys},
			want:   ErrAlgorithm,
		},
		{
			name:   "HMAC with another secret",
			token:  s.token(t, "HS256", "", []byte("not-the-secret")),
			policy: Policy{Algorithms: []string{"HS256"}, Keys: s.keys, Secret: secret},
			want:   ErrSignature,
		},
		{
			name:   "an RS256 signature under an ES256 header",
			token:  forge(t, s.token(t, "RS256", "rsa", nil), "ES256", "P-256"),
			policy: Policy{Algorithms: []string{"ES256", "RS256"}, Keys: s.keys},
			want:   ErrSignature,
		},
		{
			name:   "the empty default accepts RS256 alone",
			token:  s.token(t, "PS256", "rsa", nil),
			policy: Policy{Keys: s.keys},
			want:   ErrAlgorithm,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Verify(t.Context(), tc.token, tc.policy)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	// A key named by a token signed with an algorithm for another curve is not
	// used, so the check never reaches a signature over the wrong curve.
	_, err := Verify(t.Context(), forge(t, s.token(t, "ES256", "P-256", nil), "ES256", "P-384"),
		Policy{Algorithms: []string{"ES256"}, Keys: s.keys})
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("an ES256 token naming a P-384 key: got %v", err)
	}
}

// forge rewrites a token's header and keeps its payload and signature.
func forge(t *testing.T, token, alg, kid string) string {
	t.Helper()

	parts := strings.Split(token, ".")
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid})

	return base64.RawURLEncoding.EncodeToString(header) + "." + parts[1] + "." + parts[2]
}

// The refusal for an HMAC token says what to configure, because an authentik
// provider with no signing key signs every token that way.
func TestAnHMACRefusalNamesTheClientSecret(t *testing.T) {
	t.Parallel()

	s := newSigners(t)

	_, err := Verify(t.Context(), s.token(t, "HS256", "", []byte("x")),
		Policy{Algorithms: []string{"HS256"}, Keys: s.keys})
	if err == nil || !strings.Contains(err.Error(), "no client secret is configured") {
		t.Fatalf("got %v", err)
	}
}

func TestAcceptedIsWhatTheIssuerListsAndThisPackageChecks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		advertised []string
		secret     bool
		want       []string
	}{
		{advertised: nil, want: []string{"RS256"}},
		{advertised: []string{"HS256"}, secret: true, want: []string{"HS256"}},
		{advertised: []string{"HS256"}, secret: false, want: []string{}},
		{advertised: []string{"none", "RS256", "ES256", "Ed448", "RS256"}, want: []string{"RS256", "ES256"}},
		{advertised: []string{"PS512", "HS512", "EdDSA"}, want: []string{"PS512", "EdDSA"}},
	}

	for _, tc := range cases {
		got := Accepted(tc.advertised, tc.secret)
		if !slices.Equal(got, tc.want) {
			t.Errorf("Accepted(%v, %v) = %v, want %v", tc.advertised, tc.secret, got, tc.want)
		}
	}

	if !slices.Contains(Supported(), "ES256") || slices.Contains(Supported(), "none") {
		t.Errorf("Supported() = %v", Supported())
	}
}

// A point off its curve is the input of an invalid-curve attack, so a JWK
// carrying one is refused rather than used.
func TestAnECKeyOffItsCurveIsRefused(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	b64 := base64.RawURLEncoding.EncodeToString
	x := b64(key.X.FillBytes(make([]byte, 32)))
	y := key.Y.FillBytes(make([]byte, 32))

	if _, err = ECPublicKey("P-256", x, b64(y)); err != nil {
		t.Fatalf("a good point was refused: %v", err)
	}

	y[31] ^= 1

	if _, err = ECPublicKey("P-256", x, b64(y)); err == nil {
		t.Fatal("a point off the curve was taken")
	}

	if _, err = ECPublicKey("secp256k1", x, b64(y)); err == nil {
		t.Fatal("an unknown curve was taken")
	}
}

// An issuer that publishes an RSA key and an EC key, and a token that names no
// key, still finds the one key its algorithm can use.
func TestATokenWithNoKeyIDFindsTheOnlyKeyItsAlgorithmUses(t *testing.T) {
	t.Parallel()

	s := newSigners(t)

	if _, err := Verify(t.Context(), s.token(t, "RS256", "", nil),
		Policy{Algorithms: []string{"RS256"}, Keys: s.keys}); err != nil {
		t.Fatalf("RS256 with no kid: %v", err)
	}

	if _, err := Verify(t.Context(), s.token(t, "ES384", "", nil),
		Policy{Algorithms: []string{"ES384"}, Keys: s.keys}); err != nil {
		t.Fatalf("ES384 with no kid: %v", err)
	}
}
