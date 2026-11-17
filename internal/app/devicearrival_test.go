package app

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// TestAnArrivalReachesASubscriberWhoWasAlreadyWatching is the case the developer
// page polls around. Nothing else announces a printer: the arrival is the
// one event that says a machine is here, so a stream that only carries printers
// it already knew about is a stream that never introduces one.
func TestAnArrivalReachesASubscriberWhoWasAlreadyWatching(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, grantsConfig(dir))

	// Open before the printer dials in, which is what makes this a live read.
	frames := watch(t, running)

	_ = runSimulator(t, running, simulator.Config{
		Model:  "MK4",
		Serial: "SIM-WATCHED",
	}, "")

	require.Eventually(t, func() bool {
		return frames.saw(event.TypeDeviceArrived)
	}, settle, 25*time.Millisecond,
		"a printer dialled in and the subscriber who was already watching was never told")

	require.Eventually(t, func() bool {
		return frames.saw(event.TypeDeviceTelemetrySampled)
	}, settle, 25*time.Millisecond, "telemetry never followed the arrival on the same stream")
}

// eventLog is what an open server-sent event stream has carried so far, in
// order, as the frames a browser reading it would see.
type eventLog struct {
	mu     chan struct{}
	frames []sseFrame
	// ended closes when the server stopped writing, which is how a reader finds
	// out its subscription was dropped rather than narrowed.
	ended chan struct{}
}

// still reports whether the server is still holding the stream open.
func (s *eventLog) still() bool {
	select {
	case <-s.ended:
		return false
	default:
		return true
	}
}

// total is how many frames of every type have arrived.
func (s *eventLog) total() int {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	return len(s.frames)
}

// sseFrame is one frame off the wire: its type and the JSON envelope behind it.
type sseFrame struct {
	kind string
	data string
}

// saw reports whether an event of that type has arrived.
func (s *eventLog) saw(kind string) bool { return s.count(kind) > 0 }

// kinds is the type of every frame that has arrived, in order, for a failure
// message.
func (s *eventLog) kinds() []string {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	out := make([]string, 0, len(s.frames))

	for _, frame := range s.frames {
		out = append(out, frame.kind)
	}

	return out
}

// count is how many frames of one type have arrived.
func (s *eventLog) count(kind string) int {
	<-s.mu
	defer func() { s.mu <- struct{}{} }()

	total := 0

	for _, frame := range s.frames {
		if frame.kind == kind {
			total++
		}
	}

	return total
}

// presence returns every reachability frame, in arrival order.
func (s *eventLog) presence() []sseFrame { return s.framesOfType(event.TypeDevicePresenceChanged) }

func (s *eventLog) record(frame sseFrame) {
	<-s.mu
	s.frames = append(s.frames, frame)
	s.mu <- struct{}{}
}

// watch opens the published event stream and collects the type of every frame
// it carries, the way a browser reading it does.
func watch(t *testing.T, running *process) *eventLog {
	t.Helper()

	return watchWith(t, running, nil)
}

// watchWith is watch with the headers the request carries, which is how a
// client holding a bearer credential rather than a cookie opens the stream.
func watchWith(t *testing.T, running *process, headers map[string]string) *eventLog {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+"/api/v1/events", nil)
	require.NoError(t, err)

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	t.Cleanup(func() { _ = response.Body.Close() })

	require.Equal(t, http.StatusOK, response.StatusCode,
		"the caller could not open a stream, so nothing here proves anything about what reaches one")

	seen := &eventLog{mu: make(chan struct{}, 1), ended: make(chan struct{})}
	seen.mu <- struct{}{}

	go func() {
		defer close(seen.ended)

		lines := bufio.NewScanner(response.Body)

		// A frame's type line precedes its data line, so the type is held until
		// the payload it belongs to arrives and the pair is recorded once.
		kind := ""

		for lines.Scan() {
			if named, is := strings.CutPrefix(lines.Text(), "event: "); is {
				kind = named

				continue
			}

			if data, is := strings.CutPrefix(lines.Text(), "data: "); is && kind != "" {
				seen.record(sseFrame{kind: kind, data: data})
				kind = ""
			}
		}
	}()

	// The frames a subscriber receives are the ones published after it attached,
	// so the reader has to be attached before anything is published.
	time.Sleep(100 * time.Millisecond)

	return seen
}
