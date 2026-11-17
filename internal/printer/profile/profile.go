// Package profile is the data tier of the capability model: what a given
// printer model running a given firmware range is known to expose.
//
// It is the middle of three tiers: the driver's Baseline, this data, and the
// device's own probe. A profile can only narrow what a driver's Baseline
// declares, so a stale or wrong file produces a missing capability and never
// one the printer does not have. A profile subtracts a key by leaving it out.
// It has no way to spell "off", so no entry can be read as adding something.
//
// profiles.json is not written by hand. It is generated from the recorded
// firmware matrix in testdata/retired-capability-matrix.json by the divergence
// test, which fails when the two disagree. See divergence_test.go.
package profile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// CurrentSchema is the profile format this build reads. A profile written for
// a later format is refused.
const CurrentSchema = 1

// GeneratedBy is the command that rewrites profiles.json from the recorded
// firmware matrix, and the value the embedded file records under generated_by.
//
// JSON has no comments, so the file names its generator in a field. Without
// the field the file reads as a list somebody maintains by hand.
// TestProfilesMatchTheRetiredMatrix holds the constant to the test's own name,
// so a renamed test cannot leave a file that names a command that is gone.
const GeneratedBy = "go test ./internal/printer/profile -run TestProfilesMatchTheRetiredMatrix -update"

//go:embed reserved.json
var embeddedReserved []byte

// Registry is a set of profiles, keyed at lookup time by model and firmware.
type Registry struct {
	Schema int `json:"schema"`
	// GeneratedBy names the command that rewrote this file, and is empty for a
	// file somebody wrote. It is not validated and nothing branches on it: a
	// deployment's own profile file is written by hand and says so by leaving it
	// out. The embedded file carries GeneratedBy.
	GeneratedBy string    `json:"generated_by,omitempty"`
	Profiles    []Profile `json:"profiles"`
	// Aliases are the model spellings a printer reports that the rules in
	// Canonical do not reach. The embedded registry takes them from aliases.json,
	// which is written by hand; a deployment's own profile file carries them in
	// this key.
	Aliases []Alias `json:"aliases,omitempty"`
	// Bounds are the per-model numeric ranges the shipping matrix does not
	// record. The embedded registry takes them from bounds.json, which is
	// written by hand; a deployment's own profile file carries them in this key.
	// They are folded into the profiles at load time and only ever narrow.
	Bounds []Bound `json:"bounds,omitempty"`
}

// Profile is what one model running one firmware range exposes.
type Profile struct {
	// Model is the model name a printer reports, matched without regard to case
	// because firmware and gcode metadata disagree about it.
	Model string `json:"model"`
	// DeviceTypeID is the code this model reports.
	DeviceTypeID string `json:"device_type_id"`
	// FirmwareDeviceTypeID is the code whose firmware build this model runs. It
	// differs from DeviceTypeID for the models that share a firmware image.
	FirmwareDeviceTypeID string `json:"firmware_device_type_id"`
	// Firmware is the range this profile applies to.
	Firmware Range `json:"firmware"`
	// Capabilities is the descriptor this tier contributes.
	Capabilities connectorapi.Set `json:"capabilities"`
}

// Range is a firmware version range: closed at the bottom, open at the top.
//
// The upper bound is open, so a firmware version bump still matches. A match
// on an exact version would stop matching without any notice, and the printer
// would fall back with nothing to say why.
//
// A bound with a prerelease tag admits the release candidates of its own
// version. The floor "6.4.0-0" admits a 6.4.0 beta, and a floor of "6.4.0"
// would leave a beta printer with no profile.
type Range struct {
	// From is the lowest firmware this profile applies to, inclusive.
	From string `json:"from"`
	// To is the first firmware outside the range, exclusive. Empty means the
	// range has no upper bound. A candidate for that version is below it and
	// is still in the range.
	To string `json:"to,omitempty"`
}

