package simulator_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestAPeerThatStopsReadingParksTwoWritersAndReleasesThemOnCancel is what
// observes the simulator's one shared write lock.
//
// Three goroutines write behind writeMu: the telemetry loop, the read loop
// answering a command, and the transfer resume. A real socket buffers, so one
// that is not being read keeps taking bytes and no test has ever put two of
// them on the lock at once. net.Pipe buffers nothing, so a peer that stops
// reading parks the first writer inside the write and the second on the lock.
//
// The shape is safe and this says why: the goroutine watching the context
// closes the connection, which is what returns the parked write. Without that
// watcher a fleet test would hang rather than fail, and a hung run is a
// timeout nobody can read.
func TestAPeerThatStopsReadingParksTwoWritersAndReleasesThemOnCancel(t *testing.T) {
	t.Parallel()

	sessions := make(chan string, 1)

	host := newHost(t)
	host.quiet = make(chan struct{})
	host.onSession = func(fingerprint string) { sessions <- fingerprint }

	// Closed from the start: the host serves the upgrade and reads nothing of
	// the session, so the printer's first frame has no reader at all.
	close(host.quiet)

	printer, err := simulator.New(simulator.Config{
		Model:          "MK4",
		BaseURL:        host.baseURL(),
		Dial:           host.dial,
		IdlePeriod:     10 * time.Millisecond,
		PrintingPeriod: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	defer host.stop()

	ctx, cancel := context.WithCancel(t.Context())

	if registerErr := printer.Register(ctx); registerErr != nil {
		t.Fatalf("register: %v", registerErr)
	}

	done := make(chan error, 1)
	go func() { done <- printer.Run(ctx) }()

	fingerprint := <-sessions

	// The first printer frame blocks inside its write, which can begin before
	// the session is reported.
	awaitWriters(t, 1)

	// A command the read loop has to answer, which is the second writer. It
	// reaches the lock the telemetry frame is holding.
	if _, sendErr := host.send(fingerprint, "SEND_INFO", nil); sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	parked := awaitWriters(t, 2)
	t.Logf("observed: %d printer goroutines stand in writeText behind one writeMu", parked)

	cancel()

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Errorf("Run returned %v, want nil on a cancelled context", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the context ended, so a write behind writeMu never came back")
	}
}

// awaitWriters waits until want goroutines stand in Printer.writeText, and
// reports how many it found. The stacks are the only place the lock is visible:
// one goroutine is inside the write and the rest are on the mutex, and neither
// state is reachable from the package's own API.
func awaitWriters(t *testing.T, want int) int {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for {
		buffer := make([]byte, 1<<20)
		stacks := string(buffer[:runtime.Stack(buffer, true)])

		if found := strings.Count(stacks, "simulator.(*Printer).writeText"); found >= want {
			return found
		}

		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d goroutines reached writeText, so nothing was parked on writeMu", want)
		}

		time.Sleep(time.Millisecond)
	}
}
