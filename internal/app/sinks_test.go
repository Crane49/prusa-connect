package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// sinkName is the instance every case here declares, and therefore the id the
// graph knows it as, under the sink type.
const sinkName = "audit"

// TestAPrinterStateChangeReachesAGrantedSink is the old backlog's "the same change
// reaches a webhook sink", end to end on the real composition root: the real
// registry, the real supervision plane, the real hub, the shipping webhook
// adapter and a printer dialling in over the frozen wire.
//
// The grant is written after the printer arrives because that is the only order
// an operator can work in: a printer's id is minted when it dials in.
func TestAPrinterStateChangeReachesAGrantedSink(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	running := start(t, t.TempDir(), sinkConfig(endpoint.URL()))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SINK-1")

	printer := onlyPrinter(t, running)
	grantSink(t, running, printer.ID)

	delivered := requireStateDelivered(t, endpoint, printer.ID)
	require.NotEmpty(t, delivered.Data.State, "the fact arrived with no state in it")
}

// TestTheShippedGrantExampleFeedsASink runs the request config/config.yaml
// tells an operator to make, read out of the shipped file itself and driven
// through the same composition root as the case above.
//
// The file said `POST /api/v1/grants/printer/{printer}` with
// `service_account:audit`, and both halves were wrong: the object type is
// `device`, and a sink is its own principal type, which
// TestASinkIsItsOwnPrincipalAndNotAServiceAccount pins and
// TestAServiceAccountOfTheSinksNameFeedsItNothing proves the consequence of. An
// operator following the shipped file therefore wrote a grant that fed the sink
// nothing, with no error at the API and no line in the log.
//
// The example is parsed rather than restated here on purpose. A copy of it in
// this file would agree with itself while the shipped file drifted, which is
// how the wrong one survived to be shipped in the first place.
func TestTheShippedGrantExampleFeedsASink(t *testing.T) {
	t.Parallel()

	method, path, body := shippedSinkGrant(t)

	endpoint := newWebhookEndpoint(t)
	running := start(t, t.TempDir(), sinkConfig(endpoint.URL()))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SINK-DOC")

	printer := onlyPrinter(t, running)

	written := running.call(t, method, strings.ReplaceAll(path, "{printer}", printer.ID), body)
	require.Equal(t, http.StatusCreated, written.status,
		"the grant config/config.yaml documents was refused: %s", written.body)

	delivered := requireStateDelivered(t, endpoint, printer.ID)
	require.NotEmpty(t, delivered.Data.State, "the fact arrived with no state in it")
}

// shippedGrantExample matches the request the sink section of the shipped
// configuration documents: a method and path on one comment line, and the body
// on the next.
var shippedGrantExample = regexp.MustCompile(
	`(?m)^\s*#\s+([A-Z]+) (/api/v1/grants/\S+)\n\s*#\s+(\{.*\})\s*$`)

// shippedSinkGrant reads that request out of config/config.yaml. A file that
// stops documenting one fails here rather than passing on an example nobody
// tried, which is the whole point of reading it from disk.
func shippedSinkGrant(t *testing.T) (method, path, body string) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "..", "config", "config.yaml"))
	require.NoError(t, err)

	found := shippedGrantExample.FindStringSubmatch(strings.ReplaceAll(string(content), "\r\n", "\n"))
	require.Len(t, found, 4, "config/config.yaml documents no grant request for a sink")

	return found[1], found[2], found[3]
}

// TestAnOperatorWritesTheFleetWideSinkGrantOverHTTP closes the old backlog's fleet-wide
// row at the surface an operator has. The graph could already express
// org:local#viewer@sink:audit; nothing an operator could reach could write one,
// because the grant surface administered two object types and an org was
// neither.
//
// The order is the one that makes it a fleet grant rather than a printer grant.
// The tuple is written before any printer exists, over HTTP, by the deployment's
// own administrator, and the machine that dials in afterwards reaches the sink
// with nothing written about it. The withdrawal goes back through the same
// surface, which is the half that makes the grant an operator's to manage
// rather than a line somebody has to go and edit a database to take back.
func TestAnOperatorWritesTheFleetWideSinkGrantOverHTTP(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, sinkName)
	require.NoError(t, err)

	fleet := "/api/v1/grants/org/" + bootstrapOrg

	endpoint := newWebhookEndpoint(t)
	running := start(t, t.TempDir(), sinkConfig(endpoint.URL()))

	requireSinkReady(t, running)

	written := running.call(t, http.MethodPost, fleet,
		`{"relation":"viewer","subject":"`+sinkPrincipal(instance)+`"}`)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	dialIn(t, running, "SIM-SINK-FLEET")

	printer := onlyPrinter(t, running)

	delivered := requireStateDelivered(t, endpoint, printer.ID)
	require.NotEmpty(t, delivered.Data.State,
		"one org tuple did not reach a printer admitted after it was written")

	held := running.get(t, fleet)
	require.Equal(t, http.StatusOK, held.status, held.body)
	require.Contains(t, held.body, `"relation":"viewer","subject":"`+sinkPrincipal(instance)+`"`,
		"the fleet-wide tuple is not readable through the surface that wrote it")

	withdrawn := running.call(t, http.MethodDelete,
		fleet+"?relation=viewer&subject="+url.QueryEscape(sinkPrincipal(instance)), "")
	require.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)

	held = running.get(t, fleet)
	require.NotContains(t, held.body, sinkPrincipal(instance),
		"a withdrawn fleet-wide tuple is still written on the org")
}

