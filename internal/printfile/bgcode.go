package printfile

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"io"
)

// The binary G-code container, as prusa3d/libbgcode specifies it. A file is a
// ten byte header and then a run of blocks, each with its own header saying
// what it holds, how it is compressed and how long it is.
const (
	// fileHeaderBytes is the magic, the version and the checksum type.
	fileHeaderBytes = 10

	// blockHeaderBytes is the type, the compression, and the uncompressed
	// length. A compressed block carries a second length after them.
	blockHeaderBytes = 8

	// compressedLengthBytes is the extra length field a compressed block has.
	compressedLengthBytes = 4

	// crc32Bytes is the checksum after every block when the file header says
	// the file carries one. This package walks past it and does not verify it:
	// a preview is not a transfer, and the storage already kept its own digest
	// of the whole file.
	crc32Bytes = 4

	// supportedVersion is the binarization version this package reads.
	supportedVersion = 1
)

// The block types of the specification.
const (
	blockFileMetadata    = 0
	blockGCode           = 1
	blockSlicerMetadata  = 2
	blockPrinterMetadata = 3
	blockPrintMetadata   = 4
	blockThumbnail       = 5
)

// The compression algorithms of the specification. Deflate is read with the
// standard library. Heatshrink is not in it. A metadata or thumbnail block
// compressed with heatshrink is skipped, and the start of a G-code block is
// decoded by unheatshrink.
const (
	compressionNone        = 0
	compressionDeflate     = 1
	compressionHeatshrink1 = 2
	compressionHeatshrink2 = 3
)

// The thumbnail formats of the specification.
const (
	thumbFormatPNG = 0
	thumbFormatJPG = 1
	thumbFormatQOI = 2
)

// The checksum types of the file header.
const (
	checksumNone  = 0
	checksumCRC32 = 1
)

// The parameter block that sits between a block header and its data. Every
// metadata block declares an encoding in two bytes; a thumbnail declares its
// format and its size in six.
const (
	metadataParamBytes  = 2
	thumbnailParamBytes = 6
)

// readBinaryGCode walks the container's block headers and reads only the blocks
// that hold a preview.
//
// The walk is the cheap part: a block header is at most twelve bytes and says
// where the next one starts. Settings tables are decoded as they are met, inside the
// file's whole metadata budget. Pictures are not: the walk only notes which one
// is worth having, from what each block header declares, and the one picked is
// the only one decoded. A container offering four thousand pictures therefore
// costs four thousand header reads and one decode.
//
// The G-code blocks are the whole print. The specification puts them last, so
// the walk stops at the first one, and reads only the start of it: the lines
// the firmware checks before it prints. See gcodeblock.go.
func readBinaryGCode(r io.ReaderAt, size int64, spend *budget) Preview {
	checksum, ok := readFileHeader(r, size)
	if !ok {
		return Preview{}
	}

	gathered := newCollector()

	var (
		chosen   candidate
		chosenAt block
		scanner  headScanner
	)

	offset := int64(fileHeaderBytes)

	for range maxBlocks {
		if spend.done() {
			break
		}

		current, next, found := readBlockHeader(r, size, offset, checksum)
		if !found {
			break
		}

		if current.kind == blockGCode {
			readGCodeHead(r, current, &scanner)

			break
		}

		if current.kind == blockThumbnail {
			if offered := thumbnailCandidate(current); betterCandidate(chosen, offered) {
				chosen, chosenAt = offered, current
			}
		} else {
			readMetadataBlock(r, current, gathered, spend)
		}

		offset = next
	}

	details := gathered.details()
	details.Checks = scanner.result()

	return Preview{Details: details, Thumbnail: decodeChosen(r, chosen, chosenAt, spend)}
}

// decodeChosen decodes the one picture the walk picked.
//
// A picked block that turns out to be damaged leaves the file with no picture.
// The walk does not go back for the next best one. A file full of broken
// pictures would then cost one decode for each block, and the decode is the
// expensive part of the work.
func decodeChosen(r io.ReaderAt, chosen candidate, at block, spend *budget) *Thumbnail {
	if !chosen.found || spend.done() || !spend.takePicture() {
		return nil
	}

	raw, ok := blockData(r, at, MaxThumbnailBytes)
	if !ok {
		return nil
	}

	return thumbnailFrom(raw, chosen.qoi, chosen.mediaType)
}

// thumbnailCandidate reads what a thumbnail block's own parameters say about
// the picture inside it. Nothing is decoded and nothing is read from the file.
func thumbnailCandidate(b block) candidate {
	format := binary.LittleEndian.Uint16(b.params[0:2])
	width := int64(binary.LittleEndian.Uint16(b.params[2:4]))
	height := int64(binary.LittleEndian.Uint16(b.params[4:6]))

	if !sizeAllowed(width, height) {
		return candidate{}
	}

	out := candidate{
		declaredWidth: int(width), declaredHeight: int(height), found: true,
	}

	switch format {
	case thumbFormatPNG:
		out.mediaType = mediaPNG
	case thumbFormatJPG:
		out.mediaType = mediaJPEG
	case thumbFormatQOI:
		out.mediaType, out.qoi = mediaPNG, true
	default:
		return candidate{}
	}

	return out
}

