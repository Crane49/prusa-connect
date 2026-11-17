package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// How much one instance holds before it writes, and how long it waits.
//
// One transaction of 1,000 rows was measured at 78,951 rows per second on
// SQLite, against 23,336 at one row per transaction. The interval matches the
// device record's own flush, so a quiet deployment writes on one cadence and
// not on two.
const (
	historyFlushRows     = 1000
	historyFlushInterval = 2 * time.Second
)

// historyDrainBudget bounds the last write on the way out. The feed's context is
// already cancelled by then, so the final flush runs on a context of its own and
// a store that will not answer cannot hold the shutdown open.
const historyDrainBudget = 5 * time.Second

// The query verbs the read side resolves an instance per. They are the names an
// operator writes in a `reads:` list and the names an ambiguity refusal prints,
// so they are one table and not a string typed twice.
const (
	verbSamples  = "samples"
	verbFacts    = "facts"
	verbCommands = "commands"
)

// historyPrincipal renders one history instance's identity in the
// authorization graph.
//
// A provider is a principal of its own type and not the deployment's owner. It
// holds exactly the grants an operator wrote for it, and a provider that was
// granted nothing stores nothing. No request can present this principal:
// graphPrincipals keys every id a credential resolves to under authz.TypeUser,
// so `history:` is reachable only from here.
func historyPrincipal(instance ref.Ref) string {
	return authz.TypeHistory.String() + ":" + instance.ID()
}

// historyFeed is the path from the event bus into every serving history
// provider, and the read side's answer to which one serves a query.
//
// It is a reconciling feed with sinkFeed's shape, for the same reasons. The
// supervision plane is the source of truth. A status change the hook could not
// deliver costs nothing, because the feed compares sets and so survives a
// missed change.
//
// # What happens when a provider falls behind
//
// Nothing upstream waits. Each instance has one subscription and one goroutine,
// so a slow store slows its own subscription and nothing else, and the hub's
// own two bounds then decide what is lost:
//
//   - telemetry sits on the lossless FIFO lane, which this feed asks for and no
//     other subscriber does. A browser gets the newest value per key. A store
//     gets every reading, because a superseded reading is still a measurement
//     the fleet reported. The conflating lane would give the store one row per
//     key per coalescing window: twenty readings at 50 ms become five rows
//   - the lossless lane is bounded by bytes and not by the number of distinct
//     keys, so it can overflow. When it does it is emptied and the reader is
//     told. That arrives here as a resync and becomes a gap row, so a later
//     query over that window answers incomplete and not a shorter series
//   - state and audit sit on the FIFO queue, bounded by bytes. When it
//     overflows the reader is dropped back and told, and this feed writes a gap
//     row per origin so a later query over that window reports it incomplete
//
// There is no durable outbox here. A local reader that cannot write to its own
// disk is not helped by an unbounded queue on the same disk.
type historyFeed struct {
	hub   *stream.Hub
	log   *slog.Logger
	types *registry.Registry

	wake chan struct{}
	done chan struct{}

	// swept is the start-up pass that closes the commands the last process left
	// open. It runs once per process, on the first provider that folds
	// commands, because the question it asks is about the run before this one
	// and a second provider attaching does not make it true again.
	swept sync.Once

	// sweeps counts the start-up passes that are running: the one above, and
	// everything registered through whenStoring. Each writes straight into a
	// provider, so the feed waits for the count before it detaches the
	// providers they write into. See sweep.
	sweeps sync.WaitGroup

	// serving is what the read side resolves against. It is written by the
	// reconcile goroutine and read by every request, so it has a lock of its
	// own.
	mu      sync.RWMutex
	serving map[ref.Ref]*historyStream
	// arrivals is the queue of work that waits for a provider that keeps facts.
	// The transfer sweep is one such piece of work. It is under the same lock as
	// serving, because the question it answers is whether that map has ever
	// held such a provider.
	arrivals historyReadiness
}

// historyStream is one provider's subscription, the goroutine draining it, and
// what the declaration said that instance is for.
type historyStream struct {
	instance *supervisor.Instance[any]
	provider *supervised.History
	sub      *stream.Subscription
	drained  chan struct{}

	// own is this instance as the origin its own lifecycle is published under,
	// which is the one origin the feed does not store for it (see collect).
	own event.Origin

	// consumes is the declared class filter, read off the registration's
	// descriptor. Empty means the registration catches nothing, and the
	// published reference shows that.
	consumes []event.Class
	// reads is the verbs this instance answers, empty for one that claims every
	// verb its capabilities carry.
	reads []string
	// caps is what the provider said it serves, read once at attach.
	caps historyapi.Caps

	counts historyCounts
}

