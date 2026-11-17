package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	// operator is the subject fixedPrincipal resolves every request to here,
	// and the key the graph below is written against.
	operator = "user:operator"

	// settle is how long a case waits for something that has to cross a
	// coalescing window and a socket. The window is 100 ms, so this is two
	// orders of magnitude of headroom on a loaded machine.
	settle = 10 * time.Second

	// quiet is how long a case waits to conclude that nothing more is coming.
	// It is several windows rather than several seconds, because every case
	// that uses it has already observed the frames that were supposed to
	// arrive.
	quiet = 5 * stream.MinInterval

	// printerState is a durable printer event, named from the vocabulary because
	// the hub refuses a type in the core namespace that nothing declares.
	printerState = event.TypeDeviceArrived

	// otherEpoch is a generation this hub did not mint, which is what a reader
	// holds after the process it was talking to restarted.
	otherEpoch = "01JQZ0000000000000000000FF"
)

// movableGraph is an authorization graph a case can move. Every change bumps
// the epoch, which is the only thing a live subscription reads on its tick.
type movableGraph struct {
	mu      sync.Mutex
	visible map[string][]event.Origin
	epoch   atomic.Uint64
	asked   atomic.Int64
}

func newGraph(origins ...event.Origin) *movableGraph {
	g := &movableGraph{visible: map[string][]event.Origin{operator: origins}}
	g.epoch.Store(1)

	return g
}

func (g *movableGraph) VisibleOrigins(_ context.Context, principal string) (stream.Visibility, error) {
	g.asked.Add(1)

	g.mu.Lock()
	defer g.mu.Unlock()

	origins := make([]stream.Grant, 0, len(g.visible[principal]))
	for _, origin := range g.visible[principal] {
		origins = append(origins, stream.Grant{Origin: origin})
	}

	return stream.Visibility{Origins: origins, Epoch: g.epoch.Load()}, nil
}

func (g *movableGraph) Epoch() uint64 { return g.epoch.Load() }

// grant replaces what the operator may see, which is what a policy write does.
func (g *movableGraph) grant(origins ...event.Origin) {
	g.mu.Lock()
	g.visible[operator] = origins
	g.mu.Unlock()

	g.epoch.Add(1)
}

// noEvents is the hub a server gets when its case is not about the stream. It
// refuses: a stub that granted everything would make an unrelated case the
// place a permission bug hides.
type noEvents struct{}

func (noEvents) Subscribe(context.Context, stream.SubscribeRequest) (*stream.Subscription, error) {
	return nil, stream.ErrNotPermitted
}

func (noEvents) Subscribers() int { return 0 }

// watchedHub is the real hub with the subscription it last handed out kept, so
// a case can watch a live subscription narrow rather than infer that it did.
type watchedHub struct {
	hub  *stream.Hub
	mu   sync.Mutex
	last *stream.Subscription
}

func (h *watchedHub) Subscribe(
	ctx context.Context, req stream.SubscribeRequest,
) (*stream.Subscription, error) {
	sub, err := h.hub.Subscribe(ctx, req)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.last = sub
	h.mu.Unlock()

	return sub, nil
}

func (h *watchedHub) Subscribers() int { return h.hub.Subscribers() }

func (h *watchedHub) subscription() *stream.Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.last
}

func printerOf(t testing.TB, id string) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, id)
	require.NoError(t, err)

	return origin
}

func stateOf(origin event.Origin, seq int) stream.Publication {
	return stream.Publication{
		Origin: origin,
		Type:   printerState,
		Class:  event.ClassState,
		Data:   json.RawMessage(fmt.Sprintf(`{"seq":%d}`, seq)),
	}
}

func telemetryOf(origin event.Origin, key string, value int) stream.Publication {
	return stream.Publication{
		Origin: origin,
		Type:   event.TypeDeviceTelemetrySampled,
		Class:  event.ClassTelemetry,
		Key:    key,
		Data:   json.RawMessage(fmt.Sprintf(`{"%s":%d}`, key, value)),
	}
}

// eventsFixture is a real hub behind the real server over a real socket.
// Nothing in the path is a stand-in except the graph, which is the one thing a
// case has to be able to move.
type eventsFixture struct {
	events *watchedHub
	graph  *movableGraph
	url    string
	// base is the listener's own root, which the stats path hangs off. url is
	// the stream and carries the operation's path already.
	base string
}

func newEventsFixture(t *testing.T, graph *movableGraph, opts stream.Options) *eventsFixture {
	t.Helper()

	return fixtureOver(t, graph, opts, time.Hour, defaultRetry, httptest.NewServer)
}

// fixtureOver builds the fixture with the heartbeat interval and the listener
// the case needs. The heartbeat is a configuration key, so a case about it sets
// it rather than waiting out a constant.
func fixtureOver(
	t *testing.T,
	graph *movableGraph,
	opts stream.Options,
	heartbeat, retry time.Duration,
	serve func(http.Handler) *httptest.Server,
) *eventsFixture {
	t.Helper()

	opts.Site = "local"
	opts.Authorizer = graph

	if opts.Interval == 0 {
		opts.Interval = stream.MinInterval
	}

	hub, err := stream.New(opts)
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	events := &watchedHub{hub: hub}

	server, err := New(eventsConfig(events, heartbeat, retry))
	require.NoError(t, err)

	listener := serve(server.Handler())
	t.Cleanup(listener.Close)

	return &eventsFixture{
		events: events, graph: graph, url: listener.URL + EventsPath, base: listener.URL,
	}
}

// subscribers reads the published reader count, as a client does.
func (f *eventsFixture) subscribers(t *testing.T) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.base+eventStatsPath, nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { require.NoError(t, response.Body.Close()) }()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, response.StatusCode, "the stats path answered: %s", body)

	var answer EventStreamStats

	require.NoError(t, json.Unmarshal(body, &answer))

	return answer.Subscribers
}

func eventsConfig(events EventStream, heartbeat, retry time.Duration) Config {
	return Config{
		Version:    "test",
		Principals: fixedPrincipal(operator),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     events,
		Heartbeat:  heartbeat,
		Retry:      retry,
	}
}

// get makes the request and reads the whole answer, which is what a case about
// a refusal wants: a status and a problem document, not a stream.
func (f *eventsFixture) get(t *testing.T, query string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.url+query, nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { require.NoError(t, response.Body.Close()) }()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return response.StatusCode, string(body)
}

func (f *eventsFixture) publish(t *testing.T, pub stream.Publication) {
	t.Helper()

	_, err := f.events.hub.Publish(pub)
	require.NoError(t, err)
}

// sseFrame is one frame as a client parses it. A comment frame carries nothing
// else, which is what makes a heartbeat unable to move Last-Event-ID.
type sseFrame struct {
	id      string
	name    string
	data    string
	comment string
	retry   string
}

// reader is an open stream and the frames coming off it.
type reader struct {
	frames <-chan sseFrame
	status int
	header http.Header
	cancel context.CancelFunc
	body   io.ReadCloser
}

