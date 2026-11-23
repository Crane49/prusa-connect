package dbstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

func newCredentials(tb testing.TB, e engine) *dbstore.Credentials {
	tb.Helper()

	store, err := dbstore.NewCredentials(e.Open(tb), e.Engine)
	require.NoError(tb, err)

	return store
}

// A session issued before a restart is still a session after one. That is the
// whole point of the table: an operator who was logged in stays logged in, and
// a script's api token keeps working.
func TestASessionOutlivesTheProcessThatIssuedIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		first, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		token, issued, err := auth.Issue(t.Context(), first, "user:ada", auth.KindSession, now)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// The next process: a new store and a new verifier over the same rows.
		second, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		verifier := auth.NewVerifier(second, func() time.Time { return now.Add(time.Hour) })

		resolved, err := verifier.Verify(t.Context(), token, auth.TransportHTTP)
		require.NoError(t, err)
		require.Equal(t, "user:ada", resolved.Subject)
		require.Equal(t, auth.KindSession, resolved.Kind)
		require.Equal(t, issued.ExpiresAt, resolved.ExpiresAt)
	})
}

func TestARevokedCredentialIsRefusedByTheNextProcess(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		token, cred, err := auth.IssueAPI(
			t.Context(),
			store,
			auth.APISpec{Subject: "user:ada", Name: "script", Scopes: []string{"read"}},
			now,
		)
		require.NoError(t, err)

		require.NoError(t, store.Revoke(t.Context(), cred.PublicID))

		next, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		_, err = auth.NewVerifier(next, func() time.Time { return now }).
			Verify(t.Context(), token, auth.TransportHTTP)
		require.True(t, auth.Refused(err), "a revoked credential was accepted: %v", err)

		// Revoking one that is not there is not an error: the caller wanted it
		// gone and it is gone.
		require.NoError(t, store.Revoke(t.Context(), cred.PublicID))
	})
}

// An empty id names no credential. Every row without a parent has an empty
// parent, so a revoke that matched it would take them all.
func TestRevokingAnEmptyIDRemovesNothing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)
		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		token, _, err := auth.IssueAPI(
			t.Context(),
			store,
			auth.APISpec{Subject: "user:ada", Name: "script", Scopes: []string{"read"}},
			now,
		)
		require.NoError(t, err)

		require.NoError(t, store.Revoke(t.Context(), ""))

		_, err = auth.NewVerifier(store, func() time.Time { return now }).
			Verify(t.Context(), token, auth.TransportHTTP)
		require.NoError(t, err)
	})
}

// A password change ends every session the person holds, on every transport at
// once, and the rows have to go for that to mean anything after a restart.
func TestRevokingASubjectRemovesEveryRowItHolds(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)

		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		for range 3 {
			_, _, err := auth.Issue(t.Context(), store, "user:ada", auth.KindSession, now)
			require.NoError(t, err)
		}

		_, other, err := auth.Issue(t.Context(), store, "user:grace", auth.KindSession, now)
		require.NoError(t, err)

		removed, err := store.RevokeSubject(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Equal(t, 3, removed)

		_, err = store.Lookup(t.Context(), other.PublicID)
		require.NoError(t, err, "another subject's credential went with it")

		removed, err = store.RevokeSubject(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Zero(t, removed)
	})
}

// Touch slides the window, and the slack keeps a page of requests from putting
// one write on the database each. What it must never do is hide a row that is
// not there.
func TestSlidingAWindowIsWrittenOncePerSlackWindow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)

		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		_, cred, err := auth.Issue(t.Context(), store, "user:ada", auth.KindSession, now)
		require.NoError(t, err)

		// Inside the slack window nothing is written, so the row still carries
		// the last-seen the issue wrote.
		require.NoError(t, store.Touch(t.Context(), cred.PublicID, now.Add(time.Second)))

		read, err := store.Lookup(t.Context(), cred.PublicID)
		require.NoError(t, err)
		require.Equal(t, now, read.LastSeen)

		later := now.Add(2 * time.Minute)
		require.NoError(t, store.Touch(t.Context(), cred.PublicID, later))

		read, err = store.Lookup(t.Context(), cred.PublicID)
		require.NoError(t, err)
		require.Equal(t, later, read.LastSeen)

		require.ErrorIs(t, store.Touch(t.Context(), "nobody", later), auth.ErrNoCredential)
	})
}

