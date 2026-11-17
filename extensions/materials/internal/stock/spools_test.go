package stock

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/prusa3d/materials/internal/dbtest"
)

func TestAddingSpoolsCopiesTheMaterial(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, err := s.AddSpools(
				ctx,
				"alice",
				SpoolDraft{MaterialUUID: "m-a", PackageUUID: new("p-a"), RemainingWeightG: 800, Condition: "new"},
				3,
				10,
			)
			if err != nil || len(got) != 3 {
				t.Fatalf("%v %v", got, err)
			}

			page, err := s.Spools(ctx, "m-a", 0, 0)
			if err != nil || len(page.Spools) != 3 || page.Total != 3 || page.Next != nil {
				t.Fatalf("%+v %v", page, err)
			}

			listed := page.Spools

			if sp := listed[0]; sp.MaterialName != "PLA" || sp.BrandName != "Brand a" ||
				sp.MaterialSource != "catalogue" ||
				sp.CreatedBy != "alice" ||
				sp.CreatedAt.IsZero() ||
				sp.Density == nil ||
				*sp.Density != 1.5 {
				t.Fatalf("%+v", sp)
			}
		})
	}
}

func TestAddingOverMaxSpoolsAddsNone(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			d := SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 1, Condition: "new"}

			if _, err := s.AddSpools(ctx, "a", d, 4, 5); err != nil {
				t.Fatal(err)
			}

			if _, err := s.AddSpools(ctx, "a", d, 2, 5); !errors.Is(err, ErrFull) {
				t.Fatalf("want ErrFull, got %v", err)
			}

			if n := count(t, s, "spools"); n != 4 {
				t.Fatalf("count %d", n)
			}
		})
	}
}

func TestAddingRefusesABadDraft(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			ok := SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 1, Condition: "new"}
			neg := -1.0
			over := ok
			over.FullWeightG, over.EmptyWeightG, over.RemainingWeightG = new(1200.0), new(200.0), 1001
			badCond, badWeight, heavy, heavyFull := ok, ok, ok, ok
			badCond.Condition, badWeight.RemainingWeightG = "x", neg
			heavy.RemainingWeightG, heavyFull.FullWeightG = 1e308, new(MaxWeightG+1.0)

			for name, c := range map[string]struct {
				d SpoolDraft
				q int
			}{
				"zero": {ok, 0}, "101": {ok, 101}, "condition": {badCond, 1}, "negative": {badWeight, 1}, "over net": {over, 1},
				"over 100 kg": {heavy, 1}, "full over 100 kg": {heavyFull, 1},
			} {
				if _, err := s.AddSpools(ctx, "a", c.d, c.q, 1000); !errors.Is(err, ErrInvalid) {
					t.Errorf("%s: want ErrInvalid, got %v", name, err)
				}
			}

			if _, err := s.AddSpools(
				ctx,
				"a",
				SpoolDraft{MaterialUUID: "zzz", Condition: "new"},
				1,
				10,
			); !errors.Is(
				err,
				ErrNotFound,
			) {
				t.Errorf("unknown material: %v", err)
			}

			mine, err := s.CreateCustom(ctx, "a", MaterialFields{Name: "Mine", Class: "FFF"})
			if err != nil {
				t.Fatal(err)
			}

			packed := SpoolDraft{MaterialUUID: mine.UUID, PackageUUID: new("no-such-package"), Condition: "new"}
			if _, err = s.AddSpools(ctx, "a", packed, 1, 10); !errors.Is(err, ErrInvalid) {
				t.Errorf("a custom material in a package: %v", err)
			}
		})
	}
}

