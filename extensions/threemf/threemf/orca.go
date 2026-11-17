package threemf

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// The files OrcaSlicer adds to the 3MF core.
const (
	orcaModelSettings   = "Metadata/model_settings.config"
	orcaProjectSettings = "Metadata/project_settings.config"
	orcaCustomGCodes    = "Metadata/custom_gcode_per_layer.xml"
	orcaLayerRanges     = "Metadata/layer_config_ranges.xml"
	orcaSettingsGroup   = "project_settings"
	// orcaByObject is the print_sequence that prints one object after the
	// other. The other value is "by layer".
	orcaByObject = "by object"
)

// The plate layout of OrcaSlicer. The plates stand on a grid, row by row
// from the first, with columns ceil(sqrt(plates)) wide. The next plate to the
// right starts a fifth of a plate's width after the last one ends, and the
// next row starts a fifth of a plate's depth below.
const orcaPlateGap = 0.2

// orcaOnlyKeys are settings OrcaSlicer writes into project_settings.config
// and BambuStudio does not. Both write "BambuStudio-<version>" as the
// Application, so the settings tell them apart: a file with at least
// orcaKeysNeeded of these keys is from OrcaSlicer.
var orcaOnlyKeys = []string{
	"elefant_foot_compensation_layers",
	"gcode_label_objects",
	"staggered_inner_seams",
	"wipe_on_loops",
}

const orcaKeysNeeded = 2

// The names OrcaSlicer and BambuStudio write as the Application.
const (
	orcaName  = "OrcaSlicer"
	bambuName = "BambuStudio"
)

// orca reads OrcaSlicer projects. A BambuStudio project has the same layout
// and is read the same way, with an issue that its producer is not supported.
type orca struct {
	settings map[string]json.RawMessage
	config   map[int]orcaObject
	plates   []orcaPlate
	// changes are the layer changes by plate id.
	changes map[int][]LayerChange
	// ranges are the layer ranges by the object's position, from 1.
	ranges map[int][]layerRange
}

// orcaObject is one object of model_settings.config, by the id of the
// object in the root model.
type orcaObject struct {
	name  string
	parts map[int]Part
	// partSettings are the settings of the parts, by part id.
	partSettings map[int]map[string]string
	// settings are the object's own settings.
	settings map[string]string
}

type orcaPlate struct {
	id     int
	name   string
	locked bool
	// sequence is the plate's own print_sequence, and empty when the plate
	// takes the one of the project.
	sequence string
	// gcode is the zip entry the plate's G-code is in.
	gcode string
}

func (*orca) name() Dialect { return DialectOrcaSlicer }

func (*orca) matches(p Producer, _ *archive) bool {
	return p.Name == orcaName || p.Name == bambuName
}

func (d *orca) prepare(rd *reading) error {
	if f := rd.a.entry(orcaProjectSettings); f != nil {
		data, err := readAll(rd.ctx, f, rd.limits.MaxMetadataBytes, "MaxMetadataBytes")
		if err != nil {
			return err
		}

		if json.Unmarshal(data, &d.settings) != nil {
			rd.issue(IssueSettingsUnreadable, orcaProjectSettings)
		}
	}

	if f := rd.a.entry(orcaModelSettings); f != nil {
		data, err := readAll(rd.ctx, f, rd.limits.MaxMetadataBytes, "MaxMetadataBytes")
		if err != nil {
			return err
		}

		if !d.parseModelSettings(data) {
			rd.issue(IssueSettingsUnreadable, orcaModelSettings)
		}
	}

	if err := d.readChanges(rd); err != nil {
		return err
	}

	// OrcaSlicer counts the filaments by their profiles, and gives an
	// object or a part with a filament past them the first one.
	ranges, err := readLayerRanges(rd, orcaLayerRanges)
	if err != nil {
		return err
	}

	d.ranges = ranges
	rd.rules = extruderRules{
		count: len(
			orcaList(d.settings["filament_settings_id"]),
		),
		roles:         orcaRoles,
		printSettings: jsonGetter(d.settings),
	}
	if rd.rules.count == 0 {
		rd.rules.count = len(orcaList(d.settings["filament_colour"]))
	}

	d.tellProducer(rd)

	return nil
}

