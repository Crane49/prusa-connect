package natsbind_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// noticeKey is the interface hq exposes downward in these tests, as the
// ancestor shadow of printer-1 names it.
func noticeKey() shadow.Key {
	return shadow.Key{Node: "hq", Interface: "Notice1", Instance: "1"}
}

var _ natsbind.Engine = (*nodeEngine)(nil)

// nodeEngine passes every call of a Binding to a Fractal node and counts the
// calls about the parent link. natsbind.New needs the engine before the node
// exists, so the node is stored after it is built.
type nodeEngine struct {
	node      atomic.Pointer[fractalnode.Node]
	connected atomic.Int32
	lost      atomic.Int32
}

// Deliver passes one message to the node.
func (e *nodeEngine) Deliver(ctx context.Context, in fractalnode.Inbound) {
	if n := e.node.Load(); n != nil {
		n.Deliver(ctx, in)
	}
}

// ChildConnected passes the call to the node.
func (e *nodeEngine) ChildConnected(ctx context.Context, id fractal.NodeID) {
	if n := e.node.Load(); n != nil {
		n.ChildConnected(ctx, id)
	}
}

// ChildLost passes the call to the node.
func (e *nodeEngine) ChildLost(ctx context.Context, id fractal.NodeID) {
	if n := e.node.Load(); n != nil {
		n.ChildLost(ctx, id)
	}
}

// ParentConnected counts the call and passes it to the node.
func (e *nodeEngine) ParentConnected(ctx context.Context) {
	e.connected.Add(1)
	if n := e.node.Load(); n != nil {
		n.ParentConnected(ctx)
	}
}

// ParentLost counts the call and passes it to the node.
func (e *nodeEngine) ParentLost(ctx context.Context) {
	e.lost.Add(1)
	if n := e.node.Load(); n != nil {
		n.ParentLost(ctx)
	}
}

// ChildRevoked passes the call to the node.
func (e *nodeEngine) ChildRevoked(ctx context.Context, id fractal.NodeID) {
	if n := e.node.Load(); n != nil {
		n.ChildRevoked(ctx, id)
	}
}

// ForgetAncestors passes the call to the node.
func (e *nodeEngine) ForgetAncestors(ctx context.Context) {
	if n := e.node.Load(); n != nil {
		n.ForgetAncestors(ctx)
	}
}

// SetPath passes the granted Node Path to the node.
func (e *nodeEngine) SetPath(ctx context.Context, path fractal.NodePath) error {
	if n := e.node.Load(); n != nil {
		return n.SetPath(ctx, path)
	}
	return nil
}

// fullNode is a Binding with a Fractal node behind it.
type fullNode struct {
	binding *natsbind.Binding
	node    *fractalnode.Node
	engine  *nodeEngine
}

// startFull builds a Binding and a Fractal node on it, joins the node above,
// and starts both. ancestors is the ancestor shadow of the node, or nil. Both
// stop when the test ends, the node first.
func startFull(ctx context.Context, t testing.TB, cfg natsbind.Config, ancestors *shadow.Store) *fullNode {
	t.Helper()
	engine := &nodeEngine{}
	binding, err := natsbind.New(ctx, cfg, engine)
	if err != nil {
		t.Fatalf("build %s: %v", cfg.NodeID, err)
	}
	t.Cleanup(func() {
		if closeErr := binding.Close(); closeErr != nil {
			t.Errorf("stop %s: %v", cfg.NodeID, closeErr)
		}
	})
	if joinErr := binding.Join(ctx); joinErr != nil {
		t.Fatalf("join %s: %v", cfg.NodeID, joinErr)
	}
	node, err := fractalnode.New(fractalnode.Config{
		Binding:         binding,
		Admissions:      binding,
		Executed:        binding,
		Processed:       binding,
		Ancestors:       ancestors,
		ARBRWindow:      testPoll,
		ARBRMinInterval: testPoll,
		Path:            binding.Path(),
	})
	if err != nil {
		t.Fatalf("build the node %s: %v", cfg.NodeID, err)
	}
	engine.node.Store(node)
	if startErr := node.Start(ctx); startErr != nil {
		t.Fatalf("start the node %s: %v", cfg.NodeID, startErr)
	}
	t.Cleanup(func() {
		if closeErr := node.Close(); closeErr != nil {
			t.Errorf("stop the node %s: %v", cfg.NodeID, closeErr)
		}
	})
	if startErr := binding.Start(ctx); startErr != nil {
		t.Fatalf("start %s: %v", cfg.NodeID, startErr)
	}
	return &fullNode{binding: binding, node: node, engine: engine}
}

// fullTree is hq, brno and printer-1 with a Fractal node on each. hq owns
// Notice1, exposed downward, and printer-1 keeps it in its ancestor shadow.
// brno keeps no ancestor shadow, so it only passes the messages on.
type fullTree struct {
	hq, brno, printer *fullNode
	toBrno, toPrint   *proxy
	owner             *fractaltest.StateOwner
	notice            *fractalnode.Interface
}

