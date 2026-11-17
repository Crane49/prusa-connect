package mini404

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubBudget covers one launch plus its teardown. The launch alone is
// earlyExitWindow, so this is that plus room for the reap.
const stubBudget = 30 * time.Second

// A stub in place of the emulator. It proves the lifecycle without a build:
// Launch's early-exit window, the output ring, Wait and Close all work on any
// process. What it cannot prove is that the argv boots firmware, which is what
// tools/mini404smoke is for.
func TestAStubProcessIsLaunchedWaitedOnAndClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub is a shell script")
	}
	t.Parallel()

	dir := socketDir(t)
	cfg := stubConfig(t, dir, "echo \"stub up\"\nsleep 60\n")

	ctx, cancel := context.WithTimeout(t.Context(), stubBudget)
	defer cancel()

	printer, err := Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("launch the stub: %v", err)
	}

	select {
	case <-printer.Wait():
		t.Fatalf("the stub exited during startup:\n%s", printer.OutputTail())
	default:
	}
	if port := printer.HTTPPort(); port == 0 {
		t.Error("the printer reports no forwarded port")
	}
	if tail := printer.OutputTail(); !strings.Contains(tail, "stub up") {
		t.Errorf("the output tail does not carry the stub's own output: %q", tail)
	}

	// The stub has no QMP socket, so Close falls through to the kill. That path
	// is the one that matters: a guest that stopped answering still has to be
	// reaped.
	if closeErr := printer.Close(ctx); closeErr != nil {
		t.Fatalf("close the stub: %v", closeErr)
	}
	select {
	case <-printer.Wait():
	default:
		t.Error("Close returned with the process still running")
	}
	if closeErr := printer.Close(ctx); closeErr != nil {
		t.Errorf("a second Close is not idempotent: %v", closeErr)
	}

	log, err := os.ReadFile(filepath.Join(dir, "qemu.log"))
	if err != nil {
		t.Fatalf("read the qemu log: %v", err)
	}
	if !strings.Contains(string(log), "stub up") {
		t.Errorf("the log on disk does not carry the output: %q", log)
	}
}

func TestAProcessThatExitsAtOnceIsRetriedAndThenRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("three launch attempts are three early-exit windows")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the stub is a shell script")
	}
	t.Parallel()

	cfg := stubConfig(t, socketDir(t), "echo \"bind failed\" >&2\nexit 1\n")

	ctx, cancel := context.WithTimeout(t.Context(), stubBudget)
	defer cancel()

	_, err := Launch(ctx, cfg)
	if err == nil {
		t.Fatal("a process that exits at once was reported as launched")
	}
	for _, want := range []string{"did not stay up", "attempt 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not carry %q: %v", want, err)
		}
	}
}

// stubConfig writes a shell script that ignores the QEMU arguments and runs
// body, and returns a Config pointing at it.
func stubConfig(t *testing.T, dir, body string) Config {
	t.Helper()

	stub := filepath.Join(t.TempDir(), "stub-qemu")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("write the stub: %v", err)
	}

	return Config{
		QEMUPath:     stub,
		Machine:      "prusa-mk4-034",
		FirmwarePath: "firmware.bin",
		EEPROMBank1:  filepath.Join(dir, "b1.bin"),
		EEPROMBank2:  filepath.Join(dir, "b2.bin"),
		XFlashPath:   filepath.Join(dir, "x.bin"),
		Dir:          dir,
		Logger:       slog.New(slog.DiscardHandler),
	}
}

// socketDir is a new directory short enough to hold the QMP socket. The
// temporary directory macOS gives a test is too deep for the limit on a unix
// socket path, so on every system but Windows the directory goes under /tmp.
func socketDir(t *testing.T) string {
	t.Helper()

	root := ""
	if runtime.GOOS != "windows" {
		root = "/tmp"
	}

	dir, err := os.MkdirTemp(root, "mini404-") //nolint:usetesting // t.TempDir is under $TMPDIR, too deep on macOS.
	if err != nil {
		t.Fatalf("make a socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}
