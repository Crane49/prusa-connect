package stream

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// Batch is one coalescing window's worth of output for one subscriber. Every
// byte slice in it is shared with every other subscriber on the same encoding
// and must not be modified.
type Batch struct {
	// Telemetry holds the newest value of each key that changed during the
	// window. The queue keeps only the newest value, so no superseded value
	// is in it.
	Telemetry []Frame
	// Events holds the durable classes in publication order.
	Events []Frame
	// Resync reports that continuity is gone: the event queue overflowed, or a
	// resume could not be honoured. The reader reloads state from the API and
	// carries on from this batch's cursor. There is no partial recovery.
	Resync bool
	// Withdrawn names the origins this reader followed and may no longer see,
	// either lost while it was reading or named in the cursor it resumed from.
	// Whatever was queued for them is already gone.
	Withdrawn []event.Origin
	// Cursor is the place to resume from after this batch, one offset per
	// origin this subscription follows.
	Cursor event.Cursor
}

// Stats reports what a subscriber is holding. The telemetry lane is bounded by
// the number of distinct keys a subscriber can see, not by how far behind its
// reader is.
type Stats struct {
	TelemetryKeys       int
	TelemetryBytes      int
	TelemetrySuperseded uint64
	TelemetryDropped    uint64
	EventCount          int
	EventBytes          int
	EventDropped        uint64
	Resync              bool
}

// Subscription is one reader's view of the hub: the channels it was authorized
// to see when it subscribed, two queues with different loss rules, and a pump
// that coalesces both onto one channel.
//
// The pump is the only goroutine that can block on a slow reader. A publisher
// never does.
type Subscription struct {
	hub       *Hub
	log       *slog.Logger
	id        uint64
	principal string
	// holder is the second name this subscription is counted against, and empty
	// when there is none. It bounds nothing else: what the reader may see is
	// the principal's own answer.
	holder string
	// values is the context the subscription was opened under, without its
	// deadline and its cancellation. A re-authorization runs under it, so the
	// question it asks carries what the first one carried, such as the scopes
	// of the reader's credential, and cannot come back wider.
	values   context.Context
	encoding Encoding
	interval time.Duration
	// requested is the narrowing the reader asked for, kept because a
	// re-authorization has to apply it again: a grant that appears later must
	// not widen a subscription the reader deliberately narrowed.
	requested []event.Origin
	// types is the reader's type narrowing, empty for every type. It is written
	// once, before any publisher can reach this subscription, and only read
	// after, so it needs no atomic and no lock.
	//
	// It is a preference and never an authorization: origins decide what a
	// reader may see, and this decides what of that it wants. A type nothing
	// publishes matches nothing and is not refused, so a client may name a
	// type a later release adds.
	types map[string]struct{}

	// channels is the routing table and allowed is the same decision as a
	// lookup. Both are replaced whole by the pump and read by publishers, so
	// both are atomic pointers.
	channels atomic.Pointer[[]*channel]
	// The value is whether this principal may see the audit class of that
	// origin, so the one probe deliver already pays answers both questions.
	allowed atomic.Pointer[map[event.Origin]bool]

	// lossless picks which telemetry lane this subscription is fed on. It is
	// written once, before any publisher can reach the subscription, and only
	// read after, so it needs no atomic and no lock.
	lossless bool

	// flush asks the pump for one last batch on the way out, carrying the
	// window it is holding. It is written once beside lossless and read only
	// by the pump, so it needs no atomic either.
	flush bool

	telemetry telemetryQueue
	series    seriesQueue
	events    eventQueue

	out    chan Batch
	wake   chan struct{}
	done   chan struct{}
	closed sync.Once

	// positions is owned by the pump. cursor holds the immutable snapshot any
	// other goroutine reads.
	positions map[event.Origin]uint64
	cursor    atomic.Pointer[event.Cursor]
	// withdrawn is what the next batch tells the reader it lost. drain is not
	// the pump's alone, so withdrawnMu guards it the way each lane guards its
	// queue.
	withdrawnMu sync.Mutex
	withdrawn   []event.Origin
	// aclEpoch is the generation of the authorization graph this subscription
	// was last authorized against. The pump compares it with the authorizer's
	// current epoch on every tick, so a later generation is noticed within one
	// coalescing window rather than at the next reconnect.
	aclEpoch atomic.Uint64

	// reportedDrops and lastLossReport are owned by the pump. They limit the
	// report of telemetry loss to once per telemetryLossInterval.
	reportedDrops  uint64
	lastLossReport time.Time
}

