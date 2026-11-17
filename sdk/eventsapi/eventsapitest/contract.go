// Package eventsapitest provides the conformance suite every eventsapi.Processor
// must pass, so a sink written by a community author behaves the way the event
// bus needs it to and the built-in ones do not get to be special.
//
// The contract eventsapi.Processor states is short and every clause of it is a
// property of what happens when the destination stops answering: the caller is
// the event bus, so a method that blocks stalls every other subscriber, and a
// sink that panics takes the process with it. The suite therefore runs a
// processor whose destination accepts connections and never answers, which is
// the state a broker under load, a paused container and a black-holed route all
// present, and asserts the contract holds there.
//
// Usage from a sink's _test.go:
//
//	func TestContract(t *testing.T) {
//	    address := eventsapitest.BlackHole(t)
//	    eventsapitest.RunProcessorContract(t, func(t *testing.T) eventsapi.Processor {
//	        p, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{
//	            "url": "tcp://" + address,
//	        }})
//	        if err != nil { t.Fatal(err) }
//	        return p
//	    })
//	}
//
// The destination is made once, outside newProcessor, because one subtest
// compares the live goroutines across a build and a Close and a fixture started
// inside newProcessor would be read as the sink's own.
package eventsapitest

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

const (
	// floodEvents is how many events the non-blocking case offers. It is far
	// past any sane queue depth, so the queue is full for most of the run and
	// the offer path is exercised in the state that matters.
	floodEvents = 2000

	// perCallBudget is the longest one fire-and-forget call may take. A sink
	// that buffers returns in microseconds; one that reaches the network on the
	// caller's goroutine blows this on the first call against a destination
	// that never answers.
	perCallBudget = 250 * time.Millisecond

	// concurrentCallers and callsPerCaller drive the race case. The event bus
	// has one goroutine per source, so a sink is called from several at once.
	concurrentCallers = 8
	callsPerCaller    = 250

	// censusBuffer is where the stack dump starts and censusGrowth is what it
	// is multiplied by until the dump fits. 64 KiB holds a few hundred
	// goroutines, so a test binary of any ordinary size takes the dump once.
	censusBuffer = 1 << 16
	censusGrowth = 2
)

// The two budgets a deliberately broken sink has to be watched exceeding. They
// are variables so this package's own self-test can shorten them: a suite that
// has never been watched failing proves nothing, and waiting the production
// budget out to see it costs half a minute per case.
//
//nolint:gochecknoglobals // the shape connectorapitest already uses for the same reason.
var (
	// closeBudget bounds Close. A sink is allowed a grace period to flush, and
	// a sink that waits for a destination that never answers is a shutdown that
	// never finishes.
	closeBudget = 30 * time.Second

	// settleBudget is how long a sink's own goroutines are given to finish
	// exiting after Close. Closing a connection wakes a reader that then exits,
	// which is not instant.
	settleBudget = 5 * time.Second
)

// MakeProcessor builds a fresh Processor for one subtest, pointed at a
// destination that accepts connections and never answers.
type MakeProcessor func(t *testing.T) eventsapi.Processor

// BlackHole starts a listener that accepts every connection and answers
// nothing, and returns its host:port. It is what a sink's destination is
// pointed at for the suite: a refused connection fails fast and proves little,
// while a connection that is accepted and then silent is what a wedged broker
// looks like from the client side.
//
// Accepted connections are held open until the test ends, and nothing is
// spawned per connection, so the goroutine count a subtest reads does not move
// when a sink dials.
func BlackHole(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("black hole listener: %v", err)
	}

	var (
		mu     sync.Mutex
		held   []net.Conn
		closed bool
	)

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			mu.Lock()
			if closed {
				mu.Unlock()
				_ = conn.Close()

				return
			}
			held = append(held, conn)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()

		mu.Lock()
		defer mu.Unlock()

		closed = true

		for _, conn := range held {
			_ = conn.Close()
		}
	})

	return listener.Addr().String()
}

