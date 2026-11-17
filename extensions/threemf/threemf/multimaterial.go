package threemf

import (
	"encoding/json"
	"strconv"
	"strings"
)

// getter reads one setting as plain text, and reports whether it is set.
type getter func(string) (string, bool)

// none is a getter with no settings.
func none(string) (string, bool) { return "", false }

// extruderRules are what a slicer needs to know to resolve which extruders
// print a part.
type extruderRules struct {
	// count is the number of extruders the printer has, one per filament.
	// It is 0 when the file does not say.
	count int
	// roles are the settings that choose an extruder for one kind of
	// extrusion, such as perimeter_extruder. The first is the outer wall,
	// whose colour a person sees.
	roles []string
	// setAll is true when the extruder setting sets every role, as in
	// PrusaSlicer. In OrcaSlicer it is the extruder of every role that is 0.
	setAll bool
	// printSettings reads the print's settings.
	printSettings getter
}

// The roles of PrusaSlicer: apply_to_print_region_config in
// src/libslic3r/PrintObject.cpp of 2.9.6 copies the extruder setting into
// these three and then takes each of them that is not 0.
//
//nolint:gochecknoglobals // a constant table.
var prusaRoles = []string{"perimeter_extruder", "infill_extruder", "solid_infill_extruder"}

// The roles of OrcaSlicer: keys_extruders of apply_to_print_region_config in
// src/libslic3r/PrintObject.cpp, where 0 falls back to the extruder setting,
// and the three settings older releases wrote instead.
//
//nolint:gochecknoglobals // a constant table.
var orcaRoles = []string{
	"outer_wall_filament_id", "inner_wall_filament_id", "sparse_infill_filament_id",
	"internal_solid_filament_id", "top_surface_filament_id", "bottom_surface_filament_id",
	"wall_filament", "sparse_infill_filament", "solid_infill_filament",
}

// region resolves the extruder of every role from the levels of settings,
// the print's first: a part over its layer range, a range over its object,
// and an object over the print. An extruder the printer does not have
// becomes the first one: clamp_exturder_to_default in
// src/libslic3r/PrintObject.cpp of PrusaSlicer 2.9.6, and the load of
// OrcaSlicer, which gives an object or a part with a filament past them the
// first one (_BBS_3MF_Importer::load_model_from_file). It reports whether
// the last level sets an extruder.
func (r extruderRules) region(levels ...getter) ([]int, bool) {
	roles := r.roles
	if len(roles) == 0 {
		roles = prusaRoles
	}

	values := make([]int, len(roles))
	base, explicit := 0, false

	all := append([]getter{r.printOrNone()}, levels...)
	last := len(all) - 1

	for l, level := range all {
		set := r.apply(level, roles, values, &base)
		explicit = explicit || (set && l == last && l > 0)
	}

	for i := range values {
		if values[i] == 0 {
			values[i] = base
		}

		values[i] = r.clamp(values[i])
	}

	return values, explicit
}

// apply lays one level of settings over the extruders of the roles, and
// reports whether the level sets an extruder.
func (r extruderRules) apply(level getter, roles []string, values []int, base *int) bool {
	set := false

	if e := settingInt(level, "extruder"); e > 0 {
		*base, set = e, true

		if r.setAll {
			for i := range values {
				values[i] = e
			}
		}
	}

	for i, key := range roles {
		v, ok := level(key)
		if !ok {
			continue
		}

		n := int(parseNumber(v))

		switch {
		case n > 0:
			values[i], set = n, true
		case n == 0 && !r.setAll:
			values[i] = 0
		}
	}

	return set
}

func (r extruderRules) printOrNone() getter {
	if r.printSettings == nil {
		return none
	}

	return r.printSettings
}

func settingInt(g getter, key string) int {
	v, _ := g(key)

	return int(parseNumber(v))
}

func (r extruderRules) clamp(e int) int {
	if e < 1 || (r.count > 0 && e > r.count) {
		return 1
	}

	return e
}

// resolveParts fills each part's extruder and painting, and each object's
// extruders, painting and layer ranges.
func resolveParts(rules extruderRules, specs []objectSpec, objects []Object) {
	for o := range objects {
		resolveObject(rules, &specs[o], &objects[o])
	}
}

// resolveObject joins what its parts print with, what its layer ranges
// change, and the support its settings ask for. Support only where a person
// asked for it is printed when the object has a support enforcer, or an
// area painted to have support.
func resolveObject(rules extruderRules, spec *objectSpec, object *Object) {
	var (
		used     stateSet
		enforced bool
	)

	for p := range object.Parts {
		pt := resolvePart(rules, spec, &spec.parts[p], &object.Parts[p], &used)
		enforced = enforced || pt.enforcers || object.Parts[p].Type == PartSupportEnforcer

		object.Painting.Multimaterial = object.Painting.Multimaterial || object.Parts[p].Painting.Multimaterial
		object.Painting.Supports = object.Painting.Supports || pt.supports
		object.Painting.Seams = object.Painting.Seams || pt.seams
	}

	resolveRanges(rules, spec, object, &used)

	if !spec.support.manual || enforced {
		for _, e := range spec.support.extruders {
			used.add(rules.clamp(e))
		}
	}

	used.each(func(e int) { object.Extruders = append(object.Extruders, e) })
}

