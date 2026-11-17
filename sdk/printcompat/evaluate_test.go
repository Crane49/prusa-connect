package printcompat_test

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/printcompat"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

// file builds a print file sliced for a model, with input shaping and the
// filaments given. Each filament string is a material; one after a "-" prefix
// is listed but not used.
func file(model string, filaments ...string) *storageapi.PrintInfo {
	out := &storageapi.PrintInfo{Checks: &storageapi.PrintChecks{
		PrinterModel: model, GCodeLevel: 2, Features: []string{"Input shaper"},
	}}

	for i, material := range filaments {
		used := yes()
		if len(material) > 0 && material[0] == '-' {
			material, used = material[1:], no()
		}

		out.Filaments = append(out.Filaments, storageapi.PrintFilament{
			Index: i, Used: used, Material: material, NozzleMM: 0.4,
		})

		if *used {
			out.Checks.Nozzles = append(out.Checks.Nozzles, storageapi.PrintNozzleCheck{
				Index: i, DiameterMM: 0.4, Hardened: no(), HighFlow: no(),
			})
		}
	}

	return out
}

// state is a state tree from path and JSON text pairs.
type state map[string]string

func (s state) printer(model string) printcompat.Printer {
	raw := make(map[string]json.RawMessage, len(s))
	for path, value := range s {
		raw[path] = json.RawMessage(value)
	}

	return printcompat.PrinterFromState(model, "6.5.3+12780", raw)
}

func at(id connectorapi.ToolID, member string) string { return connectorapi.ToolPath(id, member) }

func quoted(text string) string { return strconv.Quote(text) }

// head writes one enabled head on a standard 0.4 mm nozzle with no feeder,
// with a material unless the material is "?".
func (s state) head(id connectorapi.ToolID, material string) state {
	s[at(id, connectorapi.ToolEnabled)] = "true"
	s[at(id, connectorapi.FeatureNozzleDiameter)] = "0.4"
	s[at(id, connectorapi.FeatureNozzleHighFlow)] = "false"
	s[at(id, connectorapi.FeatureNozzleHardened)] = "false"
	s[at(id, connectorapi.FeederSlotCount)] = "0"

	if material != "?" {
		s[at(id, connectorapi.MaterialAbbreviation)] = quoted(material)
	}

	return s
}

// feeder fits a feeder to a tool with one slot per material.
func (s state) feeder(id connectorapi.ToolID, materials ...string) state {
	s[at(id, connectorapi.FeederSlotCount)] = strconv.Itoa(len(materials))
	delete(s, at(id, connectorapi.MaterialAbbreviation))

	for n, material := range materials {
		slot := id.Slot(n)
		s[at(slot, connectorapi.ToolEnabled)] = "true"
		s[at(slot, connectorapi.MaterialAbbreviation)] = quoted(material)
	}

	return s
}

// printer builds a toolchanger, or a single-head printer, with one head per
// material. The empty material is a head with nothing loaded.
func printer(model string, materials ...string) printcompat.Printer {
	s := state{connectorapi.ToolCountPath: strconv.Itoa(len(materials))}
	for n, material := range materials {
		s.head(connectorapi.HeadID(n), material)
	}

	return s.printer(model)
}

// mmuPrinter builds one head with a feeder of a slot per material, or with no
// feeder and the first material on the head when there is one material.
func mmuPrinter(model string, materials ...string) printcompat.Printer {
	s := state{connectorapi.ToolCountPath: "1"}.head("0", materials[0])
	if len(materials) > 1 {
		s.feeder("0", materials...)
	}

	return s.printer(model)
}

func withVolume(p printcompat.Printer) printcompat.Printer {
	p.BuildVolume = &printcompat.Volume{XMM: 360, YMM: 360, ZMM: 360}

	return p
}

