// Package camproto encodes and decodes the few messages of the camera's
// protobuf schema (prusa_cam.proto, schema 4.4 to 4.6) this module uses.
//
// The messages are written by hand over protowire, field number by field
// number, rather than generated: the module needs five of the schema's forty
// messages, and generated code would pull the reflection runtime into the
// binary for them.
//
//nolint:mnd,gosec,exhaustive // field numbers and widths are the schema's, and a protowire.Number is no enum.
package camproto

import (
	"encoding/json"
	"errors"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// enable is BooleanState.ENABLE.
const enable = 1

// ErrMalformed reports bytes that are not the message they claim to be.
var ErrMalformed = errors.New("camproto: the message is malformed")

// Authentication is CameraAuthentication, the first event a camera sends.
type Authentication struct {
	Token       string
	Fingerprint string
}

// DecodeAuthentication reads CameraAuthentication: camera_token = 1,
// fingerprint = 2.
func DecodeAuthentication(b []byte) (Authentication, error) {
	var out Authentication

	err := walk(b, func(num protowire.Number, typ protowire.Type, value []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}

		switch num {
		case 1:
			out.Token = string(value)
		case 2:
			out.Fingerprint = string(value)
		}
	})

	return out, err
}

// webrtcModeOn is WebrtcMode.WEBRTC_MODE_ON.
const webrtcModeOn = 1

// The RtspServerMode and WebrtcMode values this module sends.
const (
	rtspOn     = 2
	rtspOff    = 3
	webrtcOff  = 2
	StreamRTSP = "rtsp"
	StreamOff  = "off"
)

// Configuration is ServerToCamera with the settings this module keeps:
// control (3) { snapshot_interval (5), rtsp_server_mode (11), webrtc_mode (12) },
// and request_id (7). stream is StreamRTSP, StreamOff, or anything else for
// WebRTC, which the live view needs. The camera runs one stream at a time.
func Configuration(snapshotInterval uint32, stream, requestID string) []byte {
	rtsp, webrtc := uint64(rtspOff), uint64(webrtcModeOn)

	switch stream {
	case StreamRTSP:
		rtsp, webrtc = rtspOn, webrtcOff
	case StreamOff:
		webrtc = webrtcOff
	}

	var control []byte
	control = protowire.AppendTag(control, 5, protowire.VarintType)
	control = protowire.AppendVarint(control, uint64(snapshotInterval))
	control = protowire.AppendTag(control, 11, protowire.VarintType)
	control = protowire.AppendVarint(control, rtsp)
	control = protowire.AppendTag(control, 12, protowire.VarintType)
	control = protowire.AppendVarint(control, webrtc)

	return serverToCamera(control, requestID)
}

// Qualities is VideoQuality by the name a route takes and the status reports.
var Qualities = map[string]uint64{"sd": 1, "hd": 2, "fhd": 3}

// Change is one setting the camera keeps itself. A zero field is not sent.
type Change struct {
	Mode   string
	Volume uint32
	// TimelapseInterval is in seconds.
	TimelapseInterval uint32
	VideoQuality      string
	Name              string
	// JobName names the folder a timelapse goes to on the SD card.
	JobName string
}

