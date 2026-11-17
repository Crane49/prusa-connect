package natsbind_test

import (
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// benchPoll is how often a benchmark checks whether a message has arrived. It
// is short because every NATS server runs in this process on the loopback
// interface.
const benchPoll = 100 * time.Microsecond

// BenchmarkStateReportOverOneHop measures one state report from a node to the
// node above it, over core NATS and a leafnode connection on the loopback
// interface.
func BenchmarkStateReportOverOneHop(b *testing.B) {
	ctx := b.Context()
	p := newPair(ctx, b)
	payload := repPayload(b, "hq/printer-1", 1)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		send(b, p.below, fractalnode.Dispatch{Kind: fractal.KindREP, Payload: payload})
	}
	awaitCount(b, p.above, fractal.KindREP, b.N)
}

// BenchmarkMethodInvocationRoundTripOverTwoHops measures a method invocation
// from hq through brno to printer-1 and its result back. Each hop goes through
// a JetStream work queue stored in files.
//
// The benchmark forwards the messages at brno itself and uses no engine. So
// the result is the time of the transport, with two JetStream publishes in
// each direction, and no time of the engine.
func BenchmarkMethodInvocationRoundTripOverTwoHops(b *testing.B) {
	ctx := b.Context()
	tr := newTree(ctx, b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		id := fractal.RPCID(idOf(i))
		call := rpcPayload(b, id)
		send(b, tr.hq, fractalnode.Dispatch{Kind: fractal.KindRPC, Payload: call, Child: "brno", ID: id})
		awaitCount(b, tr.brno, fractal.KindRPC, i+1)
		send(b, tr.brno, fractalnode.Dispatch{Kind: fractal.KindRPC, Payload: call, Child: "printer-1", ID: id})
		awaitCount(b, tr.printer, fractal.KindRPC, i+1)

		answer := resPayload(b, id)
		send(b, tr.printer, fractalnode.Dispatch{Kind: fractal.KindRES, Payload: answer, ID: id})
		awaitCount(b, tr.brno, fractal.KindRES, i+1)
		send(b, tr.brno, fractalnode.Dispatch{Kind: fractal.KindRES, Payload: answer, ID: id})
		awaitCount(b, tr.hq, fractal.KindRES, i+1)
	}
}

// awaitCount waits until the engine of a node has received want messages of
// one type. It polls every benchPoll, so the polling adds little to the
// measured time.
func awaitCount(b *testing.B, n *node, kind fractal.Kind, want int) {
	b.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if countKind(n, kind) >= want {
			return
		}
		time.Sleep(benchPoll)
	}
	b.Fatalf("waited for %d %s messages and %d arrived", want, kind, countKind(n, kind))
}

// idOf returns a method invocation id made of letters for the counter i.
func idOf(i int) string {
	const digits = "abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, 8)
	for {
		out = append(out, digits[i%len(digits)])
		i /= len(digits)
		if i == 0 {
			return string(out)
		}
	}
}
