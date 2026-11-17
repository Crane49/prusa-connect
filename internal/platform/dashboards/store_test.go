//go:build !nodashboards

package dashboards_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/dashboard"
	"github.com/prusa3d/prusa-connect/internal/platform/dashboards"
)

// The levels a caller reaches, as the composition root hands them down.
const (
	everyone  = 0
	operators = 1
	admins    = 2
)

func clock() time.Time { return time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC) }

func newStore(t *testing.T, e engine) *dashboards.Store {
	t.Helper()

	store, err := dashboards.New(e.Open(t), e.Engine, clock)
	require.NoError(t, err)

	return store
}

// made writes a dashboard of the name and visibility, with one card.
func made(t *testing.T, store *dashboards.Store, name string, visible dashboard.Visibility) dashboard.Dashboard {
	t.Helper()

	cards := []dashboard.Card{{
		ID: "p1", Type: "core/printer", X: 0, Y: 0, W: 4, H: 3,
		Config: json.RawMessage(`{"device":"mk4"}`),
	}}

	board, err := dashboard.New(dashboard.Draft{Name: name, Visibility: &visible, Cards: &cards})
	require.NoError(t, err)

	stored, err := store.Create(t.Context(), board, "user:alice")
	require.NoError(t, err)

	return stored
}

func TestADashboardIsStoredAsItWasWritten(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		board := made(t, store, "Farm", dashboard.VisibleToEveryone)

		require.Equal(t, int64(1), board.ID)
		require.Equal(t, int64(1), board.Revision)
		require.Equal(t, "user:alice", board.CreatedBy)
		require.Equal(t, clock(), board.Created)

		read, err := store.Get(t.Context(), board.ID, everyone)
		require.NoError(t, err)
		require.Equal(t, "Farm", read.Name)
		require.Equal(t, 12, read.Columns)
		require.Equal(t, dashboard.DensityComfortable, read.Density)
		require.Len(t, read.Cards, 1)
		require.JSONEq(t, `{"device":"mk4"}`, string(read.Cards[0].Config))
	})
}

func TestACallerSeesOnlyTheDashboardsTheirLevelReaches(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		made(t, store, "Wall", dashboard.VisibleToEveryone)
		ops := made(t, store, "Ops", dashboard.VisibleToOperators)
		secret := made(t, store, "Admin", dashboard.VisibleToAdmins)

		for _, step := range []struct {
			level int
			names []string
		}{
			{everyone, []string{"Wall"}},
			{operators, []string{"Ops", "Wall"}},
			{admins, []string{"Admin", "Ops", "Wall"}},
		} {
			rows, _, err := store.List(t.Context(), step.level)
			require.NoError(t, err)

			names := make([]string, 0, len(rows))
			for _, row := range rows {
				names = append(names, row.Name)
			}

			require.Equalf(t, step.names, names, "level %d", step.level)
		}

		// A dashboard past the caller's level is not there for them, on every
		// call, and a write refused that way changes nothing.
		_, err := store.Get(t.Context(), secret.ID, operators)
		require.ErrorIs(t, err, dashboard.ErrNotFound)

		_, err = store.Replace(t.Context(), ops, ops.Revision, everyone, "user:bob")
		require.ErrorIs(t, err, dashboard.ErrNotFound)

		_, err = store.Delete(t.Context(), secret.ID, operators)
		require.ErrorIs(t, err, dashboard.ErrNotFound)

		_, err = store.Get(t.Context(), secret.ID, admins)
		require.NoError(t, err)
	})
}

func TestAChangeMadeAgainstAnOlderRevisionIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		board := made(t, store, "Farm", dashboard.VisibleToEveryone)

		first := board
		first.Name = "Farm, first"

		saved, err := store.Replace(t.Context(), first, board.Revision, everyone, "user:bob")
		require.NoError(t, err)
		require.Equal(t, int64(2), saved.Revision)
		require.Equal(t, "user:bob", saved.ChangedBy)
		require.Equal(t, "user:alice", saved.CreatedBy)

		late := board
		late.Name = "Farm, late"

		_, err = store.Replace(t.Context(), late, board.Revision, everyone, "user:carol")

		var changed *dashboard.ChangedError
		require.ErrorAs(t, err, &changed)
		require.Equal(t, int64(2), changed.Revision)

		read, err := store.Get(t.Context(), board.ID, everyone)
		require.NoError(t, err)
		require.Equal(t, "Farm, first", read.Name)
	})
}

