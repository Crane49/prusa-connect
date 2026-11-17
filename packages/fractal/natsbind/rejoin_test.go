package natsbind_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestAJoinRequestFromTheSameIdentityIsGrantedAgain checks a second join
// request from a node that has already joined with the same key. The node
// above grants it with new connection credentials. The node keeps its Node
// Path and its link, and a method invocation still arrives (fractal.md,
// Uniqueness).
func TestAJoinRequestFromTheSameIdentityIsGrantedAgain(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	if err := p.below.binding.Join(ctx); err != nil {
		t.Fatalf("the second Join: %v", err)
	}
	if got := p.below.binding.Path(); got != "hq/printer-1" {
		t.Errorf("Path is %q after the second Join, want hq/printer-1", got)
	}
	waitUntil(t, "hq to see printer-1 as connected again", func() bool {
		for _, n := range p.above.binding.Status().Nodes {
			if n.ID == "printer-1" {
				return n.Connected && n.Admission == natsbind.AdmissionAdmitted
			}
		}
		return false
	})
	send(t, p.above, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "after"), Child: "printer-1", ID: "after",
	})
	awaitKind(t, p.below, fractal.KindRPC, 1)
}

// TestARevokedAdmissionComesBackRevoked checks that a revoked node is still
// revoked after the node above restarts. The account resolver of the NATS
// server is empty at every start, so the node revokes the account again from
// the admissions file (fractal-nats.md, Authorization revocation).
func TestARevokedAdmissionComesBackRevoked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{
		id: "hq", accepts: true, dataDir: t.TempDir(),
	})
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	relay.Open()
	hq := startNode(ctx, t, aboveCfg)

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	startNode(ctx, t, belowCfg)
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(hq.engine.Connected()) == 1 })

	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := hq.binding.Close(); err != nil {
		t.Fatalf("close hq: %v", err)
	}

	again := startNode(ctx, t, aboveCfg)
	nodes := again.binding.Status().Nodes
	if len(nodes) != 1 || nodes[0].Admission != natsbind.AdmissionRevoked {
		t.Fatalf("Status.Nodes is %v, want printer-1 revoked", nodes)
	}
	if again.binding.Admitted("printer-1") {
		t.Error("Admitted is true for a revoked node after the restart")
	}
}

// TestARevocationCanBeRepeatedAfterARestart revokes a node, restarts the node
// above and revokes it again. The node above runs the revocation again for a
// node its record already marks revoked, and the link it restored for that
// node has no connection.
func TestARevocationCanBeRepeatedAfterARestart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true, dataDir: t.TempDir()})
	hq := startNode(ctx, t, aboveCfg)
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	startNode(ctx, t, belowCfg)
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(hq.engine.Connected()) == 1 })
	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := hq.binding.Close(); err != nil {
		t.Fatalf("close hq: %v", err)
	}

	again := startNode(ctx, t, aboveCfg)
	if err := again.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Errorf("Revoke after the restart: %v", err)
	}
}

// TestANodeJoinsOnceWhenItStarts runs a node that has never joined. It sends
// one join request, and the leafnode connection it opens with the grant stays
// open. Each join request comes over a new connection to the client
// listener of the node above, so a relay there counts them.
func TestANodeJoinsOnceWhenItStarts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	leafRelay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + leafRelay.Addr()
	leafRelay.Open()
	startNode(ctx, t, aboveCfg)
	joinRelay := newProxy(t, aboveCfg.ClientListen)
	joinRelay.Open()

	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: joinRelay.Addr()})
	below := buildNode(ctx, t, belowCfg)
	runUntilParentUp(ctx, t, below)

	got := map[string]int{
		"join requests":        joinRelay.Accepted(),
		"leafnode connections": leafRelay.Accepted(),
		"parent link losses":   below.engine.ParentLosses(),
	}
	want := map[string]int{"join requests": 1, "leafnode connections": 1, "parent link losses": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the start the counts are %v, want %v", got, want)
	}
}

