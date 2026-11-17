package authz

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestADelegateCannotBlockTheOrgAdminThroughAParentEdge is
// TestADelegateCannotBlockTheAdministratorWhoDelegated with one tuple in front
// of it, and the tuple is device#parent.
//
// mayRemove bounds a subtraction by what the caller did not write here: a grant
// written on the object is the caller's to manage, and access flowing in from
// somewhere else is not, unless the caller holds as much of it. A delegated
// manager holds none of it, so the arming half below is refused.
//
// device#parent is written on the object and confers what another object holds.
// conferred(device, parent) asks only for manage on the child, so a manager
// delegated two devices may name one as the other's parent, and the edge then
// confers the parent's mask on the child to every principal, the caller
// included. While the projection carried one mask for everything not written
// here, that write turned the caller's own written-here manager into inherited
// standing, and the block below went through: the org admin lost a device of
// their own org and nobody could give it back, because lifting a block takes
// the action the block removed.
func TestADelegateCannotBlockTheOrgAdminThroughAParentEdge(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		root := subjectOf(t, "user:root")
		delta := deviceOf(t, "p-delta")

		// The org admin delegates two devices, which is the ordinary act the
		// containment rules exist to bound.
		for _, grant := range []string{
			"device:p-delta#manager@user:dana",
			"device:p-alpha#manager@user:dana",
		} {
			wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, grant))
			require.NoErrorf(t, err, "the org admin could not delegate through %s", grant)
			require.True(t, wrote)
		}

		// The arming half. Without the parent edge the block is refused, which
		// is the rule this case is about.
		_, err := store.WriteGrant(t.Context(), dana, mustTuple(t, "device:p-delta#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		// One tuple, written on a device dana administers, naming a device the
		// check never looks at.
		wrote, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "device:p-delta#parent@device:p-alpha"))
		require.NoError(t, err, "the parent edge is refused, so this case proves nothing")
		require.True(t, wrote)

		// The same block, with nothing else changed.
		_, err = store.WriteGrant(t.Context(), dana, mustTuple(t, "device:p-delta#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionAdminister, delta)
		require.NoError(t, err)
		require.True(t, allowed, "a delegate blocked the org admin off a device the org owns")
	})
}

// TestADelegateStillReachesWhatTheParentConfersOnTheChild is the permit half of
// the rule above, and it is what stops the fix from being "refuse every block on
// a device that has a parent". A caller who reaches the parent as much as the
// victim does may still take what the parent confers on the child: root holds
// the org, so root blocks a printer's viewer off that printer's camera.
func TestADelegateStillReachesWhatTheParentConfersOnTheChild(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		root := subjectOf(t, "user:root")
		ivy := subjectOf(t, "user:ivy")

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
			DeviceRow{ID: "cam-door", Name: "Door", Kind: ref.DeviceKindCamera}))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "org:local#admin@user:root"),
			mustTuple(t, "device:p-alpha#org@org:local"),
			mustTuple(t, "device:cam-door#org@org:local"),
			mustTuple(t, "device:cam-door#parent@device:p-alpha"),
			mustTuple(t, "device:p-alpha#viewer@user:ivy")))

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches, "the printer's viewer does not reach its camera, so this case proves nothing")

		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "device:cam-door#blocked@user:ivy"))
		require.NoError(t, err, "the org admin cannot hide one camera from a viewer of its printer")
		require.True(t, wrote)

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches)
	})
}

// TestAnOrgAdminBlockedOffTheirOwnDeviceCannotLiftTheBlock is what makes the
// case above a lockout rather than a nuisance, and it is why mayRemove has to
// hold: nothing in this build lifts a block from the principal it names.
// mayAdminister reads the projection, a block empties it, and WithdrawGrant
// takes the same permission a write takes.
func TestAnOrgAdminBlockedOffTheirOwnDeviceCannotLiftTheBlock(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		// Written straight into the graph, so this case is about the recovery
		// and not about the write that got there.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-delta#blocked@user:root")))

		err := store.WithdrawGrant(t.Context(), root, mustTuple(t, "device:p-delta#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())
	})
}

// TestAParentWalkIsBoundedByDevicesAndNotByPaths holds reachDevice to the cost
// its own comment claims: one local resolve per device.
//
// Without a memo it was one resolve per *path*. visiting is a path set and is
// unwound on the way out, so a device with two parents was walked twice for
// every device below it. A rung of two devices, each naming both devices on the
// rung above, doubles the work per rung: 40 devices took seconds on a walk that
// runs on every start and inside the transaction every grant write takes.
//
// Nothing bounds the number of parents a device may name. One tuple per pair is
// a write anybody holding manage on the child may make, and
// conferred(device, parent) asks nothing about the parent.
func TestAParentWalkIsBoundedByDevicesAndNotByPaths(t *testing.T) {
	t.Parallel()

	const (
		rungs    = 22
		deadline = 3 * time.Second
	)

	tuples, devices := parentLadder(t, rungs)
	require.Len(t, devices, 2*rungs)

	done := make(chan struct{})

	go func() {
		defer close(done)

		project(tuples, printerDirectory(devices...), nil, verbIndex{})
	}()

	select {
	case <-done:
	case <-time.After(deadline):
		require.Failf(t, "the parent walk did not converge",
			"project over %d devices did not finish in %s, so the walk is counted in paths and not in devices",
			len(devices), deadline)
	}
}

