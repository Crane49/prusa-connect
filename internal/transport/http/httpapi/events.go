package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// EventsPath is the browser read path, and the only way anything outside this
// process reads the hub.
const EventsPath = "/api/v1/events"

// eventStatsPath answers how many readers the hub serves. It is a route beside
// the stream, so a caller learns how many streams are open without opening
// one.
const eventStatsPath = "/api/v1/events/stats"

// StreamPaths are the request paths this server serves an event stream on.
//
// Which route is a stream decides which credentials are accepted on it, and it
// is decided here, where the routes are. It is exported because the package
// that resolves a caller's identity may not import this one, so the
// composition root forwards this list to it. Reading a header instead would
// let the caller choose which rule they are judged by.
func StreamPaths() []string { return []string{EventsPath} }

// ContinuityLost is the type of the one frame this transport authors itself. It
// says that the reader's place in the log is gone. Either its queue overflowed
// or the cursor it resumed from cannot be replayed. The reader reloads state
// from the API and carries on from the cursor the frame's id names. There is
// no partial recovery.
const ContinuityLost = event.TypeStreamContinuityLost

// OriginWithdrawn is the type of the frame that names an origin this reader
// followed and may no longer see. It is the hub's decision, written here once
// per origin with the window's cursor as its id.
const OriginWithdrawn = event.TypeStreamOriginWithdrawn

// CredentialExpired is the type of the other frame this transport authors. It
// says that the credential which opened the stream has stopped being one this
// server accepts. A reader then knows it was logged out and not disconnected,
// and does not reconnect on the same token.
//
// It is outside the core event namespace because it is not an event the hub
// published: nothing else can observe it, it belongs to one connection, and it
// carries no origin.
//
//nolint:gosec // G101 reads this as a credential; it is the name of an event type.
const CredentialExpired = "prusa-connect.auth.expired"

const (
	// eventStreamBuffer is one subscriber's outbound buffer. A coalescing
	// window leaves in one write while it fits inside this, so the number of
	// write syscalls follows the windows and not the event count. The size is
	// fixed, so a reader that stops reading cannot make the host allocate. The
	// hub's bounded queues drop instead.
	eventStreamBuffer = 32 << 10

	// defaultHeartbeat is how often an idle stream writes a comment frame. It
	// is under the 30 to 60 seconds an idle-connection reaper usually allows,
	// and a fleet of idle readers costs one small write each.
	defaultHeartbeat = 15 * time.Second

	// defaultRetry is the reconnection delay a reader is given. It is under the
	// three seconds every major browser engine defaults to. This server is on
	// the same network as its readers, and a shorter gap gives a better chance
	// of resuming inside the replay window without a resync.
	defaultRetry = 2 * time.Second

	// revalidateCeiling is the longest a stream may go without re-checking the
	// credential that opened it. Heartbeat is a configuration key, and a
	// deployment that makes its keep-alive less frequent must not widen the
	// window in which a revoked token keeps reading.
	revalidateCeiling = 30 * time.Second
)

// EventStream is the hub as this transport reads it: one authorization at
// subscribe, and a coalesced batch per window afterwards.
//
// It is declared here because this is where it is consumed. Nothing in it
// belongs to HTTP: the composition root satisfies it with the running hub.
type EventStream interface {
	// Subscribe authorizes the principal once and attaches them to every origin
	// they may see. The caller closes the subscription it is handed.
	Subscribe(ctx context.Context, req stream.SubscribeRequest) (*stream.Subscription, error)

	// Subscribers is how many subscriptions the hub is serving, over every
	// reader and every encoding. It is a process-wide count and names nobody,
	// so it discloses no principal and no origin.
	Subscribers() int
}

// EventStreamStats is the reader count of the hub.
//
// It has one member. Anything else worth counting here is per origin or per
// principal, and the stream itself withholds both.
type EventStreamStats struct {
	Subscribers int `json:"subscribers" doc:"How many event streams this server is serving right now, over every reader. It counts connections and not people: one browser with three tabs open is three."`
}

