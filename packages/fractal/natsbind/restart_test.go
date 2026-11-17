package natsbind_test

import (
	"fmt"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestANodeInTheMiddleComesBackWithWhatItHeld restarts brno, a node between hq
// and printer-1, while printer-1 is disconnected. After the restart brno still
// has printer-1 as admitted, its Node Path, and the stream with the method
// invocations for printer-1. printer-1 then receives them in order
// (fractal-nats.md, Provisioning the Link).
func TestANodeInTheMiddleComesBackWithWhatItHeld(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hqCfg, hqLeaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	toBrno := newProxy(t, fmt.Sprintf("127.0.0.1:%d", hqLeaf))
	hqCfg.AdvertisedLeafRemote = "nats://" + toBrno.Addr()
	toBrno.Open()
	startNode(ctx, t, hqCfg)

	brnoCfg, brnoLeaf := baseConfig(t, nodeOptions{
		id: "brno", accepts: true, parent: hqCfg.ClientListen, dataDir: t.TempDir(),
	})
	toPrinter := newProxy(t, fmt.Sprintf("127.0.0.1:%d", brnoLeaf))
	brnoCfg.AdvertisedLeafRemote = "nats://" + toPrinter.Addr()
	toPrinter.Open()
	brno := startNode(ctx, t, brnoCfg)

	printerCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: brnoCfg.ClientListen})
	printer := startNode(ctx, t, printerCfg)
	waitUntil(t, "brno to see printer-1 as connected", func() bool { return len(brno.engine.Connected()) >= 1 })

	toPrinter.Cut()
	waitUntil(t, "brno to see printer-1 as lost", func() bool { return len(brno.engine.Lost()) == 1 })

	ids := []fractal.RPCID{"one", "two", "three"}
	for _, id := range ids {
		send(t, brno, fractalnode.Dispatch{
			Kind: fractal.KindRPC, Payload: rpcPayload(t, id), Child: "printer-1", ID: id,
		})
	}

	if err := brno.binding.Close(); err != nil {
		t.Fatalf("close brno: %v", err)
	}
	again := startNode(ctx, t, brnoCfg)
	if !again.binding.Admitted("printer-1") {
		t.Fatal("after the restart brno does not have printer-1 as admitted")
	}
	if got := again.binding.Path(); got != "hq/brno" {
		t.Errorf("the Path of brno after the restart is %q, want hq/brno", got)
	}

	toPrinter.Open()
	awaitKind(t, printer, fractal.KindRPC, len(ids))
	for i, m := range onlyKind(printer, fractal.KindRPC) {
		if want := string(rpcPayload(t, ids[i])); string(m) != want {
			t.Errorf("invocation %d is %s, want %s", i, m, want)
		}
	}
}
