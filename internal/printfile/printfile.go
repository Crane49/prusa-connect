// Package printfile reads the preview a slicer already wrote into a print
// file: the picture of the model and the few numbers an operator wants before
// starting a print.
//
// Nothing here slices, renders a mesh or decodes a G-code program. PrusaSlicer
// writes a thumbnail and a table of settings into the file it exports, and this
// package reads those back. A file it does not understand, and a file that is
// damaged, report no details. That is not a failure: a listing of six files
// must still answer when one of them is a text document somebody renamed.
//
// Three formats carry a preview:
//
//   - a text .gcode file, where the thumbnail is base64 in a comment block near
//     the start and the settings are comments at the end
//   - a binary .bgcode file, a container of typed blocks
//   - a .3mf project, a zip holding Metadata/thumbnail.png
//
// An .stl file holds triangles and no picture, so it reports no details.
//
// # What one file may cost
//
// The bytes come from whoever uploaded the file, so the cost of reading one is
// fixed here and never by what the file claims about itself. One file gets one
// picture decoded, whatever number of pictures it holds, and a budget for the
// settings tables it holds. The reader is an [io.ReaderAt] and a length, and
// the parsers seek to what they need: a text file is read at both ends, a
// binary container walks its block headers, and a zip entry is read through a
// limited reader. The context is checked between blocks, so a caller that gave
// up stops the work it was waiting for.
package printfile

import (
	"context"
	"errors"
	"io"
)

// The bounds every read here is held to. They stop an uploaded file from
// deciding how much memory and how much time this server spends on it.
const (
	// MaxThumbnailBytes is the largest encoded picture this package returns. A
	// slicer's thumbnail is a few tens of kilobytes.
	MaxThumbnailBytes = 1 << 20

	// MaxMetadataBytes is the largest settings table this package decodes out
	// of one block of a binary file.
	MaxMetadataBytes = 256 << 10

	// MaxMetadataTotalBytes is how much settings text one file may cost,
	// counting every block together. A container holding four thousand
	// metadata blocks is answered from the first megabyte of them and the rest
	// are skipped.
	MaxMetadataTotalBytes = 1 << 20

	// headBytes is how much of a text G-code file is read from the start. The
	// thumbnail and the header comments are inside it.
	headBytes = 256 << 10

	// tailBytes is how much of a text G-code file is read from the end, where
	// the slicer writes what the print used and which settings produced it.
	tailBytes = 64 << 10

	// maxBlocks bounds how many block headers a binary container may have
	// before this package stops walking it. A file with more than this is
	// either not a print file or is trying to spend the server's time.
	maxBlocks = 4096

	// sniffBytes is how much of the start is read to tell the formats apart.
	sniffBytes = 8

	// maxThumbnailPixels is the largest picture this package decodes. The
	// biggest thumbnail any PrusaSlicer 2.9.6 vendor profile asks for is
	// 800x480, which is 384000 pixels, so a million leaves room for a profile
	// that asks for more and still holds a decoded picture to four megabytes.
	maxThumbnailPixels = 1 << 20

	// maxThumbnailSide bounds one edge of a picture on its own, so a width and
	// a height that would overflow when multiplied are refused before they are.
	// The longest edge in those profiles is 800.
	maxThumbnailSide = 2048

	// preferredLongEdge is the size a page wants. Of the pictures a file holds,
	// the largest one whose long edge is at most this is the one decoded. It
	// is a preference and not a limit: a file whose every picture is larger
	// gets its smallest one.
	preferredLongEdge = 1024
)

