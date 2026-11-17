package printfile

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestReadRealSlicerFiles reads the three files PrusaSlicer 2.9.6 exported from
// a 10 mm cube, and the cube's own STL. testdata/README.md says which command
// wrote each one.
func TestReadRealSlicerFiles(t *testing.T) {
	t.Parallel()

	sliced := scalarDetails{
		EstimatedSeconds: 136,
		FilamentMM:       133.16,
		FilamentG:        0.40,
		LayerHeightMM:    0.3,
		NozzleMM:         0.4,
		Material:         "PLA",
		PrinterModel:     "MK4IS",
		PrintProfile:     "0.20mm STRUCTURAL @MK4IS 0.4",
		PrinterProfile:   "Original Prusa MK4 Input Shaper 0.4 nozzle",
		FilamentProfile:  "Prusament PLA @PGIS",
	}

	cases := []struct {
		name string
		file string
		want scalarDetails
	}{
		{name: "text gcode", file: "cube10.gcode", want: sliced},
		{name: "binary gcode", file: "cube10.bgcode", want: sliced},
		{name: "project", file: "cube10.3mf", want: scalarDetails{}},
		{name: "mesh only", file: "cube10.stl", want: scalarDetails{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := readFile(t, filepath.Join("testdata", tc.file))
			if scalarsOf(got.Details) != tc.want {
				t.Errorf("details: got %+v, want %+v", scalarsOf(got.Details), tc.want)
			}

			// None of the three carries a picture, because PrusaSlicer renders
			// one in its window and not on the command line.
			if got.Thumbnail != nil {
				t.Errorf("thumbnail: got %dx%d, want none",
					got.Thumbnail.Width, got.Thumbnail.Height)
			}
		})
	}
}

// TestReadTextGCodePicture reads a picture out of a text G-code file, in each
// of the three encodings PrusaSlicer writes.
func TestReadTextGCodePicture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		kind      string
		picture   []byte
		mediaType string
	}{
		{name: "png", kind: "thumbnail", picture: fixturePNG(t, 32, 24), mediaType: mediaPNG},
		{name: "named png", kind: "thumbnail_PNG", picture: fixturePNG(t, 32, 24), mediaType: mediaPNG},
		{name: "qoi becomes png", kind: "thumbnail_QOI", picture: fixtureQOI(32, 24), mediaType: mediaPNG},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file := fixtureTextGCode(tc.kind, tc.picture, 32, 24, 0)

			got := Read(t.Context(), bytes.NewReader(file), int64(len(file)))
			if got.Thumbnail == nil {
				t.Fatal("no thumbnail")
			}

			if got.Thumbnail.MediaType != tc.mediaType {
				t.Errorf("media type: got %q, want %q", got.Thumbnail.MediaType, tc.mediaType)
			}

			if got.Thumbnail.Width != 32 || got.Thumbnail.Height != 24 {
				t.Errorf("size: got %dx%d, want 32x24", got.Thumbnail.Width, got.Thumbnail.Height)
			}

			decodePicture(t, got.Thumbnail.Bytes, 32, 24)
		})
	}
}

// TestReadTextGCodeReadsBothEnds proves the settings at the end of a long file
// are read without reading the middle of it.
func TestReadTextGCodeReadsBothEnds(t *testing.T) {
	t.Parallel()

	// Enough movement lines to push the settings well past the head this
	// package reads.
	file := fixtureTextGCode("thumbnail", fixturePNG(t, 16, 16), 16, 16, 40_000)
	if len(file) < headBytes {
		t.Fatalf("the fixture is %d bytes and the head read is %d", len(file), headBytes)
	}

	counted := &countingReaderAt{inner: bytes.NewReader(file)}

	got := Read(t.Context(), counted, int64(len(file)))
	if got.Details.EstimatedSeconds != 3736 {
		t.Errorf("estimated seconds: got %d, want 3736", got.Details.EstimatedSeconds)
	}

	if got.Thumbnail == nil {
		t.Error("no thumbnail")
	}

	bound := sniffBytes + headBytes + tailBytes
	if counted.bytes > bound {
		t.Errorf("read %d bytes, and the bound is %d", counted.bytes, bound)
	}
}

