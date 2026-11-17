package threemf_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/prusa3d/threemf/threemf"
)

// copiesOf counts the positions of a plate by item.
func copiesOf(plate threemf.Plate) map[int]int {
	out := map[int]int{}
	for _, position := range plate.Positions {
		out[position.Item]++
	}

	return out
}

func TestArrangePutsCopiesOntoAsFewPlatesAsHoldThem(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	// CubeA is 10 by 10 mm. With a 6 mm gap, and 3 mm kept at each edge of
	// the 250 by 210 mm bed, a row holds 15 and a plate 13 rows.
	plates, err := threemf.Arrange(p, []threemf.Piece{{Item: 0, Copies: 200}}, threemf.ArrangeOptions{})
	if err != nil {
		t.Fatalf("Arrange: %v", err)
	}

	if len(plates) != 2 {
		t.Fatalf("%d plates, want 2", len(plates))
	}

	if got := len(plates[0].Positions) + len(plates[1].Positions); got != 200 {
		t.Errorf("%d copies placed, want 200", got)
	}

	if len(plates[0].Positions) != 195 {
		t.Errorf("the first plate holds %d copies, want 195", len(plates[0].Positions))
	}

	for i, plate := range plates {
		checkPlate(t, p, plate, threemf.DefaultGap, i)
	}
}

func TestArrangeMakesEqualPlatesOfEqualCopies(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture3Alpha))

	// The MINI setup, a bed of 180 by 180 mm.
	plates, err := threemf.Arrange(p, []threemf.Piece{{Item: 0, Copies: 20}}, threemf.ArrangeOptions{Setup: 1, Gap: 60})
	if err != nil {
		t.Fatalf("Arrange: %v", err)
	}

	if len(plates) < 2 {
		t.Fatalf("%d plates, want several", len(plates))
	}

	for i := 1; i < len(plates)-1; i++ {
		if !slices.Equal(plates[i].Positions, plates[0].Positions) {
			t.Errorf("plate %d differs from plate 0", i)
		}
	}

	total := 0
	for i, plate := range plates {
		total += len(plate.Positions)
		checkPlate(t, p, plate, 60, i)
	}

	if total != 20 {
		t.Errorf("%d copies placed, want 20", total)
	}
}

func TestArrangeMixesObjectsLargestFirst(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	// The bracket is the largest of the three objects, and is placed first.
	plates, err := threemf.Arrange(p, []threemf.Piece{{Item: 0, Copies: 3}, {Item: 6, Copies: 2}, {Item: 4, Copies: 1}},
		threemf.ArrangeOptions{})
	if err != nil {
		t.Fatalf("Arrange: %v", err)
	}

	if len(plates) != 1 {
		t.Fatalf("%d plates, want 1", len(plates))
	}

	if got := copiesOf(plates[0]); got[0] != 3 || got[6] != 2 || got[4] != 1 {
		t.Errorf("copies %v", got)
	}

	want := []threemf.Piece{{Item: 0, Copies: 3}, {Item: 4, Copies: 1}, {Item: 6, Copies: 2}}
	if got := plates[0].Pieces(); !slices.Equal(got, want) {
		t.Errorf("Pieces = %v", got)
	}

	checkPlate(t, p, plates[0], threemf.DefaultGap, 0)
}

func TestArrangeRefusesWhatItCannotPlace(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	cases := []struct {
		name   string
		pieces []threemf.Piece
		opts   threemf.ArrangeOptions
		want   error
	}{
		{"no piece", nil, threemf.ArrangeOptions{}, threemf.ErrSelection},
		{"no copies", []threemf.Piece{{Item: 0}}, threemf.ArrangeOptions{}, threemf.ErrSelection},
		{
			"an item twice",
			[]threemf.Piece{{Item: 0, Copies: 1}, {Item: 0, Copies: 1}},
			threemf.ArrangeOptions{},
			threemf.ErrSelection,
		},
		{"no such item", []threemf.Piece{{Item: 99, Copies: 1}}, threemf.ArrangeOptions{}, threemf.ErrSelection},
		{
			"no such setup",
			[]threemf.Piece{{Item: 0, Copies: 1}},
			threemf.ArrangeOptions{Setup: 3},
			threemf.ErrSelection,
		},
		{
			"too many copies",
			[]threemf.Piece{{Item: 0, Copies: threemf.MaxCopies + 1}},
			threemf.ArrangeOptions{},
			threemf.ErrSelection,
		},
		{
			"a gap wider than the bed",
			[]threemf.Piece{{Item: 0, Copies: 1}},
			threemf.ArrangeOptions{Gap: 300},
			threemf.ErrDoesNotFit,
		},
		{
			"more plates than allowed",
			[]threemf.Piece{{Item: 0, Copies: 400}},
			threemf.ArrangeOptions{MaxPlates: 1},
			threemf.ErrDoesNotFit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := threemf.Arrange(p, tc.pieces, tc.opts); !errors.Is(err, tc.want) {
				t.Errorf("Arrange = %v, want %v", err, tc.want)
			}
		})
	}
}