// historyCounts is one instance's own tally. Every one of them is a frame this
// process saw, so they answer questions no query over the store can: a value
// dropped is a row that is not there to count.
type historyCounts struct {
	samples atomic.Uint64
	facts   atomic.Uint64
	gaps    atomic.Uint64
	// untyped counts telemetry whose value is neither a number, a boolean nor a
	// word: an object, an array or a null at a state path. A sample carries a
	// number or a word, and this feed does not invent a rendering of somebody
	// else's document.
	untyped atomic.Uint64
	// clipped counts words that arrived longer than maxSampleText and were
	// stored short. A store filling with truncated filenames points at a
	// misconfigured state path, and nothing else reports that.
	clipped atomic.Uint64
	// failed counts batches the provider refused. The frames in a refused
	// batch are lost.
	failed atomic.Uint64
	// through counts frames written straight to this provider as they were
	// published, outside the feed's own batch. They are counted separately so
	// a log line can say how much of the record does not wait for the next
	// flush.
	through atomic.Uint64
}

func newHistoryFeed(hub *stream.Hub, types *registry.Registry, logger *slog.Logger) *historyFeed {
	return &historyFeed{
		hub:     hub,
		log:     logger,
		types:   types,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		serving: map[ref.Ref]*historyStream{},
	}
}