type eventStatsOutput struct {
	Body EventStreamStats
}

// eventsInput is what a reader asks for. Last-Event-ID is the browser's own:
// EventSource replays the id of the last frame it saw, with no client code
// involved. The query parameters are what a client cannot express any other
// way, because EventSource sets no request header.
//
// The credential is not a query parameter. An api credential opens this stream
// in the same Authorization: Bearer header it opens every other operation
// with, resolved by the same Principals. A browser's EventSource cannot set
// that header, so the browser reader uses the session cookie, which
// EventSource does send. A client SDK reads the stream through fetch, which
// sets the header. A token in the URL would land in every access log, proxy
// log and referrer between the client and this process. A reader that has
// neither a cookie nor fetch has no way in, and that is intended.
type eventsInput struct {
	LastEventID string `header:"Last-Event-ID" doc:"The id of the last frame this client saw, replayed by EventSource on its own. It is either one event's place, as epoch:kind/id:offset, or the whole cursor this server stamps on a window that moves it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Origins []string `query:"origins" maxItems:"512" doc:"Narrow the stream to these origins, each as kind:id, comma separated. Omitted means every origin the caller may observe. An origin the caller may not observe refuses the whole request, and the refusal names none of them." example:"device:9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Types []string `query:"types" maxItems:"64" doc:"Narrow the stream to these event types, comma separated, each the envelope's own type such as com.prusa.connect.device.arrived. Omitted means every type this caller may see. A type this build never publishes is not refused: it matches nothing, so a client may name one a later release adds. This narrows what is sent and never what may be seen, which is what origins does." example:"com.prusa.connect.device.presence.changed"`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	IntervalMS int `query:"interval_ms" minimum:"100" maximum:"10000" doc:"The coalescing window in milliseconds: everything published inside one leaves as one write. Omitted means the server's default. The floor is the shortest window the server allows." example:"500"`
}

// continuity is the payload of the continuity-lost frame. Cursor is also the
// frame's id, so a browser that reconnects on its own resumes from it without
// the page having read the body.
type continuity struct {
	Cursor  string `json:"cursor"`
	Dropped uint64 `json:"dropped"`
}

// withdrawal is the payload of the origin-withdrawn frame.
type withdrawal struct {
	Origin string `json:"origin"`
}

// eventStream is one reader's connection. It holds no queue of its own: it
// takes a window off the subscription and writes it, so a reader that stops
// draining stalls here and the hub's bounded queues do the dropping. A second
// buffer here would be a second drop policy that nobody sees.
type eventStream struct {
	sub       *stream.Subscription
	heartbeat time.Duration
	retry     time.Duration
	closing   <-chan struct{}
	log       *slog.Logger
	principal string
	// identify re-resolves the credential this connection was opened with. It
	// is the same resolver that admitted it, closed over the same request, so
	// there is one copy of what this server accepts.
	identify func() (string, error)
	// revalidate is how often identify is called. See revalidateCeiling.
	revalidate time.Duration
	// scratch renders this reader's cursor frame. One buffer for the life of the
	// connection, because the frame is written on every window that moves the
	// cursor and a fresh slice there is an allocation per subscriber per window.
	scratch []byte
}

