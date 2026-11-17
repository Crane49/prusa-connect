package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/materials/internal/stock"
)

// sent is one emission.
type sent struct {
	endpoint, key string
	data          map[string]any
}

// bus records what was emitted, and fails while fail is set.
type bus struct {
	mu   sync.Mutex
	got  []sent
	fail bool
}

func (b *bus) Emit(_ context.Context, endpoint, key string, data any) error {
	raw, _ := json.Marshal(data)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.fail {
		return errors.New("the bus is down")
	}

	b.got = append(b.got, sent{endpoint, key, out})

	return nil
}

func (b *bus) emitted() []sent {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]sent(nil), b.got...)
}

// world is what the notifier reads.
type world struct {
	mu          sync.Mutex
	assignments map[string][]stock.Assignment
	spools      map[int64]stock.Spool
	gone        map[int64]bool
}

func (w *world) Assignments(_ context.Context, deviceID string) ([]stock.Assignment, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.assignments[deviceID], nil
}

func (w *world) Spool(_ context.Context, id int64) (stock.Spool, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	sp, ok := w.spools[id]
	if !ok {
		return stock.Spool{}, false, stock.ErrNotFound
	}

	return sp, !w.gone[id], nil
}

func (w *world) weigh(id int64, grams float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	sp := w.spools[id]
	sp.RemainingWeightG = grams
	w.spools[id] = sp
}

// start runs a notifier over one printer with spool 8 on its tool 0.
func start(t *testing.T) (*Notifier, *bus, *world) {
	t.Helper()

	n, b, w := build()
	run(t, n)

	return n, b, w
}

func build() (*Notifier, *bus, *world) {
	spoolID := int64(8)
	b := &bus{}
	w := &world{
		assignments: map[string][]stock.Assignment{
			"p1": {{DeviceID: "p1", ToolID: "0", MaterialUUID: "m-1", SpoolID: &spoolID}},
		},
		spools: map[int64]stock.Spool{8: {ID: 8, UUID: "u-8", MaterialUUID: "m-1", RemainingWeightG: 500.4}},
		gone:   map[int64]bool{},
	}
	return New(b, w, slog.New(slog.DiscardHandler)), b, w
}

func run(t *testing.T, n *Notifier) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() { defer close(done); n.Run(ctx) }()

	t.Cleanup(func() { cancel(); <-done })
}

// settle waits for the notifier to emit want events in all.
func settle(t *testing.T, b *bus, want int) []sent {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := b.emitted(); len(got) >= want {
			time.Sleep(20 * time.Millisecond)

			return b.emitted()
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("emitted %v, want %d events", b.emitted(), want)

	return nil
}

func TestAToolChangeIsEmittedWithWhatIsOnIt(t *testing.T) {
	n, b, _ := start(t)

	n.Tool("p1", "0")

	got := settle(t, b, 1)
	if got[0].endpoint != "tool" || got[0].key != "p1/0" || got[0].data["material_uuid"] != "m-1" ||
		got[0].data["spool_id"] != 8.0 {
		t.Errorf("emitted %+v", got[0])
	}

	n.Tool("p1", "1")

	if got = settle(t, b, 2); got[1].data["spool_id"] != nil || got[1].data["tool_id"] != "1" {
		t.Errorf("an empty tool was emitted as %+v", got[1])
	}
}

func TestABurstOnOneSpoolIsOneEvent(t *testing.T) {
	n, b, _ := build()

	n.Spool(8, false)
	n.Spool(8, false)
	n.Spool(8, true)
	run(t, n)

	got := settle(t, b, 1)
	if len(got) != 1 || got[0].endpoint != "spool" || got[0].key != "spool/8" ||
		got[0].data["remaining_weight_g"] != 500.4 || got[0].data["spool_uuid"] != "u-8" {
		t.Errorf("emitted %+v", got)
	}
}

func TestAWeightIsEmittedAtEachWholeGram(t *testing.T) {
	n, b, w := start(t)

	n.Spool(8, true)
	settle(t, b, 1)

	w.weigh(8, 500.1)
	n.Spool(8, true)
	time.Sleep(50 * time.Millisecond)

	if got := b.emitted(); len(got) != 1 {
		t.Fatalf("a tenth of a gram was emitted: %+v", got)
	}

	w.weigh(8, 499.9)
	n.Spool(8, true)

	if got := settle(t, b, 2); got[1].data["remaining_weight_g"] != 499.9 {
		t.Errorf("crossing a gram emitted %+v", got[1])
	}
}

func TestADeletedSpoolSaysSo(t *testing.T) {
	n, b, w := start(t)

	w.mu.Lock()
	w.gone[8] = true
	w.mu.Unlock()
	n.Spool(8, false)

	if got := settle(t, b, 1); got[0].data["deleted"] != true {
		t.Errorf("a deleted spool emitted %+v", got[0])
	}
}

func TestAFailedEmitDoesNotStopTheNext(t *testing.T) {
	n, b, _ := start(t)

	b.mu.Lock()
	b.fail = true
	b.mu.Unlock()
	n.Tool("p1", "0")
	time.Sleep(50 * time.Millisecond)

	b.mu.Lock()
	b.fail = false
	b.mu.Unlock()
	n.Spool(8, false)

	if got := settle(t, b, 1); got[0].endpoint != "spool" {
		t.Errorf("after a failure, emitted %+v", got)
	}
}
