package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

// Errors reported while reading a token. They never reach a client: every
// refusal a caller sees is the one worded in verify.go.
var (
	// ErrMalformedToken reports text that is not shaped like a token this
	// server issues.
	ErrMalformedToken = errors.New("auth: malformed token")
	// ErrNoToken reports a request that presented no credential at all. A
	// log tells it from a malformed one, and a caller still gets the same
	// refusal.
	ErrNoToken = errors.New("auth: no credential presented")
	// ErrChecksumMismatch reports a token whose trailing checksum does not
	// cover its body, which is a typo or a truncation and not a guess.
	ErrChecksumMismatch = errors.New("auth: token checksum mismatch")
)

const (
	// Prefix marks this project's credentials in a log, a config file or a
	// secret scanner. A prefix plus the checksum below lets a scanner tell a
	// real token from a lookalike with no network call.
	Prefix = "pcl_"

	// base62Alphabet is the digits and both cases of the Latin alphabet. A
	// token is copied by hand, so it carries no punctuation.
	base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// publicIDLen is 22 base62 characters, just over 130 bits. It is the
	// indexed half and carries no secret.
	publicIDLen = 22
	// secretLen is 43 base62 characters, just over 256 bits. That is above the
	// NIST SP 800-63B threshold for a look-up secret, so the stored form is a
	// plain SHA-256 and not a password hash.
	secretLen = 43
	// checksumLen holds a CRC32 in base62. Six characters cover 2^32.
	checksumLen = 6

	separator     = "_"
	separatorAt   = len(Prefix) + publicIDLen
	secretAt      = separatorAt + len(separator)
	checksumAt    = secretAt + secretLen
	tokenLen      = checksumAt + checksumLen
	base62Radix   = 62
	rejectionBase = 256
)

// Token is the text a caller presents. It is returned once, at mint, and this
// server keeps only the SHA-256 of its secret half.
type Token string

// Digest is the stored form of a token's secret half.
type Digest [sha256.Size]byte

// Mint returns a new token and the digest to store for it. A row is found by
// the public half. The secret half is never stored.
func Mint() (Token, string, Digest, error) {
	publicID, err := randomBase62(publicIDLen)
	if err != nil {
		return "", "", Digest{}, fmt.Errorf("mint public id: %w", err)
	}

	secret, err := randomBase62(secretLen)
	if err != nil {
		return "", "", Digest{}, fmt.Errorf("mint secret: %w", err)
	}

	body := Prefix + publicID + separator + secret

	return Token(body + checksumOf(body)), publicID, sha256.Sum256([]byte(secret)), nil
}

// Split reads a token without touching a store. It rejects anything that is not
// this server's shape and anything whose checksum does not cover its body, so a
// mistyped token costs no lookup.
func Split(token Token) (publicID string, secret string, err error) {
	text := string(token)
	if text == "" {
		return "", "", ErrNoToken
	}

	if len(text) != tokenLen || !strings.HasPrefix(text, Prefix) || text[separatorAt:secretAt] != separator {
		return "", "", ErrMalformedToken
	}

	body, checksum := text[:checksumAt], text[checksumAt:]

	publicID, secret = text[len(Prefix):separatorAt], text[secretAt:checksumAt]
	if !isBase62(publicID) || !isBase62(secret) || !isBase62(checksum) {
		return "", "", ErrMalformedToken
	}

	if checksum != checksumOf(body) {
		return "", "", ErrChecksumMismatch
	}

	return publicID, secret, nil
}

// DigestOf is the stored form of a secret half.
func DigestOf(secret string) Digest { return sha256.Sum256([]byte(secret)) }

// checksumOf is a CRC32 of everything before it, in fixed-width base62.
func checksumOf(body string) string {
	sum := crc32.ChecksumIEEE([]byte(body))

	out := make([]byte, checksumLen)
	for i := checksumLen - 1; i >= 0; i-- {
		out[i] = base62Alphabet[sum%base62Radix]
		sum /= base62Radix
	}

	return string(out)
}

// randomBase62 draws characters directly from crypto/rand with rejection
// sampling. Encoding a byte string into base62 needs bignum arithmetic, and
// doing it with a modulo biases the result. Drawing the characters does not.
func randomBase62(n int) (string, error) {
	// The largest multiple of 62 below 256. A byte at or above it is redrawn,
	// so every character is equally likely.
	const ceiling = rejectionBase - rejectionBase%base62Radix

	out := make([]byte, 0, n)
	buf := make([]byte, n)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("read random: %w", err)
		}

		for _, b := range buf {
			if b >= ceiling {
				continue
			}

			out = append(out, base62Alphabet[b%base62Radix])
			if len(out) == n {
				break
			}
		}
	}

	return string(out), nil
}

func isBase62(s string) bool {
	for i := range len(s) {
		if !strings.ContainsRune(base62Alphabet, rune(s[i])) {
			return false
		}
	}

	return true
}
