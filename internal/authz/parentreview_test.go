package authz

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestADelegateCannotBlockTheOrgAdminOffABoundCamera is the attack 1bf4f34 does
// not close, on the shape the product actually builds.
//
// cameraSettings.placement writes no org tuple on a camera that names a printer,
// on purpose: "a design requirement is that there is no such path". So every
// principal's standing on a bound camera is fromParent and nothing else, and
// the caller's inherited and the victim's inherited are both zero. The per
// source comparison collapses to one arm.
//
// inheritFrom copies the parent's resolved mask, which is the union of what was
// written on the parent and what the parent's org confers, into one fromParent
// mask on the child. On the camera a delegated manager and the org admin are
// therefore identical, so the delegate outranks nobody and is refused by
// nobody. The block goes in, and the camera has no second path for the admin to
// come back through.
func TestADelegateCannotBlockTheOrgAdminOffABoundCamera(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		root := subjectOf(t, "user:root")
		dana := subjectOf(t, "user:dana")
		camera := deviceOf(t, "cam-door")

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
			DeviceRow{ID: "cam-door", Name: "Door", Kind: ref.DeviceKindCamera}))

		// Exactly what the product writes: the printer is placed, the camera
		// names the printer, and the camera is placed nowhere.
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "org:acme#admin@user:root"),
			mustTuple(t, "device:p-alpha#org@org:acme"),
			mustTuple(t, "device:cam-door#parent@device:p-alpha")))

		// The ordinary delegation the containment rules exist to bound.
		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "device:p-alpha#manager@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		// On the printer the rule holds: root's manager is org-inherited and
		// dana's is written here, so dana outranks nobody.
		_, err = store.WriteGrant(t.Context(), dana, mustTuple(t, "device:p-alpha#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		// One hop down, the same two grants are the same source.
		_, err = store.WriteGrant(t.Context(), dana, mustTuple(t, "device:cam-door#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionAdminister, camera)
		require.NoError(t, err)
		require.True(t, allowed, "a delegate blocked the org admin off a camera of the org's own printer")

		// And it is a lockout rather than a nuisance: there is no org tuple on
		// a bound camera, so nothing else reaches it.
		require.NoError(t, store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "device:p-alpha#manager@user:dana")),
			"root cannot even take the delegation back")
	})
}

// TestAnOrgAdminBlockedOnAPrinterCanStillHideItsCamera is the permit half, and
// it is an action an administrator could take before 1bf4f34 and cannot now.
//
// blocked on a printer is what a design requirement names as the reason a bound
// camera carries no org: "an administrator blocked on a printer would otherwise
// still reach its camera". A camera that does carry an org is the other half of
// that sentence, and there the admin administers the camera through the org
// while holding nothing on the printer.
//
// Before 1bf4f34 the two sources were one mask, so the admin's org manager
// covered the viewer's parent-conferred viewer and the block went in. Held
// apart, the admin's fromParent is empty and the viewer's is not, so an
// administrator holding manage on the object cannot say who reaches it, and has
// no other move: they hold nothing on the printer the grant is written on.
func TestAnOrgAdminBlockedOnAPrinterCanStillHideItsCamera(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		root := subjectOf(t, "user:root")
		ivy := subjectOf(t, "user:ivy")
		camera := deviceOf(t, "cam-door")

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
			DeviceRow{ID: "cam-door", Name: "Door", Kind: ref.DeviceKindCamera}))

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "org:acme#admin@user:root"),
			mustTuple(t, "device:p-alpha#org@org:acme"),
			mustTuple(t, "device:cam-door#org@org:acme"),
			mustTuple(t, "device:cam-door#parent@device:p-alpha"),
			mustTuple(t, "device:p-alpha#viewer@user:ivy"),
			mustTuple(t, "device:p-alpha#blocked@user:root")))

		// The arming half: root administers the camera and holds nothing at
		// all on the printer.
		allowed, err := store.Can(t.Context(), root, ActionAdminister, camera)
		require.NoError(t, err)
		require.True(t, allowed, "root does not administer the camera, so this case proves nothing")

		allowed, err = store.Can(t.Context(), root, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "root is not blocked off the printer, so this case proves nothing")

		allowed, err = store.Can(t.Context(), ivy, ActionObserve, camera)
		require.NoError(t, err)
		require.True(t, allowed, "the printer's viewer does not reach its camera, so this case proves nothing")

		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "device:cam-door#blocked@user:ivy"))
		require.NoError(t, err,
			"an org admin holding manage on the camera can no longer hide it from the printer's viewer")
		require.True(t, wrote)
	})
}