// open connects and starts parsing. lastEventID is sent when it is not empty,
// which is what a browser does on its own after a disconnect.
func (f *eventsFixture) open(t *testing.T, lastEventID string) *reader {
	t.Helper()

	return f.openWith(t, "", lastEventID)
}

// openWith connects with a query string, which is how a case asks for the
// narrowing and the window the operation publishes.
func (f *eventsFixture) openWith(t *testing.T, query, lastEventID string) *reader {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url+query, nil)
	require.NoError(t, err)

	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		cancel()
		t.Fatalf("open the stream: %v", err)
	}

	// A refusal carries no frames, and neither does a stream that is quiet, so
	// every case below that asserts what did not arrive would be satisfied by a
	// refusal it was written to rule out. The status is read here rather than in
	// each of them, because a case that forgets it still reads as green.
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the stream was answered %d rather than opened", response.StatusCode)
	}

	frames := make(chan sseFrame, 256)
	go parseFrames(response.Body, frames)

	r := &reader{
		frames: frames,
		status: response.StatusCode,
		header: response.Header,
		cancel: cancel,
		body:   response.Body,
	}
	t.Cleanup(r.close)

	return r
}

func (r *reader) close() {
	r.cancel()
	_ = r.body.Close()
}

// next returns the next frame, failing the case if none arrives.
func (r *reader) next(t *testing.T) sseFrame {
	t.Helper()

	select {
	case frame, open := <-r.frames:
		require.True(t, open, "the stream closed while a frame was expected")

		return frame
	case <-time.After(settle):
		t.Fatal("no frame arrived")

		return sseFrame{}
	}
}

// nextEvent skips what a client ignores, so a case about content is not written
// around a heartbeat landing in the middle of it.
func (r *reader) nextEvent(t *testing.T) sseFrame {
	t.Helper()

	for {
		if frame := r.next(t); frame.name != "" {
			return frame
		}
	}
}

