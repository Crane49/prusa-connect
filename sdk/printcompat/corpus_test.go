package printcompat_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/printcompat"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

var update = flag.Bool("update", false, "rewrite testdata/corpus-matrix.tsv from what Evaluate returns")

// The corpus is the print files PrusaSlicer 2.9.6 wrote for every Prusa
// printer profile it ships, kept with the reader that reads them. Its golden
// files are the storageapi.PrintInfo the host reads out of each file, so these
// tests judge the same values a module is handed. See
// internal/printfile/testdata/README.md for how the files were made.
const (
	corpusGolden = "../../internal/printfile/testdata/corpus/golden"
	corpusMatrix = "testdata/corpus-matrix.tsv"
)

// corpus reads every golden file, by its name without the extension.
func corpus(t *testing.T) map[string]*storageapi.PrintInfo {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(corpusGolden, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no corpus under %s: %v", corpusGolden, err)
	}

	out := make(map[string]*storageapi.PrintInfo, len(paths))

	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}

		var info storageapi.PrintInfo
		if decodeErr := json.Unmarshal(raw, &info); decodeErr != nil {
			t.Fatalf("decode %s: %v", path, decodeErr)
		}

		out[strings.TrimSuffix(filepath.Base(path), ".json")] = &info
	}

	return out
}

func corpusFile(t *testing.T, files map[string]*storageapi.PrintInfo, name string) *storageapi.PrintInfo {
	t.Helper()

	info, ok := files[name]
	if !ok {
		t.Fatalf("the corpus has no file %s", name)
	}

	return info
}

// fleet is one printer of each model, as it leaves the factory: a standard
// 0.4 mm nozzle on every tool, PLA loaded, and the build volume of the model.
// The volumes are the beds the 2.9.6 printer profiles describe.
type fleet struct {
	name   string
	model  string
	tools  int
	mmu    *bool
	volume printcompat.Volume
}

//nolint:gochecknoglobals // a fixed table, read-only.
var factory = []fleet{
	{name: "MINI", model: "MINI", tools: 1, volume: printcompat.Volume{XMM: 180, YMM: 180, ZMM: 180}},
	{
		name:   "MK3.5",
		model:  "MK3.5",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 210},
	},
	{
		name:   "MK3.5S",
		model:  "MK3.5S",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 210},
	},
	{
		name:   "MK3.9",
		model:  "MK3.9",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 220},
	},
	{
		name:   "MK3.9S",
		model:  "MK3.9S",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 220},
	},
	{name: "MK4", model: "MK4", tools: 1, mmu: new(false), volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 220}},
	{name: "MK4S", model: "MK4S", tools: 1, mmu: new(false), volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 220}},
	{
		name:   "MK4S MMU3",
		model:  "MK4S",
		tools:  5,
		mmu:    new(true),
		volume: printcompat.Volume{XMM: 250, YMM: 210, ZMM: 220},
	},
	{name: "XL 1T", model: "XL", tools: 1, volume: printcompat.Volume{XMM: 360, YMM: 360, ZMM: 360}},
	{name: "XL 2T", model: "XL", tools: 2, volume: printcompat.Volume{XMM: 360, YMM: 360, ZMM: 360}},
	{name: "XL 5T", model: "XL", tools: 5, volume: printcompat.Volume{XMM: 360, YMM: 360, ZMM: 360}},
	{
		name:   "CORE One",
		model:  "CoreOne",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 220, ZMM: 270},
	},
	{
		name:   "CORE One MMU3",
		model:  "CoreOne",
		tools:  5,
		mmu:    new(true),
		volume: printcompat.Volume{XMM: 250, YMM: 220, ZMM: 270},
	},
	{
		name:   "CORE One L",
		model:  "CoreOneL",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 300, YMM: 300, ZMM: 330},
	},
	{
		name:   "Signature Oak",
		model:  "COREONEOAK",
		tools:  1,
		mmu:    new(false),
		volume: printcompat.Volume{XMM: 250, YMM: 220, ZMM: 270},
	},
}

// factoryPrinter is the printer of the fleet with this name.
func factoryPrinter(t *testing.T, name string) printcompat.Printer {
	t.Helper()

	for _, row := range factory {
		if row.name == name {
			return row.printer()
		}
	}

	t.Fatalf("no printer %s in the fleet", name)

	return printcompat.Printer{}
}

