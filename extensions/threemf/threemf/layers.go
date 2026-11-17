package threemf

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

// layerKind turns the number the slicers store into a kind:
// CustomGCode::Type of PrusaSlicer 2.9.6 and 3.0, and of OrcaSlicer, whose
// value 5 is an unknown change and is left out.
func layerKind(n int) (LayerChangeKind, bool) {
	kinds := [...]LayerChangeKind{
		LayerChangeColor, LayerChangePause, LayerChangeTool, LayerChangeTemplate, LayerChangeCustom,
	}
	if n < 0 || n >= len(kinds) {
		return "", false
	}

	return kinds[n], true
}

// legacyLayerKind reads a change written before PrusaSlicer 2.3, which has
// no type and keeps its G-code in the gcode attribute:
// _extract_custom_gcode_per_print_z_from_archive of PrusaSlicer 2.9.6.
func legacyLayerKind(gcode, color string) (LayerChangeKind, string) {
	switch gcode {
	case "M600":
		return LayerChangeColor, ""
	case "M601":
		return LayerChangePause, color
	case "tool_change":
		return LayerChangeTool, ""
	}

	return LayerChangeCustom, gcode
}

// layerChangeOf reads one change from the attributes of a code element of
// PrusaSlicer or a layer element of OrcaSlicer. heightKey is print_z or
// top_z.
func layerChangeOf(e xml.StartElement, heightKey string) (LayerChange, bool) {
	get := func(name string) (string, bool) {
		for _, a := range e.Attr {
			if a.Name.Local == name {
				return a.Value, true
			}
		}

		return "", false
	}

	height, err := strconv.ParseFloat(strings.TrimSpace(attrOr(get, heightKey)), 64)
	if err != nil || !finite(height) {
		return LayerChange{}, false
	}

	extruder, _ := strconv.Atoi(strings.TrimSpace(attrOr(get, "extruder")))
	change := LayerChange{Height: height, Extruder: max(extruder, 0), Color: attrOr(get, "color")}

	if typ, hasType := get("type"); hasType {
		n, errType := strconv.Atoi(strings.TrimSpace(typ))
		if errType != nil {
			return LayerChange{}, false
		}

		kind, known := layerKind(n)
		if !known {
			return LayerChange{}, false
		}

		change.Kind, change.Extra = kind, attrOr(get, "extra")
	} else {
		change.Kind, change.Extra = legacyLayerKind(attrOr(get, "gcode"), change.Color)
	}

	return tidyChange(change), true
}

func attrOr(get func(string) (string, bool), name string) string {
	v, _ := get(name)

	return v
}

// tidyChange keeps a colour only on a change that has one.
func tidyChange(c LayerChange) LayerChange {
	switch c.Kind {
	case LayerChangeColor, LayerChangeTool:
		c.Color = normalColor(c.Color)
	case LayerChangePause, LayerChangeTemplate, LayerChangeCustom:
		c.Color = ""
	}

	return c
}

// sortChanges orders changes by height, keeping the file's order for equal
// heights.
func sortChanges(changes []LayerChange) {
	slices.SortStableFunc(changes, func(a, b LayerChange) int { return cmpFloat(a.Height, b.Height) })
}

// ps2LayerChanges reads Metadata/Prusa_Slicer_custom_gcode_per_print_z.xml
// by bed index. PrusaSlicer 2.9 writes one custom_gcodes_per_print_z element
// per bed, side by side at the top of the file, each with a bed_idx. A file
// from before 2.9 has one, with no bed_idx, for bed 0.
func ps2LayerChanges(data []byte) (map[int][]LayerChange, bool) {
	out := map[int][]LayerChange{}
	bed := -1

	ok := eachElement(data, func(t xml.Token) {
		start, isStart := t.(xml.StartElement)
		if !isStart {
			return
		}

		switch start.Name.Local {
		case "custom_gcodes_per_print_z":
			bed = 0
			if n, found := intAttr(start, "bed_idx"); found && n >= 0 {
				bed = n
			}
		case "code":
			if change, read := layerChangeOf(start, "print_z"); read && bed >= 0 {
				out[bed] = append(out[bed], change)
			}
		}
	})

	return out, ok
}