// telemetryLossInterval bounds how often one subscription reports its telemetry
// lane dropping. The first loss is reported at once and the rest are summarised:
// a queue that is over its bound is over it five times a second, and a line per
// window would bury the first one.
const telemetryLossInterval = 30 * time.Second

// Batches is the stream of coalesced output. It closes when the subscription is
// closed, or when the hub is.
func (s *Subscription) Batches() <-chan Batch { return s.out }

// Cursor returns the place a reconnect should resume from.
func (s *Subscription) Cursor() event.Cursor {
	if c := s.cursor.Load(); c != nil {
		return *c
	}

	return event.Cursor{}
}

// Origins returns the origins this subscription is attached to. That set is
// the authorization decision in force.
func (s *Subscription) Origins() []event.Origin {
	channels := s.routing()

	out := make([]event.Origin, 0, len(channels))
	for _, ch := range channels {
		out = append(out, ch.origin)
	}

	return out
}

// routing returns the channels this subscription is attached to. The caller
// must not modify the slice.
func (s *Subscription) routing() []*channel {
	if channels := s.channels.Load(); channels != nil {
		return *channels
	}

	return nil
}

// setRouting publishes a new routing table and the lookup that mirrors it. The
// lookup is stored first, so a publisher that raced the detach is refused by it
// before the queues are swept.
func (s *Subscription) setRouting(channels []*channel, grants []Grant) {
	allowed := make(map[event.Origin]bool, len(grants))
	for _, grant := range grants {
		allowed[grant.Origin] = grant.Audit
	}

	s.allowed.Store(&allowed)
	s.channels.Store(&channels)
}

// ACLEpoch returns the generation of the authorization graph this subscription
// was last authorized against.
func (s *Subscription) ACLEpoch() uint64 { return s.aclEpoch.Load() }

// Principal names who is reading.
func (s *Subscription) Principal() string { return s.principal }

// Interval is the coalescing window this subscription was given: what the
// request asked for, held between MinInterval and MaxInterval.
func (s *Subscription) Interval() time.Duration { return s.interval }

// Stats reports queue occupancy.
func (s *Subscription) Stats() Stats {
	keys, telemetryBytes, superseded, telemetryDropped, telemetryResync := s.telemetryStats()
	count, eventBytes, eventDropped, resync := s.events.stats()

	return Stats{
		TelemetryKeys:       keys,
		TelemetryBytes:      telemetryBytes,
		TelemetrySuperseded: superseded,
		TelemetryDropped:    telemetryDropped,
		EventCount:          count,
		EventBytes:          eventBytes,
		EventDropped:        eventDropped,
		Resync:              resync || telemetryResync,
	}
}

// telemetryStats reads whichever telemetry lane this subscription is fed on, so
// occupancy and loss are reported the same way for both. A lossless lane
// supersedes nothing, so it counts held frames where the conflating one counts
// distinct keys and reports no supersessions.
func (s *Subscription) telemetryStats() (held, bytes int, superseded, dropped uint64, resync bool) {
	if s.lossless {
		held, bytes, dropped, resync = s.series.stats()

		return held, bytes, 0, dropped, resync
	}

	held, bytes, superseded, dropped = s.telemetry.stats()

	return held, bytes, superseded, dropped, false
}

// telemetryLimit is the byte bound on the lane in use. Both lanes are given the
// same one: what differs is whether a subscriber can reach it.
func (s *Subscription) telemetryLimit() int {
	if s.lossless {
		return s.series.maxBytes
	}

	return s.telemetry.maxBytes
}

