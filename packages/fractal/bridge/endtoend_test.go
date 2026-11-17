package bridge_test

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The tree of these tests: the node "hq" above, and the node "workshop" under
// it with the printers.
const (
	abovePath fractal.NodePath = "hq"
	siteID    fractal.NodeID   = "workshop"
	sitePath  fractal.NodePath = "hq/workshop"
)

// wait is the longest a test waits for a message to arrive, and poll is how
// often it checks. The transport is in memory, so wait only allows for a slow
// build machine.
const (
	wait = 2 * time.Second
	poll = time.Millisecond
)

// tree is two Fractal nodes on the in-memory transport. The node above has a
// shadow of every interface under it. The node below has the bridge.
type tree struct {
	above  *fractalnode.Node
	below  *fractalnode.Node
	shadow *shadow.Store
	bridge *bridge.Bridge
	fleet  *fakeFleet
}

// stand builds the two nodes, connects them and starts them.
func stand(t *testing.T, devices ...deviceapi.Device) *tree {
	t.Helper()

	ctx := t.Context()
	fabric := fractaltest.NewFabric(fractaltest.FabricOptions{})

	everything, err := fractal.ParsePattern("*")
	if err != nil {
		t.Fatalf("parse the pattern \"*\": %v", err)
	}

	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{
		Patterns: []fractal.Pattern{everything},
	}})
	if err != nil {
		t.Fatalf("build the shadow: %v", err)
	}

	above, err := fractalnode.New(fractalnode.Config{
		Binding:    fabric.Transport(abovePath),
		Admissions: oneChild{id: siteID},
		Shadow:     store,
		Clock:      stopped{},
		Path:       abovePath,
	})
	if err != nil {
		t.Fatalf("build the node above: %v", err)
	}

	below, err := fractalnode.New(fractalnode.Config{
		Binding: fabric.Transport(sitePath),
		Clock:   stopped{},
		Path:    sitePath,
	})
	if err != nil {
		t.Fatalf("build the node below: %v", err)
	}

	fabric.Attach(abovePath, above)
	fabric.Attach(sitePath, below)

	if _, err = fabric.Link(abovePath, siteID); err != nil {
		t.Fatalf("connect the node below to the node above: %v", err)
	}

	fleet := newFleet(devices...)

	made, err := bridge.New(bridge.Config{
		Fleet:     fleet,
		Node:      engineRegistrar{node: below},
		Mapping:   mapping(t),
		Principal: func() string { return "node:hq" },
		Now:       fixed(at()),
	})
	if err != nil {
		t.Fatalf("build the bridge: %v", err)
	}

	start(ctx, t, above, below)
	fabric.ConnectAll(ctx)
	fabric.Start(ctx)

	t.Cleanup(fabric.Close)

	if err = made.Sync(ctx); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	return &tree{above: above, below: below, shadow: store, bridge: made, fleet: fleet}
}

// start starts every node and closes it when the test ends.
func start(ctx context.Context, t *testing.T, nodes ...*fractalnode.Node) {
	t.Helper()

	for _, one := range nodes {
		if err := one.Start(ctx); err != nil {
			t.Fatalf("start a node: %v", err)
		}

		t.Cleanup(func() { _ = one.Close() })
	}
}

// stopped is a clock that always returns at(), the time the bridge also uses.
// A full state report from the node and a state report from the bridge then
// have the same timestamp, so the shadow discards neither as older.
type stopped struct{}

func (stopped) Now() time.Time { return at() }

func (stopped) After(d time.Duration) <-chan time.Time { return time.After(d) }

// oneChild is the admission list of a node that admits exactly one node under
// it.
type oneChild struct {
	id fractal.NodeID
}

func (c oneChild) Admitted(id fractal.NodeID) bool { return id == c.id }

func (c oneChild) Children() iter.Seq[fractal.NodeID] {
	return func(yield func(fractal.NodeID) bool) { yield(c.id) }
}

// engineRegistrar adapts a Fractal node to bridge.Registrar, as the registrar
// type of the main package does.
type engineRegistrar struct {
	node *fractalnode.Node
}

func (r engineRegistrar) Register(inst fractalnode.Instance) (bridge.Reporter, error) {
	handle, err := r.node.Register(inst)
	if err != nil {
		return nil, err //nolint:wrapcheck // the engine's own refusal, in a test.
	}

	return handle, nil
}