// observe is what the supervision plane's status hook calls. It runs on
// whichever goroutine caused the change, so it does no work beyond a
// non-blocking send.
func (f *historyFeed) observe(status supervisor.Status) {
	if status.Ref.Kind() != ref.KindHistory {
		return
	}

	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// start drives the feed until ctx is cancelled.
func (f *historyFeed) start(ctx context.Context, host *resourcehost.Host) {
	go f.run(ctx, host)
}

// wait blocks until the feed has stopped and every subscription is closed.
func (f *historyFeed) wait() { <-f.done }

func (f *historyFeed) run(ctx context.Context, host *resourcehost.Host) {
	defer close(f.done)

	refused := map[ref.Ref]string{}

	// The subscriptions are closed last. A start-up pass writes straight into a
	// provider, so detaching while one is running would drop the rows it was
	// about to write; each pass carries a deadline, so this wait has an end
	// whatever the store is doing.
	defer f.detachAll(ctx)
	defer f.sweeps.Wait()

	retry := time.NewTimer(sinkRetryInterval)
	defer retry.Stop()
	retry.Stop()

	for {
		incomplete := f.reconcile(ctx, host, refused)

		if f.attached() > 0 || incomplete {
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

func (f *historyFeed) attached() int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return len(f.serving)
}

// detachAll closes every subscription and waits for the last write of each.
func (f *historyFeed) detachAll(ctx context.Context) {
	f.mu.Lock()
	streams := f.serving
	f.serving = map[ref.Ref]*historyStream{}
	f.mu.Unlock()

	for instance, feeding := range streams {
		feeding.stop()

		f.log.InfoContext(ctx, "history provider detached",
			"resource", instance.String(),
			"samples", feeding.counts.samples.Load(), "facts", feeding.counts.facts.Load(),
			"gaps", feeding.counts.gaps.Load(), "untyped", feeding.counts.untyped.Load(),
			"clipped", feeding.counts.clipped.Load(),
			"written_through", feeding.counts.through.Load(),
			"failed_batches", feeding.counts.failed.Load())
	}
}

// reconcile moves the attached set onto what the host is supervising, and
// reports whether it left anything unattached. It compares sets and does not
// act on the transition that woke it, as sinkFeed does, so a missed wake
// loses nothing.
func (f *historyFeed) reconcile(
	ctx context.Context, host *resourcehost.Host, refused map[ref.Ref]string,
) bool {
	running := host.Supervising(ref.KindHistory)

	for instance := range refused {
		if _, ok := running[instance]; !ok {
			delete(refused, instance)
		}
	}

	f.dropStale(ctx, running)

	incomplete := false

	for instance, runner := range running {
		f.mu.RLock()
		_, held := f.serving[instance]
		f.mu.RUnlock()

		if held {
			continue
		}

		feeding, err := f.attach(ctx, host, instance, runner)
		if err != nil {
			incomplete = true

			// An instance that has not started yet is not something to report:
			// the next pass, which its own status change wakes, attaches it.
			if !errors.Is(err, supervised.ErrNotServing) && refused[instance] != err.Error() {
				refused[instance] = err.Error()

				f.log.WarnContext(ctx, "a declared history provider is storing nothing",
					"resource", instance.String(), "error", err.Error())
			}

			continue
		}

		delete(refused, instance)

		f.mu.Lock()
		f.serving[instance] = feeding
		f.mu.Unlock()
	}

	return incomplete
}

// dropStale lets go of every attached instance the host has replaced, stopped,
// or whose subscription the hub dropped.
func (f *historyFeed) dropStale(ctx context.Context, running map[ref.Ref]*supervisor.Instance[any]) {
	f.mu.Lock()

	var gone []*historyStream

	for instance, feeding := range f.serving {
		current, ok := running[instance]
		if ok && current == feeding.instance && feeding.live() {
			continue
		}

		gone = append(gone, feeding)

		delete(f.serving, instance)
	}

	f.mu.Unlock()

	for _, feeding := range gone {
		feeding.stop()

		f.log.InfoContext(ctx, "history provider detached",
			"resource", feeding.provider.Ref().String(),
			"samples", feeding.counts.samples.Load(), "facts", feeding.counts.facts.Load())
	}
}

// attach subscribes one provider to the hub and starts the goroutine that
// drains it.
func (f *historyFeed) attach(
	ctx context.Context, host *resourcehost.Host, instance ref.Ref, runner *supervisor.Instance[any],
) (*historyStream, error) {
	provider, err := supervised.AttachHistory(runner)
	if err != nil {
		return nil, err
	}

	consumes := f.consumedBy(runner.Type())

	sub, err := f.hub.Subscribe(ctx, stream.SubscribeRequest{
		Principal: historyPrincipal(instance),
		Encoding:  stream.EncodingJSON,
		// A store is asked later what the series was, so it needs every
		// reading and not only the newest value per key. Every other
		// subscriber renders the newest value and takes the conflated lane.
		Lossless: true,
		// The store also takes the last window when the subscription closes.
		// Without it, a transfer that settles just before a graceful shutdown
		// leaves a row that reads as still sending for ever.
		FlushOnClose: true,
	})
	if err != nil {
		return nil, err
	}

	// The origin is rendered the way announce renders it, so the comparison in
	// collect is on the same name the hook publishes under. A ref that will not
	// render leaves the zero origin, which matches no frame.
	own, _ := event.NewOrigin(event.Kind(instance.Kind()), instance.ID())

	feeding := &historyStream{
		instance: runner,
		provider: provider,
		sub:      sub,
		drained:  make(chan struct{}),
		own:      own,
		consumes: consumes,
		reads:    declaredReads(host, instance),
		caps:     provider.Caps(),
	}

	f.log.InfoContext(ctx, "history provider attached to the event bus",
		"resource", instance.String(),
		"principal", sub.Principal(),
		"origins", len(sub.Origins()),
		"consumes", classNames(consumes),
		"samples", feeding.caps.Has(historyapi.CapSamples),
		"facts", feeding.caps.Has(historyapi.CapFacts))

	// A provider that declares a class and holds no grant to read anything
	// stores nothing, and nothing else reports it. The log line names the
	// grant on the org that fixes it.
	if len(consumes) > 0 && len(sub.Origins()) == 0 {
		f.log.WarnContext(ctx, "a history provider is granted no origin and will store nothing",
			"resource", instance.String(),
			"principal", sub.Principal(),
			"consumes", classNames(consumes),
			"grant", "org:"+bootstrapOrg+"#viewer@"+sub.Principal())
	}

	go func() {
		defer close(feeding.drained)

		f.drain(ctx, feeding)
	}()

	// After the subscription and not before it. The sweep publishes a terminal
	// frame per open command, and the provider must be listening by then. The
	// frames would be stored anyway, because the sweep also writes them
	// directly, but the bus path would miss them.
	if feeding.caps.Has(historyapi.CapCommands) {
		f.swept.Do(func() {
			f.sweep(ctx, func(passing context.Context) {
				closeOpenCommands(passing, f.hub, provider, f.log)
			})
		})
	}

	// Run the work that waits for a store that keeps facts. The transfer
	// sweep is one: the boot sequence registers it before any provider is
	// attached, so it has to wait for this line.
	if feeding.caps.Has(historyapi.CapFacts) {
		f.storing(ctx)
	}

	return feeding, nil
}

// consumedBy is the class filter the registration declared, read off the
// published descriptor and not from the running instance.
func (f *historyFeed) consumedBy(typeName string) []event.Class {
	if f.types == nil {
		return nil
	}

	declared, err := f.types.Lookup(ref.KindHistory, typeName)
	if err != nil {
		return nil
	}

	return declared.Descriptor().Doc().Consumes
}

// declaredReads is the verbs an operator said this instance answers.
func declaredReads(host *resourcehost.Host, instance ref.Ref) []string {
	declared, ok := host.Declared(instance)
	if !ok {
		return nil
	}

	return declared.Spec.Reads
}

// classNames renders a declared filter for a log line.
func classNames(classes []event.Class) string {
	names := make([]string, 0, len(classes))
	for _, class := range classes {
		names = append(names, class.String())
	}

	return strings.Join(names, ",")
}

// stop ends one provider's subscription and waits for its goroutine.
func (s *historyStream) stop() {
	s.sub.Close()
	<-s.drained
}

// live reports whether the subscription is still delivering.
func (s *historyStream) live() bool {
	select {
	case <-s.drained:
		return false
	default:
		return true
	}
}

// answers reports whether this instance is the one to ask for a verb.
func (s *historyStream) answers(verb string) bool {
	switch verb {
	case verbSamples:
		if !s.caps.Has(historyapi.CapSamples) {
			return false
		}
	case verbFacts:
		if !s.caps.Has(historyapi.CapFacts) {
			return false
		}
	case verbCommands:
		if !s.caps.Has(historyapi.CapCommands) {
			return false
		}
	default:
		return false
	}

	if len(s.reads) == 0 {
		return true
	}

	return slices.Contains(s.reads, verb)
}

// reader resolves the one instance that answers a verb.
//
// Two serving instances that both claim the verb are refused, and the refusal
// names both. An operator may run one store for metrics and another for audit,
// and writes go to both. A read must not pick one of them silently.
func (f *historyFeed) reader(verb string) (historyapi.Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var (
		found  historyapi.Provider
		claims []string
	)

	for instance, feeding := range f.serving {
		if !feeding.answers(verb) || !feeding.provider.Serving() {
			continue
		}

		found = feeding.provider

		claims = append(claims, instance.String())
	}

	switch len(claims) {
	case 0:
		return nil, httpfault.HistoryNotConfigured()
	case 1:
		return found, nil
	}

	slices.Sort(claims)

	return nil, httpfault.HistoryReaderAmbiguous(verb, strings.Join(claims, " and "))
}

// drain reads one provider's windows until its subscription ends, writing on a
// size threshold or on a timer, whichever comes first.
func (f *historyFeed) drain(ctx context.Context, feeding *historyStream) {
	flush := time.NewTicker(historyFlushInterval)
	defer flush.Stop()

	pending := historyBatch{}

	for {
		select {
		case batch, open := <-feeding.sub.Batches():
			if !open {
				f.write(context.WithoutCancel(ctx), feeding, &pending, historyDrainBudget)

				return
			}

			f.collect(feeding, batch, &pending)

			if pending.full() {
				f.write(ctx, feeding, &pending, 0)
			}
		case <-flush.C:
			f.write(ctx, feeding, &pending, 0)
		}
	}
}

// historyBatch is what one instance has read and not yet written.
type historyBatch struct {
	samples []historyapi.Sample
	facts   []historyapi.Fact
}

// full reports a batch that has reached the size one transaction is measured at.
func (b *historyBatch) full() bool {
	return len(b.samples) >= historyFlushRows || len(b.facts) >= historyFlushRows
}

// collect turns one window into rows for one instance.
//
// The declared class filter is applied only here. The hub does not know about
// it: a provider is an ordinary subscriber, and the hub has no predicate and
// no queue for this filter.
func (f *historyFeed) collect(feeding *historyStream, batch stream.Batch, pending *historyBatch) {
	if batch.Resync {
		pending.facts = append(pending.facts, gapFacts(batch.Cursor, time.Now().UTC())...)
		feeding.counts.gaps.Add(1)
	}

	if feeding.wants(event.ClassTelemetry) && feeding.caps.Has(historyapi.CapSamples) {
		for _, frame := range batch.Telemetry {
			if sample, ok := historySampleOf(feeding, frame); ok {
				pending.samples = append(pending.samples, sample)
			}
		}
	}

	// Every durable frame the instance declared is a fact, whatever its type is
	// called. Only the class is read here, and the type is stored as it
	// arrived. A type this build does not know, from a module or from a peer,
	// is stored like any other.
	//
	// The instance's own lifecycle is the exception. The feed reconciles from
	// the plane's state on a timer, so a subscription can exist before the
	// frame that announces the instance is on the bus. Storing that frame
	// would make the row set depend on scheduling: stored on one start and
	// not on the next. The stop is never stored either, because the feed
	// drains before the instance closes. So neither end is stored.
	for _, frame := range batch.Events {
		if !feeding.wants(frame.Class) {
			continue
		}

		if !feeding.own.IsZero() && frame.Position.Origin == feeding.own {
			continue
		}

		if historyapi.VerbOf(frame.Class) != historyapi.VerbFact || !feeding.caps.Has(historyapi.CapFacts) {
			continue
		}

		if fact, ok := factOf(frame); ok {
			pending.facts = append(pending.facts, fact)
		}
	}
}

// wants reports whether the declared filter admits a class. An empty set is a
// registration that catches nothing.
func (s *historyStream) wants(class event.Class) bool { return slices.Contains(s.consumes, class) }

// write hands what is pending to one provider and empties it, whatever the
// provider answered. A refusal is counted and logged. The frames in a refused
// batch are lost, because holding them would grow without a bound while the
// store stays down.
func (f *historyFeed) write(
	ctx context.Context, feeding *historyStream, pending *historyBatch, budget time.Duration,
) {
	if len(pending.samples) == 0 && len(pending.facts) == 0 {
		return
	}

	if budget > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}

	if len(pending.samples) > 0 {
		if err := feeding.provider.AppendSamples(ctx, pending.samples); err != nil {
			feeding.counts.failed.Add(1)
			f.log.WarnContext(ctx, "a history provider refused a batch of samples",
				"resource", feeding.provider.Ref().String(),
				"samples", len(pending.samples), "error", err.Error())
		} else {
			feeding.counts.samples.Add(uint64(len(pending.samples)))
		}
	}

	if len(pending.facts) > 0 {
		if err := feeding.provider.AppendFacts(ctx, pending.facts); err != nil {
			feeding.counts.failed.Add(1)
			f.log.WarnContext(ctx, "a history provider refused a batch of facts",
				"resource", feeding.provider.Ref().String(),
				"facts", len(pending.facts), "error", err.Error())
		} else {
			feeding.counts.facts.Add(uint64(len(pending.facts)))
		}
	}

	pending.samples = pending.samples[:0]
	pending.facts = pending.facts[:0]
}

// telemetryFrame is the part of a telemetry envelope a sample is read out of.
// Only the members a row needs are decoded: the origin is already on the frame.
type telemetryFrame struct {
	Time time.Time `json:"time"`
	Data struct {
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"data"`
}

// factFrame is the part of a durable envelope a fact is read out of. The data
// member stays raw, because the schema behind it belongs to whoever emitted it.
type factFrame struct {
	Time     time.Time       `json:"time"`
	Sequence string          `json:"sequence"`
	Epoch    string          `json:"epoch"`
	Data     json.RawMessage `json:"data"`
}

// historySampleOf renders one telemetry frame as a sample, and reports false for one
// that is not a measurement.
//
// The metric name is the state path. A driver that invented a name per release
// would produce a store nobody can query across versions.
//
// It reads the payload's shape and never the type name. A feed that switched
// on device.telemetry.sampled would miss the next producer that reports a
// measurement at a path. Anything published as telemetry that carries a path
// and a scalar is a sample, whoever emitted it and whatever the type is called.
//
// A word is a sample as much as a number is. /state and /material are two
// paths that report a word.
func historySampleOf(feeding *historyStream, frame stream.Frame) (historyapi.Sample, bool) {
	var decoded telemetryFrame
	if err := json.Unmarshal(frame.Bytes, &decoded); err != nil {
		return historyapi.Sample{}, false
	}

	if decoded.Data.Path == "" {
		return historyapi.Sample{}, false
	}

	sample := historyapi.Sample{
		At:     decoded.Time,
		Ref:    frame.Position.Origin,
		Metric: decoded.Data.Path,
	}

	if value, ok := numericValue(decoded.Data.Value); ok {
		sample.Value = value

		return sample, true
	}

	if word, clipped, ok := textValue(decoded.Data.Value); ok {
		if clipped {
			feeding.counts.clipped.Add(1)
		}

		sample.Kind, sample.Text = historyapi.SampleText, word

		return sample, true
	}

	feeding.counts.untyped.Add(1)

	return historyapi.Sample{}, false
}

// numericValue reads a state tree node as a number.
//
// A boolean is a number here, because a state tree carries flags beside its
// measurements and a metric store holds them as 1 and 0.
func numericValue(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}

	switch string(raw) {
	case "true":
		return 1, true
	case "false":
		return 0, true
	}

	number, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0, false
	}

	return number, true
}

// maxSampleText bounds a word this feed will store, in bytes.
//
// A word-valued sample exists for paths like /state and /material, which
// report a few characters from a closed set: PRINTING, ATTENTION, PLA. But
// every state-tree patch becomes a telemetry frame, so any string a driver
// puts at any path is a row here at the device's report rate. Moonraker's
// /job/name is a filename, and a fleet reporting one at 10 Hz would fill the
// store with the same long string. 256 bytes is far more than a state word
// needs and still keeps a row small.
//
// A long word is clipped and not refused, and it is clipped here and not in
// historyapi.Sample.Check. A refusal in the contract would fail the whole
// batch the long word arrived in, so one misconfigured path would cost every
// other printer's readings for that window.
const maxSampleText = 256

// textValue reads a state tree node as a word, clipped to maxSampleText, and
// reports whether it had to clip.
//
// Only a JSON string is a word. An object, an array or a null at a state path
// is not a series, and storing its JSON text would put a document in a column
// an operator queries as a value. Those are counted as untyped.
func textValue(raw json.RawMessage) (string, bool, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false, false
	}

	var word string
	if err := json.Unmarshal(raw, &word); err != nil {
		return "", false, false
	}

	if len(word) <= maxSampleText {
		return word, false, true
	}

	return clipToBound(word), true, true
}

// clipToBound cuts a word to maxSampleText bytes on a rune boundary, marked as
// cut. The marker is inside the bound, so a stored word never exceeds it.
func clipToBound(word string) string {
	return machinetext.Clip(word, maxSampleText-len(machinetext.Marker))
}

// factOf renders one durable frame as a fact, and reports false for one whose
// envelope carries no place in its origin's numbering.
func factOf(frame stream.Frame) (historyapi.Fact, bool) {
	var decoded factFrame
	if err := json.Unmarshal(frame.Bytes, &decoded); err != nil {
		return historyapi.Fact{}, false
	}

	sequence, err := event.ParseSequence(decoded.Sequence)
	if err != nil {
		return historyapi.Fact{}, false
	}

	return historyapi.Fact{
		At:       decoded.Time,
		Ref:      frame.Position.Origin,
		Type:     frame.Type,
		Sequence: sequence,
		Epoch:    decoded.Epoch,
		Payload:  decoded.Data,
		Class:    frame.Class,
	}, true
}

// gapFacts renders one continuity loss as a marker per origin the reader
// follows.
//
// The hub declares continuity lost for the whole subscription and hands back a
// cursor at the head of every origin. So a gap is a fact about each origin: a
// query over any origin's window has to say the window is incomplete. Each
// row carries the offset the reader carried on from. A second loss in one
// generation has a new offset and is a second row. A replay of the same loss
// has the same offset and is not.
func gapFacts(cursor event.Cursor, at time.Time) []historyapi.Fact {
	var facts []historyapi.Fact

	for _, generation := range cursor.Generations {
		for _, position := range generation.Positions {
			facts = append(facts, historyapi.Fact{
				At:       at,
				Ref:      position.Origin,
				Type:     event.TypeStreamContinuityLost,
				Sequence: position.Offset,
				Epoch:    generation.Epoch,
				Class:    event.ClassState,
			})
		}
	}

	return facts
}
