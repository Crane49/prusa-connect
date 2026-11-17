package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// recordingEmitter is the hub's half, keeping what the link published.
type recordingEmitter struct {
	mu        sync.Mutex
	emissions []bus.Emission
}

func (e *recordingEmitter) Emit(emission bus.Emission) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.emissions = append(e.emissions, emission)

	return nil
}

// fakePlane is the host as the feed reads it.
type fakePlane struct {
	mu      sync.Mutex
	running map[ref.Ref]*supervisor.Instance[any]
}

func (p *fakePlane) Supervising(kind ref.Kind) map[ref.Ref]*supervisor.Instance[any] {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := map[ref.Ref]*supervisor.Instance[any]{}

	for instance, runner := range p.running {
		if instance.Kind() == kind {
			out[instance] = runner
		}
	}

	return out
}

// fakeInventory is the configured inventory, already narrowed to what the
// caller may see: the real store resolves the permission in the query, so a
// test stands in the answer rather than the graph.
type fakeInventory struct {
	items []httpapi.ResourceInstance
}

func (f fakeInventory) List(
	_ context.Context, _ string, kind ref.Kind,
) (httpapi.ResourceListing, error) {
	listing := httpapi.ResourceListing{}

	for _, item := range f.items {
		if kind == "" || item.Kind == kind.String() {
			listing.Items = append(listing.Items, item)
		}
	}

	return listing, nil
}

func (f fakeInventory) Get(
	_ context.Context, _ string, instance ref.Ref,
) (httpapi.ResourceInstance, error) {
	for _, item := range f.items {
		if item.Name == instance.ID() {
			return item, nil
		}
	}

	return httpapi.ResourceInstance{}, httpfault.ResourceNotFound(instance.String())
}

// fakeGraph answers one action for one caller, so the permit and the refusal
// are both testable.
type fakeGraph struct {
	allow map[string]bool
}

func (g fakeGraph) RequireResource(
	_ context.Context, who authz.Subject, action authz.Action, _ ref.Ref,
) error {
	if g.allow[who.String()+"/"+action.String()] {
		return nil
	}

	return errors.New("the caller holds no grant reaching this action")
}

// declaredLink is the link module as the inventory holds it.
func declaredLink(enabled bool) httpapi.ResourceInstance {
	return httpapi.ResourceInstance{
		ResourceSpec: httpapi.ResourceSpec{Type: "com.example.fractal/fractal", Enabled: enabled},
		Kind:         ref.KindLink.String(),
		Name:         "fractal",
	}
}

// storeOverModuleEmitting stands the links surface up over one module, one
// graph and one event stream, so a case can read what a decision published.
func storeOverModuleEmitting(t *testing.T, target any, hub bus.Emitter) linkStore {
	t.Helper()

	instance, err := ref.New(ref.KindLink, "fractal")
	require.NoError(t, err)

	plane := &fakePlane{running: map[ref.Ref]*supervisor.Instance[any]{
		instance: runningModuleLink(t, instance, target, 0),
	}}

	return newLinkStore(
		fakeInventory{items: []httpapi.ResourceInstance{declaredLink(true)}},
		plane,
		operatorGraph(),
		func(ref.Ref) bus.Emitter { return hub })
}

// TestTheLinkListingCarriesWhatOnlyTheRunningLinkKnows: the declaration says
// the name, the type and the switch, and everything else comes from the
// instance the plane is supervising.
func TestTheLinkListingCarriesWhatOnlyTheRunningLinkKnows(t *testing.T) {
	t.Parallel()

	module := &moduleLink{status: moduleReading()}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), fakeGraph{}, 0)

	listing, err := store.ListLinks(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Len(t, listing.Items, 1)

	link := listing.Items[0]
	require.Equal(t, "fractal", link.Name)
	require.Equal(t, "link", link.Kind)
	require.True(t, link.Enabled)
	require.True(t, link.Running)
	require.Equal(t, "parent", link.Mode)
	require.Equal(t, "explicit", link.Enrolment)
	require.Equal(t, "b856eab039d11de1", link.NodeID)

	require.Len(t, link.Nodes, 1)
	require.Equal(t, "child", link.Nodes[0].Role)
	require.Equal(t, "pending", link.Nodes[0].Admission)
	require.True(t, link.Nodes[0].Alive)
	require.NotNil(t, link.Nodes[0].LastHeartbeat)
	require.NotNil(t, link.Nodes[0].LatencyMS)
	require.InDelta(t, 0.84, *link.Nodes[0].LatencyMS, 0.001)
}

// TestALinkThatIsNotRunningCarriesTheDeclarationAlone: a value that cannot
// exist yet is absent rather than zero, because zero is an answer.
func TestALinkThatIsNotRunningCarriesTheDeclarationAlone(t *testing.T) {
	t.Parallel()

	store := newLinkStore(
		fakeInventory{items: []httpapi.ResourceInstance{declaredLink(false)}},
		&fakePlane{},
		fakeGraph{},
		nil,
	)

	link, err := store.GetLink(t.Context(), "user:operator", "fractal")
	require.NoError(t, err)

	require.False(t, link.Enabled)
	require.False(t, link.Running)
	require.Empty(t, link.Mode)
	require.Empty(t, link.Enrolment)
	require.Empty(t, link.NodeID)
	require.Empty(t, link.PublicKey)
	require.Empty(t, link.Address)
	require.Empty(t, link.Nodes)
}

