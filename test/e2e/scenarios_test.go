package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// printerSampled is the event type one node of a printer's state tree travels
// under. It is spelled out rather than imported: a subscriber outside this
// process reads it off the wire, and a constant shared with the publisher would
// make a rename invisible to exactly the readers it breaks.
const printerSampled = "com.prusa.connect.device.telemetry.sampled"

// chamberTemperature is a verb the CoreOne family declares and a MINI does not,
// so it is what a refusal is asked for. The model rather than the verb is what
// makes it undeclared, which is the point.
const chamberTemperature = "core.command.set_chamber_target_temp"

// sendInfo is a verb every profile declares and which takes no arguments, so a
// command scenario needs no argument value it would have had to invent.
const sendInfo = "core.command.send_info"

// nozzleVerb is what this API calls the command the shipping server calls
// SET_NOZZLE_TEMPERATURE. It is spelled out rather than imported for the reason
// printerSampled is: a caller outside this process reads it off the wire.
const nozzleVerb = "core.command.set_nozzle_temperature"

// createOnly is the precondition a write that must not replace anything sends.
//
//nolint:gochecknoglobals // a fixture, read-only after init.
var createOnly = map[string]string{"If-None-Match": "*"}

// TestTheBinaryStartsAndBecomesReady is the scenario every other one rests on:
// the shipped binary reads an operator's file, constructs the plane, binds a
// listener and reports itself ready.
//
// It catches a startup order regression, a configuration key that stopped being
// read, and a required resource that never reaches ready.
func TestTheBinaryStartsAndBecomesReady(t *testing.T) {
	t.Parallel()

	server := startServer(t)

	health := server.get(t, "/api/v1/health")
	if health.status != http.StatusOK {
		t.Fatalf("/api/v1/health answered %d: %s", health.status, health.body)
	}

	server.awaitReady(t)

	report := server.get(t, "/api/v1/readiness").decode(t)
	t.Logf("observed readiness: %v", report)

	// The storage type builds its root, so the directory existing is the
	// resource having actually run rather than having been declared.
	root := filepath.Join(server.dir, "storage")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("the required storage instance reported ready and %s is not a directory: %v", root, err)
	}

	listed := server.get(t, "/api/v1/resources")
	if listed.status != http.StatusOK {
		t.Fatalf("/api/v1/resources answered %d: %s", listed.status, listed.body)
	}

	if !strings.Contains(string(listed.body), `"name":"local"`) {
		t.Fatalf("the declared instance is not in the listing: %s", listed.body)
	}

	t.Logf("observed: the binary served %s and its required storage instance is ready", server.baseURL)
}

// TestThePrinterPathIsServed is the guard on every scenario below it. Each one
// needs the inbound connector mounted and the printer routes published, and a
// suite whose printer scenarios quietly stopped exercising a printer is how a
// lane stays green for a year without running.
func TestThePrinterPathIsServed(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	// The probe is the Code header and not the status. The code is the one thing
	// no other handler on this origin can produce, so a status alone would
	// report an unmounted connector as a mounted one.
	probe := `{"sn":"PROBE","fingerprint":"PROBE","printer_type":"1.4.0","firmware":"6.4.0"}`

	answer := server.do(t, http.MethodPost, "/p/register", probe, nil)
	if answer.status != http.StatusOK || answer.header.Get("Code") == "" {
		t.Fatalf("POST /p/register answered %d and issued code %q, so no inbound connector is mounted and "+
			"every printer scenario below is exercising nothing. The driver is "+
			"internal/connector/xbuddy and the mount is internal/app/connectors.go. Log:\n%s",
			answer.status, answer.header.Get("Code"), server.stderr.String())
	}

	t.Logf("observed: POST /p/register answered %d and issued code %q",
		answer.status, answer.header.Get("Code"))

	for _, route := range []string{"/api/v1/devices", "/api/v1/devices/{device_id}/commands"} {
		if !server.publishes(t, route) {
			t.Errorf("the served OpenAPI document does not publish %s, so there is no route to reach a "+
				"printer through", route)
		}
	}
}

