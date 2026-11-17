package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// TestThePasswordCostKeepsTheNormalBuildAsItWas pins the argon2id cost a file
// with no password keys gets. A normal build hashes at 19 MiB with 2 passes,
// and the printer build at 7 MiB with 5 passes.
func TestThePasswordCostKeepsTheNormalBuildAsItWas(t *testing.T) {
	t.Parallel()

	core := config.Default().WithDefaults(write(t, ""))
	require.NoError(t, core.Validate())

	want := auth.Cost{MemoryKiB: 19 * 1024, Passes: 2}
	if buildprofile.Name == "printer" {
		want = auth.Cost{MemoryKiB: 7 * 1024, Passes: 5}
	}

	require.Equal(t, want, core.Auth.PasswordCost())
	require.Equal(t, auth.DefaultCost(), core.Auth.PasswordCost())
}

// TestThePasswordHashBoundKeepsTheNormalBuildAsItWas pins how many password
// hashes run at once in a file that names no bound. A normal build runs four,
// and the printer build one.
func TestThePasswordHashBoundKeepsTheNormalBuildAsItWas(t *testing.T) {
	t.Parallel()

	core := config.Default().WithDefaults(write(t, ""))
	require.NoError(t, core.Validate())

	want := 4
	if buildprofile.Name == "printer" {
		want = 1
	}

	require.Equal(t, want, core.Auth.PasswordHashesAtOnce)
}

// TestThePasswordHashBoundIsRead proves the bound an operator writes is the
// bound the server gets, and that a bound of no slot is refused.
func TestThePasswordHashBoundIsRead(t *testing.T) {
	t.Parallel()

	path := write(t, `
core:
  auth:
    password_hashes_at_once: 2
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())
	require.Equal(t, 2, core.Auth.PasswordHashesAtOnce)

	core.Auth.PasswordHashesAtOnce = -1

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.ErrorContains(t, err, "core.auth.password_hashes_at_once")
}

// TestThePasswordKeysAreRead proves the cost an operator writes is the cost
// the server gets.
func TestThePasswordKeysAreRead(t *testing.T) {
	t.Parallel()

	path := write(t, `
core:
  auth:
    password_memory_bytes: 12582912
    password_passes: 3
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())

	require.Equal(t, auth.Cost{MemoryKiB: 12 * 1024, Passes: 3}, core.Auth.PasswordCost())
}

// TestAPasswordCostBelowOWASPIsRefused covers the values that would make a
// password hash weaker than every setting OWASP lists, and the values that
// are not a size in bytes.
func TestAPasswordCostBelowOWASPIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		memory int
		passes int
		says   string
	}{
		{name: "19 MiB with 1 pass", memory: 19 << 20, passes: 1, says: "at least 2 passes"},
		{name: "8 MiB with 4 passes", memory: 8 << 20, passes: 4, says: "at least 5 passes"},
		{name: "6 MiB with 10 passes", memory: 6 << 20, passes: 10, says: "at least 7168 KiB"},
		{name: "a size in KiB instead of bytes", memory: 7168, passes: 5, says: "core.auth.password_memory_bytes"},
		{name: "a size that is not whole KiB", memory: 19<<20 + 1, passes: 2, says: "whole number of KiB"},
		{name: "negative memory", memory: -1, passes: 2, says: "core.auth.password_memory_bytes"},
		{name: "negative passes", memory: 19 << 20, passes: -1, says: "core.auth.password_passes"},
		{name: "memory over the upper limit", memory: 2 << 30, passes: 2, says: "upper limit"},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			core := config.Default().WithDefaults(write(t, ""))
			core.Auth.PasswordMemoryBytes = each.memory
			core.Auth.PasswordPasses = each.passes

			err := core.Validate()
			require.ErrorIs(t, err, config.ErrInvalid)
			require.ErrorContains(t, err, each.says)
		})
	}
}

// TestAnAuthBlockWithNoPasswordKeysHasTheDefaultCost covers a caller that
// builds the block without WithDefaults. The keys it left at zero mean the
// default of this build, and never a cost of nothing.
func TestAnAuthBlockWithNoPasswordKeysHasTheDefaultCost(t *testing.T) {
	t.Parallel()

	require.Equal(t, auth.DefaultCost(), config.Auth{}.PasswordCost())
}
