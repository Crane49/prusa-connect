package app

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	mathrand "math/rand"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/quick"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// cameraFrames is what one subscriber has read of the camera list frames, as
// the device each one named.
type cameraFrames struct {
	mu      sync.Mutex
	devices []string
}

func (c *cameraFrames) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.devices...)
}

// readCameraFrames subscribes as principal and collects every camera list
// frame the hub routes to it.
func readCameraFrames(t *testing.T, hub *stream.Hub, principal string) *cameraFrames {
	t.Helper()

	sub, err := hub.Subscribe(context.WithoutCancel(t.Context()), stream.SubscribeRequest{
		Principal: principal,
		Encoding:  stream.EncodingJSON,
		Interval:  stream.MinInterval,
	})
	require.NoError(t, err)

	t.Cleanup(sub.Close)

	frames := &cameraFrames{}

	go func() {
		for batch := range sub.Batches() {
			for _, frame := range batch.Events {
				if frame.Type != event.TypeDeviceCamerasChanged {
					continue
				}

				var envelope event.Envelope
				if json.Unmarshal(frame.Bytes, &envelope) != nil {
					continue
				}

				var change cameraListChange
				if json.Unmarshal(envelope.Data, &change) != nil {
					continue
				}

				frames.mu.Lock()
				frames.devices = append(frames.devices, change.Device)
				frames.mu.Unlock()
			}
		}
	}()

	return frames
}

