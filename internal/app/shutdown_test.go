package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAShutdownDrainsTheListenerBeforeTheResources is the ordering the whole
// shutdown exists for: a request in flight finishes against instances that are
// still up, rather than against ones being torn down underneath it.
//
// It cancels the context cmd/prusa-connect's signal handler cancels, and
// reads the order out of the logger's own output, because the ordering is not
// observable from outside the process: by the time a client sees the listener
// close, both halves have happened.
//
// It does not call t.Parallel: it replaces os.Stderr for the length of the run,
// and the go test runner holds every parallel test until the sequential ones
// have finished.
func TestAShutdownDrainsTheListenerBeforeTheResources(t *testing.T) {
	dir := t.TempDir()

	write(t, filepath.Join(dir, "config.yaml"), lines(
		"core:", "  log:", "    level: info", "    format: text",
		"resources:", "  storage:", "    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(filepath.Join(dir, "storage"))))

	addr := freeAddr(t)
	logged := captureStderr(t)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, Options{
			ConfigPath:   filepath.Join(dir, "config.yaml"),
			APIAddr:      addr,
			Version:      "test",
			LoopbackOnly: true,
		})
	}()

	require.Eventually(t, func() bool {
		request, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, "http://"+addr+"/api/v1/health", nil)
		require.NoError(t, err)

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}

		_ = response.Body.Close()

		return response.StatusCode == http.StatusOK
	}, settle, 20*time.Millisecond, "the core never answered on %s", addr)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "a cancelled core must stop cleanly")
	case <-time.After(settle):
		t.Fatal("the core did not stop")
	}

	output := logged()

	drained := strings.Index(output, "listeners drained")
	stopping := strings.Index(output, "stopping the supervised resources")
	stopped := strings.Index(output, "supervision stopped")

	require.Positive(t, drained, "the listeners never reported that they drained:\n%s", output)
	require.Positive(t, stopping, "the supervision plane was never asked to stop:\n%s", output)
	require.Positive(t, stopped, "the supervision plane never came back:\n%s", output)

	require.Less(t, drained, stopping,
		"the resources were torn down while requests could still reach them:\n%s", output)
	require.Less(t, stopping, stopped)

	// And the socket is genuinely closed, not merely reported closed.
	_, err := net.DialTimeout("tcp", addr, time.Second)
	require.Error(t, err, "the listener must be closed once the core has stopped")
}

// captureStderr redirects os.Stderr for the length of one test and returns what
// was written to it. newLogger reads os.Stderr when it builds the handler, so
// this is the only seam the composition root offers a test that needs to read
// its own log.
func captureStderr(t *testing.T) func() string {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	saved := os.Stderr
	//nolint:reassign // newLogger reads os.Stderr when it builds the handler,
	// which is the only seam a test has for reading the composition root's log.
	os.Stderr = writer

	collected := make(chan string, 1)

	go func() {
		content, _ := io.ReadAll(reader)
		collected <- string(content)
	}()

	var (
		output string
		read   bool
	)

	restore := func() string {
		if read {
			return output
		}

		//nolint:reassign // restoring what the redirection above replaced.
		os.Stderr = saved
		_ = writer.Close()
		output, read = <-collected, true

		return output
	}

	t.Cleanup(func() { restore() })

	return restore
}
