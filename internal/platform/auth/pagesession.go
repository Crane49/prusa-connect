package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// A page session lets a page inside an app use the app's pairing credential
// without holding it (ADR-20261112-nay). The app hands the page its
// credential, the page trades it once for a page session, and the server sets
// the page session in an HttpOnly cookie no script reads. The page session is
// an api credential of its own: it speaks for the same person with the same
// scopes, at the same generations, and the verifier refuses it once the
// credential it stands in for is gone or expired.

// ErrNotAppCredential reports a credential that cannot open a page session:
// anything but a live app pairing credential, a page session included.
var ErrNotAppCredential = errors.New("auth: only an app pairing credential opens a page session")

// IssuePageSession mints a page session for a verified app pairing
// credential. It lives PageSessionLifetime, and never past the parent's
// expiry.
func IssuePageSession(ctx context.Context, store Store, parent Credential, now time.Time) (Token, Credential, error) {
	if err := OpensPageSession(parent, now); err != nil {
		return "", Credential{}, err
	}

	expiresAt := now.Add(PageSessionLifetime)
	if parent.ExpiresAt.Before(expiresAt) {
		expiresAt = parent.ExpiresAt
	}

	token, cred, err := mint(ctx, store, Credential{
		Subject:          parent.Subject,
		Kind:             KindAPI,
		Name:             parent.Name,
		MintedBy:         parent.MintedBy,
		Generation:       parent.Generation,
		MinterGeneration: parent.MinterGeneration,
		RootCreator:      parent.RootCreator,
		RootGeneration:   parent.RootGeneration,
		IssuedAt:         now,
		ExpiresAt:        expiresAt,
		Scopes:           slices.Clone(parent.Scopes),
		Origin:           OriginAppPairing,
		Parent:           parent.PublicID,
	})
	if err != nil {
		return "", Credential{}, err
	}

	if err = prunePageSessions(ctx, store, parent.PublicID, now); err != nil {
		return "", Credential{}, err
	}

	return token, cred, nil
}

// MaxPageSessions is how many live page sessions one app pairing credential
// keeps. Opening one more ends the oldest, so an app that opens a page
// session on every page load does not grow the table.
const MaxPageSessions = 4

// prunePageSessions deletes the expired page sessions of a parent, and the
// oldest live ones past MaxPageSessions.
func prunePageSessions(ctx context.Context, store Store, parent string, now time.Time) error {
	children, err := store.Children(ctx, parent)
	if err != nil {
		return fmt.Errorf("list the page sessions of %q: %w", parent, err)
	}

	live := make([]Credential, 0, len(children))

	for _, child := range children {
		if child.Live(now) {
			live = append(live, child)

			continue
		}

		if err = store.Revoke(ctx, child.PublicID); err != nil {
			return fmt.Errorf("delete the expired page session %q: %w", child.PublicID, err)
		}
	}

	// Children are oldest first, so the ones past the cap are at the front.
	for len(live) > MaxPageSessions {
		if err = store.Revoke(ctx, live[0].PublicID); err != nil {
			return fmt.Errorf("end the page session %q: %w", live[0].PublicID, err)
		}

		live = live[1:]
	}

	return nil
}

// OpensPageSession reports why a verified credential cannot open a page
// session, or nil when it can: it is a live app pairing credential and not a
// page session itself.
func OpensPageSession(parent Credential, now time.Time) error {
	if parent.Kind != KindAPI || parent.Origin != OriginAppPairing || parent.Parent != "" {
		return fmt.Errorf("%w: %q is a %s credential of origin %q",
			ErrNotAppCredential, parent.PublicID, parent.Kind, parent.Origin)
	}

	if !parent.Live(now) {
		return fmt.Errorf("%w: %q expired at %s", ErrNotAppCredential, parent.PublicID, parent.ExpiresAt)
	}

	return nil
}

// parentLive reports why a page session's parent no longer backs it, or nil
// when it does. The parent is read again on every request, so revoking it
// ends the page session at once.
func (v *Verifier) parentLive(ctx context.Context, cred Credential, now time.Time) error {
	if cred.Parent == "" {
		return nil
	}

	parent, err := v.store.Lookup(ctx, cred.Parent)

	switch {
	case err != nil:
		return fmt.Errorf("the parent %q of %q: %w", cred.Parent, cred.PublicID, err)
	case parent.Kind != KindAPI || parent.Origin != OriginAppPairing || parent.Parent != "":
		return fmt.Errorf("%w: the parent %q of %q", ErrNotAppCredential, cred.Parent, cred.PublicID)
	case !parent.Live(now):
		return fmt.Errorf("the parent %q of %q expired at %s", cred.Parent, cred.PublicID, parent.ExpiresAt)
	case parent.Generation < parent.CurrentGeneration:
		return fmt.Errorf("the parent %q of %q is from an older generation", cred.Parent, cred.PublicID)
	}

	return nil
}
