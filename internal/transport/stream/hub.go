// Package stream carries an event from the state core to every subscriber
// allowed to see it, in process and with no broker. The envelope it carries and
// the cursor a reader resumes from are the public contract and live in
// sdk/event; what is here is the delivery: one channel per origin, two queue
// classes per subscriber, one marshal shared by all of them, and a coalescing
// window that bounds what leaves.
//
// What is lock-free on the publish path: the channel registry lookup and the
// subscriber slice read, both atomic pointer loads. What is not: minting an
// event id and appending to an origin's replay window each take that origin's
// own mutex, uncontended because one goroutine publishes for one origin.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	// MinInterval is the fastest a subscriber may be fed. Nothing renders
	// faster than ten frames a second, and forwarding every printer frame to
	// every subscriber costs more than a four-core Pi has.
	MinInterval = 100 * time.Millisecond
	// DefaultInterval is five frames a second. A slower interval is allowed
	// and costs less.
	DefaultInterval = 200 * time.Millisecond
	// MaxInterval is the slowest a subscriber may be fed. Past it a proxy that
	// closes idle connections sees a stream that carries events as silent, and
	// the reader's event queue holds a whole window of a fleet before anything
	// leaves.
	MaxInterval = 10 * time.Second

	// DefaultMaxPerPrincipal bounds how many subscriptions one principal may
	// hold at once. Each subscription costs a goroutine and its queues, and an
	// SSE reader adds a 32 KiB write buffer, so 64 is about 2 MiB of buffer and
	// 64 goroutines for one caller. A deployment with no issuer configured
	// serves every request as one principal, so every browser tab in a small
	// office counts against this one bound.
	DefaultMaxPerPrincipal = 64

	// AuthorizeInterval is how often an idle subscription re-reads the
	// authorization epoch. It bounds how long a withdrawn grant can keep a
	// stream attached when no event flows through it. A stream that carries
	// events re-reads on every window. The read is one atomic load, so one
	// second is affordable.
	AuthorizeInterval = time.Second

	// defaultTelemetryBytes is the backstop under one subscriber's conflated
	// telemetry. Conflation is the bound, so this only has to sit above what
	// conflation holds. Below that it becomes the bound and drops values.
	// A site of 400 printers has 400 origins with about fifteen telemetry keys
	// each. One telemetry envelope is about 400 bytes, so a full window is
	// 2.4 MB.
	defaultTelemetryBytes = 4 << 20
	// defaultEventBytes bounds the durable lane. Past it the reader is told to
	// resync. It is never handed part of a stream without being told.
	defaultEventBytes = 1 << 20

	// The replay window is per origin, so an operator multiplies these by the
	// fleet size to bound it: 400 printers at 32 KB is 12.5 MB of worst case,
	// reached only if every printer fills its window.
	defaultHistoryEvents = 32
	defaultHistoryBytes  = 32 << 10

	// defaultAuthorizeTimeout bounds a re-authorization on the pump. It is
	// generous for a local store and short for a stalled one. A
	// re-authorization that gets no answer drops the reader.
	defaultAuthorizeTimeout = 5 * time.Second
)

// Errors reported by the hub. Callers inspect them with errors.Is.
var (
	// ErrHubClosed reports use of a hub that has shut down.
	ErrHubClosed = errors.New("stream hub closed")
	// ErrNotPermitted reports a subscription to a resource the principal may not
	// see. It names no resource and reads the same for a resource that does
	// not exist, so a refusal never confirms that a resource exists.
	ErrNotPermitted = errors.New("subscription not permitted")
	// ErrInvalidRequest reports a malformed publication or subscription.
	ErrInvalidRequest = errors.New("invalid stream request")
	// ErrAuthorize reports that the authorization decision could not be made.
	ErrAuthorize = errors.New("authorize subscription")
	// ErrTooManySubscriptions reports a principal already holding as many
	// subscriptions as this hub allows one. Callers that need the number match
	// a LimitError with errors.As.
	ErrTooManySubscriptions = errors.New("too many subscriptions for this principal")
)

// LimitError reports a refusal that met a configured bound. It carries the
// bound, so a transport can tell the caller the number. It matches
// ErrTooManySubscriptions under errors.Is.
type LimitError struct {
	// Limit is the bound that was met.
	Limit int
	// Name is which name met it, and empty when the refusal names none. A
	// subscription is counted under the principal and under the holder, so
	// the refusal says which of the two met the bound.
	Name string
}

