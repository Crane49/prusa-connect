package printfile

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
)

// The keys a slicer writes its settings under. A text G-code file writes them
// as `; key = value` comments and a binary file writes the same keys into its
// metadata blocks, so one table serves both.
const (
	keyEstimatedNormal = "estimated printing time (normal mode)"
	keyEstimatedPlain  = "estimated printing time"
	keyFilamentMM      = "filament used [mm]"
	keyFilamentG       = "filament used [g]"
	keyTotalFilamentG  = "total filament used [g]"
	keyLayerHeight     = "layer_height"
	keyNozzleDiameter  = "nozzle_diameter"
	keyFilamentType    = "filament_type"
	keyPrinterModel    = "printer_model"
	keyPrintProfile    = "print_settings_id"
	keyPrinterProfile  = "printer_settings_id"
	keyFilamentProfile = "filament_settings_id"
	keyHighFlow        = "nozzle_high_flow"

	// The keys below are lists with one entry per tool.
	keyFilamentAbrasive         = "filament_abrasive"
	keyTemperature              = "temperature"
	keyFirstLayerTemperature    = "first_layer_temperature"
	keyBedTemperature           = "bed_temperature"
	keyFirstLayerBedTemperature = "first_layer_bed_temperature"
	keyExtruderColour           = "extruder_colour"
	keyFilamentColour           = "filament_colour"

	keyMaxLayerZ      = "max_layer_z"
	keyObjectsInfo    = "objects_info"
	keyBedShape       = "bed_shape"
	keyMaxPrintHeight = "max_print_height"
)

// wanted is the whole set of keys this package reads. A key outside it is
// dropped where it is read, so a file of two hundred thousand settings costs
// the same as a file of nine: nothing is kept that nothing asks for.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var wanted = map[string]struct{}{
	keyEstimatedNormal:          {},
	keyEstimatedPlain:           {},
	keyFilamentMM:               {},
	keyFilamentG:                {},
	keyTotalFilamentG:           {},
	keyLayerHeight:              {},
	keyNozzleDiameter:           {},
	keyFilamentType:             {},
	keyPrinterModel:             {},
	keyPrintProfile:             {},
	keyPrinterProfile:           {},
	keyFilamentProfile:          {},
	keyHighFlow:                 {},
	keyFilamentAbrasive:         {},
	keyTemperature:              {},
	keyFirstLayerTemperature:    {},
	keyBedTemperature:           {},
	keyFirstLayerBedTemperature: {},
	keyExtruderColour:           {},
	keyFilamentColour:           {},
	keyMaxLayerZ:                {},
	keyObjectsInfo:              {},
	keyBedShape:                 {},
	keyMaxPrintHeight:           {},
}

// maxKeyBytes is the length of the longest key in wanted. A key longer than it
// cannot be one. It is dropped before it is lowercased, so a quarter megabyte
// of comment lines is not lowercased line by line for nothing.
const maxKeyBytes = len(keyEstimatedNormal)

// maxValueBytes bounds one value before it is kept. Every value this package
// reads is a number, a material name, a printer name, or a list of those with
// one entry per filament. A list is the longest of them, and this holds a few
// hundred entries, so it also bounds how many filaments one file describes.
const maxValueBytes = 4 << 10

// maxObjectsInfoBytes bounds the one value that is larger: the outline of
// every object on the bed. A plate of a few dozen objects fits, and a value
// longer than this is dropped unparsed.
const maxObjectsInfoBytes = 16 << 10

// The units a slicer writes a duration in.
const (
	secondsPerMinute = 60
	secondsPerHour   = 60 * secondsPerMinute
	secondsPerDay    = 24 * secondsPerHour
)

// maxDurationDigits is how many digits one number inside a duration may have.
// A print of a thousand million hours is not a print, and reading one would
// overflow the total.
const maxDurationDigits = 9

// decimalBase is the base a duration's digits are read in.
const decimalBase = 10

// collector gathers key and value pairs from wherever they came from and
// renders the Details at the end.
//
// The first value for a key wins, because a text G-code file repeats several of
// these keys: the slicer writes what the print used near the end and then the
// whole configuration after it, and the first spelling is the one about this
// print.
type collector struct {
	values map[string]string
}

func newCollector() *collector {
	return &collector{values: make(map[string]string, len(wanted))}
}

// put records one pair, keeping the first value seen for a key. A key nothing
// reads is dropped, so the map never holds more than len(wanted) entries.
func (c *collector) put(key, value string) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxKeyBytes {
		return
	}

	key = strings.ToLower(key)
	if _, ours := wanted[key]; !ours {
		return
	}

	value = strings.TrimSpace(value)
	if value == "" || len(value) > valueBound(key) {
		return
	}

	if _, seen := c.values[key]; seen {
		return
	}

	c.values[key] = value
}

