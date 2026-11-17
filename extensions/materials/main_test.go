package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/materials/internal/routes"
	"github.com/prusa3d/materials/internal/settings"
)

// TestARequestBeforeConfigureIsRefusedByCode: the host configures before it
// routes, and a request that came first anyway reads a code, not a crash.
func TestARequestBeforeConfigureIsRefusedByCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&database{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/materials", nil))

	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}

	if recorder.Code != http.StatusServiceUnavailable || problem.Code != routes.StoreFailed.Name {
		t.Fatalf("a request before configure answered %d %q", recorder.Code, problem.Code)
	}
}

// TestTheManifestPublishesTheCodesDefault holds module.yaml and this code to
// one default. An operator reads the manifest to learn what a setting does when
// they write nothing, so a manifest that publishes a different number than the
// code uses is documentation that lies.
func TestTheManifestPublishesTheCodesDefault(t *testing.T) {
	manifest, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	declared := `default: "` + strconv.Itoa(settings.DefaultMaxSpools) + `"`
	if !strings.Contains(string(manifest), declared) {
		t.Fatalf("module.yaml does not declare %s for the max_spools setting", declared)
	}
}

// TestNoSettingIsReadFromTheEnvironment keeps every setting in the manifest. A
// setting read from the environment is one an operator cannot find in the
// manifest, cannot see on the settings pane, and cannot set through the API.
func TestNoSettingIsReadFromTheEnvironment(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	if strings.Contains(string(source), "os.Getenv") {
		t.Fatal("main.go reads an environment variable. Declare the setting in module.yaml instead")
	}
}

// TestTheHostAcceptsTheFragment: a fragment the host refuses is a module that
// does not start.
func TestTheHostAcceptsTheFragment(t *testing.T) {
	if _, err := routesapi.ParseFragment(openAPI); err != nil {
		t.Fatal(err)
	}
}