// resolveRanges fills the layer ranges of an object. A range that sets an
// extruder prints every model part in it with that extruder, unless the part
// sets its own: region_config_from_model_volume applies the range between the
// object and the part.
func resolveRanges(rules extruderRules, spec *objectSpec, object *Object, used *stateSet) {
	for _, rg := range spec.ranges {
		out := LayerRange{MinZ: rg.minZ, MaxZ: rg.maxZ, Settings: rg.values}

		roles, explicit := rules.region(spec.settings, rg.get)
		if explicit {
			out.Extruder = roles[0]

			for p := range spec.parts {
				if spec.parts[p].part.Type != PartModel {
					continue
				}

				partRoles, _ := rules.region(spec.settings, rg.get, spec.parts[p].settings)
				for _, e := range partRoles {
					used.add(e)
				}
			}
		}

		object.LayerRanges = append(object.LayerRanges, out)
	}
}

// resolvePart fills a part's extruder and painting, adds the extruders it
// prints with to used, and returns its painting. A model part has its
// extruder. A modifier has one only when it sets one, over the object's.
// Painting with extruders is used only on a printer with more than one
// extruder, and a painted state the printer has no extruder for is ignored:
// PrintObject::is_mm_painted and the num_facets_states of
// src/libslic3r/MultiMaterialSegmentation.cpp of PrusaSlicer 2.9.6. The
// part's own extruders are used unless painting covers every triangle.
func resolvePart(rules extruderRules, spec *objectSpec, ps *partSpec, part *Part, used *stateSet) paint {
	pt := paintOfPart(ps.pieces)
	pt.merge(ps.paint)

	part.Painting = Painting{Multimaterial: pt.colors.painted(), Supports: pt.supports, Seams: pt.seams}

	var roles []int

	switch part.Type {
	case PartModel:
		roles, _ = rules.region(spec.settings, ps.getter())
	case PartModifier:
		if r, explicit := rules.region(spec.settings, ps.getter()); explicit {
			roles = r
		}
	case PartNegative, PartSupportBlocker, PartSupportEnforcer:
	}

	if len(roles) == 0 {
		return pt
	}

	part.Extruder = roles[0]
	part.PaintedExtruders = paintedExtruders(rules, pt.colors)

	if part.Type != PartModel || pt.colors.empty() || pt.colors.has(0) || len(part.PaintedExtruders) == 0 {
		for _, e := range roles {
			used.add(e)
		}
	}

	for _, e := range part.PaintedExtruders {
		used.add(e)
	}

	return pt
}

// paintedExtruders lists the extruders a multi-material painting prints
// with.
func paintedExtruders(rules extruderRules, colors stateSet) []int {
	if rules.count == 1 {
		return nil
	}

	var out []int

	colors.each(func(state int) {
		if state > 0 && (rules.count == 0 || state <= rules.count) {
			out = append(out, state)
		}
	})

	return out
}

// supportKeys name the settings that turn support on and choose the
// extruders it prints with.
type supportKeys struct {
	on, enforce, raft, extruder, iface, ifaceLayers string
	// auto names the setting that says whether the slicer finds where
	// support is needed, or puts it only where a person asked for it.
	auto string
	// manual reports whether a value of auto puts support only where a
	// person asked for it.
	manual func(string) bool
	// mode reads the on setting when it names a mode rather than a yes or
	// no: whether support is on and whether only where a person asked.
	mode func(string) (bool, bool)
}

// The support settings of PrusaSlicer and of OrcaSlicer. An object prints
// support when support is on, when it enforces support on its lowest layers,
// or when it has a raft: PrintObject::has_support_material in
// src/libslic3r/Print.hpp of PrusaSlicer 2.9.6. OrcaSlicer names the same
// settings enable_support, enforce_support_layers and support_filament.
//
//nolint:gochecknoglobals // two constant tables.
var (
	prusaSupport = supportKeys{
		on: "support_material", enforce: "support_material_enforce_layers", raft: "raft_layers",
		extruder: "support_material_extruder", iface: "support_material_interface_extruder",
		ifaceLayers: "support_material_interface_layers",
		auto:        "support_material_auto",
		manual:      func(v string) bool { return v == "0" || v == "false" },
	}
	orcaSupport = supportKeys{
		on: "enable_support", enforce: "enforce_support_layers", raft: "raft_layers",
		extruder: "support_filament", iface: "support_interface_filament",
		ifaceLayers: "support_interface_top_layers",
		auto:        "support_type",
		manual:      func(v string) bool { return strings.HasSuffix(v, "(manual)") },
	}
	// PrusaSlicer 3.0 names the mode in support_material: none,
	// enforcers_only or everywhere (ConfigDefsFDM.cpp of 3.0.0-alpha12).
	prusa3Support = supportKeys{
		on: "support_material", enforce: "support_material_enforce_layers", raft: "raft_layers",
		extruder: "support_material_extruder", iface: "support_material_interface_extruder",
		ifaceLayers: "support_material_interface_layers",
		mode: func(v string) (bool, bool) {
			return v == "everywhere" || v == "enforcers_only", v == "enforcers_only"
		},
	}
)