// tellProducer tells an OrcaSlicer project written as BambuStudio by its
// settings, and reports any other BambuStudio project as unsupported.
func (d *orca) tellProducer(rd *reading) {
	if rd.producer.Name == orcaName {
		return
	}

	found := 0

	for _, key := range orcaOnlyKeys {
		if _, ok := d.settings[key]; ok {
			found++
		}
	}

	if found >= orcaKeysNeeded {
		rd.producer.Name = orcaName
	} else {
		rd.issue(IssueUnsupportedProducer, rd.producer.Application)
	}
}

// readChanges reads the layer changes of every plate.
func (d *orca) readChanges(rd *reading) error {
	f := rd.a.entry(orcaCustomGCodes)
	if f == nil {
		return nil
	}

	data, err := readAll(rd.ctx, f, rd.limits.MaxMetadataBytes, "MaxMetadataBytes")
	if err != nil {
		return err
	}

	changes, ok := orcaLayerChanges(data)
	if !ok {
		rd.issue(IssueSettingsUnreadable, orcaCustomGCodes)
	}

	d.changes = changes

	return nil
}

// parseModelSettings reads the objects with the names and types of their
// parts, and the plates. The instances listed on each plate are not read:
// OrcaSlicer places them by where they stand.
func (d *orca) parseModelSettings(data []byte) bool {
	type meta struct {
		Key   string `xml:"key,attr"`
		Value string `xml:"value,attr"`
	}

	var doc struct {
		Objects []struct {
			ID       int    `xml:"id,attr"`
			Metadata []meta `xml:"metadata"`
			Parts    []struct {
				ID       int    `xml:"id,attr"`
				Subtype  string `xml:"subtype,attr"`
				Metadata []meta `xml:"metadata"`
			} `xml:"part"`
		} `xml:"object"`
		Plates []struct {
			Metadata []meta `xml:"metadata"`
		} `xml:"plate"`
	}

	if xml.NewDecoder(bytes.NewReader(data)).Decode(&doc) != nil {
		return false
	}

	value := func(entries []meta, key string) string {
		for _, m := range entries {
			if m.Key == key {
				return m.Value
			}
		}

		return ""
	}
	number := func(entries []meta, key string) (int, bool) {
		n, err := strconv.Atoi(value(entries, key))

		return n, err == nil
	}

	d.config = make(map[int]orcaObject, len(doc.Objects))

	for _, o := range doc.Objects {
		object := orcaObject{
			name: value(o.Metadata, "name"), parts: map[int]Part{}, partSettings: map[int]map[string]string{},
			settings: map[string]string{},
		}

		for _, m := range o.Metadata {
			object.settings[m.Key] = m.Value
		}

		for _, part := range o.Parts {
			object.parts[part.ID] = Part{Name: value(part.Metadata, "name"), Type: orcaPartType(part.Subtype)}
			settings := map[string]string{}
			for _, m := range part.Metadata {
				settings[m.Key] = m.Value
			}

			object.partSettings[part.ID] = settings
		}

		d.config[o.ID] = object
	}

	for i, p := range doc.Plates {
		id, ok := number(p.Metadata, "plater_id")
		if !ok {
			id = i + 1
		}

		plate := orcaPlate{
			id:       id,
			name:     value(p.Metadata, "plater_name"),
			locked:   value(p.Metadata, "locked") == "true",
			sequence: value(p.Metadata, "print_sequence"),
			gcode:    value(p.Metadata, "gcode_file"),
		}

		d.plates = append(d.plates, plate)
	}

	slices.SortStableFunc(d.plates, func(x, y orcaPlate) int { return x.id - y.id })

	return true
}

func orcaPartType(subtype string) PartType {
	switch subtype {
	case "negative_part":
		return PartNegative
	case "modifier_part":
		return PartModifier
	case "support_blocker":
		return PartSupportBlocker
	case "support_enforcer":
		return PartSupportEnforcer
	}

	return PartModel
}

// objects groups the build items by the object they name, as the 3MF core
// does, and takes the names and part types from model_settings.config. Each
// component of an object is one part, keyed by the id of the object the
// component points at.
func (d *orca) objects(rd *reading) ([]objectSpec, error) {
	specs, err := groupByObject(rd, func(res *resource, c component) PartType {
		if part, ok := d.config[res.key.id].parts[c.ref.id]; ok {
			return part.Type
		}

		return PartModel
	})
	if err != nil {
		return nil, err
	}

	for o := range specs {
		specs[o].ranges = d.ranges[o+1]
		d.objectSettings(rd, &specs[o])
	}

	return specs, nil
}

