package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A host talks to this module over a pair of operating system pipes. The tests
// in this file open such a connection and call the module as the host does.

// pipeEnd is one end of that connection: it reads one pipe and writes the
// other.
type pipeEnd struct {
	reader *os.File
	writer *os.File
}

func (p pipeEnd) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p pipeEnd) Write(b []byte) (int, error) { return p.writer.Write(b) }
func (p pipeEnd) Close() error                { return errors.Join(p.reader.Close(), p.writer.Close()) }

// openSession serves the module's methods on a connection over pipes and
// returns the host end of it.
func openSession(t *testing.T, methods wirerpc.Methods) *wirerpc.Conn {
	t.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	host, err := wirerpc.NewConn(pipeEnd{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost})
	if err != nil {
		t.Fatalf("build the host connection: %v", err)
	}

	child, err := wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild, Methods: methods})
	if err != nil {
		t.Fatalf("build the module connection: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Add(2)

	go func() { defer running.Done(); _ = host.Serve(ctx) }()
	go func() { defer running.Done(); _ = child.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return host
}

// decisions is a transport that records every Approve, Reject and Revoke
// call. When refuse is set, all three return it.
type decisions struct {
	mu       sync.Mutex
	approved []fractal.NodeID
	rejected []fractal.NodeID
	revoked  []fractal.NodeID
	refuse   error
	status   natsbind.Status
}

func (d *decisions) Status() natsbind.Status {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.status
}

// heard sets when the transport last heard from the one server below.
func (d *decisions) heard(at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.status = natsbind.Status{
		NodeID: "workshop",
		Nodes:  []natsbind.Below{{ID: "bench", Connected: true, LastHeartbeat: &at}},
	}
}

// TestTheHostReadsTheLatestHeartbeat: the transport hears from a server below
// on every heartbeat, without a change of status. Status reads the transport
// at each call, so the host sees the latest one.
func TestTheHostReadsTheLatestHeartbeat(t *testing.T) {
	t.Parallel()

	state := newLink()
	transport := &decisions{}
	state.serve(transport, newCounters())

	client := linkapi.NewClient(openSession(t, methods(state, &tree{}, make(chan Config, 1))))

	for _, at := range []time.Time{seen(), seen().Add(time.Minute)} {
		transport.heard(at)

		held, err := client.Status(t.Context())
		if err != nil {
			t.Fatalf("read the link: %v", err)
		}

		want := linkapi.Status{
			NodeID: "workshop",
			Nodes: []linkapi.Node{{
				ID: "bench", Admission: linkapi.AdmissionPending, Connected: true, LastSeen: &at,
			}},
		}
		if !reflect.DeepEqual(held, want) {
			t.Errorf("the host read %+v, want %+v", held, want)
		}
	}
}

func (d *decisions) Approve(_ context.Context, id fractal.NodeID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.refuse != nil {
		return d.refuse
	}

	d.approved = append(d.approved, id)

	return nil
}

func (d *decisions) Reject(_ context.Context, id fractal.NodeID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.refuse != nil {
		return d.refuse
	}

	d.rejected = append(d.rejected, id)

	return nil
}

func (d *decisions) Revoke(_ context.Context, id fractal.NodeID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.refuse != nil {
		return d.refuse
	}

	d.revoked = append(d.revoked, id)

	return nil
}

func (d *decisions) taken() ([]fractal.NodeID, []fractal.NodeID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]fractal.NodeID(nil), d.approved...), append([]fractal.NodeID(nil), d.revoked...)
}

// TestTheHostReadsThisModuleOverTheWire: the host calls Status over the
// connection and receives the last status the module stored.
func TestTheHostReadsThisModuleOverTheWire(t *testing.T) {
	t.Parallel()

	state := newLink()
	state.update(linkapi.Status{
		NodeID:    "workshop",
		NodePath:  "hq/workshop",
		PublicKey: "UAJWORKSHOP",
		Accepting: true,
		Admits:    linkapi.AdmitsExplicit,
		Uplink:    &linkapi.Uplink{Address: "nats://hq:4222", State: linkapi.UplinkConnected},
		Nodes: []linkapi.Node{{
			ID:        "bench",
			Path:      "hq/workshop/bench",
			Admission: linkapi.AdmissionPending,
		}},
		Listeners: []linkapi.Listener{{Protocol: "nats", Address: "0.0.0.0", Port: 4222}},
	})

	client := linkapi.NewClient(openSession(t, methods(state, &tree{}, make(chan Config, 1))))

	held, err := client.Status(t.Context())
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}

	if held.NodePath != "hq/workshop" || held.Uplink == nil ||
		held.Uplink.State != linkapi.UplinkConnected {
		t.Fatalf("the Node Path or the uplink state is wrong: %+v", held)
	}

	if len(held.Nodes) != 1 || held.Nodes[0].Admission != linkapi.AdmissionPending {
		t.Fatalf("the servers below are %+v, want one pending server", held.Nodes)
	}

	if len(held.Listeners) != 1 || held.Listeners[0].Port != 4222 {
		t.Fatalf("the listeners are %+v, want one on port 4222", held.Listeners)
	}
}

