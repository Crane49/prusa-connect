package natsbind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nats-io/nkeys"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The permissions of the files and directories the node creates. Only the
// owner can read a seed file and the directory it is in. A seed is the private
// part of an NKEY, the Ed25519 key pair NATS uses to authenticate.
const (
	keyMode = 0o600
	dirMode = 0o700
)

// identityFile is the name of the file in the data directory with the seed of
// the node's identity key.
const identityFile = "identity.nk"

// Signer signs with the node's identity NKEY. It has no method that returns
// the seed, so no log record, error or Status can have the seed.
type Signer = joinwire.Signer

// ErrNotASeed means a seed file, or the text given to NewSigner, is not a
// valid NKEY seed.
var ErrNotASeed = errors.New("the stored identity is not an nkey seed")

// seedSigner signs with one NKEY key pair.
type seedSigner struct {
	pair nkeys.KeyPair
	pub  string
}

// NewSigner builds a Signer from a seed in the text encoding of an NKEY.
func NewSigner(seed string) (Signer, error) {
	pair, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotASeed, err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("read the public half: %w", err)
	}
	return &seedSigner{pair: pair, pub: pub}, nil
}

// GenerateSigner creates a new identity key pair. It returns the Signer and
// the seed. The caller has to save the seed: the Signer cannot return it
// later.
func GenerateSigner() (signer Signer, seed string, err error) {
	pair, err := nkeys.CreateUser()
	if err != nil {
		return nil, "", fmt.Errorf("create an identity: %w", err)
	}
	raw, err := pair.Seed()
	if err != nil {
		return nil, "", fmt.Errorf("export the identity: %w", err)
	}
	pub, err := pair.PublicKey()
	if err != nil {
		return nil, "", fmt.Errorf("read the public half: %w", err)
	}
	return &seedSigner{pair: pair, pub: pub}, string(raw), nil
}

// PublicKey returns the public key in the text encoding of an NKEY.
func (s *seedSigner) PublicKey() (string, error) { return s.pub, nil }

// Sign returns the signature of data.
func (s *seedSigner) Sign(data []byte) ([]byte, error) {
	sig, err := s.pair.Sign(data)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	return sig, nil
}

// loadOrCreateIdentity reads the seed of the node's identity key from the data
// directory. On the first start it creates a key pair and saves the seed.
//
// It returns an error when it cannot save the seed. The node above accepts
// one public key for each Node ID. A node that restarted with a new key and
// the same Node ID would get a collision denial, and Fractal forbids a retry
// after that denial.
func loadOrCreateIdentity(dir string) (Signer, error) {
	path := filepath.Join(dir, identityFile)
	seed, err := os.ReadFile(path)
	switch {
	case err == nil:
		return NewSigner(string(seed))
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("read the identity: %w", err)
	}
	signer, fresh, err := GenerateSigner()
	if err != nil {
		return nil, err
	}
	if mkErr := os.MkdirAll(dir, dirMode); mkErr != nil {
		return nil, fmt.Errorf("make %s: %w", dir, mkErr)
	}
	if writeErr := os.WriteFile(path, []byte(fresh), keyMode); writeErr != nil {
		return nil, fmt.Errorf("write the identity: %w", writeErr)
	}
	return signer, nil
}

// identityOf returns the public key of a Signer as a fractal.Identity.
func identityOf(s Signer) (fractal.Identity, error) {
	pub, err := s.PublicKey()
	if err != nil {
		return "", fmt.Errorf("read the identity: %w", err)
	}
	return fractal.Identity(pub), nil
}
