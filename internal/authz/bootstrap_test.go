package authz

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestBootstrapGivesTheFirstOwnerEveryResourceKind is what makes a fresh
// install usable. An empty graph grants nobody anything, and there is no
// authorized call able to write the grant that would fix that, so the first
// owner is written once.
func TestBootstrapGivesTheFirstOwnerEveryResourceKind(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		written, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.True(t, written)

		for _, kind := range ref.ConfigurableKinds() {
			for _, action := range []Action{
				ActionViewResource, ActionOperateResource, ActionConfigureResource,
				ActionReadStorage, ActionWriteStorage,
			} {
				allowed, canErr := store.Can(t.Context(), owner, action, kindOf(t, kind))
				require.NoError(t, canErr)
				require.Truef(t, allowed, "the first owner may not %s a %s", action, kind)
			}
		}
	})
}

// TestBootstrapWritesNothingIntoAGraphThatHasATuple is the whole safety of it.
// The emptiness is read once, inside the transaction that ends it; a graph an
// operator has already narrowed must not be widened again by a restart.
func TestBootstrapWritesNothingIntoAGraphThatHasATuple(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		// One tuple, and deliberately not one that reaches anything: a graph
		// somebody has begun writing is a graph that has been decided about.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:dana")))

		written, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, written, "a graph with a tuple in it has already been decided about")

		allowed, err := store.Can(t.Context(), owner, ActionViewResource, kindOf(t, ref.KindStorage))
		require.NoError(t, err)
		require.False(t, allowed, "the first grant was written into a graph that was not empty")
	})
}

// TestBootstrapIsIdempotent covers the restart: the second call sees the tuples
// the first one wrote and does nothing, so the epoch does not move and no
// cached decision anywhere is invalidated for a no-op.
func TestBootstrapIsIdempotent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		written, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.True(t, written)

		epoch := store.Epoch()

		written, err = store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, written)
		require.Equal(t, epoch, store.Epoch(), "a bootstrap that wrote nothing moved the epoch")
	})
}

// TestBootstrapWritesNoPrinterGrant keeps the first grant to what it is for. A
// printer is invisible until it is registered, and an owner tuple that reached
// every printer ever registered afterwards would be a standing grant nobody
// wrote.
func TestBootstrapWritesNoPrinterGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		_, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)

		require.NoError(t, store.UpsertDevice(t.Context(), "p-alpha", "Alpha"))

		allowed, err := store.Can(t.Context(), owner, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "the first grant reached a printer nobody placed in the org")
	})
}

// TestBootstrapRefusesASubjectTheVocabularyDoesNotTake fails before a
// transaction is opened, so a misconfigured core.principal is a startup failure
// with the key named rather than a half-written graph.
func TestBootstrapRefusesASubjectTheVocabularyDoesNotTake(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		_, err := store.Bootstrap(t.Context(), "local", subjectOf(t, "service_account:robot"))
		require.Error(t, err, "a service account is not a person and never holds an org role")

		tuples, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Empty(t, tuples, "a refused bootstrap left tuples behind")
	})
}

// TestResourcesReadsTheWholeDirectory is the raw read a reconciler needs: what
// the directory holds, not what one caller may see.
func TestResourcesReadsTheWholeDirectory(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		local := instanceOf(t, ref.KindStorage, "local")
		hook := instanceOf(t, ref.KindSink, "hook")

		require.NoError(t, store.RegisterResources(t.Context(), local, hook))

		registered, err := store.Resources(t.Context())
		require.NoError(t, err)
		require.Equal(t, []ref.Ref{hook, local}, registered,
			"the directory reads back in kind then id order, whoever holds a grant")

		require.NoError(t, store.RemoveResource(t.Context(), hook))

		registered, err = store.Resources(t.Context())
		require.NoError(t, err)
		require.Equal(t, []ref.Ref{local}, registered)
	})
}

// TestAFullRevocationDoesNotHandTheGraphBack is what the marker row is for.
// Revoking every grant is something an operator can do, one withdrawal at a
// time, through the grant API; a restart afterwards must leave the graph they
// emptied empty. Reading the tuple table instead would read a fully revoked
// graph as a fresh install and make the configured principal an admin again,
// which on a deployment that authenticates nobody is every anonymous request.
func TestAFullRevocationDoesNotHandTheGraphBack(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		written, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.True(t, written)

		held, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.NotEmpty(t, held)

		require.NoError(t, store.Delete(t.Context(), held...))

		emptied, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Empty(t, emptied, "the revocation left tuples behind, so the case below proves nothing")

		written, err = store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, written, "a restart rewrote the first grant into a graph somebody emptied")

		allowed, err := store.Can(t.Context(), owner, ActionViewResource, kindOf(t, ref.KindStorage))
		require.NoError(t, err)
		require.False(t, allowed, "the graph was handed back to the configured principal")
	})
}

