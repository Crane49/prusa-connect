package fractal

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// RPCIDLength is the length of a generated RPC id: a 16-byte UUID in base64url
// without padding.
const RPCIDLength = 22

// The bits RFC 4122 sets in a random UUID: the version in the high half of
// byte 6 and the variant in the high bits of byte 8.
const (
	uuidVersionByte    = 6
	uuidVersionMask    = 0xF0
	uuidVersion4       = 0x40
	uuidVariantByte    = 8
	uuidVariantMask    = 0xC0
	uuidVariantRFC4122 = 0x80
)

// ErrBadRPCID reports that an RPC id has a character outside the base64url
// alphabet.
var ErrBadRPCID = errors.New("not a base64url request id")

// RPCID identifies one RPC. A RES has the id of the RPC it answers. A receiver
// that gets the same RPC twice recognizes the second delivery by its id and
// does not run the method again. A valid id is any non-empty base64url string
// of at most RPCIDLength characters.
type RPCID string

// NewRPCID generates a random version 4 UUID and returns it in base64url
// without padding. It fails only when the system source of randomness fails.
func NewRPCID() (RPCID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[uuidVersionByte] = b[uuidVersionByte]&^uuidVersionMask | uuidVersion4
	b[uuidVariantByte] = b[uuidVariantByte]&^uuidVariantMask | uuidVariantRFC4122
	return RPCID(base64.RawURLEncoding.EncodeToString(b[:])), nil
}

// Validate returns an error for an empty id, for an id over RPCIDLength
// characters, and for an id with a character outside the base64url alphabet.
// The padding character "=" is outside that alphabet.
func (id RPCID) Validate() error {
	s := string(id)
	if s == "" {
		return nameErr("rpc id", s, ErrEmpty)
	}
	if len(s) > RPCIDLength {
		return nameErr("rpc id", s, ErrTooLong)
	}
	for i := range len(s) {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || digitByte(c) || c == '-' || c == '_'
		if !ok {
			return nameErr("rpc id", s, ErrBadRPCID)
		}
	}
	return nil
}

// String returns the id unchanged.
func (id RPCID) String() string { return string(id) }

// ParseRPCID validates s and returns it as an RPCID.
func ParseRPCID(s string) (RPCID, error) {
	id := RPCID(s)
	return id, id.Validate()
}

// UUID decodes the id to the 16 bytes of its UUID. ok is false when the id is
// not RPCIDLength characters long or is not valid base64url.
func (id RPCID) UUID() (uuid [16]byte, ok bool) {
	if len(id) != RPCIDLength {
		return uuid, false
	}
	b, err := base64.RawURLEncoding.DecodeString(string(id))
	if err != nil || len(b) != len(uuid) {
		return uuid, false
	}
	copy(uuid[:], b)
	return uuid, true
}