// Close detaches the subscription from every channel and stops its pump. It is
// safe to call more than once, and safe to call while a batch is in flight.
//
// The detach happens before the pump is told to stop. A FlushOnClose
// subscription depends on that order for a complete last batch. Once detach
// has returned, no publisher can reach this subscription's queues, so what the
// pump then drains is everything the reader is owed. In the other order a
// frame delivered between the two steps would be queued after the last drain
// and lost.
func (s *Subscription) Close() {
	s.closed.Do(func() {
		s.hub.detach(s)
		close(s.done)
	})
}

// finalBatchBudget bounds the last batch of a FlushOnClose subscription. Its
// reader is draining until the channel closes, so the budget is only there so
// that a reader which stopped early cannot hold a shutdown open.
const finalBatchBudget = 2 * time.Second

// deliver queues a message. It is called on the publish path, so it must not
// block: both queues are bounded and both answer immediately.
//
// The lookup repeats the check that channel membership already makes. It costs
// a map probe and no allocation, and it closes the window where a publisher
// loaded the subscriber slice just before a grant was withdrawn.
func (s *Subscription) deliver(m *Message) {
	allowed := s.allowed.Load()
	if allowed == nil {
		return
	}

	audit, permitted := (*allowed)[m.position.Origin]
	if !permitted {
		return
	}

	// The audit narrowing. It costs nothing beyond the probe the origin check
	// already made. It is recomputed with the origins on every epoch move, so
	// a long-lived stream cannot outlive its grant.
	if m.env.Class == event.ClassAudit && !audit {
		return
	}

	// Before the queues, so a narrowed reader spends none of its bounded queue
	// on an event it asked not to receive, and the durable lane's offsets never
	// see it. The set is per subscription and probed by value, so this costs one
	// map lookup and no allocation on the publish path.
	if !s.admits(m.env.Type) {
		return
	}

	switch m.env.Class {
	case event.ClassTelemetry:
		if s.lossless {
			s.series.put(m)
		} else {
			s.telemetry.put(m)
		}
	case event.ClassState, event.ClassCommand, event.ClassAudit:
		s.events.put(m)
	}

	s.signal()
}

// admits reports whether the reader asked for this type, which an unnarrowed
// reader did for every type.
func (s *Subscription) admits(eventType string) bool {
	if len(s.types) == 0 {
		return true
	}

	_, wanted := s.types[eventType]

	return wanted
}

// withdraw tells the reader at its next window that it may no longer see
// these origins. It runs on the pump, or before the pump starts.
func (s *Subscription) withdraw(origins ...event.Origin) {
	if !s.admits(event.TypeStreamOriginWithdrawn) {
		return
	}

	s.withdrawnMu.Lock()
	for _, origin := range origins {
		if !slices.Contains(s.withdrawn, origin) {
			s.withdrawn = append(s.withdrawn, origin)
		}
	}
	s.withdrawnMu.Unlock()

	s.signal()
}

// signal wakes the pump without blocking a publisher. A wake already pending is
// one that has not been acted on, so dropping this one loses nothing.
func (s *Subscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// pump coalesces. Everything published during one interval leaves as one batch,
// so a subscriber gets at most ten batches a second however many frames a
// printer sends.
func (s *Subscription) pump() {
	defer close(s.out)
	defer s.finalBatch()

	// One timer for the life of the pump. Since Go 1.23 a stopped or reset
	// timer cannot deliver a stale value, so there is nothing to drain.
	timer := time.NewTimer(s.interval)
	defer timer.Stop()
	timer.Stop()

	// A stream with no traffic still has to notice a grant being withdrawn. The
	// delivery path cannot carry that check on its own, because a subscriber
	// that is being delivered nothing never reaches it.
	idle := time.NewTicker(AuthorizeInterval)
	defer idle.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		case <-idle.C:
			if !s.reauthorize() {
				return
			}

			continue
		}

		timer.Reset(s.interval)

		select {
		case <-s.done:
			return
		case <-timer.C:
		}

		// Before anything leaves, and before the cursor moves over it.
		if !s.reauthorize() {
			return
		}

		// Read before the drain empties the queue, so the occupancy in the line
		// is the occupancy that was over budget.
		s.reportTelemetryLoss(time.Now())

		batch, ready := s.drain()
		if !ready {
			continue
		}

		if batch.Resync {
			_, _, _, telemetryDropped, _ := s.telemetryStats()

			s.log.Warn("stream subscriber lost continuity",
				"principal", s.principal,
				"dropped", s.events.dropCount()+telemetryDropped,
			)
		}

		select {
		case <-s.done:
			return
		case s.out <- batch:
		}
	}
}