// objectSettings takes an object's name, the names of its parts, its
// extruders and its support from model_settings.config. Each component of an
// object is one part, keyed by the id of the object the component points at.
func (d *orca) objectSettings(rd *reading, spec *objectSpec) {
	config, ok := d.config[spec.resourceID]
	spec.settings = textGetter(config.settings)
	spec.support = supportExtruders(orcaSupport, overlay(spec.settings, jsonGetter(d.settings)))

	if !ok || len(spec.items) == 0 {
		return
	}

	if config.name != "" {
		spec.name = config.name
	}

	// An object of one part prints with the object's extruder: OrcaSlicer
	// drops the part's own as it loads the file.
	for p := range spec.parts {
		settings := config.partSettings[spec.parts[p].id]
		if len(spec.parts) == 1 && settings != nil {
			settings = maps.Clone(settings)
			delete(settings, "extruder")
		}

		spec.parts[p].settings = textGetter(settings)
	}

	res := rd.models.resource(spec.items[0].ref)
	if res == nil {
		return
	}

	first := 0
	if res.mesh != nil {
		first = 1
	}

	for c, comp := range res.comps {
		if part, found := config.parts[comp.ref.id]; found && part.Name != "" && first+c < len(spec.parts) {
			spec.parts[first+c].part.Name = part.Name
		}
	}
}

// place builds the one setup and a bed for every plate. OrcaSlicer does not
// trust the instances model_settings.config lists for each plate: when it
// opens a project, PartPlateList::reload_all_objects puts every instance on
// the first plate, in plate order, whose box its box overlaps. This does the
// same. A file without plates has one.
func (d *orca) place(rd *reading, p *Project, _ []objectSpec) {
	setup := d.setup()
	poly := bedHull(setup.BedShape)
	setup.Beds = d.beds(orcaSize(setup.BedShape))
	d.finishBeds(rd, setup.Beds)

	for b := range setup.Beds {
		setup.Beds[b].Size = size(setup.BedShape)
	}

	p.Setups = []Setup{setup}

	if len(poly) < minPolygon {
		for o := range p.Objects {
			for i := range p.Objects[o].Instances {
				inst := &p.Objects[o].Instances[i]
				inst.Bed, inst.Placement = &BedRef{}, PlacementUnknown
				p.Setups[0].Beds[0].Instances = append(
					p.Setups[0].Beds[0].Instances,
					InstanceRef{Object: o, Instance: i},
				)
			}
		}

		return
	}

	lo, hi := bounds2(setup.BedShape)

	for o := range p.Objects {
		for i := range p.Objects[o].Instances {
			inst := &p.Objects[o].Instances[i]

			for b := range p.Setups[0].Beds {
				bed := &p.Setups[0].Beds[b]
				at := bed.Offset

				if !orcaOnPlate(Point{X: lo.X + at.X, Y: lo.Y + at.Y}, Point{X: hi.X + at.X, Y: hi.Y + at.Y},
					setup.MaxPrintHeight, inst.Bounds) {
					continue
				}

				inst.Bed = &BedRef{Setup: 0, Bed: b}
				inst.Placement = state(inst.Outline, inst.Bounds, translate(poly, at), setup.MaxPrintHeight)
				bed.Instances = append(bed.Instances, InstanceRef{Object: o, Instance: i})

				break
			}
		}
	}
}

// finishBeds gives each plate its print order, its layer changes and its
// G-code. A plate's own print_sequence wins over the project's:
// PartPlate::get_real_print_seq. The G-code is kept only when the archive
// holds the entry the plate names.
func (d *orca) finishBeds(rd *reading, beds []Bed) {
	global := jsonString(string(d.settings["print_sequence"]))

	plates := d.plates
	if len(plates) == 0 {
		plates = []orcaPlate{{id: 1}}
	}

	for b := range beds {
		if b >= len(plates) {
			break
		}

		plate := plates[b]
		sequence := firstNonEmpty(plate.sequence, global)
		beds[b].Sequential = sequence == orcaByObject

		if changes := d.changes[plate.id]; len(changes) > 0 {
			beds[b].LayerChanges = slices.Clone(changes)
			sortChanges(beds[b].LayerChanges)
		}

		if name := strings.TrimPrefix(plate.gcode, "/"); name != "" && rd.a.entry(name) != nil {
			beds[b].GCode = name
		}
	}
}