// TestAResourceRoundTripsThroughTheAPI writes an instance through the API,
// reads it back, and requires the overlay on disk to be what changed.
//
// It catches a write that reaches the operator's hand-authored file, a
// reconcile that never runs, and a read that answers from memory rather than
// from the plane.
func TestAResourceRoundTripsThroughTheAPI(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	scratch := filepath.Join(server.dir, "scratch")
	body := fmt.Sprintf(`{"type":"filesystem","enabled":true,"required":false,"config":{"root":%q}}`,
		filepath.ToSlash(scratch))

	// The overlay has another writer, so a write carries a precondition. A
	// create sends If-None-Match: * and a replace sends the tag it read: a
	// write with neither is refused with 428, which is an earlier design decision and not a
	// quirk of this test.
	if bare := server.do(t, http.MethodPut, "/api/v1/resources/storage/scratch", body, nil); bare.status !=
		http.StatusPreconditionRequired {
		t.Errorf("an unfenced write answered %d, want %d", bare.status, http.StatusPreconditionRequired)
	}

	created := server.do(t, http.MethodPut, "/api/v1/resources/storage/scratch", body, createOnly)
	if created.status != http.StatusOK && created.status != http.StatusCreated {
		t.Fatalf("PUT answered %d: %s", created.status, created.body)
	}

	read := server.get(t, "/api/v1/resources/storage/scratch")
	if read.status != http.StatusOK {
		t.Fatalf("GET answered %d: %s", read.status, read.body)
	}

	instance := read.decode(t)
	if instance["source"] != "overlay" {
		t.Errorf("the instance came from %v, and a write through the API lands in the overlay",
			instance["source"])
	}

	overlay, err := os.ReadFile(filepath.Join(server.dir, "resources.json"))
	if err != nil {
		t.Fatalf("read the overlay: %v", err)
	}

	if !strings.Contains(string(overlay), "scratch") {
		t.Fatalf("the overlay does not carry the instance the API wrote: %s", overlay)
	}

	eventually(t, "the new instance built its root", func() bool {
		info, statErr := os.Stat(scratch)

		return statErr == nil && info.IsDir()
	})

	removed := server.do(t, http.MethodDelete, "/api/v1/resources/storage/scratch", "",
		map[string]string{"If-Match": read.header.Get("ETag")})
	if removed.status != http.StatusNoContent {
		t.Fatalf("DELETE answered %d: %s", removed.status, removed.body)
	}

	if gone := server.get(t, "/api/v1/resources/storage/scratch"); gone.status != http.StatusNotFound {
		t.Fatalf("the deleted instance answered %d: %s", gone.status, gone.body)
	}

	t.Logf("observed: storage/scratch was written to the overlay, built its root and was removed")
}

// TestAnEventStreamSubscriberSeesWhatThePlanePublishes opens the server-sent
// event stream as a browser does and requires a change made through the API to
// reach it.
func TestAnEventStreamSubscriberSeesWhatThePlanePublishes(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	scratch := filepath.Join(server.dir, "watched")
	body := fmt.Sprintf(`{"type":"filesystem","enabled":%%t,"required":false,"config":{"root":%q}}`,
		filepath.ToSlash(scratch))

	created := server.do(t, http.MethodPut, "/api/v1/resources/storage/watched",
		fmt.Sprintf(body, true), createOnly)
	if created.status != http.StatusOK && created.status != http.StatusCreated {
		t.Fatalf("PUT answered %d: %s", created.status, created.body)
	}

	eventually(t, "the new instance reached ready", func() bool {
		return strings.Contains(string(server.get(t, "/api/v1/resources/storage/watched").body), `"ready"`)
	})

	// The instance exists before the subscription opens. An origin created
	// after one opens does not reliably reach it, which is an open task on
	// the old backlog and the reason this scenario is ordered the way it is.
	ctx, cancel := context.WithTimeout(t.Context(), settleBudget)
	defer cancel()

	frames, errs := server.subscribe(ctx, t)

	read := server.get(t, "/api/v1/resources/storage/watched")

	stopped := server.do(t, http.MethodPut, "/api/v1/resources/storage/watched",
		fmt.Sprintf(body, false), map[string]string{"If-Match": read.header.Get("ETag")})
	if stopped.status != http.StatusOK {
		t.Fatalf("disabling the instance answered %d: %s", stopped.status, stopped.body)
	}

	seen := awaitFrame(ctx, t, frames, errs, func(f frame) bool {
		return strings.Contains(f.data, "storage:watched")
	})

	t.Logf("observed frame: id %s, event %s, data %s", seen.id, seen.event, seen.data)
}

