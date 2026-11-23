package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// A page session speaks for the person of the app pairing credential, with
// its scopes and at its generations, lives at most an hour and never past the
// credential, and is refused once the credential is revoked or expired, or
// the person's password changes (ADR-20261112-nay).
func TestAPageSessionStandsInForItsAppPairingCredential(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	clock := pairedClock
	verifier := auth.NewVerifier(store, func() time.Time { return clock })

	_, parent := issuePaired(t, store, auth.OriginAppPairing)

	token, child, err := auth.IssuePageSession(t.Context(), store, parent, pairedClock)
	require.NoError(t, err)
	require.Equal(t, parent.PublicID, child.Parent)
	require.Equal(t, parent.Scopes, child.Scopes)
	require.Equal(t, parent.Subject, child.Subject)
	require.Equal(t, pairedClock.Add(auth.PageSessionLifetime), child.ExpiresAt)

	got, err := verifier.Verify(t.Context(), token, auth.TransportHTTP)
	require.NoError(t, err)
	require.Equal(t, child.PublicID, got.PublicID)

	_, err = verifier.Verify(t.Context(), auth.WrapJWT(token, child), auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a page session was read in the JWT shape")

	t.Run("it never outlives its parent", func(t *testing.T) {
		t.Parallel()

		short := store
		_, near, issueErr := auth.IssueAPI(t.Context(), short, auth.APISpec{
			Subject: "user:ada", Name: "PrusaSlicer", Scopes: []string{"operate"},
			Origin: auth.OriginAppPairing, ExpiresAt: pairedClock.Add(time.Minute),
		}, pairedClock)
		require.NoError(t, issueErr)

		_, bounded, issueErr := auth.IssuePageSession(t.Context(), short, near, pairedClock)
		require.NoError(t, issueErr)
		require.Equal(t, near.ExpiresAt, bounded.ExpiresAt)
	})

	t.Run("only an app pairing credential opens one", func(t *testing.T) {
		t.Parallel()

		_, minted := issuePaired(t, store, auth.OriginMint)
		_, _, refused := auth.IssuePageSession(t.Context(), store, minted, pairedClock)
		require.ErrorIs(t, refused, auth.ErrNotAppCredential)

		_, _, refused = auth.IssuePageSession(t.Context(), store, child, pairedClock)
		require.ErrorIs(t, refused, auth.ErrNotAppCredential, "a page session opened another")

		_, session, issueErr := auth.Issue(t.Context(), store, "user:ada", auth.KindSession, pairedClock)
		require.NoError(t, issueErr)

		_, _, refused = auth.IssuePageSession(t.Context(), store, session, pairedClock)
		require.ErrorIs(t, refused, auth.ErrNotAppCredential)
	})
}

// Revoking, expiring or fencing the parent ends the page session.
func TestAPageSessionEndsWithItsParent(t *testing.T) {
	t.Parallel()

	for name, end := range map[string]func(t *testing.T, store *auth.MemoryStore, parent auth.Credential) *auth.Verifier{
		"revoked": func(t *testing.T, store *auth.MemoryStore, parent auth.Credential) *auth.Verifier {
			t.Helper()

			verifier := auth.NewVerifier(store, func() time.Time { return pairedClock })
			require.NoError(t, verifier.Revoke(t.Context(), parent.PublicID))

			return verifier
		},
		"expired": func(_ *testing.T, store *auth.MemoryStore, parent auth.Credential) *auth.Verifier {
			return auth.NewVerifier(store, func() time.Time { return parent.ExpiresAt })
		},
		"a password change": func(_ *testing.T, store *auth.MemoryStore, parent auth.Credential) *auth.Verifier {
			store.Bump(parent.Subject)

			return auth.NewVerifier(store, func() time.Time { return pairedClock })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := auth.NewMemoryStore()

			_, parent, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
				Subject: "user:ada", Name: "PrusaSlicer", Scopes: []string{"operate"},
				Origin: auth.OriginAppPairing, ExpiresAt: pairedClock.Add(30 * time.Minute),
			}, pairedClock)
			require.NoError(t, err)

			token, _, err := auth.IssuePageSession(t.Context(), store, parent, pairedClock)
			require.NoError(t, err)

			verifier := end(t, store, parent)

			_, err = verifier.Verify(t.Context(), token, auth.TransportHTTP)
			require.True(t, auth.Refused(err), "the page session outlived a parent that was %s", name)
		})
	}
}

// One app pairing credential keeps at most MaxPageSessions live page
// sessions, an expired one is deleted when the next one opens, and revoking
// the credential deletes every page session it opened (ADR-20261112-nay).
func TestAParentKeepsAFewPageSessionsAndTakesThemWithIt(t *testing.T) {
	t.Parallel()

	store := auth.NewMemoryStore()
	_, parent := issuePaired(t, store, auth.OriginAppPairing)

	// One opened long enough ago to have expired.
	_, stale, err := auth.IssuePageSession(t.Context(), store, parent, pairedClock.Add(-2*time.Hour))
	require.NoError(t, err)

	opened := make([]auth.Credential, 0, auth.MaxPageSessions+2)

	for i := range auth.MaxPageSessions + 2 {
		_, child, issueErr := auth.IssuePageSession(
			t.Context(),
			store,
			parent,
			pairedClock.Add(time.Duration(i)*time.Second),
		)
		require.NoError(t, issueErr)

		opened = append(opened, child)
	}

	children, err := store.Children(t.Context(), parent.PublicID)
	require.NoError(t, err)
	require.Len(t, children, auth.MaxPageSessions)

	kept := make([]string, 0, len(children))
	for _, child := range children {
		kept = append(kept, child.PublicID)
	}

	require.NotContains(t, kept, stale.PublicID, "an expired page session stayed")
	require.NotContains(t, kept, opened[0].PublicID, "the oldest live page session stayed past the cap")
	require.Contains(t, kept, opened[len(opened)-1].PublicID)

	require.NoError(t, auth.NewVerifier(store, nil).Revoke(t.Context(), parent.PublicID))

	children, err = store.Children(t.Context(), parent.PublicID)
	require.NoError(t, err)
	require.Empty(t, children, "revoking the parent left its page sessions")
}
