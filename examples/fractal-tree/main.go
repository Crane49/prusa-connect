// A Prusa Fractal network of three nodes in one process: hq, hq/brno and
// hq/brno/printer-1. The printer owns one interface, described by printer.json
// and loaded at run time. Run it with `go run .`.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// files embeds printer.json, so the binary needs no other file at run time.
// To add a property to the printer, edit printer.json.
//
//go:embed printer.json
var files embed.FS

// The three nodes and the one interface the printer owns.
const (
	hq        = fractal.NodePath("hq")
	brno      = fractal.NodePath("hq/brno")
	printer   = fractal.NodePath("hq/brno/printer-1")
	brnoID    = fractal.NodeID("brno")
	printerID = fractal.NodeID("printer-1")
	iface     = fractal.InterfaceName("FDMPrinter1")
	instance  = fractal.InstanceID("1")
	setpoint  = fractal.PropertyName("nozzleTempTarget")
	measured  = fractal.PropertyName("nozzleTemp")
)

// key names the one interface hq keeps a copy of. The protocol calls that copy
// a shadow.
var key = shadow.Key{Node: printer, Interface: iface, Instance: instance}

// childSet lists the Node IDs of the nodes directly below one node. A
// deployment reads this list from the join records it keeps.
type childSet []fractal.NodeID

// Admitted reports whether id is one of the nodes below.
func (c childSet) Admitted(id fractal.NodeID) bool { return slices.Contains(c, id) }

// Children iterates the nodes below.
func (c childSet) Children() iter.Seq[fractal.NodeID] { return slices.Values(c) }

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run builds the network, watches one state change reach the root, and calls a
// method on the printer from the root.
func run() error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// The transport. This one keeps every link in memory. A deployment uses
	// the NATS transport, and the code below this line stays the same.
	fabric := fractaltest.NewFabric(fractaltest.FabricOptions{})
	hqLink, err := fabric.Link(hq, brnoID)
	if err != nil {
		return fmt.Errorf("link hq to brno: %w", err)
	}
	brnoLink, err := fabric.Link(brno, printerID)
	if err != nil {
		return fmt.Errorf("link brno to the printer: %w", err)
	}

	// hq keeps a copy of the state of the printer's interface. The selection
	// names the interface, so the copy exists with the status EMPTY before the
	// first report arrives.
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	if err != nil {
		return fmt.Errorf("build the shadow: %w", err)
	}
	root, err := build(fabric, hq, childSet{brnoID}, store)
	if err != nil {
		return err
	}
	middle, err := build(fabric, brno, childSet{printerID}, nil)
	if err != nil {
		return err
	}
	leaf, err := build(fabric, printer, node.NoChildren{}, nil)
	if err != nil {
		return err
	}

	owner := fractaltest.NewStateOwner(fractal.Props{
		measured: json.RawMessage(`25.0`),
		setpoint: json.RawMessage(`null`),
	})
	handle, err := register(leaf, owner)
	if err != nil {
		return err
	}
	// The method hq calls. It stores the setpoint, reports the change to the
	// node above, and returns true.
	owner.Handle("SetNozzleTemp", func(ctx context.Context, params []json.RawMessage) (json.RawMessage, error) {
		owner.Set(setpoint, params[0])
		if err := handle.Report(ctx, fractal.Props{setpoint: params[0]}, stamp()); err != nil {
			return nil, err
		}
		return json.RawMessage(`true`), nil
	})

	for _, engine := range []*node.Node{leaf, middle, root} {
		fabric.Attach(engine.Path(), engine)
		if err := engine.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", engine.Path(), err)
		}
		defer func() { _ = engine.Close() }()
	}
	fabric.Start(ctx)
	defer fabric.Close()

	// When a link connects, the node above asks the node below for a full
	// state report. brno forwards the report of the printer to hq, and hq
	// stores it in its copy.
	brnoLink.Connect(ctx)
	hqLink.Connect(ctx)
	show(root, "after the printer reported its state")

	// A state change at the printer reaches hq the same way.
	owner.Set(measured, json.RawMessage(`200.0`))
	if err := handle.Report(ctx, fractal.Props{measured: json.RawMessage(`200.0`)}, stamp()); err != nil {
		return fmt.Errorf("report the temperature: %w", err)
	}
	show(root, "after the nozzle heated up")

	// hq calls a method on the printer. brno forwards the call to the printer.
	res, err := root.Call(ctx, node.CallRequest{
		Target: printer, Interface: iface, Instance: instance, Method: "SetNozzleTemp",
		Params:  []json.RawMessage{json.RawMessage(`250.0`)},
		Expires: fractal.TimestampOf(time.Now().Add(30 * time.Second)),
	})
	if err != nil {
		return fmt.Errorf("call SetNozzleTemp: %w", err)
	}
	fmt.Printf("SetNozzleTemp answered %s\n", res.Result)
	show(root, "after the setpoint was accepted")
	return nil
}

// build makes one node. A node built with a nil store keeps no copy of the
// state of the nodes below it. That fits the printer, which has no nodes
// below, and brno, which only forwards.
func build(f *fractaltest.Fabric, path fractal.NodePath, kids node.Admissions, s *shadow.Store) (*node.Node, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	built, err := node.New(node.Config{
		Binding: f.Transport(path), Admissions: kids, Shadow: s, Logger: logger, Path: path,
	})
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", path, err)
	}
	return built, nil
}

// register gives the printer the interface printer.json describes.
func register(leaf *node.Node, owner node.Owner) (*node.Interface, error) {
	registry, err := schema.Load(files, "printer.json")
	if err != nil {
		return nil, fmt.Errorf("read printer.json: %w", err)
	}
	definition, ok := registry.Definition(iface)
	if !ok {
		return nil, fmt.Errorf("printer.json describes no %s", iface)
	}
	handle, err := leaf.Register(node.Instance{
		Definition: definition, Owner: owner, Name: iface, Instance: instance,
	})
	if err != nil {
		return nil, fmt.Errorf("register %s: %w", iface, err)
	}
	return handle, nil
}

// show waits a moment for the messages to arrive and prints hq's copy of the
// printer's state.
func show(root *node.Node, what string) {
	time.Sleep(50 * time.Millisecond)
	snap, ok := root.Shadow().Get(key)
	if !ok {
		fmt.Printf("%s: hq keeps no copy yet\n", what)
		return
	}
	fmt.Printf("%s: %s nozzleTemp=%s nozzleTempTarget=%s\n",
		what, snap.Status, snap.Props[measured], snap.Props[setpoint])
}

// stamp returns the current time as a fractal.Timestamp, the time format of
// every Fractal message.
func stamp() fractal.Timestamp { return fractal.TimestampOf(time.Now()) }