// TestLaunderingIsRefusedThroughAGrandparentAndADiamond runs the original
// attack at depth two and over two parents at once, on devices the org places.
// Every test the fix shipped uses one parent edge.
func TestLaunderingIsRefusedThroughAGrandparentAndADiamond(t *testing.T) {
	t.Parallel()

	for _, shape := range []struct {
		name  string
		edges []string
	}{
		// p-delta sits under p-alpha which sits under p-beta, so what reaches
		// p-delta arrives two hops away.
		{name: "grandparent", edges: []string{
			"device:p-delta#parent@device:p-alpha",
			"device:p-alpha#parent@device:p-beta",
		}},
		// One device reached by two paths at once.
		{name: "diamond", edges: []string{
			"device:p-delta#parent@device:p-alpha",
			"device:p-delta#parent@device:p-beta",
			"device:p-alpha#parent@device:p-gamma",
			"device:p-beta#parent@device:p-gamma",
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			forEachEngine(t, func(t *testing.T, e engine) {
				store := seedFixture(t, e)
				root := subjectOf(t, "user:root")
				dana := subjectOf(t, "user:dana")

				for _, id := range []string{"p-delta", "p-alpha", "p-beta", "p-gamma"} {
					_, err := store.WriteGrant(t.Context(), root,
						mustTuple(t, "device:"+id+"#manager@user:dana"))
					require.NoErrorf(t, err, "the org admin could not delegate %s", id)
				}

				for _, edge := range shape.edges {
					_, err := store.WriteGrant(t.Context(), dana, mustTuple(t, edge))
					require.NoErrorf(t, err, "the edge %s is refused, so this case proves nothing", edge)
				}

				for _, id := range []string{"p-delta", "p-alpha", "p-beta", "p-gamma"} {
					_, err := store.WriteGrant(t.Context(), dana,
						mustTuple(t, "device:"+id+"#blocked@user:root"))
					requireCode(t, err, CodeAccessDenied())

					allowed, canErr := store.Can(t.Context(), root, ActionAdminister, deviceOf(t, id))
					require.NoError(t, canErr)
					require.Truef(t, allowed, "a delegate blocked the org admin off %s", id)
				}
			})
		})
	}
}