// TestEvaluateMirrorsTheFirmware holds the verdict to the checks the public
// firmware makes before a print. Each case names the firmware check it
// mirrors.
func TestEvaluateMirrorsTheFirmware(t *testing.T) {
	t.Parallel()

	finding := func(rule printcompat.Rule, level printcompat.Level, filament int) printcompat.Finding {
		out := printcompat.Finding{Rule: rule, Level: level}
		if filament >= 0 {
			out.Filament = &filament
			out.Tool = connectorapi.HeadID(filament)
		}

		return out
	}
	onSlot := func(rule printcompat.Rule, level printcompat.Level, filament int, tool connectorapi.ToolID) printcompat.Finding {
		out := finding(rule, level, filament)
		out.Tool = tool

		return out
	}

	cases := []struct {
		name    string
		file    *storageapi.PrintInfo
		printer printcompat.Printer
		level   printcompat.Level
		want    []printcompat.Finding
	}{
		{
			name: "a file for the same model", file: file("MK4", "PLA"),
			printer: mmuPrinter("MK4", "PLA"), level: printcompat.LevelOK,
		},
		{
			name: "an MK3.9 file on an MK4 is the same group", file: file("MK3.9", "PLA"),
			printer: mmuPrinter("MK4", "PLA"), level: printcompat.LevelOK,
		},
		{
			name: "an MK4 file on an MK4S runs in compatibility mode", file: file("MK4", "PLA"),
			printer: mmuPrinter("MK4S", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleCompatibilityMode, printcompat.LevelWarning, -1)},
		},
		{
			name: "an MK3S file on a Core One follows the upgrade chain", file: file("MK3S", "PLA"),
			printer: mmuPrinter("CoreOne", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleCompatibilityMode, printcompat.LevelWarning, -1)},
		},
		{
			name: "a Core One file on a Core One L runs as its own", file: file("COREONE", "PLA"),
			printer: mmuPrinter("CoreOneL", "PLA"), level: printcompat.LevelOK,
		},
		{
			name: "an MK4S file on a Core One L keeps the Core One's mode", file: file("MK4S", "PLA"),
			printer: mmuPrinter("CoreOneL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleCompatibilityMode, printcompat.LevelWarning, -1)},
		},
		{
			name: "a Core One file on an MK4S is the wrong model", file: file("COREONE", "PLA"),
			printer: mmuPrinter("MK4S", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RulePrinterModel, printcompat.LevelWarning, -1)},
		},
		{
			name: "an XL file on a MINI is the wrong model", file: file("XL", "PLA"),
			printer: printer("MINI", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RulePrinterModel, printcompat.LevelWarning, -1)},
		},
		{
			name: "a model the firmware does not know", file: file("MK9", "PLA"),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RulePrinterModel, printcompat.LevelWarning, -1)},
		},
		{
			name: "a file not sliced for input shaping", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Checks.Features = nil

				return f
			}(),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleInputShaper, printcompat.LevelWarning, -1)},
		},
		{
			name: "a G-code level the firmware does not have", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Checks.GCodeLevel = 3

				return f
			}(),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleGCodeLevel, printcompat.LevelWarning, -1)},
		},
		{
			name: "a firmware older than the file asks for", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Checks.MinFirmware = "6.6.0"

				return f
			}(),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleFirmware, printcompat.LevelWarning, -1)},
		},
		{
			name: "an XL file using three tools on a one-tool XL", file: file("XL", "PLA", "PETG", "ASA"),
			printer: printer("XL", "PLA"), level: printcompat.LevelRefusal,
			want: []printcompat.Finding{
				finding(printcompat.RuleToolCount, printcompat.LevelRefusal, -1),
				finding(printcompat.RuleToolEnabled, printcompat.LevelRefusal, 1),
				finding(printcompat.RuleToolEnabled, printcompat.LevelRefusal, 2),
			},
		},
		{
			name: "a tool switched off on a toolchanger", file: file("XL", "PLA", "-PLA", "ASA"),
			printer: func() printcompat.Printer {
				s := state{connectorapi.ToolCountPath: "5"}.head("0", "PLA").head("1", "PETG")
				s[at("2", connectorapi.ToolEnabled)] = "false"

				return s.printer("XL")
			}(),
			level: printcompat.LevelRefusal,
			want:  []printcompat.Finding{finding(printcompat.RuleToolEnabled, printcompat.LevelRefusal, 2)},
		},
		{
			name:    "unused tools of a five-tool file are not checked",
			file:    file("XL", "PLA", "-PETG", "-ASA", "-PLA", "-PVA"),
			printer: printer("XL", "PLA"),
			level:   printcompat.LevelOK,
		},
		{
			name: "an MMU file on an MK4S with the MMU enabled", file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "PETG")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer: mmuPrinter("MK4S", "PLA", "PETG", "PLA", "PLA", "PLA"), level: printcompat.LevelOK,
		},
		{
			name: "an MMU file on an MK4S without one", file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "PETG")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer: mmuPrinter("MK4S", "PLA"), level: printcompat.LevelRefusal,
			want: []printcompat.Finding{
				finding(printcompat.RuleMMU, printcompat.LevelWarning, -1),
				finding(printcompat.RuleToolCount, printcompat.LevelRefusal, -1),
			},
		},
		{
			name: "an MMU file that uses one tool on an MK4S without one", file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "-PETG")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer: mmuPrinter("MK4S", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleMMU, printcompat.LevelWarning, -1)},
		},
		{
			name: "an MMU feature on a firmware without MMU support", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleUnsupportedFeature, printcompat.LevelWarning, -1)},
		},
		{
			name: "a wrong nozzle on the second tool", file: file("XL", "PLA", "PETG"),
			printer: func() printcompat.Printer {
				s := state{connectorapi.ToolCountPath: "2"}.head("0", "PLA").head("1", "PETG")
				s[at("1", connectorapi.FeatureNozzleDiameter)] = "0.6"

				return s.printer("XL")
			}(),
			level: printcompat.LevelWarning,
			want:  []printcompat.Finding{finding(printcompat.RuleNozzleDiameter, printcompat.LevelWarning, 1)},
		},
		{
			name: "a hardened high-flow nozzle the tool does not have", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Checks.Nozzles[0].Hardened, f.Checks.Nozzles[0].HighFlow = yes(), yes()

				return f
			}(),
			printer: printer("XL", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{
				finding(printcompat.RuleNozzleHardened, printcompat.LevelWarning, 0),
				finding(printcompat.RuleNozzleHighFlow, printcompat.LevelWarning, 0),
			},
		},
		{
			name: "a tool with nothing loaded is a known empty tool", file: file("XL", "PLA", "PETG"),
			printer: printer("XL", "PLA", ""), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleMaterial, printcompat.LevelWarning, 1)},
		},
		{
			name: "a print taller than the machine", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.MaxLayerZMM = 361

				return f
			}(),
			printer: withVolume(printer("XL", "PLA")), level: printcompat.LevelRefusal,
			want: []printcompat.Finding{finding(printcompat.RuleBuildVolume, printcompat.LevelRefusal, -1)},
		},
		{
			name: "a print wider than the bed", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.Bounds = &storageapi.PrintBounds{MinXMM: 10, MinYMM: 10, MaxXMM: 370, MaxYMM: 100}

				return f
			}(),
			printer: withVolume(printer("XL", "PLA")), level: printcompat.LevelRefusal,
			want: []printcompat.Finding{finding(printcompat.RuleBuildVolume, printcompat.LevelRefusal, -1)},
		},
		{
			name: "a print that fits", file: func() *storageapi.PrintInfo {
				f := file("XL", "PLA")
				f.MaxLayerZMM = 9.9
				f.Bounds = &storageapi.PrintBounds{MinXMM: 175, MinYMM: 175, MaxXMM: 185, MaxYMM: 185}

				return f
			}(),
			printer: withVolume(printer("XL", "PLA")), level: printcompat.LevelOK,
		},
		{
			name: "an MMU file lands on the slots and checks the head's nozzle", file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "-PETG", "ASA")
				f.Checks.Features = append(f.Checks.Features, "MMU3")
				f.Checks.Nozzles = f.Checks.Nozzles[:1]
				f.Checks.Nozzles[0].Hardened = yes()

				return f
			}(),
			printer: mmuPrinter("MK4S", "PLA", "PETG", "PETG", "PLA", "PLA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{
				onSlot(printcompat.RuleNozzleHardened, printcompat.LevelWarning, 0, "0.0"),
				onSlot(printcompat.RuleMaterial, printcompat.LevelWarning, 2, "0.2"),
				onSlot(printcompat.RuleNozzleHardened, printcompat.LevelWarning, 2, "0.2"),
			},
		},
		{
			name: "a slot switched off on the feeder", file: func() *storageapi.PrintInfo {
				f := file("MK4S", "PLA", "PETG")
				f.Checks.Features = append(f.Checks.Features, "MMU3")

				return f
			}(),
			printer: func() printcompat.Printer {
				s := state{connectorapi.ToolCountPath: "1"}.head("0", "?").feeder("0", "PLA", "PETG", "ASA")
				s[at("0.1", connectorapi.ToolEnabled)] = "false"

				return s.printer("MK4S")
			}(),
			level: printcompat.LevelRefusal,
			want:  []printcompat.Finding{onSlot(printcompat.RuleToolEnabled, printcompat.LevelRefusal, 1, "0.1")},
		},
		{
			name: "two heads each with a feeder, with the mapping the driver reports",
			file: file("XL", "PLA", "PETG", "ASA"),
			printer: func() printcompat.Printer {
				s := state{connectorapi.ToolCountPath: "2"}.
					head("0", "?").feeder("0", "PLA", "PC").
					head("1", "?").feeder("1", "ASA", "PETG")
				s[at("1", connectorapi.FeatureNozzleHardened)] = "true"
				s[connectorapi.GCodeToolMapPath(0)] = quoted("0.0")
				s[connectorapi.GCodeToolMapPath(1)] = quoted("1.1")
				s[connectorapi.GCodeToolMapPath(2)] = quoted("0.1")

				return s.printer("XL")
			}(),
			level: printcompat.LevelWarning,
			want: []printcompat.Finding{
				onSlot(printcompat.RuleMaterial, printcompat.LevelWarning, 2, "0.1"),
			},
		},
		{
			name: "two heads each with a feeder and no mapping land on the heads",
			file: file("XL", "PLA", "PETG"),
			printer: state{connectorapi.ToolCountPath: "2"}.
				head("0", "?").feeder("0", "PLA", "PC").
				head("1", "?").feeder("1", "ASA", "PETG").printer("XL"),
			level: printcompat.LevelOK,
		},
		{
			name: "a different filament loaded", file: file("XL", "PLA", "PETG"),
			printer: printer("XL", "PLA", "ASA"), level: printcompat.LevelWarning,
			want: []printcompat.Finding{finding(printcompat.RuleMaterial, printcompat.LevelWarning, 1)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := printcompat.Evaluate(tc.file, tc.printer)
			if got.Level != tc.level {
				t.Errorf("level: got %s, want %s (findings %+v)", got.Level, tc.level, got.Findings)
			}

			rules := make([]printcompat.Finding, 0, len(got.Findings))
			for _, f := range got.Findings {
				rules = append(
					rules,
					printcompat.Finding{Rule: f.Rule, Level: f.Level, Filament: f.Filament, Tool: f.Tool},
				)
			}

			if len(rules) == 0 {
				rules = nil
			}

			if !reflect.DeepEqual(rules, tc.want) {
				t.Errorf("findings: got %+v, want %+v", rules, tc.want)
			}

			if again := printcompat.Evaluate(tc.file, tc.printer); !reflect.DeepEqual(again, got) {
				t.Error("a second evaluation of the same input answered differently")
			}
		})
	}
}

