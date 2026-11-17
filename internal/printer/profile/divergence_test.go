package profile_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// update rewrites profiles.json from the recorded matrix. The file is generated,
// never edited:
//
//	go test ./internal/printer/profile -run TestProfilesMatchTheRetiredMatrix -update
var update = flag.Bool("update", false, "rewrite profiles.json from the recorded firmware matrix")

const (
	featurePrefix = "core.feature."
	profilesFile  = "profiles.json"

	// matrixFile is the firmware capability matrix of the retired connector's
	// command builder, recorded by asking that builder. Its provenance key says
	// so, and extract refuses a copy that has lost it.
	matrixFile = "testdata/retired-capability-matrix.json"

	// firstCandidate is semver's lowest prerelease tag, and the suffix that turns
	// a bound naming a release into one that admits its candidates too.
	firstCandidate = "-0"
)

// dump is the shape of the recorded matrix. It is a transport shape, not a
// model: the only thing that reads it is the conversion below.
type dump struct {
	// Provenance says where the rows came from. JSON carries no comments and
	// this file is what a reader opens, so a sentence inside it is the only
	// place the answer can live, and extract fails on an empty one.
	Provenance         string              `json:"provenance"`
	Models             []dumpModel         `json:"models"`
	SupportedFirmware  map[string]bool     `json:"supported_firmware"`
	FeaturesByFirmware map[string][]string `json:"features_by_firmware"`
}

type dumpModel struct {
	Model                string        `json:"model"`
	DeviceTypeID         string        `json:"device_type_id"`
	FirmwareDeviceTypeID string        `json:"firmware_device_type_id"`
	Features             []string      `json:"features"`
	Commands             []dumpCommand `json:"commands"`
}

type dumpCommand struct {
	Name             string         `json:"name"`
	Args             []dumpArgument `json:"args,omitempty"`
	RequiresFeatures []string       `json:"requires_features,omitempty"`
	FromVersion      string         `json:"from_version,omitempty"`
	ToVersion        string         `json:"to_version,omitempty"`
}

type dumpArgument struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// The gate this whole package exists for. profiles.json is a second copy of the
// firmware capability matrix, in the descriptor shape rather than the matrix's
// own. A second copy that nothing checks is a fork, and a fifty-row matrix
// diverges within a month, so this converts the recorded matrix and fails when
// the two answers differ.
//
// The two sides stay two sides. testdata/retired-capability-matrix.json holds
// models, features and commands with their arguments; profiles.json holds
// capability keys. Between them sit encode, floorOf, capabilitiesOf,
// profile.HostMinted and profile.Validate, and every one of those is what this
// runs. An edit to either file, or to any step between them, fails here.
func TestProfilesMatchTheRetiredMatrix(t *testing.T) {
	// The file says how it is rewritten, and this is what holds that sentence to
	// the test that rewrites it. A reader opens JSON, which carries no comments,
	// so generated_by is the only place the answer can live; a published command
	// naming a test that has been renamed is worse than none, because somebody
	// runs it and reads "no tests to run" as "nothing to regenerate".
	if want := regenerateCommand(t.Name()); profile.GeneratedBy != want {
		t.Fatalf("the profile file publishes %q as the command that rewrites it, and this test is run by "+
			"%q", profile.GeneratedBy, want)
	}

	extracted := extract(t)
	generated := encode(t, extracted)

	path := filepath.Join(repoRoot(t), "internal", "printer", "profile", profilesFile)

	if *update {
		if err := os.WriteFile(path, generated, 0o600); err != nil {
			t.Fatalf("writing %s: %v", profilesFile, err)
		}

		t.Logf("rewrote %s from %s", profilesFile, matrixFile)
	}

	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", profilesFile, err)
	}

	if !bytes.Equal(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n")), generated) {
		t.Errorf("%s and the recorded firmware matrix disagree.\n"+
			"Regenerate with: %s\n"+
			"%s", profilesFile, regenerateCommand(t.Name()), firstDifference(committed, generated))
	}
}

