package stock

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/materials/internal/dbtest"
)

// metre is what 1000 mm of 1.75 mm PLA weighs at 1.24 g/cm³.
const metre = 2.98255

// odometerAt is a reading of tool 0's odometer, under odometer id 7.
func odometerAt(e float64) Reading {
	id := int64(7)

	return Reading{E: &e, ID: &id}
}

// onTool puts a PLA spool of grams onto d1's tool 0 and answers it.
func onTool(t *testing.T, s *Store, grams float64) Spool {
	t.Helper()

	sp := addSpools(t, s, "alice", "m-1", 1, grams)[0]
	if _, err := s.Assign(context.Background(), "alice",
		Assignment{DeviceID: "d1", ToolID: "0", SpoolID: &sp.ID, By: ByPerson}); err != nil {
		t.Fatal(err)
	}

	return sp
}

// taggedOnTool puts a spool a printer read off tag-1, full at grams, onto
// d1's tool 0 and answers it.
func taggedOnTool(t *testing.T, s *Store, grams float64) Spool {
	t.Helper()

	ctx := context.Background()

	sp, err := s.AddReported(ctx, "alice",
		ReportedSpool{UUID: "tag-1", MaterialUUID: "m-1", Abbreviation: "PLA", FullWeightG: &grams}, 10)
	if err == nil {
		_, err = s.Assign(ctx, "alice", Assignment{DeviceID: "d1", ToolID: "0", SpoolID: &sp.ID, By: ByPerson})
	}

	if err != nil {
		t.Fatal(err)
	}

	return sp
}

func consumeAll(t *testing.T, s *Store, readings ...Reading) {
	t.Helper()

	for _, r := range readings {
		if err := s.Consume(context.Background(), "d1", "0", r); err != nil {
			t.Fatal(err)
		}
	}
}

// weights answers a spool's remaining weight and its consumed total.
func weights(t *testing.T, s *Store, id int64) (remaining, consumed float64) {
	t.Helper()

	if err := s.db.QueryRowContext(
		context.Background(),
		`SELECT remaining_weight_g, consumed_weight_g FROM spools WHERE id = $1`,
		id,
	).Scan(&remaining, &consumed); err != nil {
		t.Fatal(err)
	}

	return remaining, consumed
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-4 }

func TestConsumeTakesTheFedFilamentOffTheSpool(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1000)

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			if left, used := weights(t, s, sp.ID); !near(left, 1000-metre) || !near(used, metre) {
				t.Errorf("a metre fed leaves %v g, %v g used", left, used)
			}
		})
	}
}

func TestConsumeCreditsAReadingOnce(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1000)

			consumeAll(t, s, odometerAt(100), odometerAt(1100), odometerAt(1100))

			if left, _ := weights(t, s, sp.ID); !near(left, 1000-metre) {
				t.Errorf("a reading sent twice leaves %v g", left)
			}
		})
	}
}

func TestConsumeCreditsNothingOnAReset(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1000)
			e, rebooted := 5000.0, int64(8)

			consumeAll(t, s, odometerAt(100), Reading{E: &e, ID: &rebooted})

			if left, _ := weights(t, s, sp.ID); left != 1000 {
				t.Fatalf("a new odometer id was charged: %v g left", left)
			}

			e = 6000
			consumeAll(t, s, Reading{E: &e, ID: &rebooted})

			if left, _ := weights(t, s, sp.ID); !near(left, 1000-metre) {
				t.Errorf("the count after the reset leaves %v g", left)
			}

			consumeAll(t, s, odometerAt(40))

			if left, _ := weights(t, s, sp.ID); !near(left, 1000-metre) {
				t.Errorf("a lower reading was charged: %v g left", left)
			}
		})
	}
}

func TestConsumeStopsAtZero(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1)

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			if left, used := weights(t, s, sp.ID); left != 0 || !near(used, metre) {
				t.Errorf("a spool used past empty: %v g left, %v g used", left, used)
			}
		})
	}
}

func TestConsumeCountsNothingBeforeTheRow(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			sp := onTool(t, s, 1000)
			consumeAll(t, s, odometerAt(2100))

			if left, _ := weights(t, s, sp.ID); left != 1000 {
				t.Fatalf("the first reading after the assign was charged: %v g left", left)
			}

			consumeAll(t, s, odometerAt(3100))

			if left, _ := weights(t, s, sp.ID); !near(left, 1000-metre) {
				t.Errorf("the next reading leaves %v g", left)
			}
		})
	}
}

func TestConsumeKeepsTheReadingOfABareMaterial(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := listStore(t, engine), context.Background()

			if _, err := s.Assign(ctx, "alice",
				Assignment{DeviceID: "d1", ToolID: "0", MaterialUUID: "m-1", By: ByPerson}); err != nil {
				t.Fatal(err)
			}

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			sp := onTool(t, s, 1000)

			if left, _ := weights(t, s, sp.ID); left != 1000 {
				t.Fatalf("the bare material's metre went onto the spool: %v g left", left)
			}

			// The spool is charged from the last reading before it went on.
			consumeAll(t, s, odometerAt(2100))

			if left, _ := weights(t, s, sp.ID); !near(left, 1000-metre) {
				t.Errorf("the metre after the spool went on leaves %v g", left)
			}
		})
	}
}

