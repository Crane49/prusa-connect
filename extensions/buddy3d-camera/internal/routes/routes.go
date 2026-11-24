// Package routes is the module's own HTTP surface, which the settings page
// calls through the host. The host authorizes every request as the person on
// the page: reading takes view_resource on this module's instance, and
// anything else takes operate_resource.
//
//	GET    /status                    where cameras send, and whether it is listening
//	GET    /cameras                   the paired cameras and what each last reported
//	POST   /cameras                   pair a camera to a push camera of the core
//	DELETE /cameras/{name}            forget a camera and its token
//	POST   /cameras/{name}/snapshot   ask a connected camera for a picture now
//	PUT    /cameras/{name}/settings   what a person chose for a camera
//	POST   /cameras/{name}/control    turn, restart or change a setting the camera keeps
//	GET    /certificate               the self-signed.pem a camera trusts
//	POST   /cameras/{name}/live                     start a live view: the camera's offer
//	POST   /cameras/{name}/live/{session}/answer    the browser's answer
//	DELETE /cameras/{name}/live/{session}           end the live view
//
// The live view and settings routes are asked of the core first, beside the
// module route's own rule: access.check answers whether the person may see
// the camera, or operate it to change its settings or turn it.
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
	"github.com/prusa3d/buddy3d-camera/internal/device"
	"github.com/prusa3d/buddy3d-camera/internal/sio"
	"github.com/prusa3d/buddy3d-camera/internal/store"
)

// State is what the routes read, filled once the host configured the module.
type State struct {
	Ready   bool
	Address string
	Port    int
	// Listening is false when the port could not be opened, and Problem
	// says why.
	Listening bool
	Problem   string
	Cert      []byte
	// SnapshotInterval is the module's default, in seconds, which a camera
	// with no interval of its own is told.
	SnapshotInterval int
	Store            *store.Store
	Device           Device
	// May answers whether a person may take an action on a camera's device
	// row: observe to see it, operate to change it. Nil refuses every route
	// that asks.
	May func(ctx context.Context, principal, action, camera string) (bool, error)
}

// Device is the part of the camera server the routes call.
type Device interface {
	Snapshot(name string) error
	Connected(name string) bool
	Drop(name string)
	Configure(name string) error
	Move(ctx context.Context, name, direction string, angle uint32) error
	Change(ctx context.Context, name string, change camproto.Change) error
	Timelapse(name string, on bool) error
	Restart(name string) error
	StartLive(ctx context.Context, camera string) (session, offer string, err error)
	AnswerLive(camera, session, answer string) error
	StopLive(camera, session string) error
}

// The server's rule for a resource name, a DNS label of at most 63
// characters, so a name the core would refuse is refused here first.
var namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

const maxBody = 4 << 10

// New returns the handler over state.
func New(state func() State) http.Handler {
	mux := http.NewServeMux()
	h := handlers{state: state}

	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /cameras", h.withStore(h.list))
	mux.HandleFunc("POST /cameras", h.withStore(h.pair))
	mux.HandleFunc("DELETE /cameras/{name}", h.withStore(h.remove))
	mux.HandleFunc("POST /cameras/{name}/snapshot", h.withStore(h.snapshot))
	mux.HandleFunc("GET /certificate", h.certificate)
	mux.HandleFunc("PUT /cameras/{name}/settings", h.allowed(actionOperate, h.settings))
	mux.HandleFunc("POST /cameras/{name}/control", h.allowed(actionOperate, connected(h.control)))
	mux.HandleFunc("POST /cameras/{name}/live", h.allowed(actionObserve, connected(streaming(h.startLive))))
	mux.HandleFunc("POST /cameras/{name}/live/{session}/answer", h.allowed(actionObserve, connected(h.answerLive)))
	mux.HandleFunc("DELETE /cameras/{name}/live/{session}", h.allowed(actionObserve, connected(h.stopLive)))

	return mux
}

type handlers struct {
	state func() State
}

