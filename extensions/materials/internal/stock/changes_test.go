package stock

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/prusa3d/materials/internal/dbtest"
)

// heard records what the store said it changed.
type heard struct {
	mu     sync.Mutex
	tools  []string
	spools []int64
	weight []int64
}

func (h *heard) Tool(deviceID, toolID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.tools = append(h.tools, deviceID+"/"+toolID)
}

func (h *heard) Spool(id int64, weightOnly bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if weightOnly {
		h.weight = append(h.weight, id)
	} else {
		h.spools = append(h.spools, id)
	}
}

// reset forgets what was heard.
func (h *heard) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.tools, h.spools, h.weight = nil, nil, nil
}

func TestTheStoreSaysWhatItChanged(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx, h := listStore(t, engine), context.Background(), &heard{}
			s.Watch(h)

			spools := addSpools(t, s, "alice", "m-1", 2, 1000)
			a, b := spools[0].ID, spools[1].ID

			if !slices.Contains(h.spools, a) || !slices.Contains(h.spools, b) {
				t.Errorf("added spools were not heard: %v", h.spools)
			}

			h.reset()

			if _, err := s.Assign(
				ctx,
				"alice",
				Assignment{DeviceID: "d1", ToolID: "0", SpoolID: &a, By: ByPerson},
			); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(h.tools, []string{"d1/0"}) || !slices.Contains(h.spools, a) {
				t.Errorf("an assign was heard as tools %v spools %v", h.tools, h.spools)
			}

			// The printer reports the spool on another tool, and it moves there.
			h.reset()

			if _, err := s.Report(
				ctx,
				Assignment{DeviceID: "d2", ToolID: "0", SpoolID: &a, By: ByPrinter},
				nil,
			); err != nil {
				t.Fatal(err)
			}

			if !slices.Contains(h.tools, "d1/0") || !slices.Contains(h.tools, "d2/0") {
				t.Errorf("a spool moved between tools was heard on %v", h.tools)
			}

			h.reset()

			if _, err := s.Assign(
				ctx,
				"alice",
				Assignment{DeviceID: "d2", ToolID: "0", SpoolID: &b, By: ByPerson},
			); err != nil {
				t.Fatal(err)
			}

			if !slices.Contains(h.spools, a) || !slices.Contains(h.spools, b) {
				t.Errorf("a swap was heard on spools %v, want the one taken off too", h.spools)
			}

			h.reset()

			consumeOn := func(e float64) {
				id := int64(7)
				if err := s.Consume(ctx, "d2", "0", Reading{E: &e, ID: &id}); err != nil {
					t.Fatal(err)
				}
			}
			consumeOn(100)
			consumeOn(1100)

			if !slices.Equal(h.weight, []int64{b}) || len(h.spools) != 0 {
				t.Errorf("filament fed was heard as weight %v spools %v", h.weight, h.spools)
			}

			h.reset()

			if err := s.Unassign(ctx, "d2", "0"); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(h.tools, []string{"d2/0"}) || !slices.Contains(h.spools, b) {
				t.Errorf("an unassign was heard as tools %v spools %v", h.tools, h.spools)
			}

			h.reset()

			if err := s.DeleteSpool(ctx, b, t0); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(h.spools, []int64{b}) {
				t.Errorf("a delete was heard as %v", h.spools)
			}

			if _, live, err := s.Spool(ctx, b); err != nil || live {
				t.Errorf("a deleted spool reads live %v (%v)", live, err)
			}
		})
	}
}

func TestRenamingACustomMaterialIsHeardOnItsTools(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx, h := listStore(t, engine), context.Background(), &heard{}
			fields := MaterialFields{Name: "Galaxy", Class: "FFF", Type: "PLA", Abbreviation: "PLA"}

			m, err := s.CreateCustom(ctx, "alice", fields)
			if err != nil {
				t.Fatal(err)
			}

			if _, err = s.Assign(ctx, "alice",
				Assignment{DeviceID: "d1", ToolID: "0", MaterialUUID: m.UUID, By: ByPerson}); err != nil {
				t.Fatal(err)
			}

			s.Watch(h)

			fields.Name = "Nebula"
			if _, err = s.UpdateCustom(ctx, "alice", m.UUID, fields); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(h.tools, []string{"d1/0"}) {
				t.Errorf("a renamed material was heard on tools %v", h.tools)
			}
		})
	}
}