func registerEvents(api huma.API, cfg Config, closing <-chan struct{}) {
	registerEventStats(api, cfg.Events)

	register(api, huma.Operation{
		OperationID: "stream-events",
		Method:      http.MethodGet,
		Path:        EventsPath,
		Summary:     "Read the event stream",
		Description: "Opens a server-sent event stream of everything the caller may observe. The " +
			"authorization decision is taken once, here, and channel membership is the decision " +
			"afterwards, so no event costs a permission check. A grant withdrawn while " +
			"the stream is open narrows it without a reconnect and names each origin the reader lost in a " +
			OriginWithdrawn + " frame, and a credential that stops being " +
			"accepted, by revocation or by expiry, ends the stream with a " + CredentialExpired +
			" frame within 30 seconds. Frames are coalesced into windows " +
			"rather than forwarded one per event, and a reader that falls far enough " +
			"behind is told so by a " + ContinuityLost + " frame rather than quietly given part of a " +
			"stream.",
		Tags:      []string{"events"},
		Errors:    []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests},
		Responses: eventStreamResponse(),
		Metadata:  streams(),
	}, func(ctx context.Context, in *eventsInput) (*huma.StreamResponse, error) {
		principal := principalFrom(ctx)
		if principal == "" {
			return nil, fault.AuthTokenInvalid.New("the request carries no usable identity")
		}

		// The stream outlives the decision that opened it, so it keeps the
		// request in order to ask again. A composition that cannot produce one
		// is refused, because nothing could re-check its stream.
		request := requestFrom(ctx)
		if request == nil {
			return nil, fault.AuthTokenInvalid.New("the request carries no usable identity")
		}

		resume, err := resumeFrom(in.LastEventID)
		if err != nil {
			return nil, err
		}

		origins, err := originsFrom(in.Origins)
		if err != nil {
			return nil, err
		}

		types, err := typesFrom(in.Types)
		if err != nil {
			return nil, err
		}

		sub, err := cfg.Events.Subscribe(ctx, stream.SubscribeRequest{
			Principal: principal,
			Origins:   origins,
			Types:     types,
			Resume:    resume,
			Encoding:  stream.EncodingSSE,
			Interval:  time.Duration(in.IntervalMS) * time.Millisecond,
		})
		if err != nil {
			return nil, subscribeFault(err)
		}

		reader := &eventStream{
			sub:        sub,
			heartbeat:  cfg.Heartbeat,
			retry:      cfg.Retry,
			closing:    closing,
			log:        cfg.Logger,
			principal:  principal,
			identify:   func() (string, error) { return cfg.Principals.Principal(request) },
			revalidate: revalidateEvery(cfg.Heartbeat),
		}

		return &huma.StreamResponse{Body: reader.run}, nil
	})
}

// registerEventStats publishes the reader count.
//
// It carries the ordinary identity requirement and no permission of its own.
// The answer is one integer over the whole process: it names no printer, no
// principal and no origin, so there is no object for the graph to decide about
// and nothing here that the fleet listing withholds.
//
// get-metrics takes a fleet-wide grant and this route does not. The difference
// is the payload and not the route. That exposition names every device this
// process has seen, so identity alone would let any account that can log in
// read the fleet through it. This answer names nothing. What decides is
// whether the answer names an object the graph can decide about.
func registerEventStats(api huma.API, events EventStream) {
	register(api, huma.Operation{
		OperationID: "get-event-stream-stats",
		Method:      http.MethodGet,
		Path:        eventStatsPath,
		Summary:     "How many readers the event stream has",
		Description: "Counts the event streams this server is serving right now, over every reader. " +
			"It is a process-wide number: it names no printer and no caller, and it is the same " +
			"answer whatever the caller may see. Use it to tell a server nobody is watching from " +
			"one under load before opening a stream of your own.\n" +
			"\n" +
			"It counts connections rather than people. One browser with three tabs open is three, " +
			"and a reader that has gone away is counted until this server notices the socket " +
			"closed.",
		Tags:   []string{"events"},
		Errors: []int{http.StatusUnauthorized},
	}, func(_ context.Context, _ *struct{}) (*eventStatsOutput, error) {
		return &eventStatsOutput{Body: EventStreamStats{Subscribers: events.Subscribers()}}, nil
	})
}