// TestASinkTheGraphGrantsNothingReceivesNothing is the other half of the same
// mechanism, and the reason feeding a sink is not a bypass of it. A sink is a
// principal like any other: it holds the grants an operator wrote
// for it and no others, so one that was granted nothing observes nothing,
// exactly as a person granted nothing does.
//
// The printer is proved to be publishing by the same process's own API, so the
// silence at the sink is the authorization filter and not an absent printer.
func TestASinkTheGraphGrantsNothingReceivesNothing(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	running := start(t, t.TempDir(), sinkConfig(endpoint.URL()))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SINK-2")

	printer := onlyPrinter(t, running)
	require.Contains(t, printer.State, statePath, "the printer never reported a state to withhold")

	// Ten times the coalescing window and ten times the sink's re-authorization
	// interval, so silence here is a decision rather than a wait.
	time.Sleep(2 * time.Second)

	require.Empty(t, endpoint.posted(), "a sink the graph grants nothing received an event")
}

// TestASinkStopsBeingFedWhenItIsWithdrawn holds the other end of the lifecycle:
// an instance the operator removes leaves the bus with it, rather than leaving a
// subscription and a goroutine attached to a resource that no longer exists.
//
// The subscriber count is the assertion, not the silence at the endpoint. A
// withdrawn sink is closed, so it would deliver nothing whether or not the feed
// let go of it, and a test that watched only the endpoint would pass against a
// feed that leaks a subscription and a goroutine per reconfiguration.
func TestASinkStopsBeingFedWhenItIsWithdrawn(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	bus := newSinkPlane(t, sinkConfig(endpoint.URL()))

	bus.settled(t)

	bus.declare(t, lines("core:", "  log:", "    level: error"))

	require.Eventually(t, func() bool {
		return bus.hub.Subscribers() == 0
	}, settle, 20*time.Millisecond, "a withdrawn sink kept its subscription")
}

// TestAStateSampleReachesASinkThroughTheSupervisedQueue is the feed on its own,
// with the hub driven directly rather than by a printer: what the device plane
// publishes for a state sample comes out of the shipping webhook adapter as the
// domain fact, having crossed the instance's queue on the way.
func TestAStateSampleReachesASinkThroughTheSupervisedQueue(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	bus := newSinkPlane(t, sinkConfig(endpoint.URL()))

	bus.settled(t)

	origin, err := event.NewOrigin(event.KindDevice, bus.printer)
	require.NoError(t, err)

	// Published on a ticker, because a subscription attaches at the channel's
	// head: a sample published before the sink was attached is one it was never
	// going to be given.
	stop := make(chan struct{})
	defer close(stop)

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_, _ = bus.hub.Publish(stream.Publication{
					Origin: origin,
					Type:   event.TypeDeviceTelemetrySampled,
					Class:  event.ClassTelemetry,
					Key:    statePath,
					Data:   []byte(`{"path":"/state","value":"PRINTING"}`),
				})
			}
		}
	}()

	delivered := requireStateDelivered(t, endpoint, bus.printer)
	require.Equal(t, "PRINTING", delivered.Data.State)
}

// TestASinkIsToldAStateOnlyWhenItChanges is a printer that reports the same
// state in every telemetry frame. The hub publishes each one, because a sample
// is a measurement. A sink that was told READY every two seconds would read a
// change that never happened, and a script that alerts on ERROR would alert on
// each frame for as long as the printer stays in it.
func TestASinkIsToldAStateOnlyWhenItChanges(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	bus := newSinkPlane(t, sinkConfig(endpoint.URL()))

	bus.settled(t)

	origin, err := event.NewOrigin(event.KindDevice, bus.printer)
	require.NoError(t, err)

	sample := func(state string) {
		_, _ = bus.hub.Publish(stream.Publication{
			Origin: origin,
			Type:   event.TypeDeviceTelemetrySampled,
			Class:  event.ClassTelemetry,
			Key:    statePath,
			Data:   []byte(`{"path":"/state","value":"` + state + `"}`),
		})
	}

	// Published until one arrives, for the reason the case above gives.
	stop := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				sample("READY")
			}
		}
	}()

	requireStateDelivered(t, endpoint, bus.printer)
	close(stop)
	<-stopped

	// Each repeat in a window of its own, so the hub's coalescing is not what
	// keeps them apart.
	for range 3 {
		time.Sleep(250 * time.Millisecond)
		sample("READY")
	}

	sample("IDLE")

	var states []string

	require.Eventually(t, func() bool {
		states = nil

		for _, got := range endpoint.posted() {
			if got.Kind == "printer.state_changed" {
				states = append(states, got.Data.State)
			}
		}

		return slices.Contains(states, "IDLE")
	}, settle, 50*time.Millisecond, "the printer's change to IDLE never reached the sink")

	require.Equal(t, []string{"READY", "IDLE"}, states)
}

