package threemf

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"strconv"
	"strings"
)

// prusaThumbnail is the picture every PrusaSlicer writes. PrusaSlicer looks it
// up without regard to case, and so does this package.
const prusaThumbnail = "Metadata/thumbnail.png"

// thumbnailNames lists the entries that may hold a picture, best first: the
// PrusaSlicer picture, the pictures the package names, and pictures of single
// plates, which some slicers write as Metadata/plate_1.png and on.
func thumbnailNames(a *archive, rels relationships) []string {
	var names []string

	add := func(name string) {
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	if f := a.entryFold(prusaThumbnail); f != nil {
		add(f.Name)
	}

	for _, name := range rels.thumbnails {
		add(name)
	}

	var plates []string

	for name := range a.byName {
		if _, ok := plateNumber(name); ok {
			plates = append(plates, name)
		}
	}

	slices.SortFunc(plates, func(x, y string) int {
		nx, _ := plateNumber(x)
		ny, _ := plateNumber(y)

		return nx - ny
	})

	for _, name := range plates {
		add(name)
	}

	return names
}

// plateNumber reads n from Metadata/plate_n.png.
func plateNumber(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "Metadata/plate_")
	if !ok {
		return 0, false
	}

	digits, ok := strings.CutSuffix(rest, ".png")
	if !ok {
		return 0, false
	}

	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, false
	}

	return n, true
}

// readThumbnail reads one picture and its size from its own header. A picture
// that is neither PNG nor JPEG is refused.
func readThumbnail(ctx context.Context, a *archive, name string) (Thumbnail, error) {
	f := a.entry(name)
	if f == nil {
		return Thumbnail{}, formatError("no entry %q", name)
	}

	data, err := readAll(ctx, f, a.limits.MaxThumbnailBytes, "MaxThumbnailBytes")
	if err != nil {
		return Thumbnail{}, err
	}

	out := Thumbnail{Path: name, Data: data}

	switch {
	case bytes.HasPrefix(data, pngSignature):
		out.MediaType = "image/png"
		out.Width, out.Height, err = pngSize(data)
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		out.MediaType = "image/jpeg"
		out.Width, out.Height, err = jpegSize(data)
	default:
		err = formatError("%s is not a PNG or a JPEG", name)
	}

	if err != nil {
		return Thumbnail{}, err
	}

	return out, nil
}

//nolint:gochecknoglobals // a constant byte string.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}

// pngSize reads the width and height from a PNG's IHDR chunk, which the format
// requires to come first.
func pngSize(data []byte) (int, int, error) {
	const ihdrEnd = 24 // signature 8, length 4, type 4, width 4, height 4.
	if len(data) < ihdrEnd || string(data[12:16]) != "IHDR" {
		return 0, 0, formatError("a PNG without its header")
	}

	w := binary.BigEndian.Uint32(data[16:20])
	h := binary.BigEndian.Uint32(data[20:24])

	if w == 0 || h == 0 || w > 1<<16 || h > 1<<16 {
		return 0, 0, formatError("a PNG of size %dx%d", w, h)
	}

	return int(w), int(h), nil
}

// jpegSize walks a JPEG's markers to the first start of frame, which holds
// the size.
func jpegSize(data []byte) (int, int, error) {
	at := jpegMarkerBytes

	for at+jpegSegmentHead <= len(data) {
		if data[at] != jpegMarker {
			return 0, 0, formatError("a JPEG with a broken marker")
		}

		marker := data[at+1]
		length := int(binary.BigEndian.Uint16(data[at+jpegMarkerBytes:]))

		if jpegFrame(marker) {
			if at+jpegFrameHead > len(data) {
				break
			}

			h := int(binary.BigEndian.Uint16(data[at+jpegHeightAt:]))
			w := int(binary.BigEndian.Uint16(data[at+jpegWidthAt:]))

			if w == 0 || h == 0 {
				break
			}

			return w, h, nil
		}

		if length < jpegMarkerBytes {
			break
		}

		at += jpegMarkerBytes + length
	}

	return 0, 0, formatError("a JPEG without a frame header")
}

// The layout of a JPEG segment: a two-byte marker, then a two-byte length
// that counts itself. A start-of-frame segment holds a precision byte, then
// the height and the width.
const (
	jpegMarker      = 0xFF
	jpegMarkerBytes = 2
	jpegSegmentHead = 4
	jpegFrameHead   = 9
	jpegHeightAt    = 5
	jpegWidthAt     = 7
)

// jpegFrame reports whether a marker starts a frame: SOF0 to SOF15, except
// DHT (C4), JPG (C8) and DAC (CC), which share the range.
func jpegFrame(marker byte) bool {
	const (
		sof0  = 0xC0
		sof15 = 0xCF
		dht   = 0xC4
		jpg   = 0xC8
		dac   = 0xCC
	)

	return marker >= sof0 && marker <= sof15 && marker != dht && marker != jpg && marker != dac
}
