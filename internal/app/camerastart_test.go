package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
)

// TestEveryCameraDoesNotDialAtTheSameInstant is Wyze's trigger condition, held
// against this design.
//
// Wyze's incident was not a crypto failure and not an access-control design
// failure. It was correct behaviour under normal load and wrong behaviour
// during recovery, when every device came back at once. A node restart brings
// every supervised camera back at once by construction, so this deployment
// reproduces that condition on every restart, and the scatter before the first
// attempt is what stops it being a thundering herd at the devices and at
// whatever is behind them.
//
// The measurement is the spread of the instants the instances actually started,
// through the real registry and the real supervision plane, taking the host's
// own default policy. Removing the scatter collapses that spread and this case
// says so.
func TestEveryCameraDoesNotDialAtTheSameInstant(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	const cameras = 12

	started := newStartLog()

	types, err := builtin.Registry()
	require.NoError(t, err)

	host, err := resourcehost.New(resourcehost.Config{
		HostKeys: builtin.HostKeys,
		Registry: types,
		Logger:   discard(),
		OnStatus: started.observe,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	addCameras(t, newGraph(t), host, manyCameras(device, cameras)...)

	require.Eventually(t, func() bool {
		return started.count() == cameras
	}, settle, 20*time.Millisecond, "only %d of %d cameras started", started.count(), cameras)

	spread := started.spread()

	// The default spread is two seconds and twelve instances are drawn over it
	// uniformly, so the widest gap being under a millisecond means the draw is
	// not happening. It is far below the expected value and far above the
	// scheduler noise a start with no scatter produces.
	require.Greater(t, spread, time.Millisecond,
		"twelve cameras started within %s of each other, so a restart dials every device at once, "+
			"which is the condition earlier research records as Wyze's", spread)

	// And the scatter is bounded, because an operator restarting a node has to
	// see their cameras come back rather than trickle in.
	require.LessOrEqual(t, spread, supervisor.DefaultStartSpread+time.Second,
		"the spread is wider than the policy's window, so something other than the scatter is "+
			"delaying a start")

	// The two host settings a camera's own kind decides, read off a running
	// instance rather than off the table that produced it. No breaker, because
	// disconnection is a camera's steady state and a breaker there turns a
	// device that is merely switched off into one nothing will dial again.
	instance, supervising := host.Instance(cameraRefAt(t, 0))
	require.True(t, supervising)

	policy := instance.Policy()
	require.Positive(t, policy.Backoff.Spread,
		"a camera's first attempt is not scattered, so every camera in a deployment dials at once")
	require.Nil(t, policy.Breaker,
		"a camera has a breaker, and a camera that is merely switched off would trip it")
}

// cameraRefAt names one of the instances manyCameras declares.
func cameraRefAt(t *testing.T, index int) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindCamera, fmt.Sprintf("watch-%02d", index))
	require.NoError(t, err)

	return instance
}

// startLog records when each camera first began starting.
type startLog struct {
	mu    sync.Mutex
	first map[ref.Ref]time.Time
}

func newStartLog() *startLog { return &startLog{first: map[ref.Ref]time.Time{}} }

// observe keeps the moment each camera moved into starting.
//
// It is that transition and not the one before it. An instance is announced as
// pending the moment the plan reaches the host, which is before the scatter and
// therefore says nothing about when the device is dialled. Starting is the
// first attempt, and the scatter is the wait in front of it.
func (l *startLog) observe(status supervisor.Status) {
	if status.Ref.Kind() != ref.KindCamera || status.State != condition.StateStarting() {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, seen := l.first[status.Ref]; !seen {
		l.first[status.Ref] = time.Now()
	}
}

func (l *startLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.first)
}

// spread is the time between the earliest start and the latest.
func (l *startLog) spread() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.first) < 2 {
		return 0
	}

	instants := make([]time.Time, 0, len(l.first))
	for _, at := range l.first {
		instants = append(instants, at)
	}

	sort.Slice(instants, func(i, j int) bool { return instants[i].Before(instants[j]) })

	return instants[len(instants)-1].Sub(instants[0])
}

// manyCameras declares one deployment's worth of cameras, all pointed at one
// device and all bound to one printer, because what is measured is when each
// one starts and not what it is pointed at.
func manyCameras(device *cameraDevice, count int) []devicestore.Camera {
	out := make([]devicestore.Camera, 0, count)

	for i := range count {
		out = append(out, devicestore.Camera{
			ID:   fmt.Sprintf("watch-%02d", i),
			Type: "mjpeg",
			Config: json.RawMessage(fmt.Sprintf(`{"printer":"pfixedprinter0000000000","snapshot_url":%q}`,
				device.snapshotURL())),
		})
	}

	return out
}
