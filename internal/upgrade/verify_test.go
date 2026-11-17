package upgrade_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestVerifierAcceptsOnlyTheSignedArtifact(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	verifier, err := upgrade.NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	content := []byte("one release binary")
	sum := sha256.Sum256(content)
	name := "prusa-connect_linux_amd64"
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	signature := detached(t, private, public, sums)

	path := filepath.Join(t.TempDir(), name)
	if err = os.WriteFile(path, content, 0o700); err != nil {
		t.Fatalf("write staged binary: %v", err)
	}

	verified, err := verifier.Verify(path, name, sums, signature)
	if err != nil {
		t.Fatalf("verify signed binary: %v", err)
	}

	if verified.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("verified digest is %s, expected %x", verified.SHA256, sum)
	}

	if verified.KeyID != upgrade.KeyID(public) {
		t.Errorf("verified key is %s, expected %s", verified.KeyID, upgrade.KeyID(public))
	}

	if err = os.WriteFile(path, append(content, '!'), 0o700); err != nil {
		t.Fatalf("damage staged binary: %v", err)
	}

	if _, err = verifier.Verify(path, name, sums, signature); !errors.Is(err, upgrade.ErrChecksum) {
		t.Fatalf("damaged binary failed with %v, expected %v", err, upgrade.ErrChecksum)
	}
}

func TestVerifierRefusesChangedAndUnknownSignatures(t *testing.T) {
	t.Parallel()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}

	verifier, err := upgrade.NewVerifier([]ed25519.PublicKey{public}, false)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	sums := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  core\n")

	if _, err = verifier.Verify("unused", "core", sums, nil); !errors.Is(err, upgrade.ErrSignature) {
		t.Fatalf("missing signature failed with %v, expected %v", err, upgrade.ErrSignature)
	}

	unknown := detached(t, private, otherPublic, sums)
	if _, err = verifier.Verify("unused", "core", sums, unknown); !errors.Is(err, upgrade.ErrSignature) {
		t.Fatalf("unknown key failed with %v, expected %v", err, upgrade.ErrSignature)
	}

	changed := detached(t, private, public, sums)
	if _, err = verifier.Verify("unused", "core", append(sums, '\n'), changed); !errors.Is(err, upgrade.ErrSignature) {
		t.Fatalf("changed manifest failed with %v, expected %v", err, upgrade.ErrSignature)
	}
}

func TestUnsignedVerifierStillRequiresTheDeclaredChecksum(t *testing.T) {
	t.Parallel()

	verifier, err := upgrade.NewVerifier(nil, true)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	content := []byte("local build")
	sum := sha256.Sum256(content)
	name := "core"
	sums := []byte(hex.EncodeToString(sum[:]) + "  core\n")
	path := filepath.Join(t.TempDir(), name)

	if err = os.WriteFile(path, content, 0o700); err != nil {
		t.Fatalf("write local build: %v", err)
	}

	verified, err := verifier.Verify(path, name, sums, nil)
	if err != nil {
		t.Fatalf("verify allowed unsigned build: %v", err)
	}

	if verified.KeyID != "" {
		t.Errorf("unsigned build reports key %q", verified.KeyID)
	}
}

func TestVersionPolicyOrdersChannelsAndPrereleases(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		current   string
		candidate string
		allowed   bool
	}{
		{name: "upgrade", current: "1.0.0", candidate: "1.1.0", allowed: true},
		{name: "reinstall", current: "1.0.0", candidate: "1.0.0", allowed: true},
		{name: "beta candidate", current: "1.0.0", candidate: "1.1.0-beta.1", allowed: true},
		{name: "next alpha", current: "0.0.1-alpha.7", candidate: "0.0.1-alpha.8", allowed: true},
		{name: "downgrade", current: "1.1.0", candidate: "1.0.0"},
		{name: "older alpha", current: "0.0.1-alpha.8", candidate: "0.0.1-alpha.7"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := upgrade.PermitVersion(testCase.current, testCase.candidate)
			if testCase.allowed && err != nil {
				t.Errorf("allowed version failed: %v", err)
			}

			if !testCase.allowed && !errors.Is(err, upgrade.ErrVersionPolicy) {
				t.Errorf("refused version failed with %v, expected %v", err, upgrade.ErrVersionPolicy)
			}
		})
	}
}

func detached(
	t *testing.T,
	private ed25519.PrivateKey,
	declared ed25519.PublicKey,
	sums []byte,
) []byte {
	t.Helper()

	body, err := json.Marshal(upgrade.Signature{
		Algorithm: "ed25519",
		KeyID:     upgrade.KeyID(declared),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(private, sums)),
	})
	if err != nil {
		t.Fatalf("encode detached signature: %v", err)
	}

	return body
}