// TestAPrinterDialsInAndIsAdmitted drives a simulated printer over a real
// socket against the running binary: the frozen registration handshake, the
// websocket, and then the printer appearing on the API a caller reads.
func TestAPrinterDialsInAndIsAdmitted(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	printer, listed := server.attachPrinter(t, "MK4")

	if listed.Model != "MK4" {
		t.Errorf("the API reports the model as %q, want MK4", listed.Model)
	}

	if listed.Serial != printer.Serial() {
		t.Errorf("the API reports the serial as %q, want %q", listed.Serial, printer.Serial())
	}

	if !listed.Connected {
		t.Error("the API reports the printer as not connected while its session is open")
	}

	if !listed.ProfileMatched {
		t.Errorf("no capability profile covered %s on firmware %q", listed.Model, listed.Firmware)
	}

	if len(listed.Capabilities) == 0 {
		t.Error("the printer was admitted with no capabilities, so nothing can be commanded")
	}

	t.Logf("observed: %s registered against %s, is %s on the API with %d capabilities and epoch %d",
		printer.Serial(), server.baseURL, listed.ID, len(listed.Capabilities), listed.CapabilityEpoch)
}

// TestTelemetryReachesAnEventStreamSubscriber is the product's main path, end
// to end: a printer's own cadence on one socket, a browser's event stream on
// another, and the whole binary in between.
func TestTelemetryReachesAnEventStreamSubscriber(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	ctx, cancel := context.WithTimeout(t.Context(), settleBudget)
	defer cancel()

	frames, errs := server.subscribe(ctx, t)

	printer, listed := server.attachPrinter(t, "MK4")

	// The printer's arrival is not asserted here. It is published to the new
	// printer's channel before any live subscriber has been attached to it, so
	// it lands in that channel's history and reaches nobody who was already
	// watching. That is an open task on the old backlog rather than something this
	// scenario can assert around.
	sample := awaitFrame(ctx, t, frames, errs, func(f frame) bool {
		return f.event == printerSampled && strings.Contains(f.data, `"/state"`)
	})
	t.Logf("observed telemetry: %s", sample.data)

	var envelope struct {
		Type    string `json:"type"`
		Subject string `json:"subject"`
		Data    struct {
			Path  string          `json:"path"`
			Value json.RawMessage `json:"value"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(sample.data), &envelope); err != nil {
		t.Fatalf("the telemetry frame is not an envelope: %v", err)
	}

	if envelope.Type != printerSampled {
		t.Errorf("the envelope carries type %q, want %q", envelope.Type, printerSampled)
	}

	if !strings.Contains(envelope.Subject, listed.ID) {
		t.Errorf("the envelope names subject %q, and the printer is %q", envelope.Subject, listed.ID)
	}

	if envelope.Data.Path != "/state" {
		t.Errorf("the frame carries path %q, want /state", envelope.Data.Path)
	}

	if string(envelope.Data.Value) != `"IDLE"` {
		t.Errorf("the frame carries state %s, want \"IDLE\"", envelope.Data.Value)
	}

	// The same value reaches a caller that asks rather than subscribes.
	eventually(t, "the state tree carried the printer's state", func() bool {
		return server.printer(t, listed.ID).State["/state"] != nil
	})

	t.Logf("observed: %s telemetry reached the stream as %s and /api/v1/devices/%s",
		printer.Serial(), envelope.Subject, listed.ID)
}

// TestACommandSentThroughTheAPIArrivesAndReportsBack is the other direction: a
// caller's request becomes a frame on the printer's socket and the printer's
// answer becomes the response body.
func TestACommandSentThroughTheAPIArrivesAndReportsBack(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MK4")

	answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q}`, sendInfo), nil)
	if answer.status != http.StatusOK {
		t.Fatalf("POST a command answered %d: %s", answer.status, answer.body)
	}

	result := answer.decode(t)
	if result["ok"] != true {
		t.Fatalf("the printer answered %s, and ok is not true", answer.body)
	}

	t.Logf("observed: %s reached the printer and came back %s", sendInfo, answer.body)
}

