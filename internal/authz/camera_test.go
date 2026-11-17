package authz

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// cameraFleet is one printer, one camera bound to it and one camera bound to
// nothing. The two cameras are registered devices like the printer, which is
// ADR-20260826-5yj rule 14: a camera holds a row of its own rather than borrowing the
// printer's.
func cameraFleet() []DeviceRow {
	return []DeviceRow{
		{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
		{ID: "cam-door", Name: "Door", Kind: ref.DeviceKindCamera},
		{ID: "cam-shed", Name: "Shed", Kind: ref.DeviceKindCamera},
	}
}

// registerCameraFleet writes the fleet above and the one edge that binds the
// bound camera to its printer.
func registerCameraFleet(tb testing.TB, store *Store) {
	tb.Helper()

	require.NoError(tb, store.UpsertDevices(tb.Context(), cameraFleet()...))
	require.NoError(tb, store.Write(tb.Context(), mustTuple(tb, "device:cam-door#parent@device:p-alpha")))
}

// TestABoundCameraIsReachedThroughItsPrinter holds ADR-20260826-5yj rule 15. The grant
// is written on the printer and nothing is written on the camera, and the
// camera answers the same as the printer does. This is the default path and the
// one an existing deployment is already on, so it is the case that must not
// change on upgrade.
func TestABoundCameraIsReachedThroughItsPrinter(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:ivy")))

		ivy := subjectOf(t, "user:ivy")

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches, "a viewer of the printer reaches the camera bound to it")

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-shed"))
		require.NoError(t, err)
		require.False(t, reaches, "the printer's grant says nothing about a camera bound to nothing")
	})
}

// TestAStandaloneCameraCarriesItsOwnGrant holds ADR-20260826-5yj rule 16. A camera that
// binds to no printer is grantable on its own row, which is the whole of what
// makes a design requirement's host camera and IP camera reachable.
func TestAStandaloneCameraCarriesItsOwnGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:cam-shed#viewer@user:ivy")))

		ivy := subjectOf(t, "user:ivy")

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-shed"))
		require.NoError(t, err)
		require.True(t, reaches, "a standalone camera is grantable, so somebody can be granted it")

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, reaches, "a grant on a camera is not a grant on any printer")
	})
}

// TestAGrantOnABoundCameraReachesSomebodyWhoCannotSeeThePrinter holds ADR-20260826-5yj
// rule 17, which is the owner's reading of a design requirement taken on 2026-08-27:
// an administrator may grant one camera directly, to somebody who cannot see
// the printer it watches.
func TestAGrantOnABoundCameraReachesSomebodyWhoCannotSeeThePrinter(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:cam-door#viewer@user:ivy")))

		ivy := subjectOf(t, "user:ivy")

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches, "a grant written straight on a bound camera is legal and reaches it")

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, reaches, "the camera's grant does not reach up the parent edge")
	})
}

// TestABlockOnACameraCutsWhatItsPrinterConfers is the other direction of rule
// 17. A camera is an object like any other, so the deny the vocabulary already
// carries is what hides one camera from somebody who may see the printer, and
// it wins over what flows down the parent edge.
func TestABlockOnACameraCutsWhatItsPrinterConfers(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-alpha#viewer@user:ivy"),
			mustTuple(t, "device:cam-door#blocked@user:ivy"),
		))

		ivy := subjectOf(t, "user:ivy")

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches, "a block on the camera cuts what the printer confers on it")

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, reaches, "and it does not cut the printer itself")
	})
}

// TestAPrintersCredentialDoesNotReachItsCamera holds the assertion the package
// doc makes about the agent relation: a device reaches the device it is the
// agent of and nothing else. A parent edge inherits the role layer and never
// that.
func TestAPrintersCredentialDoesNotReachItsCamera(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-alpha#agent@device_credential:sn-alpha")))

		credential := subjectOf(t, "device_credential:sn-alpha")

		reaches, err := store.Can(t.Context(), credential, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, reaches, "a credential reaches the device it is the agent of")

		reaches, err = store.Can(t.Context(), credential, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches, "and it does not reach that device's camera")
	})
}

