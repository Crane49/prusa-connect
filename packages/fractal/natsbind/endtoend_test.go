package natsbind_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prusa3d/fractal-link/internal/testport"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// pair is a root node that accepts joins, one node that joined it, and the
// relay their leafnode connection goes through.
type pair struct {
	above *node
	below *node
	link  *proxy
}

// newPair starts the node hq and the node printer-1, which joins hq. It
// returns after each node sees the other as connected.
func newPair(ctx context.Context, t testing.TB) *pair {
	t.Helper()
	p := startPair(ctx, t, nodeOptions{id: "printer-1"})
	p.link.Open()
	waitUntil(t, "the link to come up", func() bool {
		return len(p.above.engine.Connected()) == 1 && p.below.engine.ParentConnections() == 1
	})
	return p
}

// startPair starts both nodes with the relay closed. The node below connects
// only after the test calls link.Open.
func startPair(ctx context.Context, t testing.TB, below nodeOptions) *pair {
	t.Helper()
	aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
	aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
	aboveNode := startNode(ctx, t, aboveCfg)

	below.parent = aboveCfg.ClientListen
	belowCfg, _ := baseConfig(t, below)
	belowNode := startNode(ctx, t, belowCfg)
	return &pair{above: aboveNode, below: belowNode, link: relay}
}

// TestAJoinedNodeTakesThePathItWasGranted checks a whole join between two
// nodes. The node that joined has the Node Path the node above assigned, and
// the node above lists it as admitted and connected.
func TestAJoinedNodeTakesThePathItWasGranted(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	if got := p.below.binding.Path(); got != "hq/printer-1" {
		t.Errorf("the Path of the node that joined is %q, want %q", got, "hq/printer-1")
	}
	if !p.above.binding.Admitted("printer-1") {
		t.Error("Admitted is false on the node above for the node that joined")
	}
	status := p.above.binding.Status()
	if len(status.Nodes) != 1 || status.Nodes[0].Path != "hq/printer-1" {
		t.Fatalf("Status.Nodes is %v, want one node at hq/printer-1", status.Nodes)
	}
	if !status.Nodes[0].Connected {
		t.Error("Status shows the node that joined as not connected")
	}
	if len(status.Listeners) != 2 {
		t.Errorf("Status lists %d listeners, want the client and the leafnode listener", len(status.Listeners))
	}
}

// TestAStateReportReachesTheNodeAbove checks that SendBestEffort delivers a
// state report from the node below to the engine of the node above.
func TestAStateReportReachesTheNodeAbove(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	payload := repPayload(t, "hq/printer-1", 1)
	if err := p.below.binding.SendBestEffort(ctx, fractalnode.Dispatch{
		Kind: fractal.KindREP, Payload: payload,
	}); err != nil {
		t.Fatalf("send a state report: %v", err)
	}
	waitUntil(t, "the state report to arrive", func() bool {
		for _, m := range p.above.engine.Messages() {
			if m.Kind == fractal.KindREP && m.From == "printer-1" {
				return true
			}
		}
		return false
	})
}

// TestTheParentLinkIsUpOnceTheNodeAboveListens grants printer-1 the leafnode
// address of a bare NATS server, where nobody listens on the subjects a node
// sends upward. The leafnode connection opens, but a message sent on it now
// would be dropped, so the link is up only after a client there subscribes.
// The node above sends its subscriptions right after it accepts the
// connection, and a node that sends an ARBR in that gap waits for an answer
// that never comes (fractal-nats.md, Connectivity Detection).
func TestTheParentLinkIsUpOnceTheNodeAboveListens(t *testing.T) {
	t.Parallel()
	bare, below := startUnderBareServer(t.Context(), t)

	waitUntil(t, "printer-1 to connect to the bare server", func() bool { return bare.NumLeafNodes() == 1 })
	for deadline := time.Now().Add(settle); time.Now().Before(deadline); time.Sleep(testPoll) {
		if got := below.engine.ParentConnections(); got != 0 {
			t.Fatalf("printer-1 saw its parent link up %d times while nobody listened above, want 0", got)
		}
	}
	listenOn(t, bare, natsbind.SubjectREP, natsbind.SubjectLOS, natsbind.SubjectARBR, natsbind.SubjectHB)
	waitUntil(t, "printer-1 to see its parent link up", func() bool { return below.engine.ParentConnections() == 1 })
}

