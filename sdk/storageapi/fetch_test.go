package storageapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module reads a large file through storage.fetch, over a real session on
// operating system pipes, the channel a host hands a child (ADR-20261027-wvj
// rules 13 and 14).

// heldFile is a host that holds one file in memory under one name, or under
// one handle.
type heldFile struct {
	content []byte
	handle  string
}

func (h heldFile) known(principalHandle, path string) error {
	if principalHandle == h.handle && h.handle != "" {
		return nil
	}

	if path == "part.bin" {
		return nil
	}

	return fmt.Errorf("%w: unknown", storageapi.ErrNoFile)
}

func (h heldFile) Stat(_ context.Context, params storageapi.StatParams) (storageapi.FileInfo, error) {
	if err := h.known(params.Handle, params.Path); err != nil {
		return storageapi.FileInfo{}, err
	}

	return storageapi.FileInfo{Name: "part.bin", Path: "part.bin", Size: int64(len(h.content))}, nil
}

func (h heldFile) Read(_ context.Context, params storageapi.ReadParams) (storageapi.Chunk, error) {
	if err := h.known(params.Handle, params.Path); err != nil {
		return storageapi.Chunk{}, err
	}

	end := min(int64(len(h.content)), params.Offset+int64(params.Length))
	start := min(params.Offset, end)

	return storageapi.Chunk{
		Data: h.content[start:end], Size: int64(len(h.content)), EOF: end == int64(len(h.content)),
	}, nil
}

func (h heldFile) Fetch(
	_ context.Context, params storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	if err := h.known(params.Handle, params.Path); err != nil {
		return storageapi.Fetched{}, err
	}

	end := min(int64(len(h.content)), params.Offset+int64(params.Length))
	start := min(params.Offset, end)

	n, err := w.Write(h.content[start:end])

	return storageapi.Fetched{Size: int64(len(h.content)), Length: n}, err
}

// Write refuses: a held file is read and never written.
func (h heldFile) Write(context.Context, storageapi.WriteParams, io.Reader) (storageapi.Written, error) {
	return storageapi.Written{}, storageapi.ErrDenied
}

func (h heldFile) Move(context.Context, storageapi.MoveParams) (storageapi.Written, error) {
	return storageapi.Written{}, storageapi.ErrDenied
}

// List refuses: a held file is one file and no folder.
func (h heldFile) List(context.Context, storageapi.ListParams) (storageapi.Listing, error) {
	return storageapi.Listing{}, storageapi.ErrNoFile
}

// pipeEnd is one side of a duplex channel made of two pipes.
type pipeEnd struct {
	reader *os.File
	writer *os.File
}

func (c pipeEnd) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c pipeEnd) Write(p []byte) (int, error) { return c.writer.Write(p) }

