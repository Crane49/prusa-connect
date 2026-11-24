// Package device is the server a Buddy3D camera dials: the cloud's HTTPS
// routes for its picture and its description, and the Socket.IO session it
// takes commands over. Both are on one port.
//
// The routes and their answers are the ones the firmware (3.1.7) was seen to
// use against prusacam-probe:
//
//	PUT /v1/camera-info   the camera describes itself (also /c/info)
//	PUT /v1/snapshot      one JPEG (also /c/snapshot)
//	GET /socket.io/       the websocket the commands go over
//
// Every request carries the camera's token and fingerprint in the Token and
// Fingerprint headers.
package device

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
	"github.com/prusa3d/buddy3d-camera/internal/sio"
	"github.com/prusa3d/buddy3d-camera/internal/store"
)

const (
	// MaxFrameBytes is the largest picture taken, the bound the core holds a
	// push camera's frame to.
	MaxFrameBytes = 8 << 20
	maxInfoBytes  = 64 << 10
)

// Frames is where a picture goes: the core's camera.accept.
type Frames interface {
	Accept(ctx context.Context, principal, camera string, frame []byte) error
}

// Service answers the camera.
type Service struct {
	store    *store.Store
	frames   Frames
	interval uint32
	log      *slog.Logger
	sio      *sio.Server
	live     live
}

// New returns the service. interval is the snapshot interval in seconds each
// camera is told when it connects.
func New(cameras *store.Store, frames Frames, interval uint32, log *slog.Logger) *Service {
	s := &Service{store: cameras, frames: frames, interval: interval, log: log,
		live: live{sessions: map[string]*liveSession{}}}
	s.sio = sio.New(s, log)

	return s
}

// Handler is the camera's routes.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	for _, path := range []string{"/v1/camera-info", "/c/info", "/v0/c/info"} {
		mux.HandleFunc(path, s.info)
	}

	for _, path := range []string{"/v1/snapshot", "/c/snapshot", "/v0/c/snapshot"} {
		mux.HandleFunc(path, s.snapshot)
	}

	mux.Handle("/socket.io/", s.sio)

	return mux
}

// Snapshot asks the camera name for a picture now.
func (s *Service) Snapshot(name string) error {
	return s.sio.Send(name, "trigger", camproto.Trigger(camproto.TriggerSnapshot, "snapshot"))
}

// ackTimeout is how long a command waits for the camera to say whether it did
// it. A turn is acked once the motor has moved.
const ackTimeout = 10 * time.Second

// RefusedError is a command the camera answered with a code other than done,
// such as 102 for one it does not support.
type RefusedError struct{ Code int }

func (e RefusedError) Error() string {
	return fmt.Sprintf("device: the camera refused the command with code %d", e.Code)
}

// Move turns the camera name by angle degrees in a direction of
// camproto.Directions, then asks it for a picture, which shows where it
// points now.
func (s *Service) Move(ctx context.Context, name, direction string, angle uint32) error {
	if err := s.command(ctx, name, camproto.Rotation(direction, angle, "move")); err != nil {
		return err
	}

	return s.Snapshot(name)
}

// Change sends the camera name a setting it keeps itself, then asks how it
// is, so its report shows the change.
func (s *Service) Change(ctx context.Context, name string, change camproto.Change) error {
	if err := s.command(ctx, name, change.Message("change")); err != nil {
		return err
	}

	return s.askStatus(name)
}

// Timelapse starts or stops the camera name keeping a picture on its SD card
// at each timelapse interval. A timelapse started goes to a folder named for
// the time it started.
func (s *Service) Timelapse(name string, on bool) error {
	if err := s.sio.Send(name, "trigger", camproto.Toggle(camproto.TriggerTimelapse, on, "timelapse")); err != nil {
		return err
	}

	if on {
		job := camproto.Change{JobName: "timelapse_" + time.Now().UTC().Format("20060102_150405")}
		if err := s.sio.Send(name, "configuration", job.Message("job")); err != nil {
			return err
		}
	}

	return s.askStatus(name)
}

// Restart reboots the camera name. Its session ends, and it connects again.
func (s *Service) Restart(name string) error {
	return s.sio.Send(name, "trigger", camproto.Trigger(camproto.TriggerReboot, "reboot"))
}

func (s *Service) askStatus(name string) error {
	return s.sio.Send(name, "trigger", camproto.Trigger(camproto.TriggerStatus, "status"))
}

// command sends one configuration and waits for the camera's ack.
func (s *Service) command(ctx context.Context, name string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, ackTimeout)
	defer cancel()

	code, err := s.sio.Ask(ctx, name, "configuration", payload)
	if err != nil {
		return err
	}

	if code != sio.AckOK {
		return RefusedError{Code: code}
	}

	return nil
}

// Connected reports whether the camera name has its session open.
func (s *Service) Connected(name string) bool { return s.sio.Connected(name) }

// Drop ends the session of a camera that was removed.
func (s *Service) Drop(name string) { s.sio.Drop(name) }

// Authenticate is sio.Handler: the session's first event.
func (s *Service) Authenticate(payload []byte) (string, bool) {
	auth, err := camproto.DecodeAuthentication(payload)
	if err != nil {
		return "", false
	}

	held, err := s.store.Authenticate(auth.Token, auth.Fingerprint)
	if err != nil {
		s.log.Info("a camera's session was refused", "reason", err.Error())

		return "", false
	}

	return held.Name, true
}

// Ready is sio.Handler: the camera is told its settings and asked how it is
// and what it can do. It sends camera-info only when it starts, so its
// features are asked for on every session.
func (s *Service) Ready(name string) {
	_ = s.Configure(name)
	_ = s.sio.Send(name, "trigger", camproto.Trigger(camproto.TriggerFeatures, "features"))
}

