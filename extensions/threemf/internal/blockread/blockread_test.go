package blockread

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// counting is a ReaderAt that counts the reads it answers.
type counting struct {
	data  []byte
	reads int
}

func (c *counting) ReadAt(p []byte, off int64) (int, error) {
	c.reads++

	return bytes.NewReader(c.data).ReadAt(p, off)
}

func TestSmallReadsCostOneReadPerBlock(t *testing.T) {
	t.Parallel()

	data := make([]byte, 10_000)
	for i := range data {
		data[i] = byte(i)
	}

	src := &counting{data: data}
	r := New(src, int64(len(data)), 4096, 2)

	got := make([]byte, 0, len(data))
	piece := make([]byte, 100)

	for off := int64(0); off < int64(len(data)); off += int64(len(piece)) {
		n, err := r.ReadAt(piece, off)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("read at %d: %v", off, err)
		}

		got = append(got, piece[:n]...)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("the bytes read differ from the file")
	}

	if src.reads != 3 {
		t.Errorf("the file was read %d times, want 3, one per block", src.reads)
	}
}

func TestAReadPastTheEndAnswersEOF(t *testing.T) {
	t.Parallel()

	r := New(bytes.NewReader([]byte("hello")), 5, 2, 0)
	p := make([]byte, 4)

	n, err := r.ReadAt(p, 3)
	if n != 2 || !errors.Is(err, io.EOF) || string(p[:n]) != "lo" {
		t.Fatalf("got %d %q %v, want 2 \"lo\" EOF", n, p[:n], err)
	}

	if _, err = r.ReadAt(p, -1); !errors.Is(err, ErrNegativeOffset) {
		t.Fatalf("a negative offset answered %v", err)
	}
}

func TestOnlyTheNewestBlocksAreKept(t *testing.T) {
	t.Parallel()

	src := &counting{data: make([]byte, 40)}
	r := New(src, 40, 10, 2)
	one := make([]byte, 1)

	for _, off := range []int64{0, 10, 0, 20, 0, 10} {
		if _, err := r.ReadAt(one, off); err != nil {
			t.Fatal(err)
		}
	}

	// 0 and 10 are read, 0 is kept warm, 20 pushes 10 out, so 10 is read
	// again: four reads.
	if src.reads != 4 {
		t.Errorf("the file was read %d times, want 4", src.reads)
	}
}
