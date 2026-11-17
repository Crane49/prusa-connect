package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A link that runs in a process of its own is reached the way every module is
// reached: the supervised target makes one control call over the session the
// host opened, and the published contract says what the host asks. These tests
// stand a real session up over operating system pipes, so the encoding, the
// method names and the refusals are the ones a module would meet.

// moduleLink is the module side of the contract, answering what the case wants
// and recording what it was asked.
type moduleLink struct {
	mu       sync.Mutex
	status   linkapi.Status
	err      error
	hold     time.Duration
	approved []string
	rejected []string
	revoked  []string
}

func (l *moduleLink) Status(context.Context) (linkapi.Status, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.status, l.err
}

func (l *moduleLink) Approve(ctx context.Context, params linkapi.NodeParams) error {
	if err := l.wait(ctx); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.err != nil {
		return l.err
	}

	l.approved = append(l.approved, params.ID.String())

	for i := range l.status.Nodes {
		if l.status.Nodes[i].ID == params.ID {
			l.status.Nodes[i].Admission = linkapi.AdmissionAdmitted
		}
	}

	return nil
}

func (l *moduleLink) Reject(ctx context.Context, params linkapi.NodeParams) error {
	if err := l.wait(ctx); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.err != nil {
		return l.err
	}

	l.rejected = append(l.rejected, params.ID.String())

	l.status.Nodes = slices.DeleteFunc(l.status.Nodes, func(n linkapi.Node) bool {
		return n.ID == params.ID
	})

	return nil
}

func (l *moduleLink) Revoke(ctx context.Context, params linkapi.NodeParams) error {
	if err := l.wait(ctx); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.err != nil {
		return l.err
	}

	l.revoked = append(l.revoked, params.ID.String())

	for i := range l.status.Nodes {
		if l.status.Nodes[i].ID == params.ID {
			l.status.Nodes[i].Admission = linkapi.AdmissionRevoked
		}
	}

	return nil
}

// wait is how a case makes the module slow. A module that never answers is what
// the call budget exists for.
func (l *moduleLink) wait(ctx context.Context) error {
	if l.hold == 0 {
		return nil
	}

	timer := time.NewTimer(l.hold)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		//nolint:wrapcheck // the module reports the caller's own cancellation.
		return ctx.Err()
	}
}

func (l *moduleLink) decisions() ([]string, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.approved...), append([]string(nil), l.revoked...)
}

// pipeEnd is one side of a duplex channel built from operating system pipes,
// which is the channel a host really hands a child.
type pipeEnd struct {
	reader *os.File
	writer *os.File
}

func (p pipeEnd) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p pipeEnd) Write(b []byte) (int, error) { return p.writer.Write(b) }
func (p pipeEnd) Close() error                { return errors.Join(p.reader.Close(), p.writer.Close()) }

// moduleTarget is what the resource host builds for an instance running out of
// process: one control call over the session. It is the shape the links surface
// reads a module through.
type moduleTarget struct {
	conn *wirerpc.Conn
}

func (t moduleTarget) Call(
	ctx context.Context, method string, params any,
) (json.RawMessage, error) {
	//nolint:wrapcheck // the test target reports the session's own refusal.
	return t.conn.Call(ctx, wirerpc.Method(method), params)
}