// TestACommandQueuedWhileOfflineDrainsOnReconnect is the rule earlier research
// section 3 says is the one most likely to be lost in a rewrite, asserted at
// the API boundary rather than in the driver.
func TestACommandQueuedWhileOfflineDrainsOnReconnect(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	printer, listed := server.attachPrinter(t, "MK4")

	printer.GoOffline()

	if !printer.Offline() {
		t.Fatal("the printer was told to go offline and reports itself online")
	}

	// GoOffline closes the socket from the printer's end, and the server learns
	// of it when the read fails. Until it has, a command is dispatched onto a
	// connection that is already going and comes back COMMAND_UNANSWERED, which
	// is the right answer to a command that raced a disconnect and the wrong
	// scenario for this test. Waiting for the server's own view is what makes
	// the queue the thing under test rather than the width of that window.
	eventually(t, "the server reports the printer away", func() bool {
		return !server.printer(t, listed.ID).Connected
	})

	answered := make(chan response, 1)

	go func() {
		answered <- server.doWithin(t, settleBudget, http.MethodPost,
			"/api/v1/devices/"+listed.ID+"/commands", fmt.Sprintf(`{"verb":%q}`, sendInfo), nil)
	}()

	select {
	case early := <-answered:
		t.Fatalf("the command was answered %d while the printer was away: %s", early.status, early.body)
	case <-time.After(earlyAnswerWindow):
	}

	t.Log("observed: the command is queued rather than refused while the printer is away")

	printer.GoOnline()

	select {
	case drained := <-answered:
		if drained.status != http.StatusOK {
			t.Fatalf("the queued command answered %d after the printer came back: %s",
				drained.status, drained.body)
		}

		if drained.decode(t)["ok"] != true {
			t.Fatalf("the queued command drained and was refused: %s", drained.body)
		}

		t.Logf("observed: the queued command drained and answered %s", drained.body)
	case <-time.After(settleBudget):
		t.Fatalf("the queued command never drained after the printer came back")
	}
}

// TestACapabilityTheModelDoesNotDeclareIsRefused is an earlier design decision at the API
// boundary: the check is above the driver, so a verb this model does not have
// never becomes a frame.
func TestACapabilityTheModelDoesNotDeclareIsRefused(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MINI")

	for _, verb := range listed.Capabilities {
		if verb == chamberTemperature {
			t.Fatalf("a MINI was admitted with %s, so this scenario is asking for a verb it has", verb)
		}
	}

	answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"args":{"chamber.target_temp":"40"}}`, chamberTemperature), nil)

	if answer.status < http.StatusBadRequest || answer.status >= http.StatusInternalServerError {
		t.Fatalf("a MINI was asked for a chamber temperature and answered %d: %s", answer.status, answer.body)
	}

	problem := answer.decode(t)
	if code, _ := problem["code"].(string); !strings.Contains(code, "CAPABILITY_NOT_SUPPORTED") {
		t.Errorf("the refusal carries code %v, and it is not a capability refusal", problem["code"])
	}

	t.Logf("observed: a MINI refused %s with %d %s", chamberTemperature, answer.status, answer.body)

	// The session still works, which is what says the refusal cost nothing.
	after := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q}`, sendInfo), nil)
	if after.status != http.StatusOK {
		t.Fatalf("the printer is unusable after a refusal: %d %s", after.status, after.body)
	}
}