// revalidateEvery is how often one stream re-checks its credential. A
// heartbeat shorter than the ceiling sets the interval, and a longer one does
// not raise it above the ceiling. The interval is a security property, and
// Heartbeat is a liveness key an operator sets for an intermediary.
func revalidateEvery(heartbeat time.Duration) time.Duration {
	return min(heartbeat, revalidateCeiling)
}

// originsFrom reads the narrowing a caller asked for. A name this server could
// never have issued is refused and not dropped. A client that asked for two
// printers and was silently given one would not know it is missing the other.
func originsFrom(requested []string) ([]event.Origin, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	origins := make([]event.Origin, 0, len(requested))

	for _, name := range requested {
		origin, err := event.ParseOrigin(name)
		if err != nil {
			return nil, httpfault.RequestInvalid("query.origins", "it is not an origin, as kind:id", err.Error())
		}

		origins = append(origins, origin)
	}

	return origins, nil
}

// typesFrom validates the reader's type narrowing. Only the syntax is checked,
// and never membership of this build's vocabulary.
//
// A client may name a type a later release adds. If membership were checked,
// an older server would refuse that client's whole stream. An unknown but
// well-formed type matches nothing, and a reader that asked only for that gets
// an empty stream.
func typesFrom(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	for _, name := range requested {
		if err := event.ValidateTypeSyntax(name); err != nil {
			return nil, httpfault.RequestInvalid("query.types", "it is not an event type", err.Error())
		}
	}

	return requested, nil
}

// eventStreamResponse documents the body, which Huma cannot derive: a streaming
// operation's Body field is a function, so there is no struct to reflect over.
func eventStreamResponse() map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: "A server-sent event stream. Each frame carries the CloudEvents JSON envelope " +
				"in its data field, the envelope's type in its event field, and the place to resume " +
				"from in its id field. Telemetry frames carry no id: they are conflated rather than " +
				"replayed, so there is no place in them to resume from.",
			Content: map[string]*huma.MediaType{
				"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		},
	}
}

// resumeFrom reads the place a reconnecting reader left off at. Both forms this
// server issues are accepted: one event's id, which is what a browser replays,
// and the whole cursor, which is what a continuity-lost frame carries so a
// reader following several origins can resume all of them.
//
// Text this server never issued is refused. Starting such a reader live would
// answer it as if it had resumed, and it would miss the events it reconnected
// to collect.
func resumeFrom(lastEventID string) (event.Cursor, error) {
	if lastEventID == "" {
		return event.Cursor{}, nil
	}

	if cursor, err := event.ParseID(lastEventID); err == nil {
		return cursor, nil
	}

	cursor, err := event.ParseCursor(lastEventID)
	if err != nil {
		return event.Cursor{}, httpfault.RequestInvalid("header.Last-Event-ID",
			"it is neither an event id this server issued nor a cursor", err.Error())
	}

	return cursor, nil
}

// subscribeFault renders the hub's refusal. A narrowing that named an origin
// the caller may not observe is answered as access denied, and the refusal
// names none of the origins, so it does not disclose which printers exist. A
// caller over the stream limit and a request the hub finds invalid get their
// own refusals. A caller who named nothing is given a stream of what they may
// see, which for a caller who may see nothing is an empty one.
func subscribeFault(err error) error {
	var limit stream.LimitError

	switch {
	case errors.As(err, &limit):
		return httpfault.TooManyStreams(limit.Limit)
	case errors.Is(err, stream.ErrNotPermitted):
		return fault.New(authz.CodeAccessDenied(),
			"this request named an origin the caller may not observe").With(
			fault.StringParam("object", "events"),
			fault.StringParam("action", "subscribe"),
		)
	case errors.Is(err, stream.ErrInvalidRequest):
		return httpfault.RequestInvalid("header.Last-Event-ID", "the hub refused the subscription", err.Error())
	default:
		return httpfault.Unexpected("open an event stream").Wrap(err)
	}
}

