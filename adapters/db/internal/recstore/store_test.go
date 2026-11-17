package recstore_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/recstore"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// The two namespaces every containment case uses. They are what
// extension.NamespaceOf renders for two module paths, so the strings here are
// the strings a deployment holds.
const (
	oneNamespace   = "com.github.aux.acme.jobs"
	otherNamespace = "com.github.aux.acme.other"
)

// TestARecordIsKeptAndReadBack is the whole of what a module author is
// promised, on both engines: a value survives the write, its version rises, a
// prefix lists sorted keys and no values, and a delete leaves nothing.
func TestARecordIsKeptAndReadBack(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		// Nothing is there, and that is ErrNoRecord rather than an empty value.
		_, err = records.Get(t.Context(), "spools/petg")
		require.ErrorIs(t, err, stateapi.ErrNoRecord)

		version, err := records.Put(t.Context(), "spools/petg", []byte(`{"grams":840}`))
		require.NoError(t, err)
		require.Equal(t, uint64(1), version)

		held, err := records.Get(t.Context(), "spools/petg")
		require.NoError(t, err)
		require.Equal(t, `{"grams":840}`, string(held.Bytes))
		require.Equal(t, uint64(1), held.Version)

		// A second write to one key is a new version of it, never a second row.
		version, err = records.Put(t.Context(), "spools/petg", []byte(`{"grams":700}`))
		require.NoError(t, err)
		require.Equal(t, uint64(2), version)

		held, err = records.Get(t.Context(), "spools/petg")
		require.NoError(t, err)
		require.Equal(t, `{"grams":700}`, string(held.Bytes))
		require.Equal(t, uint64(2), held.Version)

		at, err := records.Version(t.Context(), "spools/petg")
		require.NoError(t, err)
		require.Equal(t, uint64(2), at)

		// Sorted, prefixed, and keys only.
		for _, key := range []stateapi.Key{"spools/pla", "spools/abs", "jobs/17"} {
			_, err = records.Put(t.Context(), key, []byte("x"))
			require.NoError(t, err)
		}

		under, err := records.List(t.Context(), "spools/")
		require.NoError(t, err)
		require.Equal(t, []stateapi.Key{"spools/abs", "spools/petg", "spools/pla"}, under)

		every, err := records.List(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, every, 4)

		require.NoError(t, records.Delete(t.Context(), "spools/petg"))

		_, err = records.Get(t.Context(), "spools/petg")
		require.ErrorIs(t, err, stateapi.ErrNoRecord)
		require.ErrorIs(t, records.Delete(t.Context(), "spools/petg"), stateapi.ErrNoRecord)

		// A zero-byte value is a record, and reads back as one rather than as
		// nothing. A flag key is the shape that needs it.
		_, err = records.Put(t.Context(), "flags/seeded", nil)
		require.NoError(t, err)

		flag, err := records.Get(t.Context(), "flags/seeded")
		require.NoError(t, err)
		require.Empty(t, flag.Bytes)
		require.Equal(t, uint64(1), flag.Version)
	})
}

