package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// queued is one transfer about to start on one machine.
func queued(id, device string) transferapi.Record {
	return transferapi.Record{
		TransferID: id, Device: device,
		State: transferapi.StateQueued, StartedMS: time.Now().UnixMilli(),
	}
}

// A printer fetches one file at a time, so the register refuses a second
// transfer to a machine that is already fetching one, and takes it again once
// the first has settled. Another machine is unaffected.
func TestASecondTransferToOnePrinterIsRefused(t *testing.T) {
	t.Parallel()

	register := newTransferRegister()

	first, err := register.begin(queued("01FIRST", "device:pone"), "", nil)
	require.NoError(t, err)
	require.NotNil(t, first)

	_, err = register.begin(queued("01SECOND", "device:pone"), "", nil)
	require.Error(t, err, "a second transfer to one printer was filed")
	require.True(t, fault.HasCode(err, connectorapi.TransferBusy.Code()),
		"the refusal carries no code a caller can branch on: %v", err)
	require.ErrorIs(t, err, connectorapi.ErrTransferBusy)

	elsewhere, err := register.begin(queued("01ELSEWHERE", "device:ptwo"), "", nil)
	require.NoError(t, err, "a transfer to another printer was refused")
	require.NotNil(t, elsewhere)

	register.end("01FIRST")

	again, err := register.begin(queued("01SECOND", "device:pone"), "", nil)
	require.NoError(t, err, "the printer stayed busy after its transfer settled")
	require.NotNil(t, again)
}

// Giving back a place that was never taken changes nothing, so a caller that
// releases twice cannot make room for a seventeenth print.
func TestReleasingAPlaceThatWasNeverTakenLiftsNoLimit(t *testing.T) {
	t.Parallel()

	register := newTransferRegister()

	register.releaseDetached()
	register.releaseDetached()

	for range maxDetachedPrints {
		require.True(t, register.claimDetached())
	}

	require.False(t, register.claimDetached(), "the limit was lifted by a release with nothing claimed")
}

// A print nobody is waiting for takes one of a fixed number of places, so a
// caller asking in a loop cannot put a goroutine on this server for every ask.
// The place is given back when the print ends.
func TestTheDetachedPrintsThisServerRelaysAreBounded(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane, id := planePrinting(t, graph, "SIM-PRINT-BOUND")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, printingModule)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, printingModule)))

	request := httpapi.DevicePrint{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	for range maxDetachedPrints {
		require.True(t, plane.transfers.claimDetached())
	}

	_, err := plane.StartPrint(t.Context(), printingModule, id, request)
	require.Error(t, err, "a print was detached with every place taken")
	require.True(t, fault.HasCode(err, connectorapi.TransferBusy.Code()),
		"the refusal carries no code a caller can branch on: %v", err)

	for range maxDetachedPrints {
		plane.transfers.releaseDetached()
	}

	_, err = plane.StartPrint(t.Context(), printingModule, id, request)
	require.False(t, fault.HasCode(err, connectorapi.TransferBusy.Code()),
		"the places were not given back: %v", err)
}