func (f fleet) printer() printcompat.Printer {
	tree := toolTree{}

	if f.mmu != nil && *f.mmu {
		tree.head("0", 0.4, false, false, "")
		tree.feeder("0", slices.Repeat([]string{"PLA"}, f.tools)...)
	} else {
		for n := range f.tools {
			tree.head(connectorapi.HeadID(n), 0.4, false, false, "PLA")
		}
	}

	tree[connectorapi.ToolCountPath] = strconv.Itoa(max(1, headsOf(f)))

	if f.mmu != nil && !*f.mmu {
		tree[connectorapi.ToolPath("0", connectorapi.FeederSlotCount)] = "0"
	}

	return tree.printer(f.model, f.volume)
}

// headsOf is how many heads a printer of the fleet has: one in front of an
// MMU, and one per tool otherwise.
func headsOf(f fleet) int {
	if f.mmu != nil && *f.mmu {
		return 1
	}

	return f.tools
}

// toolTree is a printer's state tree, path to JSON text, built the way a
// driver writes it.
type toolTree map[string]string

// head writes one enabled head with its nozzle and, unless the material is
// empty, the filament loaded in it.
func (tree toolTree) head(id connectorapi.ToolID, nozzle float64, highFlow, hardened bool, material string) {
	tree[connectorapi.ToolPath(id, connectorapi.ToolEnabled)] = "true"
	tree[connectorapi.ToolPath(id, connectorapi.FeatureNozzleDiameter)] = strconv.FormatFloat(nozzle, 'f', -1, 64)
	tree[connectorapi.ToolPath(id, connectorapi.FeatureNozzleHighFlow)] = strconv.FormatBool(highFlow)
	tree[connectorapi.ToolPath(id, connectorapi.FeatureNozzleHardened)] = strconv.FormatBool(hardened)

	if material != "" {
		tree[connectorapi.ToolPath(id, connectorapi.MaterialAbbreviation)] = strconv.Quote(material)
	}
}

// feeder fits a feeder to a head with one slot per material.
func (tree toolTree) feeder(id connectorapi.ToolID, materials ...string) {
	tree[connectorapi.ToolPath(id, connectorapi.FeederSlotCount)] = strconv.Itoa(len(materials))
	delete(tree, connectorapi.ToolPath(id, connectorapi.MaterialAbbreviation))

	for n, material := range materials {
		tree[connectorapi.ToolPath(id.Slot(n), connectorapi.ToolEnabled)] = "true"
		tree[connectorapi.ToolPath(id.Slot(n), connectorapi.MaterialAbbreviation)] = strconv.Quote(material)
	}
}

// printer reads the tree back as a printer of a model with a build volume.
func (tree toolTree) printer(model string, volume printcompat.Volume) printcompat.Printer {
	state := make(map[string]json.RawMessage, len(tree))
	for path, value := range tree {
		state[path] = json.RawMessage(value)
	}

	out := printcompat.PrinterFromState(model, "6.5.3+12780", state)
	out.BuildVolume = &volume

	return out
}

// TestCorpusOnItsOwnPrinter runs every file on the printer it was sliced for,
// with the nozzles and the filament the file asks for. The firmware starts
// each of them without a question, except the two profiles that predate input
// shaping, and nothing is left unverified.
func TestCorpusOnItsOwnPrinter(t *testing.T) {
	t.Parallel()

	noInputShaper := []string{"mini-025-asa", "mk4-04-pla"}

	for name, info := range corpus(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(info, ownPrinter(t, info))

			want := printcompat.Verdict{Level: printcompat.LevelOK}
			if slices.Contains(noInputShaper, name) {
				want = printcompat.Verdict{
					Level: printcompat.LevelWarning,
					Findings: []printcompat.Finding{
						{Rule: printcompat.RuleInputShaper, Level: printcompat.LevelWarning},
					},
				}
			}

			if !sameVerdict(got, want) {
				t.Errorf("got %s, want %s", verdictText(got), verdictText(want))
			}
		})
	}
}

