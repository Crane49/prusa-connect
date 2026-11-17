package dbstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// newAccounts is a migrated, empty account store on this engine.
func newAccounts(tb testing.TB, e engine) *dbstore.Accounts {
	tb.Helper()

	store, err := dbstore.NewAccounts(e.Open(tb), e.Engine)
	require.NoError(tb, err)

	return store
}

// anAccount is one row, with the two instants a store round trip has to keep.
func anAccount(id, username string) local.Account {
	at := time.Date(2026, time.September, 2, 10, 30, 0, 0, time.UTC)

	return local.Account{
		ID:          id,
		Username:    username,
		DisplayName: "A person",
		Hash:        "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		CreatedAt:   at,
		PasswordAt:  at,
	}
}

func TestAnAccountSurvivesTheProcessThatWroteIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		first, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		want := anAccount("u1", "ada@example.com")
		require.NoError(t, first.Create(t.Context(), want))

		// A second store over the same database is what the next process is.
		second, err := dbstore.NewAccounts(db, e.Engine)
		require.NoError(t, err)

		got, err := second.ByUsername(t.Context(), want.Username)
		require.NoError(t, err)
		require.Equal(t, want, got)

		count, err := second.Count(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, count, "the next process reports a first run")
	})
}

func TestAnAccountStoreRefusesADuplicateIdAndADuplicateUsername(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newAccounts(t, e)

		require.NoError(t, store.Create(t.Context(), anAccount("u1", "ada@example.com")))

		err := store.Create(t.Context(), anAccount("u1", "grace@example.com"))
		require.ErrorIs(t, err, local.ErrIDTaken)

		err = store.Create(t.Context(), anAccount("u2", "ada@example.com"))
		require.ErrorIs(t, err, local.ErrUsernameTaken)
	})
}

func TestAnAccountStoreReadsBackWhatItWrote(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newAccounts(t, e)

		require.NoError(t, store.Create(t.Context(), anAccount("u2", "grace@example.com")))
		require.NoError(t, store.Create(t.Context(), anAccount("u1", "ada@example.com")))

		rows, err := store.List(t.Context())
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Equal(t, "ada@example.com", rows[0].Username, "the listing is not in username order")

		changed := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)
		require.NoError(t, store.SetPassword(t.Context(), "u1", "", "$argon2id$new", changed))

		got, err := store.ByID(t.Context(), "u1")
		require.NoError(t, err)
		require.Equal(t, "$argon2id$new", got.Hash)
		require.Equal(t, changed, got.PasswordAt)

		require.ErrorIs(t, store.SetPassword(t.Context(), "nobody", "", "$argon2id$new", changed),
			local.ErrNoAccount)

		_, err = store.ByID(t.Context(), "nobody")
		require.ErrorIs(t, err, local.ErrNoAccount)

		_, err = store.ByUsername(t.Context(), "nobody@example.com")
		require.ErrorIs(t, err, local.ErrNoAccount)
	})
}

// Nothing could log in to a server with no accounts, and the setup path would
// reopen for whoever reached it first.
func TestTheLastAccountCannotBeDeleted(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newAccounts(t, e)

		require.NoError(t, store.Create(t.Context(), anAccount("u1", "ada@example.com")))
		require.NoError(t, store.Create(t.Context(), anAccount("u2", "grace@example.com")))

		require.NoError(t, store.Delete(t.Context(), "u2"))
		require.ErrorIs(t, store.Delete(t.Context(), "u1"), local.ErrLastAccount)
		require.ErrorIs(t, store.Delete(t.Context(), "nobody"), local.ErrNoAccount)

		count, err := store.Count(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}

// A password write names the hash it expects to replace. Two changes that both
// expected the old hash cannot both land, and a missing account is still a
// miss rather than a conflict.
func TestAPasswordWriteLandsOnlyOverTheHashItExpected(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store, err := dbstore.NewAccounts(e.Open(t), e.Engine)
		require.NoError(t, err)

		require.NoError(t, store.Create(t.Context(), anAccount("u1", "ada@example.com")))

		before, err := store.ByID(t.Context(), "u1")
		require.NoError(t, err)

		changed := time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC)

		_, err = store.ReplacePassword(t.Context(), "u1", before.Hash, "$argon2id$first", changed)
		require.NoError(t, err)
		_, err = store.ReplacePassword(t.Context(), "u1", before.Hash, "$argon2id$second", changed)
		require.ErrorIs(t, err, local.ErrPasswordChanged)
		require.ErrorIs(t, store.SetPassword(t.Context(), "u1", before.Hash, "$argon2id$rehash", changed),
			local.ErrPasswordChanged)

		got, err := store.ByID(t.Context(), "u1")
		require.NoError(t, err)
		require.Equal(t, "$argon2id$first", got.Hash)

		_, err = store.ReplacePassword(t.Context(), "nobody", "x", "y", changed)
		require.ErrorIs(t, err, local.ErrNoAccount)
	})
}
