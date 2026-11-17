package printfile

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The files in this test are not damaged files. They are files built to make
// this package spend a lot of memory and a lot of time on a small upload, and
// each one of them did before the bound beside it was added. The bounds asserted
// here are generous: they are far above what the package costs now and far
// below what it cost then, so the test says "the bound is still there" and not
// "this machine is this fast today".
const (
	// hostileDeadline is how long one hostile file may take to read. Reading
	// any of them took minutes before.
	hostileDeadline = 5 * time.Second

	// hostileAllocBytes is how much one hostile file may make this package
	// allocate. One of them asked for forty-five gigabytes before.
	hostileAllocBytes = 64 << 20
)

// TestManyPicturesCostOneDecode builds a container offering four thousand
// pictures, each one a tiny payload claiming to be a megapixel.
//
// Every block passes the per-block checks. What refused this file was nothing,
// because there was no bound across blocks: the reader decoded each picture in
// turn. One file gets one decode now, so the cost is the walk over the headers.
func TestManyPicturesCostOneDecode(t *testing.T) {
	blocks := make([]bgcodeBlock, 0, maxBlocks)
	for range maxBlocks {
		blocks = append(blocks, bgcodeBlock{
			kind: blockThumbnail, compression: compressionDeflate,
			params: thumbnailParams(thumbFormatQOI, 1024, 1024),
			data:   lyingQOI(1024, 1024),
		})
	}

	file := fixtureBGCode(t, blocks)
	t.Logf("the file is %d bytes and offers %d pictures", len(file), len(blocks))

	withinBounds(t, func() {
		if got := Read(t.Context(), bytes.NewReader(file), int64(len(file))); !got.Empty() {
			t.Error("a file of pictures that cannot be decoded reported a preview")
		}
	})
}

// TestManySettingsTablesCostOneBudget builds a container of metadata blocks,
// each inside the per-block bound and together far over it.
func TestManySettingsTablesCostOneBudget(t *testing.T) {
	table := strings.Repeat("printer_model=MK4IS\n", MaxMetadataBytes/20)

	blocks := make([]bgcodeBlock, 0, maxBlocks)
	for range maxBlocks {
		blocks = append(blocks, bgcodeBlock{
			kind: blockPrinterMetadata, compression: compressionDeflate,
			params: metadataParams(), data: []byte(table),
		})
	}

	file := fixtureBGCode(t, blocks)
	t.Logf("the file is %d bytes and offers %d settings tables", len(file), len(blocks))

	withinBounds(t, func() {
		got := Read(t.Context(), bytes.NewReader(file), int64(len(file)))
		if got.Details.PrinterModel != "MK4IS" {
			t.Errorf("printer model: got %q, want MK4IS", got.Details.PrinterModel)
		}
	})
}

// TestManyTextPicturesCostOneDecode fills the head of a text G-code file with
// picture blocks that each claim a megapixel in a handful of bytes.
func TestManyTextPicturesCostOneDecode(t *testing.T) {
	var file strings.Builder

	encoded := base64.StdEncoding.EncodeToString(lyingQOI(1024, 1024))

	blocks := 0
	for file.Len() < headBytes-len(encoded)-64 {
		fmt.Fprintf(&file, "; thumbnail_QOI begin 1024x1024 %d\n; %s\n; thumbnail_QOI end\n",
			len(encoded), encoded)

		blocks++
	}

	text := file.String()
	t.Logf("the head is %d bytes and offers %d pictures", len(text), blocks)

	withinBounds(t, func() {
		if got := Read(t.Context(), strings.NewReader(text), int64(len(text))); got.Thumbnail != nil {
			t.Error("a file of pictures that cannot be decoded reported one")
		}
	})
}

