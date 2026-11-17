package threemf

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
)

// The end of central directory record, the last thing in a zip: a four-byte
// signature, then fields, with the number of entries at offset 10. A comment
// of up to 65535 bytes may follow it, so it is looked for from the end.
const (
	eocdBytes      = 22
	eocdCountAt    = 10
	eocdMaxComment = 1 << 16
	// eocdCountOverflow is the count a zip writes when the real count is in
	// the Zip64 record. archive/zip reads that record, and the count is then
	// checked after it has.
	eocdCountOverflow = 0xFFFF
)

//nolint:gochecknoglobals // a constant byte string.
var eocdSignature = []byte{'P', 'K', 0x05, 0x06}

// archive is an opened zip with its entries by name.
type archive struct {
	zip    *zip.Reader
	byName map[string]*zip.File
	// byFold is the same map keyed by the lower-case name, for the few
	// entries a slicer looks up without regard to case.
	byFold map[string]*zip.File
	limits Limits
	// modelBytes is how many bytes of model XML have been read so far.
	modelBytes int64
}

// openArchive opens r as a zip, after checking the entry count the archive
// declares. A zip that declares a million entries would cost a million structs
// inside zip.NewReader before any check could run.
func openArchive(r io.ReaderAt, size int64, limits Limits) (*archive, error) {
	if r == nil || size <= 0 {
		return nil, formatError("empty file")
	}

	if count, ok := declaredEntries(r, size); ok && count > limits.MaxEntries && count != eocdCountOverflow {
		return nil, limitError("MaxEntries", int64(limits.MaxEntries))
	}

	reader, err := zip.NewReader(r, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: %w", ErrFormat, err)
	}

	if len(reader.File) > limits.MaxEntries {
		return nil, limitError("MaxEntries", int64(limits.MaxEntries))
	}

	a := &archive{
		zip:    reader,
		byName: make(map[string]*zip.File, len(reader.File)),
		byFold: make(map[string]*zip.File, len(reader.File)),
		limits: limits,
	}

	for _, f := range reader.File {
		// Two entries of one name are refused: a reader that takes the first
		// and a slicer that takes the last would see two different projects.
		if _, dup := a.byName[f.Name]; dup {
			return nil, formatError("two entries named %q", f.Name)
		}

		a.byName[f.Name] = f

		fold := strings.ToLower(f.Name)
		if _, dup := a.byFold[fold]; !dup {
			a.byFold[fold] = f
		}
	}

	return a, nil
}

// declaredEntries reads the entry count from the end of central directory
// record, and reports false when the record cannot be found.
func declaredEntries(r io.ReaderAt, size int64) (int, bool) {
	window := min(int64(eocdBytes+eocdMaxComment), size)
	tail := make([]byte, window)

	n, err := r.ReadAt(tail, size-window)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, false
	}

	tail = tail[:n]

	for at := len(tail) - eocdBytes; at >= 0; at-- {
		if !bytes.HasPrefix(tail[at:], eocdSignature) {
			continue
		}

		return int(binary.LittleEndian.Uint16(tail[at+eocdCountAt:])), true
	}

	return 0, false
}

// entry finds the entry of a part name. A name written in a relationship or
// a p:path is a URI, so %XX escapes stand for the bytes they encode, and the
// Open Packaging Conventions compare part names without regard to case. The
// exact name wins over every other match.
func (a *archive) entry(name string) *zip.File {
	if f := a.byName[name]; f != nil {
		return f
	}

	if decoded, err := url.PathUnescape(name); err == nil && decoded != name {
		if f := a.byName[decoded]; f != nil {
			return f
		}

		name = decoded
	}

	return a.byFold[strings.ToLower(name)]
}

// entryFold finds an entry by name without regard to case.
func (a *archive) entryFold(name string) *zip.File {
	if f := a.byName[name]; f != nil {
		return f
	}

	return a.byFold[strings.ToLower(name)]
}

// resolve turns a target written inside the archive, such as the Target of a
// relationship or a p:path, into an entry name. It refuses anything that is
// not a plain path inside the archive: a scheme, a backslash, a NUL, and a
// path that climbs out with "..".
func resolve(base, target string) (string, bool) {
	if target == "" || strings.ContainsAny(target, "\\\x00") || strings.Contains(target, "://") {
		return "", false
	}

	for part := range strings.SplitSeq(target, "/") {
		if part == ".." {
			return "", false
		}
	}

	joined := target
	if !strings.HasPrefix(target, "/") {
		joined = path.Join("/", path.Dir("/"+base), target)
	}

	clean := strings.TrimPrefix(path.Clean(joined), "/")
	if clean == "" || clean == "." {
		return "", false
	}

	return clean, true
}

// readAll reads one entry whole, if it declares no more than limit bytes and
// holds no more than it declares. The declared size is the archive's own claim,
// so the read is held to the limit as well.
func readAll(ctx context.Context, f *zip.File, limit int64, limitName string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if limit < 0 || f.UncompressedSize64 > uint64(limit) {
		return nil, limitError(limitName, limit)
	}

	body, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %w", ErrFormat, f.Name, err)
	}

	defer func() { _ = body.Close() }() // a reader's Close reports nothing a read did not.

	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrFormat, f.Name, err)
	}

	if int64(len(data)) > limit {
		return nil, limitError(limitName, limit)
	}

	return data, nil
}

// openModel opens a model entry for streaming, counting its bytes against
// MaxModelBytes for the whole file.
func (a *archive) openModel(f *zip.File) (io.ReadCloser, error) {
	left := a.limits.MaxModelBytes - a.modelBytes
	if left <= 0 || f.UncompressedSize64 > uint64(left) {
		return nil, limitError("MaxModelBytes", a.limits.MaxModelBytes)
	}

	body, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %w", ErrFormat, f.Name, err)
	}

	return &countingReader{body: body, a: a}, nil
}

// countingReader charges every byte read to the archive's model budget.
type countingReader struct {
	body io.ReadCloser
	a    *archive
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	c.a.modelBytes += int64(n)

	if c.a.modelBytes > c.a.limits.MaxModelBytes {
		return n, limitError("MaxModelBytes", c.a.limits.MaxModelBytes)
	}

	return n, err //nolint:wrapcheck // the reader's own error, passed through as io.Reader requires.
}

func (c *countingReader) Close() error { return c.body.Close() } //nolint:wrapcheck // as above.
