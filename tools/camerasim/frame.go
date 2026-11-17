package camerasim

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// counterBits is how wide the machine-readable counter is. Thirty-two bits
	// is more than a simulator can ever reach at any believable frame rate, and
	// it fits in 256 pixels, which is inside the smallest frame worth serving.
	counterBits = 32
	// blockSize is the side of one counter block in pixels. Eight matches JPEG's
	// own block grid, so the encoder never spreads one bit's edge into the next
	// bit's centre however low the quality is set.
	blockSize = 8
	// blockThreshold splits a block into a one or a zero. It is the midpoint of
	// the luminance range: the blocks are drawn pure white and pure black, so
	// only a badly damaged frame lands anywhere near it.
	blockThreshold = 128
	// blockCentre is the pixel the readout samples inside a block: as far from
	// every edge as the block allows.
	blockCentre = blockSize / 2

	// minFrameWidth is the width the counter readout needs, and so the width
	// below which no frame is served.
	minFrameWidth = counterBits * blockSize
	// minFrameHeight leaves room for the readout row, four lines of text, the
	// progress bar and the moving bar.
	minFrameHeight = 128

	// backgroundGoroutines is how many goroutines Start adds to the wait group:
	// the HTTP server and the renderer.
	backgroundGoroutines = 2

	// timestampLayout is the RFC 3339 instant in UTC, which is what every log
	// line in this project already carries, so a frame photographed off a screen
	// can be lined up against one.
	timestampLayout = "2006-01-02T15:04:05Z"
)

const (
	// The 5x7 font's geometry. glyphAdvance is one column of space between
	// glyphs, so text stays legible at scale one.
	glyphCols    = 5
	glyphRows    = 7
	glyphAdvance = glyphCols + 1
	// textScale magnifies every glyph pixel into a square of this side. Two is
	// the smallest that survives a JPEG at quality 70 and a browser scaling the
	// frame down into a grid tile.
	textScale = 2
	// textMargin is the gap between the readout row, the text lines, and each
	// other.
	textMargin = 4
	lineHeight = glyphRows*textScale + textMargin
)

const (
	// The moving bar. It is the part a person watches: a stream that has frozen
	// looks identical to one that is working until something moves.
	barHeight = 20
	barWidth  = 48
	barStep   = 3

	// The background cycles through hues so a frozen stream is visible even when
	// the bar happens to sit at the edge. tau is a full turn; the three channels
	// are a third of a turn apart, which is what makes the cycle read as a hue
	// rather than as a brightness pulse.
	tau          = 2 * math.Pi
	channelPhase = tau / 3
	huePerFrame  = 0.04
	hueBase      = 70.0
	hueAmplitude = 45.0

	maxByte     = 255
	alphaOpaque = 255

	// The bar's colour, an amber that stays distinct from every hue the
	// background cycles through.
	barGreen = 190
	barBlue  = 40

	// The progress bar. It sits above the moving bar and spans the frame, and it
	// is drawn with a border so a print at zero percent is still a bar rather
	// than nothing.
	progressHeight = 14
	progressBorder = 2
	// progressGap separates the progress bar from the moving bar below it. It is
	// two margins rather than one so the two never read as a single thick bar.
	progressGap = textMargin + textMargin
	// fullPercent is the top of the progress scale. A figure above it is drawn
	// full rather than past the end of the frame.
	fullPercent = 100
)

// The fixed colours. They are values rather than constants only because Go has
// no constant structs.
var (
	colourWhite = color.RGBA{R: maxByte, G: maxByte, B: maxByte, A: alphaOpaque}
	colourBlack = color.RGBA{R: 0, G: 0, B: 0, A: alphaOpaque}
	colourBar   = color.RGBA{R: maxByte, G: barGreen, B: barBlue, A: alphaOpaque}
)

