package printcompat

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Level is how hard the firmware objects to a print.
type Level string

// The levels of a Verdict and a Finding, mildest first.
const (
	// LevelOK is a print the firmware starts without a question.
	LevelOK Level = "ok"
	// LevelWarning is a print the firmware stops at a dialog for. A person at
	// the printer may continue. This is the firmware default. A printer set
	// to ignore or to refuse these checks is not visible here.
	LevelWarning Level = "warning"
	// LevelRefusal is a print the firmware will not start, or one that cannot
	// fit the machine whatever the firmware does.
	LevelRefusal Level = "refusal"
)

// rank orders the levels, so the worst of several can be taken.
func (l Level) rank() int {
	switch l {
	case LevelWarning:
		return 1
	case LevelRefusal:
		return 2 //nolint:mnd // the third of three levels.
	case LevelOK:
		return 0
	}

	return 0
}

// Rule names one check the firmware makes before a print. The names follow
// the firmware's own checks in gcode_compatibility.hpp.
type Rule string

// The rules. Each comment starts with the name of the firmware check.
const (
	// RulePrinterModel is printer_model: the file is sliced for a model whose
	// G-code this printer does not run.
	RulePrinterModel Rule = "printer_model"
	// RuleCompatibilityMode is gcode_compatibility_mode: the file is sliced for
	// an older model this printer runs in a compatibility mode, such as an
	// MK4 file on an MK4S.
	RuleCompatibilityMode Rule = "compatibility_mode"
	// RuleGCodeLevel is gcode_level: the file asks for a newer G-code level
	// than the firmware has.
	RuleGCodeLevel Rule = "gcode_level"
	// RuleFirmware is minimum_fw_version: the file asks for a newer firmware.
	RuleFirmware Rule = "firmware"
	// RuleInputShaper is input_shaper: the file was not sliced for input
	// shaping.
	RuleInputShaper Rule = "input_shaper"
	// RuleINDXLock is indx_lock: a Core One with INDX, on firmware 6.6.2 or
	// newer, runs a file whose M862.6 lines do not name the INDX lock
	// feature. The printer cannot be set to ignore this warning.
	RuleINDXLock Rule = "indx_lock"
	// RuleMMU is mmu: the file was sliced for an MMU the printer does not
	// have enabled.
	RuleMMU Rule = "mmu"
	// RuleUnsupportedFeature is unsupported_features: the file asks for a
	// firmware feature this printer does not have.
	RuleUnsupportedFeature Rule = "unsupported_feature"
	// RuleToolCount is not_enough_tools: the file uses more tools than the
	// printer has enabled.
	RuleToolCount Rule = "tool_count"
	// RuleNozzleCleaner is nozzle_cleaner_not_calibrated: a Core One with
	// INDX refuses every print until its nozzle cleaner is calibrated. The
	// host cannot read that state, so the check is always unverified.
	RuleNozzleCleaner Rule = "nozzle_cleaner"
	// RuleToolEnabled is correct_tool: a tool the file uses is not enabled on
	// the printer.
	RuleToolEnabled Rule = "tool_enabled"
	// RuleNozzleDiameter is nozzle_diameter: a tool's nozzle differs from the
	// one the file was sliced for by more than 0.001 mm.
	RuleNozzleDiameter Rule = "nozzle_diameter"
	// RuleNozzleHardened is nozzle_hardened: the file asks for a hardened
	// nozzle the tool does not have.
	RuleNozzleHardened Rule = "nozzle_hardened"
	// RuleNozzleHighFlow is nozzle_high_flow: the file asks for a high-flow
	// nozzle the tool does not have.
	RuleNozzleHighFlow Rule = "nozzle_high_flow"
	// RuleNozzleNotHighFlow is nozzle_not_high_flow, the firmware's check for
	// a multi-material print on a printer with an MMU.
	RuleNozzleNotHighFlow Rule = "nozzle_not_high_flow"
	// RuleFilamentAbrasive is the abrasive check of filament_compatibility,
	// on firmware 6.8.0 or newer: an abrasive filament on a nozzle that is
	// not hardened. The host cannot read whether a filament defined on the
	// printer is abrasive, so this rule is never a finding. It is unverified
	// for a filament type that is no preset, on a nozzle not known as
	// hardened.
	RuleFilamentAbrasive Rule = "filament_abrasive"
	// RuleMaterial is filament_type: the filament loaded in a tool is not the
	// one the file was sliced for, or the tool has nothing loaded.
	RuleMaterial Rule = "material"
	// RuleBuildVolume is the print reaching outside the printer's build
	// volume. The firmware makes no such check: the machine cannot print it.
	RuleBuildVolume Rule = "build_volume"
)

