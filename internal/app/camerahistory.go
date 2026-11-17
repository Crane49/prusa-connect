package app

import (
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The cadence the recorder reconciles at and the shape of a stored path.
const (
	// cameraHistorySweep is how often the recorder compares what it is
	// capturing against what the host is supervising. It is a reconcile and
	// not a subscription, for the reason Host.Supervising gives: one missed
	// transition would leave a camera recording after it had gone, for the life
	// of the process.
	cameraHistorySweep = 5 * time.Second
	// cameraHistoryStamp is how a stored snapshot's moment is spelled in its
	// name. It is the RFC 3339 instant with the punctuation a filename cannot
	// carry taken out, so a directory listing sorts in time order and a person
	// reading one filename can say when it was taken.
	cameraHistoryStamp = "20060102T150405.000Z"
)

// cameraHistory is the periodic capture: one goroutine per camera that has it
// switched on, taking a still image every so often and writing it into a
// storage instance so a job can be replayed with pictures later.
//
// The frames come from the driver, the storage provider writes them and the
// record is one event on the bus. This type holds a ticker, a path and the
// reconcile that starts and stops the tickers as cameras come and go. Nothing
// is buffered, retried or retained. A snapshot that could not be taken is a
// log line and the next tick tries again, because an old picture is not worth
// a queue.
//
// It is off unless an operator switches it on, per camera. A deployment with
// many cameras and a short cadence writes a very large number of files a day,
// and an operator must choose that.
type cameraHistory struct {
	host  *resourcehost.Host
	hub   *stream.Hub
	saver cameraSnapshotSaver
	log   *slog.Logger

	// running is the capture goroutine per camera, keyed by the instance name,
	// each with the cancel that ends it. The recorder owns the map and only its
	// own loop touches it, so there is nothing to lock.
	running map[ref.Ref]*cameraCapture
}

// cameraSnapshotSaver is the storage plane as the recorder writes through it.
// It is declared here because this is where it is consumed, and stores
// satisfies it.
//
// A recorder needs one method. The provider makes the parent directory, writes
// to a sibling and renames on success, so a capture interrupted half way
// leaves no half file under the name a reader will look for.
type cameraSnapshotSaver interface {
	Save(ctx context.Context, instance, path string, body io.Reader) (storageapi.WriteResult, error)
}

// cameraCapture is one camera's capture goroutine and the settings it was
// started under. The settings are held so the reconcile can tell a camera with
// a new declaration from one with the same declaration, and restart only the
// first.
type cameraCapture struct {
	settings builtin.CameraHistory
	stop     context.CancelFunc
	done     chan struct{}
}

// newCameraHistory builds the recorder. It captures nothing until Run.
func newCameraHistory(
	host *resourcehost.Host, hub *stream.Hub, saver cameraSnapshotSaver, logger *slog.Logger,
) *cameraHistory {
	return &cameraHistory{
		host:    host,
		hub:     hub,
		saver:   saver,
		log:     logger,
		running: make(map[ref.Ref]*cameraCapture),
	}
}

// Run reconciles the capture goroutines against the supervised cameras until
// the context ends, and then waits for every one of them to stop.
//
// The reconcile owns every capture goroutine. A camera the host stopped
// supervising, one whose declaration turned the capture off, and one whose
// cadence changed are handled the same way: stop what is running and start
// what the declaration says.
func (c *cameraHistory) Run(ctx context.Context) {
	ticker := time.NewTicker(cameraHistorySweep)
	defer ticker.Stop()

	defer c.stopAll()

	for {
		c.reconcile(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reconcile starts a capture for every supervised camera that declares one and
// stops every capture whose camera has stopped declaring one.
func (c *cameraHistory) reconcile(ctx context.Context) {
	wanted := make(map[ref.Ref]builtin.CameraHistory)

	for instance := range c.host.Supervising(ref.KindCamera) {
		declared, held := c.host.Declared(instance)
		if !held {
			continue
		}

		settings, err := builtin.CameraHistoryOf(instance, declared.Spec.Config)
		if err != nil {
			c.log.WarnContext(ctx, "a camera declares a snapshot history that cannot be read",
				"resource", instance.String(), "error", err.Error())

			continue
		}

		if settings.On() {
			wanted[instance] = settings
		}
	}

	for instance, capture := range c.running {
		if settings, still := wanted[instance]; still && settings == capture.settings {
			continue
		}

		c.halt(instance)
	}

	for instance, settings := range wanted {
		if _, already := c.running[instance]; already {
			continue
		}

		c.begin(ctx, instance, settings)
	}
}

// begin starts one camera's capture goroutine.
func (c *cameraHistory) begin(ctx context.Context, instance ref.Ref, settings builtin.CameraHistory) {
	captureCtx, cancel := context.WithCancel(ctx)
	capture := &cameraCapture{settings: settings, stop: cancel, done: make(chan struct{})}

	c.running[instance] = capture

	c.log.InfoContext(ctx, "a camera is recording its snapshots",
		"resource", instance.String(), "every", settings.Interval.String(), "storage", settings.Storage)

	go func() {
		defer close(capture.done)

		c.capture(captureCtx, instance, settings)
	}()
}

// halt stops one camera's capture goroutine and waits for it, so a camera that
// is restarted does not have two goroutines writing under the same name.
func (c *cameraHistory) halt(instance ref.Ref) {
	capture, running := c.running[instance]
	if !running {
		return
	}

	capture.stop()
	<-capture.done

	delete(c.running, instance)
}

// stopAll ends every capture. It is the deferred half of Run.
func (c *cameraHistory) stopAll() {
	for instance := range c.running {
		c.halt(instance)
	}
}

// capture is one camera's goroutine: a still image every interval, written and
// announced, until the context ends.
//
// The first frame is taken after one interval. A camera that has just started
// has produced nothing yet, so a capture at zero would find no frame.
func (c *cameraHistory) capture(ctx context.Context, instance ref.Ref, settings builtin.CameraHistory) {
	ticker := time.NewTicker(settings.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if err := c.store(ctx, instance, settings); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "a camera snapshot was not stored",
				"resource", instance.String(), "error", err.Error())
		}
	}
}

// store takes one still image, writes it and publishes the fact.
//
// The path is camera/{instance}/{timestamp}.jpg inside the storage the operator
// named, which is one directory per camera and a name that sorts in time order.
// A reader replaying a job lists that directory and reads it forwards.
func (c *cameraHistory) store(ctx context.Context, instance ref.Ref, settings builtin.CameraHistory) error {
	runner, running := c.host.Instance(instance)
	if !running {
		return nil
	}

	camera, err := supervised.AttachCamera(runner)
	if err != nil {
		return nil //nolint:nilerr // an instance that is not a camera is the reconcile's business, not a capture failure.
	}

	shot, err := camera.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, cameraapi.ErrNoFrame) {
			return nil
		}

		return fmt.Errorf("read the camera: %w", err)
	}

	if err = shot.Validate(); err != nil {
		return err
	}

	taken := shot.Taken
	if taken.IsZero() {
		taken = time.Now()
	}

	path := fmt.Sprintf("camera/%s/%s.jpg", instance.ID(), taken.UTC().Format(cameraHistoryStamp))

	written, err := c.saver.Save(ctx, settings.Storage, path, bytes.NewReader(shot.Bytes))
	if err != nil {
		return fmt.Errorf("store the snapshot: %w", err)
	}

	c.announce(ctx, instance, camera.Printer(), settings.Storage, path, written.Size, taken)

	return nil
}