// RunProcessorContract runs the conformance suite against the processor
// produced by newProcessor. Each subtest gets its own instance.
func RunProcessorContract(t *testing.T, newProcessor MakeProcessor) {
	t.Helper()

	t.Run("NeverBlocks", func(t *testing.T) { testNeverBlocks(t, newProcessor(t)) })
	t.Run("AcceptsZeroValues", func(t *testing.T) { testZeroValues(t, newProcessor(t)) })
	t.Run("CloseReturns", func(t *testing.T) { testCloseReturns(t, newProcessor(t)) })
	t.Run("CloseIsIdempotent", func(t *testing.T) { testCloseIsIdempotent(t, newProcessor(t)) })
	t.Run("SurvivesCallsAfterClose", func(t *testing.T) { testCallsAfterClose(t, newProcessor(t)) })
	t.Run("ConcurrentCallers", func(t *testing.T) { testConcurrentCallers(t, newProcessor(t)) })
	t.Run("StopsEveryGoroutineItStarts", func(t *testing.T) { testNoGoroutineLeak(t, newProcessor) })
}

// The external ids the probe facts carry, one per subtest that sends any, so
// a fact surfacing in the wrong subtest names its origin.
const (
	closeProbe      = 1
	afterCloseProbe = 2
	leakProbe       = 3
)

// censusPoll is how often the leak check re-reads the goroutine census while
// waiting for the settle budget to run out.
const censusPoll = 10 * time.Millisecond

// deliver offers one of each domain fact, so a sink is exercised on every
// method rather than on whichever one the suite happened to pick.
func deliver(p eventsapi.Processor, n int) {
	p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "PRINTING"})
	p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "printer-1", Mode: "replace", Labels: []string{"floor-2"}})
	p.JobUpdated(eventsapi.JobInfo{ID: "job-1", ExternalID: n, PrinterID: "printer-1", State: "PRINTING"})
	p.PrinterDeleted("printer-2")
	p.LabelDeleted("label-1")
	p.FactObserved(probeFact())
}

// probeSequence numbers the probe facts. Origin, epoch and sequence are the
// dedup key a sink is handed, so every probe carrying the same one would let a
// sink that deduplicates pass the flood case by dropping all but the first.
//
//nolint:gochecknoglobals // the counter belongs to the fixture and not to one run of it.
var probeSequence atomic.Uint64

// probeFact is one command lifecycle frame, which is the fact a sink is most
// likely to be built for and the one whose envelope carries every optional
// member: a causation id joining it to its two siblings, a payload with a
// schema this package does not own, and a place in its origin's numbering.
func probeFact() eventsapi.Fact {
	origin, err := event.NewOrigin(event.KindDevice, "printer-1")
	if err != nil {
		// The id is a literal three lines up, so this cannot happen; returning
		// the zero origin rather than panicking keeps the suite from taking a
		// sink's test binary down over its own fixture.
		return eventsapi.Fact{}
	}

	return eventsapi.Fact{
		Origin:      origin,
		Type:        "com.prusa.connect.device.command.requested",
		Class:       event.ClassCommand,
		At:          time.Now().UTC(),
		Sequence:    probeSequence.Add(1),
		Epoch:       "01JB0000000000000000000000",
		CausationID: "01JB000000000000000000000C",
		Payload:     json.RawMessage(`{"command_id":"01JB000000000000000000000C","verb":"printer.pause"}`),
	}
}

// calls is every method under one name, so a case that has to hit all of them
// and time each of them does not repeat the list.
func calls(p eventsapi.Processor) map[string]func() {
	return map[string]func(){
		"PrinterStateChanged": func() {
			p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "IDLE"})
		},
		"PrinterLabelsChanged": func() {
			p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "printer-1", Mode: "add", Labels: []string{"a"}})
		},
		"PrinterDeleted": func() { p.PrinterDeleted("printer-1") },
		"JobUpdated": func() {
			p.JobUpdated(eventsapi.JobInfo{ID: "job-1", ExternalID: 1, PrinterID: "printer-1", State: "PRINTING"})
		},
		"LabelDeleted": func() { p.LabelDeleted("label-1") },
		"FactObserved": func() { p.FactObserved(probeFact()) },
	}
}