// held waits until the shadow of the node above has the interface key in a
// state that check accepts, and returns it.
func held(t *testing.T, store *shadow.Store, key shadow.Key, check func(shadow.Snapshot) bool) shadow.Snapshot {
	t.Helper()

	deadline := time.Now().Add(wait)

	for time.Now().Before(deadline) {
		if snapshot, ok := store.Get(key); ok && check(snapshot) {
			return snapshot
		}

		time.Sleep(poll)
	}

	t.Fatalf("the shadow of the node above never had %s in the state the test waits for", key)

	return shadow.Snapshot{}
}

// TestAPrinterReachesTheServerAbove: the readings of one printer arrive in the
// shadow of the node above, under the interface names and the property names
// of the specification, with the values converted.
func TestAPrinterReachesTheServerAbove(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))
	built.bridge.Flush(t.Context())

	key := shadow.Key{
		Node:      sitePath,
		Interface: "FDMPrinterHead1",
		Instance:  fractal.InstanceID(printerOne + "_1"),
	}

	snapshot := held(t, built.shadow, key, func(s shadow.Snapshot) bool {
		return s.Status == shadow.StatusOK
	})

	for name, want := range map[fractal.PropertyName]string{
		"tempNozzle":   `214.8`,
		"targetNozzle": `230`,
		"material":     `"PETG"`,
		"nozzleType":   `null`,
	} {
		if held := snapshot.Props[name]; string(held) != want {
			t.Errorf("%s is %s in the shadow of the node above, want %s", name, held, want)
		}
	}

	if snapshot.UpdatedAt != fractal.TimestampOf(at()) {
		t.Errorf("UpdatedAt in the shadow is %d, want the time of the reading %d",
			snapshot.UpdatedAt, fractal.TimestampOf(at()))
	}

	state := shadow.Key{Node: sitePath, Interface: "Printer1", Instance: fractal.InstanceID(printerOne)}

	printing := held(t, built.shadow, state, func(s shadow.Snapshot) bool {
		return string(s.Props["state"]) == `"PRINTING"`
	})
	if string(printing.Props["speed"]) != `100` {
		t.Errorf("speed is %s in the shadow of the node above, want 100", printing.Props["speed"])
	}
}

// TestAChangedReadingTravelsOnItsOwn: after the first report, the bridge sends
// only the property that changed. The shadow of the node above keeps the
// values of the other properties (fractal.md, Reported Properties Message).
func TestAChangedReadingTravelsOnItsOwn(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))
	built.bridge.Flush(t.Context())

	key := shadow.Key{Node: sitePath, Interface: "Printer1", Instance: fractal.InstanceID(printerOne)}
	held(t, built.shadow, key, func(s shadow.Snapshot) bool { return s.Status == shadow.StatusOK })

	built.bridge.Sample(t.Context(), printerOne, "/speed", json.RawMessage(`80`), at().Add(time.Second))
	built.bridge.Flush(t.Context())

	snapshot := held(t, built.shadow, key, func(s shadow.Snapshot) bool {
		return string(s.Props["speed"]) == `80`
	})

	if string(snapshot.Props["state"]) != `"PRINTING"` {
		t.Errorf("state is %s in the shadow, and the report with only speed must not change it",
			snapshot.Props["state"])
	}
}

// TestAMethodFromAboveBecomesOneCommand: a PauseJob call from the node above
// becomes one pause_print command for the printer, with the principal
// "node:hq".
func TestAMethodFromAboveBecomesOneCommand(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))

	answer, err := built.above.Call(t.Context(), fractalnode.CallRequest{
		Target:    sitePath,
		Interface: "Printer1",
		Instance:  fractal.InstanceID(printerOne),
		Method:    "PauseJob",
		Expires:   fractal.TimestampOf(time.Now().Add(wait)),
	})
	if err != nil {
		t.Fatalf("the PauseJob call got no result: %v", err)
	}

	if answer.Error != nil {
		t.Fatalf("the PauseJob call returned the error %s", answer.Error)
	}

	sent := built.fleet.sent()
	if len(sent) != 1 {
		t.Fatalf("the method call became %d commands, want 1", len(sent))
	}

	if sent[0].verb != "core.command.pause_print" || sent[0].principal != "node:hq" {
		t.Fatalf("the command is %s with the principal %q, want core.command.pause_print with node:hq",
			sent[0].verb, sent[0].principal)
	}
}