// checkPlate checks every copy of a plate stands on the bed of setup 0 or the
// setup the plate was made for, and that no two copies come closer than gap.
func checkPlate(t *testing.T, p *threemf.Project, plate threemf.Plate, gap float64, index int) {
	t.Helper()

	type box struct{ lo, hi threemf.Point }

	var boxes []box

	for _, position := range plate.Positions {
		ref, _ := p.Item(position.Item)
		inst := p.Instance(ref)
		w := (inst.Bounds.Max[0] - inst.Bounds.Min[0]) / 2
		d := (inst.Bounds.Max[1] - inst.Bounds.Min[1]) / 2
		boxes = append(boxes, box{
			lo: threemf.Point{X: position.X - w, Y: position.Y - d},
			hi: threemf.Point{X: position.X + w, Y: position.Y + d},
		})
	}

	for i, a := range boxes {
		if a.lo.X < -1e-6 || a.lo.Y < -1e-6 {
			t.Errorf("plate %d: copy %d starts at %v, off the bed", index, i, a.lo)
		}

		for j := i + 1; j < len(boxes); j++ {
			b := boxes[j]
			dx := math.Max(b.lo.X-a.hi.X, a.lo.X-b.hi.X)
			dy := math.Max(b.lo.Y-a.hi.Y, a.lo.Y-b.hi.Y)

			if math.Max(dx, dy) < gap-1e-6 {
				t.Errorf(
					"plate %d: copies %d and %d stand %.3f mm apart, want %.0f",
					index,
					i,
					j,
					math.Max(dx, dy),
					gap,
				)
			}
		}
	}
}

func TestRewritePlacesCopiesOnTheChosenBed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fixture string
		bed     threemf.BedRef
		item    int
		setup   int
	}{
		// CubeB of bed 0, copied onto bed 2, which PrusaSlicer 2 then
		// slices as bed 0.
		{"2.9.6 onto bed 3", fixture296, threemf.BedRef{Bed: 2}, 4, 0},
		{"2.9.6 onto bed 1", fixture296, threemf.BedRef{}, 6, 0},
		{"3.0 onto CORE One bed 2", fixture3Alpha, threemf.BedRef{Bed: 1}, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := readFile(t, tc.fixture)
			p := read(t, data)

			plates, err := threemf.Arrange(
				p,
				[]threemf.Piece{{Item: tc.item, Copies: 5}},
				threemf.ArrangeOptions{Setup: tc.setup},
			)
			if err != nil || len(plates) != 1 {
				t.Fatalf("Arrange: %v, %d plates", err, len(plates))
			}

			out := rewrite(t, data, p, threemf.Selection{Bed: tc.bed, Positions: plates[0].Positions})
			q := read(t, out)

			if q.Items() != p.Items()+4 {
				t.Fatalf("%d build items, want %d", q.Items(), p.Items()+4)
			}

			ref, _ := p.Item(tc.item)
			printed := 0

			for o, object := range q.Objects {
				for _, inst := range object.Instances {
					if !inst.Printable {
						continue
					}

					printed++

					if o != ref.Object {
						t.Errorf("item %d of object %d is printable", inst.Item, o)
					}

					if inst.Placement != threemf.PlacementInside || inst.Bed == nil {
						t.Errorf("item %d stands %s on %v", inst.Item, inst.Placement, inst.Bed)
					}
				}
			}

			if printed != 5 {
				t.Errorf("%d printable copies, want 5", printed)
			}
		})
	}
}

