package account

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The avatar is a picture of the person's initials, which PrusaSlicer shows
// beside their name. PrusaSlicer downloads avatar_small without a credential,
// so the avatar route is public: module.yaml lists it under routes.public.
// The query alone decides the picture, and the route reads nothing the host
// holds, so it shows nobody more than the address already says.
//
// The me reply cannot name the server's address: the host passes the module
// only the path below its mount. It answers a relative reference instead,
// such as avatar.png?i=D&c=fa6831. PrusaSlicer resolves it against the me
// address, which ends in a slash (RFC 3986 section 5.2), so it fetches
// <mount>/account/api/v1/me/avatar.png?i=D&c=fa6831.
const (
	// PathAvatar is the avatar route, relative to the mount.
	PathAvatar = "/account/api/v1/me/avatar.png"
	// avatarRef is the avatar route relative to the me route.
	avatarRef = "avatar.png"
	// The query keys: the initials and the colour.
	queryInitials = "i"
	queryColour   = "c"
	// maxInitials is how many letters the picture holds.
	maxInitials = 2
	// avatarSize is the width and the height of the picture, in pixels, and
	// avatarCentre is the middle of either.
	avatarSize   = 128
	avatarCentre = avatarSize / 2
	// The letter size in points, at the DPI that makes a point one pixel.
	oneLetterSize  = 60
	twoLettersSize = 50
	fontDPI        = 72
	// avatarCache is how long a client may keep the picture: a year. The
	// address decides every pixel, so the picture behind it never changes.
	avatarCache = "public, max-age=31536000, immutable"
)

// palette is the colours the background may take. Each is dark enough for white
// letters, and the route refuses any other colour.
//
//nolint:gochecknoglobals // a closed table read on every request, never written.
var palette = [...]string{
	"fa6831", // the Prusa orange
	"2563eb",
	"0d9488",
	"7c3aed",
	"db2777",
	"16a34a",
	"b45309",
	"475569",
}

// avatarRefFor is the relative avatar_small of the person with this display
// name and subject, or "" when the name holds no letter to draw.
func avatarRefFor(name, subject string) string {
	initials := initialsOf(name)
	if initials == "" {
		return ""
	}

	query := url.Values{queryInitials: {initials}, queryColour: {colourOf(subject)}}

	return avatarRef + "?" + query.Encode()
}

// initialsOf is the first letter of each of the first two words of name that
// hold a letter, in upper case. A word with no letter, such as "(viewer)"
// begins with, counts from its first letter.
func initialsOf(name string) string {
	var initials []rune

	for word := range strings.FieldsSeq(name) {
		i := strings.IndexFunc(word, unicode.IsLetter)
		if i < 0 {
			continue
		}

		first, _ := utf8.DecodeRuneInString(word[i:])
		initials = append(initials, unicode.ToUpper(first))

		if len(initials) == maxInitials {
			break
		}
	}

	return string(initials)
}

// colourOf is the palette colour a subject always gets.
func colourOf(subject string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(subject))

	return palette[h.Sum32()%uint32(len(palette))]
}

// Avatar answers the avatar route. It needs no principal: the picture is the
// initials and the colour the query names, after both are checked.
func Avatar(w http.ResponseWriter, r *http.Request) {
	initials, colour, err := avatarQuery(r.URL.RawQuery)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": err.Error()})

		return
	}

	picture, err := drawAvatar(initials, colour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "The avatar could not be drawn."})

		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", avatarCache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(picture) //nolint:gosec // a PNG drawn here, sent as image/png with nosniff, never HTML
}

// avatarQuery reads the initials and the colour, and refuses any other key, a
// key given twice, initials that are not one or two letters, and a colour
// outside the palette.
func avatarQuery(raw string) (string, color.RGBA, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return "", color.RGBA{}, fmt.Errorf("the query is not valid: %w", err)
	}

	for key, values := range query {
		if key != queryInitials && key != queryColour {
			return "", color.RGBA{}, fmt.Errorf("the query key %q is not known", key)
		}

		if len(values) != 1 {
			return "", color.RGBA{}, fmt.Errorf("the query key %q is given %d times", key, len(values))
		}
	}

	initials := query.Get(queryInitials)
	if !validInitials(initials) {
		return "", color.RGBA{}, fmt.Errorf("%s must be one or two letters", queryInitials)
	}

	code := query.Get(queryColour)
	if !slices.Contains(palette[:], code) {
		return "", color.RGBA{}, fmt.Errorf("%s must be one of %s", queryColour, strings.Join(palette[:], ", "))
	}

	rgb, err := hex.DecodeString(code)
	if err != nil {
		return "", color.RGBA{}, fmt.Errorf("%s is not hexadecimal: %w", queryColour, err)
	}

	return initials, color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: math.MaxUint8}, nil
}

// validInitials reports whether s is one or two letters of any script.
func validInitials(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}

	count := utf8.RuneCountInString(s)
	if count < 1 || count > maxInitials {
		return false
	}

	for _, c := range s {
		if !unicode.IsLetter(c) {
			return false
		}
	}

	return true
}

// avatarFont is the bold Go font, parsed once.
//
//nolint:gochecknoglobals // parsed on first use and only read after.
var avatarFont = sync.OnceValues(func() (*opentype.Font, error) {
	return opentype.Parse(gobold.TTF)
})

// drawAvatar is an opaque PNG square in colour, with the initials in white at
// its centre. PrusaSlicer rounds an avatar itself and expects no alpha channel,
// as a Prusa Account avatar has none. A letter the font has no glyph for is
// left out, so the colour stands alone instead of showing a box.
func drawAvatar(initials string, colour color.RGBA) ([]byte, error) {
	picture := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	draw.Draw(picture, picture.Bounds(), &image.Uniform{C: colour}, image.Point{}, draw.Src)

	parsed, err := avatarFont()
	if err != nil {
		return nil, fmt.Errorf("parse the font: %w", err)
	}

	drawn := drawable(parsed, initials)
	if drawn != "" {
		if err = drawInitials(picture, parsed, drawn); err != nil {
			return nil, err
		}
	}

	var out bytes.Buffer

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(&out, picture); err != nil {
		return nil, fmt.Errorf("encode the avatar: %w", err)
	}

	return out.Bytes(), nil
}

// drawable is the part of initials the font has a glyph for.
func drawable(parsed *opentype.Font, initials string) string {
	var buffer sfnt.Buffer

	var kept []rune

	for _, c := range initials {
		index, err := parsed.GlyphIndex(&buffer, c)
		if err == nil && index != 0 {
			kept = append(kept, c)
		}
	}

	return string(kept)
}

// drawInitials writes the letters in white, centred on the ink they leave,
// so a letter with a descender sits as centred as one without.
func drawInitials(picture *image.NRGBA, parsed *opentype.Font, initials string) error {
	size := float64(oneLetterSize)
	if utf8.RuneCountInString(initials) > 1 {
		size = twoLettersSize
	}

	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: fontDPI, Hinting: font.HintingFull})
	if err != nil {
		return fmt.Errorf("size the font: %w", err)
	}

	defer func() { _ = face.Close() }()

	drawer := font.Drawer{Dst: picture, Src: image.White, Face: face}
	ink, _ := drawer.BoundString(initials)

	centre := fixed.I(avatarCentre)
	drawer.Dot = fixed.Point26_6{
		X: centre - (ink.Min.X+ink.Max.X)/2,
		Y: centre - (ink.Min.Y+ink.Max.Y)/2,
	}
	drawer.DrawString(initials)

	return nil
}