// Error names the bound that was met and who met it.
func (e LimitError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("%s: the limit is %d", ErrTooManySubscriptions, e.Limit)
	}

	return fmt.Sprintf("%s: %s holds the limit of %d", ErrTooManySubscriptions, e.Name, e.Limit)
}

// Unwrap reports the sentinel a caller branches on.
func (e LimitError) Unwrap() error { return ErrTooManySubscriptions }

// Grant is one origin a principal may see, and the generation of the
// authorization graph at which that sight began.
//
// Since bounds a replay of this origin. Visibility.Epoch cannot do that. A
// site powering up moves the graph once per machine, so a replay bounded by
// Epoch would skip every machine that arrived before the last one. Zero means
// the authorizer recorded no generation. The hub then falls back to Epoch,
// which is never lower.
type Grant struct {
	Origin event.Origin
	Since  uint64
	// Audit is whether this principal may see the audit class of this origin,
	// which is the view_audit action. It is per origin because the action is
	// per device. It is recomputed in the same re-authorization pass that
	// rebuilds the origins, so revoking audit sight takes effect when revoking
	// an origin does.
	//
	// Seeing a printer does not include being told which operator commanded
	// it. Every other class of the origin is delivered on the origin grant
	// alone. Audit narrows one class inside an origin that is already
	// authorized.
	Audit bool
}

// Visibility is one principal's answer from the Authorizer.
type Visibility struct {
	// Origins is the set the principal may see. It is taken once, at subscribe
	// time, and becomes the routing table for the subscription.
	//
	// The hub grants nothing it is not handed. An authorizer that answers with
	// printers alone produces a subscriber that sees printers alone, however
	// many kinds the bus carries: a principal permitted zero printers is still
	// permitted zero of everything. What a principal may see of a camera, a
	// node or a storage instance is a question for the authorization graph,
	// and until that graph answers it the hub refuses.
	Origins []Grant
	// Epoch is the generation of the authorization graph the answer was
	// computed at.
	Epoch uint64
}

// Authorizer answers the two questions the hub asks. VisibleOrigins is asked
// once per subscription and not once per event. Channel membership is then the
// decision, so the cost grows with subscribes and not with events times
// subscribers. Epoch is asked once per subscriber per coalescing window, so a
// grant withdrawn mid-stream takes effect before the reader reconnects.
//
// It is declared where it is consumed. The policy behind it lives elsewhere.
type Authorizer interface {
	VisibleOrigins(ctx context.Context, principal string) (Visibility, error)
	// Epoch is the generation of the authorization graph, bumped by any grant
	// change. It is read on every subscriber's tick, so it must be an atomic
	// load and never a query.
	Epoch() uint64
}

// Options configures a hub. Every field except Site and Authorizer has a usable
// zero value.
type Options struct {
	// Site is the node that owns these resources. It is the site token of every
	// source URN, and a relay never rewrites it.
	Site string
	// Authorizer decides who may subscribe to what.
	Authorizer Authorizer
	// Logger is where the hub reports a reader losing continuity. Discarded
	// when nil.
	Logger *slog.Logger
	// Interval is the default coalescing window for a subscription that asks
	// for none.
	Interval time.Duration
	// TelemetryBytes and EventBytes bound one subscriber's two queues.
	TelemetryBytes int
	EventBytes     int
	// HistoryEvents and HistoryBytes bound the replay window of one origin.
	HistoryEvents int
	HistoryBytes  int
	// AuthorizeTimeout bounds one re-authorization. A pump waiting on the
	// policy store is a reader receiving nothing, so the wait is bounded and a
	// timeout is treated as a refusal.
	AuthorizeTimeout time.Duration
	// MaxPerPrincipal bounds how many subscriptions one principal may hold at
	// once. Zero takes DefaultMaxPerPrincipal; there is no value that lifts the
	// bound, because a subscription costs a goroutine and a queue whoever asked
	// for it.
	MaxPerPrincipal int
}

