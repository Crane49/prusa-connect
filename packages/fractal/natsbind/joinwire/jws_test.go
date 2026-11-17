package joinwire_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// testSigner signs with one NKEY pair.
type testSigner struct {
	pair nkeys.KeyPair
	pub  string
}

// newSigner creates an NKEY key pair with the given role.
func newSigner(t *testing.T, create func() (nkeys.KeyPair, error)) *testSigner {
	t.Helper()
	pair, err := create()
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		t.Fatalf("read the public half: %v", err)
	}
	return &testSigner{pair: pair, pub: pub}
}

// PublicKey gives the public half.
func (s *testSigner) PublicKey() (string, error) { return s.pub, nil }

// Sign signs the bytes it is given.
func (s *testSigner) Sign(data []byte) ([]byte, error) { return s.pair.Sign(data) }

// TestASignedJoinRequestVerifies checks that Verify accepts a join request
// signed with a user key and returns the fields Sign encoded.
func TestASignedJoinRequestVerifies(t *testing.T) {
	t.Parallel()
	signer := newSigner(t, nkeys.CreateUser)
	envelope, err := joinwire.Sign(joinwire.Request{
		Version:   fractal.Version,
		NodeID:    "printer-1",
		JoinToken: "a-token",
	}, signer)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req, err := joinwire.ProofVerifier{}.Verify(t.Context(), envelope)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if req.NodeID != "printer-1" {
		t.Errorf("the request names %q, want printer-1", req.NodeID)
	}
	if req.Identity != fractal.Identity(signer.pub) {
		t.Errorf("the request presents %q, want the key that signed it", req.Identity)
	}
	if req.Credential != "a-token" {
		t.Errorf("the request carries %q, want a-token", req.Credential)
	}
	if req.Version != fractal.Version {
		t.Errorf("the request names version %d, want %d", req.Version, fractal.Version)
	}
}

// TestASignedJoinRequestNamesItsPurpose checks the protected header Sign
// writes: the typ of a join request, EdDSA and the key that signed it.
func TestASignedJoinRequestNamesItsPurpose(t *testing.T) {
	t.Parallel()
	signer := newSigner(t, nkeys.CreateUser)
	envelope, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, signer)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	want := joinwire.Header{Typ: "fractal-join+jws", Alg: "EdDSA", NKey: signer.pub}
	if got := headerOf(t, envelope); got != want {
		t.Errorf("the header is %+v, want %+v", got, want)
	}
}

// TestAJoinRequestIsRefused checks that Verify returns the right error for
// each kind of invalid join request.
func TestAJoinRequestIsRefused(t *testing.T) {
	t.Parallel()
	user := newSigner(t, nkeys.CreateUser)
	good, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, user)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	account := newSigner(t, nkeys.CreateAccount)
	byAccount, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, account)
	if err != nil {
		t.Fatalf("sign with an account key: %v", err)
	}
	cases := []struct {
		name     string
		envelope []byte
		want     error
	}{
		{"not three parts", []byte("one.two"), joinwire.ErrNotCompact},
		{
			"no typ",
			rewriteHeader(t, good, joinwire.Header{Alg: joinwire.AlgEdDSA, NKey: user.pub}),
			joinwire.ErrBadType,
		},
		{
			"the typ of another purpose",
			rewriteHeader(t, good, joinwire.Header{Typ: "fractal-proof+jws", Alg: joinwire.AlgEdDSA, NKey: user.pub}),
			joinwire.ErrBadType,
		},
		{
			"another typ and another algorithm",
			rewriteHeader(t, good, joinwire.Header{Typ: "JWT", Alg: "HS256", NKey: user.pub}),
			joinwire.ErrBadType,
		},
		{"another algorithm", rewriteAlg(t, good, "HS256"), joinwire.ErrBadAlgorithm},
		{
			"no identity key",
			rewriteHeader(t, good, joinwire.Header{Typ: joinwire.JoinTyp, Alg: joinwire.AlgEdDSA}),
			joinwire.ErrNoNKey,
		},
		{"a changed body", tamper(t, good), joinwire.ErrBadSignature},
		{"a key that is not a user key", byAccount, joinwire.ErrNotAUserKey},
		{
			"over the size bound",
			[]byte(strings.Repeat("a", joinwire.DefaultMaxEnvelopeBytes+1)),
			joinwire.ErrEnvelopeTooLarge,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, verifyErr := (joinwire.ProofVerifier{}).Verify(
				t.Context(),
				c.envelope,
			); !errors.Is(
				verifyErr,
				c.want,
			) {
				t.Errorf("the refusal is %v, want %v", verifyErr, c.want)
			}
		})
	}
}

// TestNothingOfAnUnprovenRequestIsRead checks that Verify returns no field of
// a join request whose signature is invalid.
func TestNothingOfAnUnprovenRequestIsRead(t *testing.T) {
	t.Parallel()
	user := newSigner(t, nkeys.CreateUser)
	good, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, user)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req, verifyErr := (joinwire.ProofVerifier{}).Verify(t.Context(), tamper(t, good))
	if verifyErr == nil {
		t.Fatal("a changed request verified")
	}
	if req != (fractal.JoinRequest{}) {
		t.Errorf("the refusal carried %+v, want nothing", req)
	}
}

// headerOf decodes the protected header of a signed join request.
func headerOf(t *testing.T, envelope []byte) joinwire.Header {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(string(envelope), ".")[0])
	if err != nil {
		t.Fatalf("decode the header: %v", err)
	}
	var header joinwire.Header
	if unmarshalErr := json.Unmarshal(raw, &header); unmarshalErr != nil {
		t.Fatalf("parse the header: %v", unmarshalErr)
	}
	return header
}

// rewriteAlg replaces the algorithm in the header of a signed join request.
// The signature stays, so it no longer matches the header.
func rewriteAlg(t *testing.T, envelope []byte, alg string) []byte {
	t.Helper()
	header := headerOf(t, envelope)
	header.Alg = alg
	return rewriteHeader(t, envelope, header)
}

// rewriteHeader replaces the protected header of a signed request.
func rewriteHeader(t *testing.T, envelope []byte, header joinwire.Header) []byte {
	t.Helper()
	parts := strings.Split(string(envelope), ".")
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("encode the header: %v", err)
	}
	parts[0] = base64.RawURLEncoding.EncodeToString(raw)
	return []byte(strings.Join(parts, "."))
}

// tamper changes the body of a signed request.
func tamper(t *testing.T, envelope []byte) []byte {
	t.Helper()
	parts := strings.Split(string(envelope), ".")
	body, err := json.Marshal(joinwire.Request{Version: fractal.Version, NodeID: "another"})
	if err != nil {
		t.Fatalf("encode a body: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(body)
	return []byte(strings.Join(parts, "."))
}