// run writes the stream until the client goes away, the listener drains, or the
// subscription ends underneath it. Every one of those ends the same way: the
// handler returns, so the connection stops being in flight and a shutdown that
// is waiting on it stops waiting.
func (s *eventStream) run(hctx huma.Context) {
	defer s.sub.Close()

	_, w := humachi.Unwrap(hctx)
	control := http.NewResponseController(w)

	// The server's whole-response write timeout would end a stream that is
	// working correctly. A deadline per write replaces it, set in flush, and
	// that deadline bounds a reader that has stopped draining.
	s.clearDeadline(control)

	hctx.SetHeader("Content-Type", "text/event-stream")
	hctx.SetHeader("Cache-Control", "no-cache")
	// nginx buffers a proxied response by default, which holds every frame back
	// until the buffer fills or the stream ends.
	hctx.SetHeader("X-Accel-Buffering", "no")
	hctx.SetStatus(http.StatusOK)

	if err := control.Flush(); err != nil {
		return
	}

	// A HEAD asked what this operation answers with, not for the stream itself.
	// net/http discards the body of one, so writing a stream into it would hold
	// a connection open for a probe that has already read everything it wanted.
	if headOf(hctx.Context()) {
		return
	}

	out := bufio.NewWriterSize(w, eventStreamBuffer)

	// The retry delay goes out before any event, so a connection that drops on
	// its first window still reconnects on this server's delay.
	if err := s.write(out, control, stream.AppendSSERetry(nil, s.retry)); err != nil {
		return
	}

	s.pump(hctx.Context(), out, control)
}

func (s *eventStream) pump(ctx context.Context, out *bufio.Writer, control *http.ResponseController) {
	idle := time.NewTicker(s.heartbeat)
	defer idle.Stop()

	// The re-check has its own ticker, because every window that leaves resets
	// the heartbeat. On the heartbeat's ticker a stream with events on it would
	// never reach a tick, and a revoked token would keep reading a printer that
	// reports telemetry.
	recheck := time.NewTicker(s.revalidate)
	defer recheck.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closing:
			return
		case <-recheck.C:
			if !s.stillAdmitted(ctx, out, control) {
				return
			}
		case <-idle.C:
			if err := s.write(out, control, []byte(stream.SSEHeartbeat)); err != nil {
				return
			}
		case batch, open := <-s.sub.Batches():
			if !open {
				return
			}

			if err := s.writeWindow(out, control, batch); err != nil {
				s.log.DebugContext(ctx, "event stream ended on a write",
					"principal", s.principal, "error", err.Error())

				return
			}

			// The heartbeat is for an idle stream, and a window just left.
			idle.Reset(s.heartbeat)
		}
	}
}

// stillAdmitted re-checks the credential this stream was opened with, and ends
// the stream when it has stopped being one this server accepts.
//
// It is credential re-validation and not authorization: a grant withdrawn while
// the stream is open narrows it through the hub's own generation and never
// reaches here. What reaches here is a token revoked, expired, or slid past its
// idle window, which the decision at subscribe cannot see because it was taken
// before any of them happened.
//
// The check calls the resolver that admitted the connection, so a credential
// is refused here in the same words and on the same evidence as at a fresh
// request. An identity that resolves to a different
// subject ends the stream too: the authorization behind it was decided for the
// subject that opened it.
func (s *eventStream) stillAdmitted(
	ctx context.Context, out *bufio.Writer, control *http.ResponseController,
) bool {
	principal, err := s.identify()
	if err == nil && principal == s.principal {
		return true
	}

	// A reader that went away fails the check for a reason that is not the
	// credential, and there is nobody left to tell.
	if ctx.Err() != nil {
		return false
	}

	s.log.InfoContext(ctx, "an event stream ended because its credential is no longer accepted",
		"principal", s.principal)

	// The response is over either way. A failure to write the last frame leaves
	// the reader with a closed connection, which is the outcome it describes.
	_ = s.write(out, control, stream.SSEControlFrame("", CredentialExpired, []byte("{}")))

	return false
}

