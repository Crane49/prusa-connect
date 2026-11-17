package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// instanceReader is the one method of the host a reader of running instances
// needs. It is an interface so a test can stand a plane in without starting
// one; the links surface and the bearer path both read through it.
type instanceReader interface {
	Supervising(ref.Kind) map[ref.Ref]*supervisor.Instance[any]
}

// linkResources is the configured inventory as the links surface reads it: the
// resource store narrowed to the two reads, because the links surface writes no
// declaration. Authorization is already resolved inside both calls, so a link
// the caller holds no grant on is absent from the listing and unknown to the
// lookup.
type linkResources interface {
	List(ctx context.Context, principal string, kind ref.Kind) (httpapi.ResourceListing, error)
	Get(ctx context.Context, principal string, instance ref.Ref) (httpapi.ResourceInstance, error)
}

// linkAccess is the authorization graph as the approve operation reads it.
// Approving a node changes what the link admits, so it takes the level between
// reading an instance and rewriting it, which is the split every other resource
// surface draws.
type linkAccess interface {
	RequireResource(ctx context.Context, who authz.Subject, action authz.Action, instance ref.Ref) error
}

// linkStore answers the links API from the two things that know: the configured
// inventory, which says what was declared and who may see it, and the
// supervision plane, which holds the running link and everything only it can
// measure.
type linkStore struct {
	resources linkResources
	host      instanceReader
	graph     linkAccess

	// events binds one link instance to this server's event stream. The
	// decision about a server is published here, on the route the person
	// called, because the person is known here and not in the link. A module
	// cannot publish these types, so nothing below can forge a person.
	events func(ref.Ref) bus.Emitter

	// publish makes the events of one origin go out one at a time, which the
	// event hub requires. Two people deciding about two servers under one link
	// at the same moment reach this from two request goroutines.
	publish *sync.Mutex
}

var _ httpapi.LinkStore = linkStore{}

// newLinkStore binds the links surface to the inventory, the plane, the graph
// and this server's event stream.
func newLinkStore(
	resources linkResources, host instanceReader, graph linkAccess, events func(ref.Ref) bus.Emitter,
) linkStore {
	return linkStore{
		resources: resources,
		host:      host,
		graph:     graph,
		events:    events,
		publish:   &sync.Mutex{},
	}
}

// ListLinks returns every link the caller may see, in name order.
func (s linkStore) ListLinks(ctx context.Context, principal string) (httpapi.LinkListing, error) {
	listing, err := s.resources.List(ctx, principal, ref.KindLink)
	if err != nil {
		return httpapi.LinkListing{}, err
	}

	running := s.host.Supervising(ref.KindLink)

	items := make([]httpapi.LinkInstance, 0, len(listing.Items))
	for _, declared := range listing.Items {
		items = append(items, projectLink(ctx, declared, running))
	}

	slices.SortFunc(items, func(a, b httpapi.LinkInstance) int {
		return strings.Compare(a.Name, b.Name)
	})

	return httpapi.LinkListing{Items: items}, nil
}

// GetLink returns one link. A caller holding no grant on it is told what a
// caller naming a link that is not declared is told, because the inventory
// takes that decision before it reads the configuration.
func (s linkStore) GetLink(ctx context.Context, principal, name string) (httpapi.LinkInstance, error) {
	instance, err := linkRef(name)
	if err != nil {
		return httpapi.LinkInstance{}, err
	}

	declared, err := s.resources.Get(ctx, principal, instance)
	if err != nil {
		return httpapi.LinkInstance{}, err
	}

	return projectLink(ctx, declared, s.host.Supervising(ref.KindLink)), nil
}

// linkRef names one link. A name the naming rule refuses is answered the way a
// name no link carries is, because neither can ever identify an instance.
func linkRef(name string) (ref.Ref, error) {
	instance, err := ref.New(ref.KindLink, name)
	if err != nil {
		return ref.Ref{}, httpfault.ResourceNotFound(httpfault.Reference(ref.KindLink.String(), name))
	}

	return instance, nil
}

// ApproveLinkNode admits a node that waits for a decision under this link.
func (s linkStore) ApproveLinkNode(
	ctx context.Context, principal, name, node string,
) (httpapi.LinkInstance, error) {
	return s.decide(ctx, principal, name, node, "approve", event.TypeLinkNodeApproved,
		func(callCtx context.Context, link readableLink) error {
			return link.Approve(callCtx, linkapi.NodeID(node))
		})
}

// RejectLinkNode removes a node that waits for a decision under this link. It
// publishes no event: the link's own reading is what says which nodes wait, and
// this server has no event type for a name that was never admitted.
func (s linkStore) RejectLinkNode(
	ctx context.Context, principal, name, node string,
) (httpapi.LinkInstance, error) {
	return s.decide(ctx, principal, name, node, "reject", "",
		func(callCtx context.Context, link readableLink) error {
			return link.Reject(callCtx, linkapi.NodeID(node))
		})
}

