package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestRunWritesAVerifiableSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	sumsPath := filepath.Join(root, "SHA256SUMS")
	signaturePath := filepath.Join(root, "SHA256SUMS.sig")
	sums := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  core\n")

	if err = os.WriteFile(sumsPath, sums, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPGRADE_SIGNING_KEY", base64.StdEncoding.EncodeToString(private))
	t.Setenv("UPGRADE_PUBLIC_KEYS", base64.StdEncoding.EncodeToString(public))

	if err = run([]string{sumsPath, signaturePath}); err != nil {
		t.Fatal(err)
	}

	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := upgrade.NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = verifier.Verify("missing", "core", sums, signature); err == nil {
		t.Fatal("verification reached the missing artifact without accepting the signature")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verification error is %v", err)
	}
}

// TestRunRefusesAKeyTheReleaseDoesNotTrust is the release secret drifting from
// the published key. Every server would refuse such an update, so the release
// stops at the signature and writes none.
func TestRunRefusesAKeyTheReleaseDoesNotTrust(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	sumsPath := filepath.Join(root, "SHA256SUMS")
	signaturePath := filepath.Join(root, "SHA256SUMS.sig")
	if err = os.WriteFile(sumsPath, []byte("sums\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPGRADE_SIGNING_KEY", base64.StdEncoding.EncodeToString(private))
	t.Setenv("UPGRADE_PUBLIC_KEYS", base64.StdEncoding.EncodeToString(trusted))

	err = run([]string{sumsPath, signaturePath})
	if err == nil || !strings.Contains(err.Error(), upgrade.KeyID(trusted)) {
		t.Fatalf("a key outside UPGRADE_PUBLIC_KEYS signed, or the refusal names no trusted key: %v", err)
	}
	if strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(private)) {
		t.Fatal("the refusal carries the signing key")
	}
	if _, statErr := os.Stat(signaturePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a refused signature left a file behind: %v", statErr)
	}
}

// TestRunRefusesAPrivateKeyWhosePublicHalfLies is a 64-byte key whose second
// half is not the seed's public key. It names a trusted key id and signs with
// another key, and only reading the signature back catches it.
func TestRunRefusesAPrivateKeyWhosePublicHalfLies(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	forged := append(append([]byte{}, private.Seed()...), trusted...)

	root := t.TempDir()
	sumsPath := filepath.Join(root, "SHA256SUMS")
	signaturePath := filepath.Join(root, "SHA256SUMS.sig")
	if err = os.WriteFile(sumsPath, []byte("sums\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPGRADE_SIGNING_KEY", base64.StdEncoding.EncodeToString(forged))
	t.Setenv("UPGRADE_PUBLIC_KEYS", base64.StdEncoding.EncodeToString(trusted))

	if err = run([]string{sumsPath, signaturePath}); err == nil {
		t.Fatal("a signature the trusted key did not make was written")
	}
	if _, statErr := os.Stat(signaturePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a refused signature left a file behind: %v", statErr)
	}
}

func TestRunRefusesWithoutThePublishedKeys(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	sumsPath := filepath.Join(root, "SHA256SUMS")
	if err = os.WriteFile(sumsPath, []byte("sums\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPGRADE_SIGNING_KEY", base64.StdEncoding.EncodeToString(private))
	t.Setenv("UPGRADE_PUBLIC_KEYS", "")

	if err = run([]string{sumsPath, filepath.Join(root, "SHA256SUMS.sig")}); err == nil ||
		!strings.Contains(err.Error(), "UPGRADE_PUBLIC_KEYS") {
		t.Fatalf("signing went ahead with no published key: %v", err)
	}
}
