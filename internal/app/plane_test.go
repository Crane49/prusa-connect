package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// givingUpPlane reports every module it is asked to start as switched off by
// the host, from inside Reconcile, which is where the real supervisor's hook
// runs while the store holds its lock.
type givingUpPlane struct {
	offs *switchOffs
}

func (p givingUpPlane) Reconcile(_ context.Context, plan resource.Plan) error {
	for _, step := range plan.Steps {
		if step.Outcome == resource.OutcomeStart {
			p.offs.observe(supervisor.Status{
				Ref:         step.Ref,
				Result:      supervisor.ResultSwitchedOffByHost,
				RecentStops: []supervisor.Stop{{Result: supervisor.ResultExited, Code: 1}},
			})
		}
	}

	return nil
}

func (givingUpPlane) Status(ref.Ref) (httpapi.ResourceStatus, bool) {
	return httpapi.ResourceStatus{}, false
}
func (givingUpPlane) Documented(ref.Ref) bool               { return false }
func (givingUpPlane) UI(ref.Ref) (httpapi.ResourceUI, bool) { return httpapi.ResourceUI{}, false }
func (givingUpPlane) Restart(ref.Ref) bool                  { return false }

func (givingUpPlane) MemoryHistory(ref.Ref, time.Time) (httpapi.ResourceMemoryHistory, bool) {
	return httpapi.ResourceMemoryHistory{}, false
}

func (givingUpPlane) Log(ref.Ref) (httpapi.ResourceLog, bool) { return httpapi.ResourceLog{}, false }

func (givingUpPlane) Database(ref.Ref) (wirerpc.DatabaseState, bool) {
	return wirerpc.DatabaseState{}, false
}

func TestASwitchOffDuringAWriteDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	overlay := filepath.Join(dir, "config.d", "resources.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlay), 0o700))
	require.NoError(t, os.WriteFile(overlay, []byte(`{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "farm", "type": "com.github.aux.acme.farm/farm", "runtime": "service",
     "enabled": false, "required": false,
     "service": {"path": "/opt/pcl/plugins", "checksum": "sha256:`+strings.Repeat("a", 64)+`",
                 "contract_major": 1}}
  ]
}
`), 0o600))

	offs := newSwitchOffs(discard())

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go offs.run(ctx)

	store, err := resource.New(resource.Config{
		Paths:  resource.Paths{File: filepath.Join(dir, "config.yaml"), Overlay: overlay},
		Plane:  givingUpPlane{offs: offs},
		Graph:  newGraph(t),
		Logger: discard(),
	})
	require.NoError(t, err)
	offs.bind(store)
	require.NoError(t, store.Reload(t.Context()))

	principal := "user:" + config.DefaultPrincipal

	farm, err := ref.New(ref.KindSink, "farm")
	require.NoError(t, err)

	current, err := store.Get(t.Context(), principal, farm)
	require.NoError(t, err)

	body := current.ResourceSpec
	body.Enabled = true

	applied := make(chan error, 1)

	go func() {
		_, applyErr := store.Apply(t.Context(), principal, farm, body,
			httpapi.Precondition{IfMatch: []string{current.Version}})
		applied <- applyErr
	}()

	select {
	case err = <-applied:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the write did not finish while the host switched the module off")
	}

	require.Eventually(t, func() bool {
		listed, getErr := store.Get(t.Context(), principal, farm)

		return getErr == nil && !listed.Enabled && listed.Status.Memory.SwitchedOff != nil
	}, 5*time.Second, 10*time.Millisecond, "the switch-off was not recorded")
}
