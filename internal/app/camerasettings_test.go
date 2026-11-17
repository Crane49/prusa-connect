package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// addCameras puts cameras on graph's database the way the camera API does,
// and runs them on plane.
func addCameras(t *testing.T, graph *authz.Store, plane cameraPlane, cameras ...devicestore.Camera) *cameraSettings {
	t.Helper()

	book := cameraBookOn(t, graph, plane, nil)

	for _, camera := range cameras {
		camera.Generation = 1
		require.NoError(t, book.write(t.Context(), camera, devicestore.Camera{}, false))
	}

	return book
}

// cameraBookOn is the camera settings over the database graph was opened on.
func cameraBookOn(t *testing.T, graph *authz.Store, plane cameraPlane, types resource.Declared) *cameraSettings {
	t.Helper()

	host, ok := openedHosts.Load(graph)
	require.True(t, ok, "the graph was not opened through openTestHost")

	if types == nil {
		types = declaredSettings{}
	}

	return &cameraSettings{
		rows: host.(hostStore).devices, graph: graph, plane: plane, types: types, org: bootstrapOrg, log: discard(),
	}
}

// planeLog is the supervision plane as a camera write drives it.
type planeLog struct {
	mu      sync.Mutex
	steps   []resource.Step
	offered map[string]resource.ModuleCameraType
}

func (p *planeLog) Reconcile(_ context.Context, plan resource.Plan) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.steps = append(p.steps, plan.Steps...)

	return nil
}

func (p *planeLog) CameraTypes() map[string]resource.ModuleCameraType { return p.offered }

func (p *planeLog) outcomes() []resource.Outcome {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]resource.Outcome, 0, len(p.steps))
	for _, step := range p.steps {
		out = append(out, step.Outcome)
	}

	return out
}

// cameraTypes is a type table with the generic camera and one driver whose
// password is a credential.
type cameraTypes struct{}

func (cameraTypes) DeclaredOptions(kind ref.Kind, typeName string) ([]extension.Option, bool) {
	switch {
	case kind != ref.KindCamera:
		return nil, false
	case typeName == "generic" || typeName == resource.GenericCameraType:
		return nil, true
	case typeName == "rtsp":
		return []extension.Option{{Key: "password", Type: extension.OptionSecret, Summary: "The password."}}, true
	}

	return nil, false
}

var cameraOperator = "user:" + config.DefaultPrincipal

func create() httpapi.Precondition { return httpapi.Precondition{MustNotExist: true} }

func matching(version string) httpapi.Precondition {
	return httpapi.Precondition{IfMatch: []string{version}}
}

func settingsSpec(typeName, config string) httpapi.CameraSpec {
	return httpapi.CameraSpec{Type: typeName, Config: httpapi.ResourceConfig(config)}
}

// TestACameraIsADeviceRowAndACameraRow holds ADR-20261116-g5x: a camera added
// is a device under its printer, carrying the printer's grants, and runs; a
// change restarts it; a removal takes the device and stops it.
func TestACameraIsADeviceRowAndACameraRow(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := &planeLog{}
	book := cameraBookOn(t, graph, plane, cameraTypes{})

	require.NoError(t, graph.UpsertDevices(t.Context(),
		authz.DeviceRow{ID: "p1", Name: "MK4", Kind: authz.DeviceKindPrinter}))
	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, "p1", authz.RelViewer, "user:ivy")))

	added, created, err := book.PutCamera(
		t.Context(),
		cameraOperator,
		"door",
		settingsSpec("generic", `{"printer":"p1"}`),
		create(),
	)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "1", added.Version)

	ivy, err := authz.ParseSubject("user:ivy")
	require.NoError(t, err)

	reaches, err := graph.Can(t.Context(), ivy, authz.ActionObserve, deviceObject(t, "door"))
	require.NoError(t, err)
	require.True(t, reaches, "the printer's viewer sees its camera")

	_, _, err = book.PutCamera(
		t.Context(),
		cameraOperator,
		"door",
		settingsSpec("generic", `{"printer":"p1"}`),
		create(),
	)
	require.True(t, fault.HasCode(err, httpfault.CodeVersionConflict()), "got %v", err)

	_, _, err = book.PutCamera(
		t.Context(),
		cameraOperator,
		"door",
		settingsSpec("rtsp", `{"printer":"p1"}`),
		matching("1"),
	)
	require.True(t, fault.HasCode(err, httpfault.CodeFieldImmutable()), "got %v", err)

	changed, created, err := book.PutCamera(t.Context(), cameraOperator, "door",
		settingsSpec("generic", `{"printer":"p1","snapshot_url":"http://cam/shot"}`), matching("1"))
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "2", changed.Version)

	require.Error(t, book.DeleteCamera(t.Context(), cameraOperator, "door", matching("1")), "a stale version")
	require.NoError(t, book.DeleteCamera(t.Context(), cameraOperator, "door", matching("2")))

	reaches, err = graph.Can(t.Context(), ivy, authz.ActionObserve, deviceObject(t, "door"))
	require.NoError(t, err)
	require.False(t, reaches, "the device went with the camera")

	listed, err := book.CameraSettings(t.Context(), cameraOperator)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Equal(t, []resource.Outcome{resource.OutcomeStart, resource.OutcomeRestart, resource.OutcomeStop},
		plane.outcomes())
}