// Configure tells a connected camera its settings again and asks how it is,
// so its report shows them. A camera not connected is told when it connects.
func (s *Service) Configure(name string) error {
	settings := s.store.Settings(name)

	interval := settings.SnapshotInterval
	if interval == 0 {
		interval = s.interval
	}

	if err := s.sio.Send(
		name,
		"configuration",
		camproto.Configuration(interval, settings.Stream, "interval"),
	); err != nil {
		return err
	}

	return s.askStatus(name)
}

// Event is sio.Handler: the camera's report of how it is.
func (s *Service) Event(name, event string, payload []byte) {
	if event == "webrtc" {
		s.onWebRTC(name, payload)

		return
	}

	if event == "features" {
		if features, err := camproto.DecodeFeatures(payload); err == nil {
			s.keepFeatures(name, features)
		}

		return
	}

	if event != "status" {
		return
	}

	status, err := camproto.DecodeStatus(payload)
	if err != nil {
		return
	}

	s.store.Note(name, func(seen *store.Seen) { seen.Status = &status })
}

// keepFeatures stores what the camera said it can do, so a restart of the
// module does not hide its controls until the camera reports them again.
func (s *Service) keepFeatures(name string, features []string) {
	if err := s.store.SetFeatures(name, features); err != nil {
		s.log.Warn("the camera's features were not kept", "camera", name, "error", err)
	}
}

func (s *Service) info(w http.ResponseWriter, r *http.Request) {
	held, ok := s.camera(w, r)
	if !ok {
		return
	}

	var sent struct {
		Config map[string]any `json:"config"`
		Device struct {
			Model string `json:"model"`
		} `json:"device"`
		Features []string `json:"features"`
	}

	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxInfoBytes))
		if err != nil || json.Unmarshal(body, &sent) != nil {
			refuse(w, http.StatusBadRequest, "Body Validation failed")

			return
		}

		s.store.Note(held.Name, func(seen *store.Seen) { seen.Model = sent.Device.Model })

		if sent.Features != nil {
			s.keepFeatures(held.Name, sent.Features)
		}
	}

	if sent.Config == nil {
		sent.Config = map[string]any{}
	}

	if sent.Features == nil {
		sent.Features = []string{}
	}

	name, _ := sent.Config["name"].(string)

	// The shape the cloud answers with. The camera reads the status, and
	// nothing it reads here changes what it does.
	answer(w, http.StatusOK, map[string]any{
		"id": held.Name, "config": sent.Config, "options": map[string]any{}, "capabilities": []string{},
		"features": sent.Features, "sort_order": 0, "name": name, "token": held.Token,
		"origin": "OTHER", "type": "OTHER", "registered": true, "team_id": 0, "printer_uuid": held.Printer,
	})
}

func (s *Service) snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		refuse(w, http.StatusMethodNotAllowed, "a snapshot is sent with PUT")

		return
	}

	held, ok := s.camera(w, r)
	if !ok {
		return
	}

	frame, err := io.ReadAll(io.LimitReader(r.Body, MaxFrameBytes+1))

	switch {
	case err != nil:
		refuse(w, http.StatusBadRequest, "the picture did not arrive whole")

		return
	case len(frame) == 0:
		refuse(w, http.StatusBadRequest, "the picture is empty")

		return
	case len(frame) > MaxFrameBytes:
		refuse(w, http.StatusRequestEntityTooLarge, "the picture is larger than the server takes")

		return
	}

	if err = s.frames.Accept(r.Context(), "", held.Name, frame); err != nil {
		problem := err.Error()
		s.store.Note(held.Name, func(seen *store.Seen) { seen.Problem = problem })
		s.log.WarnContext(
			r.Context(),
			"a camera's picture did not reach the core",
			"camera",
			held.Name,
			"error",
			problem,
		)
		refuse(w, http.StatusBadGateway, "the server could not keep the picture")

		return
	}

	s.store.Note(held.Name, func(seen *store.Seen) {
		seen.LastFrame = seen.LastSeen
		seen.Problem = ""
	})

	answer(w, http.StatusOK, map[string]any{"status": "OK"})
}

// camera is the paired camera a request comes from, or false once it has
// answered the refusal.
func (s *Service) camera(w http.ResponseWriter, r *http.Request) (store.Camera, bool) {
	token := first(r.Header.Get("Token"), r.URL.Query().Get("token"))
	fingerprint := first(r.Header.Get("Fingerprint"), r.URL.Query().Get("fingerprint"))

	if token == "" || fingerprint == "" {
		refuse(w, http.StatusUnauthorized, "Missing camera 'token' or 'fingerprint'")

		return store.Camera{}, false
	}

	held, err := s.store.Authenticate(token, fingerprint)
	if err != nil {
		if !errors.Is(err, store.ErrUnknownToken) {
			s.log.InfoContext(r.Context(), "a camera's request was refused", "reason", err.Error())
		}

		refuse(w, http.StatusForbidden, "Invalid camera 'token' or 'fingerprint'")

		return store.Camera{}, false
	}

	return held, true
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}

// refuse answers in the cloud's error shape.
func refuse(w http.ResponseWriter, code int, message string) {
	answer(w, code, map[string]any{"statusCode": code, "message": message})
}

func answer(w http.ResponseWriter, code int, body any) {
	held, err := json.Marshal(body)
	if err != nil {
		code, held = http.StatusInternalServerError, []byte(`{"status":"ERROR"}`)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(held)
}
