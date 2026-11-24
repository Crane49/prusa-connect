package camproto_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
)

// The hand-written messages are checked against the camera's own schema,
// compiled here and only here, so the field numbers cannot drift from it
// without a test failing.
func schema(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()

	source, err := os.ReadFile("testdata/prusa_cam.proto")
	if err != nil {
		t.Fatal(err)
	}

	compiler := protocompile.Compiler{Resolver: &protocompile.SourceResolver{
		Accessor: protocompile.SourceAccessorFromMap(map[string]string{"prusa_cam.proto": string(source)}),
	}}

	files, err := compiler.Compile(context.Background(), "prusa_cam.proto")
	if err != nil {
		t.Fatal(err)
	}

	return files[0]
}

func message(t *testing.T, name protoreflect.Name, fromJSON string) []byte {
	t.Helper()

	held := dynamicpb.NewMessage(schema(t).Messages().ByName(name))
	if err := protojson.Unmarshal([]byte(fromJSON), held); err != nil {
		t.Fatal(err)
	}

	out, err := proto.Marshal(held)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func decode(t *testing.T, name protoreflect.Name, b []byte) string {
	t.Helper()

	held := dynamicpb.NewMessage(schema(t).Messages().ByName(name))
	if err := proto.Unmarshal(b, held); err != nil {
		t.Fatal(err)
	}

	// protojson varies its spacing on purpose, so the spacing is taken out.
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(protojson.Format(held))); err != nil {
		t.Fatal(err)
	}

	return compact.String()
}

func TestAuthenticationIsReadAsTheSchemaWritesIt(t *testing.T) {
	t.Parallel()

	got, err := camproto.DecodeAuthentication(message(t, "CameraAuthentication",
		`{"camera_token":"0123456789abcdefghij","fingerprint":"19aa6bb40cb11836dabbb2db33625305"}`))
	if err != nil {
		t.Fatal(err)
	}

	if got.Token != "0123456789abcdefghij" || got.Fingerprint != "19aa6bb40cb11836dabbb2db33625305" {
		t.Fatalf("got %+v", got)
	}
}

