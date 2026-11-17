// Command sign-upgrade-checksums writes the detached Ed25519 signature a core
// release publishes beside SHA256SUMS.
//
// It signs only with a key whose public half is one of UPGRADE_PUBLIC_KEYS,
// the keys the release builds trust, and it reads the signature back through
// the verifier a server runs. A signing secret that does not match the
// published keys fails the release here, before a server refuses its update.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

const signerArgCount = 2

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign-upgrade-checksums: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != signerArgCount {
		return errors.New("usage: sign-upgrade-checksums <SHA256SUMS> <SHA256SUMS.sig>")
	}

	encoded := strings.TrimSpace(os.Getenv("UPGRADE_SIGNING_KEY"))
	if encoded == "" {
		return errors.New("UPGRADE_SIGNING_KEY is empty")
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode UPGRADE_SIGNING_KEY: %w", err)
	}

	var private ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		private = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		private = ed25519.PrivateKey(raw)
	default:
		return fmt.Errorf("UPGRADE_SIGNING_KEY holds %d bytes, expected %d or %d",
			len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
	}

	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("Ed25519 private key returned a different public key type")
	}

	verifier, err := publishedVerifier(public)
	if err != nil {
		return err
	}

	sums, err := os.ReadFile(args[0]) // #nosec G703 -- the release operator supplies this CLI path
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	signature := upgrade.Signature{
		Algorithm: "ed25519",
		KeyID:     upgrade.KeyID(public),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(private, sums)),
	}

	var written bytes.Buffer
	if err = json.NewEncoder(&written).Encode(signature); err != nil {
		return fmt.Errorf("encode signature: %w", err)
	}

	// The signature is checked before the file exists, so a refused one
	// leaves nothing behind for a later step to publish.
	if _, err = verifier.VerifySignature(sums, written.Bytes()); err != nil {
		return fmt.Errorf("the signature does not verify against UPGRADE_PUBLIC_KEYS: %w", err)
	}

	// #nosec G703 -- the release operator supplies this exclusive output path.
	output, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create signature: %w", err)
	}
	defer func() { _ = output.Close() }()

	if _, err = output.Write(written.Bytes()); err != nil {
		return fmt.Errorf("write signature: %w", err)
	}

	return output.Sync()
}

// publishedVerifier is the verifier over UPGRADE_PUBLIC_KEYS, and refuses a
// signing key outside it. The error names key ids only, which are public.
func publishedVerifier(public ed25519.PublicKey) (upgrade.Verifier, error) {
	declared := strings.TrimSpace(os.Getenv("UPGRADE_PUBLIC_KEYS"))
	if declared == "" {
		return upgrade.Verifier{}, errors.New("UPGRADE_PUBLIC_KEYS is empty, so nothing says which key a server trusts")
	}

	keys := make([]upgrade.TrustedKey, 0, strings.Count(declared, ",")+1)
	ids := make([]string, 0, cap(keys))

	for encoded := range strings.SplitSeq(declared, ",") {
		keys = append(keys, upgrade.TrustedKey{Key: strings.TrimSpace(encoded)})

		raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(encoded))
		if err == nil && len(raw) == ed25519.PublicKeySize {
			ids = append(ids, upgrade.KeyID(ed25519.PublicKey(raw)))
		}
	}

	verifier, err := upgrade.BuildVerifier(keys, false)
	if err != nil {
		return upgrade.Verifier{}, fmt.Errorf("read UPGRADE_PUBLIC_KEYS: %w", err)
	}

	if !slices.Contains(ids, upgrade.KeyID(public)) {
		return upgrade.Verifier{}, fmt.Errorf("UPGRADE_SIGNING_KEY is key %s, and UPGRADE_PUBLIC_KEYS trusts %s",
			upgrade.KeyID(public), strings.Join(ids, ", "))
	}

	return verifier, nil
}