// TestANodeKeepsItsLinkWhenItRestarts restarts a node that has a saved Node
// Path while the node above is up. It joins before it connects, so the grant
// does not close the leafnode connection it opened with its saved credentials.
func TestANodeKeepsItsLinkWhenItRestarts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	leafRelay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + leafRelay.Addr()
	leafRelay.Open()
	// The restart joins within a few milliseconds of the first join, faster
	// than the default rate of the node above allows.
	aboveCfg.JoinsPerSecond = 1000
	startNode(ctx, t, aboveCfg)
	joinRelay := newProxy(t, aboveCfg.ClientListen)
	joinRelay.Open()
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: joinRelay.Addr(), dataDir: t.TempDir()})
	first := startNode(ctx, t, belowCfg)
	waitUntil(t, "printer-1 to see its parent link up", func() bool { return first.engine.ParentConnections() == 1 })
	if err := first.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}
	// A relay counts a connection when it accepts it, which can be after the
	// node has already used it.
	waitUntil(t, "the relays to count the first run", func() bool {
		return joinRelay.Accepted() == 1 && leafRelay.Accepted() == 1
	})

	again := buildNode(ctx, t, belowCfg)
	runUntilParentUp(ctx, t, again)

	got := map[string]int{
		"join requests":        joinRelay.Accepted(),
		"leafnode connections": leafRelay.Accepted(),
		"parent link losses":   again.engine.ParentLosses(),
	}
	want := map[string]int{"join requests": 2, "leafnode connections": 2, "parent link losses": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the restart the counts are %v, want %v", got, want)
	}
}

// TestANodeWithChildrenFollowsItsParentToANewAddress restarts a node that has
// a node below it, with a new address of the same node above. The grant
// carries the saved parent identity, so the node keeps its Node Path and
// links up (fractal.md, After the Join Exchange).
func TestANodeWithChildrenFollowsItsParentToANewAddress(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	// brno asks again within milliseconds, faster than the default rate allows.
	hqCfg.JoinsPerSecond = 1000
	startNode(ctx, t, hqCfg)
	oldAddr, newAddr := newProxy(t, hqCfg.ClientListen), newProxy(t, hqCfg.ClientListen)
	oldAddr.Open()
	newAddr.Open()
	brnoCfg, _ := baseConfig(t, nodeOptions{
		id: "brno", parent: oldAddr.Addr(), accepts: true, dataDir: t.TempDir(),
	})
	brno := startNode(ctx, t, brnoCfg)
	printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brnoCfg.ClientListen})
	startNode(ctx, t, printerCfg)
	if err := brno.binding.Close(); err != nil {
		t.Fatalf("close brno: %v", err)
	}

	brnoCfg.ParentAddr = newAddr.Addr()
	again := buildNode(ctx, t, brnoCfg)
	runUntilParentUp(ctx, t, again)

	if got := again.binding.Path(); got != "hq/brno" {
		t.Errorf("Path is %q after the restart at the new address, want hq/brno", got)
	}
}

// TestANodeWithChildrenRefusesAnotherParent restarts a node that has a node
// below it, with the address of another node above. That node grants, the
// grant carries another parent identity, and the node refuses it before it
// opens the connection. It serves the node below, and it waits and asks
// no more, so its process is not started again to ask (fractal.md, After the
// Join Exchange).
func TestANodeWithChildrenRefusesAnotherParent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(ctx, t, hqCfg)
	prahaCfg, _ := baseConfig(t, nodeOptions{id: "praha", accepts: true})
	startNode(ctx, t, prahaCfg)
	asksPraha := newProxy(t, prahaCfg.ClientListen)
	asksPraha.Open()
	brnoCfg, _ := baseConfig(t, nodeOptions{
		id: "brno", parent: hqCfg.ClientListen, accepts: true, dataDir: t.TempDir(),
	})
	brno := startNode(ctx, t, brnoCfg)
	printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brnoCfg.ClientListen})
	startNode(ctx, t, printerCfg)
	if err := brno.binding.Close(); err != nil {
		t.Fatalf("close brno: %v", err)
	}

	brnoCfg.ParentAddr = asksPraha.Addr()
	again := buildNode(ctx, t, brnoCfg)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- again.binding.Run(runCtx) }()
	waitUntil(t, "brno to refuse praha", func() bool { return again.binding.Status().Uplink.Refused })
	waitUntil(t, "printer-1 to link to brno again", func() bool { return len(again.engine.Connected()) == 1 })
	cancel()

	got := map[string]any{"run": <-ran, "path": again.binding.Path(), "join requests": asksPraha.Accepted()}
	want := map[string]any{"run": context.Canceled, "path": fractal.NodePath("hq/brno"), "join requests": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the restart under praha brno has %v, want %v", got, want)
	}
}

