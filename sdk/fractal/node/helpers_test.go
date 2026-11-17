package node_test

import (
	"context"
	"encoding/json"
	"iter"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The tree of the tests. The node under test is usually hq/brno, with hq above
// it and printer-1 below it.
const (
	hqPath      = fractal.NodePath("hq")
	brnoPath    = fractal.NodePath("hq/brno")
	printerPath = fractal.NodePath("hq/brno/printer-1")
	printerID   = fractal.NodeID("printer-1")
)

// The interface the tests register.
const (
	ifaceName     = fractal.InterfaceName("FDMPrinter1")
	ifaceInstance = fractal.InstanceID("1")
	setTemp       = fractal.MethodName("SetNozzleTemp")
	tempProp      = fractal.PropertyName("nozzleTemp")
	targetProp    = fractal.PropertyName("nozzleTempTarget")
)

// now is the time the test clock starts at, in unix milliseconds. callWindow is
// how many milliseconds after now a test RPC expires.
const (
	now        = fractal.Timestamp(1751400000000)
	callWindow = fractal.Timestamp(1000)
)

// definitionJSON describes the interface the tests register.
const definitionJSON = `{
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

// sent is one message the node gave to the binding. durable says whether the
// node called SendDurable.
type sent struct {
	dispatch node.Dispatch
	durable  bool
}

// recordBinding is a Binding that records every message the node sends. A test
// can make its sends return an error.
type recordBinding struct {
	err      error
	downErr  error
	messages []sent
	mu       sync.Mutex
}

// SendBestEffort records the message as a best-effort send.
func (b *recordBinding) SendBestEffort(_ context.Context, d node.Dispatch) error {
	return b.keep(d, false)
}

// SendDurable records the message as a durable send.
func (b *recordBinding) SendDurable(_ context.Context, d node.Dispatch) error {
	return b.keep(d, true)
}

// Sent returns a copy of every recorded message, in the order the node sent
// them.
func (b *recordBinding) Sent() []sent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]sent(nil), b.messages...)
}

// OfKind returns the recorded messages of one type, in order.
func (b *recordBinding) OfKind(k fractal.Kind) []sent {
	var out []sent
	for _, m := range b.Sent() {
		if m.dispatch.Kind == k {
			out = append(out, m)
		}
	}
	return out
}

// Reset deletes the recorded messages.
func (b *recordBinding) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = nil
}

// Fail makes every later send return err.
func (b *recordBinding) Fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

// FailDown makes every later send to a node below return err. A send to the
// node above still succeeds.
func (b *recordBinding) FailDown(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.downErr = err
}

// keep records one message, or returns the error a test set with Fail or
// FailDown.
func (b *recordBinding) keep(d node.Dispatch, durable bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.err
	}
	if b.downErr != nil && d.Down() {
		return b.downErr
	}
	b.messages = append(b.messages, sent{dispatch: d, durable: durable})
	return nil
}

// children is an Admissions with a fixed list of the Node IDs directly below
// the node.
type children []fractal.NodeID

// Admitted reports whether id is in the list.
func (c children) Admitted(id fractal.NodeID) bool { return slices.Contains(c, id) }

// Children iterates over the list.
func (c children) Children() iter.Seq[fractal.NodeID] { return slices.Values(c) }

// fixedClock is a Clock whose Now returns the time the test set with Set.
type fixedClock struct {
	at fractal.Timestamp
	mu sync.Mutex
}

// Now returns the time the test set.
func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at.Time()
}

// After returns time.After(d), which uses the fake clock of the synctest
// bubble and not the time from Set.
func (c *fixedClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Set changes the time Now returns.
func (c *fixedClock) Set(at fractal.Timestamp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

// testOwner is an Owner whose return values the test sets in its fields.
type testOwner struct {
	snapshot    fractal.Props
	snapshotAt  fractal.Timestamp
	snapshotErr error
	desireErr   error
	allowErr    error
	result      json.RawMessage
	invokeErr   error
	panics      bool
	runs        int
	mu          sync.Mutex
}

// Snapshot returns the fields snapshot, snapshotAt and snapshotErr.
func (o *testOwner) Snapshot(context.Context) (fractal.Props, fractal.Timestamp, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshot, o.snapshotAt, o.snapshotErr
}

// Desire returns desireErr when the test set it. Otherwise it accepts the
// request and returns the requested props as the changed ones.
func (o *testOwner) Desire(_ context.Context, props fractal.Props) (fractal.Props, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.desireErr != nil {
		return nil, o.desireErr
	}
	return props, nil
}

// Invoke counts the call. It panics when panics is set, and returns result and
// invokeErr otherwise.
func (o *testOwner) Invoke(context.Context, node.Call) (json.RawMessage, error) {
	o.mu.Lock()
	o.runs++
	panics, result, err := o.panics, o.result, o.invokeErr
	o.mu.Unlock()
	if panics {
		panic("the owner broke")
	}
	return result, err
}

// Runs returns how many times the node called Invoke.
func (o *testOwner) Runs() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.runs
}

// guardedOwner is a testOwner that also implements node.Guard.
type guardedOwner struct {
	*testOwner
}

// Allow returns allowErr, which is nil unless the test set it.
func (o *guardedOwner) Allow(context.Context, node.Call) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.allowErr
}

// harness is one running node with the binding, the clock, the owner, the
// interface handle and the shadow store a test reads.
type harness struct {
	node    *node.Node
	binding *recordBinding
	clock   *fixedClock
	owner   *testOwner
	iface   *node.Interface
	shadow  *shadow.Store
	log     *logRecorder
}

// newHarness builds and starts a node at path. The node admits printer-1 below
// it, its shadow selects every interface, and it owns one interface instance.
// newHarness deletes the messages the node sent during Start.
func newHarness(t *testing.T, path fractal.NodePath) *harness {
	t.Helper()
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}
	h := &harness{
		binding: &recordBinding{},
		clock:   &fixedClock{at: now},
		owner:   &testOwner{snapshot: fractal.Props{tempProp: json.RawMessage(`25.0`)}},
		shadow:  store,
		log:     &logRecorder{},
	}
	h.node, err = node.New(node.Config{
		Binding:    h.binding,
		Admissions: children{printerID},
		Shadow:     store,
		Clock:      h.clock,
		Path:       path,
		Logger:     slog.New(h.log),
	})
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}
	h.iface = registerOn(t, h.node, h.owner)
	if startErr := h.node.Start(t.Context()); startErr != nil {
		t.Fatalf("start the node: %v", startErr)
	}
	t.Cleanup(func() { _ = h.node.Close() })
	h.binding.Reset()
	return h
}

// registerOn registers the test interface on a node with the given owner.
func registerOn(t *testing.T, n *node.Node, owner node.Owner) *node.Interface {
	t.Helper()
	handle, err := n.Register(node.Instance{
		Definition: definition(t),
		Owner:      owner,
		Name:       ifaceName,
		Instance:   ifaceInstance,
	})
	if err != nil {
		t.Fatalf("register the interface: %v", err)
	}
	return handle
}

// definition builds the schema.Definition from definitionJSON.
func definition(t *testing.T) *schema.Definition {
	t.Helper()
	builder := schema.NewBuilder()
	if err := builder.AddJSON("printer.json", []byte(definitionJSON)); err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	registry, err := builder.Build()
	if err != nil {
		t.Fatalf("build the registry: %v", err)
	}
	def, ok := registry.Definition(ifaceName)
	if !ok {
		t.Fatalf("the registry has no definition of %s", ifaceName)
	}
	return def
}

// encode returns a message as JSON, the payload a transport delivers.
func encode(t *testing.T, m any) []byte {
	t.Helper()
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return payload
}

// decodeRES decodes the payload of a RES the node sent.
func decodeRES(t *testing.T, payload []byte) fractal.RES {
	t.Helper()
	var m fractal.RES
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	return m
}

// report builds a REP whose sender is hq/brno/printer-1.
func report(at fractal.Timestamp, props fractal.Props) fractal.REP {
	return fractal.REP{
		Version:   fractal.Version,
		Sender:    printerPath,
		Interface: ifaceName,
		Instance:  ifaceInstance,
		Timestamp: at,
		Props:     props,
	}
}

// invocation builds an RPC from hq to receiver with the id "a". It expires
// callWindow milliseconds after now.
func invocation(receiver fractal.NodePath) fractal.RPC {
	return fractal.RPC{
		Version:   fractal.Version,
		ID:        "a",
		Sender:    hqPath,
		Receiver:  receiver,
		Interface: ifaceName,
		Instance:  ifaceInstance,
		Method:    setTemp,
		Params:    []json.RawMessage{json.RawMessage(`250.0`)},
		Expires:   now + callWindow,
	}
}

// acker counts how many times the node called Inbound.Ack.
type acker struct {
	count int
	mu    sync.Mutex
}

// ack is the function a test puts in Inbound.Ack.
func (a *acker) ack(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.count++
	return nil
}

// Count returns how many times the node called ack.
func (a *acker) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.count
}

// shadowStore builds a shadow store that selects every interface.
func shadowStore() (*shadow.Store, error) {
	return shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
}

// wrongVersion returns m as JSON with the version 99, which this package does
// not support.
func wrongVersion(t *testing.T, m any) []byte {
	t.Helper()
	return []byte(strings.Replace(string(encode(t, m)), `"version":1`, `"version":99`, 1))
}

// logRecorder is a slog.Handler that keeps the level of every record by its
// message.
type logRecorder struct {
	mu     sync.Mutex
	levels map[string][]slog.Level
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.levels == nil {
		r.levels = map[string][]slog.Level{}
	}
	r.levels[record.Message] = append(r.levels[record.Message], record.Level)
	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *logRecorder) WithGroup(string) slog.Handler { return r }

// Levels returns the levels of the records written with message.
func (r *logRecorder) Levels(message string) []slog.Level {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.levels[message])
}