// Message is ServerToCamera with the change:
//
//	timelapse_interval (2), control (3) { camera_mode (4), volume (7),
//	printing_job_name (10) }, system (4) { camera_name (1) },
//	request_id (7), video (8) { quality (1) }
func (c Change) Message(requestID string) []byte {
	var control []byte
	if c.Mode != "" {
		control = protowire.AppendTag(control, 4, protowire.VarintType)
		control = protowire.AppendVarint(control, Modes[c.Mode])
	}

	if c.Volume != 0 {
		control = protowire.AppendTag(control, 7, protowire.VarintType)
		control = protowire.AppendVarint(control, uint64(c.Volume))
	}

	control = appendString(control, 10, c.JobName)

	var out []byte
	if c.TimelapseInterval != 0 {
		out = protowire.AppendTag(out, 2, protowire.VarintType)
		out = protowire.AppendVarint(out, uint64(c.TimelapseInterval))
	}

	if control != nil {
		out = protowire.AppendTag(out, 3, protowire.BytesType)
		out = protowire.AppendBytes(out, control)
	}

	if c.Name != "" {
		out = protowire.AppendTag(out, 4, protowire.BytesType)
		out = protowire.AppendBytes(out, appendString(nil, 1, c.Name))
	}

	out = appendString(out, 7, requestID)

	if c.VideoQuality != "" {
		var video []byte
		video = protowire.AppendTag(video, 1, protowire.VarintType)
		video = protowire.AppendVarint(video, Qualities[c.VideoQuality])
		out = protowire.AppendTag(out, 8, protowire.BytesType)
		out = protowire.AppendBytes(out, video)
	}

	return out
}

// Directions is RotationDirection by the name a route takes.
var Directions = map[string]uint64{"left": 1, "right": 2, "up": 3, "down": 4}

// Modes is CameraMode, the infrared mode, by the name a route takes and the
// status reports.
var Modes = map[string]uint64{"auto": 1, "day": 2, "night": 3}

// Rotation turns the camera by angle degrees in one of Directions:
// ServerToCamera { control (3) { rotation (1) { direction (1), angle (2) } } }.
// The camera reports no position, so a turn is always relative.
func Rotation(direction string, angle uint32, requestID string) []byte {
	var rotation []byte
	rotation = protowire.AppendTag(rotation, 1, protowire.VarintType)
	rotation = protowire.AppendVarint(rotation, Directions[direction])
	rotation = protowire.AppendTag(rotation, 2, protowire.VarintType)
	rotation = protowire.AppendVarint(rotation, uint64(angle))

	var control []byte
	control = protowire.AppendTag(control, 1, protowire.BytesType)
	control = protowire.AppendBytes(control, rotation)

	return serverToCamera(control, requestID)
}

// serverToCamera is ServerToCamera with control (3) and request_id (7).
func serverToCamera(control []byte, requestID string) []byte {
	var out []byte
	out = protowire.AppendTag(out, 3, protowire.BytesType)
	out = protowire.AppendBytes(out, control)

	return appendString(out, 7, requestID)
}

// The CameraTrigger fields this module sets.
const (
	TriggerStatus    protowire.Number = 1
	TriggerFeatures  protowire.Number = 2
	TriggerSnapshot  protowire.Number = 3
	TriggerTimelapse protowire.Number = 5
	TriggerReboot    protowire.Number = 9
)

// Trigger is CameraTrigger with one field set to ENABLE, and request_id (13).
func Trigger(field protowire.Number, requestID string) []byte {
	return Toggle(field, true, requestID)
}

// Toggle is CameraTrigger with one field set to ENABLE or DISABLE.
func Toggle(field protowire.Number, on bool, requestID string) []byte {
	state := uint64(booleanDisable)
	if on {
		state = enable
	}

	var out []byte
	out = protowire.AppendTag(out, field, protowire.VarintType)
	out = protowire.AppendVarint(out, state)
	out = appendString(out, 13, requestID)

	return out
}

// DecodeFeatures reads CameraFeatures: features (6), a JSON array of names
// such as RotationZ. The camera sends it on the features event.
func DecodeFeatures(b []byte) ([]string, error) {
	var raw []byte

	err := walk(b, func(num protowire.Number, typ protowire.Type, value []byte, _ uint64) {
		if num == 6 && typ == protowire.BytesType {
			raw = value
		}
	})
	if err != nil {
		return nil, err
	}

	features := []string{}
	if json.Unmarshal(raw, &features) != nil {
		return nil, ErrMalformed
	}

	return features, nil
}