// TestARefusedVerbNamesTheVerbsThePrinterAccepts is the operator's next step. A
// caller who sent a verb this printer does not have is told which verbs it does
// have, so they do not go looking for a second endpoint to find out.
//
// The verb is one this API defines and this model withholds, so the refusal is
// the printer's answer about a verb that exists. A verb the API does not define
// at all is decided before any printer is resolved and is the case below.
func TestARefusedVerbNamesTheVerbsThePrinterAccepts(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MK4")

	if slices.Contains(listed.Capabilities, chamberTemperature) {
		t.Fatalf("an MK4 was admitted with %s, so this scenario is asking for a verb it has",
			chamberTemperature)
	}

	answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"args":{"chamber.target_temp":"40"}}`, chamberTemperature), nil)

	t.Logf("observed: a verb this model withholds answered %d %s %s",
		answer.status, answer.header.Get("Content-Type"), answer.body)

	if answer.status != http.StatusConflict {
		t.Fatalf("a verb the printer does not have answered %d, want 409: %s", answer.status, answer.body)
	}

	problem := answer.decode(t)

	params, ok := problem["params"].(map[string]any)
	if !ok {
		t.Fatalf("the refusal carries no params: %s", answer.body)
	}

	offered, _ := params["verbs"].(string)

	var verbs []string
	if err := json.Unmarshal([]byte(offered), &verbs); err != nil {
		t.Fatalf("the verbs the refusal offers are not a JSON array: %q: %v", offered, err)
	}

	if !slices.Contains(verbs, nozzleVerb) {
		t.Errorf("the refusal offers %d verbs and %s is not among them, so the caller is told "+
			"their word is wrong and never which word is right", len(verbs), nozzleVerb)
	}

	// The count is the whole set and the list is a window on it, so a count below
	// the list would leave a caller unable to tell a whole list from a clipped one.
	count, _ := params["verb_count"].(float64)
	if int(count) < len(verbs) {
		t.Errorf("the refusal reports %v verbs in total and offers %d of them, so the count "+
			"says nothing about whether the list is whole", params["verb_count"], len(verbs))
	}

	// The offered list is this printer's, not a fixed one. Every verb in it has
	// to be a verb the printer was admitted with.
	for _, verb := range verbs {
		if !slices.Contains(listed.Capabilities, verb) {
			t.Errorf("the refusal offers %s, which /api/v1/devices/%s does not list",
				verb, listed.ID)
		}
	}

	t.Logf("observed: the refusal offers %d of this printer's %v verbs, including %s",
		len(verbs), params["verb_count"], nozzleVerb)
}

// TestTheShippingServersSpellingIsAnsweredWithThisAPIsVerb is the first request
// an integrator moving off the shipping server sends. That server names the
// command SET_NOZZLE_TEMPERATURE and this one gives it the name nozzleVerb
// holds, so the refusal has to close that gap. Otherwise the integrator reads a
// vocabulary of 59 verbs to find the one they already asked for.
//
// The refusal is decided before any printer is resolved, so it must say nothing
// about the printer addressed. The same request against an id no printer answers
// to has to produce the same answer, which is what the second half asserts.
func TestTheShippingServersSpellingIsAnsweredWithThisAPIsVerb(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MK4")

	if !slices.Contains(listed.Capabilities, nozzleVerb) {
		t.Fatalf("an MK4 was admitted without %s, so the suggested verb is one it cannot run", nozzleVerb)
	}

	const guessed = `{"verb":"SET_NOZZLE_TEMPERATURE"}`

	answer := server.do(t, http.MethodPost,
		"/api/v1/devices/"+listed.ID+"/commands", guessed, nil)

	t.Logf("observed: the shipping server's spelling answered %d %s %s",
		answer.status, answer.header.Get("Content-Type"), answer.body)

	if answer.status != http.StatusBadRequest {
		t.Fatalf("a verb this API does not define answered %d, want 400: %s", answer.status, answer.body)
	}

	problem := answer.decode(t)
	if code, _ := problem["code"].(string); !strings.Contains(code, "VERB_UNKNOWN") {
		t.Errorf("the refusal carries code %v, and it is not a refusal about the verb", problem["code"])
	}

	params, ok := problem["params"].(map[string]any)
	if !ok {
		t.Fatalf("the refusal carries no params: %s", answer.body)
	}

	if got, _ := params["verb"].(string); got != "SET_NOZZLE_TEMPERATURE" {
		t.Errorf("the refusal reports verb %q, so it does not echo what the caller sent", got)
	}

	if got, _ := params["suggested_verb"].(string); got != nozzleVerb {
		t.Errorf("the refusal suggests %q, want %s, so a caller who guessed the shipping server's "+
			"spelling is told their word is wrong and never which word is right", got, nozzleVerb)
	}

	// Nothing about the printer that was addressed. A parameter naming it would
	// make this refusal a way to ask whether an id belongs to a printer.
	if got, carried := params["device_id"]; carried {
		t.Errorf("the refusal carries device_id %v, so a caller learns the id reached a device", got)
	}

	onNothing := server.do(t, http.MethodPost,
		"/api/v1/devices/00000000-0000-4000-8000-00000dead000/commands", guessed, nil)

	if onNothing.status != answer.status {
		t.Fatalf("an id no printer answers to answered %d and a real one answered %d, so the status "+
			"says whether the id reached a printer: %s", onNothing.status, answer.status, onNothing.body)
	}

	absent, _ := onNothing.decode(t)["params"].(map[string]any)
	if !reflect.DeepEqual(params, absent) {
		t.Errorf("the two refusals carry %v and %v, so the difference says whether the id reached "+
			"a printer", params, absent)
	}

	// The verb the refusal named is one the caller can actually send.
	corrected := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"args":{"nozzle_temperature":"210"}}`, nozzleVerb), nil)
	if corrected.status != http.StatusOK {
		t.Fatalf("the suggested verb answered %d, so the refusal sent the caller somewhere that does "+
			"not work: %s", corrected.status, corrected.body)
	}

	t.Logf("observed: the refusal suggested %s and that verb answered %d",
		nozzleVerb, corrected.status)
}

