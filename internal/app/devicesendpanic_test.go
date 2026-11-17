package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// panickingSession is a driver whose first transfer stops the goroutine it runs
// on. A driver is third-party code and a panic in it is a case this server has
// to survive, not a case it may assume away.
type panickingSession struct {
	identity connectorapi.Identity
	patches  chan connectorapi.StatePatch
	sends    atomic.Int64
}

func (s *panickingSession) Identity() connectorapi.Identity { return s.identity }

func (s *panickingSession) Capabilities() (connectorapi.Set, uint64) {
	return connectorapi.Set{connectorapi.VerbFileUpload: {Flag: true}}, 1
}

func (s *panickingSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{OK: true}, nil
}

func (s *panickingSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *panickingSession) Close() error { return nil }

func (s *panickingSession) Send(context.Context, connectorapi.Upload) error {
	if s.sends.Add(1) == 1 {
		panic("the driver read past the end of its own buffer")
	}

	return nil
}

// A driver that stops in the middle of a transfer leaves the printer free. The
// register holds one transfer per machine, so a record left behind would refuse
// every later transfer to that printer until somebody restarted this server.
func TestAPrinterIsFreeAfterItsDriverStoppedMidTransfer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	graph := newGraph(t)
	plane := planeReading(t, graph, stageOne(t, root))

	session := &panickingSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-SEND-PANIC"},
		patches:  make(chan connectorapi.StatePatch),
	}

	host := plane.Arrivals("test",
		connectorapi.Set{connectorapi.VerbFileUpload: {Flag: true}},
		capability.NewResolver(profile.Registry{}, discard()), "")

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	id := plane.idOf(session.identity)
	who := subjectOf(config.Default().Principal)
	upload := httpapi.DeviceUpload{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, who)))

	// The relay runs on a goroutine of its own, so the panic stops there and
	// the caller is told the driver stopped.
	var sendErr error

	require.NotPanics(t, func() {
		_, sendErr = plane.Send(t.Context(), who, id, upload)
	})
	require.True(t, fault.HasCode(sendErr, connectorapi.TransferFailed.Code()),
		"the caller was not told the driver stopped: %v", sendErr)

	require.Empty(t, plane.transfers.running("device:"+id),
		"the transfer the driver stopped is still filed against the printer")

	_, err = plane.Send(t.Context(), who, id, upload)
	require.False(t, fault.HasCode(err, connectorapi.TransferBusy.Code()),
		"the printer stayed busy after its driver stopped: %v", err)
	require.NoError(t, err)
}

// A driver that stops while relaying a print nobody is waiting for does not
// take this server with it. The print runs on a goroutine of the plane's own,
// and a panic reaching the top of a goroutine ends the process and every
// printer this server holds.
func TestADriverThatStopsUnderADetachedPrintDoesNotEndTheServer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	graph := newGraph(t)
	plane := planeReading(t, graph, stageOne(t, root))

	// The line the plane writes about the stopped driver is read at the end,
	// because it is the only record an operator gets of where the driver
	// stopped.
	written := &guardedText{}
	plane.log = slog.New(slog.NewTextHandler(written, nil))

	session := &panickingSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-PRINT-PANIC"},
		patches:  make(chan connectorapi.StatePatch),
	}

	host := plane.Arrivals("test",
		connectorapi.Set{connectorapi.VerbFileUpload: {Flag: true}},
		capability.NewResolver(profile.Registry{}, discard()), "")

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	id := plane.idOf(session.identity)
	who := subjectOf(config.Default().Principal)
	request := httpapi.DevicePrint{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, who)))

	// The answer comes while the file is still moving, so the driver stops
	// after this call has returned.
	_, err = plane.StartPrint(t.Context(), who, id, request)
	require.NoError(t, err)

	// The test process is still running, which is the finding. What is left is
	// that the transfer and the place it held were both given back.
	require.Eventually(t, func() bool {
		return len(plane.transfers.running("device:"+id)) == 0
	}, 5*time.Second, 10*time.Millisecond,
		"the transfer the driver stopped is still filed against the printer")

	require.Eventually(t, func() bool {
		return plane.transfers.claimDetached()
	}, 5*time.Second, 10*time.Millisecond,
		"the place the stopped print held was never given back")

	plane.transfers.releaseDetached()

	_, err = plane.StartPrint(t.Context(), who, id, request)
	require.False(t, fault.HasCode(err, connectorapi.TransferBusy.Code()),
		"the printer stayed busy after its driver stopped: %v", err)

	// The relay closes the transfer before the goroutine that recovers the
	// panic writes its line. The checks above can pass in that gap, so the
	// test waits for the line instead of reading the log once.
	var logged string

	require.Eventually(t, func() bool {
		logged = written.text()

		return strings.Contains(logged, "a driver stopped while relaying a print")
	}, 5*time.Second, 10*time.Millisecond, "the plane never logged the stopped driver")
	require.Contains(t, logged, "the driver read past the end of its own buffer")
	require.Contains(t, logged, "runtime/debug.Stack",
		"the line names no stack, so nothing says which driver call stopped")
}

// guardedText collects what a logger writes from another goroutine, so a test
// can read it once the work that wrote it has finished.
type guardedText struct {
	mu   sync.Mutex
	held bytes.Buffer
}

func (g *guardedText) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.held.Write(p)
}

func (g *guardedText) text() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.held.String()
}

// Closing the record of a stopped transfer can stop too, on a server whose
// event hub is already gone. That second failure is written to the log and
// goes no further, so the caller can raise the driver's own panic, which is
// the one that says what broke.
func TestAFailureToCloseAStoppedTransferGoesNoFurther(t *testing.T) {
	t.Parallel()

	plane := planeOn(t, newGraph(t))

	entry, err := plane.transfers.begin(queued("01SETTLE", "device:nobody"), "", nil)
	require.NoError(t, err)

	// The hub every frame is written to is taken away, so closing the record
	// stops on the way out.
	plane.hub = nil

	require.NotPanics(t, func() {
		plane.settleAfterPanic(t.Context(), entry, "usb/part.gcode", time.Millisecond)
	})

	require.Empty(t, plane.transfers.running("device:nobody"),
		"the printer stayed busy after the record could not be closed")
}