// Status is what GET /status answers.
type Status struct {
	Ready     bool   `json:"ready"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Listening bool   `json:"listening"`
	Problem   string `json:"problem,omitempty"`
	// SrvCfg is the srv_cfg file a camera's SD card holds.
	SrvCfg string `json:"srv_cfg"`
	// SnapshotInterval is the module's default, in seconds.
	SnapshotInterval int `json:"snapshot_interval"`
}

func (h handlers) status(w http.ResponseWriter, _ *http.Request) {
	state := h.state()
	answer(w, http.StatusOK, Status{
		Ready: state.Ready, Address: state.Address, Port: state.Port,
		Listening: state.Listening, Problem: state.Problem, SrvCfg: SrvCfg(state.Address, state.Port),
		SnapshotInterval: state.SnapshotInterval,
	})
}

// SrvCfg is the srv_cfg file that points a camera's picture and its session
// at this module. The time and update servers are left out, so the camera
// keeps its own; the pairing page adds a time server a person names.
func SrvCfg(address string, port int) string {
	if address == "" || port == 0 {
		return ""
	}

	at := fmt.Sprintf("%s:%d", address, port)
	return "IMG_SRV=custom\nIMG_URL=" + at + "\n\nSIO_SRV=custom\nSIO_URL=" + at + "\n"
}

// Camera is one paired camera, and whether its session is open.
type Camera struct {
	store.Entry

	Connected bool `json:"connected"`
}

func (h handlers) withStore(next func(http.ResponseWriter, *http.Request, State)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := h.state()
		if state.Store == nil {
			refuse(w, http.StatusServiceUnavailable, "NOT_READY",
				"the module has no data directory yet. Install it again with approve.")

			return
		}

		next(w, r, state)
	}
}

func (h handlers) list(w http.ResponseWriter, _ *http.Request, state State) {
	entries := state.Store.List()
	items := make([]Camera, 0, len(entries))

	for _, entry := range entries {
		items = append(
			items,
			Camera{Entry: entry, Connected: state.Device != nil && state.Device.Connected(entry.Name)},
		)
	}

	answer(w, http.StatusOK, map[string]any{"items": items})
}

// PairRequest is what POST /cameras takes.
type PairRequest struct {
	// Name is the push camera the page declared in the core.
	Name    string `json:"name"`
	Printer string `json:"printer"`
	// Token is the token the camera already holds, or empty for a new one.
	Token string `json:"token,omitempty"`
}

// Paired is what POST /cameras answers: the token the camera's sdcard_cfg.txt
// carries, and the srv_cfg its SD card holds.
type Paired struct {
	Name    string `json:"name"`
	Printer string `json:"printer"`
	Token   string `json:"token"`
	SrvCfg  string `json:"srv_cfg"`
}

func (h handlers) pair(w http.ResponseWriter, r *http.Request, state State) {
	var sent PairRequest

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil || json.Unmarshal(body, &sent) != nil {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST", "the body is a JSON object with name and printer")

		return
	}

	if !namePattern.MatchString(sent.Name) || sent.Printer == "" {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST",
			"name is lowercase letters, digits and dashes, and printer is a printer's id")

		return
	}

	paired, err := state.Store.Pair(sent.Name, sent.Printer, sent.Token)

	switch {
	case errors.Is(err, store.ErrBadToken), errors.Is(err, store.ErrTokenTaken):
		refuse(w, http.StatusBadRequest, "BAD_TOKEN", err.Error())

		return
	case err != nil:
		refuse(w, http.StatusInternalServerError, "NOT_SAVED", "the camera could not be saved")

		return
	}

	// A camera paired again starts a new session with the new token.
	if state.Device != nil {
		state.Device.Drop(paired.Name)
	}

	answer(w, http.StatusCreated, Paired{
		Name: paired.Name, Printer: paired.Printer, Token: paired.Token,
		SrvCfg: SrvCfg(state.Address, state.Port),
	})
}

func (h handlers) remove(w http.ResponseWriter, r *http.Request, state State) {
	name := r.PathValue("name")

	if err := state.Store.Remove(name); err != nil {
		if errors.Is(err, store.ErrNoSuchCamera) {
			refuse(w, http.StatusNotFound, "NOT_FOUND", "no camera of that name is paired")

			return
		}

		refuse(w, http.StatusInternalServerError, "NOT_SAVED", "the camera could not be removed")

		return
	}

	if state.Device != nil {
		state.Device.Drop(name)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h handlers) snapshot(w http.ResponseWriter, r *http.Request, state State) {
	name := r.PathValue("name")

	if !state.Store.Has(name) {
		refuse(w, http.StatusNotFound, "NOT_FOUND", "no camera of that name is paired")

		return
	}

	if state.Device == nil || state.Device.Snapshot(name) != nil {
		refuse(w, http.StatusConflict, "NOT_CONNECTED", "the camera is not connected")

		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h handlers) certificate(w http.ResponseWriter, _ *http.Request) {
	state := h.state()
	if len(state.Cert) == 0 {
		refuse(w, http.StatusServiceUnavailable, "NOT_READY", "the module has made no certificate yet")

		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="self-signed.pem"`)
	_, _ = w.Write(state.Cert)
}

// The actions the core's access.check is asked about, on a camera's device
// row.
const (
	actionObserve = "observe"
	actionOperate = "operate"
)