// sawEvent reports whether any frame carrying an event name arrived inside the
// window. It is the negative form the next helpers cannot express: they fail a
// case when nothing arrives, and here nothing arriving is the answer.
func (r *reader) sawEvent(t *testing.T, within time.Duration) bool {
	t.Helper()

	deadline := time.After(within)

	for {
		select {
		case frame, open := <-r.frames:
			if !open {
				return false
			}

			if frame.name != "" {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// lastEventID drains what is left and returns what a browser would replay: the
// id of the last frame that carried one, whether or not that frame dispatched an
// event to page code. A frame with an id and no data member moves it and
// delivers nothing, which is how this server hands a reader following several
// origins a complete place to resume from.
func (r *reader) lastEventID() string {
	deadline := time.After(quiet)

	var last string

	for {
		select {
		case frame, open := <-r.frames:
			if !open {
				return last
			}

			if frame.id != "" {
				last = frame.id
			}
		case <-deadline:
			return last
		}
	}
}

// nextComment skips ahead to the next comment frame, so a case about the
// heartbeat is not written around the reconnection delay that precedes it.
func (r *reader) nextComment(t *testing.T) sseFrame {
	t.Helper()

	for {
		if frame := r.next(t); frame.comment != "" {
			return frame
		}
	}
}

// collect returns every event frame that arrives before the stream falls quiet,
// which is how a case asserts what it does and does not carry from here on.
func (r *reader) collect() []sseFrame {
	deadline := time.After(quiet)

	var frames []sseFrame

	for {
		select {
		case frame, open := <-r.frames:
			if !open {
				return frames
			}

			if frame.name != "" {
				frames = append(frames, frame)
			}
		case <-deadline:
			return frames
		}
	}
}

// parseFrames turns the byte stream into frames, one per blank line, which is
// what the server-sent events grammar says a frame is.
func parseFrames(body io.ReadCloser, out chan<- sseFrame) {
	defer close(out)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	var frame sseFrame

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "":
			if frame != (sseFrame{}) {
				out <- frame
				frame = sseFrame{}
			}
		case strings.HasPrefix(line, ":"):
			frame.comment = strings.TrimPrefix(line, ":")
		case strings.HasPrefix(line, "id: "):
			frame.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			frame.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			frame.data = strings.TrimPrefix(line, "data: ")
		case strings.HasPrefix(line, "retry: "):
			frame.retry = strings.TrimPrefix(line, "retry: ")
		}
	}
}

// envelopeOf reads the CloudEvents envelope out of a frame, the way a browser
// reading the stream does.
func envelopeOf(t *testing.T, frame sseFrame) event.Envelope {
	t.Helper()

	var envelope event.Envelope
	require.NoError(t, json.Unmarshal([]byte(frame.data), &envelope))

	return envelope
}

// continuityOf reads the payload of the frame this transport authors itself.
func continuityOf(t *testing.T, frame sseFrame) continuity {
	t.Helper()

	var lost continuity
	require.NoError(t, json.Unmarshal([]byte(frame.data), &lost))

	return lost
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestAStreamCarriesThePublishedEnvelope is the whole point of the endpoint:
// something outside the process reads what the hub published, framed as
// server-sent events, with the place to resume from on every frame.
func TestAStreamCarriesThePublishedEnvelope(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	open := fixture.open(t, "")
	require.Equal(t, http.StatusOK, open.status)

	fixture.publish(t, stateOf(printer, 1))

	frame := open.nextEvent(t)
	require.Equal(t, printerState, frame.name)
	require.Equal(t, printer.String(), envelopeOf(t, frame).Subject)
	require.NotEmpty(t, frame.id, "the frame carries no place to resume from")
}

// TestTelemetryCarriesNoPlaceToResumeFrom is the half of the frame layout a
// reader depends on without knowing it: telemetry is conflated rather than
// replayed, so stamping it with an id would hand a browser a Last-Event-ID that
// names a position no replay can honour.
func TestTelemetryCarriesNoPlaceToResumeFrom(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	open := fixture.open(t, "")
	fixture.publish(t, telemetryOf(printer, "nozzle", 215))

	frame := open.nextEvent(t)
	require.Empty(t, frame.id, "a telemetry frame carried a place a reader cannot resume from")
}

// TestTheStreamSetsTheHeadersAnIntermediaryReads is the difference between a
// stream that works on a developer's machine and one that works behind nginx,
// which buffers a proxied response by default.
func TestTheStreamSetsTheHeadersAnIntermediaryReads(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	header := fixture.open(t, "").header

	require.Equal(t, "text/event-stream", header.Get("Content-Type"))
	require.Equal(t, "no-cache", header.Get("Cache-Control"))
	require.Equal(t, "no", header.Get("X-Accel-Buffering"))
}

// TestAnIdleStreamHeartbeats is what keeps a quiet stream from being reaped by
// something between this server and its reader.
func TestAnIdleStreamHeartbeats(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := fixtureOver(t, newGraph(printer), stream.Options{}, 20*time.Millisecond,
		defaultRetry, httptest.NewServer)

	beat := fixture.open(t, "").nextComment(t)
	require.Equal(t, " heartbeat", beat.comment)
	require.Empty(t, beat.id, "a heartbeat moved the place the reader would resume from")
}

// TestTheStreamIsAuthorizedOnceAtSubscribe is an earlier design decision. Authorization is a
// question asked when the connection is made, and channel membership is the
// answer for every event afterwards.
func TestTheStreamIsAuthorizedOnceAtSubscribe(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	open := fixture.open(t, "")

	asked := fixture.graph.asked.Load()
	require.Equal(t, int64(1), asked, "opening one stream asked the graph more than once")

	for i := range 20 {
		fixture.publish(t, stateOf(printer, i+1))
	}

	for range 20 {
		require.Equal(t, printerState, open.nextEvent(t).name)
	}

	require.Equal(t, asked, fixture.graph.asked.Load(),
		"the graph was asked again while the stream ran, which is a permission check per event")
}

// TestAPrincipalTheGraphGrantsNothingGetsAnEmptyStream is the other half of
// E10 on the wire. A caller who may observe nothing opens the stream and is
// given no event, which withholds exactly what a refusal withholds and does not
// leave a page opened before the first printer dialled in on its own reconnect
// ladder for the arrival it was waiting for.
func TestAPrincipalTheGraphGrantsNothingGetsAnEmptyStream(t *testing.T) {
	t.Parallel()

	fixture := newEventsFixture(t, newGraph(), stream.Options{})

	open := fixture.open(t, "")
	require.Equal(t, http.StatusOK, open.status, "the caller was refused rather than given a stream")

	fixture.publish(t, stateOf(printerOf(t, "mk4-017"), 1))

	require.False(t, open.sawEvent(t, 4*stream.DefaultInterval),
		"a caller the graph grants nothing was given an event")
}

// TestACursorFromAPrincipalTheGraphGrantsNothingIsAnsweredWithSilence is the
// third case of a resume this hub cannot honour. An origin that exists and one
// that does not are already indistinguishable; a generation this hub never
// minted must be too, because the frame that answers one carries the generation
// this process did mint, and that is a ULID whose first 48 bits are the moment
// it started.
func TestACursorFromAPrincipalTheGraphGrantsNothingIsAnsweredWithSilence(t *testing.T) {
	t.Parallel()

	fixture := newEventsFixture(t, newGraph(), stream.Options{})

	open := fixture.openWith(t, "", otherEpoch+":printer/mk4-017:7")

	require.Empty(t, open.collect(), "a caller the graph grants nothing was told about a cursor it never held")
	require.Empty(t, open.lastEventID(), "this process's generation reached a caller the graph grants nothing")
}

// TestALastEventIDDoesNotReachBehindTheGrant is the leak as a client reaches
// it. Last-Event-ID is a request header the client writes, and offset zero in it
// asks for a printer's whole retained window: model, firmware and serial for
// every arrival still in it. A principal granted that printer only after it had
// been publishing is given none of what predates the grant, and is told its
// place is gone rather than quietly started at the head.
func TestALastEventIDDoesNotReachBehindTheGrant(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(), stream.Options{})

	for seq := range 5 {
		fixture.publish(t, stateOf(printer, seq+1))
	}

	fixture.graph.grant(printer)

	open := fixture.openWith(t, "",
		fixture.events.hub.Epoch()+":"+printer.Path()+"=0")

	frames := open.collect()
	require.NotEmpty(t, frames, "the reader was told nothing at all about a cursor it handed over")

	for _, frame := range frames {
		require.Equalf(t, ContinuityLost, frame.name,
			"a window published before the grant reached the reader: %s", frame.data)
	}

	// And the stream carries on from the head, so the reader that was told its
	// place is gone is not left with a connection carrying nothing.
	fixture.publish(t, stateOf(printer, 6))
	require.Equal(t, printer.String(), envelopeOf(t, open.nextEvent(t)).Subject)
}

// TestANarrowedRequestFromAPrincipalTheGraphGrantsNothingIsRefused is the line
// the empty stream above does not cross. A caller who named the printer they
// mean to watch is told no, in a refusal that names no printer: one that
// distinguished "not yours" from "does not exist" would answer the question it
// exists to withhold.
func TestANarrowedRequestFromAPrincipalTheGraphGrantsNothingIsRefused(t *testing.T) {
	t.Parallel()

	fixture := newEventsFixture(t, newGraph(), stream.Options{})

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fixture.url+"?origins="+printerOf(t, "mk4-017").String(), nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { require.NoError(t, response.Body.Close()) }()

	require.Equal(t, http.StatusForbidden, response.StatusCode)

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	var problem fault.Problem
	require.NoError(t, json.Unmarshal(body, &problem))
	require.Equal(t, "core.auth/ACCESS_DENIED", problem.Code.String())
	require.NotContains(t, string(body), "printer:", "the refusal names a resource")
}

// TestARevokedGrantNarrowsALiveStream is the old backlog's revocation box and E10's
// second half over the wire: the reader keeps its connection, loses one printer
// inside a coalescing window, and keeps receiving the other.
func TestARevokedGrantNarrowsALiveStream(t *testing.T) {
	t.Parallel()

	kept := printerOf(t, "mk4-017")
	lost := printerOf(t, "core-one-2")

	fixture := newEventsFixture(t, newGraph(kept, lost), stream.Options{})
	open := fixture.open(t, "")

	fixture.publish(t, stateOf(lost, 1))
	require.Equal(t, lost.String(), envelopeOf(t, open.nextEvent(t)).Subject,
		"the printer was granted when this was published")

	fixture.graph.grant(kept)

	// The pump re-reads the epoch once per window, so a window has to close for
	// the withdrawal to land. Publishing is what closes one.
	require.Eventually(t, func() bool {
		fixture.publish(t, stateOf(kept, 2))

		return len(fixture.events.subscription().Origins()) == 1
	}, settle, stream.MinInterval, "the withdrawn printer is still routed to an open subscription")

	open.collect()

	fixture.publish(t, stateOf(lost, 3))
	fixture.publish(t, stateOf(kept, 3))

	frames := open.collect()
	require.NotEmpty(t, frames, "the stream stopped carrying the printer the reader still holds")

	for _, frame := range frames {
		require.Equal(t, kept.String(), envelopeOf(t, frame).Subject,
			"a printer the reader lost kept reaching its open stream")
	}
}

// TestAReaderResumesFromLastEventID is an earlier design decision over SSE: the client hands
// back the id of the last frame it saw and is given what it missed, which no
// comparable printer project does at all.
func TestAReaderResumesFromLastEventID(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	first := fixture.open(t, "")
	fixture.publish(t, stateOf(printer, 1))

	seen := first.nextEvent(t)
	require.JSONEq(t, `{"seq":1}`, string(envelopeOf(t, seen).Data))

	first.close()

	// Published while nobody is connected, which is the gap a resume fills.
	fixture.publish(t, stateOf(printer, 2))

	resumed := fixture.open(t, seen.id)
	require.JSONEq(t, `{"seq":2}`, string(envelopeOf(t, resumed.nextEvent(t)).Data),
		"the reader was not given the event it missed, or was given one it already had")
}

// TestACursorThisHubNeverIssuedIsToldTheContinuityIsGone is the resume that
// cannot be honoured: a restart recreates the log, so offset 7 in it is a
// different event than offset 7 here. The reader is told outright rather than
// handed part of a stream, because there is no partial recovery (E6).
func TestACursorThisHubNeverIssuedIsToldTheContinuityIsGone(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	frame := fixture.open(t, otherEpoch+":printer/mk4-017:7").nextEvent(t)

	require.Equal(t, ContinuityLost, frame.name)
	require.Equal(t, frame.id, continuityOf(t, frame).Cursor,
		"the frame's id is not the cursor it tells the reader to carry on from")
	require.NotContains(t, frame.id, otherEpoch,
		"the reader was handed back the generation that no longer exists")
}

// TestAReaderThatFallsBehindIsToldRatherThanQuietlyTruncated bounds one
// subscriber's event queue at a single byte, so the first event overflows it.
// What matters is what the reader receives: a frame saying continuity is gone,
// not a stream quietly missing events (E8).
func TestAReaderThatFallsBehindIsToldRatherThanQuietlyTruncated(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{EventBytes: 1})

	open := fixture.open(t, "")
	fixture.publish(t, stateOf(printer, 1))

	frame := open.nextEvent(t)
	require.Equal(t, ContinuityLost, frame.name)
	require.Positive(t, continuityOf(t, frame).Dropped,
		"the reader was told it lost frames without being told how many")
}

// TestAMalformedLastEventIDIsRefused keeps a client that resumed from nothing
// from being answered as though it had resumed, which would cost it exactly the
// events it reconnected to collect. Text that is a cursor but names a
// generation this hub never minted is a different case: that reader is told its
// continuity is gone rather than refused, which is the case above.
func TestAMalformedLastEventIDIsRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"an event id with no generation": "printer/mk4-017:9",
		"a position with no offset":      otherEpoch + ":printer/mk4-017",
	}

	for name, lastEventID := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			printer := printerOf(t, "mk4-017")
			fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fixture.url, nil)
			require.NoError(t, err)

			request.Header.Set("Last-Event-ID", lastEventID)

			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)

			defer func() { require.NoError(t, response.Body.Close()) }()

			require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
		})
	}
}