// finalBatch hands the reader whatever the last coalescing window was holding,
// for a subscription that asked for it.
//
// It runs on the pump, after the loop and before the output channel closes, so
// the reader sees it as an ordinary batch followed by the close it was already
// waiting for. It does not run for a subscription whose pump ended because its
// grants were withdrawn: this principal may not see those frames.
//
// It reauthorizes once first. A grant made while the pump was idle is applied
// on the authorize tick, once a second, and applying it replays the window the
// reader was not yet allowed to see. A store subscribed to a fleet that is
// powering up is such a reader. Without this step a shutdown inside that
// second would leave the arrival of a printer, and every command sent to it,
// unread in the channel history.
func (s *Subscription) finalBatch() {
	if !s.flush {
		return
	}

	select {
	case <-s.done:
	default:
		return
	}

	if !s.reauthorize() {
		return
	}

	batch, ready := s.drain()
	if !ready {
		return
	}

	timer := time.NewTimer(finalBatchBudget)
	defer timer.Stop()

	select {
	case s.out <- batch:
	case <-timer.C:
		s.log.Warn("a closing stream subscriber did not take its last batch",
			"principal", s.principal,
			"events", len(batch.Events), "telemetry", len(batch.Telemetry))
	}
}

// reportTelemetryLoss says that this reader is not being given everything it
// may see. Conflation alone drops nothing a reader could notice, so a count
// here means the byte bound dropped values: the stream looks healthy and its
// values are older than the reader thinks.
// On a lossless lane the same count is readings the reader will never be given,
// and there the batch's Resync flag says so as well.
//
// It runs on the pump, which owns reportedDrops and lastLossReport.
func (s *Subscription) reportTelemetryLoss(now time.Time) {
	keys, held, _, dropped, _ := s.telemetryStats()
	if dropped == s.reportedDrops {
		return
	}

	if !s.lastLossReport.IsZero() && now.Sub(s.lastLossReport) < telemetryLossInterval {
		return
	}

	s.log.Warn("stream subscriber is not being given all the telemetry it may see",
		"principal", s.principal,
		"origins", len(s.routing()),
		"dropped", dropped,
		"dropped_since_reported", dropped-s.reportedDrops,
		"keys_held", keys,
		"bytes_held", held,
		"bytes_limit", s.telemetryLimit(),
	)

	s.reportedDrops = dropped
	s.lastLossReport = now
}

func (s *Subscription) drain() (Batch, bool) {
	// One scratch slice for both lanes: frames copies out of it before the
	// second drain refills it.
	var (
		buf    []*Message
		resync bool
	)

	if s.lossless {
		buf, resync = s.series.drain(nil)
	} else {
		buf = s.telemetry.drain(nil)
	}

	telemetry := s.frames(buf)

	buf, eventsLost := s.events.drain(buf[:0])
	events := s.frames(buf)
	// Either lane losing continuity is the reader losing it: one flag, because
	// there is no partial recovery to distinguish.
	resync = resync || eventsLost

	s.withdrawnMu.Lock()
	withdrawn := s.withdrawn
	s.withdrawn = nil
	s.withdrawnMu.Unlock()

	if len(telemetry) == 0 && len(events) == 0 && len(withdrawn) == 0 && !resync {
		return Batch{}, false
	}

	return Batch{
		Telemetry: telemetry,
		Events:    events,
		Resync:    resync,
		Withdrawn: withdrawn,
		// Only the durable lane moves the cursor to the head. A telemetry
		// overflow costs readings and no offsets, so fast-forwarding over
		// events this reader has not been handed would turn one lane's loss
		// into the other's.
		Cursor: s.advance(events, eventsLost),
	}, true
}

