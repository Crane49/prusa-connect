package joinwire_test

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"
)

// jwksEntry is one key of a served document.
type jwksEntry struct {
	Kty string `json:"kty,omitempty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg,omitempty"`
	Use string `json:"use,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	D   string `json:"d,omitempty"`
}

// publicEntry renders one RSA public key as a document entry.
func publicEntry(kid string, key *rsa.PublicKey) jwksEntry {
	return jwksEntry{
		Kty: "RSA",
		Kid: kid,
		Alg: joinwire.AlgRS256,
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

// servedDocument encodes a document out of its entries.
func servedDocument(t *testing.T, entries ...jwksEntry) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"keys": entries})
	if err != nil {
		t.Fatalf("encode the document: %v", err)
	}
	return body
}

// fakeSource serves whatever a test puts in it and counts the requests.
type fakeSource struct {
	body   atomic.Pointer[[]byte]
	status atomic.Int32
	calls  atomic.Int32
}

// newFakeSource starts a server that answers with body.
func newFakeSource(t *testing.T, body []byte) (*fakeSource, string) {
	t.Helper()
	s := &fakeSource{}
	s.body.Store(&body)
	s.status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.calls.Add(1)
		code := int(s.status.Load())
		w.WriteHeader(code)
		if code == http.StatusOK {
			_, _ = w.Write(*s.body.Load()) // A test server that cannot write says so in the test that reads it.
		}
	}))
	t.Cleanup(server.Close)
	return s, server.URL
}

// TestAServedKeyIsUsed checks that Key returns a key the JWKS endpoint
// serves.
func TestAServedKeyIsUsed(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	_, url := newFakeSource(t, servedDocument(t, publicEntry("one", &key.PublicKey)))
	source := joinwire.NewJWKS(url)
	got, err := source.Key(t.Context(), "one")
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if got.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("the key read is not the key served")
	}
}

// TestAnUnknownKidLooksAgainOnce checks that a kid that is not cached makes
// the key source fetch the key document again. A second fetch within
// MinRefreshInterval returns ErrRefreshRateLimited, which means unavailable
// and not unauthorized.
func TestAnUnknownKidLooksAgainOnce(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	served, url := newFakeSource(t, servedDocument(t, publicEntry("one", &key.PublicKey)))
	source := &joinwire.JWKS{URL: url, MinRefreshInterval: time.Hour}

	if _, err := source.Key(t.Context(), "one"); err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if got := served.calls.Load(); got != 1 {
		t.Fatalf("the document was fetched %d times, want once", got)
	}
	if _, err := source.Key(t.Context(), "two"); !errors.Is(err, joinwire.ErrRefreshRateLimited) {
		t.Fatalf("the refusal is %v, want the rate bound", err)
	}
	if got := served.calls.Load(); got != 1 {
		t.Errorf("the document was fetched %d times, want once", got)
	}

	// The test clock moves one minute forward at every read, so
	// MinRefreshInterval has passed when the test asks for the second kid.
	var reads atomic.Int64
	moving := func() time.Time { return now.Add(time.Duration(reads.Add(1)) * time.Minute) }
	eager := &joinwire.JWKS{URL: url, MinRefreshInterval: time.Second, Now: moving}
	if _, err := eager.Key(t.Context(), "one"); err != nil {
		t.Fatalf("read the key: %v", err)
	}
	before := served.calls.Load()
	if _, err := eager.Key(t.Context(), "two"); !errors.Is(err, joinwire.ErrUnknownKey) {
		t.Fatalf("the refusal is %v, want an unknown kid", err)
	}
	if got := served.calls.Load(); got != before+1 {
		t.Errorf("the document was fetched %d times, want one look more", got-before)
	}
}

// TestAStaleCacheKeepsServing checks that the key source keeps its cached keys
// when a fetch fails.
func TestAStaleCacheKeepsServing(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	served, url := newFakeSource(t, servedDocument(t, publicEntry("one", &key.PublicKey)))
	var reads atomic.Int64
	moving := func() time.Time { return now.Add(time.Duration(reads.Add(1)) * time.Minute) }
	source := &joinwire.JWKS{URL: url, MinRefreshInterval: time.Second, Now: moving}

	if _, err := source.Key(t.Context(), "one"); err != nil {
		t.Fatalf("read the key: %v", err)
	}
	served.status.Store(http.StatusInternalServerError)
	if _, err := source.Key(t.Context(), "two"); err == nil {
		t.Fatal("a kid nothing holds was answered")
	}
	if _, err := source.Key(t.Context(), "one"); err != nil {
		t.Errorf("the key already read is gone: %v", err)
	}
}

// TestAKeyThatCannotBeUsedIsSkipped checks that the key source skips a key it
// cannot use and still reads the other keys of the document.
func TestAKeyThatCannotBeUsedIsSkipped(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	weak := mintKey(t, weakBits)
	entries := []jwksEntry{
		{Kty: "EC", Kid: "elliptic"},
		{Kty: "RSA", Kid: "no-kid-value", N: "!!!", E: "AQAB"},
		publicEntry("weak", &weak.PublicKey),
		{Kty: "RSA", Kid: "wrong-use", Use: "enc", N: "AQAB", E: "AQAB"},
		publicEntry("good", &key.PublicKey),
	}
	_, url := newFakeSource(t, servedDocument(t, entries...))
	source := joinwire.NewJWKS(url)
	if _, err := source.Key(t.Context(), "good"); err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if got := source.Len(); got != 1 {
		t.Errorf("the source holds %d keys, want the one it can use", got)
	}
}

// TestADocumentWithPrivateMaterialIsRefusedWhole checks that the key source
// refuses every key of a document when one key has a private key member.
func TestADocumentWithPrivateMaterialIsRefusedWhole(t *testing.T) {
	t.Parallel()
	key := mintKey(t, strongBits)
	private := publicEntry("private", &key.PublicKey)
	private.D = base64.RawURLEncoding.EncodeToString(key.D.Bytes())
	_, url := newFakeSource(t, servedDocument(t, publicEntry("good", &key.PublicKey), private))
	source := joinwire.NewJWKS(url)
	if _, err := source.Key(t.Context(), "good"); !errors.Is(err, joinwire.ErrPrivateKeyMaterial) {
		t.Fatalf("the refusal is %v, want private key material", err)
	}
	if got := source.Len(); got != 0 {
		t.Errorf("the source holds %d keys, want none", got)
	}
}

// TestADocumentOverTheBoundIsRefused checks that the key source refuses a key
// document with more bytes than MaxDocumentBytes.
func TestADocumentOverTheBoundIsRefused(t *testing.T) {
	t.Parallel()
	_, url := newFakeSource(t, []byte(strings.Repeat("a", 4096)))
	source := &joinwire.JWKS{URL: url, MaxDocumentBytes: 16}
	if _, err := source.Key(t.Context(), "one"); !errors.Is(err, joinwire.ErrDocumentTooLarge) {
		t.Errorf("the refusal is %v, want the size bound", err)
	}
}

// TestADocumentWithNothingUsableIsUnavailable checks that Key returns
// ErrNoUsableKey, and not ErrUnknownKey, when the document has no usable key.
func TestADocumentWithNothingUsableIsUnavailable(t *testing.T) {
	t.Parallel()
	_, url := newFakeSource(t, servedDocument(t, jwksEntry{Kty: "EC", Kid: "elliptic"}))
	source := joinwire.NewJWKS(url)
	if _, err := source.Key(t.Context(), "one"); !errors.Is(err, joinwire.ErrNoUsableKey) {
		t.Errorf("the refusal is %v, want no usable key", err)
	}
}