func TestSpoolsComeInPages(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, err := s.AddSpools(ctx, "a", SpoolDraft{MaterialUUID: "m-a", Condition: "new"}, 3, 10)
			if err != nil {
				t.Fatal(err)
			}

			first, err := s.Spools(ctx, "m-a", 0, 2)
			if err != nil || len(first.Spools) != 2 || first.Total != 3 || first.Next == nil || *first.Next != 2 {
				t.Fatalf("first page %+v %v", first, err)
			}

			second, err := s.Spools(ctx, "m-a", 2, 2)
			if err != nil || len(second.Spools) != 1 || second.Spools[0].ID != got[2].ID || second.Next != nil {
				t.Fatalf("second page %+v %v", second, err)
			}

			// A limit past MaxPage reads MaxPage.
			if all, _ := s.Spools(ctx, "m-a", 0, MaxPage+1); len(all.Spools) != 3 {
				t.Fatalf("all %+v", all)
			}
		})
	}
}

// Every text a person writes has a bound, so 100 of anything fits an answer.
func TestTextFieldsHaveABound(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			long := strings.Repeat

			full := MaterialFields{
				Name: long("n", 200), BrandName: long("b", 200), Class: "FFF", Type: long("t", 200),
				Abbreviation: long("a", 200),
			}
			if _, err := s.CreateCustom(ctx, "a", full); err != nil {
				t.Fatalf("at the bound: %v", err)
			}

			for name, change := range map[string]func(*MaterialFields){
				"name":         func(f *MaterialFields) { f.Name += "x" },
				"brand_name":   func(f *MaterialFields) { f.BrandName += "x" },
				"type":         func(f *MaterialFields) { f.Type += "x" },
				"abbreviation": func(f *MaterialFields) { f.Abbreviation += "x" },
				"control":      func(f *MaterialFields) { f.Name = "a\x00b" },
				"not utf-8":    func(f *MaterialFields) { f.Name = "a\xffb" },
			} {
				f := full
				change(&f)

				if _, err := s.CreateCustom(ctx, "a", f); !errors.Is(err, ErrInvalid) {
					t.Errorf("%s: want ErrInvalid, got %v", name, err)
				}
			}

			d := SpoolDraft{MaterialUUID: "m-a", Condition: "new", Notes: long("n", 2000)}

			got, err := s.AddSpools(ctx, "a", d, 1, 10)
			if err != nil {
				t.Fatalf("draft at the bound: %v", err)
			}

			for name, change := range map[string]func(*SpoolDraft){
				"notes": func(d *SpoolDraft) { d.Notes += "x" },
			} {
				over := d
				change(&over)

				if _, err = s.AddSpools(ctx, "a", over, 1, 10); !errors.Is(err, ErrInvalid) {
					t.Errorf("draft %s: want ErrInvalid, got %v", name, err)
				}
			}

			for name, p := range map[string]SpoolPatch{
				"notes": {Notes: new(long("n", 2001))},
			} {
				if _, err = s.UpdateSpool(ctx, got[0].ID, p); !errors.Is(err, ErrInvalid) {
					t.Errorf("patch %s: want ErrInvalid, got %v", name, err)
				}
			}
		})
	}
}

func TestSpoolsAreDeletedTogetherOrNotAtAll(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, err := s.AddSpools(
				ctx,
				"a",
				SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 5, Condition: "new"},
				3,
				10,
			)
			if err != nil {
				t.Fatal(err)
			}

			for _, bad := range [][]int64{nil, {got[0].ID, got[0].ID}} {
				if err = s.DeleteSpools(ctx, bad, t0); !errors.Is(err, ErrInvalid) {
					t.Fatalf("%v: %v", bad, err)
				}
			}

			// A missing id keeps the spools before it.
			if err = s.DeleteSpools(ctx, []int64{got[0].ID, 9999}, t0); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing id: %v", err)
			}

			if left, _ := s.Spools(ctx, "m-a", 0, 0); left.Total != 3 {
				t.Fatalf("a refused batch deleted %d", 3-left.Total)
			}

			if err = s.DeleteSpools(ctx, []int64{got[0].ID, got[2].ID}, t0); err != nil {
				t.Fatal(err)
			}

			left, _ := s.Spools(ctx, "m-a", 0, 0)
			if left.Total != 1 || left.Spools[0].ID != got[1].ID {
				t.Fatalf("%+v", left)
			}
		})
	}
}

