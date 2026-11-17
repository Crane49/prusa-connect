package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// cameraListSettle is how long after a camera's status change the lists are
// read a second time.
//
// A status change and the host's own set of running instances do not move
// together. A stopped camera reports its last status before the host takes it
// out of the set, so the read the change wakes can still list it. The second
// read sees the set after it settled, and it is a no-op when nothing moved.
const cameraListSettle = time.Second

// cameraListGap is the shortest time between two frames on one device.
//
// A change inside the gap is held, and one frame goes out when the gap ends if
// the list still differs from the one the last frame announced. A camera that
// keeps starting and stopping then costs each client one frame per gap and no
// more, whatever the reason it moves.
const cameraListGap = 5 * time.Second

// cameraLists publishes event.TypeDeviceCamerasChanged: one frame on a device's
// origin whenever the cameras GET /api/v1/cameras?device= lists for it change.
//
// The frame says only that the list changed. Images, live views and
// uploads never travel on the event stream: a client fetches them from the
// camera's own paths. The frame tracks only the fields cameraListIdentity
// holds, which are which cameras a device has and what each can serve. Those
// change when an operator or a driver acts, and never once per frame.
//
// The frame names only the device. A camera is a device with
// grants of its own, and an administrator may hide one camera from a person
// who still sees the printer it watches. The hub routes on the printer's
// origin, so a frame listing the cameras would show that person the camera
// that was hidden. A reader who gets the frame reads the listing again, and the
// listing applies the camera's own grants.
//
// It compares what the listing would answer and ignores the status it was
// woken by, the way the sink feed does. A wake that arrives while a read
// is pending then costs nothing, and a camera that changed twice between two
// reads is one frame. A device publishes at most one frame per cameraListGap.
type cameraLists struct {
	hub *stream.Hub
	log *slog.Logger

	// settle is cameraListSettle and gap is cameraListGap, held on the value
	// so a test can change them.
	settle time.Duration
	gap    time.Duration

	// emit publishes one device's frame. It is publish, held on the value so
	// a test can count frames without a hub.
	emit func(ctx context.Context, device string)

	// wake carries one pending read. It is depth one and never blocks,
	// because the sender is the supervisor's status hook.
	wake chan struct{}
	done chan struct{}
}