// TestARestartedNodeStartsWhenItsParentIsSilent restarts a node whose node
// above accepts the connection and never answers, as a host behind a firewall
// that drops packets does. The node starts on its saved credentials within
// the wait for its first join, not the longer timeout of one request, and it
// stops at once while it joins again.
func TestARestartedNodeStartsWhenItsParentIsSilent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	startNode(ctx, t, hqCfg)
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: hqCfg.ClientListen, dataDir: t.TempDir()})
	first := startNode(ctx, t, belowCfg)
	if err := first.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}
	silent, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	t.Cleanup(func() { _ = silent.Close() })
	go func() {
		var held []net.Conn
		for {
			conn, acceptErr := silent.Accept()
			if acceptErr != nil {
				for _, c := range held {
					_ = c.Close()
				}
				return
			}
			held = append(held, conn)
		}
	}()

	belowCfg.ParentAddr = silent.Addr().String()
	again := buildNode(ctx, t, belowCfg)
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	began := time.Now()
	go func() { done <- again.binding.Run(runCtx) }()
	waitUntil(t, "printer-1 to see its parent link up", func() bool { return again.engine.ParentConnections() == 1 })
	started := time.Since(began)
	// Run is joining the silent node again by now, and must still stop at once.
	stopping := time.Now()
	stop()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	stopped := time.Since(stopping)

	got := map[string]bool{
		"started within the wait for the first join": started < 10*time.Second,
		"stopped at once": stopped < 2*time.Second,
	}
	want := map[string]bool{
		"started within the wait for the first join": true,
		"stopped at once": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("printer-1 started after %v and stopped after %v: %v, want %v", started, stopped, got, want)
	}
}

// runUntilParentUp calls Run on a node, waits until its parent link is up and
// stays up for settle, and stops it.
func runUntilParentUp(ctx context.Context, t *testing.T, n *node) {
	t.Helper()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- n.binding.Run(runCtx) }()
	waitUntil(t, "the parent link to come up", func() bool { return n.engine.ParentConnections() == 1 })
	time.Sleep(settle) // the window in which a second join or a lost link would show
	stop()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
}

// TestAdmittedIsSafeDuringARejoin reads Admitted on the node above while the
// node below joins again. Each grant writes the admission of its link. The
// race detector is the real check: a run with -race fails when Admitted reads
// the admission without the lock of the link. The count only catches a lost
// admission.
func TestAdmittedIsSafeDuringARejoin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	stop := make(chan struct{})
	stopReading := sync.OnceFunc(func() { close(stop) })
	t.Cleanup(stopReading)
	refused := make(chan int, 1)
	go func() {
		n := 0
		for {
			select {
			case <-stop:
				refused <- n
				return
			default:
			}
			if !p.above.binding.Admitted("printer-1") {
				n++
			}
		}
	}()
	for range 5 {
		if err := p.below.binding.Join(ctx); err != nil {
			t.Fatalf("Join again: %v", err)
		}
	}
	stopReading()
	if n := <-refused; n != 0 {
		t.Errorf("Admitted was false %d times for a node that stayed admitted", n)
	}
}

// TestARevokedNodeStopsDialingItsParent: after ERR_JOIN_REVOKED a node does not
// dial the leafnode port of its parent again (fractal.md, Error Codes: "MUST
// NOT retry"). A relay in front of that port counts the dials. The revoked
// node keeps the link to the node below it.
func TestARevokedNodeStopsDialingItsParent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tr := newTree(ctx, t)

	if err := tr.hq.binding.Revoke(ctx, "brno"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	var denial *join.DenialError
	if err := tr.brno.binding.Join(ctx); !errors.As(err, &denial) || denial.Denial.Code != fractal.CodeJoinRevoked {
		t.Fatalf("Join after the revocation returned %v, want ERR_JOIN_REVOKED", err)
	}
	waitUntil(t, "brno's connection to hq to close", func() bool { return tr.toBrno.Live() == 0 })
	before := tr.toBrno.Accepted()
	// A dial proves itself only by happening, so the test watches three of
	// the NATS server's leafnode redial intervals for one.
	<-time.After(3 * server.DEFAULT_LEAF_NODE_RECONNECT)
	if dials := tr.toBrno.Accepted() - before; dials != 0 {
		t.Errorf("brno dialed hq's leafnode port %d times after ERR_JOIN_REVOKED, want 0", dials)
	}

	send(t, tr.printer, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: repPayload(t, "hq/brno/printer-1", 1)})
	awaitKind(t, tr.brno, fractal.KindREP, 1)
}

