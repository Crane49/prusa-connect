package resourcehost

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// A module names a principal on a fleet call or a subscription when it is
// serving that person's request. The host acts as that person only if the host
// itself gave the module that principal. Every request reaches a module
// through the host, so the host knows which requests it is forwarding. Each
// routes.serve call is recorded in flights for as long as it is open. A
// principal a module names is accepted while such a call carries it, and
// refused otherwise. A module therefore cannot act as a person it is not
// serving.
//
// A call that names no principal is the module's own work, and it passes here
// unchanged. The fleet and the feed decide who it runs as and what it may
// reach.
//
// A link is the exception. A link relays calls from another server, and that
// server is not a person with an open request on this host, so no open call
// has its principal. A link may name the principal node:<id>, which is another
// server connected through that link. The host accepts it only for the servers
// the link reported with link.changed, and linkNodes records those. The host
// refuses node:<id> from every other kind of instance.

// nodePrefix starts the principal of another server of the network: node:<id>.
const nodePrefix = "node:"

// flights is what the host knows about the principals a module may name: every
// open routes.serve call, counted by the instance it went to and the principal
// it carried, and the servers each link has reported that it answers for.
type flights struct {
	mu   sync.Mutex
	open map[flight]*boarding
	// served is every principal this host has handed each instance since the
	// instance started, in the order it first handed them. A module asks the
	// authorization graph about a person it has served. That person's request
	// may have closed hours before, for example when a queue starts their
	// print at night, so the set of open calls cannot answer that question.
	//
	// It is bounded per instance and it is kept only in memory. A restart of
	// the instance empties it, and a module asking about somebody it has not
	// served since the restart is refused.
	served map[ref.Ref][]string
	// nodes records the servers each link connects. It has its own lock,
	// because a link reports its servers rarely and a module makes a device
	// call often.
	nodes linkNodes
}

// maxServedPrincipals is how many served principals the host keeps for one
// instance. Beyond it the oldest is dropped. A module that has served
// thousands of people can still ask about the ones it served last, and the
// host's memory does not grow with the number of people who ever used it.
const maxServedPrincipals = 512

// flight is one instance serving one principal.
type flight struct {
	instance  ref.Ref
	principal string
}

// boarding counts the calls open on one flight, and closes gone when the last
// of them ends. Closing gone ends a subscription that flight opened.
//
// scoped counts the open calls by the scopes their credential carried, keyed
// by the scope names joined. A call the module makes in the person's name
// while any of them is open runs under the narrowest of those scopes. A
// limited credential therefore cannot use the wider scopes of another session
// of the same person that is open at the same time.
type boarding struct {
	count  int
	gone   chan struct{}
	scoped map[string]int
}

// narrowest is the context a call in this flight's name runs under. Of the
// scopes of the open calls it keeps a set that lies within the sets seen
// before it.
func (b *boarding) narrowest(ctx context.Context) context.Context {
	var narrowest []authz.Scope

	for key := range b.scoped {
		scopes := scopesOfKey(key)
		if scopes == nil {
			continue
		}

		if narrowest == nil || authz.WithinScopes(scopes, narrowest) {
			narrowest = scopes
		}
	}

	return authz.WithScopes(ctx, narrowest)
}

// scopesKey and scopesOfKey write scopes as a map key and read them back. An
// empty key is a call with no scope, which narrows nothing.
func scopesKey(scopes []authz.Scope) string {
	return strings.Join(authz.ScopeNames(scopes), " ")
}

func scopesOfKey(key string) []authz.Scope {
	if key == "" {
		return nil
	}

	scopes, err := authz.ParseScopes(strings.Fields(key))
	if err != nil {
		// scopesKey wrote the key from scopes that were already parsed, so
		// this is unreachable. If it is reached, the narrowest scope applies.
		return []authz.Scope{authz.ScopeRead}
	}

	return scopes
}

// board records one call and returns the function the caller runs when the
// call ends. A call carrying no principal is not recorded.
func (f *flights) board(instance ref.Ref, principal string, scopes []authz.Scope) func() {
	if principal == "" {
		return func() {}
	}

	scopeKey := scopesKey(scopes)

	key := flight{instance: instance, principal: principal}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.open == nil {
		f.open = map[flight]*boarding{}
	}

	held, ok := f.open[key]
	if !ok {
		held = &boarding{gone: make(chan struct{}), scoped: map[string]int{}}
		f.open[key] = held
	}

	held.count++
	held.scoped[scopeKey]++

	f.remember(instance, principal)

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		held.count--
		held.scoped[scopeKey]--

		if held.scoped[scopeKey] == 0 {
			delete(held.scoped, scopeKey)
		}

		if held.count == 0 {
			close(held.gone)
			delete(f.open, key)
		}
	}
}

