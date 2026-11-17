package files

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"strings"
)

// Algorithm is the type byte of an identity: the hash function that produced
// its digest.
type Algorithm uint8

// SHA256 is SHA-256, the one algorithm this version defines. Its text name is
// "sha256" and its digest is 32 bytes.
const SHA256 Algorithm = 0x01

// sha256Name is the text name of SHA256. The match is case-sensitive.
const sha256Name = "sha256"

// Identity names an object by a digest of its bytes and the algorithm that
// produced the digest. It is comparable, so it can be a map key. The zero
// value names nothing.
type Identity struct {
	alg    Algorithm
	digest [sha256.Size]byte
}

// Sum returns the identity of data.
func Sum(data []byte) Identity {
	return Identity{alg: SHA256, digest: sha256.Sum256(data)}
}

// ParseText reads an identity in its text form: the text name, a colon, and
// the digest in base64url with no padding and its unused bits zero. It
// refuses any other spelling of the same digest, so an identity has one text.
func ParseText(s string) (Identity, error) {
	name, encoded, ok := strings.Cut(s, ":")
	if !ok {
		return Identity{}, fmt.Errorf("%w: identity %q has no colon", ErrMalformed, s)
	}
	if name != sha256Name {
		return Identity{}, fmt.Errorf("%w: identity %q names algorithm %q", ErrUnsupportedAlgorithm, s, name)
	}
	digest, err := base64.RawURLEncoding.DecodeString(encoded)
	// The decoder skips line breaks and accepts unused bits that are not
	// zero. Encoding the digest again and comparing refuses both.
	if err != nil || len(digest) != sha256.Size || base64.RawURLEncoding.EncodeToString(digest) != encoded {
		return Identity{}, fmt.Errorf("%w: identity %q is not a %s digest in base64url", ErrMalformed, s, name)
	}
	return Identity{alg: SHA256, digest: [sha256.Size]byte(digest)}, nil
}

// ParseBytes reads an identity in its byte form: the type byte followed by
// the digest. It refuses a type this version does not define and a digest of
// any length other than the one its type requires.
func ParseBytes(b []byte) (Identity, error) {
	if len(b) == 0 {
		return Identity{}, fmt.Errorf("%w: identity is empty", ErrMalformed)
	}
	if Algorithm(b[0]) != SHA256 {
		return Identity{}, fmt.Errorf("%w: identity has type byte 0x%02x", ErrUnsupportedAlgorithm, b[0])
	}
	if len(b) != 1+sha256.Size {
		return Identity{}, fmt.Errorf("%w: a %s identity is %d bytes, not %d",
			ErrMalformed, sha256Name, 1+sha256.Size, len(b))
	}
	return Identity{alg: SHA256, digest: [sha256.Size]byte(b[1:])}, nil
}

// Text returns the identity in text form, as in
// "sha256:yvWzZXKtGDZw7s2x9uya3irUm_l_-Zv-kvojJgtG-_E". It returns "" for the
// zero value.
func (id Identity) Text() string {
	if id.IsZero() {
		return ""
	}
	return sha256Name + ":" + base64.RawURLEncoding.EncodeToString(id.digest[:])
}

// Bytes returns the identity in byte form: the type byte followed by the
// digest. It returns nil for the zero value.
func (id Identity) Bytes() []byte {
	if id.IsZero() {
		return nil
	}
	return append([]byte{byte(id.alg)}, id.digest[:]...)
}

// IsZero reports whether id is the zero value, which names no object.
func (id Identity) IsZero() bool { return id.alg == 0 }

// Hasher computes the identity of bytes written to it, for an object too
// large to hold in memory. It is an io.Writer.
type Hasher struct {
	h hash.Hash
}

// NewHasher returns a Hasher that has seen no bytes.
func NewHasher() *Hasher { return &Hasher{h: sha256.New()} }

// Write adds p to the bytes hashed. It never returns an error.
func (h *Hasher) Write(p []byte) (int, error) { return h.h.Write(p) }

// Identity returns the identity of every byte written so far. Writing more
// after the call continues the same object.
func (h *Hasher) Identity() Identity {
	id := Identity{alg: SHA256}
	h.h.Sum(id.digest[:0])
	return id
}
