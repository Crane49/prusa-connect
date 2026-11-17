// Package routes is the HTTP surface of this module.
//
// A module opens no HTTP port. The server receives the request, works out who
// is calling, and decides whether that person may call this module. It takes
// view_resource for a read and operate_resource for a write. Only then does it
// hand the request over. The setup route sends a G-code to a printer as the
// person who called it, so the server checks that person's right to send raw
// G-code to that printer as well.
//
// A route that reads records asks the server which printers the caller may
// see, as that caller, and answers only their records. The records of a MAC
// matched to no printer go only to a person who may see every printer the
// module reads, and so does a purge, which removes the records of every
// printer.
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
	"github.com/prusa3d/buddy-debug-logs/internal/query"
	"github.com/prusa3d/buddy-debug-logs/internal/receiver"
	"github.com/prusa3d/buddy-debug-logs/internal/settings"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// MaxBodyBytes is the largest request body a handler reads. Every body here
// is a few short fields.
const MaxBodyBytes = 16 << 10

// maxHostBytes is the longest host the printer takes: its metrics_host
// setting holds 20 characters (config_store metrics_host_size).
const maxHostBytes = 20

// sendGCode is the verb a raw G-code line travels as.
const sendGCode = "core.command.send_gcode"

// hostPattern is a host the printer can resolve: an address or a name of
// letters, digits, dots and hyphens.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

// Devices is the part of the host's device door the routes use.
type Devices interface {
	sources.Fleet
	Command(ctx context.Context, params deviceapi.CommandParams) (deviceapi.Result, error)
}

// State is what the module holds once the host sent its settings.
type State struct {
	// Ready is false until plugin.configure arrived.
	Ready bool
	// Book is the kept records. Its Store is nil for a module granted no
	// data directory.
	Book     query.Book
	Receiver *receiver.Receiver
	Settings settings.Values
	// Ports is the port the operator granted each stream, and absent for a
	// stream granted none.
	Ports map[store.Stream]int
	// Problem is why the module keeps nothing, when it keeps nothing.
	Problem string
}

// Service is what the routes read.
type Service struct {
	State   func() State
	Devices Devices

	// seen keeps what each person may see for a few seconds, so a page that
	// polls does not ask the host for the fleet every time. New sets it.
	seen *visibleCache
}

// New is the whole HTTP surface of this module. Every path is relative to the
// mount the host assigned.
func New(s Service) http.Handler {
	mux := http.NewServeMux()

	s.seen = newVisibleCache(time.Now)

	mux.HandleFunc("GET /status", s.acting(s.status, false))
	mux.HandleFunc("GET /sources", s.acting(s.reading(s.listSources), true))
	mux.HandleFunc("PUT /sources/{mac}", s.acting(s.reading(s.assignSource), true))
	mux.HandleFunc("GET /logs", s.acting(s.reading(s.logs), true))
	mux.HandleFunc("GET /metrics/names", s.acting(s.reading(s.metricNames), true))
	mux.HandleFunc("GET /metrics/series", s.acting(s.reading(s.series), true))
	mux.HandleFunc("GET /export", s.acting(s.reading(s.export), true))
	mux.HandleFunc("POST /devices/{device_id}/setup", s.acting(s.setup, false))
	mux.HandleFunc("POST /data/purge", s.acting(s.reading(s.purge), true))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusNotFound, map[string]string{"module_saw": r.URL.Path})
	})

	return mux
}

// handler is one route, with the person and the state already read.
type handler func(w http.ResponseWriter, r *http.Request, state State, by string)

// acting refuses a request the server attributed to nobody, and one that
// arrived before the settings. A route that reads the records refuses too
// when there are none to read.
func (s Service) acting(next handler, reads bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := routesapi.PrincipalOf(r)
		if strings.TrimSpace(by) == "" {
			refuse(w, NoPrincipal, "the host named no person for this request", nil)

			return
		}

		state := s.State()
		if !state.Ready {
			refuse(w, NotReady, "the host has not sent the settings yet. Try again in a moment.", nil)

			return
		}

		if reads && state.Book.Store == nil {
			refuse(w, NoStorage, state.Problem, nil)

			return
		}

		next(w, r, state, by)
	}
}

