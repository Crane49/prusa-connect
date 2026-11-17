package instanceid_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/instanceid"
)

var firstStart = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)

// TestTheIDIsWrittenOnceAndReadBackAfterARestart opens one database through
// two handles, one after the other, which is what a process that stopped and
// started again leaves behind. A store that kept the id in memory, or wrote a
// new one on each start, fails here.
func TestTheIDIsWrittenOnceAndReadBackAfterARestart(t *testing.T) {
	t.Parallel()

	authztest.ForEachEngine(t, func(t *testing.T, e authztest.Engine) {
		handles := e.OpenShared(t, 2)
		first := migrate(t, e, handles[0])
		id, err := instanceid.Ensure(t.Context(), first, e.Engine, firstStart)
		require.NoError(t, err)

		parsed, err := uuid.Parse(id)
		require.NoError(t, err, "the id is a UUID")
		require.Equal(t, uuid.Version(4), parsed.Version(), "the id is random")

		again, err := instanceid.Ensure(t.Context(), first, e.Engine, firstStart.Add(time.Hour))
		require.NoError(t, err)
		require.Equal(t, id, again, "a second read in one process")
		require.NoError(t, first.Close())

		second := migrate(t, e, handles[1])
		restarted, err := instanceid.Ensure(t.Context(), second, e.Engine, firstStart.Add(24*time.Hour))
		require.NoError(t, err)
		require.Equal(t, id, restarted, "the id after a restart")
	})
}

// TestTwoDatabasesHaveTwoIDs holds that the id is random, and not derived from
// anything two installs on one machine would share.
func TestTwoDatabasesHaveTwoIDs(t *testing.T) {
	t.Parallel()

	authztest.ForEachEngine(t, func(t *testing.T, e authztest.Engine) {
		one, err := instanceid.Ensure(t.Context(), e.Open(t), e.Engine, firstStart)
		require.NoError(t, err)

		other, err := instanceid.Ensure(t.Context(), e.Open(t), e.Engine, firstStart)
		require.NoError(t, err)

		require.NotEqual(t, one, other)
	}, instanceid.Migrations())
}

// migrate brings one handle to the schema the host migrates this package in:
// the graph's first, then this one's.
func migrate(t *testing.T, e authztest.Engine, db *sql.DB) *sql.DB {
	t.Helper()

	schema := sqlstore.Schema{Owners: []sqlstore.Migrations{authz.Migrations(), instanceid.Migrations()}}
	migrator, err := sqlstore.NewMigrator(db, e.Engine, schema, "")
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(t.Context()))

	return db
}
