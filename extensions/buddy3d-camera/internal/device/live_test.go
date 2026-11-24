package device_test

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
	"github.com/prusa3d/buddy3d-camera/internal/device"
	"github.com/prusa3d/buddy3d-camera/internal/store"
)

const offer = "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nt=0 0\r\na=group:BUNDLE 0 1\r\n" +
	"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:0\r\na=sendonly\r\n" +
	"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\na=mid:1\r\n"

// webrtcFromCamera is a camera's WebRtc message.
func webrtcFromCamera(requestID string, kind uint64, sdp, mid string) []byte {
	var inner []byte
	inner = protowire.AppendTag(inner, 1, protowire.BytesType)
	inner = protowire.AppendString(inner, sdp)
	if mid != "" {
		inner = protowire.AppendTag(inner, 2, protowire.BytesType)
		inner = protowire.AppendString(inner, mid)
	}

	var out []byte
	out = protowire.AppendTag(out, 2, protowire.BytesType)
	out = protowire.AppendString(out, requestID)
	out = protowire.AppendTag(out, 4, protowire.BytesType)
	out = protowire.AppendBytes(out, inner)
	out = protowire.AppendTag(out, 5, protowire.VarintType)

	return protowire.AppendVarint(out, kind)
}

// nextWebRTC reads events off the camera's socket until a webrtc one, and
// answers what it carried.
func (s socket) nextWebRTC() camproto.WebRTC {
	s.t.Helper()

	for {
		header := s.read()
		payload := s.read()

		if strings.Contains(header, `"webrtc"`) {
			message, err := camproto.DecodeWebRTC([]byte(payload))
			if err != nil {
				s.t.Fatal(err)
			}

			return message
		}
	}
}

// The browser asks, the camera offers with a candidate that is put into the
// offer, the browser's answer goes back without its candidate and then the
// candidate alone, and a stop ends it.
func TestTheLiveViewIsSignalledThroughTheCameraSession(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	paired, err := cameras.Pair("bed-cam", "printer-1", "")
	if err != nil {
		t.Fatal(err)
	}

	service := device.New(cameras, &core{}, 30, slog.New(slog.DiscardHandler))
	server := httptest.NewTLSServer(service.Handler())
	t.Cleanup(server.Close)

	camera := dial(t, server)
	camera.emit("1", "camera_authentication", auth(paired.Token))
	_ = camera.read() // the ack

	type started struct {
		session, offer string
		err            error
	}

	done := make(chan started, 1)

	go func() {
		session, sdp, startErr := service.StartLive(t.Context(), "bed-cam")
		done <- started{session, sdp, startErr}
	}()

	request := camera.nextWebRTC()
	if request.Type != camproto.WebRTCRequest || request.RequestID == "" {
		t.Fatalf("the camera was asked %+v", request)
	}

	camera.emit("2", "webrtc", webrtcFromCamera(request.RequestID, camproto.WebRTCOffer, offer, ""))
	camera.emit("3", "webrtc", webrtcFromCamera(request.RequestID, camproto.WebRTCCandidate,
		"a=candidate:1 1 udp 2122260223 10.0.10.56 50000 typ host", "0"))

	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}

	if got.session != request.RequestID {
		t.Fatalf("the session is %q and the camera was asked under %q", got.session, request.RequestID)
	}

	video := got.offer[strings.Index(got.offer, "m=video"):strings.Index(got.offer, "m=application")]
	if !strings.Contains(video, "a=candidate:1 1 udp 2122260223 10.0.10.56 50000 typ host\r\n") {
		t.Fatalf("the candidate is not in the video section:\n%s", got.offer)
	}

	answer := "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:0\r\n" +
		"a=candidate:7 1 udp 1 192.168.1.2 60000 typ host\r\na=end-of-candidates\r\n"
	if err = service.AnswerLive("bed-cam", got.session, answer); err != nil {
		t.Fatal(err)
	}

	sentAnswer := camera.nextWebRTC()
	if sentAnswer.Type != camproto.WebRTCAnswer || strings.Contains(sentAnswer.SDP, "candidate") {
		t.Fatalf("the answer sent was %+v", sentAnswer)
	}

	sentCandidate := camera.nextWebRTC()
	if sentCandidate.Type != camproto.WebRTCCandidate || sentCandidate.Mid != "0" ||
		sentCandidate.SDP != "a=candidate:7 1 udp 1 192.168.1.2 60000 typ host" {
		t.Fatalf("the candidate sent was %+v", sentCandidate)
	}

	if err = service.StopLive("bed-cam", got.session); err != nil {
		t.Fatal(err)
	}

	if stop := camera.nextWebRTC(); stop.RequestID != got.session {
		t.Fatalf("the stop named %q", stop.RequestID)
	}

	if err = service.AnswerLive("bed-cam", got.session, answer); err == nil {
		t.Fatal("an answer was taken for a live view that was stopped")
	}
}