// Publication is what a producer hands the hub, which owns identity, time,
// sequence and offset.
//
// Events for one origin must be published from one goroutine. That keeps the
// sequence of one source in order.
type Publication struct {
	// Origin is the channel and the envelope's subject: the resource this event
	// came from, which is a printer, a camera, a storage instance or any other
	// kind the registry names. An instance state transition is published here
	// like any other event.
	Origin event.Origin
	// Type is reverse-DNS and past tense.
	Type  string
	Class event.Class
	// Key names what a telemetry value supersedes. Empty means Type, which is
	// right whenever one event type carries one metric.
	Key         string
	Data        json.RawMessage
	DataSchema  string
	TraceParent string
	CausationID string
	// TTL is the remaining seconds of a command. Commands only.
	TTL *uint32
	// AuthType and AuthID record who authorized a command, for audit.
	AuthType string
	AuthID   string
}

// SubscribeRequest asks for a subscription.
type SubscribeRequest struct {
	// Principal is who is reading, as the Authorizer understands it.
	Principal string
	// Holder is a second name this subscription is counted against, and empty
	// when the principal is the only one. It decides nothing about what the
	// reader may see.
	//
	// It is for a caller that opens subscriptions as principals other than
	// itself. A link module reads for every server that joined this one, and
	// each of those is a principal of its own, so the per-principal bound alone
	// would let one module hold that bound once per server it names. The
	// instance behind the subscriptions is named here and counted too.
	Holder string
	// Origins narrows the subscription. Empty means every origin the principal
	// may see. An origin the principal may not see is refused.
	Origins []event.Origin
	// Types narrows the subscription to these event types. Empty is every type.
	//
	// It is a preference and not an authorization: Origins decides what this
	// principal may see and this decides what of that it asked for. A type
	// nothing publishes is not refused, it simply matches nothing, so a client
	// may name a type a later release adds.
	Types []string
	// Resume is the place a reconnecting reader left off at. The zero value
	// starts live.
	Resume event.Cursor
	// Encoding is the wire form this reader wants its frames in.
	Encoding Encoding
	// Interval is the coalescing window. Below MinInterval it is raised to it;
	// zero means the hub's default.
	Interval time.Duration
	// Lossless asks for every telemetry frame, in arrival order, instead of the
	// newest value per key that the window would otherwise coalesce to. It is
	// for a history store, the one subscriber for which a superseded reading is
	// a measurement lost rather than one nobody needed; a reader that only
	// renders the current value wants the default and should not ask for this.
	//
	// It changes what bounds the lane. Conflation bounds the default lane by
	// the number of distinct keys this subscriber can see, so no reader is slow
	// enough to overflow it. This one is bounded by the lag instead, so a
	// reader that falls behind can overflow it, and an overflow arrives as a
	// batch with Resync raised rather than as a quietly shorter series.
	Lossless bool
	// FlushOnClose asks for one last batch when the subscription is closed,
	// carrying whatever the coalescing window was still holding.
	//
	// Without it a Close discards that window, so this reader loses every
	// durable frame published in the last window before a shutdown.
	//
	// It is opt-in because something must read the last batch. An SSE stream
	// closes its subscription and stops reading, so its pump would wait the
	// whole of finalBatchBudget with nothing reading the channel. A store's
	// reader drains Batches until it is closed, so for it the last batch costs
	// nothing and keeps a record that would be lost.
	FlushOnClose bool
}

// Hub fans one published event out to every subscriber allowed to see it, in
// process and with no broker. A few hundred connections are far below what one
// Go server holds, so the design guards memory under a stalled reader and
// correctness after a gap, not throughput.
type Hub struct {
	site  string
	epoch string
	authz Authorizer
	log   *slog.Logger

	interval         time.Duration
	telemetryBytes   int
	eventBytes       int
	historyEvents    int
	historyBytes     int
	authorizeTimeout time.Duration

	channels atomic.Pointer[map[event.Origin]*channel]
	chmu     sync.Mutex

	submu   sync.Mutex
	subs    map[uint64]*Subscription
	nextSub uint64
	shut    bool
	// held counts one principal's live subscriptions. An entry is removed at
	// zero, so the map is bounded by the principals that hold a subscription.
	held            map[string]int
	maxPerPrincipal int

	closed atomic.Bool
	pumps  sync.WaitGroup
}