// TestASinkComesBackWhenTheAuthorizationGraphDoes is the failure with nobody to
// report it. The hub drops a subscriber whose authorization it could not
// recompute, which is right, and the supervision plane has no status change to
// announce for it, because the instance never moved. Without the drain
// goroutine's own wake and the retry that follows a pass it could not finish, a
// sink would go silent for the life of the process on one unreadable query.
func TestASinkComesBackWhenTheAuthorizationGraphDoes(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	bus := newSinkPlane(t, sinkConfig(endpoint.URL()))

	// Settled and not merely attached: the start ladder scatters the first
	// attempt, so a case that interrupted the graph before the instance reached
	// ready would be repaired by that last status change rather than by the pass
	// this is about.
	bus.settled(t)

	bus.authz.interrupt()

	require.Eventually(t, func() bool {
		return bus.hub.Subscribers() == 0
	}, settle, 20*time.Millisecond, "the hub kept a subscriber it could not authorize")

	bus.authz.restore()

	require.Eventually(t, func() bool {
		return bus.hub.Subscribers() == 1
	}, settle, 20*time.Millisecond, "the sink never came back once the graph answered again")
}

// TestASinkIsItsOwnPrincipalAndNotAServiceAccount pins the identity an operator
// writes a grant for. It is the whole configuration surface of who a sink may
// observe, so it is a contract and not an implementation detail.
//
// The second assertion is the one that matters. Under the service-account type
// a sink and any service account of the same id would be one principal holding
// one set of grants.
func TestASinkIsItsOwnPrincipalAndNotAServiceAccount(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, sinkName)
	require.NoError(t, err)

	require.Equal(t, "sink:audit", sinkPrincipal(instance))
	require.NotEqual(t, "service_account:"+sinkName, sinkPrincipal(instance))
}

// TestAServiceAccountOfTheSinksNameFeedsItNothing is the collision, on the real
// composition root. An operator writes a grant for the service account named
// `audit`, the way they would for an integration they are about to build, and
// the webhook sink named `audit` must not be reading that printer because of it.
//
// Under one type it would read exactly that grant: the sink would subscribe as
// service_account:audit, the two would be one principal and one row of the
// projection, and a URL configured months earlier would start receiving a
// machine nobody had pointed it at.
func TestAServiceAccountOfTheSinksNameFeedsItNothing(t *testing.T) {
	t.Parallel()

	endpoint := newWebhookEndpoint(t)
	running := start(t, t.TempDir(), sinkConfig(endpoint.URL()))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SINK-3")

	printer := onlyPrinter(t, running)
	require.Contains(t, printer.State, statePath, "the printer never reported a state to withhold")

	written := running.call(t, http.MethodPost, "/api/v1/grants/device/"+printer.ID,
		`{"relation":"viewer","subject":"service_account:`+sinkName+`"}`)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	// Ten times the coalescing window and ten times the sink's re-authorization
	// interval, so the silence is a decision and not a wait.
	time.Sleep(2 * time.Second)

	require.Empty(t, endpoint.posted(),
		"the sink read a printer granted to a service account of the same name")
}

// TestOneOrgGrantFeedsASinkAPrinterAdmittedAfterIt is the old backlog's fleet-wide row on
// the real feed. One tuple, org:local#viewer@sink:audit, written before the
// printer existed, and the printer that dials in afterwards reaches the sink
// with no grant of its own.
//
// The printer is placed the way internal/app's register places one: a directory
// row and the org edge, and nothing else. What makes this work is that the org
// edge is written by the same function that admits the device, so there is no
// second mechanism to forget: a fleet of four hundred is one tuple, and the
// four hundred and first machine needs nobody at a keyboard.
func TestOneOrgGrantFeedsASinkAPrinterAdmittedAfterIt(t *testing.T) {
	t.Parallel()

	const arrivesLater = "plateradmitted"

	endpoint := newWebhookEndpoint(t)
	bus := newSinkPlaneGranting(t, sinkConfig(endpoint.URL()),
		func(t *testing.T, graph *authz.Store) {
			t.Helper()

			grantFleetToSink(t, graph)
		})

	bus.settled(t)

	// Nothing is granted per printer, so until the machine is admitted there is
	// nothing for the sink to be given.
	admitPrinter(t, bus.graph, arrivesLater)

	origin, err := event.NewOrigin(event.KindDevice, arrivesLater)
	require.NoError(t, err)

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_, _ = bus.hub.Publish(stream.Publication{
					Origin: origin,
					Type:   event.TypeDeviceTelemetrySampled,
					Class:  event.ClassTelemetry,
					Key:    statePath,
					Data:   []byte(`{"path":"/state","value":"IDLE"}`),
				})
			}
		}
	}()

	delivered := requireStateDelivered(t, endpoint, arrivesLater)
	require.Equal(t, "IDLE", delivered.Data.State)
}