// TestTheParentLinkIsUpUnderAnEarlierRelease puts printer-1 under a node above
// that listens only on what the release 0.0.1-alpha.6 listens on, which has no
// ARBR. That node is still the node above, so the link comes up.
func TestTheParentLinkIsUpUnderAnEarlierRelease(t *testing.T) {
	t.Parallel()
	bare, below := startUnderBareServer(t.Context(), t)
	waitUntil(t, "printer-1 to connect to the bare server", func() bool { return bare.NumLeafNodes() == 1 })

	listenOn(t, bare, natsbind.SubjectREP, natsbind.SubjectLOS, natsbind.SubjectHB)
	waitUntil(t, "printer-1 to see its parent link up", func() bool { return below.engine.ParentConnections() == 1 })
}

// TestOnePollDoesNotBringTheParentLinkUp checks that the link comes up only on
// the second poll that sees the node above listen. The server below applies
// the subscriptions it is sent one at a time, so one poll can see part of
// them. The test subscribes right after a poll, so with a poll interval of
// one second one poll brings the link up after about one second, and two
// polls after about two.
func TestOnePollDoesNotBringTheParentLinkUp(t *testing.T) {
	t.Parallel()
	bare, below := startUnderBareServer(t.Context(), t, func(cfg *natsbind.Config) {
		cfg.PollInterval = natsbind.DefaultPollInterval
	})
	subjects := []string{natsbind.SubjectREP, natsbind.SubjectLOS, natsbind.SubjectARBR, natsbind.SubjectHB}
	first := listenOn(t, bare, subjects...)
	waitUntil(t, "printer-1 to see its parent link up", func() bool { return below.engine.ParentConnections() == 1 })
	first.Close()
	// A poll sees the loss, and the test sees it within testPoll of that poll.
	waitUntil(t, "printer-1 to see its parent link down", func() bool { return below.engine.ParentLosses() == 1 })

	subscribed := time.Now()
	listenOn(t, bare, subjects...)
	waitUntil(t, "printer-1 to see its parent link up again", func() bool {
		return below.engine.ParentConnections() == 2
	})
	if took, want := time.Since(subscribed), 3*natsbind.DefaultPollInterval/2; took < want {
		t.Errorf("the parent link came up %s after the node above listened, want at least %s", took, want)
	}
}

// startUnderBareServer starts hq, which grants the leafnode address of a bare
// NATS server, and printer-1, which joins hq and so connects to the bare
// server. Each function in tune changes the configuration of printer-1.
func startUnderBareServer(ctx context.Context, t *testing.T, tune ...func(*natsbind.Config)) (*server.Server, *node) {
	t.Helper()
	bareLeaf := testport.Free(t)
	bare, err := server.NewServer(&server.Options{
		Host:     "127.0.0.1",
		Port:     server.RANDOM_PORT,
		NoLog:    true,
		NoSigs:   true,
		LeafNode: server.LeafNodeOpts{Host: "127.0.0.1", Port: bareLeaf},
	})
	if err != nil {
		t.Fatalf("build the bare server: %v", err)
	}
	go bare.Start()
	t.Cleanup(func() {
		bare.Shutdown()
		bare.WaitForShutdown()
	})
	if !bare.ReadyForConnections(testWait) {
		t.Fatal("the bare server did not start")
	}
	aboveCfg, _ := baseConfig(t, nodeOptions{
		id: "hq", accepts: true, advertise: fmt.Sprintf("nats://127.0.0.1:%d", bareLeaf),
	})
	startNode(ctx, t, aboveCfg)
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
	for _, change := range tune {
		change(&belowCfg)
	}
	return bare, startNode(ctx, t, belowCfg)
}

