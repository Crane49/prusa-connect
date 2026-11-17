package joinwire_test

import (
	"errors"
	"testing"

	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// verified is what VerifyCompact returns, held as one value so a test compares
// the whole of it.
type verified struct {
	body     string
	identity fractal.Identity
}

// TestProofRoundTrip: a body signed under ProofTyp comes back unchanged with
// the key that signed it.
func TestProofRoundTrip(t *testing.T) {
	t.Parallel()
	signer := newSigner(t, nkeys.CreateUser)
	compact, err := joinwire.SignCompact(joinwire.ProofTyp, []byte(`{"iat":1700000000}`), signer)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	body, identity, err := joinwire.VerifyCompact(string(compact), joinwire.ProofTyp)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	got := verified{body: string(body), identity: identity}
	want := verified{body: `{"iat":1700000000}`, identity: fractal.Identity(signer.pub)}
	if got != want {
		t.Errorf("VerifyCompact returned %+v, want %+v", got, want)
	}
}

// TestProofIsNoJoinRequest: a proof offered where a join request is expected
// is refused by its typ, and so is a join request offered as a proof.
func TestProofIsNoJoinRequest(t *testing.T) {
	t.Parallel()
	signer := newSigner(t, nkeys.CreateUser)
	proof, err := joinwire.SignCompact(joinwire.ProofTyp, []byte(`{}`), signer)
	if err != nil {
		t.Fatalf("sign the proof: %v", err)
	}
	if _, _, err = joinwire.Verify(string(proof)); !errors.Is(err, joinwire.ErrBadType) {
		t.Errorf("Verify of a proof returned %v, want ErrBadType", err)
	}
	request, err := joinwire.Sign(joinwire.Request{Version: fractal.Version, NodeID: "printer-1"}, signer)
	if err != nil {
		t.Fatalf("sign the join request: %v", err)
	}
	if _, _, err = joinwire.VerifyCompact(string(request), joinwire.ProofTyp); !errors.Is(err, joinwire.ErrBadType) {
		t.Errorf("VerifyCompact of a join request returned %v, want ErrBadType", err)
	}
}