// Finding is one check that failed, or one that could not be made.
type Finding struct {
	Rule Rule `json:"rule"`
	// Level is how hard the firmware objects. It is empty on a finding that
	// could not be checked.
	Level Level `json:"level,omitempty"`
	// Filament is n of the Tn the check is about, counted from 0, and nil for
	// a check about the whole print.
	Filament *int `json:"filament,omitempty"`
	// Tool is the id of the printer's tool that filament lands on, such as
	// "1" or "0.2", and empty when the check is about the whole print.
	Tool connectorapi.ToolID `json:"tool,omitempty"`
	// Want is what the file asks for and Have what the printer has, as text a
	// person reads. Either is empty where it is not known.
	Want string `json:"want,omitempty"`
	Have string `json:"have,omitempty"`
}

// Verdict is how the firmware would take one file on one printer.
type Verdict struct {
	// Level is the worst level among the findings, and LevelOK when there is
	// none.
	Level Level `json:"level"`
	// Findings are the checks that failed, in the order Evaluate makes them.
	Findings []Finding `json:"findings,omitempty"`
	// Unverified are the checks the firmware would make that could not be
	// made here, because a fact on either side is unknown.
	Unverified []Finding `json:"unverified,omitempty"`
}

// nozzleTolerance is how far a nozzle may differ from the file's and still
// pass, in millimetres (gcode_compatibility.cpp, the nozzle_diameter check).
const nozzleTolerance = 0.001

// gcodeLevel is the G-code level the firmware supports (gcode_level in
// src/common/gcode/gcode_info.hpp).
const gcodeLevel = 2

// The feature names an M862.6 line may carry that a firmware knows.
const (
	featureInputShaper = "input shaper"
	featureMMU         = "mmu3"
	featureINDXLock    = "indx lock"
)

// indxLockFirmware is the first firmware that knows the INDX lock feature.
const indxLockFirmware = "6.6.2"

// filamentChecksFirmware is the first firmware that runs the
// filament_compatibility checks for each filament a file uses.
const filamentChecksFirmware = "6.8.0"

// Evaluate compares what a file needs with what a printer has and says how
// the printer's firmware would take the print. The checks run in the order
// of the firmware, and the build volume check comes last. Each filament is
// checked on the tool that connectorapi.ToolTree.Resolve gives its T number.
// A nil file is OK.
func Evaluate(file *storageapi.PrintInfo, printer Printer) Verdict {
	var e evaluation

	if file == nil {
		return e.verdict()
	}

	fw, known := printerModel(printer.Model)

	e.gcodeChecks(file.Checks, printer, fw, known)
	e.filamentChecks(file, printer, fw, known)
	e.volumeCheck(file, printer.BuildVolume)

	return e.verdict()
}

// evaluation gathers findings as the checks run.
type evaluation struct {
	findings   []Finding
	unverified []Finding
}

// subject is what a check is about: one filament and the tool it lands on,
// or the whole print.
type subject struct {
	filament int
	tool     connectorapi.ToolID
}

// whole is the subject of a check about the whole print.
//
//nolint:gochecknoglobals // a fixed value, read-only.
var whole = subject{filament: -1}

func (s subject) finding(rule Rule, level Level) Finding {
	out := Finding{Rule: rule, Level: level, Tool: s.tool}
	if s.filament >= 0 {
		filament := s.filament
		out.Filament = &filament
	}

	return out
}

func (e *evaluation) fail(rule Rule, level Level, on subject, want, have string) {
	finding := on.finding(rule, level)
	finding.Want, finding.Have = want, have
	e.findings = append(e.findings, finding)
}

func (e *evaluation) unknown(rule Rule, on subject) {
	e.unverified = append(e.unverified, on.finding(rule, ""))
}

func (e *evaluation) verdict() Verdict {
	out := Verdict{Level: LevelOK, Findings: e.findings, Unverified: e.unverified}

	for _, finding := range e.findings {
		if finding.Level.rank() > out.Level.rank() {
			out.Level = finding.Level
		}
	}

	return out
}