// TestReadBinaryGCode covers the container: an uncompressed metadata block, a
// deflated one, a picture, and a heatshrink block that has to be skipped.
func TestReadBinaryGCode(t *testing.T) {
	t.Parallel()

	picture := fixturePNG(t, 64, 48)

	file := fixtureBGCode(t, []bgcodeBlock{
		{
			kind: blockPrinterMetadata, compression: compressionNone, params: metadataParams(),
			data: []byte("printer_model=MK4IS\nnozzle_diameter=0.4\nfilament_type=PLA\nlayer_height=0.3\n"),
		},
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatPNG, 64, 48), data: picture,
		},
		{
			kind: blockPrintMetadata, compression: compressionDeflate, params: metadataParams(),
			data: []byte("filament used [mm]=133.16\nfilament used [g]=0.40\n" +
				"estimated printing time (normal mode)=2m 16s\n"),
		},
		{
			kind: blockSlicerMetadata, compression: compressionHeatshrink2,
			params: metadataParams(), data: []byte("this block cannot be read"),
		},
		{kind: blockGCode, compression: compressionNone, params: metadataParams(), data: []byte("G1 X1\n")},
	})

	got := Read(t.Context(), bytes.NewReader(file), int64(len(file)))

	want := scalarDetails{
		EstimatedSeconds: 136,
		FilamentMM:       133.16,
		FilamentG:        0.40,
		LayerHeightMM:    0.3,
		NozzleMM:         0.4,
		Material:         "PLA",
		PrinterModel:     "MK4IS",
	}
	if scalarsOf(got.Details) != want {
		t.Errorf("details: got %+v, want %+v", scalarsOf(got.Details), want)
	}

	if got.Thumbnail == nil {
		t.Fatal("no thumbnail")
	}

	if !bytes.Equal(got.Thumbnail.Bytes, picture) {
		t.Error("the picture is not the one the block held")
	}
}

// TestBinaryGCodePicksTheLargestPicture proves a file with several pictures
// serves the biggest, so one page can show a large preview and another can
// scale it down.
func TestBinaryGCodePicksTheLargestPicture(t *testing.T) {
	t.Parallel()

	file := fixtureBGCode(t, []bgcodeBlock{
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatPNG, 16, 16), data: fixturePNG(t, 16, 16),
		},
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatQOI, 48, 32), data: fixtureQOI(48, 32),
		},
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatPNG, 24, 24), data: fixturePNG(t, 24, 24),
		},
	})

	got := Read(t.Context(), bytes.NewReader(file), int64(len(file)))
	if got.Thumbnail == nil {
		t.Fatal("no thumbnail")
	}

	if got.Thumbnail.Width != 48 || got.Thumbnail.Height != 32 {
		t.Errorf("size: got %dx%d, want 48x32", got.Thumbnail.Width, got.Thumbnail.Height)
	}

	if got.Thumbnail.MediaType != mediaPNG {
		t.Errorf("media type: got %q, want %q", got.Thumbnail.MediaType, mediaPNG)
	}

	decodePicture(t, got.Thumbnail.Bytes, 48, 32)
}

// TestReadProject reads the picture a project carries, and answers nothing for
// a zip that is not one.
func TestReadProject(t *testing.T) {
	t.Parallel()

	picture := fixturePNG(t, 40, 30)

	cases := []struct {
		name string
		file []byte
		want bool
	}{
		{name: "project thumbnail", file: fixtureProject(t, "Metadata/thumbnail.png", picture), want: true},
		{name: "some other zip", file: fixtureProject(t, "notes.txt", []byte("hello")), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Read(t.Context(), bytes.NewReader(tc.file), int64(len(tc.file)))
			if (got.Thumbnail != nil) != tc.want {
				t.Fatalf("thumbnail present: got %v, want %v", got.Thumbnail != nil, tc.want)
			}

			if tc.want && (got.Thumbnail.Width != 40 || got.Thumbnail.Height != 30) {
				t.Errorf("size: got %dx%d, want 40x30", got.Thumbnail.Width, got.Thumbnail.Height)
			}
		})
	}
}