// TestNoCallerCanPresentASinkPrincipal is what makes the sink type safe to put
// in an org role at all. A service account is kept out of every org role
// because a credential must not become fleet-wide by accident; a sink may hold
// one because there is no credential.
//
// Both halves are asserted. An id that happens to be a sink's name is still a
// person, and an operator who writes the sink's whole principal into the
// anonymous-subject key gets a refusal rather than the sink's grants.
func TestNoCallerCanPresentASinkPrincipal(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, sinkName)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)

	who, err := graphPrincipals{inner: unauthenticated{subject: sinkName}}.Principal(request)
	require.NoError(t, err)
	require.Equal(t, "user:"+sinkName, who)

	_, err = graphPrincipals{inner: unauthenticated{subject: sinkPrincipal(instance)}}.Principal(request)
	require.Error(t, err, "a configured subject spelled as the sink resolved to the sink")
}

// TestOnlyThePrinterStateSampleBecomesADomainFact is the mapping table in
// sinkFeed's doc, asserted. Everything the hub carries that sdk/eventsapi has no
// fact for is dropped here rather than translated into the nearest fact, because
// the nearest fact means something else to whoever is subscribed to it.
func TestOnlyThePrinterStateSampleBecomesADomainFact(t *testing.T) {
	t.Parallel()

	printer, err := event.NewOrigin(event.KindDevice, "mk4-017")
	require.NoError(t, err)

	instance, err := event.NewOrigin(event.Kind(ref.KindSink), sinkName)
	require.NoError(t, err)

	cases := map[string]struct {
		frame stream.Frame
		want  []eventsapi.PrinterState
	}{
		"the state sample is the one fact": {
			frame: sampledFrame(t, printer, `{"path":"/state","value":"PRINTING"}`),
			want:  []eventsapi.PrinterState{{ID: "mk4-017", State: "PRINTING"}},
		},
		"another telemetry path has no fact": {
			frame: sampledFrame(t, printer, `{"path":"/tools/0/nozzle_temp","value":215.4}`),
		},
		"an arrival has no fact": {
			frame: stream.Frame{
				Position: event.Position{Origin: printer},
				Class:    event.ClassState,
				Type:     event.TypeDeviceArrived,
				Bytes:    []byte(`{"data":{"device_id":"mk4-017"}}`),
			},
		},
		"a departure is not a deletion": {
			frame: stream.Frame{
				Position: event.Position{Origin: printer},
				Class:    event.ClassState,
				Type:     event.TypeDeviceDeparted,
				Bytes:    []byte(`{"data":{"device_id":"mk4-017"}}`),
			},
		},
		"presence has no fact": {
			frame: stream.Frame{
				Position: event.Position{Origin: printer},
				Class:    event.ClassState,
				Type:     event.TypeDevicePresenceChanged,
				Bytes:    []byte(`{"data":{"device_id":"mk4-017","connected":false}}`),
			},
		},
		"an instance transition is not a printer fact": {
			frame: stream.Frame{
				Position: event.Position{Origin: instance},
				Class:    event.ClassState,
				Type:     event.TypeResourceStateChanged,
				Bytes:    []byte(`{"data":{"resource":"sink:audit","state":"ready"}}`),
			},
		},
		"a state sample from something that is not a printer is dropped": {
			frame: sampledFrame(t, instance, `{"path":"/state","value":"PRINTING"}`),
		},
		"a payload that is not the sample shape is dropped": {
			frame: sampledFrame(t, printer, `{"path":"/state","value":{"unexpected":true}}`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingProcessor{}
			forwardToSink(recorder, tc.frame)

			require.Equal(t, tc.want, recorder.states)
		})
	}
}

// commandFrame is one of the three command transitions as the hub marshals it:
// a full envelope, with the place in the origin's numbering and the causation
// id that joins the three into one invocation.
func commandFrame(
	origin event.Origin,
	eventType string,
	class event.Class,
	sequence, commandID, data string,
) stream.Frame {
	return stream.Frame{
		Position: event.Position{Origin: origin},
		Class:    class,
		Type:     eventType,
		Bytes: []byte(`{"specversion":"1.0","id":"01JC0000000000000000000001",` +
			`"source":"urn:prusa:site:local/printer/` + origin.ID() + `","type":"` + eventType +
			`","time":"2026-09-02T10:00:00Z","sequence":"` + sequence +
			`","epoch":"01JC0000000000000000000EPO","causationid":"` + commandID +
			`","evclass":"` + string(class) + `","hops":0,"data":` + data + `}`),
	}
}

