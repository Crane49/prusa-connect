package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// nozzleVerb is a capability every profile in this repository declares, and the
// one the simulator answers with an ACCEPTED event.
const nozzleVerb = "core.command.set_nozzle_temperature"

// TestASimulatedPrinterDialsInAndTakesACommand is the demo, on the real
// composition root: the binary's own connector mount, the real capability
// resolver, the real authorization graph, and a printer that dials in over the
// frozen wire and is driven through the published API.
func TestASimulatedPrinterDialsInAndTakesACommand(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-0001")

	listed := printers(t, running)
	require.Len(t, listed, 1, "the printer that dialled in is not in the listing")

	found := listed[0]
	require.Equal(t, "MK4", found.Model)
	require.Equal(t, "Prusa", found.Vendor)
	require.Equal(t, "SIM-0001", found.Serial)
	require.Equal(t, "xbuddy", found.Driver)
	require.True(t, found.Connected)
	require.True(t, found.ProfileMatched, "no capability profile covered a model this repository ships one for")
	require.NotEmpty(t, found.Capabilities)
	require.Contains(t, found.Capabilities, nozzleVerb)

	// Arrive and not Resolve. The epoch is per unit and starts at one, so a
	// resolution taken at an epoch the caller named would report zero here.
	require.GreaterOrEqual(t, found.CapabilityEpoch, uint64(1),
		"the capability epoch never moved, so the printer was resolved rather than admitted")

	require.Contains(t, found.State, "/tools/0/nozzle_temp", "no telemetry reached the state tree")
	require.Contains(t, found.State, "/state")

	sent := running.call(t, http.MethodPost, "/api/v1/devices/"+found.ID+"/commands",
		`{"verb":"`+nozzleVerb+`","args":{"nozzle_temperature":"215"}}`)
	require.Equal(t, http.StatusOK, sent.status, sent.body)
	require.Contains(t, sent.body, `"ok":true`)
}

// unmountedPath names a storage device the simulated printer has not mounted.
// The printer answers a command on one with REJECTED and MEDIA_NOT_INSERTED,
// which is a refusal nothing above the driver can predict: which sticks are in
// a machine is not in any capability set.
const unmountedPath = "/nomedia/thing.bgcode"

// TestAPrinterRefusalCarriesThePrintersOwnWordsToTheCaller is the bench case a
// colleague hit on a real Core One: the machine answers a command with the
// sentence it refused by, and an operator over HTTP who learns only that it
// said no cannot act on it.
//
// The assertion is on the bytes POST .../commands answered with. A unit test on
// the struct proves nothing about the wire, and the wire is where the reason
// was being dropped.
//
// It is also where the two refusals are told apart. The verb is in the resolved
// set and every argument passed the descriptor, so the API rejected nothing:
// this is 200 with a body that reports what the machine answered, and not the
// problem document a host-side refusal gets.
func TestAPrinterRefusalCarriesThePrintersOwnWordsToTheCaller(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-REFUSAL")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	refused := running.call(t, http.MethodPost, "/api/v1/devices/"+listed[0].ID+"/commands",
		`{"verb":"core.command.start_print","args":{"path":"`+unmountedPath+`"}}`)

	t.Logf("observed: POST /api/v1/devices/%s/commands answered %d %s",
		listed[0].ID, refused.status, refused.body)

	require.Equal(t, http.StatusOK, refused.status, refused.body)

	var result httpapi.PrinterResult
	require.NoError(t, json.Unmarshal([]byte(refused.body), &result))
	require.False(t, result.OK, "the printer accepted a print from a stick that is not in it")

	require.Contains(t, refused.body, `"machine_reason":"`+simulator.ReasonMediaNotInserted+`"`,
		"the printer said why it refused and the HTTP caller was not told")

	// The same field on a command the printer carried out would leave a caller
	// holding two answers, and connectorapitest fails a driver that does it. The
	// wire keeps that true whatever a driver sends.
	accepted := running.call(t, http.MethodPost, "/api/v1/devices/"+listed[0].ID+"/commands",
		`{"verb":"`+nozzleVerb+`","args":{"nozzle_temperature":"215"}}`)

	t.Logf("observed: an accepted command answered %d %s", accepted.status, accepted.body)

	require.Equal(t, http.StatusOK, accepted.status, accepted.body)
	require.Contains(t, accepted.body, `"ok":true`)
	require.NotContains(t, accepted.body, "machine_reason",
		"a command the printer carried out reported a reason it was refused")
}