// TestBothFormsOfLastEventIDResume covers the two ids this server issues: one
// event's place, which a browser replays on its own, and the whole cursor,
// which a continuity-lost frame carries so a reader following several origins
// resumes all of them rather than one.
func TestBothFormsOfLastEventIDResume(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	position := event.Position{Origin: printer, Offset: 3}
	cursor := event.NewCursor(event.NewGeneration(otherEpoch, []event.Position{position}))

	tests := map[string]string{
		"one event's place": position.ID(otherEpoch),
		"the whole cursor":  cursor.String(),
	}

	for name, lastEventID := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resume, err := resumeFrom(lastEventID)
			require.NoError(t, err)

			generation, held := resume.Generation(otherEpoch)
			require.True(t, held)

			offset, named := generation.Offset(printer)
			require.True(t, named)
			require.Equal(t, uint64(3), offset)
		})
	}
}

// TestAHeadRequestDoesNotOpenAStream is the hazard of answering HEAD from the
// GET handler. The client returns as soon as it has the headers either way, so
// what is asserted is what is left behind: a subscription and the connection it
// rides on, held for a probe that has already read everything it asked for.
func TestAHeadRequestDoesNotOpenAStream(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	request, err := http.NewRequestWithContext(t.Context(), http.MethodHead, fixture.url, nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { require.NoError(t, response.Body.Close()) }()

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))

	require.Eventually(t, func() bool {
		return fixture.events.hub.Subscribers() == 0
	}, quiet, time.Millisecond, "the HEAD left a subscription open, and the connection with it")
}

// TestADrainingListenerEndsAnOpenStream is the shutdown budget. An event stream
// is one request that ends when its reader says so, so a drain would otherwise
// wait out its whole budget on a reader that is behaving perfectly.
func TestADrainingListenerEndsAnOpenStream(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	graph := newGraph(printer)

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: graph, Interval: stream.MinInterval})
	require.NoError(t, err)

	defer hub.Close()

	events := &watchedHub{hub: hub}

	server, err := New(eventsConfig(events, time.Hour, defaultRetry))
	require.NoError(t, err)

	socket, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listener := server.HTTPServer(socket.Addr().String())
	go func() { _ = listener.Serve(socket) }()

	fixture := &eventsFixture{
		events: events,
		graph:  graph,
		url:    "http://" + socket.Addr().String() + EventsPath,
	}

	open := fixture.open(t, "")
	fixture.publish(t, stateOf(printer, 1))
	require.Equal(t, printerState, open.nextEvent(t).name)

	stopping, cancel := context.WithTimeout(t.Context(), settle)
	defer cancel()

	started := time.Now()
	require.NoError(t, listener.Shutdown(stopping), "the drain ran out of budget on an open stream")
	require.Less(t, time.Since(started), settle, "the drain waited on a stream that should have ended")

	_, stillOpen := <-open.frames
	require.False(t, stillOpen, "the stream outlived the listener that was draining")
}

// TestNewRefusesWithoutAHubBehindTheStream keeps the operation from being
// published by a process that cannot serve it.
func TestNewRefusesWithoutAHubBehindTheStream(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		Principals: fixedPrincipal(operator),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
	})
	require.ErrorIs(t, err, ErrNoEventStream)
}

// countingWriter records how many times something reached the socket, which is
// the number an earlier design decision is about.
type countingWriter struct {
	writes int
	bytes  []byte
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	w.bytes = append(w.bytes, p...)

	return len(p), nil
}

// deadWriter is a ResponseWriter that supports neither flushing nor deadlines,
// so the writer's tolerance of one is exercised rather than assumed.
type deadWriter struct{}

func (deadWriter) Header() http.Header         { return http.Header{} }
func (deadWriter) Write(p []byte) (int, error) { return len(p), nil }
func (deadWriter) WriteHeader(int)             {}

// liveWriter is a ResponseWriter that supports both, which is what a real
// connection is. The allocation count is measured against this rather than
// against deadWriter, because net/http allocates the refusal it answers an
// unsupported call with.
type liveWriter struct{ deadWriter }

func (liveWriter) Flush()                           {}
func (liveWriter) SetWriteDeadline(time.Time) error { return nil }

