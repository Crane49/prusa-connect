package printfile

import (
	"bytes"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// toolSummary is the part of one Filament these tests compare.
type toolSummary struct {
	index    int
	used     bool
	material string
	nozzle   float64
}

func summaryOf(tools []Filament) []toolSummary {
	out := make([]toolSummary, 0, len(tools))
	for _, tool := range tools {
		out = append(out, toolSummary{
			index: tool.Index, used: tool.Used != nil && *tool.Used,
			material: tool.Material, nozzle: tool.NozzleMM,
		})
	}

	return out
}

// TestReadToolNeedsFromRealFiles reads what each tool needs out of files
// PrusaSlicer 2.9.6 wrote for one tool, for an XL with five toolheads and for
// an MK4S with an MMU3. testdata/README.md says how each file was made. The
// text and the binary form of each print must say the same.
func TestReadToolNeedsFromRealFiles(t *testing.T) {
	t.Parallel()

	single := []toolSummary{{index: 0, used: true, material: "PLA", nozzle: 0.4}}
	xl := []toolSummary{
		{index: 0, used: true, material: "PLA", nozzle: 0.4},
		{index: 1, used: true, material: "PETG", nozzle: 0.4},
		{index: 2, used: true, material: "ASA", nozzle: 0.4},
		{index: 3, used: false, material: "PLA", nozzle: 0.4},
		{index: 4, used: false, material: "PETG", nozzle: 0.4},
	}
	mmu := []toolSummary{
		{index: 0, used: true, material: "PLA", nozzle: 0.4},
		{index: 1, used: true, material: "PETG", nozzle: 0.4},
		{index: 2, used: false, material: "PLA", nozzle: 0.4},
		{index: 3, used: false, material: "PLA", nozzle: 0.4},
		{index: 4, used: false, material: "PLA", nozzle: 0.4},
	}

	cases := []struct {
		name      string
		files     []string
		model     string
		tools     []toolSummary
		checked   string
		features  []string
		nozzles   []int
		firmware  string
		filamentG float64
		bed       Bed
		bounds    Bounds
	}{
		{
			name: "one tool", files: []string{"cube10.gcode", "cube10.bgcode"}, model: "MK4IS",
			tools: single, checked: "MK4", features: []string{"Input shaper"}, nozzles: []int{0},
			firmware: "6.5.3+12780", filamentG: 0.40,
			bed:    Bed{WidthMM: 250, DepthMM: 210, HeightMM: 220},
			bounds: Bounds{MinXMM: 120, MinYMM: 100, MaxXMM: 130, MaxYMM: 110},
		},
		{
			name: "XL with five tools", files: []string{"cube10-xl5t.gcode", "cube10-xl5t.bgcode"}, model: "XL5IS",
			tools: xl, checked: "XL", features: []string{"Input shaper"}, nozzles: []int{0, 1, 2},
			firmware: "6.4.1+12026", filamentG: 0.84,
			bed:    Bed{WidthMM: 360, DepthMM: 360, HeightMM: 360},
			bounds: Bounds{MinXMM: 175, MinYMM: 175, MaxXMM: 185, MaxYMM: 185},
		},
		{
			name:  "MK4S with an MMU3",
			files: []string{"cube10-mk4s-mmu3.gcode", "cube10-mk4s-mmu3.bgcode"}, model: "MK4SMMU3",
			tools: mmu, checked: "MK4S", features: []string{"Input shaper", "MMU3"}, nozzles: []int{0},
			firmware: "6.5.3+12780", filamentG: 0.75,
			bed:    Bed{WidthMM: 250, DepthMM: 210, HeightMM: 220},
			bounds: Bounds{MinXMM: 120, MinYMM: 100, MaxXMM: 130, MaxYMM: 110},
		},
	}

	for _, tc := range cases {
		for _, file := range tc.files {
			t.Run(tc.name+"/"+file, func(t *testing.T) {
				t.Parallel()

				got := readFile(t, filepath.Join("testdata", file)).Details

				if got.PrinterModel != tc.model {
					t.Errorf("printer model: got %q, want %q", got.PrinterModel, tc.model)
				}

				if summary := summaryOf(got.Filaments); !reflect.DeepEqual(summary, tc.tools) {
					t.Errorf("tools: got %+v, want %+v", summary, tc.tools)
				}

				if got.FilamentG != tc.filamentG {
					t.Errorf("filament: got %v g, want %v g, the sum over the tools", got.FilamentG, tc.filamentG)
				}

				if got.Bed != tc.bed {
					t.Errorf("bed: got %+v, want %+v", got.Bed, tc.bed)
				}

				if got.Bounds == nil || *got.Bounds != tc.bounds {
					t.Errorf("bounds: got %+v, want %+v", got.Bounds, tc.bounds)
				}

				if len(got.Objects) != 1 || got.Objects[0].ID != 0 || got.Objects[0].Name == "" ||
					len(got.Objects[0].Polygon) < 3 {
					t.Errorf("objects: got %+v, want one named object with its outline", got.Objects)
				}

				if got.MaxLayerZMM != 9.9 {
					t.Errorf("top layer: got %v, want 9.9", got.MaxLayerZMM)
				}

				checks := got.Checks
				if checks == nil {
					t.Fatal("no checks were read out of the G-code")
				}

				if checks.PrinterModel != tc.checked || checks.GCodeLevel != 2 || checks.Firmware != tc.firmware {
					t.Errorf("checks: got model %q, level %d, firmware %q", checks.PrinterModel,
						checks.GCodeLevel, checks.Firmware)
				}

				if !slices.Equal(checks.Features, tc.features) {
					t.Errorf("features: got %q, want %q", checks.Features, tc.features)
				}

				nozzles := make([]int, 0, len(checks.Nozzles))
				for _, nozzle := range checks.Nozzles {
					nozzles = append(nozzles, nozzle.Index)

					if nozzle.DiameterMM != 0.4 || nozzle.Hardened == nil || *nozzle.Hardened {
						t.Errorf("nozzle check: got %+v, want 0.4 mm and not hardened", nozzle)
					}
				}

				if !slices.Equal(nozzles, tc.nozzles) {
					t.Errorf("nozzle checks name tools %v, want %v", nozzles, tc.nozzles)
				}
			})
		}
	}
}

// TestReadToolTemperaturesAndColours reads the values the XL file lists per
// tool, which differ from tool to tool.
func TestReadToolTemperaturesAndColours(t *testing.T) {
	t.Parallel()

	got := readFile(t, filepath.Join("testdata", "cube10-xl5t.bgcode")).Details
	if len(got.Filaments) != 5 {
		t.Fatalf("got %d tools, want 5", len(got.Filaments))
	}

	asa := got.Filaments[2]
	if asa.NozzleTempC != 260 || asa.FirstLayerNozzleTempC != 260 || asa.BedTempC != 105 ||
		asa.FirstLayerBedTempC != 100 || asa.FilamentG != 0.27 || asa.Colour != "#3EC0FF" {
		t.Errorf("tool 3: got %+v", asa)
	}

	if asa.HighFlow == nil || *asa.HighFlow || asa.Abrasive == nil || *asa.Abrasive {
		t.Errorf("tool 3 flags: got high flow %v and abrasive %v, want both false", asa.HighFlow, asa.Abrasive)
	}
}

// TestToolsFromSettings covers the per-tool lists a real file did not
// exercise.
func TestToolsFromSettings(t *testing.T) {
	t.Parallel()

	read := func(lines ...string) Details {
		gathered := newCollector()
		for _, line := range lines {
			gathered.putLine(line)
		}

		return gathered.details()
	}

	t.Run("no grams leaves use unknown", func(t *testing.T) {
		t.Parallel()

		got := read("filament_type = PLA;PETG", "nozzle_diameter = 0.4,0.6")
		if len(got.Filaments) != 2 || got.Filaments[0].Used != nil || got.Filaments[1].NozzleMM != 0.6 {
			t.Errorf("got %+v", got.Filaments)
		}
	})

	t.Run("a purge is not use", func(t *testing.T) {
		t.Parallel()

		got := read("filament used [g] = 0.0002, 0.0004")
		if *got.Filaments[0].Used || !*got.Filaments[1].Used {
			t.Errorf("got %v and %v, want the first unused and the second used",
				*got.Filaments[0].Used, *got.Filaments[1].Used)
		}
	})

	t.Run("hardened, high flow and a filament colour", func(t *testing.T) {
		t.Parallel()

		got := read(`extruder_colour = ""`, "filament_colour = #ff8000",
			"nozzle_high_flow = 1", "filament_abrasive = 1")
		tool := got.Filaments[0]

		if tool.Colour != "#FF8000" || !*tool.HighFlow || !*tool.Abrasive {
			t.Errorf("got %+v", tool)
		}
	})

	t.Run("a list longer than any printer today is read whole", func(t *testing.T) {
		t.Parallel()

		got := read("nozzle_diameter = " + strings.Repeat("0.4,", 40) + "0.4")
		if len(got.Filaments) != 41 || got.Filaments[40].Index != 40 {
			t.Errorf("got %d filaments, want 41", len(got.Filaments))
		}
	})

	t.Run("a bed that does not start at zero", func(t *testing.T) {
		t.Parallel()

		got := read("bed_shape = -2x-3,178x-3,178x177,-2x177", "max_print_height = 180")
		if got.Bed != (Bed{WidthMM: 180, DepthMM: 180, HeightMM: 180}) {
			t.Errorf("got %+v", got.Bed)
		}
	})

	t.Run("an outline that is not JSON", func(t *testing.T) {
		t.Parallel()

		if got := read(`objects_info = {"objects":[`); got.Bounds != nil || got.Objects != nil {
			t.Errorf("got %+v and %+v, want none", got.Bounds, got.Objects)
		}
	})

	t.Run("two labelled objects", func(t *testing.T) {
		t.Parallel()

		got := read(`objects_info = {"objects":[{"name":"A","polygon":[[1,2],[3,2],[3,4]]},` +
			`{"name":"B (Instance 2)","polygon":[[10,10],[12,10],[12,12]]}]}`)
		if len(got.Objects) != 2 || got.Objects[1].ID != 1 || got.Objects[1].Name != "B (Instance 2)" ||
			got.Bounds == nil || *got.Bounds != (Bounds{MinXMM: 1, MinYMM: 2, MaxXMM: 12, MaxYMM: 12}) {
			t.Errorf("got %+v, bounds %+v", got.Objects, got.Bounds)
		}
	})
}

// TestHeadScannerReadsWhatTheFirmwareReads holds the G-code check lines to
// the firmware's reading of them (GCodeInfo::parse_gcode).
func TestHeadScannerReadsWhatTheFirmwareReads(t *testing.T) {
	t.Parallel()

	scan := func(text string) *Checks {
		var scanner headScanner

		scanGCodeText(text, &scanner)

		return scanner.result()
	}

	t.Run("only comments is no G-code read", func(t *testing.T) {
		t.Parallel()

		if got := scan("; a comment\n;another\n"); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("every check line", func(t *testing.T) {
		t.Parallel()

		got := scan(strings.Join([]string{
			"N12 M862.3 P \"MK4SMMU3\" ; printer model check",
			"M862.2 P30260",
			"M862.4 P6.4.0+12000",
			"m862.5 P2",
			`M862.6 P"Input shaper"`,
			`M862.6 P "MMU3"`,
			"M862.1 T1 P0.6 A1 F1",
			"M862.1 P0.4",
			"M115 U6.5.3+12780 ; newest",
		}, "\n"))

		want := &Checks{
			PrinterModel: "MK4SMMU3", PrinterCode: 30260, MinFirmware: "6.4.0+12000", GCodeLevel: 2,
			Features: []string{"Input shaper", "MMU3"}, Firmware: "6.5.3+12780",
			Nozzles: []NozzleCheck{
				{Index: 1, DiameterMM: 0.6, Hardened: new(true), HighFlow: new(true)},
				{Index: 0, DiameterMM: 0.4},
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("a check after two hundred G-codes is not read", func(t *testing.T) {
		t.Parallel()

		text := strings.Repeat("G1 X1\n", maxScannedGCodes) + "M862.3 P \"MK4\"\n"
		if got := scan(text); got == nil || got.PrinterModel != "" {
			t.Errorf("got %+v, want checks with no model", got)
		}
	})

	t.Run("object labels do not count against the two hundred", func(t *testing.T) {
		t.Parallel()

		text := strings.Repeat("M486 S1\n", 2*maxScannedGCodes) + "M862.3 P \"MK4\"\n"
		if got := scan(text); got == nil || got.PrinterModel != "MK4" {
			t.Errorf("got %+v, want the model read", got)
		}
	})
}

// TestHeatshrinkAndMeatPack decodes streams built by hand, so each rule of the
// two formats is pinned without a slicer.
func TestHeatshrinkAndMeatPack(t *testing.T) {
	t.Parallel()

	t.Run("heatshrink literals and a back reference", func(t *testing.T) {
		t.Parallel()

		var bits bitWriter

		for _, c := range []byte("ab") {
			bits.write(1, 1)
			bits.write(int(c), 8)
		}

		// Copy four bytes from two back: abab.
		bits.write(0, 1)
		bits.write(2-1, heatshrinkWindow12)
		bits.write(4-1, heatshrinkLookahead)

		got := unheatshrink(bits.bytes(), heatshrinkWindow12, 64)
		if string(got) != "ababab" {
			t.Errorf("got %q, want ababab", got)
		}
	})

	t.Run("a reference before the start reads the zeros the window starts with", func(t *testing.T) {
		t.Parallel()

		var bits bitWriter

		bits.write(1, 1)
		bits.write('a', 8)

		// Copy four bytes from three back: two zeros before the start, the
		// a, and the first of the bytes this reference writes.
		bits.write(0, 1)
		bits.write(3-1, heatshrinkWindow12)
		bits.write(4-1, heatshrinkLookahead)

		bits.write(1, 1)
		bits.write('b', 8)

		got := unheatshrink(bits.bytes(), heatshrinkWindow12, 64)
		if want := "a\x00\x00a\x00b"; string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("output stops at the limit", func(t *testing.T) {
		t.Parallel()

		var bits bitWriter

		bits.write(1, 1)
		bits.write('x', 8)

		for range 100 {
			bits.write(0, 1)
			bits.write(0, heatshrinkWindow11)
			bits.write(15, heatshrinkLookahead)
		}

		if got := unheatshrink(bits.bytes(), heatshrinkWindow11, 40); len(got) != 40 {
			t.Errorf("got %d bytes, want 40", len(got))
		}
	})

	t.Run("meatpack packed and literal characters", func(t *testing.T) {
		t.Parallel()

		packed := []byte{
			meatpackSignal, meatpackSignal, meatpackEnablePacking,
			// "G1": G is nibble 13, 1 is nibble 1, low nibble first.
			0x1D,
			// " M": space is nibble 11, then M is a literal in the high nibble.
			0xFB, 'M',
			// "M" and "8" with the first a literal: the 8 waits for it.
			0x8F, 'M',
			// A newline in the low nibble ends the byte.
			0x0C,
		}

		if got := string(unmeatpack(packed, 64)); got != "G1 MM8\n" {
			t.Errorf("got %q, want %q", got, "G1 MM8\n")
		}
	})

	t.Run("meatpack without spaces packs E where the space was", func(t *testing.T) {
		t.Parallel()

		packed := []byte{
			meatpackSignal, meatpackSignal, meatpackEnablePacking,
			meatpackSignal, meatpackSignal, meatpackEnableNoSpaces,
			0xB1,
		}

		if got := string(unmeatpack(packed, 64)); got != "1E" {
			t.Errorf("got %q, want 1E", got)
		}
	})
}

// TestReadBinaryGCodeHeadIsBounded builds a G-code block far larger than the
// head this package reads, and checks the read stays inside the bound.
func TestReadBinaryGCodeHeadIsBounded(t *testing.T) {
	t.Parallel()

	gcode := []byte("M862.3 P \"MK4\"\n" + strings.Repeat("G1 X1 Y1\n", 200_000))

	file := fixtureBGCode(t, []bgcodeBlock{
		{kind: blockPrinterMetadata, compression: compressionNone, params: metadataParams(),
			data: []byte("printer_model=MK4IS\n")},
		{kind: blockGCode, compression: compressionNone, params: metadataParams(), data: gcode},
	})

	counted := &countingReaderAt{inner: bytes.NewReader(file)}

	got := Read(t.Context(), counted, int64(len(file)))
	if got.Details.Checks == nil || got.Details.Checks.PrinterModel != "MK4" {
		t.Fatalf("checks: got %+v", got.Details.Checks)
	}

	if counted.bytes > 2*maxGCodeHeadInput {
		t.Errorf("read %d bytes of a %d byte file", counted.bytes, len(file))
	}
}

// bitWriter writes bits the most significant first, the order heatshrink
// reads them in.
type bitWriter struct {
	out  []byte
	used uint
}

func (b *bitWriter) write(value int, count uint) {
	for i := int(count) - 1; i >= 0; i-- {
		if b.used == 0 {
			b.out = append(b.out, 0)
		}

		if value>>i&1 == 1 {
			b.out[len(b.out)-1] |= 1 << (7 - b.used)
		}

		b.used = (b.used + 1) % 8
	}
}

func (b *bitWriter) bytes() []byte { return b.out }