// regenerateCommand is the one spelling of how the file is rewritten. The
// failure above and the generated_by key in the file are the same sentence, so
// they cannot say two different things.
func regenerateCommand(test string) string {
	return "go test ./internal/printer/profile -run " + test + " -update"
}

// The committed file has to survive its own loader, or the divergence gate is
// green against a profile the binary refuses at startup.
func TestTheCommittedProfilesLoadAndValidate(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	if len(registry.Profiles) == 0 {
		t.Fatal("the built-in profile file is empty")
	}

	// The sentence survives the parse and the per-caller copy. A registry handed
	// to a caller with the field dropped is a file that says how it is generated
	// to nothing that reads it.
	if registry.GeneratedBy != profile.GeneratedBy {
		t.Errorf("the built-in registry says it was generated by %q, want %q",
			registry.GeneratedBy, profile.GeneratedBy)
	}
}

// A profile the loader drops is a capability set the binary never offers, while
// the file on disk still says it does. So the committed bytes are read again
// here without the loader, and every model, code, firmware bound and capability
// key in them has to come back out of Builtin, in the order the file writes
// them.
//
// Key sets are compared and values are not. bounds.json is folded in at load
// time and narrows a value, which is the one difference a caller sees between
// this file and what it receives.
func TestTheLoaderCarriesEveryProfileTheFileHolds(t *testing.T) {
	t.Parallel()

	onDisk := profilesOnDisk(t)

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	if registry.Schema != onDisk.Schema {
		t.Errorf("%s declares schema %d and the loader returns %d",
			profilesFile, onDisk.Schema, registry.Schema)
	}

	if len(registry.Profiles) != len(onDisk.Profiles) {
		t.Fatalf("%s holds %d profiles and the loader returns %d",
			profilesFile, len(onDisk.Profiles), len(registry.Profiles))
	}

	for i, want := range onDisk.Profiles {
		got := registry.Profiles[i]

		if got.Model != want.Model ||
			got.DeviceTypeID != want.DeviceTypeID ||
			got.FirmwareDeviceTypeID != want.FirmwareDeviceTypeID {
			t.Errorf("profile %d: the file holds %s %s/%s and the loader returns %s %s/%s",
				i, want.Model, want.DeviceTypeID, want.FirmwareDeviceTypeID,
				got.Model, got.DeviceTypeID, got.FirmwareDeviceTypeID)

			continue
		}

		if got.Firmware.From != want.Firmware.From || got.Firmware.To != want.Firmware.To {
			t.Errorf("%s: the file holds firmware %s to %q and the loader returns %s to %q",
				want.Model, want.Firmware.From, want.Firmware.To,
				got.Firmware.From, got.Firmware.To)
		}

		held := slices.Sorted(maps.Keys(want.Capabilities))

		loaded := make([]string, 0, len(got.Capabilities))
		for _, key := range got.Capabilities.Keys() {
			loaded = append(loaded, key.String())
		}

		slices.Sort(loaded)

		if !slices.Equal(loaded, held) {
			t.Errorf("%s: the file holds %d capability keys and the loader returns %d.\n\t%s",
				want.Model, len(held), len(loaded), firstKeyDifference(held, loaded))
		}
	}
}

// diskRegistry is profiles.json read without the loader, so that the loader is
// never the thing that says what the file holds.
type diskRegistry struct {
	Schema   int `json:"schema"`
	Profiles []struct {
		Model                string `json:"model"`
		DeviceTypeID         string `json:"device_type_id"`
		FirmwareDeviceTypeID string `json:"firmware_device_type_id"`
		Firmware             struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"firmware"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	} `json:"profiles"`
}

func profilesOnDisk(t *testing.T) diskRegistry {
	t.Helper()

	path := filepath.Join(repoRoot(t), "internal", "printer", "profile", profilesFile)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", profilesFile, err)
	}

	var onDisk diskRegistry
	if decodeErr := json.Unmarshal(content, &onDisk); decodeErr != nil {
		t.Fatalf("decoding %s: %v", profilesFile, decodeErr)
	}

	if len(onDisk.Profiles) == 0 {
		t.Fatalf("%s holds no profile, so this test compares nothing", profilesFile)
	}

	return onDisk
}

