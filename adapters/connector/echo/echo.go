// Package echo is an inbound printer connector: the device opens the
// connection, says what it is, and is driven over the routes it dialled.
//
// It is a reference implementation of sdk/connectorapi, meant to be copied.
// The dialect is JSON over HTTP. Replace the five handlers with a vendor's own
// framing. Paths are relative to where the host mounts the driver.
//
//	POST /connect            identity and probe in, a device token out
//	GET  /commands?device=   long poll: one command, or 204 when none arrived
//	POST /results?device=    the outcome of one command, by its id
//	POST /state?device=      one patch to the addressable state tree
//	POST /disconnect?device= the device is going away
//
// It authenticates nothing. The token only names a connection. A real dialect
// checks the device's credentials before it calls Admit.
package echo

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `type` of a connector that selects this driver.
const Type connectorapi.TypeName = "echo"

// The capabilities this driver can do. A profile and a device's probe can only
// narrow them.
const (
	// VerbStatus reads the printer's state and moves nothing, so a conformance
	// run may send it.
	VerbStatus = connectorapi.Capability("core.command.get_status")
	// VerbNozzleTarget sets the nozzle's target temperature.
	VerbNozzleTarget = connectorapi.Capability("core.command.set_nozzle_temperature")
	// ArgNozzleCelsius is the target, in whole degrees celsius.
	ArgNozzleCelsius = connectorapi.Capability("core.command.set_nozzle_temperature#celsius")
	// VerbStartPrint starts a print of a file the printer can already reach.
	VerbStartPrint = connectorapi.Capability("core.command.start_print")
	// ArgStartPrintPath names that file.
	ArgStartPrintPath = connectorapi.Capability("core.command.start_print#path")
	// FeatureStateStream reports that the device pushes state without being
	// asked.
	FeatureStateStream = connectorapi.Capability("core.feature.state_stream")
)

const (
	// nozzleCeiling is the highest nozzle target, in degrees celsius. The host
	// enforces it from Baseline, so no handler checks it again.
	nozzleCeiling = 300
	// pathLimit bounds a print path in bytes.
	pathLimit = 255

	defaultQueueDepth = 16
	defaultPollWindow = 25 * time.Second
)

var (
	// ErrSessionClosed reports work offered to a device that has gone away.
	ErrSessionClosed = errors.New("echo: the device is no longer connected")
	// ErrQueueFull reports a command offered to a device whose queue is full.
	ErrQueueFull = errors.New("echo: the device is not draining its command queue")
	// ErrInvalidConfig reports a config block this driver cannot act on.
	ErrInvalidConfig = errors.New("echo: invalid configuration")
)

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { connectorapi.Register(Type, doc(), New, connectorapi.Dialect{States: states}) }

// doc is this driver's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Reference inbound dialect: JSON over HTTP, a long poll for commands, a post for each result.",
		Description: "The plainest dialect that is still real. A device opens the connection, says what it " +
			"is, and is driven over the five routes it dialled. It authenticates nothing: the token it mints " +
			"proves only that a device completed a handshake, and a dialect carrying credentials checks them " +
			"before it calls Admit. Copy it and replace the five handlers with a vendor's own framing.",
		Version:   "1.0.0",
		Contract:  connectorapi.Contract,
		Stability: extension.StabilityExperimental,
		Options: []extension.Option{
			{
				Key:     "queue_depth",
				Type:    extension.OptionInt,
				Default: "16",
				Summary: "How many commands may wait for one device before the host is told it is not keeping up.",
			},
			{
				Key:     "poll_window",
				Type:    extension.OptionDuration,
				Default: "25s",
				Summary: "How long a command poll waits before answering that nothing arrived.",
			},
		},
	}
}

// Config is what an operator writes under a connector's `config` block.
type Config struct {
	// QueueDepth is how many commands may wait for one device before new ones
	// fail.
	QueueDepth int `json:"queue_depth,omitempty"`
	// PollWindow is how long a command poll waits before it answers 204, as
	// a time.ParseDuration string. Keep it below the host's read timeout.
	PollWindow string `json:"poll_window,omitempty"`
}

// Driver implements connectorapi.Inbound.
type Driver struct {
	queueDepth int
	pollWindow time.Duration

	mu       sync.Mutex
	sessions map[string]*Session
}