// RevokeLinkNode removes the admission of a node under this link.
func (s linkStore) RevokeLinkNode(
	ctx context.Context, principal, name, node string,
) (httpapi.LinkInstance, error) {
	return s.decide(ctx, principal, name, node, "revoke", event.TypeLinkNodeRevoked,
		func(callCtx context.Context, link readableLink) error {
			return link.Revoke(callCtx, linkapi.NodeID(node))
		})
}

// decide runs an approve or a revoke.
//
// It first checks that the caller may see the link, so a caller with no grant
// on the link gets the same error as for a link that does not exist. It then
// checks the action operate_resource, and then looks for the running link. A
// link that is not running has no nodes, so the error is the same as for a
// node that does not exist.
//
// It calls the link through supervisor.CallVoid and not supervisor.Peek,
// because a link module answers over a socket. The call counts against the
// concurrent calls of the instance and has the call timeout of the instance,
// so a link that never answers fails the request and does not block it.
func (s linkStore) decide(
	ctx context.Context,
	principal, name, node, operation, decided string,
	act func(context.Context, readableLink) error,
) (httpapi.LinkInstance, error) {
	instance, err := linkRef(name)
	if err != nil {
		return httpapi.LinkInstance{}, err
	}

	declared, err := s.resources.Get(ctx, principal, instance)
	if err != nil {
		return httpapi.LinkInstance{}, err
	}

	who, err := authz.ParseSubject(principal)
	if err != nil {
		return httpapi.LinkInstance{}, httpfault.Unexpected("read the caller's principal")
	}

	if err = s.graph.RequireResource(ctx, who, authz.ActionOperateResource, instance); err != nil {
		return httpapi.LinkInstance{}, err
	}

	running := s.host.Supervising(ref.KindLink)

	runner, supervised := running[instance]
	if !supervised {
		return httpapi.LinkInstance{}, unknownNode(node)
	}

	refusal := supervisor.CallVoid(ctx, runner, operation, func(callCtx context.Context, target any) error {
		link, ok := readableLinkOf(target)
		if !ok {
			return linkapi.ErrNoNode
		}

		return act(callCtx, link)
	})

	switch {
	case errors.Is(refusal, linkapi.ErrNoNode):
		return httpapi.LinkInstance{}, unknownNode(node)
	case refusal != nil:
		return httpapi.LinkInstance{}, linkFailed(instance, refusal)
	}

	s.record(ctx, instance, decided, principal, node)

	return projectLink(ctx, declared, running), nil
}

// record publishes the decision a person made about one server.
//
// It runs after the link confirmed the decision, so the record never names a
// decision the link refused. It never fails the request: the decision has
// already been made, and telling the person it failed would have them make it
// again.
func (s linkStore) record(ctx context.Context, instance ref.Ref, decided, principal, node string) {
	if decided == "" || s.events == nil {
		return
	}

	emitter := s.events(instance)
	if emitter == nil {
		return
	}

	authType, authID := principalParts(principal)

	body, err := json.Marshal(event.LinkNodeDecided{
		Link:     instance.String(),
		Node:     node,
		AuthType: authType,
		AuthID:   authID,
	})
	if err != nil {
		return
	}

	s.publish.Lock()
	defer s.publish.Unlock()

	if err = emitter.Emit(bus.Emission{
		Type: decided,
		Key:  instance.String() + "/" + node,
		Body: string(body),
	}); err != nil {
		slog.Default().WarnContext(ctx, "a decision about a server was not published",
			"resource", instance.String(), "node", node, "error", err.Error())
	}
}

// unknownNode returns the not-found error for a node id. decide uses it for a
// node the link does not know and for any node of a link that is not running.
func unknownNode(node string) error {
	return httpfault.ResourceNotFound(httpfault.Reference("node", node))
}

// linkFailed converts the error of a failed link call to an API error.
//
// An error this server raised is already a registered fault, and that fault is
// the API error.
//
// A link module sends its own registered code beside its English sentence. The
// answer then carries that code as the reason and the module's sentence as the
// detail, so a page keys its own translation by the code and never by the
// sentence. The module's code is its own, in its own namespace, so it is a
// value of the answer and not the code of the answer.
//
// Every other error becomes LINK_NOT_ANSWERING with a reason in plain words.
func linkFailed(instance ref.Ref, err error) error {
	if known, ok := errors.AsType[*fault.Fault](err); ok {
		return known
	}

	if payload, carried := linkapi.FaultOf(err); carried {
		detail := payload.Detail
		if detail == "" {
			detail = payload.Title
		}

		return httpfault.LinkNotAnswering(instance.String(), payload.Code.String(), detail)
	}

	reason := "the link did not answer"

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		reason = "the link took longer than the call budget"
	case errors.Is(err, linkapi.ErrInvalidReading):
		reason = "the link answered a reading this server will not read"
	case errors.Is(err, linkapi.ErrNotReady):
		reason = "the link is not serving yet"
	case errors.Is(err, linkapi.ErrBusy):
		reason = "the link is already answering as many calls as it allows"
	}

	return httpfault.LinkNotAnswering(instance.String(), reason, reason)
}