// advance moves the cursor over the events in this batch. A resync moves it to
// the head instead: continuity has been declared gone, so the reader reloads
// state from the API, and handing it back a place behind what was discarded
// would replay events the reload already reflects.
func (s *Subscription) advance(events []Frame, resync bool) event.Cursor {
	moved := false

	for _, frame := range events {
		if frame.Position.Offset > s.positions[frame.Position.Origin] {
			s.positions[frame.Position.Origin] = frame.Position.Offset
			moved = true
		}
	}

	if resync {
		for _, ch := range s.routing() {
			s.positions[ch.origin] = ch.hist.position()
		}

		moved = true
	}

	// A telemetry-only window moves nothing, and re-rendering an unchanged
	// cursor would allocate on every tick for every subscriber.
	if !moved {
		return s.Cursor()
	}

	return s.snapshot()
}

func (s *Subscription) snapshot() event.Cursor {
	positions := make([]event.Position, 0, len(s.positions))

	for origin, offset := range s.positions {
		// An origin at offset zero is one this reader has seen nothing from,
		// which is what a cursor that does not name it already says.
		if offset == 0 {
			continue
		}

		positions = append(positions, event.Position{Origin: origin, Offset: offset})
	}

	// One generation, because every offset in it was assigned by this hub. A
	// reader following several nodes merges one of these per node. The slice is
	// built here rather than through NewCursor's variadic form, which would
	// allocate a second one on a path every subscriber walks.
	cursor := event.Cursor{
		Generations: []event.Generation{event.NewGeneration(s.hub.Epoch(), positions)},
	}
	s.cursor.Store(&cursor)

	return cursor
}

func (s *Subscription) frames(messages []*Message) []Frame {
	if len(messages) == 0 {
		return nil
	}

	out := make([]Frame, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.frame(s.encoding))
	}

	return out
}

// reauthorize re-runs the authorization decision when the graph has moved, and
// reports whether the subscription survives it. It runs on the pump, before the
// window's batch is built, so a grant withdrawn between two windows takes
// effect on the first of them rather than at the reader's next reconnect.
//
// It never fails open. An answer the hub could not get is not an answer that
// says yes, so a refusal and an unreachable policy store both end the
// subscription and the reader is told by its channel closing.
func (s *Subscription) reauthorize() bool {
	previous := s.aclEpoch.Load()

	current := s.hub.authz.Epoch()
	if current == previous {
		return true
	}

	parent := s.values
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, s.hub.authorizeTimeout)
	defer cancel()

	visibility, err := s.hub.authz.VisibleOrigins(ctx, s.principal)
	if err != nil {
		s.log.Error("stream subscriber dropped: authorization could not be recomputed",
			"principal", s.principal,
			"error", err,
		)
		s.Close()

		return false
	}

	origins, err := selectOrigins(s.requested, visibility.Origins)
	if err != nil {
		s.log.Info("stream subscriber dropped: a grant it was reading on was withdrawn",
			"principal", s.principal,
		)
		s.Close()

		return false
	}

	s.applyVisibility(origins, previous, visibility.Epoch)
	s.aclEpoch.Store(visibility.Epoch)

	return true
}

