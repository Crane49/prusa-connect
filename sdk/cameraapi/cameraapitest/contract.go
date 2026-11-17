// Package cameraapitest provides the conformance suite every cameraapi.Camera
// must pass, so a driver written by a community author behaves the way the
// media plane needs it to and the built-in ones do not get to be special.
//
// The clauses it holds are the ones a host cannot work around. A declared
// format set that changes under the host makes negotiation meaningless; a
// snapshot whose media type is not the one its format means is a media type a
// browser acts on; a stream whose Read does not return when the host closes it
// is a goroutine per abandoned viewer; and a Close that does not stop the
// stream is a device socket held for the life of the process.
//
// Usage from a driver's _test.go:
//
//	func TestContract(t *testing.T) {
//	    device := somefixture.Start(t)
//	    cameraapitest.RunCameraContract(t, func(t *testing.T) cameraapi.Camera {
//	        c, err := New(cameraapi.ProviderSpec{Type: Type, Printer: "p", Config: map[string]any{
//	            "snapshot_url": device.SnapshotURL(),
//	        }})
//	        if err != nil { t.Fatal(err) }
//	        return c
//	    })
//	}
//
// The device is started once, outside newCamera, because one subtest compares
// the live goroutines across a build and a Close and a fixture started inside
// newCamera would be read as the driver's own.
package cameraapitest

import (
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
)

const (
	// closeBudget is the longest a Close may take to make a blocked Read
	// return. A driver that closes its transport releases the reader at once;
	// one that waits for the device to notice does not.
	closeBudget = 5 * time.Second

	// concurrentCallers and callsPerCaller drive the race case. A host serving
	// a thumbnail grid and a live view calls one camera from several
	// goroutines at once.
	concurrentCallers = 8
	callsPerCaller    = 20

	// goroutineSettle is how long the leak case waits for a driver's own
	// goroutines to finish after Close before it counts them.
	goroutineSettle = 500 * time.Millisecond
	// settleStep is how often that wait looks again.
	settleStep = 10 * time.Millisecond
	// settleSamples is how many identical readings in a row mean the runtime
	// has stopped moving, which is what the baseline is taken from.
	settleSamples = 3
)

// NewCamera builds the camera under test. It is called once per subtest, so a
// driver that cannot be built twice fails here rather than intermittently.
type NewCamera func(t *testing.T) cameraapi.Camera

// RunCameraContract runs every clause of cameraapi.Camera against newCamera.
func RunCameraContract(t *testing.T, newCamera NewCamera) {
	t.Helper()

	t.Run("the declared format set is stable and known", func(t *testing.T) {
		formatsAreStable(t, newCamera)
	})

	t.Run("an undeclared format is refused as unsupported", func(t *testing.T) {
		undeclaredFormatIsRefused(t, newCamera)
	})

	t.Run("a snapshot is servable or says it has no frame", func(t *testing.T) {
		snapshotIsServable(t, newCamera)
	})

	t.Run("closing a stream releases a blocked read", func(t *testing.T) {
		closingAStreamReleasesTheReader(t, newCamera)
	})

	t.Run("concurrent callers are answered", func(t *testing.T) {
		concurrentCallersAreAnswered(t, newCamera)
	})

	t.Run("close is idempotent and leaves no goroutine", func(t *testing.T) {
		closeIsIdempotentAndLeavesNoGoroutine(t, newCamera)
	})
}

// formatsAreStable holds the negotiation vocabulary. A set that is empty, names
// something outside the closed set, or changes between two calls is a set the
// host cannot negotiate over.
func formatsAreStable(t *testing.T, newCamera NewCamera) {
	t.Helper()

	camera := newCamera(t)
	defer closeQuietly(camera)

	first := camera.Formats()
	if err := cameraapi.ValidateFormats(first); err != nil {
		t.Fatalf("Formats() = %v: %v", first, err)
	}

	second := camera.Formats()
	if !slices.Equal(first, second) {
		t.Fatalf("Formats() returned %v then %v: a declared set the host cannot rely on is not a "+
			"negotiation vocabulary", first, second)
	}

	// The returned slice is the host's to hold. A driver handing back its own
	// backing array lets one host's sort reorder another's copy.
	if len(first) > 0 {
		first[0] = "mutated-by-the-host"

		if got := camera.Formats(); got[0] == "mutated-by-the-host" {
			t.Fatal("Formats() hands back the driver's own slice, so a caller can rewrite the " +
				"declared set")
		}
	}
}

// undeclaredFormatIsRefused holds an earlier design decision: a format the camera does not
// emit is reported as unsupported rather than converted to.
func undeclaredFormatIsRefused(t *testing.T, newCamera NewCamera) {
	t.Helper()

	camera := newCamera(t)
	defer closeQuietly(camera)

	declared := camera.Formats()

	var missing cameraapi.Format

	for _, format := range cameraapi.Formats() {
		if !slices.Contains(declared, format) {
			missing = format

			break
		}
	}

	if missing == "" {
		t.Skip("this camera declares every format in the vocabulary, so there is none to refuse")
	}

	stream, err := camera.Open(t.Context(), missing)
	if err == nil {
		_ = stream.Body.Close()

		t.Fatalf("Open(%q) succeeded for a format Formats() does not declare", missing)
	}

	if !errors.Is(err, cameraapi.ErrFormatNotSupported) {
		t.Fatalf("Open(%q) reported %v, and a host tells an unsupported format from a failure with "+
			"errors.Is(err, cameraapi.ErrFormatNotSupported)", missing, err)
	}
}