func TestAnExpiredCredentialIsSweptAndAMissIsNotAnError(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)

		now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)

		_, expired, err := auth.Issue(t.Context(), store, "user:ada", auth.KindSession, now)
		require.NoError(t, err)

		_, live, err := auth.IssueAPI(
			t.Context(),
			store,
			auth.APISpec{Subject: "user:ada", Name: "script", Scopes: []string{"read"}},
			now,
		)
		require.NoError(t, err)

		// Past the session's absolute lifetime and inside the api one.
		swept, err := store.DeleteExpired(t.Context(), now.Add(auth.SessionLifetime+time.Hour))
		require.NoError(t, err)
		require.Equal(t, 1, swept)

		_, err = store.Lookup(t.Context(), expired.PublicID)
		require.ErrorIs(t, err, auth.ErrNoCredential)

		_, err = store.Lookup(t.Context(), live.PublicID)
		require.NoError(t, err)
	})
}

// An api credential's name, its absent last-used time and its place in the
// subject's listing all survive the round trip through the table, on both
// engines. The zero time is the case worth holding: a mint writes one so a
// listing can say "never presented", and a driver that rendered it as some
// other instant would turn that into a date.
func TestAnAPICredentialListsByNameWithNoLastUsedUntilItIsPresented(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)

		now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

		token, first, err := auth.IssueAPI(t.Context(), store,
			auth.APISpec{Scopes: []string{"administer"}, Subject: "user:ada", Name: "nightly export"}, now)
		require.NoError(t, err)

		_, second, err := auth.IssueAPI(t.Context(), store,
			auth.APISpec{Scopes: []string{"administer"}, Subject: "user:ada", Name: "grafana"}, now.Add(time.Second))
		require.NoError(t, err)

		_, _, err = auth.Issue(t.Context(), store, "user:ada", auth.KindSession, now.Add(2*time.Second))
		require.NoError(t, err)

		_, _, err = auth.IssueAPI(
			t.Context(),
			store,
			auth.APISpec{Scopes: []string{"administer"}, Subject: "user:grace", Name: "hers"},
			now,
		)
		require.NoError(t, err)

		held, err := store.ListSubject(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Len(t, held, 3, "the store lists every kind the subject holds")

		require.Equal(t, first.PublicID, held[0].PublicID, "oldest first")
		require.Equal(t, "nightly export", held[0].Name)
		require.Equal(t, auth.KindAPI, held[0].Kind)
		require.True(t, held[0].LastSeen.IsZero(), "never presented reads back as never presented")
		require.Equal(t, second.PublicID, held[1].PublicID)
		require.Equal(t, "grafana", held[1].Name)

		presented := now.Add(time.Hour)
		_, err = auth.NewVerifier(store, func() time.Time { return presented }).
			Verify(t.Context(), token, auth.TransportHTTP)
		require.NoError(t, err)

		read, err := store.Lookup(t.Context(), first.PublicID)
		require.NoError(t, err)
		require.Equal(t, presented, read.LastSeen, "presenting it records when")

		none, err := store.ListSubject(t.Context(), "user:nobody")
		require.NoError(t, err)
		require.Empty(t, none)
	})
}