// reading sets the Book's viewer to what the caller may see before next
// runs. The server answers the list of printers as the caller, so the module
// never decides that by itself.
func (s Service) reading(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request, state State, by string) {
		visible, err := s.visible(r, by)
		if err != nil {
			refuse(w, FleetUnavailable, FleetUnavailable.Title+". Try again in a moment.", nil)

			return
		}

		state.Book.Viewer = query.ViewerOf(visible, state.Book.Sources.Covers(visible))

		next(w, r, state, by)
	}
}

// streamStatus is one stream in GET /status.
type streamStatus struct {
	Stream   string            `json:"stream"`
	Listener receiver.Listener `json:"listener"`
	Counts   receiver.Counts   `json:"counts"`
	LastDrop string            `json:"last_drop,omitempty"`
	// LastReceived is when the newest datagram arrived since the module
	// started.
	LastReceived *time.Time `json:"last_received,omitempty"`
	Bytes        int64      `json:"bytes"`
	Limit        int64      `json:"limit"`
	Segments     int        `json:"segments"`
	Oldest       *time.Time `json:"oldest,omitempty"`
}

// Status is what GET /status answers, and what the diagnostics bundle
// carries as status.json.
type Status struct {
	Streams        []streamStatus `json:"streams"`
	RetentionHours int            `json:"retention_hours"`
	MaxMiB         int            `json:"max_mib"`
	Storing        bool           `json:"storing"`
	Problem        string         `json:"problem,omitempty"`
	SourcesProblem string         `json:"sources_problem,omitempty"`
}

// StatusOf reads the status of one state.
func StatusOf(state State) Status {
	out := Status{
		Streams:        []streamStatus{},
		RetentionHours: state.Settings.RetentionHours,
		MaxMiB:         state.Settings.MaxMiB,
		Storing:        state.Book.Store != nil,
		Problem:        state.Problem,
	}

	var (
		received map[store.Stream]receiver.Status
		usage    map[store.Stream]store.Usage
	)

	if state.Receiver != nil {
		received = state.Receiver.Status()
	}

	if state.Book.Store != nil {
		usage = state.Book.Store.Usage()
	}

	if state.Book.Sources != nil {
		if err := state.Book.Sources.SaveError(); err != nil {
			out.SourcesProblem = err.Error()
		}
	}

	limits := state.Settings.Limits()

	for _, stream := range store.Streams() {
		held := streamStatus{Stream: string(stream), Limit: limits.Bytes[stream]}

		if status, ok := received[stream]; ok {
			held.Listener, held.Counts, held.LastDrop = status.Listener, status.Counts, status.LastDrop
			held.LastReceived = status.LastReceived
		}

		if port, granted := state.Ports[stream]; granted && state.Receiver == nil {
			held.Listener = receiver.Listener{
				Granted: true, Network: "udp", Port: port,
				Error: "nothing listens, because the module keeps nothing without a data directory",
			}
		}

		if u, ok := usage[stream]; ok {
			held.Bytes, held.Segments = u.Bytes, u.Segments

			if !u.Oldest.IsZero() {
				oldest := u.Oldest
				held.Oldest = &oldest
			}
		}

		out.Streams = append(out.Streams, held)
	}

	return out
}

func (s Service) status(w http.ResponseWriter, _ *http.Request, state State, _ string) {
	answer(w, http.StatusOK, StatusOf(state))
}

// listSources answers the sources of the printers the caller may see.
func (s Service) listSources(w http.ResponseWriter, _ *http.Request, state State, _ string) {
	answer(w, http.StatusOK, map[string]any{"sources": visibleSources(state.Book)})
}

func visibleSources(book query.Book) []sources.Source {
	return slices.DeleteFunc(book.Sources.List(), func(source sources.Source) bool {
		return !book.Viewer.Sees(source.Device)
	})
}