// readMetadataBlock decodes one settings table, as long as the file has
// metadata budget left for it.
func readMetadataBlock(r io.ReaderAt, b block, gathered *collector, spend *budget) {
	switch b.kind {
	case blockPrinterMetadata, blockPrintMetadata, blockFileMetadata, blockSlicerMetadata:
	default:
		return
	}

	// The block's own declared plain length is spent before it is decoded, so a
	// block claiming a megabyte costs the budget a megabyte whether or not it
	// can deliver one.
	if !spend.takeMetadata(b.plainBytes) {
		return
	}

	if text, ok := blockData(r, b, MaxMetadataBytes); ok {
		gathered.putTable(string(text))
	}
}

// readFileHeader checks the magic and the version, and reports how many
// checksum bytes follow every block.
func readFileHeader(r io.ReaderAt, size int64) (int64, bool) {
	if size < fileHeaderBytes {
		return 0, false
	}

	head := readSection(r, 0, fileHeaderBytes)
	if len(head) < fileHeaderBytes || !hasPrefix(head, magicBinaryGCode) {
		return 0, false
	}

	if binary.LittleEndian.Uint32(head[4:8]) != supportedVersion {
		return 0, false
	}

	switch binary.LittleEndian.Uint16(head[8:10]) {
	case checksumNone:
		return 0, true
	case checksumCRC32:
		return crc32Bytes, true
	}

	return 0, false
}

// block is one block's header and where its data starts.
type block struct {
	kind       uint16
	params     []byte
	dataAt     int64
	dataBytes  int64
	plainBytes int64
	compressed uint16
}

// readBlockHeader reads one block header and reports where the next block
// starts.
//
// Every length is checked against the file's own length before anything is
// allocated, so a header claiming a block larger than the file stops the walk
// before that much memory is asked for.
func readBlockHeader(r io.ReaderAt, size, offset, checksum int64) (block, int64, bool) {
	head := readSection(r, offset, blockHeaderBytes+compressedLengthBytes)
	if len(head) < blockHeaderBytes {
		return block{}, 0, false
	}

	out := block{
		kind:       binary.LittleEndian.Uint16(head[0:2]),
		compressed: binary.LittleEndian.Uint16(head[2:4]),
		plainBytes: int64(binary.LittleEndian.Uint32(head[4:8])),
	}

	headerBytes := int64(blockHeaderBytes)
	out.dataBytes = out.plainBytes

	if out.compressed != compressionNone {
		if len(head) < blockHeaderBytes+compressedLengthBytes {
			return block{}, 0, false
		}

		headerBytes += compressedLengthBytes
		out.dataBytes = int64(binary.LittleEndian.Uint32(head[8:12]))
	}

	paramBytes := int64(metadataParamBytes)
	if out.kind == blockThumbnail {
		paramBytes = thumbnailParamBytes
	}

	out.dataAt = offset + headerBytes + paramBytes

	next := out.dataAt + out.dataBytes + checksum
	if out.dataAt > size || next > size || next <= offset {
		return block{}, 0, false
	}

	out.params = readSection(r, offset+headerBytes, paramBytes)
	if int64(len(out.params)) < paramBytes {
		return block{}, 0, false
	}

	return out, next, true
}

// blockData returns one block's data, decompressing it when it is deflated.
//
// limit bounds both the bytes read from the file and the bytes the
// decompression may produce, so a block that claims to expand to a gigabyte is
// refused.
func blockData(r io.ReaderAt, b block, limit int64) ([]byte, bool) {
	if b.dataBytes <= 0 || b.dataBytes > limit || b.plainBytes > limit {
		return nil, false
	}

	raw := readSection(r, b.dataAt, b.dataBytes)
	if int64(len(raw)) < b.dataBytes {
		return nil, false
	}

	switch b.compressed {
	case compressionNone:
		return raw, true
	case compressionDeflate:
		return inflate(raw, limit)
	case compressionHeatshrink1, compressionHeatshrink2:
		// Heatshrink has no decoder in the standard library. The block is left
		// unread, and the file reports whatever its other blocks say.
		return nil, false
	}

	return nil, false
}

// inflate decompresses a deflated block, stopping at limit bytes.
//
// PrusaSlicer writes a zlib stream, which is a deflate stream with a two byte
// header. A raw deflate stream is read too, because the specification names the
// algorithm and not the framing.
func inflate(raw []byte, limit int64) ([]byte, bool) {
	if out, ok := inflateWith(zlib.NewReader, raw, limit); ok {
		return out, true
	}

	rawReader := func(r io.Reader) (io.ReadCloser, error) { return flate.NewReader(r), nil }

	return inflateWith(rawReader, raw, limit)
}

func inflateWith(
	open func(io.Reader) (io.ReadCloser, error), raw []byte, limit int64,
) ([]byte, bool) {
	reader, err := open(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}

	defer func() { _ = reader.Close() }()

	out, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil || len(out) == 0 {
		return nil, false
	}

	return out, true
}