// New builds a Driver from its spec.
func New(spec connectorapi.Spec) (connectorapi.Driver, error) {
	cfg := Config{QueueDepth: defaultQueueDepth}
	if err := spec.DecodeConfig(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if cfg.QueueDepth < 1 {
		return nil, fmt.Errorf("%w: queue_depth must be at least 1, and it is %d",
			ErrInvalidConfig, cfg.QueueDepth)
	}

	window := defaultPollWindow

	if cfg.PollWindow != "" {
		parsed, err := time.ParseDuration(cfg.PollWindow)
		if err != nil {
			return nil, fmt.Errorf("%w: poll_window: %w", ErrInvalidConfig, err)
		}

		if parsed <= 0 {
			return nil, fmt.Errorf("%w: poll_window must be positive, and it is %s", ErrInvalidConfig, parsed)
		}

		window = parsed
	}

	return NewDriver(cfg.QueueDepth, window), nil
}

// NewDriver returns a driver with the given queue depth and poll window.
func NewDriver(queueDepth int, pollWindow time.Duration) *Driver {
	return &Driver{
		queueDepth: queueDepth,
		pollWindow: pollWindow,
		sessions:   make(map[string]*Session),
	}
}

// Baseline is what this driver's code can do, for any device.
func (d *Driver) Baseline() connectorapi.Set {
	return connectorapi.Set{
		VerbStatus:       {Flag: true},
		VerbNozzleTarget: {Flag: true},
		ArgNozzleCelsius: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: nozzleCeiling, HasLimit: true, Unit: connectorapi.UnitCelsius,
		},
		VerbStartPrint: {Flag: true},
		ArgStartPrintPath: {
			Flag: true, Kind: connectorapi.KindString, Required: true,
			Limit: pathLimit, HasLimit: true,
		},
		FeatureStateStream: {Flag: true},
	}
}

// Handler returns the routes a device dials. The host mounts them under a
// prefix and strips it, so a pattern here must not include that prefix.
func (d *Driver) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	if arrivals == nil {
		return nil, errors.New("echo: the host passed no Arrivals, so a device that dials in reaches nothing")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /connect", func(w http.ResponseWriter, r *http.Request) {
		d.handleConnect(w, r, arrivals)
	})
	mux.HandleFunc("GET /commands", d.handleCommands)
	mux.HandleFunc("POST /results", d.handleResult)
	mux.HandleFunc("POST /state", d.handleState)
	mux.HandleFunc("POST /disconnect", d.handleDisconnect)

	return mux, nil
}

// connectRequest is what a device says about itself. Devices depend on the
// field names. A field whose meaning changes gets a new name.
type connectRequest struct {
	Vendor   string                    `json:"vendor"`
	Model    string                    `json:"model"`
	Firmware string                    `json:"firmware"`
	Serial   string                    `json:"serial"`
	UnitUID  string                    `json:"unit_uid"`
	Probe    []connectorapi.Capability `json:"probe,omitempty"`
}

// connectResponse tells the device its token and its enabled capabilities.
type connectResponse struct {
	Device       string                    `json:"device"`
	Capabilities []connectorapi.Capability `json:"capabilities"`
	Digest       string                    `json:"digest"`
}

// wireCommand is one command on its way to the device.
type wireCommand struct {
	ID   uint64                  `json:"id"`
	Verb connectorapi.Capability `json:"verb"`
	Args map[string]string       `json:"args,omitempty"`
}

// wireResult is the device's answer to one command.
type wireResult struct {
	ID          uint64            `json:"id"`
	OK          bool              `json:"ok"`
	Ignored     []string          `json:"ignored,omitempty"`
	Substituted map[string]string `json:"substituted,omitempty"`
	Raw         json.RawMessage   `json:"raw,omitempty"`
}

// handleConnect admits a device and announces its session. Call Admit first
// and Arrived second. A session announced before Admit answers would serve a
// printer the host has not accepted.
func (d *Driver) handleConnect(w http.ResponseWriter, r *http.Request, arrivals connectorapi.Arrivals) {
	var reported connectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&reported); err != nil {
		http.Error(w, "the identity is not readable", http.StatusBadRequest)

		return
	}

	identity := connectorapi.Identity{
		Vendor:   reported.Vendor,
		Model:    reported.Model,
		Firmware: connectorapi.NewFirmwareLabel(reported.Firmware),
		Serial:   reported.Serial,
		UnitUID:  reported.UnitUID,
	}

	profile, err := arrivals.Admit(r.Context(), identity)
	if err != nil {
		http.Error(w, "the host does not accept this printer", http.StatusForbidden)

		return
	}

	// The profile narrows the baseline, and the device's probe narrows it
	// again.
	resolved := d.Baseline().Intersect(profile)
	if probe := reported.Probe; len(probe) > 0 {
		resolved = resolved.Intersect(probeSet(resolved, probe))
	}

	// The token comes first, so the session's close callback is set before
	// any other goroutine can reach the session.
	token := rand.Text()
	session := newSession(identity, resolved, d.queueDepth, func() { d.forget(token) })

	d.remember(token, session)

	if taken := arrivals.Arrived(r.Context(), session); taken != nil {
		_ = session.Close()
		http.Error(w, "the host did not take the session", http.StatusServiceUnavailable)

		return
	}

	writeJSON(w, connectResponse{
		Device:       token,
		Capabilities: enabledKeys(resolved),
		Digest:       resolved.Digest(),
	})
}

