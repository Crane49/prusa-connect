package resource_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// operatorsFarm is a service module the operator declares in their own file.
var operatorsFarm = `
resources:
  sink:
    farm:
      type: com.github.aux.acme.farm/farm
      runtime: service
      service:
        path: /opt/pcl/plugins
        checksum: sha256:` + strings.Repeat("a", 64) + `
        contract_major: 1
`

// threeStops is the record the host writes when the third stop in an hour
// switches a module off.
func threeStops() resource.SwitchOff {
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	return resource.SwitchOff{At: at.Add(3 * time.Minute), Stops: []supervisor.Stop{
		{Result: supervisor.ResultOverMemory, At: at, Used: 20 * mib, Budget: 16 * mib},
		{Result: supervisor.ResultExited, At: at.Add(time.Minute), Code: 2},
		{Result: supervisor.ResultSignal, At: at.Add(2 * time.Minute), Signal: "SIGSEGV"},
	}}
}

// restarted is a second store over the same files, which is what the next
// server start reads.
func (h *harness) restarted() (*resource.Store, *recordingPlane) {
	h.t.Helper()

	plane := &recordingPlane{status: map[ref.Ref]httpapi.ResourceStatus{}}
	store, err := resource.New(resource.Config{Paths: h.paths, Plane: plane, Graph: h.graph})
	require.NoError(h.t, err)
	require.NoError(h.t, store.Reload(h.t.Context()))

	return store, plane
}

func TestTheHostSwitchesAnOverlayModuleOffAndItStaysOff(t *testing.T) {
	t.Parallel()

	farm := mustRef(t, ref.KindSink, "farm")
	h := newHarness(t)
	h.writeOverlayByHand(installedFarm())
	require.NoError(t, h.store.Reload(t.Context()))

	require.NoError(t, h.store.SwitchOffByHost(t.Context(), farm, threeStops()))

	written := string(h.overlayBytes())
	require.Contains(t, written, `"enabled": false`)
	require.Contains(t, written, `"switched_off"`)
	require.Contains(t, written, `"signal": "SIGSEGV"`)

	_, running := h.plane.Status(farm)
	require.False(t, running, "the module was not stopped")

	require.NoError(t, h.store.SwitchOffByHost(t.Context(), farm, threeStops()))
	require.Equal(t, written, string(h.overlayBytes()), "a second switch-off wrote again")

	store, plane := h.restarted()
	require.Empty(t, plane.started(), "the switched-off module started at the next start")

	listed, err := store.Get(t.Context(), owner, farm)
	require.NoError(t, err)
	require.False(t, listed.Enabled)
	require.Equal(t, condition.StateDisabled().String(), listed.Status.State)
	require.NotNil(t, listed.Status.Memory.SwitchedOff)
	require.Len(t, listed.Status.Memory.SwitchedOff.Stops, 3)
	require.Equal(t, threeStops().At, listed.Status.Memory.SwitchedOff.At)
	require.Equal(t, int64(20*mib), *listed.Status.Memory.SwitchedOff.Stops[0].UsedBytes)
	require.False(t, listed.Status.Memory.HeldUntilRestart)
}

func TestTheHostHoldsAnOperatorFileModuleUntilRestart(t *testing.T) {
	t.Parallel()

	farm := mustRef(t, ref.KindSink, "farm")
	h := newHarness(t, func(cfg *resource.Config) { cfg.Pool = resource.Pool{Bytes: 512 * mib, Known: true} })
	h.writeFile(operatorsFarm)
	require.NoError(t, h.store.Reload(t.Context()))

	require.NoError(t, h.store.SwitchOffByHost(t.Context(), farm, threeStops()))

	_, running := h.plane.Status(farm)
	require.False(t, running, "the module was not stopped")

	listed, err := h.get(farm)
	require.NoError(t, err)
	require.True(t, listed.Enabled, "the operator's file still says enabled")
	require.Equal(t, condition.StateDisabled().String(), listed.Status.State)
	require.True(t, listed.Status.Memory.HeldUntilRestart)
	require.Len(t, listed.Status.Memory.SwitchedOff.Stops, 3)

	promised, _ := h.store.PoolUse()
	require.Zero(t, promised.Bytes, "a held module still counts against the pool")

	require.NoError(t, h.store.Reload(t.Context()))
	_, running = h.plane.Status(farm)
	require.False(t, running, "a reload started the held module")

	store, plane := h.restarted()
	require.Equal(t, []ref.Ref{farm}, plane.started())

	again, err := store.Get(t.Context(), owner, farm)
	require.NoError(t, err)
	require.False(t, again.Status.Memory.HeldUntilRestart)
	require.Nil(t, again.Status.Memory.SwitchedOff)
}

