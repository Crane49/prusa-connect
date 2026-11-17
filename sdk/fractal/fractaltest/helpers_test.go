package fractaltest_test

import (
	"context"
	"encoding/json"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The three nodes of the worked examples of fractal.md.
const (
	hqPath      = fractal.NodePath("hq")
	brnoPath    = fractal.NodePath("hq/brno")
	printerPath = fractal.NodePath("hq/brno/printer-1")
	brnoID      = fractal.NodeID("brno")
	printerID   = fractal.NodeID("printer-1")
)

// The interface the printer of the worked examples owns.
const (
	printerInterface = fractal.InterfaceName("FDMPrinter1")
	printerInstance  = fractal.InstanceID("1")
	setNozzleTemp    = fractal.MethodName("SetNozzleTemp")
	nozzleTemp       = fractal.PropertyName("nozzleTemp")
	nozzleTempTarget = fractal.PropertyName("nozzleTempTarget")
)

// The interface hq owns in Examples 7 and 8. Its definition states Exposure:
// downward, so hq reports it to the nodes below and never to a node above.
const (
	siteInterface   = fractal.InterfaceName("Site1")
	siteInstance    = fractal.InstanceID("1")
	maintenanceMode = fractal.PropertyName("maintenanceMode")
)

// The times the worked examples of fractal.md use, in unix milliseconds.
const (
	tStartup      = fractal.Timestamp(1751400000000)
	tHeating      = fractal.Timestamp(1751400010000)
	tDesired      = fractal.Timestamp(1751400020000)
	tInvoked      = fractal.Timestamp(1751400025000)
	tLost         = fractal.Timestamp(1751400060000)
	tReconnect    = fractal.Timestamp(1751400120000)
	tSite         = fractal.Timestamp(1751400180000)
	tAncestorLost = fractal.Timestamp(1751400240000)
)

// printerDefinition is the interface definition of the printer in the worked
// examples: a measured nozzle temperature, a target temperature that may be
// null, and the method that sets the target.
const printerDefinition = `{
  "interface": "FDMPrinter1",
  "exposure": "upward",
  "properties": [
    { "name": "nozzleTemp", "type": "float", "access": "ro" },
    { "name": "nozzleTempTarget", "type": "float", "access": "rw", "nullable": true }
  ],
  "methods": [
    { "name": "SetNozzleTemp", "params": [{ "name": "target", "type": "float" }], "returns": "bool" }
  ]
}`

// manualClock is a node.Clock whose time changes only when a test calls Set.
// Every timestamp in a message is then the timestamp fractal.md prints in its
// worked examples. After does not use that time. It waits with time.After,
// which inside a synctest bubble uses the fake clock of the bubble.
type manualClock struct {
	now time.Time
	mu  sync.Mutex
}

// newManualClock builds a clock set to the timestamp at.
func newManualClock(at fractal.Timestamp) *manualClock {
	return &manualClock{now: at.Time()}
}

// Now returns the time the test set.
func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After waits for d with time.After. The time set with Set has no effect on it.
func (c *manualClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Set sets the clock to the timestamp at.
func (c *manualClock) Set(at fractal.Timestamp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at.Time()
}

// childSet is a node.Admissions with a fixed list: the Node IDs of the nodes
// that may connect below this node.
type childSet []fractal.NodeID

// Admitted reports whether id is in the list.
func (c childSet) Admitted(id fractal.NodeID) bool { return slices.Contains(c, id) }

// Children iterates over the Node IDs in the list.
func (c childSet) Children() iter.Seq[fractal.NodeID] { return slices.Values(c) }

// outcomeKey is the key of one counter in counters.messages: a message type
// and what the node did with the message.
type outcomeKey struct {
	kind    fractal.Kind
	outcome node.Outcome
}

// dropKey is the key of one counter in counters.drops: a message type and the
// reason the node dropped the message.
type dropKey struct {
	kind   fractal.Kind
	reason node.DropReason
}

// counters is a node.Metrics that keeps each count in memory. A test reads how
// many messages of one type a node processed, forwarded or dropped.
type counters struct {
	messages map[outcomeKey]int
	drops    map[dropKey]int
	mu       sync.Mutex
}

// newCounters builds an empty set of counters.
func newCounters() *counters {
	return &counters{messages: make(map[outcomeKey]int), drops: make(map[dropKey]int)}
}

// Message counts one message the node handled.
func (c *counters) Message(k fractal.Kind, o node.Outcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[outcomeKey{kind: k, outcome: o}]++
}

// Drop counts one message the node did not act on.
func (c *counters) Drop(k fractal.Kind, r node.DropReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drops[dropKey{kind: k, reason: r}]++
}

// Messages returns how many messages of type k had the outcome o.
func (c *counters) Messages(k fractal.Kind, o node.Outcome) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messages[outcomeKey{kind: k, outcome: o}]
}

// Drops returns how many messages of type k the node dropped for the reason r.
func (c *counters) Drops(k fractal.Kind, r node.DropReason) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drops[dropKey{kind: k, reason: r}]
}

