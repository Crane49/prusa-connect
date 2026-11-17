package joinwire_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// The RSA key sizes of the tests in this file, in bits. weakBits is under
// MinModulusBits.
const (
	strongBits = 2048
	weakBits   = 1024
)

// now is the current time for every test in this file.
var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// keySource answers with one key and counts how often it was asked. A verifier
// under test may be asked from several goroutines at once, and a test may share
// one source between parallel subtests, so the counter is atomic.
type keySource struct {
	key   *rsa.PublicKey
	kid   string
	err   error
	calls atomic.Int32
}

// Key gives the key stored under kid.
func (s *keySource) Key(_ context.Context, kid string) (*rsa.PublicKey, error) {
	s.calls.Add(1)
	switch {
	case s.err != nil:
		return nil, s.err
	case kid != s.kid:
		return nil, fmt.Errorf("kid %q: %w", kid, joinwire.ErrUnknownKey)
	}
	return s.key, nil
}

// claims is the claim set a test signs.
type claims struct {
	NodeID     fractal.NodeID   `json:"nodeId,omitempty"`
	ParentPath fractal.NodePath `json:"parentPath,omitempty"`
	Subject    string           `json:"sub,omitempty"`
	Expires    json.RawMessage  `json:"exp,omitempty"`
	NotBefore  json.RawMessage  `json:"nbf,omitempty"`
}

// mintKey builds an RSA key of the given size.
func mintKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("mint a key: %v", err)
	}
	return key
}

// signToken builds a join credential with the header and the claims it is
// given.
func signToken(t *testing.T, key *rsa.PrivateKey, alg, kid string, c claims) fractal.JoinCredential {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "kid": kid})
	if err != nil {
		t.Fatalf("encode the header: %v", err)
	}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("encode the claims: %v", err)
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(body)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return fractal.JoinCredential(input + "." + enc.EncodeToString(sig))
}

// seconds renders one instant as a numeric date.
func seconds(at time.Time) json.RawMessage {
	return json.RawMessage(strconv.FormatInt(at.Unix(), 10))
}

// goodClaims is the claim set every permitting case starts from.
func goodClaims() claims {
	return claims{
		NodeID:     "printer-1",
		ParentPath: "hq/brno",
		Expires:    seconds(now.Add(time.Hour)),
	}
}

// TestAJoinCredentialIsAccepted checks that TokenAuthorizer accepts a valid
// join credential, with a sub claim for the identity key and without one.
func TestAJoinCredentialIsAccepted(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	source := &keySource{key: &key.PublicKey, kid: "one"}
	authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}

	bound := goodClaims()
	bound.Subject = "UAAAA"
	bound.NotBefore = seconds(now.Add(-time.Minute))
	for _, c := range []struct {
		name   string
		claims claims
	}{
		{"with no subject", goodClaims()},
		{"bound to the identity key", bound},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			token := signToken(t, key, joinwire.AlgRS256, "one", c.claims)
			req := fractal.JoinRequest{
				Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: token,
			}
			if err := authorizer.Authorize(t.Context(), req, "hq/brno"); err != nil {
				t.Errorf("the credential was refused: %v", err)
			}
		})
	}
}

// TestAJoinCredentialIsRefused checks that TokenAuthorizer returns the right
// error for each kind of invalid join credential. Each error wraps
// join.ErrUnauthorized, so the node denies with fractal.CodeJoinUnauthorized.
func TestAJoinCredentialIsRefused(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	other := mintKey(t, strongBits)
	weak := mintKey(t, weakBits)

	wrongNode := goodClaims()
	wrongNode.NodeID = "printer-2"
	wrongParent := goodClaims()
	wrongParent.ParentPath = "hq"
	wrongSubject := goodClaims()
	wrongSubject.Subject = "UBBBB"
	expired := goodClaims()
	expired.Expires = seconds(now.Add(-time.Second))
	expiresNow := goodClaims()
	expiresNow.Expires = seconds(now)
	tooEarly := goodClaims()
	tooEarly.NotBefore = seconds(now.Add(2 * time.Minute))
	noExpiry := goodClaims()
	noExpiry.Expires = nil

	cases := []struct {
		name   string
		key    *rsa.PrivateKey
		source *rsa.PublicKey
		alg    string
		kid    string
		claims claims
		want   error
	}{
		{"another algorithm", key, &key.PublicKey, "HS256", "one", goodClaims(), joinwire.ErrTokenAlgorithm},
		{
			"a key that did not sign it",
			other,
			&key.PublicKey,
			joinwire.AlgRS256,
			"one",
			goodClaims(),
			joinwire.ErrTokenSignature,
		},
		{
			"a modulus under the floor",
			weak,
			&weak.PublicKey,
			joinwire.AlgRS256,
			"one",
			goodClaims(),
			joinwire.ErrWeakKey,
		},
		{
			"a kid the source does not hold",
			key,
			&key.PublicKey,
			joinwire.AlgRS256,
			"two",
			goodClaims(),
			joinwire.ErrUnknownKey,
		},
		{"another node id", key, &key.PublicKey, joinwire.AlgRS256, "one", wrongNode, joinwire.ErrTokenMismatch},
		{"another node above", key, &key.PublicKey, joinwire.AlgRS256, "one", wrongParent, joinwire.ErrTokenMismatch},
		{
			"another identity key",
			key,
			&key.PublicKey,
			joinwire.AlgRS256,
			"one",
			wrongSubject,
			joinwire.ErrTokenMismatch,
		},
		{"an expiry that has passed", key, &key.PublicKey, joinwire.AlgRS256, "one", expired, joinwire.ErrTokenExpired},
		{
			"an expiry at this instant",
			key,
			&key.PublicKey,
			joinwire.AlgRS256,
			"one",
			expiresNow,
			joinwire.ErrTokenExpired,
		},
		{
			"a not-before beyond the tolerance",
			key,
			&key.PublicKey,
			joinwire.AlgRS256,
			"one",
			tooEarly,
			joinwire.ErrTokenTooEarly,
		},
		{"no expiry at all", key, &key.PublicKey, joinwire.AlgRS256, "one", noExpiry, joinwire.ErrBadNumericDate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			source := &keySource{key: c.source, kid: "one"}
			authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}
			token := signToken(t, c.key, c.alg, c.kid, c.claims)
			req := fractal.JoinRequest{
				Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: token,
			}
			err := authorizer.Authorize(t.Context(), req, "hq/brno")
			if !errors.Is(err, c.want) {
				t.Fatalf("the refusal is %v, want %v", err, c.want)
			}
			if !errors.Is(err, join.ErrUnauthorized) {
				t.Errorf("the refusal does not answer with the unauthorized code: %v", err)
			}
		})
	}
}

