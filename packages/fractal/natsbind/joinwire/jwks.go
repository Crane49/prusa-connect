package joinwire

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// The defaults of a JWKS key source.
const (
	// DefaultMinRefreshInterval is the shortest time between two fetches of
	// the key document. A join request with an unknown kid, the key ID in a
	// token's header, causes a fetch, and every machine that can reach the
	// join endpoint can send one. So the fetches have a rate limit.
	DefaultMinRefreshInterval = time.Minute
	// DefaultMaxDocumentBytes is the largest key document accepted, in bytes.
	DefaultMaxDocumentBytes = 1 << 20
	// DefaultFetchTimeout is how long one fetch of the key document may take.
	DefaultFetchTimeout = 10 * time.Second
)

// The errors of a key source. For each of them the node denies the join
// request as unavailable and never with fractal.CodeJoinUnauthorized, because
// the node could not check the join credential.
var (
	// ErrRefreshRateLimited means the key source did not fetch the key
	// document, because the last fetch was less than MinRefreshInterval ago.
	ErrRefreshRateLimited = errors.New("the key document was fetched too recently")
	// ErrNoUsableKey means the key document has no key this package can
	// verify with.
	ErrNoUsableKey = errors.New("the key document holds no usable key")
	// ErrPrivateKeyMaterial means the key document has a private key member.
	// The key source then refuses the whole document: a server that publishes
	// a private key cannot be trusted.
	ErrPrivateKeyMaterial = errors.New("the key document carries private key material")
	// ErrDocumentTooLarge means the key document has more bytes than
	// MaxDocumentBytes.
	ErrDocumentTooLarge = errors.New("the key document is over the size bound")
)

// JWKS fetches RSA public keys from a JWKS endpoint, a URL that serves a JSON
// list of public keys, and caches them.
//
// When a fetch fails it keeps the cached keys. When a token names a kid that
// is not cached it fetches again, but at most once per MinRefreshInterval. It
// skips a key it cannot use, and it refuses a whole document that has a
// private key member.
type JWKS struct {
	// URL is the address of the key document. It is required.
	URL string
	// Client is the HTTP client. Nil means a client with FetchTimeout.
	Client *http.Client
	// Logger receives a warning for every skipped key. Nil means no log.
	Logger *slog.Logger
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// MinRefreshInterval is the shortest time between two fetches. Zero or
	// less means DefaultMinRefreshInterval.
	MinRefreshInterval time.Duration
	// FetchTimeout is how long one fetch may take. Zero or less means
	// DefaultFetchTimeout.
	FetchTimeout time.Duration
	// MaxDocumentBytes is the largest key document accepted, in bytes. Zero
	// or less means DefaultMaxDocumentBytes.
	MaxDocumentBytes int64

	// fetchMu lets one fetch run at a time. Many tokens with unknown kids at
	// once then cause one HTTP request.
	fetchMu sync.Mutex

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	last    time.Time
	fetched bool
}

// NewJWKS returns a key source that fetches the key document at url.
func NewJWKS(url string) *JWKS { return &JWKS{URL: url} }

// Key returns the public key with this kid. When the kid is not cached it
// fetches the key document again, unless the last fetch was less than
// MinRefreshInterval ago.
func (s *JWKS) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if key, ok := s.cached(kid); ok {
		return key, nil
	}
	if err := s.refresh(ctx); err != nil {
		return nil, err
	}
	if key, ok := s.cached(kid); ok {
		return key, nil
	}
	if s.count() == 0 {
		return nil, fmt.Errorf("%s: %w", s.URL, ErrNoUsableKey)
	}
	return nil, fmt.Errorf("kid %q: %w", kid, ErrUnknownKey)
}

// Len returns the number of cached keys.
func (s *JWKS) Len() int { return s.count() }

// cached returns the cached key with this kid.
func (s *JWKS) cached(kid string) (*rsa.PublicKey, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[kid]
	return key, ok
}

// count returns the number of cached keys.
func (s *JWKS) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys)
}

// refresh fetches the key document, unless the last fetch was less than
// MinRefreshInterval ago. A failed fetch counts as a fetch for that limit.
// When the fetch fails refresh keeps the cached keys, so a token signed with a
// cached key still verifies.
func (s *JWKS) refresh(ctx context.Context) error {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	now := s.now()
	s.mu.Lock()
	wait := s.minRefresh()
	tooSoon := s.fetched && now.Sub(s.last) < wait
	s.mu.Unlock()
	if tooSoon {
		return fmt.Errorf("%s: %w", s.URL, ErrRefreshRateLimited)
	}

	keys, err := s.fetch(ctx)
	s.mu.Lock()
	s.fetched = true
	s.last = now
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.keys = keys
	s.mu.Unlock()
	return nil
}