// TestAParentCycleResolvesRatherThanRecursing writes a cycle straight into the
// graph. A cycle is a configuration error and it must converge rather than
// recurse: the compiler runs inside the transaction every grant write takes, so
// an unbounded walk there is the whole server, not one request.
func TestAParentCycleResolvesRatherThanRecursing(t *testing.T) {
	t.Parallel()

	compiled := project(
		mustTuples(t,
			"device:cam-door#parent@device:cam-shed",
			"device:cam-shed#parent@device:cam-door",
			"device:cam-shed#viewer@user:ivy",
		),
		printerDirectory("cam-door", "cam-shed"),
		configurableKinds(),
		verbIndex{},
	)

	held := map[string]actionSet{}
	for _, row := range compiled.Devices {
		if row.Subject.id == "ivy" {
			held[row.Device] = row.Actions
		}
	}

	require.Equal(t, maskObserve, held["cam-shed"], "the grant is written here")
	require.Equal(t, maskObserve, held["cam-door"], "and reaches the other end of the cycle once")
}

// TestTheParentEdgeIsInheritedThroughTheWholeChain holds the walk over more
// than one hop. Nothing in the product builds a chain this deep today; the
// shape permits one, and a walk that stopped at the first hop would be a rule
// that is right only for the arrangement somebody happened to test.
func TestTheParentEdgeIsInheritedThroughTheWholeChain(t *testing.T) {
	t.Parallel()

	compiled := project(
		mustTuples(t,
			"device:cam-door#parent@device:cam-shed",
			"device:cam-shed#parent@device:p-alpha",
			"device:p-alpha#operator@user:ivy",
		),
		printerDirectory("p-alpha", "cam-door", "cam-shed"),
		configurableKinds(),
		verbIndex{},
	)

	for _, row := range compiled.Devices {
		if row.Subject.id == "ivy" && row.Device == "cam-door" {
			require.Equal(t, levelOperator.deviceMask(), row.Actions,
				"the level reaches the far end of the chain unchanged")

			return
		}
	}

	require.Fail(t, "the far end of the chain reached nobody")
}

// TestACameraCannotTakeOverAPrintersRow refuses the id collision. The device
// directory is one id space, so an operator who names a camera after a printer
// would otherwise hand that camera every grant the printer carries, silently
// and at the moment the configuration is reloaded.
func TestACameraCannotTakeOverAPrintersRow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter}))

		err := store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Door", Kind: ref.DeviceKindCamera})
		require.Error(t, err, "a camera must not be registered over a printer of the same id")

		rows, err := store.ListDevices(t.Context(), subjectOf(t, "user:ivy"), ActionObserve, Page{})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}

// TestTheDirectoryRemembersWhatKindEachDeviceIs is the read half of the column
// the refusal above rests on. A directory that cannot say what a row is cannot
// refuse a row of the wrong kind.
func TestTheDirectoryRemembersWhatKindEachDeviceIs(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		registerCameraFleet(t, store)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:ivy")))

		rows, err := store.ListDevices(t.Context(), subjectOf(t, "user:ivy"), ActionObserve, Page{})
		require.NoError(t, err)

		kinds := map[string]ref.DeviceKind{}
		for _, row := range rows {
			kinds[row.ID] = row.Kind
		}

		require.Equal(t, ref.DeviceKindPrinter, kinds["p-alpha"])
		require.Equal(t, ref.DeviceKindCamera, kinds["cam-door"])
	})
}

