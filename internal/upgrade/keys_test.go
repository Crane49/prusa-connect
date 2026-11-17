package upgrade

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestBuildVerifierChecksConfiguredKeyIDs(t *testing.T) {
	t.Parallel()

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key := base64.StdEncoding.EncodeToString(public)
	if _, err = BuildVerifier([]TrustedKey{{ID: KeyID(public), Key: key}}, false); err != nil {
		t.Fatal(err)
	}

	if _, err = BuildVerifier([]TrustedKey{{ID: "wrong", Key: key}}, false); err == nil {
		t.Fatal("a mismatched key id was accepted")
	}
}
