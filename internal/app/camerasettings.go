package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// cameraPlane is the supervision plane as the camera settings drive it.
type cameraPlane interface {
	Reconcile(ctx context.Context, plan resource.Plan) error
	CameraTypes() map[string]resource.ModuleCameraType
}

// cameraSettings is the cameras people add. Each is a device row in the graph
// beside the printers and a camera row in the host database, written in one
// transaction, and the supervision plane runs it (ADR-20261116-g5x).
type cameraSettings struct {
	// mu orders the writes, so a version is read and moved by one at a time.
	mu    sync.Mutex
	rows  *devicestore.Store
	graph *authz.Store
	plane cameraPlane
	types resource.Declared
	org   string
	log   *slog.Logger
}

var _ httpapi.CameraSettings = (*cameraSettings)(nil)

// CameraSettings is every camera's settings, for a caller who may read them.
func (c *cameraSettings) CameraSettings(ctx context.Context, principal string) ([]httpapi.CameraSetting, error) {
	allowed, err := c.may(ctx, principal, authz.ActionViewResource)
	if err != nil || !allowed {
		return nil, err
	}

	rows, err := c.rows.Cameras(ctx)
	if err != nil {
		return nil, httpfault.Unexpected("read the cameras").Wrap(err)
	}

	out := make([]httpapi.CameraSetting, 0, len(rows))
	for _, row := range rows {
		out = append(out, c.setting(row))
	}

	return out, nil
}

// PutCamera adds a camera or changes one.
func (c *cameraSettings) PutCamera(
	ctx context.Context, principal, name string, desired httpapi.CameraSpec, pre httpapi.Precondition,
) (httpapi.CameraSetting, bool, error) {
	instance, err := c.authorize(ctx, principal, name)
	if err != nil {
		return httpapi.CameraSetting{}, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists, err := c.find(ctx, name)
	if err != nil {
		return httpapi.CameraSetting{}, false, err
	}

	if !pre.Holds(exists, cameraVersion(current)) {
		return httpapi.CameraSetting{}, false, httpfault.VersionConflict(instance.String(), cameraVersion(current))
	}

	if exists && desired.Type != current.Type {
		return httpapi.CameraSetting{}, false, httpfault.FieldImmutable("body.type")
	}

	options, known := c.types.DeclaredOptions(ref.KindCamera,
		resource.DriverType(ref.KindCamera, desired.Type, c.plane.CameraTypes()))
	if !known && !exists {
		return httpapi.CameraSetting{}, false, httpfault.RequestInvalid("body.type",
			"generic, a core camera driver or a type an installed module offers",
			"this build has no camera type named "+strconv.Quote(desired.Type))
	}

	settings, err := validCameraConfig(instance, options, known, json.RawMessage(desired.Config), current.Config)
	if err != nil {
		return httpapi.CameraSetting{}, false, err
	}

	next := devicestore.Camera{ID: name, Type: desired.Type, Config: settings, Generation: current.Generation + 1}
	if err = c.write(ctx, next, current, exists); err != nil {
		return httpapi.CameraSetting{}, false, err
	}

	return c.setting(next), !exists, nil
}

// DeleteCamera removes a camera with its device row and every grant on it.
func (c *cameraSettings) DeleteCamera(
	ctx context.Context, principal, name string, pre httpapi.Precondition,
) error {
	instance, err := c.authorize(ctx, principal, name)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists, err := c.find(ctx, name)
	if err != nil {
		return err
	}

	if !exists {
		return httpapi.CameraNotFound(name)
	}

	if !pre.Holds(exists, cameraVersion(current)) {
		return httpfault.VersionConflict(instance.String(), cameraVersion(current))
	}

	return c.remove(ctx, current)
}

// withdraw removes the cameras that watch a printer an operator removed, so
// none keeps running with no page to reach it from.
func (c *cameraSettings) withdraw(ctx context.Context, printer string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rows, err := c.rows.Cameras(ctx)
	if err != nil {
		c.log.WarnContext(ctx, "the cameras of a removed printer could not be read", "printer", printer,
			"error", err.Error())

		return
	}

	for _, row := range rows {
		if watched, _ := cameraPrinter(row.ID, row.Config); watched != printer {
			continue
		}

		if err = c.remove(ctx, row); err != nil {
			c.log.WarnContext(ctx, "a removed printer's camera could not be removed", "camera", row.ID,
				"printer", printer, "error", err.Error())
		}
	}
}

// reconcileDeclared starts what the configuration declares, then the cameras:
// after the modules, so a camera of a module's type starts once it is offered.
func reconcileDeclared(
	ctx context.Context, logger *slog.Logger, core config.Core, store *resource.Store, cameras *cameraSettings,
	configPath string,
) error {
	if err := store.Reload(ctx); err != nil {
		return fmt.Errorf("reconcile the declared resources: %w", err)
	}

	logger.InfoContext(ctx, "resources reconciled", "declared", len(store.Document().Instances))

	if err := cameras.start(ctx, resource.Paths{File: configPath, Overlay: core.Overlay}); err != nil {
		return fmt.Errorf("start the cameras: %w", err)
	}

	logModulesOff(ctx, logger, core, store)
	logOverCommit(ctx, logger, store)

	return nil
}

// start moves an older build's cameras out of the files and runs every camera.
func (c *cameraSettings) start(ctx context.Context, paths resource.Paths) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.moveFromFiles(ctx, paths); err != nil {
		return err
	}

	rows, err := c.rows.Cameras(ctx)
	if err != nil {
		return err
	}

	var plan resource.Plan
	for _, row := range rows {
		plan.Steps = append(plan.Steps, resource.Step{
			Outcome: resource.OutcomeStart, Ref: cameraRef(row.ID), Desired: declaration(row),
		})
	}

	return c.plane.Reconcile(ctx, plan)
}