// writeWindow writes one coalescing window. Every frame in it is the hub's own
// bytes, shared with every other subscriber on this encoding. They are written
// as they are and not encoded again, so a thousand readers cost one marshal.
func (s *eventStream) writeWindow(out *bufio.Writer, control *http.ResponseController, batch stream.Batch) error {
	// bufio keeps the first write error and reports it from Flush, so the
	// per-frame results are the same error read twice.
	for _, frame := range batch.Telemetry {
		_, _ = out.Write(frame.Bytes)
	}

	for _, frame := range batch.Events {
		_, _ = out.Write(frame.Bytes)
	}

	s.writeWithdrawn(out, batch)

	if batch.Resync {
		s.writeContinuityLost(out, batch.Cursor)

		return s.flush(out, control)
	}

	s.writeCursor(out, batch)

	return s.flush(out, control)
}

// writeCursor stamps the whole place to resume from after the window's events,
// so it is the last id the reader saw and the one a browser replays.
//
// It is written only when the cursor names more than one origin, because a
// cursor naming one is already what that origin's last event id says, and only
// when the window carried a durable event, because nothing else moves the id
// the reader holds.
func (s *eventStream) writeCursor(out *bufio.Writer, batch stream.Batch) {
	if len(batch.Events) == 0 || batch.Cursor.Places() < 2 {
		return
	}

	s.scratch = stream.AppendSSECursor(s.scratch[:0], batch.Cursor)
	_, _ = out.Write(s.scratch)
}

// writeWithdrawn names each origin the reader lost, after the window's events
// and before its cursor.
func (s *eventStream) writeWithdrawn(out *bufio.Writer, batch stream.Batch) {
	for _, origin := range batch.Withdrawn {
		payload, err := json.Marshal(withdrawal{Origin: origin.String()})
		if err != nil {
			s.log.Error("an origin-withdrawn frame could not be encoded",
				"principal", s.principal, "error", err.Error())

			continue
		}

		_, _ = out.Write(stream.SSEControlFrame(batch.Cursor.String(), OriginWithdrawn, payload))
	}
}

// writeContinuityLost tells the reader its place is gone and where to carry on
// from. The cursor is the frame's id as well as its payload, so a browser that
// reconnects on its own resumes from it with no page code involved.
func (s *eventStream) writeContinuityLost(out *bufio.Writer, cursor event.Cursor) {
	payload, err := json.Marshal(continuity{Cursor: cursor.String(), Dropped: s.sub.Stats().EventDropped})
	if err != nil {
		s.log.Error("a continuity-lost frame could not be encoded",
			"principal", s.principal, "error", err.Error())

		return
	}

	_, _ = out.Write(stream.SSEControlFrame(cursor.String(), ContinuityLost, payload))
}

// write puts one server-authored frame on the wire: the retry delay, the
// heartbeat or the credential-expired frame. None carries anything the hub
// published.
func (s *eventStream) write(out *bufio.Writer, control *http.ResponseController, frame []byte) error {
	_, _ = out.Write(frame)

	return s.flush(out, control)
}

// flush bounds the write before making it. Without the deadline a reader that
// has stopped draining fills the socket buffer and blocks here forever.
func (s *eventStream) flush(out *bufio.Writer, control *http.ResponseController) error {
	if err := control.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("bound the write to a stalled reader: %w", err)
	}

	if err := out.Flush(); err != nil {
		return fmt.Errorf("write the window: %w", err)
	}

	if err := control.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("flush the window: %w", err)
	}

	return nil
}

// clearDeadline lifts the server's whole-response write timeout for this
// connection, because this response never ends.
func (s *eventStream) clearDeadline(control *http.ResponseController) {
	if err := control.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.log.Warn("an event stream keeps the server's write timeout",
			"principal", s.principal, "error", err.Error())
	}
}