// TestOneNamespaceReachesNoOtherNamespacesRecords is ADR-20260827-rwq rule 2's
// containment, attacked rather than asserted.
//
// The three attacks are the three shapes a key could take that a reader might
// expect to escape: another namespace spelled into the key, a relative path out
// of it, and a LIKE wildcard that would list the store. None of them is a
// permission this door refuses. Each one is a key, stored under the caller's own
// namespace or refused outright, because the namespace is not in the key at all.
func TestOneNamespaceReachesNoOtherNamespacesRecords(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		mine, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		theirs, err := store.Records(otherNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		_, err = theirs.Put(t.Context(), "secret", []byte("the other module's"))
		require.NoError(t, err)

		// Naming their key gets my namespace's absence, not their value.
		_, err = mine.Get(t.Context(), "secret")
		require.ErrorIs(t, err, stateapi.ErrNoRecord)

		for _, escape := range []stateapi.Key{
			stateapi.Key(otherNamespace + "/secret"),
			"../" + otherNamespace + "/secret",
			"..",
		} {
			_, err = mine.Get(t.Context(), escape)
			require.ErrorIs(t, err, stateapi.ErrNoRecord, "%q read something", escape)

			// And writing it puts a record in my own namespace, where it is
			// mine and invisible to them.
			_, err = mine.Put(t.Context(), escape, []byte("mine"))
			require.NoError(t, err)

			_, err = theirs.Get(t.Context(), escape)
			require.ErrorIs(t, err, stateapi.ErrNoRecord, "%q crossed into the other namespace", escape)
		}

		// A wildcard prefix lists nothing but my own literal keys. Without the
		// escaping in likePrefix this would answer with every key I hold.
		_, err = mine.Put(t.Context(), "a%b", []byte("literal"))
		require.NoError(t, err)

		wild, err := mine.List(t.Context(), "a%")
		require.NoError(t, err)
		require.Equal(t, []stateapi.Key{"a%b"}, wild)

		// Their namespace still holds exactly what they wrote, and my four
		// escape attempts are all in mine.
		theirKeys, err := theirs.List(t.Context(), "")
		require.NoError(t, err)
		require.Equal(t, []stateapi.Key{"secret"}, theirKeys)

		myKeys, err := mine.List(t.Context(), "")
		require.NoError(t, err)
		require.Len(t, myKeys, 4)

		// The store's own operator view agrees: two namespaces, and each one
		// holds what its own door wrote.
		namespaces, err := store.Namespaces(t.Context())
		require.NoError(t, err)
		require.Len(t, namespaces, 2)
		require.Equal(t, oneNamespace, namespaces[0].Name)
		require.Equal(t, int64(4), namespaces[0].Keys)
		require.Equal(t, otherNamespace, namespaces[1].Name)
		require.Equal(t, int64(1), namespaces[1].Keys)
	})
}

// TestAKeyOutsideTheGrammarIsRefused is the bound on a key, which is what stops
// a namespace being filled with one enormous name and what keeps a key
// printable in an operator surface.
func TestAKeyOutsideTheGrammarIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		refused := map[string]stateapi.Key{
			"empty":            "",
			"over the length":  stateapi.Key(strings.Repeat("k", stateapi.MaxKeyBytes+1)),
			"a newline":        "spools\nrogue",
			"a null":           "spools\x00rogue",
			"a leading slash":  "/spools",
			"a trailing slash": "spools/",
		}

		for name, key := range refused {
			t.Run(name, func(t *testing.T) {
				_, put := records.Put(t.Context(), key, []byte("x"))
				require.ErrorIs(t, put, stateapi.ErrKey)

				_, read := records.Get(t.Context(), key)
				require.ErrorIs(t, read, stateapi.ErrKey)
			})
		}

		// The limit itself is reached and not refused, so the bound is exact
		// rather than approximately right.
		at := stateapi.Key(strings.Repeat("k", stateapi.MaxKeyBytes))
		_, err = records.Put(t.Context(), at, []byte("x"))
		require.NoError(t, err)
	})
}

// TestANamespaceCannotBeFilledPastItsQuota is ADR-20260827-wgc rule 12's byte limit,
// which is what stops a module filling the host's disk.
//
// It writes real values through the real store until the namespace refuses, then
// reads three things back: the refusal names the limit, the namespace is at or
// under it, and deleting a record makes room again. A quota that refused and
// then stayed refused for ever would be a module that has to be reinstalled to
// keep working.
func TestANamespaceCannotBeFilledPastItsQuota(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		value := make([]byte, stateapi.MaxValueBytes)
		for i := range value {
			value[i] = byte('a' + i%26)
		}

		var (
			written int
			refusal error
		)

		// One more attempt than the quota can hold, so the refusal is the
		// store's and never the loop running out.
		for i := range stateapi.DefaultNamespaceBytes/stateapi.MaxValueBytes + 2 {
			key := stateapi.Key(fmt.Sprintf("bulk/%04d", i))

			if _, err = records.Put(t.Context(), key, value); err != nil {
				refusal = err

				break
			}

			written++
		}

		require.ErrorIs(t, refusal, stateapi.ErrNamespaceFull)
		require.Contains(t, refusal.Error(), strconv.Itoa(stateapi.DefaultNamespaceBytes),
			"the refusal does not name the limit, so an author cannot read it")
		require.Positive(t, written)

		held, err := store.Namespaces(t.Context())
		require.NoError(t, err)
		require.Len(t, held, 1)
		require.LessOrEqual(t, held[0].Bytes, int64(stateapi.DefaultNamespaceBytes),
			"the namespace went over its own limit")

		// A rewrite of a key already there is not a new allocation, so a full
		// namespace can still replace what it holds.
		_, err = records.Put(t.Context(), "bulk/0000", value)
		require.NoError(t, err, "a full namespace could not rewrite a value it already held")

		// And a delete makes room for the write that was refused.
		require.NoError(t, records.Delete(t.Context(), "bulk/0001"))

		_, err = records.Put(t.Context(), stateapi.Key(fmt.Sprintf("bulk/%04d", written)), value)
		require.NoError(t, err, "deleting a record did not make room")

		// One namespace's quota is its own. The neighbour is empty and writes.
		theirs, err := store.Records(otherNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		_, err = theirs.Put(t.Context(), "first", value)
		require.NoError(t, err, "one namespace's quota refused another namespace's first write")
	})
}

