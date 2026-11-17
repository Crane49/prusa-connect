package printcompat

import (
	"strings"
	"sync"
)

// The printer models the firmware knows, from printer_model_data.hpp of the
// public firmware. A G-code file names one in its M862.3 line by id, or in
// its M862.2 line by code.

// group is a firmware compatibility group: models in one group run each
// other's G-code as their own.
type group string

const (
	groupMK3          group = "mk3"
	groupMK35         group = "mk3_5"
	groupMK4          group = "mk4"
	groupMK4S         group = "mk4s"
	groupMINI         group = "mini"
	groupXL           group = "xl"
	groupXLS          group = "xls"
	groupIX           group = "ix"
	groupCoreOne      group = "coreone"
	groupCoreOneL     group = "coreonel"
	groupCoreOneINDX  group = "coreone_indx"
	groupCoreOneLINDX group = "coreonel_indx"
)

// model is one row of printer_model_info.
type model struct {
	id    string
	group group
	code  int
	// mmuID and mmuCode name the model's MMU variant. They are empty for a
	// model whose firmware is built without MMU support.
	mmuID   string
	mmuCode int
}

// models is printer_model_info with printer_model_mmu_variant folded in. The
// codes are the firmware's own.
//
//nolint:gochecknoglobals,mnd // a fixed table, transcribed and read-only.
var models = []model{
	{id: "MK3", group: groupMK3, code: 300, mmuID: "MK3MMU2", mmuCode: 20300},
	{id: "MK3S", group: groupMK3, code: 302, mmuID: "MK3SMMU2S", mmuCode: 20302},
	{id: "MK3.5", group: groupMK35, code: 230, mmuID: "MK3.5MMU3", mmuCode: 30230},
	{id: "MK3.5S", group: groupMK35, code: 280, mmuID: "MK3.5SMMU3", mmuCode: 30280},
	{id: "MK3.9", group: groupMK4, code: 210, mmuID: "MK3.9MMU3", mmuCode: 30210},
	{id: "MK3.9S", group: groupMK4S, code: 270, mmuID: "MK3.9SMMU3", mmuCode: 30270},
	{id: "MK4", group: groupMK4, code: 130, mmuID: "MK4MMU3", mmuCode: 30130},
	{id: "MK4S", group: groupMK4S, code: 260, mmuID: "MK4SMMU3", mmuCode: 30260},
	{id: "MINI", group: groupMINI, code: 120},
	{id: "XL", group: groupXL, code: 170},
	{id: "iX", group: groupIX, code: 160},
	{id: "COREONE", group: groupCoreOne, code: 310, mmuID: "COREONEMMU3", mmuCode: 30310},
	{id: "COREONEL", group: groupCoreOneL, code: 350, mmuID: "COREONELMMU3", mmuCode: 30350},
	{id: "COREONEINDX", group: groupCoreOneINDX, code: 360},
	{id: "COREONEL-INDX", group: groupCoreOneLINDX, code: 370},
	{id: "COREONEOAK", group: groupCoreOne, code: 380, mmuID: "COREONEOAKMMU3", mmuCode: 30380},
	{id: "XLP", group: groupXLS, code: 410},
}

// upgradeFrom is the group whose G-code a group also runs. The path is
// followed further: a Core One runs MK4S G-code, and through it MK4 and MK3.
//
//nolint:gochecknoglobals,exhaustive // a fixed table, read-only.
var upgradeFrom = map[group]upgrade{
	groupXLS:      {from: groupXL, mode: true},
	groupMK35:     {from: groupMK3, mode: true},
	groupMK4:      {from: groupMK3, mode: true},
	groupMK4S:     {from: groupMK4, mode: true},
	groupCoreOne:  {from: groupMK4S, mode: true},
	groupCoreOneL: {from: groupCoreOne},
}

// upgrade is one step of a group's upgrade path: the group whose G-code it
// takes, and whether the firmware runs that G-code in a compatibility mode.
type upgrade struct {
	from group
	mode bool
}

// fit is how one model's firmware takes G-code sliced for another model.
type fit int

const (
	fitNone fit = iota
	fitCompatibilityMode
	fitFull
)

// fitOf is how a printer of one group takes G-code sliced for another. A step
// of the path that sets a mode makes the whole fit a compatibility mode.
func fitOf(printer, sliced group) fit {
	if printer == sliced {
		return fitFull
	}

	mode := false

	for step, ok := upgradeFrom[printer]; ok; step, ok = upgradeFrom[step.from] {
		mode = mode || step.mode
		if step.from == sliced {
			if mode {
				return fitCompatibilityMode
			}

			return fitFull
		}
	}

	return fitNone
}

// slicedModel is the model a G-code file's M862.3 id names. The MMU variants
// are looked up first, and the match is case-sensitive, as in the firmware.
func slicedModel(id string) (model, bool) {
	for _, row := range models {
		if row.mmuID != "" && row.mmuID == id {
			return row, true
		}
	}

	for _, row := range models {
		if row.id == id {
			return row, true
		}
	}

	return model{}, false
}

// slicedCode is the model a G-code file's M862.2 code names.
func slicedCode(code int) (model, bool) {
	for _, row := range models {
		if row.mmuCode != 0 && row.mmuCode == code {
			return row, true
		}
	}

	for _, row := range models {
		if row.code == code {
			return row, true
		}
	}

	return model{}, false
}

// printerModel is the firmware model a device record's model names. Both
// sides are compared without case and without separators.
func printerModel(name string) (model, bool) {
	wanted := squash(name)
	if wanted == "" {
		return model{}, false
	}

	row, ok := squashedModels()[wanted]

	return row, ok
}

// squashedModels is the models table keyed by its squashed id, built once.
var squashedModels = sync.OnceValue(func() map[string]model {
	byID := make(map[string]model, len(models))
	for _, row := range models {
		if _, taken := byID[squash(row.id)]; !taken {
			byID[squash(row.id)] = row
		}
	}

	return byID
})

// separators drops the spaces, hyphens and underscores squash ignores.
var separators = strings.NewReplacer(" ", "", "-", "", "_", "")

// squash is a model name in upper case with no space, hyphen or underscore.
func squash(name string) string {
	return separators.Replace(strings.ToUpper(strings.TrimSpace(name)))
}

// takesMMU reports whether the model's firmware is built with MMU support.
func (m model) takesMMU() bool { return m.mmuID != "" }

// isINDX reports whether the model is a Core One with INDX. Its firmware has
// the INDX lock feature and the nozzle cleaner check.
func (m model) isINDX() bool { return m.group == groupCoreOneINDX || m.group == groupCoreOneLINDX }

// presetFilaments is the filament presets of a public release firmware, by
// name. None of them is abrasive.
//
//nolint:gochecknoglobals // a fixed table, transcribed and read-only.
var presetFilaments = map[string]bool{
	"PLA": true, "PETG": true, "ASA": true, "PC": true, "PVB": true,
	"ABS": true, "HIPS": true, "PP": true, "FLEX": true, "PA": true,
}