// TestAWindowLeavesInOneWrite is an earlier design decision at the socket. Coalescing bounds
// the frames, and this bounds the write syscalls, which is the other half of
// what the decision claims and the half a per-frame writer loses quietly.
func TestAWindowLeavesInOneWrite(t *testing.T) {
	t.Parallel()

	// The stamped case is here because the cursor frame is written into the same
	// buffer: a window that fitted before must still fit with it.
	for name, batch := range map[string]stream.Batch{
		"one origin":  windowOf(t, 8),
		"two origins": windowAcross(t, 8),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			counter := &countingWriter{}
			out := bufio.NewWriterSize(counter, eventStreamBuffer)
			reader := &eventStream{log: discardLogger()}

			require.NoError(t, reader.writeWindow(out, http.NewResponseController(deadWriter{}), batch))
			require.Equal(t, 1, counter.writes, "a window of %d frames took %d writes",
				len(batch.Telemetry)+len(batch.Events), counter.writes)
		})
	}
}

// TestAWindowIsWrittenAsTheHubEncodedIt is an earlier design decision at this layer. The hub
// marshals once per encoding and shares the bytes, and a transport that copied
// or re-encoded them per reader would undo that without failing anything else.
func TestAWindowIsWrittenAsTheHubEncodedIt(t *testing.T) {
	t.Parallel()

	batch := windowOf(t, 4)

	var expected []byte

	for _, frame := range append(batch.Telemetry, batch.Events...) {
		expected = append(expected, frame.Bytes...)
	}

	counter := &countingWriter{}
	out := bufio.NewWriterSize(counter, eventStreamBuffer)
	reader := &eventStream{log: discardLogger()}

	require.NoError(t, reader.writeWindow(out, http.NewResponseController(deadWriter{}), batch))
	require.Equal(t, string(expected), string(counter.bytes))
}

// TestTheStreamWritesAWithdrawnFrame is the wire shape a client routes on: the
// frame names the origin, and its id is the window's cursor, so a browser that
// reconnects after it resumes past it.
func TestTheStreamWritesAWithdrawnFrame(t *testing.T) {
	t.Parallel()

	printerB := printerOf(t, "mk4-018")
	cursor := event.NewCursor(event.NewGeneration("e1", []event.Position{{Origin: printerOf(t, "mk4-017"), Offset: 3}}))

	var written strings.Builder

	out := bufio.NewWriterSize(&written, eventStreamBuffer)
	reader := &eventStream{log: discardLogger()}

	require.NoError(t, reader.writeWindow(out, http.NewResponseController(deadWriter{}),
		stream.Batch{Withdrawn: []event.Origin{printerB}, Cursor: cursor}))

	frames := make(chan sseFrame, 4)
	parseFrames(io.NopCloser(strings.NewReader(written.String())), frames)

	frame := <-frames
	require.Equal(t, OriginWithdrawn, frame.name)
	require.Equal(t, "com.prusa.connect.stream.origin.withdrawn", frame.name)
	require.JSONEq(t, `{"origin":"device:mk4-018"}`, frame.data)
	require.Equal(t, cursor.String(), frame.id)
	require.Empty(t, frames, "a window carrying only a withdrawal wrote more than one frame")
}

// TestWritingAWindowAllocatesNothing is the hot-path rule: nothing between a
// published frame and a subscriber's socket may allocate in proportion to the
// subscriber count, and an allocation here is one per reader per window.
// It does not run in parallel, because AllocsPerRun measures the whole process.
func TestWritingAWindowAllocatesNothing(t *testing.T) {
	batch := windowOf(t, 4)

	out := bufio.NewWriterSize(io.Discard, eventStreamBuffer)
	control := http.NewResponseController(liveWriter{})
	reader := &eventStream{log: discardLogger()}

	allocs := testing.AllocsPerRun(100, func() {
		_ = reader.writeWindow(out, control, batch)
	})

	require.Zero(t, allocs, "writing one window allocated %v times per reader", allocs)
}

// windowOf builds one coalescing window the way the hub does, so the writer is
// handed the real encoding rather than a test's idea of it.
func windowOf(t testing.TB, n int) stream.Batch {
	t.Helper()

	printer := printerOf(t, "mk4-017")

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Authorizer: newGraph(printer),
		Interval:   stream.MinInterval,
	})
	require.NoError(t, err)

	defer hub.Close()

	sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{
		Principal: operator,
		Encoding:  stream.EncodingSSE,
	})
	require.NoError(t, err)

	defer sub.Close()

	for i := range n {
		// A distinct key each time: telemetry conflates, so one key published
		// n times is one frame rather than n.
		_, err = hub.Publish(telemetryOf(printer, fmt.Sprintf("metric-%d", i), i))
		require.NoError(t, err)

		_, err = hub.Publish(stateOf(printer, i+1))
		require.NoError(t, err)
	}

	var batch stream.Batch

	deadline := time.After(settle)

	for len(batch.Telemetry) < n || len(batch.Events) < n {
		select {
		case next, open := <-sub.Batches():
			require.True(t, open, "the subscription closed before the window was full")

			batch.Telemetry = append(batch.Telemetry, next.Telemetry...)
			batch.Events = append(batch.Events, next.Events...)
		case <-deadline:
			t.Fatal("the published events never reached the subscription")
		}
	}

	return batch
}

// TestAReconnectReplaysOnlyWhatTheReaderMissed is the fleet-wide reconnect. A
// browser replays the last id it was given and the stream it reopens follows
// several origins, so every one of them has to resume where this reader actually
// left off: no duplicate, and nothing missed.
func TestAReconnectReplaysOnlyWhatTheReaderMissed(t *testing.T) {
	t.Parallel()

	first := printerOf(t, "mk4-017")
	second := printerOf(t, "core-one-2")
	fixture := newEventsFixture(t, newGraph(first, second), stream.Options{})

	open := fixture.open(t, "")

	fixture.publish(t, stateOf(first, 1))
	require.Equal(t, first.String(), envelopeOf(t, open.nextEvent(t)).Subject)

	fixture.publish(t, stateOf(second, 1))
	require.Equal(t, second.String(), envelopeOf(t, open.nextEvent(t)).Subject)

	resume := open.lastEventID()
	open.close()

	// Published while nobody is connected: one event per origin, and both are
	// what the reconnect exists to collect.
	fixture.publish(t, stateOf(first, 2))
	fixture.publish(t, stateOf(second, 2))

	got := make([]string, 0, 2)
	for _, frame := range fixture.open(t, resume).collect() {
		envelope := envelopeOf(t, frame)
		got = append(got, envelope.Subject+" "+string(envelope.Data))
	}

	require.Equal(t, []string{
		first.String() + ` {"seq":2}`,
		second.String() + ` {"seq":2}`,
	}, got, "the reconnect did not replay exactly the two events published while it was gone")
}