// ownPrinter is the printer a file was sliced for, set up as the file asks:
// its model, its nozzles, its filament, an MMU where the file names one, and
// the bed of its profile.
func ownPrinter(t *testing.T, info *storageapi.PrintInfo) printcompat.Printer {
	t.Helper()

	if info.Checks == nil || info.Bed == nil {
		t.Fatal("the file has no checks or no bed")
	}

	model := info.Checks.PrinterModel
	tree := toolTree{}
	mmu := slices.Contains(info.Checks.Features, "MMU3")

	switch {
	case mmu && len(info.Filaments) > 0:
		first := info.Filaments[0]
		tree.head("0", first.NozzleMM, flagOf(first.HighFlow), flagOf(first.Abrasive), "")

		materials := make([]string, 0, len(info.Filaments))
		for _, filament := range info.Filaments {
			materials = append(materials, filament.Material)
		}

		tree.feeder("0", materials...)
		tree[connectorapi.ToolCountPath] = "1"
	default:
		for _, filament := range info.Filaments {
			tree.head(connectorapi.HeadID(filament.Index), filament.NozzleMM,
				flagOf(filament.HighFlow), flagOf(filament.Abrasive), filament.Material)
		}

		tree[connectorapi.ToolCountPath] = strconv.Itoa(len(info.Filaments))

		if !strings.EqualFold(model, "XL") && !strings.EqualFold(model, "MINI") {
			tree[connectorapi.ToolPath("0", connectorapi.FeederSlotCount)] = "0"
		}
	}

	return tree.printer(model, printcompat.Volume{XMM: info.Bed.WidthMM, YMM: info.Bed.DepthMM, ZMM: info.Bed.HeightMM})
}

func flagOf(value *bool) bool { return value != nil && *value }

// toolRef is a T number as a finding carries it, and nil for -1, a check
// about the whole print.
func toolRef(n int) *int {
	if n < 0 {
		return nil
	}

	return &n
}