// Contains reports whether a reported firmware falls in the range.
//
// A label that does not parse as a version is in no range. Ordering it as text
// would put every label that starts with a letter above every numeric version.
// That would select a profile for a device nothing is known about.
func (r Range) Contains(firmware connectorapi.FirmwareLabel) bool {
	reported, ok := parseVersion(firmware.String())
	if !ok {
		return false
	}

	from, ok := parseVersion(r.From)
	if !ok {
		return false
	}

	if reported.compare(from) < 0 {
		return false
	}

	if r.To == "" {
		return true
	}

	to, ok := parseVersion(r.To)
	if !ok {
		return false
	}

	return reported.compare(to) < 0
}

// parsed is the embedded file read once per process.
//
// Decoding and validating the file is costly, and the result does not depend
// on the caller. The driver builds a resolver from it, the simulator projects
// a catalog from it and the capability vocabulary answers from it.
//
// A failure is remembered like a success. If the first caller got an error
// and the next a registry, the coverage a printer receives would depend on
// the order the drivers started in.
var parsed = sync.OnceValues(loadBuiltin)

// Builtin returns the profiles compiled into this binary.
//
// Each caller gets its own registry. A Registry carries a slice of profiles
// and a capability map inside each of them, so every holder of a shared value
// could write through it. A resolver that narrowed a set in place would narrow
// it for every other holder. The copy is much cheaper than the parse it
// replaces. See BenchmarkBuiltin and BenchmarkLoad.
func Builtin() (Registry, error) {
	registry, err := parsed()
	if err != nil {
		return Registry{}, err
	}

	return registry.clone(), nil
}

// clone deep-copies everything a caller can reach and write: the profile slice,
// each profile's capability map, and the supported values inside it. Every other
// field is a string, an int or a bool.
func (r Registry) clone() Registry {
	out := Registry{
		Schema:      r.Schema,
		GeneratedBy: r.GeneratedBy,
		Profiles:    slices.Clone(r.Profiles),
		Aliases:     slices.Clone(r.Aliases),
		Bounds:      slices.Clone(r.Bounds),
	}
	for i := range out.Profiles {
		out.Profiles[i].Capabilities = out.Profiles[i].Capabilities.Clone()
	}

	for i := range out.Bounds {
		out.Bounds[i] = out.Bounds[i].clone()
	}

	return out
}

// Load parses and validates a profile file. A file that does not validate is
// refused whole, because a half-read profile is a capability set nobody wrote.
func Load(data []byte) (Registry, error) {
	var registry Registry

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&registry); err != nil {
		return Registry{}, fmt.Errorf("parse profiles: %w", err)
	}

	if err := Validate(registry); err != nil {
		return Registry{}, err
	}

	// A file that carries its own bound rows has them folded in here, so that a
	// deployment's profiles reach a caller bounded the same way the built-in
	// ones do.
	registry.applyBounds()

	return registry, nil
}

// Match returns the profile for a reported model and firmware. ok is false when
// nothing matches. The caller then resolves without a profile and logs a
// warning. It does not refuse the connection.
//
// The model reaches the file through Canonical, so a printer is matched on what
// it is and not on how its firmware spelled it.
func (r Registry) Match(model string, firmware connectorapi.FirmwareLabel) (Profile, bool) {
	canonical, ok := r.Canonical(model)
	if !ok {
		return Profile{}, false
	}

	for _, candidate := range r.Profiles {
		if !strings.EqualFold(candidate.Model, canonical) {
			continue
		}

		if candidate.Firmware.Contains(firmware) {
			return candidate, true
		}
	}

	return Profile{}, false
}

// Models returns every model any profile covers, in file order.
func (r Registry) Models() []string {
	seen := make(map[string]bool, len(r.Profiles))

	var models []string

	for _, candidate := range r.Profiles {
		if seen[candidate.Model] {
			continue
		}

		seen[candidate.Model] = true

		models = append(models, candidate.Model)
	}

	return models
}