// font is a hand-rolled 5x7 bitmap covering the digits, the punctuation an
// RFC 3339 timestamp uses, the percent sign the progress figure needs, and the
// uppercase letters.
//
// It is here rather than from a font package because the only text this draws
// is a timestamp, a frame number, a label and one state line, and a dependency
// on a font library to draw forty-one glyphs would be the largest thing in the
// simulator. Each glyph is five column bytes, least significant bit at the top
// row, so 0x7F is a full-height vertical stroke.
var font = map[rune][glyphCols]byte{
	' ': {0x00, 0x00, 0x00, 0x00, 0x00},
	'-': {0x08, 0x08, 0x08, 0x08, 0x08},
	'.': {0x00, 0x00, 0x40, 0x00, 0x00},
	':': {0x00, 0x00, 0x24, 0x00, 0x00},
	'%': {0x63, 0x13, 0x08, 0x64, 0x63},
	'0': {0x3E, 0x51, 0x49, 0x45, 0x3E},
	'1': {0x00, 0x42, 0x7F, 0x40, 0x00},
	'2': {0x42, 0x61, 0x51, 0x49, 0x46},
	'3': {0x21, 0x41, 0x45, 0x4B, 0x31},
	'4': {0x18, 0x14, 0x12, 0x7F, 0x10},
	'5': {0x27, 0x45, 0x45, 0x45, 0x39},
	'6': {0x3C, 0x4A, 0x49, 0x49, 0x30},
	'7': {0x01, 0x71, 0x09, 0x05, 0x03},
	'8': {0x36, 0x49, 0x49, 0x49, 0x36},
	'9': {0x06, 0x49, 0x49, 0x29, 0x1E},
	'A': {0x7E, 0x11, 0x11, 0x11, 0x7E},
	'B': {0x7F, 0x49, 0x49, 0x49, 0x36},
	'C': {0x3E, 0x41, 0x41, 0x41, 0x22},
	'D': {0x7F, 0x41, 0x41, 0x22, 0x1C},
	'E': {0x7F, 0x49, 0x49, 0x49, 0x41},
	'F': {0x7F, 0x09, 0x09, 0x09, 0x01},
	'G': {0x3E, 0x41, 0x49, 0x49, 0x7A},
	'H': {0x7F, 0x08, 0x08, 0x08, 0x7F},
	'I': {0x00, 0x41, 0x7F, 0x41, 0x00},
	'J': {0x20, 0x40, 0x41, 0x3F, 0x01},
	'K': {0x7F, 0x08, 0x14, 0x22, 0x41},
	'L': {0x7F, 0x40, 0x40, 0x40, 0x40},
	'M': {0x7F, 0x02, 0x0C, 0x02, 0x7F},
	'N': {0x7F, 0x04, 0x08, 0x10, 0x7F},
	'O': {0x3E, 0x41, 0x41, 0x41, 0x3E},
	'P': {0x7F, 0x09, 0x09, 0x09, 0x06},
	'Q': {0x3E, 0x41, 0x51, 0x21, 0x5E},
	'R': {0x7F, 0x09, 0x19, 0x29, 0x46},
	'S': {0x46, 0x49, 0x49, 0x49, 0x31},
	'T': {0x01, 0x01, 0x7F, 0x01, 0x01},
	'U': {0x3F, 0x40, 0x40, 0x40, 0x3F},
	'V': {0x1F, 0x20, 0x40, 0x20, 0x1F},
	'W': {0x3F, 0x40, 0x38, 0x40, 0x3F},
	'X': {0x63, 0x14, 0x08, 0x14, 0x63},
	'Y': {0x07, 0x08, 0x70, 0x08, 0x07},
	'Z': {0x61, 0x51, 0x49, 0x45, 0x43},
}

// renderFrame draws one frame.
//
// The order matters: the counter blocks go on last, so nothing a later change
// adds to this function can ever paint over the one part of the image a machine
// reads.
func renderFrame(opts Options, status Status, counter uint64, now time.Time) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))

	fill(img, img.Bounds(), backgroundColour(counter))
	drawBar(img, counter)
	drawProgress(img, status)
	drawCaption(img, opts, status, counter, now)
	drawCounterBlocks(img, counter)

	return img
}

// backgroundColour shifts the background hue with the frame counter, so a
// stopped stream is obvious at a glance and a slow one is obvious from how far
// the colour has travelled between two frames.
func backgroundColour(counter uint64) color.RGBA {
	phase := float64(counter) * huePerFrame

	return color.RGBA{
		R: wave(phase),
		G: wave(phase + channelPhase),
		B: wave(phase + channelPhase + channelPhase),
		A: alphaOpaque,
	}
}

// wave is one colour channel of the background cycle, clamped into a byte.
func wave(phase float64) uint8 {
	value := hueBase + hueAmplitude*math.Sin(phase)

	if value <= 0 {
		return 0
	}

	if value >= maxByte {
		return maxByte
	}

	return uint8(value)
}