// orcaLayerChanges reads Metadata/custom_gcode_per_layer.xml by plate id.
// The changes of each plate are in a plate element named by its plate_info.
// A file with no plate element holds the changes of plate 1:
// _extract_custom_gcode_per_print_z_from_archive of OrcaSlicer.
func orcaLayerChanges(data []byte) (map[int][]LayerChange, bool) {
	out := map[int][]LayerChange{}
	plate := orcaChangePlate{id: 1}

	ok := eachElement(data, func(t xml.Token) {
		switch e := t.(type) {
		case xml.StartElement:
			plate.start(e)
		case xml.EndElement:
			if e.Name.Local == "plate" && plate.open {
				plate.flush(out)
			}
		}
	})

	if !plate.open {
		plate.id = 1
		plate.flush(out)
	}

	return out, ok
}

// orcaChangePlate collects the changes of one plate element.
type orcaChangePlate struct {
	open    bool
	id      int
	pending []LayerChange
}

func (p *orcaChangePlate) start(e xml.StartElement) {
	switch e.Name.Local {
	case "plate":
		*p = orcaChangePlate{open: true, id: -1}
	case "plate_info":
		if n, found := intAttr(e, "id"); found {
			p.id = n
		}
	case "layer":
		if change, read := layerChangeOf(e, "top_z"); read {
			p.pending = append(p.pending, change)
		}
	}
}

func (p *orcaChangePlate) flush(out map[int][]LayerChange) {
	if p.id > 0 && len(p.pending) > 0 {
		out[p.id] = append(out[p.id], p.pending...)
	}

	*p = orcaChangePlate{}
}

// eachElement calls f for every token of an XML document, and reports
// whether the whole document was read. It takes a document with several
// top elements, as PrusaSlicer 2.9 writes.
func eachElement(data []byte, f func(xml.Token)) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return true
		}

		if err != nil {
			return false
		}

		f(tok)
	}
}

// intAttr reads a whole number attribute.
func intAttr(e xml.StartElement, name string) (int, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			n, err := strconv.Atoi(strings.TrimSpace(a.Value))

			return n, err == nil
		}
	}

	return 0, false
}

// parseLayerRanges reads the layer ranges of every object, by the object's
// position in the project counted from 1. PrusaSlicer 2.x writes them into
// Metadata/Prusa_Slicer_layer_config_ranges.xml and OrcaSlicer into
// Metadata/layer_config_ranges.xml, both as objects of ranges of options:
// _extract_layer_config_ranges_from_archive of PrusaSlicer 2.9.6 and of
// OrcaSlicer.
func parseLayerRanges(data []byte) (map[int][]layerRange, bool) {
	out := map[int][]layerRange{}

	var doc struct {
		Objects []struct {
			ID     int `xml:"id,attr"`
			Ranges []struct {
				MinZ    float64 `xml:"min_z,attr"`
				MaxZ    float64 `xml:"max_z,attr"`
				Options []struct {
					Key   string `xml:"opt_key,attr"`
					Value string `xml:",chardata"`
				} `xml:"option"`
			} `xml:"range"`
		} `xml:"object"`
	}

	if xml.NewDecoder(bytes.NewReader(data)).Decode(&doc) != nil {
		return out, false
	}

	for _, o := range doc.Objects {
		if o.ID <= 0 {
			continue
		}

		for _, r := range o.Ranges {
			if !finite(r.MinZ) || !finite(r.MaxZ) {
				continue
			}

			values := make(map[string]string, len(r.Options))
			for _, opt := range r.Options {
				values[opt.Key] = strings.TrimSpace(opt.Value)
			}

			out[o.ID] = append(out[o.ID], layerRange{
				minZ: r.MinZ, maxZ: r.MaxZ, values: values, get: textGetter(values),
			})
		}
	}

	for id := range out {
		slices.SortStableFunc(out[id], func(a, b layerRange) int { return cmpFloat(a.minZ, b.minZ) })
	}

	return out, true
}

// readLayerRanges reads a layer ranges entry, if the archive has it.
func readLayerRanges(rd *reading, name string) (map[int][]layerRange, error) {
	f := rd.a.entry(name)
	if f == nil {
		return map[int][]layerRange{}, nil
	}

	data, err := readAll(rd.ctx, f, rd.limits.MaxMetadataBytes, "MaxMetadataBytes")
	if err != nil {
		return nil, err
	}

	ranges, ok := parseLayerRanges(data)
	if !ok {
		rd.issue(IssueSettingsUnreadable, name)
	}

	return ranges, nil
}
