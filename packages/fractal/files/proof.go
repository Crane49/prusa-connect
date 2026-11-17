package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// ProofHeader is the request header with the caller's proof of its identity.
const ProofHeader = "Fractal-Proof"

// maxUnixSeconds keeps iat inside the seconds a time.Time and a float64 hold
// exactly.
const maxUnixSeconds = 1 << 53

// ErrProof means a proof whose signature verified carries a payload that does
// not hold: it is not a JSON object, it has no whole-number iat, or its iat
// is further from this server's clock than the tolerance.
var ErrProof = errors.New("the proof does not hold")

// SignProof returns a proof of the signer's identity, made at now.
func SignProof(s joinwire.Signer, now time.Time) (string, error) {
	body, err := json.Marshal(struct {
		IAT int64 `json:"iat"`
	}{IAT: now.Unix()})
	if err != nil {
		return "", fmt.Errorf("encode the proof: %w", err)
	}

	compact, err := joinwire.SignCompact(joinwire.ProofTyp, body, s)
	if err != nil {
		return "", fmt.Errorf("sign the proof: %w", err)
	}

	return string(compact), nil
}

// VerifyProof returns the identity that made a proof. joinwire checks typ, alg
// and the signature before it returns the payload, so nothing here reads a
// payload whose signature did not verify.
func VerifyProof(header string, now time.Time, tolerance time.Duration) (fractal.Identity, error) {
	body, identity, err := joinwire.VerifyCompact(header, joinwire.ProofTyp)
	if err != nil {
		return "", fmt.Errorf("verify the proof: %w", err)
	}

	issued, err := issuedAt(body)
	if err != nil {
		return "", err
	}

	if skew := now.Sub(issued).Abs(); skew > tolerance {
		return "", fmt.Errorf("%w: it was made %s away from this clock", ErrProof, skew)
	}

	return identity, nil
}

// issuedAt reads iat from a verified payload.
func issuedAt(body []byte) (time.Time, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return time.Time{}, fmt.Errorf("%w: the payload is not a JSON object", ErrProof)
	}

	raw, ok := payload["iat"]
	if !ok {
		return time.Time{}, fmt.Errorf("%w: the payload has no iat", ErrProof)
	}

	// A JSON number may be written as 1.8e9, so iat is read as a float and
	// must be whole.
	seconds, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || seconds != math.Trunc(seconds) || math.Abs(seconds) > maxUnixSeconds {
		return time.Time{}, fmt.Errorf("%w: iat is not a whole number of seconds", ErrProof)
	}

	return time.Unix(int64(seconds), 0), nil
}
