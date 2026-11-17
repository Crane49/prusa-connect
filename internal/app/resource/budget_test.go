package resource_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const mib = 1 << 20

// serviceSpec is an enabled service module with the operator's budget and
// the author's recommendation, each zero for none.
func serviceSpec(operator, author int) resource.Spec {
	spec := resource.Spec{
		Type:    "acme/stats",
		Enabled: true,
		Runtime: install.RuntimeService,
		Service: &install.Service{Path: "stats", Checksum: "sha256:00", MemoryMiB: operator},
	}

	if author > 0 {
		spec.Module = &install.ModuleEntry{Asked: &install.ModuleAsks{MemoryMiB: author}}
	}

	return spec
}

func TestBudgetOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		spec   resource.Spec
		bytes  int64
		source resource.BudgetSource
		ok     bool
	}{
		{"no override, no ask", serviceSpec(0, 0), 128 * mib, resource.BudgetDefault, true},
		{"ask", serviceSpec(0, 192), 192 * mib, resource.BudgetAuthor, true},
		{"override beats ask", serviceSpec(64, 192), 64 * mib, resource.BudgetOperator, true},
		{"override 0 follows the ask", serviceSpec(0, 96), 96 * mib, resource.BudgetAuthor, true},
		{"builtin", resource.Spec{Type: "camera", Enabled: true}, 0, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			budget, ok := resource.BudgetOf(tc.spec)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.bytes, budget.Bytes)
			require.Equal(t, tc.source, budget.Source)
		})
	}
}

// The operator's value wins over the server's, the server's over the
// install defaults, one value at a time, and an operator 0 is never.
func TestSwitchOffOf(t *testing.T) {
	t.Parallel()

	withOperator := func(failures *int, window time.Duration) resource.Spec {
		spec := serviceSpec(0, 0)
		spec.Service.SwitchOff = install.SwitchOff{Failures: failures, Window: window}

		return spec
	}
	server := install.SwitchOff{Failures: new(5), Window: 15 * time.Minute}

	cases := []struct {
		name   string
		spec   resource.Spec
		server install.SwitchOff
		want   resource.SwitchOffPolicy
		ok     bool
	}{
		{"nothing set", serviceSpec(0, 0), install.SwitchOff{},
			resource.SwitchOffPolicy{Failures: 3, Window: time.Hour, Source: resource.BudgetDefault}, true},
		{"server", serviceSpec(0, 0), server,
			resource.SwitchOffPolicy{Failures: 5, Window: 15 * time.Minute, Source: resource.BudgetDefault}, true},
		{"operator never beats server", withOperator(new(0), 0), server,
			resource.SwitchOffPolicy{Failures: 0, Window: 15 * time.Minute, Source: resource.BudgetOperator}, true},
		{"operator window only", withOperator(nil, 6*time.Hour), server,
			resource.SwitchOffPolicy{Failures: 5, Window: 6 * time.Hour, Source: resource.BudgetOperator}, true},
		{"builtin", resource.Spec{Type: "camera", Enabled: true}, server, resource.SwitchOffPolicy{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := resource.SwitchOffOf(tc.spec, tc.server)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPoolOf(t *testing.T) {
	t.Parallel()

	detected := func(total int64) func() (int64, bool) {
		return func() (int64, bool) { return total, true }
	}

	cases := []struct {
		name       string
		configured int64
		detect     func() (int64, bool)
		want       resource.Pool
	}{
		{"configured", 512 * mib, detected(4096 * mib), resource.Pool{Bytes: 512 * mib, Known: true, Configured: true}},
		{"1 GiB keeps 256 MiB", 0, detected(1024 * mib),
			resource.Pool{Bytes: 768 * mib, Known: true, Reserve: 256 * mib}},
		{"4 GiB keeps a quarter", 0, detected(4096 * mib),
			resource.Pool{Bytes: 3072 * mib, Known: true, Reserve: 1024 * mib}},
		{"256 MiB leaves no pool", 0, detected(256 * mib),
			resource.Pool{Bytes: 0, Known: true, Reserve: 256 * mib}},
		{"undetected", 0, func() (int64, bool) { return 0, false }, resource.Pool{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, resource.PoolOf(tc.configured, tc.detect))
		})
	}
}

// installedFarm is the overlay an install writes for one enabled service
// module.
func installedFarm() string {
	return `{
  "version": 1,
  "instances": [
    {"kind": "sink", "name": "farm", "type": "com.github.aux.acme.farm/farm", "runtime": "service",
     "enabled": true, "required": false,
     "service": {"path": "/opt/pcl/plugins", "checksum": "sha256:` + strings.Repeat("a", 64) + `",
                 "contract_major": 1}}
  ]
}
`
}

func TestTheBodyMemoryBudgetIsHeldToServiceModulesAndBounds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeOverlayByHand(installedFarm())
	require.NoError(t, h.store.Reload(t.Context()))

	farm := mustRef(t, ref.KindSink, "farm")

	for _, tc := range []struct {
		memoryMiB int
		refused   bool
	}{{8, true}, {9000, true}, {0, false}, {16, false}} {
		current, err := h.get(farm)
		require.NoError(t, err)

		body := current.ResourceSpec
		body.MemoryMiB = tc.memoryMiB

		_, err = h.update(farm, body, current.Version)
		if !tc.refused {
			require.NoError(t, err, "memory_mib %d", tc.memoryMiB)

			continue
		}

		require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "memory_mib %d: got %v", tc.memoryMiB, err)
		require.ErrorContains(t, err, "expected 0 to follow the module, or 16 to 8192")
	}

	builtin := spec("filesystem", `{"root":"/tmp/x"}`)
	builtin.MemoryMiB = 64

	_, err := h.create(mustRef(t, ref.KindStorage, "local"), builtin)
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "got %v", err)
	require.ErrorContains(t, err, "runs no process of its own")
}

