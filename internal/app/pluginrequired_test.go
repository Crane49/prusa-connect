package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite is earlier research on
// the real binary. required makes GET /api/v1/readiness answer 503 while the
// instance is not serving, which empties the pod out of a Kubernetes Service and
// leaves every printer reaching nothing, so one boolean on an installed module
// switches a print farm off.
//
// Each case declares the refused block beside a core instance that is required
// and does start. That instance is the arming half: a run where the file was
// never read, or where nothing resolved, leaves an empty listing and passes
// every assertion about an absent sink.
//
// A third case stood here, a compiled-in type outside the core namespace. It
// moved to TestAModuleDeclaredAsBuiltinNeverReachesTheSupervisionPlane, which
// refuses that block earlier and for a different reason: ADR-20260827-wmv rule 5 keeps
// it out of the address space whether or not it carries required.
func TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block   []string
		refused string
	}{
		// The type name here is bare, which is the form a name test reads as
		// core, and the binary behind it is a file somebody else compiled.
		"a child process declared under a bare type name": {
			refused: "driver/bambu",
			block: []string{
				"  driver:", "    bambu:",
				"      type: bambu",
				"      required: true",
				"      runtime: service",
				"      service:",
				"        path: plugins/bambu",
				"        checksum: sha256:" + strings.Repeat("1f", 32),
				"        contract_major: 1",
			},
		},
		"a script, whose body the operator wrote": {
			refused: "sink/relay",
			block: []string{
				"  sink:", "    relay:",
				"      type: script",
				"      required: true",
				"      runtime: script",
				"      script: |",
				"        " + transformScript,
				"      config:",
				"        endpoints:",
				"          audit: https://192.0.2.1/events",
				"        timeout: 2s",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			running := start(t, dir, coreRequiredStorage(dir)+lines(tc.block...))

			require.Eventually(t, func() bool {
				return running.instanceState(t, "storage/local") == "ready"
			}, settle, 20*time.Millisecond, "the core instance beside the refused block never came up")

			require.Empty(t, running.instanceState(t, tc.refused),
				"the refused block is running, and required on it can take the process out of rotation")

			require.Equal(t, []string{"storage:local"}, declaredInstances(t, running),
				"the refused block reached the merged document")

			// The whole point of the refusal: the process answers ready, so a load
			// balancer keeps routing printers to it.
			probe := running.get(t, "/api/v1/readiness")
			require.Equal(t, http.StatusOK, probe.status, probe.body)
			require.True(t, readinessOf(t, probe).Ready)

			running.stopAndWait(t)
		})
	}
}

// TestACoreInstanceMayStillBeRequired is the other direction, and the reason
// the refusal is about who wrote the implementation rather than about required
// itself. A core type carries the flag into the declaration, the supervision
// plane and the readiness report exactly as it always has.
//
// The 503 the flag produces when such an instance will not start is
// TestReadinessIsServedAndMoves, which drives it in both directions on one
// process.
func TestACoreInstanceMayStillBeRequired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, coreRequiredStorage(dir))

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "a required core instance never brought the process into rotation")

	report := readinessOf(t, running.get(t, "/api/v1/readiness"))
	require.True(t, report.Ready)
	require.Equal(t, 1, report.Serving)
	require.Len(t, report.Instances, 1)
	require.True(t, report.Instances[0].Required, "required was dropped from a core instance")
	require.True(t, report.Instances[0].Serving)

	running.stopAndWait(t)
}

// TestTheAPIRefusesRequiredOnATypeOutsideTheCore closes the second door onto the
// same flag. Every instance the resource API writes is builtin, so the type name
// is the whole question, and a caller told 201 and then left with an instance
// the next load drops has learned nothing about which field to change.
func TestTheAPIRefusesRequiredOnATypeOutsideTheCore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, coreRequiredStorage(dir))

	refused := running.put(t, "/api/v1/resources/sink/audit",
		`{"type":"acme/webhook","enabled":true,"required":true,"config":{"url":"https://192.0.2.1/e"}}`)
	require.Equal(t, http.StatusUnprocessableEntity, refused.status, refused.body)
	require.Contains(t, refused.body, "body.required", "the refusal does not name the field to change")

	// A core type carrying the same flag is accepted, so this is a refusal of
	// required on an implementation the core did not write and not a refusal of
	// the flag. Dropping the flag off the block above does not make it legal:
	// ADR-20260827-wmv rule 5 refuses the type itself, which is the case below.
	accepted := running.put(t, "/api/v1/resources/sink/audit",
		`{"type":"webhook","enabled":true,"required":true,"config":{"url":"https://192.0.2.1/e"}}`)
	require.Equal(t, http.StatusCreated, accepted.status, accepted.body)

	running.stopAndWait(t)
}

