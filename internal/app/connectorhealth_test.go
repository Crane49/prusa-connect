package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestTheConnectorRollupCountsWhatTheCallerMaySee is an earlier design decision reaching a
// caller. The rollup lives in internal/platform/observability/health, and
// GET /api/v1/health reads no dependency by design, so this endpoint is the
// only surface that carries it.
//
// It is the counts that make it a health report rather than a listing. Four
// hundred entries of "fine" is 80 KB of JSON nobody reads.
func TestTheConnectorRollupCountsWhatTheCallerMaySee(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const (
		fleet = 12
		away  = 5
	)

	sessions := make([]*presenceSession, 0, fleet)
	for i := range fleet {
		sessions = append(sessions, arrivePresent(t, plane, fmt.Sprintf("SIM-%04d", i)))
	}

	for _, session := range sessions[:away] {
		session.setConnected(false)
	}

	rollups, err := plane.Connectors(t.Context(), fleetOwner)
	require.NoError(t, err)
	require.Len(t, rollups, 1, "every printer arrived through one connector")

	rollup := rollups[0]
	require.Equal(t, "test", rollup.Connector)
	require.Equal(t, fleet, rollup.Total)
	require.Equal(t, fleet-away, rollup.Counts["connected"])
	require.Equal(t, away, rollup.Counts["disconnected"])
	require.Zero(t, rollup.Counts["error"], "every state is present, at zero when empty")
	require.Len(t, rollup.Degraded, away)
	require.Zero(t, rollup.DegradedTruncated)

	// Some down but most up is degraded, which for a fleet is expected. A report
	// that said "fail" about one printer being switched off gets muted inside a
	// week.
	require.Equal(t, "degraded", rollup.Status)

	// Nothing records a transition time, so the wire says absent rather than
	// 1970: a client rendering a zero timestamp reports every device as broken
	// since then. LastSeen is the one the plane does answer, and it is what a
	// person acts on: a machine last seen a minute ago and one last seen a month
	// ago are not the same problem.
	require.Nil(t, rollup.Degraded[0].Since)
	require.NotNil(t, rollup.Degraded[0].LastSeen, "a device this process holds has been seen")
}

// TestTheRollupAnswersLastSeenBeforeTheFirstRecordIsWritten pins which half of
// the plane owns last_seen for a device this process is holding.
//
// The record is written by one goroutine on a two second timer. A rollup that
// asked the store would report that nothing had ever seen a machine whose
// frames are arriving, for as long as a flush is owed, and a device this
// process holds a session to has been seen by definition.
//
// It reads the rollup straight after the arrival on purpose, which is the
// window the store is empty in. Nothing here waits for the writer, so it races
// no goroutine and holds at -count=20.
//
// health.Roll reads the same field to decide that a connector has gone quiet.
// The plane passes no grace period today, so the answer is the reported state
// either way; a zero last_seen would make every device unknown the day one is
// configured.
func TestTheRollupAnswersLastSeenBeforeTheFirstRecordIsWritten(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	session := arrivePresent(t, plane, "SIM-UNWRITTEN")
	session.setConnected(false)

	rollups, err := plane.Connectors(t.Context(), fleetOwner)
	require.NoError(t, err)
	require.Len(t, rollups, 1)
	require.Len(t, rollups[0].Degraded, 1)

	seen := rollups[0].Degraded[0].LastSeen
	require.NotNil(t, seen, "the rollup said a device that is here has never been seen")
	require.WithinDuration(t, time.Now(), *seen, time.Minute)
}

// TestTheRollupCountsOnlyTheCallersOwnPopulation is the security half, and it
// is the reason the rollup is built per caller rather than once per process. A
// count over the whole fleet tells a caller who may see one printer that there
// are twelve, which is exactly what the printer listing refuses them.
func TestTheRollupCountsOnlyTheCallersOwnPopulation(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	const fleet = 12

	ids := make([]string, 0, fleet)
	for i := range fleet {
		ids = append(ids, arrivePresent(t, plane, fmt.Sprintf("SIM-%04d", i)).id(t, plane))
	}

	// One grant on one printer, which is all a viewer holds.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, ids[0], authz.RelViewer, operator)))

	mine, err := plane.Connectors(t.Context(), operator)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, 1, mine[0].Total, "the rollup counted printers this caller may not see")
	require.Equal(t, 1, mine[0].Counts["connected"])

	// A caller holding nothing sees no connector at all, which is the same
	// answer a fleet of none gives.
	nothing, err := plane.Connectors(t.Context(), stranger)
	require.NoError(t, err)
	require.Empty(t, nothing)
}

