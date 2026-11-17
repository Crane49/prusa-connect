package stock

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/materials/internal/dbtest"
)

func TestAnArchivedSpoolLeavesStockButNotTheList(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			spools := addSpools(t, s, "alice", "m-a", 2, 300)

			for _, bad := range []SpoolPatch{
				{ArchiveReason: new("lost")},
				{Archived: new(true), ArchiveReason: new("stolen")},
				{Archived: new(true), ArchiveReasonNote: new(strings.Repeat("é", maxArchiveNote+1))},
			} {
				if _, err := s.UpdateSpool(ctx, spools[0].ID, bad); !errors.Is(err, ErrInvalid) {
					t.Fatalf("%+v: %v", bad, err)
				}
			}

			archived, err := s.UpdateSpool(ctx, spools[0].ID, SpoolPatch{
				Archived:          new(true),
				ArchiveReason:     new("lost"),
				ArchiveReasonNote: new(strings.Repeat("é", maxArchiveNote)),
			})
			if err != nil || archived.ArchivedAt == nil || archived.ArchiveReason != "lost" {
				t.Fatalf("%+v %v", archived, err)
			}

			// It is listed, but neither in the stock totals nor against the limit.
			if page, pageErr := s.Spools(ctx, "m-a", 0, 0); pageErr != nil || page.Total != 2 ||
				page.Spools[0].ArchivedAt == nil {
				t.Fatalf("%+v %v", page, pageErr)
			}

			p, err := s.List(ctx, "alice", ListQuery{})
			if err != nil || p.StockSpools != 1 || p.StockG != 300 {
				t.Fatalf("stock %d spools, %v g (%v)", p.StockSpools, p.StockG, err)
			}

			if _, err = s.AddSpools(ctx, "alice", SpoolDraft{MaterialUUID: "m-a"}, 1, 2); err != nil {
				t.Fatalf("an archived spool counted against the limit: %v", err)
			}

			// A person cannot put it on a tool, nor merge it.
			onTool := Assignment{DeviceID: "d1", ToolID: "0", SpoolID: &archived.ID, By: ByPerson}
			if _, err = s.Assign(ctx, "alice", onTool); !errors.Is(err, ErrSpoolArchived) {
				t.Fatalf("assign: %v", err)
			}

			// Restoring clears the reason and the note.
			back, err := s.UpdateSpool(ctx, archived.ID, SpoolPatch{Archived: new(false)})
			if err != nil || back.ArchivedAt != nil || back.ArchiveReason != "" || back.ArchiveReasonNote != "" {
				t.Fatalf("%+v %v", back, err)
			}
		})
	}
}

func TestAPrinterReadingAnArchivedSpoolRestoresIt(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := newStore(t, engine), context.Background()
			tag := ReportedSpool{UUID: "tag-1", MaterialUUID: "m-a", Abbreviation: "PLA", FullWeightG: new(1000.0)}

			archive := func() Spool {
				t.Helper()

				sp, err := s.AddReported(ctx, "alice", tag, 10)
				if err == nil {
					sp, err = s.UpdateSpool(ctx, sp.ID, SpoolPatch{Archived: new(true), ArchiveReason: new("other")})
				}

				if err != nil || sp.ArchivedAt == nil {
					t.Fatalf("%+v %v", sp, err)
				}

				return sp
			}

			sp := archive()

			manual := addSpools(t, s, "alice", "m-a", 1, 300)[0]
			if _, err := s.MergeSpool(ctx, manual.ID, sp.ID, t0); !errors.Is(err, ErrSpoolArchived) {
				t.Fatalf("merge: %v", err)
			}

			// Adding the tag again, from a printer's page.
			if again, err := s.AddReported(ctx, "alice", tag, 0); err != nil || again.ID != sp.ID ||
				again.ArchivedAt != nil || again.ArchiveReason != "" {
				t.Fatalf("%+v %v", again, err)
			}

			// The printer reporting it on a tool.
			sp = archive()
			read := Assignment{DeviceID: "d1", ToolID: "0", SpoolID: &sp.ID, Abbreviation: "PLA"}
			if _, err := s.Report(ctx, read, nil); err != nil {
				t.Fatal(err)
			}

			if got, _, err := s.Spool(ctx, sp.ID); err != nil || got.ArchivedAt != nil || got.Tool == nil {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