// applyVisibility moves the subscription onto the origins it may now see, and
// replays each newly granted one from the generation its own grant began at: a
// grant is permission to read from now, never a licence to replay what happened
// before it was made.
//
// A resource that arrives while somebody is watching announces itself in one
// event and there is no later one that says it is here, so the window is
// replayed rather than skipped. What bounds it is a generation of the graph and
// not the age of the subscription: the write that admits a printer is the write
// that makes it visible, so its announcement carries that generation and
// everything a reader was not yet granted carries an older one.
//
// The bound is per origin, because the graph moves for reasons that have
// nothing to do with this reader. A site powering up writes once per machine,
// so a bound taken from the graph as it stands is several writes past the first
// machine's announcement and hands the reader none of them. previous is the
// floor under an origin the authorizer reported no generation for: the answer at
// the reader's last check did not carry that origin, so anything stamped at or
// below it predates the grant. stamp is that answer's own generation, which is
// the other end of the same fallback.
func (s *Subscription) applyVisibility(origins []Grant, previous, stamp uint64) {
	held := make(map[event.Origin]*channel, len(s.routing()))
	for _, ch := range s.routing() {
		held[ch.origin] = ch
	}

	var (
		channels = make([]*channel, 0, len(origins))
		replayed []*Message
		current  []*Message
	)

	for _, grant := range origins {
		origin := grant.Origin

		if ch, attached := held[origin]; attached {
			channels = append(channels, ch)
			delete(held, origin)

			continue
		}

		ch := s.hub.channel(origin)

		// Back in sight before the reader was told it left, so it is not told.
		s.withdrawnMu.Lock()
		s.withdrawn = slices.DeleteFunc(s.withdrawn, func(o event.Origin) bool { return o == origin })
		s.withdrawnMu.Unlock()

		// Attached before the window is read, so an event published in between
		// is queued rather than lost. adopt orders the replay ahead of it and
		// drops the copy it covers.
		ch.attach(s)
		channels = append(channels, ch)

		floor := max(previous+1, floorOf(grant.Since, stamp))

		missed, from := ch.hist.grantedSince(floor)
		s.positions[origin] = from
		s.events.expect(origin, from)
		replayed = append(replayed, admitted(missed, grant.Audit)...)

		// The telemetry this origin last reported, under the same authorization
		// floor the durable window takes. A conflating reader is shown the
		// machine at once rather than at its next sample; a lossless reader
		// wants the series and a value it may already hold would be a reading
		// counted twice, so it is given none.
		if !s.lossless {
			current = append(current, ch.last.since(floor)...)
		}
	}

	// held now holds what was withdrawn. The lookup is replaced before the
	// detach so a publisher already inside the fan-out is refused by it, and
	// the queues are swept after so anything it queued first is taken back.
	s.setRouting(channels, origins)

	if len(replayed) > 0 {
		s.events.adopt(replayed)
		s.signal()
	}

	if len(current) > 0 {
		s.telemetry.seed(current)
		s.signal()
	}

	if len(held) == 0 {
		return
	}

	lost := make(map[event.Origin]struct{}, len(held))
	gone := make([]event.Origin, 0, len(held))

	for origin, ch := range held {
		ch.detach(s)
		lost[origin] = struct{}{}
		gone = append(gone, origin)
		delete(s.positions, origin)
	}

	// What is already buffered is checked again, as the Mosquitto broker does.
	// The telemetry lane holds the newest value per key with no expiry, so a
	// value for a withdrawn origin would stay there until that origin published
	// again.
	s.telemetry.dropOrigins(lost)
	s.series.dropOrigins(lost)
	s.events.dropOrigins(lost)

	// The cursor stops naming what the reader may no longer see. A place in a
	// printer's stream is itself the answer to "does this printer exist".
	s.snapshot()
	s.reclaim(lost)

	// After the sweep, so nothing about the origin is handed over after the
	// reader is told it is gone.
	s.withdraw(gone...)

	s.log.Info("stream subscriber lost a grant mid-stream",
		"principal", s.principal,
		"withdrawn", len(lost),
	)
}

// reclaim takes back a window the reader has not collected yet and drops the
// frames it may no longer see. Sweeping the queues cannot reach it: it was
// built, and authorized, before the grant was withdrawn. Only the pump sends on
// this channel, so taking one batch out guarantees room to put it back.
func (s *Subscription) reclaim(lost map[event.Origin]struct{}) {
	var parked Batch

	select {
	case parked = <-s.out:
	default:
		return
	}

	parked.Telemetry = keepPermitted(parked.Telemetry, lost)
	parked.Events = keepPermitted(parked.Events, lost)
	parked.Cursor = s.Cursor()

	if len(parked.Telemetry) == 0 && len(parked.Events) == 0 && len(parked.Withdrawn) == 0 && !parked.Resync {
		return
	}

	select {
	case s.out <- parked:
	case <-s.done:
	}
}

func keepPermitted(frames []Frame, lost map[event.Origin]struct{}) []Frame {
	kept := frames[:0]

	for _, frame := range frames {
		if _, withdrawn := lost[frame.Position.Origin]; withdrawn {
			continue
		}

		kept = append(kept, frame)
	}

	return kept
}