// TestAMeasurementNobodyTookIsAbsent is the other half of the rule above, on a
// running link whose node has never answered.
func TestAMeasurementNobodyTookIsAbsent(t *testing.T) {
	t.Parallel()

	reading := moduleReading()
	reading.Nodes[0].Connected = false
	reading.Nodes[0].LastSeen = nil
	reading.Nodes[0].RoundTripMS = nil

	module := &moduleLink{status: reading}
	store := storeOverModule(t, linkSession(t, linkapi.Handlers(module)), fakeGraph{}, 0)

	link, err := store.GetLink(t.Context(), "user:operator", "fractal")
	require.NoError(t, err)

	require.Len(t, link.Nodes, 1)
	require.False(t, link.Nodes[0].Alive)
	require.Nil(t, link.Nodes[0].LastHeartbeat)
	require.Nil(t, link.Nodes[0].LatencyMS)
}

// TestANameNoLinkCarriesIsNotFound holds the lookup's refusal, which is also
// what a caller holding no grant is told.
func TestANameNoLinkCarriesIsNotFound(t *testing.T) {
	t.Parallel()

	store := newLinkStore(fakeInventory{}, &fakePlane{}, fakeGraph{}, nil)

	_, err := store.GetLink(t.Context(), "user:operator", "nowhere")
	require.Error(t, err)

	_, err = store.GetLink(t.Context(), "user:operator", "a name the rule refuses/")
	require.Error(t, err)
}

// TestADecisionAboutAServerIsPublishedWithThePerson: the record of who
// approved which server is written on the route the person called, because the
// person is known there and not in the link.
func TestADecisionAboutAServerIsPublishedWithThePerson(t *testing.T) {
	t.Parallel()

	hub := &recordingEmitter{}
	module := &moduleLink{status: moduleReading()}
	store := storeOverModuleEmitting(t, linkSession(t, linkapi.Handlers(module)), hub)

	_, err := store.ApproveLinkNode(t.Context(), "user:owner", "fractal", "26c70f1f054e7fdb")
	require.NoError(t, err)

	require.Len(t, hub.emissions, 1)
	require.Equal(t, event.TypeLinkNodeApproved, hub.emissions[0].Type)
	require.Equal(t, "link:fractal/26c70f1f054e7fdb", hub.emissions[0].Key)

	var body event.LinkNodeDecided
	require.NoError(t, json.Unmarshal([]byte(hub.emissions[0].Body), &body))
	require.Equal(t, "link:fractal", body.Link)
	require.Equal(t, "26c70f1f054e7fdb", body.Node)
	require.Equal(t, "user", body.AuthType)
	require.Equal(t, "owner", body.AuthID)
}

// TestADecisionTheLinkRefusedIsNotRecorded: the record is written after the
// link confirmed the decision, so it never names a decision that did not
// happen.
func TestADecisionTheLinkRefusedIsNotRecorded(t *testing.T) {
	t.Parallel()

	hub := &recordingEmitter{}
	module := &moduleLink{status: moduleReading(), err: linkapi.ErrNoNode}
	store := storeOverModuleEmitting(t, linkSession(t, linkapi.Handlers(module)), hub)

	_, err := store.ApproveLinkNode(t.Context(), "user:owner", "fractal", "0000000000000000")
	require.Error(t, err)
	require.Empty(t, hub.emissions)
}

// TestADecisionAboutAServerIsStoredAsAFact: the history store keeps a durable
// frame whatever kind its origin is, so the record of who approved which server
// reaches a deployment that keeps its past. The sink half of the same question
// is TestOnlyADurablePrinterFrameBecomesAFact.
func TestADecisionAboutAServerIsStoredAsAFact(t *testing.T) {
	t.Parallel()

	site, err := event.NewOrigin(event.Kind(ref.KindLink), "fractal")
	require.NoError(t, err)

	frame := stream.Frame{
		Position: event.Position{Origin: site},
		Class:    event.ClassState,
		Type:     event.TypeLinkNodeApproved,
		Bytes: []byte(`{"time":"2026-09-21T10:00:00Z","sequence":"00000000000000000001",` +
			`"data":{"link":"link:fractal","node":"26c70f1f054e7fdb","auth_type":"user","auth_id":"owner"}}`),
	}

	stored, ok := factOf(frame)
	require.True(t, ok, "a decision about a server produced no fact")
	require.Equal(t, event.TypeLinkNodeApproved, stored.Type)
	require.Equal(t, site, stored.Ref)
	require.Equal(t, event.ClassState, stored.Class)
	require.True(t, stored.Class.Durable(), "the class is not one the history store keeps")
	require.JSONEq(t,
		`{"link":"link:fractal","node":"26c70f1f054e7fdb","auth_type":"user","auth_id":"owner"}`,
		string(stored.Payload))
}