// TestAVerbThePrinterDoesNotHaveNeverReachesTheDriver is an earlier design decision on the
// real surface. The refusal is the host's, decided against the resolved
// descriptor, so it names the verb rather than reporting that the printer said
// no.
//
// start_inline_download is in the capability vocabulary and in no driver's
// baseline, so it is a verb that exists and that no printer here resolves. That
// is the case this decision is about, and it is separate from a verb nothing
// declares at all, which the test below covers.
func TestAVerbThePrinterDoesNotHaveNeverReachesTheDriver(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-0002")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	const declaredNowhere = "core.command.start_inline_download"

	require.Contains(t, authz.VerbActions(authz.DeviceKindPrinter), declaredNowhere,
		"the vocabulary no longer names this verb, so this case is no longer the one it was written for")

	refused := running.call(t, http.MethodPost, "/api/v1/devices/"+listed[0].ID+"/commands",
		`{"verb":"`+declaredNowhere+`"}`)

	require.Equal(t, http.StatusConflict, refused.status, refused.body)
	require.Contains(t, refused.body, connectorapi.CapabilityNotSupported.Code().String())
	require.Contains(t, refused.body, declaredNowhere)
}

// A verb the capability vocabulary does not name takes no authorization action,
// so nothing can decide it and this server refuses it.
//
// The refusal carries the verb and no fact about a printer. It is the same
// answer for a printer that is there and for an id that never answered, which
// is what stops a caller holding nothing from using a typo to test whether a
// printer exists. The instance member echoes the path the caller sent, which is
// what RFC 9457 puts there and is the caller's own input.
func TestAVerbTheVocabularyDoesNotNameIsRefusedWithoutNamingAPrinter(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-0003")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	const undeclared = `{"verb":"core.command.launch_rocket"}`

	require.NotContains(t, authz.VerbActions(authz.DeviceKindPrinter), "core.command.launch_rocket",
		"the vocabulary now names this verb, so this case no longer tests an unknown one")

	onAPrinter := running.call(t, http.MethodPost,
		"/api/v1/devices/"+listed[0].ID+"/commands", undeclared)

	require.Equal(t, http.StatusBadRequest, onAPrinter.status, onAPrinter.body)
	require.Contains(t, onAPrinter.body, fault.PrinterVerbUnknown.Code().String())
	require.Contains(t, onAPrinter.body, "core.command.launch_rocket")
	require.NotContains(t, onAPrinter.body, "device_id",
		"the refusal carries a printer parameter, so a caller learns the id reached one")

	onNothing := running.call(t, http.MethodPost,
		"/api/v1/devices/00000000-0000-4000-8000-00000dead000/commands", undeclared)

	require.Equal(t, onAPrinter.status, onNothing.status, onNothing.body)
	require.Contains(t, onNothing.body, fault.PrinterVerbUnknown.Code().String())
	require.Equal(t, paramsOf(t, onAPrinter.body), paramsOf(t, onNothing.body),
		"the two refusals differ, so the difference says whether the id reached a printer")
}

