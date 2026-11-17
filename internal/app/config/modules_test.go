package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestInstalledModulesAreOnUnlessTheFileSwitchesThemOff holds the default of
// ADR-20261019-h76 rule 1: a file that says nothing keeps modules on, and only
// `modules_enabled: false` switches them off.
func TestInstalledModulesAreOnUnlessTheFileSwitchesThemOff(t *testing.T) {
	t.Parallel()

	for _, step := range []struct {
		name string
		file string
		on   bool
	}{
		{"no key", "core:\n  principal: someone\n", true},
		{"switched on", "core:\n  modules_enabled: true\n", true},
		{"switched off", "core:\n  modules_enabled: false\n", false},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t, step.file))
			require.NoError(t, err)
			require.Equal(t, step.on, core.ModulesOn())
		})
	}

	require.True(t, config.Default().ModulesOn(), "a server nobody configured keeps its modules")
}