// Status is the part of CameraToServer this module shows a person.
type Status struct {
	SnapshotInterval uint32 `json:"snapshot_interval,omitempty"`
	// Mode is the infrared mode, a key of Modes.
	Mode   string `json:"mode,omitempty"`
	Volume uint32 `json:"volume,omitempty"`
	// VideoQuality is a key of Qualities.
	VideoQuality      string  `json:"video_quality,omitempty"`
	Timelapse         bool    `json:"timelapse,omitempty"`
	TimelapseInterval uint32  `json:"timelapse_interval,omitempty"`
	Name              string  `json:"name,omitempty"`
	SSID              string  `json:"ssid,omitempty"`
	Address           string  `json:"address,omitempty"`
	IPv6              string  `json:"ipv6,omitempty"`
	MAC               string  `json:"mac,omitempty"`
	SignalQuality     uint32  `json:"signal_quality,omitempty"`
	Firmware          string  `json:"firmware,omitempty"`
	Hardware          string  `json:"hardware,omitempty"`
	RTSP              string  `json:"rtsp,omitempty"`
	SDCard            SDCard  `json:"sd_card,omitzero"`
	Temperature       float32 `json:"temperature,omitempty"`
	Uptime            string  `json:"uptime,omitempty"`
	RequestID         string  `json:"-"`
}

// SDCard is the camera's SD card. The sizes are in MB.
type SDCard struct {
	Detected bool   `json:"detected"`
	Size     uint32 `json:"size,omitempty"`
	Free     uint32 `json:"free,omitempty"`
	Used     uint32 `json:"used,omitempty"`
	Status   string `json:"status,omitempty"`
}

// DecodeStatus reads CameraToServer:
//
//	timelapse (2)     { enable (1), interval (2) }
//	control (3)       { camera_mode (3), snapshot_interval (4), volume (6) }
//	network (4)       { wifi (1) { ssid (1), mac (2), ipv4 (3), ipv6 (4), signal_quality (5) },
//	                    ethernet (2) { mac (1), ipv4 (2), ipv6 (3) } }
//	system (5)        { fw_version (1), hw_version (2), camera_name (3),
//	                    sd_card (4) { detected (1), size (2), free (3), used (4), status (5) },
//	                    rtsp_server (6) { url (3) } }
//	camera_status (9) { mcu_temperature (1, float), uptime (3) }
//	request_id (10), video (11) { quality (1) }
func DecodeStatus(b []byte) (Status, error) {
	var out Status

	err := walk(b, func(num protowire.Number, typ protowire.Type, value []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}

		switch num {
		case 2:
			decodeTimelapse(value, &out)
		case 3:
			decodeControl(value, &out)
		case 4:
			_ = walk(value, func(n protowire.Number, t protowire.Type, link []byte, _ uint64) {
				switch {
				case n == 1 && t == protowire.BytesType:
					decodeWiFi(link, &out)
				case n == 2 && t == protowire.BytesType:
					decodeEthernet(link, &out)
				}
			})
		case 5:
			decodeSystem(value, &out)
		case 9:
			decodeCameraStatus(value, &out)
		case 10:
			out.RequestID = string(value)
		case 11:
			_ = walk(value, func(n protowire.Number, t protowire.Type, _ []byte, v uint64) {
				if n == 1 && t == protowire.VarintType {
					out.VideoQuality = nameOf(Qualities, v)
				}
			})
		}
	})

	return out, err
}

// booleanDisable is DISABLE of the schema's BooleanState.
const booleanDisable = 2

func decodeTimelapse(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, _ []byte, v uint64) {
		switch {
		case n == 1 && t == protowire.VarintType:
			out.Timelapse = v == enable
		case n == 2 && t == protowire.VarintType:
			out.TimelapseInterval = uint32(v)
		}
	})
}

func decodeControl(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, _ []byte, v uint64) {
		if t != protowire.VarintType {
			return
		}

		switch n {
		case 3:
			out.Mode = nameOf(Modes, v)
		case 4:
			out.SnapshotInterval = uint32(v)
		case 6:
			out.Volume = uint32(v)
		}
	})
}

