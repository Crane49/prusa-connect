package document

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/prusa3d/threemf/threemf"
)

// settingsSummary is the summary of one setup's settings that a page shows
// beside a project's beds. Each field is read from the slicer's own tables,
// which spell the same setting differently, and a zero or nil field is one the
// file does not hold.
type settingsSummary struct {
	LayerHeightMM      float64
	FirstLayerHeightMM float64
	// InfillPercent is the sparse infill density, from 0 to 100.
	InfillPercent *float64
	InfillPattern string
	Perimeters    int
	// Supports is none, everywhere, build_plate_only or enforcers_only, and
	// empty when the file does not say or says something else.
	Supports    string
	BrimWidthMM *float64
	WipeTower   *bool
	Filaments   []summaryFilament
}

// summaryFilament is one filament of a setup. Colour is #RRGGBB or empty.
type summaryFilament struct {
	Type   string
	Colour string
}

// The words Supports takes.
const (
	SupportsNone           = "none"
	SupportsEverywhere     = "everywhere"
	SupportsBuildPlateOnly = "build_plate_only"
	SupportsEnforcersOnly  = "enforcers_only"
)

// percentScale turns a percentage into a share.
const percentScale = 100

// maxFilaments bounds the filaments read from one setup. A list in a settings
// table is as long as the file says, and no printer has more tools than this.
const maxFilaments = 64

// summariseSettings reads the summary out of one setup's raw tables.
//
// PrusaSlicer 2 writes one table of plain text, with a list joined by
// semicolons. PrusaSlicer 3 and OrcaSlicer write JSON values: a string in
// quotes, a list in brackets, and PrusaSlicer 3 writes a percentage as an
// object with is_percent and value. Each value is read into plain text first,
// so one lookup serves every dialect.
func summariseSettings(setup threemf.Setup) settingsSummary {
	values := settingsOf(setup.Settings)

	out := settingsSummary{
		LayerHeightMM: number(values.first("layer_height")),
		InfillPattern: values.first("fill_pattern", "sparse_infill_pattern"),
		Perimeters:    int(number(values.first("perimeters", "wall_loops"))),
	}

	out.FirstLayerHeightMM = firstLayer(
		values.first("first_layer_height", "initial_layer_print_height"),
		out.LayerHeightMM,
	)

	if density, ok := percent(values.first("fill_density", "sparse_infill_density")); ok {
		out.InfillPercent = &density
	}

	out.Supports = supports(values)
	out.BrimWidthMM = brim(values)

	out.WipeTower = flag(values.first("wipe_tower", "enable_prime_tower"))

	out.Filaments = filaments(values, len(setup.MaterialProfiles))

	return out
}

// settingValues holds every setting of a setup as plain text, by key. A list
// is kept as its items.
type settingValues map[string][]string

func settingsOf(groups []threemf.SettingsGroup) settingValues {
	out := settingValues{}

	for _, group := range groups {
		for key, raw := range group.Values {
			if _, seen := out[key]; seen {
				continue
			}

			out[key] = plainText(raw)
		}
	}

	return out
}

// first answers the first item of the first key the setup holds.
func (v settingValues) first(keys ...string) string {
	for _, key := range keys {
		if items, ok := v[key]; ok && len(items) > 0 {
			return items[0]
		}
	}

	return ""
}

// list answers every item of the first key the setup holds.
func (v settingValues) list(key string) []string {
	return v[key]
}

// plainText turns one value as a slicer wrote it into its items as text.
func plainText(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	switch trimmed[0] {
	case '"', '[', '{':
		var decoded any
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			return jsonText(decoded)
		}
	case 't', 'f':
		if trimmed == "true" {
			return []string{"1"}
		}

		if trimmed == "false" {
			return []string{"0"}
		}
	}

	// PrusaSlicer 2 joins a list with semicolons and writes no quotes.
	return strings.Split(trimmed, ";")
}

// jsonText reads a decoded JSON value into its items as text.
func jsonText(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case float64:
		return []string{strconv.FormatFloat(typed, 'f', -1, 64)}
	case bool:
		if typed {
			return []string{"1"}
		}

		return []string{"0"}
	case []any:
		out := make([]string, 0, min(len(typed), maxFilaments))
		for _, item := range typed[:min(len(typed), maxFilaments)] {
			out = append(out, jsonText(item)...)
		}

		return out
	case map[string]any:
		// PrusaSlicer 3 writes a value that may be a percentage as
		// {"is_percent": true, "value": 15}.
		amount, ok := typed["value"].(float64)
		if !ok {
			return nil
		}

		text := strconv.FormatFloat(amount, 'f', -1, 64)
		if isPercent, _ := typed["is_percent"].(bool); isPercent {
			text += "%"
		}

		return []string{text}
	}

	return nil
}

// percent reads a density such as "15%" or "15", from 0 to 100.
func percent(text string) (float64, bool) {
	text = strings.TrimSuffix(strings.TrimSpace(text), "%")

	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || value > 100 {
		return 0, false
	}

	return value, true
}