// fetch downloads the key document and returns the usable keys by kid.
func (s *JWKS) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	if s.URL == "" {
		return nil, errors.New("jwks: no url is configured")
	}
	timeout := s.FetchTimeout
	if timeout <= 0 {
		timeout = DefaultFetchTimeout
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build the key document request: %w", err)
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", s.URL, err)
	}
	defer func() { _ = resp.Body.Close() }() // A failed close of a body that was read changes nothing.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", s.URL, resp.StatusCode)
	}
	limit := s.MaxDocumentBytes
	if limit <= 0 {
		limit = DefaultMaxDocumentBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.URL, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s: over %d bytes: %w", s.URL, limit, ErrDocumentTooLarge)
	}
	return s.parse(ctx, body)
}

// jwksKey is one key of the key document. D, P, Q, DP, DQ, QI and K are
// private key members. parse reads them only to refuse a document that has
// one.
type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	D   string `json:"d"`
	P   string `json:"p"`
	Q   string `json:"q"`
	DP  string `json:"dp"`
	DQ  string `json:"dq"`
	QI  string `json:"qi"`
	K   string `json:"k"`
}

// private reports whether the key has a private key member.
func (k jwksKey) private() bool {
	return k.D != "" || k.P != "" || k.Q != "" || k.DP != "" || k.DQ != "" || k.QI != "" || k.K != ""
}

// jwksDocument is the JSON document a JWKS endpoint serves.
type jwksDocument struct {
	Keys []jwksKey `json:"keys"`
}

// parse decodes a key document and returns the usable keys by kid. It skips a
// key it cannot use and logs a warning. It refuses the whole document when any
// key has a private key member.
func (s *JWKS) parse(ctx context.Context, body []byte) (map[string]*rsa.PublicKey, error) {
	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.URL, err)
	}
	for _, k := range doc.Keys {
		if k.private() {
			return nil, fmt.Errorf("%s: %w", s.URL, ErrPrivateKeyMaterial)
		}
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		key, err := rsaPublicKey(k)
		if err != nil {
			s.logger().WarnContext(ctx, "a key in the key document was skipped",
				"url", s.URL, "kid", k.Kid, "reason", err)
			continue
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s: %w", s.URL, ErrNoUsableKey)
	}
	return keys, nil
}

// maxExponentBits is the largest public exponent accepted, in bits. RSA keys
// in use have a small public exponent.
const maxExponentBits = 32

// rsaPublicKey builds an RSA public key from one key of the document. When it
// returns an error, parse skips that key and continues with the next one.
func rsaPublicKey(k jwksKey) (*rsa.PublicKey, error) {
	switch {
	case k.Kid == "":
		return nil, errors.New("the key has no kid")
	case k.Kty != "RSA":
		return nil, fmt.Errorf("kty %q is not RSA", k.Kty)
	case k.Use != "" && k.Use != "sig":
		return nil, fmt.Errorf("use %q is not sig", k.Use)
	case k.Alg != "" && k.Alg != AlgRS256:
		return nil, fmt.Errorf("alg %q is not %s", k.Alg, AlgRS256)
	}
	modulus, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode the modulus: %w", err)
	}
	exponent, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode the exponent: %w", err)
	}
	e := new(big.Int).SetBytes(exponent)
	if e.BitLen() == 0 || e.BitLen() > maxExponentBits {
		return nil, fmt.Errorf("the exponent is %d bits", e.BitLen())
	}
	key := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: int(e.Int64())}
	if bits := key.N.BitLen(); bits < MinModulusBits {
		return nil, fmt.Errorf("%w: %d bits", ErrWeakKey, bits)
	}
	return key, nil
}

// minRefresh returns MinRefreshInterval, or its default when it is not set.
func (s *JWKS) minRefresh() time.Duration {
	if s.MinRefreshInterval <= 0 {
		return DefaultMinRefreshInterval
	}
	return s.MinRefreshInterval
}

// now returns s.Now(), or time.Now() when Now is nil.
func (s *JWKS) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// logger returns s.Logger, or a logger that writes nothing when it is nil.
func (s *JWKS) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}