// remember records that this host handed the principal to the instance. The
// caller holds f.mu.
//
// A principal already remembered keeps its place in the list and is not moved
// to the end.
func (f *flights) remember(instance ref.Ref, principal string) {
	if f.served == nil {
		f.served = map[ref.Ref][]string{}
	}

	held := f.served[instance]
	if slices.Contains(held, principal) {
		return
	}

	if len(held) >= maxServedPrincipals {
		held = held[1:]
	}

	f.served[instance] = append(held, principal)
}

// hasServed reports whether this host has handed the principal to the instance
// since the instance started.
func (f *flights) hasServed(instance ref.Ref, principal string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Contains(f.served[instance], principal)
}

// forget empties the list of people one instance served. It runs when the
// instance starts, so a module installed under a name another module held
// cannot ask about the people that module served.
func (f *flights) forget(instance ref.Ref) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.served, instance)
}

// carries reports whether a call for the principal is open on the instance,
// and the channel that closes when the last one ends.
func (f *flights) carries(instance ref.Ref, principal string) (<-chan struct{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	held, ok := f.open[flight{instance: instance, principal: principal}]
	if !ok {
		return nil, false
	}

	return held.gone, true
}

// within reports whether a call for the principal is open on the instance,
// and returns ctx narrowed to the scopes of every such call.
func (f *flights) within(ctx context.Context, instance ref.Ref, principal string) (context.Context, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	held, ok := f.open[flight{instance: instance, principal: principal}]
	if !ok {
		return ctx, false
	}

	return held.narrowest(ctx), true
}

// vouchedFleet refuses a fleet call naming a principal the host did not hand
// this instance in a request still being served.
type vouchedFleet struct {
	inner    deviceapi.Fleet
	instance ref.Ref
	board    *flights
}

// vouchFleet wraps a fleet. It returns nil for a nil fleet, so the method set
// of a host that serves no fleet stays empty.
func (h *Host) vouchFleet(instance ref.Ref, inner deviceapi.Fleet) deviceapi.Fleet {
	if inner == nil {
		return nil
	}

	return vouchedFleet{inner: inner, instance: instance, board: &h.flights}
}

func (v vouchedFleet) List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Page{}, err
	}

	return v.inner.List(ctx, params)
}

func (v vouchedFleet) Get(ctx context.Context, params deviceapi.GetParams) (deviceapi.Device, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Device{}, err
	}

	return v.inner.Get(ctx, params)
}

func (v vouchedFleet) Command(
	ctx context.Context, params deviceapi.CommandParams,
) (deviceapi.Result, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Result{}, err
	}

	return v.inner.Command(ctx, params)
}

func (v vouchedFleet) Approve(
	ctx context.Context, params deviceapi.ApproveParams,
) (deviceapi.Registration, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Registration{}, err
	}

	return v.inner.Approve(ctx, params)
}

func (v vouchedFleet) Jobs(ctx context.Context, params deviceapi.JobsParams) (deviceapi.Jobs, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Jobs{}, err
	}

	return v.inner.Jobs(ctx, params)
}

func (v vouchedFleet) Print(ctx context.Context, params deviceapi.PrintParams) (deviceapi.Started, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Started{}, err
	}

	return v.inner.Print(ctx, params)
}

func (v vouchedFleet) JobHistory(
	ctx context.Context, params deviceapi.JobHistoryParams,
) (deviceapi.JobPage, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.JobPage{}, err
	}

	return v.inner.JobHistory(ctx, params)
}

func (v vouchedFleet) Files(ctx context.Context, params deviceapi.FilesParams) (deviceapi.Listing, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return deviceapi.Listing{}, err
	}

	return v.inner.Files(ctx, params)
}

// vouch returns an error for a principal that is in no open request the host
// forwarded to this instance. It accepts an empty principal, which means the
// module acts in its own name. It accepts node:<id> from a link that reported
// that server.
func (v vouchedFleet) vouch(ctx context.Context, principal string) (context.Context, error) {
	if principal == "" {
		return ctx, nil
	}

	if narrowed, ok := v.board.within(ctx, v.instance, principal); ok {
		return narrowed, nil
	}

	if v.board.actsAsNode(v.instance, principal) {
		return ctx, nil
	}

	return ctx, fmt.Errorf("%w: %s is not a principal this host handed %s in a request it is serving",
		deviceapi.ErrDenied, principal, v.instance)
}