// New returns a hub and mints its epoch. A restart recreates the log and
// offsets start again, so a cursor issued by the previous process must be
// refused. Its offsets would match different events.
func New(opts Options) (*Hub, error) {
	if opts.Site == "" {
		return nil, fmt.Errorf("%w: site is empty", ErrInvalidRequest)
	}

	if opts.Authorizer == nil {
		return nil, fmt.Errorf("%w: authorizer is nil", ErrInvalidRequest)
	}

	var minter ref.Minter

	epoch, err := minter.Mint(time.Now())
	if err != nil {
		return nil, fmt.Errorf("mint stream epoch: %w", err)
	}

	h := &Hub{
		site:           opts.Site,
		epoch:          epoch.String(),
		authz:          opts.Authorizer,
		log:            opts.Logger,
		interval:       clampInterval(orDuration(opts.Interval, DefaultInterval)),
		telemetryBytes: orInt(opts.TelemetryBytes, defaultTelemetryBytes),
		eventBytes:     orInt(opts.EventBytes, defaultEventBytes),
		historyEvents:  orInt(opts.HistoryEvents, defaultHistoryEvents),
		historyBytes:   orInt(opts.HistoryBytes, defaultHistoryBytes),
		subs:           make(map[uint64]*Subscription),
		held:           make(map[string]int),
	}

	h.maxPerPrincipal = orInt(opts.MaxPerPrincipal, DefaultMaxPerPrincipal)

	h.authorizeTimeout = orDuration(opts.AuthorizeTimeout, defaultAuthorizeTimeout)

	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}

	h.channels.Store(&map[event.Origin]*channel{})

	return h, nil
}

// Epoch identifies this hub's numbering generation. It appears in every
// envelope and in every cursor. It tells a reconnecting client that the
// offsets it remembers belong to an earlier stream.
func (h *Hub) Epoch() string { return h.epoch }

// Publish assigns the envelope its identity, sequence and offset, marshals it
// once, and hands the same bytes to every subscriber of that origin. It never
// blocks: both per-subscriber queues answer immediately, so a stalled reader
// cannot slow a printer down or slow another reader down.
//
// It takes no context for that reason. There is nothing here to cancel.
func (h *Hub) Publish(pub Publication) (event.Position, error) {
	_, position, err := h.PublishStored(pub)

	return position, err
}

// PublishStored is Publish for a caller that also writes the frame into its
// own store. It returns the envelope, so the row the caller writes is the row
// the store would have written off the bus.
//
// The position's offset and the envelope's sequence are different numbers.
// The offset counts an origin's durable events, because a reader resumes by it
// and a telemetry frame the reader never receives must not move it. The
// sequence counts everything published on that origin, telemetry included, and
// a history row is keyed by it, on (ref, epoch, sequence). A frame stored
// under its offset becomes a second row for a frame the store already holds
// under its sequence.
func (h *Hub) PublishStored(pub Publication) (event.Envelope, event.Position, error) {
	if err := pub.validate(); err != nil {
		return event.Envelope{}, event.Position{}, err
	}

	if h.closed.Load() {
		return event.Envelope{}, event.Position{}, ErrHubClosed
	}

	// Read before the event exists, so the stamp is never newer than the graph
	// that was in force when it was published. A stamp that ran ahead would let
	// a grant written afterwards replay this event.
	granted := h.authz.Epoch()

	ch := h.channel(pub.Origin)

	env, err := h.envelope(ch, pub)
	if err != nil {
		return event.Envelope{}, event.Position{}, err
	}

	m, err := newMessage(env, pub.Origin, pub.conflationKey())
	if err != nil {
		return event.Envelope{}, event.Position{}, err
	}

	m.aclEpoch = granted

	// Telemetry takes no offset at all. An offset counts an origin's durable
	// events, and a reader that never receives one must not be handed a number
	// that moves when other origins publish.
	if env.Class.Durable() {
		ch.hist.append(m)
	} else {
		ch.last.keep(m)
	}

	for _, sub := range ch.subscribers() {
		sub.deliver(m)
	}

	return env, m.position, nil
}