// serveFile runs a host serving the storage methods over one file, and
// answers the child's session.
func serveFile(tb testing.TB, files storageapi.Files) *wirerpc.Conn {
	tb.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		tb.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		tb.Fatalf("open a pipe: %v", err)
	}

	host, err := wirerpc.NewConn(pipeEnd{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost, Methods: storageapi.Handlers(files)})
	if err != nil {
		tb.Fatalf("build the host: %v", err)
	}

	child, err := wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild})
	if err != nil {
		tb.Fatalf("build the child: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Add(2)

	go func() { defer running.Done(); _ = host.Serve(ctx) }()
	go func() { defer running.Done(); _ = child.Serve(ctx) }()

	tb.Cleanup(func() {
		cancel()
		_ = host.Close()
		_ = child.Close()
		_ = hostWrites.Close()
		_ = childWrites.Close()
		running.Wait()
	})

	return child
}

func randomBytes(size int) []byte {
	out := make([]byte, size)
	source := rand.NewChaCha8([32]byte{7})
	_, _ = source.Read(out)

	return out
}

func TestFetchReadsAFileThroughAStream(t *testing.T) {
	t.Parallel()

	content := randomBytes(storageapi.MaxFetchBytes + 3<<20)
	client := storageapi.NewClient(serveFile(t, heldFile{content: content}))

	file, err := client.Open(t.Context(), storageapi.Target{Storage: "bench", Path: "part.bin"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if file.Size() != int64(len(content)) {
		t.Fatalf("the size is %d, want %d", file.Size(), len(content))
	}

	// One read across the bound of a single fetch.
	whole := make([]byte, len(content))
	if n, readErr := file.ReadAt(whole, 0); readErr != nil || n != len(content) {
		t.Fatalf("read the whole file: %d, %v", n, readErr)
	}

	if !bytes.Equal(whole, content) {
		t.Fatal("the bytes that arrived are not the file's")
	}

	// A range past the end reads what is there and says io.EOF, as
	// io.ReaderAt does.
	tail := make([]byte, 100)
	n, err := file.ReadAt(tail, int64(len(content)-10))
	if n != 10 || !errors.Is(err, io.EOF) || !bytes.Equal(tail[:10], content[len(content)-10:]) {
		t.Fatalf("a read past the end answered %d, %v", n, err)
	}

	if n, err = file.ReadAt(tail, int64(len(content))); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("a read at the end answered %d, %v", n, err)
	}
}

func TestAFileFromAHandleIsAZipReaderAt(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer

	writer := zip.NewWriter(&archive)

	entry, err := writer.Create("Metadata/thumbnail.png")
	if err != nil {
		t.Fatal(err)
	}

	_, _ = entry.Write([]byte("picture"))

	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	client := storageapi.NewClient(serveFile(t, heldFile{content: archive.Bytes(), handle: "h1"}))

	file, err := client.Open(t.Context(), storageapi.Target{Handle: "h1"})
	if err != nil {
		t.Fatalf("open through a handle: %v", err)
	}

	read, err := zip.NewReader(file, file.Size())
	if err != nil || len(read.File) != 1 || read.File[0].Name != "Metadata/thumbnail.png" {
		t.Fatalf("archive/zip read %v, %v", read, err)
	}

	if _, err = client.Open(t.Context(), storageapi.Target{Handle: "h2"}); !errors.Is(err, storageapi.ErrNoFile) {
		t.Fatalf("a handle the host did not give read as %v", err)
	}
}

func TestAHandleIsNeverNamedWithAPath(t *testing.T) {
	t.Parallel()

	both := storageapi.FetchParams{Handle: "h", Storage: "s", Path: "p"}
	if err := both.Validate(); !errors.Is(err, storageapi.ErrDenied) {
		t.Fatalf("a handle with a path held as %v", err)
	}

	over := storageapi.FetchParams{Storage: "s", Path: "p", Length: storageapi.MaxFetchBytes + 1}
	if err := over.Validate(); !errors.Is(err, storageapi.ErrInvalidRange) {
		t.Fatalf("a fetch over the bound held as %v", err)
	}
}

// benchSize is the file the measurement reads: the size of a large 3MF
// project.
const benchSize = 100 << 20

// BenchmarkReadAWholeFile reads a 100 MiB file once per round, through the
// JSON read and through the stream. ADR-20261027-wvj quotes the numbers.
func BenchmarkReadAWholeFile(b *testing.B) {
	content := randomBytes(benchSize)

	b.Run("storage.read", func(b *testing.B) {
		client := storageapi.NewClient(serveFile(b, heldFile{content: content}))
		b.SetBytes(benchSize)

		for b.Loop() {
			var offset int64

			for {
				chunk, err := client.Read(b.Context(), storageapi.ReadParams{
					Storage: "bench", Path: "part.bin", Offset: offset, Length: storageapi.MaxReadBytes,
				})
				if err != nil {
					b.Fatal(err)
				}

				offset += int64(len(chunk.Data))
				if chunk.EOF {
					break
				}
			}
		}
	})

	b.Run("storage.fetch", func(b *testing.B) {
		client := storageapi.NewClient(serveFile(b, heldFile{content: content}))
		buffer := make([]byte, benchSize)
		b.SetBytes(benchSize)

		for b.Loop() {
			file, err := client.Open(b.Context(), storageapi.Target{Storage: "bench", Path: "part.bin"})
			if err != nil {
				b.Fatal(err)
			}

			if _, err = file.ReadAt(buffer, 0); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// TestManySmallFetchesAllArrive reads a file in pieces of a few chunks each,
// many times over. A fetch closes its stream as soon as the answer lands,
// and the bytes still in the stream's buffer must reach the reader whichever
// of the two wake-ups the reader sees first.
func TestManySmallFetchesAllArrive(t *testing.T) {
	t.Parallel()

	content := randomBytes(1 << 20)
	client := storageapi.NewClient(serveFile(t, heldFile{content: content}))

	file, err := client.Open(t.Context(), storageapi.Target{Storage: "bench", Path: "part.bin"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	piece := make([]byte, 80<<10)

	for round := range 3000 {
		offset := int64(round*4096) % (file.Size() - int64(len(piece)))

		if n, readErr := file.ReadAt(piece, offset); readErr != nil || n != len(piece) {
			t.Fatalf("round %d read %d bytes: %v", round, n, readErr)
		}

		if !bytes.Equal(piece, content[offset:offset+int64(len(piece))]) {
			t.Fatalf("round %d read other bytes", round)
		}
	}
}
