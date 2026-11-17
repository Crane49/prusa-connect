package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestTheOptionalPartsAreOffUntilTheFileAsksForThem is the default every
// deployment runs on. Each of these keys links code or opens a port, so a file
// that says nothing about one gets a binary without it.
func TestTheOptionalPartsAreOffUntilTheFileAsksForThem(t *testing.T) {
	t.Parallel()

	for name, file := range map[string]string{
		"no block at all":       "core:\n  site: local\n",
		"the block and nothing": "core:\n  observability:\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t, file))
			require.NoError(t, err)
			require.False(t, core.Observability.On())
			require.NoError(t, core.Validate())
		})
	}
}

// TestTheMetricsSwitchIsReadWhereTheBuildReadsIt is the owner's ruling on
// earlier research. `make build` derives the observability tag from this same key,
// so one line in the file decides both halves.
func TestTheMetricsSwitchIsReadWhereTheBuildReadsIt(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, `
core:
  observability:
    enabled: true
`))
	require.NoError(t, err)

	require.True(t, core.Observability.On())
	require.NoError(t, core.Validate())
}

// TestNoMetricsListenerIsBoundUntilTheFileNamesAnAddress keeps the exposition
// on the API listener, behind the fleet grant, for every deployment that did
// not ask for a socket of its own.
func TestNoMetricsListenerIsBoundUntilTheFileNamesAnAddress(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  observability:\n    enabled: true\n"))
	require.NoError(t, err)

	require.True(t, core.Observability.On())
	require.Empty(t, core.Observability.Addr)
	require.NoError(t, core.Validate())
}

// TestAMetricsListenerIsReadOffTheSameBlockAsTheClient holds the address to the
// key that asks for the exposition, so a reader of the block sees the build tag
// and the port together.
func TestAMetricsListenerIsReadOffTheSameBlockAsTheClient(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t,
		"core:\n  observability:\n    enabled: true\n    addr: \":9090\"\n"))
	require.NoError(t, err)

	require.Equal(t, ":9090", core.Observability.Addr)
	require.NoError(t, core.Validate())
}

// TestAMetricsListenerWithNoClientIsRefused is the pair of these two keys that
// can disagree. A binary built without the metrics client has no exposition to
// serve, so the socket would publish a 404 to the network it was opened for.
func TestAMetricsListenerWithNoClientIsRefused(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  observability:\n    addr: \":9090\"\n"))
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "core.observability.addr")
	require.Contains(t, err.Error(), "core.observability.enabled")
}

// TestAMetricsAddressThatWillNotBindIsRefused reports the key by name before a
// port is taken. Binding is the only other way to find out, and by then the
// process is running and a collector is waiting on a socket nobody opened.
func TestAMetricsAddressThatWillNotBindIsRefused(t *testing.T) {
	t.Parallel()

	for name, addr := range map[string]string{
		"no port at all": "0.0.0.0",
		"a named port":   ":metrics",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t,
				"core:\n  observability:\n    enabled: true\n    addr: \""+addr+"\"\n"))
			require.NoError(t, err)

			err = core.Validate()
			require.ErrorIs(t, err, config.ErrInvalid)
			require.Contains(t, err.Error(), "core.observability.addr")
		})
	}
}