// TestAChangedPrincipalBecomesTheAdmin is the recovery a changed core.principal
// promises. A first start in open mode writes the marker for the principal of
// that start; an operator who then names their subject at an issuer must be the
// administrator after the restart, and a second restart writes nothing more.
func TestAChangedPrincipalBecomesTheAdmin(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		first := subjectOf(t, "user:operator")
		next := subjectOf(t, "user:1d77e74b-a4f2-4085-9848-4c942da9e2f1")

		written, err := store.Bootstrap(t.Context(), "local", first)
		require.NoError(t, err)
		require.True(t, written)

		handed, err := store.HandOver(t.Context(), "local", first)
		require.NoError(t, err)
		require.False(t, handed, "the owner the marker names was handed the graph again")

		written, err = store.Bootstrap(t.Context(), "local", next)
		require.NoError(t, err)
		require.False(t, written)

		handed, err = store.HandOver(t.Context(), "local", next)
		require.NoError(t, err)
		require.True(t, handed)

		allowed, err := store.Can(t.Context(), next, ActionConfigureResource, kindOf(t, ref.KindStorage))
		require.NoError(t, err)
		require.True(t, allowed, "the changed principal does not reach what the org manages")

		epoch := store.Epoch()

		handed, err = store.HandOver(t.Context(), "local", next)
		require.NoError(t, err)
		require.False(t, handed, "a second restart with the same principal handed over again")
		require.Equal(t, epoch, store.Epoch(), "a handover that wrote nothing moved the epoch")
	})
}

// TestARestartAfterAFullRevocationHandsNothingBack keeps the handover from
// undoing what TestAFullRevocationDoesNotHandTheGraphBack holds: the same
// principal on the next start is not a change, so nothing is written. A
// different principal is, and gets the whole first grant.
func TestARestartAfterAFullRevocationHandsNothingBack(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		_, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)

		held, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.NoError(t, store.Delete(t.Context(), held...))

		handed, err := store.HandOver(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, handed)

		emptied, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Empty(t, emptied, "a restart wrote a grant into a graph somebody emptied")

		// A changed principal is the recovery, and an admin role alone would
		// reach nothing here: the org's edges over the kinds went too, and no
		// call can write them.
		successor := subjectOf(t, "user:successor")

		handed, err = store.HandOver(t.Context(), "local", successor)
		require.NoError(t, err)
		require.True(t, handed)

		allowed, err := store.Can(t.Context(), successor, ActionViewResource, kindOf(t, ref.KindStorage))
		require.NoError(t, err)
		require.True(t, allowed, "the handover wrote an admin role that reaches nothing")
	})
}

// TestADatabaseWrittenBeforeTheMarkerIsAdopted is the upgrade path. A graph
// that already holds grants and no marker was bootstrapped by an older build:
// the marker goes in so that a later full revocation is safe, and no grant is
// written, because the graph has already been decided about.
func TestADatabaseWrittenBeforeTheMarkerIsAdopted(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		owner := subjectOf(t, "user:operator")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "resource_kind:storage#manager@user:dana")))

		written, err := store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, written)

		held, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.NoError(t, store.Delete(t.Context(), held...))

		written, err = store.Bootstrap(t.Context(), "local", owner)
		require.NoError(t, err)
		require.False(t, written, "an adopted database was bootstrapped again once it was emptied")
	})
}

// TestTwoFirstStartsAgainstOneDatabaseBothSurvive is the deployment failure the
// transaction mode fixes. Bootstrap reads the graph before it writes, and a
// deferred transaction that upgrades to a write after another writer has
// committed is refused outright: busy_timeout does not cover that case, because
// retrying a stale snapshot can never succeed. Without the immediate lock, one
// of the two processes dies on SQLITE_BUSY and refuses to start.
//
// It opens handles the way the composition root does rather than through the
// engine harness, because the transaction mode is a property of that DSN. The
// schema is migrated once first: concurrent migration is a separate open item
// and would decide this case for the wrong reason.
func TestTwoFirstStartsAgainstOneDatabaseBothSurvive(t *testing.T) {
	t.Parallel()

	const starts = 8

	path := filepath.Join(t.TempDir(), "authz.db")
	owner := subjectOf(t, "user:operator")

	prepared := openGraphFile(t, path)
	require.NoError(t, migrateGraph(t.Context(), prepared))
	require.NoError(t, prepared.Reconcile(t.Context()))

	stores := make([]*Store, 0, starts)
	for range starts {
		stores = append(stores, openGraphFile(t, path))
	}

	var (
		start   sync.WaitGroup
		running sync.WaitGroup
		mu      sync.Mutex
		wrote   int
		errs    []error
	)

	start.Add(1)

	for _, store := range stores {
		running.Go(func() {
			start.Wait()

			written, err := store.Bootstrap(t.Context(), "local", owner)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errs = append(errs, err)

				return
			}

			if written {
				wrote++
			}
		})
	}

	start.Done()
	running.Wait()

	require.Empty(t, errs, "a concurrent first start refused to bring the process up")
	require.Equal(t, 1, wrote, "the first grant was written more than once")

	held, err := prepared.Tuples(t.Context())
	require.NoError(t, err)
	require.Len(t, held, len(ref.ConfigurableKinds())+1)
}