func TestRewriteRefusesPositionsTheBedCannotHold(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)
	p := read(t, data)

	cases := []struct {
		name string
		sel  threemf.Selection
	}{
		{"off the bed", threemf.Selection{Positions: []threemf.Position{{Item: 0, X: 249, Y: 100}}}},
		{
			"two copies in one spot",
			threemf.Selection{Positions: []threemf.Position{{Item: 0, X: 100, Y: 100}, {Item: 0, X: 104, Y: 100}}},
		},
		{
			"items beside positions",
			threemf.Selection{Items: []int{0}, Positions: []threemf.Position{{Item: 0, X: 100, Y: 100}}},
		},
		{"no such item", threemf.Selection{Positions: []threemf.Position{{Item: 42, X: 100, Y: 100}}}},
		{"no number", threemf.Selection{Positions: []threemf.Position{{Item: 0, X: math.NaN(), Y: 100}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := threemf.Rewrite(
				t.Context(),
				io.Discard,
				bytes.NewReader(data),
				int64(len(data)),
				p,
				tc.sel,
				threemf.Limits{},
			)
			if !errors.Is(err, threemf.ErrSelection) {
				t.Errorf("Rewrite = %v, want ErrSelection", err)
			}
		})
	}
}

// TestRewrittenCopiesSliceWithPrusaSlicer slices arranged copies with the real
// command line and counts the objects the G-code labels. It runs when
// THREEMF_PRUSASLICER2 or THREEMF_PRUSASLICER3 names a binary, as
// TestRewriteSlicesWithPrusaSlicer does.
func TestRewrittenCopiesSliceWithPrusaSlicer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		env     string
		fixture string
		bed     threemf.BedRef
		piece   threemf.Piece
	}{
		{
			"2.9.6 seven CubeB on bed 3",
			"THREEMF_PRUSASLICER2",
			fixture296,
			threemf.BedRef{Bed: 2},
			threemf.Piece{Item: 4, Copies: 7},
		},
		{
			"3.0 six copies on CORE One bed 2",
			"THREEMF_PRUSASLICER3",
			fixture3Alpha,
			threemf.BedRef{Bed: 1},
			threemf.Piece{Item: 0, Copies: 6},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			binary := os.Getenv(tc.env)
			if binary == "" {
				t.Skipf("%s names no PrusaSlicer binary", tc.env)
			}

			data := readFile(t, tc.fixture)
			p := read(t, data)

			plates, err := threemf.Arrange(p, []threemf.Piece{tc.piece}, threemf.ArrangeOptions{Setup: tc.bed.Setup})
			if err != nil || len(plates) != 1 {
				t.Fatalf("Arrange: %v, %d plates", err, len(plates))
			}

			dir := t.TempDir()
			project := filepath.Join(dir, "copies.3mf")
			gcode := filepath.Join(dir, "copies.gcode")

			if err = os.WriteFile(
				project,
				rewrite(t, data, p, threemf.Selection{Bed: tc.bed, Positions: plates[0].Positions}),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			cmd := exec.CommandContext(t.Context(), binary,
				"--datadir", filepath.Join(dir, "datadir"), "--export-gcode", "--output", gcode, project)

			if out, runErr := cmd.CombinedOutput(); runErr != nil {
				t.Fatalf("%s: %v\n%s", binary, runErr, out)
			}

			if labels := objectsInfoOf(t, gcode); len(labels) != tc.piece.Copies {
				t.Errorf("the G-code labels %d objects, want %d", len(labels), tc.piece.Copies)
			}
		})
	}
}