// TestReadRefusesHostileInput proves that nothing a file claims about itself
// makes this package allocate in proportion to the claim.
func TestReadRefusesHostileInput(t *testing.T) {
	t.Parallel()

	huge := func(plain, packed uint32) []byte {
		file := []byte("GCDE")
		file = append(file, 1, 0, 0, 0, 0, 0)
		file = append(file, byte(blockThumbnail), 0, byte(compressionDeflate), 0)
		file = appendUint32(file, plain)
		file = appendUint32(file, packed)
		file = append(file, thumbnailParams(thumbFormatPNG, 100, 100)...)

		return file
	}

	cases := []struct {
		name string
		file []byte
	}{
		{name: "empty", file: nil},
		{name: "magic only", file: []byte("GCDE")},
		{name: "unknown version", file: []byte("GCDE\x09\x00\x00\x00\x00\x00")},
		{name: "block longer than the file", file: huge(1<<30, 1<<30)},
		{name: "block that claims to expand hugely", file: huge(1<<30, 4)},
		{name: "truncated zip", file: []byte("PK\x03\x04 and nothing else")},
		{name: "a text file", file: []byte("Dear team,\n\nthe printer is on fire.\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Read(t.Context(), bytes.NewReader(tc.file), int64(len(tc.file)))
			if !got.Empty() {
				t.Errorf("got %+v, want an empty preview", got)
			}
		})
	}
}

// TestReadRefusesAnOversizedPicture proves the bound on a picture is the
// package's and not the file's.
func TestReadRefusesAnOversizedPicture(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, MaxThumbnailBytes+1)
	copy(oversized, fixturePNG(t, 8, 8))

	file := fixtureBGCode(t, []bgcodeBlock{
		{
			kind: blockThumbnail, compression: compressionNone,
			params: thumbnailParams(thumbFormatPNG, 8, 8), data: oversized,
		},
	})

	if got := Read(t.Context(), bytes.NewReader(file), int64(len(file))); got.Thumbnail != nil {
		t.Errorf("got a thumbnail of %d bytes, want none", len(got.Thumbnail.Bytes))
	}
}

// TestSeconds covers the durations a slicer writes.
func TestSeconds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want int64
	}{
		{in: "2h 1m 30s", want: 7290},
		{in: "2m 16s", want: 136},
		{in: "18s", want: 18},
		{in: "1d 2h 3m 4s", want: 93784},

		// A slicer writes these every day, and a guard that compared the value
		// with the digit count answered zero for each of them.
		{in: "100m", want: 6000},
		{in: "120m", want: 7200},
		{in: "999s", want: 999},
		{in: "", want: 0},
		{in: "soon", want: 0},
		{in: "12", want: 0},
		{in: "99999999999999h", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			if got := seconds(tc.in); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestNumber covers the decimals a slicer writes, with and without a unit.
func TestNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want float64
	}{
		{in: "133.16", want: 133.16},
		{in: "0.4", want: 0.4},
		{in: "0.45mm", want: 0.45},
		{in: "-1", want: 0},
		{in: "many", want: 0},
		{in: "", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			if got := number(tc.in); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFirstFieldStaysInsideTheSchema proves a long value is cut to the length
// the API schema publishes, marker and all. A value one byte over what the
// schema says is a client's validator refusing the server's own answer.
func TestFirstFieldStaysInsideTheSchema(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 200)

	got := firstField(long)
	if len(got) > maxFieldBytes {
		t.Errorf("the value is %d bytes and the schema says %d", len(got), maxFieldBytes)
	}

	if !utf8.ValidString(got) {
		t.Error("the cut value is not valid UTF-8")
	}
}

// TestFirstField covers a multi-tool file, which names one value per tool.
func TestFirstField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "one tool", in: "PLA", want: "PLA"},
		{name: "several tools", in: "PLA;PETG;PLA", want: "PLA"},
		{name: "comma separated", in: "0.4,0.4", want: "0.4"},
		{name: "quoted", in: `"MK4IS"`, want: "MK4IS"},
		{name: "not utf-8", in: string([]byte{'P', 'L', 0xFF, 'A'}), want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := firstField(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func readFile(t *testing.T, path string) Preview {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the test file: %v", err)
	}

	t.Cleanup(func() { _ = file.Close() })

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("measure the test file: %v", err)
	}

	return Read(t.Context(), file, info.Size())
}

// decodePicture checks that what this package returned is a picture a browser
// could show, and that it is the size the details claim.
func decodePicture(t *testing.T, raw []byte, width, height int) {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode the returned picture: %v", err)
	}

	if got := img.Bounds(); got.Dx() != width || got.Dy() != height {
		t.Errorf("decoded size: got %dx%d, want %dx%d", got.Dx(), got.Dy(), width, height)
	}
}