func TestSwitchingBackOnClearsTheRecord(t *testing.T) {
	t.Parallel()

	farm := mustRef(t, ref.KindSink, "farm")
	h := newHarness(t)
	h.writeOverlayByHand(installedFarm())
	require.NoError(t, h.store.Reload(t.Context()))
	require.NoError(t, h.store.SwitchOffByHost(t.Context(), farm, threeStops()))

	current, err := h.get(farm)
	require.NoError(t, err)

	body := current.ResourceSpec
	body.Enabled = true

	_, err = h.update(farm, body, current.Version)
	require.NoError(t, err)

	listed, err := h.get(farm)
	require.NoError(t, err)
	require.True(t, listed.Enabled)
	require.Nil(t, listed.Status.Memory.SwitchedOff)
	require.NotContains(t, string(h.overlayBytes()), "switched_off")

	// A start, not a restart: the plane builds the instance anew, so its
	// failure window starts empty.
	h.plane.mu.Lock()
	last := h.plane.steps[len(h.plane.steps)-1]
	h.plane.mu.Unlock()
	require.Equal(t, resource.OutcomeStart, last.Outcome)
	require.Equal(t, farm, last.Ref)
}

func TestAnOperatorWriteCannotSetTheRecord(t *testing.T) {
	t.Parallel()

	farm := mustRef(t, ref.KindSink, "farm")
	h := newHarness(t)
	h.writeOverlayByHand(installedFarm())
	require.NoError(t, h.store.Reload(t.Context()))

	current, err := h.get(farm)
	require.NoError(t, err)

	body := current.ResourceSpec
	body.Enabled = false

	_, err = h.update(farm, body, current.Version)
	require.NoError(t, err)

	listed, err := h.get(farm)
	require.NoError(t, err)
	require.False(t, listed.Enabled)
	require.Nil(t, listed.Status.Memory.SwitchedOff)
	require.NotContains(t, string(h.overlayBytes()), "switched_off")
}

// TestSwitchingAHeldFileModuleOffAndOnInTheFileReleasesIt is the operator's
// own switch for a module the API cannot write: the file's enabled key.
func TestSwitchingAHeldFileModuleOffAndOnInTheFileReleasesIt(t *testing.T) {
	t.Parallel()

	farm := mustRef(t, ref.KindSink, "farm")
	h := newHarness(t)
	h.writeFile(operatorsFarm)
	require.NoError(t, h.store.Reload(t.Context()))
	require.NoError(t, h.store.SwitchOffByHost(t.Context(), farm, threeStops()))

	h.writeFile(strings.Replace(operatorsFarm, "runtime: service\n", "runtime: service\n      enabled: false\n", 1))
	require.NoError(t, h.store.Reload(t.Context()))

	h.writeFile(operatorsFarm)
	require.NoError(t, h.store.Reload(t.Context()))

	_, running := h.plane.Status(farm)
	require.True(t, running, "the module the operator switched on again did not start")

	listed, err := h.get(farm)
	require.NoError(t, err)
	require.False(t, listed.Status.Memory.HeldUntilRestart)
	require.Nil(t, listed.Status.Memory.SwitchedOff)
}
