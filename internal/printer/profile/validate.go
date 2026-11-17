package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The ways a profile file is refused. Callers inspect them with errors.Is.
var (
	// ErrSchema reports a file written for a format this build does not read.
	ErrSchema = errors.New("profile: unsupported schema")
	// ErrInvalidProfile reports a profile whose contents cannot be acted on.
	ErrInvalidProfile = errors.New("profile: invalid")
	// ErrReservedKey reports a capability key that was retired. A retired name
	// is never reissued: a key whose meaning changed gets a new name, and the
	// old one stays refused so a reader cannot silently get the new meaning
	// under the old name.
	ErrReservedKey = errors.New("profile: reserved capability key")
	// ErrOverlappingRange reports two profiles for one model whose firmware
	// ranges overlap, which would make the match order-dependent.
	ErrOverlappingRange = errors.New("profile: overlapping firmware ranges")
	// ErrAmbiguousSpelling reports one model spelling that reaches two models.
	// Matching would then answer whichever the file lists first, and a printer
	// would be offered another machine's capability set.
	ErrAmbiguousSpelling = errors.New("profile: one spelling names two models")
	// ErrUnknownAliasModel reports an alias row naming a model the file does not
	// carry.
	ErrUnknownAliasModel = errors.New("profile: alias names a model no profile covers")
	// ErrInvalidBound reports a numeric bound row that cannot be acted on.
	ErrInvalidBound = errors.New("profile: invalid argument bound")
	// ErrUnknownBoundModel reports a bound row naming a model the file does not
	// carry.
	ErrUnknownBoundModel = errors.New("profile: bound names a model no profile covers")
)

// reservedFile is the shape of reserved.json.
type reservedFile struct {
	Capabilities []connectorapi.Capability `json:"capabilities"`
}

// Reserved returns the capability keys that were retired and may never be used
// again. It panics on a malformed embedded file, which only a broken build
// can contain.
func Reserved() []connectorapi.Capability {
	var file reservedFile

	if err := json.Unmarshal(embeddedReserved, &file); err != nil {
		panic("profile: embedded reserved.json is malformed: " + err.Error())
	}

	return file.Capabilities
}

// Validate refuses a registry that cannot be acted on. Without this check a
// wrong file could not be told from a right one.
func Validate(registry Registry) error {
	return ValidateWithReserved(registry, Reserved())
}

// ValidateWithReserved is Validate against a caller-supplied retired-key list,
// so a test can run the check against a key that is retired.
func ValidateWithReserved(registry Registry, reserved []connectorapi.Capability) error {
	if registry.Schema != CurrentSchema {
		return fmt.Errorf("%w: file declares %d, this build reads %d",
			ErrSchema, registry.Schema, CurrentSchema)
	}

	var problems []error

	for _, candidate := range registry.Profiles {
		problems = append(problems, profileProblems(candidate, reserved)...)
	}

	problems = append(problems, overlapProblems(registry)...)
	problems = append(problems, spellingProblems(registry)...)
	problems = append(problems, boundProblems(registry)...)

	return errors.Join(problems...)
}

// spellingProblems refuses a file in which one reported spelling could reach two
// models. Canonical answers with the first profile it finds, so an ambiguity
// here is a printer holding another machine's capability set, decided by file
// order.
//
// The two key spaces are the two Canonical uses. A model name is compared with
// the separators dropped, and a device type code is compared as it is written.
func spellingProblems(registry Registry) []error {
	var problems []error

	byName := map[string]string{}
	byCode := map[string]string{}

	for _, candidate := range registry.Profiles {
		problems = append(problems, claim(byName, normaliseModel(candidate.Model),
			candidate.Model, "model "+candidate.Model)...)
	}

	for _, candidate := range registry.Profiles {
		problems = append(problems, claim(byCode, strings.ToLower(strings.TrimSpace(candidate.DeviceTypeID)),
			candidate.Model, "device type "+candidate.DeviceTypeID)...)
	}

	return append(problems, aliasProblems(registry, byName, byCode)...)
}

// claim files one spelling under the model that answers to it, and reports the
// second model to ask for a spelling somebody else holds.
func claim(owner map[string]string, spelling, model, kind string) []error {
	if spelling == "" {
		return nil
	}

	if held, taken := owner[spelling]; taken && !strings.EqualFold(held, model) {
		return []error{fmt.Errorf("%w: %s reaches %s and %s", ErrAmbiguousSpelling, kind, held, model)}
	}

	owner[spelling] = model

	return nil
}