// TestAReconnectSurvivesAnotherOriginOutrunningItsWindow is the same reconnect
// against a fleet where somebody else is busy. A reader that holds its own place
// must not be told its continuity is gone because a printer it happened not to
// be receiving from filled its replay window.
func TestAReconnectSurvivesAnotherOriginOutrunningItsWindow(t *testing.T) {
	t.Parallel()

	slow, busy := printerOf(t, "mk4-017"), printerOf(t, "core-one-2")
	fixture := newEventsFixture(t, newGraph(slow, busy), stream.Options{HistoryEvents: 2})

	open := fixture.open(t, "")

	fixture.publish(t, stateOf(slow, 1))
	require.Equal(t, slow.String(), envelopeOf(t, open.nextEvent(t)).Subject)

	resume := open.lastEventID()
	open.close()

	// Past the two-event window while nobody is connected, so this origin's
	// oldest retained event is no longer the one the reader last saw of it.
	for seq := range 4 {
		fixture.publish(t, stateOf(busy, seq+1))
	}

	resumed := fixture.open(t, resume)
	fixture.publish(t, stateOf(slow, 2))

	for _, frame := range resumed.collect() {
		require.NotEqual(t, ContinuityLost, frame.name,
			"a reader holding its own place was told its continuity was gone")
	}
}

// TestAWindowStampsTheWholePlaceToResumeFrom is what makes the reconnect above
// possible. One event's id names one origin, so a window that moved a reader on
// more than one ends with a frame carrying the whole cursor. It has no data
// member, so the HTML standard's dispatch step takes the id and delivers nothing.
func TestAWindowStampsTheWholePlaceToResumeFrom(t *testing.T) {
	t.Parallel()

	first := printerOf(t, "mk4-017")
	second := printerOf(t, "core-one-2")
	fixture := newEventsFixture(t, newGraph(first, second), stream.Options{})

	open := fixture.open(t, "")

	fixture.publish(t, stateOf(first, 1))
	fixture.publish(t, stateOf(second, 1))

	require.NotEmpty(t, open.nextEvent(t).name)
	require.NotEmpty(t, open.nextEvent(t).name)

	stamped := open.lastEventID()

	cursor, err := event.ParseCursor(stamped)
	require.NoError(t, err, "the stamped place is not a cursor this server can read back")
	require.Equal(t, 2, cursor.Places(),
		"the stamped place names %d origins, and the reader is following two", cursor.Places())
}

// TestASingleOriginStreamIsNotStampedTwice keeps the cursor frame off the panel
// that watches one printer: that reader's last event id already is its whole
// cursor, so a second frame saying the same thing is bytes on every window.
func TestASingleOriginStreamIsNotStampedTwice(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	open := fixture.open(t, "")
	fixture.publish(t, stateOf(printer, 1))

	require.NotEmpty(t, open.nextEvent(t).id)
	require.Empty(t, open.lastEventID(),
		"a one-origin stream was stamped with a cursor frame after its event, which said the same thing")
}

// TestTheStreamTellsTheReaderWhenToComeBack is the reconnection delay. Without
// it a browser waits whatever its own engine defaults to, which is three seconds
// in every major one and is not a number this deployment chose.
func TestTheStreamTellsTheReaderWhenToComeBack(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := fixtureOver(t, newGraph(printer), stream.Options{}, time.Hour,
		1500*time.Millisecond, httptest.NewServer)

	require.Equal(t, "1500", fixture.open(t, "").next(t).retry,
		"the stream did not tell its reader how long to wait before coming back")
}

// TestAStreamCanBeNarrowedToOneOrigin is what a panel watching one printer asks
// for. Filtering client-side costs the reader every other printer's frames on
// the socket, which is the whole fleet on a bench.
func TestAStreamCanBeNarrowedToOneOrigin(t *testing.T) {
	t.Parallel()

	wanted := printerOf(t, "mk4-017")
	other := printerOf(t, "core-one-2")
	fixture := newEventsFixture(t, newGraph(wanted, other), stream.Options{})

	open := fixture.openWith(t, "?origins="+wanted.String(), "")

	fixture.publish(t, stateOf(other, 1))
	fixture.publish(t, stateOf(wanted, 1))

	frames := open.collect()
	require.NotEmpty(t, frames, "the narrowed stream carried nothing at all")

	for _, frame := range frames {
		require.Equal(t, wanted.String(), envelopeOf(t, frame).Subject,
			"a printer the reader did not ask for reached its narrowed stream")
	}
}

// TestNarrowingToAnOriginTheCallerMayNotSeeIsRefused is the door beside the one
// the narrowing opens. Asking for a printer by name must not tell the caller
// whether it exists.
func TestNarrowingToAnOriginTheCallerMayNotSeeIsRefused(t *testing.T) {
	t.Parallel()

	mine := printerOf(t, "mk4-017")
	theirs := printerOf(t, "core-one-2")
	fixture := newEventsFixture(t, newGraph(mine), stream.Options{})

	status, body := fixture.get(t, "?origins="+theirs.String())
	require.Equal(t, http.StatusForbidden, status)
	require.NotContains(t, body, theirs.ID(), "the refusal named the printer the caller may not see")
}

// TestAMalformedOriginIsRefused keeps a client that asked for two printers from
// being quietly given one, which is a client watching a printer it does not know
// it is missing.
func TestAMalformedOriginIsRefused(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

	status, body := fixture.get(t, "?origins=mk4-017")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Contains(t, body, "query.origins")
}

// TestACallerMayAskForTheFastestWindow is an earlier design decision's floor made reachable. A
// bench reading a firmware quirk needs the frames, not a summary of them.
func TestACallerMayAskForTheFastestWindow(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{Interval: time.Second})

	open := fixture.openWith(t, "?interval_ms=100", "")
	fixture.publish(t, stateOf(printer, 1))

	require.Equal(t, printerState, open.nextEvent(t).name)
	require.Equal(t, stream.MinInterval, fixture.events.subscription().Interval(),
		"the window the caller asked for is not the one the hub gave it")
}

// TestAWindowOutsideTheBandIsRefused tells the caller the number rather than
// quietly serving a different one: a client that asked for 50 ms and was given
// 100 ms without being told is a client whose measurements are wrong.
func TestAWindowOutsideTheBandIsRefused(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"under the floor":  "?interval_ms=50",
		"over the ceiling": "?interval_ms=60000",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			printer := printerOf(t, "mk4-017")
			fixture := newEventsFixture(t, newGraph(printer), stream.Options{})

			status, _ := fixture.get(t, query)
			require.Equal(t, http.StatusUnprocessableEntity, status)
		})
	}
}

// TestAPrincipalIsHeldToItsStreamBound is the cap on what one caller can make
// this process hold. Every stream is a goroutine, two queues and a 32 KiB write
// buffer, so an uncounted stream is memory one credential can take at will.
func TestAPrincipalIsHeldToItsStreamBound(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "mk4-017")
	fixture := newEventsFixture(t, newGraph(printer), stream.Options{MaxPerPrincipal: 2})

	for range 2 {
		require.Equal(t, http.StatusOK, fixture.open(t, "").status)
	}

	require.Eventually(t, func() bool {
		status, _ := fixture.get(t, "")

		return status == http.StatusTooManyRequests
	}, settle, time.Millisecond, "a caller opened more streams than the server allows one")

	_, body := fixture.get(t, "")

	var problem fault.Problem

	require.NoError(t, json.Unmarshal([]byte(body), &problem))
	require.Equal(t, "core.validation/TOO_MANY_STREAMS", problem.Code.String())
	require.Contains(t, body, `"limit_streams"`, "the refusal does not tell the caller what the bound is")
}

