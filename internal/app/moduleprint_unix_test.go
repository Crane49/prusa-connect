//go:build unix

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// A FIFO in the print folder is refused without being opened. Opening one
// for reading waits for a writer, and would hold the print until one came.
func TestAPrintRefusesAFifoInThePrintFolder(t *testing.T) {
	t.Parallel()

	graph, plane, id := planeGranted(t, "SIM-PRINT-DATA-FIFO")
	fleet := modulePrinting(t, graph, plane,
		authz.ActionObserve, authz.ActionOperate, authz.ActionReadStorage)
	fleet.data = dataHolding(t)

	fifo := filepath.Join(fleet.data, "print", "pipe.gcode")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	refused := make(chan error, 1)

	go func() {
		_, err := fleet.Print(t.Context(), fromData(id, "pipe.gcode"))
		refused <- err
	}()

	select {
	case err := <-refused:
		require.ErrorIs(t, err, deviceapi.ErrNoFile)
	case <-time.After(5 * time.Second):
		// Opening the FIFO for both ends releases the reader the print left
		// waiting, so the test binary can end.
		if release, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			_ = release.Close()
		}

		t.Fatal("the print waited on a FIFO")
	}
}