// firstKeyDifference names the first key one side has and the other does not, so
// a 400-key set reports as one name rather than as two lists.
func firstKeyDifference(held, loaded []string) string {
	for _, key := range held {
		if !slices.Contains(loaded, key) {
			return "the file holds " + key + " and the loader does not"
		}
	}

	for _, key := range loaded {
		if !slices.Contains(held, key) {
			return "the loader returns " + key + " and the file does not"
		}
	}

	return "the two sides hold the same keys in a different count"
}

// The websocket floor, extracted rather than transcribed: whatever version the
// recorded matrix starts answering at is the version a profile starts matching
// at.
func TestTheFirmwareFloorMatchesTheRetiredMatrix(t *testing.T) {
	extracted := extract(t)

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	served, refused := 0, 0

	for version, supported := range extracted.SupportedFirmware {
		_, matched := registry.Match("MK4", connectorapi.NewFirmwareLabel(version))
		if matched != supported {
			t.Errorf("firmware %s: the recorded matrix serves it = %v, a profile matches it = %v",
				version, supported, matched)
		}

		if supported {
			served++
		} else {
			refused++
		}
	}

	// Both sides of the floor, or this watches nothing. Its two siblings carry
	// the same guard and this one did not: deleting the extractor's firmware
	// probe left an empty map, every comparison in the loop unreached, and both
	// this gate and the byte-diff gate green over the floor the whole profile
	// range hangs on.
	if served == 0 || refused == 0 {
		t.Errorf("the recorded matrix straddles no floor: %d versions served, %d refused. "+
			"This test compares nothing until it sees at least one of each", served, refused)
	}
}

// Every model this binary ships carries one open-ended firmware band, so the
// profile format cannot express a difference between two firmware versions. That
// is right only while there is no difference to express, and this is the
// measurement rather than the assumption.
//
// Two things in the shipping matrix could record one and neither does. No command
// declares a ToVersion and every FromVersion is the same floor, which floorOf
// refuses to flatten if it changes; and the firmware feature delta registry,
// which exists to say "this version adds this feature", is empty, so every
// version answers with the same feature set. A firmware difference nobody has
// recorded is not one this repository can invent: a band written from a guess
// ships as a fact, and the intersection would then subtract a capability a
// printer really has.
//
// When this fails, the answer is a second band extracted the same way the first
// was, and a printer on the older firmware stops being offered what the newer one
// added.
func TestNoNewerFirmwareChangesAnythingTheMatrixRecords(t *testing.T) {
	extracted := extract(t)

	var floor []string

	for _, version := range slices.Sorted(maps.Keys(extracted.FeaturesByFirmware)) {
		if !extracted.SupportedFirmware[version] {
			continue
		}

		features := extracted.FeaturesByFirmware[version]

		if floor == nil {
			floor = features

			continue
		}

		if !slices.Equal(features, floor) {
			t.Errorf("firmware %s has features %v and the floor has %v. The matrix now records a "+
				"difference between two firmware versions, and the profile carries one band per model, "+
				"so this needs a band per range before it can be extracted", version, features, floor)
		}
	}

	if floor == nil {
		t.Fatal("no probed firmware version yields a feature set, so this test compares nothing")
	}
}