func TestConsumeChargesANewSpoolFromTheLastReading(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			first := onTool(t, s, 1000)

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			second := onTool(t, s, 1000)
			consumeAll(t, s, odometerAt(2100))

			if left, _ := weights(t, s, second.ID); !near(left, 1000-metre) {
				t.Fatalf("the metre fed after the swap leaves the new spool %v g", left)
			}

			if left, _ := weights(t, s, first.ID); !near(left, 1000-metre) {
				t.Errorf("the old spool kept %v g", left)
			}
		})
	}
}

func TestConsumeTakesTheTagWeight(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := taggedOnTool(t, s, 1000)
			tag := func(e, grams float64, uuid string) Reading {
				r := odometerAt(e)
				r.TagConsumedG, r.TagUUID = &grams, uuid

				return r
			}

			consumeAll(t, s, tag(100, 50, sp.UUID))

			if left, used := weights(t, s, sp.ID); left != 950 || used != 50 {
				t.Fatalf("the first tag reading: %v g left, %v g used", left, used)
			}

			consumeAll(t, s, tag(1100, 60, sp.UUID))

			if left, used := weights(t, s, sp.ID); left != 940 || used != 60 {
				t.Fatalf("ten grams more on the tag, and a metre fed: %v g left, %v g used", left, used)
			}

			consumeAll(t, s, tag(2100, 500, "another"))

			if left, used := weights(t, s, sp.ID); !near(left, 940-metre) || !near(used, 60+metre) {
				t.Errorf("another spool's tag: %v g left, %v g used", left, used)
			}
		})
	}
}

func TestConsumeTakesTheTagAfterAnOdometerCount(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := taggedOnTool(t, s, 1000)
			used := 300.0

			consumeAll(t, s, odometerAt(100), odometerAt(1100))

			r := odometerAt(1100)
			r.TagConsumedG, r.TagUUID = &used, sp.UUID
			consumeAll(t, s, r)

			if left, total := weights(t, s, sp.ID); left != 700 || total != 300 {
				t.Errorf("a tag read after a metre counted: %v g left, %v g used", left, total)
			}
		})
	}
}

func TestAReportedSpoolKeepsItsTagTotal(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			full, used := 1000.0, 120.0

			sp, err := s.AddReported(context.Background(), "alice",
				ReportedSpool{UUID: "tag-1", MaterialUUID: "m-1", FullWeightG: &full, ConsumedG: &used}, 1000)
			if err != nil {
				t.Fatal(err)
			}

			if left, total := weights(t, s, sp.ID); left != 880 || total != 120 {
				t.Errorf("a reported spool: %v g left, %v g used", left, total)
			}
		})
	}
}

func TestATagNamingAManualSpoolIsNotTrusted(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s, ctx := listStore(t, engine), context.Background()
			sp := onTool(t, s, 500)

			// A manual spool's uuid is shown to anyone who reads the stock.
			if _, known, err := s.SpoolByUUID(ctx, sp.UUID); err != nil || known {
				t.Fatalf("a report of a manual spool's uuid found it: %v %v", known, err)
			}

			if _, err := s.AddReported(
				ctx,
				"alice",
				ReportedSpool{
					UUID:         sp.UUID,
					MaterialUUID: "m-1",
					Abbreviation: "PLA",
				},
				10,
			); !errors.Is(err, ErrInvalid) {
				t.Fatalf("adding a tag that names a manual spool: %v", err)
			}

			drain := odometerAt(100)
			grams := 5000.0
			drain.TagConsumedG, drain.TagUUID = &grams, sp.UUID
			consumeAll(t, s, drain)

			if left, used := weights(t, s, sp.ID); left != 500 || used != 0 {
				t.Errorf("a tag naming the manual spool drained it: %v g left, %v g used", left, used)
			}
		})
	}
}

func TestConsumeIgnoresAStaleReading(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1000)

			// 250 under the same odometer id came late: the count is 1000 mm.
			consumeAll(t, s, odometerAt(0), odometerAt(500), odometerAt(250), odometerAt(1000))

			if left, used := weights(t, s, sp.ID); !near(left, 1000-metre) || !near(used, metre) {
				t.Errorf("a stale reading was counted: %v g left, %v g used", left, used)
			}
		})
	}
}

func TestConsumeCountsOneReadingOnceAcrossWriters(t *testing.T) {
	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			s := listStore(t, engine)
			sp := onTool(t, s, 1000)
			consumeAll(t, s, odometerAt(0))

			// Many writers holding the same reading: the guard on the last
			// reading lets one of them count it.
			const writers = 64

			var wg sync.WaitGroup

			errs := make(chan error, writers)

			for range writers {
				wg.Go(func() {
					if err := s.Consume(context.Background(), "d1", "0", odometerAt(1000)); err != nil {
						errs <- err
					}
				})
			}

			wg.Wait()
			close(errs)

			for err := range errs {
				// SQLite serialises the writers; a busy one is not a count.
				if !strings.Contains(err.Error(), "database is locked") {
					t.Fatal(err)
				}
			}

			if left, used := weights(t, s, sp.ID); !near(left, 1000-metre) || !near(used, metre) {
				t.Errorf("one reading was counted more than once: %v g left, %v g used", left, used)
			}
		})
	}
}