// paramsOf reads the params member of a problem document, which is where a
// refusal carries anything a caller can branch on.
func paramsOf(t *testing.T, body string) map[string]any {
	t.Helper()

	var document struct {
		Params map[string]any `json:"params"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &document), body)

	return document.Params
}

// TestAPrinterNothingDialledInIsAbsent keeps the refusal to one shape, and keeps
// the listing honest on a process nothing has reached.
func TestAPrinterNothingDialledInIsAbsent(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	require.Empty(t, printers(t, running))

	missing := running.get(t, "/api/v1/devices/00000000-0000-4000-8000-00000dead000")

	require.Equal(t, http.StatusNotFound, missing.status)
	require.Contains(t, missing.body, fault.PrinterNotFound.Code().String())
}

// TestTheStateChannelClosingReleasesTheSession is the host's half of the
// connectorapi contract: it owns the session from Arrived and releases it when
// the state channel closes. A host that only dropped the reference would leave
// the driver holding a session nothing will ever close, and the printer would
// stay in the listing for the life of the process.
func TestTheStateChannelClosingReleasesTheSession(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &countedSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: "SIM-0003"},
		patches:  make(chan connectorapi.StatePatch),
	}

	_, err := host.Admit(t.Context(), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(t.Context(), session))

	id := plane.idOf(session.identity)

	session.patches <- connectorapi.StatePatch{Path: "/state", Value: json.RawMessage(`"IDLE"`)}

	require.Eventually(t, func() bool {
		entry, live := plane.entry(id)

		return live && len(entry.snapshot()) == 1
	}, time.Second, time.Millisecond, "the patch never reached the state entry")

	close(session.patches)

	require.Eventually(t, func() bool {
		_, live := plane.entry(id)

		return !live && session.closes.Load() > 0
	}, time.Second, time.Millisecond,
		"the state channel closed and the session was left open, or the entry was left in the table")
}

// TestADialogTheMachineRaisedIsReadableThroughTheAPI is the bench half of
// core.command.dialog_action.
//
// The verb names a dialog id, and an id on its own is a button somebody presses
// without knowing what it agrees to. What the firmware put on the screen is what
// says whether to press Retry, and it has to reach the same place the id does.
//
// The frames are written onto the socket by hand. The simulator raises no dialog,
// and the point of this case is the frame a real machine sends rather than one
// this repository's own encoder produces.
func TestADialogTheMachineRaisedIsReadableThroughTheAPI(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-DIALOG",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	takeOffTheWire(t, running, printer, id)

	conn := dialAsPrinter(t, running, printer.printer)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(
		`{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
			`"data":{"code":"13501","title":"Filament runout",`+
			`"text":"Insert filament, then press Retry.","buttons":["Retry","Cancel"]}}`)))

	require.Eventually(t, func() bool {
		return len(dialogOf(t, running, id)) == dialogPaths
	}, settle, 25*time.Millisecond,
		"the dialog never reached the state tree; it stands at %v", dialogOf(t, running, id))

	raised := dialogOf(t, running, id)
	t.Logf("observed: GET /api/v1/devices/%s reports the dialog as %v", id, raised)

	require.Equal(t, "42", raised["/dialog/id"])
	require.Equal(t, `"13501"`, raised["/dialog/code"])
	require.Equal(t, `"Filament runout"`, raised["/dialog/title"])
	require.Equal(t, `"Insert filament, then press Retry."`, raised["/dialog/text"])
	require.Equal(t, `["Retry","Cancel"]`, raised["/dialog/buttons"],
		"the button labels are what a caller chooses between, and they did not arrive")

	// The machine leaves the state a dialog is up in, which is the only thing
	// that says the dialog is gone. A title that outlives its dialog describes a
	// machine that has moved on.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"state":"IDLE","temp_nozzle":215}`)))

	require.Eventually(t, func() bool {
		return len(dialogOf(t, running, id)) == 0
	}, settle, 25*time.Millisecond,
		"the dialog outlived the state it was raised in; it stands at %v", dialogOf(t, running, id))

	t.Logf("observed: IDLE left the state tree with no dialog paths at all")
}

// dialogPaths is how many paths one whole dialog occupies: the id and the four
// things the firmware displayed.
const dialogPaths = 5

