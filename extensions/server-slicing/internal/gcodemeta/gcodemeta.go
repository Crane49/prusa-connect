// Package gcodemeta reads what PrusaSlicer wrote about a print into the
// G-code file it exported: the estimated time, the filament, the objects a
// printer can cancel one by one and the pictures it carries.
//
// A binary G-code file holds this in metadata blocks before the G-code, in the
// public format of libbgcode. A text file holds it in comments, the objects
// and the estimate near the end and the pictures near the start, so a text
// file is read at its head and its tail and never whole.
package gcodemeta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/threemf/threemf"
)

// Meta is what the file says about its print. A field the file did not
// carry is zero.
type Meta struct {
	Producer         string  `json:"producer,omitempty"`
	PrinterModel     string  `json:"printer_model,omitempty"`
	EstimatedSeconds int64   `json:"estimated_seconds,omitempty"`
	FilamentMM       float64 `json:"filament_mm,omitempty"`
	FilamentG        float64 `json:"filament_g,omitempty"`
	FilamentType     string  `json:"filament_type,omitempty"`
	NozzleMM         float64 `json:"nozzle_mm,omitempty"`
	LayerHeightMM    float64 `json:"layer_height_mm,omitempty"`
	MaxLayerZMM      float64 `json:"max_layer_z_mm,omitempty"`
	// Objects are the objects a printer can cancel one by one. An object's
	// ID is the number the G-code labels it with, M486 S<id>.
	Objects    []threemf.LabelledObject `json:"objects"`
	Thumbnails []Thumbnail              `json:"thumbnails"`
}

// Thumbnail is one picture the file carries.
type Thumbnail struct {
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ErrUnreadable reports a file this package cannot read.
var ErrUnreadable = errors.New("the G-code file cannot be read")

// The bounds on what is read.
const (
	// textWindow is how much of a text file is read at its head and at its
	// tail.
	textWindow = 1 << 20
	// maxBlockBytes bounds one metadata block, packed or unpacked.
	maxBlockBytes = 4 << 20
	// maxBlocks bounds the blocks walked before the G-code starts.
	maxBlocks = 512
)

// magic is how a binary G-code file starts.
var magic = []byte("GCDE")

// Read reads one file, binary or text by its first bytes.
func Read(name string) (Meta, error) {
	file, err := os.Open(name)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	defer func() { _ = file.Close() }() // read-only

	head := make([]byte, len(magic))
	if _, err = io.ReadFull(file, head); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	if bytes.Equal(head, magic) {
		return readBinary(file)
	}

	return readText(file)
}

// The block types of a binary G-code file.
const (
	blockFileMetadata    = 0
	blockGCode           = 1
	blockSlicerMetadata  = 2
	blockPrinterMetadata = 3
	blockPrintMetadata   = 4
	blockThumbnail       = 5
)

// The compressions of a block.
const (
	compressNone    = 0
	compressDeflate = 1
)

// The parameters after a block's header: the encoding of a metadata or a
// G-code block, and the format, the width and the height of a picture.
const (
	blockParamBytes     = 2
	thumbnailParamBytes = 6
)

// blockHeader is the fixed start of every block.
type blockHeader struct {
	Type, Compression uint16
	Size              uint32
}

func readBinary(file io.ReadSeeker) (Meta, error) {
	var header struct {
		Magic    [4]byte
		Version  uint32
		Checksum uint16
	}

	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		return Meta{}, fmt.Errorf("%w: the file header: %w", ErrUnreadable, err)
	}

	walker := blockWalker{file: file, values: map[string]string{}}
	if header.Checksum == 1 {
		walker.checksum = crc32.Size
	}

	for range maxBlocks {
		more, err := walker.next()
		if err != nil {
			return Meta{}, err
		}

		if !more {
			break
		}
	}

	meta := Meta{Thumbnails: walker.thumbnails}
	fill(&meta, walker.values)

	return meta, nil
}

// blockWalker reads the blocks before the G-code, one at a time.
type blockWalker struct {
	file       io.ReadSeeker
	checksum   int64
	values     map[string]string
	thumbnails []Thumbnail
}

