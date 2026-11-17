// Package install downloads PrusaSlicer builds, checks them against the
// digest their manifest names, unpacks them side by side under the module's
// data directory, and remembers what is installed.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// ErrDigest reports a download whose SHA-256 is not the one its manifest
// names. The download is deleted.
var ErrDigest = errors.New("the download does not match the digest its manifest names")

// ErrDownload reports a download that did not complete.
var ErrDownload = errors.New("the download did not complete")

// maxRedirects is how many redirects a download follows.
const maxRedirects = 5

// downloadTimeout bounds one download, however slow the line.
const downloadTimeout = 30 * time.Minute

// NewHTTPClient is the client every download uses. It follows a redirect only
// to another https address, as GitHub sends a release download to its file
// host.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: downloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("%w: a redirect to %s, which is not https", ErrDownload, req.URL.Scheme)
			}

			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: more than five redirects", ErrDownload)
			}

			return nil
		},
	}
}

// Fetch returns the path of the build's download in cacheDir, downloading it
// when the cache does not hold it. A cached file is named by its digest and is
// checked again before it is used, so a file that changed on disk is
// downloaded afresh.
//
// progress is called as bytes arrive, with the total when the server or the
// manifest gives one.
func Fetch(
	ctx context.Context, client *http.Client, build catalog.Build, cacheDir string,
	progress func(done, total int64),
) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return "", fmt.Errorf("make the download cache: %w", err)
	}

	// A build with no published digest is downloaded every time, because
	// nothing names the file in the cache before it arrives.
	if build.SHA256 != "" {
		cached := filepath.Join(cacheDir, build.SHA256)

		if digest, err := digestOf(cached); err == nil && digest == build.SHA256 {
			return cached, nil
		}
	}

	part, err := os.CreateTemp(cacheDir, "download.part-*")
	if err != nil {
		return "", fmt.Errorf("open a file for the download: %w", err)
	}

	partName := part.Name()
	kept := false

	defer func() {
		if !kept {
			_ = os.Remove(partName) // the download failed, and this is its partial file
		}
	}()

	digest, err := download(ctx, client, build, part, progress)

	// A full disk reports itself on Close, and nowhere else on some file
	// systems, so its error is the one that counts when the copy succeeded.
	if closeErr := part.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("%w: close the download: %w", ErrDownload, closeErr)
	}

	if err != nil {
		return "", err
	}

	if build.SHA256 != "" && digest != build.SHA256 {
		return "", fmt.Errorf("%w: it is %s and the manifest names %s", ErrDigest, digest, build.SHA256)
	}

	cached := filepath.Join(cacheDir, digest)

	if err = os.Rename(partName, cached); err != nil {
		return "", fmt.Errorf("keep the download: %w", err)
	}

	kept = true

	return cached, nil
}

func download(
	ctx context.Context, client *http.Client, build catalog.Build, into io.Writer,
	progress func(done, total int64),
) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, build.URL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDownload, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDownload, err)
	}

	defer func() { _ = response.Body.Close() }() // a read error below is the one worth reporting

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: the server answered %s", ErrDownload, response.Status)
	}

	total := build.Size
	if total == 0 && response.ContentLength > 0 {
		total = response.ContentLength
	}

	if total > catalog.MaxDownloadBytes {
		return "", fmt.Errorf("%w: it is %d bytes and the limit is %d", ErrDownload, total, catalog.MaxDownloadBytes)
	}

	hash := sha256.New()
	counter := &counting{report: progress, total: total}

	copied, err := io.Copy(
		io.MultiWriter(into, hash, counter),
		io.LimitReader(response.Body, catalog.MaxDownloadBytes+1),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDownload, err)
	}

	if copied > catalog.MaxDownloadBytes {
		return "", fmt.Errorf("%w: it is over the limit of %d bytes", ErrDownload, catalog.MaxDownloadBytes)
	}

	if build.Size > 0 && copied != build.Size {
		return "", fmt.Errorf("%w: %d bytes arrived and the manifest names %d", ErrDownload, copied, build.Size)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// counting reports how many bytes went through it.
type counting struct {
	report func(done, total int64)
	total  int64
	done   int64
}

func (c *counting) Write(p []byte) (int, error) {
	c.done += int64(len(p))

	if c.report != nil {
		c.report(c.done, c.total)
	}

	return len(p), nil
}

// digestOf is the SHA-256 of one file, lowercase hex.
func digestOf(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }() // read-only, so Close reports nothing worth keeping

	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