// Details is what a slicer said about the print, as far as the file states it.
//
// Every field is optional. A zero means the file did not say, not that the
// value is zero, so a caller shows a field only when it is set.
type Details struct {
	// EstimatedSeconds is the print time the slicer estimated in its normal
	// mode, in whole seconds.
	EstimatedSeconds int64
	// FilamentMM is how much filament the print uses, in millimetres.
	FilamentMM float64
	// FilamentG is how much filament the print uses, in grams.
	FilamentG float64
	// LayerHeightMM is the layer height the file was sliced at, in millimetres.
	LayerHeightMM float64
	// NozzleMM is the nozzle diameter the file was sliced for, in millimetres.
	// A multi-tool file names one diameter per tool and this is the first.
	NozzleMM float64
	// Material is the filament name, such as PLA. A multi-tool file names one
	// material per tool and this is the first.
	Material string
	// PrinterModel is the printer the file was sliced for, such as MK4IS.
	PrinterModel string
	// PrintProfile, PrinterProfile and FilamentProfile are the names of the
	// slicer profiles the file was sliced with, such as "0.20mm STRUCTURAL
	// @MK4IS 0.4". A multi-tool file names one filament profile per tool and
	// this is the first.
	PrintProfile    string
	PrinterProfile  string
	FilamentProfile string
	// HighFlow says the file was sliced for a high flow nozzle on the first
	// tool.
	HighFlow bool

	// Filaments is what the print needs of each filament, T0 first. A
	// single-filament file has one entry. It is empty when the file names no
	// filament settings at all.
	Filaments []Filament
	// MaxLayerZMM is the height of the top layer, in millimetres.
	MaxLayerZMM float64
	// Objects are the objects the slicer labelled, in the order of their
	// numbers: the number a printer cancels an object by is its index here.
	// Each has its name and its outline on the bed. It is nil when the file
	// labels no object.
	Objects []LabelledObject
	// Bounds is the area the objects cover on the bed, read from the outline
	// the slicer writes for each object. It is nil when the file has none.
	Bounds *Bounds
	// Bed is the bed of the printer profile the file was sliced with.
	Bed Bed
	// Checks is what the start of the G-code asks the printer to check. It is
	// nil when the G-code could not be read, and empty when it was read and
	// asks for nothing.
	Checks *Checks
}

// Thumbnail is one picture a slicer stored in the file, ready to be served.
type Thumbnail struct {
	// Bytes is the encoded picture.
	Bytes []byte
	// MediaType is what those bytes are: image/png or image/jpeg. A QOI picture
	// is re-encoded as PNG before it is returned, so no caller has to know the
	// format.
	MediaType string
	// Width and Height are the picture's size in pixels, read out of the
	// encoded picture itself and never out of what the file said around it.
	Width  int
	Height int
}

// Preview is everything this package found in one file.
type Preview struct {
	// Details is what the slicer said about the print.
	Details Details
	// Thumbnail is the one picture this package decoded, or nil when the file
	// carries none it can read.
	Thumbnail *Thumbnail
}

// Empty reports whether the file yielded neither details nor a picture.
func (p Preview) Empty() bool { return p.Details.Empty() && p.Thumbnail == nil }

// The media types this package returns a picture as.
const (
	mediaPNG  = "image/png"
	mediaJPEG = "image/jpeg"
)

// Read returns what the file at r says about the print it holds.
//
// It never reports an error. A format it does not know, a truncated file and a
// file whose own header contradicts its length all answer an empty Preview,
// because a caller is asking "is there a preview" and "no" is an answer. A
// failure to read the bytes at all is the caller's own I/O error and shows up
// as an empty Preview too: the caller already knows its storage is unwell. A
// cancelled context answers with whatever had been read by then.
//
// One call decodes at most one picture and reads at most MaxMetadataTotalBytes
// of settings, whatever the file holds. For a text G-code file it reads
// headBytes from the start and tailBytes from the end; for a binary container
// it reads the block headers and the blocks it chose; for a project it opens
// one zip entry.
func Read(ctx context.Context, r io.ReaderAt, size int64) Preview {
	if r == nil || size <= 0 {
		return Preview{}
	}

	spend := newBudget(ctx)

	switch sniff(r, size) {
	case formatBinaryGCode:
		return readBinaryGCode(r, size, spend)
	case formatZip:
		return readProject(r, size, spend)
	case formatTextGCode:
		return readTextGCode(r, size, spend)
	case formatUnknown:
		return Preview{}
	}

	return Preview{}
}

// budget is what one call to Read may spend, across every block of the file.
//
// It exists because the bounds on a single block are not a bound on a file: a
// container may declare four thousand blocks, and four thousand blocks each
// inside its own limit is four thousand times the work.
type budget struct {
	ctx context.Context //nolint:containedctx // one parse, one context, ended with the call.

	// metadata is how many bytes of settings text are left to decode.
	metadata int64
	// pictures is how many pictures are left to decode, which is one.
	pictures int
}

func newBudget(ctx context.Context) *budget {
	if ctx == nil {
		ctx = context.Background()
	}

	return &budget{ctx: ctx, metadata: MaxMetadataTotalBytes, pictures: 1}
}

