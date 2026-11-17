package upgrade

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// ErrSignature reports release metadata that no trusted key signed.
var ErrSignature = errors.New("upgrade: release signature is not trusted")

// ErrChecksum reports an artefact whose normalized bytes do not match the
// signed release manifest.
var ErrChecksum = errors.New("upgrade: release checksum does not match")

// ErrVersionPolicy reports a reinstall or downgrade the version policy does
// not permit.
var ErrVersionPolicy = errors.New("upgrade: version is not allowed")

const (
	checksumScanBuffer = 4 << 10
	checksumScanMax    = 1 << 20
)

// Signature is the detached signature stored in SHA256SUMS.sig.
type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Value     string `json:"signature"`
}

// Verified is the immutable identity established for one staged binary.
type Verified struct {
	Name   string `json:"name"             doc:"The signed release filename."`
	SHA256 string `json:"sha256"           doc:"The lowercase SHA-256 digest of the staged binary."`
	KeyID  string `json:"key_id,omitempty" doc:"The identifier of the trusted signing key, when signed."`
	Size   int64  `json:"size"             doc:"The verified binary size in bytes."`
}

// Verifier checks release metadata against a closed set of trusted keys.
type Verifier struct {
	keys          map[string]ed25519.PublicKey
	allowUnsigned bool
}

// NewVerifier copies trusted keys. A key is addressed by KeyID, so a declared
// name cannot silently point at different bytes.
func NewVerifier(keys []ed25519.PublicKey, allowUnsigned bool) (Verifier, error) {
	trusted := make(map[string]ed25519.PublicKey, len(keys))

	for _, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			return Verifier{}, fmt.Errorf("%w: an Ed25519 public key holds %d bytes", ErrSignature, len(key))
		}

		id := KeyID(key)
		trusted[id] = bytes.Clone(key)
	}

	return Verifier{keys: trusted, allowUnsigned: allowUnsigned}, nil
}

// KeyID is the first 128 bits of SHA256 over a public key. It selects a key;
// Ed25519 still verifies the full key.
func KeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)

	return hex.EncodeToString(sum[:16])
}

// Verify checks the detached signature and the normalized staged binary.
func (v Verifier) Verify(binaryPath, name string, sums, detached []byte) (Verified, error) {
	keyID, err := v.verifySignature(sums, detached)
	if err != nil {
		return Verified{}, err
	}

	want, err := checksumFor(sums, name)
	if err != nil {
		return Verified{}, err
	}

	file, err := os.Open(binaryPath)
	if err != nil {
		return Verified{}, fmt.Errorf("upgrade: open staged binary: %w", err)
	}

	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return Verified{}, fmt.Errorf("upgrade: inspect staged binary: %w", err)
	}

	if !info.Mode().IsRegular() {
		return Verified{}, fmt.Errorf("%w: staged binary is not a regular file", ErrChecksum)
	}

	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return Verified{}, fmt.Errorf("upgrade: hash staged binary: %w", err)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if got != want {
		return Verified{}, fmt.Errorf("%w: %s has %s, signed manifest has %s", ErrChecksum, name, got, want)
	}

	return Verified{Name: name, SHA256: got, KeyID: keyID, Size: info.Size()}, nil
}

// VerifySignature checks the detached signature over SHA256SUMS alone and
// names the trusted key that made it. A release pipeline calls it on what it
// just signed, before anything is published.
func (v Verifier) VerifySignature(sums, detached []byte) (string, error) {
	return v.verifySignature(sums, detached)
}

func (v Verifier) verifySignature(sums, detached []byte) (string, error) {
	if len(bytes.TrimSpace(detached)) == 0 {
		if v.allowUnsigned {
			return "", nil
		}

		return "", fmt.Errorf("%w: SHA256SUMS.sig is absent", ErrSignature)
	}

	decoder := json.NewDecoder(bytes.NewReader(detached))
	decoder.DisallowUnknownFields()

	var signature Signature
	if err := decoder.Decode(&signature); err != nil {
		return "", fmt.Errorf("%w: decode SHA256SUMS.sig: %w", ErrSignature, err)
	}

	if decoder.Decode(&struct{}{}) != io.EOF {
		return "", fmt.Errorf("%w: SHA256SUMS.sig holds more than one value", ErrSignature)
	}

	if signature.Algorithm != "ed25519" {
		return "", fmt.Errorf("%w: algorithm is %q", ErrSignature, signature.Algorithm)
	}

	key, found := v.keys[signature.KeyID]
	if !found {
		return "", fmt.Errorf("%w: key %q is unknown", ErrSignature, signature.KeyID)
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(signature.Value)
	if err != nil {
		return "", fmt.Errorf("%w: decode signature: %w", ErrSignature, err)
	}

	if !ed25519.Verify(key, sums, raw) {
		return "", fmt.Errorf("%w: Ed25519 verification failed", ErrSignature)
	}

	return signature.KeyID, nil
}

func checksumFor(sums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	scanner.Buffer(make([]byte, checksumScanBuffer), checksumScanMax)

	found := ""

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		digest, file, ok := strings.Cut(line, "  ")
		if !ok || file == "" || strings.ContainsAny(file, `/\`) {
			return "", fmt.Errorf("%w: malformed SHA256SUMS line", ErrChecksum)
		}

		decoded, decodeErr := hex.DecodeString(digest)
		if decodeErr != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("%w: malformed digest for %s", ErrChecksum, file)
		}

		if file != name {
			continue
		}

		if found != "" {
			return "", fmt.Errorf("%w: SHA256SUMS names %s more than once", ErrChecksum, name)
		}

		found = strings.ToLower(digest)
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%w: read SHA256SUMS: %w", ErrChecksum, err)
	}

	if found == "" {
		return "", fmt.Errorf("%w: SHA256SUMS does not name %s", ErrChecksum, name)
	}

	return found, nil
}

// PermitVersion accepts any release that is not older, prereleases included,
// because the beta offers only alpha and beta builds. The same version may be
// reinstalled; an older one is restored only with its backup.
func PermitVersion(current, candidate string) error {
	held, err := manifest.ParseVersion(current)
	if err != nil {
		return fmt.Errorf("%w: current version %q: %w", ErrVersionPolicy, current, err)
	}

	next, err := manifest.ParseVersion(candidate)
	if err != nil {
		return fmt.Errorf("%w: candidate version %q: %w", ErrVersionPolicy, candidate, err)
	}

	if next.Compare(held) < 0 {
		return fmt.Errorf("%w: %s is older than %s; restore its backup to downgrade",
			ErrVersionPolicy, candidate, current)
	}

	return nil
}