// drawBar draws the coloured bar that slides across the frame with the counter.
func drawBar(img *image.RGBA, counter uint64) {
	span := img.Bounds().Dx() - barWidth
	if span <= 0 {
		return
	}

	x := int(math.Mod(float64(counter)*barStep, float64(span)))
	y := img.Bounds().Dy() - barHeight - textMargin

	fill(img, image.Rect(x, y, x+barWidth, y+barHeight), colourBar)
}

// drawProgress draws how far the print has gone, as a bordered bar above the
// moving bar. It draws nothing for a camera that watches no printer.
//
// The border is drawn whatever the figure is, so a print at zero percent reads
// as a bar that has not filled rather than as a camera that lost its printer.
func drawProgress(img *image.RGBA, status Status) {
	if status.State == "" {
		return
	}

	bottom := img.Bounds().Dy() - barHeight - progressGap
	top := bottom - progressHeight

	left := textMargin
	right := img.Bounds().Dx() - textMargin

	if top <= 0 || right <= left {
		return
	}

	fill(img, image.Rect(left, top, right, bottom), colourBlack)

	inner := image.Rect(left+progressBorder, top+progressBorder,
		right-progressBorder, bottom-progressBorder)

	fill(img, inner, colourWhite)

	filled := inner.Dx() * clampPercent(status.Progress) / fullPercent
	fill(img, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+filled, inner.Max.Y), colourBar)
}

// clampPercent holds a figure to the scale the bar is drawn on.
func clampPercent(percent int) int {
	return min(max(percent, 0), fullPercent)
}

// stateLine is what the printer is doing, as one line of the caption. It is
// empty for a camera that watches no printer, and an empty line is skipped.
func stateLine(status Status) string {
	if status.State == "" {
		return ""
	}

	return strings.ToUpper(status.State) + " " + strconv.Itoa(clampPercent(status.Progress)) + "%"
}

// drawCaption draws the lines a person reads: the instant the frame was
// rendered, the frame number, whatever the operator labelled this camera, and
// what the printer is doing.
func drawCaption(img *image.RGBA, opts Options, status Status, counter uint64, now time.Time) {
	top := blockSize + textMargin

	lines := []string{
		now.Format(timestampLayout),
		"FRAME " + strconv.FormatUint(counter, 10),
		strings.ToUpper(opts.Label),
		stateLine(status),
	}

	for i, line := range lines {
		if line == "" {
			continue
		}

		// A dark shadow one pixel down and right, because the background hue
		// travels through colours that white text alone disappears into.
		drawText(img, textMargin+1, top+i*lineHeight+1, line, colourBlack)
		drawText(img, textMargin, top+i*lineHeight, line, colourWhite)
	}
}

// drawCounterBlocks writes the counter along the top edge as black and white
// squares, most significant bit on the left. This is the readout ReadCounter
// samples, and it is the reason a test can assert on frame freshness.
func drawCounterBlocks(img *image.RGBA, counter uint64) {
	for bit := range counterBits {
		shift := counterBits - 1 - bit

		colour := colourBlack
		if counter>>shift&1 == 1 {
			colour = colourWhite
		}

		x := bit * blockSize
		fill(img, image.Rect(x, 0, x+blockSize, blockSize), colour)
	}
}

// drawText draws a string in the 5x7 font with its top left corner at x, y.
// Characters the font does not carry are skipped rather than substituted, so a
// missing glyph shows as a gap instead of as a wrong character.
func drawText(img *image.RGBA, x, y int, text string, colour color.RGBA) {
	for i, r := range text {
		glyph, ok := font[r]
		if !ok {
			continue
		}

		drawGlyph(img, x+i*glyphAdvance*textScale, y, glyph, colour)
	}
}

// drawGlyph paints one glyph, magnified by textScale.
func drawGlyph(img *image.RGBA, x, y int, glyph [glyphCols]byte, colour color.RGBA) {
	for col, bits := range glyph {
		for row := range glyphRows {
			if bits>>row&1 == 0 {
				continue
			}

			left := x + col*textScale
			topRow := y + row*textScale

			fill(img, image.Rect(left, topRow, left+textScale, topRow+textScale), colour)
		}
	}
}

// fill paints a rectangle, clipped to the image.
func fill(img *image.RGBA, rect image.Rectangle, colour color.RGBA) {
	draw.Draw(img, rect.Intersect(img.Bounds()), image.NewUniform(colour), image.Point{}, draw.Src)
}