// parentLadder builds rungs of two devices each, where every device names both
// devices on the rung above it as parents. Every tuple is a legal one and every
// device is registered.
func parentLadder(tb testing.TB, rungs int) ([]Tuple, []string) {
	tb.Helper()

	var (
		texts   []string
		devices []string
	)

	name := func(rung, side int) string { return fmt.Sprintf("d-%d-%d", rung, side) }

	for rung := range rungs {
		for side := range 2 {
			devices = append(devices, name(rung, side))

			if rung+1 == rungs {
				continue
			}

			for up := range 2 {
				texts = append(texts,
					fmt.Sprintf("device:%s#parent@device:%s", name(rung, side), name(rung+1, up)))
			}
		}
	}

	texts = append(texts, fmt.Sprintf("device:%s#viewer@user:ivy", name(rungs-1, 0)))

	return mustTuples(tb, texts...), devices
}

// TestACameraBoundToAnUnregisteredPrinterReachesNobody records the tenancy hole
// the missing org tuple leaves. It is the old backlog's open row, and it asserts what the
// build does today rather than what it should do.
//
// A bound camera carries no org tuple on purpose, so every path to it runs
// through the parent edge. The parent walk reads no directory, so the edge
// confers whatever tuples name the parent, and a printer that has not
// registered has none: printers are registered when a machine dials in, and a
// camera is declared from the configuration file at start-up. Between the two,
// and forever if the configuration names a printer that never appears, the
// camera is in the directory, the supervision plane is running it, and nobody
// reaches it. Nobody holds manage on it either, so nobody can grant it, which
// is the failure ADR-20260826-5yj rule 16 refuses.
func TestACameraBoundToAnUnregisteredPrinterReachesNobody(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		root := subjectOf(t, "user:root")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:local#admin@user:root")))

		// What cameraSettings.write writes for a camera that names a
		// printer: the binding, and no placement.
		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Parent: "p-alpha"},
				nil,
			),
		)

		reaches, err := store.Can(t.Context(), root, ActionAdminister, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches, "the org administrator now reaches it, so this gap is closed and the row can go")

		_, err = store.WriteGrant(t.Context(), root, mustTuple(t, "device:cam-door#viewer@user:ivy"))
		requireCode(t, err, CodeAccessDenied())

		// The same camera, declared with no printer, is grantable. So the gap
		// is the binding and not the declaration.
		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Org: "local"},
				nil,
			),
		)

		reaches, err = store.Can(t.Context(), root, ActionAdminister, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches)
	})
}

// TestAParentEdgeConfersNothingUpwardsOrSideways is the negative result of this
// review, held as a case rather than as a claim. The walk carries access down
// the edge and never up it, and it says nothing about the devices beside the
// child. What the writer needs is manage on the child alone, so this is also
// what conferred(device, parent) is worth: it is checked against the object,
// and the subject is unchecked.
func TestAParentEdgeConfersNothingUpwardsOrSideways(t *testing.T) {
	t.Parallel()

	// The permission the write takes, read off the function the check uses.
	require.Equal(t, levelManager.deviceMask(), conferred(TypeDevice, RelParent),
		"a parent edge is writable below manager on the child")

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")

		wrote, err := store.WriteGrant(t.Context(), subjectOf(t, "user:root"),
			mustTuple(t, "device:p-delta#manager@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		// dana reaches nothing on p-alpha or p-beta, and names p-alpha as her
		// device's parent anyway: the check reads the object and never the
		// subject.
		for _, action := range printerActions() {
			allowed, canErr := store.Can(t.Context(), dana, action, deviceOf(t, "p-alpha"))
			require.NoError(t, canErr)
			require.Falsef(t, allowed, "dana already holds %s on the parent, so this proves nothing", action)
		}

		wrote, err = store.WriteGrant(t.Context(), dana,
			mustTuple(t, "device:p-delta#parent@device:p-alpha"))
		require.NoError(t, err)
		require.True(t, wrote)

		for _, id := range []string{"p-alpha", "p-beta", "p-gamma"} {
			for _, action := range printerActions() {
				allowed, canErr := store.Can(t.Context(), dana, action, deviceOf(t, id))
				require.NoError(t, canErr)
				require.Falsef(t, allowed, "naming p-alpha as a parent handed dana %s on %s", action, id)
			}
		}
	})
}