// A copy that fits a bed only turned is turned by a right angle, and one that
// fits as it stands is not.
func TestArrangeTurnsACopyThatFitsOnlyTurned(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))

	// CubeB, item 4, is longer one way than the other. With 3 mm kept at
	// each edge, a bed a little longer than CubeB the other way, and too
	// short for it the way it stands, holds it only turned.
	ref, _ := p.Item(4)
	box := p.Instance(ref).Bounds
	width, depth := box.Max[0]-box.Min[0], box.Max[1]-box.Min[1]

	if math.Abs(width-depth) < 2 {
		t.Fatalf("CubeB is %.1f by %.1f mm, too near a square to need a turn", width, depth)
	}

	edge := 2 * threemf.DefaultGap / 2
	bedX, bedY := depth+edge+1, width+edge+1

	p.Setups[0].BedShape = []threemf.Point{{X: 0, Y: 0}, {X: bedX, Y: 0}, {X: bedX, Y: bedY}, {X: 0, Y: bedY}}

	plates, err := threemf.Arrange(p, []threemf.Piece{{Item: 4, Copies: 1}}, threemf.ArrangeOptions{})
	if err != nil || len(plates) != 1 {
		t.Fatalf("Arrange: %v, %d plates", err, len(plates))
	}

	if got := plates[0].Positions[0].Rotate; got != threemf.RightAngle {
		t.Errorf("the copy is turned by %d degrees, want %d", got, threemf.RightAngle)
	}

	p.Setups[0].BedShape = []threemf.Point{{X: 0, Y: 0}, {X: 250, Y: 0}, {X: 250, Y: 210}, {X: 0, Y: 210}}

	plates, err = threemf.Arrange(p, []threemf.Piece{{Item: 4, Copies: 1}}, threemf.ArrangeOptions{})
	if err != nil || plates[0].Positions[0].Rotate != 0 {
		t.Errorf("a copy that fits as it stands was turned: %v %+v", err, plates)
	}
}

// A turned copy is written turned: its box in the rewritten project is as
// wide as the item is deep.
func TestRewriteTurnsATurnedCopy(t *testing.T) {
	t.Parallel()

	data := readFile(t, fixture296)
	p := read(t, data)

	ref, _ := p.Item(4)
	before := p.Instance(ref).Bounds

	out := rewrite(t, data, p, threemf.Selection{Positions: []threemf.Position{
		{Item: 4, X: 125, Y: 105, Rotate: threemf.RightAngle},
	}})
	q := read(t, out)

	after := q.Instance(ref).Bounds
	width := func(b threemf.Box) float64 { return b.Max[0] - b.Min[0] }
	depth := func(b threemf.Box) float64 { return b.Max[1] - b.Min[1] }

	if math.Abs(width(after)-depth(before)) > 1e-6 || math.Abs(depth(after)-width(before)) > 1e-6 {
		t.Errorf("the copy is %.3f by %.3f mm, want %.3f by %.3f", width(after), depth(after),
			depth(before), width(before))
	}

	err := threemf.Rewrite(t.Context(), io.Discard, bytes.NewReader(data), int64(len(data)), p,
		threemf.Selection{Positions: []threemf.Position{{Item: 4, X: 125, Y: 105, Rotate: 45}}}, threemf.Limits{})
	if !errors.Is(err, threemf.ErrSelection) {
		t.Errorf("a turn of 45 degrees was taken: %v", err)
	}
}

// A setup that prints with a wipe tower and names no place for it keeps the
// place PrusaSlicer gives the tower free.
func TestArrangeKeepsTheDefaultWipeTowerFree(t *testing.T) {
	t.Parallel()

	p := read(t, readFile(t, fixture296))
	p.Extruders = []int{1, 2}
	p.Setups[0].Settings = []threemf.SettingsGroup{{Name: "config", Values: map[string]string{"wipe_tower": "1"}}}

	plates, err := threemf.Arrange(p, []threemf.Piece{{Item: 0, Copies: 200}}, threemf.ArrangeOptions{})
	if err != nil {
		t.Fatalf("Arrange: %v", err)
	}

	ref, _ := p.Item(0)
	box := p.Instance(ref).Bounds
	half := threemf.Point{X: (box.Max[0] - box.Min[0]) / 2, Y: (box.Max[1] - box.Min[1]) / 2}

	for _, plate := range plates {
		for _, at := range plate.Positions {
			if at.X+half.X > 180 && at.X-half.X < 240 && at.Y+half.Y > 140 && at.Y-half.Y < 200 {
				t.Fatalf("a copy at %.1f, %.1f stands on the wipe tower", at.X, at.Y)
			}
		}
	}

	if len(plates[0].Positions) >= 195 {
		t.Errorf("the first plate holds %d copies, as many as with no tower", len(plates[0].Positions))
	}
}