func decodeCameraStatus(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, s []byte, v uint64) {
		switch {
		case n == 1 && t == protowire.Fixed32Type:
			out.Temperature = math.Float32frombits(uint32(v))
		case n == 3 && t == protowire.BytesType:
			out.Uptime = string(s)
		}
	})
}

func nameOf(names map[string]uint64, v uint64) string {
	for name, value := range names {
		if value == v {
			return name
		}
	}

	return ""
}

func decodeWiFi(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, s []byte, v uint64) {
		switch {
		case n == 1 && t == protowire.BytesType:
			out.SSID = string(s)
		case n == 2 && t == protowire.BytesType:
			out.MAC = string(s)
		case n == 3 && t == protowire.BytesType:
			out.Address = string(s)
		case n == 4 && t == protowire.BytesType:
			out.IPv6 = string(s)
		case n == 5 && t == protowire.VarintType:
			out.SignalQuality = uint32(v)
		}
	})
}

// decodeEthernet fills what Wi-Fi left empty, for a camera on a cable.
func decodeEthernet(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, s []byte, _ uint64) {
		if t != protowire.BytesType || len(s) == 0 {
			return
		}

		switch {
		case n == 1 && out.MAC == "":
			out.MAC = string(s)
		case n == 2 && out.Address == "":
			out.Address = string(s)
		case n == 3 && out.IPv6 == "":
			out.IPv6 = string(s)
		}
	})
}

func decodeSDCard(b []byte, out *SDCard) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, s []byte, v uint64) {
		switch {
		case n == 1 && t == protowire.VarintType:
			out.Detected = v == enable
		case n == 2 && t == protowire.VarintType:
			out.Size = uint32(v)
		case n == 3 && t == protowire.VarintType:
			out.Free = uint32(v)
		case n == 4 && t == protowire.VarintType:
			out.Used = uint32(v)
		case n == 5 && t == protowire.BytesType:
			out.Status = string(s)
		}
	})
}

func decodeSystem(b []byte, out *Status) {
	_ = walk(b, func(n protowire.Number, t protowire.Type, s []byte, _ uint64) {
		if t != protowire.BytesType {
			return
		}

		switch n {
		case 1:
			out.Firmware = string(s)
		case 2:
			out.Hardware = string(s)
		case 3:
			out.Name = string(s)
		case 4:
			decodeSDCard(s, &out.SDCard)
		case 6:
			_ = walk(s, func(rn protowire.Number, rt protowire.Type, url []byte, _ uint64) {
				if rn == 3 && rt == protowire.BytesType {
					out.RTSP = string(url)
				}
			})
		}
	})
}

func appendString(b []byte, num protowire.Number, value string) []byte {
	if value == "" {
		return b
	}

	b = protowire.AppendTag(b, num, protowire.BytesType)

	return protowire.AppendString(b, value)
}

// walk calls visit for each field of one message. A bytes field passes its
// payload, a varint or fixed field its number. Fields of any other type are
// skipped.
func walk(b []byte, visit func(protowire.Number, protowire.Type, []byte, uint64)) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return ErrMalformed
		}

		b = b[n:]

		var (
			payload []byte
			number  uint64
		)

		switch typ {
		case protowire.BytesType:
			payload, n = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			number, n = protowire.ConsumeVarint(b)
		case protowire.Fixed32Type:
			var v uint32
			v, n = protowire.ConsumeFixed32(b)
			number = uint64(v)
		case protowire.Fixed64Type:
			number, n = protowire.ConsumeFixed64(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}

		if n < 0 {
			return ErrMalformed
		}

		b = b[n:]
		visit(num, typ, payload, number)
	}

	return nil
}

// The WebRtcMsgType values.
const (
	WebRTCRequest   = 1
	WebRTCAnswer    = 2
	WebRTCOffer     = 3
	WebRTCCandidate = 4
)

// The values this module sends in a WebRtc message.
const (
	webrtcClient = 2 // WebRtcClientType.CLIENT
	webrtcStop   = 2 // WebRtcStreamStatus.STOP
	videoHD      = 2 // VideoQuality.HD
	videoFPS     = 15
	planPriority = 3
	applyAnyPath = 5 // WebRtcVideoApplyScope.ANY
)