// Subscribe authorizes once, attaches the reader to one channel per origin it
// may see, and starts its pump. An origin the principal may not see is not
// attached, is not replayed and is not named in the refusal.
func (h *Hub) Subscribe(ctx context.Context, req SubscribeRequest) (*Subscription, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	if h.closed.Load() {
		return nil, ErrHubClosed
	}

	// Reserved before the authorization query and before a channel is attached,
	// so a caller already at the bound cannot make the hub run a policy read and
	// replay a window per refused attempt. Every path below either hands back a
	// Subscription, whose Close releases it, or releases it here.
	if err := h.reserve(req.Principal, req.Holder); err != nil {
		return nil, err
	}

	visibility, err := h.authz.VisibleOrigins(ctx, req.Principal)
	if err != nil {
		h.release(req.Principal, req.Holder)

		return nil, fmt.Errorf("%w: %w", ErrAuthorize, err)
	}

	origins, err := selectOrigins(req.Origins, visibility.Origins)
	if err != nil {
		h.release(req.Principal, req.Holder)

		return nil, err
	}

	// stamp bounds the replay of a grant the authorizer reported no generation
	// for. It is read again after the answer. The graph reads its counter
	// before its rows, so an answer can carry a grant together with a stamp
	// older than the write that made the grant, and a replay bounded by that
	// stamp would start before the grant. An event stamped at or above a
	// generation read after the answer was published while the grant existed.
	stamp := max(visibility.Epoch, h.authz.Epoch())

	sub := h.newSubscription(req, origins, visibility.Epoch)
	sub.values = context.WithoutCancel(ctx)

	// Attach first, then merge the replay ahead of whatever the live path
	// queued in between: replaying first leaves a hole, appending after
	// reorders.
	for _, ch := range sub.routing() {
		ch.attach(sub)
	}

	h.replay(sub, req.Resume, origins, stamp)

	if started := h.start(sub); started != nil {
		sub.Close()

		return nil, started
	}

	return sub, nil
}

// Subscribers reports how many subscriptions the hub is serving.
func (h *Hub) Subscribers() int {
	h.submu.Lock()
	defer h.submu.Unlock()

	return len(h.subs)
}

// Close detaches every subscriber and waits for its pump to stop. A reader sees
// its batch channel close.
func (h *Hub) Close() {
	h.submu.Lock()

	if h.shut {
		h.submu.Unlock()

		return
	}

	h.shut = true
	h.closed.Store(true)

	subs := make([]*Subscription, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}

	h.submu.Unlock()

	for _, sub := range subs {
		sub.Close()
	}

	h.pumps.Wait()
}

func (h *Hub) newSubscription(req SubscribeRequest, origins []Grant, aclEpoch uint64) *Subscription {
	channels := make([]*channel, 0, len(origins))
	for _, grant := range origins {
		channels = append(channels, h.channel(grant.Origin))
	}

	sub := &Subscription{
		hub:       h,
		log:       h.log,
		principal: req.Principal,
		holder:    req.Holder,
		encoding:  req.Encoding,
		interval:  clampInterval(orDuration(req.Interval, h.interval)),
		requested: slices.Clone(req.Origins),
		types:     typeSet(req.Types),
		lossless:  req.Lossless,
		flush:     req.FlushOnClose,
		telemetry: newTelemetryQueue(h.telemetryBytes),
		series:    newSeriesQueue(h.telemetryBytes),
		events:    newEventQueue(h.eventBytes),
		out:       make(chan Batch, 1),
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
		positions: make(map[event.Origin]uint64, len(channels)),
	}

	sub.setRouting(channels, origins)
	sub.aclEpoch.Store(aclEpoch)

	// A fresh reader starts at each origin's head, because it has no earlier
	// state to reconcile. So does an origin the resume cursor holds no place
	// in. A cursor names only the origins a reader has seen an event from, so
	// an origin it does not name is one the reader never saw, and starting at
	// the oldest retained event would replay a window the reader was not owed.
	// Only the generation this hub minted is read: an offset another node
	// issued names nothing here.
	mine, held := req.Resume.Generation(h.epoch)

	for _, ch := range channels {
		offset, named := mine.Offset(ch.origin)

		if req.Resume.IsZero() || !held || !named {
			sub.positions[ch.origin] = ch.hist.position()
		} else {
			sub.positions[ch.origin] = offset
		}

		// The reader is already past the place it starts at, whether the cursor
		// named it or the head supplied it. The queue is told, so a publisher
		// that minted an offset before this attach cannot queue an event the
		// reader already holds.
		sub.events.expect(ch.origin, sub.positions[ch.origin])
	}

	sub.snapshot()

	return sub
}