// projectLink builds the API body of one link from its declaration and, when
// the link runs, from its status. A link that is disabled, that is
// restarting, or whose target answers no call gets the fields of the
// declaration alone. The status fields are then absent from the JSON and not
// zero.
//
// When the status call fails, the link is reported as running with no nodes
// and no listeners, and the call returns no error. A listing reads every link
// the caller may see, and one link that does not answer must not fail the
// whole listing.
func projectLink(
	ctx context.Context,
	declared httpapi.ResourceInstance,
	running map[ref.Ref]*supervisor.Instance[any],
) httpapi.LinkInstance {
	out := httpapi.LinkInstance{
		Name:      declared.Name,
		Kind:      ref.KindLink.String(),
		Type:      declared.Type,
		Enabled:   declared.Enabled,
		Nodes:     []httpapi.LinkNode{},
		Listeners: []httpapi.LinkListener{},
	}

	instance, err := ref.New(ref.KindLink, declared.Name)
	if err != nil {
		return out
	}

	runner, supervised := running[instance]
	if !supervised {
		return out
	}

	reading, err := supervisor.Call(ctx, runner, "status",
		func(callCtx context.Context, target any) (linkapi.Status, error) {
			link, ok := readableLinkOf(target)
			if !ok {
				return linkapi.Status{}, errNotAReadableLink
			}

			return link.Status(callCtx)
		})
	if err != nil {
		out.Running = !errors.Is(err, errNotAReadableLink)

		return out
	}

	out.Running = true

	return fillLink(out, reading)
}

// errNotAReadableLink reports a running link whose target has no Call method,
// so the links API cannot read its state.
var errNotAReadableLink = errors.New("app: the link instance reports nothing")

// fillLink copies the status of a link into the body the API serves.
//
// The web app shows Mode and Enrolment. Mode is `parent` when this site
// accepts sites below it and `child` when it does not. Accepting, Uplink and
// Listeners have the same facts in full.
func fillLink(out httpapi.LinkInstance, reading linkapi.Status) httpapi.LinkInstance {
	out.Accepting = reading.Accepting
	out.Enrolment = reading.Admits.String()
	out.NodeID = reading.NodeID.String()
	out.NodePath = reading.NodePath.String()
	out.PublicKey = reading.PublicKey

	out.Mode = "child"
	if reading.Accepting {
		out.Mode = "parent"
	}

	if reading.Uplink != nil {
		out.Address = reading.Uplink.Address
		out.Uplink = &httpapi.LinkUplink{
			Address:     reading.Uplink.Address,
			State:       reading.Uplink.State.String(),
			Reason:      reading.Uplink.Reason,
			Code:        reading.Uplink.Code,
			FaultCode:   reading.Uplink.FaultCode,
			ConnectedAt: reading.Uplink.ConnectedAt,
		}
	}

	for _, listener := range reading.Listeners {
		out.Listeners = append(out.Listeners, httpapi.LinkListener{
			Protocol: listener.Protocol,
			Address:  listener.Address,
			Port:     listener.Port,
		})
	}

	if out.Address == "" && len(out.Listeners) > 0 {
		out.Address = out.Listeners[0].Address
	}

	for _, node := range reading.Nodes {
		out.Nodes = append(out.Nodes, projectNode(node))
	}

	out.Counters = projectCounters(reading.Counters)

	return out
}

// projectCounters converts what a link has handled into the API body. It
// returns nil for a link that counts nothing, so the member is absent from the
// JSON rather than an empty object a reader would draw a table from.
func projectCounters(counters *linkapi.Counters) *httpapi.LinkCounters {
	if counters == nil || (len(counters.Messages) == 0 && len(counters.Drops) == 0) {
		return nil
	}

	out := &httpapi.LinkCounters{
		Messages: make([]httpapi.LinkMessageCount, 0, len(counters.Messages)),
		Drops:    make([]httpapi.LinkDropCount, 0, len(counters.Drops)),
	}

	for _, row := range counters.Messages {
		out.Messages = append(out.Messages, httpapi.LinkMessageCount{
			Kind:    row.Kind,
			Outcome: row.Outcome,
			Count:   row.Count,
		})
	}

	for _, row := range counters.Drops {
		out.Drops = append(out.Drops, httpapi.LinkDropCount{
			Kind:   row.Kind,
			Reason: row.Reason,
			Count:  row.Count,
		})
	}

	return out
}

// projectNode converts one site below this one to the API body. LatencyMS is
// in milliseconds. It is absent, and not zero, for a link protocol that does
// not measure the round trip.
func projectNode(node linkapi.Node) httpapi.LinkNode {
	return httpapi.LinkNode{
		ID:            node.ID.String(),
		Role:          "child",
		Path:          node.Path.String(),
		Admission:     node.Admission.String(),
		PublicKey:     node.PublicKey,
		Address:       node.Address,
		Alive:         node.Connected,
		LastHeartbeat: node.LastSeen,
		LatencyMS:     node.RoundTripMS,
	}
}