// TestTheAPIRefusesAModuleItWouldRunInsideItself is ADR-20260827-wmv rule 5 at the third
// door. Every instance this surface writes is builtin, so a namespaced type here
// is a request to load code the core did not write into the serving process,
// which is the shape the record exists to refuse.
//
// The refusal is at the write and not at the next load, for the reason the
// required refusal is: a caller told 201 and then left with an instance that
// never starts has learned nothing about which field to change. It names
// body.type, because there is no value of it this API would accept.
func TestTheAPIRefusesAModuleItWouldRunInsideItself(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, coreRequiredStorage(dir))

	refused := running.put(t, "/api/v1/resources/sink/audit",
		`{"type":"acme/webhook","enabled":true,"required":false,"config":{"url":"https://192.0.2.1/e"}}`)
	require.Equal(t, http.StatusUnprocessableEntity, refused.status, refused.body)
	require.Contains(t, refused.body, "body.type", "the refusal does not name the field to change")
	require.Contains(t, refused.body, "runtime: service",
		"the refusal does not name the runtime a module has")

	// Nothing was written, so a caller that retries the same request twice does
	// not find the instance the first one was refused.
	require.Equal(t, []string{"storage:local"}, declaredInstances(t, running),
		"the refused instance reached the merged document")

	// The arming half: the same surface accepts a type this binary does ship, so
	// a run where every write was refused for some other reason cannot pass.
	accepted := running.put(t, "/api/v1/resources/sink/audit",
		`{"type":"webhook","enabled":true,"required":false,"config":{"url":"https://192.0.2.1/e"}}`)
	require.Equal(t, http.StatusCreated, accepted.status, accepted.body)

	running.stopAndWait(t)
}

// TestAModuleDeclaredAsBuiltinNeverReachesTheSupervisionPlane is ADR-20260827-wmv rule 5
// on the real binary, through the operator's own file. A module is a pluggable
// bit that extends the core and cannot break it, and the only runtime that keeps
// that true is a process of its own: ADR-20260824-nyw tier 1 shares this address space,
// so a compiled-in module holds every secret the process holds and stops the
// server when it stops.
//
// The refused block sits beside a core instance that does start, which is the
// arming half: a run where the file was never read leaves an empty listing and
// passes every assertion about an absent sink.
func TestAModuleDeclaredAsBuiltinNeverReachesTheSupervisionPlane(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, coreRequiredStorage(dir)+lines(
		"  sink:", "    audit:",
		"      type: acme/webhook",
		"      enabled: true",
		"      config:",
		"        url: https://192.0.2.1/events",
	))

	require.Eventually(t, func() bool {
		return running.instanceState(t, "storage/local") == "ready"
	}, settle, 20*time.Millisecond, "the core instance beside the refused block never came up")

	require.Empty(t, running.instanceState(t, "sink/audit"),
		"a module is running inside this binary, where nothing can contain it")

	require.Equal(t, []string{"storage:local"}, declaredInstances(t, running),
		"the refused block reached the merged document")

	// The process serves. One bad declaration fails itself and nothing else,
	// which is what keeps an install of a module off the critical path.
	probe := running.get(t, "/api/v1/readiness")
	require.Equal(t, http.StatusOK, probe.status, probe.body)
	require.True(t, readinessOf(t, probe).Ready)

	running.stopAndWait(t)
}

// TestAnOverlayCarryingAModuleTypeLosesThatInstance is the upgrade case for the
// same rule. Every overlay entry is builtin, and one written by a binary that
// accepted a namespaced type still holds it, so refusing at the API alone would
// leave the defect in the deployments that already have it.
//
// The entry is dropped and the process serves, for the reason the required
// refusal gives: an upgrade that refused to start would be the outage.
func TestAnOverlayCarryingAModuleTypeLosesThatInstance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.d", "resources.json"), `{"version":1,"instances":[`+
		`{"kind":"sink","name":"audit","type":"acme/webhook","enabled":true,"required":false,`+
		`"config":{"url":"https://192.0.2.1/events"}}]}`)

	running := start(t, dir, coreRequiredStorage(dir))

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "an overlay entry took the process out of rotation")

	require.Equal(t, []string{"storage:local"}, declaredInstances(t, running),
		"the overlay entry reached the merged document")

	running.stopAndWait(t)
}

// TestAnOverlayCarryingRequiredOnAThirdPartyTypeLosesThatInstance is the upgrade
// case. The overlay is machine-owned, and one written by a binary that accepted
// the flag still holds it, so refusing at the API alone leaves the defect in the
// deployments that already have it.
//
// The instance is dropped and the process serves, rather than the load failing:
// an upgrade that refused to start would be the outage this refusal exists to
// prevent.
func TestAnOverlayCarryingRequiredOnAThirdPartyTypeLosesThatInstance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.d", "resources.json"), `{"version":1,"instances":[`+
		`{"kind":"sink","name":"audit","type":"acme/webhook","enabled":true,"required":true,`+
		`"config":{"url":"https://192.0.2.1/events"}}]}`)

	running := start(t, dir, coreRequiredStorage(dir))

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/readiness").status == http.StatusOK
	}, settle, 20*time.Millisecond, "an overlay entry took the process out of rotation")

	require.Equal(t, []string{"storage:local"}, declaredInstances(t, running),
		"the overlay entry reached the merged document")

	running.stopAndWait(t)
}

// coreRequiredStorage is one required core instance, which every case here
// declares so that an empty listing cannot pass for a refusal.
func coreRequiredStorage(dir string) string {
	return lines(
		"core:", "  log:", "    level: error",
		"resources:", "  storage:", "    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(filepath.Join(dir, "storage")))
}

// declaredInstances is every instance the merged document carries, as the API
// lists them. A declaration that was refused is absent from it.
func declaredInstances(t *testing.T, running *process) []string {
	t.Helper()

	listed := running.get(t, "/api/v1/resources")
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	var listing struct {
		Items []httpapi.ResourceInstance `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(listed.body), &listing), listed.body)

	out := make([]string, 0, len(listing.Items))
	for _, item := range listing.Items {
		out = append(out, item.Kind+":"+item.Name)
	}

	return out
}
