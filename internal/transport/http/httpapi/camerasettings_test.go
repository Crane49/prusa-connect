package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubCameraSettings adds on If-None-Match: * and changes otherwise, and keeps
// what it was given.
type stubCameraSettings struct {
	listed []CameraSetting
	put    CameraSpec
	pre    Precondition
	gone   string
}

func (s *stubCameraSettings) CameraSettings(context.Context, string) ([]CameraSetting, error) {
	return s.listed, nil
}

func (s *stubCameraSettings) PutCamera(
	_ context.Context, _, name string, desired CameraSpec, pre Precondition,
) (CameraSetting, bool, error) {
	s.put, s.pre = desired, pre

	return CameraSetting{Name: name, CameraSpec: desired, Version: "1"}, pre.MustNotExist, nil
}

func (s *stubCameraSettings) DeleteCamera(_ context.Context, _, name string, _ Precondition) error {
	s.gone = name

	return nil
}

func TestACameraWriteIsFencedAndAnswersItsVersion(t *testing.T) {
	t.Parallel()

	settings := &stubCameraSettings{}
	handler := serverWithCameraSettings(t, settings).Handler()

	body := `{"type":"generic","config":{"printer":"p1"}}`

	blind := call(t, handler, http.MethodPut, camerasPath+"/door", body)
	requireProblem(t, blind, http.StatusPreconditionRequired)

	req := httptest.NewRequest(http.MethodPut, camerasPath+"/door", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-None-Match", "*")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, `"1"`, rec.Header().Get("ETag"))
	require.True(t, settings.pre.MustNotExist)
	require.Equal(t, "generic", settings.put.Type)
	require.JSONEq(t, `{"printer":"p1"}`, string(settings.put.Config))

	req = httptest.NewRequest(http.MethodDelete, camerasPath+"/door", nil)
	req.Header.Set("If-Match", `"1"`)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, "door", settings.gone)
}

func serverWithCameraSettings(t *testing.T, settings CameraSettings) *Server {
	t.Helper()

	server, err := New(Config{
		Version:        "test",
		Principals:     fixedPrincipal("user:operator"),
		Resources:      seededStore(),
		Presets:        seededPresets(),
		Endpoints:      seededEndpoints(),
		Grants:         &fakeGrantStore{},
		Sessions:       newFakeSessions(),
		Accounts:       &fakeAccounts{},
		Readiness:      &fakeReadiness{report: readyReport()},
		Devices:        noPrinters{},
		Cameras:        &stubCameras{},
		CameraSettings: settings,
		Stores:         noStorages{},
		Events:         noEvents{},
	})
	require.NoError(t, err)

	return server
}