// TestEveryCommandLifecycleFactReachesASink is the gap this feed had: the three
// frames a command contributes to its device's origin are the whole record of
// who told a machine to do what, and no sink could see any of them, because a
// frame with no entry in the typed table reached nothing at all.
//
// It asserts the envelope arrives whole. A sink that is handed a command with
// no causation id cannot join the settled frame to the requested one, and a
// sink handed no sequence cannot tell a replay from a second command.
func TestEveryCommandLifecycleFactReachesASink(t *testing.T) {
	t.Parallel()

	printer, err := event.NewOrigin(event.KindDevice, "mk4-017")
	require.NoError(t, err)

	const commandID = "01JC000000000000000000000C"

	payloads := []string{
		`{"command_id":"` + commandID + `","verb":"printer.pause"}`,
		`{"command_id":"` + commandID + `","verb":"printer.pause","auth_type":"user","auth_id":"ada"}`,
		`{"command_id":"` + commandID + `","verb":"printer.pause","status":"succeeded","ok":true}`,
	}

	frames := []stream.Frame{
		commandFrame(printer, event.TypeDeviceCommandRequested, event.ClassCommand,
			"00000000000000000007", commandID, payloads[0]),
		commandFrame(printer, event.TypeDeviceCommandAuthorized, event.ClassAudit,
			"00000000000000000008", commandID, payloads[1]),
		commandFrame(printer, event.TypeDeviceCommandSettled, event.ClassState,
			"00000000000000000009", commandID, payloads[2]),
	}

	recorder := &recordingProcessor{}
	for _, frame := range frames {
		forwardToSink(recorder, frame)
	}

	require.Len(t, recorder.facts, len(frames), "every transition of one command has to reach the sink")
	require.Empty(t, recorder.states, "a command is not a printer state change")

	wantTypes := []string{
		event.TypeDeviceCommandRequested,
		event.TypeDeviceCommandAuthorized,
		event.TypeDeviceCommandSettled,
	}
	wantClasses := []event.Class{event.ClassCommand, event.ClassAudit, event.ClassState}

	for i, fact := range recorder.facts {
		require.Equal(t, wantTypes[i], fact.Type)
		require.Equal(t, wantClasses[i], fact.Class)
		require.Equal(t, printer, fact.Origin)
		require.Equal(t, "device:mk4-017", fact.Origin.String())
		require.Equal(t, commandID, fact.CausationID, "the three frames are joined by the command id")
		require.Equal(t, uint64(7+i), fact.Sequence)
		require.Equal(t, "01JC0000000000000000000EPO", fact.Epoch)
		require.Equal(t, 2026, fact.At.Year())
		require.JSONEq(t, payloads[i], string(fact.Payload),
			"the payload reaches the sink as the emitter wrote it")
	}
}

// TestOnlyADurablePrinterFrameBecomesAFact holds the two rules the generic door
// carries: telemetry is not a fact, and a frame that did not come from a
// printer never reaches a sink at all.
//
// The second is loop prevention and not tidiness. A script sink publishes under
// the ref of the instance that ran it, so its own event carries a sink origin;
// a sink instance changing state publishes one too. Either one forwarded back
// is a cycle that feeds itself, which is why the origin is checked before
// anything else is read.
func TestOnlyADurablePrinterFrameBecomesAFact(t *testing.T) {
	t.Parallel()

	printer, err := event.NewOrigin(event.KindDevice, "mk4-017")
	require.NoError(t, err)

	instance, err := event.NewOrigin(event.Kind(ref.KindSink), sinkName)
	require.NoError(t, err)

	site, err := event.NewOrigin(event.Kind(ref.KindLink), "fractal")
	require.NoError(t, err)

	cases := map[string]struct {
		frame stream.Frame
		want  int
	}{
		"a decision about a server under this site is a fact": {
			frame: commandFrame(site, event.TypeLinkNodeApproved, event.ClassState,
				"00000000000000000001", "", `{"link":"link:fractal","node":"b","auth_type":"user"}`),
			want: 1,
		},
		"a command from a printer is a fact": {
			frame: commandFrame(printer, event.TypeDeviceCommandSettled, event.ClassState,
				"00000000000000000001", "01JC000000000000000000000C", `{"status":"succeeded"}`),
			want: 1,
		},
		"a telemetry sample is not a fact": {
			frame: sampledFrame(t, printer, `{"path":"/state","value":"PRINTING"}`),
		},
		"a sink's own instance transition never comes back": {
			frame: commandFrame(instance, event.TypeResourceStateChanged, event.ClassState,
				"00000000000000000001", "", `{"resource":"sink:audit","state":"ready"}`),
		},
		"a frame with no place in its numbering is dropped": {
			frame: stream.Frame{
				Position: event.Position{Origin: printer},
				Class:    event.ClassState,
				Type:     event.TypeDeviceArrived,
				Bytes:    []byte(`{"data":{"device_id":"mk4-017"}}`),
			},
		},
		"a frame nothing classified is dropped": {
			frame: stream.Frame{
				Position: event.Position{Origin: printer},
				Type:     event.TypeDeviceArrived,
				Bytes:    []byte(`{"sequence":"00000000000000000001","data":{"device_id":"mk4-017"}}`),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingProcessor{}
			forwardToSink(recorder, tc.frame)

			require.Len(t, recorder.facts, tc.want)
		})
	}
}

