package app

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheDemoKeepsItsCamerasOnAKeptDirectory runs the demonstration twice on
// one directory, which is what `make demo` does on a second evening. The first
// run creates the camera rows; the second finds them there and has to write
// over its own rows rather than be refused for having created them before.
//
// The camera test beside this one runs on a fresh directory every time, so it
// could not see this: the create is fenced with If-None-Match, and a kept
// directory answers that fence with 412 on every camera.
func TestTheDemoKeepsItsCamerasOnAKeptDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runDemoUntilCamerasWatch(t, dir)
	runDemoUntilCamerasWatch(t, dir)
}

// runDemoUntilCamerasWatch starts one demonstration on dir, waits until every
// printer has its camera, and stops it.
func runDemoUntilCamerasWatch(t *testing.T, dir string) {
	t.Helper()

	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	// The demo's own lines are kept and shown when the case fails: a listing
	// that never fills is explained by the demo saying "no camera" and why,
	// which io.Discard throws away.
	said := &demoSaid{}

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the demo said:\n%s", said.text())
		}
	})

	go func() {
		done <- Demo(ctx, DemoOptions{
			APIAddr: addr, Dir: dir, Flap: 0, Cameras: true,
			Password: demoTestPassword, Out: said,
		})
	}()

	defer func() {
		cancel()
		require.NoError(t, <-done, "a demo the person stopped must stop cleanly")
	}()

	base := "http://" + addr
	reader := &demoFleet{client: &http.Client{Timeout: demoRequest}}

	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token

		return err == nil
	}, demoSettle, demoPoll, "the demo account never signed in")

	var fleet demoListing

	require.Eventually(t, func() bool {
		fleet = demoRead[demoListing](t, reader, base+"/api/v1/devices")

		return connectedIn(fleet) >= demoFleetSize
	}, demoSettle, demoPoll, "the demo never listed %d connected printers", demoFleetSize)

	// The listing the last finished poll read, shown when the case fails, for
	// the reason the camera case beside this one gives: require.Eventually
	// takes its message as ordinary arguments, so a listing named there is the
	// empty one and never what the poll found.
	var seen atomic.Pointer[demoCameraListing]

	t.Cleanup(func() {
		last := seen.Load()
		if !t.Failed() || last == nil {
			return
		}

		t.Logf("the last camera listing read was %+v, against a fleet of %d machines",
			last.Items, len(fleet.Items))
	})

	require.Eventually(t, func() bool {
		page := demoRead[demoCameraListing](t, reader, base+"/api/v1/cameras")
		seen.Store(&page)

		return watchedOnce(page, fleet)
	}, 2*demoSettle, demoPoll,
		"every machine in the fleet should have exactly one camera watching it")
}