// listenOn subscribes a client of the bare server to each subject and waits
// until the server has them. Closing the client it returns ends them.
func listenOn(t *testing.T, bare *server.Server, subjects ...string) *nats.Conn {
	t.Helper()
	client, err := nats.Connect(bare.ClientURL())
	if err != nil {
		t.Fatalf("connect to the bare server: %v", err)
	}
	t.Cleanup(client.Close)
	for _, subject := range subjects {
		if _, subErr := client.Subscribe(subject, func(*nats.Msg) {}); subErr != nil {
			t.Fatalf("subscribe to %s: %v", subject, subErr)
		}
	}
	if err = client.Flush(); err != nil {
		t.Fatalf("flush the subscriptions: %v", err)
	}
	return client
}

// TestALinkBelowIsUpOnceTheNodeBelowListens lets hq hear only the CONNECT of
// printer-1. hq accepts the leafnode connection, but a message it sent down now
// would be dropped, so the link is up only after printer-1's subscriptions
// arrive. The node below sends them just after the connection is accepted,
// and an RBR sent in that gap draws no state report (fractal-nats.md,
// Connectivity Detection).
func TestALinkBelowIsUpOnceTheNodeBelowListens(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := startPair(ctx, t, nodeOptions{id: "printer-1"})
	release := p.link.HoldAfterConnect()
	p.link.Open()

	waitUntil(t, "printer-1 to see its parent link up", func() bool { return p.below.engine.ParentConnections() == 1 })
	for deadline := time.Now().Add(settle); time.Now().Before(deadline); time.Sleep(testPoll) {
		if got := p.above.engine.Connected(); len(got) != 0 {
			t.Fatalf("hq saw %v connected before printer-1 listened, want nobody", got)
		}
	}
	release()
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(p.above.engine.Connected()) == 1 })
}

// TestALinkBelowIsUpOverAnEarlierRelease lets printer-1 look like a node of
// the release 0.0.1-alpha.6, which listens only for DES and RBR. The relay
// renames its AREP and ALOS subscriptions. hq still counts the link up.
func TestALinkBelowIsUpOverAnEarlierRelease(t *testing.T) {
	t.Parallel()
	p := startPair(t.Context(), t, nodeOptions{id: "printer-1"})
	p.link.Rename(natsbind.SubjectAREP("printer-1"), "fractal.down.xrep.printer-1")
	p.link.Rename(natsbind.SubjectALOS("printer-1"), "fractal.down.xlos.printer-1")
	p.link.Open()

	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(p.above.engine.Connected()) == 1 })
}

// TestAPropertyChangeReachesTheNodeBelow checks that SendBestEffort delivers a
// requested property change from the node above to the engine of the node
// below.
func TestAPropertyChangeReachesTheNodeBelow(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	payload := desPayload(t, "hq/printer-1")
	if err := p.above.binding.SendBestEffort(ctx, fractalnode.Dispatch{
		Kind: fractal.KindDES, Payload: payload, Child: "printer-1",
	}); err != nil {
		t.Fatalf("send a property change: %v", err)
	}
	waitUntil(t, "the property change to arrive", func() bool {
		for _, m := range p.below.engine.Messages() {
			if m.Kind == fractal.KindDES && m.From == "" {
				return true
			}
		}
		return false
	})
}