// BenchmarkForwardToSink measures what one sink costs per event. The fan-out
// itself carries none of it: Hub.Publish, the queues and Subscription.deliver
// are untouched, and a sink is one more subscriber to them, so the whole cost
// is reading the data member back out of the envelope the hub already
// marshalled once.
//
// The two cases are the two shapes a sink's window carries: the one frame that
// becomes a domain fact, and every other frame, which is refused on its type or
// its path before anything is decoded.
func BenchmarkForwardToSink(b *testing.B) {
	printer, err := event.NewOrigin(event.KindDevice, "01JC0000000000000000000000")
	if err != nil {
		b.Fatal(err)
	}

	cases := map[string]stream.Frame{
		"the state sample": benchFrame(printer, event.TypeDeviceTelemetrySampled,
			`{"path":"/state","value":"PRINTING"}`),
		"another telemetry path": benchFrame(printer, event.TypeDeviceTelemetrySampled,
			`{"path":"/tools/0/nozzle_temp","value":215.4}`),
		"an event that is not telemetry": benchFrame(printer, event.TypeDeviceArrived,
			`{"device_id":"01JC0000000000000000000000"}`),
	}

	sink := &recordingProcessor{}

	for name, frame := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				sink.states = sink.states[:0]
				sink.facts = sink.facts[:0]

				forwardToSink(sink, frame)
			}
		})
	}
}

// benchFrame builds one frame as the hub marshals it, without a testing.T.
func benchFrame(origin event.Origin, eventType, data string) stream.Frame {
	return stream.Frame{
		Position: event.Position{Origin: origin},
		Type:     eventType,
		Bytes: []byte(`{"specversion":"1.0","id":"01JC0000000000000000000001",` +
			`"source":"urn:prusa:site:local:device:01JC0000000000000000000000","type":"` + eventType +
			`","time":"2026-08-26T10:00:00Z","evclass":"telemetry","hops":0,"data":` + data + `}`),
	}
}

// recordingProcessor is an eventsapi.Processor that keeps what it was told. It
// is not concurrent: one frame is forwarded per case.
type recordingProcessor struct {
	states []eventsapi.PrinterState
	facts  []eventsapi.Fact
}

func (r *recordingProcessor) PrinterStateChanged(state eventsapi.PrinterState) {
	r.states = append(r.states, state)
}

func (r *recordingProcessor) FactObserved(fact eventsapi.Fact) {
	r.facts = append(r.facts, fact)
}

func (r *recordingProcessor) PrinterLabelsChanged(eventsapi.PrinterLabels) {}
func (r *recordingProcessor) PrinterDeleted(string)                        {}
func (r *recordingProcessor) JobUpdated(eventsapi.JobInfo)                 {}
func (r *recordingProcessor) LabelDeleted(string)                          {}
func (r *recordingProcessor) Close() error                                 { return nil }

// TestAWindowBecomesOneSamplesPerDevice holds the third door (ADR-20261025-frz): the
// telemetry of one window reaches a sink that takes samples as one call per
// device, with the nodes in path order, and nothing else comes through it.
func TestAWindowBecomesOneSamplesPerDevice(t *testing.T) {
	t.Parallel()

	first, err := event.NewOrigin(event.KindDevice, "mk4-017")
	require.NoError(t, err)

	second, err := event.NewOrigin(event.KindDevice, "xl-002")
	require.NoError(t, err)

	instance, err := event.NewOrigin(event.Kind(ref.KindSink), sinkName)
	require.NoError(t, err)

	got := sinkSamplesOf([]stream.Frame{
		sampledFrame(t, first, `{"path":"/tools/0/nozzle_temp","value":215.4}`),
		sampledFrame(t, second, `{"path":"/tools/1/active","value":true}`),
		sampledFrame(t, instance, `{"path":"/tools/0/nozzle_temp","value":1}`),
		sampledFrame(t, first, `{"path":"/state","value":"IDLE"}`),
		sampledFrame(t, first, `not json`),
		{
			Position: event.Position{Origin: first},
			Class:    event.ClassState,
			Type:     event.TypeDeviceArrived,
			Bytes:    []byte(`{"data":{"device_id":"mk4-017"}}`),
		},
	})

	require.Equal(t, []eventsapi.Samples{
		{DeviceID: "mk4-017", Nodes: []eventsapi.Sample{
			{Path: "/state", Value: json.RawMessage(`"IDLE"`)},
			{Path: "/tools/0/nozzle_temp", Value: json.RawMessage(`215.4`)},
		}},
		{DeviceID: "xl-002", Nodes: []eventsapi.Sample{
			{Path: "/tools/1/active", Value: json.RawMessage(`true`)},
		}},
	}, got)

	require.Equal(t, eventsapi.Samples{DeviceID: "mk4-017", Whole: true, Nodes: []eventsapi.Sample{
		{Path: "/state", Value: json.RawMessage(`"IDLE"`)},
		{Path: "/tools/0/active", Value: json.RawMessage(`true`)},
	}}, wholeTree("mk4-017", map[string]json.RawMessage{
		"/tools/0/active": json.RawMessage(`true`), "/state": json.RawMessage(`"IDLE"`),
	}))
}

