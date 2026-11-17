package authz

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// retiredShape is the shape the retirement cases drop from the vocabulary. A
// peer node's view of a printer is a real grant with real tuples, so removing
// it is the shape of the change that has to be refused rather than applied.
var retiredShape = relationShape{
	ObjectType: TypeDevice, Relation: RelViewer, SubjectType: TypeNode,
}

// TestReconcileWritesTheVocabulary is the parity case: what the lookup tables
// hold after a start is what the Go vocabulary declares, nothing more. It fails
// when someone adds a const and forgets relationSpecs, which is the drift the
// CHECK constraint this replaced could only catch by being edited by hand.
func TestReconcileWritesTheVocabulary(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		types, err := scanNames(t.Context(), store.db, store.dialect, selectTypesSQL)
		require.NoError(t, err)
		require.ElementsMatch(t, vocabularyNames(Types()), types)

		actions, err := scanNames(t.Context(), store.db, store.dialect, selectActionsSQL)
		require.NoError(t, err)
		require.ElementsMatch(t, vocabularyNames(Actions()), actions)

		shapes, err := store.storedShapes(t.Context(), store.db)
		require.NoError(t, err)
		require.ElementsMatch(t, expandShapes(relationSpecs), shapes)

		// The count spelled out by hand, so a spec list that silently grows or
		// shrinks is visible here too.
		require.Len(t, shapes, 89)
	})
}

// TestReconcileIsIdempotent covers the ordinary restart: every start reconciles,
// and a start that has nothing to do must change nothing.
func TestReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Reconcile(t.Context()))

		shapes, err := store.storedShapes(t.Context(), store.db)
		require.NoError(t, err)
		require.ElementsMatch(t, expandShapes(relationSpecs), shapes)

		stored, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Len(t, stored, len(fixtureTuples))
	})
}

// TestReconcileRetiresAnUnusedShape is the cheap half of a vocabulary change: a
// shape nothing depends on leaves on the next start, with no DDL, and the
// database stops accepting it from that moment.
func TestReconcileRetiresAnUnusedShape(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		narrowed := withoutShape(relationSpecs, retiredShape)

		require.NoError(t, store.reconcile(t.Context(), Types(), Actions(), narrowed))

		shapes, err := store.storedShapes(t.Context(), store.db)
		require.NoError(t, err)
		require.NotContains(t, shapes, retiredShape)
		require.ElementsMatch(t, expandShapes(narrowed), shapes)

		_, err = store.db.ExecContext(t.Context(), store.q(insertTupleSQL),
			"device", "p-alpha", "viewer", "node", "site-b", "")
		require.Error(t, err, "the database still accepts a shape the vocabulary dropped")
	})
}

// TestReconcileRefusesToRetireAShapeInUse is the refusal the whole design turns
// on. Narrowing the vocabulary under tuples that still name the dropped shape
// would either delete authorization data or leave the database disagreeing with
// the code about what an edge means, so the start stops instead, and says which
// shape and how many rows.
func TestReconcileRefusesToRetireAShapeInUse(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		narrowed := withoutShape(relationSpecs, retiredShape)

		err := store.reconcile(t.Context(), Types(), Actions(), narrowed)
		requireCode(t, err, CodeInvalidStoredTuple())
		require.Contains(t, err.Error(), retiredShape.String()+" (1)")
		require.Contains(t, err.Error(), "1 stored tuple(s) still name them")
		require.Contains(t, err.Error(), "SELECT object_type, object_id, relation")

		// The refusal is a refusal, not a partial application: the shape is
		// still there and so is the tuple that blocked it.
		shapes, shapesErr := store.storedShapes(t.Context(), store.db)
		require.NoError(t, shapesErr)
		require.Contains(t, shapes, retiredShape)

		stored, tuplesErr := store.Tuples(t.Context())
		require.NoError(t, tuplesErr)
		require.Len(t, stored, len(fixtureTuples))

		// And the vocabulary the code still declares reconciles as before.
		require.NoError(t, store.Reconcile(t.Context()))
	})
}

// TestReconcileRefusesToRetireATypeAShapeNames is the same closure one table up:
// authz_relation_shape's own foreign keys mean a type cannot leave while a
// shape still names it, so the two lookup tables cannot end up disagreeing.
func TestReconcileRefusesToRetireATypeAShapeNames(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		narrowed := slices.DeleteFunc(slices.Clone(Types()), func(typ Type) bool { return typ == TypeNode })
		require.Len(t, narrowed, len(Types())-1)

		requireCode(t,
			store.reconcile(t.Context(), narrowed, Actions(), relationSpecs),
			CodeGraphUnavailable(),
		)

		types, err := scanNames(t.Context(), store.db, store.dialect, selectTypesSQL)
		require.NoError(t, err)
		require.Contains(t, types, TypeNode.String())
	})
}

