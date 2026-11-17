package auth

import (
	"context"
	"errors"
	"time"
)

// ErrNoOIDCSubject reports a principal nobody has signed in as through the
// OpenID Connect provider.
var ErrNoOIDCSubject = errors.New("auth: nobody signed in at the provider under that id")

// OIDCSubject is one person who signed in through the OpenID Connect provider:
// the id the authorization graph knows them by, the issuer and subject it was
// made from, and what the provider last said about them.
//
// The id is derived from the issuer and the subject, so a login never needs
// this row to find it. The row lets a person reading an id such as
// oidc-p5tcc... find the subject and the name behind it.
type OIDCSubject struct {
	// Principal is the bare user id, without the user: type.
	Principal   string
	Issuer      string
	Subject     string
	DisplayName string
	Email       string
	FirstSeen   time.Time
	LastSeen    time.Time
}

// OIDCSubjects keeps the people who signed in through the OpenID Connect
// provider.
type OIDCSubjects interface {
	// Record writes the person at a sign-in. A person seen before keeps their
	// FirstSeen, and the rest is replaced.
	Record(ctx context.Context, subject OIDCSubject) error
	// Get returns the person under a bare user id, or ErrNoOIDCSubject.
	Get(ctx context.Context, principal string) (OIDCSubject, error)
}