// benchModel is the machine on the bench, so the walkthrough resolves the same
// capability profile a real Core One resolves. A verb that model withholds is
// refused here rather than at the printer.
const benchModel = "CoreOne"

// The two verbs the walkthrough spends. They are spelled out rather than
// imported for the reason printerSampled is: a person types these into curl and
// a constant shared with the producer would hide a rename from them.
const (
	startPrintVerb = "core.command.start_print"
	stopPrintVerb  = "core.command.stop_print"
)

// benchStorage is the mountpoint a simulated printer reports, without the
// leading slash the firmware spells it with. The API takes the storage-prefixed
// form, so this is the prefix every path in the walkthrough carries.
const benchStorage = "usb"

// TestTheBenchWalkthroughReadsListsPrintsAndCancels runs the four steps a
// person standing at a printer takes, in order, against the shipped binary:
// read the status, list the files, start a print, cancel it.
//
// It is one case and not four because the order is the thing under test. A
// print cannot be cancelled that was never started, and a path cannot be
// printed that no listing offered, so each step takes its input from the answer
// before it rather than from a value written here.
func TestTheBenchWalkthroughReadsListsPrintsAndCancels(t *testing.T) {
	t.Parallel()

	server := startServer(t)
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, benchModel)

	// Step 1, read the status. A printer is on the fleet from the moment it is
	// admitted, and its state tree is empty until its first telemetry frame, so
	// the state is waited for rather than read once. A person at the bench sees
	// the same empty tree, and asks for it with core.command.send_state_info.
	status := server.printer(t, listed.ID)
	if !status.Connected {
		t.Fatalf("the printer is on the fleet and not connected: %+v", status)
	}

	var state string

	arrived := time.Now()

	eventually(t, "the printer reported a state", func() bool {
		status = server.printer(t, listed.ID)
		state = stateOf(t, status)

		return state != ""
	})

	t.Logf("observed step 1: GET /api/v1/devices/%s answered model=%s firmware=%s "+
		"connected=%t, and /state=%s arrived %s after the first read",
		listed.ID, status.Model, status.Firmware, status.Connected, state,
		time.Since(arrived).Round(time.Millisecond))

	// Step 2, list the files. The storages come first because an empty path
	// lists the devices and every other path hangs off one of them.
	storages := server.get(t, "/api/v1/devices/"+listed.ID+"/storages")
	if storages.status != http.StatusOK {
		t.Fatalf("the storages answered %d: %s", storages.status, storages.body)
	}

	t.Logf("observed step 2a: GET /api/v1/devices/%s/storages answered %s", listed.ID, storages.body)

	printable := firstPrintable(t, server, listed.ID, benchStorage, 0)

	// Step 3, start a print, on the path the listing offered.
	started := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"args":{"path":%q}}`, startPrintVerb, printable), nil)
	if started.status != http.StatusOK {
		t.Fatalf("starting a print answered %d: %s", started.status, started.body)
	}

	if started.decode(t)["ok"] != true {
		t.Fatalf("the printer refused to start %s: %s", printable, started.body)
	}

	t.Logf("observed step 3: POST a %s for %s answered %s", startPrintVerb, printable, started.body)

	eventually(t, "the printer reported that it is printing", func() bool {
		return stateOf(t, server.printer(t, listed.ID)) == "PRINTING"
	})

	t.Logf("observed step 3: /api/v1/devices/%s now carries /state=PRINTING", listed.ID)

	// Step 4, cancel it. The state leaving PRINTING is what says the cancel
	// reached the machine, because a 200 alone is the queue accepting it.
	stopped := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q}`, stopPrintVerb), nil)
	if stopped.status != http.StatusOK {
		t.Fatalf("cancelling the print answered %d: %s", stopped.status, stopped.body)
	}

	if stopped.decode(t)["ok"] != true {
		t.Fatalf("the printer refused to cancel: %s", stopped.body)
	}

	var settled string

	eventually(t, "the printer reported that it stopped printing", func() bool {
		settled = stateOf(t, server.printer(t, listed.ID))

		return settled != "" && settled != "PRINTING"
	})

	t.Logf("observed step 4: POST a %s left /state at %s, and answered %s",
		stopPrintVerb, settled, stopped.body)
}

