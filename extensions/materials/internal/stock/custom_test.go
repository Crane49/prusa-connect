package stock

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prusa3d/materials/internal/dbtest"
)

func newStore(t *testing.T, engine dbtest.Engine) *Store {
	t.Helper()

	s := New(dbtest.Open(t, engine.Grant(t)), engine.Name)
	if _, _, err := s.ReplaceCatalogue(context.Background(), snapshot("a"), t0); err != nil {
		t.Fatal(err)
	}

	return s
}

func TestACustomMaterialIsOnlyItsOwnersBusiness(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			c, err := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Mine", Class: "FFF"})
			if err != nil {
				t.Fatal(err)
			}

			if _, err = s.UpdateCustom(
				ctx,
				"bob",
				c.UUID,
				MaterialFields{Name: "Hers", Class: "FFF"},
			); !errors.Is(
				err,
				ErrNotFound,
			) {
				t.Fatalf("update: %v", err)
			}

			if err = s.DeleteCustom(ctx, "bob", c.UUID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("delete: %v", err)
			}

			if err = s.Star(ctx, "bob", c.UUID, true); !errors.Is(err, ErrNotFound) {
				t.Fatalf("star: %v", err)
			}

			if _, err = s.AddSpools(
				ctx,
				"bob",
				SpoolDraft{MaterialUUID: c.UUID, RemainingWeightG: 1, Condition: "new"},
				1,
				10,
			); !errors.Is(
				err,
				ErrNotFound,
			) {
				t.Fatalf("add spool: %v", err)
			}

			got, err := s.UpdateCustom(ctx, "alice", c.UUID, MaterialFields{Name: "Renamed", Class: "FFF"})
			if err != nil || got.Name != "Renamed" {
				t.Fatalf("owner update: %+v %v", got, err)
			}
		})
	}
}

func TestACustomMaterialInUseCannotBeDeleted(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			c, _ := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Mine", Class: "FFF"})
			if err := s.Star(ctx, "alice", c.UUID, true); err != nil {
				t.Fatal(err)
			}

			sp, err := s.AddSpools(
				ctx,
				"alice",
				SpoolDraft{MaterialUUID: c.UUID, RemainingWeightG: 100, Condition: "new"},
				1,
				10,
			)
			if err != nil || sp[0].MaterialSource != "custom" {
				t.Fatalf("add: %+v %v", sp, err)
			}

			if err = s.DeleteCustom(ctx, "alice", c.UUID); !errors.Is(err, ErrInUse) {
				t.Fatalf("want ErrInUse, got %v", err)
			}

			if err = s.DeleteSpool(ctx, sp[0].ID, t0); err != nil {
				t.Fatal(err)
			}

			if err = s.DeleteCustom(ctx, "alice", c.UUID); err != nil {
				t.Fatal(err)
			}

			if n := count(t, s, "favourites"); n != 0 {
				t.Fatalf("favourites left: %d", n)
			}
		})
	}
}

func TestStarringIsPerPersonAndNeedsAMaterial(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			if err := s.Star(ctx, "alice", "nope", true); !errors.Is(err, ErrNotFound) {
				t.Fatalf("unknown: %v", err)
			}

			// Bob's custom material is in Alice's list once a live spool holds
			// it, so she may star it.
			c, created := s.CreateCustom(ctx, "bob", MaterialFields{Name: "Shed PETG", Class: "FFF"})
			if created != nil {
				t.Fatal(created)
			}

			err := s.Star(ctx, "alice", c.UUID, true)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("not held yet: %v", err)
			}

			if _, err = s.AddSpools(ctx, "bob", SpoolDraft{MaterialUUID: c.UUID, Condition: "new"}, 1, 10); err != nil {
				t.Fatal(err)
			}

			if err = s.Star(ctx, "alice", c.UUID, true); err != nil {
				t.Fatalf("held: %v", err)
			}

			for range 2 { // starring twice is fine
				if err = s.Star(ctx, "alice", "m-a", true); err != nil {
					t.Fatal(err)
				}
			}

			if err = s.Star(ctx, "bob", "m-a", true); err != nil {
				t.Fatal(err)
			}

			if err = s.Star(ctx, "alice", "m-a", false); err != nil {
				t.Fatal(err)
			}

			if n := count(t, s, "favourites"); n != 2 {
				t.Fatalf("want bob's one and alice's held one, got %d", n)
			}
		})
	}
}

func TestACustomMaterialNeedsANameAndAKnownClass(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			c, err := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Mine", Class: "SLA"})
			if err != nil {
				t.Fatal(err)
			}

			for name, f := range map[string]MaterialFields{
				"no class":      {Name: "x", Class: ""},
				"unknown class": {Name: "x", Class: "PETG"},
				"blank name":    {Name: "  ", Class: "FFF"},
				"six colours": {Name: "x", Class: "FFF", SecondaryColors: []string{
					"#000000", "#111111", "#222222", "#333333", "#444444", "#555555",
				}},
				"bad colour": {Name: "x", Class: "FFF", SecondaryColors: []string{"red"}},
				"css in colour": {
					Name: "x", Class: "FFF", ColorRGBA: "#fff; background-image: url(https://evil.example/x)",
				},
				"bad tag":   {Name: "x", Class: "FFF", Tags: []string{"Abrasive"}},
				"comma tag": {Name: "x", Class: "FFF", Certifications: []string{"a,b"}},
			} {
				if _, err = s.CreateCustom(ctx, "alice", f); !errors.Is(err, ErrInvalid) {
					t.Errorf("create %s: %v", name, err)
				}

				if _, err = s.UpdateCustom(ctx, "alice", c.UUID, f); !errors.Is(err, ErrInvalid) {
					t.Errorf("update %s: %v", name, err)
				}
			}

			if n := count(t, s, "custom_materials"); n != 1 {
				t.Fatalf("count %d", n)
			}
		})
	}
}