// TestThePublishedWindowBoundsAreTheHubs keeps the schema a client generates
// from in step with the band the hub enforces. Two numbers that must agree and
// are written in two places is how they stop agreeing.
func TestThePublishedWindowBoundsAreTheHubs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	var document struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name   string `json:"name"`
					Schema struct {
						Minimum *float64 `json:"minimum"`
						Maximum *float64 `json:"maximum"`
					} `json:"schema"`
				} `json:"parameters"`
			} `json:"get"`
		} `json:"paths"`
	}

	require.NoError(t, json.Unmarshal(renderedDocument(t, server), &document))

	var published *float64

	for _, parameter := range document.Paths[EventsPath].Get.Parameters {
		if parameter.Name == "interval_ms" {
			require.NotNil(t, parameter.Schema.Minimum, "the window parameter publishes no floor")
			require.NotNil(t, parameter.Schema.Maximum, "the window parameter publishes no ceiling")
			require.InDelta(t, float64(stream.MinInterval.Milliseconds()), *parameter.Schema.Minimum, 0)
			require.InDelta(t, float64(stream.MaxInterval.Milliseconds()), *parameter.Schema.Maximum, 0)

			published = parameter.Schema.Minimum
		}
	}

	require.NotNil(t, published, "the events operation publishes no window parameter")
}

// TestStampingTheCursorAllocatesNothingPerWindow is the hot-path rule applied to
// the frame that closes the fleet-wide reconnect. It is written per subscriber
// per window, so a fresh buffer there is an allocation that scales with the
// subscriber count, which is the one thing the fan-out path may not do.
// It does not run in parallel, because AllocsPerRun measures the whole process.
func TestStampingTheCursorAllocatesNothingPerWindow(t *testing.T) {
	batch := windowAcross(t, 4)
	require.Equal(t, 2, batch.Cursor.Places(), "this window would not be stamped at all")

	out := bufio.NewWriterSize(io.Discard, eventStreamBuffer)
	control := http.NewResponseController(liveWriter{})
	reader := &eventStream{log: discardLogger()}

	allocs := testing.AllocsPerRun(100, func() {
		_ = reader.writeWindow(out, control, batch)
	})

	require.Zero(t, allocs, "stamping one window's cursor allocated %v times per reader", allocs)
}

// windowAcross builds one coalescing window carrying two origins, which is the
// shape that needs the cursor stamped: one event's id names one of them.
func windowAcross(t testing.TB, n int) stream.Batch {
	t.Helper()

	first, second := printerOf(t, "mk4-017"), printerOf(t, "core-one-2")

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Authorizer: newGraph(first, second),
		Interval:   stream.MinInterval,
	})
	require.NoError(t, err)

	defer hub.Close()

	sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{
		Principal: operator,
		Encoding:  stream.EncodingSSE,
	})
	require.NoError(t, err)

	defer sub.Close()

	for i := range n {
		for _, printer := range []event.Origin{first, second} {
			_, err = hub.Publish(stateOf(printer, i+1))
			require.NoError(t, err)
		}
	}

	var batch stream.Batch

	deadline := time.After(settle)

	for len(batch.Events) < 2*n {
		select {
		case next, open := <-sub.Batches():
			require.True(t, open, "the subscription closed before the window was full")

			batch.Events = append(batch.Events, next.Events...)
			batch.Cursor = next.Cursor
		case <-deadline:
			t.Fatal("the published events never reached the subscription")
		}
	}

	return batch
}

// BenchmarkWriteWindow is what stamping the cursor costs the path a published
// frame takes to a subscriber's socket. Both cases write the same eight frames
// and differ only in how many origins the window's cursor names, so the
// difference is the whole price of closing the fleet-wide reconnect.
func BenchmarkWriteWindow(b *testing.B) {
	stamped := windowAcross(b, 4)

	// The same window as a reader following one origin sees it, whose last event
	// id is already its whole cursor.
	single := stamped
	single.Cursor = event.NewCursor(event.NewGeneration(
		stamped.Cursor.Generations[0].Epoch,
		stamped.Cursor.Generations[0].Positions[:1],
	))

	control := http.NewResponseController(liveWriter{})

	for name, batch := range map[string]stream.Batch{
		"one origin, not stamped": single,
		"two origins, stamped":    stamped,
	} {
		b.Run(name, func(b *testing.B) {
			out := bufio.NewWriterSize(io.Discard, eventStreamBuffer)
			reader := &eventStream{log: discardLogger()}

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				_ = reader.writeWindow(out, control, batch)
			}
		})
	}
}

// revocable is a Principals whose credential a case can take away, which is
// what a revocation, a logout and an expiry all look like from here: the
// resolver that admitted the connection stops admitting it.
type revocable struct {
	live  atomic.Bool
	asked atomic.Int64
}

func newRevocable() *revocable {
	p := &revocable{}
	p.live.Store(true)

	return p
}

func (p *revocable) Principal(*http.Request) (string, error) {
	p.asked.Add(1)

	if !p.live.Load() {
		return "", fault.AuthTokenInvalid.New("the credential is not one this server accepts")
	}

	return operator, nil
}

// revoke takes the credential away, so every resolution after it refuses.
func (p *revocable) revoke() { p.live.Store(false) }

// revocableFixture is the fixture with a credential a case can withdraw while a
// stream is open.
func revocableFixture(
	t *testing.T, graph *movableGraph, heartbeat time.Duration,
) (*eventsFixture, *revocable) {
	t.Helper()

	who := newRevocable()

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: graph, Interval: stream.MinInterval})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	events := &watchedHub{hub: hub}

	cfg := eventsConfig(events, heartbeat, defaultRetry)
	cfg.Principals = who

	server, err := New(cfg)
	require.NoError(t, err)

	listener := httptest.NewServer(server.Handler())
	t.Cleanup(listener.Close)

	return &eventsFixture{events: events, graph: graph, url: listener.URL + EventsPath}, who
}

