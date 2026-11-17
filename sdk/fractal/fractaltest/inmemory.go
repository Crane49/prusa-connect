package fractaltest

import (
	"context"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// InMemoryLink is the LinkFactory for the in-memory transport of this package.
// The tests of this package pass it to RunBindingSuite. It is also the example
// to follow for the LinkFactory of a transport over a real network.
func InMemoryLink(t *testing.T, cfg LinkConfig) TestLink {
	t.Helper()
	link, err := NewLink(cfg)
	if err != nil {
		t.Fatalf("build the link: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	running := &runningLink{Link: link, cancel: cancel}
	link.Start(ctx, &running.wg)
	return running
}

// runningLink makes a started Link into a TestLink. cancel stops the
// goroutines of the link, and wg waits for them.
type runningLink struct {
	*Link

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ParentSide returns the binding that the node above sends through.
func (r *runningLink) ParentSide() node.Binding { return r.Link.ParentSide() }

// ChildSide returns the binding that the node below sends through.
func (r *runningLink) ChildSide() node.Binding { return r.Link.ChildSide() }

// Close stops the goroutines of the link and waits until they end.
func (r *runningLink) Close() {
	r.cancel()
	r.wg.Wait()
}
