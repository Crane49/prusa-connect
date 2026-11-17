// Package notify tells the rest of the deployment what Materials changed, as
// events on the host's stream: a tool whose material or spool changed, and a
// spool that changed. Another module, such as Farm, subscribes to them and
// reads the routes for more.
//
// The store says what it changed, after each commit. The notifier keeps the
// keys it was told and emits each once with what the store then holds, so a
// burst on one key is one event and a slow bus never holds up a write. An
// event is best-effort: one that fails is logged and dropped.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/materials/internal/stock"
)

// The endpoints module.yaml declares under events.
const (
	EndpointTool  = "tool"
	EndpointSpool = "spool"
)

// Emitter publishes one event, as busapi.Client does.
type Emitter interface {
	Emit(ctx context.Context, endpoint, key string, data any) error
}

// Reader is what the notifier reads of the store.
type Reader interface {
	Assignments(ctx context.Context, deviceID string) ([]stock.Assignment, error)
	Spool(ctx context.Context, id int64) (stock.Spool, bool, error)
}

// toolKey is one printer tool.
type toolKey struct{ deviceID, toolID string }

// Notifier is a stock.Changes that emits what it is told.
type Notifier struct {
	emit Emitter
	read Reader
	log  *slog.Logger
	wake chan struct{}

	mu    sync.Mutex
	tools map[toolKey]bool
	// spools holds true for a spool told of nothing but its weight.
	spools map[int64]bool

	// grams is the whole grams last emitted of each spool. Only Run reads it.
	grams map[int64]int64
}

// New returns a notifier that emits nothing until Run.
func New(emit Emitter, read Reader, log *slog.Logger) *Notifier {
	return &Notifier{
		emit: emit, read: read, log: log, wake: make(chan struct{}, 1),
		tools: map[toolKey]bool{}, spools: map[int64]bool{}, grams: map[int64]int64{},
	}
}

// Tool keeps a tool to emit.
func (n *Notifier) Tool(deviceID, toolID string) {
	n.mu.Lock()
	n.tools[toolKey{deviceID, toolID}] = true
	n.mu.Unlock()
	n.poke()
}

// Spool keeps a spool to emit. A spool told of anything but its weight is
// emitted whatever its weight.
func (n *Notifier) Spool(id int64, weightOnly bool) {
	n.mu.Lock()
	only, held := n.spools[id]
	n.spools[id] = weightOnly && (!held || only)
	n.mu.Unlock()
	n.poke()
}

func (n *Notifier) poke() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// Run emits what it is told until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.wake:
		}

		n.mu.Lock()
		tools, spools := n.tools, n.spools
		n.tools, n.spools = map[toolKey]bool{}, map[int64]bool{}
		n.mu.Unlock()

		for _, k := range slices.SortedFunc(maps.Keys(tools), func(a, b toolKey) int {
			return strings.Compare(a.deviceID+"/"+a.toolID, b.deviceID+"/"+b.toolID)
		}) {
			n.report(ctx, n.tool(ctx, k))
		}

		for _, id := range slices.Sorted(maps.Keys(spools)) {
			n.report(ctx, n.spool(ctx, id, spools[id]))
		}
	}
}

// report logs an event that could not be emitted.
func (n *Notifier) report(ctx context.Context, err error) {
	if err != nil && ctx.Err() == nil {
		n.log.WarnContext(ctx, "materials could not tell of a change", "error", err)
	}
}

// toolEvent is what a tool event carries.
type toolEvent struct {
	DeviceID     string `json:"device_id"`
	ToolID       string `json:"tool_id"`
	MaterialUUID string `json:"material_uuid,omitempty"`
	SpoolID      *int64 `json:"spool_id,omitempty"`
}

func (n *Notifier) tool(ctx context.Context, k toolKey) error {
	held, err := n.read.Assignments(ctx, k.deviceID)
	if err != nil {
		return fmt.Errorf("read printer %s: %w", k.deviceID, err)
	}

	event := toolEvent{DeviceID: k.deviceID, ToolID: k.toolID}

	for _, a := range held {
		if a.ToolID == k.toolID {
			event.MaterialUUID, event.SpoolID = a.MaterialUUID, a.SpoolID
		}
	}

	return n.emit.Emit(ctx, EndpointTool, k.deviceID+"/"+k.toolID, event)
}

// spoolEvent is what a spool event carries.
type spoolEvent struct {
	SpoolID          int64   `json:"spool_id"`
	SpoolUUID        string  `json:"spool_uuid"`
	MaterialUUID     string  `json:"material_uuid"`
	RemainingWeightG float64 `json:"remaining_weight_g"`
	Deleted          bool    `json:"deleted,omitempty"`
}

// spool emits a spool, and a spool told of nothing but its weight only when
// its whole grams changed, because the consumption is shown in whole grams.
func (n *Notifier) spool(ctx context.Context, id int64, weightOnly bool) error {
	sp, live, err := n.read.Spool(ctx, id)
	if errors.Is(err, stock.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read spool %d: %w", id, err)
	}

	grams := int64(math.Floor(sp.RemainingWeightG))
	if last, ok := n.grams[id]; weightOnly && ok && last == grams {
		return nil
	}

	n.grams[id] = grams

	return n.emit.Emit(ctx, EndpointSpool, fmt.Sprintf("spool/%d", id), spoolEvent{
		SpoolID: sp.ID, SpoolUUID: sp.UUID, MaterialUUID: sp.MaterialUUID,
		RemainingWeightG: sp.RemainingWeightG, Deleted: !live,
	})
}