// A password write raises the account's generation in its own transaction. A
// row carrying an older generation is refused, and ending the account removes
// only those rows, so a session opened with the new password stays.
func TestAPasswordWriteFencesEveryRowOfAnEarlierGeneration(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		accounts, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		accounts = accounts.WithCredentialSubjects(func(id string) []string { return []string{id, "user:" + id} })

		now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)
		require.NoError(t, accounts.Create(t.Context(), local.Account{
			ID: "ada", Username: "ada@example.com", Hash: "$argon2id$old", CreatedAt: now, PasswordAt: now,
		}))

		checked, err := store.Generation(t.Context(), "ada")
		require.NoError(t, err)

		minter, err := store.Generation(t.Context(), "user:ada")
		require.NoError(t, err)

		_, err = accounts.ReplacePassword(t.Context(), "ada", "", "$argon2id$new", now)
		require.NoError(t, err)

		read, err := accounts.ByID(t.Context(), "ada")
		require.NoError(t, err)
		require.Equal(t, checked+1, read.Generation, "the password write did not raise the generation")

		verifier := auth.NewVerifier(store, func() time.Time { return now.Add(time.Minute) })

		raced, _, err := auth.IssueAtGeneration(t.Context(), store, "ada", auth.KindSession, now, checked)
		require.NoError(t, err)

		_, err = verifier.Verify(t.Context(), raced, auth.TransportHTTP)
		require.True(t, auth.Refused(err), "a session from before the change was accepted")

		forService, _, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
			Scopes: []string{
				"administer",
			},
			Subject:  "service_account:ci",
			Name:     "ci",
			MintedBy: "user:ada",
			Fence:    &auth.Fence{Minter: minter},
		}, now)
		require.NoError(t, err)

		_, err = verifier.Verify(t.Context(), forService, auth.TransportHTTP)
		require.True(t, auth.Refused(err), "a token the person minted before the change was accepted")

		other, _, err := auth.IssueAPI(
			t.Context(),
			store,
			auth.APISpec{
				Scopes:   []string{"administer"},
				Subject:  "service_account:ci",
				Name:     "grace's",
				MintedBy: "user:grace",
			},
			now,
		)
		require.NoError(t, err)

		fresh, _, err := auth.IssueAtGeneration(t.Context(), store, "ada", auth.KindSession, now, read.Generation)
		require.NoError(t, err)

		ended, err := verifier.EndSubjects(t.Context(), "ada", "user:ada")
		require.NoError(t, err)
		require.Equal(t, auth.Ended{Held: []int{1, 0}, MintedForOthers: 1}, ended)

		_, err = verifier.Verify(t.Context(), fresh, auth.TransportHTTP)
		require.NoError(t, err, "ending the older rows removed the session opened with the new password")

		_, err = verifier.Verify(t.Context(), other, auth.TransportHTTP)
		require.NoError(t, err, "a token somebody else minted for the service account was ended")
	})
}

// An empty subject, or a type prefix with no id, names nobody. Ending it
// would match rows it was never meant to reach, such as every session, whose
// minter is empty. The store refuses it and changes nothing.
func TestEndingASubjectThatNamesNobodyIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		pairings, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)

		held, _, err := auth.Issue(t.Context(), store, "grace", auth.KindSession, now)
		require.NoError(t, err)

		for _, subject := range []string{"", "user:", " "} {
			_, err = store.EndSubjects(t.Context(), []string{"ada", subject})
			require.ErrorIsf(t, err, auth.ErrInvalidSubject, "ending %q was not refused", subject)

			_, err = pairings.DeletePrincipal(t.Context(), subject)
			require.ErrorIsf(t, err, auth.ErrInvalidSubject, "unpairing %q was not refused", subject)
		}

		_, err = auth.NewVerifier(store, func() time.Time { return now }).Verify(t.Context(), held, auth.TransportHTTP)
		require.NoError(t, err, "a refused end changed somebody else's session")
	})
}