func TestADeletedSpoolIsGoneFromTheMaterial(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, _ := s.AddSpools(
				ctx,
				"a",
				SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 5, Condition: "new"},
				2,
				10,
			)
			if err := s.DeleteSpool(ctx, got[0].ID, t0); err != nil {
				t.Fatal(err)
			}

			if err := s.DeleteSpool(ctx, got[0].ID, t0); !errors.Is(err, ErrNotFound) {
				t.Fatalf("second delete: %v", err)
			}

			if _, err := s.UpdateSpool(ctx, got[0].ID, SpoolPatch{Notes: new("x")}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("update deleted: %v", err)
			}

			left, _ := s.Spools(ctx, "m-a", 0, 0)
			if left.Total != 1 || left.Spools[0].ID != got[1].ID {
				t.Fatalf("%+v", left)
			}

			up, err := s.UpdateSpool(ctx, got[1].ID, SpoolPatch{RemainingWeightG: new(2.0), Notes: new("n")})
			if err != nil || up.RemainingWeightG != 2 || up.Notes != "n" {
				t.Fatalf("%+v %v", up, err)
			}

			if _, err = s.UpdateSpool(ctx, got[1].ID, SpoolPatch{Condition: new("x")}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("bad patch: %v", err)
			}
		})
	}
}

//go:fix inline

func TestLocationsAreSharedNamedOnceAndCountTheirSpoolsInStock(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			shelf, err := s.CreateLocation(ctx, "  Shelf A ")
			if err != nil || shelf.Name != "Shelf A" {
				t.Fatalf("%+v %v", shelf, err)
			}

			// The same place typed again in another case is the place there is.
			again, err := s.CreateLocation(ctx, "SHELF a")
			if err != nil || again.ID != shelf.ID {
				t.Fatalf("%+v %v", again, err)
			}

			box, err := s.CreateLocation(ctx, "dry box")
			if err != nil {
				t.Fatal(err)
			}

			for _, bad := range []string{"", "   ", "two\nlines", strings.Repeat("l", 201)} {
				if _, err = s.CreateLocation(ctx, bad); !errors.Is(err, ErrInvalid) {
					t.Errorf("%.20q: want ErrInvalid, got %v", bad, err)
				}
			}

			added, err := s.AddSpools(ctx, "alice",
				SpoolDraft{MaterialUUID: "m-a", Condition: "new", LocationID: &shelf.ID}, 3, 10)
			if err != nil || added[0].Location != "Shelf A" {
				t.Fatalf("%+v %v", added, err)
			}

			if err = s.DeleteSpool(ctx, added[1].ID, time.Now()); err != nil {
				t.Fatal(err)
			}

			if _, err = s.UpdateSpool(ctx, added[2].ID, SpoolPatch{Archived: new(true)}); err != nil {
				t.Fatal(err)
			}

			got, err := s.Locations(ctx)
			if err != nil || fmt.Sprint(got) != fmt.Sprintf("[{%d dry box 0} {%d Shelf A 1}]", box.ID, shelf.ID) {
				t.Fatalf("%+v %v", got, err)
			}

			if _, err = s.AddSpools(
				ctx,
				"alice",
				SpoolDraft{
					MaterialUUID: "m-a",
					Condition:    "new",
					LocationID:   new(int64(999)),
				},
				1,
				10,
			); !errors.Is(err, ErrInvalid) {
				t.Fatalf("an unknown location: want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestARenameReachesEverySpoolAndRefusesATakenName(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			shelf, _ := s.CreateLocation(ctx, "Shelf")
			box, _ := s.CreateLocation(ctx, "Box")

			added, err := s.AddSpools(ctx, "alice",
				SpoolDraft{MaterialUUID: "m-a", Condition: "new", LocationID: &shelf.ID}, 1, 10)
			if err != nil {
				t.Fatal(err)
			}

			if _, err = s.RenameLocation(ctx, shelf.ID, "box"); !errors.Is(err, ErrTaken) {
				t.Fatalf("want ErrTaken, got %v", err)
			}

			// A change of case of its own name is no clash.
			if _, err = s.RenameLocation(ctx, box.ID, "BOX"); err != nil {
				t.Fatal(err)
			}

			if _, err = s.RenameLocation(ctx, 999, "Attic"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}

			if _, err = s.RenameLocation(ctx, shelf.ID, "Top shelf"); err != nil {
				t.Fatal(err)
			}

			page, err := s.Spools(ctx, "m-a", 0, 0)
			if err != nil || page.Spools[0].ID != added[0].ID || page.Spools[0].Location != "Top shelf" {
				t.Fatalf("%+v %v", page, err)
			}
		})
	}
}

