package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestADeploymentReadsTheMarketplaceUntilSwitchedOff is ADR-20261005-64j rule 1: with
// no key the catalogue is read from the Prusa Research marketplace, and
// enabled: false is the one way to read none.
func TestADeploymentReadsTheMarketplaceUntilSwitchedOff(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)
	require.True(t, core.Marketplace.Configured())
	require.Equal(t, config.DefaultMarketplaceURL, core.Marketplace.Base())
	require.Equal(t, config.DefaultMarketplaceTimeout, core.Marketplace.Timeout)
	require.NoError(t, core.Validate())

	off, err := config.Load(write(t, "core:\n  principal: someone\n  marketplace:\n    enabled: false\n"))
	require.NoError(t, err)
	require.False(t, off.Marketplace.Configured())
	require.NoError(t, off.Validate())

	// A marketplace of the operator's own replaces the default and stays on.
	own, err := config.Load(write(t,
		"core:\n  principal: someone\n  marketplace:\n    url: http://localhost:4455\n"))
	require.NoError(t, err)
	require.True(t, own.Marketplace.Configured())
	require.Equal(t, "http://localhost:4455", own.Marketplace.Base())
}

// loaded is a configuration that validates, with every derived key filled.
func loaded(t *testing.T) config.Core {
	t.Helper()

	core, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)

	return core
}

// TestAMarketplaceIsHttpsOrALoopbackHost is ADR-20261005-64j rule 1, both ways.
func TestAMarketplaceIsHttpsOrALoopbackHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url   string
		valid bool
	}{
		{"https://plugins.example", true},
		{"https://plugins.example/", true},
		{"https://plugins.example:8443", true},
		{"http://localhost:4455", true},
		{"http://127.0.0.1:4455", true},
		{"http://[::1]:4455", true},
		{"http://plugins.example", false},
		{"http://192.168.1.10:4455", false},
		{"ftp://plugins.example", false},
		{"plugins.example", false},
		{"https://", false},
		{"https://user@plugins.example", false},
		{"https://plugins.example/?q=1", false},
	}

	for _, row := range cases {
		t.Run(row.url, func(t *testing.T) {
			t.Parallel()

			core := loaded(t)
			core.Marketplace.URL = row.url

			err := core.Validate()
			if row.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, config.ErrInvalid)
				require.Contains(t, err.Error(), "core.marketplace.url")
			}
		})
	}
}

// TestAMarketplaceTimeoutIsABound refuses a timeout that is not one.
func TestAMarketplaceTimeoutIsABound(t *testing.T) {
	t.Parallel()

	core := loaded(t)
	core.Marketplace.Timeout = -time.Second

	require.ErrorIs(t, core.Validate(), config.ErrInvalid)
}

// TestOnlyALiteralLoopbackHostCounts keeps a name that merely resolves to
// loopback out, because what it resolves to can change before the dial.
func TestOnlyALiteralLoopbackHostCounts(t *testing.T) {
	t.Parallel()

	require.True(t, config.IsLoopbackHost("localhost"))
	require.True(t, config.IsLoopbackHost("LOCALHOST"))
	require.True(t, config.IsLoopbackHost("127.0.0.1"))
	require.True(t, config.IsLoopbackHost("127.5.5.5"))
	require.True(t, config.IsLoopbackHost("::1"))
	require.False(t, config.IsLoopbackHost("localhost.example"))
	require.False(t, config.IsLoopbackHost("10.0.0.1"))
	require.False(t, config.IsLoopbackHost(""))
}