// camerasOnTwoPrinters is a graph holding two printers and a person who may
// see only the first, and a hub routing on that graph.
func camerasOnTwoPrinters(t *testing.T) (*stream.Hub, string) {
	t.Helper()

	graph := newGraph(t)

	for _, id := range []string{"p-alpha", "p-beta"} {
		require.NoError(t, graph.UpsertDevice(t.Context(), id, id))
		require.NoError(t, graph.Write(t.Context(), tuple(t,
			authz.TypeDevice, id, authz.RelOrg, "org:"+bootstrapOrg)))
	}

	viewer := "user:viewer"
	require.NoError(t, graph.Write(t.Context(), tuple(t,
		authz.TypeDevice, "p-alpha", authz.RelViewer, viewer)))

	hub, err := stream.New(stream.Options{
		Site: "local", Interval: stream.MinInterval, Authorizer: visibility{graph: graph}, Logger: discard(),
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	return hub, viewer
}

// TestACameraListChangeReachesOnlyThoseWhoSeeTheDevice holds the frame to the
// device's grant. A person who sees one printer is told when that printer's
// cameras change and is told nothing about the printer next to it, which is the
// same answer the listing gives them.
func TestACameraListChangeReachesOnlyThoseWhoSeeTheDevice(t *testing.T) {
	t.Parallel()

	hub, viewer := camerasOnTwoPrinters(t)

	viewed := readCameraFrames(t, hub, viewer)
	owned := readCameraFrames(t, hub, "user:"+config.DefaultPrincipal)

	lists := newCameraLists(hub, discard())

	moved, _ := lists.reconcile(t.Context(), newCameraListBook(), time.Now(), map[string]string{
		"p-alpha": `[{"name":"front"}]`,
		"p-beta":  `[{"name":"side"}]`,
	})
	require.True(t, moved)

	require.Eventually(t, func() bool { return len(owned.seen()) == 2 },
		2*time.Second, 10*time.Millisecond, "the owner was not told about both printers")
	require.Equal(t, []string{"p-alpha", "p-beta"}, owned.seen())

	require.Eventually(t, func() bool { return len(viewed.seen()) == 1 },
		2*time.Second, 10*time.Millisecond, "the viewer was not told about the printer it sees")
	require.Equal(t, []string{"p-alpha"}, viewed.seen(),
		"a person who cannot see a printer was told its cameras changed")
}

// TestOnlyADeviceWhoseListMovedIsPublished is the comparison: a list read
// again unchanged publishes nothing, a changed one and an emptied one each
// publish one frame.
func TestOnlyADeviceWhoseListMovedIsPublished(t *testing.T) {
	t.Parallel()

	hub, _ := camerasOnTwoPrinters(t)
	owned := readCameraFrames(t, hub, "user:"+config.DefaultPrincipal)

	lists := newCameraLists(hub, discard())
	book := newCameraListBook()
	start := time.Now()

	lists.reconcile(t.Context(), book, start, map[string]string{"p-alpha": "a", "p-beta": "b"})
	require.Eventually(t, func() bool { return len(owned.seen()) == 2 },
		2*time.Second, 10*time.Millisecond)

	moved, due := lists.reconcile(t.Context(), book, start.Add(cameraListGap),
		map[string]string{"p-alpha": "a", "p-beta": "b"})
	require.False(t, moved, "a list read again unchanged counted as a change")
	require.True(t, due.IsZero(), "a device whose list did not move was held for a frame")

	// The camera on alpha changed, and the one on beta was taken away.
	moved, _ = lists.reconcile(t.Context(), book, start.Add(cameraListGap), map[string]string{"p-alpha": "a2"})
	require.True(t, moved)

	require.Eventually(t, func() bool { return len(owned.seen()) == 4 },
		2*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"p-alpha", "p-beta", "p-alpha", "p-beta"}, owned.seen())
	require.Equal(t, map[string]string{"p-alpha": "a2"}, book.told)
}

// TestTheListsAreReadAgainOnceTheySettle covers the order the host moves in. A
// stopped camera reports its last status before the host takes it out of the
// running set, so the read that status wakes still lists it. The second read
// after the settle is what publishes the removal.
func TestTheListsAreReadAgainOnceTheySettle(t *testing.T) {
	t.Parallel()

	hub, _ := camerasOnTwoPrinters(t)
	owned := readCameraFrames(t, hub, "user:"+config.DefaultPrincipal)

	lists := newCameraLists(hub, discard())
	lists.settle = 20 * time.Millisecond
	lists.gap = 0

	var (
		mu      sync.Mutex
		current = map[string]string{"p-alpha": "a"}
	)

	read := func() map[string]string {
		mu.Lock()
		defer mu.Unlock()

		return maps.Clone(current)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		lists.wait()
	})

	go lists.run(ctx, read)

	camera, err := ref.New(ref.KindCamera, "front")
	require.NoError(t, err)

	lists.observe(supervisor.Status{Ref: camera})

	require.Eventually(t, func() bool { return len(owned.seen()) == 1 },
		2*time.Second, 10*time.Millisecond, "a camera's status change published nothing")

	// The status arrives, and only then does the host drop the camera.
	lists.observe(supervisor.Status{Ref: camera})

	mu.Lock()
	current = map[string]string{}
	mu.Unlock()

	require.Eventually(t, func() bool { return len(owned.seen()) == 2 },
		2*time.Second, 10*time.Millisecond, "a camera the host dropped after its status was never published")

	// A status of any other kind wakes nothing.
	storage, err := ref.New(ref.KindStorage, "local")
	require.NoError(t, err)

	lists.observe(supervisor.Status{Ref: storage})
	require.Empty(t, lists.wake, "a storage's status woke the camera lists")
}

// TestDeclaringAndRemovingACameraPublishesItsPrinterTwice is the whole path
// over the real supervision plane and its status hook: a camera declared for a
// printer is one frame on that printer once it serves, and taking the
// declaration away is a second one.
func TestDeclaringAndRemovingACameraPublishesItsPrinterTwice(t *testing.T) {
	t.Parallel()

	hub, _ := camerasOnTwoPrinters(t)
	owned := readCameraFrames(t, hub, "user:"+config.DefaultPrincipal)

	supervision := startTestPlane(t, hub)

	camera := pushedCameraConfig("front", "p-alpha")
	book := addCameras(t, newGraph(t), supervision.host, camera)

	require.Eventually(t, func() bool { return slices.Contains(owned.seen(), "p-alpha") },
		settle, 20*time.Millisecond, "a camera declared for a printer published nothing on it")

	before := len(owned.seen())

	camera.Generation = 1
	require.NoError(t, book.remove(t.Context(), camera))

	require.Eventually(t, func() bool { return len(owned.seen()) > before },
		settle, 20*time.Millisecond, "a camera taken off a printer published nothing on it")
	require.Equal(t, "p-alpha", owned.seen()[len(owned.seen())-1])
	require.NotContains(t, owned.seen(), "p-beta", "a printer with no camera was told its cameras changed")
}