func TestAnIDIsNeverGivenOutTwice(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		first := made(t, store, "One", dashboard.VisibleToEveryone)
		second := made(t, store, "Two", dashboard.VisibleToEveryone)

		_, err := store.Delete(t.Context(), second.ID, everyone)
		require.NoError(t, err)

		third := made(t, store, "Three", dashboard.VisibleToEveryone)
		require.Equal(t, first.ID+2, third.ID)
	})
}

func TestOneDashboardIsTheDefaultAndOnlyOneEveryoneSees(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		wall := made(t, store, "Wall", dashboard.VisibleToEveryone)
		farm := made(t, store, "Farm", dashboard.VisibleToEveryone)
		ops := made(t, store, "Ops", dashboard.VisibleToOperators)

		_, err := store.SetDefault(t.Context(), &wall.ID, "user:root")
		require.NoError(t, err)
		_, err = store.SetDefault(t.Context(), &farm.ID, "user:root")
		require.NoError(t, err)

		rows, listed, err := store.List(t.Context(), everyone)
		require.NoError(t, err)
		require.Equal(t, farm.ID, *listed)

		// Choosing the default again changes nothing: no revision moves.
		before, err := store.Get(t.Context(), farm.ID, everyone)
		require.NoError(t, err)

		was, err := store.SetDefault(t.Context(), &farm.ID, "user:root")
		require.NoError(t, err)
		require.Equal(t, farm.ID, *was)

		after, err := store.Get(t.Context(), farm.ID, everyone)
		require.NoError(t, err)
		require.Equal(t, before.Revision, after.Revision)

		defaults := 0
		for _, row := range rows {
			if row.IsDefault {
				defaults++
			}
		}

		require.Equal(t, 1, defaults)

		var invalid *dashboard.InvalidError
		_, err = store.SetDefault(t.Context(), &ops.ID, "user:root")
		require.ErrorAs(t, err, &invalid)

		missing := int64(99)
		_, err = store.SetDefault(t.Context(), &missing, "user:root")
		require.ErrorIs(t, err, dashboard.ErrNotFound)

		// A deleted default leaves no default behind.
		_, err = store.Delete(t.Context(), farm.ID, everyone)
		require.NoError(t, err)
		_, held, err := store.List(t.Context(), everyone)
		require.NoError(t, err)
		require.Nil(t, held)

		_, err = store.SetDefault(t.Context(), &wall.ID, "user:root")
		require.NoError(t, err)
		_, err = store.SetDefault(t.Context(), nil, "user:root")
		require.NoError(t, err)
		_, held, err = store.List(t.Context(), everyone)
		require.NoError(t, err)
		require.Nil(t, held)
	})
}

func TestTheServerKeepsAtMostItsLimitOfDashboards(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		for range dashboard.MaxDashboards {
			made(t, store, "Board", dashboard.VisibleToEveryone)
		}

		board, err := dashboard.New(dashboard.Draft{Name: "One more"})
		require.NoError(t, err)

		_, err = store.Create(t.Context(), board, "user:alice")
		require.ErrorIs(t, err, dashboard.ErrFull)
	})
}

func TestTheListIsByNameWhateverTheCase(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		made(t, store, "beta", dashboard.VisibleToEveryone)
		made(t, store, "Alpha", dashboard.VisibleToEveryone)
		made(t, store, "alpha", dashboard.VisibleToEveryone)

		rows, _, err := store.List(t.Context(), everyone)
		require.NoError(t, err)
		require.Equal(t, "Alpha", rows[0].Name)
		require.Equal(t, "alpha", rows[1].Name)
		require.Equal(t, "beta", rows[2].Name)
		require.Equal(t, 1, rows[0].Cards)
	})
}