// TestManySettingsLinesKeepOnlyWhatIsRead fills a text file with settings
// nothing reads. The gathering map holds only the keys this package asks for,
// so a file naming a hundred thousand of them costs the same as one naming
// nine.
func TestManySettingsLinesKeepOnlyWhatIsRead(t *testing.T) {
	t.Parallel()

	var file strings.Builder

	for i := 0; file.Len() < headBytes; i++ {
		fmt.Fprintf(&file, "; setting_number_%d = %d\n", i, i)
	}

	file.WriteString("; printer_model = MK4IS\n")

	text := file.String()

	gathered := newCollector()
	scanTextGCode(text, gathered, newBudget(t.Context()))

	if len(gathered.values) > len(wanted) {
		t.Errorf("the collector holds %d settings and the package reads %d",
			len(gathered.values), len(wanted))
	}

	if got := gathered.details().PrinterModel; got != "MK4IS" {
		t.Errorf("printer model: got %q, want MK4IS", got)
	}
}

// TestAQOIHeaderIsCheckedAgainstItsPayload proves the picture buffer is not
// allocated from what the header claims.
//
// A QOI chunk stands for at most sixty-two pixels, so a payload of a few bytes
// cannot fill a megapixel however the header is written.
func TestAQOIHeaderIsCheckedAgainstItsPayload(t *testing.T) {
	t.Parallel()

	if _, ok := decodeQOI(lyingQOI(1024, 1024)); ok {
		t.Fatal("a header claiming a megapixel over a few bytes decoded")
	}

	// A picture whose payload can cover what its header claims still decodes,
	// so the check refuses a lie and not a small picture.
	if _, ok := decodeQOI(fixtureQOI(8, 8)); !ok {
		t.Error("a real picture did not decode")
	}
}

// TestASmuggledPictureIsRefused proves the bytes are decoded before they are
// served. An SVG carries script, and a file is free to call one a PNG.
func TestASmuggledPictureIsRefused(t *testing.T) {
	t.Parallel()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	cases := []struct {
		name string
		file []byte
	}{
		{
			name: "in a binary container",
			file: fixtureBGCode(t, []bgcodeBlock{{
				kind: blockThumbnail, compression: compressionNone,
				params: thumbnailParams(thumbFormatPNG, 64, 64), data: svg,
			}}),
		},
		{
			name: "in a text file",
			file: fixtureTextGCode("thumbnail", svg, 64, 64, 0),
		},
		{
			name: "in a project",
			file: fixtureProject(t, "Metadata/thumbnail.png", svg),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Read(t.Context(), bytes.NewReader(tc.file), int64(len(tc.file)))
			if got.Thumbnail != nil {
				t.Errorf("served %d bytes as %q", len(got.Thumbnail.Bytes), got.Thumbnail.MediaType)
			}
		})
	}
}

// TestAPictureIsMeasuredNotAsked proves the size a caller is told comes out of
// the picture and not out of what the file wrote beside it.
func TestAPictureIsMeasuredNotAsked(t *testing.T) {
	t.Parallel()

	file := fixtureBGCode(t, []bgcodeBlock{{
		kind: blockThumbnail, compression: compressionNone,
		params: thumbnailParams(thumbFormatPNG, 999, 7), data: fixturePNG(t, 40, 30),
	}})

	got := Read(t.Context(), bytes.NewReader(file), int64(len(file)))
	if got.Thumbnail == nil {
		t.Fatal("no thumbnail")
	}

	if got.Thumbnail.Width != 40 || got.Thumbnail.Height != 30 {
		t.Errorf("size: got %dx%d, want 40x30", got.Thumbnail.Width, got.Thumbnail.Height)
	}
}