// The event bus calls a sink on the goroutine that produced the fact, so a
// method that waits for the network stalls every other subscriber behind a
// destination that has stopped answering. The contract says buffer internally,
// and this is the case that holds it.
func testNeverBlocks(t *testing.T, p eventsapi.Processor) {
	t.Cleanup(func() { closeQuietly(p) })

	slowest := time.Duration(0)
	slowestCall := ""

	for i := range floodEvents {
		for name, call := range calls(p) {
			start := time.Now()

			call()

			if elapsed := time.Since(start); elapsed > slowest {
				slowest, slowestCall = elapsed, name
			}
		}

		if slowest > perCallBudget {
			t.Fatalf("%s took %s on offer %d, past the %s a fire-and-forget call has.\n"+
				"The caller is the event bus: a sink that reaches its destination on the caller's "+
				"goroutine stalls every other subscriber whenever that destination stops answering. "+
				"Buffer internally and drop what does not fit.",
				slowestCall, slowest, i, perCallBudget)
		}
	}

	t.Logf("%d events offered, slowest call %s (%s)", floodEvents*len(calls(p)), slowest, slowestCall)
}

// A domain fact can carry an empty id or no labels, and a sink that indexes into
// what it was handed panics on the event bus's goroutine. The contract says a
// Processor must not panic, and a zero value is the cheapest way to break one.
func testZeroValues(t *testing.T, p eventsapi.Processor) {
	t.Cleanup(func() { closeQuietly(p) })

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a zero-value event panicked the sink: %v.\n"+
				"A Processor must not panic: it is called on the event bus's goroutine, so a panic "+
				"takes the process down and every other subscriber with it.", r)
		}
	}()

	p.PrinterStateChanged(eventsapi.PrinterState{})
	p.PrinterLabelsChanged(eventsapi.PrinterLabels{})
	p.JobUpdated(eventsapi.JobInfo{})
	p.PrinterDeleted("")
	p.LabelDeleted("")
	p.FactObserved(eventsapi.Fact{})
}

// Close is called on the shutdown path, with the destination already gone more
// often than not. A sink that waits for an answer that is not coming turns a
// restart into a hang and then into a SIGKILL.
func testCloseReturns(t *testing.T, p eventsapi.Processor) {
	deliver(p, closeProbe)

	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = p.Close()
	}()

	select {
	case <-done:
	case <-time.After(closeBudget):
		t.Fatalf("Close did not return within %s against a destination that never answers.\n"+
			"A sink may flush for a bounded grace period and must then give up: a shutdown that "+
			"waits for a dead destination is a shutdown that ends in SIGKILL.", closeBudget)
	}
}

// A host shutting down after an error calls Close on a path that may already
// have closed. The second call reports whatever the first did and must not
// panic or hang, so cleanup can be unconditional.
func testCloseIsIdempotent(t *testing.T, p eventsapi.Processor) {
	returned, first := closeWithin(p)
	if !returned {
		t.Fatalf("the first Close did not return within %s, so a second cannot be asked for. "+
			"CloseReturns names the defect.", closeBudget)
	}

	done := make(chan error, 1)
	panicked := make(chan any, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- r
			}
		}()

		done <- p.Close()
	}()

	select {
	case second := <-done:
		t.Logf("Close returned %v, then %v", first, second)
	case r := <-panicked:
		t.Fatalf("the second Close panicked the sink: %v.\n"+
			"Close is called from more than one shutdown path, so it has to be safe to call twice.", r)
	case <-time.After(closeBudget):
		t.Fatalf("the second Close did not return within %s.\n"+
			"Close is called from more than one shutdown path, so it has to be safe to call twice.",
			closeBudget)
	}
}

// An event already in flight reaches a sink after Close, because the event bus
// and the shutdown run on different goroutines. Sending on a closed channel is
// the usual way this panics, and it panics on the bus.
func testCallsAfterClose(t *testing.T, p eventsapi.Processor) {
	returned, err := closeWithin(p)
	if !returned {
		t.Fatalf("Close did not return within %s, so nothing can be delivered after it. "+
			"CloseReturns names the defect.", closeBudget)
	}

	if err != nil {
		t.Logf("Close reported %v, which a sink that dropped is allowed to", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("an event delivered after Close panicked the sink: %v.\n"+
				"Shutdown and the event bus are different goroutines, so an event already in flight "+
				"arrives after Close. It may be dropped and must not panic.", r)
		}
	}()

	deliver(p, afterCloseProbe)
}