// assignSource matches a MAC to a printer by hand, or takes the match away.
// The caller must see the source as it is and the printer it goes to.
func (s Service) assignSource(w http.ResponseWriter, r *http.Request, state State, _ string) {
	mac := strings.ToLower(r.PathValue("mac"))
	if !datagram.ValidMAC(mac) {
		refuse(w, InvalidQuery, "mac is not a MAC address such as 10:9c:70:2a:bb:cd", map[string]any{"field": "mac"})

		return
	}

	var body struct {
		DeviceID string `json:"device_id"`
	}

	if !decoded(w, r, &body) {
		return
	}

	device := strings.TrimSpace(body.DeviceID)

	if !slices.ContainsFunc(visibleSources(state.Book), func(source sources.Source) bool { return source.MAC == mac }) {
		refuse(w, SourceNotFound, sources.ErrUnknownSource.Error(), map[string]any{"mac": mac})

		return
	}

	if device != "" && !state.Book.Viewer.Sees(device) {
		refuse(w, DeviceNotFound, sources.ErrUnknownDevice.Error(), map[string]any{"device_id": body.DeviceID})

		return
	}

	held, err := state.Book.Sources.Assign(mac, device)

	switch {
	case errors.Is(err, sources.ErrUnknownSource):
		refuse(w, SourceNotFound, err.Error(), map[string]any{"mac": mac})
	case errors.Is(err, sources.ErrUnknownDevice):
		refuse(w, DeviceNotFound, err.Error(), map[string]any{"device_id": body.DeviceID})
	case err != nil:
		refuse(w, StoreFailed, StoreFailed.Title, nil)
	default:
		answer(w, http.StatusOK, map[string]any{"source": held})
	}
}

// logs answers one page of log lines.
func (s Service) logs(w http.ResponseWriter, r *http.Request, state State, _ string) {
	q := r.URL.Query()

	filter, ok := logFilter(w, q)
	if !ok {
		return
	}

	limit, ok := whole(w, q, "limit")
	if !ok {
		return
	}

	page, err := state.Book.Logs(filter, q.Get("before"), q.Get("after"), limit)
	answered(w, page, err)
}

// logFilter reads the query a person narrows the log with.
func logFilter(w http.ResponseWriter, q map[string][]string) (query.LogFilter, bool) {
	get := func(key string) string {
		if values := q[key]; len(values) > 0 {
			return strings.TrimSpace(values[0])
		}

		return ""
	}

	filter := query.LogFilter{
		Device:    get("device"),
		MAC:       strings.ToLower(get("mac")),
		Component: get("component"),
		Text:      get("q"),
	}

	if word := get("severity"); word != "" {
		severity, known := datagram.ParseSeverity(word)
		if !known {
			refuse(
				w,
				InvalidQuery,
				"severity is emergency, alert, critical, error, warning, notice, info or debug",
				map[string]any{"field": "severity"},
			)

			return query.LogFilter{}, false
		}

		filter.Severity = &severity
	}

	window, ok := windowOf(w, get("since"), get("until"))
	filter.Window = window

	return filter, ok
}

// windowOf reads since and until, both RFC 3339 and both optional.
func windowOf(w http.ResponseWriter, since, until string) (store.Window, bool) {
	var window store.Window

	for _, pair := range []struct {
		key, value string
		into       *time.Time
	}{{"since", since, &window.Since}, {"until", until, &window.Until}} {
		if pair.value == "" {
			continue
		}

		at, err := time.Parse(time.RFC3339, pair.value)
		if err != nil {
			refuse(w, InvalidQuery, pair.key+" is not an RFC 3339 time such as 2026-10-19T14:00:00Z",
				map[string]any{"field": pair.key})

			return store.Window{}, false
		}

		*pair.into = at
	}

	return window, true
}

func (s Service) metricNames(w http.ResponseWriter, r *http.Request, state State, _ string) {
	q := r.URL.Query()

	window, ok := windowOf(w, q.Get("since"), q.Get("until"))
	if !ok {
		return
	}

	names, truncated, err := state.Book.Names(q.Get("device"), window)
	answered(w, map[string]any{"names": names, "truncated": truncated}, err)
}

func (s Service) series(w http.ResponseWriter, r *http.Request, state State, _ string) {
	q := r.URL.Query()

	window, ok := windowOf(w, q.Get("since"), q.Get("until"))
	if !ok {
		return
	}

	points, ok := whole(w, q, "max_points")
	if !ok {
		return
	}

	field := q.Get("field")
	if field == "" {
		field = "v"
	}

	held, err := state.Book.Series(query.SeriesQuery{
		Device: q.Get("device"), MAC: strings.ToLower(q.Get("mac")), Name: q.Get("name"), Field: field,
		Window: window, MaxPoints: points,
	})
	answered(w, map[string]any{"name": q.Get("name"), "field": field, "points": held}, err)
}

func (s Service) export(w http.ResponseWriter, r *http.Request, state State, _ string) {
	q := r.URL.Query()

	window, ok := windowOf(w, q.Get("since"), q.Get("until"))
	if !ok {
		return
	}

	page, err := state.Book.Export(store.Stream(q.Get("stream")), q.Get("device"), window, q.Get("cursor"))
	answered(w, page, err)
}