// start registers the subscription and launches its pump under the lock Close
// takes, so a Close that lands mid-subscribe cannot leave a pump running past
// the wait, nor a reader holding a channel that never closes.
func (h *Hub) start(sub *Subscription) error {
	h.submu.Lock()
	defer h.submu.Unlock()

	if h.shut {
		return ErrHubClosed
	}

	h.nextSub++
	sub.id = h.nextSub
	h.subs[sub.id] = sub
	h.pumps.Go(sub.pump)

	return nil
}

// replay fills the gap between the reader's cursor and each channel's head.
// Only channels this subscription is attached to are read, so a replay cannot
// deliver, or count, an event on an origin the reader may not see.
//
// Each origin is bounded by the generation its own grant began at, exactly as
// the same generation bounds a re-authorization mid-stream: what predates the
// grant is neither replayed nor counted, whether the reader was handed the
// origin while it was reading or arrived carrying a cursor. stamp is what an
// origin the authorizer reported no generation for falls back to.
func (h *Hub) replay(sub *Subscription, resume event.Cursor, origins []Grant, stamp uint64) {
	if resume.IsZero() {
		return
	}

	// A cursor may span several nodes, and each node's epoch scopes that node's
	// offsets alone. This hub reads the generation it minted and ignores the
	// rest, so another node restarting costs a reader nothing here.
	mine, held := resume.Generation(h.epoch)

	// Every origin the reader's own cursor names and it may no longer see, told
	// before the return below, so a reader who lost everything still learns
	// what. Whether the origin still exists is not consulted: one that never
	// existed reads the same as one taken away. A reader that named its origins
	// left the rest out itself, and is closed rather than told when it loses one.
	if held && len(sub.requested) == 0 {
		for _, position := range mine.Positions {
			if !slices.ContainsFunc(origins, func(g Grant) bool { return g.Origin == position.Origin }) {
				sub.withdraw(position.Origin)
			}
		}
	}

	// A reader following no origin holds no place in one, so there is nothing
	// else it can have lost. The frame that says otherwise carries the
	// generation this hub minted, which is a timestamp of the process, and a
	// caller the graph permits nothing must not be handed one for a cursor it
	// could not have held.
	if len(sub.routing()) == 0 {
		return
	}

	if !held {
		// The reader holds places, but none this hub issued: the log its
		// offsets belong to no longer exists, so offset 10 in it is a different
		// event than offset 10 here.
		sub.events.requireResync()
		sub.signal()

		return
	}

	var (
		replayed []*Message
		gap      bool
	)

	// Over the grants rather than over the routing table, because the bound is a
	// property of the grant. They are the same set: the routing table was built
	// from these, and the channel lookup is the one the build took.
	for _, grant := range origins {
		ch := h.channel(grant.Origin)

		// An origin the cursor holds no place in was attached at its head above,
		// so there is no gap under it to fill and no window of it to replay. A
		// replay from zero would also declare the whole subscription's
		// continuity gone the moment any origin outran its window.
		after, named := mine.Offset(ch.origin)
		if !named {
			continue
		}

		// A cursor is text the client supplies and the offset in it is whatever
		// the client chose, so it narrows a replay and never widens one. Reaching
		// behind the grant is the same loss as reaching behind the window: what
		// it asks for cannot be delivered, and the reader is told, because a
		// place quietly moved forward is a reader that believes it has continuity
		// it does not have.
		if after < ch.hist.grantedFloor(floorOf(grant.Since, stamp)) {
			gap = true

			continue
		}

		missed, missing := ch.hist.since(after)
		if missing {
			gap = true

			continue
		}

		replayed = append(replayed, admitted(missed, grant.Audit)...)
	}

	sub.events.adopt(replayed)

	if gap {
		sub.events.requireResync()
	}

	if gap || len(replayed) > 0 {
		sub.signal()
	}
}

