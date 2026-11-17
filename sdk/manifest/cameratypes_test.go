package manifest_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withCameraTypes is the full manifest with a camera_types list added, and the
// operate permission camera.accept takes in place of the full manifest's
// checks.
func withCameraTypes(list string) string {
	document := strings.Replace(full, `"checks": ["operate"],`, ``, 1)
	document = strings.Replace(document, `"permissions": ["view_device", "operate_device"],`,
		`"permissions": ["view_device", "operate"],`, 1)

	return strings.Replace(document, `"lifecycle": "beta",`, `"lifecycle": "beta", "camera_types": `+list+`,`, 1)
}

// ADR-20261106-km2 rule 1: a module offers camera types by name and title.
func TestAModuleOffersCameraTypes(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withCameraTypes(
		`[{"name":"buddy3d","title":"Buddy3D camera"}]`)), decode)
	if err != nil {
		t.Fatalf("a camera type was refused: %v", err)
	}

	if !slices.Equal(held.CameraTypes, []manifest.CameraType{{Name: "buddy3d", Title: "Buddy3D camera"}}) {
		t.Fatalf("read %+v", held.CameraTypes)
	}
}

func TestACameraTypeIsHeldToItsShape(t *testing.T) {
	t.Parallel()

	for name, list := range map[string]string{
		"a name with a slash":    `[{"name":"acme/cam","title":"Cam"}]`,
		"an uppercase name":      `[{"name":"Cam","title":"Cam"}]`,
		"no title":               `[{"name":"cam"}]`,
		"a key it does not read": `[{"name":"cam","title":"Cam","driver":"x"}]`,
		"one name twice":         `[{"name":"cam","title":"A"},{"name":"cam","title":"B"}]`,
	} {
		_, err := manifest.Parse("module.yaml", []byte(withCameraTypes(list)), decode)
		if !errors.Is(err, manifest.ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// A camera of a module's type is fed by the module's process.
func TestACameraTypeNeedsAProcess(t *testing.T) {
	t.Parallel()

	document := strings.Replace(withCameraTypes(`[{"name":"cam","title":"Cam"}]`), `"contract_major": 1,`, ``, 1)
	if _, err := manifest.Parse("module.yaml", []byte(document), decode); !errors.Is(err, manifest.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

// A module that cannot take operate would have every frame refused.
func TestACameraTypeNeedsOperate(t *testing.T) {
	t.Parallel()

	document := strings.Replace(withCameraTypes(`[{"name":"cam","title":"Cam"}]`),
		`"permissions": ["view_device", "operate"],`, `"permissions": ["view_device"],`, 1)
	if _, err := manifest.Parse("module.yaml", []byte(document), decode); !errors.Is(err, manifest.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}