// TestAValueOverTheLimitIsRefused is the per-record bound, checked at the limit
// and one byte past it.
func TestAValueOverTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		_, err = records.Put(t.Context(), "at", make([]byte, stateapi.MaxValueBytes))
		require.NoError(t, err)

		_, err = records.Put(t.Context(), "over", make([]byte, stateapi.MaxValueBytes+1))
		require.ErrorIs(t, err, stateapi.ErrValueTooLarge)

		_, err = records.Get(t.Context(), "over")
		require.ErrorIs(t, err, stateapi.ErrNoRecord)
	})
}

// TestTwoWritersRacingOneKeyLeaveOneWinner is what CompareAndPut is for: two
// instances of one module, or a restart racing a write.
//
// Every writer reads the same version and writes against it. Exactly one may
// win, and the others have to be told they lost rather than silently overwriting
// each other, which is what Put would do.
func TestTwoWritersRacingOneKeyLeaveOneWinner(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		// The create race first: every writer expects no record, and the key is
		// created once.
		const writers = 8

		var (
			ready   sync.WaitGroup
			start   = make(chan struct{})
			won     sync.Map
			refused sync.Map
		)

		ready.Add(writers)

		results := make(chan error, writers)

		for i := range writers {
			go func() {
				ready.Done()
				<-start

				_, putErr := records.CompareAndPut(context.Background(), "leader",
					stateapi.AbsentVersion, fmt.Appendf(nil, "writer-%d", i))

				switch {
				case putErr == nil:
					won.Store(i, true)
				case errors.Is(putErr, stateapi.ErrVersion):
					refused.Store(i, true)
				}

				results <- putErr
			}()
		}

		ready.Wait()
		close(start)

		for range writers {
			err = <-results
			if err != nil {
				require.ErrorIs(t, err, stateapi.ErrVersion)
			}
		}

		require.Equal(t, 1, countOf(&won), "a create race did not settle on one winner")
		require.Equal(t, writers-1, countOf(&refused), "a loser was not told it lost")

		created, err := records.Get(t.Context(), "leader")
		require.NoError(t, err)
		require.Equal(t, uint64(1), created.Version)

		// The update race second: every writer reads the same version and
		// writes against it, and one write lands.
		held, err := records.Get(t.Context(), "leader")
		require.NoError(t, err)

		var (
			second  sync.WaitGroup
			go2     = make(chan struct{})
			landed  sync.Map
			blocked sync.Map
		)

		second.Add(writers)

		updates := make(chan error, writers)

		for i := range writers {
			go func() {
				second.Done()
				<-go2

				_, putErr := records.CompareAndPut(context.Background(), "leader",
					held.Version, fmt.Appendf(nil, "update-%d", i))

				switch {
				case putErr == nil:
					landed.Store(i, true)
				case errors.Is(putErr, stateapi.ErrVersion):
					blocked.Store(i, true)
				}

				updates <- putErr
			}()
		}

		second.Wait()
		close(go2)

		for range writers {
			err = <-updates
			if err != nil {
				require.ErrorIs(t, err, stateapi.ErrVersion)
			}
		}

		require.Equal(t, 1, countOf(&landed), "an update race did not settle on one winner")
		require.Equal(t, writers-1, countOf(&blocked), "a loser was not told it lost")

		final, err := records.Get(t.Context(), "leader")
		require.NoError(t, err)
		require.Equal(t, uint64(2), final.Version,
			"the version moved by more than the one write that won")
		require.True(t, strings.HasPrefix(string(final.Bytes), "update-"))

		// A stale expectation is refused for ever, not retried into a win.
		_, err = records.CompareAndPut(t.Context(), "leader", held.Version, []byte("late"))
		require.ErrorIs(t, err, stateapi.ErrVersion)

		// And a key that is not there refuses an update-shaped expectation.
		_, err = records.CompareAndPut(t.Context(), "absent", 3, []byte("x"))
		require.ErrorIs(t, err, stateapi.ErrVersion)
	})
}

