package httpapi

import (
	"encoding/json"
	"time"

	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The examples that are not JSON bodies: the WebRTC negotiation the camera
// operations carry as text, the Prometheus exposition, and the event stream as
// a reader's client actually receives it.
//
// They sit apart from the operation table because each is a whole wire format
// quoted as text, and not a Go value the encoder writes.

// exampleOffer and exampleAnswer are one negotiation, trimmed to the lines a
// reader has to send and read. The fingerprint and the candidate are shortened,
// because a real SDP has about forty lines of values that differ every time.
const (
	exampleOffer = `v=0
o=- 4611731400430051336 2 IN IP4 127.0.0.1
s=-
t=0 0
m=video 9 UDP/TLS/RTP/SAVPF 102
c=IN IP4 0.0.0.0
a=recvonly
a=mid:0
a=ice-ufrag:Jd7B
a=ice-pwd:eaC1s0pcxSuNraNBAB9Vzq7L
a=fingerprint:sha-256 6B:8B:45:...:5C:2F
a=setup:actpass
a=rtpmap:102 H264/90000
a=fmtp:102 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f
`

	exampleAnswer = `v=0
o=- 8291374650192837465 2 IN IP4 127.0.0.1
s=-
t=0 0
m=video 51423 UDP/TLS/RTP/SAVPF 102
c=IN IP4 192.168.1.20
a=sendonly
a=mid:0
a=ice-ufrag:qNvR
a=ice-pwd:9BcT2mLp0dXfKwYs4RzAeH
a=fingerprint:sha-256 A1:04:9E:...:D3:77
a=setup:active
a=rtpmap:102 H264/90000
a=fmtp:102 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f
a=candidate:1 1 udp 2130706431 192.168.1.20 51423 typ host
a=end-of-candidates
`
)

// exampleExposition is what a scrape reads: three of the families the metric
// catalogue declares, with the help text the catalogue gives each one.
//
// Every family shown here is one this server writes.
const exampleExposition = `# HELP pcl_build_info Always 1. The build this process is running, in its labels.
# TYPE pcl_build_info gauge
pcl_build_info{version="0.1.0",commit="1da9adb",go_version="go1.26.0"} 1
# HELP pcl_printer_up 1 while a printer is connected, 0 otherwise.
# TYPE pcl_printer_up gauge
pcl_printer_up{device_id="9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"} 1
# HELP pcl_printer_connections Printers in each connection state, by driver.
# TYPE pcl_printer_connections gauge
pcl_printer_connections{driver="prusa-xbuddy",state="connected"} 397
pcl_printer_connections{driver="prusa-xbuddy",state="disconnected"} 3
`

// exampleEventStream is what a reader sees on the stream, framed by the same
// functions the hub and this transport frame a real one with, so the field
// order, the blank lines and the placement of the id are the server's own.
//
// Three frames: the reconnection delay, one telemetry sample with no id because
// telemetry holds no place to resume from, and one durable state event that
// does.
func exampleEventStream() string {
	out := stream.AppendSSERetry(nil, defaultRetry)

	origin, err := event.NewOrigin(event.KindDevice, examplePrinterID)
	if err != nil {
		return string(out)
	}

	telemetry := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3Q7X8N4B2VJ5R0YH6M9CDEF",
		Source:      event.SourceURN("workshop", origin),
		Type:        "com.prusa.connect.device.telemetry.sampled",
		Time:        exampleTime,
		Subject:     origin.String(),
		Class:       event.ClassTelemetry,
		Data:        json.RawMessage(`{"path":"/tools/0/nozzle_temp","value":214.8}`),
	}

	state := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3Q7XA26D0WM4T1ZJ8P5GHIJ",
		Source:      event.SourceURN("workshop", origin),
		Type:        "com.prusa.connect.device.presence.changed",
		Time:        exampleTime,
		Subject:     origin.String(),
		Sequence:    "00000000000000000412",
		Epoch:       "01K3Q7W0",
		Class:       event.ClassState,
		Data:        json.RawMessage(`{"device_id":"` + examplePrinterID + `","driver":"xbuddy","connected":false}`),
	}

	// The print that ended. Its payload is built from the type internal/app
	// publishes, so a client written to this example is a client written to
	// what the hub sends.
	settled, err := json.Marshal(event.JobSettled{
		JobID:             "0199e3a4-9c0e-7d42-8d25-6a1f0c3b7e94",
		DeviceJobID:       "412",
		Path:              "/usb/bracket.bgcode",
		DisplayName:       "bracket.bgcode",
		Started:           exampleTime.Add(-exampleJobLength),
		Ended:             exampleTime,
		Outcome:           event.JobFinished,
		MachineState:      "FINISHED",
		Progress:          100,
		TimePrintingS:     int64(exampleJobLength.Seconds()),
		TimePrintingKnown: true,
		FilamentUsedMM:    4128.6,
		FilamentUsedKnown: true,
	})
	if err != nil {
		return string(out)
	}

	job := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3Q7XB37E1XN5U2AK9Q6HIJK",
		Source:      event.SourceURN("workshop", origin),
		Type:        event.TypeDeviceJobSettled,
		Time:        exampleTime,
		Subject:     origin.String(),
		Sequence:    "00000000000000000413",
		Epoch:       "01K3Q7W0",
		Class:       event.ClassState,
		CausationID: "0199e3a4-9c0e-7d42-8d25-6a1f0c3b7e94",
		Data:        settled,
	}

	position := event.Position{Origin: origin, Offset: 412}
	after := event.Position{Origin: origin, Offset: 413}

	out = append(out, frameOf(telemetry, "")...)
	out = append(out, frameOf(state, position.ID(state.Epoch))...)
	out = append(out, frameOf(job, after.ID(job.Epoch))...)

	return string(out)
}

// exampleJobLength is how long the print in the example ran. It is a plausible
// length for the file it names and nothing depends on the number.
const exampleJobLength = 3 * time.Hour

// frameOf renders one envelope the way stream.Message.derive renders it: the
// same builder, so an example frame cannot differ from a real one in its
// framing.
func frameOf(envelope event.Envelope, id string) []byte {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil
	}

	return stream.SSEControlFrame(id, envelope.Type, encoded)
}
