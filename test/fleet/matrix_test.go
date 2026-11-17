package fleet

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// releasesFile records the shipping firmware releases and which models each was
// published for. It is the firmware axis of the matrix: adding a release is one
// line here and no new test function.
const releasesFile = "releases.json"

// The two sets. Fast is one cell per model at the newest firmware released for
// it and gates every commit; full is every cell and runs nightly.
const (
	setFast = "fast"
	setFull = "full"
)

// matrixSetEnv selects the set. The make targets set it; a bare `go test` runs
// the fast set.
const matrixSetEnv = "PCL_MATRIX"

// update rewrites the committed capability goldens.
var update = flag.Bool("update", false, "rewrite testdata/capabilities.json from this run")

// Cell is one (model, firmware) pair the matrix drives.
type Cell struct {
	// Model is the model as internal/printer/profile spells it.
	Model string
	// Firmware is the version the simulated printer reports.
	Firmware string
	// Band is the profile range the model's profile declares.
	Band profile.Range
	// Source is where the firmware version came from: the profile band's floor,
	// or a shipping release named in releases.json.
	Source string
}

// The two values Cell.Source takes.
const (
	sourceBandFloor = "band-floor"
	sourceReleased  = "released"
)

// String is the cell's name in a test, a log line and a failure.
func (c Cell) String() string { return c.Model + "@" + c.Firmware }

// releases is releases.json.
type releases struct {
	Schema int                 `json:"schema"`
	Source string              `json:"source"`
	Naming map[string]string   `json:"naming"`
	Models map[string][]string `json:"models"`
}

// loadReleases reads the firmware release table.
func loadReleases(t *testing.T) releases {
	t.Helper()

	data, err := os.ReadFile(releasesFile)
	if err != nil {
		t.Fatalf("read %s: %v", releasesFile, err)
	}

	var table releases

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(&table); err != nil {
		t.Fatalf("parse %s: %v", releasesFile, err)
	}

	if table.Schema != 1 {
		t.Fatalf("%s declares schema %d, and this build reads schema 1", releasesFile, table.Schema)
	}

	return table
}

// matrix computes the cells for a set.
//
// Both sets are derived from the same two data files, so a model added to
// profiles.json and a firmware version added to releases.json both reach the
// matrix without a line of Go changing.
func matrix(t *testing.T, set string) []Cell {
	t.Helper()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	table := loadReleases(t)

	var cells []Cell

	for _, model := range registry.Models() {
		band := bandOf(t, registry, model)
		published := table.Models[model]

		if set == setFast {
			cells = append(cells, fastCell(model, band, published))

			continue
		}

		cells = append(cells, Cell{
			Model: model, Firmware: band.From, Band: band, Source: sourceBandFloor,
		})

		for _, firmware := range sortedVersions(published) {
			cells = append(cells, Cell{
				Model: model, Firmware: firmware, Band: band, Source: sourceReleased,
			})
		}
	}

	return cells
}

// fastCell is the one cell per model the fast set runs: the newest firmware
// released for that model, or the band floor when no release names it.
//
// Newest rather than oldest because it is what the field runs. What that misses
// is stated in the package comment: a change that breaks only an older firmware
// of a model whose newest still passes.
func fastCell(model string, band profile.Range, published []string) Cell {
	ordered := sortedVersions(published)
	if len(ordered) == 0 {
		return Cell{Model: model, Firmware: band.From, Band: band, Source: sourceBandFloor}
	}

	return Cell{
		Model:    model,
		Firmware: ordered[len(ordered)-1],
		Band:     band,
		Source:   sourceReleased,
	}
}

// bandOf is the firmware range the model's profile declares.
func bandOf(t *testing.T, registry profile.Registry, model string) profile.Range {
	t.Helper()

	for _, candidate := range registry.Profiles {
		if strings.EqualFold(candidate.Model, model) {
			return candidate.Firmware
		}
	}

	t.Fatalf("%s is in Models() and no profile declares it", model)

	return profile.Range{}
}