// countOf is how many keys a sync.Map holds.
func countOf(held *sync.Map) int {
	var count int

	held.Range(func(any, any) bool {
		count++

		return true
	})

	return count
}

// TestMigrateIsSafeToRunTwiceAndRefusesAnEditedFile is the migration half. A
// second Migrate on a database that already has the schema does nothing, and a
// ledger that disagrees with the file this build ships is refused rather than
// reapplied.
func TestMigrateIsSafeToRunTwiceAndRefusesAnEditedFile(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		db := e.Open(t)

		store, err := recstore.New(db, e.Dialect)
		require.NoError(t, err)

		require.NoError(t, store.Migrate(t.Context()))
		require.NoError(t, store.Migrate(t.Context()))

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		_, err = records.Put(t.Context(), "survives", []byte("yes"))
		require.NoError(t, err)

		require.NoError(t, store.Migrate(t.Context()))

		held, err := records.Get(t.Context(), "survives")
		require.NoError(t, err)
		require.Equal(t, "yes", string(held.Bytes))

		// The ledger recorded what ran, read with a statement of its own rather
		// than through the migrator that wrote it.
		var applied int
		require.NoError(t, db.QueryRowContext(t.Context(),
			"SELECT count(*) FROM module_state_migration").Scan(&applied))
		require.Positive(t, applied)

		// A ledger row whose checksum is not this build's is the shape of a
		// migration edited after it shipped.
		forged := "UPDATE module_state_migration SET checksum = 'forged'"
		_, err = db.ExecContext(t.Context(), forged)
		require.NoError(t, err)

		require.ErrorIs(t, store.Migrate(t.Context()), recstore.ErrMigrationChanged)
	})
}

// TestAStoreIsOpenedOnAHandleAndANamespace is the two refusals the constructor
// owes. Both are host defects: a module names neither.
func TestAStoreIsOpenedOnAHandleAndANamespace(t *testing.T) {
	t.Parallel()

	_, err := recstore.New(nil, recstore.DialectSQLite)
	require.Error(t, err)

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		_, opened := store.Records("   ", stateapi.DefaultNamespaceBytes)
		require.ErrorIs(t, opened, recstore.ErrNamespace)

		// A limit of nothing, and one past what an operator can grant, are
		// refused where the door is made rather than on the first write.
		for _, limit := range []int64{0, -1, stateapi.MaxGrantedNamespaceBytes + 1} {
			_, opened = store.Records(oneNamespace, limit)
			require.ErrorIs(t, opened, recstore.ErrNamespace, "a limit of %d was taken", limit)
		}
	})
}