func TestDeletingALocationLeavesItsSpoolsWithNone(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			shelf, _ := s.CreateLocation(ctx, "Shelf")

			added, err := s.AddSpools(ctx, "alice",
				SpoolDraft{MaterialUUID: "m-a", Condition: "new", LocationID: &shelf.ID}, 2, 10)
			if err != nil {
				t.Fatal(err)
			}

			if err = s.DeleteSpool(ctx, added[1].ID, time.Now()); err != nil {
				t.Fatal(err)
			}

			if err = s.DeleteLocation(ctx, shelf.ID); err != nil {
				t.Fatal(err)
			}

			page, err := s.Spools(ctx, "m-a", 0, 0)
			if err != nil || page.Spools[0].LocationID != nil || page.Spools[0].Location != "" {
				t.Fatalf("%+v %v", page, err)
			}

			var held int
			if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM spools WHERE location_id IS NOT NULL`).
				Scan(&held); err != nil || held != 0 {
				t.Fatalf("%d spools still point at it, %v", held, err)
			}

			if err = s.DeleteLocation(ctx, shelf.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
		})
	}
}

func TestAPatchMovesASpoolAndZeroTakesItOut(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			shelf, _ := s.CreateLocation(ctx, "Shelf")

			added, err := s.AddSpools(ctx, "alice", SpoolDraft{MaterialUUID: "m-a", Condition: "new"}, 1, 10)
			if err != nil {
				t.Fatal(err)
			}

			sp, err := s.UpdateSpool(ctx, added[0].ID, SpoolPatch{LocationID: &shelf.ID})
			if err != nil || sp.Location != "Shelf" || *sp.LocationID != shelf.ID {
				t.Fatalf("%+v %v", sp, err)
			}

			if sp, err = s.UpdateSpool(ctx, added[0].ID, SpoolPatch{LocationID: new(int64(0))}); err != nil ||
				sp.LocationID != nil || sp.Location != "" {
				t.Fatalf("%+v %v", sp, err)
			}

			if _, err = s.UpdateSpool(
				ctx,
				added[0].ID,
				SpoolPatch{LocationID: new(int64(42))},
			); !errors.Is(
				err,
				ErrInvalid,
			) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

// TestAnIntegerOver32BitsIsKeptOrRefusedForWhatWasSent. SQLite keeps 64
// bits and Postgres's INTEGER 32, so Postgres refuses the spool, and that
// refusal reads as the caller's to fix.
func TestAnIntegerOver32BitsIsKeptOrRefusedForWhatWasSent(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			long := SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 1, Condition: "new", LengthMM: new(1 << 40)}

			if _, err := s.AddSpools(ctx, "a", long, 1, 10); err != nil && !Refused(err) {
				t.Fatalf("a length over 32 bits failed as the store's own fault: %v", err)
			}
		})
	}
}

// TestTheHundredAndFirstLocationIsRefused: GET /locations answers every
// location, so their count is what keeps the answer inside the body bound.
// A known name still picks its place.
func TestTheHundredAndFirstLocationIsRefused(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			for n := range MaxLocations {
				if _, err := s.CreateLocation(ctx, fmt.Sprintf("shelf %d", n)); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := s.CreateLocation(ctx, "one more"); !errors.Is(err, ErrTooManyLocations) {
				t.Fatalf("location %d: want ErrTooManyLocations, got %v", MaxLocations+1, err)
			}

			if _, err := s.CreateLocation(ctx, "SHELF 7"); err != nil {
				t.Fatalf("a known name: %v", err)
			}
		})
	}
}

func TestAddedSpoolsAreManualWithTheirOwnUUID(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()

			got, err := s.AddSpools(
				ctx,
				"alice",
				SpoolDraft{MaterialUUID: "m-a", RemainingWeightG: 800, Condition: "new"},
				3,
				10,
			)
			if err != nil || len(got) != 3 {
				t.Fatalf("%v %v", got, err)
			}

			page, err := s.Spools(ctx, "m-a", 0, 0)
			if err != nil {
				t.Fatal(err)
			}

			seen := map[string]bool{}

			for i, sp := range append(got, page.Spools...) {
				if _, err = uuid.Parse(sp.UUID); err != nil || sp.Source != SourceManual {
					t.Fatalf("spool %d = %q, %q, %v", i, sp.UUID, sp.Source, err)
				}

				seen[sp.UUID] = true
			}

			if len(seen) != 3 {
				t.Fatalf("the spools share uuids: %v", seen)
			}
		})
	}
}

func TestAReportedSpoolIsOneSpoolPerTag(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			tag := ReportedSpool{UUID: "tag-1", MaterialUUID: "m-a", Abbreviation: "PLA", FullWeightG: new(1000.0)}

			first, err := s.AddReported(ctx, "alice", tag, 10)
			if err != nil || first.Source != SourcePrinter || first.MaterialName != "PLA" ||
				first.RemainingWeightG != 1000 {
				t.Fatalf("%+v %v", first, err)
			}

			// A tag of a deleted spool brings that spool back.
			if err = s.DeleteSpool(ctx, first.ID, t0); err != nil {
				t.Fatal(err)
			}

			if again, againErr := s.AddReported(ctx, "alice", tag, 10); againErr != nil || again.ID != first.ID {
				t.Fatalf("the tag of a deleted spool added %+v (%v)", again, againErr)
			}

			manual := addSpools(t, s, "alice", "m-a", 1, 300)[0]
			if _, err = s.MergeSpool(ctx, first.ID, manual.ID, t0); !errors.Is(err, ErrMergeRefused) {
				t.Fatalf("a printer spool merged into a manual one: %v", err)
			}

			merged, err := s.MergeSpool(ctx, manual.ID, first.ID, t0)
			if err != nil || merged.UUID != "tag-1" || merged.RemainingWeightG != 1000 {
				t.Fatalf("%+v %v", merged, err)
			}

			if page, pageErr := s.Spools(ctx, "m-a", 0, 0); pageErr != nil || page.Total != 1 {
				t.Fatalf("the merge left %+v (%v)", page, pageErr)
			}
		})
	}
}

// A tag that carries no full weight takes its package's nominal one.
func TestAReportedSpoolTakesItsPackagesWeight(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			used := 233.0

			sp, err := s.AddReported(context.Background(), "alice", ReportedSpool{
				UUID: "tag-1", MaterialUUID: "m-1", PackageUUID: "p-1", Abbreviation: "PLA", ConsumedG: &used,
			}, 10)
			if err != nil {
				t.Fatal(err)
			}

			if sp.FullWeightG == nil || *sp.FullWeightG != 1000 || sp.RemainingWeightG != 767 ||
				sp.PackageUUID == nil || *sp.PackageUUID != "p-1" {
				t.Errorf("a 1 kg package with 233 g used: %+v", sp)
			}
		})
	}
}
