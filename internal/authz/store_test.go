package authz

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, migrateGraph(t.Context(), store))

		allowed, err := store.Can(t.Context(), subjectOf(t, "user:dana"), ActionObserve, deviceOf(t, "p-delta"))
		require.NoError(t, err)
		require.True(t, allowed, "a second migration must not disturb the graph")
	})
}

func TestWriteIsIdempotent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		before, err := store.Tuples(t.Context())
		require.NoError(t, err)

		require.NoError(t, store.Write(t.Context(), mustTuples(t, fixtureTuples...)...))

		after, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Len(t, after, len(before))
	})
}

func TestDeleteOfAMissingTupleIsNotAnError(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Delete(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:mallory")))

		stored, err := store.Tuples(t.Context())
		require.NoError(t, err)
		require.Len(t, stored, len(fixtureTuples))
	})
}

func TestTuplesReadsBackWhatWasWritten(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		stored, err := store.Tuples(t.Context())
		require.NoError(t, err)

		got := make([]string, 0, len(stored))
		for _, tuple := range stored {
			got = append(got, tuple.String())
		}

		want := slices.Clone(fixtureTuples)
		slices.Sort(want)
		slices.Sort(got)
		require.Equal(t, want, got)
	})
}

func TestWritingNothingIsNotAnError(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context()))
		require.NoError(t, store.Delete(t.Context()))
		require.NoError(t, store.UpsertDevices(t.Context()))
	})
}

func TestUpsertPrinterRenames(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.UpsertDevice(t.Context(), "p-alpha", "Zulu"))

		rows, err := store.ListDevices(t.Context(), subjectOf(t, "user:root"), ActionObserve, Page{})
		require.NoError(t, err)
		require.Equal(t, []string{"Alpha camera", "Beta", "Delta", "Gamma", "Zulu"}, names(rows))
	})
}

func TestUpsertPrinterRejectsAnInvalidID(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		requireCode(t, store.UpsertDevice(t.Context(), "P Alpha", "Alpha"), CodeInvalidID())
		requireCode(t, store.RemoveDevice(t.Context(), "P Alpha"), CodeInvalidID())
	})
}

// TestRemovePrinterPurgesItsTuples is the reuse case: an operator-chosen id
// outlives the machine that carried it, and re-registering one must not
// resurrect the grants of its predecessor.
func TestRemovePrinterPurgesItsTuples(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")

		require.NoError(t, store.RemoveDevice(t.Context(), "p-delta"))
		require.NoError(t, store.UpsertDevice(t.Context(), "p-delta", "Delta"))

		allowed, err := store.Can(t.Context(), dana, ActionObserve, deviceOf(t, "p-delta"))
		require.NoError(t, err)
		require.False(t, allowed, "a replacement printer must not inherit the grants of the one it replaced")

		stored, err := store.Tuples(t.Context())
		require.NoError(t, err)

		for _, tuple := range stored {
			require.NotEqualf(t, "p-delta", tuple.Object().ID(), "tuple survived the purge: %s", tuple)
		}

		// Every other printer is untouched.
		olga, err := store.Can(t.Context(), subjectOf(t, "user:olga"), ActionObserve, deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.True(t, olga)
	})
}

// TestEpochMovesOnlyOnRealChanges holds the counter to meaning something. A
// bump invalidates every cached decision and every live subscription in the
// deployment, so a write that stores nothing new must not move it.
func TestEpochMovesOnlyOnRealChanges(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		epoch := func(t *testing.T) uint64 {
			t.Helper()

			stored, err := store.readEpoch(t.Context(), store.db)
			require.NoError(t, err)
			require.Equal(t, stored, store.Epoch(),
				"the cached epoch and its row disagree")

			return store.Epoch()
		}

		cases := []struct {
			name string
			act  func(t *testing.T)
			want bool
		}{
			{
				name: "writing a tuple already present",
				act: func(t *testing.T) {
					require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-delta#viewer@user:dana")))
				},
			},
			{
				name: "deleting a tuple that is not there",
				act: func(t *testing.T) {
					require.NoError(t, store.Delete(t.Context(), mustTuple(t, "device:p-beta#viewer@user:mallory")))
				},
			},
			{
				name: "registering a printer under the name it already has",
				act: func(t *testing.T) {
					require.NoError(t, store.UpsertDevice(t.Context(), "p-alpha", "Alpha"))
				},
			},
			{
				name: "unregistering a printer that was never registered",
				act: func(t *testing.T) {
					require.NoError(t, store.RemoveDevice(t.Context(), "p-nowhere"))
				},
			},
			{
				name: "writing a new tuple",
				act: func(t *testing.T) {
					require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-beta#viewer@user:mallory")))
				},
				want: true,
			},
			{
				name: "renaming a printer",
				act: func(t *testing.T) {
					require.NoError(t, store.UpsertDevice(t.Context(), "p-alpha", "Alpha II"))
				},
				want: true,
			},
			{
				name: "deleting a tuple that is there",
				act: func(t *testing.T) {
					require.NoError(t, store.Delete(t.Context(), mustTuple(t, "device:p-beta#viewer@user:mallory")))
				},
				want: true,
			},
			{
				name: "registering a resource that is already registered",
				act: func(t *testing.T) {
					require.NoError(t, store.RegisterResources(t.Context(), instanceOf(t, ref.KindStorage, "local")))
				},
			},
			{
				name: "unregistering a resource that was never registered",
				act: func(t *testing.T) {
					require.NoError(t, store.RemoveResource(t.Context(), instanceOf(t, ref.KindSink, "nowhere")))
				},
			},
			{
				name: "registering a resource",
				act: func(t *testing.T) {
					require.NoError(t, store.RegisterResources(t.Context(), instanceOf(t, ref.KindSink, "pager")))
				},
				want: true,
			},
			{
				name: "unregistering a resource",
				act: func(t *testing.T) {
					require.NoError(t, store.RemoveResource(t.Context(), instanceOf(t, ref.KindSink, "pager")))
				},
				want: true,
			},
		}

		// Sequential on purpose: each case observes the epoch the one before
		// it left behind.
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				before := epoch(t)
				tc.act(t)
				after := epoch(t)

				if tc.want {
					require.Greater(t, after, before)

					return
				}

				require.Equal(t, before, after)
			})
		}
	})
}

