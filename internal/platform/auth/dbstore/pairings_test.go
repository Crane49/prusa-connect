package dbstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// A pairing written by one process is read by the next. That is the point of
// the table: an administrator pairs a Prusa Account once and the bearer path
// still resolves it after a restart, on both engines.
func TestAPairingOutlivesTheProcessThatWroteIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		first, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		written := auth.Pairing{
			Provider:  "prusa-account",
			Subject:   "8f1e1b2c-0000-4000-8000-abcdefabcdef",
			Principal: "user:ada",
			Label:     "Ada's account",
			CreatedAt: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC),
		}
		require.NoError(t, first.Save(t.Context(), written))

		second, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		read, err := second.Lookup(t.Context(), written.Provider, written.Subject)
		require.NoError(t, err)
		require.Equal(t, written, read)

		_, err = second.Lookup(t.Context(), written.Provider, "nobody")
		require.ErrorIs(t, err, auth.ErrNoPairing)

		_, err = second.Lookup(t.Context(), "another-provider", written.Subject)
		require.ErrorIs(t, err, auth.ErrNoPairing, "a pairing is keyed by provider and subject together")
	})
}

// A subject is paired once. The second write is refused and leaves the first
// row as it was, so a subject cannot be pointed at another principal without
// unpairing it first.
func TestASubjectIsPairedOnce(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store, err := dbstore.NewPairings(e.Open(t), e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
		row := auth.Pairing{Provider: "prusa-account", Subject: "s1", Principal: "user:ada", CreatedAt: now}
		require.NoError(t, store.Save(t.Context(), row))

		row.Principal = "user:mallory"
		require.ErrorIs(t, store.Save(t.Context(), row), auth.ErrPairingExists)

		kept, err := store.Lookup(t.Context(), "prusa-account", "s1")
		require.NoError(t, err)
		require.Equal(t, "user:ada", kept.Principal)

		require.ErrorIs(t, store.Save(t.Context(), auth.Pairing{Provider: "prusa-account", Subject: "s2"}),
			auth.ErrInvalidPairing)
	})
}

// The listing is ordered by provider then subject, filtered by provider when
// asked, and a delete reports whether anything was there.
func TestPairingsAreListedInOrderAndDeletedOnce(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store, err := dbstore.NewPairings(e.Open(t), e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)

		for _, row := range []auth.Pairing{
			{Provider: "prusa-account", Subject: "b", Principal: "user:b", CreatedAt: now},
			{Provider: "other", Subject: "z", Principal: "user:z", CreatedAt: now},
			{Provider: "prusa-account", Subject: "a", Principal: "service_account:a", CreatedAt: now},
		} {
			require.NoError(t, store.Save(t.Context(), row))
		}

		all, err := store.List(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, all, 3)
		require.Equal(t, []string{"other", "prusa-account", "prusa-account"},
			[]string{all[0].Provider, all[1].Provider, all[2].Provider})
		require.Equal(t, []string{"z", "a", "b"}, []string{all[0].Subject, all[1].Subject, all[2].Subject})

		one, err := store.List(t.Context(), "prusa-account")
		require.NoError(t, err)
		require.Len(t, one, 2)

		require.NoError(t, store.Delete(t.Context(), "prusa-account", "a"))
		require.ErrorIs(t, store.Delete(t.Context(), "prusa-account", "a"), auth.ErrNoPairing)

		left, err := store.List(t.Context(), "prusa-account")
		require.NoError(t, err)
		require.Len(t, left, 1)
		require.Equal(t, "b", left[0].Subject)
	})
}

func TestAPairingStoreNeedsAHandleAndAKnownEngine(t *testing.T) {
	t.Parallel()

	_, err := dbstore.NewPairings(nil, dbstore.EngineSQLite)
	require.Error(t, err)

	forEachEngine(t, func(t *testing.T, e engine) {
		_, refused := dbstore.NewPairings(e.Open(t), dbstore.Engine(0))
		require.ErrorIs(t, refused, dbstore.ErrUnsupportedEngine)
	})
}

// A pairing carries the principal's generation from when it was checked for.
// A password write raises the generation. The older pairing then reads as
// older, and ending the principal removes it and keeps one written after.
func TestAPairingIsFencedByItsPrincipalsGeneration(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		accounts, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		accounts = accounts.WithCredentialSubjects(func(id string) []string { return []string{id, "user:" + id} })

		now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)
		require.NoError(t, accounts.Create(t.Context(), local.Account{
			ID: "ada", Username: "ada@example.com", Hash: "$argon2id$old", CreatedAt: now, PasswordAt: now,
		}))

		for _, row := range []auth.Pairing{
			{Provider: "prusa-account", Subject: "s1", Principal: "user:ada", CreatedAt: now},
			{Provider: "prusa-account", Subject: "s3", Principal: "user:grace", CreatedAt: now},
		} {
			require.NoError(t, store.Save(t.Context(), row))
		}

		_, err = accounts.ReplacePassword(t.Context(), "ada", "", "$argon2id$new", now)
		require.NoError(t, err)

		older, err := store.Lookup(t.Context(), "prusa-account", "s1")
		require.NoError(t, err)
		require.Less(t, older.Generation, older.CurrentGeneration, "the pairing does not read as older")

		require.NoError(t, store.Save(t.Context(), auth.Pairing{
			Provider: "prusa-account", Subject: "s2", Principal: "user:ada", CreatedAt: now,
			Generation: older.CurrentGeneration,
		}))

		removed, err := store.DeletePrincipal(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Equal(t, 1, removed)

		left, err := store.List(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, left, 2)
		require.Equal(t, "s2", left[0].Subject, "the pairing written after the change was removed")
		require.Equal(t, "user:grace", left[1].Principal)
	})
}

// A pairing records who made it and the maker's generation. The maker's
// password change leaves it older, and ending the maker removes it, whatever
// principal it names.
func TestAPairingIsFencedByItsCreatorsGeneration(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewPairings(db, e.Engine)
		require.NoError(t, err)

		accounts, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		accounts = accounts.WithCredentialSubjects(func(id string) []string { return []string{id, "user:" + id} })

		now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
		require.NoError(t, accounts.Create(t.Context(), local.Account{
			ID: "ada", Username: "ada@example.com", Hash: "$argon2id$old", CreatedAt: now, PasswordAt: now,
		}))

		require.NoError(t, store.Save(t.Context(), auth.Pairing{
			Provider: "prusa-account", Subject: "attacker", Principal: "service_account:ci",
			CreatedBy: "user:ada", CreatedAt: now,
		}))
		require.NoError(t, store.Save(t.Context(), auth.Pairing{
			Provider: "prusa-account", Subject: "grace's", Principal: "service_account:ci",
			CreatedBy: "user:grace", CreatedAt: now,
		}))

		_, err = accounts.ReplacePassword(t.Context(), "ada", "", "$argon2id$new", now)
		require.NoError(t, err)

		older, err := store.Lookup(t.Context(), "prusa-account", "attacker")
		require.NoError(t, err)
		require.Equal(t, "user:ada", older.CreatedBy)
		require.Less(t, older.CreatorGeneration, older.CreatorCurrentGeneration,
			"the pairing does not read as older than its creator")

		removed, err := store.DeleteCreatedBy(t.Context(), "user:ada")
		require.NoError(t, err)
		require.Equal(t, 1, removed)

		left, err := store.List(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, left, 1)
		require.Equal(t, "grace's", left[0].Subject)
	})
}