// actsAsNode reports whether this instance may act as the principal node:<id>.
// Only a link may, and only for a server it reported to this host.
//
// This check does not limit which servers a link reports. Each status
// replaces the set before it, so a link can report a new server and then act
// as that server. The check makes sure that every node:<id> call matches the
// status the link sent, which is the status an operator sees on the links
// page. A link that has sent no status may act as no server.
//
// The authorization graph and the permissions list of the instance limit what
// a node may do. A node id with no grant has no permission on any printer,
// and the permissions list limits every call the link relays.
func (f *flights) actsAsNode(instance ref.Ref, principal string) bool {
	if instance.Kind() != ref.KindLink {
		return false
	}

	id, ok := strings.CutPrefix(principal, nodePrefix)
	if !ok || id == "" {
		return false
	}

	return f.nodes.holds(instance, id)
}

// vouchedStorage refuses a storage call naming a principal the host did not
// hand this instance in a request still being served.
type vouchedStorage struct {
	inner    storageapi.Files
	instance ref.Ref
	board    *flights
}

// vouchStorage wraps the storage files. It returns nil for nil, so the method
// set of a host that serves no storage stays empty.
func (h *Host) vouchStorage(instance ref.Ref, inner storageapi.Files) storageapi.Files {
	if inner == nil {
		return nil
	}

	return vouchedStorage{inner: inner, instance: instance, board: &h.flights}
}

func (v vouchedStorage) Stat(
	ctx context.Context, params storageapi.StatParams,
) (storageapi.FileInfo, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.FileInfo{}, err
	}

	return v.inner.Stat(ctx, params)
}

func (v vouchedStorage) Read(
	ctx context.Context, params storageapi.ReadParams,
) (storageapi.Chunk, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.Chunk{}, err
	}

	return v.inner.Read(ctx, params)
}

func (v vouchedStorage) List(
	ctx context.Context, params storageapi.ListParams,
) (storageapi.Listing, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.Listing{}, err
	}

	return v.inner.List(ctx, params)
}

func (v vouchedStorage) Fetch(
	ctx context.Context, params storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.Fetched{}, err
	}

	return v.inner.Fetch(ctx, params, w)
}

func (v vouchedStorage) Write(
	ctx context.Context, params storageapi.WriteParams, body io.Reader,
) (storageapi.Written, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.Written{}, err
	}

	return v.inner.Write(ctx, params, body)
}

func (v vouchedStorage) Move(
	ctx context.Context, params storageapi.MoveParams,
) (storageapi.Written, error) {
	ctx, err := v.vouch(ctx, params.Principal)
	if err != nil {
		return storageapi.Written{}, err
	}

	return v.inner.Move(ctx, params)
}

// vouch is vouchedFleet.vouch with storageapi.ErrDenied as the refusal.
func (v vouchedStorage) vouch(ctx context.Context, principal string) (context.Context, error) {
	if principal == "" {
		return ctx, nil
	}

	if narrowed, ok := v.board.within(ctx, v.instance, principal); ok {
		return narrowed, nil
	}

	if v.board.actsAsNode(v.instance, principal) {
		return ctx, nil
	}

	return ctx, fmt.Errorf("%w: %s is not a principal this host handed %s in a request it is serving",
		storageapi.ErrDenied, principal, v.instance)
}

// vouchedCameras refuses a frame naming a principal the host did not hand this
// instance in a request still being served.
type vouchedCameras struct {
	inner    cameraapi.Frames
	instance ref.Ref
	board    *flights
}

// vouchCameras wraps the cameras, and leaves a host that serves none as it is
// so the method set stays empty.
func (h *Host) vouchCameras(instance ref.Ref, inner cameraapi.Frames) cameraapi.Frames {
	if inner == nil {
		return nil
	}

	return vouchedCameras{inner: inner, instance: instance, board: &h.flights}
}

func (v vouchedCameras) Admit(ctx context.Context, params cameraapi.AcceptParams) error {
	ctx, err := v.vouch(ctx, params)
	if err != nil {
		return err
	}

	return v.inner.Admit(ctx, params)
}

func (v vouchedCameras) Accept(ctx context.Context, params cameraapi.AcceptParams, frame []byte) error {
	ctx, err := v.vouch(ctx, params)
	if err != nil {
		return err
	}

	return v.inner.Accept(ctx, params, frame)
}

