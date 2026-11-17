package natsbind_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestTheBindingPassesTheConformanceSuite runs the conformance suite of
// sdk/fractal/fractaltest against two real NATS servers in this process. The
// suite tests the transport interface of sdk/fractal/node and uses no detail
// of this package, so the in-memory transport and this one pass the same
// cases.
//
// It is the one end-to-end test here that does not run in parallel. The suite
// waits a fixed five seconds for each message, and on a runner with two cores,
// shared with every other test in this package and under the race detector,
// a case once took longer than that. A test that is not parallel finishes
// before the parallel ones start, so the suite has the machine to itself.
func TestTheBindingPassesTheConformanceSuite(t *testing.T) {
	fractaltest.RunBindingSuite(t, natsLink)
}

// natsLink builds two nodes for one case of the suite: a root node and a node
// that joins it through a relay.
func natsLink(t *testing.T, cfg fractaltest.LinkConfig) fractaltest.TestLink {
	t.Helper()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: cfg.Parent.Last(), accepts: true})
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	above := startEngineNode(ctx, t, aboveCfg, cfg.Above)

	belowCfg, _ := baseConfig(t, nodeOptions{id: cfg.Child, parent: aboveCfg.ClientListen})
	below := startEngineNode(ctx, t, belowCfg, cfg.Below)
	return &natsTestLink{above: above, below: below, relay: relay, child: cfg.Child}
}

// startEngineNode builds, joins and starts one node with the engine from the
// suite.
func startEngineNode(
	ctx context.Context,
	t *testing.T,
	cfg natsbind.Config,
	engine fractaltest.Engine,
) *natsbind.Binding {
	t.Helper()
	binding, err := natsbind.New(ctx, cfg, suiteEngine{engine})
	if err != nil {
		t.Fatalf("build %s: %v", cfg.NodeID, err)
	}
	if joinErr := binding.Join(ctx); joinErr != nil {
		t.Fatalf("join %s: %v", cfg.NodeID, joinErr)
	}
	if startErr := binding.Start(ctx); startErr != nil {
		t.Fatalf("start %s: %v", cfg.NodeID, startErr)
	}
	t.Cleanup(func() {
		if closeErr := binding.Close(); closeErr != nil {
			t.Errorf("stop %s: %v", cfg.NodeID, closeErr)
		}
	})
	return binding
}

// suiteEngine gives an engine of the suite the two methods of natsbind.Engine
// that fractaltest.Engine lacks. The in-memory transport has no revocation, so
// the suite has no case for it.
type suiteEngine struct{ fractaltest.Engine }

// ForgetAncestors passes the call on when the engine of the suite has it.
func (e suiteEngine) ForgetAncestors(ctx context.Context) {
	if forgetter, ok := e.Engine.(interface{ ForgetAncestors(ctx context.Context) }); ok {
		forgetter.ForgetAncestors(ctx)
	}
}

// ChildRevoked forwards the call when the engine of the suite has it.
func (e suiteEngine) ChildRevoked(ctx context.Context, id fractal.NodeID) {
	if revoker, ok := e.Engine.(interface {
		ChildRevoked(ctx context.Context, id fractal.NodeID)
	}); ok {
		revoker.ChildRevoked(ctx, id)
	}
}

// natsTestLink implements fractaltest.TestLink with two nodes. Their leafnode
// connection goes through the relay, so a case can cut it.
type natsTestLink struct {
	above *natsbind.Binding
	below *natsbind.Binding
	relay *proxy
	child fractal.NodeID
}

// ParentSide returns the Binding of the node above.
func (l *natsTestLink) ParentSide() fractalnode.Binding { return l.above }

// ChildSide returns the Binding of the node that joined.
func (l *natsTestLink) ChildSide() fractalnode.Binding { return l.below }

// Connect opens the relay and waits until each node sees the other as
// connected. A message the case sends right after Connect is then delivered
// and not dropped.
func (l *natsTestLink) Connect(context.Context) {
	l.relay.Open()
	l.await(true)
}

// Cut closes the relay and waits until each node sees the other as
// disconnected.
func (l *natsTestLink) Cut(context.Context) {
	l.relay.Cut()
	l.await(false)
}

// Restore opens the relay again.
func (l *natsTestLink) Restore(ctx context.Context) { l.Connect(ctx) }

// Close closes both nodes. The test cleanup closes them again, and a second
// Close does nothing.
func (l *natsTestLink) Close() {
	_ = l.below.Close() // Binding.Close always returns nil.
	_ = l.above.Close()
	l.relay.stop()
}

// await waits until the Status of both nodes shows the link as connected (up)
// or disconnected. It gives up after testWait without an error.
func (l *natsTestLink) await(up bool) {
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if l.childConnected() == up && l.parentConnected() == up {
			return
		}
		time.Sleep(testPoll)
	}
}

// parentConnected reports whether the Status of the node below shows the node
// above as connected.
func (l *natsTestLink) parentConnected() bool {
	return l.below.Status().Uplink.State == natsbind.UplinkConnected
}

// childConnected reports whether the Status of the node above shows the node
// below as connected.
func (l *natsTestLink) childConnected() bool {
	for _, n := range l.above.Status().Nodes {
		if n.ID == l.child {
			return n.Connected
		}
	}
	return false
}