// WebRTC is the part of a camera's WebRtc message this module reads:
//
//	request_id (2), sdp (4) { candidate (1), mid (2) }, message_type (5)
type WebRTC struct {
	RequestID string
	// SDP is the whole description of an offer or an answer, or one
	// a=candidate line of a candidate.
	SDP  string
	Mid  string
	Type int
}

// DecodeWebRTC reads a camera's WebRtc message.
func DecodeWebRTC(b []byte) (WebRTC, error) {
	var out WebRTC

	err := walk(b, func(num protowire.Number, typ protowire.Type, value []byte, number uint64) {
		switch {
		case num == 2 && typ == protowire.BytesType:
			out.RequestID = string(value)
		case num == 4 && typ == protowire.BytesType:
			_ = walk(value, func(n protowire.Number, t protowire.Type, s []byte, _ uint64) {
				if t != protowire.BytesType {
					return
				}

				switch n {
				case 1:
					out.SDP = string(s)
				case 2:
					out.Mid = string(s)
				}
			})
		case num == 5 && typ == protowire.VarintType:
			out.Type = int(number)
		}
	})

	return out, err
}

// WebRTCRequestMessage asks the camera for an offer, as one client, with the
// video it should send: quality, a key of Qualities, or HD when it is not
// one, at 15 frames a second with no time limit, on any path. A camera sent
// no video settings falls back to SD.
func WebRTCRequestMessage(requestID, quality string) []byte {
	level, ok := Qualities[quality]
	if !ok {
		level = videoHD
	}

	var video []byte
	video = protowire.AppendTag(video, 1, protowire.VarintType)
	video = protowire.AppendVarint(video, level)
	video = protowire.AppendTag(video, 2, protowire.VarintType)
	video = protowire.AppendVarint(video, videoFPS)
	video = protowire.AppendTag(video, 3, protowire.VarintType)
	video = protowire.AppendVarint(video, planPriority)
	video = protowire.AppendTag(video, 5, protowire.VarintType)
	video = protowire.AppendVarint(video, applyAnyPath)

	out := webrtcHead(requestID, WebRTCRequest)
	out = protowire.AppendTag(out, 9, protowire.BytesType)

	return protowire.AppendBytes(out, video)
}

// WebRTCAnswerMessage carries the browser's answer, with no candidate in it.
func WebRTCAnswerMessage(requestID, sdp string) []byte {
	return appendSDP(webrtcHead(requestID, WebRTCAnswer), sdp, "")
}

// WebRTCCandidateMessage carries one of the browser's candidates, as its
// a=candidate line.
func WebRTCCandidateMessage(requestID, line, mid string) []byte {
	return appendSDP(webrtcHead(requestID, WebRTCCandidate), line, mid)
}

// WebRTCStopMessage ends the camera's WebRTC sessions. The camera ends every
// one of them, whichever request it names.
func WebRTCStopMessage(requestID string) []byte {
	out := webrtcHead(requestID, 0)
	out = protowire.AppendTag(out, 6, protowire.VarintType)

	return protowire.AppendVarint(out, webrtcStop)
}

func webrtcHead(requestID string, messageType int) []byte {
	out := appendString(nil, 2, requestID)
	if messageType != 0 {
		out = protowire.AppendTag(out, 5, protowire.VarintType)
		out = protowire.AppendVarint(out, uint64(messageType))
	}

	out = protowire.AppendTag(out, 7, protowire.VarintType)

	return protowire.AppendVarint(out, webrtcClient)
}

func appendSDP(out []byte, candidate, mid string) []byte {
	var sdp []byte
	sdp = appendString(sdp, 1, candidate)
	sdp = appendString(sdp, 2, mid)
	out = protowire.AppendTag(out, 4, protowire.BytesType)

	return protowire.AppendBytes(out, sdp)
}