// TestConcurrentFirstStartsAgainstOneDatabaseAllBootstrapOnce is the same
// deployment failure on both engines rather than on the one whose transaction
// mode already covers it. SQLite holds a write lock for the whole transaction,
// so the loser reads the marker and stops. Postgres runs READ COMMITTED: every
// process reads no marker, every process inserts one, and the losers die on the
// unique violation, which openGraph turns into a refusal to start.
//
// A deployment reaches this by restarting under a supervisor that had not
// noticed the old process, or by running two replicas against one database.
//
// The schema is migrated once before the race. Concurrent migration is held by
// TestConcurrentFirstMigrationsAgainstOneDatabaseAllSurvive, and leaving it in
// here would decide this case for the other reason.
func TestConcurrentFirstStartsAgainstOneDatabaseAllBootstrapOnce(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		handles := e.Shared(t, concurrentStarts)
		owner := subjectOf(t, "user:operator")

		stores := make([]*Store, 0, len(handles))

		for _, db := range handles {
			store, err := Open(db, e.Dialect)
			require.NoError(t, err)

			stores = append(stores, store)
		}

		require.NoError(t, migrateGraph(t.Context(), stores[0]))
		require.NoError(t, stores[0].Reconcile(t.Context()))

		var (
			start   sync.WaitGroup
			running sync.WaitGroup
			mu      sync.Mutex
			wrote   int
			errs    []error
		)

		start.Add(1)

		for _, store := range stores {
			running.Go(func() {
				start.Wait()

				written, err := store.Bootstrap(t.Context(), "local", owner)

				mu.Lock()
				defer mu.Unlock()

				switch {
				case err != nil:
					errs = append(errs, err)
				case written:
					wrote++
				}
			})
		}

		start.Done()
		running.Wait()

		require.Empty(t, errs, "a concurrent first start refused to bring the process up")
		require.Equal(t, 1, wrote, "the first grant was written more than once")

		require.Equal(t, 1, countRows(t, stores[0], "authz_bootstrap"),
			"the graph carries more than one bootstrap marker")

		held, err := stores[0].Tuples(t.Context())
		require.NoError(t, err)
		require.Len(t, held, len(ref.ConfigurableKinds())+1)
	})
}

// openGraphFile opens the graph the way the composition root does, with the
// pragmas and the transaction mode that ship.
func openGraphFile(tb testing.TB, path string) *Store {
	tb.Helper()

	db, err := OpenSQLite(tb.Context(), path)
	require.NoError(tb, err)

	tb.Cleanup(func() { require.NoError(tb, db.Close()) })

	store, err := Open(db, DialectSQLite)
	require.NoError(tb, err)

	return store
}

// TestAHistoryProviderReadsAuditWithoutAdministeringAnything is ADR-20260901-b2z
// rule 34, and it is a seam: ADR-20260901-atx rule 12 gates the authorized frame of a
// durable command record on view_audit, and a provider that does not hold it
// stores nothing for that class, silently.
//
// Both halves. It reaches audit on every device the org holds, including one
// admitted after the grant was written, and it reaches administer on none of
// them: a provider is not the administrator of a fleet.
func TestAHistoryProviderReadsAuditWithoutAdministeringAnything(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		provider := subjectOf(t, "history:past")

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "org:acme#viewer@history:past")))

		for _, id := range []string{"p-alpha", "p-beta"} {
			device := deviceOf(t, id)

			//nolint:exhaustive // the device half; a provider holds no resource action
			for action, want := range map[Action]bool{
				ActionObserve:    true,
				ActionViewAudit:  true,
				ActionOperate:    false,
				ActionConfigure:  false,
				ActionAdminister: false,
			} {
				got, err := store.Can(t.Context(), provider, action, device)
				require.NoError(t, err)
				require.Equalf(t, want, got, "history:past %s on %s", action, id)
			}
		}

		// A device admitted after the grant was written is covered, because the
		// grant is relation-shaped over the org and never an enumerated list.
		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-later", Name: "Later"}))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-later#org@org:acme")))

		got, err := store.Can(t.Context(), provider, ActionViewAudit, deviceOf(t, "p-later"))
		require.NoError(t, err)
		require.True(t, got, "a device admitted after the grant is outside what the provider reads")

		// A provider with no grant still reads nothing: the pair is added to a
		// provider that already reaches the device and never on its own.
		got, err = store.Can(t.Context(), subjectOf(t, "history:ungranted"),
			ActionViewAudit, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, got, "a provider holding no grant reads audit")
	})
}

// countRows counts a table the tests read to check what a run left behind.
func countRows(t *testing.T, store *Store, table string) int {
	t.Helper()

	var count int

	// The table name is a literal from this file and never a caller's value.
	require.NoError(t, store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))

	return count
}
