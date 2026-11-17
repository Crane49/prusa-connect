package storageapitest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// bufferingEnv makes the test binary run the streaming case against a provider
// that reads the whole object into memory. Breaching the bound kills the process
// it happens in, so the only way to assert that the bound is real is to watch a
// second process die of it.
const bufferingEnv = "STORAGEAPITEST_RUN_BUFFERING"

// TestStreamingPassesAProviderThatStreams is the half that keeps the case
// honest in the other direction: a bound nothing can satisfy would be reported
// as a failing provider rather than as a broken suite.
func TestStreamingPassesAProviderThatStreams(t *testing.T) {
	RunStreaming(t, func(t *testing.T) Subject {
		root := t.TempDir()

		return Subject{Provider: &unconfined{root: root}, Root: root}
	})
}

// TestStreamingCatchesABufferingProvider runs the case against a provider
// written the way earlier research bans, io.ReadAll onto the upload, and requires the
// bound to kill it.
//
// Without this the case would be a test that streams a large file, which passes
// on a machine with memory to spare whether or not the provider buffers. What is
// under test here is the bound, not the transfer.
func TestStreamingCatchesABufferingProvider(t *testing.T) {
	if os.Getenv(bufferingEnv) == "1" {
		RunStreaming(t, func(t *testing.T) Subject {
			root := t.TempDir()

			return Subject{Provider: &buffering{unconfined: &unconfined{root: root}}, Root: root}
		})

		return
	}

	if testing.Short() {
		t.Skip("streaming half a gigabyte through the provider is not a short test")
	}

	// childEnv goes in too, so the spawned process runs the transfer under the
	// watchdog rather than spawning one of its own.
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestStreamingCatchesABufferingProvider$", "-test.v")
	cmd.Env = append(os.Environ(), bufferingEnv+"=1", childEnv+"=1")

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the bound let a provider read %s into memory:\n%s", bytesAs(transferBytes), indent(string(out)))
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != exitOverBudget {
		t.Fatalf("the buffering provider failed for some other reason (%v), so this case is not "+
			"evidence that the bound is what stops it:\n%s", err, indent(string(out)))
	}

	if !strings.Contains(string(out), "over the "+bytesAs(heapBound)+" bound") {
		t.Errorf("the process died at exit %d without saying the heap bound was what killed it:\n%s",
			exitOverBudget, indent(string(out)))
	}

	t.Logf("the buffering provider was killed at the bound:\n%s", indent(string(out)))
}

// buffering is a provider that holds the whole upload in memory before writing
// it, which earlier research names as the defect ("ban io.ReadAll"). It is not a
// reference implementation.
type buffering struct{ *unconfined }

func (p *buffering) Create(ctx context.Context, relPath string) (storageapi.Writer, error) {
	inner, err := p.unconfined.Create(ctx, relPath)
	if err != nil {
		return nil, err
	}

	return &bufferingWrite{inner: inner}, nil
}

// bufferingWrite keeps every byte until Commit, which is the shape a store with
// no staging area reaches for and the one the heap bound has to catch.
type bufferingWrite struct {
	inner storageapi.Writer
	held  bytes.Buffer
}

func (w *bufferingWrite) Write(p []byte) (int, error) { return w.held.Write(p) }

func (w *bufferingWrite) Commit(ctx context.Context) (storageapi.WriteResult, error) {
	if _, err := io.Copy(w.inner, bytes.NewReader(w.held.Bytes())); err != nil {
		return storageapi.WriteResult{}, err
	}

	return w.inner.Commit(ctx)
}

func (w *bufferingWrite) Abort(ctx context.Context) error { return w.inner.Abort(ctx) }