// sinkPlane is the feed with everything under it real and nothing above it: the
// hub, the authorization graph, the type registry, the supervision plane and
// the resource store, with no listener and no printer.
type sinkPlane struct {
	hub     *stream.Hub
	host    *resourcehost.Host
	store   *resource.Store
	graph   *authz.Store
	authz   *interruptible
	path    string
	printer string
}

// feedPrinter is the printer the graph grants this plane's sink. It is a fixed
// id because nothing dials in here.
const feedPrinter = "psinkfeed"

func newSinkPlane(t *testing.T, content string) *sinkPlane {
	t.Helper()

	return newSinkPlaneGranting(t, content, func(t *testing.T, graph *authz.Store) {
		t.Helper()

		grantPrinterToSink(t, graph, feedPrinter)
	})
}

// newSinkPlaneGranting is the same plane with the operator's grants left to the
// caller, so a case can decide what the sink was given before it ever attached.
func newSinkPlaneGranting(
	t *testing.T, content string, seed func(*testing.T, *authz.Store),
) *sinkPlane {
	t.Helper()

	dir := t.TempDir()
	graph := newGraph(t)

	seed(t, graph)

	answers := &interruptible{inner: visibility{graph: graph}}

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: answers})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	types, err := builtin.Registry()
	require.NoError(t, err)

	feed := newSinkFeed(hub, discard())

	host, err := resourcehost.New(resourcehost.Config{
		Registry: types,
		Logger:   discard(),
		OnStatus: func(status supervisor.Status) { feed.observe(status) },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	feed.start(ctx, host)

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
		feed.wait()
	})

	plane := &sinkPlane{
		hub:     hub,
		host:    host,
		graph:   graph,
		authz:   answers,
		path:    filepath.Join(dir, "config.yaml"),
		printer: feedPrinter,
	}

	write(t, plane.path, content)

	plane.store, err = resource.New(resource.Config{
		Paths:  resource.Paths{File: plane.path, Overlay: filepath.Join(dir, "config.d", "resources.json")},
		Plane:  host,
		Graph:  graph,
		Logger: discard(),
	})
	require.NoError(t, err)

	require.NoError(t, plane.store.Reload(t.Context()))

	return plane
}

// settled waits until the sink has reached ready and is on the bus, so a case
// that then breaks something is not racing the last status of the start-up.
func (p *sinkPlane) settled(t *testing.T) {
	t.Helper()

	instance, err := ref.New(ref.KindSink, sinkName)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		status, supervised := p.host.Status(instance)

		return supervised && status.Serving && p.hub.Subscribers() == 1
	}, settle, 20*time.Millisecond, "the declared sink never reached the bus")
}

// declare rewrites the operator's file and reconciles it, which is the path an
// instance is added or withdrawn on.
func (p *sinkPlane) declare(t *testing.T, content string) {
	t.Helper()

	write(t, p.path, content)
	require.NoError(t, p.store.Reload(t.Context()))
}

// interruptible is the authorization graph as the hub reads it, with a switch
// that makes it unanswerable. It stands in for the policy store being
// unreachable, which is the one failure that ends a subscription with nothing
// to report about the instance behind it.
type interruptible struct {
	inner stream.Authorizer
	down  atomic.Bool
	bumps atomic.Uint64
}

var errPolicyStoreDown = errors.New("the authorization graph could not be read")

func (i *interruptible) VisibleOrigins(ctx context.Context, principal string) (stream.Visibility, error) {
	if i.down.Load() {
		return stream.Visibility{}, errPolicyStoreDown
	}

	return i.inner.VisibleOrigins(ctx, principal)
}

// Epoch adds the test's own bumps to the graph's, so a case can force a
// re-authorization without writing a grant.
func (i *interruptible) Epoch() uint64 { return i.inner.Epoch() + i.bumps.Load() }

// interrupt makes the graph unanswerable and moves the epoch, which is what
// sends every live subscription through a re-authorization that fails.
func (i *interruptible) interrupt() {
	i.down.Store(true)
	i.bumps.Add(1)
}

func (i *interruptible) restore() {
	i.down.Store(false)
	i.bumps.Add(1)
}

// grantPrinterToSink writes the one tuple an operator writes to let this sink
// observe this printer.
func grantPrinterToSink(t *testing.T, graph *authz.Store, printer string) {
	t.Helper()

	require.NoError(t, graph.UpsertDevice(t.Context(), printer, printer))

	object, err := authz.NewObject(authz.TypeDevice, printer)
	require.NoError(t, err)

	subject, err := authz.NewSubject(authz.TypeSink, sinkName)
	require.NoError(t, err)

	granted, err := authz.NewTuple(object, authz.RelViewer, subject)
	require.NoError(t, err)

	require.NoError(t, graph.Write(t.Context(), granted))
}