// next reads one block, and answers false at the G-code or the end of the
// file.
func (w *blockWalker) next() (bool, error) {
	var block blockHeader

	if err := binary.Read(w.file, binary.LittleEndian, &block); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}

		return false, fmt.Errorf("%w: a block header: %w", ErrUnreadable, err)
	}

	if block.Type == blockGCode {
		return false, nil
	}

	packed := int64(block.Size)

	if block.Compression != compressNone {
		var compressed uint32
		if err := binary.Read(w.file, binary.LittleEndian, &compressed); err != nil {
			return false, fmt.Errorf("%w: a block size: %w", ErrUnreadable, err)
		}

		packed = int64(compressed)
	}

	params := make([]byte, blockParamBytes)
	if block.Type == blockThumbnail {
		params = make([]byte, thumbnailParamBytes)
	}

	if _, err := io.ReadFull(w.file, params); err != nil {
		return false, fmt.Errorf("%w: block parameters: %w", ErrUnreadable, err)
	}

	if block.Type == blockThumbnail {
		w.thumbnails = append(w.thumbnails, thumbnailOf(params))
	}

	if !isMetadata(block.Type) || packed > maxBlockBytes || block.Size > maxBlockBytes {
		return w.skip(packed + w.checksum)
	}

	data := make([]byte, packed)
	if _, err := io.ReadFull(w.file, data); err != nil {
		return false, fmt.Errorf("%w: a block: %w", ErrUnreadable, err)
	}

	if text, ok := unpacked(block.Compression, data, int64(block.Size)); ok {
		readINI(text, w.values)
	}

	return w.skip(w.checksum)
}

func (w *blockWalker) skip(length int64) (bool, error) {
	if _, err := w.file.Seek(length, io.SeekCurrent); err != nil {
		return false, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	return true, nil
}

func isMetadata(kind uint16) bool {
	return kind == blockFileMetadata || kind == blockPrinterMetadata || kind == blockPrintMetadata ||
		kind == blockSlicerMetadata
}

// unpacked is a block's text. A compression this package does not read, such
// as heatshrink, is skipped: PrusaSlicer packs only the G-code that way.
func unpacked(compression uint16, data []byte, size int64) ([]byte, bool) {
	switch compression {
	case compressNone:
		return data, true
	case compressDeflate:
		reader, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, false
		}

		out, err := io.ReadAll(io.LimitReader(reader, size))
		if err != nil {
			return nil, false
		}

		return out, true
	}

	return nil, false
}

func thumbnailOf(params []byte) Thumbnail {
	format := "png"

	const jpg, qoi = 1, 2

	switch binary.LittleEndian.Uint16(params[0:]) {
	case jpg:
		format = "jpg"
	case qoi:
		format = "qoi"
	}

	return Thumbnail{
		Format: format,
		Width:  int(binary.LittleEndian.Uint16(params[blockParamBytes:])),
		Height: int(binary.LittleEndian.Uint16(params[2*blockParamBytes:])),
	}
}

// readINI reads key=value lines. A key read earlier keeps its value, so the
// printer metadata, which comes first, wins over the full configuration.
func readINI(text []byte, into map[string]string) {
	for line := range strings.SplitSeq(string(text), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		if _, seen := into[key]; !seen {
			into[key] = strings.TrimSpace(value)
		}
	}
}

// thumbnailComment is the line that opens a picture in a text file, such as
// "; thumbnail begin 16x16 1234" or "; thumbnail_QOI begin 220x124 5678".
var thumbnailComment = regexp.MustCompile(`^; thumbnail(?:_(PNG|JPG|QOI))? begin (\d+)x(\d+) \d+`)

func readText(file io.ReadSeeker) (Meta, error) {
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	head, err := window(file, 0, min(size, textWindow))
	if err != nil {
		return Meta{}, err
	}

	tailStart := max(size-textWindow, int64(len(head)))

	tail, err := window(file, tailStart, size-tailStart)
	if err != nil {
		return Meta{}, err
	}

	values := map[string]string{}
	meta := Meta{}

	for _, part := range [][]byte{head, tail} {
		for line := range strings.SplitSeq(string(part), "\n") {
			if thumbnail, isThumbnail := readComment(strings.TrimSpace(line), values); isThumbnail {
				meta.Thumbnails = append(meta.Thumbnails, thumbnail)
			}
		}
	}

	fill(&meta, values)

	return meta, nil
}