// TestARevokedNodeDoesNotAskAgainWhenItStarts: a node that starts and is
// denied with ERR_JOIN_REVOKED sends no second join request and does not
// return from Run. So its process is not started again to ask once more
// (fractal.md, Error Codes: "MUST NOT retry").
func TestARevokedNodeDoesNotAskAgainWhenItStarts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	hq := startNode(ctx, t, hqCfg)
	asks := newProxy(t, hqCfg.ClientListen)
	asks.Open()
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: asks.Addr(), dataDir: t.TempDir()})
	first := startNode(ctx, t, belowCfg)
	if err := first.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}
	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	waiting := newLogWatch(refusedLog)
	belowCfg.Logger = slog.New(waiting)
	again := buildNode(ctx, t, belowCfg)
	stop := runInBackground(ctx, t, again)
	waitUntil(t, "printer-1 to learn of the revocation", func() bool {
		return again.binding.Status().Uplink.Code == fractal.CodeJoinRevoked
	})
	before := asks.Accepted()
	awaitLog(t, waiting)

	got := map[string]any{"run": stop(), "asked": asks.Accepted() - before}
	want := map[string]any{"run": nil, "asked": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation Run and the join requests are %v, want %v", got, want)
	}
}

// TestARevokedNodeServesItsChildrenWhenItStarts: a node with a node below it
// that starts and is denied with ERR_JOIN_REVOKED still starts its NATS
// server, so the node below links to it again. Only the node above is not
// asked again (fractal.md, Error Codes: "MUST NOT retry").
func TestARevokedNodeServesItsChildrenWhenItStarts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	// brno asks again within milliseconds, faster than the default rate allows.
	hqCfg.JoinsPerSecond = 1000
	hq := startNode(ctx, t, hqCfg)
	asks := newProxy(t, hqCfg.ClientListen)
	asks.Open()
	brnoCfg, _ := baseConfig(t, nodeOptions{id: "brno", parent: asks.Addr(), accepts: true, dataDir: t.TempDir()})
	brno := startNode(ctx, t, brnoCfg)
	printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brnoCfg.ClientListen})
	startNode(ctx, t, printerCfg)
	if err := brno.binding.Close(); err != nil {
		t.Fatalf("close brno: %v", err)
	}
	if err := hq.binding.Revoke(ctx, "brno"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	again := buildNode(ctx, t, brnoCfg)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- again.binding.Run(runCtx) }()
	waitUntil(t, "brno to learn of the revocation", func() bool {
		return again.binding.Status().Uplink.Code == fractal.CodeJoinRevoked
	})
	waitUntil(t, "printer-1 to link to brno again", func() bool { return len(again.engine.Connected()) == 1 })
	cancel()

	got := map[string]any{"run": <-ran, "join requests": asks.Accepted()}
	want := map[string]any{"run": context.Canceled, "join requests": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the restart Run and the join requests are %v, want %v", got, want)
	}
}

// TestARevocationLearnedElsewhereStopsTheRenewals: a Join from outside Run
// that learns of a revocation stops the renewals of Run, and Run keeps the
// node up until its context ends. A second Join asks the node above nothing.
// The files client calls Join so when the node above refuses its
// credentials.
func TestARevocationLearnedElsewhereStopsTheRenewals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	hq := startNode(ctx, t, hqCfg)
	asks := newProxy(t, hqCfg.ClientListen)
	asks.Open()
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: asks.Addr()})
	renewalsStopped := newLogWatch(refusedLog)
	belowCfg.Logger = slog.New(renewalsStopped)
	below := buildNode(ctx, t, belowCfg)
	stop := runInBackground(ctx, t, below)
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(hq.engine.Connected()) >= 1 })

	if err := hq.binding.Revoke(ctx, "printer-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	first := below.binding.Join(ctx)
	before := asks.Accepted()
	awaitLog(t, renewalsStopped)
	second := below.binding.Join(ctx)

	got := map[string]any{
		"first join": denialCode(first), "second join": denialCode(second),
		"asked": asks.Accepted() - before, "run": stop(),
	}
	want := map[string]any{
		"first join": fractal.CodeJoinRevoked, "second join": fractal.CodeJoinRevoked,
		"asked": 0, "run": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation Run and the join requests are %v, want %v", got, want)
	}
}

// denialCode returns the code of the denial in err, or zero.
func denialCode(err error) fractal.Code {
	var denial *join.DenialError
	if !errors.As(err, &denial) {
		return 0
	}
	return denial.Denial.Code
}
