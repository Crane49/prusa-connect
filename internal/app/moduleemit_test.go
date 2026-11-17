package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// moduleEventType is what the declaration in moduleConfig produces: the
// namespace the operator's type gave the instance, then the name the operator
// wrote beside the endpoint.
//
// It is spelled out here rather than composed, so a change to either half is a
// change a reader of this file sees.
const moduleEventType = "com.github.aux.acme.planner.spool.changed"

// No case in this file calls t.Parallel, for the reason statemount_test.go
// gives: each one starts a core and two child processes through the real
// composition root.

// TestAModuleEmitsAnEventEveryReaderOfTheStreamSees is the emit path end to
// end on the real composition root: the operator's own configuration file, the
// real supervision plane, a real second process, the real hub, and a browser
// reading the published event stream.
//
// What the reader receives is not something the host did on the module's
// behalf. It is an event on the same bus every other producer publishes on, so
// a reader that never heard of this module routes on it like any other.
//
// The two halves a reader routes on are both asserted, because both are the
// point. The type is the module's namespace and the operator's name, and the
// module named neither. The source is the instance, minted by the host from the
// ref an operator declared.
func TestAModuleEmitsAnEventEveryReaderOfTheStreamSees(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	// Attached before anything is published, because a subscriber receives the
	// frames published after it attached.
	seen := watch(t, running)

	emitted := running.call(t, http.MethodPost,
		moduleGroup+"/emit?key=spool/petg", `{"spool":"petg","grams":840}`)
	require.Equal(t, http.StatusOK, emitted.status, emitted.body)
	require.Contains(t, emitted.body, `"emitted":"changed"`)

	var arrived sseFrame

	require.Eventually(t, func() bool {
		frames := seen.framesOfType(moduleEventType)
		if len(frames) == 0 {
			return false
		}

		arrived = frames[0]

		return true
	}, settle, 50*time.Millisecond,
		"nothing the module emitted reached a reader of the published event stream")

	// The frame as it left the server, printed so the page that documents this
	// quotes what the code produced rather than what somebody remembered.
	t.Logf("event: %s\ndata: %s", arrived.kind, arrived.data)

	require.Contains(t, arrived.data, `"grams":840`,
		"the payload the module built did not survive onto the bus")
	require.Contains(t, arrived.data, "sink/planner",
		"the event does not name the instance that emitted it as its source")
	require.NotContains(t, arrived.data, event.CoreNamespace,
		"a module's event carries a type in this project's own closed namespace")

	running.stopAndWait(t)
}

// TestAModuleEmitsOnlyWhatItsDeclarationNames is the containment: an endpoint
// the operator did not write is refused, and nothing reaches the bus.
//
// It is the whole of what stops a module publishing something an operator did
// not agree to. The module names an endpoint, the host resolves it in a table
// built from the operator's file, and a name that is not in the table has no
// event type to be published under.
//
// The control half is the declared endpoint, asserted after the refusal.
// Without it the case would pass against a host that publishes nothing at all.
func TestAModuleEmitsOnlyWhatItsDeclarationNames(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	seen := watch(t, running)

	forged := running.call(t, http.MethodPost, moduleGroup+"/emit?endpoint=forged", `{"x":1}`)
	require.Equal(t, http.StatusNotFound, forged.status, forged.body)
	require.Contains(t, forged.body, "the endpoints changed",
		"the refusal does not say which endpoints the declaration does carry")
	require.Contains(t, forged.body, moduleInstanceRef,
		"the refusal does not say whose declaration was read")

	// The endpoint that is declared still publishes, so the refusal above is
	// the table and not a door that is shut.
	require.Equal(t, http.StatusOK,
		running.call(t, http.MethodPost, moduleGroup+"/emit", `{"ok":true}`).status)

	require.Eventually(t, func() bool {
		return len(seen.framesOfType(moduleEventType)) > 0
	}, settle, 50*time.Millisecond, "the declared endpoint published nothing")

	// And the refused one put nothing on the bus under any name of this
	// module's. A host that resolved an unknown endpoint to the name itself
	// would have published one here.
	require.Len(t, framesUnder(seen, moduleNamespace), 1,
		"an endpoint the declaration does not carry reached the event stream")

	running.stopAndWait(t)
}

