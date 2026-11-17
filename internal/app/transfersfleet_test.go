package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// viewer may watch one printer and do nothing to it.
const viewer = "user:vera"

// fleetOfTwo is a plane with two printers and the grants the fleet listing is
// asserted against: the viewer reads the first printer, the operator runs both,
// and the manager runs the first and may read its audit.
func fleetOfTwo(t *testing.T) (*devices, string, string) {
	t.Helper()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	first := arrive(t, plane, "SIM-FLEET-ONE")
	second := arrive(t, plane, "SIM-FLEET-TWO")

	for _, grant := range []authz.Tuple{
		tuple(t, authz.TypeDevice, first, authz.RelViewer, viewer),
		tuple(t, authz.TypeDevice, first, authz.RelOperator, operator),
		tuple(t, authz.TypeDevice, second, authz.RelOperator, operator),
		tuple(t, authz.TypeDevice, first, authz.RelManager, manager),
	} {
		require.NoError(t, graph.Write(t.Context(), grant))
	}

	return plane, first, second
}

// startRunning puts one running transfer on the register, begun by startedBy.
func startRunning(t *testing.T, plane *devices, device, id, startedBy string, startedMS int64) {
	t.Helper()

	_, err := plane.transfers.begin(transferapi.Record{
		TransferID: id, Device: "device:" + device, Storage: "local",
		Source: id + ".bgcode", Destination: "usb/" + id + ".bgcode",
		BytesTotal: 4096, BytesSent: 1024,
		State: transferapi.StateSending, StartedMS: startedMS,
	}, startedBy, func() {})
	require.NoError(t, err)
}

// byID indexes a listing by transfer id.
func byID(items []httpapi.RunningTransfer) map[string]httpapi.RunningTransfer {
	out := make(map[string]httpapi.RunningTransfer, len(items))
	for _, item := range items {
		out[item.TransferID] = item
	}

	return out
}

// The listing of every running transfer answers for the caller's own printers
// and for nobody else's, whoever began each transfer. A person who cannot read
// a printer must not learn that a file is on its way to it.
func TestTheRunningTransfersAreThoseOfThePrintersTheCallerMayRead(t *testing.T) {
	t.Parallel()

	plane, first, second := fleetOfTwo(t)
	now := time.Now().UnixMilli()

	startRunning(t, plane, first, "01FIRSTPRINTER", operator, now-1_000)
	startRunning(t, plane, second, "01SECONDPRINTER", owner, now)

	t.Run("a caller who reads no printer sees nothing", func(t *testing.T) {
		t.Parallel()

		items, err := plane.RunningTransfers(t.Context(), stranger)
		require.NoError(t, err)
		require.Empty(t, items, "a caller who reads no printer was told a file is on its way")
	})

	t.Run("a viewer sees the transfer of the printer they read and cannot stop it", func(t *testing.T) {
		t.Parallel()

		items, err := plane.RunningTransfers(t.Context(), viewer)
		require.NoError(t, err)
		require.Len(t, items, 1, "the viewer saw a transfer of a printer they cannot read")

		item := items[0]
		require.Equal(t, "01FIRSTPRINTER", item.TransferID)
		require.Equal(t, first, item.DeviceID)
		require.NotEmpty(t, item.PrinterName)
		require.False(t, item.Mine)
		require.False(t, item.CanStop, "a viewer was offered a stop the stop route refuses them")
		require.Empty(t, item.StartedBy, "a caller without view_audit was told who began the transfer")
	})

	t.Run("the operator sees both, newest first, and which one they began", func(t *testing.T) {
		t.Parallel()

		items, err := plane.RunningTransfers(t.Context(), operator)
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.Equal(t, "01SECONDPRINTER", items[0].TransferID, "the listing is not newest first")

		held := byID(items)
		require.True(t, held["01FIRSTPRINTER"].Mine, "the operator's own transfer is not marked theirs")
		require.False(t, held["01SECONDPRINTER"].Mine, "somebody else's transfer is marked the operator's")
		require.True(t, held["01FIRSTPRINTER"].CanStop)
		require.True(t, held["01SECONDPRINTER"].CanStop)
		require.Empty(t, held["01SECONDPRINTER"].StartedBy)
	})

	t.Run("a manager is told who began the transfer", func(t *testing.T) {
		t.Parallel()

		items, err := plane.RunningTransfers(t.Context(), manager)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, operator, items[0].StartedBy)
		require.False(t, items[0].Mine)
		require.True(t, items[0].CanStop)
	})
}

