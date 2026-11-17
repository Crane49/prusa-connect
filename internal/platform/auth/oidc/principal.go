package oidc

import (
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// ErrSubject reports a `sub` claim this server will not sign anybody in with.
var ErrSubject = errors.New("oidc: unusable subject")

const (
	// DerivedPrefix starts every id derived from an issuer and a subject. A
	// subject that starts with it is derived too, even when it would fit the
	// id grammar as it is. So no subject an issuer hands out can be kept as an
	// id that another subject derives to.
	DerivedPrefix = "oidc-"

	// derivedChars is the base32 text kept from the hash: 160 bits.
	derivedChars = 32

	// maxSubjectBytes is the length OpenID Connect Core section 2 allows a
	// `sub` claim.
	maxSubjectBytes = 255
)

// derivedEncoding is base32 in lowercase, which the id grammar takes.
var derivedEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// PrincipalID is the user id a person signed in at issuer under subject has
// in the authorization graph.
//
// A subject that is already a valid id, and does not start with
// DerivedPrefix, is the id unchanged. Any other subject, such as authentik's
// 64-character hash or Auth0's `auth0|...`, becomes DerivedPrefix and 32
// characters of the SHA-256 of the issuer and the subject. The issuer is in
// the hash because a subject is unique only at one issuer.
func PrincipalID(issuer, subject string) string {
	if ref.ValidateID(subject) == nil && !strings.HasPrefix(subject, DerivedPrefix) {
		return subject
	}

	sum := sha256.Sum256([]byte(normalIssuer(issuer) + "\x00" + subject))

	return DerivedPrefix + derivedEncoding.EncodeToString(sum[:])[:derivedChars]
}

// CheckSubject refuses a subject this server will not take: empty, longer
// than the specification allows, or holding a space or a control character.
// Any other subject has an id through PrincipalID.
func CheckSubject(subject string) error {
	if subject == "" {
		return fmt.Errorf("%w: it is empty", ErrSubject)
	}

	if len(subject) > maxSubjectBytes {
		return fmt.Errorf("%w: it is %d bytes, and a subject has at most %d", ErrSubject, len(subject), maxSubjectBytes)
	}

	for _, c := range subject {
		if c <= ' ' || c == 0x7f {
			return fmt.Errorf("%w: it holds a space or a control character", ErrSubject)
		}
	}

	return nil
}

// normalIssuer drops a trailing slash. Providers disagree about it, and the
// configured issuer and the one in the discovery document are compared the
// same way.
func normalIssuer(issuer string) string {
	return strings.TrimSuffix(issuer, "/")
}
