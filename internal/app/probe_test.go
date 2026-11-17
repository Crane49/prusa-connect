package app

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheProbeReachesARunningCore is the container's health check against the
// real thing. The image carries no curl, so this function is the only thing
// standing between a running core and a container reported unhealthy for ever.
//
// It also holds ProbePath against the surface: the path is written out here
// rather than shared with the server, so a path that stopped being served fails
// here rather than in a health check nobody reads until a deployment stalls.
func TestTheProbeReachesARunningCore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	running := start(t, dir, lines("core:", "  log:", "    level: error"))

	// The body as well as the status. A 200 carrying anything other than the
	// readiness report is a probe pointed at the wrong path, and a status alone
	// cannot tell the two apart.
	answered := running.get(t, ProbePath)
	require.Equal(t, http.StatusOK, answered.status, "the probe asks for a path this server does not serve")
	require.Contains(t, answered.body, `"ready"`, "the probe asks for something that is not the readiness report")

	require.NoError(t, Probe(t.Context(), options(dir, running.addr)))

	running.stopAndWait(t)
}

// TestTheProbeFailsWhenNothingIsListening is the other half. A probe that
// cannot tell a running core from a dead one reports every container healthy,
// which is the same as having no health check at all.
func TestTheProbeFailsWhenNothingIsListening(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), lines("core:", "  log:", "    level: error"))

	// A port nothing is on: freeAddr binds one, reads the address and gives it
	// back.
	err := Probe(t.Context(), options(dir, freeAddr(t)))

	require.Error(t, err, "a probe of an address nothing serves must fail")
	require.Contains(t, err.Error(), ProbePath, "the failure names what was asked for")
}

// TestTheProbeFailsWhenARequiredResourceIsNotServing is why the probe asks for
// readiness rather than liveness. A process that is up and cannot serve is not
// one a caller should be sent to, and the health check is what says so.
func TestTheProbeFailsWhenARequiredResourceIsNotServing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A filesystem storage whose root cannot be created: the path runs through
	// a file rather than a directory, so the instance never reaches serving.
	blocked := filepath.Join(dir, "occupied")
	write(t, blocked, "not a directory")

	running := start(t, dir, lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		"  storage:",
		"    blocked:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(filepath.Join(blocked, "root")),
	))

	require.Equal(t, http.StatusServiceUnavailable, running.get(t, ProbePath).status)
	require.Error(t, Probe(t.Context(), options(dir, running.addr)),
		"a core that cannot serve must fail its own health check")

	running.stopAndWait(t)
}

// options is what the command hands over for a probe of the core in dir.
func options(dir, addr string) Options {
	return Options{ConfigPath: filepath.Join(dir, "config.yaml"), APIAddr: addr, LoopbackOnly: true}
}