// gcodeChecks mirrors GCodeInfo::parse_m862 and the input shaper check.
func (e *evaluation) gcodeChecks(checks *storageapi.PrintChecks, printer Printer, fw model, known bool) {
	if checks == nil {
		for _, rule := range []Rule{RulePrinterModel, RuleGCodeLevel, RuleFirmware, RuleInputShaper} {
			e.unknown(rule, whole)
		}

		if indxLockOf(printer, fw, known) != firmwareLacks {
			e.unknown(RuleINDXLock, whole)
		}

		return
	}

	if checks.PrinterModel != "" {
		sliced, found := slicedModel(checks.PrinterModel)
		e.modelCheck(sliced, found, checks.PrinterModel, printer, fw, known)
	}

	if checks.PrinterCode != 0 {
		sliced, found := slicedCode(checks.PrinterCode)
		e.modelCheck(sliced, found, strconv.Itoa(checks.PrinterCode), printer, fw, known)
	}

	if checks.GCodeLevel > gcodeLevel {
		e.fail(RuleGCodeLevel, LevelWarning, whole, strconv.Itoa(checks.GCodeLevel), strconv.Itoa(gcodeLevel))
	}

	if checks.MinFirmware != "" {
		switch {
		case printer.Firmware == "":
			e.unknown(RuleFirmware, whole)
		case !upToDate(checks.MinFirmware, printer.Firmware):
			e.fail(RuleFirmware, LevelWarning, whole, checks.MinFirmware, printer.Firmware)
		}
	}

	named := e.featureChecks(checks.Features, printer, fw, known)

	switch {
	case !known:
		e.unknown(RuleInputShaper, whole)
	case !named.inputShaper && fw.group != groupIX:
		e.fail(RuleInputShaper, LevelWarning, whole, "", "")
	}

	if !named.indxLock {
		switch indxLockOf(printer, fw, known) {
		case firmwareHas:
			e.fail(RuleINDXLock, LevelWarning, whole, "", "")
		case firmwareUnknown:
			e.unknown(RuleINDXLock, whole)
		case firmwareLacks:
		}
	}
}

// indxLockOf is whether the printer's firmware knows the INDX lock feature:
// an INDX model on firmware 6.6.2 or newer.
func indxLockOf(printer Printer, fw model, known bool) firmwareSupport {
	if !known || !fw.isINDX() {
		return firmwareLacks
	}

	return firmwareSince(printer.Firmware, indxLockFirmware)
}

// modelCheck is the check one M862.3 or M862.2 line makes.
func (e *evaluation) modelCheck(sliced model, found bool, named string, printer Printer, fw model, known bool) {
	switch {
	case !known:
		e.unknown(RulePrinterModel, whole)
	case !found:
		e.fail(RulePrinterModel, LevelWarning, whole, named, printer.Model)
	default:
		switch fitOf(fw.group, sliced.group) {
		case fitFull:
		case fitCompatibilityMode:
			e.fail(RuleCompatibilityMode, LevelWarning, whole, named, printer.Model)
		case fitNone:
			e.fail(RulePrinterModel, LevelWarning, whole, named, printer.Model)
		}
	}
}

// namedFeatures are the features of an M862.6 line that later checks read.
type namedFeatures struct {
	inputShaper bool
	indxLock    bool
}

// featureChecks is the M862.6 arm of parse_m862. It reports whether the file
// names input shaping and the INDX lock.
func (e *evaluation) featureChecks(features []string, printer Printer, fw model, known bool) namedFeatures {
	var named namedFeatures

	for _, feature := range features {
		switch strings.ToLower(strings.TrimSpace(feature)) {
		case featureInputShaper:
			named.inputShaper = true
		case featureMMU:
			switch {
			case !known:
				e.unknown(RuleMMU, whole)
			case !fw.takesMMU():
				e.fail(RuleUnsupportedFeature, LevelWarning, whole, feature, "")
			default:
				e.feederCheck(feature, printer.Tools.Tool("0"))
			}
		case featureINDXLock:
			named.indxLock = true

			switch indxLockOf(printer, fw, known) {
			case firmwareLacks:
				if known {
					e.fail(RuleUnsupportedFeature, LevelWarning, whole, feature, "")
				}
			case firmwareUnknown:
				// An INDX firmware older than 6.6.2 does not know it.
				e.unknown(RuleUnsupportedFeature, whole)
			case firmwareHas:
			}
		default:
			if known {
				e.fail(RuleUnsupportedFeature, LevelWarning, whole, feature, "")
			}
		}
	}

	return named
}

// feederCheck is the mmu arm of parse_m862: the file asks for an MMU, and the
// one head of the printer must have a feeder.
func (e *evaluation) feederCheck(feature string, head *connectorapi.Tool) {
	switch {
	case head == nil || head.Feeder == nil:
		e.unknown(RuleMMU, whole)
	case !head.HasFeeder():
		e.fail(RuleMMU, LevelWarning, whole, feature, "")
	}
}