// admitted drops the audit frames of a replay a grant does not carry sight of.
//
// The replay path does not run deliver, so the class check is repeated here. A
// reconnecting reader is handed the same events the live path filtered, and
// the audit narrowing must hold on both paths.
func admitted(replayed []*Message, audit bool) []*Message {
	if audit {
		return replayed
	}

	kept := make([]*Message, 0, len(replayed))

	for _, m := range replayed {
		if m.env.Class == event.ClassAudit {
			continue
		}

		kept = append(kept, m)
	}

	return kept
}

// channel returns the origin's channel, creating it on first use. The registry
// is copy-on-write because it is read on every event and written when an origin
// appears.
//
// A channel is never removed: its sequence and replay window outlive every
// subscriber, so the registry grows with the fleet and not with connections.
func (h *Hub) channel(origin event.Origin) *channel {
	if ch, ok := (*h.channels.Load())[origin]; ok {
		return ch
	}

	h.chmu.Lock()
	defer h.chmu.Unlock()

	current := *h.channels.Load()
	if ch, ok := current[origin]; ok {
		return ch
	}

	ch := newChannel(origin, h.historyEvents, h.historyBytes)

	next := make(map[event.Origin]*channel, len(current)+1)
	maps.Copy(next, current)
	next[origin] = ch
	h.channels.Store(&next)

	return ch
}

func (h *Hub) envelope(ch *channel, pub Publication) (event.Envelope, error) {
	now := time.Now().UTC()

	id, sequence, err := ch.next(now)
	if err != nil {
		return event.Envelope{}, err
	}

	env := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          id.String(),
		Source:      event.SourceURN(h.site, pub.Origin),
		Type:        pub.Type,
		Time:        now,
		Subject:     pub.Origin.String(),
		DataSchema:  pub.DataSchema,
		Sequence:    event.FormatSequence(sequence),
		TraceParent: pub.TraceParent,
		CausationID: pub.CausationID,
		Epoch:       h.epoch,
		Class:       pub.Class,
		TTL:         pub.TTL,
		AuthType:    pub.AuthType,
		AuthID:      pub.AuthID,
		Data:        pub.Data,
	}

	if invalid := env.Validate(); invalid != nil {
		return event.Envelope{}, invalid
	}

	return env, nil
}

func (h *Hub) detach(sub *Subscription) {
	for _, ch := range sub.routing() {
		ch.detach(sub)
	}

	h.submu.Lock()
	defer h.submu.Unlock()

	delete(h.subs, sub.id)

	for _, name := range countedNames(sub.principal, sub.holder) {
		h.releaseLocked(name)
	}
}

// reserve takes one place for the principal and one for the holder, or reports
// that either holds them all. It is the only place a place is taken, and detach
// the only place one is given back, so a subscription that exists holds exactly
// one of each name it was opened under.
//
// A holder equal to the principal is one name and takes one place. Counting it
// twice would halve what a reader holding its own subscriptions may open.
func (h *Hub) reserve(principal, holder string) error {
	h.submu.Lock()
	defer h.submu.Unlock()

	if h.shut {
		return ErrHubClosed
	}

	for _, name := range countedNames(principal, holder) {
		if h.held[name] >= h.maxPerPrincipal {
			h.giveBackLocked(principal, holder, name)

			return LimitError{Limit: h.maxPerPrincipal, Name: name}
		}

		h.held[name]++
	}

	return nil
}

// giveBackLocked releases the names taken before the one that met the bound.
// The caller holds h.submu.
func (h *Hub) giveBackLocked(principal, holder, stopped string) {
	for _, name := range countedNames(principal, holder) {
		if name == stopped {
			return
		}

		h.releaseLocked(name)
	}
}

// countedNames is the names one subscription occupies a place under.
func countedNames(principal, holder string) []string {
	if holder == "" || holder == principal {
		return []string{principal}
	}

	return []string{principal, holder}
}

func (h *Hub) release(principal, holder string) {
	h.submu.Lock()
	defer h.submu.Unlock()

	for _, name := range countedNames(principal, holder) {
		h.releaseLocked(name)
	}
}

func (h *Hub) releaseLocked(principal string) {
	remaining := h.held[principal] - 1
	if remaining <= 0 {
		delete(h.held, principal)

		return
	}

	h.held[principal] = remaining
}

