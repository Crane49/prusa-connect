package resource_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// stagedFarm is the overlay an approved install writes for a service module
// with one staged route.
func stagedFarm() string {
	return `{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "farm", "type": "com.github.aux.acme.farm/farm", "runtime": "service",
     "enabled": true, "required": false,
     "service": {"path": "/opt/pcl/plugins", "checksum": "sha256:` + strings.Repeat("a", 64) + `",
                 "contract_major": 1, "data": true,
                 "staged_routes": [{"method": "PUT", "path": "/files/{name}", "max_mib": 1024}]}}
  ]
}
`
}

// An operator changes the limit of a staged route the install approved, up
// to the core's ceiling, and the change restarts the module. A write cannot
// stage a route the install did not approve, and a write that leaves the list
// out keeps the limits (ADR-20261120-xys rule 5).
func TestAWriteChangesOnlyTheLimitOfAnApprovedStagedRoute(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeOverlayByHand(stagedFarm())
	require.NoError(t, h.store.Reload(t.Context()))

	farm := mustRef(t, ref.KindSink, "farm")

	current, err := h.get(farm)
	require.NoError(t, err)
	require.Equal(t, []httpapi.ModuleStagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 1024}},
		current.StagedRoutes)

	for _, tc := range []struct {
		name    string
		routes  []httpapi.ModuleStagedRoute
		refused string
	}{
		{"zero", []httpapi.ModuleStagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 0}}, "expected 1 to 4096"},
		{"over the ceiling", []httpapi.ModuleStagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 4097}},
			"expected 1 to 4096"},
		{"not approved", []httpapi.ModuleStagedRoute{{Method: "POST", Path: "/files/{name}", MaxMiB: 8}},
			"is not a staged route of this module"},
	} {
		body := current.ResourceSpec
		body.StagedRoutes = tc.routes

		_, err = h.update(farm, body, current.Version)
		require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "%s: got %v", tc.name, err)
		require.ErrorContains(t, err, tc.refused, tc.name)
	}

	body := current.ResourceSpec
	body.StagedRoutes = []httpapi.ModuleStagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 4096}}

	applied, err := h.update(farm, body, current.Version)
	require.NoError(t, err)

	raised := applied.Instance
	require.Equal(t, []httpapi.ModuleStagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 4096}},
		raised.StagedRoutes)
	require.Greater(t, raised.Generation, current.Generation)

	body = raised.ResourceSpec
	body.StagedRoutes = nil

	applied, err = h.update(farm, body, raised.Version)
	require.NoError(t, err)
	require.Equal(t, raised.StagedRoutes, applied.Instance.StagedRoutes)
	require.Equal(t, raised.Generation, applied.Instance.Generation)
}

// A changed staged limit restarts the module, because the module learns its
// limit only at start.
func TestAStagedLimitEditRestarts(t *testing.T) {
	t.Parallel()

	instance := mustRef(t, ref.KindSink, "stats")
	was := serviceSpec(0, 0)
	next := serviceSpec(0, 0)
	was.Service.StagedRoutes = []install.StagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 1024}}
	next.Service.StagedRoutes = []install.StagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 2048}}

	plan := resource.Diff(
		resource.Document{Instances: []resource.Declaration{{Ref: instance, Spec: was}}},
		resource.Document{Instances: []resource.Declaration{{Ref: instance, Spec: next}}})
	require.Len(t, plan.Steps, 1)
	require.Equal(t, resource.OutcomeRestart, plan.Steps[0].Outcome)
}