func newCameraLists(hub *stream.Hub, logger *slog.Logger) *cameraLists {
	c := &cameraLists{
		hub:    hub,
		log:    logger,
		settle: cameraListSettle,
		gap:    cameraListGap,
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	c.emit = c.publish

	return c
}

// observe is what the supervision plane's status hook calls. It does no work
// beyond a non-blocking send, for the reason sinkFeed.observe gives.
func (c *cameraLists) observe(status supervisor.Status) {
	if status.Ref.Kind() != ref.KindCamera {
		return
	}

	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// start reads the host's cameras until ctx is cancelled.
func (c *cameraLists) start(ctx context.Context, host *resourcehost.Host) {
	go c.run(ctx, func() map[string]string { return cameraListsOf(host) })
}

// wait blocks until the loop has stopped.
func (c *cameraLists) wait() { <-c.done }

// cameraListBook is what the loop remembers between two reads.
type cameraListBook struct {
	// seen is the lists the last read answered, which decides whether the
	// lists are still settling.
	seen map[string]string
	// told is the list each device's last frame announced, which decides
	// whether a device owes a frame.
	told map[string]string
	// last is when each device's last frame went out.
	last map[string]time.Time
}

func newCameraListBook() *cameraListBook {
	return &cameraListBook{
		seen: map[string]string{},
		told: map[string]string{},
		last: map[string]time.Time{},
	}
}

// run waits for a wake, reads the lists, and reads them again once they have
// settled. The second read repeats while it keeps finding a change. A device
// held back by the gap is read again when its gap ends.
func (c *cameraLists) run(ctx context.Context, read func() map[string]string) {
	defer close(c.done)

	book := newCameraListBook()

	settle := time.NewTimer(c.settle)
	defer settle.Stop()

	settle.Stop()

	held := time.NewTimer(c.gap)
	defer held.Stop()

	held.Stop()

	for {
		woken := false

		select {
		case <-ctx.Done():
			return
		case <-c.wake:
			woken = true
		case <-settle.C:
		case <-held.C:
		}

		moved, due := c.reconcile(ctx, book, time.Now(), read())
		if moved || woken {
			settle.Reset(c.settle)
		}

		if due.IsZero() {
			held.Stop()
		} else {
			held.Reset(time.Until(due))
		}
	}
}

// reconcile publishes one frame for every device whose list differs from the
// one its last frame announced, unless that frame went out less than a gap
// before now. It reports whether the lists moved since the last read, and the
// earliest time a held device may publish, which is zero when none is held.
func (c *cameraLists) reconcile(
	ctx context.Context, book *cameraListBook, now time.Time, lists map[string]string,
) (bool, time.Time) {
	moved := !maps.Equal(book.seen, lists)

	clear(book.seen)
	maps.Copy(book.seen, lists)

	devices := slices.Sorted(maps.Keys(lists))

	for device := range book.told {
		if _, listed := lists[device]; !listed {
			devices = append(devices, device)
		}
	}

	slices.Sort(devices)

	var due time.Time

	for _, device := range devices {
		list, listed := lists[device]

		if next := c.announce(ctx, book, now, device, list, listed); !next.IsZero() &&
			(due.IsZero() || next.Before(due)) {
			due = next
		}
	}

	book.forget(now, c.gap)

	return moved, due
}

// announce publishes device's frame when its list differs from the one its
// last frame announced and the gap since that frame is over. It answers when
// the device may publish, for a device the gap holds back, and zero otherwise.
func (c *cameraLists) announce(
	ctx context.Context, book *cameraListBook, now time.Time, device, list string, listed bool,
) time.Time {
	announced, told := book.told[device]
	if list == announced && listed == told {
		return time.Time{}
	}

	if at, ok := book.last[device]; ok && now.Sub(at) < c.gap {
		return at.Add(c.gap)
	}

	c.emit(ctx, device)

	book.last[device] = now

	if listed {
		book.told[device] = list
	} else {
		delete(book.told, device)
	}

	return time.Time{}
}

// forget drops a device that has no list and owes no frame once its gap is
// over, so the book does not grow with every printer ever seen.
func (b *cameraListBook) forget(now time.Time, gap time.Duration) {
	for device, at := range b.last {
		if _, told := b.told[device]; !told && now.Sub(at) >= gap {
			delete(b.last, device)
		}
	}
}

// cameraListChange is the data of event.TypeDeviceCamerasChanged.
type cameraListChange struct {
	Device string `json:"device_id"`
}

func (c *cameraLists) publish(ctx context.Context, device string) {
	origin, err := event.NewOrigin(event.KindDevice, device)
	if err != nil {
		c.log.WarnContext(ctx, "a camera names a device that is not an origin",
			"device", device, "error", err.Error())

		return
	}

	data, err := json.Marshal(cameraListChange{Device: device})
	if err != nil {
		c.log.ErrorContext(ctx, "a camera list change could not be rendered and is unpublished",
			"device", device, "error", err.Error())

		return
	}

	if _, err = c.hub.Publish(stream.Publication{
		Origin: origin,
		Type:   event.TypeDeviceCamerasChanged,
		Class:  event.ClassState,
		Data:   data,
	}); err != nil {
		c.log.DebugContext(ctx, "a camera list change reached no hub",
			"device", device, "error", err.Error())
	}
}

// cameraListField says whether a change to one field of httpapi.Camera
// publishes event.TypeDeviceCamerasChanged.
type cameraListField int

const (
	// listIdentity is a field that says which cameras a device has or what one
	// of them can serve. It is copied into cameraListIdentity, and a change to
	// it publishes a frame.
	listIdentity cameraListField = iota + 1
	// listVolatile is a field that may change often, such as the time of the
	// last upload, a frame counter or a count of viewers. It is never copied
	// into cameraListIdentity, so a change to it publishes nothing.
	listVolatile
)

// cameraListFields classifies every field of httpapi.Camera, and it is the one
// place a field is classified. TestEveryCameraFieldIsClassified fails when the
// listing gains a field this map does not name, so a field that changes every
// second cannot start publishing a frame every second without someone choosing
// listIdentity for it here.
var cameraListFields = map[string]cameraListField{
	"Name":          listIdentity,
	"Type":          listIdentity,
	"DeviceID":      listIdentity,
	"Formats":       listIdentity,
	"Serving":       listIdentity,
	"MinIntervalMS": listIdentity,
	"Snapshot":      listIdentity,
	"Stream":        listIdentity,
	"Watch":         listIdentity,
}

// cameraListIdentity is the part of one listing row a frame tracks: the fields
// cameraListFields marks listIdentity, under the same names and types.
type cameraListIdentity struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	DeviceID      string   `json:"device_id"`
	Formats       []string `json:"formats"`
	Serving       bool     `json:"serving"`
	MinIntervalMS int64    `json:"min_interval_ms"`
	Snapshot      bool     `json:"snapshot"`
	Stream        bool     `json:"stream"`
	Watch         bool     `json:"watch"`
}

// cameraIdentityOf is the part of row a frame tracks.
func cameraIdentityOf(row httpapi.Camera) cameraListIdentity {
	return cameraListIdentity{
		Name:          row.Name,
		Type:          row.Type,
		DeviceID:      row.DeviceID,
		Formats:       row.Formats,
		Serving:       row.Serving,
		MinIntervalMS: row.MinIntervalMS,
		Snapshot:      row.Snapshot,
		Stream:        row.Stream,
		Watch:         row.Watch,
	}
}

// cameraListsOf is what GET /api/v1/cameras?device= would answer for each
// device, before the caller's grants narrow it, reduced to cameraListIdentity
// and rendered as one comparable text per device. A device with no camera is
// absent.
//
// It reads the rows the listing reads, through describe, and keeps only the
// identity of each. A field the listing gains publishes nothing until
// cameraListFields calls it listIdentity.
func cameraListsOf(host *resourcehost.Host) map[string]string {
	rows := map[string][]cameraListIdentity{}

	for instance, running := range host.Supervising(ref.KindCamera) {
		camera, err := supervised.AttachCamera(running)
		if err != nil {
			continue
		}

		printer := camera.Printer()
		if printer == "" {
			continue
		}

		rows[printer] = append(rows[printer],
			cameraIdentityOf(describe(instance, printer, declaredType(host, instance), camera)))
	}

	out := make(map[string]string, len(rows))

	for printer, cameras := range rows {
		slices.SortFunc(cameras, func(a, b cameraListIdentity) int { return strings.Compare(a.Name, b.Name) })

		text, err := json.Marshal(cameras)
		if err != nil {
			continue
		}

		out[printer] = string(text)
	}

	return out
}
