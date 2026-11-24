package routes_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/buddy3d-camera/internal/camproto"
	"github.com/prusa3d/buddy3d-camera/internal/device"
	"github.com/prusa3d/buddy3d-camera/internal/routes"
	"github.com/prusa3d/buddy3d-camera/internal/store"
)

// fakeDevice is a camera server whose cameras are connected as it says, and
// which records the answer it is handed.
type fakeDevice struct {
	connected  bool
	answered   string
	configured []string
	// commands is each command the camera was sent, and refuse the code it
	// answers them with.
	commands []string
	refuse   int
}

func (d *fakeDevice) Move(_ context.Context, _, direction string, angle uint32) error {
	return d.command(fmt.Sprintf("move %s %d", direction, angle))
}

func (d *fakeDevice) Change(_ context.Context, _ string, change camproto.Change) error {
	return d.command(fmt.Sprintf("change %+v", change))
}

func (d *fakeDevice) Timelapse(_ string, on bool) error {
	return d.command(fmt.Sprintf("timelapse %t", on))
}
func (d *fakeDevice) Restart(string) error { return d.command("restart") }

func (d *fakeDevice) command(what string) error {
	if d.refuse != 0 {
		return device.RefusedError{Code: d.refuse}
	}

	d.commands = append(d.commands, what)

	return nil
}

func (*fakeDevice) Snapshot(string) error   { return nil }
func (d *fakeDevice) Connected(string) bool { return d.connected }
func (*fakeDevice) Drop(string)             {}

func (d *fakeDevice) Configure(name string) error {
	d.configured = append(d.configured, name)

	return nil
}
func (*fakeDevice) StopLive(_, _ string) error { return nil }

func (*fakeDevice) StartLive(context.Context, string) (string, string, error) {
	return "s1", "v=0\r\n", nil
}

func (d *fakeDevice) AnswerLive(_, _, answer string) error {
	d.answered = answer

	return nil
}