// done reports whether the caller gave up. It is asked between blocks, so a
// file that takes a long time stops when nobody is waiting for it.
func (b *budget) done() bool { return b.ctx.Err() != nil }

// takeMetadata reports whether this many bytes of settings text may still be
// decoded, and spends them.
func (b *budget) takeMetadata(want int64) bool {
	if want <= 0 || want > b.metadata {
		return false
	}

	b.metadata -= want

	return true
}

// takePicture reports whether a picture may still be decoded, and spends the
// one this file gets.
func (b *budget) takePicture() bool {
	if b.pictures <= 0 {
		return false
	}

	b.pictures--

	return true
}

// format is what the first bytes of a file say it is.
type format int

const (
	formatUnknown format = iota
	formatTextGCode
	formatBinaryGCode
	formatZip
)

// The magic numbers this package tells formats apart by. A name is a hint a
// caller chose and the bytes are what the file is, so only the bytes decide.
var (
	//nolint:gochecknoglobals // constant byte strings, never written to.
	magicBinaryGCode = []byte("GCDE")
	//nolint:gochecknoglobals // constant byte strings, never written to.
	magicZip = []byte("PK\x03\x04")
	//nolint:gochecknoglobals // constant byte strings, never written to.
	magicASCIISTL = []byte("solid")
)

// sniff reads the first few bytes and says which parser to run.
//
// Anything that is not a binary container, a zip or an ASCII STL is offered to
// the text G-code parser, which finds nothing in a file that holds nothing.
func sniff(r io.ReaderAt, size int64) format {
	head := make([]byte, minInt64(sniffBytes, size))

	if _, err := r.ReadAt(head, 0); err != nil && !atEnd(err) {
		return formatUnknown
	}

	switch {
	case hasPrefix(head, magicBinaryGCode):
		return formatBinaryGCode
	case hasPrefix(head, magicZip):
		return formatZip
	case hasPrefix(head, magicASCIISTL):
		return formatUnknown
	}

	return formatTextGCode
}

// atEnd reports whether a read stopped because the file ended, which is the
// normal answer for a file shorter than the buffer asked for.
func atEnd(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}

	for i := range prefix {
		if b[i] != prefix[i] {
			return false
		}
	}

	return true
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}

	return b
}

// readSection reads at most limit bytes starting at offset, and reports what it
// managed to read. A short read at the end of the file is not a failure.
func readSection(r io.ReaderAt, offset, limit int64) []byte {
	if offset < 0 || limit <= 0 {
		return nil
	}

	buf := make([]byte, limit)

	read, err := r.ReadAt(buf, offset)
	if err != nil && !atEnd(err) {
		return nil
	}

	return buf[:read]
}

// sizeAllowed reports whether a picture of this size may be decoded. Each edge
// is checked before the two are multiplied, because a width and a height a file
// declares are whatever the file says and their product can overflow.
func sizeAllowed(width, height int64) bool {
	if width <= 0 || height <= 0 || width > maxThumbnailSide || height > maxThumbnailSide {
		return false
	}

	return width*height <= maxThumbnailPixels
}

// candidate is one picture a file offers, as its own header describes it. It is
// what the pick is made from, before any picture is decoded.
type candidate struct {
	// declaredWidth and declaredHeight are what the file said. They decide
	// which picture is worth decoding and never what the answer reports.
	declaredWidth  int
	declaredHeight int
	// qoi says the bytes are QOI and have to be re-encoded as PNG.
	qoi bool
	// mediaType is what the file says the bytes are.
	mediaType string
	// found is false for the zero value, so a caller can tell "nothing yet"
	// from "a picture of no size".
	found bool
}

// preferred reports whether this picture is small enough for a page to show
// without scaling it down.
func (c candidate) preferred() bool {
	return c.declaredWidth <= preferredLongEdge && c.declaredHeight <= preferredLongEdge
}

func (c candidate) area() int { return c.declaredWidth * c.declaredHeight }

// betterCandidate reports whether next is the picture to decode instead of
// chosen.
//
// The largest picture that fits preferredLongEdge wins. When every picture is
// larger than that, the smallest of them wins, because it is the cheapest to
// decode and a page scales it down either way.
func betterCandidate(chosen, next candidate) bool {
	switch {
	case !next.found:
		return false
	case !chosen.found:
		return true
	case chosen.preferred() != next.preferred():
		return next.preferred()
	case next.preferred():
		return next.area() > chosen.area()
	}

	return next.area() < chosen.area()
}