// A token and a pairing written down a chain through a second person carry
// their root, and the root's password change refuses and removes both, on
// both engines.
func TestARootsPasswordChangeEndsWhatItsChainWrote(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		pairings, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		accounts, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		accounts = accounts.WithCredentialSubjects(func(id string) []string { return []string{id, "user:" + id} })

		now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
		require.NoError(t, accounts.Create(t.Context(), local.Account{
			ID: "ada", Username: "ada@example.com", Hash: "$argon2id$old", CreatedAt: now, PasswordAt: now,
		}))

		token, _, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
			Scopes: []string{
				"administer",
			},
			Subject:     "service_account:ci",
			Name:        "ci",
			MintedBy:    "user:bob",
			RootCreator: "user:ada",
		}, now)
		require.NoError(t, err)

		require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
			Provider: "prusa-account", Subject: "second", Principal: "service_account:ci",
			CreatedBy: "user:bob", RootCreator: "user:ada", CreatedAt: now,
		}))

		_, err = accounts.ReplacePassword(t.Context(), "ada", "", "$argon2id$new", now)
		require.NoError(t, err)

		verifier := auth.NewVerifier(store, func() time.Time { return now.Add(time.Minute) })

		_, err = verifier.Verify(t.Context(), token, auth.TransportHTTP)
		require.True(t, auth.Refused(err), "a token whose root changed their password was accepted")

		paired, err := pairings.Lookup(t.Context(), "prusa-account", "second")
		require.NoError(t, err)
		require.Less(t, paired.RootGeneration, paired.RootCurrentGeneration,
			"the pairing does not read as older than its root")

		ended, err := verifier.EndSubjects(t.Context(), "ada", "user:ada")
		require.NoError(t, err)
		require.Equal(t, 1, ended.MintedForOthers)

		removed, err := pairings.DeleteCreatedBy(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Equal(t, 1, removed)
	})
}

// The scopes and the origin of an api credential are read back as they were
// minted, on both engines, so a restart cannot widen a credential.
func TestTheScopesOfACredentialOutliveTheProcess(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		first, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)

		_, minted, err := auth.IssueAPI(t.Context(), first, auth.APISpec{
			Subject: "user:ada", Name: "kitchen display", Scopes: []string{"read", "operate"},
			Origin: auth.OriginAppPairing,
		}, now)
		require.NoError(t, err)

		second, err := dbstore.NewCredentials(db, e.Engine)
		require.NoError(t, err)

		read, err := second.Lookup(t.Context(), minted.PublicID)
		require.NoError(t, err)
		require.Equal(t, []string{"read", "operate"}, read.Scopes)
		require.Equal(t, auth.OriginAppPairing, read.Origin)

		listed, err := second.ListSubject(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.Equal(t, []string{"read", "operate"}, listed[0].Scopes)
	})
}

// A page session names the app pairing credential it stands in for, on both
// engines, and the verifier over the table refuses it once that credential is
// revoked (ADR-20261112-nay).
func TestAPageSessionKeepsItsParentInTheTable(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newCredentials(t, e)
		now := time.Now().UTC().Truncate(time.Second)

		_, parent, err := auth.IssueAPI(t.Context(), store, auth.APISpec{
			Subject: "user:ada", Name: "PrusaSlicer", Scopes: []string{"operate"},
			Origin: auth.OriginAppPairing,
		}, now)
		require.NoError(t, err)

		token, child, err := auth.IssuePageSession(t.Context(), store, parent, now)
		require.NoError(t, err)

		read, err := store.Lookup(t.Context(), child.PublicID)
		require.NoError(t, err)
		require.Equal(t, parent.PublicID, read.Parent)

		verifier := auth.NewVerifier(store, nil)

		_, err = verifier.Verify(t.Context(), token, auth.TransportHTTP)
		require.NoError(t, err)

		children, err := store.Children(t.Context(), parent.PublicID)
		require.NoError(t, err)
		require.Len(t, children, 1)

		require.NoError(t, verifier.Revoke(t.Context(), parent.PublicID))

		_, err = verifier.Verify(t.Context(), token, auth.TransportHTTP)
		require.True(t, auth.Refused(err), "a page session outlived its parent: %v", err)

		_, err = store.Lookup(t.Context(), child.PublicID)
		require.ErrorIs(t, err, auth.ErrNoCredential, "revoking the parent left the page session's row")
	})
}
