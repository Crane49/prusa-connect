package fractaltest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// farmSize is the number of printers below one node in the reconnect test. A
// print farm connects hundreds of printers to one small computer.
const farmSize = 400

// farm is one root node with many printer nodes below it, all in one process.
type farm struct {
	fabric   *fractaltest.Fabric
	root     *node.Node
	links    []*fractaltest.Link
	owners   []*fractaltest.StateOwner
	printers []*node.Node
}

// newFarm builds a root node with size printers below it. Each printer owns
// one interface. The shadow of the root selects every interface with the
// pattern "*", so it creates a shadow interface when the first report for that
// interface arrives.
func newFarm(tb testing.TB, size int) *farm {
	tb.Helper()
	clock := newManualClock(tStartup)
	fabric := fractaltest.NewFabric(fractaltest.FabricOptions{Clock: clock})
	children := make(childSet, 0, size)
	for i := range size {
		children = append(children, printerNodeID(i))
	}
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		tb.Fatalf("build the shadow: %v", err)
	}
	root, err := node.New(node.Config{
		Binding:    fabric.Transport(hqPath),
		Admissions: children,
		Shadow:     store,
		Clock:      clock,
		Path:       hqPath,
	})
	if err != nil {
		tb.Fatalf("build the root: %v", err)
	}
	f := &farm{fabric: fabric, root: root}
	fabric.Attach(hqPath, root)
	for i := range size {
		f.addPrinter(tb, fabric, clock, i)
	}
	return f
}

// addPrinter builds printer number i below the root, with its link, its node
// and its StateOwner.
func (f *farm) addPrinter(tb testing.TB, fabric *fractaltest.Fabric, clock node.Clock, i int) {
	tb.Helper()
	id := printerNodeID(i)
	link, err := fabric.Link(hqPath, id)
	if err != nil {
		tb.Fatalf("link %s: %v", id, err)
	}
	path := link.ChildPath()
	printer, err := node.New(node.Config{
		Binding:    fabric.Transport(path),
		Admissions: node.NoChildren{},
		Clock:      clock,
		Path:       path,
	})
	if err != nil {
		tb.Fatalf("build %s: %v", id, err)
	}
	owner := fractaltest.NewStateOwner(fractal.Props{nozzleTemp: json.RawMessage(`25.0`)})
	if _, registerErr := printer.Register(node.Instance{
		Owner:    owner,
		Name:     printerInterface,
		Instance: printerInstance,
	}); registerErr != nil {
		tb.Fatalf("register on %s: %v", id, registerErr)
	}
	fabric.Attach(path, printer)
	f.links = append(f.links, link)
	f.owners = append(f.owners, owner)
	f.printers = append(f.printers, printer)
}

// start starts the root, every printer node and the fabric. No link is up
// after it returns.
func (f *farm) start(ctx context.Context, tb testing.TB) {
	tb.Helper()
	if err := f.root.Start(ctx); err != nil {
		tb.Fatalf("start the root: %v", err)
	}
	tb.Cleanup(func() { _ = f.root.Close() })
	for _, printer := range f.printers {
		if err := printer.Start(ctx); err != nil {
			tb.Fatalf("start a printer: %v", err)
		}
		tb.Cleanup(func() { _ = printer.Close() })
	}
	f.fabric.Start(ctx)
	tb.Cleanup(f.fabric.Close)
}

// connectAll brings every printer link up, one after the other. The root gets
// a connect signal and then a full state report from every printer, as it does
// when a network returns after a failure.
func (f *farm) connectAll(ctx context.Context) {
	for _, link := range f.links {
		link.Connect(ctx)
	}
}

// cutAll takes every printer link down.
func (f *farm) cutAll(ctx context.Context) {
	for _, link := range f.links {
		link.Cut(ctx)
	}
}

// printerNodeID returns the Node ID of printer number i, such as printer-007.
func printerNodeID(i int) fractal.NodeID {
	return fractal.NodeID(fmt.Sprintf("printer-%03d", i))
}

// TestAReconnectBurstCompletes cuts the links of farmSize printers, changes
// nozzleTemp on each printer, and brings all the links back. Every printer's
// interface in the shadow of the root must then have the status OK. fractal.md
// says that a reconnection causes many reports at once, and asks an
// implementation to handle them (fractal.md, Synchronization of the Shadow -
// Reaction to Connection Loss and Reconnection).
func TestAReconnectBurstCompletes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newFarm(t, farmSize)
		f.start(t.Context(), t)
		f.connectAll(t.Context())
		synctest.Wait()
		if got := f.root.Shadow().Len(); got != farmSize {
			t.Fatalf("the shadow of the root has %d interfaces, want %d, one for each printer", got, farmSize)
		}

		f.cutAll(t.Context())
		synctest.Wait()
		wantEvery(t, f, shadow.StatusStale)

		for i, owner := range f.owners {
			owner.Set(nozzleTemp, json.RawMessage(fmt.Sprintf("%d.0", i)))
		}
		f.connectAll(t.Context())
		synctest.Wait()
		wantEvery(t, f, shadow.StatusOK)
	})
}

// wantEvery checks that every printer's interface in the shadow of the root
// has the given status.
func wantEvery(t *testing.T, f *farm, status shadow.Status) {
	t.Helper()
	for i := range f.links {
		key := shadow.Key{
			Node:      f.links[i].ChildPath(),
			Interface: printerInterface,
			Instance:  printerInstance,
		}
		got, _, ok := f.root.Shadow().Status(key)
		if !ok {
			t.Fatalf("the shadow of the root has no interface %s", key)
		}
		if got != status {
			t.Fatalf("the shadow interface %s has the status %s, want %s", key, got, status)
		}
	}
}
