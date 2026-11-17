package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// emittedType is the event type the operator declares beside the endpoint name
// in these cases. It sits outside com.prusa.connect., which is this project's
// own closed namespace.
const emittedType = "com.example.fleet.observed"

// emitScript turns a printer's state change into a fact of the operator's own
// shape and puts it back on this server's bus.
const emitScript = `msg("bus", "printer." + event.device_id, ` +
	`toJSON({"machine": event.device_id, "status": event.state}))`

// TestAScriptEmitsAnEventEveryReaderOfTheStreamSees is the emit half of
// ADR-20260826-w3y rule 26, end to end on the real composition root: the operator's
// file, the real registry, the real supervision plane, the real hub, the real
// authorization graph, a printer dialling in over the frozen wire, and a
// browser reading the published event stream.
//
// What the reader receives is not an HTTP request the host made on the script's
// behalf. It is an event on the same bus every other producer publishes on, so
// a reader that never heard of this instance routes on it like any other.
func TestAScriptEmitsAnEventEveryReaderOfTheStreamSees(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), scriptBusConfig(emitScript))

	requireSinkReady(t, running)

	seen := watch(t, running)

	dialIn(t, running, "SIM-SCRIPT-EMIT")

	printer := onlyPrinter(t, running)
	grantSink(t, running, printer.ID)

	var arrived sseFrame

	require.Eventually(t, func() bool {
		frames := seen.framesOfType(emittedType)
		if len(frames) == 0 {
			return false
		}

		arrived = frames[0]

		return true
	}, settle, 50*time.Millisecond, "nothing a script emitted reached a reader of the stream")

	require.Contains(t, arrived.data, `"`+printer.ID+`"`,
		"the body the script built did not survive onto the bus")
	require.Contains(t, arrived.data, "sink/"+sinkName,
		"the event does not name the instance that emitted it as its source")
}

// TestAScriptCannotEmitInTheProjectsOwnNamespace is the boundary the emit half
// must not move. The core namespace is closed and every type in it is a
// published, gated identifier, so an instance that declares one never starts.
//
// The refusal is the operator's to read, and the printer half of the case is
// the containment: one bad endpoint takes nothing else with it.
func TestAScriptCannotEmitInTheProjectsOwnNamespace(t *testing.T) {
	t.Parallel()

	forged := scriptBusConfigFor(emitScript, "event:"+event.TypeDeviceArrived)
	running := start(t, t.TempDir(), forged)

	var status httpapi.ResourceStatus

	require.Eventually(t, func() bool {
		status = instanceStatus(t, running, "sink/"+sinkName)

		return status.Result == "start-error"
	}, settle, 20*time.Millisecond, "an instance publishing a core event type started anyway")

	require.False(t, status.Serving)
	require.Contains(t, conditionMessages(status), event.CoreNamespace,
		"the operator is not told which namespace they may not publish in")

	dialIn(t, running, "SIM-SCRIPT-EMIT-2")
	require.Len(t, printers(t, running), 1, "one refused endpoint stopped the rest of the process")
}

// TestASinkIsNotFedAnEventASinkEmitted is what stops a script that emits from
// triggering itself. The feed carries a frame to a sink only when a printer
// published it, so an event a sink emitted reaches no sink and a cycle of any
// length cannot form.
//
// A federated loop is refused the same way, at the link rather than by counting
// hops, with Envelope.Hops as the backstop behind it.
//
// The control half is the assertion that matters. Without it the case would
// pass against a feed that carries nothing at all.
func TestASinkIsNotFedAnEventASinkEmitted(t *testing.T) {
	t.Parallel()

	emitted, err := event.NewOrigin(event.Kind(ref.KindSink.String()), sinkName)
	require.NoError(t, err)

	fromASink := &recordedFacts{}
	forwardToSink(fromASink, sampledFrame(t, emitted, `{"path":"/state","value":"PRINTING"}`))
	require.Empty(t, fromASink.states, "a sink was fed an event a sink emitted")

	published, err := event.NewOrigin(event.KindDevice, feedPrinter)
	require.NoError(t, err)

	fromAPrinter := &recordedFacts{}
	forwardToSink(fromAPrinter, sampledFrame(t, published, `{"path":"/state","value":"PRINTING"}`))
	require.Len(t, fromAPrinter.states, 1, "the feed carries nothing, so the case above proves nothing")
}

// recordedFacts is a sink that keeps the domain facts it was given.
type recordedFacts struct {
	states []eventsapi.PrinterState
}

var _ eventsapi.Processor = (*recordedFacts)(nil)

func (r *recordedFacts) PrinterStateChanged(state eventsapi.PrinterState) {
	r.states = append(r.states, state)
}

func (r *recordedFacts) PrinterLabelsChanged(eventsapi.PrinterLabels) {}
func (r *recordedFacts) PrinterDeleted(string)                        {}
func (r *recordedFacts) JobUpdated(eventsapi.JobInfo)                 {}
func (r *recordedFacts) LabelDeleted(string)                          {}
func (r *recordedFacts) FactObserved(eventsapi.Fact)                  {}
func (r *recordedFacts) Close() error                                 { return nil }

// scriptBusConfig declares one script sink whose only destination is this
// server's own event stream.
func scriptBusConfig(source string) string {
	return scriptBusConfigFor(source, "event:"+emittedType)
}

// scriptBusConfigFor is the same declaration with the destination spelled out,
// for a case that writes one the host refuses.
func scriptBusConfigFor(source, address string) string {
	return lines(
		"core:", "  log:", "    level: error",
		"resources:", "  sink:", "    "+sinkName+":",
		"      type: script",
		"      runtime: script",
		"      enabled: true",
		"      script: |",
		"        "+source,
		"      config:",
		"        endpoints:",
		"          bus: "+address,
		"        timeout: 2s")
}

// all returns every frame that has arrived, in order. It is the accessor a
// case uses when the question is about a set of types rather than one.
func (s *eventLog) all() []sseFrame {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	return append([]sseFrame(nil), s.frames...)
}

// framesOfType returns every frame of one type, in arrival order.
func (s *eventLog) framesOfType(kind string) []sseFrame {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	out := []sseFrame{}

	for _, frame := range s.frames {
		if frame.kind == kind {
			out = append(out, frame)
		}
	}

	return out
}