// storedSnapshot is the payload of the fact one stored snapshot publishes. It
// carries where the picture is and never the picture: a frame is megabytes and
// an event is a record of something happening.
type storedSnapshot struct {
	Camera  string    `json:"camera"`
	Printer string    `json:"printer,omitempty"`
	Storage string    `json:"storage"`
	Path    string    `json:"path"`
	Bytes   int64     `json:"bytes"`
	Taken   time.Time `json:"taken"`
}

// announce publishes the fact that a snapshot was stored, in the same envelope
// every other durable frame uses.
//
// The origin is the camera and not the printer it watches. A camera is a device
// with a row of its own, so the frame is authorized the way the camera is. A
// reader following one printer's pictures asks for the camera's origin.
//
// A frame that reached no hub is a debug line and not a failure. The picture is
// stored either way, and a deployment with no history provider is the normal
// one for a first run.
func (c *cameraHistory) announce(
	ctx context.Context, instance ref.Ref, printer, storage, path string, written int64, taken time.Time,
) {
	if c.hub == nil {
		return
	}

	origin, err := event.NewOrigin(event.Kind(ref.KindCamera.String()), instance.ID())
	if err != nil {
		c.log.ErrorContext(ctx, "a camera id is not an origin",
			"resource", instance.String(), "error", err.Error())

		return
	}

	data, err := json.Marshal(storedSnapshot{
		Camera:  instance.ID(),
		Printer: printer,
		Storage: storage,
		Path:    path,
		Bytes:   written,
		Taken:   taken.UTC(),
	})
	if err != nil {
		c.log.ErrorContext(ctx, "a stored snapshot could not be rendered and is unpublished",
			"resource", instance.String(), "error", err.Error())

		return
	}

	if _, err = c.hub.Publish(stream.Publication{
		Origin: origin,
		Type:   event.TypeCameraSnapshotCaptured,
		Class:  event.ClassState,
		Data:   data,
	}); err != nil {
		c.log.DebugContext(ctx, "a stored snapshot reached no hub",
			"resource", instance.String(), "error", err.Error())
	}
}
