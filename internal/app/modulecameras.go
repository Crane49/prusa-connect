package app

// The module wire onto the push cameras: one frame handed to one camera. It
// goes through the camera plane's Push, so a module and a device sending over
// HTTP take the same permission and land in the same driver.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// moduleCamera answers sdk/cameraapi's module wire for one module instance.
//
// A frame naming a person runs as that person, whom the supervision plane has
// already vouched for. A frame naming nobody is the module's own work and runs
// as the instance, inside the ceiling the operator wrote. Either way Push
// decides operate on the camera.
type moduleCamera struct {
	cameras  *cameras
	standing moduleStanding
	log      *refusalLog
}

var _ cameraapi.Frames = moduleCamera{}

// moduleCameras binds an instance to the cameras it may hand frames to.
//
// It is a late binding for the reason moduleStorages is: the camera plane is
// built after the supervision plane. A plane run with no cameras, which the
// tests do, reads CodeMethodNotFound rather than a nil.
type moduleCameras struct {
	cameras *cameras
	log     *refusalLog
}

// bind fills the holder. It runs once, before any module starts.
func (m *moduleCameras) bind(cameras *cameras) { m.cameras = cameras }

// forModule is what resourcehost.Config.Cameras takes.
func (m *moduleCameras) forModule(module resourcehost.Module) cameraapi.Frames {
	if m.cameras == nil {
		return nil
	}

	return moduleCamera{cameras: m.cameras, standing: standingOf(module), log: m.log}
}

// Admit answers whether the call may hand the camera a frame, before the
// frame is read.
func (m moduleCamera) Admit(ctx context.Context, params cameraapi.AcceptParams) error {
	who, err := m.who(params)
	if err != nil {
		return err
	}

	return m.refused(ctx, m.cameras.MayPush(ctx, who, params.Camera))
}

// Accept stores one frame as the camera's most recent one.
func (m moduleCamera) Accept(ctx context.Context, params cameraapi.AcceptParams, frame []byte) error {
	who, err := m.who(params)
	if err != nil {
		return err
	}

	// The bytes are checked as the HTTP route checks them: they came from a
	// device on somebody's network and are served back from this origin.
	if !httpapi.IsJPEG(frame) {
		return fmt.Errorf("%w: the frame is not a JPEG", cameraapi.ErrInvalidSnapshot)
	}

	return m.refused(ctx, m.cameras.Push(ctx, who, params.Camera, httpapi.CameraFrame{
		Bytes:     frame,
		MediaType: cameraapi.FormatJPEG.MediaType(),
		Taken:     time.Now().UTC(),
	}))
}

// who is the principal the call runs as, inside the ceiling the operator wrote.
func (m moduleCamera) who(params cameraapi.AcceptParams) (string, error) {
	who, err := m.standing.as(params.Principal, authz.ActionOperate)
	if err != nil {
		return "", fmt.Errorf("%w: %s takes %s, and %w",
			cameraapi.ErrDenied, m.standing.principal, authz.ActionOperate, err)
	}

	return who, nil
}

// refused is the camera plane's answer as the module reads it.
func (m moduleCamera) refused(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if raised, ok := errors.AsType[*fault.Fault](err); ok {
		if raised.Code() == authz.CodeAccessDenied() {
			return fmt.Errorf("%w: %w", cameraapi.ErrDenied, err)
		}

		// Not found, not visible and not a push camera are one answer here,
		// as they are on the HTTP route.
		return fmt.Errorf("%w: %s", cameraapi.ErrNoCamera, noCameraMessage)
	}

	if m.log != nil {
		m.log.refused(ctx, "a module's camera frame was not stored", m.standing.principal, err)
	}

	return err
}

// noCameraMessage is what a module reads for a camera it cannot reach. It names
// no camera, so a module cannot collect the names this server declares.
const noCameraMessage = "no camera of that name takes frames from this module"
