package oidctoken

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// keyServer publishes one RSA key under a header the test chooses and counts
// what it costs.
type keyServer struct {
	*httptest.Server

	hits         atomic.Int64
	cacheControl atomic.Pointer[string]
}

func newKeyServer(t *testing.T) *keyServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}

	server := &keyServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		server.hits.Add(1)

		if header := server.cacheControl.Load(); header != nil {
			w.Header().Set("Cache-Control", *header)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
		}}})
	}))
	t.Cleanup(server.Close)

	return server
}

// clock is a time the test moves by hand.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

// lookup asks for the one key the server publishes and fails the test on a
// refusal.
func lookup(t *testing.T, keys *KeySet) {
	t.Helper()

	if _, err := keys.Lookup(t.Context(), "k1"); err != nil {
		t.Fatalf("looking up k1: %v", err)
	}
}

// The key set is trusted for the max-age the issuer sends, and refetched on the
// first lookup after it, so a rotation the issuer announces through its cache
// header is picked up when the issuer said it would be.
func TestTheKeySetHonoursTheIssuersMaxAge(t *testing.T) {
	t.Parallel()

	server := newKeyServer(t)
	header := "public, max-age=600"
	server.cacheControl.Store(&header)

	now := &clock{at: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	keys := NewKeySet(server.URL, server.Client(), now.now)

	lookup(t, keys)

	if hits := server.hits.Load(); hits != 1 {
		t.Fatalf("the first lookup cost %d fetches, want 1", hits)
	}

	if keys.ttl != 600*time.Second {
		t.Fatalf("ttl = %s, want 600s", keys.ttl)
	}

	now.at = now.at.Add(599 * time.Second)
	lookup(t, keys)

	if hits := server.hits.Load(); hits != 1 {
		t.Errorf("a lookup inside max-age reached the issuer: %d fetches", hits)
	}

	now.at = now.at.Add(2 * time.Second)
	lookup(t, keys)

	if hits := server.hits.Load(); hits != 2 {
		t.Errorf("a lookup past max-age did not refetch: %d fetches", hits)
	}
}

// No cache header, and a header that forbids caching, both take the 7200 second
// default. A verifier that could not cache would call the issuer on every
// request.
func TestNoCacheHeaderTakesTheCloudsDefault(t *testing.T) {
	t.Parallel()

	for name, header := range map[string]string{
		"none":         "",
		"no-store":     "no-store",
		"max-age zero": "max-age=0",
		"unreadable":   "max-age=soon",
	} {
		if got := ttlFrom(header); got != DefaultKeyTTL {
			t.Errorf("%s: ttlFrom(%q) = %s, want the default %s", name, header, got, DefaultKeyTTL)
		}
	}

	if got := ttlFrom("max-age=1"); got != MinKeyTTL {
		t.Errorf("a max-age under the floor gave %s, want the floor %s", got, MinKeyTTL)
	}

	if got := ttlFrom(`private, max-age="3600", must-revalidate`); got != 3600*time.Second {
		t.Errorf("a quoted max-age gave %s, want 3600s", got)
	}
}

// A refetch that fails leaves the held keys in place. The issuer's keys are
// still the issuer's while it is down, and refusing every caller for the
// length of somebody else's outage helps nobody.
func TestAFailedRefreshKeepsTheHeldKeys(t *testing.T) {
	t.Parallel()

	server := newKeyServer(t)
	now := &clock{at: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	keys := NewKeySet(server.URL, server.Client(), now.now)

	lookup(t, keys)

	server.Close()
	now.at = now.at.Add(DefaultKeyTTL + time.Second)

	if _, err := keys.Lookup(t.Context(), "k1"); err != nil {
		t.Fatalf("a stale key the issuer published was dropped while the issuer is down: %v", err)
	}

	if _, err := keys.Lookup(t.Context(), "k2"); err == nil {
		t.Fatal("a key never held was answered")
	}
}

// An unreachable issuer is reported as its own error, so a caller can tell an
// outage from a forgery.
func TestAnUnreachableIssuerIsItsOwnError(t *testing.T) {
	t.Parallel()

	server := newKeyServer(t)
	server.Close()

	_, err := NewKeySet(server.URL, server.Client(), nil).Lookup(t.Context(), "k1")
	if err == nil {
		t.Fatal("a dead issuer answered a key")
	}

	if !errors.Is(err, ErrKeysUnavailable) {
		t.Fatalf("want ErrKeysUnavailable, got %v", err)
	}
}

// A token names the key that signed it, so an unknown kid is input an attacker
// controls. Without the throttle a stream of tokens naming random kids turns
// this server into an amplifier pointed at its own issuer.
func TestAnUnknownKeyIsFetchedOnceAndThenThrottled(t *testing.T) {
	t.Parallel()

	server := newKeyServer(t)
	now := &clock{at: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	keys := NewKeySet(server.URL, server.Client(), now.now)

	lookup(t, keys)

	for range 50 {
		if _, err := keys.Lookup(t.Context(), "a-kid-nobody-publishes"); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("an unknown kid: %v", err)
		}
	}

	if hits := server.hits.Load(); hits != 1 {
		t.Fatalf("50 tokens naming unknown keys caused %d fetches at the issuer", hits)
	}

	// Once the window has passed, one unknown kid earns a refetch again, which
	// picks up a rotation. The throttle delays a rotation; it does not close
	// for good.
	now.at = now.at.Add(MinKeyTTL + time.Second)

	if _, err := keys.Lookup(t.Context(), "a-kid-nobody-publishes"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("an unknown kid after the window: %v", err)
	}

	if hits := server.hits.Load(); hits != 2 {
		t.Fatalf("the throttle did not open again: %d fetches", hits)
	}
}

// A key set this cannot rebuild a key from is one bad answer at the issuer.
// Overwriting the keys already held with nothing would make every token fail.
// The entries are shapes that get past a decoder which does not bound what a
// JWK carries.
func TestAnAnswerWithNoUsableKeyLeavesTheHeldOnesAlone(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	var broken atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entries := []map[string]string{{
			"kty": "RSA", "kid": "the-live-key",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
		}}

		if broken.Load() {
			entries = []map[string]string{
				// Nine bytes whose low eight read as 3. A decoder that takes the
				// low bits builds a key from an exponent the issuer never
				// published, under the kid of the key it did.
				{
					"kty": "RSA", "kid": "the-live-key",
					"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e": base64.RawURLEncoding.EncodeToString([]byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0x03}),
				},
				{"kty": "RSA", "kid": "no-modulus", "n": "", "e": "AQAB"},
				{"kty": "EC", "kid": "off-curve", "crv": "P-256", "x": "AA", "y": "AA"},
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"keys": entries})
	}))
	t.Cleanup(server.Close)

	now := &clock{at: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	keys := NewKeySet(server.URL, server.Client(), now.now)

	if _, err = keys.Lookup(t.Context(), "the-live-key"); err != nil {
		t.Fatal(err)
	}

	broken.Store(true)
	now.at = now.at.Add(MinKeyTTL + time.Second)

	if _, err = keys.Lookup(t.Context(), "a-kid-nobody-publishes"); err == nil {
		t.Fatal("a document with no usable key answered a key")
	}

	still, err := keys.Lookup(t.Context(), "the-live-key")
	if err != nil {
		t.Fatalf("the key held before the bad answer was dropped: %v", err)
	}

	if still.N.Cmp(key.N) != 0 || still.E != key.E {
		t.Fatal("the key held is not the one the issuer published")
	}
}