// TestReconcileRetiresAnActionNothingCompiles covers the third lookup table. A
// stored action name the vocabulary never declared is a published list entry
// with nothing behind it, and retiring it removes nothing else.
func TestReconcileRetiresAnActionNothingCompiles(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		_, err := store.db.ExecContext(t.Context(), store.q(insertActionSQL), "legacy_verb")
		require.NoError(t, err)

		require.NoError(t, store.reconcile(t.Context(), Types(), Actions(), relationSpecs))

		actions, err := scanNames(t.Context(), store.db, store.dialect, selectActionsSQL)
		require.NoError(t, err)
		require.NotContains(t, actions, "legacy_verb")
		require.ElementsMatch(t, vocabularyNames(Actions()), actions)
	})
}

// TestRetiringAnActionThisBuildStillCompilesIsRefused is trap 1 of ADR-20260831-c7b,
// which an adversarial round measured.
//
// A retireNames that deletes an authz_action row with no dependency check, where
// retireShapes refuses loudly, makes ADR-20260827-rqc rule 11's claim that Reconcile
// refuses to retire an entry a tuple still names false for an action: an
// action appears in no tuple, its bit lives in the level masks, and the next
// recompile wrote the bit back into every projected row. A stored device_access
// mask kept a retired action's bit at value 9247.
//
// Rule 16 needs that path to revoke or to refuse, and this is the refusal. The
// case is the drift itself: Actions no longer declares fetch_job and
// Action.mask still answers a bit for it, which is the two lists disagreeing.
func TestRetiringAnActionThisBuildStillCompilesIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		narrowed := slices.DeleteFunc(slices.Clone(Actions()),
			func(action Action) bool { return action == ActionFetchJob })

		err := store.reconcile(t.Context(), Types(), narrowed, relationSpecs)
		requireCode(t, err, CodeInvalidStoredTuple())
		require.ErrorContains(t, err, ActionFetchJob.String())
		require.ErrorContains(t, err, "still compiles it to bit")

		// The refusal rolls the whole reconcile back, so the row is still
		// there and the projection still means what it meant.
		actions, scanErr := scanNames(t.Context(), store.db, store.dialect, selectActionsSQL)
		require.NoError(t, scanErr)
		require.Contains(t, actions, ActionFetchJob.String(),
			"a refused retirement removed the row anyway")
	})
}

// TestMigrateRefusesSQLiteWithoutForeignKeys is why the pragma is not left to a
// comment in a DSN. SQLite ignores foreign keys unless the connection asked for
// them, and a database that silently stops enforcing the vocabulary is worse
// than one that never claimed to: the schema, the tests and the package doc all
// go on saying it holds.
func TestMigrateRefusesSQLiteWithoutForeignKeys(t *testing.T) {
	t.Parallel()

	db, ok := unsafeSQLite(t)
	if !ok {
		t.Skip(noEngineSkip)
	}

	store, err := Open(db, DialectSQLite)
	require.NoError(t, err)
	require.ErrorIs(t, migrateGraph(t.Context(), store), sqlstore.ErrForeignKeysDisabled)
}

// TestForeignKeysAreEnforcedOnEveryConnection holds the pragma where it has to
// be set, in the DSN. Setting it with one Exec lands on one pooled connection
// and the next query runs on another, which is a constraint that holds until
// the pool grows.
func TestForeignKeysAreEnforcedOnEveryConnection(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		if e.Dialect != DialectSQLite {
			t.Skip("postgres enforces foreign keys unconditionally")
		}

		db := e.Open(t)

		// Held together on purpose: released one at a time, the pool would hand
		// the same connection back every time and prove nothing.
		const probes = 4

		conns := make([]*sql.Conn, 0, probes)

		defer func() {
			for _, conn := range conns {
				require.NoError(t, conn.Close())
			}
		}()

		for i := range probes {
			conn, err := db.Conn(t.Context())
			require.NoError(t, err)

			conns = append(conns, conn)

			var enabled int
			require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&enabled))
			require.Equalf(t, 1, enabled, "connection %d does not enforce foreign keys", i)
		}
	})
}

// unsafeSQLite opens a SQLite handle with foreign key enforcement left off,
// reporting false when this build has no SQLite. It goes through engines first,
// so a run that requires the engine fails rather than skipping.
func unsafeSQLite(t *testing.T) (*sql.DB, bool) {
	t.Helper()

	engines(t)

	return openUnsafeSQLite(t)
}

// withoutShape returns the vocabulary with one subject shape removed, which is
// what a relation leaving the vocabulary looks like in a diff.
func withoutShape(specs []relationSpec, drop relationShape) []relationSpec {
	out := make([]relationSpec, 0, len(specs))

	for _, spec := range specs {
		if spec.Object != drop.ObjectType || spec.Relation != drop.Relation {
			out = append(out, spec)

			continue
		}

		kept := make([]subjectShape, 0, len(spec.Subjects))

		for _, subject := range spec.Subjects {
			if subject.Type != drop.SubjectType || subject.Relation != drop.SubjectRelation {
				kept = append(kept, subject)
			}
		}

		spec.Subjects = kept
		out = append(out, spec)
	}

	return out
}

// vocabularyNames renders a vocabulary list as the strings its lookup table
// stores.
func vocabularyNames[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}

	return out
}
