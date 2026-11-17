package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

func TestUpgradeTrialBlocksOnAMissingBundledExtension(t *testing.T) {
	t.Setenv(upgradeTrialExtensionsEnvironment, "sink:spools")

	got := (readiness{}).verdictOf(resource.Document{})
	require.False(t, got.Ready)
	require.Equal(t, []string{"sink:spools"}, got.Blocked)
}

// TestReadinessIsServedAndMoves is an earlier design decision as a load balancer sees it, over
// the real binary. A required instance that cannot be constructed answers 503
// and names itself; the same process answers 200 once the reason it could not
// start goes away, with no restart and no reconfiguration.
func TestReadinessIsServedAndMoves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A regular file is not a directory, so the built-in storage type refuses
	// to construct against it until the file is gone.
	blocked := filepath.Join(dir, "storage")
	write(t, blocked, "")

	running := start(t, dir, lines(
		"core:", "  log:", "    level: error",
		"resources:", "  storage:", "    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(blocked)))

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/readiness").status == http.StatusServiceUnavailable
	}, settle, 20*time.Millisecond, "a required instance that will not start must take the process out of rotation")

	unready := running.get(t, "/api/v1/readiness")
	report := readinessOf(t, unready)

	require.False(t, report.Ready)
	require.Equal(t, []string{"storage:local"}, report.Blocked, "the body names what to look at")
	require.Len(t, report.Instances, 1)
	require.True(t, report.Instances[0].Required)
	require.False(t, report.Instances[0].Serving)

	// Liveness is a different question and answers it differently: the process
	// is serving correctly and refusing traffic on purpose.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)

	require.NoError(t, os.Remove(blocked))

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "the supervisor retried and readiness never followed it")

	ready := readinessOf(t, running.get(t, "/api/v1/readiness"))
	require.True(t, ready.Ready)
	require.NotNil(t, ready.Blocked, "a ready process sends an empty list, not null")
	require.Empty(t, ready.Blocked)
	require.Equal(t, 1, ready.Serving)

	running.stopAndWait(t)
}

// TestReadinessTakesNoCredential is what makes it usable by the thing that
// reads it. A load balancer holds none.
func TestReadinessTakesNoCredential(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, lines("core:", "  log:", "    level: error"))

	probe := running.get(t, "/api/v1/readiness")
	require.Equal(t, http.StatusOK, probe.status, probe.body)

	// The resource routes on the same server do want one, so the readiness
	// answer is not a server that lets everything through.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/resources").status,
		"this deployment has no issuer, so it serves every request as one subject")

	running.stopAndWait(t)
}