// orcaSize is the plate size OrcaSlicer lays plates out by: the width and
// depth of the printable area, cut to whole millimetres, as
// PartPlateList::reset_size stores them in ints.
func orcaSize(shape []Point) Point {
	s := size(shape)

	return Point{X: math.Trunc(s.X), Y: math.Trunc(s.Y)}
}

// beds makes a bed of every plate, or one bed when the file names none. s is
// the plate size the plates are laid out by.
func (d *orca) beds(s Point) []Bed {
	plates := d.plates
	if len(plates) == 0 {
		plates = []orcaPlate{{id: 1}}
	}

	cols := plateColumns(len(plates))
	out := make([]Bed, 0, len(plates))

	for i, plate := range plates {
		offset := Point{
			X: float64(i%cols) * s.X * (1 + orcaPlateGap),
			Y: -float64(i/cols) * s.Y * (1 + orcaPlateGap),
		}
		out = append(out, Bed{Index: i, Name: plate.name, Locked: plate.locked, Offset: offset, Size: s})
	}

	return out
}

// plateColumns is how many plates OrcaSlicer puts in one row: the square root
// of the count, rounded up.
func plateColumns(plates int) int {
	cols := 1
	for cols*cols < plates {
		cols++
	}

	return cols
}

// setup reads the setup from project_settings.config.
func (d *orca) setup() Setup {
	str := func(key string) string { return jsonString(string(d.settings[key])) }

	out := Setup{
		PrinterModel:   str("printer_model"),
		PrinterProfile: str("printer_settings_id"),
		PrintProfile:   str("print_settings_id"),
		BedShape:       parseBedShape(strings.Join(orcaList(d.settings["printable_area"]), ",")),
		MaxPrintHeight: orcaNumber(d.settings["printable_height"]),
		Technology:     str("printer_technology"),
		GCodeFlavor:    str("gcode_flavor"),
	}

	out.MaterialProfiles = orcaList(d.settings["filament_settings_id"])

	for _, field := range orcaList(d.settings["nozzle_diameter"]) {
		out.Nozzles = append(out.Nozzles, Nozzle{DiameterMM: parseNumber(field)})
	}

	out.Nozzles = singleHotEnd(out.Nozzles, str("single_extruder_multi_material"))

	// OrcaSlicer shows each filament in its filament colour.
	out.Filaments = filaments(
		len(orcaList(d.settings["filament_settings_id"])),
		orcaList(d.settings["filament_colour"]),
		nil,
		orcaList(d.settings["filament_type"]),
	)

	if len(d.settings) > 0 {
		group := SettingsGroup{Name: orcaSettingsGroup, Values: make(map[string]string, len(d.settings))}

		for key, raw := range d.settings {
			var compact bytes.Buffer
			if json.Compact(&compact, raw) == nil {
				group.Values[key] = compact.String()
			}
		}

		out.Settings = []SettingsGroup{group}
	}

	return out
}

// orcaList reads a value OrcaSlicer writes as a list of strings, or as one
// string.
func orcaList(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}

	if s := jsonString(string(raw)); s != "" {
		return []string{s}
	}

	return nil
}

// orcaNumber reads a number OrcaSlicer writes as a string or as a number.
func orcaNumber(raw json.RawMessage) float64 {
	if s := jsonString(string(raw)); s != "" {
		return parseNumber(s)
	}

	return jsonNumber(string(raw))
}

// thumbnail ties Metadata/plate_<n>.png to the bed of plate n.
func (d *orca) thumbnail(path string) (BedRef, bool) {
	n, ok := plateNumber(path)
	if !ok {
		return BedRef{}, false
	}

	if len(d.plates) == 0 {
		return BedRef{}, n == 1
	}

	for b, plate := range d.plates {
		if plate.id == n {
			return BedRef{Bed: b}, true
		}
	}

	return BedRef{}, false
}

func (*orca) rewrite(context.Context, *rewriting) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, DialectOrcaSlicer)
}