// moveFromFiles writes the cameras an older build declared in a file into the
// database, once. An overlay camera is moved and taken out of the overlay. A
// camera of the operator's file is moved while its device row, which that
// older build wrote, carries no camera; after that the block is not read.
func (c *cameraSettings) moveFromFiles(ctx context.Context, paths resource.Paths) error {
	declared, failures, err := resource.FileCameras(paths)
	if err != nil {
		return err
	}

	for _, failure := range failures {
		c.log.ErrorContext(ctx, "a camera of the configuration file does not read and was not moved",
			"key", failure.Key, "error", failure.Err.Error())
	}

	bare, err := c.rows.BareCameras(ctx)
	if err != nil {
		return err
	}

	for _, camera := range declared {
		_, held, findErr := c.find(ctx, camera.Name)
		if findErr != nil {
			return findErr
		}

		fromFile := camera.Source == resource.SourceFile

		switch {
		case held && !fromFile:
		case held || !slices.Contains(bare, camera.Name) && fromFile:
			c.log.WarnContext(ctx, "the configuration file declares a camera, and cameras are added in the app. "+
				"Remove resources.camera."+camera.Name, "file", paths.File)
		default:
			row := devicestore.Camera{ID: camera.Name, Type: camera.Type, Config: camera.Config, Generation: 1}
			if err = c.write(ctx, row, devicestore.Camera{}, false); err != nil {
				return err
			}

			c.log.InfoContext(ctx, "a camera was moved into the database", "camera", camera.Name,
				"from", camera.Source.String())
		}
	}

	if err = resource.DropOverlayCameras(paths); err != nil {
		return err
	}

	// A camera device no camera carries is one an older build declared and
	// nothing moved, such as a block that did not read.
	if bare, err = c.rows.BareCameras(ctx); err != nil {
		return err
	}

	for _, id := range bare {
		if err = c.graph.RemoveDevice(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

// write registers the camera's device and writes its row in one transaction,
// copies the printer's grants the first time it is put on that printer, and
// runs it.
func (c *cameraSettings) write(ctx context.Context, next, current devicestore.Camera, exists bool) error {
	printer, _ := cameraPrinter(next.ID, next.Config)

	err := c.graph.DeclareDevice(ctx, ref.DeviceKindCamera,
		authz.DeclaredDevice{ID: next.ID, Name: next.ID, Org: c.placement(printer), Parent: printer},
		func(ctx context.Context, tx *sql.Tx) error { return c.rows.PutCamera(ctx, tx, next) })
	if err != nil {
		return httpfault.Unexpected("write the camera").Wrap(err)
	}

	if was, _ := cameraPrinter(current.ID, current.Config); printer != "" && (!exists || was != printer) {
		c.copyPrinterGrants(ctx, printer, next.ID)
	}

	previous := resource.Document{}
	if exists {
		previous.Instances = []resource.Declaration{declaration(current)}
	}

	c.reconcile(ctx, resource.Diff(previous, resource.Document{Instances: []resource.Declaration{declaration(next)}}))

	return nil
}

// remove takes the camera's device out of the graph, which takes its row with
// it, and stops it.
func (c *cameraSettings) remove(ctx context.Context, current devicestore.Camera) error {
	if err := c.graph.RemoveDevice(ctx, current.ID); err != nil {
		return httpfault.Unexpected("remove the camera").Wrap(err)
	}

	c.reconcile(ctx, resource.Plan{Steps: []resource.Step{{
		Outcome: resource.OutcomeStop, Ref: cameraRef(current.ID), Previous: declaration(current),
	}}})

	return nil
}

// reconcile hands the plan to the plane on a context the caller cannot end:
// the camera is written, and a caller who hung up must not leave it unstarted.
// A failure is the camera's status, which the listing shows.
func (c *cameraSettings) reconcile(ctx context.Context, plan resource.Plan) {
	if err := c.plane.Reconcile(context.WithoutCancel(ctx), plan); err != nil {
		c.log.WarnContext(ctx, "a camera did not start", "error", err.Error())
	}
}

// copyPrinterGrants gives a camera the grants of the printer it is put on. It
// is a snapshot, so a grant withdrawn from the camera later stays withdrawn; a
// grant written on the printer later reaches the camera down the parent edge.
// A failure is logged: the camera is written, and the edge already carries the
// printer's grants.
func (c *cameraSettings) copyPrinterGrants(ctx context.Context, printer, camera string) {
	copied, err := c.graph.CopyDeviceGrants(ctx, printer, camera)
	if err != nil {
		c.log.WarnContext(ctx, "a camera could not be given the grants of the printer it watches",
			"camera", camera, "printer", printer, "error", err.Error())

		return
	}

	if copied > 0 {
		c.log.InfoContext(ctx, "a camera was put on a printer and took its grants",
			"camera", camera, "printer", printer, "grants", copied)
	}
}

// placement is the org a camera is written into: none when it sits inside a
// printer, which it takes its tenancy from, and the deployment's when it sits
// inside nothing, so somebody administers it.
func (c *cameraSettings) placement(printer string) string {
	if printer != "" {
		return ""
	}

	return c.org
}

// authorize names the camera and refuses a caller who may not configure
// cameras.
func (c *cameraSettings) authorize(ctx context.Context, principal, name string) (ref.Ref, error) {
	instance, err := ref.New(ref.KindCamera, name)
	if err != nil {
		return ref.Ref{}, httpfault.RequestInvalid("path.camera", "a DNS-1123 label", err.Error())
	}

	allowed, err := c.may(ctx, principal, authz.ActionConfigureResource)
	if err != nil {
		return ref.Ref{}, err
	}

	if !allowed {
		return ref.Ref{}, authz.Denied(authz.ActionConfigureResource, instance)
	}

	return instance, nil
}

// may reports whether the caller holds an action on the camera kind, which is
// where a camera's settings are granted (ADR-20260826-5yj rule 18).
func (c *cameraSettings) may(ctx context.Context, principal string, action authz.Action) (bool, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return false, httpfault.Unexpected("read the caller's principal").Wrap(err)
	}

	object, err := authz.NewObject(authz.TypeResourceKind, ref.KindCamera.String())
	if err != nil {
		return false, httpfault.Unexpected("name the camera kind").Wrap(err)
	}

	allowed, err := c.graph.Can(ctx, who, action, object)
	if err != nil {
		return false, httpfault.Unexpected("read what the caller may do").Wrap(err)
	}

	return allowed, nil
}

// find is one camera's row.
func (c *cameraSettings) find(ctx context.Context, name string) (devicestore.Camera, bool, error) {
	rows, err := c.rows.Cameras(ctx)
	if err != nil {
		return devicestore.Camera{}, false, httpfault.Unexpected("read the cameras").Wrap(err)
	}

	at := slices.IndexFunc(rows, func(row devicestore.Camera) bool { return row.ID == name })
	if at < 0 {
		return devicestore.Camera{}, false, nil
	}

	return rows[at], true, nil
}

// setting is one row as a read answers it, with every credential masked.
func (c *cameraSettings) setting(row devicestore.Camera) httpapi.CameraSetting {
	options, known := c.types.DeclaredOptions(ref.KindCamera,
		resource.DriverType(ref.KindCamera, row.Type, c.plane.CameraTypes()))

	return httpapi.CameraSetting{
		Name: row.ID,
		CameraSpec: httpapi.CameraSpec{
			Type: row.Type, Config: httpapi.ResourceConfig(resource.Redact(options, known, row.Config)),
		},
		Version: cameraVersion(row),
	}
}

// validCameraConfig holds a written settings block to an object whose
// printer reads, with every credential the caller sent back masked restored.
func validCameraConfig(
	instance ref.Ref, options []extension.Option, known bool, written, stored json.RawMessage,
) (json.RawMessage, error) {
	if len(written) == 0 {
		written = json.RawMessage("{}")
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(written, &object); err != nil {
		return nil, httpfault.RequestInvalid("body.config", "a JSON object", err.Error())
	}

	kept, err := resource.Restore(options, known, written, stored)
	if err != nil {
		return nil, httpfault.RequestInvalid("body.config",
			"the credential itself. "+resource.RedactedValue+" keeps the stored one, and this camera has none",
			err.Error())
	}

	if _, err = builtin.CameraPrinter(instance, kept); err != nil {
		return nil, httpfault.RequestInvalid("body.config.printer", "the id of the printer it watches", err.Error())
	}

	return kept, nil
}

// cameraPrinter is the printer a camera's settings name, or empty.
func cameraPrinter(name string, config json.RawMessage) (string, error) {
	if name == "" {
		return "", errors.New("no camera")
	}

	return builtin.CameraPrinter(cameraRef(name), config)
}

// declaration is a row as the supervision plane runs it.
func declaration(row devicestore.Camera) resource.Declaration {
	return resource.Declaration{
		Ref:        cameraRef(row.ID),
		Spec:       resource.Spec{Type: row.Type, Enabled: true, Config: row.Config},
		Generation: row.Generation,
	}
}

// cameraVersion is the row's version, as an ETag quotes it.
func cameraVersion(row devicestore.Camera) string { return strconv.FormatInt(row.Generation, 10) }

// cameraRef is the camera's instance. Its name was refused at the door if it
// was not a DNS-1123 label, so a row always names one.
func cameraRef(name string) ref.Ref {
	instance, _ := ref.New(ref.KindCamera, name)

	return instance
}