// TestAMethodInvocationAndItsResultCrossTheLink checks SendDurable, which
// goes through JetStream, in both directions: a method invocation downward
// and its result upward.
func TestAMethodInvocationAndItsResultCrossTheLink(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)

	if err := p.above.binding.SendDurable(ctx, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "one"), Child: "printer-1", ID: "one",
	}); err != nil {
		t.Fatalf("send a method invocation: %v", err)
	}
	waitUntil(t, "the method invocation to arrive", func() bool {
		return countKind(p.below, fractal.KindRPC) == 1
	})

	if err := p.below.binding.SendDurable(ctx, fractalnode.Dispatch{
		Kind: fractal.KindRES, Payload: resPayload(t, "one"), ID: "one",
	}); err != nil {
		t.Fatalf("send a result: %v", err)
	}
	waitUntil(t, "the result to arrive", func() bool {
		return countKind(p.above, fractal.KindRES) == 1
	})
}

// TestCloseDoesNotWaitForAPendingFetch closes the node below right after a
// method invocation reached it. Its reader has then just started to wait for
// the next one, and Close must not wait out that read.
func TestCloseDoesNotWaitForAPendingFetch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	send(t, p.above, fractalnode.Dispatch{
		Kind: fractal.KindRPC, Payload: rpcPayload(t, "one"), Child: "printer-1", ID: "one",
	})
	awaitKind(t, p.below, fractal.KindRPC, 1)

	began := time.Now()
	if err := p.below.binding.Close(); err != nil {
		t.Fatalf("close printer-1: %v", err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("Close took %v, want at most 1s", took)
	}
}

// parentSignals counts the ParentConnected and ParentLost calls one engine
// received.
type parentSignals struct {
	connected int
	lost      int
}

// TestTheParentLinkIsReportedWhenItMoves checks the calls the node below gets
// about its link to the node above. It gets no loss before the link first
// opens, then one call each time the link opens or closes. The root node has
// no link above it and gets none (fractal-nats.md, Connectivity Detection).
func TestTheParentLinkIsReportedWhenItMoves(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := startPair(ctx, t, nodeOptions{id: "printer-1"})

	p.link.Open()
	waitUntil(t, "the link to open", func() bool { return p.below.engine.ParentConnections() == 1 })
	if got := p.below.engine.ParentLosses(); got != 0 {
		t.Fatalf("ParentLost was called %d times before the link first opened, want 0", got)
	}
	p.link.Cut()
	waitUntil(t, "the link to close", func() bool { return p.below.engine.ParentLosses() == 1 })
	p.link.Open()
	waitUntil(t, "the link to open again", func() bool { return p.below.engine.ParentConnections() == 2 })

	got := map[string]parentSignals{
		"hq":        {p.above.engine.ParentConnections(), p.above.engine.ParentLosses()},
		"printer-1": {p.below.engine.ParentConnections(), p.below.engine.ParentLosses()},
	}
	want := map[string]parentSignals{"hq": {0, 0}, "printer-1": {2, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the parent link calls are %+v, want %+v", got, want)
	}
}

// TestAReconnectGoesThroughTheAdvertisedAddress cuts the relay in front of
// the node above twice. The node below redials the leafnode address it was
// granted, so the second cut closes its link again. A NATS server tells the
// node below its leafnode address, and a listen address it told would let the
// node below bypass the relay, which stands for a NAT or a port forward.
func TestAReconnectGoesThroughTheAdvertisedAddress(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := startPair(ctx, t, nodeOptions{id: "printer-1"})

	p.link.Open()
	waitUntil(t, "the link to open", func() bool { return p.below.engine.ParentConnections() == 1 })
	p.link.Cut()
	waitUntil(t, "the link to close", func() bool { return p.below.engine.ParentLosses() == 1 })
	p.link.Open()
	waitUntil(t, "the link to open again", func() bool { return p.below.engine.ParentConnections() == 2 })
	p.link.Cut()
	waitUntil(t, "the second cut to close the link", func() bool { return p.below.engine.ParentLosses() == 2 })
}

// TestTheLeafnodeAddressSentBelowIsTheGrantedOne reads the leafnode address
// the NATS server of a node sends a node below. It is the address the grant
// names, with the default leafnode port the node below dials when the grant
// names none, so the node below never learns a second address to redial.
func TestTheLeafnodeAddressSentBelowIsTheGrantedOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		advertise string
		want      []string
	}{
		{name: "explicit port", advertise: "nats://hq.example:7500", want: []string{"hq.example:7500"}},
		{name: "host name without a port", advertise: "tls://hq.example", want: []string{"hq.example:7422"}},
		{name: "IPv6 literal without a port", advertise: "nats://[::1]", want: []string{"[::1]:7422"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, leafPort := baseConfig(t, nodeOptions{id: "hq", accepts: true, advertise: tc.advertise})
			startNode(t.Context(), t, cfg)
			if got := leafnodeURLs(t, leafPort); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the leafnode addresses sent below are %v, want %v", got, tc.want)
			}
		})
	}
}

