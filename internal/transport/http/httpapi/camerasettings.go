package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// cameraSettingsPath lists what each camera is set up as, beside the camera
// paths that serve its pictures.
const (
	cameraSettingsPath = "/api/v1/camera-settings"
	cameraPath         = camerasPath + "/{camera}"
)

// CameraSettings is the cameras people add, change and remove. A camera is a
// device kept in the host database beside the printers (ADR-20261116-g5x),
// and these are its settings. It is declared here because this is where it is
// consumed, and the composition root in internal/app satisfies it.
type CameraSettings interface {
	// CameraSettings is every camera's settings, sorted by name, or none for
	// a caller who may not read them.
	CameraSettings(ctx context.Context, principal string) ([]CameraSetting, error)
	// PutCamera adds a camera or changes one, and reports whether it added.
	PutCamera(ctx context.Context, principal, name string, desired CameraSpec, pre Precondition) (
		CameraSetting, bool, error)
	// DeleteCamera removes a camera, with its device row and every grant on it.
	DeleteCamera(ctx context.Context, principal, name string, pre Precondition) error
}

// CameraSpec is what a write sets a camera up as.
type CameraSpec struct {
	Type   string         `json:"type"   minLength:"1" maxLength:"160" doc:"generic, a core driver, or a type an installed module offers as <namespace>/<name>. Fixed once the camera exists: changing it is remove and add."`
	Config ResourceConfig `json:"config"                               doc:"The camera's settings. printer names the printer it watches, and a grant on that printer reaches the camera. A credential reads back as xxxxx, and writing xxxxx keeps the stored one."`
}

// CameraSetting is one camera's settings as a read answers them.
type CameraSetting struct {
	CameraSpec

	Name string `json:"name" doc:"The camera's name, which is its device id."`

	Version string `json:"version" doc:"The version to quote in If-Match on the next write, as the ETag of a write answers it."`
}

type cameraSettingsOutput struct {
	Body struct {
		Items []CameraSetting `json:"items" doc:"Every camera's settings, by name."`
	}
}

type putCameraInput struct {
	CameraRef

	IfMatch     string `header:"If-Match"      doc:"The version last read, quotes included."`
	IfNoneMatch string `header:"If-None-Match" doc:"Send * to add only, failing if the camera already exists."`

	Body CameraSpec
}

type putCameraOutput struct {
	Status int
	ETag   string `header:"ETag" doc:"The version to quote in If-Match on the next write."`
	Body   CameraSetting
}

type deleteCameraInput struct {
	CameraRef

	IfMatch string `header:"If-Match" doc:"The version last read, quotes included."`
}

type deleteCameraOutput struct {
	Status int
}

// registerCameraSettings publishes the settings of the cameras. Nil publishes
// none of their paths.
func registerCameraSettings(api huma.API, settings CameraSettings) {
	if settings == nil {
		return
	}

	register(api, huma.Operation{
		OperationID: "list-camera-settings",
		Method:      http.MethodGet,
		Path:        cameraSettingsPath,
		Summary:     "What each camera is set up as",
		Description: "Every camera's type and settings, with the version a write quotes. It takes " +
			"view_resource on resource_kind:camera, and a caller without it is answered an empty list.",
		Tags:   []string{"cameras"},
		Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*cameraSettingsOutput, error) {
		listed, err := settings.CameraSettings(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &cameraSettingsOutput{}
		out.Body.Items = listed

		if out.Body.Items == nil {
			out.Body.Items = []CameraSetting{}
		}

		return out, nil
	})

	register(api, huma.Operation{
		OperationID: "put-camera",
		Method:      http.MethodPut,
		Path:        cameraPath,
		Summary:     "Add or change a camera",
		Description: "A camera is a device beside the printers: adding one registers it, under the printer " +
			"its printer setting names, and starts it. The printer's grants are copied onto it the first " +
			"time it is put on that printer. If-None-Match: * adds, and the version last read in If-Match " +
			"changes; a write with neither answers 428. It takes configure_resource on " +
			"resource_kind:camera. A write that adds answers 201, one that changes answers 200.",
		Tags:          []string{"cameras"},
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusPreconditionFailed,
			http.StatusUnprocessableEntity,
			http.StatusPreconditionRequired,
		},
	}, func(ctx context.Context, in *putCameraInput) (*putCameraOutput, error) {
		pre, err := precondition(in.IfMatch, in.IfNoneMatch, false)
		if err != nil {
			return nil, err
		}

		applied, created, err := settings.PutCamera(ctx, principalFrom(ctx), in.Camera, in.Body, pre)
		if err != nil {
			return nil, err
		}

		etag, err := validatedETag(applied.Version)
		if err != nil {
			return nil, err
		}

		out := &putCameraOutput{Status: http.StatusOK, ETag: etag, Body: applied}
		if created {
			out.Status = http.StatusCreated
		}

		return out, nil
	})

	documentCreatedResponse(api, cameraPath)

	register(api, huma.Operation{
		OperationID: "delete-camera",
		Method:      http.MethodDelete,
		Path:        cameraPath,
		Summary:     "Remove a camera",
		Description: "Stops the camera and removes its device row with every grant written on it. It takes " +
			"configure_resource on resource_kind:camera and the version last read in If-Match.",
		Tags:          []string{"cameras"},
		DefaultStatus: http.StatusNoContent,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusPreconditionFailed,
			http.StatusPreconditionRequired,
		},
	}, func(ctx context.Context, in *deleteCameraInput) (*deleteCameraOutput, error) {
		pre, err := precondition(in.IfMatch, "", false)
		if err != nil {
			return nil, err
		}

		if err = settings.DeleteCamera(ctx, principalFrom(ctx), in.Camera, pre); err != nil {
			return nil, err
		}

		return &deleteCameraOutput{Status: http.StatusNoContent}, nil
	})
}