// growTree starts the full tree. Notice1 is registered before brno joins, so
// every full state report hq sends has it.
func growTree(ctx context.Context, t testing.TB) *fullTree {
	t.Helper()
	s := setUpTree(t)
	ancestors, err := shadow.New(shadow.Config{
		Selection: shadow.Selection{Interfaces: []shadow.Key{noticeKey()}},
	})
	if err != nil {
		t.Fatalf("build the ancestor shadow of printer-1: %v", err)
	}
	hq := startFull(ctx, t, s.hq, nil)
	owner := fractaltest.NewStateOwner(fractal.Props{"message": json.RawMessage(`"hello"`)})
	notice, err := hq.node.Register(fractalnode.Instance{
		Owner:    owner,
		Name:     "Notice1",
		Instance: "1",
		Exposure: schema.ExposureDownward,
	})
	if err != nil {
		t.Fatalf("register Notice1 on hq: %v", err)
	}
	brno := startFull(ctx, t, s.brno, nil)
	printer := startFull(ctx, t, s.printer, ancestors)
	return &fullTree{
		hq: hq, brno: brno, printer: printer,
		toBrno: s.toBrno, toPrint: s.toPrint,
		owner: owner, notice: notice,
	}
}

// awaitNotice waits until the ancestor shadow of printer-1 holds Notice1 of hq
// with this status and this message.
func awaitNotice(t testing.TB, tr *fullTree, status shadow.Status, message string) {
	t.Helper()
	store := tr.printer.node.Ancestors()
	waitUntil(t, fmt.Sprintf("Notice1 of hq to be %s with %s at printer-1", status, message), func() bool {
		held, _, ok := store.Status(noticeKey())
		value, _ := store.Prop(noticeKey(), "message")
		return ok && held == status && string(value) == message
	})
}

// TestAnAncestorShadowFollowsTheParentLink runs the ancestor messages on a
// tree of three servers. A report of hq reaches the ancestor shadow of
// printer-1. A cut between brno and hq marks it stale through the ALOS brno
// sends. brno redials through the relay only, so the shadow stays stale until
// the relay opens, and the ARBR brno sends on the new link brings the full
// state back (fractal-nats.md, AREP, ALOS, ARBR and Connectivity Detection).
func TestAnAncestorShadowFollowsTheParentLink(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := growTree(ctx, t)
	awaitNotice(t, tr, shadow.StatusOK, `"hello"`)

	tr.owner.Set("message", json.RawMessage(`"changed"`))
	if err := tr.notice.Report(ctx, fractal.Props{"message": json.RawMessage(`"changed"`)},
		fractal.TimestampOf(time.Now())); err != nil {
		t.Fatalf("report Notice1 on hq: %v", err)
	}
	awaitNotice(t, tr, shadow.StatusOK, `"changed"`)

	tr.toBrno.Cut()
	awaitNotice(t, tr, shadow.StatusStale, `"changed"`)
	waitUntil(t, "the relay to refuse a redial of brno", func() bool { return tr.toBrno.Refused() >= 1 })
	if held, _, _ := tr.printer.node.Ancestors().Status(noticeKey()); held != shadow.StatusStale {
		t.Fatalf("Notice1 of hq is %s at printer-1 while the relay is cut, want %s", held, shadow.StatusStale)
	}

	tr.toBrno.Open()
	awaitNotice(t, tr, shadow.StatusOK, `"changed"`)
	got := parentSignals{int(tr.brno.engine.connected.Load()), int(tr.brno.engine.lost.Load())}
	if want := (parentSignals{connected: 2, lost: 1}); got != want {
		t.Errorf("brno got the parent link calls %+v, want %+v", got, want)
	}
}

// TestARenewalRepairsTheAncestorShadow renews the credentials of printer-1.
// The renewal closes and opens its parent link, so printer-1 marks its
// ancestor shadow stale, and the ARBR it sends on the new link brings it back
// (fractal-nats.md, Provisioning the Link).
func TestARenewalRepairsTheAncestorShadow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := growTree(ctx, t)
	awaitNotice(t, tr, shadow.StatusOK, `"hello"`)
	waitUntil(t, "printer-1 to see its parent link open", func() bool {
		return tr.printer.engine.connected.Load() >= 1
	})

	if err := tr.printer.binding.Join(ctx); err != nil {
		t.Fatalf("renew the credentials of printer-1: %v", err)
	}
	awaitNotice(t, tr, shadow.StatusStale, `"hello"`)
	awaitNotice(t, tr, shadow.StatusOK, `"hello"`)

	got := parentSignals{int(tr.printer.engine.connected.Load()), int(tr.printer.engine.lost.Load())}
	if want := (parentSignals{connected: 2, lost: 1}); got != want {
		t.Errorf("printer-1 got the parent link calls %+v, want %+v", got, want)
	}
}

// TestARevokedNodeForgetsItsAncestorShadow revokes printer-1 on brno. The
// next join request of printer-1 is denied with ERR_JOIN_REVOKED, and the node
// then holds nothing of the nodes above (fractal.md, Admission and
// Revocation).
func TestARevokedNodeForgetsItsAncestorShadow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := growTree(ctx, t)
	awaitNotice(t, tr, shadow.StatusOK, `"hello"`)

	if err := tr.brno.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	var denial *join.DenialError
	if err := tr.printer.binding.Join(ctx); !errors.As(err, &denial) || denial.Denial.Code != fractal.CodeJoinRevoked {
		t.Fatalf("the join request of a revoked node returned %v, want ERR_JOIN_REVOKED", err)
	}

	if got := tr.printer.node.Ancestors().Len(); got != 0 {
		t.Errorf("printer-1 keeps %d interfaces of the nodes above after its revocation, want none", got)
	}
}