func appendUint32(dst []byte, value uint32) []byte {
	return append(dst, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

// countingReaderAt counts how many bytes were read through it, so a test can
// hold a parser to the section bounds this package publishes.
type countingReaderAt struct {
	inner *bytes.Reader
	bytes int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	read, err := c.inner.ReadAt(p, off)
	c.bytes += read

	return read, err //nolint:wrapcheck // a test double passes the reader's own answer through.
}

// TestReadProfiles reads the profile names of a multi-tool file: a filament
// profile whose name holds a comma stays whole, the first tool's is kept, and
// a high flow nozzle is read from its flag.
func TestReadProfiles(t *testing.T) {
	t.Parallel()

	file := []byte("; print_settings_id = 0.20mm SPEED @COREONE HF0.4\n" +
		"; printer_settings_id = Prusa CORE One HF0.4 nozzle\n" +
		"; filament_settings_id = \"Generic PETG, dried @COREONE HF0.4\";\"Prusament PLA\"\n" +
		"; nozzle_high_flow = 1,0\n" +
		"G1 X1\n")

	got := Read(t.Context(), bytes.NewReader(file), int64(len(file))).Details

	want := Details{
		PrintProfile:    "0.20mm SPEED @COREONE HF0.4",
		PrinterProfile:  "Prusa CORE One HF0.4 nozzle",
		FilamentProfile: "Generic PETG, dried @COREONE HF0.4",
		HighFlow:        true,
	}
	if got.PrintProfile != want.PrintProfile || got.PrinterProfile != want.PrinterProfile ||
		got.FilamentProfile != want.FilamentProfile || got.HighFlow != want.HighFlow {
		t.Errorf("details: got %+v, want %+v", got, want)
	}
}

// scalarDetails is the part of Details a file sliced for one tool reports as
// single values. It is comparable, where Details is not.
type scalarDetails struct {
	EstimatedSeconds int64
	FilamentMM       float64
	FilamentG        float64
	LayerHeightMM    float64
	NozzleMM         float64
	Material         string
	PrinterModel     string
	PrintProfile     string
	PrinterProfile   string
	FilamentProfile  string
	HighFlow         bool
}

func scalarsOf(d Details) scalarDetails {
	return scalarDetails{
		EstimatedSeconds: d.EstimatedSeconds,
		FilamentMM:       d.FilamentMM,
		FilamentG:        d.FilamentG,
		LayerHeightMM:    d.LayerHeightMM,
		NozzleMM:         d.NozzleMM,
		Material:         d.Material,
		PrinterModel:     d.PrinterModel,
		PrintProfile:     d.PrintProfile,
		PrinterProfile:   d.PrinterProfile,
		FilamentProfile:  d.FilamentProfile,
		HighFlow:         d.HighFlow,
	}
}