// Models that share a firmware image must keep saying so, because a profile
// keyed on the reporting model would otherwise need one row per rebadge and
// would drift row by row.
func TestSharedFirmwareModelsKeepTheirFirmwareDeviceType(t *testing.T) {
	extracted := extract(t)

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	shared := 0

	for _, model := range extracted.Models {
		matched, ok := registry.Match(model.Model, connectorapi.NewFirmwareLabel("6.4.0"))
		if !ok {
			t.Errorf("%s: no profile matches at the firmware floor", model.Model)

			continue
		}

		if matched.DeviceTypeID != model.DeviceTypeID ||
			matched.FirmwareDeviceTypeID != model.FirmwareDeviceTypeID {
			t.Errorf("%s: profile says %s/%s, the shipping matrix says %s/%s",
				model.Model, matched.DeviceTypeID, matched.FirmwareDeviceTypeID,
				model.DeviceTypeID, model.FirmwareDeviceTypeID)
		}

		if model.DeviceTypeID != model.FirmwareDeviceTypeID {
			shared++
		}
	}

	if shared == 0 {
		t.Error("no model reports a firmware device type different from its own, " +
			"so this test is no longer watching the shared-firmware rule")
	}
}

// A command the shipping matrix gates on a feature must only appear where that
// feature does. This re-checks the filtering from the other side, so a change
// that drops the feature gate shows up here rather than as an extra verb.
func TestFeatureGatedCommandsOnlyAppearWhereTheFeatureDoes(t *testing.T) {
	extracted := extract(t)

	gated := 0

	for _, model := range extracted.Models {
		for _, command := range model.Commands {
			for _, feature := range command.RequiresFeatures {
				gated++

				if !slices.Contains(model.Features, feature) {
					t.Errorf("%s offers %s, which needs feature %s that the model does not have",
						model.Model, command.Name, feature)
				}
			}
		}
	}

	if gated == 0 {
		t.Error("no command in the matrix is gated on a feature, so this test watches nothing")
	}
}

