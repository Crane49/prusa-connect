package printfile

import (
	"math"
	"strconv"
	"strings"
)

// The list separators a slicer writes. A list of names is separated by
// semicolons, because a name may hold a comma, and a list of numbers by
// commas. The firmware reads them the same way (GCodeInfo::parse_comment in
// src/common/gcode/gcode_info.cpp).
const (
	nameSeparator   = ";"
	numberSeparator = ","
)

// filaments renders what the file says about each filament, T0 first. The
// number of filaments is the longest per-filament list the file carries. There
// is no count bound: maxValueBytes bounds each list, and so the entries.
func (c *collector) filaments() []Filament {
	names := func(key string) []string { return items(c.values[key], nameSeparator) }
	numbers := func(key string) []string { return items(c.values[key], numberSeparator) }

	materials := names(keyFilamentType)
	colours := names(keyExtruderColour)
	fallbackColours := names(keyFilamentColour)
	nozzles := numbers(keyNozzleDiameter)
	highFlow := numbers(keyHighFlow)
	abrasive := numbers(keyFilamentAbrasive)
	temperature := numbers(keyTemperature)
	firstLayerTemperature := numbers(keyFirstLayerTemperature)
	bed := numbers(keyBedTemperature)
	firstLayerBed := numbers(keyFirstLayerBedTemperature)
	grams := numbers(keyFilamentG)
	millimetres := numbers(keyFilamentMM)

	count := max(len(materials), len(nozzles), len(highFlow), len(abrasive),
		len(temperature), len(firstLayerTemperature), len(bed), len(firstLayerBed),
		len(grams), len(millimetres))
	if count == 0 {
		return nil
	}

	out := make([]Filament, count)

	for i := range out {
		filament := Filament{
			Index:                 i,
			Material:              cleanField(at(materials, i)),
			NozzleMM:              number(at(nozzles, i)),
			HighFlow:              flag(at(highFlow, i)),
			Abrasive:              flag(at(abrasive, i)),
			NozzleTempC:           number(at(temperature, i)),
			FirstLayerNozzleTempC: number(at(firstLayerTemperature, i)),
			BedTempC:              number(at(bed, i)),
			FirstLayerBedTempC:    number(at(firstLayerBed, i)),
			FilamentG:             number(at(grams, i)),
			FilamentMM:            number(at(millimetres, i)),
			Colour:                colourOf(at(colours, i), at(fallbackColours, i)),
		}

		// The firmware counts a filament as used by the grams alone, and a file
		// that does not say the grams uses none as far as it can tell.
		if i < len(grams) {
			used := filament.FilamentG > FilamentUsedMinG
			filament.Used = &used
		}

		out[i] = filament
	}

	return out
}

// totalOf is how much filament the whole print uses: the sum over the
// filaments where the file lists it per filament, and otherwise the single
// figure it wrote.
//
// A multi-filament file lists one figure per filament, and the first of them
// is the share of one filament, not the whole print.
func totalOf(filaments []Filament, per func(Filament) float64, single string) float64 {
	sum, listed := 0.0, false

	for _, filament := range filaments {
		if value := per(filament); value > 0 {
			sum += value
			listed = true
		}
	}

	if listed {
		return math.Round(sum*hundredths) / hundredths
	}

	return number(single)
}

// hundredths is the precision a summed figure is rounded to, which is the
// precision the slicer writes each figure in.
const hundredths = 100

// items splits one list value into its entries. An empty value is no list.
func items(value, separator string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return strings.Split(value, separator)
}

// at is the entry at i, and empty past the end of the list.
func at(list []string, i int) string {
	if i < len(list) {
		return strings.TrimSpace(list[i])
	}

	return ""
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

// labelledObjects reads the objects the slicer labelled, with their names and
// outlines, from objects_info. It answers nil for a file that wrote none or
// one this package cannot read.
func labelledObjects(value string) []LabelledObject {
	if value == "" {
		return nil
	}

	objects, err := parseObjectsInfo(value)
	if err != nil || len(objects) == 0 {
		return nil
	}

	return objects
}

// boundsOf is the rectangle every object outline fits in, and nil when no
// outline has a point.
func boundsOf(objects []LabelledObject) *Bounds {
	var (
		out   Bounds
		found bool
	)

	for _, object := range objects {
		for _, point := range object.Polygon {
			if !found {
				out = Bounds{MinXMM: point.X, MaxXMM: point.X, MinYMM: point.Y, MaxYMM: point.Y}
				found = true

				continue
			}

			out.MinXMM, out.MaxXMM = min(out.MinXMM, point.X), max(out.MaxXMM, point.X)
			out.MinYMM, out.MaxYMM = min(out.MinYMM, point.Y), max(out.MaxYMM, point.Y)
		}
	}

	if !found {
		return nil
	}

	return &out
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// bedOf reads the bed the printer profile describes. The shape is a list of
// corners such as 0x0,250x0,250x210,0x210, and the size is the rectangle they
// span.
func bedOf(shape, height string) Bed {
	out := Bed{HeightMM: number(height)}

	var (
		minX, minY, maxX, maxY float64
		found                  bool
	)

	for _, corner := range items(shape, numberSeparator) {
		left, right, ok := strings.Cut(strings.TrimSpace(corner), "x")
		if !ok {
			continue
		}

		x, y := signed(left), signed(right)
		if x == nil || y == nil {
			continue
		}

		if !found {
			minX, maxX, minY, maxY = *x, *x, *y, *y
			found = true

			continue
		}

		minX, maxX = min(minX, *x), max(maxX, *x)
		minY, maxY = min(minY, *y), max(maxY, *y)
	}

	if found {
		out.WidthMM, out.DepthMM = maxX-minX, maxY-minY
	}

	return out
}

// signed reads a decimal that may be negative, and answers nil for anything
// that is not a finite number.
func signed(value string) *float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || !finite(parsed) {
		return nil
	}

	return &parsed
}
