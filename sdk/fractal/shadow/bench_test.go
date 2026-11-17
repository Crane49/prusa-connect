package shadow_test

import (
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// These benchmarks measure the calls a node above several hundred printers
// makes: ApplyREP for each state change, many ApplyREP calls at once after a
// node connects again, and MarkStale when a node below is lost.

// benchProps has the property values of a printer that reports its nozzle
// temperature.
var benchProps = fractal.Props{
	"nozzleTemp":       json.RawMessage("200.0"),
	"nozzleTempTarget": json.RawMessage("250.0"),
}

// benchKey returns the key of the interface of printer n.
func benchKey(n int) shadow.Key {
	return shadow.Key{
		Node:      fractal.NodePath("hq/hall-" + strconv.Itoa(n%4) + "/printer-" + strconv.Itoa(n)),
		Interface: "FDMPrinter1",
		Instance:  "1",
	}
}

// benchStore builds a Store with one OK interface for each of nodes printers.
func benchStore(b *testing.B, nodes int) *shadow.Store {
	b.Helper()
	store, err := shadow.New(shadow.Config{
		Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}},
	})
	if err != nil {
		b.Fatal(err)
	}
	for n := range nodes {
		store.ApplyREP(rep(benchKey(n), 1, benchProps))
	}
	return store
}

func BenchmarkApplyREPToHeldInterface(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	m := rep(benchKey(0), 0, benchProps)
	var ts fractal.Timestamp
	for b.Loop() {
		ts++
		m.Timestamp = ts
		if res := store.ApplyREP(m); res.Outcome != shadow.OutcomeApplied {
			b.Fatalf("outcome = %s", res.Outcome)
		}
	}
}

func BenchmarkApplyREPNotSelected(b *testing.B) {
	b.ReportAllocs()
	store, err := shadow.New(shadow.Config{
		Selection: shadow.Selection{Patterns: []fractal.Pattern{"FDM*"}},
	})
	if err != nil {
		b.Fatal(err)
	}
	m := rep(shadow.Key{Node: "hq/a", Interface: "Camera1", Instance: "1"}, 1, benchProps)
	for b.Loop() {
		if res := store.ApplyREP(m); res.Outcome != shadow.OutcomeNotSelected {
			b.Fatalf("outcome = %s", res.Outcome)
		}
	}
}

func BenchmarkApplyREPDiscarded(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	store.ApplyREP(rep(benchKey(0), 1000, benchProps))
	m := rep(benchKey(0), 1, benchProps)
	for b.Loop() {
		if res := store.ApplyREP(m); res.Outcome != shadow.OutcomeDiscarded {
			b.Fatalf("outcome = %s", res.Outcome)
		}
	}
}

// BenchmarkReconnectBurst measures the largest load of fractal.md,
// Synchronization of the Shadow. After a reconnection the node asks the nodes
// below for their state with an RBR message, and every printer answers with a
// REP for each of its interfaces at the same time.
func BenchmarkReconnectBurst(b *testing.B) {
	const nodes, interfaces = 400, 5
	b.ReportAllocs()
	store := benchStore(b, 0)
	messages := make([]fractal.REP, 0, nodes*interfaces)
	for n := range nodes {
		for i := range interfaces {
			key := benchKey(n)
			key.Instance = fractal.InstanceID(strconv.Itoa(i + 1))
			messages = append(messages, rep(key, 1, benchProps))
		}
	}
	var ts fractal.Timestamp
	for b.Loop() {
		ts++
		for i := range messages {
			messages[i].Timestamp = ts
			store.ApplyREP(messages[i])
		}
	}
	b.ReportMetric(float64(len(messages)), "msgs/op")
}

// BenchmarkMarkStaleSubtree measures MarkStale for one hall of printers, in a
// Store with several hundred printers.
func BenchmarkMarkStaleSubtree(b *testing.B) {
	const nodes = 400
	b.ReportAllocs()
	store := benchStore(b, nodes)
	var ts fractal.Timestamp
	for b.Loop() {
		b.StopTimer()
		ts++
		for n := range nodes {
			store.ApplyREP(rep(benchKey(n), ts, nil))
		}
		b.StartTimer()
		store.MarkStale("hq/hall-2")
	}
}

// BenchmarkMarkStaleWholeStore measures MarkStale when the lost node is "hq",
// so every interface in the Store becomes STALE.
func BenchmarkMarkStaleWholeStore(b *testing.B) {
	const nodes = 400
	b.ReportAllocs()
	store := benchStore(b, nodes)
	var ts fractal.Timestamp
	for b.Loop() {
		b.StopTimer()
		ts++
		for n := range nodes {
			store.ApplyREP(rep(benchKey(n), ts, nil))
		}
		b.StartTimer()
		store.MarkStale("hq")
	}
}

// BenchmarkApplyREPParallel measures ApplyREP when several goroutines apply
// REP messages of different printers at the same time.
func BenchmarkApplyREPParallel(b *testing.B) {
	const nodes = 400
	b.ReportAllocs()
	store := benchStore(b, nodes)
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		n := int(counter.Add(1))
		m := rep(benchKey(n%nodes), 0, benchProps)
		var ts fractal.Timestamp = 1
		for pb.Next() {
			ts++
			m.Timestamp = ts
			store.ApplyREP(m)
		}
	})
}

// BenchmarkApplyREPParallelOneInterface measures ApplyREP when every goroutine
// applies a REP to the same interface, so they all wait for the lock of one
// node.
func BenchmarkApplyREPParallelOneInterface(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	b.RunParallel(func(pb *testing.PB) {
		m := rep(benchKey(0), 1, benchProps)
		for pb.Next() {
			store.ApplyREP(m)
		}
	})
}

func BenchmarkGet(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	key := benchKey(0)
	for b.Loop() {
		if _, ok := store.Get(key); !ok {
			b.Fatal("the interface is gone")
		}
	}
}

func BenchmarkStatus(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	key := benchKey(0)
	for b.Loop() {
		if _, _, ok := store.Status(key); !ok {
			b.Fatal("the interface is gone")
		}
	}
}

func BenchmarkProp(b *testing.B) {
	b.ReportAllocs()
	store := benchStore(b, 1)
	key := benchKey(0)
	for b.Loop() {
		if _, ok := store.Prop(key, "nozzleTemp"); !ok {
			b.Fatal("the property is gone")
		}
	}
}