// ackGate is a node with a switch. While swallow is true, the acknowledgement
// of a durable message does not reach the transport. This models a node that
// ran a method and stopped before it acknowledged the request. The transport
// then delivers the request again.
type ackGate struct {
	*node.Node

	swallow atomic.Bool
}

// Deliver passes the message to the node. While swallow is true, it first
// replaces the Ack function with one that does nothing.
func (g *ackGate) Deliver(ctx context.Context, in node.Inbound) {
	if in.Ack != nil && g.swallow.Load() {
		in.Ack = func(context.Context) error { return nil }
	}
	g.Node.Deliver(ctx, in)
}

// network is the three-node tree of the worked examples, running in one
// process over the in-memory transport.
type network struct {
	fabric     *fractaltest.Fabric
	clock      *manualClock
	hq         *node.Node
	brno       *node.Node
	printer    *node.Node
	printerAck *ackGate
	hqCount    *counters
	brnoCount  *counters
	owner      *fractaltest.StateOwner
	printerIf  *node.Interface
	hqLink     *fractaltest.Link
	brnoLink   *fractaltest.Link
	site       *fractaltest.StateOwner
	siteIf     *node.Interface
}

// newNetwork builds hq, hq/brno and hq/brno/printer-1. A shadow is the copy a
// node keeps of the state of a node below it. hq and brno each keep a shadow
// of the printer's interface. The selection names that interface by its full
// key, so the shadow interface exists with status EMPTY before any report
// arrives. hq also owns Site1, exposed downward, and the printer keeps it in
// its ancestor shadow, as in Example 7. Both links are up when newNetwork returns, and no node runs yet: a
// test starts the nodes it wants.
func newNetwork(t testing.TB) *network {
	t.Helper()
	clock := newManualClock(tStartup)
	fabric := fractaltest.NewFabric(fractaltest.FabricOptions{Clock: clock})
	hqLink, err := fabric.Link(hqPath, brnoID)
	if err != nil {
		t.Fatalf("link hq to brno: %v", err)
	}
	brnoLink, err := fabric.Link(brnoPath, printerID)
	if err != nil {
		t.Fatalf("link brno to the printer: %v", err)
	}

	n := &network{
		fabric:    fabric,
		clock:     clock,
		hqLink:    hqLink,
		brnoLink:  brnoLink,
		hqCount:   newCounters(),
		brnoCount: newCounters(),
	}
	n.hq = newNode(t, node.Config{
		Binding:    fabric.Transport(hqPath),
		Admissions: childSet{brnoID},
		Shadow:     printerShadow(t),
		Clock:      clock,
		Metrics:    n.hqCount,
		Path:       hqPath,
	})
	n.brno = newNode(t, node.Config{
		Binding:    fabric.Transport(brnoPath),
		Admissions: childSet{printerID},
		Shadow:     printerShadow(t),
		Clock:      clock,
		Metrics:    n.brnoCount,
		Path:       brnoPath,
	})
	n.owner = fractaltest.NewStateOwner(fractal.Props{
		nozzleTemp:       json.RawMessage(`25.0`),
		nozzleTempTarget: json.RawMessage(`null`),
	})
	n.printer = newNode(t, node.Config{
		Binding:    fabric.Transport(printerPath),
		Admissions: node.NoChildren{},
		Ancestors:  siteShadow(t),
		Clock:      clock,
		Path:       printerPath,
	})
	n.printerIf = register(t, n.printer, n.owner)
	n.owner.Handle(setNozzleTemp, n.setNozzleTemp)
	n.printerAck = &ackGate{Node: n.printer}
	n.site = fractaltest.NewStateOwner(fractal.Props{maintenanceMode: json.RawMessage(`false`)})
	n.siteIf, err = n.hq.Register(node.Instance{
		Owner:    n.site,
		Name:     siteInterface,
		Instance: siteInstance,
		Exposure: schema.ExposureDownward,
	})
	if err != nil {
		t.Fatalf("register the site on hq: %v", err)
	}

	fabric.Attach(hqPath, n.hq)
	fabric.Attach(brnoPath, n.brno)
	fabric.Attach(printerPath, n.printerAck)
	fabric.Start(t.Context())
	t.Cleanup(fabric.Close)
	// Both links come up here, before any node runs. A test then starts the
	// nodes in the order its example needs, as a device joins the network when
	// it is switched on.
	n.hqLink.Connect(t.Context())
	n.brnoLink.Connect(t.Context())
	return n
}