// TestAModuleThatIsStillStartingAnswersItsReading: when the host calls Status
// before the node has joined, the call returns the empty status at once and
// does not wait for the join.
func TestAModuleThatIsStillStartingAnswersItsReading(t *testing.T) {
	t.Parallel()

	client := linkapi.NewClient(openSession(t, methods(newLink(), &tree{}, make(chan Config, 1))))

	held, err := client.Status(t.Context())
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}

	if held.NodePath != "" || held.Uplink != nil || held.Accepting {
		t.Fatalf("the status of a node that has not joined is not empty: %+v", held)
	}
}

// TestADecisionReachesTheTransport: an Approve and a Revoke call from the host
// reach the transport with the node id the host sent.
func TestADecisionReachesTheTransport(t *testing.T) {
	t.Parallel()

	state := newLink()
	taken := &decisions{}
	state.serve(taken, newCounters())

	client := linkapi.NewClient(openSession(t, methods(state, &tree{}, make(chan Config, 1))))

	if err := client.Approve(t.Context(), linkapi.NodeParams{ID: "bench"}); err != nil {
		t.Fatalf("approve the server bench: %v", err)
	}

	if err := client.Revoke(t.Context(), linkapi.NodeParams{ID: "bench"}); err != nil {
		t.Fatalf("revoke the server bench: %v", err)
	}

	approved, revoked := taken.taken()
	if len(approved) != 1 || approved[0] != "bench" {
		t.Errorf("the transport approved %v, want bench", approved)
	}

	if len(revoked) != 1 || revoked[0] != "bench" {
		t.Errorf("the transport revoked %v, want bench", revoked)
	}
}

// TestADecisionBeforeThisModuleIsServingIsRefusedToRetry: while the transport
// is not built, Approve and Revoke return linkapi.ErrNotReady, which tells the
// host to try again later.
func TestADecisionBeforeThisModuleIsServingIsRefusedToRetry(t *testing.T) {
	t.Parallel()

	client := linkapi.NewClient(openSession(t, methods(newLink(), &tree{}, make(chan Config, 1))))

	for name, decide := range map[string]func() error{
		"approve": func() error {
			return client.Approve(t.Context(), linkapi.NodeParams{ID: "bench"})
		},
		"revoke": func() error {
			return client.Revoke(t.Context(), linkapi.NodeParams{ID: "bench"})
		},
	} {
		if err := decide(); !errors.Is(err, linkapi.ErrNotReady) {
			t.Errorf("%s returned %v, want linkapi.ErrNotReady", name, err)
		}
	}
}

// TestADecisionAboutAServerThisNodeNeverMet: when the transport does not know
// the node id, the host receives linkapi.ErrNoNode.
func TestADecisionAboutAServerThisNodeNeverMet(t *testing.T) {
	t.Parallel()

	state := newLink()
	state.serve(&decisions{refuse: natsbind.ErrUnknownNode}, newCounters())

	client := linkapi.NewClient(openSession(t, methods(state, &tree{}, make(chan Config, 1))))

	if err := client.Approve(t.Context(), linkapi.NodeParams{ID: "bench"}); !errors.Is(err, linkapi.ErrNoNode) {
		t.Fatalf("Approve returned %v, want linkapi.ErrNoNode", err)
	}
}

// TestTheHostIsToldWhatThisModuleIs: plugin.info returns the name and the
// version of module.yaml.
func TestTheHostIsToldWhatThisModuleIs(t *testing.T) {
	t.Parallel()

	answered, err := openSession(t, methods(newLink(), &tree{}, make(chan Config, 1))).
		Call(t.Context(), wirerpc.MethodInfo, struct{}{})
	if err != nil {
		t.Fatalf("call plugin.info: %v", err)
	}

	if !contains(string(answered), name) || !contains(string(answered), string(release)) {
		t.Fatalf("plugin.info returned %s, want the name %s and the version %s", answered, name, release)
	}
}

// TestTheModuleAgreesToStop: plugin.shutdown returns no error, which the host
// reads as "this module agreed to stop".
func TestTheModuleAgreesToStop(t *testing.T) {
	t.Parallel()

	if _, err := openSession(t, methods(newLink(), &tree{}, make(chan Config, 1))).
		Call(t.Context(), wirerpc.MethodShutdown, struct{}{}); err != nil {
		t.Fatalf("call plugin.shutdown: %v", err)
	}
}

// contains reports whether held has the text want.
func contains(held, want string) bool {
	return strings.Contains(held, want)
}
