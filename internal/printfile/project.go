package printfile

import (
	"archive/zip"
	"encoding/binary"
	"io"
)

// The entries a PrusaSlicer project keeps its picture under. The first one that
// opens is used.
//
//nolint:gochecknoglobals // a constant list, never written to.
var projectThumbnails = []string{
	"Metadata/thumbnail.png",
	"Metadata/thumbnail.PNG",
	"Metadata/plate_1.png",
}

// maxProjectEntries bounds how many names this package will look at inside a
// project, so a zip with a million entries cannot spend the server's time.
const maxProjectEntries = 4096

// readProject reads the picture a .3mf project carries.
//
// A project holds the model and the settings in XML, and this package does not
// read either: what an operator wants before printing is in the sliced file,
// and a project is often not sliced at all. One zip entry is opened, through a
// limited reader, and nothing else is touched.
func readProject(r io.ReaderAt, size int64, spend *budget) Preview {
	// The count is read out of the archive's own directory record first,
	// because zip.NewReader builds a struct for every entry before it returns
	// and a zip claiming a million entries would have cost that already.
	if !entryCountAllowed(r, size) {
		return Preview{}
	}

	archive, err := zip.NewReader(r, size)
	if err != nil {
		return Preview{}
	}

	for _, want := range projectThumbnails {
		if spend.done() {
			return Preview{}
		}

		if picture := openProjectThumbnail(archive, want, spend); picture != nil {
			return Preview{Thumbnail: picture}
		}
	}

	return Preview{}
}

// The end of central directory record, which is the last thing in a zip: a four
// byte signature, then fields, with the number of entries in the archive at
// offset 10. It may be followed by a comment of up to 65535 bytes, so it is
// looked for from the end.
const (
	eocdBytes      = 22
	eocdCountAt    = 10
	eocdMaxComment = 1 << 16
	// eocdCountOverflow is the value the count field carries when the real
	// count is in the Zip64 record instead. An archive that large is not a
	// project this server previews, so it is refused.
	eocdCountOverflow = 0xFFFF
)

//nolint:gochecknoglobals // a constant byte string, never written to.
var eocdSignature = []byte{'P', 'K', 0x05, 0x06}

// entryCountAllowed reports whether the archive declares few enough entries to
// be worth opening. An archive whose record cannot be found is left to
// zip.NewReader, which refuses it.
func entryCountAllowed(r io.ReaderAt, size int64) bool {
	window := minInt64(eocdBytes+eocdMaxComment, size)

	tail := readSection(r, size-window, window)
	if len(tail) < eocdBytes {
		return true
	}

	for at := len(tail) - eocdBytes; at >= 0; at-- {
		if !hasPrefix(tail[at:], eocdSignature) {
			continue
		}

		count := binary.LittleEndian.Uint16(tail[at+eocdCountAt : at+eocdCountAt+2])

		return count != eocdCountOverflow && int(count) <= maxProjectEntries
	}

	return true
}

// openProjectThumbnail reads one named entry and makes a picture of it.
func openProjectThumbnail(archive *zip.Reader, name string, spend *budget) *Thumbnail {
	entry := findEntry(archive, name)
	if entry == nil {
		return nil
	}

	// The declared uncompressed size is the archive's own claim. It is checked
	// first so an entry claiming a gigabyte is refused before it is opened, and
	// the read below is limited as well so a claim of one kilobyte cannot
	// expand into more.
	if entry.UncompressedSize64 > MaxThumbnailBytes {
		return nil
	}

	body, err := entry.Open()
	if err != nil {
		return nil
	}

	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(body, MaxThumbnailBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > MaxThumbnailBytes {
		return nil
	}

	if !spend.takePicture() {
		return nil
	}

	// The entry is named thumbnail.png, so PNG is what it has to be. A project
	// holding an SVG under that name is a project with no picture here.
	return thumbnailFrom(raw, false, mediaPNG)
}

// findEntry looks a name up in the archive. A zip carries its own directory, so
// this is a walk over names and not a read of any data.
func findEntry(archive *zip.Reader, name string) *zip.File {
	for _, entry := range archive.File {
		if entry.Name == name {
			return entry
		}
	}

	return nil
}