// allowed holds a route to a paired camera the person may take action on. A
// camera the person may not see is answered as one that is not paired, so the
// routes tell nobody which cameras exist.
func (h handlers) allowed(action string, next func(http.ResponseWriter, *http.Request, State)) http.HandlerFunc {
	return h.withStore(func(w http.ResponseWriter, r *http.Request, state State) {
		name := r.PathValue("name")
		principal := routesapi.PrincipalOf(r)

		allowed := false
		if state.May != nil && principal != "" && state.Store.Has(name) {
			var err error
			if allowed, err = state.May(r.Context(), principal, action, name); err != nil {
				refuse(w, http.StatusServiceUnavailable, "NOT_ASKED", "the server could not say who may use the camera")

				return
			}
		}

		if !allowed {
			refuse(w, http.StatusNotFound, "NOT_FOUND", "no camera of that name is paired")

			return
		}

		next(w, r, state)
	})
}

// connected holds a route to a camera whose session is open.
func connected(next func(http.ResponseWriter, *http.Request, State)) func(http.ResponseWriter, *http.Request, State) {
	return func(w http.ResponseWriter, r *http.Request, state State) {
		if state.Device == nil || !state.Device.Connected(r.PathValue("name")) {
			refuse(w, http.StatusConflict, "NOT_CONNECTED", "the camera is not connected")

			return
		}

		next(w, r, state)
	}
}

// streaming holds the live view of a camera whose WebRTC is not switched off.
func streaming(next func(http.ResponseWriter, *http.Request, State)) func(http.ResponseWriter, *http.Request, State) {
	return func(w http.ResponseWriter, r *http.Request, state State) {
		if stream := state.Store.Settings(
			r.PathValue("name"),
		).Stream; stream == camproto.StreamRTSP ||
			stream == camproto.StreamOff {
			refuse(w, http.StatusConflict, "LIVE_OFF", "the camera's WebRTC stream is switched off in its settings")

			return
		}

		next(w, r, state)
	}
}

// settings keeps what a person chose for a camera and tells the camera now,
// when it is connected. One that is not is told when it connects.
func (h handlers) settings(w http.ResponseWriter, r *http.Request, state State) {
	var sent store.Settings

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil || json.Unmarshal(body, &sent) != nil {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST", "the body is a JSON object with snapshot_interval and stream")

		return
	}

	name := r.PathValue("name")

	switch err = state.Store.SetSettings(name, sent); {
	case errors.Is(err, store.ErrBadInterval):
		refuse(w, http.StatusBadRequest, "BAD_INTERVAL", err.Error())

		return
	case errors.Is(err, store.ErrBadStream):
		refuse(w, http.StatusBadRequest, "BAD_STREAM", err.Error())

		return
	case errors.Is(err, store.ErrNoSuchCamera):
		refuse(w, http.StatusNotFound, "NOT_FOUND", "no camera of that name is paired")

		return
	case err != nil:
		refuse(w, http.StatusInternalServerError, "NOT_SAVED", "the settings could not be saved")

		return
	}

	if state.Device != nil && state.Device.Connected(name) {
		_ = state.Device.Configure(name)
	}

	answer(w, http.StatusOK, sent)
}

// Control is what POST /cameras/{name}/control takes: one of its fields.
type Control struct {
	Move *Move `json:"move,omitempty"`
	// Mode is the infrared mode, a key of camproto.Modes.
	Mode   string `json:"mode,omitempty"`
	Volume uint32 `json:"volume,omitempty"`
	// Timelapse starts or stops keeping pictures on the camera's SD card.
	Timelapse *bool `json:"timelapse,omitempty"`
	// TimelapseInterval is in seconds.
	TimelapseInterval uint32 `json:"timelapse_interval,omitempty"`
	// VideoQuality is a key of camproto.Qualities.
	VideoQuality string `json:"video_quality,omitempty"`
	// Name is the name the camera gives itself on the network.
	Name    string `json:"name,omitempty"`
	Restart bool   `json:"restart,omitempty"`
}

// Move is one relative turn.
type Move struct {
	Direction string `json:"direction"`
	Angle     uint32 `json:"angle"`
}

// The bounds of what one control request takes. MaxAngle stops a slip of a
// number driving the motor against its stop.
const (
	MaxAngle             = 90
	MaxVolume            = 100
	MaxTimelapseInterval = 3600
	MaxNameLength        = 32
)

// fields counts the fields of a control request that are set.
func (c Control) fields() int {
	n := 0

	for _, set := range []bool{
		c.Move != nil, c.Mode != "", c.Volume != 0, c.Timelapse != nil,
		c.TimelapseInterval != 0, c.VideoQuality != "", c.Name != "", c.Restart,
	} {
		if set {
			n++
		}
	}

	return n
}