// The bus calls a sink from one goroutine per source, so a farm with four
// hundred printers calls it from many at once. Run under -race this is the case
// that catches a queue guarded by nothing.
func testConcurrentCallers(t *testing.T, p eventsapi.Processor) {
	t.Cleanup(func() { closeQuietly(p) })

	var wg sync.WaitGroup

	for caller := range concurrentCallers {
		wg.Go(func() {
			for i := range callsPerCaller {
				deliver(p, caller*callsPerCaller+i)
			}
		})
	}

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		wg.Wait()
	}()

	// The wait is bounded for the same reason every other one here is: a sink
	// that hands its caller's goroutine to a destination that never answers
	// would otherwise hold this case until the whole test binary times out,
	// which tells its author nothing.
	select {
	case <-finished:
	case <-time.After(closeBudget):
		t.Fatalf("%d caller(s) were still offering after %s.\n"+
			"A sink is called from one goroutine per event source, and none of them may be held "+
			"waiting on the destination.", concurrentCallers, closeBudget)
	}
}

// A sink buffers, which means it owns at least one goroutine, and a goroutine
// with no stop condition is a leak the host cannot see. A host that rebuilds
// its sinks on a config reload accumulates one set per reload.
func testNoGoroutineLeak(t *testing.T, newProcessor MakeProcessor) {
	// One build and close first, so a goroutine the sink or the runtime starts
	// once on the first dial and then keeps is already running, and its site is
	// already populated, when the first census is taken.
	closeQuietly(newProcessor(t))

	first := oneLifecycle(t, newProcessor)
	if len(first) == 0 {
		return
	}

	// A sink that leaks does it once per instance, so a second build gains at
	// the same site again. A goroutine belonging to the binary around the sink
	// does not, and that is the whole difference between a leak and the churn
	// of whatever else the author's test binary is running.
	second := oneLifecycle(t, newProcessor)

	leaked := make([]growth, 0, len(second))

	for site, g := range second {
		if _, again := first[site]; again {
			leaked = append(leaked, g)
		}
	}

	if len(leaked) == 0 {
		t.Logf("%d site(s) gained a goroutine over one build and close and none of them gained "+
			"again on a second, so the growth was not the sink's", len(first))

		return
	}

	slices.SortFunc(leaked, func(a, b growth) int { return strings.Compare(a.site, b.site) })

	total := 0
	detail := &strings.Builder{}

	for _, g := range leaked {
		total += g.count

		fmt.Fprintf(detail, "\n%s\ngained %d goroutine(s) that Close did not stop. One of them:\n\n%s\n",
			g.site, g.count, g.sample)
	}

	t.Fatalf("%d goroutine(s) outlived Close, at %d creation site(s), on each of two separate builds.\n"+
		"Every goroutine a sink starts has to end when Close returns, or a host that rebuilds "+
		"its sinks accumulates one set per reload.%s", total, len(leaked), detail)
}

// oneLifecycle builds a sink, delivers to it and closes it, and reports the
// creation sites left holding goroutines that were not there before the build.
// The wait is there because closing a connection wakes a reader that then
// exits, so a sink's goroutines go a moment after Close returns rather than
// during it.
func oneLifecycle(t *testing.T, newProcessor MakeProcessor) map[string]growth {
	t.Helper()

	before := takeCensus()

	p := newProcessor(t)
	deliver(p, leakProbe)

	returned, err := closeWithin(p)
	if !returned {
		t.Fatalf("Close did not return within %s, so the goroutines it should have stopped cannot be "+
			"counted. CloseReturns names the defect.", closeBudget)
	}

	if err != nil {
		t.Logf("Close reported %v", err)
	}

	deadline := time.Now().Add(settleBudget)

	for {
		grown := grownSites(before, takeCensus())
		if len(grown) == 0 || time.Now().After(deadline) {
			return grown
		}

		time.Sleep(censusPoll)
	}
}

// growth is one creation site holding more live goroutines than it did, with
// the stack of one of the goroutines that were not there before.
type growth struct {
	site   string
	count  int
	sample string
}

