package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// statePath is the node of the addressable state tree that carries a printer's
// state machine value. It is the one telemetry path with a typed fact behind
// it: see the mapping table in this file's doc for sinkFeed.
const statePath = "/state"

// sinkRetryInterval is how long the feed waits before looking at the supervision
// plane again. It matches the interval an idle subscription re-reads the
// authorization epoch at, because both cover the same failure: an
// authorization read that could not be answered.
const sinkRetryInterval = stream.AuthorizeInterval

// sinkPrincipal renders one supervised instance's identity in the
// authorization graph.
//
// It is built from the instance name and not from the kind, and every module
// reads the graph through it too. So `sink: {audit: ...}` and
// `link: {audit: ...}` would be one principal with one set of grants. The two
// cannot both run: Host.claimName in internal/app/resourcehost/modules.go
// refuses to start an instance whose name another kind is running under.
//
// A sink is a principal of its own type and not a person. It holds exactly the
// grants an operator wrote for it, and a sink that was granted nothing
// receives nothing.
//
// The id is the instance's own name, so `sink: {audit: ...}` reads events as
// `sink:audit`. The type is `sink` and not `service_account`. Under
// `service_account` a sink and a service account an operator named `audit`
// would be one principal. A grant written for the service account would then
// feed the sink, and a grant written for the sink would be open to the
// service account's credential.
//
// No request can present this principal. graphPrincipals keys every id a
// credential resolves to under authz.TypeUser, so `sink:` is reachable only
// from here, for an instance the resource host supervises.
func sinkPrincipal(instance ref.Ref) string {
	return authz.TypeSink.String() + ":" + instance.ID()
}

// sinkFeed is the path from a domain event to every configured sink.
//
// A sink is a hub subscriber like any other reader. It is not fed from the
// publish path, because the hub already owns fan-out, the two queue classes,
// coalescing and the authorization filter.
//
// # What a sink can be told
//
// A sink is told through three doors.
//
// The typed door is sdk/eventsapi's five domain facts. One of them has a
// producer:
//
//	device.telemetry.sampled at /state -> PrinterStateChanged
//
// A printer reports its state in every telemetry frame, changed or not, and the
// hub publishes each one, because a sample is a measurement. A state change is
// not a measurement. Each sink is told a printer's state when it differs from
// the last one that sink was told, and the first time after it attaches.
//
// Nothing else maps to a typed fact. device.arrived, device.departed and
// device.presence.changed have no typed fact: PrinterDeleted means removed
// from the fleet, and a departure keeps the record. PrinterLabelsChanged,
// LabelDeleted and JobUpdated have no producer in this core.
//
// The generic door is FactObserved. Every durable frame goes through it under
// the envelope it was published with: the three command transitions, the
// arrivals and departures, the record changes, the continuity markers.
// Telemetry does not go through it. A sample is superseded and not kept, and a
// sink fed every sample would get telemetry without the hub's conflation.
//
// The third door is SamplesObserved, for a sink that implements
// eventsapi.SampleObserver. It carries the telemetry of a window after the hub
// has conflated it, one call per device, so the sink gets the newest value of
// each node once per window and not every sample. It also carries the whole
// tree of a device the first time the sink can read it. A sink that does not
// implement it is offered nothing.
type sinkFeed struct {
	hub *stream.Hub
	log *slog.Logger

	// trees reads one device's whole state tree. It is nil until the fleet
	// exists. See readTrees.
	trees atomic.Pointer[func(id string) (map[string]json.RawMessage, bool)]

	// wake carries one pending reconcile. It is depth one and never blocks,
	// because the sender is the supervisor's own status hook. A wake already
	// pending covers the reconcile a second one would ask for.
	wake chan struct{}
	done chan struct{}
}

// sinkStream is one sink's subscription and the goroutine draining it.
type sinkStream struct {
	instance *supervisor.Instance[any]
	sub      *stream.Subscription
	drained  chan struct{}
}

