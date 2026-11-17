package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// maxRedirects is how many redirects a download follows.
const maxRedirects = 10

// tempPattern names a downloaded catalogue in the data directory.
const tempPattern = "materials-catalogue-*.zip"

// Sweep removes the downloads left in dir by a process that died during an
// import, or by a removal that failed. Call it while no import runs.
func Sweep(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if ok, _ := filepath.Match(tempPattern, e.Name()); ok && !e.IsDir() {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Download fetches url into a temporary file in dir, which the answer removes
// when it is closed. It refuses a body over limit bytes, and a URL or a
// redirect that is not https. The client's own timeout applies.
func Download(client *http.Client, rawURL string, limit int64, dir string) Fetch {
	return func(ctx context.Context) (io.ReaderAt, int64, error) {
		if u, err := url.Parse(rawURL); err != nil || u.Scheme != "https" {
			return nil, 0, fmt.Errorf("refusing %q: not an https URL", rawURL)
		}

		c := *client
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing redirect to %s", req.URL.Redacted())
			}

			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}

			return nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}

		resp, err := c.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("download: %w", err)
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("download: status %d", resp.StatusCode)
		}

		return kept(resp.Body, limit, dir)
	}
}

// kept writes body to a temporary file, and removes it again when the body is
// more than limit bytes or cannot be read whole.
func kept(body io.Reader, limit int64, dir string) (io.ReaderAt, int64, error) {
	f, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}

	file := tempFile{f}

	n, err := io.Copy(f, io.LimitReader(body, limit+1))
	switch {
	case err != nil:
		err = fmt.Errorf("download: %w", err)
	case n > limit:
		err = fmt.Errorf("download: more than %d bytes", limit)
	}

	if err != nil {
		_ = file.Close()

		return nil, 0, err
	}

	return file, n, nil
}

// tempFile is a downloaded catalogue, removed when it is closed.
type tempFile struct{ *os.File }

func (t tempFile) Close() error {
	return errors.Join(t.File.Close(), os.Remove(t.Name()))
}