// TestANamespaceIsHeldToTheLimitItWasGranted is ADR-20261007-br4 on both engines: a
// namespace the operator granted more holds more than the default, Usage
// answers what it holds against that limit, and the same namespace opened at
// the default is refused the next write. The limit is the door's and not the
// rows', so lowering a grant takes effect at once and deletes nothing.
func TestANamespaceIsHeldToTheLimitItWasGranted(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		const granted = 2 * stateapi.DefaultNamespaceBytes

		records, err := store.Records(oneNamespace, granted)
		require.NoError(t, err)

		value := make([]byte, stateapi.MaxValueBytes)

		// Past the default and well inside the grant.
		past := stateapi.DefaultNamespaceBytes/stateapi.MaxValueBytes + 4
		for i := range past {
			_, err = records.Put(t.Context(), stateapi.Key(fmt.Sprintf("bulk/%04d", i)), value)
			require.NoError(t, err, "write %d of a granted namespace was refused", i)
		}

		usage, err := records.Usage(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(granted), usage.Limit)
		require.Greater(t, usage.Bytes, int64(stateapi.DefaultNamespaceBytes))

		held, err := store.Namespaces(t.Context())
		require.NoError(t, err)
		require.Len(t, held, 1)
		require.Equal(t, held[0].Bytes, usage.Bytes, "Usage and the accounting disagree")

		// The same rows, opened at the default: nothing is lost, and nothing
		// new fits.
		narrowed, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		_, err = narrowed.Get(t.Context(), "bulk/0000")
		require.NoError(t, err)

		_, err = narrowed.Put(t.Context(), "one/more", []byte("x"))
		require.ErrorIs(t, err, stateapi.ErrNamespaceFull)
		require.Contains(t, err.Error(), strconv.Itoa(stateapi.DefaultNamespaceBytes))

		usage, err = narrowed.Usage(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(stateapi.DefaultNamespaceBytes), usage.Limit)
	})
}

// TestListAnswersTheSameOrderOnBothEngines pins the half of ADR-20260827-wgc rule 3
// that a single-engine test cannot see. List "returns keys in sorted order",
// which makes the order a contract, and a contract that two engines answer
// differently is not one.
//
// The keys are chosen so that a locale collation and a byte collation disagree.
// A locale collation orders by letter first and treats case and punctuation as
// tie-breakers, so '/' and '-' stop separating the levels of a hierarchical key
// and 'A' stops sorting before 'a'. Measured against the project's own Postgres
// fixture, whose database is en_US.utf8, before the columns were pinned:
//
//	SQLite (BINARY):   A/b, a-b, a/b, ab
//	Postgres (locale): a-b, a/b, ab, A/b
//
// The expected order is written out rather than computed with slices.Sort, so
// this compares the engine against a stated answer and not against Go's own
// idea of it. Go's string comparison is byte-wise, which is the answer under
// test, and deriving the expectation from it would let both engines drift
// together on a day the store started sorting somewhere else.
func TestListAnswersTheSameOrderOnBothEngines(t *testing.T) {
	t.Parallel()

	// Written in an order that is neither engine's answer, so a store that
	// returned insertion order fails here too.
	written := []stateapi.Key{"ab", "a/b", "A/b", "a-b"}

	// Byte order, which is what COLLATE BINARY and COLLATE "C" both give.
	want := []stateapi.Key{"A/b", "a-b", "a/b", "ab"}

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		records, err := store.Records(oneNamespace, stateapi.DefaultNamespaceBytes)
		require.NoError(t, err)

		for _, key := range written {
			_, err = records.Put(t.Context(), key, []byte("v"))
			require.NoError(t, err)
		}

		listed, err := records.List(t.Context(), "")
		require.NoError(t, err)
		require.Equal(t, want, listed,
			"the keys came back in this engine's collation rather than in byte order")
	})
}

// TestADatabaseAnotherBuildMadeIsRefused is the two refusals a database this
// build did not migrate gets: a ledger row naming a file this build does not
// ship, and a table of this store that exists with no ledger row creating it.
// Neither is written to.
func TestADatabaseAnotherBuildMadeIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		migrated := e.Open(t)

		store, err := recstore.New(migrated, e.Dialect)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(t.Context()))

		_, err = migrated.ExecContext(t.Context(),
			"UPDATE module_state_migration SET name = '0001_renamed_after_shipping.sql'")
		require.NoError(t, err)
		require.ErrorIs(t, store.Migrate(t.Context()), recstore.ErrMigrationMissing)

		foreign := e.Open(t)

		_, err = foreign.ExecContext(t.Context(), "CREATE TABLE module_state (ref TEXT)")
		require.NoError(t, err)

		store, err = recstore.New(foreign, e.Dialect)
		require.NoError(t, err)

		err = store.Migrate(t.Context())
		require.ErrorIs(t, err, recstore.ErrUnrecordedTable)
		require.ErrorContains(t, err, "module_state")
	})
}