// The status a real camera sent prusacam-probe, as the schema encodes it.
func TestAStatusIsReadAsTheSchemaWritesIt(t *testing.T) {
	t.Parallel()

	got, err := camproto.DecodeStatus(message(t, "CameraToServer", `{
		"timelapse": {"enable": "ENABLE", "interval": 15},
		"control": {"cameraMode": "DAY", "snapshotInterval": 30, "snapshotEnable": "ENABLE", "volume": 5},
		"network": {"wifi": {"ssid": "Workshop", "mac": "02:00:00:00:00:01", "ipv4": "10.0.10.56", "signalQuality": 86},
			"ethernet": {"mac": "14:5D:34:95:71:E5", "ipv6": "fe80::1"}},
		"system": {"fwVersion": "3.1.7 rc-7", "hwVersion": "NB.1.0.1", "cameraName": "Buddy3D Camera",
			"sdCard": {"detected": "ENABLE", "size": 30436, "free": 30420, "used": 16, "status": "OK"},
			"rtspServer": {"mode": "RTSP_SERVER_ON", "status": "ENABLE", "url": "rtsp://10.0.10.56/live"},
			"systemTime": {"timezone": "UTC+0", "timesync": "DISABLE"}},
		"timestamp": "1793965565",
		"cameraToken": "0123456789abcdefghij",
		"cameraStatus": {"mcuTemperature": 63.5, "uptimeRaw": "513", "uptime": "0 days, 0:8:33", "totalRam": "35151872"},
		"requestId": "status",
		"video": {"quality": "VIDEO_QUALITY_FHD"}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	want := camproto.Status{
		SnapshotInterval: 30, Mode: "day", Volume: 5, VideoQuality: "fhd", Timelapse: true, TimelapseInterval: 15,
		Name: "Buddy3D Camera", SSID: "Workshop", Address: "10.0.10.56", IPv6: "fe80::1", MAC: "02:00:00:00:00:01",
		SignalQuality: 86, Firmware: "3.1.7 rc-7", Hardware: "NB.1.0.1", RTSP: "rtsp://10.0.10.56/live",
		SDCard:      camproto.SDCard{Detected: true, Size: 30436, Free: 30420, Used: 16, Status: "OK"},
		Temperature: 63.5, Uptime: "0 days, 0:8:33", RequestID: "status",
	}
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestWhatIsSentIsWhatTheSchemaReads(t *testing.T) {
	t.Parallel()

	for stream, want := range map[string]string{
		"":     `"rtspServerMode":"RTSP_SERVER_OFF","webrtcMode":"WEBRTC_MODE_ON"`,
		"rtsp": `"rtspServerMode":"RTSP_SERVER_ON","webrtcMode":"WEBRTC_MODE_OFF"`,
		"off":  `"rtspServerMode":"RTSP_SERVER_OFF","webrtcMode":"WEBRTC_MODE_OFF"`,
	} {
		if got := decode(t, "ServerToCamera", camproto.Configuration(30, stream, "interval")); got !=
			`{"control":{"snapshotInterval":30,`+want+`},"requestId":"interval"}` {
			t.Fatalf("configuration with stream %q: %s", stream, got)
		}
	}

	if got := decode(t, "CameraTrigger", camproto.Trigger(camproto.TriggerSnapshot, "snap")); got !=
		`{"getSnapshot":"ENABLE","requestId":"snap"}` {
		t.Fatalf("trigger: %s", got)
	}

	if got := decode(t, "CameraTrigger", camproto.Trigger(camproto.TriggerFeatures, "features")); got !=
		`{"getFeatures":"ENABLE","requestId":"features"}` {
		t.Fatalf("features trigger: %s", got)
	}

	if got := decode(t, "ServerToCamera", camproto.Rotation("down", 20, "move")); got !=
		`{"control":{"rotation":{"direction":"DOWN","angle":20}},"requestId":"move"}` {
		t.Fatalf("rotation: %s", got)
	}

	if got := decode(t, "ServerToCamera", camproto.Change{Mode: "night"}.Message("mode")); got !=
		`{"control":{"cameraMode":"NIGHT"},"requestId":"mode"}` {
		t.Fatalf("mode: %s", got)
	}

	all := camproto.Change{
		Mode: "auto", Volume: 40, TimelapseInterval: 60, VideoQuality: "sd", Name: "Bed", JobName: "timelapse_1",
	}
	if got := decode(t, "ServerToCamera", all.Message("c")); got !=
		`{"timelapseInterval":60,"control":{"cameraMode":"AUTO","volume":40,"printingJobName":"timelapse_1"},`+
			`"system":{"cameraName":"Bed"},"requestId":"c","video":{"quality":"VIDEO_QUALITY_SD"}}` {
		t.Fatalf("every change: %s", got)
	}

	if got := decode(t, "CameraTrigger", camproto.Toggle(camproto.TriggerTimelapse, false, "t")); got !=
		`{"setTimelapseEnable":"DISABLE","requestId":"t"}` {
		t.Fatalf("timelapse off: %s", got)
	}

	if got := decode(t, "CameraTrigger", camproto.Trigger(camproto.TriggerReboot, "r")); got !=
		`{"startDeviceReboot":"ENABLE","requestId":"r"}` {
		t.Fatalf("reboot: %s", got)
	}
}

// The features a real camera (firmware 3.1.7 rc-7) answered get_features with,
// shortened.
func TestFeaturesAreReadAsTheSchemaWritesThem(t *testing.T) {
	t.Parallel()

	got, err := camproto.DecodeFeatures(message(t, "CameraFeatures", `{
		"cameraToken": "0123456789abcdefghij", "fwVersion": "3.1.7 rc-7", "hwVersion": "NB.1.0.1",
		"protobufSchemaVersion": "4.62", "fingerprint": "19aa6bb40cb11836dabbb2db33625305",
		"features": "[\"GetSnapshot\",\"RotationX\",\"RotationZ\",\"IrMode\"]"
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"GetSnapshot", "RotationX", "RotationZ", "IrMode"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	if _, err = camproto.DecodeFeatures(message(t, "CameraFeatures", `{"features": "RotationX"}`)); err == nil {
		t.Fatal("features that are not a JSON list were read")
	}
}

func TestMalformedBytesAreRefused(t *testing.T) {
	t.Parallel()

	if _, err := camproto.DecodeStatus([]byte{0x1a, 0x10, 0x01}); err == nil {
		t.Fatal("a truncated message was read")
	}
}

// The WebRTC messages this module sends, read back with the camera's schema.
func TestTheWebRTCMessagesAreWhatTheSchemaReads(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		got  []byte
		want string
	}{
		"request": {camproto.WebRTCRequestMessage("s1", ""),
			`{"requestId":"s1","messageType":"WEBRTC_MSG_TYPE_REQUEST","clientType":"WEBRTC_CLIENT_TYPE_CLIENT",` +
				`"videoSettings":{"videoQuality":"VIDEO_QUALITY_HD","videoFps":15,"planPriority":3,` +
				`"applyScope":"WEBRTC_VIDEO_APPLY_SCOPE_ANY"}}`},
		"request at the camera's quality": {camproto.WebRTCRequestMessage("s1", "fhd"),
			`{"requestId":"s1","messageType":"WEBRTC_MSG_TYPE_REQUEST","clientType":"WEBRTC_CLIENT_TYPE_CLIENT",` +
				`"videoSettings":{"videoQuality":"VIDEO_QUALITY_FHD","videoFps":15,"planPriority":3,` +
				`"applyScope":"WEBRTC_VIDEO_APPLY_SCOPE_ANY"}}`},
		"answer": {camproto.WebRTCAnswerMessage("s1", "v=0"),
			`{"requestId":"s1","sdp":{"candidate":"v=0"},"messageType":"WEBRTC_MSG_TYPE_ANSWER",` +
				`"clientType":"WEBRTC_CLIENT_TYPE_CLIENT"}`},
		"candidate": {camproto.WebRTCCandidateMessage("s1", "a=candidate:1 1 udp 1 10.0.0.2 5000 typ host", "0"),
			`{"requestId":"s1","sdp":{"candidate":"a=candidate:1 1 udp 1 10.0.0.2 5000 typ host","mid":"0"},` +
				`"messageType":"WEBRTC_MSG_TYPE_CANDIDATE","clientType":"WEBRTC_CLIENT_TYPE_CLIENT"}`},
		"stop": {camproto.WebRTCStopMessage("s1"),
			`{"requestId":"s1","webrtcStreamStatus":"WEBRTC_STREAM_STATUS_STOP","clientType":"WEBRTC_CLIENT_TYPE_CLIENT"}`},
	} {
		if got := decode(t, "WebRtc", tc.got); got != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", name, got, tc.want)
		}
	}
}

func TestACamerasOfferIsRead(t *testing.T) {
	t.Parallel()

	got, err := camproto.DecodeWebRTC(message(t, "WebRtc",
		`{"requestId":"s1","sdp":{"candidate":"v=0\r\n","mid":"0"},"messageType":"WEBRTC_MSG_TYPE_OFFER",`+
			`"clientType":"WEBRTC_CLIENT_TYPE_CAMERA"}`))
	if err != nil {
		t.Fatal(err)
	}

	if got != (camproto.WebRTC{RequestID: "s1", SDP: "v=0\r\n", Mid: "0", Type: camproto.WebRTCOffer}) {
		t.Fatalf("got %+v", got)
	}
}
