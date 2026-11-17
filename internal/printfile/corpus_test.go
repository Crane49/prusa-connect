package printfile

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	goflag "flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

var update = goflag.Bool("update", false,
	"rewrite the golden files under testdata/corpus/golden from what Read returns")

// The corpus is what PrusaSlicer 2.9.6 wrote for every Prusa printer profile
// it ships. testdata/README.md says how it was made, and
// testdata/corpus/make.sh makes it again.
const (
	corpusDir   = "testdata/corpus"
	corpusList  = corpusDir + "/corpus.tsv"
	goldenDir   = corpusDir + "/golden"
	goldenSufix = ".json"
)

// corpusRow is one row of corpus.tsv: what one file was sliced for.
type corpusRow struct {
	file      string
	nozzleMM  float64
	highFlow  bool
	materials []string
	abrasive  []int
	tools     int
	used      []int
	// nozzleChecks is how many M862.1 lines the printer profile writes.
	nozzleChecks int
	printer      string
	printProfile string
	filaments    []string
	text         bool
	modelID      string
}

// corpusRows reads corpus.tsv.
func corpusRows(t *testing.T) []corpusRow {
	t.Helper()

	raw, err := os.ReadFile(corpusList)
	if err != nil {
		t.Fatalf("read the corpus list: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = '\t'
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse the corpus list: %v", err)
	}

	if len(records) < 2 {
		t.Fatal("the corpus list names no file")
	}

	column := map[string]int{}
	for i, name := range records[0] {
		column[name] = i
	}

	field := func(record []string, name string) string {
		i, ok := column[name]
		if !ok {
			t.Fatalf("the corpus list has no column %q", name)
		}

		return record[i]
	}

	rows := make([]corpusRow, 0, len(records)-1)

	for _, record := range records[1:] {
		nozzle, nozzleErr := strconv.ParseFloat(field(record, "nozzle_mm"), 64)
		tools, toolsErr := strconv.Atoi(field(record, "tools"))
		nozzleChecks, checksErr := strconv.Atoi(field(record, "nozzle_checks"))

		if nozzleErr != nil || toolsErr != nil || checksErr != nil {
			t.Fatalf("%s: a number column does not read: %v, %v, %v", record[0], nozzleErr, toolsErr, checksErr)
		}

		rows = append(rows, corpusRow{
			file:         field(record, "file"),
			nozzleMM:     nozzle,
			highFlow:     field(record, "high_flow") == "1",
			materials:    strings.Split(field(record, "materials"), ";"),
			abrasive:     indexes(t, field(record, "abrasive")),
			tools:        tools,
			used:         indexes(t, field(record, "tools_used")),
			nozzleChecks: nozzleChecks,
			printer:      field(record, "printer_profile"),
			printProfile: field(record, "print_profile"),
			filaments:    strings.Split(field(record, "filament_profiles"), ";"),
			text:         field(record, "text") == "1",
			modelID:      field(record, "model_id"),
		})
	}

	return rows
}

// indexes reads a list of tool indexes such as 0,2, where a dash is none.
func indexes(t *testing.T, value string) []int {
	t.Helper()

	if value == "-" || value == "" {
		return nil
	}

	var out []int

	for part := range strings.SplitSeq(value, ",") {
		index, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("tool index %q: %v", part, err)
		}

		out = append(out, index)
	}

	return out
}

// TestCorpusGolden reads every file of the corpus and compares what Read
// returns with the golden file beside it. Run it with -update to rewrite the
// golden files after the corpus or the reader changed, and read the diff.
//
// The golden file is the storageapi.PrintInfo the host hands a module, so the
// printcompat tests in sdk/ judge the same values this test holds.
func TestCorpusGolden(t *testing.T) {
	t.Parallel()

	for _, row := range corpusRows(t) {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()

			binary := readFile(t, filepath.Join(corpusDir, row.file+".bgcode")).Details
			got := printInfoOf(binary)

			if row.text {
				text := readFile(t, filepath.Join(corpusDir, row.file+".gcode")).Details
				if !reflect.DeepEqual(text, binary) {
					t.Errorf("the text and the binary form read differently:\ntext:   %+v\nbinary: %+v", text, binary)
				}
			}

			checkGolden(t, filepath.Join(goldenDir, row.file+goldenSufix), got)
		})
	}
}