// grownSites reports the creation sites that gained goroutines between two
// censuses.
//
// Growth is counted per site and never netted across the process. A single
// process-wide total lets a goroutine ending anywhere else in the test binary
// pay for one the sink leaked, and the suite then reports a leaking sink clean,
// which is the one direction a conformance suite must not fail in.
func grownSites(before, after map[uint64]liveGoroutine) map[string]growth {
	was, now := population(before), population(after)
	grown := make(map[string]growth)

	for id, g := range after {
		if _, known := before[id]; known {
			continue
		}

		gain := now[g.site] - was[g.site]
		if gain <= 0 {
			continue
		}

		grown[g.site] = growth{site: g.site, count: gain, sample: g.stack}
	}

	return grown
}

// population counts a census by creation site.
func population(census map[uint64]liveGoroutine) map[string]int {
	counts := make(map[string]int, len(census))

	for _, g := range census {
		counts[g.site]++
	}

	return counts
}

// liveGoroutine is one goroutine in a census: the site that started it, which
// does not move while it runs, and its stack when the census was taken.
type liveGoroutine struct {
	site  string
	stack string
}

// takeCensus records every live goroutine in the process against the site that
// created it, keyed by the runtime's own goroutine id.
//
// Identity is read out of the runtime because the suite is compiled against a
// sink whose internals it knows nothing about: anything keyed to a package name
// would work for the sinks shipped here and for no third-party one.
func takeCensus() map[uint64]liveGoroutine {
	census := make(map[uint64]liveGoroutine)

	for block := range strings.SplitSeq(string(stackDump()), "\n\n") {
		block = strings.TrimSpace(block)

		id, ok := goroutineID(block)
		if !ok {
			continue
		}

		census[id] = liveGoroutine{site: creationSite(block), stack: block}
	}

	return census
}

// stackDump returns the traceback of every goroutine in the process.
//
// runtime.Stack truncates instead of reporting how much room it needed, and a
// census missing the goroutine the sink leaked is a leaking sink reported
// clean, so the buffer grows until the whole dump fits inside it.
func stackDump() []byte {
	buf := make([]byte, censusBuffer)

	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}

		buf = make([]byte, censusGrowth*len(buf))
	}
}

// goroutineID reads the id out of a stack block's header line, reporting
// whether the block had one.
func goroutineID(stack string) (uint64, bool) {
	header, _, _ := strings.Cut(stack, "\n")

	rest, ok := strings.CutPrefix(header, "goroutine ")
	if !ok {
		return 0, false
	}

	digits, _, _ := strings.Cut(rest, " ")

	id, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, false
	}

	return id, true
}

// creationSite names where a goroutine was started, which is the one part of
// its stack that holds still while it runs. The runtime writes the id of the
// parent goroutine and a code offset alongside it, and both differ between two
// builds of the same sink, so neither belongs in the name.
func creationSite(stack string) string {
	lines := strings.Split(stack, "\n")

	for i, line := range lines {
		site, ok := strings.CutPrefix(strings.TrimSpace(line), "created by ")
		if !ok {
			continue
		}

		if parent := strings.Index(site, " in goroutine "); parent >= 0 {
			site = site[:parent]
		}

		if i+1 < len(lines) {
			frame := strings.TrimSpace(lines[i+1])
			if offset := strings.LastIndex(frame, " +0x"); offset >= 0 {
				frame = frame[:offset]
			}

			site += "\n\t" + frame
		}

		return site
	}

	// The goroutine that runs main has no creator. There is exactly one of it
	// and it never appears twice, so any name a real site cannot take will do.
	return "the goroutine the process started on"
}

// closeQuietly is how every cleanup closes a sink: bounded, and with nothing to
// report.
func closeQuietly(p eventsapi.Processor) {
	_, _ = closeWithin(p)
}

// closeWithin is how every close outside the CloseReturns case is made. A sink
// whose Close never returns has to fail that case rather than hang the binary
// running the suite, so nothing here waits on Close without a bound. It reports
// whether Close returned within closeBudget at all, and what it returned.
//
// A Close that panics panics again on the caller's goroutine, where the test
// binary reports it against the case, rather than on the one that ran it,
// where it would take the whole binary down.
func closeWithin(p eventsapi.Processor) (bool, error) {
	type outcome struct {
		err      error
		panicked any
	}

	done := make(chan outcome, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{panicked: r}
			}
		}()

		done <- outcome{err: p.Close()}
	}()

	select {
	case got := <-done:
		if got.panicked != nil {
			panic(got.panicked)
		}

		return true, got.err
	case <-time.After(closeBudget):
		return false, nil
	}
}