// dialogOf is the dialog subtree as the published API reports it, with each
// value as the printer encoded it.
func dialogOf(t *testing.T, running *process, id string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	for path, value := range fetch(t, running, id).State {
		if strings.HasPrefix(path, "/dialog/") {
			tree[path] = string(value)
		}
	}

	return tree
}

// TestADriverThatExplainsAnAcceptedCommandIsNotBelieved is the guard on the
// half of the refusal-only rule a conformant driver cannot reach.
//
// The driver contract says a machine reason accompanies a refusal and nothing
// else, and connectorapitest fails a driver that breaks it. A driver is
// third-party code, so passing that suite once is not a property of the driver
// running in front of an operator. A caller handed both "the printer did it" and
// "here is why it did not" acts on whichever it read first, and reprints a file
// the machine already had.
func TestADriverThatExplainsAnAcceptedCommandIsNotBelieved(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	id := arriveTalking(t, plane, "SIM-TALKATIVE", connectorapi.Result{
		OK: true, MachineReason: "FILE_EXISTS",
	})

	accepted, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
		httpapi.PrinterCommand{Verb: nozzleVerb})
	require.NoError(t, err)

	t.Logf("observed: the driver answered OK=%v MachineReason=%q and the plane reported %+v",
		true, "FILE_EXISTS", accepted)

	require.True(t, accepted.OK)
	require.Empty(t, accepted.MachineReason,
		"a command the printer carried out reported a reason it was refused")

	// The same session refusing. Without this the case above is satisfied by a
	// plane that never carries a reason at all.
	refusing := newPlane(t)
	refused, err := refusing.Command(t.Context(), "user:"+config.DefaultPrincipal,
		arriveTalking(t, refusing, "SIM-REFUSING", connectorapi.Result{
			OK: false, MachineReason: "FILE_EXISTS",
		}),
		httpapi.PrinterCommand{Verb: nozzleVerb})
	require.NoError(t, err)

	require.False(t, refused.OK)
	require.Equal(t, "FILE_EXISTS", refused.MachineReason)
}

// talkingSession is a driver session that declares one verb and answers every
// command with a result the test chose, including one the contract forbids.
type talkingSession struct {
	countedSession

	answer connectorapi.Result
}

func (s *talkingSession) Capabilities() (connectorapi.Set, uint64) { return talkingBaseline(), 1 }

// talkingBaseline is the one verb the session above offers, as both the
// driver's baseline and the printer's own probe. A resolved set is the first
// narrowed by the second, so a case that declared only one of them would be
// commanding a printer with no capabilities.
func talkingBaseline() connectorapi.Set {
	return connectorapi.Set{connectorapi.Capability(nozzleVerb): {Flag: true}}
}

func (s *talkingSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return s.answer, nil
}

// arriveTalking puts one printer in the plane through the host surface a driver
// uses, with a session that answers commands rather than refusing every verb.
func arriveTalking(
	t *testing.T, plane *devices, serial string, answer connectorapi.Result,
) string {
	t.Helper()

	host := plane.Arrivals("test", talkingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &talkingSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
			patches:  make(chan connectorapi.StatePatch),
		},
		answer: answer,
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}

// dialIn runs a simulated printer against the address the core is serving on
// and waits until it is in the listing with telemetry behind it.
func dialIn(t *testing.T, running *process, serial string) {
	t.Helper()

	dialInFleet(t, running, serial)
}