func TestACustomMaterialKeepsItsDetailsAndLists(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			nozzle, td, shoreA, shoreD, minC, maxC := 0.4, 2.5, 95.0, 55.0, 30, 60
			f := MaterialFields{
				Name: "Shed PETG", BrandName: "Local", Class: "FFF", Type: "PETG", ColorRGBA: "#E02020",
				MinNozzleDiameter: &nozzle, TransmissionDistance: &td, HardnessShoreA: &shoreA, HardnessShoreD: &shoreD,
				MinChamberTemp: &minC, MaxChamberTemp: &maxC, RALCode: "RAL 3020",
				SecondaryColors: []string{"#111111", "#F5F5F5ff"},
				Certifications:  []string{"ul_94_v0"},
				Tags:            []string{"abrasive", "contains_carbon_fiber"},
			}

			c, err := s.CreateCustom(ctx, "alice", f)
			if err != nil {
				t.Fatal(err)
			}

			got, err := s.custom(ctx, "alice", c.UUID)
			if err != nil || *got.MinNozzleDiameter != 0.4 || *got.HardnessShoreD != 55 || *got.MaxChamberTemp != 60 ||
				got.RALCode != "RAL 3020" || fmt.Sprint(got.SecondaryColors) != "[#111111 #F5F5F5ff]" ||
				fmt.Sprint(
					got.Certifications,
				) != "[ul_94_v0]" || fmt.Sprint(got.Tags) != "[abrasive contains_carbon_fiber]" {
				t.Fatalf("%+v %v", got, err)
			}

			bare, err := s.UpdateCustom(ctx, "alice", c.UUID, MaterialFields{Name: "Bare", Class: "SLA"})
			if err != nil || bare.Tags == nil || len(bare.Tags) != 0 || bare.MinNozzleDiameter != nil {
				t.Fatalf("%+v %v", bare, err)
			}

			plain, err := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Plain", Class: "FFF"})
			if err != nil || plain.SecondaryColors == nil || plain.Certifications == nil {
				t.Fatalf("a left-out list answers as empty: %+v %v", plain, err)
			}
		})
	}
}

func TestEditingACustomMaterialReachesItsLiveSpools(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			c, err := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Mine", Class: "FFF", ColorRGBA: "#000000"})
			if err != nil {
				t.Fatal(err)
			}

			added, err := s.AddSpools(ctx, "alice",
				SpoolDraft{MaterialUUID: c.UUID, RemainingWeightG: 1, Condition: "new"}, 2, 10)
			if err != nil {
				t.Fatal(err)
			}

			if err = s.DeleteSpool(ctx, added[1].ID, time.Now()); err != nil {
				t.Fatal(err)
			}

			if _, err = s.UpdateCustom(ctx, "alice", c.UUID,
				MaterialFields{Name: "Renamed", BrandName: "Me", Class: "FFF", ColorRGBA: "#ff0000"}); err != nil {
				t.Fatal(err)
			}

			page, err := s.Spools(ctx, c.UUID, 0, 0)
			if err != nil || len(page.Spools) != 1 {
				t.Fatalf("%+v %v", page, err)
			}

			if sp := page.Spools[0]; sp.MaterialName != "Renamed" || sp.BrandName != "Me" || sp.ColorRGBA != "#ff0000" {
				t.Fatalf("the live spool kept the old copy: %+v", sp)
			}

			var deleted string
			if err = s.db.QueryRowContext(ctx, `SELECT material_name FROM spools WHERE id = $1`, added[1].ID).
				Scan(&deleted); err != nil || deleted != "Mine" {
				t.Fatalf("the deleted spool became %q (%v)", deleted, err)
			}
		})
	}
}

func TestEditingACustomMaterialUpdatesItsAssignments(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			c, err := s.CreateCustom(ctx, "alice", MaterialFields{Name: "Mine", Class: "FFF", Abbreviation: "MY"})
			if err != nil {
				t.Fatal(err)
			}

			if _, err = s.Assign(
				ctx,
				"alice",
				Assignment{DeviceID: "d1", ToolID: "0", MaterialUUID: c.UUID, By: ByPerson},
			); err != nil {
				t.Fatal(err)
			}

			if _, err = s.UpdateCustom(ctx, "alice", c.UUID, MaterialFields{
				Name: "Renamed", BrandName: "Me", Class: "FFF", Type: "PETG", Abbreviation: "RN", ColorRGBA: "#ff0000",
			}); err != nil {
				t.Fatal(err)
			}

			a := s.mustAssignments(t, "d1")[0]
			if a.MaterialName != "Renamed" || a.BrandName != "Me" || a.Type != "PETG" || a.Abbreviation != "RN" ||
				a.ColorRGBA != "#ff0000" {
				t.Fatalf("the tool kept the old copy: %+v", a)
			}
		})
	}
}