// selectedSet is the set this run drives.
func selectedSet(t *testing.T) string {
	t.Helper()

	switch value := os.Getenv(matrixSetEnv); value {
	case "", setFast:
		return setFast
	case setFull:
		return setFull
	default:
		t.Fatalf("%s is %q, and the sets are %q and %q", matrixSetEnv, value, setFast, setFull)

		return ""
	}
}

// sortedVersions orders firmware versions oldest first. Text order is wrong for
// exactly the versions that matter, because "6.9.0" sorts above "6.10.0" as
// text.
//
// The sort is stable, which compareVersions being a total order makes
// unnecessary and which is here anyway: an ordering defect should produce an
// answer that is wrong the same way every run rather than one that depends on
// the order releases.json happens to be written in.
func sortedVersions(versions []string) []string {
	ordered := slices.Clone(versions)
	slices.SortStableFunc(ordered, compareVersions)

	return ordered
}

// compareVersions orders two firmware labels oldest first, and orders every
// label it can be handed.
//
// Precedence is the running product's, read out of it rather than written
// again. profile.Range is the only firmware comparison a printer is ever
// subject to and it follows semver rule 11, so 6.5.0-RC is below 6.5.0 and a
// capability the release added is not offered to a candidate built before it
// existed. A second ordering written here would be a copy of that rule and the
// matrix would select a different firmware from the one the field resolves. See
// TestTheMatrixNeverInventsASecondFirmwareOrdering.
//
// Precedence alone is not an ordering of labels, and this has to be one: it
// decides which cell gates every commit. Two rules sit above it.
//
//  1. a label the resolver cannot read sorts below every label it can. Such a
//     label is in no band, so a cell built from one runs the driver baseline
//     while its name claims a model and a firmware, and fastCell picks the last
//     entry. The bottom is where it can be seen rather than selected
//  2. two labels of one precedence break on text. Build metadata is the case
//     that ships: semver rule 10 excludes "+8628" from precedence, so 6.5.7 and
//     6.5.7+8628 resolve the same profile and the matrix still has to pick one
//     of them as a cell. It picks the same one every run
//
// TestTheMatrixOrderingIsATotalOrder proves both by exhaustion, and
// TestALabelTheResolverCannotReadSortsBelowEveryRelease holds rule 1's
// direction.
func compareVersions(a, b string) int {
	readableA, readableB := readableVersion(a), readableVersion(b)

	switch {
	case !readableA && !readableB:
		return strings.Compare(a, b)
	case !readableA:
		return -1
	case !readableB:
		return 1
	}

	aAboveB, bAboveA := atOrAbove(a, b), atOrAbove(b, a)

	switch {
	case aAboveB && !bAboveA:
		return 1
	case bAboveA && !aAboveB:
		return -1
	}

	return strings.Compare(a, b)
}

// readableVersion reports whether the resolver can place a label at all, asked
// through the same call the ordering uses: a range floored at the label itself
// contains it exactly when the label reads as a version.
func readableVersion(label string) bool { return atOrAbove(label, label) }

// atOrAbove reports whether version is at or above floor, read out of the one
// exported call that applies the product's firmware ordering. A range with no
// ceiling contains everything from its floor upwards, so Contains is exactly
// that question. A label or a floor the resolver cannot read is in no range and
// reports false in both directions.
func atOrAbove(version, floor string) bool {
	return profile.Range{From: floor}.Contains(connectorapi.NewFirmwareLabel(version))
}

// TestTheFirmwareTableNamesEveryModel is what makes adding a model a data
// change. A model added to profiles.json with no entry here would silently run
// on its band floor alone and the matrix would look complete.
func TestTheFirmwareTableNamesEveryModel(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	table := loadReleases(t)
	covered := registry.Models()

	for _, model := range covered {
		if _, named := table.Models[model]; !named {
			t.Errorf("%s is a model the profiles cover and %s does not name it. Add its released "+
				"firmware versions, or an empty list if none is published", model, releasesFile)
		}
	}

	for model := range table.Models {
		if !slices.Contains(covered, model) {
			t.Errorf("%s names %s and no profile covers it", releasesFile, model)
		}
	}
}