// aliasProblems refuses an alias row that names nothing, that names a model the
// file does not carry, or that takes a spelling the profile data already
// answers to. A row the data shadows never matches, and the person who adds
// one must be told.
func aliasProblems(registry Registry, byName, byCode map[string]string) []error {
	var problems []error

	for _, alias := range registry.Aliases {
		spelling := normaliseModel(alias.Spelling)
		if spelling == "" {
			problems = append(problems, fmt.Errorf("%w: an alias for %q has no spelling",
				ErrInvalidProfile, alias.Model))

			continue
		}

		if alias.Source == "" {
			problems = append(problems, fmt.Errorf(
				"%w: alias %q names no source. A spelling nobody observed is a guess",
				ErrInvalidProfile, alias.Spelling))
		}

		if !carries(registry, alias.Model) {
			problems = append(problems, fmt.Errorf("%w: %q names %q",
				ErrUnknownAliasModel, alias.Spelling, alias.Model))

			continue
		}

		code := strings.ToLower(strings.TrimSpace(alias.Spelling))
		if held, taken := byCode[code]; taken && !strings.EqualFold(held, alias.Model) {
			problems = append(problems, fmt.Errorf("%w: alias %q is the device type of %s and names %s",
				ErrAmbiguousSpelling, alias.Spelling, held, alias.Model))

			continue
		}

		problems = append(problems, claim(byName, spelling, alias.Model, "alias "+alias.Spelling)...)
	}

	return problems
}

func carries(registry Registry, model string) bool {
	for _, candidate := range registry.Profiles {
		if strings.EqualFold(candidate.Model, model) {
			return true
		}
	}

	return false
}

func profileProblems(candidate Profile, reserved []connectorapi.Capability) []error {
	var problems []error

	if candidate.Model == "" {
		problems = append(problems, fmt.Errorf("%w: a profile has no model", ErrInvalidProfile))
	}

	problems = append(problems, rangeProblems(candidate)...)

	// The descriptor rules the SDK states once, applied to this tier. A profile
	// is a capability set a caller is held to exactly as a driver's baseline is,
	// so it answers to the same rules: a default its own limit refuses, an
	// argument of a verb the file does not declare, a required argument with a
	// default that makes the requirement unenforceable.
	for _, problem := range candidate.Capabilities.Problems() {
		problems = append(problems, fmt.Errorf("%w: %s %w", ErrInvalidProfile, candidate.Model, problem))
	}

	for _, key := range candidate.Capabilities.Keys() {
		problems = append(problems, keyProblems(candidate, key, reserved)...)
	}

	return problems
}

// rangeProblems refuses a firmware range whose own bounds are not versions. A
// bound nothing can read matches nothing, so the profile would cover no
// printer and nothing would report it.
func rangeProblems(candidate Profile) []error {
	var problems []error

	switch {
	case candidate.Firmware.From == "":
		problems = append(problems, fmt.Errorf("%w: %s has no lower firmware bound",
			ErrInvalidProfile, candidate.Model))
	case !isVersion(candidate.Firmware.From):
		problems = append(problems, fmt.Errorf("%w: %s starts at %q, which is not a firmware version",
			ErrInvalidProfile, candidate.Model, candidate.Firmware.From))
	}

	if candidate.Firmware.To == "" {
		return problems
	}

	if !isVersion(candidate.Firmware.To) {
		return append(problems, fmt.Errorf("%w: %s ends at %q, which is not a firmware version",
			ErrInvalidProfile, candidate.Model, candidate.Firmware.To))
	}

	if order, ok := compareBounds(candidate.Firmware.From, candidate.Firmware.To); ok && order >= 0 {
		problems = append(problems, fmt.Errorf("%w: %s covers %s to %s, which is empty",
			ErrInvalidProfile, candidate.Model, candidate.Firmware.From, candidate.Firmware.To))
	}

	return problems
}

func keyProblems(candidate Profile, key connectorapi.Capability, reserved []connectorapi.Capability) []error {
	var problems []error

	value := candidate.Capabilities[key]
	where := candidate.Model + " " + key.String()

	if slices.Contains(reserved, key) {
		problems = append(problems, fmt.Errorf("%w: %s", ErrReservedKey, where))
	}

	if !value.Flag {
		problems = append(problems, fmt.Errorf(
			"%w: %s is present with its flag off. A profile subtracts by leaving a key out, "+
				"so that no profile can ever be read as an instruction to add", ErrInvalidProfile, where))
	}

	return problems
}

// overlapProblems reports two profiles for one model that both match some
// firmware, which would leave the answer depending on file order.
func overlapProblems(registry Registry) []error {
	var problems []error

	byModel := make(map[string][]Profile, len(registry.Profiles))
	for _, candidate := range registry.Profiles {
		key := strings.ToLower(candidate.Model)
		byModel[key] = append(byModel[key], candidate)
	}

	for model, candidates := range byModel {
		for i, left := range candidates {
			for _, right := range candidates[i+1:] {
				if overlaps(left.Firmware, right.Firmware) {
					problems = append(problems, fmt.Errorf("%w: %s covers %+v and %+v",
						ErrOverlappingRange, model, left.Firmware, right.Firmware))
				}
			}
		}
	}

	slices.SortFunc(problems, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return problems
}

func overlaps(left, right Range) bool {
	return startsBefore(left, right) && startsBefore(right, left)
}

// startsBefore reports whether left begins before right ends. A bound that is
// not a version is reported by rangeProblems, and is treated here as covering
// nothing so that one unreadable bound does not also report every pair it is in.
func startsBefore(left, right Range) bool {
	if right.To == "" {
		return isVersion(left.From) && isVersion(right.From)
	}

	order, ok := compareBounds(left.From, right.To)

	return ok && order < 0
}
