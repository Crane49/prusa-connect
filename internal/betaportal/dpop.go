package betaportal

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	dpopType      = "dpop+jwt"
	jtiBytesCount = 16
	curveBytes    = 32
	signatureSize = 2 * curveBytes
)

type proofHeader struct {
	Type      string    `json:"typ"`
	Algorithm string    `json:"alg"`
	JWK       publicJWK `json:"jwk"`
}

type publicJWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	X       string `json:"x"`
	Y       string `json:"y"`
}

type proofClaims struct {
	JTI        string `json:"jti"`
	HTTPMethod string `json:"htm"`
	HTTPURI    string `json:"htu"`
	IssuedAt   int64  `json:"iat"`
	TokenHash  string `json:"ath,omitempty"`
}

// Authorize adds proof-of-possession authorization to an access-token
// protected request. An unpaired store leaves the request unchanged.
func (s *Store) Authorize(request *http.Request) error {
	if s == nil {
		return nil
	}
	token, key := s.access()
	if token == "" {
		return nil
	}

	proof, err := makeProof(key, request.Method, request.URL, token, s.portalTime(time.Now()))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "DPoP "+token)
	request.Header.Set("DPoP", proof)

	return nil
}

func makeProof(
	key *ecdsa.PrivateKey, method string, address *url.URL, token string, now time.Time,
) (string, error) {
	if key == nil || address == nil {
		return "", errors.New("build DPoP proof: key and request URL are required")
	}
	jtiBytes := make([]byte, jtiBytesCount)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", fmt.Errorf("build DPoP proof identifier: %w", err)
	}

	public, err := key.PublicKey.Bytes()
	if err != nil {
		return "", fmt.Errorf("encode DPoP public key: %w", err)
	}
	header := proofHeader{
		Type:      dpopType,
		Algorithm: "ES256",
		JWK: publicJWK{
			KeyType: "EC",
			Curve:   "P-256",
			X:       base64.RawURLEncoding.EncodeToString(public[1 : curveBytes+1]),
			Y:       base64.RawURLEncoding.EncodeToString(public[curveBytes+1:]),
		},
	}
	claims := proofClaims{
		JTI:        base64.RawURLEncoding.EncodeToString(jtiBytes),
		HTTPMethod: strings.ToUpper(method),
		HTTPURI:    canonicalHTU(address),
		IssuedAt:   now.Unix(),
	}
	if token != "" {
		digest := sha256.Sum256([]byte(token))
		claims.TokenHash = base64.RawURLEncoding.EncodeToString(digest[:])
	}

	encodedHeader, err := encodeJWTPart(header)
	if err != nil {
		return "", fmt.Errorf("build DPoP header: %w", err)
	}
	encodedClaims, err := encodeJWTPart(claims)
	if err != nil {
		return "", fmt.Errorf("build DPoP claims: %w", err)
	}
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	r, sigS, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign DPoP proof: %w", err)
	}

	signature := make([]byte, signatureSize)
	r.FillBytes(signature[:curveBytes])
	sigS.FillBytes(signature[curveBytes:])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func encodeJWTPart(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(content), nil
}

func canonicalHTU(address *url.URL) string {
	canonical := *address
	canonical.Scheme = strings.ToLower(canonical.Scheme)
	host := strings.ToLower(canonical.Hostname())
	port := canonical.Port()
	if port != "" && !isDefaultPort(canonical.Scheme, port) {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	canonical.Host = host
	canonical.User = nil
	canonical.RawQuery = ""
	canonical.ForceQuery = false
	canonical.Fragment = ""
	canonical.RawFragment = ""
	if canonical.Path == "" {
		canonical.Path = "/"
	}

	return canonical.String()
}

func isDefaultPort(scheme, port string) bool {
	return scheme == "https" && port == "443" || scheme == "http" && port == "80"
}

// clockTolerance is how far this server's clock may differ from the portal's
// before a proof is dated with the portal's time. The portal accepts a proof
// dated up to 60 seconds ahead of its clock.
const clockTolerance = 30 * time.Second

// maxSkew is the largest clock difference the store corrects. A Date header
// further off is a broken or hostile answer, not a clock that drifted.
const maxSkew = 24 * time.Hour

// portalTime is local corrected by the clock difference a refused proof showed.
func (s *Store) portalTime(local time.Time) time.Time {
	return local.Add(time.Duration(s.skew.Load()))
}

// learnSkew reads the portal's clock from a response Date header. It returns
// the portal's clock minus local, and whether that differs enough from the
// correction in use that a proof dated with it could be accepted now.
func (s *Store) learnSkew(local time.Time, date string) (time.Duration, bool) {
	portal, err := http.ParseTime(date)
	if err != nil {
		return time.Duration(s.skew.Load()), false
	}
	offset := portal.Sub(local)
	if offset.Abs() > maxSkew {
		return time.Duration(s.skew.Load()), false
	}
	if (offset - time.Duration(s.skew.Load())).Abs() < clockTolerance {
		return offset, false
	}
	s.skew.Store(int64(offset))

	return offset, true
}

// Rejected is called with a 401 response to a request Authorize signed. The
// portal names why in WWW-Authenticate (RFC 6750 §3):
//
//   - invalid_dpop_proof: when the Date header shows this server's clock is
//     wrong, later proofs carry the portal's time and Rejected returns true:
//     sign the request again and send it once more. The token stays.
//   - access_suspended: the tester's access is suspended. The pairing stays,
//     its status says so, and Rejected returns ErrAccessSuspended.
//   - invalid_token: the token is gone, and the pairing moves to
//     StateRevoked so the page asks for a new one.
//
// A portal that names no reason gets the clock check and then the
// revocation.
func (s *Store) Rejected(response *http.Response) (bool, error) {
	if s == nil || response == nil || response.StatusCode != http.StatusUnauthorized || response.Request == nil {
		return false, nil
	}
	// Only a refusal of a request signed with the token in use says anything
	// about the pairing or the clock: an unsigned request may have ended at
	// another origin, whose Date header this server must not trust.
	scheme, token, found := strings.Cut(response.Request.Header.Get("Authorization"), " ")
	if current, _ := s.access(); !found || !strings.EqualFold(scheme, "DPoP") || token == "" || token != current {
		return false, nil
	}
	reason := refusal(response.Header)
	switch reason {
	case refusalSuspended:
		s.suspended.Store(true)

		return false, ErrAccessSuspended
	case refusalInvalidToken:
	default:
		if _, learned := s.learnSkew(time.Now(), response.Header.Get("Date")); learned {
			return true, nil
		}
		if reason == refusalInvalidProof {
			return false, nil
		}
	}
	if err := s.revoke(token); err != nil {
		return false, fmt.Errorf("store the revoked Beta Portal pairing: %w", err)
	}

	return false, nil
}

// The errors a portal names in WWW-Authenticate when it refuses a device.
const (
	refusalInvalidToken = "invalid_token"
	refusalInvalidProof = "invalid_dpop_proof"
	refusalSuspended    = "access_suspended"
)

// refusal reads the error parameter of a WWW-Authenticate challenge.
func refusal(header http.Header) string {
	for _, challenge := range header.Values("WWW-Authenticate") {
		_, params, _ := strings.Cut(challenge, " ")
		for part := range strings.SplitSeq(params, ",") {
			name, value, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.EqualFold(name, "error") {
				return strings.Trim(value, `"`)
			}
		}
	}

	return ""
}
