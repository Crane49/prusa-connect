package printfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The fuzz targets. Each one asserts the same contract: Read answers, it does
// not panic, and whatever it returns is inside the bounds this package
// publishes. A corpus entry that makes it allocate a picture larger than
// MaxThumbnailBytes is a finding, not a curiosity.

// FuzzReadTextGCode drives the text G-code parser.
func FuzzReadTextGCode(f *testing.F) {
	f.Add(seedFile(f, "cube10.gcode"))
	f.Add(fixtureTextGCode("thumbnail", fixturePNG(f, 8, 8), 8, 8, 4))
	f.Add(fixtureTextGCode("thumbnail_QOI", fixtureQOI(8, 8), 8, 8, 4))
	f.Add([]byte("; thumbnail begin 4x4 12\n; ####\n; thumbnail end\n"))
	f.Add([]byte("; estimated printing time (normal mode) = 2h 1m 30s\n"))

	f.Fuzz(func(t *testing.T, in []byte) {
		checkPreview(t, Read(t.Context(), bytes.NewReader(in), int64(len(in))))
	})
}

// FuzzReadBinaryGCode drives the container walk.
func FuzzReadBinaryGCode(f *testing.F) {
	f.Add(seedFile(f, "cube10.bgcode"))
	f.Add(fixtureBGCode(f, []bgcodeBlock{
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatPNG, 8, 8), data: fixturePNG(f, 8, 8),
		},
		{
			kind: blockPrintMetadata, compression: compressionDeflate,
			params: metadataParams(), data: []byte("filament used [g]=0.40\n"),
		},
	}))
	f.Add([]byte("GCDE\x01\x00\x00\x00\x01\x00"))

	f.Fuzz(func(t *testing.T, in []byte) {
		checkPreview(t, Read(t.Context(), bytes.NewReader(in), int64(len(in))))
	})
}

// FuzzReadProject drives the zip reader.
func FuzzReadProject(f *testing.F) {
	f.Add(seedFile(f, "cube10.3mf"))
	f.Add(fixtureProject(f, "Metadata/thumbnail.png", fixturePNG(f, 8, 8)))
	f.Add([]byte("PK\x03\x04"))

	f.Fuzz(func(t *testing.T, in []byte) {
		checkPreview(t, Read(t.Context(), bytes.NewReader(in), int64(len(in))))
	})
}

// FuzzDecodeQOI drives the picture decoder this package had to write itself,
// because the standard library has no QOI.
func FuzzDecodeQOI(f *testing.F) {
	f.Add(fixtureQOI(4, 4))
	f.Add(fixtureQOI(1, 1))
	f.Add([]byte("qoif\x00\x00\x00\x01\x00\x00\x00\x01\x04\x00\x00\x00\x00\x00\x00\x00\x00\x01"))

	f.Fuzz(func(t *testing.T, in []byte) {
		out, ok := pngFromQOI(in)
		if !ok {
			return
		}

		if len(out) > MaxThumbnailBytes {
			t.Fatalf("the picture is %d bytes and the bound is %d", len(out), MaxThumbnailBytes)
		}

		width, height, sized := pictureSize(out, mediaPNG)
		if !sized {
			t.Fatal("the converted picture is not a PNG this package would serve")
		}

		if width*height > maxThumbnailPixels {
			t.Fatalf("the picture is %dx%d", width, height)
		}
	})
}

// checkPreview holds whatever a parser returned to the bounds this package
// publishes.
func checkPreview(t *testing.T, got Preview) {
	t.Helper()

	if got.Thumbnail == nil {
		return
	}

	if len(got.Thumbnail.Bytes) > MaxThumbnailBytes {
		t.Fatalf("the picture is %d bytes and the bound is %d",
			len(got.Thumbnail.Bytes), MaxThumbnailBytes)
	}

	if got.Thumbnail.Width <= 0 || got.Thumbnail.Height <= 0 {
		t.Fatalf("the picture is %dx%d", got.Thumbnail.Width, got.Thumbnail.Height)
	}

	if got.Thumbnail.MediaType != mediaPNG && got.Thumbnail.MediaType != mediaJPEG {
		t.Fatalf("the picture is served as %q", got.Thumbnail.MediaType)
	}
}

// seedFile reads one of the slicer's own files into the corpus, so the fuzzer
// starts from a file that is real rather than from noise.
func seedFile(f *testing.F, name string) []byte {
	f.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.Fatalf("read the seed file: %v", err)
	}

	return raw
}
