package storageapitest

// The range subtests. A provider that serves storageapi.RangeReader answers
// part of an object without a seekable handle, which is what a store reached
// over HTTP does with a Range header of its own and what makes a download
// resumable against a store that is not a disk.

import (
	"io"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The offsets and lengths the range subtests read with. They are named because
// each one is a case and not a number: a range wholly inside the object, one
// that runs to its end, and one that starts past it.
const (
	// rangeOffset and rangeLength read a window in the middle of the object.
	rangeOffset = 4
	rangeLength = 6
	// tailOffset reads from there to the end.
	tailOffset = 12
	// pastTheEnd is how far beyond the last byte the final case asks from.
	pastTheEnd = 64
)

const rangeContent = "0123456789abcdefghij"

// readRange stores rangeContent once and reads one range back out of it.
func readRange(t *testing.T, p storageapi.Provider, offset, length int64) string {
	t.Helper()
	ctx := t.Context()
	save(t, p, "range.gcode", rangeContent)

	rc, err := rangeReaderOf(t, p).OpenRange(ctx, "range.gcode", offset, length)
	if err != nil {
		t.Fatalf("OpenRange(%d, %d): %v", offset, length, err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read the range: %v", err)
	}
	return string(got)
}

func testRangeMiddle(t *testing.T, p storageapi.Provider) {
	if got := readRange(t, p, rangeOffset, rangeLength); got != rangeContent[rangeOffset:rangeOffset+rangeLength] {
		t.Errorf("OpenRange(4, 6) = %q, want %q", got, rangeContent[rangeOffset:rangeOffset+rangeLength])
	}
}

// A length of -1 means "to the end", which is how an open-ended HTTP Range
// header arrives.
func testRangeToTheEnd(t *testing.T, p storageapi.Provider) {
	if got := readRange(t, p, tailOffset, -1); got != rangeContent[tailOffset:] {
		t.Errorf("OpenRange(12, -1) = %q, want %q", got, rangeContent[tailOffset:])
	}
}

// An offset at the length is the byte after the last one. It has an answer,
// which is no bytes, and a provider that errors instead turns an empty range
// into a failed download.
func testRangeAtTheLength(t *testing.T, p storageapi.Provider) {
	if got := readRange(t, p, int64(len(rangeContent)), -1); got != "" {
		t.Errorf("OpenRange at the length = %q, want no bytes", got)
	}
}

func testRangePastTheLength(t *testing.T, p storageapi.Provider) {
	if got := readRange(t, p, int64(len(rangeContent))+pastTheEnd, -1); got != "" {
		t.Errorf("OpenRange past the length = %q, want no bytes", got)
	}
}
