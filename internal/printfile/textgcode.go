package printfile

import (
	"encoding/base64"
	"io"
	"strconv"
	"strings"
)

// The comment markers PrusaSlicer wraps a stored picture in. The word after
// `thumbnail` names the encoding; the plain spelling is PNG.
const (
	thumbnailBegin = "thumbnail"
	thumbnailEnd   = "end"
)

// readTextGCode reads a text G-code file at both ends.
//
// The picture sits near the start, after the slicer's header comments, and the
// settings sit at the end, after the last movement. Reading the middle would
// mean reading a file that is often hundreds of megabytes of coordinates.
func readTextGCode(r io.ReaderAt, size int64, spend *budget) Preview {
	head := readSection(r, 0, minInt64(headBytes, size))

	tailAt := max(size-tailBytes, int64(len(head)))

	tail := readSection(r, tailAt, minInt64(tailBytes, size-tailAt))

	gathered := newCollector()
	headText := string(head)
	picked := scanTextGCode(headText, gathered, spend)

	if fromTail := scanTextGCode(string(tail), gathered, spend); betterCandidate(picked.what, fromTail.what) {
		picked = fromTail
	}

	// The checks are in the first G-code lines, after the header comments and
	// the pictures, so the head is read a second time for them.
	var scanner headScanner
	if !spend.done() {
		scanGCodeText(headText, &scanner)
	}

	details := gathered.details()
	details.Checks = scanner.result()

	return Preview{Details: details, Thumbnail: decodePicked(picked, spend)}
}

// picked is the one picture a section offered that is worth decoding: what its
// opening line declared, and the base64 that followed it.
type picked struct {
	what    candidate
	encoded string
}

// decodePicked turns the base64 of the chosen block into the picture a caller
// is served. It is the only decode one file gets.
func decodePicked(chosen picked, spend *budget) *Thumbnail {
	if !chosen.what.found || spend.done() || !spend.takePicture() {
		return nil
	}

	// The whitespace is stripped from the one block that won, so a file of
	// three thousand blocks does not pay for it three thousand times.
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(chosen.encoded), ""))
	if err != nil {
		return nil
	}

	return thumbnailFrom(raw, chosen.what.qoi, chosen.what.mediaType)
}

// scanTextGCode walks one section of a text G-code file, gathering settings out
// of its comments and keeping the base64 of the one picture worth decoding.
//
// Only that block's text is kept. For a file holding three thousand picture
// blocks, this reads three thousand opening lines and decodes nothing until
// the section is finished and one block has won. A block that starts inside
// the section and ends outside it is dropped: half a base64 stream decodes to
// a broken image, and no picture serves an operator better than a grey
// rectangle.
func scanTextGCode(text string, gathered *collector, spend *budget) picked {
	var (
		best    picked
		encoded strings.Builder
		open    bool
		header  candidate
		lines   int
	)

	for line := range strings.SplitSeq(text, "\n") {
		lines++

		// Asked once per batch of lines. A caller that gave up stops this scan,
		// without the check costing a read of the context on every line of a
		// quarter megabyte.
		if lines%cancelCheckLines == 0 && spend.done() {
			break
		}

		comment, isComment := commentBody(line)
		if !isComment {
			continue
		}

		if open {
			if isThumbnailEnd(comment) {
				best = picked{what: header, encoded: encoded.String()}
				open = false

				encoded.Reset()

				continue
			}

			if encoded.Len()+len(comment) > base64Ceiling {
				// The block claims more base64 than a picture this package
				// serves could decode to. Drop it and keep reading: the file
				// may hold a smaller picture further on.
				open = false

				encoded.Reset()
			} else {
				encoded.WriteString(comment)
			}

			continue
		}

		if offered, ok := parseThumbnailHeader(comment); ok {
			// The base64 of a block that cannot win is never copied, so the
			// cost of a block nobody wants is its opening line.
			if betterCandidate(best.what, offered) {
				header = offered
				open = true

				encoded.Reset()
			}

			continue
		}

		gathered.putLine(comment)
	}

	return best
}

// cancelCheckLines is how many lines of a text G-code file are read between two
// looks at the caller's context.
const cancelCheckLines = 4096

// isThumbnailEnd reports whether a comment closes a picture block, in any of
// the spellings PrusaSlicer writes: `thumbnail end`, `thumbnail_QOI end`.
func isThumbnailEnd(comment string) bool {
	return strings.HasPrefix(comment, thumbnailBegin) &&
		strings.HasSuffix(comment, " "+thumbnailEnd)
}

// base64Ceiling is how much base64 text one picture may be built from. Base64
// grows bytes by a third, so this is the text form of MaxThumbnailBytes.
const base64Ceiling = MaxThumbnailBytes/3*4 + 4

// commentBody returns what a G-code comment line says, without its semicolon.
func commentBody(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))

	body, found := strings.CutPrefix(line, ";")
	if !found {
		return "", false
	}

	return strings.TrimSpace(body), true
}

// parseThumbnailHeader reads `thumbnail begin 220x124 5312` and its
// `thumbnail_JPG` and `thumbnail_QOI` spellings.
//
// The byte count on the line is the slicer's own and nothing reads it: the
// decoded picture is bounded by MaxThumbnailBytes whatever the line claims. The
// size on the line decides only which block is worth decoding; the size a
// caller is told comes out of the decoded picture.
func parseThumbnailHeader(comment string) (candidate, bool) {
	fields := strings.Fields(comment)
	if len(fields) < 3 || fields[1] != "begin" {
		return candidate{}, false
	}

	kind, found := strings.CutPrefix(fields[0], thumbnailBegin)
	if !found {
		return candidate{}, false
	}

	out := candidate{found: true}

	switch strings.ToUpper(strings.TrimPrefix(kind, "_")) {
	case "", "PNG":
		out.mediaType = mediaPNG
	case "JPG", "JPEG":
		out.mediaType = mediaJPEG
	case "QOI":
		out.mediaType, out.qoi = mediaPNG, true
	default:
		return candidate{}, false
	}

	width, height, ok := parseSize(fields[2])
	if !ok {
		return candidate{}, false
	}

	out.declaredWidth, out.declaredHeight = width, height

	return out, true
}

// parseSize reads the `220x124` on a picture block's opening line.
func parseSize(field string) (int, int, bool) {
	left, right, found := strings.Cut(field, "x")
	if !found {
		return 0, 0, false
	}

	width, err := strconv.ParseInt(left, 10, 64)
	if err != nil {
		return 0, 0, false
	}

	height, err := strconv.ParseInt(right, 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if !sizeAllowed(width, height) {
		return 0, 0, false
	}

	return int(width), int(height), true
}
