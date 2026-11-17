package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// A binary whose vocabulary changed reads a projection compiled under the
// previous one. Reconcile runs at every start and is the moment the new
// vocabulary reaches the database, so it recompiles: without that, an action
// that moved between role levels reaches nobody until the deployment's next
// grant write, and the deployment has no reason to make one.
//
// The projection is emptied out of band rather than compiled under a narrowed
// vocabulary, because both produce the same thing to prove: rows that disagree
// with the tuples, which only a recompile repairs. Every other path that could
// repair it is a mutation, and this test performs none.
func TestReconcileRecompilesAProjectionThatDisagreesWithTheTuples(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		root := subjectOf(t, "user:root")
		beta := deviceOf(t, "p-beta")

		allowed, err := store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.True(t, allowed, "the fixture's org admin cannot provision, so this test proves nothing")

		_, err = store.db.ExecContext(t.Context(), store.q(truncateAccessSQL))
		require.NoError(t, err)

		allowed, err = store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.False(t, allowed, "the projection was emptied and a decision still found a row")

		require.NoError(t, store.Reconcile(t.Context()))

		allowed, err = store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.True(t, allowed,
			"Reconcile left the projection as it found it, so an upgrade that moves an action "+
				"between role levels reaches nobody until the next grant write")
	})
}

// The refusal half of the case above, and the half with the consequence. An
// operator who revokes access by deleting tuples with a SQL client leaves the
// projection holding rows the tuples no longer compile to, and every surface
// reads the projection rather than the tuples. So the revoked principal keeps
// what was taken from them until the deployment's next grant write, which
// nothing obliges it to make.
//
// Both projections are asserted. They are two tables truncated by two
// statements, so a recompile that repaired one and not the other would pass a
// case that read either alone.
func TestReconcileNarrowsAProjectionWhoseTuplesWereDeletedOutOfBand(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		root := subjectOf(t, "user:root")
		beta := deviceOf(t, "p-beta")
		local := instanceOf(t, ref.KindStorage, "local")

		allowed, err := store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.True(t, allowed, "the fixture's org admin cannot provision, so this test proves nothing")

		allowed, err = store.CanResource(t.Context(), root, ActionViewResource, local)
		require.NoError(t, err)
		require.True(t, allowed, "the fixture's org admin cannot view storage:local, so this test proves nothing")

		_, err = store.db.ExecContext(t.Context(), "DELETE FROM authz_tuple")
		require.NoError(t, err)

		allowed, err = store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.True(t, allowed,
			"every tuple is gone and the stale projection no longer answers, so this test proves nothing")

		require.NoError(t, store.Reconcile(t.Context()))

		allowed, err = store.Can(t.Context(), root, ActionConfigure, beta)
		require.NoError(t, err)
		require.False(t, allowed,
			"every tuple was deleted and the device projection still grants provision, "+
				"so an out-of-band revocation does not take effect at the next start")

		allowed, err = store.CanResource(t.Context(), root, ActionViewResource, local)
		require.NoError(t, err)
		require.False(t, allowed,
			"every tuple was deleted and the resource projection still grants a read, "+
				"so an out-of-band revocation does not take effect at the next start")
	})
}