// TestTheChosenPictureIsTheOneAPageWants proves the pick: the largest picture
// that fits the long edge a page shows, and the smallest when every picture is
// larger than that.
func TestTheChosenPictureIsTheOneAPageWants(t *testing.T) {
	t.Parallel()

	container := func(sizes ...[2]int) []byte {
		blocks := make([]bgcodeBlock, 0, len(sizes))
		for _, size := range sizes {
			blocks = append(blocks, bgcodeBlock{
				kind: blockThumbnail, compression: compressionNone,
				params: thumbnailParams(thumbFormatPNG, uint16(size[0]), uint16(size[1])),
				data:   fixturePNG(t, size[0], size[1]),
			})
		}

		return fixtureBGCode(t, blocks)
	}

	cases := []struct {
		name  string
		file  []byte
		width int
	}{
		{
			name:  "the largest that fits",
			file:  container([2]int{16, 16}, [2]int{800, 480}, [2]int{220, 124}),
			width: 800,
		},
		{
			name:  "the smallest when none fits",
			file:  container([2]int{1100, 100}, [2]int{1200, 60}, [2]int{1030, 200}),
			width: 1200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Read(t.Context(), bytes.NewReader(tc.file), int64(len(tc.file)))
			if got.Thumbnail == nil {
				t.Fatal("no thumbnail")
			}

			if got.Thumbnail.Width != tc.width {
				t.Errorf("width: got %d, want %d", got.Thumbnail.Width, tc.width)
			}
		})
	}
}

// TestAProjectClaimingManyEntriesIsRefusedBeforeItIsOpened proves the entry
// count is read out of the archive's own record, before the zip reader builds a
// struct for every entry it declares.
func TestAProjectClaimingManyEntriesIsRefusedBeforeItIsOpened(t *testing.T) {
	t.Parallel()

	// A real archive, with the entry count in its end-of-directory record
	// rewritten to claim more than this package will look at.
	file := fixtureProject(t, "Metadata/thumbnail.png", fixturePNG(t, 8, 8))

	at := bytes.LastIndex(file, []byte("PK\x05\x06"))
	if at < 0 {
		t.Fatal("the fixture has no end of directory record")
	}

	binary.LittleEndian.PutUint16(file[at+eocdCountAt:], maxProjectEntries+1)

	if entryCountAllowed(bytes.NewReader(file), int64(len(file))) {
		t.Error("an archive claiming more entries than this package reads was allowed")
	}
}

// TestACancelledReadStops proves the context reaches the walk.
func TestACancelledReadStops(t *testing.T) {
	t.Parallel()

	blocks := make([]bgcodeBlock, 0, maxBlocks)
	for range maxBlocks {
		blocks = append(blocks, bgcodeBlock{
			kind: blockPrinterMetadata, compression: compressionNone,
			params: metadataParams(), data: []byte("printer_model=MK4IS\n"),
		})
	}

	file := fixtureBGCode(t, blocks)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if got := Read(ctx, bytes.NewReader(file), int64(len(file))); !got.Empty() {
		t.Error("a cancelled read still reported a preview")
	}
}

// lyingQOI writes a QOI header declaring the given size over a payload that
// cannot possibly fill it.
func lyingQOI(width, height uint32) []byte {
	var out bytes.Buffer

	out.WriteString("qoif")
	_ = binary.Write(&out, binary.BigEndian, width)
	_ = binary.Write(&out, binary.BigEndian, height)
	out.WriteByte(4)
	out.WriteByte(0)
	out.Write([]byte{qoiOpRGB, 0x20, 0x40, 0x60})
	out.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})

	return out.Bytes()
}

// withinBounds runs the read and fails when it took too long or allocated too
// much. Both numbers are the point of the test, so both are reported.
//
// A test that calls this does not run in parallel. The allocation counter is
// the whole process's, so another test running beside this one would be counted
// into the answer.
func withinBounds(t *testing.T, read func()) {
	t.Helper()

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	started := time.Now()
	read()
	took := time.Since(started)

	runtime.ReadMemStats(&after)

	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("the read took %s and allocated %d bytes", took, allocated)

	if took > hostileDeadline {
		t.Errorf("the read took %s and the bound is %s", took, hostileDeadline)
	}

	if allocated > hostileAllocBytes {
		t.Errorf("the read allocated %d bytes and the bound is %d", allocated, hostileAllocBytes)
	}
}