// TestARevokedCredentialEndsAnOpenStream is the credential half of what reaches
// a live stream.
//
// The authorization half already works: the hub carries a generation and a
// withdrawn grant narrows a subscription that is already open. This is the other
// half, and it is the one a reader can hold open for the whole token lifetime:
// the identity behind a stream is resolved when it opens, so without this a
// token revoked a minute later keeps reading until the client hangs up.
func TestARevokedCredentialEndsAnOpenStream(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "PRN-1")
	fixture, who := revocableFixture(t, newGraph(printer), 50*time.Millisecond)

	open := fixture.open(t, "")

	fixture.publish(t, stateOf(printer, 1))
	require.Equal(t, printerState, open.nextEvent(t).name,
		"the stream was not carrying events before the credential was taken away")

	who.revoke()

	// Published after the revocation and before the stream has had time to
	// notice, which is the frame a reader must not be given.
	fixture.publish(t, stateOf(printer, 2))

	var expired bool

	for {
		select {
		case frame, live := <-open.frames:
			if !live {
				require.True(t, expired,
					"the stream ended without telling the reader the credential had gone")

				return
			}

			if frame.name == CredentialExpired {
				expired = true

				continue
			}

			require.NotEqual(t, printerState, frame.name,
				"an event reached a reader whose credential had been revoked")
		case <-time.After(settle):
			t.Fatal("the stream was still open after the credential was revoked")
		}
	}
}

// TestARevokedCredentialEndsAStreamThatIsBusy is the case a check riding the
// keep-alive cannot answer.
//
// The heartbeat is reset by every window that leaves, so a stream with events on
// it never reaches a second keep-alive tick. A printer reporting telemetry is
// exactly that stream, and it is the one a revoked token would otherwise keep
// reading for the whole token lifetime.
func TestARevokedCredentialEndsAStreamThatIsBusy(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "PRN-1")

	// The keep-alive is three windows long and a window leaves every one of
	// them, so the heartbeat ticker is reset before it can ever fire. That is
	// the condition, and it is an ordinary printer reporting telemetry.
	const heartbeat = 3 * stream.MinInterval

	fixture, who := revocableFixture(t, newGraph(printer), heartbeat)

	open := fixture.open(t, "")

	publishing, stop := context.WithCancel(t.Context())
	defer stop()

	go func() {
		for seq := 1; publishing.Err() == nil; seq++ {
			_, _ = fixture.events.hub.Publish(stateOf(printer, seq))
			time.Sleep(stream.MinInterval / 5)
		}
	}()

	require.Equal(t, printerState, open.nextEvent(t).name,
		"the stream was not carrying events before the credential was taken away")

	who.revoke()

	deadline := time.After(settle)

	for {
		select {
		case frame, live := <-open.frames:
			if !live {
				t.Fatal("the stream closed without telling the reader the credential had gone")
			}

			if frame.name == CredentialExpired {
				t.Logf("observed: a stream taking a window every %s, with a %s keep-alive, still "+
					"noticed the revocation", stream.MinInterval, heartbeat)

				return
			}
		case <-deadline:
			t.Fatal("a stream with events on it never re-checked its credential, so the keep-alive " +
				"was reset before the check could run")
		}
	}
}

// TestALiveCredentialKeepsItsStream is the other direction. A check that closed
// every stream would pass the revocation cases and be worthless.
func TestALiveCredentialKeepsItsStream(t *testing.T) {
	t.Parallel()

	printer := printerOf(t, "PRN-1")
	fixture, who := revocableFixture(t, newGraph(printer), 20*time.Millisecond)

	open := fixture.open(t, "")

	fixture.publish(t, stateOf(printer, 1))
	require.Equal(t, printerState, open.nextEvent(t).name)

	// Long enough for many re-checks at a 20 ms cadence.
	time.Sleep(10 * 20 * time.Millisecond)

	fixture.publish(t, stateOf(printer, 2))

	for {
		frame := open.next(t)
		if frame.name == "" {
			continue
		}

		require.NotEqual(t, CredentialExpired, frame.name,
			"a credential that was never revoked closed its own stream")
		require.Equal(t, printerState, frame.name)

		break
	}

	require.Greater(t, who.asked.Load(), int64(1),
		"the credential was resolved once and never again, so nothing was re-checked")

	t.Logf("observed: the credential was resolved %d times and the stream stayed open",
		who.asked.Load())
}

// TestTheRevalidationCadenceHasACeiling is the rule that keeps a liveness key
// from widening a security window.
//
// Heartbeat is configurable (core.events.heartbeat) and the re-check cadence is
// not: a deployment that set its keep-alive to five minutes must not thereby
// give a revoked token five minutes of reading.
func TestTheRevalidationCadenceHasACeiling(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		heartbeat time.Duration
		want      time.Duration
	}{
		"the default keep-alive carries the check": {heartbeat: 15 * time.Second, want: 15 * time.Second},
		"a keep-alive above the ceiling is capped": {heartbeat: 5 * time.Minute, want: 30 * time.Second},
		"a keep-alive at the ceiling is the ceiling": {
			heartbeat: 30 * time.Second, want: 30 * time.Second,
		},
		"a keep-alive below it is taken as it is": {
			heartbeat: 100 * time.Millisecond, want: 100 * time.Millisecond,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, revalidateEvery(tc.heartbeat))
		})
	}
}

// TestTheReaderCountFollowsTheStreamsThatAreOpen is the whole claim
// /api/v1/events/stats makes. The number is the hub's own count of live
// subscriptions, so it rises when a reader connects and falls when one goes
// away, and nothing in this transport keeps a second tally that could disagree.
func TestTheReaderCountFollowsTheStreamsThatAreOpen(t *testing.T) {
	t.Parallel()

	fixture := newEventsFixture(t, newGraph(printerOf(t, "mk4-1")), stream.Options{})

	require.Equal(t, 0, fixture.subscribers(t), "nobody has opened a stream yet")

	first := fixture.open(t, "")
	require.Equal(t, http.StatusOK, first.status)
	require.Equal(t, 1, fixture.subscribers(t))

	second := fixture.open(t, "")
	require.Equal(t, http.StatusOK, second.status)
	require.Equal(t, 2, fixture.subscribers(t))

	second.close()

	// The count falls when the handler behind that connection returns, which is
	// the client's cancellation reaching the server rather than anything this
	// process can observe directly.
	require.Eventually(t, func() bool { return fixture.subscribers(t) == 1 }, settle, quiet,
		"a reader that went away is still counted")

	first.close()

	require.Eventually(t, func() bool { return fixture.subscribers(t) == 0 }, settle, quiet,
		"every reader has gone and the count did not follow")
}

// TestTheReaderCountIsRefusedWithoutACredential holds the one refusal this
// operation has. It carries no permission of its own, so an identity is the
// whole of the check and a request with none must not be answered.
func TestTheReaderCountIsRefusedWithoutACredential(t *testing.T) {
	t.Parallel()

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: newGraph(), Interval: stream.MinInterval})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	cfg := eventsConfig(&watchedHub{hub: hub}, time.Hour, defaultRetry)
	cfg.Principals = fixedPrincipal("")

	server, err := New(cfg)
	require.NoError(t, err)

	listener := httptest.NewServer(server.Handler())
	t.Cleanup(listener.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, listener.URL+eventStatsPath, nil)
	require.NoError(t, err)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { require.NoError(t, response.Body.Close()) }()

	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

// Caller is Principal with no scope.
func (p *revocable) Caller(r *http.Request) (Caller, error) {
	return Unscoped(p.Principal(r))
}