// leafnodeURLs connects to a leafnode listener as a node below would and
// returns the leafnode addresses in the INFO the NATS server sends first.
func leafnodeURLs(t testing.TB, port int) []string {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("connect to the leafnode listener: %v", err)
	}
	defer func() { _ = conn.Close() }() // The test read what it needed.
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read the INFO of the leafnode listener: %v", err)
	}
	var info struct {
		LeafnodeURLs []string `json:"leafnode_urls"`
	}
	payload := strings.TrimPrefix(strings.TrimSpace(line), "INFO ")
	if parseErr := json.Unmarshal([]byte(payload), &info); parseErr != nil {
		t.Fatalf("parse the INFO %q: %v", line, parseErr)
	}
	return info.LeafnodeURLs
}

// countKind counts the messages of one type that the engine of a node
// received.
func countKind(n *node, kind fractal.Kind) int {
	count := 0
	for _, m := range n.engine.Messages() {
		if m.Kind == kind {
			count++
		}
	}
	return count
}

// repPayload builds a state report.
func repPayload(t testing.TB, sender fractal.NodePath, at fractal.Timestamp) []byte {
	t.Helper()
	return encode(t, fractal.REP{
		Version:   fractal.Version,
		Sender:    sender,
		Interface: "FDMPrinter1",
		Instance:  "1",
		Timestamp: at,
		Props:     fractal.Props{"nozzleTemp": json.RawMessage(`25`)},
	})
}

// desPayload builds a requested property change.
func desPayload(t testing.TB, receiver fractal.NodePath) []byte {
	t.Helper()
	return encode(t, fractal.DES{
		Version:   fractal.Version,
		Receiver:  receiver,
		Interface: "FDMPrinter1",
		Instance:  "1",
		Props:     fractal.Props{"nozzleTempTarget": json.RawMessage(`220`)},
	})
}

// rpcPayload builds a method invocation with an expiry time far in the
// future.
func rpcPayload(t testing.TB, id fractal.RPCID) []byte {
	t.Helper()
	return encode(t, fractal.RPC{
		Version:   fractal.Version,
		ID:        id,
		Sender:    "hq",
		Receiver:  "hq/printer-1",
		Interface: "FDMPrinter1",
		Instance:  "1",
		Method:    "SetNozzleTemp",
		Expires:   fractal.Timestamp(1) << 42,
	})
}

// resPayload builds a result for the root node hq. In the examples of
// fractal.md the root node is the caller.
func resPayload(t testing.TB, id fractal.RPCID) []byte {
	t.Helper()
	return encode(t, fractal.RES{
		Version:  fractal.Version,
		ID:       id,
		Receiver: "hq",
		Result:   json.RawMessage(`true`),
	})
}

// encode encodes a message as JSON. The test files build every message, so an
// error here is a fault in a test file.
func encode(t testing.TB, m any) []byte {
	t.Helper()
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return payload
}