// firstLayer reads the first layer's height. PrusaSlicer may write it as a
// percentage of the layer height.
func firstLayer(text string, layer float64) float64 {
	if share, ok := strings.CutSuffix(strings.TrimSpace(text), "%"); ok {
		return layer * number(share) / percentScale
	}

	return number(text)
}

// supports reads where support is generated.
//
// PrusaSlicer 2 has a switch, support_material, and support_material_auto
// off means only the enforcers make support. PrusaSlicer 3 writes one word in
// support_material. OrcaSlicer has enable_support, and a support type marked
// manual means only the enforcers make support.
func supports(values settingValues) string {
	if word := values.first("support_material"); word != "" {
		if on := flag(word); on != nil {
			return prusaSupports(*on, values)
		}

		return supportWord(word)
	}

	on := flag(values.first("enable_support"))
	if on == nil {
		return ""
	}

	switch {
	case !*on:
		return SupportsNone
	case strings.Contains(values.first("support_type"), "manual"):
		return SupportsEnforcersOnly
	case values.first("support_on_build_plate_only") == "1":
		return SupportsBuildPlateOnly
	}

	return SupportsEverywhere
}

func prusaSupports(on bool, values settingValues) string {
	switch {
	case !on:
		return SupportsNone
	case values.first("support_material_auto") == "0":
		return SupportsEnforcersOnly
	case values.first("support_material_buildplate_only") == "1":
		return SupportsBuildPlateOnly
	}

	return SupportsEverywhere
}

// supportWord reads the word PrusaSlicer 3 writes. A word this server does not
// know is left out rather than guessed.
func supportWord(word string) string {
	switch word {
	case SupportsNone, "disabled", "off":
		return SupportsNone
	case SupportsEverywhere, SupportsBuildPlateOnly, SupportsEnforcersOnly:
		return word
	}

	return ""
}

// brim reads the brim width. OrcaSlicer turns the brim off with brim_type and
// leaves the width as it was, and its automatic brim has no width to report.
func brim(values settingValues) *float64 {
	kind := values.first("brim_type")

	if kind == "no_brim" {
		none := 0.0

		return &none
	}

	text := values.first("brim_width")
	if text == "" {
		return nil
	}

	width := number(text)
	if kind == "auto_brim" && width == 0 {
		return nil
	}

	return &width
}

// filaments reads each filament's type and colour. PrusaSlicer shows a
// filament in its extruder's colour when one is set, and in the filament's own
// colour otherwise.
func filaments(values settingValues, profiles int) []summaryFilament {
	types := values.list("filament_type")
	colours := values.list("filament_colour")
	extruders := values.list("extruder_colour")

	count := min(max(len(types), len(colours), profiles), maxFilaments)

	out := make([]summaryFilament, 0, count)

	for i := range count {
		item := summaryFilament{
			Type:   itemAt(types, i),
			Colour: colourOf(alphaless(itemAt(extruders, i)), alphaless(itemAt(colours, i))),
		}

		if item.Type == "" && item.Colour == "" {
			continue
		}

		out = append(out, item)
	}

	return out
}

func itemAt(items []string, i int) string {
	if i < len(items) {
		return strings.TrimSpace(items[i])
	}

	return ""
}

// alphaless drops the alpha channel of a colour written as #RRGGBBAA, which
// OrcaSlicer may write.
func alphaless(colour string) string {
	const withAlpha = hexColourLen + 2

	if len(colour) == withAlpha && colour[0] == '#' {
		return colour[:hexColourLen]
	}

	return colour
}

// number reads a decimal a slicer wrote, ignoring a unit written after it. It
// answers zero for anything it cannot read, which is the same answer as "the
// file did not say".
func number(value string) float64 {
	value = strings.TrimSpace(value)

	end := 0
	for end < len(value) {
		c := value[end]
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' {
			break
		}

		end++
	}

	parsed, err := strconv.ParseFloat(value[:end], 64)
	if err != nil || parsed < 0 {
		return 0
	}

	return parsed
}

// flag reads a 0 or a 1, and answers nil for anything else.
func flag(value string) *bool {
	var out bool

	switch strings.TrimSpace(value) {
	case "1":
		out = true
	case "0":
		out = false
	default:
		return nil
	}

	return &out
}

// colourOf is the colour the slicer shows a tool in: the extruder's own
// colour when one is set, and the filament's colour otherwise. Only a colour
// written as #RRGGBB is kept.
func colourOf(extruder, filament string) string {
	for _, candidate := range []string{extruder, filament} {
		candidate = strings.Trim(strings.TrimSpace(candidate), `"`)
		if isHexColour(candidate) {
			return strings.ToUpper(candidate)
		}
	}

	return ""
}

// hexColourLen is the length of #RRGGBB.
const hexColourLen = 7

func isHexColour(value string) bool {
	if len(value) != hexColourLen || value[0] != '#' {
		return false
	}

	for _, c := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}

	return true
}
