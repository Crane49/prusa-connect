package authz

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// storedProjection reads both projection tables as sorted text. Two compiles of
// one graph have to agree row for row, and this is the form that says which row
// they disagree on.
func storedProjection(tb testing.TB, store *Store) []string {
	tb.Helper()

	var out []string

	for _, query := range []string{
		`SELECT subject_type, subject_id, device_id, actions FROM device_access
			ORDER BY subject_type, subject_id, device_id`,
		`SELECT subject_type, subject_id, kind, actions FROM resource_access
			ORDER BY subject_type, subject_id, kind`,
	} {
		out = append(out, projectedRows(tb, store, query)...)
	}

	return out
}

// projectedRows reads one of the two projection tables as text.
func projectedRows(tb testing.TB, store *Store, query string) []string {
	tb.Helper()

	rows, err := store.db.QueryContext(tb.Context(), store.q(query))
	require.NoError(tb, err)

	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var subjectType, subjectID, object string

		var actions int64

		require.NoError(tb, rows.Scan(&subjectType, &subjectID, &object, &actions))
		out = append(out, subjectType+":"+subjectID+" "+object+" "+strconv.FormatInt(actions, 10))
	}

	require.NoError(tb, rows.Err())

	return out
}

// requireScopedMatchesWhole is the equivalence the incremental projection rests
// on: what the scoped recompile left in the two tables is exactly what a whole
// recompile of the same tuples writes.
//
// It compares against Rebuild rather than against a second implementation on
// purpose. The oracle in equivalence_test.go already holds the compiler to a
// walk of the raw tuples; what this holds is the narrower claim that scoping the
// recompile changed no row, which is a claim about the scope and not about the
// vocabulary.
func requireScopedMatchesWhole(tb testing.TB, store *Store) {
	tb.Helper()

	scoped := storedProjection(tb, store)

	require.NoError(tb, store.Rebuild(tb.Context()))
	require.Equal(tb, storedProjection(tb, store), scoped)
}

// scopedMutations is every shape of write the store takes, each one exercising
// an edge the scope walk has to follow. A row here that the walk does not reach
// leaves the projection stale, which is what requireScopedMatchesWhole reports.
var scopedMutations = []struct {
	name string
	do   func(tb testing.TB, store *Store)
}{
	{name: "a role written on a device", do: writeTuples("device:p-beta#manager@user:mallory")},
	{name: "a role withdrawn from a device", do: deleteTuples("device:p-delta#viewer@user:dana")},
	{name: "a block written on a device", do: writeTuples("device:p-beta#blocked@user:alice")},
	{name: "a block withdrawn from a device", do: deleteTuples("device:p-gamma#blocked@user:olga")},
	{name: "an owner written on a device", do: writeTuples("device:p-beta#owner@user:bob")},
	{name: "an agent written on a device", do: writeTuples("device:p-beta#agent@device_credential:sn-beta")},
	{name: "an agent withdrawn from a device", do: deleteTuples("device:p-alpha#agent@device_credential:sn-alpha")},
	{name: "a device placed in an org", do: writeTuples("device:cam-alpha#org@org:acme")},
	{name: "a device taken out of its org", do: deleteTuples("device:p-beta#org@org:acme")},
	{name: "a device put into a group", do: writeTuples("device:p-delta#group@device_group:floor-two")},
	{name: "a device taken out of its group", do: deleteTuples("device:p-beta#group@device_group:floor-one")},

	// The parent edge is the one the scope has to walk downwards: the tuple is
	// written on the camera and the rows that move are the camera's.
	{name: "a device put inside another", do: writeTuples("device:cam-alpha#parent@device:p-beta")},
	{name: "a device taken out of its parent", do: deleteTuples("device:cam-alpha#parent@device:p-alpha")},

	{name: "a role written on a device group", do: writeTuples("device_group:cell-a#manager@user:mallory")},
	{
		name: "a role withdrawn from a device group",
		do:   deleteTuples("device_group:floor-one#operator@group:maintenance#member"),
	},
	{
		name: "a device group nested under another",
		do:   writeTuples("device_group:floor-two#parent@device_group:floor-one"),
	},
	{
		name: "a device group taken out of its parent",
		do:   deleteTuples("device_group:cell-a#parent@device_group:floor-one"),
	},
	{name: "a device group placed in an org", do: writeTuples("device_group:floor-two#org@org:brno")},
	{name: "a device group taken out of its org", do: deleteTuples("device_group:floor-one#org@org:acme")},

	{name: "a role written on an org", do: writeTuples("org:acme#operator@user:mallory")},
	{name: "a role withdrawn from an org", do: deleteTuples("org:acme#admin@user:root")},

	{name: "a member added to a group", do: writeTuples("group:maintenance#member@user:mallory")},
	{name: "a member removed from a group", do: deleteTuples("group:maintenance#member@user:alice")},
	{name: "a group nested inside another", do: writeTuples("group:maintenance#member@group:all-staff#member")},
	{name: "a nested group removed", do: deleteTuples("group:all-staff#member@group:maintenance#member")},

	{name: "a role written on a resource kind", do: writeTuples("resource_kind:sink#manager@user:mallory")},
	{name: "a role withdrawn from a resource kind", do: deleteTuples("resource_kind:storage#manager@user:dana")},
	{name: "a resource kind placed in an org", do: writeTuples("resource_kind:camera#org@org:acme")},
	{name: "a resource kind taken out of its org", do: deleteTuples("resource_kind:sink#org@org:acme")},

	{name: "a device registered", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.UpsertDevice(tb.Context(), "p-hidden", "Hidden"))
	}},
	{name: "a device renamed", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.UpsertDevice(tb.Context(), "p-alpha", "Renamed"))
	}},
	// p-alpha is the device cam-alpha sits inside, so this is the removal that
	// has to reach a second device's rows.
	{name: "a device unregistered", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.RemoveDevice(tb.Context(), "p-alpha"))
	}},
	{name: "a device registered with its placement", do: func(tb testing.TB, store *Store) {
		object, err := NewObject(TypeDevice, "p-new")
		require.NoError(tb, err)

		placement, err := NewTuple(object, RelOrg, subjectOf(tb, "org:acme"))
		require.NoError(tb, err)

		require.NoError(tb, store.RegisterDevice(tb.Context(), DeviceRow{ID: "p-new", Name: "New"}, placement))
	}},

	{name: "an instance registered", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.RegisterResources(tb.Context(), instanceOf(tb, ref.KindSink, "second")))
	}},
	{name: "an instance unregistered", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.RemoveResource(tb.Context(), instanceOf(tb, ref.KindStorage, "local")))
	}},
	{name: "the instance directory reconciled", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.SyncResources(tb.Context(), instanceOf(tb, ref.KindSink, "webhook")))
	}},

	{name: "a camera declared against another printer", do: func(tb testing.TB, store *Store) {
		require.NoError(
			tb,
			store.DeclareDevice(
				tb.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-alpha", Name: "Alpha camera", Parent: "p-beta"},
				nil,
			),
		)
	}},
	{name: "a declared camera retired", do: func(tb testing.TB, store *Store) {
		require.NoError(tb, store.RemoveDevice(tb.Context(), "cam-alpha"))
	}},
	{name: "a camera declared in an org", do: func(tb testing.TB, store *Store) {
		require.NoError(
			tb,
			store.DeclareDevice(
				tb.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-beta", Name: "Beta camera", Org: "acme"},
				nil,
			),
		)
	}},
}