// TestAFullQueueRefusesTheNextMessage fills the result stream of a node
// whose link is down, and the method stream of the node above for it. Each
// stream refuses the message that does not fit, and keeps every message it
// took: a pending method invocation or result is never lost or deleted
// (fractal-requirements.md, Method Invocation Guarantees).
func TestAFullQueueRefusesTheNextMessage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		kind    fractal.Kind
		payload func(testing.TB, fractal.RPCID) []byte
		child   fractal.NodeID
		upward  bool
	}{
		{name: "results", kind: fractal.KindRES, payload: resPayload, upward: true},
		{name: "method invocations", kind: fractal.KindRPC, payload: rpcPayload, child: "printer-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			aboveCfg, aboveLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
			aboveCfg.StreamMaxBytes = 1024
			relay := newProxy(t, fmt.Sprintf("127.0.0.1:%d", aboveLeaf))
			aboveCfg.AdvertisedLeafRemote = "nats://" + relay.Addr()
			above := startNode(ctx, t, aboveCfg)
			belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: aboveCfg.ClientListen})
			belowCfg.StreamMaxBytes = 1024
			below := startNode(ctx, t, belowCfg)
			sender, receiver := above, below
			if c.upward {
				sender, receiver = below, above
			}

			var taken [][]byte
			for i := range 50 {
				id := fractal.RPCID(fmt.Sprintf("r%d", i))
				payload := c.payload(t, id)
				err := sender.binding.SendDurable(ctx, fractalnode.Dispatch{
					Kind: c.kind, Payload: payload, Child: c.child, ID: id,
				})
				if err != nil {
					break
				}
				taken = append(taken, payload)
			}
			if len(taken) == 50 {
				t.Fatal("the stream took 50 messages into 1024 bytes, want it to refuse one")
			}

			relay.Open()
			awaitKind(t, receiver, c.kind, len(taken))
			if got := onlyKind(receiver, c.kind); !reflect.DeepEqual(got, taken) {
				t.Errorf("the receiver got %d messages, want the %d the stream took, in order", len(got), len(taken))
			}
		})
	}
}

// TestOnlyAHeartbeatKeepsANodeAlive: the relay renames the heartbeats of
// printer-1, so none reaches hq, and printer-1 still sends state reports. hq
// treats the absence of heartbeats as a disconnect (fractal-nats.md,
// Connectivity Detection).
func TestOnlyAHeartbeatKeepsANodeAlive(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := startPair(ctx, t, nodeOptions{id: "printer-1"})
	p.link.Rename(natsbind.SubjectHB, "fractal.up.xb")
	p.link.Open()
	waitUntil(t, "hq to see printer-1 as connected", func() bool { return len(p.above.engine.Connected()) == 1 })

	reporting, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		for at := fractal.Timestamp(1); reporting.Err() == nil; at++ {
			_ = p.below.binding.SendBestEffort(reporting, fractalnode.Dispatch{
				Kind: fractal.KindREP, Payload: repPayload(t, "hq/printer-1", at),
			})
			select {
			case <-reporting.Done():
			case <-time.After(testHeartbeat):
			}
		}
	}()
	waitUntil(t, "hq to lose printer-1", func() bool { return len(p.above.engine.Lost()) == 1 })
}

// TestTheChildrenCanBeReadDuringAJoin reads the nodes below while printer-1
// joins again. Join requests are answered on their own goroutines, and the
// engine reads the nodes below at any time. So go test -race fails when the
// read and the join share no lock.
func TestTheChildrenCanBeReadDuringAJoin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	done := make(chan struct{})
	read := make(chan []fractal.NodeID)
	go func() {
		var children []fractal.NodeID
		for {
			select {
			case <-done:
				read <- children
				return
			default:
				children = slices.Collect(p.above.binding.Children())
			}
		}
	}()

	err := p.below.binding.Join(ctx)
	close(done)
	if children := <-read; err != nil || !slices.Equal(children, []fractal.NodeID{"printer-1"}) {
		t.Errorf("after the join hq lists %v as its children (join: %v), want printer-1", children, err)
	}
}
