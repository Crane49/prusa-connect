// Package colour sorts a #rrggbb[aa] colour into one of twelve families, the
// way the swatches of the add dialog filter.
package colour

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

var families = []string{
	"black",
	"white",
	"grey",
	"red",
	"orange",
	"yellow",
	"green",
	"blue",
	"purple",
	"pink",
	"brown",
	"natural",
}

// IsFamily reports whether name is one of the twelve family names.
func IsFamily(name string) bool { return slices.Contains(families, name) }

// Valid is true for #rrggbb or #rrggbbaa, the only colour a page may draw.
func Valid(rgba string) bool {
	if len(rgba) != 7 && len(rgba) != 9 || rgba[0] != '#' {
		return false
	}

	return !strings.ContainsFunc(rgba[1:], func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF", r)
	})
}

// Family is the family of a #rrggbb or #rrggbbaa colour, "" for anything else.
//
//nolint:mnd // the byte, hue and lightness cut-offs are the table; naming each would hide it.
func Family(rgba string) string {
	if len(rgba) != 7 && len(rgba) != 9 || rgba[0] != '#' {
		return ""
	}

	v, err := strconv.ParseUint(rgba[1:], 16, 32)
	if err != nil {
		return ""
	}

	alpha := uint64(0xff)
	if len(rgba) == 9 {
		alpha, v = v&0xff, v>>8
	}

	if alpha < 0x80 {
		return "natural"
	}

	r, g, b := float64(v>>16&0xff)/255, float64(v>>8&0xff)/255, float64(v&0xff)/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l, d := (hi+lo)/2, hi-lo

	switch {
	case l < 0.15:
		return "black"
	case l > 0.90:
		return "white"
	case d == 0 || d/(1-math.Abs(2*l-1)) < 0.15:
		return "grey"
	}

	var h float64

	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}

	if h *= 60; h < 0 {
		h += 360
	}

	switch {
	case h < 10 || h >= 345:
		return "red"
	case h < 40 && l < 0.45:
		return "brown"
	case h < 40:
		return "orange"
	case h < 70:
		return "yellow"
	case h < 170:
		return "green"
	case h < 255:
		return "blue"
	case h < 315:
		return "purple"
	}

	return "pink"
}