// TestAnotherAlgorithmIsRefusedBeforeAnyKeyIsResolved checks the order
// fractal-nats.md requires: TokenAuthorizer refuses a credential whose alg is
// not RS256 before it asks the key source for a key.
func TestAnotherAlgorithmIsRefusedBeforeAnyKeyIsResolved(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	source := &keySource{key: &key.PublicKey, kid: "one"}
	authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}
	token := signToken(t, key, "none", "one", goodClaims())
	req := fractal.JoinRequest{Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: token}
	if err := authorizer.Authorize(t.Context(), req, "hq/brno"); !errors.Is(err, joinwire.ErrTokenAlgorithm) {
		t.Fatalf("the refusal is %v, want the algorithm", err)
	}
	if got := source.calls.Load(); got != 0 {
		t.Errorf("the key source was asked %d times, want none", got)
	}
}

// TestAKeySourceThatCannotAnswerIsNotUnauthorized checks that an error of the
// key source does not wrap join.ErrUnauthorized, so the node denies as
// unavailable and never as unauthorized.
func TestAKeySourceThatCannotAnswerIsNotUnauthorized(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	for _, c := range []struct {
		name string
		err  error
	}{
		{"unreachable", errors.New("no route to the key source")},
		{"rate limited", joinwire.ErrRefreshRateLimited},
		{"holding nothing usable", joinwire.ErrNoUsableKey},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			source := &keySource{err: c.err}
			authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}
			token := signToken(t, key, joinwire.AlgRS256, "one", goodClaims())
			req := fractal.JoinRequest{
				Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: token,
			}
			err := authorizer.Authorize(t.Context(), req, "hq/brno")
			if err == nil {
				t.Fatal("an unreachable key source authorized the request")
			}
			if errors.Is(err, join.ErrUnauthorized) {
				t.Errorf("the refusal answers with the unauthorized code: %v", err)
			}
		})
	}
}

// TestEveryNumericDateFormIsRead checks that parseNumericDate accepts every
// number form RFC 7519 allows and refuses a value that is out of range.
func TestEveryNumericDateFormIsRead(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	unix := now.Add(time.Hour).Unix()
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"an integer", strconv.FormatInt(unix, 10), true},
		{"a fraction", fmt.Sprintf("%d.500", unix), true},
		{"an exponent", "1.8e9", true},
		{"a whole number written as a fraction", fmt.Sprintf("%d.0", unix), true},
		{"text", `"1800000000"`, false},
		{"far out of range", "1e300", false},
		{"negative and out of range", "-1e300", false},
		{"not a number", "true", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			source := &keySource{key: &key.PublicKey, kid: "one"}
			authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}
			held := goodClaims()
			held.Expires = json.RawMessage(c.value)
			token := signToken(t, key, joinwire.AlgRS256, "one", held)
			req := fractal.JoinRequest{
				Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: token,
			}
			err := authorizer.Authorize(t.Context(), req, "hq/brno")
			if c.ok && err != nil {
				t.Errorf("the credential was refused: %v", err)
			}
			if !c.ok && !errors.Is(err, joinwire.ErrBadNumericDate) {
				t.Errorf("the refusal is %v, want a bad numeric date", err)
			}
		})
	}
}

// TestACredentialIsRefusedBeforeItIsParsed checks that TokenAuthorizer refuses
// a missing credential and a credential over MaxTokenBytes before it parses
// anything.
func TestACredentialIsRefusedBeforeItIsParsed(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	source := &keySource{key: &key.PublicKey, kid: "one"}
	authorizer := joinwire.TokenAuthorizer{Keys: source, Now: func() time.Time { return now }}
	cases := []struct {
		name       string
		credential fractal.JoinCredential
		want       error
	}{
		{"no credential", "", joinwire.ErrNoToken},
		{
			"over the size bound",
			fractal.JoinCredential(strings.Repeat("a", joinwire.DefaultMaxTokenBytes+1)),
			joinwire.ErrTokenTooLarge,
		},
		{"not three parts", "one.two", joinwire.ErrNotCompact},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req := fractal.JoinRequest{
				Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA", Credential: c.credential,
			}
			if err := authorizer.Authorize(t.Context(), req, "hq/brno"); !errors.Is(err, c.want) {
				t.Errorf("the refusal is %v, want %v", err, c.want)
			}
		})
	}
}
