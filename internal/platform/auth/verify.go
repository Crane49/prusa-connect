package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// refusalDetail is the only thing a refused caller is ever told. Every refusal
// reaches it: a token that was never issued, one that expired, one presented on
// a transport its kind is refused on, one that is not shaped like a token, and
// a store that could not answer. Saying which of those happened would tell an
// attacker which half of the token to keep and which to change.
const refusalDetail = "the credential is not one this server accepts"

// dummyDigest is compared against when no row was found, so a miss costs the
// same comparison as a hit. Without it the fastest way out of Verify would be
// the "this token does not exist" path, which is a timing oracle for guessing
// public ids.
var dummyDigest = DigestOf("credential that does not exist")

// Verifier resolves a presented token to the credential behind it. It is the
// only thing that decides whether a caller is admitted, and it has no bypass.
// There is no flag on it, no environment it behaves differently in, and no
// branch a test can take that a request cannot.
type Verifier struct {
	store Store
	now   func() time.Time
}

// NewVerifier returns a Verifier over a store. A nil clock means time.Now.
func NewVerifier(store Store, now func() time.Time) *Verifier {
	if now == nil {
		now = time.Now
	}

	return &Verifier{store: store, now: now}
}

// Verify resolves a token presented on a transport, or refuses. Every refusal
// is the same fault with the same detail. The reason is wrapped for the log
// line, and the code is registered as redacting, so it does not reach a client.
//
// It is fail-closed: a store that returns an error refuses the request. No
// configuration admits a credential that cannot be resolved, so
// session.Require is fail-closed too.
//
// A token of the JWT shape an app pairing credential travels in is read from
// its third segment, and its payload must name the row that segment found
// (ADR-20261112-nay).
func (v *Verifier) Verify(ctx context.Context, token Token, on Transport) (Credential, error) {
	inner, claims, err := unwrapJWT(token)
	if err != nil {
		subtle.ConstantTimeCompare(dummyDigest[:], dummyDigest[:])

		return Credential{}, refuse(err)
	}

	return v.verify(ctx, inner, claims, on)
}

// verify is Verify over the minted token, with the claims of the JWT shape it
// arrived in, nil when it arrived bare.
func (v *Verifier) verify(ctx context.Context, token Token, claims *MintedClaims, on Transport) (Credential, error) {
	publicID, secret, err := Split(token)
	if err != nil {
		// The comparison is charged anyway. A malformed token must not be the
		// cheapest answer this function can give.
		subtle.ConstantTimeCompare(dummyDigest[:], dummyDigest[:])

		return Credential{}, refuse(err)
	}

	cred, lookupErr := v.store.Lookup(ctx, publicID)
	if lookupErr != nil {
		cred = Credential{Digest: dummyDigest}
	}

	presented := DigestOf(secret)
	matched := subtle.ConstantTimeCompare(presented[:], cred.Digest[:]) == 1

	if lookupErr != nil {
		return Credential{}, refuse(fmt.Errorf("lookup %q: %w", publicID, lookupErr))
	}

	if !matched {
		return Credential{}, refuse(fmt.Errorf("%w: secret does not match %q", ErrNoCredential, publicID))
	}

	if cred.Generation < cred.CurrentGeneration {
		return Credential{}, refuse(fmt.Errorf("credential %q is from generation %d of %q, which is now %d",
			publicID, cred.Generation, cred.Subject, cred.CurrentGeneration))
	}

	if cred.RootCreator != "" && cred.RootGeneration < cred.RootCurrentGeneration {
		return Credential{}, refuse(fmt.Errorf(
			"credential %q was minted under root %q at generation %d, which is now %d",
			publicID, cred.RootCreator, cred.RootGeneration, cred.RootCurrentGeneration))
	}

	if cred.MintedBy != "" && cred.MinterGeneration < cred.MinterCurrentGeneration {
		return Credential{}, refuse(fmt.Errorf("credential %q was minted at generation %d of %q, which is now %d",
			publicID, cred.MinterGeneration, cred.MintedBy, cred.MinterCurrentGeneration))
	}

	now := v.now()
	if !cred.Live(now) {
		return Credential{}, refuse(fmt.Errorf("credential %q expired at %s", publicID, cred.ExpiresAt))
	}

	// The row's own expiry is not trusted on its own. A row written by hand or
	// by an older build may name any expiry.
	if limit, ok := upperLimitOf(cred.Kind); ok && !now.Before(cred.IssuedAt.Add(limit)) {
		return Credential{}, refuse(fmt.Errorf("credential %q is older than a %s credential may live",
			publicID, cred.Kind))
	}

	if !cred.Kind.AcceptedOn(on) {
		return Credential{}, refuse(fmt.Errorf("%s credential presented on %s", cred.Kind, on))
	}

	// An api credential with no scope would read as one with no limit.
	if cred.Kind == KindAPI && len(cred.Scopes) == 0 {
		return Credential{}, refuse(fmt.Errorf("%w: %q", ErrNoScope, publicID))
	}

	if claims != nil {
		if matchErr := claims.match(cred); matchErr != nil {
			return Credential{}, refuse(matchErr)
		}
	}

	if parentErr := v.parentLive(ctx, cred, now); parentErr != nil {
		return Credential{}, refuse(parentErr)
	}

	// Every kind is touched. For a session the write slides the idle window,
	// and a store that cannot write it cannot keep the window, so the request
	// is refused. For an api credential the write is the last-used time a
	// listing renders, and the refusal is the same: a store that cannot write
	// cannot answer.
	if touchErr := v.store.Touch(ctx, publicID, now); touchErr != nil {
		return Credential{}, refuse(fmt.Errorf("slide %q: %w", publicID, touchErr))
	}

	cred.LastSeen = now

	return cred, nil
}

