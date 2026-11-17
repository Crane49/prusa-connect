package profile

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Alias is one spelling a printer reports and the model in the profile file it
// names.
//
// It covers a spelling the rules in Canonical do not reach. A machine that
// reports an unexpected string needs a row here and no code change.
type Alias struct {
	// Spelling is what the printer reported. It is compared after the same
	// normalisation the model names take, so case, spaces, dots, hyphens and
	// underscores in it change nothing.
	Spelling string `json:"spelling"`
	// Model is the model in Profiles this spelling resolves to.
	Model string `json:"model"`
	// Source says where the spelling was observed. A row nobody can trace back
	// to a real machine is a guess, and a guess here grants a printer another
	// model's capability set.
	Source string `json:"source"`
}

// Canonical resolves a model spelling a printer reported to the model a profile
// in this registry is filed under. ok is false when no rule and no alias reaches
// one. internal/printer/capability then resolves the printer as unmatched and
// logs a warning.
//
// Three rules, in this order. A model name matches with case and every
// separator ignored, so "Core One", "core-one" and "COREONE" all reach
// "CoreOne". A device type code matches as it is written, because a real
// printer reports one where a model name is expected. An alias row matches last,
// so a hand-written row cannot take a spelling away from the file's own data.
func (r Registry) Canonical(reported string) (model string, ok bool) {
	trimmed := strings.TrimSpace(reported)

	key := normaliseModel(trimmed)
	if key == "" {
		return "", false
	}

	for _, candidate := range r.Profiles {
		if normaliseModel(candidate.Model) == key {
			return candidate.Model, true
		}
	}

	// The code keeps its dots. Stripping them makes "1.3.10" and "13.1.0" one
	// spelling, and the two are different printers.
	for _, candidate := range r.Profiles {
		if strings.EqualFold(candidate.DeviceTypeID, trimmed) {
			return candidate.Model, true
		}
	}

	return r.aliased(key)
}

// aliased resolves a normalised spelling through the alias rows. A row naming a
// model this registry does not carry resolves to nothing, so a stale row leaves
// a printer unmatched.
func (r Registry) aliased(key string) (model string, ok bool) {
	for _, alias := range r.Aliases {
		if normaliseModel(alias.Spelling) != key {
			continue
		}

		for _, candidate := range r.Profiles {
			if strings.EqualFold(candidate.Model, alias.Model) {
				return candidate.Model, true
			}
		}
	}

	return "", false
}

// normaliseModel reduces a model spelling to the letters and digits in it,
// lowercased. Everything else goes, so a separator a firmware or a person chose
// is not the reason a printer matches no profile.
//
// With the separators dropped, a spelling such as "mk39" or "coreoneindx" is
// the same string as the model name and needs no row of its own. The rule
// also covers the spacing a printer or a person invents.
//
// A digit is kept, so "MK4" and "MK4S" stay apart. The models this file
// carries stay distinct under this rule, and
// TestNoTwoModelsAnswerToOneSpelling checks it.
//
// The rule lives in the SDK, so a driver's declared models are checked against
// one another with the comparison this lookup makes.
func normaliseModel(reported string) string {
	return connectorapi.FoldModel(reported)
}