// maxPurgeHours bounds older_than_hours: a year, far past any retention.
const maxPurgeHours = 24 * 366

// purgeBody is what POST /data/purge takes.
type purgeBody struct {
	// OlderThanHours keeps the records of the newest hours and removes the
	// rest. Absent removes every record.
	OlderThanHours *int `json:"older_than_hours"`
	// Streams names the streams to purge. Absent purges both.
	Streams []string `json:"streams"`
}

// purge removes kept records: all of them, or those older than a number of
// hours. Which printers are matched to which MAC stays, so the lines that
// arrive next are still named by printer. It removes every printer's records,
// so it takes a person who may see every printer.
func (s Service) purge(w http.ResponseWriter, r *http.Request, state State, _ string) {
	if !state.Book.Viewer.Fleet() {
		refuse(w, PurgeNotAllowed, "deleting the kept records removes those of every printer, and you may "+
			"not see every printer. Ask an administrator.", nil)

		return
	}

	var body purgeBody
	if !decoded(w, r, &body) {
		return
	}

	streams := store.Streams()
	if body.Streams != nil {
		streams = nil

		for _, name := range body.Streams {
			stream := store.Stream(name)
			if !stream.Valid() || slices.Contains(streams, stream) {
				refuse(w, Invalid, "streams names "+name+", expected syslog or metrics once each",
					map[string]any{"field": "streams"})

				return
			}

			streams = append(streams, stream)
		}
	}

	var cutoff time.Time

	if body.OlderThanHours != nil {
		hours := *body.OlderThanHours
		if hours < 1 || hours > maxPurgeHours {
			refuse(w, Invalid, fmt.Sprintf("older_than_hours is %d, expected 1 to %d, or absent for every record",
				hours, maxPurgeHours), map[string]any{"field": "older_than_hours"})

			return
		}

		cutoff = state.Book.Now().Add(-time.Duration(hours) * time.Hour)
	}

	purged, err := state.Book.Store.Purge(streams, cutoff)
	if err != nil {
		refuse(w, PurgeFailed, err.Error(), nil)

		return
	}

	out := map[string]store.Purged{}
	for stream, held := range purged {
		out[string(stream)] = held
	}

	answer(w, http.StatusOK, map[string]any{"purged": out})
}

// setupBody is what POST /devices/{device_id}/setup takes.
type setupBody struct {
	Host        string `json:"host"`
	Syslog      bool   `json:"syslog"`
	Metrics     bool   `json:"metrics"`
	SyslogPort  *int   `json:"syslog_port"`
	MetricsPort *int   `json:"metrics_port"`
}

// setup sends M334 to the printer as the person who asked. The printer then
// shows a warning and changes nothing until a person presses Yes on its
// display. This module never answers that warning.
func (s Service) setup(w http.ResponseWriter, r *http.Request, state State, by string) {
	device := strings.TrimSpace(r.PathValue("device_id"))
	if device == "" || len(device) > deviceapi.MaxIDBytes {
		refuse(w, InvalidQuery, "device_id is empty or too long", map[string]any{"field": "device_id"})

		return
	}

	var body setupBody
	if !decoded(w, r, &body) {
		return
	}

	gcode, code, detail := setupLine(body, state.Ports)
	if code != nil {
		refuse(w, *code, detail, nil)

		return
	}

	result, err := s.Devices.Command(r.Context(), deviceapi.CommandParams{
		Principal: by, ID: device, Verb: sendGCode, Args: map[string]string{"gcode": gcode},
	})
	if err != nil {
		refuseCommand(w, device, err)

		return
	}

	if !result.OK {
		refuse(
			w,
			PrinterRefused,
			"the printer refused the line: "+result.MachineReason,
			map[string]any{"device_id": device},
		)

		return
	}

	answer(w, http.StatusAccepted, map[string]any{
		"device_id": device,
		"gcode":     gcode,
		"confirm": "The printer shows G-CODE METRICS CONFIG CHANGE. " +
			"Press Yes on its display, and the printer starts sending.",
	})
}