// TestCorpusVerdicts holds named files on named printers to the verdict the
// public firmware gives, with the check that decides each one.
func TestCorpusVerdicts(t *testing.T) {
	t.Parallel()

	files := corpus(t)

	finding := func(rule printcompat.Rule, level printcompat.Level, tool int) printcompat.Finding {
		return printcompat.Finding{Rule: rule, Level: level, Filament: toolRef(tool)}
	}

	warn := func(rule printcompat.Rule, tool int) printcompat.Finding {
		return finding(rule, printcompat.LevelWarning, tool)
	}

	refuse := func(rule printcompat.Rule, tool int) printcompat.Finding {
		return finding(rule, printcompat.LevelRefusal, tool)
	}

	// set changes one printer of the fleet: its nozzle and its materials, by
	// tool.
	set := func(name string, nozzle float64, highFlow, hardened bool, materials ...string) printcompat.Printer {
		for _, row := range factory {
			if row.name != name {
				continue
			}

			tree := toolTree{connectorapi.ToolCountPath: strconv.Itoa(max(1, headsOf(row)))}

			if row.mmu != nil && *row.mmu {
				tree.head("0", nozzle, highFlow, hardened, "")

				slots := slices.Repeat([]string{"PLA"}, row.tools)
				copy(slots, materials)
				tree.feeder("0", slots...)

				return tree.printer(row.model, row.volume)
			}

			for n := range row.tools {
				material := "PLA"
				if n < len(materials) {
					material = materials[n]
				}

				tree.head(connectorapi.HeadID(n), nozzle, highFlow, hardened, material)
			}

			if row.mmu != nil {
				tree[connectorapi.ToolPath("0", connectorapi.FeederSlotCount)] = "0"
			}

			return tree.printer(row.model, row.volume)
		}

		t.Fatalf("no printer %s in the fleet", name)

		return printcompat.Printer{}
	}

	cases := []struct {
		name    string
		file    string
		printer printcompat.Printer
		want    printcompat.Verdict
	}{
		{
			name: "a CORE One file on a CORE One L runs as its own",
			file: "coreone-hf04-pla", printer: set("CORE One L", 0.4, true, false, "PLA"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name: "an MK4S file on a CORE One L runs in compatibility mode",
			file: "mk4s-04-pla", printer: set("CORE One L", 0.4, false, false, "PLA"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleCompatibilityMode, -1)},
			},
		},
		{
			name: "a CORE One L file on a CORE One is the wrong model",
			file: "coreonel-hf04-pla", printer: set("CORE One", 0.4, true, false, "PLA"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RulePrinterModel, -1)},
			},
		},
		{
			name: "a Signature Oak file on a CORE One is the same group",
			file: "oak-hf04-pla", printer: set("CORE One", 0.4, true, false, "PLA"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name:    "an XL file using five tools on an XL with two is refused",
			file:    "xl5t-hf05-5of5",
			printer: set("XL 2T", 0.5, true, false, "PLA", "PETG"),
			want: printcompat.Verdict{
				Level: printcompat.LevelRefusal,
				Findings: []printcompat.Finding{
					refuse(printcompat.RuleToolCount, -1),
					refuse(printcompat.RuleToolEnabled, 2),
					refuse(printcompat.RuleToolEnabled, 3),
					refuse(printcompat.RuleToolEnabled, 4),
				},
				Unverified: []printcompat.Finding{
					{Rule: printcompat.RuleMaterial, Filament: toolRef(2)},
					{Rule: printcompat.RuleNozzleDiameter, Filament: toolRef(2)},
					{Rule: printcompat.RuleNozzleHighFlow, Filament: toolRef(2)},
					{Rule: printcompat.RuleMaterial, Filament: toolRef(3)},
					{Rule: printcompat.RuleNozzleDiameter, Filament: toolRef(3)},
					{Rule: printcompat.RuleNozzleHighFlow, Filament: toolRef(3)},
					{Rule: printcompat.RuleMaterial, Filament: toolRef(4)},
					{Rule: printcompat.RuleNozzleDiameter, Filament: toolRef(4)},
					{Rule: printcompat.RuleNozzleHighFlow, Filament: toolRef(4)},
				},
			},
		},
		{
			name: "an XL file that uses only its second tool on a one-tool XL",
			file: "xl2t-04-t1", printer: set("XL 1T", 0.4, false, false, "PETG"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelRefusal,
				Findings: []printcompat.Finding{refuse(printcompat.RuleToolEnabled, 1)},
				Unverified: []printcompat.Finding{
					{Rule: printcompat.RuleMaterial, Filament: toolRef(1)},
					{Rule: printcompat.RuleNozzleDiameter, Filament: toolRef(1)},
				},
			},
		},
		{
			name: "a high-flow file on a standard nozzle",
			file: "mk4s-hf04-asa", printer: set("MK4S", 0.4, false, false, "ASA"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleNozzleHighFlow, 0)},
			},
		},
		{
			name: "a standard file on a high-flow nozzle passes",
			file: "mk4s-04-pla", printer: set("MK4S", 0.4, true, false, "PLA"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name: "a carbon fibre filament on a nozzle that is not hardened",
			file: "mk4s-06-petgcf", printer: set("MK4S", 0.6, false, false, "PETG"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleNozzleHardened, 0)},
			},
		},
		{
			name: "a carbon fibre filament on the fifth tool of an XL",
			file: "xl5t-06-cf", printer: set("XL 5T", 0.6, false, false, "PLA", "PLA", "PLA", "PLA", "PETG"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleNozzleHardened, 4)},
			},
		},
		{
			name: "a 0.6 mm file on a 0.4 mm nozzle",
			file: "coreone-06-pccf", printer: set("CORE One", 0.4, false, true, "PC"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleNozzleDiameter, 0)},
			},
		},
		{
			name: "a PETG file with PLA loaded",
			file: "coreone-04-petg", printer: factoryPrinter(t, "CORE One"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleMaterial, 0)},
			},
		},
		{
			name: "an MK4 file on an MK4S runs in compatibility mode",
			file: "mk4is-04-pla", printer: factoryPrinter(t, "MK4S"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleCompatibilityMode, -1)},
			},
		},
		{
			name: "an MK4S file on an MK4 is the wrong model",
			file: "mk4s-04-pla", printer: factoryPrinter(t, "MK4"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RulePrinterModel, -1)},
			},
		},
		{
			name: "an MK3.9 file on an MK4 is the same group",
			file: "mk39-04-pla", printer: factoryPrinter(t, "MK4"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name: "the MK3.5 profile serves an MK3.5S",
			file: "mk35-04-pla", printer: factoryPrinter(t, "MK3.5S"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name: "an MK4 file from before input shaping",
			file: "mk4-04-pla", printer: factoryPrinter(t, "MK4"),
			want: printcompat.Verdict{
				Level:    printcompat.LevelWarning,
				Findings: []printcompat.Finding{warn(printcompat.RuleInputShaper, -1)},
			},
		},
		{
			name: "an MMU file on an MK4S with its MMU",
			file: "mk4s-mmu3-04", printer: set("MK4S MMU3", 0.4, false, false, "PLA", "PETG"),
			want: printcompat.Verdict{Level: printcompat.LevelOK},
		},
		{
			name: "an MMU file on an MK4S without one",
			file: "mk4s-mmu3-04", printer: factoryPrinter(t, "MK4S"),
			want: printcompat.Verdict{
				Level: printcompat.LevelRefusal,
				Findings: []printcompat.Finding{
					warn(printcompat.RuleMMU, -1),
					refuse(printcompat.RuleToolCount, -1),
				},
				// The printer reports one tool, so the second slot's
				// filament is not known.
				Unverified: []printcompat.Finding{{Rule: printcompat.RuleMaterial, Filament: toolRef(1)}},
			},
		},
		{
			name: "an XL file on a MINI does not fit",
			file: "xl-is-04-pla", printer: factoryPrinter(t, "MINI"),
			want: printcompat.Verdict{
				Level: printcompat.LevelRefusal,
				Findings: []printcompat.Finding{
					warn(printcompat.RulePrinterModel, -1),
					refuse(printcompat.RuleBuildVolume, -1),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(corpusFile(t, files, tc.file), tc.printer)
			if !sameVerdict(got, tc.want) {
				t.Errorf("got %s,\nwant %s", verdictText(got), verdictText(tc.want))
			}
		})
	}
}

// TestCorpusMatrix runs every file on every printer of the fleet and compares
// the verdicts with testdata/corpus-matrix.tsv. Run it with -update to rewrite
// the table after the corpus or the rules changed, and read the diff: each row
// that moved is a print the firmware now takes differently.
func TestCorpusMatrix(t *testing.T) {
	t.Parallel()

	files := corpus(t)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	slices.Sort(names)

	var table bytes.Buffer

	table.WriteString("file\tprinter\tlevel\tfindings\tunverified\n")

	for _, name := range names {
		for _, row := range factory {
			got := printcompat.Evaluate(files[name], row.printer())
			fmt.Fprintf(&table, "%s\t%s\t%s\t%s\t%s\n",
				name, row.name, got.Level, findingsText(got.Findings), findingsText(got.Unverified))
		}
	}

	if *update {
		if err := os.WriteFile(corpusMatrix, table.Bytes(), 0o600); err != nil {
			t.Fatalf("write the matrix: %v", err)
		}

		return
	}

	want, err := os.ReadFile(corpusMatrix)
	if err != nil {
		t.Fatalf("read the matrix, or run with -update to write it: %v", err)
	}

	if !bytes.Equal(table.Bytes(), want) {
		gotRows := strings.Split(table.String(), "\n")
		wantRows := strings.Split(string(want), "\n")

		for i := range max(len(gotRows), len(wantRows)) {
			got, wanted := rowAt(gotRows, i), rowAt(wantRows, i)
			if got != wanted {
				t.Errorf("row %d:\n got  %s\n want %s", i+1, got, wanted)
			}
		}
	}
}

func rowAt(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}

	return ""
}

// findingsText writes findings as rule:level@Tn, by the filament each is
// about, joined by commas, and a dash for none.
func findingsText(findings []printcompat.Finding) string {
	if len(findings) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(findings))

	for _, finding := range findings {
		part := string(finding.Rule)
		if finding.Level != "" {
			part += ":" + string(finding.Level)
		}

		if finding.Filament != nil {
			part += fmt.Sprintf("@T%d", *finding.Filament)
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, ",")
}

// sameVerdict compares the level and the rules, levels and tools of every
// finding. The texts a person reads are not compared.
func sameVerdict(got, want printcompat.Verdict) bool {
	return got.Level == want.Level &&
		findingsText(got.Findings) == findingsText(want.Findings) &&
		findingsText(got.Unverified) == findingsText(want.Unverified)
}

func verdictText(v printcompat.Verdict) string {
	return fmt.Sprintf("%s [%s] unverified [%s]", v.Level, findingsText(v.Findings), findingsText(v.Unverified))
}