// vouch narrows ctx to the request that handed the instance the call's
// principal, and refuses a principal no request still served handed it.
func (v vouchedCameras) vouch(ctx context.Context, params cameraapi.AcceptParams) (context.Context, error) {
	if params.Principal == "" {
		return ctx, nil
	}

	narrowed, ok := v.board.within(ctx, v.instance, params.Principal)
	if !ok {
		return ctx, fmt.Errorf("%w: %s is not a principal this host handed %s in a request it is serving",
			cameraapi.ErrDenied, params.Principal, v.instance)
	}

	return narrowed, nil
}

// vouchedAccess refuses a question about a subject this host has not handed
// this instance since it started, unless the operator let the instance ask
// about people it is not serving, or the subject is a server the instance, a
// link, reported.
//
// The host makes this check because only the host knows which requests it
// forwarded. The inner Access checks which actions the instance may ask about.
type vouchedAccess struct {
	inner    accessapi.Access
	instance ref.Ref
	board    *flights
	absent   absentAccess
}

// vouchAccess wraps the decider for an instance without the permission to ask
// about people it is not serving. It returns nil for a nil decider, so the
// method set of a host that answers no such question stays empty.
func (h *Host) vouchAccess(instance ref.Ref, inner accessapi.Access) accessapi.Access {
	return h.vouchAccessFor(Module{Instance: instance}, inner)
}

// AccessFor is the access.check the host serves one module: the decider the
// configuration binds, wrapped in the host's own check. A module's session
// gets it, and a test of the whole composition calls it.
func (h *Host) AccessFor(module Module) accessapi.Access {
	if h.cfg.Access == nil {
		return nil
	}

	return h.vouchAccessFor(module, h.cfg.Access(module))
}

// vouchAccessFor wraps the decider for one module, with the permission its
// declaration carries.
func (h *Host) vouchAccessFor(module Module, inner accessapi.Access) accessapi.Access {
	if inner == nil {
		return nil
	}

	return vouchedAccess{
		inner:    inner,
		instance: module.Instance,
		board:    &h.flights,
		absent: absentAccess{
			allowed: module.CheckAbsentPeople,
			people:  h.cfg.People,
			budget:  &h.absent,
			quiet:   &h.quiet,
			log:     h.cfg.Logger,
		},
	}
}

func (v vouchedAccess) Check(
	ctx context.Context, params accessapi.CheckParams,
) (accessapi.Decision, error) {
	// An empty subject is the instance asking about itself. That needs no
	// check against the people it served.
	if params.Subject == "" {
		return v.inner.Check(ctx, params)
	}

	// A link may ask about a server it reported. It already calls the fleet
	// as that principal.
	if v.board.actsAsNode(v.instance, params.Subject) {
		return v.inner.Check(ctx, params)
	}

	if !v.board.hasServed(v.instance, params.Subject) {
		return v.checkAbsent(ctx, params)
	}

	// While the person's request is open, the question is limited to the
	// scopes of the credential that request carried.
	ctx, _ = v.board.within(ctx, v.instance, params.Subject)

	return v.checkServed(ctx, params)
}

// vouchedFeed is vouchedFleet for subscriptions. A subscription that a
// person's request opened ends when the last open request of that person on
// the instance ends.
type vouchedFeed struct {
	inner    busapi.Feed
	instance ref.Ref
	board    *flights
}

// vouchFeed wraps a feed, and returns nil for a nil feed.
func (h *Host) vouchFeed(instance ref.Ref, inner busapi.Feed) busapi.Feed {
	if inner == nil {
		return nil
	}

	return vouchedFeed{inner: inner, instance: instance, board: &h.flights}
}

func (v vouchedFeed) Subscribe(
	ctx context.Context, params busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	if params.Principal == "" {
		return v.inner.Subscribe(ctx, params, write)
	}

	gone, ok := v.board.carries(v.instance, params.Principal)
	if !ok {
		// A link that subscribes as node:<id> has no open request that could
		// end the stream, so the stream stays open until the link closes it.
		if v.board.actsAsNode(v.instance, params.Principal) {
			return v.inner.Subscribe(ctx, params, write)
		}

		return fmt.Errorf("%w: %s is not a principal this host handed %s in a request it is serving",
			busapi.ErrDenied, params.Principal, v.instance)
	}

	ctx, _ = v.board.within(ctx, v.instance, params.Principal)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-gone:
			cancel()
		case <-ctx.Done():
		}
	}()

	return v.inner.Subscribe(ctx, params, write)
}