// TestEveryReleasedFirmwareFallsInItsModelsBand is the check that a firmware
// version added to the table is one the profile data actually claims. A version
// outside the band would resolve to no profile, and the cell would exercise the
// fallback path while claiming to exercise the model.
//
// The band is asked directly rather than compared against its two bounds,
// because Range.Contains is what a printer dialling in is put through. A
// candidate for the band's ceiling is the case the two spellings disagree on:
// 6.5.0-RC is inside a band ending at 6.5.0 and a bound comparison that reads
// the tag as text puts it above one.
func TestEveryReleasedFirmwareFallsInItsModelsBand(t *testing.T) {
	t.Parallel()

	for _, cell := range matrix(t, setFull) {
		if cell.Source != sourceReleased {
			continue
		}

		if !cell.Band.Contains(connectorapi.NewFirmwareLabel(cell.Firmware)) {
			t.Errorf("%s: firmware %s is outside the profile band [%s, %s), so no profile covers it and "+
				"the cell would exercise the driver baseline while claiming to exercise the model",
				cell, cell.Firmware, cell.Band.From, orOpen(cell.Band.To))
		}
	}
}

// orOpen names an absent ceiling in a failure message.
func orOpen(bound string) string {
	if bound == "" {
		return "no ceiling"
	}

	return bound
}

// TestABandWithACeilingRefusesTheFirmwareAboveIt exercises the half of the band
// check the shipping data cannot reach.
//
// All thirteen profiles in internal/printer/profile/profiles.json declare
// {"from": "6.4.0-0"} and none declares a "to", so every cell
// TestEveryReleasedFirmwareFallsInItsModelsBand builds has an open range: the
// ceiling comparison in Range.Contains never fires and orOpen's other branch is
// dead. That is a fact about the data and it can change with one profile edit,
// so the case is constructed here rather than left unreached.
func TestABandWithACeilingRefusesTheFirmwareAboveIt(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	open := 0

	for _, candidate := range registry.Profiles {
		if candidate.Firmware.To == "" {
			open++
		}
	}

	t.Logf("observed: %d of %d shipping profiles declare no ceiling", open, len(registry.Profiles))

	band := profile.Range{From: "6.4.0-0", To: "6.5.0"}

	cases := []struct {
		firmware string
		inside   bool
	}{
		{firmware: "6.4.0-0", inside: true},
		{firmware: "6.4.2", inside: true},
		{firmware: "6.5.0-RC", inside: true},
		{firmware: "6.5.0", inside: false},
		{firmware: "6.5.7", inside: false},
	}

	for _, testCase := range cases {
		if got := band.Contains(connectorapi.NewFirmwareLabel(testCase.firmware)); got != testCase.inside {
			t.Errorf("[%s, %s) contains %s = %v, want %v",
				band.From, orOpen(band.To), testCase.firmware, got, testCase.inside)
		}
	}

	if got := orOpen(band.To); got != "6.5.0" {
		t.Errorf("a band ending at %s is named %q in a failure", band.To, got)
	}

	if got := orOpen(""); got != "no ceiling" {
		t.Errorf("a band with no ceiling is named %q in a failure", got)
	}
}

// TestTheMatrixIsNotEmpty refuses a run whose fixture produced nothing, and
// prints the cells so a reader of the log knows what was covered.
func TestTheMatrixIsNotEmpty(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	models := len(registry.Models())

	fast := matrix(t, setFast)
	if len(fast) != models {
		t.Errorf("the fast set has %d cells for %d models, and it is one cell per model", len(fast), models)
	}

	full := matrix(t, setFull)
	if len(full) <= len(fast) {
		t.Errorf("the full set has %d cells and the fast set has %d, so the split covers nothing extra",
			len(full), len(fast))
	}

	t.Logf("observed: %d models, %d cells in the fast set, %d cells in the full set",
		models, len(fast), len(full))

	for _, cell := range full {
		t.Logf("cell %s (%s, band from %s)", cell, cell.Source, cell.Band.From)
	}
}

