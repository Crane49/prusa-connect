package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The Prometheus exposition names every device this process has seen, and a
// scrape cannot be filtered per caller. So the decision is all or nothing, and
// what it asks is whether the caller reaches view_telemetry everywhere.
//
// The fixture is the fleet every other case here runs on, and it holds one
// principal of each interesting shape: an org admin who reaches everything, an
// org viewer with one device blocked, a service account scoped to three of the
// four, and one principal holding nothing at all.
func TestOnlyAFleetWideGrantReadsFleetWideTelemetry(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		cases := []struct {
			subject string
			want    bool
			why     string
		}{
			{"user:root", true, "an org admin reaches every device in the org"},
			{"sink:audit", true, "the fleet-wide sink holds one org viewer tuple and reaches the fleet"},
			{"user:olga", false, "an org viewer with one device blocked does not reach the whole fleet"},
			{"service_account:sa-ui", false, "three devices of four is not the fleet"},
			{"user:dana", false, "one direct grant is not the fleet"},
			{"user:mallory", false, "a principal the graph grants nothing reads nothing"},
		}

		for _, c := range cases {
			t.Run(c.subject, func(t *testing.T) {
				allowed, err := store.CanReadFleetTelemetry(t.Context(), subjectOf(t, c.subject))
				require.NoError(t, err)
				require.Equalf(t, c.want, allowed, "%s: %s", c.subject, c.why)
			})
		}
	})
}

// A deployment that has seen no device names none, so a scrape of it withholds
// nothing and is permitted. Without this the rule would refuse every caller on
// a server that has just been installed, which is the state a self-hoster
// wires their scraper up in.
func TestAnEmptyDirectoryPermitsAFleetWideRead(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		allowed, err := store.CanReadFleetTelemetry(t.Context(), subjectOf(t, "user:mallory"))
		require.NoError(t, err)
		require.True(t, allowed, "a deployment with no device in it has nothing to withhold")
	})
}

// One device arriving that the caller cannot see takes the fleet-wide answer
// away, in the same transaction that registers it. A rule that read a count
// taken earlier would keep answering yes until something else moved.
func TestOneUnreachableArrivalTakesTheFleetWideAnswer(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		allowed, err := store.CanReadFleetTelemetry(t.Context(), root)
		require.NoError(t, err)
		require.True(t, allowed, "the org admin does not reach the fixture fleet, so this case proves nothing")

		// Registered with no org edge, so nothing reaches it.
		require.NoError(t, store.RegisterDevice(t.Context(), DeviceRow{ID: "p-orphan", Name: "Orphan"}))

		allowed, err = store.CanReadFleetTelemetry(t.Context(), root)
		require.NoError(t, err)
		require.False(t, allowed,
			"a device the caller cannot see joined the directory and the fleet-wide answer survived")
	})
}
