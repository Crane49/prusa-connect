package health_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/observability/health"
)

// now is a fixed instant, so a case that depends on a grace period does not
// depend on how long the test took to run.
var now = time.Date(2026, time.August, 25, 9, 0, 0, 0, time.UTC)

// A whole farm produces one entry, whatever its size.
func TestARollupIsCountsAndACappedList(t *testing.T) {
	t.Parallel()

	const (
		fleet   = 400
		offline = 30
	)

	devices := make([]health.Device, 0, fleet)

	for i := range fleet {
		device := health.Device{
			ID:       fmt.Sprintf("mk4-%03d", i),
			State:    health.StateConnected,
			LastSeen: now,
		}

		if i < offline {
			device.State = health.StateDisconnected
			device.Reason = "DialTimeout"
			device.Since = now.Add(-time.Duration(i) * time.Minute)
		}

		devices = append(devices, device)
	}

	rollup := health.Roll("xbuddy", now, health.Policy{}, devices)

	if rollup.Status != health.StatusDegraded {
		t.Errorf("status is %q, want degraded: 30 of 400 offline is normal operations", rollup.Status)
	}

	if got := rollup.Counts[health.StateConnected]; got != fleet-offline {
		t.Errorf("connected count is %d, want %d", got, fleet-offline)
	}

	if len(rollup.Degraded) != health.DefaultCap {
		t.Errorf("the degraded list is %d entries, want the cap of %d",
			len(rollup.Degraded), health.DefaultCap)
	}

	if rollup.DegradedTruncated != offline-health.DefaultCap {
		t.Errorf("truncated is %d, want %d", rollup.DegradedTruncated, offline-health.DefaultCap)
	}

	if rollup.Total != fleet {
		t.Errorf("total is %d, want %d", rollup.Total, fleet)
	}
}

// The first entry is the one to look at, and it does not move between polls.
func TestTheDegradedListLeadsWithTheLongestBroken(t *testing.T) {
	t.Parallel()

	devices := []health.Device{
		{ID: "xl-004", State: health.StateDisconnected, Since: now.Add(-time.Minute), LastSeen: now},
		{ID: "mk4-017", State: health.StateError, Since: now.Add(-time.Hour), LastSeen: now},
		{ID: "mk4-002", State: health.StateConnecting, Since: now.Add(-time.Hour), LastSeen: now},
	}

	rollup := health.Roll("xbuddy", now, health.Policy{}, devices)

	want := []string{"mk4-002", "mk4-017", "xl-004"}
	for i, id := range want {
		if rollup.Degraded[i].ID != id {
			t.Fatalf("the degraded order is %v, want %v", ids(rollup.Degraded), want)
		}
	}
}

// A device whose connector has nothing recent to say about it is reported as
// unknown.
func TestADeviceUnheardFromIsUnknownRatherThanConnected(t *testing.T) {
	t.Parallel()

	const heartbeat = 30 * time.Second

	devices := []health.Device{
		{ID: "fresh", State: health.StateConnected, LastSeen: now.Add(-heartbeat)},
		{ID: "stale", State: health.StateConnected, LastSeen: now.Add(-5 * time.Minute)},
		{ID: "never", State: health.StateConnected},
	}

	rollup := health.Roll("xbuddy", now, health.Policy{Grace: 3 * heartbeat}, devices)

	if got := rollup.Counts[health.StateUnknown]; got != 2 {
		t.Errorf("unknown count is %d, want 2", got)
	}

	if got := rollup.Counts[health.StateConnected]; got != 1 {
		t.Errorf("connected count is %d, want 1", got)
	}
}

func TestTheVerdictIsAThresholdRatherThanAnyFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		connected int
		total     int
		floor     float64
		want      health.Status
	}{
		{name: "everything connected", connected: 400, total: 400, want: health.StatusPass},
		{name: "one printer unplugged", connected: 399, total: 400, want: health.StatusDegraded},
		{name: "half the fleet", connected: 200, total: 400, want: health.StatusDegraded},
		{name: "below the floor", connected: 199, total: 400, want: health.StatusFail},
		{name: "everything down", connected: 0, total: 400, want: health.StatusFail},
		{name: "no devices at all", connected: 0, total: 0, want: health.StatusPass},
		{name: "a stricter floor", connected: 399, total: 400, floor: 0.999, want: health.StatusFail},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			devices := make([]health.Device, 0, testCase.total)
			for i := range testCase.total {
				state := health.StateDisconnected
				if i < testCase.connected {
					state = health.StateConnected
				}

				devices = append(devices, health.Device{
					ID: fmt.Sprintf("p-%03d", i), State: state, LastSeen: now,
				})
			}

			rollup := health.Roll("xbuddy", now, health.Policy{Floor: testCase.floor}, devices)
			if rollup.Status != testCase.want {
				t.Fatalf("status is %q, want %q", rollup.Status, testCase.want)
			}
		})
	}
}

// A client never has to tell a zero count from a missing key.
func TestAnEmptyFleetReportsEveryState(t *testing.T) {
	t.Parallel()

	rollup := health.Roll("xbuddy", now, health.Policy{}, nil)

	for _, state := range []health.State{
		health.StateConnected, health.StateConnecting, health.StateDisconnected,
		health.StateError, health.StateUnknown,
	} {
		if _, ok := rollup.Counts[state]; !ok {
			t.Errorf("%q is missing from the counts", state)
		}
	}

	if len(rollup.Degraded) != 0 {
		t.Errorf("the degraded list is %v, want empty", ids(rollup.Degraded))
	}
}

func ids(devices []health.Device) []string {
	out := make([]string, 0, len(devices))
	for _, device := range devices {
		out = append(out, device.ID)
	}

	return out
}