// extract reads the recorded matrix. Every guard below refuses a file that
// parses and says nothing, because this is the authority profiles.json is held
// against: a matrix with no model, or with no firmware version, leaves every
// gate that reads it green over an empty map.
func extract(t *testing.T) dump {
	t.Helper()

	content, err := os.ReadFile(matrixFile)
	if err != nil {
		t.Fatalf("reading %s: %v", matrixFile, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()

	var extracted dump
	if decodeErr := decoder.Decode(&extracted); decodeErr != nil {
		t.Fatalf("decoding %s: %v", matrixFile, decodeErr)
	}

	if extracted.Provenance == "" {
		t.Fatalf("%s names no source. A file a reader opens and cannot place is one somebody "+
			"edits by hand", matrixFile)
	}

	if len(extracted.Models) == 0 {
		t.Fatalf("%s holds no model, which means this gate is reading nothing", matrixFile)
	}

	if len(extracted.SupportedFirmware) == 0 {
		t.Fatalf("%s records no firmware version, so the floor every profile range starts at is "+
			"read from nothing and every gate that reads it is green over an empty map", matrixFile)
	}

	return extracted
}

// encode converts the dump into the profile format and serialises it exactly as
// the committed file is written.
func encode(t *testing.T, extracted dump) []byte {
	t.Helper()

	registry := profile.Registry{Schema: profile.CurrentSchema, GeneratedBy: profile.GeneratedBy}
	floor := floorOf(t, extracted)

	for _, model := range extracted.Models {
		registry.Profiles = append(registry.Profiles, profile.Profile{
			Model:                model.Model,
			DeviceTypeID:         model.DeviceTypeID,
			FirmwareDeviceTypeID: model.FirmwareDeviceTypeID,
			Firmware:             profile.Range{From: floor},
			Capabilities:         capabilitiesOf(t, model),
		})
	}

	if err := profile.Validate(registry); err != nil {
		t.Fatalf("the extracted profiles do not satisfy the profile rules: %v", err)
	}

	encoded, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatalf("encoding profiles: %v", err)
	}

	return append(encoded, '\n')
}

// floorOf returns the lowest firmware the recorded matrix serves, and refuses
// to flatten a matrix that has grown a second band.
//
// The bound it returns is the first candidate for that version rather than the
// version itself, whenever the dump observed the builder serving one. The
// shipping comparator strips a prerelease tag before comparing, so it serves
// "6.4.0-RC"; internal/printer/profile orders a candidate below its release, so
// a bound of "6.4.0" would leave a beta printer unmatched. "-0" is how semver
// spells a bound that admits every candidate for its own version.
func floorOf(t *testing.T, extracted dump) string {
	t.Helper()

	var bands []string

	for _, model := range extracted.Models {
		for _, command := range model.Commands {
			if command.ToVersion != "" {
				t.Fatalf("%s %s is withdrawn at %s. The profile format carries one firmware range per "+
					"model, so a withdrawal needs a second band before it can be extracted",
					model.Model, command.Name, command.ToVersion)
			}

			if command.FromVersion != "" && !slices.Contains(bands, command.FromVersion) {
				bands = append(bands, command.FromVersion)
			}
		}
	}

	if len(bands) != 1 {
		t.Fatalf("the matrix now has %d firmware bands (%v). The profile format carries one range per "+
			"model, so this needs a band per range before it can be extracted", len(bands), bands)
	}

	if extracted.SupportedFirmware[bands[0]+firstCandidate] {
		return bands[0] + firstCandidate
	}

	return bands[0]
}

// capabilitiesOf turns one model's row into a descriptor.
func capabilitiesOf(t *testing.T, model dumpModel) connectorapi.Set {
	t.Helper()

	set := make(connectorapi.Set, len(model.Commands)+len(model.Features))

	// The keys the matrix has no row for, written in from one declared list so
	// that the profile tier does not subtract a verb no profile was ever going
	// to mention (ADR-20260825-z65 rule 18).
	maps.Copy(set, profile.HostMinted(model.Model))

	for _, feature := range model.Features {
		set[featureKey(feature)] = connectorapi.Value{Flag: true}
	}

	for _, command := range model.Commands {
		verb := verbKey(command.Name)
		set[verb] = connectorapi.Value{Flag: true}

		for _, arg := range command.Args {
			kind, ok := kindOf(arg.Type)
			if !ok {
				t.Fatalf("%s %s: argument %q declares type %q, which the descriptor vocabulary "+
					"does not define", model.Model, command.Name, arg.Name, arg.Type)
			}

			set[connectorapi.ArgumentKey(verb, arg.Name)] = connectorapi.Value{
				Flag:     true,
				Kind:     kind,
				Required: arg.Required,
			}
		}
	}

	return set
}

// verbKey is the published verb one recorded command maps to. The conversion
// is the profile package's own, because the driver's bound comparison converts
// the same names and the two have to agree.
func verbKey(name string) connectorapi.Capability {
	return profile.VerbForShippingCommand(name)
}

func featureKey(name string) connectorapi.Capability {
	return connectorapi.Capability(featurePrefix + strings.ToLower(name))
}

// kindOf maps the shipping argument types onto descriptor kinds. A structured
// argument becomes opaque: the host passes it through and the driver reads it,
// because the descriptor constrains scalars and nothing else.
func kindOf(argumentType string) (connectorapi.Kind, bool) {
	switch argumentType {
	case "string":
		return connectorapi.KindString, true
	case "integer":
		return connectorapi.KindInteger, true
	case "float":
		return connectorapi.KindNumber, true
	case "boolean":
		return connectorapi.KindBoolean, true
	case "object", "array":
		return connectorapi.KindOpaque, true
	default:
		return connectorapi.KindFlag, false
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}

		dir = parent
	}
}

// firstDifference names the first line that differs, so a 6,000-line diff
// reports as one line rather than as a wall.
func firstDifference(committed, generated []byte) string {
	left := strings.Split(strings.ReplaceAll(string(committed), "\r\n", "\n"), "\n")
	right := strings.Split(string(generated), "\n")

	for i := range max(len(left), len(right)) {
		got, want := lineAt(left, i), lineAt(right, i)
		if got != want {
			return fmt.Sprintf("first difference at line %d:\n\tcommitted: %s\n\textracted: %s", i+1, got, want)
		}
	}

	return "the files differ in trailing bytes only"
}

func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "(end of file)"
	}

	return lines[i]
}
