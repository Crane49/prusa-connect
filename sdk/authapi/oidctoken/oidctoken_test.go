package oidctoken_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/authapi/oidctoken"
)

// TestASegmentIsUnpaddedBase64URL. The padded and the standard alphabets are
// the two spellings a decoder must not take: either one accepts a second
// encoding of the same token, which is a second thing to compare against.
func TestASegmentIsUnpaddedBase64URL(t *testing.T) {
	t.Parallel()

	raw := []byte{0xff, 0xef, 0x00, 0x3f, 0x7a}

	decoded, err := oidctoken.DecodeSegment(base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("decoding a segment this package encoded: %v", err)
	}

	if !reflect.DeepEqual(decoded, raw) {
		t.Errorf("decoded %x, want %x", decoded, raw)
	}

	for name, segment := range map[string]string{
		"padded":            base64.URLEncoding.EncodeToString(raw),
		"standard alphabet": base64.RawStdEncoding.EncodeToString([]byte{0xff, 0xef}),
		"not base64 at all": "!!!!",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, refused := oidctoken.DecodeSegment(segment); refused == nil {
				t.Errorf("%q was accepted", segment)
			}
		})
	}

	// An empty segment is a valid encoding of no bytes. A JWS with an empty
	// payload is the caller's to refuse, not this decoder's.
	if _, empty := oidctoken.DecodeSegment(""); empty != nil {
		t.Errorf("the empty segment was refused: %v", empty)
	}
}

// TestAnAudienceIsAStringOrAListOfThem is RFC 7519 section 4.1.3. Both shapes
// are in the wild and an issuer picks without telling anybody.
func TestAnAudienceIsAStringOrAListOfThem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{raw: `"prusa-connect"`, want: []string{"prusa-connect"}},
		{raw: `["prusa-connect"]`, want: []string{"prusa-connect"}},
		{raw: `["other","prusa-connect"]`, want: []string{"other", "prusa-connect"}},
		{raw: `[]`, want: []string{}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			var aud oidctoken.Audience
			if err := json.Unmarshal([]byte(tc.raw), &aud); err != nil {
				t.Fatalf("unmarshalling %s: %v", tc.raw, err)
			}

			if !reflect.DeepEqual([]string(aud), tc.want) {
				t.Errorf("read %v, want %v", []string(aud), tc.want)
			}
		})
	}

	for _, raw := range []string{`42`, `{"aud":"x"}`, `[1,2]`, `null9`} {
		var aud oidctoken.Audience
		if err := json.Unmarshal([]byte(raw), &aud); err == nil {
			t.Errorf("%s was read as an audience: %v", raw, aud)
		}
	}
}

// TestAJWKRebuildsTheKeyThatSignedIt is the whole point of the JWKS: the
// modulus and exponent an issuer publishes have to come back as the key a
// signature checks against, or every login fails for a reason nobody can see.
func TestAJWKRebuildsTheKeyThatSignedIt(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}

	rebuilt, err := oidctoken.RSAPublicKey(
		base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()))
	if err != nil {
		t.Fatalf("rebuilding the published key: %v", err)
	}

	if !rebuilt.Equal(&key.PublicKey) {
		t.Error("the rebuilt key is not the one that was published")
	}
}

// TestAnUnusableJWKIsRefusedRatherThanTruncated. A JWK carries a number of any
// length and the field it lands in is an int, so a conversion that wraps hands
// back a key with an exponent the issuer never published. Refusing is what
// keeps the map of trusted keys a map of the issuer's keys.
func TestAnUnusableJWKIsRefusedRatherThanTruncated(t *testing.T) {
	t.Parallel()

	modulus := base64.RawURLEncoding.EncodeToString(make([]byte, 256))

	// Nine bytes whose low eight read as 65537. A decoder that takes the low
	// bits of a big integer accepts this as the ordinary exponent and builds a
	// key nobody published.
	wrapping := []byte{0x01, 0, 0, 0, 0, 0, 0x01, 0x00, 0x01}

	for name, tc := range map[string]struct{ modulus, exponent string }{
		"no modulus":            {"", base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})},
		"no exponent":           {modulus, ""},
		"modulus is not base64": {"!!!!", base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})},
		"exponent is not base64": {
			modulus, "!!!!",
		},
		"exponent wraps an int": {modulus, base64.RawURLEncoding.EncodeToString(wrapping)},
		"exponent is wider than any key uses": {
			modulus, base64.RawURLEncoding.EncodeToString([]byte{0x7f, 0xff, 0xff, 0xff, 0xff}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			built, err := oidctoken.RSAPublicKey(tc.modulus, tc.exponent)
			if err == nil {
				t.Errorf("the jwk was accepted as a key with exponent %d", built.E)
			}
		})
	}
}
