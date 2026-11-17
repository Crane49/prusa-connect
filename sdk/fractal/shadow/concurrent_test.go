package shadow_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// printerKeyAt returns the key of the interface of printer n. The printers are
// spread over the nodes "hq/hall-0" to "hq/hall-3".
func printerKeyAt(n int) shadow.Key {
	return shadow.Key{
		Node:      fractal.NodePath("hq/hall-" + strconv.Itoa(n%4) + "/printer-" + strconv.Itoa(n)),
		Interface: "FDMPrinter1",
		Instance:  "1",
	}
}

// TestConcurrentApplyOnManyNodes applies REP messages for many nodes to one
// Store, with one goroutine per node. A node above many printers gets its REP
// messages this way.
func TestConcurrentApplyOnManyNodes(t *testing.T) {
	t.Parallel()
	const nodes, messages = 64, 200
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	var wg sync.WaitGroup
	for n := range nodes {
		wg.Go(func() {
			key := printerKeyAt(n)
			for i := range messages {
				store.ApplyREP(rep(key, fractal.Timestamp(i+1), nil))
			}
		})
	}
	wg.Wait()
	if got := store.Len(); got != nodes {
		t.Fatalf("the store holds %d interfaces, want %d", got, nodes)
	}
	for n := range nodes {
		key := printerKeyAt(n)
		status, updatedAt, ok := store.Status(key)
		if !ok || status != shadow.StatusOK || updatedAt != messages {
			t.Fatalf("%s is %s at %d, want OK at %d", key, status, updatedAt, messages)
		}
	}
}

// TestConcurrentApplyOnOneInterface checks the result when several goroutines
// apply REP messages to the same interface: the interface ends with the
// highest timestamp any of them sent.
func TestConcurrentApplyOnOneInterface(t *testing.T) {
	t.Parallel()
	const writers, messages = 16, 200
	key := shadow.Key{Node: "hq/printer-1", Interface: "FDMPrinter1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Interfaces: []shadow.Key{key}}})
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for i := range messages {
				store.ApplyREP(rep(key, fractal.Timestamp(i+1), nil))
			}
		})
	}
	wg.Wait()
	status, updatedAt, ok := store.Status(key)
	if !ok || status != shadow.StatusOK || updatedAt != messages {
		t.Fatalf("the interface is %s at %d, want OK at %d", status, updatedAt, messages)
	}
}

// TestConcurrentApplyAndStale calls ApplyREP, MarkStale, RemoveNode and the
// read functions on one Store at the same time, while a goroutine reads a
// Subscription. It checks no result. It exists so that the Go race detector
// checks these calls.
func TestConcurrentApplyAndStale(t *testing.T) {
	t.Parallel()
	const nodes, rounds, halls = 32, 100, 4
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	sub := store.Subscribe(8)
	defer sub.Close()
	done := make(chan struct{})
	var wg, reader sync.WaitGroup

	reader.Go(func() {
		for {
			select {
			case <-done:
				return
			case <-sub.Events():
			}
		}
	})

	for n := range nodes {
		wg.Go(func() {
			key := printerKeyAt(n)
			for i := range rounds {
				store.ApplyREP(rep(key, fractal.Timestamp(i+1), nil))
				store.Status(key)
				store.Get(key)
			}
		})
	}
	for hall := range halls {
		wg.Go(func() {
			path := fractal.NodePath("hq/hall-" + strconv.Itoa(hall))
			for range rounds {
				store.MarkStale(path)
				for key := range store.Subtree(path) {
					store.Status(key)
				}
			}
		})
	}
	wg.Go(func() {
		for range rounds {
			store.RemoveNode("hq/hall-3")
			for node := range store.Nodes() {
				store.Interfaces(node)
			}
		}
	})
	wg.Wait()
	close(done)
	reader.Wait()
}

// TestConcurrentApplyAndRebase runs ApplyREP for a former ancestor and for a
// current one while Rebase moves the Store to a new path. Once Rebase and every
// ApplyREP have returned, the Store holds nothing of the former ancestor, so a
// late report from a former parent cannot bring it back.
func TestConcurrentApplyAndRebase(t *testing.T) {
	t.Parallel()
	former := shadow.Key{Node: "hq/brno", Interface: "Site1", Instance: "1"}
	kept := shadow.Key{Node: "hq", Interface: "Site1", Instance: "1"}
	store := newStore(t, shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"Site1"}}})
	store.Rebase("hq/brno/printer-1")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			<-start
			for i := range 100 {
				at := fractal.Timestamp(worker*100 + i)
				store.ApplyREP(rep(former, at, nil))
				store.ApplyREP(rep(kept, at, nil))
			}
		})
	}
	close(start)
	store.Rebase("hq/praha/printer-1")
	wg.Wait()

	if _, _, ok := store.Status(former); ok {
		t.Fatalf("the store holds %s after Rebase moved it below hq/praha", former)
	}
}