// nozzleNeed is what the M862.1 lines ask of the nozzle a filament prints
// through.
type nozzleNeed struct {
	diameter float64
	hardened *bool
	highFlow *bool
}

// nozzleNeeds is what each filament's M862.1 line asks. On a firmware built
// with MMU support one line applies to every filament: the slots share a nozzle.
func nozzleNeeds(checks *storageapi.PrintChecks, filaments int, mmuFirmware bool) map[int]nozzleNeed {
	out := map[int]nozzleNeed{}
	if checks == nil {
		return out
	}

	for _, line := range checks.Nozzles {
		targets := []int{line.Index}

		if mmuFirmware {
			targets = targets[:0]
			for index := range max(filaments, 1) {
				targets = append(targets, index)
			}
		}

		for _, index := range targets {
			need := out[index]
			if line.DiameterMM > 0 {
				need.diameter = line.DiameterMM
			}

			if line.Hardened != nil {
				need.hardened = line.Hardened
			}

			if line.HighFlow != nil {
				need.highFlow = line.HighFlow
			}

			out[index] = need
		}
	}

	return out
}

// filamentChecks mirrors the tool count check and the per-tool checks, for
// each filament the file uses on the tool it lands on.
func (e *evaluation) filamentChecks(file *storageapi.PrintInfo, printer Printer, fw model, known bool) {
	used := usedFilaments(file.Filaments)
	tree := printer.Tools

	enabled := reachable(tree)

	switch {
	case len(used) <= 1 && enabled == 0:
	case enabled == 0:
		e.unknown(RuleToolCount, whole)
	case len(used) > enabled:
		e.fail(RuleToolCount, LevelRefusal, whole, strconv.Itoa(len(used)), strconv.Itoa(enabled))
	}

	// The firmware makes this check after the tool count, in
	// generate_without_toolmapping, and refuses the print when it fails.
	if known && fw.isINDX() {
		e.unknown(RuleNozzleCleaner, whole)
	}

	firmware := firmwareRules{
		mmu:      known && fw.takesMMU(),
		abrasive: firmwareSince(printer.Firmware, filamentChecksFirmware) != firmwareLacks,
	}
	needs := nozzleNeeds(file.Checks, len(file.Filaments), firmware.mmu)
	single := singleFilament(file.Filaments)

	for _, filament := range used {
		on := subject{filament: filament.Index, tool: tree.Resolve(filament.Index)}
		e.filamentCheck(filament, on, needs[filament.Index], tree, firmware, single)
	}
}

// reachable is how many tools the file's T numbers can land on: the live tools
// of a reported G-code tool map, else the live slots of a single head's
// feeder, else the live heads.
func reachable(tree connectorapi.ToolTree) int {
	count := 0

	switch heads := tree.Heads(); {
	case len(tree.GCodeToolMap) > 0:
		seen := map[connectorapi.ToolID]bool{}
		for _, id := range tree.GCodeToolMap {
			if tree.Live(id) && !seen[id] {
				seen[id] = true
				count++
			}
		}
	case len(heads) == 1 && tree.Tool(heads[0]).HasFeeder():
		for _, slot := range tree.Slots(heads[0]) {
			if tree.Live(slot) {
				count++
			}
		}
	default:
		for _, head := range heads {
			if tree.Live(head) {
				count++
			}
		}
	}

	return count
}

// firmwareRules is what the printer's firmware brings to the per-tool checks.
type firmwareRules struct {
	// mmu is a firmware built with MMU support.
	mmu bool
	// abrasive is a firmware that makes the abrasive check, or one whose
	// version is unknown.
	abrasive bool
}

