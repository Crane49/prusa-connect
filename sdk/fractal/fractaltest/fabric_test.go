package fractaltest_test

import (
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// TestALinkNamesBothItsEnds checks Parent, Child, ChildPath and Connected of a
// new link, and that NewLink refuses an invalid Node ID.
func TestALinkNamesBothItsEnds(t *testing.T) {
	t.Parallel()
	link, err := fractaltest.NewLink(fractaltest.LinkConfig{Parent: brnoPath, Child: printerID})
	if err != nil {
		t.Fatalf("build the link: %v", err)
	}
	if link.Parent() != brnoPath || link.Child() != printerID || link.ChildPath() != printerPath {
		t.Errorf("the link has Parent %s, Child %s and ChildPath %s, want %s, %s and %s",
			link.Parent(), link.Child(), link.ChildPath(), brnoPath, printerID, printerPath)
	}
	if link.Connected() {
		t.Error("Connected returned true for a link before Connect was called")
	}
	if _, linkErr := fractaltest.NewLink(fractaltest.LinkConfig{Parent: brnoPath, Child: "-bad"}); linkErr == nil {
		t.Error("NewLink accepted the Node ID \"-bad\", want an error: a Node ID does not start with a hyphen")
	}
}

// TestATransportRefusesAMessageWithNoLink checks that a send returns ErrNoLink
// when the transport has no link for the message: no link to the node below
// that the message names, and no link to a node above.
func TestATransportRefusesAMessageWithNoLink(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fabric := fractaltest.NewFabric(fractaltest.FabricOptions{})
		transport := fabric.Transport(hqPath)
		err := transport.SendBestEffort(t.Context(), node.Dispatch{Kind: fractal.KindDES, Child: printerID})
		if !errors.Is(err, fractaltest.ErrNoLink) {
			t.Errorf("a send down to a node with no link returned %v, want %v", err, fractaltest.ErrNoLink)
		}
		err = transport.SendDurable(t.Context(), node.Dispatch{Kind: fractal.KindRES})
		if !errors.Is(err, fractaltest.ErrNoLink) {
			t.Errorf("a send up from a root node returned %v, want %v", err, fractaltest.ErrNoLink)
		}
		if transport.Path() != hqPath {
			t.Errorf("Path returned %s, want %s", transport.Path(), hqPath)
		}
	})
}

// TestTheFabricHoldsItsLinks checks Link, Links, LinkTo, Start, ConnectAll and
// Close of a Fabric with two links.
func TestTheFabricHoldsItsLinks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fabric := fractaltest.NewFabric(fractaltest.FabricOptions{})
		first, err := fabric.Link(hqPath, brnoID)
		if err != nil {
			t.Fatalf("link hq to brno: %v", err)
		}
		again, err := fabric.Link(hqPath, brnoID)
		if err != nil {
			t.Fatalf("link hq to brno again: %v", err)
		}
		if first != again {
			t.Error("the second Link call for hq and brno built a new link, want the existing link")
		}
		if _, linkErr := fabric.Link(hqPath, "-bad"); linkErr == nil {
			t.Error("Link accepted the Node ID \"-bad\", want an error: a Node ID does not start with a hyphen")
		}
		if _, linkErr := fabric.Link(brnoPath, printerID); linkErr != nil {
			t.Fatalf("link brno to the printer: %v", linkErr)
		}
		if got := len(fabric.Links()); got != 2 {
			t.Errorf("Links returned %d links, want 2", got)
		}
		if _, ok := fabric.LinkTo(brnoPath); !ok {
			t.Error("LinkTo found no link between brno and hq")
		}
		if _, ok := fabric.LinkTo(hqPath); ok {
			t.Error("LinkTo found a link above hq, and hq is the root node")
		}

		above, below := fractaltest.NewRecorder(true), fractaltest.NewRecorder(true)
		fabric.Attach(hqPath, above)
		fabric.Attach(brnoPath, below)
		fabric.Start(t.Context())
		fabric.Start(t.Context())
		t.Cleanup(fabric.Close)
		fabric.ConnectAll(t.Context())
		synctest.Wait()

		if _, linkErr := fabric.Link(hqPath, "praha"); !errors.Is(linkErr, fractaltest.ErrFabricStarted) {
			t.Errorf("Link after Start returned %v, want %v", linkErr, fractaltest.ErrFabricStarted)
		}
		if got := len(above.Connected()); got != 1 {
			t.Errorf("the engine of hq got %d ChildConnected calls, want 1", got)
		}
		fabric.Close()
		fabric.Close()
	})
}