// A transfer leaves the listing the moment it settles, so the listing is what
// is on its way and never a history.
func TestASettledTransferLeavesTheRunningTransfers(t *testing.T) {
	t.Parallel()

	plane, first, _ := fleetOfTwo(t)
	startRunning(t, plane, first, "01SETTLES", operator, time.Now().UnixMilli())

	items, err := plane.RunningTransfers(t.Context(), viewer)
	require.NoError(t, err)
	require.Len(t, items, 1)

	plane.transfers.end("01SETTLES")

	items, err = plane.RunningTransfers(t.Context(), viewer)
	require.NoError(t, err)
	require.Empty(t, items, "a settled transfer is still listed as running")
}

// transferWatcher is one person's subscription to every transfer frame, with no
// origin named, which is how the header indicator reads the fleet.
type transferWatcher struct {
	mu     sync.Mutex
	frames []event.Envelope
}

func watchTransfers(t *testing.T, hub *stream.Hub, principal string) *transferWatcher {
	t.Helper()

	sub, err := hub.Subscribe(context.WithoutCancel(t.Context()), stream.SubscribeRequest{
		Principal: principal,
		Encoding:  stream.EncodingJSON,
		Types: []string{
			event.TypeDeviceTransferStarted,
			event.TypeDeviceTransferProgressed,
			event.TypeDeviceTransferSettled,
		},
	})
	require.NoError(t, err)

	t.Cleanup(sub.Close)

	watcher := &transferWatcher{}

	go func() {
		for batch := range sub.Batches() {
			watcher.mu.Lock()

			for _, frame := range batch.Events {
				var envelope event.Envelope
				if json.Unmarshal(frame.Bytes, &envelope) == nil {
					watcher.frames = append(watcher.frames, envelope)
				}
			}

			watcher.mu.Unlock()
		}
	}()

	return watcher
}

func (w *transferWatcher) subjects() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([]string, 0, len(w.frames))
	for _, frame := range w.frames {
		out = append(out, frame.Subject)
	}

	return out
}

// The frames that keep the listing current go to the same people the listing
// answers. A subscription that names no printer carries the transfers of every
// printer the reader may see and of no other, and the started frame names no
// person.
func TestTransferFramesReachOnlyThoseWhoReadThePrinter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, first, second := fleetOfTwo(t)

		watching := watchTransfers(t, plane.hub, viewer)
		outside := watchTransfers(t, plane.hub, stranger)

		upload := httpapi.DeviceUpload{Storage: "local", Source: "part.bgcode", Path: "usb/part.bgcode"}

		for _, device := range []string{first, second} {
			entry, err := plane.beginTransferRecord(t.Context(), operator, device, upload, 4096, func() {})
			require.NoError(t, err)
			plane.settleTransferRecord(t.Context(), entry, nil, time.Millisecond)
		}

		// A second of the bubble's clock is the hub's authorize tick, so every
		// frame the readers may get has reached them.
		time.Sleep(2 * time.Second)
		synctest.Wait()

		seen := watching.subjects()
		require.NotEmpty(t, seen, "the viewer heard nothing of the transfer on the printer they read")

		for _, subject := range seen {
			require.Equal(t, "device:"+first, subject,
				"the viewer heard a transfer of a printer they cannot read")
		}

		require.Empty(t, outside.subjects(), "a caller who reads no printer heard a transfer")

		watching.mu.Lock()
		defer watching.mu.Unlock()

		for _, frame := range watching.frames {
			require.Empty(t, frame.AuthID, "a transfer frame names who began it")
			require.NotContains(t, strings.ToLower(string(frame.Data)), "eve",
				"a transfer frame carries who began it")
		}
	})
}
