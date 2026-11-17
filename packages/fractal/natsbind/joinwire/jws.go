package joinwire

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nats-io/nkeys"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// DefaultMaxEnvelopeBytes is the largest join request ProofVerifier accepts,
// in bytes. Every machine that can reach the client listener can send a join
// request, so ProofVerifier checks the size before it parses the request.
const DefaultMaxEnvelopeBytes = 8 << 10

// jwsParts is the number of parts in a compact JWS: the protected header, the
// body and the signature, separated by dots.
const jwsParts = 3

// JoinTyp is the typ the protected header of a join request names. The
// identity key may sign other kinds of JWS, so each names its purpose and a
// verifier refuses one made for another.
const JoinTyp = "fractal-join+jws"

// Signer signs with the node's identity NKEY, the Ed25519 key pair NATS uses
// to authenticate. It has no method that returns the private seed, so the seed
// is in no value this package returns.
type Signer interface {
	// PublicKey returns the public key in the text encoding of an NKEY.
	PublicKey() (string, error)
	// Sign returns the signature of data.
	Sign(data []byte) ([]byte, error)
}

// Sign builds the join request as a compact JWS. The signature covers the
// protected header and the body, so it also covers the typ and the public key
// in the header.
func Sign(req Request, signer Signer) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode the join request: %w", err)
	}
	return SignCompact(JoinTyp, body, signer)
}

// ProofTyp is the typ of the proof a node sends on every request for a file
// object, so a verifier refuses a join request offered as a proof and a proof
// offered as a join request.
const ProofTyp = "fractal-proof+jws"

// SignCompact builds a compact JWS of body whose protected header names typ,
// EdDSA and the public key of signer.
func SignCompact(typ string, body []byte, signer Signer) ([]byte, error) {
	nkey, err := signer.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("read the identity key: %w", err)
	}
	header, err := json.Marshal(Header{Typ: typ, Alg: AlgEdDSA, NKey: nkey})
	if err != nil {
		return nil, fmt.Errorf("encode the jws header: %w", err)
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(body)
	sig, err := signer.Sign([]byte(input))
	if err != nil {
		return nil, fmt.Errorf("sign the jws: %w", err)
	}
	return []byte(input + "." + enc.EncodeToString(sig)), nil
}

// ProofVerifier decodes a join request and returns it only when its signature
// matches the public key in its header. That signature proves the sender has
// the private key. ProofVerifier implements join.ProofVerifier of the sdk.
//
// The order of the checks is required. It refuses a wrong typ, then a wrong
// algorithm, before it reads the key, and it decodes the body only after it
// has verified the signature.
type ProofVerifier struct {
	// MaxEnvelopeBytes is the largest join request accepted, in bytes. Zero or
	// less means DefaultMaxEnvelopeBytes.
	MaxEnvelopeBytes int
}

// Verify checks the size and the signature of a join request and returns the
// request with the sender's public key as its Identity.
func (v ProofVerifier) Verify(_ context.Context, envelope []byte) (fractal.JoinRequest, error) {
	limit := v.MaxEnvelopeBytes
	if limit <= 0 {
		limit = DefaultMaxEnvelopeBytes
	}
	if len(envelope) > limit {
		return fractal.JoinRequest{}, fmt.Errorf("%d bytes over %d: %w", len(envelope), limit, ErrEnvelopeTooLarge)
	}
	req, identity, err := Verify(string(envelope))
	if err != nil {
		return fractal.JoinRequest{}, err
	}
	return fractal.JoinRequest{
		Version:    req.Version,
		NodeID:     req.NodeID,
		Identity:   identity,
		Credential: req.JoinToken,
	}, nil
}

// Verify parses a compact JWS and refuses every typ except JoinTyp and every
// algorithm except EdDSA. It verifies the signature with the public key in the
// header, and decodes the body only after that.
func Verify(compact string) (Request, fractal.Identity, error) {
	body, identity, err := VerifyCompact(compact, JoinTyp)
	if err != nil {
		return Request{}, "", err
	}
	var req Request
	if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
		return Request{}, "", fmt.Errorf("parse the join request: %w", unmarshalErr)
	}
	return req, identity, nil
}

// VerifyCompact checks a compact JWS in the required order:
// the typ, then the algorithm, then the signature against the key in the
// header. It returns the body undecoded, only after the signature verified.
func VerifyCompact(compact, typ string) ([]byte, fractal.Identity, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != jwsParts {
		return nil, "", fmt.Errorf("%d parts: %w", len(parts), ErrNotCompact)
	}
	enc := base64.RawURLEncoding
	rawHeader, err := enc.DecodeString(parts[0])
	if err != nil {
		return nil, "", fmt.Errorf("decode the jws header: %w", err)
	}
	var header Header
	if unmarshalErr := json.Unmarshal(rawHeader, &header); unmarshalErr != nil {
		return nil, "", fmt.Errorf("parse the jws header: %w", unmarshalErr)
	}
	if header.Typ != typ {
		return nil, "", fmt.Errorf("typ %q: %w", header.Typ, ErrBadType)
	}
	if header.Alg != AlgEdDSA {
		return nil, "", fmt.Errorf("alg %q: %w", header.Alg, ErrBadAlgorithm)
	}
	if header.NKey == "" {
		return nil, "", ErrNoNKey
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return nil, "", fmt.Errorf("decode the jws signature: %w", err)
	}
	if verifyErr := verifyNKey(header.NKey, []byte(parts[0]+"."+parts[1]), sig); verifyErr != nil {
		return nil, "", verifyErr
	}
	body, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, "", fmt.Errorf("decode the jws body: %w", err)
	}
	return body, fractal.Identity(header.NKey), nil
}

// verifyNKey verifies a signature with a public user NKEY.
//
// It checks first that the key is a user key. An NKEY has a role: operator,
// account or user. Every role signs with Ed25519, so a request signed with an
// account key or an operator key would pass the signature check. It would
// fail only later, when the node above issues the connection credentials, and
// by then that node has already created resources for the request.
func verifyNKey(nkey string, input, sig []byte) error {
	if !nkeys.IsValidPublicUserKey(nkey) {
		return ErrNotAUserKey
	}
	pub, err := nkeys.FromPublicKey(nkey)
	if err != nil {
		return fmt.Errorf("read the identity key: %w", err)
	}
	if verifyErr := pub.Verify(input, sig); verifyErr != nil {
		return ErrBadSignature
	}
	return nil
}