// TestTheParentMemoIsIndependentOfTheOrderDevicesAreProjected holds the memo to
// the claim its comment makes: a result stored is the device's answer and not
// one chain's answer.
//
// The graph below reaches the same devices through an acyclic path, through a
// cycle, and through a diamond, so a truncated answer written into the memo
// would be served to a later walk that met no cycle. Projecting one device at a
// time starts from an empty memo every time and is therefore the memo-free
// answer; projecting them together, in any order, has to agree with it.
func TestTheParentMemoIsIndependentOfTheOrderDevicesAreProjected(t *testing.T) {
	t.Parallel()

	devices := []string{"d-top-a", "d-top-b", "d-mid", "d-leaf", "c-a", "c-b", "c-below", "d-both"}

	tuples := mustTuples(t,
		// a diamond over two roots
		"device:d-mid#parent@device:d-top-a",
		"device:d-mid#parent@device:d-top-b",
		"device:d-leaf#parent@device:d-mid",
		// a two-device cycle, and a device below it
		"device:c-a#parent@device:c-b",
		"device:c-b#parent@device:c-a",
		"device:c-below#parent@device:c-a",
		// one device reached both by the acyclic side and by the cycle
		"device:d-both#parent@device:d-mid",
		"device:d-both#parent@device:c-a",
		// something to inherit
		"device:d-top-a#viewer@user:ivy",
		"device:d-top-b#operator@user:bob",
		"device:c-b#manager@user:mallory",
		"device:c-a#viewer@user:dana",
	)

	alone := make(map[string]actionSet)

	for _, id := range devices {
		for _, row := range project(tuples, printerDirectory(id), nil, verbIndex{}).Devices {
			alone[row.Subject.typ.String()+":"+row.Subject.id+" on "+row.Device] = row.Actions
		}
	}

	reversed := slices.Clone(devices)
	slices.Reverse(reversed)

	// Several orders, because the memo is filled in the order project walks.
	for _, order := range [][]string{
		devices,
		reversed,
		{"d-leaf", "d-both", "c-below", "d-mid", "c-a", "d-top-b", "c-b", "d-top-a"},
		{"c-a", "d-both", "d-top-a", "c-below", "d-leaf", "c-b", "d-mid", "d-top-b"},
	} {
		together := make(map[string]actionSet)
		for _, row := range project(tuples, printerDirectory(order...), nil, verbIndex{}).Devices {
			together[row.Subject.typ.String()+":"+row.Subject.id+" on "+row.Device] = row.Actions
		}

		require.Equalf(t, alone, together,
			"projecting %v together differs from projecting each device on its own", order)
	}

	require.NotEmpty(t, alone, "the fixture confers nothing, so this case proves nothing")
	require.True(t, slices.Contains(slices.Collect(maps.Keys(alone)), "user:ivy on d-leaf"),
		"the acyclic chain confers nothing two hops down, so this case proves nothing")
}

// TestOwnershipCrossesTheParentEdgeAndStaysRemovable holds the
// ownership-versus-parent pair: ownership reaches a child through the edge, and
// the child's administrator can still withdraw it.
//
// localDeviceAccess adds ownerMask to held and to no source mask, which is
// deliberate: "the tuple is written on the device, so it inherits nothing and
// the device's manager may withdraw it". inheritFrom used to copy the parent's
// held, ownerMask included, into the child's fromParent, so the same ownership
// was removable on the device it is written on and protected one hop down, and
// the administrator of the child could not take it back unless they reached the
// parent at least as far.
//
// Ownership still crosses the edge, because what a parent confers is the mask
// the parent resolved. It crosses in held alone, so it is standing nowhere and
// whoever administers the object it lands on may remove it, one hop down
// exactly as on the device it is written on.
func TestOwnershipCrossesTheParentEdgeAndStaysRemovable(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		root := subjectOf(t, "user:root")
		dana := subjectOf(t, "user:dana")
		camera := deviceOf(t, "cam-door")

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
			DeviceRow{ID: "cam-door", Name: "Door", Kind: ref.DeviceKindCamera}))

		// The camera is placed and the printer is not, so root administers the
		// camera through the org and holds nothing on the printer.
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "org:acme#admin@user:root"),
			mustTuple(t, "device:cam-door#org@org:acme"),
			mustTuple(t, "device:cam-door#parent@device:p-alpha"),
			mustTuple(t, "device:p-alpha#owner@user:dana")))

		// Owning the printer reaches provision on the camera inside it.
		allowed, err := store.Can(t.Context(), dana, ActionConfigure, camera)
		require.NoError(t, err)
		require.True(t, allowed, "ownership no longer crosses the edge, so this case proves nothing")

		// One hop down it comes off, the same way it comes off where it is
		// written, and root reaches nothing at all on the printer.
		_, err = store.WriteGrant(t.Context(), root, mustTuple(t, "device:cam-door#blocked@user:dana"))
		require.NoError(t, err,
			"ownership of the printer is protected standing on its camera, which it is on neither")

		allowed, err = store.Can(t.Context(), dana, ActionConfigure, camera)
		require.NoError(t, err)
		require.False(t, allowed)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:cam-door#owner@user:ivy")))

		_, err = store.WriteGrant(t.Context(), root, mustTuple(t, "device:cam-door#blocked@user:ivy"))
		require.NoError(t, err, "ownership written on the camera is the camera manager's to remove")
	})
}