// readComment reads one line of a text file: a picture's opening line, the
// producer, or a "; key = value" comment. A key read earlier keeps its value.
func readComment(line string, values map[string]string) (Thumbnail, bool) {
	if found := thumbnailComment.FindStringSubmatch(line); found != nil {
		return textThumbnail(found), true
	}

	comment, ok := strings.CutPrefix(line, ";")
	if !ok {
		return Thumbnail{}, false
	}

	// A text file names its producer in its first comment and not as a key:
	// "; generated by PrusaSlicer 2.9.6 on ...".
	if rest, generated := strings.CutPrefix(strings.TrimSpace(comment), "generated by "); generated {
		if _, seen := values["Producer"]; !seen {
			values["Producer"] = producerName(rest)
		}

		return Thumbnail{}, false
	}

	key, value, found := strings.Cut(comment, "=")
	if !found {
		return Thumbnail{}, false
	}

	key = strings.TrimSpace(key)
	if _, seen := values[key]; !seen {
		values[key] = strings.TrimSpace(value)
	}

	return Thumbnail{}, false
}

func textThumbnail(found []string) Thumbnail {
	width, _ := strconv.Atoi(found[2])
	height, _ := strconv.Atoi(found[3])

	format := strings.ToLower(found[1])
	if format == "" {
		format = "png"
	}

	return Thumbnail{Format: format, Width: width, Height: height}
}

func window(file io.ReadSeeker, offset, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	out := make([]byte, length)
	if _, err := io.ReadFull(file, out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	return out, nil
}

// fill reads the values PrusaSlicer writes, under the names it writes them.
func fill(meta *Meta, values map[string]string) {
	meta.Producer = values["Producer"]
	meta.PrinterModel = values["printer_model"]
	meta.FilamentType = firstOf(values["filament_type"])
	meta.NozzleMM = number(firstOf(values["nozzle_diameter"]))
	meta.LayerHeightMM = number(values["layer_height"])
	meta.MaxLayerZMM = number(values["max_layer_z"])
	meta.FilamentMM = sum(values["filament used [mm]"])
	meta.FilamentG = sum(values["filament used [g]"])
	meta.EstimatedSeconds = duration(values["estimated printing time (normal mode)"])
	meta.Objects = objectsOf(values["objects_info"])

	if meta.Thumbnails == nil {
		meta.Thumbnails = []Thumbnail{}
	}
}

// producerName keeps the name and the version, "PrusaSlicer 2.9.6", of what
// follows "generated by".
func producerName(rest string) string {
	const nameAndVersion = 2

	fields := strings.Fields(rest)
	if len(fields) > nameAndVersion {
		fields = fields[:nameAndVersion]
	}

	return strings.Join(fields, " ")
}

func firstOf(list string) string {
	first, _, _ := strings.Cut(list, ";")
	first, _, _ = strings.Cut(first, ",")

	return strings.TrimSpace(first)
}

func number(text string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0
	}

	return value
}

// sum adds the values of a list, which a file sliced for several tools writes
// one per tool.
func sum(list string) float64 {
	total := 0.0

	for part := range strings.SplitSeq(list, ",") {
		total += number(part)
	}

	return total
}

// duration reads a time the way PrusaSlicer writes it, such as "1d 2h 3m 4s".
func duration(text string) int64 {
	const day = 24 * time.Hour

	seconds := map[byte]time.Duration{'d': day, 'h': time.Hour, 'm': time.Minute, 's': time.Second}

	var total time.Duration

	for part := range strings.FieldsSeq(text) {
		unit, known := seconds[part[len(part)-1]]
		if !known {
			return 0
		}

		value, err := strconv.ParseInt(part[:len(part)-1], 10, 64)
		if err != nil {
			return 0
		}

		total += time.Duration(value) * unit
	}

	return int64(total / time.Second)
}

// objectsOf reads objects_info. A value that does not read is no objects.
func objectsOf(text string) []threemf.LabelledObject {
	if text == "" {
		return []threemf.LabelledObject{}
	}

	objects, err := threemf.ParseObjectsInfo(text)
	if err != nil {
		return []threemf.LabelledObject{}
	}

	return objects
}
