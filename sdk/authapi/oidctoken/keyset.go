package oidctoken

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultKeyTTL is how long a fetched key set is trusted when the issuer's
// answer has no cache header.
const DefaultKeyTTL = 7200 * time.Second

// MinKeyTTL is the floor under any max-age the issuer sends, and the window an
// unknown key waits before the issuer is asked again. It stops tokens that
// name unknown keys from turning into a flood of requests at the issuer.
const MinKeyTTL = 30 * time.Second

// maxDocumentBytes bounds the key set read.
const maxDocumentBytes = 1 << 20

// The refusals a key lookup reports. A caller inspects them with errors.Is and
// wraps the one that applies in its own refusal.
var (
	// ErrKeysUnavailable reports a key set that could not be fetched or read,
	// so no signature can be checked. A retry after the issuer recovers can
	// pass.
	ErrKeysUnavailable = errors.New("oidctoken: the issuer's signing keys are unavailable")
	// ErrUnknownKey reports a key id the issuer does not publish.
	ErrUnknownKey = errors.New("oidctoken: the issuer does not publish that key")
)

// KeySet is an issuer's published signing keys. They are fetched on demand,
// kept for as long as the issuer's cache headers say, and refetched at most
// once per MinKeyTTL when a token names an unknown key.
type KeySet struct {
	uri    string
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	keys map[string]crypto.PublicKey
	// fetched is when the held set was read, and attempted is when a read was
	// last tried. They differ after a failed refresh: the set is still the old
	// one, and the throttle still counts the attempt.
	fetched   time.Time
	attempted time.Time
	ttl       time.Duration
}

// NewKeySet returns a key set read from uri through client. A nil clock means
// time.Now. Nothing is fetched until the first lookup.
func NewKeySet(uri string, client *http.Client, now func() time.Time) *KeySet {
	if now == nil {
		now = time.Now
	}

	if client == nil {
		client = http.DefaultClient
	}

	return &KeySet{uri: uri, client: client, now: now, ttl: DefaultKeyTTL}
}

// URI is where the set is read from.
func (k *KeySet) URI() string { return k.uri }

// Lookup returns the RSA key a token names, refreshing the set when it is
// stale or when the key is unknown and the window has passed. A token that
// names no key is answered when the issuer publishes a single RSA key.
func (k *KeySet) Lookup(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	key, err := k.lookup(ctx, kid, func(key crypto.PublicKey) bool {
		_, ok := key.(*rsa.PublicKey)

		return ok
	})
	if err != nil {
		return nil, err
	}

	rsaKey, _ := key.(*rsa.PublicKey)

	return rsaKey, nil
}

// Key returns the key a token signed with alg names: an *rsa.PublicKey, an
// *ecdsa.PublicKey or an ed25519.PublicKey. It refreshes the set the way
// Lookup does. A token that names no key is answered when the issuer
// publishes a single key that alg can use.
func (k *KeySet) Key(ctx context.Context, kid, alg string) (crypto.PublicKey, error) {
	method, ok := methods[alg]
	if !ok || method.keyed == nil {
		return nil, fmt.Errorf("%w: %q is not checked against a published key", ErrAlgorithm, alg)
	}

	return k.lookup(ctx, kid, method.keyed)
}

// lookup is Lookup and Key over the keys fits accepts.
func (k *KeySet) lookup(ctx context.Context, kid string, fits func(crypto.PublicKey) bool) (crypto.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	now := k.now()
	held, ok := k.pick(kid, fits)

	if ok && !k.stale(now) {
		return held, nil
	}

	if now.Sub(k.attempted) < MinKeyTTL {
		if ok {
			// The set is past its age and the issuer was asked a moment ago.
			// A key the issuer published beats no key while the window runs.
			return held, nil
		}

		return nil, unpublishedKey(kid)
	}

	// The window opens on the attempt, so a failing issuer is throttled too.
	k.attempted = now

	if err := k.refresh(ctx, now); err != nil {
		if ok {
			return held, nil
		}

		return nil, err
	}

	if key, found := k.pick(kid, fits); found {
		return key, nil
	}

	return nil, unpublishedKey(kid)
}

// stale reports whether the held set has outlived what the issuer allowed.
func (k *KeySet) stale(now time.Time) bool {
	return k.keys == nil || now.Sub(k.fetched) >= k.ttl
}

func unpublishedKey(kid string) error {
	return fmt.Errorf("%w: signed with key %q, which the issuer does not publish", ErrUnknownKey, kid)
}

// pick finds the key a token names, of a kind fits accepts. A token that
// names no key is answered when a single such key is published.
func (k *KeySet) pick(kid string, fits func(crypto.PublicKey) bool) (crypto.PublicKey, bool) {
	if kid != "" {
		key, ok := k.keys[kid]

		return key, ok && fits(key)
	}

	var (
		found crypto.PublicKey
		count int
	)

	for _, key := range k.keys {
		if fits(key) {
			found = key
			count++
		}
	}

	return found, count == 1
}

// refresh fetches the key set and reads how long the issuer lets it be kept.
// A failed fetch leaves the old set in place.
func (k *KeySet) refresh(ctx context.Context, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.uri, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %s", ErrKeysUnavailable, k.uri, resp.Status)
	}

	var document struct {
		Keys []jwk `json:"keys"`
	}

	if err = json.NewDecoder(io.LimitReader(resp.Body, maxDocumentBytes)).Decode(&document); err != nil {
		return fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}

	found := make(map[string]crypto.PublicKey, len(document.Keys))

	for _, published := range document.Keys {
		// A key published for encryption never checks a signature.
		if published.Use != "" && published.Use != "sig" {
			continue
		}

		// A key this cannot rebuild is skipped. One unreadable entry beside
		// good ones is no reason to refuse every token.
		key, keyErr := published.publicKey()
		if keyErr != nil {
			continue
		}

		found[published.ID] = key
	}

	if len(found) == 0 {
		return fmt.Errorf("%w: the issuer publishes no signing key this server can read", ErrKeysUnavailable)
	}

	k.keys = found
	k.fetched = now
	k.ttl = ttlFrom(resp.Header.Get("Cache-Control"))

	return nil
}

// ttlFrom reads how long the issuer lets the document be kept. Only max-age
// is read. No header, or one that forbids caching, takes the default.
func ttlFrom(cacheControl string) time.Duration {
	for directive := range strings.SplitSeq(cacheControl, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		if !found || !strings.EqualFold(name, "max-age") {
			continue
		}

		seconds, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64)
		if err != nil || seconds <= 0 {
			return DefaultKeyTTL
		}

		return max(time.Duration(seconds)*time.Second, MinKeyTTL)
	}

	return DefaultKeyTTL
}
