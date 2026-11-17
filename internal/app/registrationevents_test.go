package app

// The registration exchange as a page reads it, over the real composition root:
// the shipping binary's routes, its connector, its record store and its event
// stream, with a simulated printer at the door.
//
// It is written as a subscriber because that is what the add-a-device page is.
// What it can act on is the frame, so the frame is what is asserted.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// payloads is the data member of every frame of one type, in arrival order.
//
// A frame's data line is the whole envelope, and the payload is the member
// inside it, so what a case asserts on is one unwrapping away from what the
// reader collected.
func (s *eventLog) payloads(t *testing.T, kind string, into any) bool {
	t.Helper()

	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	for _, frame := range s.frames {
		if frame.kind != kind {
			continue
		}

		var envelope struct {
			Data json.RawMessage `json:"data"`
		}

		require.NoError(t, json.Unmarshal([]byte(frame.data), &envelope),
			"a registration frame is not an envelope")
		require.NoError(t, json.Unmarshal(envelope.Data, into))

		return true
	}

	return false
}

// raw is the whole data line of the first frame of one type, empty when none
// has arrived.
func (s *eventLog) raw(t *testing.T, kind string) string {
	t.Helper()

	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	for _, frame := range s.frames {
		if frame.kind == kind {
			return frame.data
		}
	}

	return ""
}

// countOf is how many frames of one type have arrived.
func (s *eventLog) countOf(kind string) int { return s.count(kind) }

// TestTheAdmissionOriginsAreOfferedToWhoeverMayDecide is the rendering on its
// own: a connector is not a declared resource, so the visibility answer cannot
// find one by walking the resource table, and this is what puts it there.
func TestTheAdmissionOriginsAreOfferedToWhoeverMayDecide(t *testing.T) {
	t.Parallel()

	held := &admissionOrigins{}
	held.bind([]string{"xbuddy"})

	seen := visibility{graph: staticGraph{admits: true}, admission: held}

	rendered, err := seen.VisibleOrigins(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Equal(t, []string{"driver:xbuddy"}, originNames(rendered.Origins))

	// And nobody else is told. The question is the one the registration surface
	// asks before it lists or decides a code.
	blind := visibility{graph: staticGraph{admits: false}, admission: held}

	rendered, err = blind.VisibleOrigins(t.Context(), "user:operator")
	require.NoError(t, err)
	require.Empty(t, rendered.Origins)
}

// TestAWaitingCodeReachesASubscriberWithoutBeingAskedFor is what a page adding
// a printer waits on. The code arrives on the stream on its own, so nothing has
// to ask /api/v1/registrations on a timer.
func TestAWaitingCodeReachesASubscriberWithoutBeingAskedFor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfig(freeAddr(t)))

	// Opened before the printer registers, which is what makes the announcement
	// a frame this reader was given rather than one it went looking for.
	frames := watch(t, running)

	stop := runPrinter(t, running, "SIM-ANNOUNCED")
	t.Cleanup(stop)

	code := waitForPendingCode(t, running)

	var announced event.RegistrationAnnounced

	require.Eventually(t, func() bool {
		return frames.payloads(t, event.TypeDeviceRegistrationAnnounced, &announced) &&
			announced.Code == code
	}, settle, 25*time.Millisecond,
		"a printer is waiting at the door and nothing on the stream said so")

	t.Logf("observed: the announcement carried %s", announced.Code)

	require.Equal(t, "xbuddy", announced.Connector)
	require.NotEmpty(t, announced.Fingerprint, "the frame names no machine")
	require.Equal(t, "SIM-ANNOUNCED", announced.Serial)

	// The machine's own spelling, passed through. The xBuddy handshake carries
	// the device type code where a model name is expected, which is exactly why
	// the model vocabulary publishes that code beside the name.
	require.NotEmpty(t, announced.Model, "the frame says nothing about what the machine is")

	// One frame per code and not one per handshake. The printer polls until
	// somebody decides, and each of those writes the same row.
	require.Equal(t, 1, frames.countOf(event.TypeDeviceRegistrationAnnounced),
		"a printer retrying its handshake announced itself twice")
}

// TestAnApprovalAnswersTheDeviceIDAndSaysSoOnTheStream is what removes the
// matching. A page that approved a code had to wait for an arrival and compare
// serial numbers; the id is on the answer and on the frame.
func TestAnApprovalAnswersTheDeviceIDAndSaysSoOnTheStream(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfig(freeAddr(t)))

	frames := watch(t, running)

	stop := runPrinter(t, running, "SIM-APPROVED")
	t.Cleanup(stop)

	code := waitForPendingCode(t, running)

	answered := running.call(t, http.MethodPost, "/api/v1/registrations/"+code+"/approve", "")
	require.Equal(t, http.StatusOK, answered.status, answered.body)

	var row httpapi.Registration

	require.NoError(t, json.Unmarshal([]byte(answered.body), &row))
	require.True(t, row.Approved)
	require.NotEmpty(t, row.DeviceID, "the approval named no device, so a page cannot open one")

	t.Logf("observed: the approval answered %s", row.DeviceID)

	var approved event.RegistrationApproved

	require.Eventually(t, func() bool {
		return frames.payloads(t, event.TypeDeviceRegistrationApproved, &approved) &&
			approved.Code == code
	}, settle, 25*time.Millisecond, "a code was approved and nothing on the stream said so")

	require.Equal(t, row.DeviceID, approved.DeviceID,
		"the answer and the frame name two different devices")
	require.Equal(t, "xbuddy", approved.Connector)

	// The device the id names is the device the printer arrives on, which is
	// what makes the frame worth acting on.
	require.Eventually(t, func() bool {
		read := running.get(t, "/api/v1/devices/"+approved.DeviceID)

		return read.status == http.StatusOK
	}, settle, 25*time.Millisecond, "the id the approval answered never became a device")
}