// TestALinkCountsWhatItCarried sends one durable message through a link that
// is not connected. The counters must show one message sent, none delivered
// and one waiting in the queue.
func TestALinkCountsWhatItCarried(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		link := fractaltest.InMemoryLink(t, fractaltest.LinkConfig{
			Above:  fractaltest.NewRecorder(true),
			Below:  fractaltest.NewRecorder(false),
			Parent: brnoPath,
			Child:  printerID,
		})
		t.Cleanup(link.Close)
		payload, err := json.Marshal(fractal.RPC{
			Version:   fractal.Version,
			ID:        "a",
			Sender:    hqPath,
			Receiver:  printerPath,
			Interface: printerInterface,
			Instance:  printerInstance,
			Method:    setNozzleTemp,
			Expires:   tInvoked,
		})
		if err != nil {
			t.Fatalf("encode the request: %v", err)
		}
		if sendErr := link.ParentSide().SendDurable(t.Context(), node.Dispatch{
			Kind: fractal.KindRPC, Payload: payload, Child: printerID, ID: "a",
		}); sendErr != nil {
			t.Fatalf("send the request: %v", sendErr)
		}
		synctest.Wait()

		running, ok := link.(interface {
			Downward() fractaltest.Stats
			Waiting() (int, int)
		})
		if !ok {
			t.Fatal("the link from InMemoryLink has no Downward and Waiting methods")
		}
		stats := running.Downward()
		if stats.Sent != 1 || stats.Delivered != 0 {
			t.Errorf("before Connect the link counts %d sent and %d delivered, want 1 and 0",
				stats.Sent, stats.Delivered)
		}
		if _, waiting := running.Waiting(); waiting != 1 {
			t.Errorf("%d messages wait in the downward queue, want 1", waiting)
		}
	})
}

// TestTheRecorderKeepsWhatItTook checks that a Recorder records a message and
// the four link signals, and that Reset removes them.
func TestTheRecorderKeepsWhatItTook(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := fractaltest.NewRecorder(true)
		r.Deliver(t.Context(), node.Inbound{Kind: fractal.KindREP, Payload: []byte("{}"), From: printerID})
		r.ChildConnected(t.Context(), printerID)
		r.ChildLost(t.Context(), printerID)
		r.ParentConnected(t.Context())
		r.ParentLost(t.Context())
		if got := r.Count(); got != 1 {
			t.Errorf("Count returned %d, want 1", got)
		}
		if got := r.Kinds(); len(got) != 1 || got[0] != fractal.KindREP {
			t.Errorf("Kinds returned %v, want one REP", got)
		}
		signals := [4]int{len(r.Connected()), len(r.Lost()), r.ParentConnections(), r.ParentLosses()}
		if signals != [4]int{1, 1, 1, 1} {
			t.Errorf("the recorder counted %v ChildConnected, ChildLost, ParentConnected "+
				"and ParentLost calls, want one of each", signals)
		}
		r.Reset()
		signals = [4]int{len(r.Connected()), len(r.Lost()), r.ParentConnections(), r.ParentLosses()}
		if r.Count() != 0 || signals != [4]int{} {
			t.Error("the recorder still has a message or a link signal after Reset")
		}
	})
}

// TestTheStateOwnerAnswersAsItWasSetUp checks Invoke, Set, Get, Desire, Refuse
// and Snapshot of a StateOwner.
func TestTheStateOwnerAnswersAsItWasSetUp(t *testing.T) {
	t.Parallel()
	owner := fractaltest.NewStateOwner(nil)
	ctx := t.Context()
	_, err := owner.Invoke(ctx, node.Call{Method: setNozzleTemp})
	var carried *fractal.Error
	if !errors.Is(err, fractaltest.ErrNoSuchMethod) || !errors.As(err, &carried) ||
		carried.Code != fractal.CodeObjectNotFound {
		t.Errorf("Invoke of a method that was not registered returned %v, want a fractal.Error with %s",
			err, fractal.CodeObjectNotFound)
	}
	owner.Set(nozzleTemp, json.RawMessage(`25.0`))
	if got, ok := owner.Get(nozzleTemp); !ok || string(got) != "25.0" {
		t.Errorf("Get returned %s for nozzleTemp, want 25.0", got)
	}
	changed, err := owner.Desire(ctx, fractal.Props{nozzleTempTarget: json.RawMessage(`220.0`)})
	if err != nil || len(changed) != 1 {
		t.Errorf("Desire returned the error %v and %d changed properties, want nil and 1", err, len(changed))
	}
	owner.Refuse(func(fractal.Props) error { return errRefused })
	if _, desireErr := owner.Desire(ctx, fractal.Props{nozzleTempTarget: json.RawMessage(`1.0`)}); desireErr == nil {
		t.Error("Desire returned no error, and the function set with Refuse rejects every change")
	}
	snap, _, err := owner.Snapshot(ctx)
	if err != nil || len(snap) != 2 {
		t.Errorf("Snapshot returned the error %v and %d properties, want nil and 2", err, len(snap))
	}
}

// TestTheTraceFollowsOneStepAtATime checks that a Trace records messages in
// both directions and that Reset removes them.
func TestTheTraceFollowsOneStepAtATime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newNetwork(t)
		trace := n.hqLink.Watch()
		n.startAll(t)
		synctest.Wait()
		if len(trace.Down()) == 0 || len(trace.Up()) == 0 {
			t.Fatalf("the trace recorded %d messages down and %d up, want at least 1 each",
				len(trace.Down()), len(trace.Up()))
		}
		trace.Reset()
		if len(trace.Down()) != 0 || len(trace.Up()) != 0 {
			t.Error("the trace still has messages after Reset")
		}
	})
}

// errRefused is the error the Refuse function of a test returns to reject a
// property change.
var errRefused = errors.New("the device refused")
