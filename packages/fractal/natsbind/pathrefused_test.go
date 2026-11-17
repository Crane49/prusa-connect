package natsbind_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
)

var errPathRefused = errors.New("the copies of the former parent cannot be dropped")

// pathRefusing is an engine that cannot take the Node Path it is granted, as
// the engine of the module cannot when the downward cache cannot drop its
// copies.
type pathRefusing struct {
	*fractaltest.Recorder
}

func (pathRefusing) SetPath(context.Context, fractal.NodePath) error { return errPathRefused }

// TestANodeThatCannotTakeItsPathDoesNotStart: a node must drop the caches of
// its former parent before it brings up the connection (fractal-files.md,
// Relaying and Caching), so an engine that cannot take the granted Node Path keeps the
// node down.
func TestANodeThatCannotTakeItsPathDoesNotStart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	binding, err := natsbind.New(ctx, belowCfg, pathRefusing{Recorder: fractaltest.NewRecorder(true)})
	if err != nil {
		t.Fatalf("build printer-1: %v", err)
	}
	t.Cleanup(func() { _ = binding.Close() })

	if err = binding.Join(ctx); err != nil {
		t.Fatalf("join printer-1: %v", err)
	}
	if err = binding.Start(ctx); !errors.Is(err, errPathRefused) {
		t.Errorf("Start returned %v, want the error of SetPath", err)
	}
}