// timedFrame is one frame the loop emitted and when, on the bubble's clock.
type timedFrame struct {
	device string
	at     time.Duration
}

// gapHarness runs the loop inside a synctest bubble against a list the test
// sets, and records every frame instead of publishing it.
type gapHarness struct {
	lists *cameraLists
	start time.Time

	mu      sync.Mutex
	current map[string]string
	frames  []timedFrame
}

func newGapHarness(t *testing.T) *gapHarness {
	t.Helper()

	h := &gapHarness{
		lists:   newCameraLists(nil, discard()),
		start:   time.Now(),
		current: map[string]string{},
	}

	h.lists.emit = func(_ context.Context, device string) {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.frames = append(h.frames, timedFrame{device: device, at: time.Since(h.start)})
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		h.lists.wait()
	})

	go h.lists.run(ctx, func() map[string]string {
		h.mu.Lock()
		defer h.mu.Unlock()

		return maps.Clone(h.current)
	})

	return h
}

// set makes the list of device text, or takes the device off when text is
// empty, and wakes the loop the way a camera's status change does.
func (h *gapHarness) set(device, text string) {
	h.mu.Lock()

	if text == "" {
		delete(h.current, device)
	} else {
		h.current[device] = text
	}

	h.mu.Unlock()

	camera, _ := ref.New(ref.KindCamera, "front")
	h.lists.observe(supervisor.Status{Ref: camera})
	synctest.Wait()
}

func (h *gapHarness) seen() []timedFrame {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.frames)
}

// TestAChangeInsideTheGapIsOneTrailingFrame is the rate limit on one device.
// The first change publishes at once. Three more inside the gap are held and
// go out as one frame when the gap ends. A change undone inside the gap
// publishes nothing, because the list is again the one the last frame
// announced. A second device is not held by the first one's gap.
func TestAChangeInsideTheGapIsOneTrailingFrame(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newGapHarness(t)

		h.set("p-alpha", "a1")
		require.Equal(t, []timedFrame{{"p-alpha", 0}}, h.seen(), "the first change was not published at once")

		for _, text := range []string{"a2", "a3", "a4"} {
			time.Sleep(time.Second)
			h.set("p-alpha", text)
		}

		h.set("p-beta", "b1")

		require.Equal(t, []timedFrame{{"p-alpha", 0}, {"p-beta", 3 * time.Second}}, h.seen(),
			"a change inside the gap was published before the gap ended")

		time.Sleep(cameraListGap)
		synctest.Wait()

		require.Equal(t, []timedFrame{
			{"p-alpha", 0}, {"p-beta", 3 * time.Second}, {"p-alpha", cameraListGap},
		}, h.seen(), "the changes inside the gap were not one frame at its end")

		// a5 and back to a4 inside the next gap: the list is what the last
		// frame said, so nothing is owed. The trailing frame at 5 s opened a
		// gap that ends at 10 s, and the clock is at 8 s here. A change at 10 s
		// would meet the held read at the same instant, and that read may
		// still see a5, so both changes land before it.
		time.Sleep(500 * time.Millisecond)
		h.set("p-alpha", "a5")
		time.Sleep(500 * time.Millisecond)
		h.set("p-alpha", "a4")

		time.Sleep(2 * cameraListGap)
		synctest.Wait()

		require.Len(t, h.seen(), 3, "a change undone inside the gap published a frame")
	})
}

// TestAFlappingCameraPublishesOncePerGap is a camera that crashes and starts
// again every 200 milliseconds for a minute. Each client gets at most one frame
// per gap, and the last frame states the list the camera ended on.
func TestAFlappingCameraPublishesOncePerGap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newGapHarness(t)

		const run = time.Minute

		serving := false
		for elapsed := time.Duration(0); elapsed < run; elapsed += 200 * time.Millisecond {
			serving = !serving
			h.set("p-alpha", map[bool]string{true: "serving", false: "stopped"}[serving])
			time.Sleep(200 * time.Millisecond)
		}

		time.Sleep(2 * cameraListGap)
		synctest.Wait()

		frames := h.seen()
		require.LessOrEqual(t, len(frames), int(run/cameraListGap)+2, "a flapping camera flooded the stream")
		require.GreaterOrEqual(t, len(frames), 2)

		for i := 1; i < len(frames); i++ {
			require.GreaterOrEqual(t, frames[i].at-frames[i-1].at, cameraListGap,
				"two frames on one device inside one gap")
		}
	})
}