// setupLine is the M334 line for a setup, or the refusal for one that cannot
// be sent. A stream that is off is sent port 0, which the firmware reads as
// that stream switched off (src/syslog/syslog_transport.cpp). Both off is a
// bare M334, which switches the feature off and keeps the values.
func setupLine(body setupBody, granted map[store.Stream]int) (string, *Code, string) {
	if !body.Syslog && !body.Metrics {
		return "M334", nil, ""
	}

	host := strings.TrimSpace(body.Host)
	if len(host) < 2 || len(host) > maxHostBytes || !hostPattern.MatchString(host) {
		return "", &Invalid, fmt.Sprintf("host is an address or a name of 2 to %d letters, digits, dots and "+
			"hyphens, the most the printer takes", maxHostBytes)
	}

	ports := map[store.Stream]int{}

	for _, pick := range []struct {
		stream store.Stream
		on     bool
		port   *int
	}{{store.Metrics, body.Metrics, body.MetricsPort}, {store.Syslog, body.Syslog, body.SyslogPort}} {
		if !pick.on {
			continue
		}

		switch {
		case pick.port != nil && (*pick.port < 1 || *pick.port > 65535):
			return "", &Invalid, fmt.Sprintf("%s_port is %d, expected 1 to 65535", pick.stream, *pick.port)
		case pick.port != nil:
			// A port the printer is told may differ from the one granted,
			// because a container may publish the granted port under another.
			ports[pick.stream] = *pick.port
		case granted[pick.stream] > 0:
			ports[pick.stream] = granted[pick.stream]
		default:
			return "", &NotListening, fmt.Sprintf("the operator granted no %s port, so nothing would receive "+
				"what the printer sends. Approve the ports of this module, or name the port", pick.stream)
		}
	}

	return fmt.Sprintf("M334 %s %d %d", host, ports[store.Metrics], ports[store.Syslog]), nil, ""
}

// refuseCommand answers a command the host refused, with the code that says
// what the person can do.
func refuseCommand(w http.ResponseWriter, device string, err error) {
	params := map[string]any{"device_id": device}

	switch {
	case errors.Is(err, deviceapi.ErrNoDevice):
		refuse(w, DeviceNotFound, "there is no printer "+device, params)
	case errors.Is(err, deviceapi.ErrDenied):
		refuse(w, GCodeNotAllowed, "setting a printer up sends raw G-code, which takes a grant of its own on "+
			"that printer. Ask an administrator, or set it up on the printer: Settings, Network, Metrics & Log.", params)
	case errors.Is(err, deviceapi.ErrVerbUnknown), errors.Is(err, deviceapi.ErrArgument):
		refuse(w, PrinterRefused, "this printer takes no raw G-code. Set it up on the printer: Settings, "+
			"Network, Metrics & Log.", params)
	case errors.Is(err, deviceapi.ErrNotReady), errors.Is(err, deviceapi.ErrBusy):
		refuse(w, PrinterUnavailable, "the printer is away or busy. Try again when it is idle.", params)
	default:
		refuse(w, PrinterUnavailable, PrinterUnavailable.Title, params)
	}
}

// answered writes value, or the refusal for err.
func answered(w http.ResponseWriter, value any, err error) {
	switch {
	case errors.Is(err, store.ErrStale):
		refuse(w, CursorStale, CursorStale.Title+". Read the newest page again.", nil)
	case errors.Is(err, query.ErrInvalid), errors.Is(err, store.ErrPosition):
		refuse(w, InvalidQuery, err.Error(), nil)
	case err != nil:
		refuse(w, StoreFailed, StoreFailed.Title+". Try the same request again in a moment.", nil)
	default:
		answer(w, http.StatusOK, value)
	}
}

// whole reads an optional whole number, zero when absent.
func whole(w http.ResponseWriter, q map[string][]string, key string) (int, bool) {
	values := q[key]
	if len(values) == 0 || values[0] == "" {
		return 0, true
	}

	n, err := strconv.Atoi(values[0])
	if err != nil || n < 1 {
		refuse(w, InvalidQuery, key+" is a whole number of 1 or more", map[string]any{"field": key})

		return 0, false
	}

	return n, true
}

// decoded reads a JSON body into into, refusing one too large or unreadable.
func decoded(w http.ResponseWriter, r *http.Request, into any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		refuse(w, Invalid, "the body could not be read", nil)

		return false
	}

	if len(raw) > MaxBodyBytes {
		refuse(w, BodyTooLarge, fmt.Sprintf("the body is larger than %d bytes", MaxBodyBytes), nil)

		return false
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(into); err != nil {
		refuse(w, Invalid, "the body is not the JSON this route takes: "+err.Error(), nil)

		return false
	}

	return true
}

// answer writes one JSON answer.
func answer(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value) //nolint:errchkjson // the caller has gone when this fails.
}