// supportPlan is the support an object's settings ask for.
type supportPlan struct {
	// extruders are the extruders the support prints with.
	extruders []int
	// manual is true when support is printed only where a person asked
	// for it, with a support enforcer or by painting. With no raft and no
	// enforced layers, an object with neither has no support.
	manual bool
}

// supportExtruders are the extruders an object prints its support with,
// when its settings turn support on. get returns the object's own value of
// a setting, or else the print's, as plain text. An extruder of 0 prints
// support with whichever extruder is in use and is left out, and so is the
// interface extruder when the support has no interface layers.
func supportExtruders(k supportKeys, get func(string) string) supportPlan {
	support := get(k.on) == "1" || get(k.on) == "true"

	var manual bool
	if k.mode != nil {
		support, manual = k.mode(get(k.on))
	} else {
		manual = k.manual(get(k.auto))
	}

	always := parseNumber(get(k.enforce)) > 0 || parseNumber(get(k.raft)) > 0

	if !support && !always {
		return supportPlan{}
	}

	out := supportPlan{manual: !always && manual}

	if e := int(parseNumber(get(k.extruder))); e > 0 {
		out.extruders = append(out.extruders, e)
	}

	layers := get(k.ifaceLayers)
	if e := int(parseNumber(get(k.iface))); e > 0 && (layers == "" || parseNumber(layers) > 0) {
		out.extruders = append(out.extruders, e)
	}

	return out
}

// overlay returns a getter that reads a setting from the object first and
// from the print after.
func overlay(object, global getter) func(string) string {
	return func(key string) string {
		if v, ok := object(key); ok {
			return v
		}

		v, _ := global(key)

		return v
	}
}

// jsonScalar turns a JSON string, number or boolean into plain text, a
// boolean as 1 or 0.
func jsonScalar(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}

	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		if t {
			return "1", true
		}

		return "0", true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}

	return "", false
}

// jsonGetter reads settings from a table of JSON values.
func jsonGetter(values map[string]json.RawMessage) getter {
	return func(key string) (string, bool) { return jsonScalar(values[key]) }
}

// textGetter reads settings from a table of plain values.
func textGetter(values map[string]string) getter {
	return func(key string) (string, bool) {
		v, ok := values[key]

		return unquote(v), ok
	}
}

// projectExtruders joins the extruders of every object with those the tool
// changes of every bed switch to.
func projectExtruders(p *Project, rules extruderRules) []int {
	var used stateSet

	for _, object := range p.Objects {
		for _, e := range object.Extruders {
			used.add(e)
		}
	}

	for _, setup := range p.Setups {
		for _, bed := range setup.Beds {
			for _, change := range bed.LayerChanges {
				if change.Kind == LayerChangeTool && change.Extruder > 0 &&
					(rules.count == 0 || change.Extruder <= rules.count) {
					used.add(change.Extruder)
				}
			}
		}
	}

	var out []int

	used.each(func(e int) { out = append(out, e) })

	return out
}

// filaments makes the filaments of a setup from the colours the slicer
// shows each extruder in and the material of each. count is the number of
// extruders, and 0 takes the longest list.
func filaments(count int, colors, fallbackColors, types []string) []Filament {
	if count == 0 {
		count = max(len(colors), len(fallbackColors), len(types))
	}

	out := make([]Filament, 0, count)

	for i := range count {
		f := Filament{Extruder: i + 1}

		if i < len(colors) {
			f.Color = normalColor(colors[i])
		}

		if f.Color == "" && i < len(fallbackColors) {
			f.Color = normalColor(fallbackColors[i])
		}

		if i < len(types) {
			f.Type = strings.TrimSpace(types[i])
		}

		out = append(out, f)
	}

	return out
}

// The lengths of a colour as #RRGGBB and as #RRGGBBAA.
const (
	rgbLength  = 7
	rgbaLength = 9
)

// normalColor returns a colour as #RRGGBB in capitals, and "" for text that
// is not one. A colour with an alpha channel, as OrcaSlicer may write it,
// loses it.
func normalColor(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == rgbaLength {
		value = value[:rgbLength]
	}

	if len(value) != rgbLength || value[0] != '#' {
		return ""
	}

	for i := 1; i < len(value); i++ {
		if _, ok := hexNibble(value[i]); !ok {
			return ""
		}
	}

	return strings.ToUpper(value)
}

// splitKeep splits a PrusaSlicer list of strings at sep and keeps empty
// values, which hold places: extruder_colour = ;#FF0000 gives the second
// extruder a colour and the first none. Each value loses its quotes.
func splitKeep(value string, sep byte) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	var (
		out    []string
		quoted bool
		start  int
	)

	for i := 0; i <= len(value); i++ {
		if i < len(value) {
			switch value[i] {
			case '"':
				quoted = !quoted

				continue
			case '\\':
				i++

				continue
			}

			if value[i] != sep || quoted {
				continue
			}
		}

		out = append(out, unquote(value[start:min(i, len(value))]))
		start = i + 1
	}

	return out
}