// TestACameraIsWrittenOnlyByWhoConfiguresCameras holds ADR-20260826-5yj rule
// 18: a camera's settings are granted on the camera kind.
func TestACameraIsWrittenOnlyByWhoConfiguresCameras(t *testing.T) {
	t.Parallel()

	book := cameraBookOn(t, newGraph(t), &planeLog{}, cameraTypes{})

	_, _, err := book.PutCamera(t.Context(), "user:nobody", "door", settingsSpec("generic", `{}`), create())
	require.True(t, fault.HasCode(err, authz.CodeAccessDenied()), "got %v", err)

	listed, err := book.CameraSettings(t.Context(), "user:nobody")
	require.NoError(t, err)
	require.Empty(t, listed)
}

// TestACameraIsJudgedByItsType holds ADR-20261106-km2: a module's type is
// judged as the generic camera, an unknown type is refused, and a credential
// reads back masked and is kept when the mask is written back.
func TestACameraIsJudgedByItsType(t *testing.T) {
	t.Parallel()

	plane := &planeLog{offered: map[string]resource.ModuleCameraType{
		"com.github.aux.acme.cams/buddy3d": {Type: "com.github.aux.acme.cams/buddy3d"},
	}}
	book := cameraBookOn(t, newGraph(t), plane, cameraTypes{})

	_, _, err := book.PutCamera(t.Context(), cameraOperator, "bed",
		settingsSpec("com.github.aux.acme.cams/buddy3d", `{"printer":"p1"}`), create())
	require.NoError(t, err)

	_, _, err = book.PutCamera(t.Context(), cameraOperator, "shed", settingsSpec("acme/other", `{}`), create())
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "got %v", err)

	_, _, err = book.PutCamera(t.Context(), cameraOperator, "shed", settingsSpec("generic", `{"printer":7}`), create())
	require.True(t, fault.HasCode(err, httpfault.CodeRequestInvalid()), "got %v", err)

	added, _, err := book.PutCamera(
		t.Context(),
		cameraOperator,
		"gate",
		settingsSpec("rtsp", `{"password":"hunter2"}`),
		create(),
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"password":"`+resource.RedactedValue+`"}`, string(added.Config))

	_, _, err = book.PutCamera(t.Context(), cameraOperator, "gate",
		settingsSpec("rtsp", `{"password":"`+resource.RedactedValue+`","printer":"p2"}`), matching(added.Version))
	require.NoError(t, err)

	held, _, err := book.find(t.Context(), "gate")
	require.NoError(t, err)
	require.JSONEq(t, `{"password":"hunter2","printer":"p2"}`, string(held.Config))
}

// TestARemovedPrinterTakesItsCameras holds the retirement: a printer an
// cameraOperator removed takes the cameras that watch it, and leaves the others.
func TestARemovedPrinterTakesItsCameras(t *testing.T) {
	t.Parallel()

	plane := &planeLog{}
	book := addCameras(t, newGraph(t), plane,
		devicestore.Camera{ID: "bed", Type: "generic", Config: json.RawMessage(`{"printer":"gone"}`)},
		devicestore.Camera{ID: "side", Type: "generic", Config: json.RawMessage(`{"printer":"kept"}`)})

	book.withdraw(t.Context(), "gone")

	rows, err := book.rows.Cameras(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "side", rows[0].ID)
}

// TestAnOlderBuildsCamerasMoveIntoTheDatabaseOnce is the upgrade. The
// overlay's cameras move and leave the overlay. A camera of the cameraOperator's
// file moves while the device row the older build wrote for it carries none,
// so one removed after the move stays removed.
func TestAnOlderBuildsCamerasMoveIntoTheDatabaseOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := resource.Paths{File: filepath.Join(dir, "config.yaml"), Overlay: filepath.Join(dir, "resources.json")}

	write(t, paths.File, `
resources:
  camera:
    door:
      type: mjpeg
      config:
        printer: p1
    fresh:
      type: mjpeg
`)
	write(t, paths.Overlay, `{"version": 1, "instances": [
  {"kind": "camera", "name": "bed", "type": "push", "enabled": true, "required": false,
   "config": {"printer": "p2"}}]}`)

	graph := newGraph(t)
	plane := &planeLog{}
	book := cameraBookOn(t, graph, plane, nil)

	// What the older build's sync left: a device row for each declared camera.
	require.NoError(t, graph.DeclareDevice(t.Context(), authz.DeviceKindCamera,
		authz.DeclaredDevice{ID: "door", Name: "door", Parent: "p1"}, nil))
	require.NoError(t, graph.DeclareDevice(t.Context(), authz.DeviceKindCamera,
		authz.DeclaredDevice{ID: "bed", Name: "bed", Parent: "p2"}, nil))

	require.NoError(t, book.start(t.Context(), paths))

	rows, err := book.rows.Cameras(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 2, "fresh has no device an older build wrote, so it is not moved")
	require.Equal(t, "bed", rows[0].ID)
	require.Equal(t, "door", rows[1].ID)
	require.JSONEq(t, `{"printer":"p1"}`, string(rows[1].Config))

	overlay, err := os.ReadFile(paths.Overlay)
	require.NoError(t, err)
	require.NotContains(t, string(overlay), "camera")

	require.NoError(t, book.remove(t.Context(), rows[1]))
	require.NoError(t, book.start(t.Context(), paths))

	rows, err = book.rows.Cameras(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1, "the file's block is not read again")
}

func deviceObject(t *testing.T, id string) authz.Object {
	t.Helper()

	object, err := authz.NewObject(authz.TypeDevice, id)
	require.NoError(t, err)

	return object
}
