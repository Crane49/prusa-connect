package fractaltest_test

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// benchSpins is the number of times waitFor checks its condition before it
// fails the benchmark.
const benchSpins = 20_000_000

// benchREP is the state report that BenchmarkForwardREP delivers to the node.
var benchREP = fractal.REP{
	Version:   fractal.Version,
	Sender:    printerPath,
	Interface: printerInterface,
	Instance:  printerInstance,
	Timestamp: tStartup,
	Props:     fractal.Props{nozzleTemp: json.RawMessage(`200.0`)},
}

// discardBinding is a node.Binding that drops every message. With it, a
// benchmark measures the node and no transport.
type discardBinding struct{}

// SendBestEffort drops the message and returns nil.
func (discardBinding) SendBestEffort(context.Context, node.Dispatch) error { return nil }

// SendDurable drops the message and returns nil.
func (discardBinding) SendDurable(context.Context, node.Dispatch) error { return nil }

// BenchmarkForwardREP measures the cost of one state report at a node between
// the printer and the root. The node decodes the report, updates its shadow
// and sends the same bytes to the node above. Every state report of every
// printer goes through this code.
func BenchmarkForwardREP(b *testing.B) {
	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		b.Fatalf("build the shadow: %v", err)
	}
	middle, err := node.New(node.Config{
		Binding:    discardBinding{},
		Admissions: childSet{printerID},
		Shadow:     store,
		Path:       brnoPath,
	})
	if err != nil {
		b.Fatalf("build the node: %v", err)
	}
	if startErr := middle.Start(b.Context()); startErr != nil {
		b.Fatalf("start the node: %v", startErr)
	}
	b.Cleanup(func() { _ = middle.Close() })

	payloads := make([][]byte, b.N)
	for i := range payloads {
		report := benchREP
		report.Timestamp = tStartup + fractal.Timestamp(i)
		payloads[i] = mustJSON(b, report)
	}
	in := node.Inbound{Kind: fractal.KindREP, From: printerID}
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		in.Payload = payloads[i]
		middle.Deliver(ctx, in)
	}
}

// BenchmarkCallOverThreeNodes measures one method invocation from hq through
// brno to the printer, together with the answer on the way back.
func BenchmarkCallOverThreeNodes(b *testing.B) {
	n := newNetwork(b)
	n.startAll(b)
	waitFor(b, func() bool { return n.hq.Shadow().Len() == 1 }, "the first report of the printer to reach hq")
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		res, err := n.hq.Call(ctx, node.CallRequest{
			Target:    printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Method:    setNozzleTemp,
			Params:    []json.RawMessage{json.RawMessage(`250.0`)},
			Expires:   fractal.TimestampOf(n.clock.Now()) + callWindow,
		})
		if err != nil {
			b.Fatalf("call %s: %v", setNozzleTemp, err)
		}
		if res.Failed() {
			b.Fatalf("the call failed: %v", res.Error)
		}
	}
}

// BenchmarkReconnectBurst measures the reconnection of farmSize printers to
// one node. The node sends a request for a full state report to each printer
// and processes the report each printer sends back.
func BenchmarkReconnectBurst(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		f := newFarm(b, farmSize)
		f.start(b.Context(), b)
		f.connectAll(b.Context())
		waitFor(b, func() bool { return everyShadowOK(f) }, "every shadow interface to be OK after the first connect")
		f.cutAll(b.Context())
		b.StartTimer()

		f.connectAll(b.Context())
		waitFor(b, func() bool { return everyShadowOK(f) }, "every shadow interface to be OK after the reconnect")
	}
}

// everyShadowOK reports whether every printer's interface in the shadow of the
// root has the status OK.
func everyShadowOK(f *farm) bool {
	if f.root.Shadow().Len() < len(f.links) {
		return false
	}
	for i := range f.links {
		key := shadow.Key{
			Node:      f.links[i].ChildPath(),
			Interface: printerInterface,
			Instance:  printerInstance,
		}
		status, _, found := f.root.Shadow().Status(key)
		if !found || status != shadow.StatusOK {
			return false
		}
	}
	return true
}

// waitFor calls runtime.Gosched until done returns true, so the goroutines of
// the nodes can run. It fails the benchmark after benchSpins checks. A
// benchmark cannot run inside a synctest bubble, so it has no synctest.Wait.
func waitFor(b *testing.B, done func() bool, what string) {
	b.Helper()
	for range benchSpins {
		if done() {
			return
		}
		runtime.Gosched()
	}
	b.Fatalf("waited for %s, and it did not happen in %d checks", what, benchSpins)
}

// mustJSON encodes one message as JSON and fails the benchmark on an error.
func mustJSON(b *testing.B, m any) []byte {
	b.Helper()
	payload, err := json.Marshal(m)
	if err != nil {
		b.Fatalf("encode the message as JSON: %v", err)
	}
	return payload
}
