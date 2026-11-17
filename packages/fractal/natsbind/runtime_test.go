package natsbind_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestTheIdentityIsTheSameAfterARestart checks that a node reads the same
// identity key from its data directory at the next start. With a new key and
// the same Node ID the node above would deny the join with a collision, and
// fractal.md forbids a retry after that (fractal.md, Uniqueness).
func TestTheIdentityIsTheSameAfterARestart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1", dataDir: dir})
	first := buildNode(ctx, t, cfg)
	again := buildNode(ctx, t, cfg)
	if first.binding.Identity() == "" {
		t.Fatal("the node created no identity key")
	}
	if first.binding.Identity() != again.binding.Identity() {
		t.Errorf("the identity after the restart is %q, want %q", again.binding.Identity(), first.binding.Identity())
	}
	seed, err := os.ReadFile(filepath.Join(dir, "identity.nk"))
	if err != nil {
		t.Fatalf("read the seed file: %v", err)
	}
	if string(seed) == string(first.binding.Identity()) {
		t.Error("Identity returned the private seed")
	}
	if got := first.binding.Status(); string(seed) == string(got.Identity) {
		t.Error("Status.Identity is the private seed")
	}
}

// TestANodeThatTakesNothingExposesNoJoinEndpoint checks that a node that does
// not accept joins has no join endpoint and no bootstrap account
// (fractal-nats.md, Bootstrap Account and Join Endpoint).
func TestANodeThatTakesNothingExposesNoJoinEndpoint(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	closedCfg, _ := baseConfig(t, nodeOptions{id: "hq"})
	closed := startNode(ctx, t, closedCfg)
	if got := closed.binding.Status().Accepting; got {
		t.Error("Status.Accepting is true on a node that does not accept joins")
	}
	belowCfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: closedCfg.ClientListen})
	below := buildNode(ctx, t, belowCfg)
	tryCtx, giveUp := context.WithTimeout(ctx, 3*time.Second)
	defer giveUp()
	if err := below.binding.Join(tryCtx); err == nil {
		t.Error("Join succeeded at a node that does not accept joins")
	}
}

// TestTheDeduplicationBucketsHoldWhatWasDone checks the deduplication buckets
// of a node: one stores the result of each method invocation the node
// executed, the other stores the id of each result it processed
// (fractal-nats.md, Deduplication).
func TestTheDeduplicationBucketsHoldWhatWasDone(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	store := p.below.binding

	if _, ok, err := store.Executed(ctx, "one"); err != nil || ok {
		t.Fatalf("Executed found an invocation the node never executed: %v %v", ok, err)
	}
	answer := resPayload(t, "one")
	if err := store.RecordExecuted(ctx, "one", answer); err != nil {
		t.Fatalf("RecordExecuted: %v", err)
	}
	held, ok, err := store.Executed(ctx, "one")
	if err != nil || !ok {
		t.Fatalf("Executed did not find the recorded invocation: %v %v", ok, err)
	}
	if string(held) != string(answer) {
		t.Errorf("Executed returned %s, want the recorded result", held)
	}

	if seen, procErr := store.Processed(ctx, "one"); procErr != nil || seen {
		t.Fatalf("Processed found a result the node never processed: %v %v", seen, procErr)
	}
	if recErr := store.RecordProcessed(ctx, "one"); recErr != nil {
		t.Fatalf("RecordProcessed: %v", recErr)
	}
	seen, err := store.Processed(ctx, "one")
	if err != nil || !seen {
		t.Fatalf("Processed did not find the recorded result: %v %v", seen, err)
	}

	store.Reset()
	if _, stillThere, resetErr := store.Executed(ctx, "one"); resetErr != nil || stillThere {
		t.Errorf("Executed still finds the invocation after Reset: %v %v", stillThere, resetErr)
	}
}