// TestTheScopedProjectionMatchesAWholeRebuild is the gate on the incremental
// path. Every mutation the store takes runs against the fixture, and the rows
// it left have to be the rows a recompile of the whole graph writes.
func TestTheScopedProjectionMatchesAWholeRebuild(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		for _, tc := range scopedMutations {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				store := seedFixture(t, e)
				tc.do(t, store)
				requireScopedMatchesWhole(t, store)
			})
		}
	})
}

// TestTheScopedProjectionMatchesAWholeRebuildInSequence runs the same mutations
// one after another against one store. A scope that is right for a fresh graph
// and wrong for one three writes old is the failure a per-case store cannot see.
func TestTheScopedProjectionMatchesAWholeRebuildInSequence(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, tc := range scopedMutations {
			t.Run(tc.name, func(t *testing.T) {
				tc.do(t, store)
				requireScopedMatchesWhole(t, store)
			})
		}
	})
}

// TestTheScopedProjectionMatchesThePointCheck sweeps the oracle over a graph
// that reached its state through scoped writes. The projection agreeing with a
// rebuild says the two compiles agree; this says the compile itself is still
// what a walk of the raw tuples answers.
func TestTheScopedProjectionMatchesThePointCheck(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(), mustTuples(t,
			"group:maintenance#member@user:mallory",
			"device_group:floor-two#parent@device_group:floor-one",
			"device:cam-alpha#parent@device:p-beta",
			"org:acme#operator@user:bob",
		)...))
		require.NoError(t, store.Delete(t.Context(), mustTuples(t,
			"org:acme#viewer@user:olga",
			"group:all-staff#member@group:maintenance#member",
			"device:p-alpha#agent@device_credential:sn-alpha",
		)...))

		reference := newOracle(t, store)

		for _, name := range fixturePrincipals {
			who := subjectOf(t, name)

			for _, action := range printerActions() {
				for _, id := range append(directoryIDs(), "p-hidden") {
					got, err := store.Can(t.Context(), who, action, deviceOf(t, id))
					require.NoError(t, err)
					require.Equalf(t, reference.can(who, action, id), got,
						"%s %s on device:%s", name, action, id)
				}
			}

			for _, action := range resourceActions() {
				for _, instance := range resourceUniverse(t) {
					got, err := store.CanResource(t.Context(), who, action, instance)
					require.NoError(t, err)
					require.Equalf(t, reference.canResource(who, action, instance), got,
						"%s %s on %s", name, action, instance)
				}
			}
		}
	})
}

