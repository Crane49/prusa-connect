package routes_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/buddy3d-camera/internal/routes"
	"github.com/prusa3d/buddy3d-camera/internal/store"
)

// Pairing answers the token and the srv_cfg that points a camera here, and
// removing the camera forgets it.
func TestAPairedCameraIsListedAndRemoved(t *testing.T) {
	t.Parallel()

	cameras, err := store.Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	handler := routes.New(func() routes.State {
		return routes.State{
			Ready:     true,
			Address:   "10.0.10.199",
			Port:      9443,
			Listening: true,
			Store:     cameras,
		}
	})

	call := func(method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))

		return recorder
	}

	got := call(http.MethodPost, "/cameras", `{"name":"bed-cam","printer":"printer-1"}`)
	if got.Code != http.StatusCreated {
		t.Fatalf("pair: %d %s", got.Code, got.Body)
	}

	var paired routes.Paired
	if err = json.Unmarshal(got.Body.Bytes(), &paired); err != nil {
		t.Fatal(err)
	}

	if len(paired.Token) != 20 || !strings.Contains(paired.SrvCfg, "IMG_URL=10.0.10.199:9443") ||
		!strings.Contains(paired.SrvCfg, "SIO_URL=10.0.10.199:9443") ||
		strings.Contains(paired.SrvCfg, "NTP_SERVER") {
		t.Fatalf("paired %+v", paired)
	}

	if got = call(
		http.MethodPost,
		"/cameras",
		`{"name":"Bed Cam","printer":"printer-1"}`,
	); got.Code != http.StatusBadRequest {
		t.Fatalf("a name the core refuses: %d", got.Code)
	}

	if got = call(http.MethodGet, "/cameras", ""); !strings.Contains(got.Body.String(), `"name":"bed-cam"`) {
		t.Fatalf("list: %s", got.Body)
	}

	if got = call(http.MethodPost, "/cameras/bed-cam/snapshot", ""); got.Code != http.StatusConflict {
		t.Fatalf("a snapshot from a camera not connected: %d", got.Code)
	}

	if got = call(http.MethodDelete, "/cameras/bed-cam", ""); got.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", got.Code)
	}

	if len(cameras.List()) != 0 {
		t.Fatal("the removed camera is still paired")
	}
}