func TestPoolUseSumsTheEnabledServiceModules(t *testing.T) {
	t.Parallel()

	pool := resource.Pool{Bytes: 512 * mib, Known: true}
	h := newHarness(t, func(cfg *resource.Config) { cfg.Pool = pool })
	h.writeOverlayByHand(installedFarm())
	require.NoError(t, h.store.Reload(t.Context()))

	promised, got := h.store.PoolUse()
	require.Equal(t, resource.Pool{Bytes: resource.DefaultBudget, Known: true}, promised)
	require.Equal(t, pool, got)
}

func TestModuleMemoryReportsPoolAndCommitted(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) { cfg.Pool = resource.PoolOf(1024*mib, nil) })

	farm := func(name string, memoryMiB int) string {
		return `{"kind": "sink", "name": "` + name + `", "type": "com.github.aux.acme.` + name + `/` + name + `",
     "runtime": "service", "enabled": true, "required": false,
     "service": {"path": "/opt/pcl/plugins", "checksum": "sha256:` + strings.Repeat("a", 64) + `",
                 "contract_major": 1, "memory_mib": ` + strconv.Itoa(memoryMiB) + `}}`
	}

	h.writeOverlayByHand(`{"version": 1, "instances": [` + farm("farm", 128) + `,` + farm("mill", 256) + `]}`)
	require.NoError(t, h.store.Reload(t.Context()))

	pool := int64(1073741824)
	require.Equal(t, httpapi.ModuleMemoryPool{
		PoolBytes: &pool, CommittedBytes: 402653184, Source: "configured",
	}, h.store.ModuleMemory())
}

func TestModuleMemoryOmitsAPoolItCannotLearn(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) { cfg.Pool = resource.Pool{} })

	require.Equal(t, httpapi.ModuleMemoryPool{}, h.store.ModuleMemory())
}

func TestSwitchingOnAModuleOverThePoolIsNotRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) { cfg.Pool = resource.Pool{Bytes: 128 * mib, Known: true} })
	h.writeOverlayByHand(strings.Replace(installedFarm(), `"enabled": true`, `"enabled": false`, 1))
	require.NoError(t, h.store.Reload(t.Context()))

	farm := mustRef(t, ref.KindSink, "farm")
	current, err := h.get(farm)
	require.NoError(t, err)

	body := current.ResourceSpec
	body.Enabled = true
	body.MemoryMiB = 256

	_, err = h.update(farm, body, current.Version)
	require.NoError(t, err)

	_, running := h.plane.Status(farm)
	require.True(t, running, "the module over the pool was not started")
}