// TestReadinessNamesNoInstanceToACallerWithNoGrant holds the one property that
// makes this endpoint safe to serve without a credential. The verdict and the
// three counts describe the process; blocked and instances name resources, and
// naming a resource here would answer, to anybody, the question every other
// surface refuses to answer.
//
// The second start is what produces a caller the graph grants nothing. The
// first start bootstraps a grant for its own core.principal, every grant is
// withdrawn between the two, and a restart under the same principal hands
// nothing back, so the second start is a running server whose caller holds
// nothing. That is the same shape as a deployment with an issuer where the
// request carries no credential.
func TestReadinessNamesNoInstanceToACallerWithNoGrant(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "storage")
	require.NoError(t, os.MkdirAll(root, 0o755))

	declared := func(principal string) string { return requiredStorage(principal, root) }

	granted := start(t, dir, declared("operator"))

	require.Eventually(t, func() bool {
		return granted.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "the declared instance never came up")

	held := readinessOf(t, granted.get(t, "/api/v1/readiness"))
	require.Equal(t, []string{"storage:local"},
		instanceNames(held), "the principal holding the grant must still see the inventory")

	granted.stopAndWait(t)

	// Same directory, so the same graph, and it holds nothing any more.
	withdrawEveryGrantIn(t, dir)

	stranger := start(t, dir, declared("operator"))
	defer stranger.stopAndWait(t)

	require.Eventually(t, func() bool {
		return stranger.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "the declared instance never came up on the second start")

	report := readinessOf(t, stranger.get(t, "/api/v1/readiness"))

	require.Empty(t, report.Instances,
		"a caller the graph grants nothing was told which instances exist")
	require.Empty(t, report.Blocked,
		"a caller the graph grants nothing was told which required instances are blocking")

	// The verdict and the counts still answer what a probe asks, or the
	// endpoint has been made safe by being made useless.
	require.True(t, report.Ready)
	require.Equal(t, 1, report.Declared)
	require.Equal(t, 1, report.Serving)
}

// TestReadinessNamesNoBlockedInstanceToACallerWithNoGrant is the half of that
// property a ready process cannot hold: Blocked is empty on a ready process
// whether or not it is narrowed to what the caller may see. This one is out of
// rotation, so the list has a resource in it to withhold.
//
// The second start is the caller the graph grants nothing, for the same reason
// as above. The storage root is replaced with a regular file between the two, so
// the required instance will not construct and the verdict names it.
func TestReadinessNamesNoBlockedInstanceToACallerWithNoGrant(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "storage")
	require.NoError(t, os.MkdirAll(root, 0o755))

	granted := start(t, dir, requiredStorage("operator", root))

	require.Eventually(t, func() bool {
		return granted.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "the declared instance never came up")

	granted.stopAndWait(t)

	// A regular file is not a directory, so the built-in storage type refuses to
	// construct against it, which is what puts a name in Blocked.
	require.NoError(t, os.RemoveAll(root))
	write(t, root, "")

	// Same directory, so the same graph, and it holds nothing any more.
	withdrawEveryGrantIn(t, dir)

	stranger := start(t, dir, requiredStorage("operator", root))
	defer stranger.stopAndWait(t)

	require.Eventually(t, func() bool {
		return stranger.get(t, "/api/v1/readiness").status == http.StatusServiceUnavailable
	}, settle, 20*time.Millisecond, "a required instance that will not start must take the process out of rotation")

	report := readinessOf(t, stranger.get(t, "/api/v1/readiness"))

	require.Empty(t, report.Blocked,
		"a caller the graph grants nothing was told which required instances are blocking")
	require.Empty(t, report.Instances,
		"a caller the graph grants nothing was told which instances exist")

	// The verdict and the counts still answer what a probe asks, or the endpoint
	// has been made safe by being made useless.
	require.False(t, report.Ready)
	require.Equal(t, 1, report.Declared)
	require.Equal(t, 0, report.Serving)

	// The principal that holds the grant is still told what to look at, or the
	// endpoint has been made safe by naming the resource to nobody. A start
	// under another core.principal makes it the admin of the org.
	named := start(t, dir, requiredStorage("successor", root))
	defer named.stopAndWait(t)

	require.Eventually(t, func() bool {
		return named.get(t, "/api/v1/readiness").status == http.StatusServiceUnavailable
	}, settle, 20*time.Millisecond, "the second start never went out of rotation")

	held := readinessOf(t, named.get(t, "/api/v1/readiness"))
	require.Equal(t, []string{"storage:local"}, held.Blocked,
		"the principal holding the grant must still be told which instance is blocking")
}

// requiredStorage declares one required filesystem instance under a named
// principal, which is the smallest configuration whose readiness moves.
func requiredStorage(principal, root string) string {
	return lines(
		"core:",
		"  principal: "+principal,
		"  log:", "    level: error",
		"resources:", "  storage:", "    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(root))
}

// withdrawEveryGrantIn empties the graph of the deployment whose operator file
// is in dir.
func withdrawEveryGrantIn(t *testing.T, dir string) {
	t.Helper()

	core, err := config.Load(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)

	require.NotEmpty(t, withdrawEveryGrant(t, core), "the first start wrote no grant")
}

func instanceNames(report httpapi.ReadinessReport) []string {
	out := make([]string, 0, len(report.Instances))
	for _, instance := range report.Instances {
		out = append(out, instance.Resource)
	}

	return out
}

func readinessOf(t *testing.T, read answer) httpapi.ReadinessReport {
	t.Helper()

	var report httpapi.ReadinessReport
	require.NoError(t, json.Unmarshal([]byte(read.body), &report), read.body)

	return report
}