// check says what is wrong with a control request, or is empty.
func (c Control) check() string {
	_, quality := camproto.Qualities[c.VideoQuality]
	_, mode := camproto.Modes[c.Mode]

	switch {
	case c.fields() != 1:
		return "the body is a JSON object with one of move, mode, volume, timelapse, " +
			"timelapse_interval, video_quality, name or restart"
	case c.Move != nil:
		if _, ok := camproto.Directions[c.Move.Direction]; !ok || c.Move.Angle < 1 || c.Move.Angle > MaxAngle {
			return fmt.Sprintf("direction is left, right, up or down, and angle is 1 to %d degrees", MaxAngle)
		}
	case c.Mode != "" && !mode:
		return "mode is auto, day or night"
	case c.Volume > MaxVolume:
		return fmt.Sprintf("volume is 1 to %d", MaxVolume)
	case c.TimelapseInterval > MaxTimelapseInterval:
		return fmt.Sprintf("timelapse_interval is 1 to %d seconds", MaxTimelapseInterval)
	case c.VideoQuality != "" && !quality:
		return "video_quality is sd, hd or fhd"
	case utf8.RuneCountInString(c.Name) > MaxNameLength || c.Name != "" && strings.TrimSpace(c.Name) != c.Name:
		return fmt.Sprintf("name is at most %d characters, with no space at either end", MaxNameLength)
	}

	return ""
}

func (h handlers) control(w http.ResponseWriter, r *http.Request, state State) {
	var sent Control

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil || json.Unmarshal(body, &sent) != nil {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST", "the body is a JSON object")

		return
	}

	if problem := sent.check(); problem != "" {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST", problem)

		return
	}

	name := r.PathValue("name")

	switch {
	case sent.Move != nil:
		err = state.Device.Move(r.Context(), name, sent.Move.Direction, sent.Move.Angle)
	case sent.Timelapse != nil:
		err = state.Device.Timelapse(name, *sent.Timelapse)
	case sent.Restart:
		err = state.Device.Restart(name)
	default:
		err = state.Device.Change(r.Context(), name, camproto.Change{
			Mode: sent.Mode, Volume: sent.Volume, TimelapseInterval: sent.TimelapseInterval,
			VideoQuality: sent.VideoQuality, Name: sent.Name,
		})
	}

	var refused device.RefusedError

	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &refused):
		refuse(w, http.StatusUnprocessableEntity, "REFUSED",
			fmt.Sprintf("the camera refused it with code %d", refused.Code))
	case errors.Is(err, sio.ErrNotConnected):
		refuse(w, http.StatusConflict, "NOT_CONNECTED", "the camera is not connected")
	default:
		refuse(w, http.StatusGatewayTimeout, "NO_ANSWER", "the camera did not say whether it did it")
	}
}

// Live is what POST /cameras/{name}/live answers.
type Live struct {
	Session string `json:"session"`
	// Offer is the camera's SDP offer, with its candidates in it.
	Offer string `json:"offer"`
}

func (h handlers) startLive(w http.ResponseWriter, r *http.Request, state State) {
	session, offer, err := state.Device.StartLive(r.Context(), r.PathValue("name"))
	if err != nil {
		refuse(w, http.StatusGatewayTimeout, "NO_OFFER", "the camera did not start a live view")

		return
	}

	answer(w, http.StatusCreated, Live{Session: session, Offer: offer})
}

// LiveAnswer is what POST /cameras/{name}/live/{session}/answer takes.
type LiveAnswer struct {
	// Answer is the browser's SDP answer, with every candidate of its own.
	Answer string `json:"answer"`
}

const maxSDP = 64 << 10

func (h handlers) answerLive(w http.ResponseWriter, r *http.Request, state State) {
	var sent LiveAnswer

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSDP))
	if err != nil || json.Unmarshal(body, &sent) != nil || sent.Answer == "" {
		refuse(w, http.StatusBadRequest, "BAD_REQUEST", "the body is a JSON object with the answer")

		return
	}

	if err = state.Device.AnswerLive(r.PathValue("name"), r.PathValue("session"), sent.Answer); err != nil {
		refuse(w, http.StatusConflict, "NO_SESSION", "the live view is not open any more")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h handlers) stopLive(w http.ResponseWriter, r *http.Request, state State) {
	if err := state.Device.StopLive(r.PathValue("name"), r.PathValue("session")); err != nil {
		refuse(w, http.StatusConflict, "NO_SESSION", "the live view is not open any more")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func refuse(w http.ResponseWriter, code int, kind, message string) {
	answer(w, code, map[string]string{"code": kind, "message": message})
}

func answer(w http.ResponseWriter, code int, body any) {
	held, err := json.Marshal(body)
	if err != nil {
		code, held = http.StatusInternalServerError, []byte(`{"code":"INTERNAL"}`)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(held)
}