// TestMatrix drives the scenario battery over every cell in the selected set. A
// failure names the model, the firmware and the scenario, which is the answer
// "it connects" never gives.
func TestMatrix(t *testing.T) {
	t.Parallel()

	set := selectedSet(t)
	cells := matrix(t, set)

	if len(cells) == 0 {
		t.Fatal("the matrix produced no cells, so this suite proves nothing")
	}

	t.Logf("driving %d cells in the %s set, %d scenarios each", len(cells), set, len(scenarios))

	for _, cell := range cells {
		t.Run(cell.String(), func(t *testing.T) {
			t.Parallel()

			for _, item := range scenarios {
				t.Run(item.name, func(t *testing.T) {
					t.Parallel()
					item.run(t, cell)
				})
			}
		})
	}
}

// goldenFile is the capability set each cell resolves to, committed.
const goldenFile = "testdata/capabilities.json"

// golden is what one cell resolved to, last time anybody looked.
type golden struct {
	// Digest is connectorapi.Set.Digest of the resolved set.
	Digest string `json:"digest"`
	// Capabilities is how many keys the resolved set holds, so a diff says
	// whether the set grew or shrank without reading the digest.
	Capabilities int `json:"capabilities"`
	// Verbs is how many of those keys are verbs rather than arguments.
	Verbs int `json:"verbs"`
	// ProfileMatched is whether a profile covered the cell's model and
	// firmware.
	ProfileMatched bool `json:"profile_matched"`
}

// goldenFileContent is goldenFile.
type goldenFileContent struct {
	Comment string            `json:"comment"`
	Cells   map[string]golden `json:"cells"`
}

// TestTheCapabilityGoldensAreCurrent is the gate on a profile edit. A change to
// profiles.json or to the driver baseline moves a digest here, and a digest
// that moves is a change to what a supported printer is believed to support.
//
// It runs the full set whatever set the battery is running, because a golden
// covering a third of the matrix would look complete.
func TestTheCapabilityGoldensAreCurrent(t *testing.T) {
	t.Parallel()

	cells := matrix(t, setFull)
	current := make(map[string]golden, len(cells))

	for _, cell := range cells {
		current[cell.String()] = goldenFor(t, cell)
	}

	if *update {
		writeGoldens(t, current)

		return
	}

	committed := readGoldens(t)

	for name, want := range committed {
		got, present := current[name]

		switch {
		case !present:
			t.Errorf("%s is committed in %s and the matrix no longer produces it. A cell that vanished "+
				"is a model or a firmware version that stopped being covered", name, goldenFile)
		case got != want:
			t.Errorf("%s resolves to %+v, and %s says %+v. Rerun with -update and say in the pull "+
				"request why what this printer supports changed", name, got, goldenFile, want)
		}
	}

	for name := range current {
		if _, present := committed[name]; !present {
			t.Errorf("%s is a new cell and %s does not carry it. Rerun with -update", name, goldenFile)
		}
	}

	if len(committed) == 0 {
		t.Fatalf("%s carries no cells, so this gate compared nothing", goldenFile)
	}
}

func readGoldens(t *testing.T) map[string]golden {
	t.Helper()

	data, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("read %s: %v. Generate it with `go test ./test/fleet -run TestTheCapabilityGoldens "+
			"-update`", goldenFile, err)
	}

	var file goldenFileContent
	if err = json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse %s: %v", goldenFile, err)
	}

	return file.Cells
}

func writeGoldens(t *testing.T, cells map[string]golden) {
	t.Helper()

	content := goldenFileContent{
		Comment: "The capability set each cell of the full matrix resolves to. Regenerate with " +
			"`go test ./test/fleet -run TestTheCapabilityGoldensAreCurrent -update`. A diff here is a " +
			"change to what a supported printer is believed to support and needs a reason in the pull request.",
		Cells: cells,
	}

	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatalf("encode the goldens: %v", err)
	}

	if err = os.MkdirAll("testdata", 0o750); err != nil {
		t.Fatalf("create testdata: %v", err)
	}

	if err = os.WriteFile(goldenFile, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write %s: %v", goldenFile, err)
	}

	fmt.Fprintf(os.Stdout, "wrote %s with %d cells\n", goldenFile, len(cells))
}