// TestAModuleWhoseDeclarationNamesNoEndpointCannotEmitAtAll is the default.
//
// The second module in this deployment is the same artefact, the same binary
// and the same routes, declared without an events table. The host installs no
// emit method on its session, so the call is answered as a method this host
// does not serve rather than as a refusal per event, and an installed module is
// silent until an operator says otherwise.
func TestAModuleWhoseDeclarationNamesNoEndpointCannotEmitAtAll(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	require.Eventually(t, func() bool {
		return running.instanceState(t, otherModuleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the second module never reached ready")

	seen := watch(t, running)

	silent := running.call(t, http.MethodPost, otherModuleGroup+"/emit", `{"x":1}`)
	require.Equal(t, http.StatusBadGateway, silent.status, silent.body)
	require.Contains(t, silent.body, string(busapi.MethodEmit),
		"the module was not told which method the host does not serve")

	// The neighbour under the same artefact does emit, so this is the
	// declaration and not the binary.
	require.Equal(t, http.StatusOK,
		running.call(t, http.MethodPost, moduleGroup+"/emit", `{"ok":true}`).status)

	require.Eventually(t, func() bool {
		return len(seen.framesOfType(moduleEventType)) > 0
	}, settle, 50*time.Millisecond, "the declaring module published nothing")

	require.Empty(t, framesUnder(seen, otherModuleNamespace),
		"a module whose declaration names no endpoint reached the event stream")

	running.stopAndWait(t)
}

// TestABurstOfEmissionsFromOneModuleArrivesInOrder is the ordering the hub
// requires, read off the wire a browser reads.
//
// internal/transport/stream says events for one origin must be published from
// one goroutine. A module is not one goroutine: sdk/wirerpc answers every
// inbound call on its own, so a module that emits from several handlers at once
// is several goroutines inside one origin. The hub takes the sequence from an
// atomic counter and the offset under the origin's history lock, which are not
// one critical section, so two publishes that overlap can be numbered in one
// order and delivered in the other.
//
// A subscriber is fed in offset order, so the sequence numbers arriving out of
// order is exactly what that looks like from outside.
//
// The bound in front of that ordering is not reachable from here and this case
// does not claim it. A module writes its frames onto one wire, one at a time,
// so the host holds one or two emissions at once however many the module makes.
// TestAnInstanceAtItsEmitLimitIsRefusedAndNotQueued in internal/app/resourcehost
// is where the refusal is proved. What this asserts is that every emission was
// accounted for and none of them failed.
func TestABurstOfEmissionsFromOneModuleArrivesInOrder(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	seen := watch(t, running)

	const calls = supervisor.DefaultConcurrency * 8

	got := running.call(t, http.MethodPost,
		moduleGroup+"/emit-burst?n="+strconv.Itoa(calls), "")
	require.Equal(t, http.StatusOK, got.status, got.body)

	var counted struct {
		Sent    int    `json:"sent"`
		Busy    int    `json:"busy"`
		Failed  int    `json:"failed"`
		Refusal string `json:"refusal"`
	}

	require.NoError(t, json.Unmarshal([]byte(got.body), &counted), got.body)

	require.Zero(t, counted.Failed,
		"an emission failed for a reason that is not the bound: %s", counted.Refusal)
	require.Equal(t, calls, counted.Sent+counted.Busy, "the module lost track of an emission")
	require.Positive(t, counted.Sent, "the module published nothing")

	require.Eventually(t, func() bool {
		return len(seen.framesOfType(moduleEventType)) >= counted.Sent
	}, settle, 50*time.Millisecond,
		"a reader received %d of the %d events the module was told it published",
		len(seen.framesOfType(moduleEventType)), counted.Sent)

	// Every event this origin published, in the order the reader was fed them.
	// A sequence that goes backwards is two goroutines that were inside the hub
	// for one origin at once.
	last := ""

	for _, frame := range seen.framesOfType(moduleEventType) {
		var envelope struct {
			Sequence string `json:"sequence"`
		}

		require.NoError(t, json.Unmarshal([]byte(frame.data), &envelope), frame.data)
		require.Greater(t, envelope.Sequence, last,
			"a reader was fed sequence %s after %s, so two goroutines published for one origin",
			envelope.Sequence, last)

		last = envelope.Sequence
	}

	// The deployment is still serving and the module can still publish.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK,
		running.call(t, http.MethodPost, moduleGroup+"/emit", `{"after":"the burst"}`).status)

	running.stopAndWait(t)
}

// TestAReaderGrantedNothingAboutAModuleReceivesNothingItEmitted is the
// negative, and it needs two callers over one hub to be worth anything.
//
// A grant cannot be taken away from the only caller there is, so the
// deployment serves two HTTP surfaces over one graph, one plane and one hub,
// each as its own principal. That is the shape serveModule already builds for
// the module route's own refusal, and the answer here has to be the same one:
// the hub routes on origin, a module is a configured instance, and a principal
// who holds nothing on that instance is attached to no channel it publishes on.
func TestAReaderGrantedNothingAboutAModuleReceivesNothingItEmitted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	served := serveModule(t, dir)

	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	// Both readers attach before anything is published, so neither is empty
	// because it arrived late.
	granted := watch(t, served.reader)
	stranger := watch(t, served.outsider)

	require.Equal(t, http.StatusOK,
		served.reader.call(t, http.MethodPost, moduleGroup+"/emit", `{"spool":"petg"}`).status)

	require.Eventually(t, func() bool {
		return len(granted.framesOfType(moduleEventType)) > 0
	}, settle, 50*time.Millisecond, "the caller who holds the module received nothing it emitted")

	// The stranger's stream is open and carrying nothing of this module's. It
	// is asserted after the granted reader has its frame, so the emptiness is
	// not a race with a publish that had not happened yet.
	require.Empty(t, framesUnder(stranger, moduleNamespace),
		"a reader granted nothing about the module received what it emitted")
}

// TestASinkIsNotFedAnEventAModuleEmitted is what stops a module that emits from
// triggering itself.
//
// The refusal is at the link and not a hop count. A sink is fed a frame only
// when a printer published it, so an event a module emitted reaches no sink and
// a cycle of any length cannot form. It is the same rule that already covers a
// script, stated again against a module's own type and origin because the two
// producers are different code paths into one feed and a case over one of them
// says nothing about the other.
//
// The control half is the printer frame. Without it the case would pass against
// a feed that carries nothing at all.
func TestASinkIsNotFedAnEventAModuleEmitted(t *testing.T) {
	t.Parallel()

	emitted, err := event.NewOrigin(event.Kind(ref.KindSink.String()), "planner")
	require.NoError(t, err)

	fromAModule := &recordedFacts{}
	forwardToSink(fromAModule, moduleFrame(t, emitted))
	require.Empty(t, fromAModule.states, "a sink was fed an event a module emitted")

	// And the same frame under a printer's origin, which is what the feed does
	// carry, so the emptiness above is the origin and not the type.
	fromAModule = &recordedFacts{}
	forwardToSink(fromAModule, sampledFrame(t, emitted, `{"path":"/state","value":"PRINTING"}`))
	require.Empty(t, fromAModule.states, "a sink was fed a module's telemetry")

	published, err := event.NewOrigin(event.KindDevice, feedPrinter)
	require.NoError(t, err)

	fromAPrinter := &recordedFacts{}
	forwardToSink(fromAPrinter, sampledFrame(t, published, `{"path":"/state","value":"PRINTING"}`))
	require.Len(t, fromAPrinter.states, 1, "the feed carries nothing, so the case above proves nothing")
}

// moduleFrame is one frame as a module's emission reaches the feed: the type
// the operator's declaration produced, and the instance's own origin.
func moduleFrame(t *testing.T, origin event.Origin) stream.Frame {
	t.Helper()

	return stream.Frame{
		Type:     moduleEventType,
		Position: event.Position{Origin: origin},
		Bytes:    []byte(`{"data":{"spool":"petg"}}`),
	}
}

// framesUnder returns every frame whose type is in one namespace, which is how
// a case asks whether anything of a module's reached a reader without naming
// each type.
func framesUnder(seen *eventLog, namespace string) []sseFrame {
	out := []sseFrame{}

	for _, frame := range seen.all() {
		if strings.HasPrefix(frame.kind, namespace+".") {
			out = append(out, frame)
		}
	}

	return out
}