// TestMissingEpochRowIsAnError covers the way the counter fails. Without the
// seed row the update would match nothing and every write would land without
// invalidating a single cached decision, which is the failure mode that makes
// stale authorization invisible.
func TestMissingEpochRowIsAnError(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		_, err := store.db.ExecContext(t.Context(), "DELETE FROM authz_epoch")
		require.NoError(t, err)

		requireCode(t,
			store.Write(t.Context(), mustTuple(t, "device:p-beta#viewer@user:mallory")),
			CodeMissingEpoch(),
		)

		// The refused write left nothing behind.
		allowed, err := store.Can(
			t.Context(),
			subjectOf(t, "user:mallory"),
			ActionObserve,
			deviceOf(t, "p-beta"),
		)
		require.NoError(t, err)
		require.False(t, allowed)
	})
}

// TestRegisterDeviceIsOneMutation holds the arrival path to one transaction. A
// machine dialling in is one event, and each mutation recompiles the whole
// projection, so the directory row and the placement have to land together.
//
// The epoch is what proves it: two mutations move it twice, and every open
// subscription re-authorizes on each move.
func TestRegisterDeviceIsOneMutation(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		object, err := NewObject(TypeDevice, "p-epsilon")
		require.NoError(t, err)

		tenant, err := NewSubject(TypeOrg, "acme")
		require.NoError(t, err)

		placement, err := NewTuple(object, RelOrg, tenant)
		require.NoError(t, err)

		before := store.Epoch()

		require.NoError(t, store.RegisterDevice(t.Context(),
			DeviceRow{ID: "p-epsilon", Name: "Epsilon"}, placement))

		require.Equal(t, before+1, store.Epoch(),
			"registering a device and placing it moved the epoch more than once")

		// The org admin reaches it, which is the whole point of the placement:
		// the directory row alone leaves a device nobody may see.
		allowed, err := store.Can(t.Context(),
			subjectOf(t, "user:root"), ActionObserve, object)
		require.NoError(t, err)
		require.True(t, allowed)
	})
}

// TestRegisteringTheSameDeviceTwiceMovesNothing is the other half. A printer
// that reconnects registers again, and a fleet reconnecting after a network
// outage must not recompile the projection once per machine for a row that has
// not changed.
func TestRegisteringTheSameDeviceTwiceMovesNothing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		object, err := NewObject(TypeDevice, "p-alpha")
		require.NoError(t, err)

		tenant, err := NewSubject(TypeOrg, "acme")
		require.NoError(t, err)

		placement, err := NewTuple(object, RelOrg, tenant)
		require.NoError(t, err)

		require.NoError(t, store.RegisterDevice(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha"}, placement))

		before := store.Epoch()

		require.NoError(t, store.RegisterDevice(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha"}, placement))

		require.Equal(t, before, store.Epoch(),
			"a re-registration that changed nothing moved the epoch")
	})
}

// TestRegisterDeviceRefusesAnUnconstructedTuple keeps the structure a device
// arrives in inside the grammar. A zero tuple would reach the table as four
// empty columns and stand for an edge nobody wrote.
func TestRegisterDeviceRefusesAnUnconstructedTuple(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		requireCode(t,
			store.RegisterDevice(t.Context(), DeviceRow{ID: "p-zeta", Name: "Zeta"}, Tuple{}),
			CodeUnknownType(),
		)

		held, err := loadDevices(t.Context(), store.db, store.dialect)
		require.NoError(t, err)
		require.NotContains(t, deviceIDsOf(held), "p-zeta",
			"a refused registration left the directory row behind")
	})
}
