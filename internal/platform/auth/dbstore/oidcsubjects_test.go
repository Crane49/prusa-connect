package dbstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
)

// A second sign-in replaces the name and the address and keeps the first one's
// time, on both engines.
func TestASignInIsRecordedUnderTheDerivedID(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := dbstore.NewOIDCSubjects(db, e.Engine)
		require.NoError(t, err)

		first := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
		later := first.Add(48 * time.Hour)

		written := auth.OIDCSubject{
			Principal:   "oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3",
			Issuer:      "http://127.0.0.1:58190/application/o/prusa-connect",
			Subject:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DisplayName: "Ada Admin",
			Email:       "ada@example.com",
			FirstSeen:   first,
			LastSeen:    first,
		}
		require.NoError(t, store.Record(t.Context(), written))

		again := written
		again.DisplayName = "Ada Lovelace"
		again.Email = ""
		again.FirstSeen = later
		again.LastSeen = later
		require.NoError(t, store.Record(t.Context(), again))

		reopened, err := dbstore.NewOIDCSubjects(db, e.Engine)
		require.NoError(t, err)

		read, err := reopened.Get(t.Context(), written.Principal)
		require.NoError(t, err)

		want := again
		want.FirstSeen = first
		require.Equal(t, want, read)

		_, err = reopened.Get(t.Context(), "nobody")
		require.ErrorIs(t, err, auth.ErrNoOIDCSubject)
	})
}

func TestARecordWithoutItsKeysIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store, err := dbstore.NewOIDCSubjects(e.Open(t), e.Engine)
		require.NoError(t, err)

		now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)

		require.Error(t, store.Record(t.Context(), auth.OIDCSubject{Issuer: "https://i", Subject: "s",
			FirstSeen: now, LastSeen: now}))
		require.Error(t, store.Record(t.Context(), auth.OIDCSubject{Principal: "p", Subject: "s",
			FirstSeen: now, LastSeen: now}))
	})
}
