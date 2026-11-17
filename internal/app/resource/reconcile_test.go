package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

func TestABudgetEditRestartsAndAnOnOverMemoryOrSwitchOffEditIsInPlace(t *testing.T) {
	t.Parallel()

	instance := mustRef(t, ref.KindSink, "stats")
	plan := func(was, next resource.Spec) resource.Plan {
		return resource.Diff(
			resource.Document{Instances: []resource.Declaration{{Ref: instance, Spec: was}}},
			resource.Document{Instances: []resource.Declaration{{Ref: instance, Spec: next}}})
	}

	raised := plan(serviceSpec(0, 0), serviceSpec(256, 0))
	require.Len(t, raised.Steps, 1)
	require.Equal(t, resource.OutcomeRestart, raised.Steps[0].Outcome)

	warn := serviceSpec(0, 0)
	warn.Service.OnOverMemory = install.OnOverMemoryWarn

	warned := plan(serviceSpec(0, 0), warn)
	require.Len(t, warned.Steps, 1)
	require.Equal(t, resource.OutcomeInPlace, warned.Steps[0].Outcome)

	never := serviceSpec(0, 0)
	never.Service.SwitchOff = install.SwitchOff{Failures: new(0)}

	switchOff := plan(serviceSpec(0, 0), never)
	require.Len(t, switchOff.Steps, 1)
	require.Equal(t, resource.OutcomeInPlace, switchOff.Steps[0].Outcome)

	off := serviceSpec(256, 0)
	off.Enabled = false

	switched := plan(serviceSpec(0, 0), off)
	require.Len(t, switched.Steps, 1)
	require.NotEqual(t, resource.OutcomeInPlace, switched.Steps[0].Outcome)
}