// Revoke deletes a credential. The token is opaque and checked against its
// row, so a revoked one is refused on the next request and does not wait for
// its expiry.
func (v *Verifier) Revoke(ctx context.Context, publicID string) error {
	if err := v.store.Revoke(ctx, publicID); err != nil {
		return fmt.Errorf("revoke %q: %w", publicID, err)
	}

	return nil
}

// RevokeSubject deletes every credential a subject holds and reports how many
// it removed. A password change calls it, so a session opened under the old
// password is refused on its next request instead of living out its window.
func (v *Verifier) RevokeSubject(ctx context.Context, subject string) (int, error) {
	removed, err := v.store.RevokeSubject(ctx, subject)
	if err != nil {
		return 0, fmt.Errorf("revoke every credential of %q: %w", subject, err)
	}

	return removed, nil
}

// EndSubjects deletes every credential of the named subjects that is older
// than the subject's current generation. A password change calls it with
// every spelling of the account, after the write that raised the generation.
//
// The delete only tidies. Verify refuses an older row whether or not it is
// still there, so a row a racing mint saves after this runs is refused too.
func (v *Verifier) EndSubjects(ctx context.Context, subjects ...string) (Ended, error) {
	removed, err := v.store.EndSubjects(ctx, subjects)
	if err != nil {
		return Ended{}, fmt.Errorf("end the credentials of %q: %w", subjects, err)
	}

	return removed, nil
}

// Refused reports whether an error is this package's refusal, whatever caused
// it. A caller branches on this and not on the cause, because the cause cannot
// be told apart from outside.
func Refused(err error) bool {
	return fault.HasCode(err, fault.AuthTokenInvalid.Code())
}

// Refuse builds the one refusal this package gives, for a caller that could not
// reach a Verifier. It is the same fault with the same wording as every refusal
// Verify gives, so "the server could not check" cannot be told apart from "the
// credential is not valid".
func Refuse(cause error) error { return refuse(cause) }

// refuse wraps a cause in the one fault a caller ever sees.
func refuse(cause error) error {
	return fault.AuthTokenInvalid.New(refusalDetail).Wrap(cause)
}

// UnwrapCause returns the reason behind a refusal, for a log line. It is not
// rendered to a client anywhere.
func UnwrapCause(err error) error {
	var f *fault.Fault
	if !errors.As(err, &f) {
		return err
	}

	return f.Unwrap()
}