// TestCorpusMatchesItsList holds what Read returns to what corpus.tsv says
// each file was sliced for. The list was written from the slicer profiles and
// never from Read, so this test checks the reader where the golden files only
// keep it from changing.
func TestCorpusMatchesItsList(t *testing.T) {
	t.Parallel()

	for _, row := range corpusRows(t) {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()

			got := readFile(t, filepath.Join(corpusDir, row.file+".bgcode")).Details
			checkAgainstRow(t, row, got)
		})
	}
}

func checkAgainstRow(t *testing.T, row corpusRow, got Details) {
	t.Helper()

	if got.PrinterProfile != row.printer || got.PrintProfile != row.printProfile ||
		got.FilamentProfile != row.filaments[0] {
		t.Errorf("profiles: got %q, %q, %q; want %q, %q, %q",
			got.PrinterProfile, got.PrintProfile, got.FilamentProfile, row.printer, row.printProfile, row.filaments[0])
	}

	if got.Checks == nil {
		t.Fatal("no checks read from the start of the G-code")
	}

	if got.Checks.PrinterModel != row.modelID {
		t.Errorf("M862.3 model: got %q, want %q", got.Checks.PrinterModel, row.modelID)
	}

	if len(got.Filaments) != row.tools {
		t.Fatalf("tools: got %d, want %d", len(got.Filaments), row.tools)
	}

	var used []int

	for _, tool := range got.Filaments {
		if tool.Used == nil {
			t.Errorf("tool %d: the file does not say whether it is used", tool.Index)
		} else if *tool.Used {
			used = append(used, tool.Index)
		}

		if tool.Material != row.materials[tool.Index] {
			t.Errorf("tool %d material: got %q, want %q", tool.Index, tool.Material, row.materials[tool.Index])
		}

		if tool.NozzleMM != row.nozzleMM {
			t.Errorf("tool %d nozzle: got %v, want %v", tool.Index, tool.NozzleMM, row.nozzleMM)
		}

		if tool.HighFlow == nil || *tool.HighFlow != row.highFlow {
			t.Errorf("tool %d high flow: got %v, want %v", tool.Index, flagText(tool.HighFlow), row.highFlow)
		}

		abrasive := slices.Contains(row.abrasive, tool.Index)
		if tool.Abrasive == nil || *tool.Abrasive != abrasive {
			t.Errorf("tool %d abrasive: got %v, want %v", tool.Index, flagText(tool.Abrasive), abrasive)
		}

		if tool.NozzleTempC <= 0 || tool.BedTempC < 0 {
			t.Errorf("tool %d temperatures: nozzle %v, bed %v", tool.Index, tool.NozzleTempC, tool.BedTempC)
		}
	}

	if !slices.Equal(used, row.used) {
		t.Errorf("used tools: got %v, want %v", used, row.used)
	}

	checkNozzleLines(t, row, got.Checks.Nozzles)

	if got.MaxLayerZMM < 9.9 || got.MaxLayerZMM > 11 {
		t.Errorf("top layer: got %v mm for a 10 mm cube", got.MaxLayerZMM)
	}

	if got.Bounds == nil || got.Bed.WidthMM <= 0 || got.Bed.DepthMM <= 0 || got.Bed.HeightMM <= 0 {
		t.Errorf("bounds %+v, bed %+v: want both", got.Bounds, got.Bed)
	}
}

// checkNozzleLines holds the M862.1 lines to the row. A file for one nozzle
// writes one line with no tool, an XL file writes one line for each tool it
// uses, and the MINI profile without input shaping writes none.
func checkNozzleLines(t *testing.T, row corpusRow, lines []NozzleCheck) {
	t.Helper()

	if len(lines) != row.nozzleChecks {
		t.Errorf("M862.1 lines: got %d, want %d", len(lines), row.nozzleChecks)

		return
	}

	if len(lines) > 1 {
		var named []int
		for _, line := range lines {
			named = append(named, line.Index)
		}

		if !slices.Equal(named, row.used) {
			t.Errorf("M862.1 lines name tools %v, and the print uses %v", named, row.used)
		}
	}

	for _, line := range lines {
		if line.DiameterMM != row.nozzleMM {
			t.Errorf("M862.1 T%d diameter: got %v, want %v", line.Index, line.DiameterMM, row.nozzleMM)
		}

		if line.HighFlow == nil || *line.HighFlow != row.highFlow {
			t.Errorf("M862.1 T%d high flow: got %v, want %v", line.Index, flagText(line.HighFlow), row.highFlow)
		}

		abrasive := false

		for _, index := range row.abrasive {
			if slices.Contains(row.used, index) && (len(lines) == 1 || index == line.Index) {
				abrasive = true
			}
		}

		if line.Hardened == nil || *line.Hardened != abrasive {
			t.Errorf("M862.1 T%d hardened: got %v, want %v", line.Index, flagText(line.Hardened), abrasive)
		}
	}
}

