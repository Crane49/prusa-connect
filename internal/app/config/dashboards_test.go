package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestDashboardsAreOnUnlessTheFileSwitchesThemOff is the default the owner
// chose: a file that says nothing about them serves them, and only
// `enabled: false` switches them off.
func TestDashboardsAreOnUnlessTheFileSwitchesThemOff(t *testing.T) {
	t.Parallel()

	for _, step := range []struct {
		name string
		file string
		on   bool
	}{
		{"no block", "core:\n  principal: someone\n", true},
		{"an empty block", "core:\n  dashboards: {}\n", true},
		{"switched on", "core:\n  dashboards:\n    enabled: true\n", true},
		{"switched off", "core:\n  dashboards:\n    enabled: false\n", false},
	} {
		core, err := config.Load(write(t, step.file))
		require.NoErrorf(t, err, step.name)
		require.Equalf(t, step.on, core.Dashboards.On(), step.name)
	}

	require.True(t, config.Default().Dashboards.On(), "a server nobody configured serves dashboards")
}

// TestThePrinterProfileSwitchesTheDashboardsOff holds the shipped printer file
// to the decision: the printer serves one printer, and its home page is it.
func TestThePrinterProfileSwitchesTheDashboardsOff(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "config", "examples", "config.printer.yaml")
	_, err := os.Stat(path)
	require.NoError(t, err)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.False(t, core.Dashboards.On())
}
