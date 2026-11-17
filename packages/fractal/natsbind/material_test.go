package natsbind

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTheHeartbeatFollowsTheShorterInterval checks that a node sends
// heartbeats at the shorter of its configured interval and the longest
// interval the node above allows (fractal-nats.md, Heartbeat pacing is set by
// the parent).
func TestTheHeartbeatFollowsTheShorterInterval(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configured time.Duration
		granted    time.Duration
		want       time.Duration
	}{
		{"the granted one is shorter", 10 * time.Second, time.Second, time.Second},
		{"the configured one is shorter", time.Second, 10 * time.Second, time.Second},
		{"nothing was granted", time.Second, 0, time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := heartbeatEvery(c.configured, c.granted); got != c.want {
				t.Errorf("heartbeatEvery returned %s, want %s", got, c.want)
			}
		})
	}
}

// TestTheTrustAnchorsComeFromFiles checks LoadAnchors with a valid CA file, a
// file with no certificate, a file that does not exist, and no files.
func TestTheTrustAnchorsComeFromFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(good, selfSigned(t), 0o600); err != nil {
		t.Fatalf("write the CA file: %v", err)
	}
	pool, err := LoadAnchors([]string{good})
	if err != nil {
		t.Fatalf("LoadAnchors: %v", err)
	}
	if pool == nil {
		t.Fatal("LoadAnchors returned a nil pool")
	}
	bad := filepath.Join(dir, "not-a-certificate.pem")
	if writeErr := os.WriteFile(bad, []byte("hello"), 0o600); writeErr != nil {
		t.Fatalf("write the file: %v", writeErr)
	}
	if _, anchorErr := LoadAnchors([]string{bad}); !errors.Is(anchorErr, ErrNoAnchors) {
		t.Errorf("LoadAnchors returned %v, want ErrNoAnchors", anchorErr)
	}
	if _, missingErr := LoadAnchors([]string{filepath.Join(dir, "gone.pem")}); missingErr == nil {
		t.Error("LoadAnchors returned nil for a file that does not exist")
	}
	if _, systemErr := LoadAnchors(nil); systemErr != nil {
		t.Errorf("LoadAnchors could not read the CA certificates of the operating system: %v", systemErr)
	}
}

// TestAnIdentityIsReadBackFromItsSeed checks that NewSigner builds the same
// identity from the seed GenerateSigner returned, and that it refuses text
// that is not a seed.
func TestAnIdentityIsReadBackFromItsSeed(t *testing.T) {
	t.Parallel()
	signer, seed, err := GenerateSigner()
	if err != nil {
		t.Fatalf("GenerateSigner: %v", err)
	}
	again, err := NewSigner(seed)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	first, err := identityOf(signer)
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	second, err := identityOf(again)
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	if first != second {
		t.Errorf("the identity from the seed is %q, want %q", second, first)
	}
	if _, badErr := NewSigner("not a seed"); !errors.Is(badErr, ErrNotASeed) {
		t.Errorf("NewSigner returned %v, want ErrNotASeed", badErr)
	}
	sig, err := signer.Sign([]byte("a nonce"))
	if err != nil || len(sig) == 0 {
		t.Errorf("Sign returned an empty signature: %v", err)
	}
}

// TestAnIdentityIsWrittenOnceAndReadAfterwards checks that a node creates its
// identity key on the first start and reads the same key at every later
// start.
func TestAnIdentityIsWrittenOnceAndReadAfterwards(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := loadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("create the identity: %v", err)
	}
	again, err := loadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("read the identity: %v", err)
	}
	one, err := identityOf(first)
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	two, err := identityOf(again)
	if err != nil {
		t.Fatalf("read the public key: %v", err)
	}
	if one != two {
		t.Errorf("the identity at the second start is %q, want %q", two, one)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, identityFile), []byte("rubbish"), 0o600); writeErr != nil {
		t.Fatalf("overwrite the seed file: %v", writeErr)
	}
	if _, corruptErr := loadOrCreateIdentity(dir); !errors.Is(corruptErr, ErrNotASeed) {
		t.Errorf("loadOrCreateIdentity returned %v, want ErrNotASeed", corruptErr)
	}
}

// selfSigned returns a self-signed certificate in PEM, for use as a CA file.
func selfSigned(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("mint a key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fractal test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("mint a certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