// linkSession serves one module method set over a pipe pair and returns the
// host's side as a supervised target.
func linkSession(t *testing.T, methods wirerpc.Methods) moduleTarget {
	t.Helper()

	hostReads, childWrites, err := os.Pipe()
	require.NoError(t, err)

	childReads, hostWrites, err := os.Pipe()
	require.NoError(t, err)

	host, err := wirerpc.NewConn(pipeEnd{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost})
	require.NoError(t, err)

	child, err := wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild, Methods: methods})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Add(2)

	go func() {
		defer running.Done()

		_ = host.Serve(ctx)
	}()

	go func() {
		defer running.Done()

		_ = child.Serve(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return moduleTarget{conn: host}
}

// runningModuleLink is one supervised instance at ready with target behind it,
// outside a bubble, because the session under it does real work on real pipes.
func runningModuleLink(
	t *testing.T, instance ref.Ref, target any, budget time.Duration,
) *supervisor.Instance[any] {
	t.Helper()

	policy := supervisor.DefaultPolicy()
	policy.Backoff.Spread = 0

	if budget > 0 {
		policy.CallTimeout = budget
	}

	running, err := supervisor.New(supervisor.Config[any]{
		Ref:    instance,
		Type:   "com.example.fractal/fractal",
		Policy: policy,
		Start:  func(context.Context, supervisor.Handle) (any, error) { return target, nil },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = running.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return running.State().String() == "ready" },
		time.Second, time.Millisecond)

	return running
}

// moduleReading is what the fake module reports: this site joined the site
// above it, accepts others under it, and holds one waiting for a decision.
func moduleReading() linkapi.Status {
	seen := time.Date(2026, time.September, 11, 9, 41, 12, 0, time.UTC)
	trip := 0.84

	return linkapi.Status{
		NodeID:    "b856eab039d11de1",
		NodePath:  "root.workshop.b856eab039d11de1",
		PublicKey: "c958b4575cc0f5c14f819d64b35c19707f6c56a2ef69cdc2552615b49e858234",
		Accepting: true,
		Admits:    linkapi.AdmitsExplicit,
		Uplink: &linkapi.Uplink{
			Address:     "nats://above.workshop.example:4222",
			State:       linkapi.UplinkConnected,
			ConnectedAt: &seen,
		},
		Nodes: []linkapi.Node{{
			ID:          "26c70f1f054e7fdb",
			Path:        "root.workshop.b856eab039d11de1.26c70f1f054e7fdb",
			Admission:   linkapi.AdmissionPending,
			Address:     "172.22.0.3:35394",
			Connected:   true,
			LastSeen:    &seen,
			RoundTripMS: &trip,
		}},
		Listeners: []linkapi.Listener{{Protocol: "nats", Address: "0.0.0.0", Port: 4222}},
	}
}

// storeOverModule stands the links surface up over one module and one graph.
func storeOverModule(
	t *testing.T, target any, graph fakeGraph, budget time.Duration,
) linkStore {
	t.Helper()

	instance, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	plane := &fakePlane{running: map[ref.Ref]*supervisor.Instance[any]{
		instance: runningModuleLink(t, instance, target, budget),
	}}

	return newLinkStore(
		fakeInventory{items: []httpapi.ResourceInstance{declaredLink(true)}}, plane, graph, nil)
}

// operatorGraph lets one principal operate and nobody else.
func operatorGraph() fakeGraph {
	return fakeGraph{allow: map[string]bool{
		"user:owner/" + authz.ActionOperateResource.String(): true,
	}}
}

// TestTheLinksSurfaceReadsAModuleOverTheWire is the whole reading crossing a
// real session: what this site is called, the site above it, the listeners it
// opened, and the site under it waiting for a decision.
func TestTheLinksSurfaceReadsAModuleOverTheWire(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading()}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), operatorGraph(), 0)

	link, err := store.GetLink(t.Context(), "user:owner", "fractal")
	require.NoError(t, err)

	require.True(t, link.Running)
	require.True(t, link.Accepting)
	require.Equal(t, "parent", link.Mode)
	require.Equal(t, "explicit", link.Enrolment)
	require.Equal(t, "b856eab039d11de1", link.NodeID)
	require.Equal(t, "root.workshop.b856eab039d11de1", link.NodePath)

	require.NotNil(t, link.Uplink)
	require.Equal(t, "connected", link.Uplink.State)
	require.Equal(t, "nats://above.workshop.example:4222", link.Uplink.Address)
	require.NotNil(t, link.Uplink.ConnectedAt)

	require.Len(t, link.Listeners, 1)
	require.Equal(t, "nats", link.Listeners[0].Protocol)
	require.Equal(t, 4222, link.Listeners[0].Port)

	require.Len(t, link.Nodes, 1)
	require.Equal(t, "26c70f1f054e7fdb", link.Nodes[0].ID)
	require.Equal(t, "pending", link.Nodes[0].Admission)
	require.NotNil(t, link.Nodes[0].LatencyMS)
	require.InDelta(t, 0.84, *link.Nodes[0].LatencyMS, 0.001)
}

// TestADecisionReachesTheModuleAndComesBackInTheReading: approve and revoke
// both cross the session, and the answer is the link as it stands afterwards.
func TestADecisionReachesTheModuleAndComesBackInTheReading(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading()}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), operatorGraph(), 0)

	admitted, err := store.ApproveLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.NoError(t, err)
	require.Equal(t, "admitted", admitted.Nodes[0].Admission)

	refused, err := store.RevokeLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.NoError(t, err)
	require.Equal(t, "revoked", refused.Nodes[0].Admission)

	approvals, revocations := module.decisions()
	require.Equal(t, []string{"26c70f1f054e7fdb"}, approvals)
	require.Equal(t, []string{"26c70f1f054e7fdb"}, revocations)
}