// TestADeclaredCameraIsRegisteredBoundAndPlaced is the write path a reconcile of
// the configuration takes. One call has to leave the camera in the directory,
// inside its org and bound to its printer, because a camera that is registered
// and placed in no org is one the bootstrap administrator cannot even grant.
func TestADeclaredCameraIsRegisteredBoundAndPlaced(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter}))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#org@org:local")))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:local#admin@user:root")))

		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Org: "local", Parent: "p-alpha"},
				nil,
			),
		)
		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-shed", Name: "Shed", Org: "local"},
				nil,
			),
		)

		root := subjectOf(t, "user:root")

		for _, camera := range []string{"cam-door", "cam-shed"} {
			reaches, err := store.Can(t.Context(), root, ActionAdminister, deviceOf(t, camera))
			require.NoError(t, err)
			require.Truef(t, reaches, "the org administrator can administer %s, so it can be granted", camera)
		}

		// The bound one is reached through its printer as well, by somebody who
		// holds nothing on the camera itself.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:ivy")))

		reaches, err := store.Can(t.Context(), subjectOf(t, "user:ivy"), ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches)
	})
}

// TestARemovedCameraLeavesTheGraph holds the removal half: a removed camera is
// out of the directory and out of the graph, and the printer it watched is
// untouched.
func TestARemovedCameraLeavesTheGraph(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter}))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-alpha#viewer@user:ivy")))

		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Parent: "p-alpha"},
				nil,
			),
		)

		ivy := subjectOf(t, "user:ivy")

		reaches, err := store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.True(t, reaches)

		require.NoError(t, store.RemoveDevice(t.Context(), "cam-door"))

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches, "a removed camera is out of the graph")

		reaches, err = store.Can(t.Context(), ivy, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, reaches, "and the printers beside it are untouched")
	})
}

// TestARebindingMovesTheCameraAndLeavesItsOwnGrant holds the two halves of what
// a reload owns. The configuration says which printer a camera watches, so a
// binding that changed leaves no edge behind. It does not say who may see the
// camera, so a grant an administrator wrote on the camera itself survives.
func TestARebindingMovesTheCameraAndLeavesItsOwnGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-alpha", Name: "Alpha", Kind: ref.DeviceKindPrinter},
			DeviceRow{ID: "p-beta", Name: "Beta", Kind: ref.DeviceKindPrinter}))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-alpha#viewer@user:ivy"),
			mustTuple(t, "device:p-beta#viewer@user:otto")))

		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Parent: "p-alpha"},
				nil,
			),
		)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:cam-door#viewer@user:nina")))

		require.NoError(
			t,
			store.DeclareDevice(
				t.Context(),
				ref.DeviceKindCamera,
				DeclaredDevice{ID: "cam-door", Name: "Door", Parent: "p-beta"},
				nil,
			),
		)

		for _, expected := range []struct {
			who    string
			holds  bool
			reason string
		}{
			{who: "user:ivy", holds: false, reason: "the old binding is gone"},
			{who: "user:otto", holds: true, reason: "the new binding is in place"},
			{who: "user:nina", holds: true, reason: "a grant on the camera survives a reload"},
		} {
			reaches, err := store.Can(t.Context(),
				subjectOf(t, expected.who), ActionObserve, deviceOf(t, "cam-door"))
			require.NoError(t, err)
			require.Equal(t, expected.holds, reaches, expected.reason)
		}
	})
}

// TestADeviceAndWhatItRunsAreOneWrite holds the transaction: when the write
// beside the device fails, the device is not registered either.
func TestADeviceAndWhatItRunsAreOneWrite(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:local#admin@user:root")))

		refused := errors.New("refused")
		err := store.DeclareDevice(t.Context(), ref.DeviceKindCamera,
			DeclaredDevice{ID: "cam-door", Name: "Door", Org: "local"},
			func(context.Context, *sql.Tx) error { return refused })
		require.ErrorIs(t, err, refused)

		reaches, err := store.Can(t.Context(), subjectOf(t, "user:root"), ActionAdminister, deviceOf(t, "cam-door"))
		require.NoError(t, err)
		require.False(t, reaches, "the device went with the write that failed")
	})
}