func (p Publication) validate() error {
	// The type reaches an SSE event field and a messaging subject verbatim, and
	// the namespace this project owns is closed. Both halves live in sdk/event,
	// so the hub and the envelope cannot disagree about what a type may be.
	if badType := event.ValidateType(p.Type); badType != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, badType)
	}

	switch {
	case p.Origin.IsZero():
		return fmt.Errorf("%w: origin is empty", ErrInvalidRequest)
	case !p.Class.Valid():
		return fmt.Errorf("%w: evclass %q", ErrInvalidRequest, p.Class)
	case p.TTL != nil && p.Class != event.ClassCommand:
		return fmt.Errorf("%w: ttl on class %q, commands only", ErrInvalidRequest, p.Class)

	// Checked before the event takes a sequence number: a number issued to an
	// event that never ships is a hole, and a hole is a lost event to a
	// consumer's gap detector.
	case len(p.Data) > 0 && !json.Valid(p.Data):
		return fmt.Errorf("%w: data is not json", ErrInvalidRequest)
	}

	return nil
}

func (p Publication) conflationKey() string {
	if p.Key != "" {
		return p.Key
	}

	return p.Type
}

func (r SubscribeRequest) validate() error {
	switch {
	case r.Principal == "":
		return fmt.Errorf("%w: principal is empty", ErrInvalidRequest)
	case !r.Encoding.Valid():
		return fmt.Errorf("%w: encoding %d", ErrInvalidRequest, r.Encoding)
	}

	return nil
}

// floorOf is the generation a replay of one origin starts at.
//
// The grant's own generation is the answer whenever the authorizer reports one,
// so a reader's floor moves when its own grant moves and not when anybody
// else's does. An authorizer that reports none leaves the stamp on the whole
// answer as the bound. The stamp is never lower than the grant's generation,
// so it leaks nothing, but it can skip events the reader was allowed to see.
func floorOf(since, stamp uint64) uint64 {
	if since == 0 {
		return stamp
	}

	return since
}

// typeSet turns the reader's type narrowing into the set deliver probes. An
// empty request is a nil set, which means every type. deliver checks the
// length first, so an unnarrowed subscription pays for no map probe on the
// publish path.
func typeSet(requested []string) map[string]struct{} {
	if len(requested) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		set[name] = struct{}{}
	}

	return set
}

// selectOrigins intersects what was asked for with what is permitted.
//
// A principal who may see nothing and named no origin gets an empty stream,
// which widens when a grant arrives. It is not refused. A page opened before
// the first printer has connected needs the stream to see that arrival, and a
// refusal would leave it retrying. A caller who named an origin they may not
// see is refused, because a stream that leaves the origin out would hide from
// the client that it is missing a printer.
//
// Nothing here defaults, widens by kind or falls back. An origin is attached
// only because the authorizer named that exact origin.
func selectOrigins(requested []event.Origin, permitted []Grant) ([]Grant, error) {
	asked := permitted

	if len(requested) > 0 {
		// The whole grant is carried through, Audit included. A grant rebuilt
		// from the origin and its generation would leave Audit false, and a
		// reader that named its origins would lose the audit class it holds
		// view_audit for, on the live path and on the replay path.
		allowed := make(map[event.Origin]Grant, len(permitted))
		for _, grant := range permitted {
			allowed[grant.Origin] = grant
		}

		asked = make([]Grant, 0, len(requested))

		for _, origin := range requested {
			grant, ok := allowed[origin]
			if !ok {
				return nil, ErrNotPermitted
			}

			asked = append(asked, grant)
		}
	}

	out := make([]Grant, 0, len(asked))
	seen := make(map[event.Origin]struct{}, len(asked))

	for _, grant := range asked {
		// An origin named twice would attach the subscriber to one channel
		// twice, and every event for it would arrive twice.
		if _, duplicate := seen[grant.Origin]; duplicate {
			continue
		}

		seen[grant.Origin] = struct{}{}
		out = append(out, grant)
	}

	return out, nil
}

// clampInterval holds a coalescing window between MinInterval and MaxInterval.
// The transport refuses a value outside the band so the caller is told. The
// clamp keeps a hub embedded by any other caller inside the band too.
func clampInterval(value time.Duration) time.Duration {
	return min(max(value, MinInterval), MaxInterval)
}

func orDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}

	return value
}

func orInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}

	return value
}
