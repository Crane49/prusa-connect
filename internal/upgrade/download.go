package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// MaxSumsBytes bounds a downloaded SHA256SUMS. A release lists a few
	// dozen files, a few kilobytes.
	MaxSumsBytes int64 = 1 << 20
	// MaxSignatureBytes bounds a downloaded SHA256SUMS.sig, one small JSON
	// document.
	MaxSignatureBytes int64 = 64 << 10
)

// ErrDownloadSize reports a release file whose length is not the one the
// catalogue declared, or that exceeds its cap.
var ErrDownloadSize = errors.New("upgrade: downloaded release file has the wrong size")

// DownloadedRelease is a release fetched into a private directory. Binary is
// a file there; the two small documents are held in memory.
type DownloadedRelease struct {
	Binary    string
	Sums      []byte
	Signature []byte
}

// DownloadRelease streams the binary of a catalogue release to a 0600 file in
// dir and reads its two signature documents under their own caps. Every file
// must have exactly the size the catalogue declared.
func DownloadRelease(
	ctx context.Context, source ReleaseSource, release Release, dir string, maxBinary int64,
) (DownloadedRelease, error) {
	if maxBinary <= 0 {
		maxBinary = DefaultMaxBinaryBytes
	}
	if err := os.MkdirAll(dir, privateDir); err != nil {
		return DownloadedRelease{}, fmt.Errorf("upgrade: create download directory: %w", err)
	}

	binary := filepath.Join(dir, filepath.Base(release.Binary.Name))
	if err := downloadToFile(ctx, source, release.Binary, binary, maxBinary); err != nil {
		_ = os.Remove(binary)

		return DownloadedRelease{}, err
	}
	sums, err := downloadToMemory(ctx, source, release.Sums, MaxSumsBytes)
	if err != nil {
		_ = os.Remove(binary)

		return DownloadedRelease{}, err
	}
	signature, err := downloadToMemory(ctx, source, release.Signature, MaxSignatureBytes)
	if err != nil {
		_ = os.Remove(binary)

		return DownloadedRelease{}, err
	}

	return DownloadedRelease{Binary: binary, Sums: sums, Signature: signature}, nil
}

func downloadToFile(ctx context.Context, source ReleaseSource, asset Asset, path string, limit int64) error {
	if err := checkDeclaredSize(asset, limit); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFile)
	if err != nil {
		return fmt.Errorf("upgrade: create downloaded %s: %w", asset.Name, err)
	}
	counted := &cappedWriter{to: file, left: asset.Size}
	downloadErr := source.Download(ctx, asset, counted)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(downloadErr, syncErr, closeErr); err != nil {
		return err
	}

	return counted.complete(asset)
}

func downloadToMemory(ctx context.Context, source ReleaseSource, asset Asset, limit int64) ([]byte, error) {
	if err := checkDeclaredSize(asset, limit); err != nil {
		return nil, err
	}
	var content memoryWriter
	counted := &cappedWriter{to: &content, left: asset.Size}
	if err := source.Download(ctx, asset, counted); err != nil {
		return nil, err
	}
	if err := counted.complete(asset); err != nil {
		return nil, err
	}

	return content, nil
}

func checkDeclaredSize(asset Asset, limit int64) error {
	if asset.Size < 1 || asset.Size > limit {
		return fmt.Errorf("%w: %s declares %d bytes, the limit is 1 to %d",
			ErrDownloadSize, asset.Name, asset.Size, limit)
	}

	return nil
}

// cappedWriter refuses the first byte past the declared size, so an answer
// longer than the catalogue said stops at once.
type cappedWriter struct {
	to   io.Writer
	left int64
}

func (w *cappedWriter) Write(chunk []byte) (int, error) {
	if int64(len(chunk)) > w.left {
		return 0, fmt.Errorf("%w: the server sent more than the catalogue declared", ErrDownloadSize)
	}
	written, err := w.to.Write(chunk)
	w.left -= int64(written)

	return written, err
}

func (w *cappedWriter) complete(asset Asset) error {
	if w.left != 0 {
		return fmt.Errorf("%w: %s ended %d bytes short of the declared %d",
			ErrDownloadSize, asset.Name, w.left, asset.Size)
	}

	return nil
}

type memoryWriter []byte

func (w *memoryWriter) Write(chunk []byte) (int, error) {
	*w = append(*w, chunk...)

	return len(chunk), nil
}
