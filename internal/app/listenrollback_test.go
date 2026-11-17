package app

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestARefusedDeviceBindGivesTheAPIPortBack is what `bind` promises in its own
// comment: a failure on a later listener closes the first, so a refused start
// leaves nothing listening.
//
// It was correct and held by nothing. Replacing either closeAll call in
// listen.go with a no-op left the whole suite green, and the process would then
// hold the API port with no server behind it until it exited.
//
// The connector's listener is aimed at a port this test is already holding,
// which is the ordinary way a second bind fails: something else took it first.
func TestARefusedDeviceBindGivesTheAPIPortBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Held open for the whole case, so the printer bind cannot succeed and
	// cannot succeed later either.
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	write(t, filepath.Join(dir, "config.yaml"), lines(
		"core:",
		"  log:",
		"    level: error",
		"  connectors:",
		"    xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        listener:",
		`          addr: "`+taken.Addr().String()+`"`,
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      config:",
		"        root: "+slashed(filepath.Join(dir, "storage"))))

	api := freeAddr(t)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), settle)
	defer cancel()

	err = Run(ctx, Options{
		ConfigPath:   filepath.Join(dir, "config.yaml"),
		APIAddr:      api,
		Version:      "test",
		LoopbackOnly: true,
	})

	require.Error(t, err, "a core whose device listener cannot bind must not report a clean start")

	// The failure has to be the device bind. Something else taking the API
	// address between freeAddr and Run would fail the start too, and the port
	// would then be free below for a reason this case is not testing.
	require.ErrorContains(t, err, taken.Addr().String(),
		"the start failed for a reason other than the device listener's address")

	// The API port is the reading. A process that kept it would answer nothing
	// and would still keep the next start from taking it.
	back, err := net.Listen("tcp", api)
	require.NoError(t, err, "the API listener on %s was not given back after the printer bind failed", api)
	require.NoError(t, back.Close())

	// Nothing answers there either, which separates a port given back from a
	// port a second process happened to take between the two lines above.
	_, err = net.DialTimeout("tcp", api, time.Second)
	require.Error(t, err, "something is still listening on %s", api)
}