// TestARefusedMethodComesBackAsADeliberateRefusal: when the host denies the
// command, the server above receives a result with CodeCallNotAllowed, which
// refines CodeDenied.
func TestARefusedMethodComesBackAsADeliberateRefusal(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))
	built.fleet.refuse = deviceapi.ErrDenied

	answer, err := built.above.Call(t.Context(), fractalnode.CallRequest{
		Target:    sitePath,
		Interface: "Printer1",
		Instance:  fractal.InstanceID(printerOne),
		Method:    "CancelJob",
		Expires:   fractal.TimestampOf(time.Now().Add(wait)),
	})
	if err != nil {
		t.Fatalf("the CancelJob call got no result: %v", err)
	}

	if answer.Error == nil || answer.Error.Code != bridge.CodeCallNotAllowed {
		t.Fatalf("the CancelJob call returned %v, want an error with CodeCallNotAllowed", answer.Error)
	}
}

// TestAMethodWithBadParametersNeverReachesTheDevice: the Fractal node checks
// the parameters of a call against the interface definition. It answers a
// parameter of the wrong type with CodeInvalidParams, and the bridge sends no
// command (fractal.md, Method Invocation Request Message).
func TestAMethodWithBadParametersNeverReachesTheDevice(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))

	answer, err := built.above.Call(t.Context(), fractalnode.CallRequest{
		Target:    sitePath,
		Interface: "FDMPrinter1",
		Instance:  fractal.InstanceID(printerOne),
		Method:    "Home",
		Params:    raws(`"X"`, `true`, `true`),
		Expires:   fractal.TimestampOf(time.Now().Add(wait)),
	})
	if err != nil {
		t.Fatalf("the Home call got no result: %v", err)
	}

	if answer.Error == nil || answer.Error.Code != fractal.CodeInvalidParams {
		t.Fatalf("the Home call returned %v, want an error with CodeInvalidParams", answer.Error)
	}

	if len(built.fleet.sent()) != 0 {
		t.Fatal("the host received a command for a call with a parameter of the wrong type")
	}
}

// TestAChangeToAReadOnlyPropertyIsRefusedBeforeTheHostIsAsked: only the
// printer sets the property state. The Fractal node rejects a request to
// change it against the interface definition, and the host receives no command
// (fractal.md, Desired Properties Message).
func TestAChangeToAReadOnlyPropertyIsRefusedBeforeTheHostIsAsked(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))

	if err := built.above.Desire(t.Context(), fractalnode.DesireRequest{
		Target:    sitePath,
		Interface: "Printer1",
		Instance:  fractal.InstanceID(printerOne),
		Props:     fractal.Props{"state": json.RawMessage(`"READY"`)},
	}); err != nil {
		t.Fatalf("send the desired-properties request: %v", err)
	}

	// A rejected request gets no answer. So the test checks for a short time
	// that the host receives no command.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(built.fleet.sent()) != 0 {
			t.Fatal("the host received a command for a change to a read-only property")
		}

		time.Sleep(poll)
	}
}

// TestAChangeFromAboveBecomesOneCommand: a request from the node above to
// change targetNozzle becomes one set_nozzle_temperature command for the
// printer the instance id names.
func TestAChangeFromAboveBecomesOneCommand(t *testing.T) {
	t.Parallel()

	built := stand(t, printer(printerOne, telemetry()))

	if err := built.above.Desire(t.Context(), fractalnode.DesireRequest{
		Target:    sitePath,
		Interface: "FDMPrinterHead1",
		Instance:  fractal.InstanceID(printerOne + "_1"),
		Props:     fractal.Props{"targetNozzle": json.RawMessage(`215`)},
	}); err != nil {
		t.Fatalf("send the desired-properties request: %v", err)
	}

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if sent := built.fleet.sent(); len(sent) == 1 {
			if sent[0].verb != "core.command.set_nozzle_temperature" {
				t.Fatalf("the verb is %s, want core.command.set_nozzle_temperature", sent[0].verb)
			}

			if sent[0].args["nozzle_temperature"] != "215" {
				t.Fatalf("the argument nozzle_temperature is %q, want 215", sent[0].args["nozzle_temperature"])
			}

			return
		}

		time.Sleep(poll)
	}

	t.Fatal("the host never received a command for the requested change")
}