// TestEveryCameraFieldIsClassified is the guard on what publishes. Every field
// of the listing row is named in cameraListFields. A listIdentity field is in
// cameraListIdentity under the same name and type, and a change to it changes
// the identity. A listVolatile field is not in cameraListIdentity, and a change
// to it leaves the identity as it was. A field the listing gains fails here
// until someone decides which of the two it is.
func TestEveryCameraFieldIsClassified(t *testing.T) {
	t.Parallel()

	row := reflect.TypeFor[httpapi.Camera]()
	identity := reflect.TypeFor[cameraListIdentity]()
	random := rand.New(rand.NewPCG(1, 2))

	named := map[string]bool{}

	for field := range row.Fields() {
		named[field.Name] = true

		class, ok := cameraListFields[field.Name]
		require.Truef(t, ok, "httpapi.Camera.%s is not classified. Add it to cameraListFields as "+
			"listIdentity if a change to it means the device's camera list changed, or as listVolatile "+
			"if it may change often, such as an upload time, a frame counter or a viewer count", field.Name)

		copied, inIdentity := identity.FieldByName(field.Name)

		switch class {
		case listIdentity:
			require.Truef(t, inIdentity, "%s is listIdentity and is missing from cameraListIdentity", field.Name)
			require.Equalf(t, field.Type, copied.Type, "%s has another type in cameraListIdentity", field.Name)
		case listVolatile:
			require.Falsef(t, inIdentity, "%s is listVolatile and is in cameraListIdentity", field.Name)
		default:
			require.Failf(t, "unknown class", "%s has class %d", field.Name, class)
		}

		// A row with every field set, and the same row with this one field
		// changed.
		before := filledCamera(t, random)
		after := before
		reflect.ValueOf(&after).Elem().FieldByName(field.Name).Set(otherValue(t, random, field.Type,
			reflect.ValueOf(before).FieldByName(field.Name)))

		changed := !reflect.DeepEqual(cameraIdentityOf(before), cameraIdentityOf(after))
		require.Equalf(t, class == listIdentity, changed,
			"a change to %s changed the identity: %t. cameraIdentityOf does not match cameraListFields",
			field.Name, changed)
	}

	for name := range cameraListFields {
		require.Truef(t, named[name], "cameraListFields names %s, which httpapi.Camera does not have", name)
	}

	for field := range identity.Fields() {
		require.Truef(t, named[field.Name],
			"cameraListIdentity holds %s, which httpapi.Camera does not have", field.Name)
	}
}

// filledCamera is a listing row with no field at its zero value.
func filledCamera(t *testing.T, random *rand.Rand) httpapi.Camera {
	t.Helper()

	var row httpapi.Camera

	for _, field := range reflect.ValueOf(&row).Elem().Fields() {
		field.Set(otherValue(t, random, field.Type(), field))
	}

	return row
}

// otherValue is a value of typ that differs from old. The value is not zero
// when old is zero, so a filled row has every field set.
func otherValue(t *testing.T, random *rand.Rand, typ reflect.Type, old reflect.Value) reflect.Value {
	t.Helper()

	source := mathrand.New(mathrand.NewSource(random.Int64()))

	for range 100 {
		next, ok := quick.Value(typ, source)
		require.Truef(t, ok, "no random value for %s", typ)

		if next.IsZero() && old.IsZero() {
			continue
		}

		if !reflect.DeepEqual(next.Interface(), old.Interface()) {
			return next
		}
	}

	require.FailNowf(t, "no other value", "found no value of %s other than %v", typ, old)

	return reflect.Value{}
}

// TestTheEventStatesTheGap holds the published description to the constant, so
// a client reading the vocabulary learns the gap the server keeps.
func TestTheEventStatesTheGap(t *testing.T) {
	t.Parallel()

	doc, ok := event.LookupType(event.TypeDeviceCamerasChanged)
	require.True(t, ok)
	require.Contains(t, doc.Description, fmt.Sprintf("at most once every %d seconds", int(cameraListGap.Seconds())))
}