func TestTheDefaultStaysVisibleToEveryone(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		wall := made(t, store, "Wall", dashboard.VisibleToEveryone)
		_, err := store.SetDefault(t.Context(), &wall.ID, "user:root")
		require.NoError(t, err)

		read, err := store.Get(t.Context(), wall.ID, admins)
		require.NoError(t, err)

		narrowed := read
		narrowed.Visibility = dashboard.VisibleToAdmins

		_, err = store.Replace(t.Context(), narrowed, read.Revision, admins, "user:root")

		var invalid *dashboard.InvalidError
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, "body.visibility", invalid.Location)

		renamed := read
		renamed.Name = "Wall, renamed"

		saved, err := store.Replace(t.Context(), renamed, read.Revision, admins, "user:root")
		require.NoError(t, err)
		require.True(t, saved.IsDefault)
	})
}

// TestTheLimitHoldsUnderWritersRacingForTheLastPlace is the cap with many
// writers at once. Each takes the sequence's lock before it counts, so one of
// them gets the last place and the rest are refused, on both engines.
func TestTheLimitHoldsUnderWritersRacingForTheLastPlace(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		for range dashboard.MaxDashboards - 1 {
			made(t, store, "Board", dashboard.VisibleToEveryone)
		}

		board, err := dashboard.New(dashboard.Draft{Name: "Last"})
		require.NoError(t, err)

		const writers = 8

		var (
			wait    sync.WaitGroup
			mu      sync.Mutex
			written int
			refused int
		)

		for range writers {
			wait.Go(func() {
				_, createErr := store.Create(t.Context(), board, "user:alice")

				mu.Lock()
				defer mu.Unlock()

				switch {
				case createErr == nil:
					written++
				case errors.Is(createErr, dashboard.ErrFull):
					refused++
				default:
					t.Errorf("a racing write failed: %v", createErr)
				}
			})
		}

		wait.Wait()

		require.Equal(t, 1, written)
		require.Equal(t, writers-1, refused)

		rows, _, err := store.List(t.Context(), admins)
		require.NoError(t, err)
		require.Len(t, rows, dashboard.MaxDashboards)
	})
}

// TestChoosingTheDefaultAtOnceLeavesOneDefault is two people choosing the
// home page at the same moment. Each choice takes the lock first, so every one
// succeeds in turn and one default is left, where the index allowing one
// default would otherwise refuse the second as a failure.
func TestChoosingTheDefaultAtOnceLeavesOneDefault(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		const writers = 8

		ids := make([]int64, writers)
		for i := range ids {
			ids[i] = made(t, store, "Wall", dashboard.VisibleToEveryone).ID
		}

		var wait sync.WaitGroup

		for _, id := range ids {
			wait.Go(func() {
				if _, err := store.SetDefault(t.Context(), &id, "user:root"); err != nil {
					t.Errorf("a racing choice failed: %v", err)
				}
			})
		}

		wait.Wait()

		rows, held, err := store.List(t.Context(), everyone)
		require.NoError(t, err)
		require.NotNil(t, held)

		defaults := 0

		for _, row := range rows {
			if row.IsDefault {
				defaults++
			}
		}

		require.Equal(t, 1, defaults)
	})
}

// TestAWriteAnswersWhatItSawUnderItsOwnLock is what the announcements are
// addressed by: a delete answers who saw the dashboard it removed, and a
// choice of default answers the default it replaced.
func TestAWriteAnswersWhatItSawUnderItsOwnLock(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		wall := made(t, store, "Wall", dashboard.VisibleToEveryone)
		farm := made(t, store, "Farm", dashboard.VisibleToEveryone)
		secret := made(t, store, "Admin", dashboard.VisibleToAdmins)

		was, err := store.SetDefault(t.Context(), &wall.ID, "user:root")
		require.NoError(t, err)
		require.Nil(t, was)

		was, err = store.SetDefault(t.Context(), &farm.ID, "user:root")
		require.NoError(t, err)
		require.Equal(t, wall.ID, *was)

		saw, err := store.Delete(t.Context(), secret.ID, admins)
		require.NoError(t, err)
		require.Equal(t, dashboard.VisibleToAdmins, saw)
	})
}