// TestADecisionTakesOperateResourceOnAModuleToo is the rule in both directions
// and for both decisions: the caller who holds the grant reaches the module,
// and the caller who does not never does.
func TestADecisionTakesOperateResourceOnAModuleToo(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading()}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), operatorGraph(), 0)

	decisions := map[string]func(principal string) (httpapi.LinkInstance, error){
		"approve": func(principal string) (httpapi.LinkInstance, error) {
			return store.ApproveLinkNode(t.Context(), principal, "fractal", "26c70f1f054e7fdb")
		},
		"revoke": func(principal string) (httpapi.LinkInstance, error) {
			return store.RevokeLinkNode(t.Context(), principal, "fractal", "26c70f1f054e7fdb")
		},
	}

	for name, decide := range decisions {
		_, err := decide("user:reader")
		require.Error(t, err, "%s: a caller with no operate_resource reached the module", name)

		_, err = decide("user:owner")
		require.NoError(t, err, "%s: a caller holding operate_resource was refused", name)
	}

	approvals, revocations := module.decisions()
	require.Len(t, approvals, 1, "the refused caller reached the module")
	require.Len(t, revocations, 1, "the refused caller reached the module")
}

// TestALinkTheCallerMayNotSeeIsUnknown: the inventory resolves view_resource in
// the query that produces the rows, so a caller holding no grant reads an empty
// listing and the same refusal a name nobody declared gets.
func TestALinkTheCallerMayNotSeeIsUnknown(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	module := &moduleLink{status: moduleReading()}
	plane := &fakePlane{running: map[ref.Ref]*supervisor.Instance[any]{
		instance: runningModuleLink(t, instance, linkSession(t, linkapi.Handlers(module)), 0),
	}}

	blind := newLinkStore(fakeInventory{}, plane, operatorGraph(), nil)

	listing, err := blind.ListLinks(t.Context(), "user:reader")
	require.NoError(t, err)
	require.Empty(t, listing.Items)

	_, err = blind.GetLink(t.Context(), "user:reader", "fractal")
	require.Error(t, err)

	_, err = blind.ApproveLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.Error(t, err)

	approvals, _ := module.decisions()
	require.Empty(t, approvals, "a caller who cannot see the link reached it")
}

// TestANodeTheModuleDoesNotHoldIsNotFound keeps the refusal for an unknown node
// the same across the wire: the module answers the contract's code and the
// surface answers the reference nobody can resolve.
func TestANodeTheModuleDoesNotHoldIsNotFound(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading(), err: linkapi.ErrNoNode}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), operatorGraph(), 0)

	_, err := store.ApproveLinkNode(t.Context(), "user:owner", "fractal", "0000000000000000")
	require.Error(t, err)

	known, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok, "the refusal is not a fault")
	require.Equal(t, httpfault.CodeResourceNotFound(), known.Code())
}

// TestAModuleThatAnswersGarbageIsAFault: a reading this server will not read is
// a registered code with the instance named, rather than a link rendered with
// fields nothing filled.
func TestAModuleThatAnswersGarbageIsAFault(t *testing.T) {
	t.Parallel()

	// A node with an admission outside the closed set. It crosses the module's
	// own door only because this fake installs a handler of its own.
	garbage := wirerpc.Methods{
		linkapi.MethodStatus: func(context.Context, wirerpc.Call) (any, error) {
			return map[string]any{
				"nodes": []map[string]any{{"id": "26c70f1f054e7fdb", "admission": "whatever"}},
			}, nil
		},
		linkapi.MethodApprove: func(context.Context, wirerpc.Call) (any, error) {
			return map[string]any{"not": "a thing this contract answers"}, nil
		},
	}

	store := storeOverModule(t, linkSession(t, garbage), operatorGraph(), 0)

	link, err := store.GetLink(t.Context(), "user:owner", "fractal")
	require.NoError(t, err, "one link that will not answer is not a reason to refuse the read")
	require.True(t, link.Running)
	require.Empty(t, link.Nodes, "a reading this server will not read is not rendered")

	// The same module read through a decision names the instance and the code.
	_, err = store.RevokeLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.Error(t, err)

	known, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok, "the refusal is not a fault")
	require.Equal(t, httpfault.CodeLinkNotAnswering(), known.Code())
}

// TestAModuleThatHangsDoesNotHangTheRequest: the instance's call budget ends
// the wait, and what the caller reads is the supervision plane's own timeout
// code, which already names the instance and the operation.
func TestAModuleThatHangsDoesNotHangTheRequest(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading(), hold: time.Minute}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)),
		operatorGraph(), 50*time.Millisecond)

	started := time.Now()

	_, err := store.ApproveLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.Error(t, err)
	require.Less(t, time.Since(started), 30*time.Second, "the request waited for the module")

	known, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok, "the refusal is not a fault")
	require.Equal(t, supervisor.CodeTimeout(), known.Code())
}