// handleCommands is the device's long poll. It answers 204 when the window
// passes with nothing to send.
func (d *Driver) handleCommands(w http.ResponseWriter, r *http.Request) {
	session, ok := d.lookup(w, r)
	if !ok {
		return
	}

	call, ok := session.nextCall(r.Context(), d.pollWindow)
	if !ok {
		w.WriteHeader(http.StatusNoContent)

		return
	}

	writeJSON(w, wireCommand{ID: call.id, Verb: call.cmd.Verb, Args: call.cmd.Args})
}

// handleResult matches one outcome to the command waiting for it. An unknown
// id is accepted and dropped: the device answered twice, or after the caller
// gave up.
func (d *Driver) handleResult(w http.ResponseWriter, r *http.Request) {
	session, ok := d.lookup(w, r)
	if !ok {
		return
	}

	var reported wireResult
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&reported); err != nil {
		http.Error(w, "the result is not readable", http.StatusBadRequest)

		return
	}

	session.complete(reported.ID, connectorapi.Result{
		OK:          reported.OK,
		Ignored:     reported.Ignored,
		Substituted: reported.Substituted,
		Raw:         reported.Raw,
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleState takes one patch to the addressable state tree.
func (d *Driver) handleState(w http.ResponseWriter, r *http.Request) {
	session, ok := d.lookup(w, r)
	if !ok {
		return
	}

	var patch connectorapi.StatePatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&patch); err != nil {
		http.Error(w, "the patch is not readable", http.StatusBadRequest)

		return
	}

	if patch.Path == "" {
		http.Error(w, "the patch addresses no node", http.StatusBadRequest)

		return
	}

	if err := session.report(r.Context(), patch); err != nil {
		http.Error(w, "the session is closed", http.StatusGone)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDisconnect ends the session. Close closes the state channel, and the
// host watches that channel to learn the printer went away.
func (d *Driver) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	session, ok := d.lookup(w, r)
	if !ok {
		return
	}

	if err := session.Close(); err != nil {
		http.Error(w, "the session did not close cleanly", http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// maxBodyBytes bounds every request body.
const maxBodyBytes = 1 << 20

// remember stores a session under its token. The token names one connection
// and proves nothing about the device.
func (d *Driver) remember(token string, session *Session) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.sessions[token] = session
}

func (d *Driver) forget(token string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.sessions, token)
}

// lookup finds the session for the request's device token. It writes the
// error response itself when there is none.
func (d *Driver) lookup(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	token := r.URL.Query().Get("device")
	if token == "" {
		http.Error(w, "no device named", http.StatusBadRequest)

		return nil, false
	}

	d.mu.Lock()
	session, ok := d.sessions[token]
	d.mu.Unlock()

	if !ok {
		http.Error(w, "this connection is not open", http.StatusGone)

		return nil, false
	}

	return session, true
}

// probeSet turns the keys a device claims into a set. A key not in resolved is
// dropped, because a probe can only narrow.
func probeSet(resolved connectorapi.Set, probe []connectorapi.Capability) connectorapi.Set {
	claimed := make(connectorapi.Set, len(probe))

	for _, key := range probe {
		declared, ok := resolved[key]
		if !ok {
			continue
		}

		claimed[key] = declared
	}

	return claimed
}

// enabledKeys is the resolved set without the keys that are switched off.
func enabledKeys(set connectorapi.Set) []connectorapi.Capability {
	keys := set.Keys()
	enabled := make([]connectorapi.Capability, 0, len(keys))

	for _, key := range keys {
		if set.Has(key) {
			enabled = append(enabled, key)
		}
	}

	return enabled
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")

	// The status is already sent when an encode fails. The device sees a
	// truncated body and retries.
	_ = json.NewEncoder(w).Encode(body)
}