// TestTheRollupIsServedBesideTheLivenessProbeAndNotInsideIt is the endpoint on
// the real binary. The two paths answer different questions on purpose: the
// probe a load balancer routes on must not fail because a printer is switched
// off, which is an earlier design decision and a design requirement.
func TestTheRollupIsServedBesideTheLivenessProbeAndNotInsideIt(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	// Nothing has dialled in, so there is no connector to report and the answer
	// is an empty list rather than a 404.
	empty := running.get(t, "/api/v1/health/connectors")
	require.Equal(t, http.StatusOK, empty.status, empty.body)
	require.Contains(t, empty.body, `"items":[]`)

	dialIn(t, running, "SIM-HEALTH")

	rolled := running.get(t, "/api/v1/health/connectors")
	require.Equal(t, http.StatusOK, rolled.status, rolled.body)

	var body struct {
		Items []httpapi.ConnectorRollup `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(rolled.body), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, "xbuddy", body.Items[0].Connector)
	require.Equal(t, 1, body.Items[0].Total)
	require.Equal(t, "pass", body.Items[0].Status)

	// The liveness probe is untouched by any of it, which is the half of this
	// that a cascading readiness failure would break.
	probe := running.get(t, "/api/v1/health")
	require.Equal(t, http.StatusOK, probe.status)
	require.NotContains(t, probe.body, "connector")

	running.stopAndWait(t)
}

// TestTheConnectorRollupLeavesRetiredPrintersOut holds the rule the listing
// already holds: a retired id has stopped naming a machine, so it is not part
// of any population a verdict is taken over.
//
// One live printer beside two retired registrations is three devices with one
// connected, which is below the floor, so Settings → Server showed the
// connector red and "Failing" with two raw identifiers under it while the
// Printers page listed the one real printer and everything worked. A health
// report that names machines nobody can see is one an operator learns to
// ignore.
func TestTheConnectorRollupLeavesRetiredPrintersOut(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	arrivePresent(t, plane, "SIM-LIVE")
	gone := []*presenceSession{
		arrivePresent(t, plane, "SIM-GONE-1"),
		arrivePresent(t, plane, "SIM-GONE-2"),
	}

	// A retired machine is refused on its next call, so it is disconnected as
	// well as retired. That is the shape the deployment reported.
	retiredAt := time.Now()

	for _, session := range gone {
		session.setConnected(false)

		entry, held := plane.known(session.id(t, plane))
		require.True(t, held, "the plane holds the printer it admitted")
		entry.markRetired(retirementRemovedByOperator, retiredAt)
	}

	rollups, err := plane.Connectors(t.Context(), fleetOwner)
	require.NoError(t, err)
	require.Len(t, rollups, 1)

	rollup := rollups[0]
	require.Equal(t, 1, rollup.Total, "only the printer that still names a machine is counted")
	require.Equal(t, 1, rollup.Counts["connected"])
	require.Zero(t, rollup.Counts["disconnected"])
	require.Empty(t, rollup.Degraded, "a retired id is nobody's to look at")
	require.Equal(t, "pass", rollup.Status)
}

// stranger is a principal the graph knows nothing about.
const stranger = "user:nobody"

// arrivePresent puts one printer in the plane through a session that outlives
// its connection, which is the only kind an absence is a question about.
func arrivePresent(t *testing.T, plane *devices, serial string) *presenceSession {
	t.Helper()

	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &presenceSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
		patches:  make(chan connectorapi.StatePatch),
		changed:  make(chan struct{}, 1),
	}
	session.setConnected(true)

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { _ = session.Close() })

	return session
}

// id is the identity the plane minted for this session's printer.
func (s *presenceSession) id(t *testing.T, plane *devices) string {
	t.Helper()

	return plane.idOf(s.identity)
}

// TestTheRollupNamesAPrinterTheWayItsPageDoes is what lets the Health tab say
// which printer is away without making a person read an identifier.
func TestTheRollupNamesAPrinterTheWayItsPageDoes(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	session := arrivePresent(t, plane, "SIM-NAMED")
	session.setConnected(false)

	rollups, err := plane.Connectors(t.Context(), fleetOwner)
	require.NoError(t, err)
	require.Len(t, rollups[0].Degraded, 1)

	away := rollups[0].Degraded[0]
	require.NotEmpty(t, away.Name, "a printer nobody named still reads as its model and serial")

	entry, seen := plane.known(away.ID)
	require.True(t, seen)
	entry.setName("Desk MK4")

	rollups, err = plane.Connectors(t.Context(), fleetOwner)
	require.NoError(t, err)
	require.Equal(t, "Desk MK4", rollups[0].Degraded[0].Name)
}