// TestAnArrivalRewritesNoOtherDevicesRows is the scale claim, read off the
// database rather than off a clock. One printer dialling in must leave every
// other device's projected rows exactly where they were.
//
// The canary is a row deliberately made wrong. A whole recompile puts it right
// again, which is how this case fails when the scope stops holding.
func TestAnArrivalRewritesNoOtherDevicesRows(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFleet(t, e)

		canary := `UPDATE device_access SET actions = 0 WHERE device_id = ?`
		_, err := store.db.ExecContext(t.Context(), store.q(canary), fleetDeviceID(0))
		require.NoError(t, err)

		admin := subjectOf(t, fleetOrgAdmin)

		allowed, err := store.Can(t.Context(), admin, ActionObserve, deviceOf(t, fleetDeviceID(0)))
		require.NoError(t, err)
		require.False(t, allowed, "the canary has to start wrong for this case to mean anything")

		object, err := NewObject(TypeDevice, "p-arrived")
		require.NoError(t, err)

		placement, err := NewTuple(object, RelOrg, subjectOf(t, "org:acme"))
		require.NoError(t, err)

		require.NoError(t, store.RegisterDevice(
			t.Context(), DeviceRow{ID: "p-arrived", Name: "Arrived"}, placement,
		))

		allowed, err = store.Can(t.Context(), admin, ActionObserve, deviceOf(t, fleetDeviceID(0)))
		require.NoError(t, err)
		require.False(t, allowed, "one arrival recompiled a device it did not move")

		allowed, err = store.Can(t.Context(), admin, ActionObserve, deviceOf(t, "p-arrived"))
		require.NoError(t, err)
		require.True(t, allowed, "the arriving device is what the write did have to compile")

		require.NoError(t, store.Rebuild(t.Context()))

		allowed, err = store.Can(t.Context(), admin, ActionObserve, deviceOf(t, fleetDeviceID(0)))
		require.NoError(t, err)
		require.True(t, allowed, "a whole recompile puts the canary back")
	})
}

// TestAFleetWideWriteTakesTheWholeRecompile holds the other side of the
// ceiling: a grant on the org moves every device under it, and the scoped path
// hands that to the whole recompile rather than visiting four hundred objects
// one at a time.
func TestAFleetWideWriteTakesTheWholeRecompile(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFleet(t, e)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:acme#operator@user:u199")))

		who := subjectOf(t, "user:u199")

		total, err := store.CountDevices(t.Context(), who, ActionOperate)
		require.NoError(t, err)
		require.Equal(t, fleetDevices, total)

		requireScopedMatchesWhole(t, store)
	})
}

// writeTuples and deleteTuples are the two one-line mutations the table above is
// mostly made of.
func writeTuples(texts ...string) func(tb testing.TB, store *Store) {
	return func(tb testing.TB, store *Store) {
		tb.Helper()
		require.NoError(tb, store.Write(tb.Context(), mustTuples(tb, texts...)...))
	}
}

func deleteTuples(texts ...string) func(tb testing.TB, store *Store) {
	return func(tb testing.TB, store *Store) {
		tb.Helper()
		require.NoError(tb, store.Delete(tb.Context(), mustTuples(tb, texts...)...))
	}
}

// TestPlaceholdersRendersAnInList holds the one string this file builds, since
// it is concatenated into a query.
func TestPlaceholdersRendersAnInList(t *testing.T) {
	t.Parallel()

	for n, want := range map[int]string{0: "", 1: "?", 3: "?, ?, ?"} {
		require.Equalf(t, want, placeholders(n), "%d placeholders", n)
	}
}

// TestScopedTuplesReadsWhatTheCompilerLooksUp is the sufficiency half of the
// scoped read: the tuples it returns for one device compile that device to the
// same rows the whole tuple table does.
func TestScopedTuplesReadsWhatTheCompilerLooksUp(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, id := range directoryIDs() {
			t.Run(id, func(t *testing.T) {
				t.Parallel()

				whole, err := store.Tuples(t.Context())
				require.NoError(t, err)

				tx, err := store.db.BeginTx(t.Context(), nil)
				require.NoError(t, err)

				defer func() { _ = tx.Rollback() }()

				scoped, err := store.scopedTuples(t.Context(), tx, []string{id}, nil)
				require.NoError(t, err)
				require.Less(t, len(scoped), len(whole), "a scoped read that took everything proves nothing")

				require.Equal(t, compiledRows(indexTuples(whole), id), compiledRows(indexTuples(scoped), id))
			})
		}
	})
}

// compiledRows is one device's projected rows as sorted text. rowsFor walks a
// map, so the slice it returns is in no order and only the set is the answer.
func compiledRows(g *graph, device string) []string {
	rows := g.rowsFor(printerDirectory(device)[0])

	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s:%s %d",
			row.Subject.typ, row.Subject.id, row.Actions))
	}

	slices.Sort(out)

	return out
}