// TestTheDeduplicationStoreIsRefusedBeforeTheNodeStarts checks that the
// deduplication methods return ErrNotStarted before Start has created the
// buckets.
func TestTheDeduplicationStoreIsRefusedBeforeTheNodeStarts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1"})
	built := buildNode(ctx, t, cfg)
	if _, _, err := built.binding.Executed(ctx, "one"); !errors.Is(err, natsbind.ErrNotStarted) {
		t.Errorf("Executed returned %v, want ErrNotStarted", err)
	}
	if err := built.binding.RecordProcessed(ctx, "one"); !errors.Is(err, natsbind.ErrNotStarted) {
		t.Errorf("RecordProcessed returned %v, want ErrNotStarted", err)
	}
}

// TestTheHeartbeatKeepsALinkAlive checks that a node sends heartbeats to the
// node above, and that Status.Nodes shows when the last one arrived
// (fractal-nats.md, Connectivity Detection).
func TestTheHeartbeatKeepsALinkAlive(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	var first *time.Time
	waitUntil(t, "the first heartbeat", func() bool {
		for _, n := range p.above.binding.Status().Nodes {
			if n.ID == "printer-1" && n.LastHeartbeat != nil {
				first = n.LastHeartbeat
				return true
			}
		}
		return false
	})
	waitUntil(t, "a later heartbeat", func() bool {
		for _, n := range p.above.binding.Status().Nodes {
			if n.ID == "printer-1" && n.LastHeartbeat != nil && n.LastHeartbeat.After(*first) {
				return true
			}
		}
		return false
	})
}

// TestANodeWithNoNodePathDoesNotStart checks that Start returns ErrNoNodePath
// before a join is granted. The node names its JetStream domain after the Node
// Path, so it cannot start without one (fractal.md, Node Path).
func TestANodeWithNoNodePathDoesNotStart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1", parent: "127.0.0.1:1"})
	built := buildNode(ctx, t, cfg)
	if err := built.binding.Start(ctx); !errors.Is(err, natsbind.ErrNoNodePath) {
		t.Errorf("Start returned %v, want ErrNoNodePath", err)
	}
	if got := built.binding.Status().Uplink.State; got != natsbind.UplinkJoining {
		t.Errorf("Status.Uplink.State is %q, want joining", got)
	}
}

// TestARunningNodeIsNotStartedTwice checks that a second Start returns
// ErrAlreadyStarted and a second Close returns nil.
func TestARunningNodeIsNotStartedTwice(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPair(ctx, t)
	if err := p.above.binding.Start(ctx); !errors.Is(err, natsbind.ErrAlreadyStarted) {
		t.Errorf("the second Start returned %v, want ErrAlreadyStarted", err)
	}
	if err := p.above.binding.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.above.binding.Close(); err != nil {
		t.Errorf("the second Close returned %v, want nil", err)
	}
}

// TestARootNodeSendsNothingUpward checks that a root node returns ErrNoUplink
// for an upward message and does not publish it in its own account.
func TestARootNodeSendsNothingUpward(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, leaf := baseConfig(t, nodeOptions{id: "hq", accepts: true})
	cfg.AdvertisedLeafRemote = fmt.Sprintf("nats://127.0.0.1:%d", leaf)
	root := startNode(ctx, t, cfg)
	if best := root.binding.SendBestEffort(ctx, fractalnode.Dispatch{
		Kind: fractal.KindREP, Payload: repPayload(t, "hq", 1),
	}); !errors.Is(best, natsbind.ErrNoUplink) {
		t.Errorf("SendBestEffort returned %v, want ErrNoUplink", best)
	}
	if durable := root.binding.SendDurable(ctx, fractalnode.Dispatch{
		Kind: fractal.KindRES, Payload: resPayload(t, "one"), ID: "one",
	}); !errors.Is(durable, natsbind.ErrNoUplink) {
		t.Errorf("SendDurable returned %v, want ErrNoUplink", durable)
	}
	if got := root.binding.Status().Uplink.State; got != natsbind.UplinkNotConfigured {
		t.Errorf("Status.Uplink.State is %q, want not_configured", got)
	}
}