func flagText(value *bool) string {
	if value == nil {
		return "unset"
	}

	return strconv.FormatBool(*value)
}

// checkGolden compares got with the golden file at path, or rewrites the file
// under -update.
func checkGolden(t *testing.T, path string, got storageapi.PrintInfo) {
	t.Helper()

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	encoded = append(encoded, '\n')

	if *update {
		if writeErr := os.WriteFile(path, encoded, 0o600); writeErr != nil {
			t.Fatalf("write the golden file: %v", writeErr)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the golden file, or run with -update to write it: %v", err)
	}

	if !bytes.Equal(encoded, want) {
		t.Errorf("%s differs from what Read returns; run with -update and read the diff.\ngot:\n%s", path, encoded)
	}
}

// printInfoOf is the storageapi.PrintInfo the host hands a module for these
// details.
func printInfoOf(d Details) storageapi.PrintInfo {
	out := storageapi.PrintInfo{
		EstimatedSeconds: d.EstimatedSeconds,
		FilamentMM:       d.FilamentMM,
		FilamentG:        d.FilamentG,
		LayerHeightMM:    d.LayerHeightMM,
		NozzleMM:         d.NozzleMM,
		Material:         d.Material,
		PrinterModel:     d.PrinterModel,
		PrintProfile:     d.PrintProfile,
		PrinterProfile:   d.PrinterProfile,
		FilamentProfile:  d.FilamentProfile,
		HighFlow:         d.HighFlow,
		MaxLayerZMM:      d.MaxLayerZMM,
	}

	for _, tool := range d.Filaments {
		out.Filaments = append(out.Filaments, storageapi.PrintFilament{
			Index:                 tool.Index,
			Used:                  tool.Used,
			Material:              tool.Material,
			NozzleMM:              tool.NozzleMM,
			HighFlow:              tool.HighFlow,
			Abrasive:              tool.Abrasive,
			NozzleTempC:           tool.NozzleTempC,
			FirstLayerNozzleTempC: tool.FirstLayerNozzleTempC,
			BedTempC:              tool.BedTempC,
			FirstLayerBedTempC:    tool.FirstLayerBedTempC,
			FilamentG:             tool.FilamentG,
			FilamentMM:            tool.FilamentMM,
			Colour:                tool.Colour,
		})
	}

	if d.Bounds != nil {
		out.Bounds = &storageapi.PrintBounds{
			MinXMM: d.Bounds.MinXMM, MinYMM: d.Bounds.MinYMM, MaxXMM: d.Bounds.MaxXMM, MaxYMM: d.Bounds.MaxYMM,
		}
	}

	if d.Bed != (Bed{}) {
		out.Bed = &storageapi.PrintBed{WidthMM: d.Bed.WidthMM, DepthMM: d.Bed.DepthMM, HeightMM: d.Bed.HeightMM}
	}

	if d.Checks != nil {
		checks := &storageapi.PrintChecks{
			PrinterModel: d.Checks.PrinterModel,
			PrinterCode:  d.Checks.PrinterCode,
			MinFirmware:  d.Checks.MinFirmware,
			GCodeLevel:   d.Checks.GCodeLevel,
			Features:     d.Checks.Features,
			Firmware:     d.Checks.Firmware,
		}

		for _, line := range d.Checks.Nozzles {
			checks.Nozzles = append(checks.Nozzles, storageapi.PrintNozzleCheck{
				Index: line.Index, DiameterMM: line.DiameterMM, Hardened: line.Hardened, HighFlow: line.HighFlow,
			})
		}

		out.Checks = checks
	}

	return out
}