// snapshotIsServable holds the one clause a host cannot check without serving
// the bytes: a still image is JPEG, or the camera says it has no frame.
func snapshotIsServable(t *testing.T, newCamera NewCamera) {
	t.Helper()

	camera := newCamera(t)
	defer closeQuietly(camera)

	if !slices.Contains(camera.Formats(), cameraapi.FormatJPEG) {
		t.Skip("this camera declares no still image format")
	}

	shot, err := camera.Snapshot(t.Context())
	if errors.Is(err, cameraapi.ErrNoFrame) {
		return
	}

	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}

	if err = shot.Validate(); err != nil {
		t.Fatalf("Snapshot() returned an image the host cannot serve: %v", err)
	}

	if shot.Taken.IsZero() {
		t.Fatal("Snapshot().Taken is zero, so every conditional GET against this camera transfers " +
			"the whole frame again")
	}
}

// closingAStreamReleasesTheReader is the clause that decides what an abandoned
// viewer costs. The host closes the body when the reader goes away, and a
// driver whose Read does not return then leaves one goroutine per viewer that
// ever left.
func closingAStreamReleasesTheReader(t *testing.T, newCamera NewCamera) {
	t.Helper()

	camera := newCamera(t)
	defer closeQuietly(camera)

	format, ok := streamable(camera)
	if !ok {
		t.Skip("this camera declares no streaming format")
	}

	stream, err := camera.Open(t.Context(), format)
	if err != nil {
		t.Fatalf("Open(%q) = %v", format, err)
	}

	if stream.MediaType != format.MediaType() {
		_ = stream.Body.Close()

		t.Fatalf("Open(%q) framed the stream as %q, and the format means %q",
			format, stream.MediaType, format.MediaType())
	}

	returned := make(chan struct{})

	go func() {
		defer close(returned)

		_, _ = io.Copy(io.Discard, stream.Body)
	}()

	if err = stream.Body.Close(); err != nil {
		t.Fatalf("closing the stream body: %v", err)
	}

	select {
	case <-returned:
	case <-time.After(closeBudget):
		t.Fatalf("a Read was still blocked %s after the stream was closed, so an abandoned viewer "+
			"leaves a goroutine behind", closeBudget)
	}
}

// concurrentCallersAreAnswered holds the concurrency clause. Every call is
// allowed to fail; none may deadlock or panic.
func concurrentCallersAreAnswered(t *testing.T, newCamera NewCamera) {
	t.Helper()

	camera := newCamera(t)
	defer closeQuietly(camera)

	ctx, cancel := context.WithTimeout(t.Context(), closeBudget)
	defer cancel()

	var wait sync.WaitGroup

	for range concurrentCallers {
		wait.Go(func() {
			for range callsPerCaller {
				_, _ = camera.Snapshot(ctx)
				_ = camera.Formats()
			}
		})
	}

	finished := make(chan struct{})

	go func() {
		wait.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(closeBudget):
		t.Fatalf("%d concurrent callers did not finish within %s", concurrentCallers, closeBudget)
	}
}

// closeIsIdempotentAndLeavesNoGoroutine holds the lifecycle clause. The host
// closes an instance once, and a driver that panics on a second Close turns a
// reconfiguration into a crash; one that leaves a goroutine turns every
// reconfiguration into a leak.
//
// The camera is used before it is closed. A driver starts its goroutines on the
// first snapshot or the first viewer, not at construction, so a case that built
// and closed without asking for anything would count nothing and pass whatever
// the driver does.
func closeIsIdempotentAndLeavesNoGoroutine(t *testing.T, newCamera NewCamera) {
	t.Helper()

	before := settledGoroutines()

	camera := newCamera(t)

	_, _ = camera.Snapshot(t.Context())

	if format, ok := streamable(camera); ok {
		if stream, err := camera.Open(t.Context(), format); err == nil {
			_, _ = stream.Body.Read(make([]byte, 1))
			_ = stream.Body.Close()
		}
	}

	if err := camera.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	// A second Close is what a host does when a stop races a reconcile.
	_ = camera.Close()

	deadline := time.Now().Add(goroutineSettle)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(settleStep)
	}

	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines went from %d to %d across build and Close, so a reconfiguration leaks",
			before, after)
	}
}

// settledGoroutines is the goroutine count once the runtime has stopped moving.
//
// The naive baseline is runtime.NumGoroutine() taken on entry, and it makes
// this clause miss the leak it exists to catch. The clause before this one runs
// eight concurrent callers, and those goroutines are still winding down when
// this one starts. Two of them exiting while one leaks reads as a count that
// fell, so the leak is reported as clean. That is a false negative in a leak
// detector, which is the one direction it may never fail in.
//
// Waiting for the count to hold steady removes the wind-down. It is not proof
// the runtime is quiet, and it does not have to be: what it removes is the
// churn this suite creates itself, which is the churn that masks the leak.
func settledGoroutines() int {
	last := runtime.NumGoroutine()
	steady := 0

	for range settleSamples * 4 {
		time.Sleep(settleStep)

		now := runtime.NumGoroutine()
		if now == last {
			steady++
			if steady == settleSamples {
				return now
			}

			continue
		}

		last = now
		steady = 0
	}

	return last
}

// streamable is the first declared format that has a media type, which is the
// set a host can open a stream in.
func streamable(camera cameraapi.Camera) (cameraapi.Format, bool) {
	for _, format := range camera.Formats() {
		if format == cameraapi.FormatMJPEG {
			return format, true
		}
	}

	return "", false
}

func closeQuietly(camera cameraapi.Camera) { _ = camera.Close() }