func newSinkFeed(hub *stream.Hub, logger *slog.Logger) *sinkFeed {
	return &sinkFeed{
		hub:  hub,
		log:  logger,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// observe is what the supervision plane's status hook calls. It runs on
// whichever goroutine caused the change, which for a stop is the goroutine a
// shutdown is waiting on, so it does no work beyond a non-blocking send.
func (f *sinkFeed) observe(status supervisor.Status) {
	if status.Ref.Kind() != ref.KindSink {
		return
	}

	f.signal()
}

func (f *sinkFeed) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// start drives the feed until ctx is cancelled.
func (f *sinkFeed) start(ctx context.Context, host *resourcehost.Host) {
	go f.run(ctx, host)
}

// wait blocks until the feed has stopped and every sink's subscription is
// closed.
func (f *sinkFeed) wait() { <-f.done }

func (f *sinkFeed) run(ctx context.Context, host *resourcehost.Host) {
	defer close(f.done)

	attached := map[ref.Ref]*sinkStream{}

	// What each unattached sink was last refused with, so a refusal that will
	// not change is reported once and not once per pass. The pass repeats on a
	// timer while anything is unattached, and a sink the host can never feed
	// is unattached for the life of the process.
	refused := map[ref.Ref]string{}

	defer func() {
		for instance, feeding := range attached {
			feeding.stop()
			delete(attached, instance)
		}
	}()

	// One timer for the life of the loop. Since Go 1.23 a stopped or reset timer
	// cannot deliver a stale value, so there is nothing to drain.
	retry := time.NewTimer(sinkRetryInterval)
	defer retry.Stop()
	retry.Stop()

	for {
		incomplete := f.reconcile(ctx, host, attached, refused)

		// A sink's subscription can end without a status change: the hub drops
		// a subscriber whose authorization it could not recompute, and the
		// instance behind it did not change state. So the pass repeats on a
		// timer while a sink is attached or one could not be attached. A
		// deployment that declares no sink arms no timer.
		if len(attached) > 0 || incomplete {
			retry.Reset(sinkRetryInterval)
		}

		select {
		case <-ctx.Done():
			return
		case <-f.wake:
		case <-retry.C:
		}
	}
}

// reconcile moves the attached set onto what the host is supervising, and
// reports whether it left anything unattached.
//
// It compares sets and does not act on the transition that woke it. A status
// change the hook could not deliver, because a reconcile was already pending,
// then costs nothing: the next pass reads the plane's state.
//
// refused is what each unattached sink was last refused with, carried across
// passes so that a refusal which will not change on its own is logged once. An
// instance the host has stopped supervising drops out of it, so a sink that is
// withdrawn and declared again is reported again.
func (f *sinkFeed) reconcile(
	ctx context.Context,
	host *resourcehost.Host,
	attached map[ref.Ref]*sinkStream,
	refused map[ref.Ref]string,
) bool {
	running := host.Supervising(ref.KindSink)

	for instance := range refused {
		if _, ok := running[instance]; !ok {
			delete(refused, instance)
		}
	}

	for instance, feeding := range attached {
		// A replace stops one instance and starts another under one name, so
		// the pointer says whether this is still the sink being fed. The other
		// reason to let go is a subscription the hub dropped after an
		// authorization read it could not answer. The drain goroutine has
		// returned by then, and only this check notices.
		current, ok := running[instance]
		if ok && current == feeding.instance && feeding.live() {
			continue
		}

		feeding.stop()
		delete(attached, instance)
	}

	incomplete := false

	for instance, runner := range running {
		if _, ok := attached[instance]; ok {
			continue
		}

		if moduleSink(host, instance) {
			continue
		}

		feeding, err := f.attach(ctx, instance, runner)
		if err != nil {
			incomplete = true

			// An instance that has not started yet is not reported: its own
			// status change wakes the next pass, which attaches it. A refusal
			// that will not change on its own is logged the first time.
			if !errors.Is(err, supervised.ErrNotServing) && refused[instance] != err.Error() {
				refused[instance] = err.Error()

				f.log.WarnContext(ctx, "a declared sink is receiving no events",
					"resource", instance.String(), "error", err.Error())
			}

			continue
		}

		delete(refused, instance)

		attached[instance] = feeding
	}

	return incomplete
}

// moduleSink reports whether a sink instance is a module: declared with
// runtime: service, which is how a module is installed. A module reads this
// server's events by subscribing to the event API itself, so the feed has
// nothing to hand it. It is skipped and not reported. The warning is for a
// sink that should receive events and cannot.
//
// It checks the declared runtime and not what the target serves. A service
// sink cannot take events from the host (see runtimesOfKind in
// internal/kernel/ref). If one ever can, this check keeps it off the bus
// silently and has to move to the target.
func moduleSink(host *resourcehost.Host, instance ref.Ref) bool {
	declared, ok := host.Declared(instance)

	return ok && declared.Spec.Runtime == install.RuntimeService
}

// attach subscribes one sink to the hub and starts the goroutine that drains it.
func (f *sinkFeed) attach(
	ctx context.Context, instance ref.Ref, runner *supervisor.Instance[any],
) (*sinkStream, error) {
	sink, err := supervised.AttachSink(runner)
	if err != nil {
		return nil, err
	}

	sub, err := f.hub.Subscribe(ctx, stream.SubscribeRequest{
		Principal: sinkPrincipal(instance),
		Encoding:  stream.EncodingJSON,
	})
	if err != nil {
		return nil, err
	}

	f.log.InfoContext(ctx, "sink attached to the event bus",
		"resource", instance.String(),
		"principal", sub.Principal(),
		"origins", len(sub.Origins()))

	feeding := &sinkStream{instance: runner, sub: sub, drained: make(chan struct{})}

	go func() {
		defer close(feeding.drained)

		f.drain(sub, sink)
	}()

	return feeding, nil
}

// drain forwards one sink's windows until its subscription ends. It is the only
// goroutine that touches this sink, and every call it makes goes through the
// instance's bounded queue, so a slow sink slows only itself.
//
// A sink that takes samples is also handed the whole tree of each device the
// first time it can read it, and again after a window that lost continuity.
// The hub attaches a subscription at the head of each channel, so without the
// whole tree a node that does not change would never reach the sink. The check
// runs after each window and on a timer, because a grant written for an idle
// printer brings no window with it.
func (f *sinkFeed) drain(sub *stream.Subscription, sink *supervised.Sink) {
	whole := map[event.Origin]bool{}
	told := changedStates{Processor: sink, states: map[string]string{}}

	check := time.NewTicker(sinkRetryInterval)
	defer check.Stop()

	for {
		select {
		case batch, open := <-sub.Batches():
			if !open {
				return
			}

			forwardWindow(sink, told, batch)

			if batch.Resync {
				clear(whole)
			}
		case <-check.C:
		}

		f.handWholeTrees(sub, sink, whole)
	}
}

// forwardWindow hands one window to a sink: each frame by the typed and the
// generic door, through told, and the telemetry by the samples door to a sink
// that takes it.
func forwardWindow(sink *supervised.Sink, told changedStates, batch stream.Batch) {
	for _, frame := range batch.Telemetry {
		forwardToSink(told, frame)
	}

	if sink.TakesSamples() {
		for _, samples := range sinkSamplesOf(batch.Telemetry) {
			sink.SamplesObserved(samples)
		}
	}

	for _, frame := range batch.Events {
		forwardToSink(told, frame)
	}
}

// changedStates passes a printer's state on to one sink only when it differs
// from the last state that sink was told. See sinkFeed's doc.
type changedStates struct {
	eventsapi.Processor

	// states is the last state told, by printer id. It is the drain
	// goroutine's own, so it needs no lock.
	states map[string]string
}

func (c changedStates) PrinterStateChanged(state eventsapi.PrinterState) {
	if last, told := c.states[state.ID]; told && last == state.State {
		return
	}

	c.states[state.ID] = state.State
	c.Processor.PrinterStateChanged(state)
}

// treeOf is a copy of the state tree of a printer whose session is live, for
// the sink feed to hand a sink that takes samples.
func (d *devices) treeOf(id string) (map[string]json.RawMessage, bool) {
	entry, ok := d.entry(id)
	if !ok {
		return nil, false
	}

	return entry.snapshot(), true
}

// handWholeTrees hands a sink that takes samples the whole tree of each device
// it can read and has not been handed yet. handed is what it was given. A
// device the sink has lost sight of drops out of it, so a grant written again
// hands the tree again.
func (f *sinkFeed) handWholeTrees(sub *stream.Subscription, sink *supervised.Sink, handed map[event.Origin]bool) {
	read := f.trees.Load()
	if read == nil || !sink.TakesSamples() {
		return
	}

	visible := sub.Origins()

	maps.DeleteFunc(handed, func(origin event.Origin, _ bool) bool { return !slices.Contains(visible, origin) })

	for _, origin := range visible {
		if origin.Kind() != event.KindDevice || handed[origin] {
			continue
		}

		tree, known := (*read)(origin.ID())
		if !known {
			continue
		}

		sink.SamplesObserved(wholeTree(origin.ID(), tree))

		handed[origin] = true
	}
}

// readTrees gives the feed the device fleet's trees. The fleet is built after
// the plane, so the feed is told where to read once the fleet exists, and
// hands no whole tree before.
func (f *sinkFeed) readTrees(read func(id string) (map[string]json.RawMessage, bool)) {
	f.trees.Store(&read)
}

// stop ends one sink's subscription and waits for its goroutine.
func (s *sinkStream) stop() {
	s.sub.Close()
	<-s.drained
}

// live reports whether the subscription is still delivering. The hub drops a
// subscriber whose authorization it could not recompute, and the reader is told
// by its channel closing, so this is the only signal that one is gone.
func (s *sinkStream) live() bool {
	select {
	case <-s.drained:
		return false
	default:
		return true
	}
}

// sampleFrame is the payload of a telemetry frame, read back out of the envelope the
// hub marshalled once for every subscriber. Only the data member is decoded:
// the type and the origin are already on the frame.
type sampleFrame struct {
	Data struct {
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"data"`
}

// sinkFactFrame is the envelope of a durable frame, read back out of the bytes
// the hub marshalled once for every subscriber. Only the members a sink's fact
// carries are decoded: the type, the class and the origin are already on the
// frame.
//
// It is not history.go's factFrame, which decodes the same envelope for the
// history feed. That one carries no causation id. A sink needs it, because the
// causation id joins a command's three frames into one invocation. The two
// structs stay apart so a change for one feed does not change the other.
type sinkFactFrame struct {
	Time        time.Time       `json:"time"`
	Sequence    string          `json:"sequence"`
	Epoch       string          `json:"epoch"`
	CausationID string          `json:"causationid"`
	Data        json.RawMessage `json:"data"`
}

// forwardToSink hands one frame to a sink, by both doors: the typed fact for
// the one telemetry path that maps to one, and the generic fact for every
// durable frame. See sinkFeed's doc for which typed facts exist and which
// do not.
//
// The origin check comes first and covers both doors. It stops a script that
// emits from triggering itself. A script publishes under the ref of the
// instance that ran it, so a sink's event carries a sink origin and reaches no
// sink, and no cycle of any length can form. A federated loop is refused the
// same way at the link, with Envelope.Hops as a second guard. The check covers
// the whole function and not one branch, because a sink instance changing
// state is a durable frame under a sink origin, and forwarding it would feed
// every sink its own lifecycle.
//
// A link origin passes as well as a device origin. The decisions a person makes
// about the servers under this site are published under the link instance, and
// a webhook that records who approved which server can read them only here. A
// link publishes nothing a sink could feed back to it, so no cycle can form
// through one.
func forwardToSink(sink eventsapi.Processor, frame stream.Frame) {
	switch frame.Position.Origin.Kind() {
	case event.KindDevice, event.Kind(ref.KindLink):
	default:
		return
	}

	// Both doors are offered the frame and each one checks it for itself. The
	// typed door answers to a type and the generic door to a class, so a frame
	// that has a typed fact is still a generic fact when it is durable.
	forwardPrinterState(sink, frame)
	forwardFact(sink, frame)
}

// forwardPrinterState is the telemetry path that has a typed fact.
func forwardPrinterState(sink eventsapi.Processor, frame stream.Frame) {
	if frame.Type != event.TypeDeviceTelemetrySampled {
		return
	}

	var decoded sampleFrame
	if err := json.Unmarshal(frame.Bytes, &decoded); err != nil {
		return
	}

	if decoded.Data.Path != statePath {
		return
	}

	var state string
	if err := json.Unmarshal(decoded.Data.Value, &state); err != nil {
		return
	}

	sink.PrinterStateChanged(eventsapi.PrinterState{
		ID:    frame.Position.Origin.ID(),
		State: state,
	})
}

// sinkSamplesOf groups the telemetry frames of one window by device, in the
// order each device first appears, with each device's nodes in path order. A
// frame that is not a device sample, or that cannot be read, is left out.
func sinkSamplesOf(frames []stream.Frame) []eventsapi.Samples {
	var (
		out   []eventsapi.Samples
		index map[string]int
	)

	for _, frame := range frames {
		if frame.Type != event.TypeDeviceTelemetrySampled || frame.Position.Origin.Kind() != event.KindDevice {
			continue
		}

		var decoded sampleFrame
		if err := json.Unmarshal(frame.Bytes, &decoded); err != nil || decoded.Data.Path == "" {
			continue
		}

		if index == nil {
			index = map[string]int{}
		}

		id := frame.Position.Origin.ID()

		at, seen := index[id]
		if !seen {
			at = len(out)
			index[id] = at
			out = append(out, eventsapi.Samples{DeviceID: id})
		}

		out[at].Nodes = append(out[at].Nodes, eventsapi.Sample{Path: decoded.Data.Path, Value: decoded.Data.Value})
	}

	for i := range out {
		slices.SortStableFunc(out[i].Nodes, func(a, b eventsapi.Sample) int { return strings.Compare(a.Path, b.Path) })
	}

	return out
}

// wholeTree is a device's whole state tree as one Samples, with Whole set.
func wholeTree(id string, tree map[string]json.RawMessage) eventsapi.Samples {
	out := eventsapi.Samples{DeviceID: id, Whole: true, Nodes: make([]eventsapi.Sample, 0, len(tree))}

	for _, path := range slices.Sorted(maps.Keys(tree)) {
		out.Nodes = append(out.Nodes, eventsapi.Sample{Path: path, Value: tree[path]})
	}

	return out
}

// forwardFact hands one durable frame on under its own envelope.
//
// A frame whose envelope carries no valid sequence is dropped, and not
// forwarded with a sequence of zero. Origin, epoch and sequence are the dedup
// key a sink is given. With zero on every frame, a sink that deduplicates on
// the key would keep one frame per origin and discard the rest.
func forwardFact(sink eventsapi.Processor, frame stream.Frame) {
	// An unset class is neither durable nor telemetry. The hub fills the class
	// in from the envelope on every frame it publishes, so this guard only
	// stops a frame built by hand from becoming a durable fact.
	if !frame.Class.Valid() || !frame.Class.Durable() {
		return
	}

	var decoded sinkFactFrame
	if err := json.Unmarshal(frame.Bytes, &decoded); err != nil {
		return
	}

	sequence, err := event.ParseSequence(decoded.Sequence)
	if err != nil {
		return
	}

	sink.FactObserved(eventsapi.Fact{
		Origin:      frame.Position.Origin,
		Type:        frame.Type,
		Class:       frame.Class,
		At:          decoded.Time,
		Sequence:    sequence,
		Epoch:       decoded.Epoch,
		CausationID: decoded.CausationID,
		Payload:     decoded.Data,
	})
}