// TestARejectedCodeIsAFrameAndNotOnlyALogLine is the third decision. A wizard
// that turned a machine away can say so rather than waiting for something that
// is never going to happen.
func TestARejectedCodeIsAFrameAndNotOnlyALogLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfig(freeAddr(t)))

	frames := watch(t, running)

	stop := runPrinter(t, running, "SIM-REJECTED")
	t.Cleanup(stop)

	code := waitForPendingCode(t, running)

	rejected := running.call(t, http.MethodDelete, "/api/v1/registrations/"+code, "")
	require.Equal(t, http.StatusNoContent, rejected.status, rejected.body)

	var turned event.RegistrationRejected

	require.Eventually(t, func() bool {
		return frames.payloads(t, event.TypeDeviceRegistrationRejected, &turned) &&
			turned.Code == code
	}, settle, 25*time.Millisecond, "a code was rejected and nothing on the stream said so")

	require.Equal(t, "xbuddy", turned.Connector)
	require.NotEmpty(t, turned.Fingerprint, "the frame names no machine")
}

// TestARefusedPrinterIsAFrameWithoutItsIdentity is the fourth frame. A printer
// carrying a token this server never issued is refused on /p/ws, and a page
// waiting for it is told why, once, without the printer's identity in clear.
func TestARefusedPrinterIsAFrameWithoutItsIdentity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, decidedAdmissionConfig(freeAddr(t)))

	frames := watch(t, running)

	const fingerprint = "FP-REFUSED-0001"

	knock := http.Header{
		"Fingerprint":        {fingerprint},
		"Token":              {"made-up"},
		"User-Agent-Printer": {"MK4"},
		"User-Agent-Version": {"6.4.0+1234"},
	}

	// A printer knocks every few seconds. Three knocks are one frame.
	for range 3 {
		refused := running.raw(t, "/p/ws", knock)
		require.Equal(t, http.StatusUnauthorized, refused.status, string(refused.body))
	}

	var turned event.RegistrationRefused

	require.Eventually(t, func() bool {
		return frames.payloads(t, event.TypeDeviceRegistrationRefused, &turned)
	}, settle, 25*time.Millisecond, "a printer was refused and nothing on the stream said so")

	require.Equal(t, event.RegistrationRefused{
		Connector:         "xbuddy",
		Reason:            event.RefusedUnknownCredentials,
		Admission:         event.AdmissionApproval,
		FingerprintDigest: "41298c5f02b92bbf1a7786536bc8d15164a05c79352370a8b90aa70e9220b8f9",
		Model:             "MK4",
		Firmware:          "6.4.0+1234",
	}, turned)

	raw := frames.raw(t, event.TypeDeviceRegistrationRefused)
	require.NotContains(t, raw, fingerprint, "the frame carries the printer's identity in clear")
	require.Equal(t, 1, frames.countOf(event.TypeDeviceRegistrationRefused),
		"three knocks inside a minute were published as more than one frame")
}

// TestAnApprovalOnArrivalIsAFrameWithItsDevice is the connector that approves
// every printer itself. Nobody calls the approval surface, so the connector's
// own approval has to reach the stream, or a page waiting for the printer
// never learns its device id.
func TestAnApprovalOnArrivalIsAFrameWithItsDevice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	open := strings.NewReplacer("mode: token", "mode: none", "auto_register: false", "auto_register: true").
		Replace(decidedAdmissionConfig(freeAddr(t)))
	running := start(t, dir, open)

	frames := watch(t, running)

	stop := runPrinter(t, running, "SIM-ON-ARRIVAL")
	t.Cleanup(stop)

	var approved event.RegistrationApproved

	require.Eventually(t, func() bool {
		return frames.payloads(t, event.TypeDeviceRegistrationApproved, &approved)
	}, settle, 25*time.Millisecond, "a connector approved a printer and nothing on the stream said so")

	require.Equal(t, "xbuddy", approved.Connector)
	require.NotEmpty(t, approved.DeviceID, "the frame names no device, so a page cannot follow the printer")
	require.Equal(t, "SIM-ON-ARRIVAL", approved.Serial)
	require.Zero(t, frames.countOf(event.TypeDeviceRegistrationAnnounced),
		"a code approved on arrival was announced as waiting for a person")

	require.Eventually(t, func() bool {
		return running.get(t, "/api/v1/devices/"+approved.DeviceID).status == http.StatusOK
	}, settle, 25*time.Millisecond, "the id the frame named never became a device")
}