// grantFleetToSink writes the one tuple an operator writes to let this sink
// observe every printer in the org, now and later. It is org#viewer and not a
// higher role because that is the only org role the vocabulary lets a sink
// hold: a sink reads.
func grantFleetToSink(t *testing.T, graph *authz.Store) {
	t.Helper()

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	subject, err := authz.NewSubject(authz.TypeSink, sinkName)
	require.NoError(t, err)

	granted, err := authz.NewTuple(org, authz.RelViewer, subject)
	require.NoError(t, err)

	require.NoError(t, graph.Write(t.Context(), granted))
}

// admitPrinter places a printer the way internal/app's register does: the
// directory row and the org edge, and no grant of any kind.
func admitPrinter(t *testing.T, graph *authz.Store, id string) {
	t.Helper()

	require.NoError(t, graph.UpsertDevice(t.Context(), id, id))

	device, err := authz.NewObject(authz.TypeDevice, id)
	require.NoError(t, err)

	tenant, err := authz.NewSubject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	placement, err := authz.NewTuple(device, authz.RelOrg, tenant)
	require.NoError(t, err)

	require.NoError(t, graph.Write(t.Context(), placement))
}

// sinkEvent is the webhook adapter's wire shape, as whatever is listening reads
// it. Naming it here rather than importing the adapter is deliberate: the field
// names are a contract with a third party, and a test that shares the
// producer's own struct cannot notice one changing.
type sinkEvent struct {
	Kind string `json:"kind"`
	Data struct {
		ID    string `json:"ID"`
		State string `json:"State"`
	} `json:"data"`
}

// webhookEndpoint is a listener standing in for whatever an operator points a
// webhook sink at.
type webhookEndpoint struct {
	server *httptest.Server

	mu       sync.Mutex
	received []sinkEvent
}

func newWebhookEndpoint(t *testing.T) *webhookEndpoint {
	t.Helper()

	endpoint := &webhookEndpoint{}

	endpoint.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		var got sinkEvent
		if err = json.Unmarshal(body, &got); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		endpoint.mu.Lock()
		endpoint.received = append(endpoint.received, got)
		endpoint.mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))

	t.Cleanup(endpoint.server.Close)

	return endpoint
}

func (e *webhookEndpoint) URL() string { return e.server.URL + "/events" }

func (e *webhookEndpoint) posted() []sinkEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]sinkEvent(nil), e.received...)
}

// sinkConfig declares one webhook sink pointed at the endpoint.
func sinkConfig(url string) string {
	return lines(
		"core:", "  log:", "    level: error",
		"resources:", "  sink:", "    "+sinkName+":",
		"      type: webhook",
		"      enabled: true",
		"      config:",
		"        url: "+url,
		"        timeout: 2s")
}

func requireSinkReady(t *testing.T, running *process) {
	t.Helper()

	require.Eventually(t, func() bool {
		return running.instanceState(t, "sink/"+sinkName) == "ready"
	}, settle, 20*time.Millisecond, "the declared sink never reported ready")
}

// grantSink writes the one tuple that lets this sink observe this printer,
// through the published API, the way an operator would.
func grantSink(t *testing.T, running *process, printer string) {
	t.Helper()

	instance, err := ref.New(ref.KindSink, sinkName)
	require.NoError(t, err)

	written := running.call(t, http.MethodPost, "/api/v1/grants/device/"+printer,
		`{"relation":"viewer","subject":"`+sinkPrincipal(instance)+`"}`)

	require.Equal(t, http.StatusCreated, written.status, written.body)
}

func onlyPrinter(t *testing.T, running *process) httpapiPrinter {
	t.Helper()

	listed := printers(t, running)
	require.Len(t, listed, 1)

	return httpapiPrinter{ID: listed[0].ID, State: listed[0].State}
}

// httpapiPrinter is the two fields these cases read, so the helper above does
// not have to name the transport's type in every signature.
type httpapiPrinter struct {
	ID    string
	State map[string]json.RawMessage
}

// requireStateDelivered waits for the printer's state to reach the endpoint and
// returns the first one that did.
func requireStateDelivered(t *testing.T, endpoint *webhookEndpoint, printer string) sinkEvent {
	t.Helper()

	var found sinkEvent

	require.Eventually(t, func() bool {
		for _, got := range endpoint.posted() {
			if got.Kind == "printer.state_changed" && got.Data.ID == printer {
				found = got

				return true
			}
		}

		return false
	}, settle, 50*time.Millisecond, "no printer state change reached the sink")

	return found
}

// sampledFrame builds a telemetry frame carrying data, as the hub marshals one.
func sampledFrame(t *testing.T, origin event.Origin, data string) stream.Frame {
	t.Helper()

	return stream.Frame{
		Position: event.Position{Origin: origin},
		Class:    event.ClassTelemetry,
		Type:     event.TypeDeviceTelemetrySampled,
		Bytes:    []byte(`{"type":"` + event.TypeDeviceTelemetrySampled + `","data":` + data + `}`),
	}
}