// dialInFleet is dialIn for a site rather than a machine. Every printer is
// started before any of them is waited for, so they register at once, which is
// the order a farm powering up arrives in and the one a single dial-in cannot
// reach.
func dialInFleet(t *testing.T, running *process, serials ...string) {
	t.Helper()

	for _, serial := range serials {
		printer, err := simulator.New(simulator.Config{
			Model:   "MK4",
			Serial:  serial,
			BaseURL: "http://" + running.addr,
		})
		require.NoError(t, err)

		ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
		done := make(chan error, 1)

		go func() { done <- printer.RegisterAndRun(ctx) }()

		t.Cleanup(func() {
			stop()
			<-done
		})
	}

	require.Eventually(t, func() bool {
		listed := printers(t, running)
		if len(listed) != len(serials) {
			return false
		}

		for _, one := range listed {
			if !telemetered(one.State) {
				return false
			}
		}

		return true
	}, settle, 25*time.Millisecond, "the simulated printers never appeared in the listing")
}

// printers reads the listing through the published API, which is the same bytes
// a client reads.
func printers(t *testing.T, running *process) []httpapi.Device {
	t.Helper()

	return listedPrinters(t, running, "")
}

// listedPrinters is the same read with the listing's own parameters, for a case
// about the rows the default answer leaves out.
func listedPrinters(t *testing.T, running *process, query string) []httpapi.Device {
	t.Helper()

	path := "/api/v1/devices"
	if query != "" {
		path += "?" + query
	}

	read := running.get(t, path)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var listing struct {
		Items []httpapi.Device `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

	return listing.Items
}

// idsOf is the id of every device on a listing, in the order it answered.
func idsOf(listed []httpapi.Device) []string {
	out := make([]string, 0, len(listed))
	for _, one := range listed {
		out = append(out, one.ID)
	}

	return out
}

// newPlane is a device plane over a real graph and a real hub, for the cases a
// simulated printer cannot reach.
func newPlane(t *testing.T) *devices {
	t.Helper()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: graph}, Logger: discard()})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	return newPlaneOn(t, graph, hub)
}

// newPlaneOn is the device plane over a hub the caller owns, for a case that
// reads what the plane published rather than only what it stored.
func newPlaneOn(t *testing.T, graph *authz.Store, hub *stream.Hub) *devices {
	t.Helper()

	plane := newDevices(graph, recordsOf(t, graph), hub, discard(), bootstrapOrg, staging{}, fleetMeters{}, 0)

	t.Cleanup(plane.Close)

	return plane
}

// countedSession is a driver session whose state channel and Close the test
// owns.
type countedSession struct {
	identity connectorapi.Identity
	patches  chan connectorapi.StatePatch
	closes   atomic.Int64
}

func (s *countedSession) Identity() connectorapi.Identity { return s.identity }

func (s *countedSession) Capabilities() (connectorapi.Set, uint64) { return connectorapi.Set{}, 1 }

func (s *countedSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{OK: true}, nil
}

func (s *countedSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *countedSession) Close() error {
	s.closes.Add(1)

	return nil
}

// TestAnAdmissionWhoseSessionNeverArrivesIsForgotten bounds the table admit
// fills. Admit and Arrived are two steps of one connection, so a resolution
// waits milliseconds for its session; a connection that drops in between, or a
// driver that reattaches the session it already had, leaves one parked that
// nothing ever claims.
func TestAnAdmissionWhoseSessionNeverArrivesIsForgotten(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane := &devices{admitted: map[string]parkedResolution{}}

		require.NoError(t, plane.record("abandoned", connectorapi.Identity{}, capability.Resolution{}))

		time.Sleep(admissionLifetime + time.Second)

		require.NoError(t, plane.record("arriving", connectorapi.Identity{}, capability.Resolution{}))

		_, waiting := plane.claim("abandoned")
		require.False(t, waiting, "a resolution whose session never arrived is still parked")

		_, waiting = plane.claim("arriving")
		require.True(t, waiting, "the resolution of the printer that just dialled in was dropped")
	})
}

// telemetered reports whether a telemetry frame reached the state tree. The
// INFO event puts the printer's network there first, so a count of paths
// would read a printer that has sent no telemetry yet as one that has.
func telemetered(state httpapi.DeviceState) bool {
	_, held := state["/tools/0/nozzle_temp"]

	return held
}