// putLine records one `key = value` line. A line with no `=` holds no pair and
// is dropped.
func (c *collector) putLine(line string) {
	key, value, found := strings.Cut(line, "=")
	if !found {
		return
	}

	c.put(key, value)
}

// putTable records every line of a settings table, which both the text and the
// binary format write as one `key=value` per line.
func (c *collector) putTable(text string) {
	for line := range strings.SplitSeq(text, "\n") {
		c.putLine(strings.TrimSpace(strings.TrimSuffix(line, "\r")))
	}
}

// valueBound is the longest value kept for one key.
func valueBound(key string) int {
	if key == keyObjectsInfo {
		return maxObjectsInfoBytes
	}

	return maxValueBytes
}

// details renders what was gathered.
func (c *collector) details() Details {
	filaments := c.filaments()
	objects := labelledObjects(c.values[keyObjectsInfo])

	out := Details{
		EstimatedSeconds: seconds(c.first(keyEstimatedNormal, keyEstimatedPlain)),
		FilamentMM:       totalOf(filaments, func(f Filament) float64 { return f.FilamentMM }, c.values[keyFilamentMM]),
		FilamentG: totalOf(filaments, func(f Filament) float64 { return f.FilamentG },
			c.first(keyFilamentG, keyTotalFilamentG)),
		LayerHeightMM:   number(c.values[keyLayerHeight]),
		NozzleMM:        number(firstField(c.values[keyNozzleDiameter])),
		Material:        firstField(c.values[keyFilamentType]),
		PrinterModel:    firstField(c.values[keyPrinterModel]),
		Filaments:       filaments,
		MaxLayerZMM:     number(c.values[keyMaxLayerZ]),
		Objects:         objects,
		Bounds:          boundsOf(objects),
		Bed:             bedOf(c.values[keyBedShape], c.values[keyMaxPrintHeight]),
		PrintProfile:    firstProfile(c.values[keyPrintProfile]),
		PrinterProfile:  firstProfile(c.values[keyPrinterProfile]),
		FilamentProfile: firstProfile(c.values[keyFilamentProfile]),
		HighFlow:        firstField(c.values[keyHighFlow]) == "1",
	}

	return out
}

// first returns the value of the first key that has one.
func (c *collector) first(keys ...string) string {
	for _, key := range keys {
		if value := c.values[key]; value != "" {
			return value
		}
	}

	return ""
}

// maxFieldBytes bounds a value this package keeps out of a file, so a name a
// slicer never wrote cannot become a long string in an API response. It is the
// maxLength the API schema publishes for the same field, so the cut has to
// leave room for the marker machinetext.Clip adds.
const maxFieldBytes = 128

// firstField takes the first entry of a list a multi-tool file writes, which
// the slicer separates with a semicolon or a comma, and bounds its length.
//
// A single-tool file writes one entry and this returns it unchanged.
func firstField(value string) string {
	value = strings.TrimSpace(value)

	if cut := strings.IndexAny(value, ";,"); cut >= 0 {
		value = value[:cut]
	}

	return cleanField(value)
}

// cleanField takes the quotes a slicer writes around a name off one entry,
// and bounds its length.
func cleanField(value string) string {
	value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"`))

	// A slicer writes UTF-8. Bytes that are not UTF-8 came from something else,
	// and a JSON encoder would turn each of them into a replacement character,
	// so the value is dropped.
	if !utf8.ValidString(value) {
		return ""
	}

	return machinetext.Clip(value, maxFieldBytes-len(machinetext.Marker))
}

// firstProfile takes the first profile name of a list a multi-tool file
// writes. A slicer separates profiles with a semicolon only: a name may hold
// a comma.
func firstProfile(value string) string {
	first, _, _ := strings.Cut(value, ";")

	return cleanField(first)
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

// seconds reads a duration a slicer wrote as `1d 2h 3m 4s`, in whole seconds.
// Any part may be missing. It answers zero for anything it cannot read.
func seconds(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	var total int64

	digits := 0
	written := 0
	found := false

	for i := range len(value) {
		c := value[i]

		if c >= '0' && c <= '9' {
			// The count of digits is checked, not the value they make. A
			// limit on the value would refuse a valid time such as "100m".
			if written >= maxDurationDigits {
				return 0
			}

			digits = digits*decimalBase + int(c-'0')
			written++

			continue
		}

		unit, ok := unitSeconds(c)
		if !ok {
			if c == ' ' {
				continue
			}

			return 0
		}

		total += int64(digits) * unit
		digits, written = 0, 0
		found = true
	}

	if !found || digits != 0 {
		return 0
	}

	return total
}

// unitSeconds is how long one of the letters a slicer writes a duration with
// lasts.
func unitSeconds(c byte) (int64, bool) {
	switch c {
	case 'd':
		return secondsPerDay, true
	case 'h':
		return secondsPerHour, true
	case 'm':
		return secondsPerMinute, true
	case 's':
		return 1, true
	}

	return 0, false
}
