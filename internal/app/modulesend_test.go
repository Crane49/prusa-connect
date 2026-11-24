package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// A module's send lands the file on the printer and starts nothing, and with
// delete_source the file leaves the print folder once it landed.
func TestAModuleSendLandsTheFileAndStartsNothing(t *testing.T) {
	t.Parallel()

	rig := newStickRig(t)
	require.NoError(t, rig.plane.graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, rig.id, authz.RelOperator, printingModule)))
	require.NoError(t, rig.plane.graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	fleet := modulePrinting(t, rig.plane.graph, rig.plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)
	fleet.data = dataHolding(t)

	params := deviceapi.SendParams(fromData(rig.id, "copy.gcode"))
	params.DeleteSource = true

	sent, err := fleet.Send(t.Context(), params)
	require.NoError(t, err)
	require.NotEmpty(t, sent.TransferID, "a send answered without the transfer a module follows it by")

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(filepath.Join(fleet.data, "print", "copy.gcode"))

		return errors.Is(statErr, fs.ErrNotExist)
	}, 5*time.Second, 10*time.Millisecond, "the landed file was not given away")

	require.Contains(t, rig.session.sends(), "usb/copy.gcode")
	require.False(t, slices.ContainsFunc(rig.session.given(), func(c connectorapi.Command) bool {
		return string(c.Verb) == httpapi.StartPrintVerb
	}), "a send started a print")
}

// A send reads a storage, so it takes read_storage as a print does.
func TestAModuleSendTakesReadStorage(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-SEND-READ")
	fleet := modulePrinting(t, graph, plane, authz.ActionObserve, authz.ActionOperate)
	fleet.data = dataHolding(t)

	_, err := fleet.Send(t.Context(), deviceapi.SendParams(fromData(id, "copy.gcode")))
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A module the operator granted no data directory has no print folder to
// send from, as a print has none.
func TestAModuleSendFromAPrintFolderItWasNotGrantedIsRefused(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-SEND-NODATA")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)

	_, err := fleet.Send(t.Context(), deviceapi.SendParams(fromData(id, "copy.gcode")))
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A module's own send is held to its ceiling on operate, as its own print is.
func TestAModulesOwnSendIsHeldToItsCeiling(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-SEND-CEILING")
	fleet := modulePrinting(t, graph, plane, authz.ActionObserve, authz.ActionReadStorage)
	fleet.data = dataHolding(t)

	_, err := fleet.Send(t.Context(), deviceapi.SendParams(fromData(id, "copy.gcode")))
	require.ErrorIs(t, err, deviceapi.ErrDenied)
}

// A send made for a person is decided for that person and not held to the
// module's ceiling: a person granted nothing on the printer is told it is not
// there, and one who may operate it sends through a module whose own ceiling
// is empty.
func TestAModuleSendForAPersonIsDecidedForThatPerson(t *testing.T) {
	t.Parallel()

	rig := newStickRig(t)
	require.NoError(t, rig.plane.graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, rig.id, authz.RelOperator, "user:ada")))
	require.NoError(t, rig.plane.graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, "user:ada")))

	fleet := modulePrinting(t, rig.plane.graph, rig.plane)
	fleet.data = dataHolding(t)

	params := deviceapi.SendParams(fromData(rig.id, "copy.gcode"))
	params.Principal = "user:stranger"

	_, err := fleet.Send(t.Context(), params)
	require.ErrorIs(t, err, deviceapi.ErrNoDevice)

	params.Principal = "user:ada"

	sent, err := fleet.Send(t.Context(), params)
	require.NoError(t, err)
	require.NotEmpty(t, sent.TransferID)
}

// A send that names no storage is refused where the transfer starts, and not
// only by its caller, so it can never become a start of a file the printer
// holds.
func TestASendNamingNoStorageNeverStartsAFile(t *testing.T) {
	t.Parallel()

	rig := newStickRig(t)
	require.NoError(t, rig.plane.graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, rig.id, authz.RelOperator, printingModule)))

	_, err := rig.plane.startDetached(t.Context(), printingModule, rig.id,
		httpapi.DevicePrint{Path: "usb/copy.gcode"}, false)
	require.ErrorIs(t, err, deviceapi.ErrInvalidQuery)
	require.False(t, slices.ContainsFunc(rig.session.given(), func(c connectorapi.Command) bool {
		return string(c.Verb) == httpapi.StartPrintVerb
	}), "a send with no storage started a print")
}