// TestEvaluateNamesWhatItCouldNotCheck holds the rule that an unknown fact is
// never a pass: the check moves to Unverified.
func TestEvaluateNamesWhatItCouldNotCheck(t *testing.T) {
	t.Parallel()

	rules := func(found []printcompat.Finding) []printcompat.Rule {
		out := make([]printcompat.Rule, 0, len(found))
		for _, f := range found {
			out = append(out, f.Rule)
		}

		return out
	}

	t.Run("a file whose G-code was not read", func(t *testing.T) {
		t.Parallel()

		f := file("XL", "PLA")
		f.Checks = nil

		got := printcompat.Evaluate(f, printer("XL", "PLA"))
		want := []printcompat.Rule{
			printcompat.RulePrinterModel, printcompat.RuleGCodeLevel,
			printcompat.RuleFirmware, printcompat.RuleInputShaper,
		}

		if got.Level != printcompat.LevelOK || !reflect.DeepEqual(rules(got.Unverified), want) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a printer that reported no tools", func(t *testing.T) {
		t.Parallel()

		got := printcompat.Evaluate(file("XL", "PLA", "PETG"), printcompat.Printer{Model: "XL"})
		want := []printcompat.Rule{
			printcompat.RuleToolCount, printcompat.RuleToolEnabled, printcompat.RuleMaterial,
			printcompat.RuleNozzleDiameter, printcompat.RuleToolEnabled, printcompat.RuleMaterial,
			printcompat.RuleNozzleDiameter,
		}

		if got.Level != printcompat.LevelOK || !reflect.DeepEqual(rules(got.Unverified), want) {
			t.Errorf("got %+v, want unverified %v", got, want)
		}
	})

	t.Run("a material and a volume the printer did not report", func(t *testing.T) {
		t.Parallel()

		p := printer("XL", "?")

		f := file("XL", "PLA")
		f.MaxLayerZMM = 10

		got := printcompat.Evaluate(f, p)
		want := []printcompat.Rule{printcompat.RuleMaterial, printcompat.RuleBuildVolume}

		if got.Level != printcompat.LevelOK || !reflect.DeepEqual(rules(got.Unverified), want) {
			t.Errorf("got %+v, want unverified %v", got, want)
		}
	})

	t.Run("a model this package does not know", func(t *testing.T) {
		t.Parallel()

		got := printcompat.Evaluate(file("MK4", "PLA"), printer("Voron 2.4", "PLA"))
		want := []printcompat.Rule{printcompat.RulePrinterModel, printcompat.RuleInputShaper}

		if got.Level != printcompat.LevelOK || !reflect.DeepEqual(rules(got.Unverified), want) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("no file", func(t *testing.T) {
		t.Parallel()

		if got := printcompat.Evaluate(nil, printer("XL")); got.Level != printcompat.LevelOK {
			t.Errorf("got %+v", got)
		}
	})
}

// TestPrinterFromStateReadsTheToolTree reads the state trees the shipping
// driver writes for an XL and for an MK4S with an MMU3.
func TestPrinterFromStateReadsTheToolTree(t *testing.T) {
	t.Parallel()

	t.Run("an XL", func(t *testing.T) {
		t.Parallel()

		got := state{
			connectorapi.ToolCountPath: "5", at("0", connectorapi.ToolActive): "false",
			at("1", connectorapi.ToolActive):            "true",
			at("1", connectorapi.FeatureNozzleHardened): "true", at("2", connectorapi.ToolEnabled): "false",
			"/temperature/nozzle": "240", "/tool/1/material": `"old"`,
			"/build_volume/x": "360", "/build_volume/y": "360", "/build_volume/z": "360",
		}.head("0", "PLA").head("1", "PETG").printer("XL")

		if len(got.Tools.Heads()) != 3 || got.Tools.Active != "1" || got.Tools.Count != 5 {
			t.Fatalf("got %+v", got.Tools)
		}

		if got.BuildVolume == nil || *got.BuildVolume != (printcompat.Volume{XMM: 360, YMM: 360, ZMM: 360}) {
			t.Errorf("build volume: got %+v", got.BuildVolume)
		}

		if got.Tools.Tool("0").NozzleTemp != nil {
			t.Error("a path outside the tree went to a head")
		}
	})

	t.Run("an MK4S with an MMU3 and an empty slot", func(t *testing.T) {
		t.Parallel()

		got := state{connectorapi.ToolCountPath: "1", at("0", connectorapi.NozzleTemp): "215"}.
			head("0", "?").feeder("0", "PLA", "", "ASA").printer("MK4S")

		if got.Tools.Resolve(1) != "0.1" || !got.Tools.Live("0.1") {
			t.Fatalf("got %+v", got.Tools)
		}

		if empty := got.Tools.Tool("0.1").Material; empty == nil || *empty != "" {
			t.Errorf("an empty slot is a known empty material: got %v", empty)
		}

		if temp := got.Tools.Tool("0").NozzleTemp; temp == nil || *temp != 215 {
			t.Errorf("the head's reading: got %v", temp)
		}
	})

	// The flat paths of an earlier release are no tools: the tree is the only
	// reading.
	t.Run("a printer that reports no tools", func(t *testing.T) {
		t.Parallel()

		got := state{
			"/material": `"PETG"`, "/temperature/nozzle": "25", "/temperature/nozzle_target": "0",
		}.printer("MINI")

		if len(got.Tools.Tools) != 0 {
			t.Errorf("got %+v", got.Tools.Tools)
		}
	})

	t.Run("an empty tree", func(t *testing.T) {
		t.Parallel()

		if got := printcompat.PrinterFromState("MINI", "", nil); len(got.Tools.Tools) != 0 || got.Tools.CountKnown {
			t.Errorf("got %+v", got)
		}
	})
}
