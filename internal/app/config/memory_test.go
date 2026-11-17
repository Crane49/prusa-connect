package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// TestTheMemoryKeysKeepTheNormalBuildAsItWas pins the values a normal build
// runs on, and the smaller values of the printer build. The preview cache holds
// 32 MiB in a normal build and 1 MiB on the printer. The SQLite page cache is
// SQLite's own 2000 KiB in a normal build and 512 KiB on the printer.
func TestTheMemoryKeysKeepTheNormalBuildAsItWas(t *testing.T) {
	t.Parallel()

	core := config.Default().WithDefaults(write(t, ""))

	if buildprofile.Name == "printer" {
		require.Equal(t, 1<<20, core.PreviewCacheBytes)
		require.Equal(t, 512<<10, core.Persistence.SQLite.PageCacheBytes)

		return
	}

	require.Equal(t, 32<<20, core.PreviewCacheBytes)
	require.Equal(t, 2000<<10, core.Persistence.SQLite.PageCacheBytes)
	require.Zero(t, core.MemoryLimit, "a normal build leaves the memory limit to Go")
}

// TestEveryMemoryKeyIsRead proves the values an operator writes are the values
// the server gets.
func TestEveryMemoryKeyIsRead(t *testing.T) {
	t.Parallel()

	path := write(t, `
persistence:
  sqlite:
    page_cache_bytes: 524288
core:
  preview_cache_bytes: 1048576
  memory_limit: 12582912
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())

	require.Equal(t, 1<<20, core.PreviewCacheBytes)
	require.Equal(t, 512<<10, core.Persistence.SQLite.PageCacheBytes)
	require.Equal(t, int64(12<<20), core.MemoryLimit)
}

// TestAMemoryKeyThatIsNoBoundIsRefused covers the values that would switch a
// bound off or make the server unusable, each refused with the key it names.
func TestAMemoryKeyThatIsNoBoundIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		edit func(*config.Core)
		key  string
	}{
		{
			name: "a negative preview cache",
			edit: func(c *config.Core) { c.PreviewCacheBytes = -1 },
			key:  "core.preview_cache_bytes",
		},
		{
			name: "a negative page cache",
			edit: func(c *config.Core) { c.Persistence.SQLite.PageCacheBytes = -1 },
			key:  "persistence.sqlite.page_cache_bytes",
		},
		{
			name: "a page cache written in KiB instead of bytes",
			edit: func(c *config.Core) { c.Persistence.SQLite.PageCacheBytes = 512 },
			key:  "persistence.sqlite.page_cache_bytes",
		},
		{
			name: "a negative memory limit",
			edit: func(c *config.Core) { c.MemoryLimit = -1 },
			key:  "core.memory_limit",
		},
		{
			name: "a memory limit written in MiB instead of bytes",
			edit: func(c *config.Core) { c.MemoryLimit = 12 },
			key:  "core.memory_limit",
		},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			core := config.Default().WithDefaults(write(t, ""))
			each.edit(&core)

			err := core.Validate()
			require.ErrorIs(t, err, config.ErrInvalid)
			require.ErrorContains(t, err, each.key)
		})
	}
}

// TestTheSmallestMemoryLimitIsAccepted keeps the lower bound where the
// refusal says it is.
func TestTheSmallestMemoryLimitIsAccepted(t *testing.T) {
	t.Parallel()

	core := config.Default().WithDefaults(write(t, ""))
	core.MemoryLimit = config.MinMemoryLimit

	require.NoError(t, core.Validate())
}

// TestAPageCacheOnPostgresIsRefused covers a key nothing would read. The
// page cache belongs to SQLite, and a server on Postgres would ignore it
// without a word.
func TestAPageCacheOnPostgresIsRefused(t *testing.T) {
	t.Parallel()

	path := write(t, `
persistence:
  type: postgres
  postgres:
    dsn: "postgres://db:5432/connect"
  sqlite:
    page_cache_bytes: 524288
`)

	core, err := config.Load(path)
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.ErrorContains(t, err, "persistence.sqlite.page_cache_bytes")
	require.ErrorContains(t, err, "postgres")
}

// TestPostgresTakesNoPageCacheDefault keeps a Postgres file that leaves the
// key out valid, because the default is filled only for SQLite.
func TestPostgresTakesNoPageCacheDefault(t *testing.T) {
	t.Parallel()

	path := write(t, `
persistence:
  type: postgres
  postgres:
    dsn: "postgres://db:5432/connect"
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())
	require.Zero(t, core.Persistence.SQLite.PageCacheBytes)
}