// stateOf reads the printer's own state word off the state tree. It returns the
// empty string for a printer that has reported nothing, which a caller has to
// separate from a printer that reported a state this walkthrough did not expect.
func stateOf(t *testing.T, view printerView) string {
	t.Helper()

	raw, carried := view.State["/state"]
	if !carried {
		return ""
	}

	var state string
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("the printer's /state is not a string: %s: %v", raw, err)
	}

	return state
}

// firstPrintable walks the printer's own tree from one storage and returns the
// first file that is not a folder, in the storage-prefixed spelling the command
// endpoint takes.
//
// It walks rather than naming a path because the walkthrough has to work
// against whatever is on the stick in the machine. A tree with no file in it
// fails here, which is the answer a person at the bench needs: the print step
// after it would otherwise report a refusal that says nothing about why.
func firstPrintable(t *testing.T, s *server, printer, directory string, depth int) string {
	t.Helper()

	// A printer answers this walk, and a printer is a machine whose firmware
	// this test does not control. A tree that names itself as its own child
	// would otherwise walk until the whole run times out.
	const deepest = 8

	if depth > deepest {
		t.Fatalf("the walk reached %s at depth %d without finding a file", directory, depth)
	}

	listing := s.deviceListing(t, printer, directory)

	t.Logf("observed step 2b: GET /api/v1/devices/%s/files?path=%s answered %s",
		printer, directory, listing.body)

	// Items is a pointer so that a listing this test cannot read is separated
	// from a directory with nothing in it. Both decode to no entries, and only
	// one of them is the printer's answer: a renamed field would otherwise be
	// reported here as an empty stick, which sends a person to the machine to
	// look for a fault that is in this file.
	var body struct {
		Items *[]struct {
			Path   string `json:"path"`
			Folder bool   `json:"folder"`
		} `json:"items"`
	}

	if err := json.Unmarshal(listing.body, &body); err != nil {
		t.Fatalf("parse the listing of %s: %v", directory, err)
	}

	if body.Items == nil {
		t.Fatalf("the listing of %s carries no items field, so this walk reads the wrong shape: %s",
			directory, listing.body)
	}

	for _, entry := range *body.Items {
		if !entry.Folder {
			return entry.Path
		}
	}

	for _, entry := range *body.Items {
		if entry.Folder {
			return firstPrintable(t, s, printer, entry.Path, depth+1)
		}
	}

	t.Fatalf("%s holds no file to print: %s", directory, listing.body)

	return ""
}