// filamentCheck is the per-tool arm of generate_toolmapping_only_noclear for
// one filament the file uses, on the tool it lands on.
func (e *evaluation) filamentCheck(
	filament storageapi.PrintFilament,
	on subject,
	need nozzleNeed,
	tree connectorapi.ToolTree,
	firmware firmwareRules,
	single bool,
) {
	tool := tree.Tool(on.tool)
	e.enabledCheck(on, tree, firmware.mmu)

	// A firmware built with MMU support has one head, and every filament
	// prints through its nozzle whether or not the MMU is on.
	nozzleAt := on.tool
	if firmware.mmu {
		nozzleAt = "0"
	}

	highFlow := featureBool(tree, nozzleAt, connectorapi.FeatureNozzleHighFlow)
	feeding := tree.Head(nozzleAt).HasFeeder()

	if need.highFlow != nil && *need.highFlow && feeding && !single && highFlow != nil && !*highFlow {
		e.fail(RuleNozzleNotHighFlow, LevelWarning, on, "", "")
	}

	var loaded *string
	if tool != nil {
		loaded = tool.Material
	}

	e.materialCheck(on, filament.Material, loaded)

	hardened := featureBool(tree, nozzleAt, connectorapi.FeatureNozzleHardened)
	if firmware.abrasive {
		e.abrasiveCheck(on, filament.Material, hardened)
	}

	if need.diameter > 0 {
		diameter := featureNumber(tree, nozzleAt, connectorapi.FeatureNozzleDiameter)

		switch {
		case diameter == nil || *diameter <= 0:
			e.unknown(RuleNozzleDiameter, on)
		case math.Abs(need.diameter-*diameter) > nozzleTolerance:
			e.fail(RuleNozzleDiameter, LevelWarning, on, millimetres(need.diameter), millimetres(*diameter))
		}
	}

	e.flagCheck(RuleNozzleHardened, on, need.hardened, hardened)
	e.flagCheck(RuleNozzleHighFlow, on, need.highFlow, highFlow)
}

// enabledCheck is the correct_tool check: the tool a filament lands on must
// exist and be switched on. A firmware built with MMU support passes any slot
// that is not switched off.
func (e *evaluation) enabledCheck(on subject, tree connectorapi.ToolTree, mmuFirmware bool) {
	tool := tree.Tool(on.tool)

	switch {
	case tool != nil && tool.Disabled:
		e.fail(RuleToolEnabled, LevelRefusal, on, "T"+strconv.Itoa(on.filament), "")
	case tree.Live(on.tool) || mmuFirmware:
	case len(tree.Tools) == 0 || (tool == nil && !tree.CountKnown && on.tool.IsHead()):
		e.unknown(RuleToolEnabled, on)
	case tool == nil && !on.tool.IsHead() && tree.Live(parentOf(on.tool)):
		// A slot the feeder's count covers and the driver said nothing of.
		e.unknown(RuleToolEnabled, on)
	default:
		e.fail(RuleToolEnabled, LevelRefusal, on, "T"+strconv.Itoa(on.filament), "")
	}
}

func parentOf(id connectorapi.ToolID) connectorapi.ToolID {
	parent, _ := id.Parent()

	return parent
}

// featureNumber is one nozzle feature as a number, and nil when unknown.
func featureNumber(tree connectorapi.ToolTree, id connectorapi.ToolID, member string) *float64 {
	raw, ok := tree.Nozzle(id, strings.TrimPrefix(member, "features/"))
	if !ok {
		return nil
	}

	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}

	return &value
}

// featureBool is one nozzle feature as a boolean, and nil when unknown.
func featureBool(tree connectorapi.ToolTree, id connectorapi.ToolID, member string) *bool {
	raw, ok := tree.Nozzle(id, strings.TrimPrefix(member, "features/"))
	if !ok {
		return nil
	}

	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}

	return &value
}

// nothingLoaded is what a finding says a tool with no filament has.
const nothingLoaded = "nothing loaded"

// materialCheck is the filament_type check. A tool with nothing loaded fails
// it. A material the printer did not report is unverified.
func (e *evaluation) materialCheck(on subject, wanted string, loaded *string) {
	want := strings.TrimSpace(wanted)
	if want == "" || want == "---" {
		return
	}

	switch {
	case loaded == nil:
		e.unknown(RuleMaterial, on)
	case *loaded == connectorapi.NoMaterial:
		e.fail(RuleMaterial, LevelWarning, on, want, nothingLoaded)
	case *loaded != want:
		e.fail(RuleMaterial, LevelWarning, on, want, *loaded)
	}
}

// abrasiveCheck is the abrasive check of filament_compatibility. A preset is
// never abrasive and a hardened nozzle takes any filament. Other names are
// unverified.
func (e *evaluation) abrasiveCheck(on subject, material string, hardened *bool) {
	name := strings.TrimSpace(material)
	if name == "" || name == "---" || presetFilaments[name] || (hardened != nil && *hardened) {
		return
	}

	e.unknown(RuleFilamentAbrasive, on)
}

// flagCheck fails a tool that lacks a nozzle property the file asks for.
func (e *evaluation) flagCheck(rule Rule, on subject, need, have *bool) {
	if need == nil || !*need {
		return
	}

	switch {
	case have == nil:
		e.unknown(rule, on)
	case !*have:
		e.fail(rule, LevelWarning, on, "", "")
	}
}