// start starts one node and closes it when the test ends.
func (n *network) start(t testing.TB, path fractal.NodePath, engine *node.Node) {
	t.Helper()
	if err := engine.Start(t.Context()); err != nil {
		t.Fatalf("start %s: %v", path, err)
	}
	t.Cleanup(func() { _ = engine.Close() })
}

// startBelow starts the printer and then brno. Example 1 of fractal.md begins
// in this state: the printer is running and brno has started.
func (n *network) startBelow(t testing.TB) {
	t.Helper()
	n.start(t, printerPath, n.printer)
	n.start(t, brnoPath, n.brno)
}

// startRoot starts hq. On start, hq asks every node directly below it for a
// full state report.
func (n *network) startRoot(t testing.TB) {
	t.Helper()
	n.start(t, hqPath, n.hq)
}

// startAll starts every node: the printer first, then brno, then hq.
func (n *network) startAll(t testing.TB) {
	t.Helper()
	n.startBelow(t)
	n.startRoot(t)
}

// setNozzleTemp is the SetNozzleTemp method of the worked examples. It writes
// the target temperature, reports the new value to the node above, and returns
// true.
func (n *network) setNozzleTemp(ctx context.Context, params []json.RawMessage) (json.RawMessage, error) {
	target := params[0]
	n.owner.Set(nozzleTempTarget, target)
	changed := fractal.Props{nozzleTempTarget: target}
	if err := n.printerIf.Report(ctx, changed, fractal.TimestampOf(n.clock.Now())); err != nil {
		return nil, err
	}
	return json.RawMessage(`true`), nil
}

// newNode builds one node, and fails the test when node.New returns an error.
func newNode(t testing.TB, cfg node.Config) *node.Node {
	t.Helper()
	n, err := node.New(cfg)
	if err != nil {
		t.Fatalf("build a node: %v", err)
	}
	return n
}

// printerShadow builds a shadow store that selects the printer's interface by
// its full key. The store creates that interface at once, with the status
// EMPTY.
func printerShadow(t testing.TB) *shadow.Store {
	t.Helper()
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{
		Interfaces: []shadow.Key{{Node: printerPath, Interface: printerInterface, Instance: printerInstance}},
	}})
	if err != nil {
		t.Fatalf("build a shadow: %v", err)
	}
	return store
}

