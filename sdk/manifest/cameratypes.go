package manifest

import (
	"fmt"
	"regexp"
)

// CameraType is one camera type a module offers (ADR-20261106-km2). A person
// picks it when adding a camera, and the core runs the camera on its own
// generic driver: the module feeds it pictures with camera.accept and draws
// what else it knows in the camera's slots. No module code runs in the core.
type CameraType struct {
	// Name is the type's short name. The type a camera is declared with is
	// <namespace>/<name>, so two modules cannot offer the same type.
	Name string
	// Title is what a person reads in the list of camera types.
	Title string
}

// MaxCameraTypes is how many camera types one module may offer.
const MaxCameraTypes = 8

//nolint:gochecknoglobals // a closed table read at parse, never written.
var (
	cameraTypeKeys = []string{"name", "title"}
	// cameraTypeName is the shape a type's short name takes: a lowercase word
	// of letters, digits and dashes, which is also a resource type part.
	cameraTypeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
)

// readCameraTypes reads the camera_types list, and answers nil for a module
// that offers none.
func readCameraTypes(tree map[string]any) ([]CameraType, error) {
	entries, err := nodeList(tree, "", "camera_types")
	if err != nil || entries == nil {
		return nil, err
	}

	if len(entries) > MaxCameraTypes {
		return nil, fmt.Errorf("%w: camera_types names %d types, and the limit is %d",
			ErrInvalid, len(entries), MaxCameraTypes)
	}

	out := make([]CameraType, 0, len(entries))

	for index, entry := range entries {
		where := fmt.Sprintf("camera_types[%d]", index)

		if err = refuseUnknown(entry, where, cameraTypeKeys); err != nil {
			return nil, err
		}

		name, nameErr := requiredText(entry, where, "name")
		if nameErr != nil {
			return nil, nameErr
		}

		if !cameraTypeName.MatchString(name) {
			return nil, fmt.Errorf("%w: %s.name is %q, expected lowercase letters, digits and dashes, "+
				"starting with a letter", ErrInvalid, where, name)
		}

		title, titleErr := requiredText(entry, where, "title")
		if titleErr != nil {
			return nil, titleErr
		}

		for _, held := range out {
			if held.Name == name {
				return nil, fmt.Errorf("%w: camera_types names %q twice", ErrInvalid, name)
			}
		}

		out = append(out, CameraType{Name: name, Title: title})
	}

	return out, nil
}