// volumeCheck compares the area the objects cover and the top layer with the
// printer's build volume. A file that gives neither is not checked.
func (e *evaluation) volumeCheck(file *storageapi.PrintInfo, volume *Volume) {
	if file.Bounds == nil && file.MaxLayerZMM <= 0 {
		return
	}

	if volume == nil {
		e.unknown(RuleBuildVolume, whole)

		return
	}

	if bounds := file.Bounds; bounds != nil && (bounds.MinXMM < 0 || bounds.MinYMM < 0 ||
		bounds.MaxXMM > volume.XMM+volumeTolerance || bounds.MaxYMM > volume.YMM+volumeTolerance) {
		e.fail(RuleBuildVolume, LevelRefusal, whole,
			millimetres(bounds.MaxXMM)+" by "+millimetres(bounds.MaxYMM),
			millimetres(volume.XMM)+" by "+millimetres(volume.YMM))
	}

	if file.MaxLayerZMM > volume.ZMM+volumeTolerance {
		e.fail(RuleBuildVolume, LevelRefusal, whole, millimetres(file.MaxLayerZMM), millimetres(volume.ZMM))
	}
}

// volumeTolerance is how far past the volume a print may reach and still
// fit, in millimetres: the rounding a slicer writes its figures with.
const volumeTolerance = 0.01

// usedFilaments are the filaments the file uses, by the firmware's used()
// rule: a filament whose grams the file does not give is not used.
func usedFilaments(filaments []storageapi.PrintFilament) []storageapi.PrintFilament {
	var out []storageapi.PrintFilament

	for _, filament := range filaments {
		if filament.Used != nil && *filament.Used && filament.Index >= 0 {
			out = append(out, filament)
		}
	}

	return out
}

// singleFilament is GCodeInfo::is_singletool_gcode: T0 is used and no other
// filament's grams are given at all.
func singleFilament(filaments []storageapi.PrintFilament) bool {
	first := false

	for _, filament := range filaments {
		switch {
		case filament.Index == 0:
			first = filament.Used != nil && *filament.Used
		case filament.Used != nil:
			return false
		}
	}

	return first
}

func millimetres(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64) + " mm"
}

// firmwareSupport is whether a printer's firmware has a check, as far as the
// version it reported says.
type firmwareSupport int

const (
	firmwareLacks firmwareSupport = iota
	firmwareHas
	// firmwareUnknown is a version the printer did not report, or one that
	// cannot be read.
	firmwareUnknown
)

// firmwareSince is whether a firmware version is at least the version that
// brought a check.
func firmwareSince(have, since string) firmwareSupport {
	if _, ok := versionOf(have); !ok {
		return firmwareUnknown
	}

	if upToDate(since, have) {
		return firmwareHas
	}

	return firmwareLacks
}

// upToDate reports whether the firmware version have is at least wanted: the
// major, minor and patch numbers, then the build number after a plus sign. It
// is true when either version cannot be read.
func upToDate(wanted, have string) bool {
	need, ok := versionOf(wanted)
	if !ok {
		return true
	}

	got, ok := versionOf(have)
	if !ok {
		return true
	}

	for i := range 3 {
		if need[i] != got[i] {
			return need[i] < got[i]
		}
	}

	// A printer that reports no build number is not held to one.
	return need[3] == 0 || got[3] == 0 || need[3] <= got[3]
}

// versionOf reads MAJOR.MINOR.PATCH, with an optional +BUILD after it. A
// prerelease label between the two is skipped, as the firmware skips it.
func versionOf(text string) ([4]int, bool) {
	var out [4]int

	text = strings.TrimSpace(text)
	core, build, _ := strings.Cut(text, "+")

	parts := strings.SplitN(core, ".", 3) //nolint:mnd // major, minor and patch.
	if len(parts) != 3 {                  //nolint:mnd // major, minor and patch.
		return out, false
	}

	for i, part := range parts {
		digits := leadingDigits(part)
		if digits == "" {
			return out, false
		}

		value, err := strconv.Atoi(digits)
		if err != nil {
			return out, false
		}

		out[i] = value
	}

	if digits := leadingDigits(build); digits != "" {
		if value, err := strconv.Atoi(digits); err == nil {
			out[3] = value
		}
	}

	return out, true
}

func leadingDigits(text string) string {
	end := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}

	return text[:end]
}