// register registers the interface of printerDefinition on the node n, with
// owner as the node.Owner of its instance.
func register(t testing.TB, n *node.Node, owner node.Owner) *node.Interface {
	t.Helper()
	builder := schema.NewBuilder()
	if err := builder.AddJSON("printer.json", []byte(printerDefinition)); err != nil {
		t.Fatalf("read the interface definition: %v", err)
	}
	registry, err := builder.Build()
	if err != nil {
		t.Fatalf("build the registry: %v", err)
	}
	def, ok := registry.Definition(printerInterface)
	if !ok {
		t.Fatalf("the registry has no definition of %s", printerInterface)
	}
	handle, err := n.Register(node.Instance{
		Definition: def,
		Owner:      owner,
		Name:       printerInterface,
		Instance:   printerInstance,
	})
	if err != nil {
		t.Fatalf("register the interface: %v", err)
	}
	return handle
}

// callWindow is the time, in milliseconds, between the moment a test invokes a
// method and the moment that invocation expires.
const callWindow = fractal.Timestamp(30000)

// report sends one state report from the printer, with the timestamp at.
func report(t *testing.T, n *network, props fractal.Props, at fractal.Timestamp) {
	t.Helper()
	if err := n.printerIf.Report(t.Context(), props, at); err != nil {
		t.Fatalf("the printer could not send its state report: %v", err)
	}
}

// call invokes SetNozzleTemp on the printer from hq and returns the answer.
// target is the JSON of the parameter.
func call(t *testing.T, n *network, target string, expires fractal.Timestamp) fractal.RES {
	t.Helper()
	res, err := n.hq.Call(t.Context(), node.CallRequest{
		Target:    printerPath,
		Interface: printerInterface,
		Instance:  printerInstance,
		Method:    setNozzleTemp,
		Params:    []json.RawMessage{json.RawMessage(target)},
		Expires:   expires,
	})
	if err != nil {
		t.Fatalf("call %s: %v", setNozzleTemp, err)
	}
	return res
}

// shadowOf returns the copy of the printer's interface in the shadow of the
// node n.
func shadowOf(t *testing.T, n *node.Node) shadow.Snapshot {
	t.Helper()
	key := shadow.Key{Node: printerPath, Interface: printerInterface, Instance: printerInstance}
	snap, ok := n.Shadow().Get(key)
	if !ok {
		t.Fatalf("the shadow has no interface %s", key)
	}
	return snap
}

// wantShadow checks the status, the timestamp and the property values of one
// shadow interface.
func wantShadow(
	t *testing.T,
	snap shadow.Snapshot,
	status shadow.Status,
	at fractal.Timestamp,
	props map[fractal.PropertyName]string,
) {
	t.Helper()
	if snap.Status != status {
		t.Errorf("the shadow interface has the status %s, want %s", snap.Status, status)
	}
	if snap.UpdatedAt != at {
		t.Errorf("the shadow interface has the updated time %d, want %d", snap.UpdatedAt, at)
	}
	for name, want := range props {
		got, ok := snap.Props[name]
		if !ok {
			t.Errorf("the shadow interface has no property %s", name)
			continue
		}
		if string(got) != want {
			t.Errorf("the shadow has %s = %s, want %s", name, got, want)
		}
	}
}

// siteShadow builds the ancestor shadow of the printer. It selects Site1 of hq
// by its full key, so the interface exists with the status EMPTY at once.
func siteShadow(t testing.TB) *shadow.Store {
	t.Helper()
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{
		Interfaces: []shadow.Key{{Node: hqPath, Interface: siteInterface, Instance: siteInstance}},
	}})
	if err != nil {
		t.Fatalf("build the ancestor shadow: %v", err)
	}
	return store
}

// ancestorOf returns the copy of Site1 of hq in the ancestor shadow of n.
func ancestorOf(t *testing.T, n *node.Node) shadow.Snapshot {
	t.Helper()
	key := shadow.Key{Node: hqPath, Interface: siteInterface, Instance: siteInstance}
	snap, ok := n.Ancestors().Get(key)
	if !ok {
		t.Fatalf("the ancestor shadow has no interface %s", key)
	}
	return snap
}