// A live view is started only for a person the core lets see the camera,
// and only while the camera is connected. A person who may not see it is
// told it does not exist.
func TestALiveViewIsForAPersonWhoMaySeeTheCamera(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	device := &fakeDevice{}
	handler := routes.New(func() routes.State {
		return routes.State{
			Ready: true, Store: cameras, Device: device,
			May: func(_ context.Context, principal, action, camera string) (bool, error) {
				return principal == "user:ada" && action == "observe" && camera == "bed-cam", nil
			},
		}
	})

	call := func(principal, method, path, body string) *httptest.ResponseRecorder {
		request, buildErr := routesapi.Request{
			Method: method, Path: path, Body: []byte(body), Principal: principal,
		}.HTTP(t.Context())
		if buildErr != nil {
			t.Fatal(buildErr)
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		return recorder
	}

	if got := call("user:bob", http.MethodPost, "/cameras/bed-cam/live", ""); got.Code != http.StatusNotFound {
		t.Fatalf("a person who may not see the camera: %d", got.Code)
	}

	if got := call("user:ada", http.MethodPost, "/cameras/bed-cam/live", ""); got.Code != http.StatusConflict {
		t.Fatalf("a camera not connected: %d", got.Code)
	}

	device.connected = true

	got := call("user:ada", http.MethodPost, "/cameras/bed-cam/live", "")
	if got.Code != http.StatusCreated || !strings.Contains(got.Body.String(), `"session":"s1"`) {
		t.Fatalf("start: %d %s", got.Code, got.Body)
	}

	if got = call(
		"user:ada",
		http.MethodPost,
		"/cameras/bed-cam/live/s1/answer",
		`{"answer":"v=0"}`,
	); got.Code != http.StatusNoContent ||
		device.answered != "v=0" {
		t.Fatalf("answer: %d, handed %q", got.Code, device.answered)
	}

	if got = call("user:ada", http.MethodDelete, "/cameras/bed-cam/live/s1", ""); got.Code != http.StatusNoContent {
		t.Fatalf("stop: %d", got.Code)
	}

	if err = cameras.SetSettings("bed-cam", store.Settings{Stream: "rtsp"}); err != nil {
		t.Fatal(err)
	}

	if got = call("user:ada", http.MethodPost, "/cameras/bed-cam/live", ""); got.Code != http.StatusConflict ||
		!strings.Contains(got.Body.String(), "LIVE_OFF") {
		t.Fatalf("a camera streaming RTSP: %d %s", got.Code, got.Body)
	}
}

// A person who may operate the camera changes its settings, and a connected
// camera is told at once. A person who may only see it is told it does not
// exist, and an interval the camera does not take is refused.
func TestACamerasSettingsAreForAPersonWhoMayOperateIt(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	device := &fakeDevice{connected: true}
	handler := routes.New(func() routes.State {
		return routes.State{
			Ready: true, Store: cameras, Device: device,
			May: func(_ context.Context, principal, action, _ string) (bool, error) {
				return principal == "user:ada" || action == "observe", nil
			},
		}
	})

	call := func(principal, body string) *httptest.ResponseRecorder {
		request, buildErr := routesapi.Request{
			Method: http.MethodPut, Path: "/cameras/bed-cam/settings", Body: []byte(body), Principal: principal,
		}.HTTP(t.Context())
		if buildErr != nil {
			t.Fatal(buildErr)
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		return recorder
	}

	if got := call("user:bob", `{"snapshot_interval":10}`); got.Code != http.StatusNotFound {
		t.Fatalf("a person who may only see the camera: %d", got.Code)
	}

	if got := call("user:ada", `{"snapshot_interval":256}`); got.Code != http.StatusBadRequest {
		t.Fatalf("an interval the camera does not take: %d", got.Code)
	}

	if got := call("user:ada", `{"snapshot_interval":10,"stream":"hls"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("a stream the camera does not run: %d", got.Code)
	}

	if got := call("user:ada", `{"snapshot_interval":10,"stream":"rtsp"}`); got.Code != http.StatusOK {
		t.Fatalf("set: %d %s", got.Code, got.Body)
	}

	if kept := cameras.Settings(
		"bed-cam",
	); kept != (store.Settings{SnapshotInterval: 10, Stream: "rtsp"}) ||
		len(device.configured) != 1 {
		t.Fatalf("kept %+v, the camera was told %d times", kept, len(device.configured))
	}

	if list := cameras.List(); list[0].Settings.SnapshotInterval != 10 {
		t.Fatalf("the listing shows %+v", list[0].Settings)
	}
}

// A person who may operate the camera turns it, restarts it and changes what
// it keeps. A person who may only see it is told it does not exist, a value
// out of bounds never reaches the camera, and the camera's refusal is passed
// on.
func TestACameraIsTurnedByAPersonWhoMayOperateIt(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = cameras.Pair("bed-cam", "printer-1", ""); err != nil {
		t.Fatal(err)
	}

	camera := &fakeDevice{connected: true}
	handler := routes.New(func() routes.State {
		return routes.State{
			Ready: true, Store: cameras, Device: camera,
			May: func(_ context.Context, principal, action, _ string) (bool, error) {
				return principal == "user:ada" || action == "observe", nil
			},
		}
	})

	call := func(principal, body string) *httptest.ResponseRecorder {
		request, buildErr := routesapi.Request{
			Method: http.MethodPost, Path: "/cameras/bed-cam/control", Body: []byte(body), Principal: principal,
		}.HTTP(t.Context())
		if buildErr != nil {
			t.Fatal(buildErr)
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		return recorder
	}

	if got := call("user:bob", `{"mode":"night"}`); got.Code != http.StatusNotFound {
		t.Fatalf("a person who may only see the camera: %d", got.Code)
	}

	for _, body := range []string{
		`{}`, `{"mode":"night","move":{"direction":"left","angle":10}}`, `{"mode":"dusk"}`,
		`{"move":{"direction":"sideways","angle":10}}`, `{"move":{"direction":"left","angle":91}}`,
		`{"move":{"direction":"left","angle":0}}`, `{"volume":101}`, `{"video_quality":"4k"}`,
		`{"timelapse_interval":3601}`, `{"name":" bed"}`, `{"name":"a name far longer than the camera keeps"}`,
		`{"restart":false}`,
	} {
		if got := call("user:ada", body); got.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", body, got.Code)
		}
	}

	if got := call("user:ada", `{"move":{"direction":"left","angle":30}}`); got.Code != http.StatusNoContent {
		t.Fatalf("turn: %d %s", got.Code, got.Body)
	}

	for _, body := range []string{
		`{"mode":"night"}`, `{"volume":40}`, `{"timelapse":false}`, `{"timelapse_interval":60}`,
		`{"video_quality":"fhd"}`, `{"name":"Bed"}`, `{"restart":true}`,
	} {
		if got := call("user:ada", body); got.Code != http.StatusNoContent {
			t.Fatalf("%s: %d %s", body, got.Code, got.Body)
		}
	}

	want := "move left 30, change {Mode:night Volume:0 TimelapseInterval:0 VideoQuality: Name: JobName:}, " +
		"change {Mode: Volume:40 TimelapseInterval:0 VideoQuality: Name: JobName:}, timelapse false, " +
		"change {Mode: Volume:0 TimelapseInterval:60 VideoQuality: Name: JobName:}, " +
		"change {Mode: Volume:0 TimelapseInterval:0 VideoQuality:fhd Name: JobName:}, " +
		"change {Mode: Volume:0 TimelapseInterval:0 VideoQuality: Name:Bed JobName:}, restart"
	if got := strings.Join(camera.commands, ", "); got != want {
		t.Fatalf("the camera was sent\n %s\nwant\n %s", got, want)
	}

	camera.refuse = 102

	got := call("user:ada", `{"mode":"day"}`)
	if got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "102") {
		t.Fatalf("a refusal: %d %s", got.Code, got.Body)
	}
}
