package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

const (
	// printerFolder is where a file from the network lands on the printer.
	printerFolder = "usb/"
	// stemLength is how many characters of the digest name a file whose URL
	// has no usable name.
	stemLength = 12
	// retryFloor is the shortest wait between two attempts of one fetch.
	retryFloor = time.Second
)

// binaryMagic starts every binary G-code file.
var binaryMagic = []byte("GCDE")

var (
	// ErrNotAFile means a fractal URL names a listing this server publishes.
	ErrNotAFile = errors.New("the identity names a listing, not a file")
	// ErrNoParent means this server has no parent with a files entry.
	ErrNoParent = errors.New("this server has no parent with a files entry")
	// ErrDropped means the copies of files from above were dropped while
	// the file was fetched.
	ErrDropped = errors.New("the copies of files from above were dropped")
	// ErrTooLarge means an object is larger than Listing may return.
	ErrTooLarge = errors.New("the object is larger than one answer may hold")
	// ErrRetention means the parent gave the object a retention other than
	// unlimited, so the module does not print it.
	ErrRetention = errors.New("the parent gave the object a retention other than unlimited")
)

// FilesConfig is the input of NewFiles.
//
//nolint:revive // Config alone would read as the settings of the whole extension.
type FilesConfig struct {
	Publisher Objects
	Storage   Storage
	Cache     *Cache
	Client    *Client
	// Parent returns the base URL of the parent's files entry.
	Parent func() (baseURL string, ok bool)
	// Attempts is files.fetch_attempts.
	Attempts int
}

// Files is what the bridge calls to print a fractal URL.
type Files struct {
	cfg FilesConfig
}

// NewFiles builds Files.
func NewFiles(cfg FilesConfig) *Files { return &Files{cfg: cfg} }

// Reachable reports whether this server publishes id, has a copy of it, or
// has a parent with a files entry to ask. It proves only that there is
// somewhere to ask.
func (f *Files) Reachable(ctx context.Context, id sdkfiles.Identity) bool {
	if _, ok := f.cfg.Publisher.Lookup(ctx, id); ok {
		return true
	}

	if f.cfg.Cache.Path(id) != "" {
		return true
	}

	_, ok := f.parent()

	return ok
}

// Obtain returns where the core reads the file a URL names: the storage of a
// file this server publishes, or module:data and a copy it has or fetches.
// release unpins the copy.
func (f *Files) Obtain(ctx context.Context, u sdkfiles.URL) (storage, source string, release func(), err error) {
	if published, ok := f.cfg.Publisher.Lookup(ctx, u.ID); ok {
		if published.Listing != nil {
			return "", "", nil, ErrNotAFile
		}

		return published.Storage, published.Path, func() {}, nil
	}

	if held, unpin, ok := f.cfg.Cache.Hold(u.ID); ok {
		return deviceapi.ModuleDataStorage, held, unpin, nil
	}

	held, unpin, err := f.fetch(ctx, u.ID)
	if err != nil {
		return "", "", nil, err
	}

	return deviceapi.ModuleDataStorage, held, unpin, nil
}

// Listing returns the bytes of the object id, from a copy or from the parent,
// when it is at most limit bytes. It serves browsing a store above this
// server. A fetch stops at the first byte past limit and keeps nothing.
func (f *Files) Listing(ctx context.Context, id sdkfiles.Identity, limit int) ([]byte, error) {
	if _, unpin, ok := f.cfg.Cache.Hold(id); ok {
		defer unpin()

		data, err := f.cfg.Cache.head(id, limit+1)
		if err == nil && len(data) > limit {
			err = ErrTooLarge
		}

		return data, err
	}

	base, ok := f.parent()
	if !ok {
		return nil, ErrNoParent
	}

	held := &boundedSink{limit: int64(limit)}
	if _, err := f.cfg.Client.Fetch(ctx, base, id, held, false); err != nil {
		return nil, err
	}

	return held.data, nil
}

// boundedSink holds an object in memory and refuses a byte past limit.
type boundedSink struct {
	data  []byte
	at    int64
	limit int64
}

func (b *boundedSink) Write(p []byte) (int, error) {
	if b.at+int64(len(p)) > b.limit {
		return 0, ErrTooLarge
	}

	b.data = append(b.data[:b.at], p...)
	b.at += int64(len(p))

	return len(p), nil
}

func (b *boundedSink) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.at = offset
	case io.SeekEnd:
		b.at = int64(len(b.data)) + offset
	default:
		b.at += offset
	}

	return b.at, nil
}

func (b *boundedSink) Truncate(size int64) error {
	b.data = b.data[:min(size, int64(len(b.data)))]

	return nil
}

// fetch fetches id from the parent into the cache and pins the copy. It tries
// at most Attempts times, after the parent's Retry-After, never after a 404.
func (f *Files) fetch(ctx context.Context, id sdkfiles.Identity) (string, func(), error) {
	ctx, stop := f.cfg.Cache.untilDropped(ctx)
	defer stop()

	var failure error

	// fresh stays set once a copy did not match, so no attempt after it takes
	// a copy of a server in between.
	fresh := false

	for attempt := range max(f.cfg.Attempts, 1) {
		if attempt > 0 {
			if err := pause(ctx, failure); err != nil {
				return "", nil, err
			}
		}

		base, ok := f.parent()
		if !ok {
			return "", nil, ErrNoParent
		}

		held, unpin, err := f.cfg.Cache.PutHeld(id, func(sink Sink) (Retention, error) {
			return f.cfg.Client.Fetch(ctx, base, id, sink, fresh)
		})
		if err == nil {
			return held, unpin, nil
		}

		if cause := context.Cause(ctx); cause != nil {
			return "", nil, fmt.Errorf("fetch: %w", cause)
		}

		failure = err
		fresh = fresh || errors.Is(err, ErrMismatch)

		refused, isFetch := errors.AsType[*FetchError](err)
		if !isFetch || !refused.Retryable() {
			return "", nil, err
		}
	}

	return "", nil, failure
}

// pause waits until the parent's Retry-After, and at least retryFloor.
func pause(ctx context.Context, failure error) error {
	wait := retryFloor

	if refused, ok := errors.AsType[*FetchError](failure); ok {
		wait = max(wait, time.Until(refused.RetryAfter))
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait to fetch again: %w", context.Cause(ctx))
	case <-timer.C:
		return nil
	}
}

func (f *Files) parent() (string, bool) {
	if f.cfg.Parent == nil || f.cfg.Client == nil {
		return "", false
	}

	return f.cfg.Parent()
}

// PrinterPaths are where the file may land on the printer, in the order to
// try them: the URL's name, then that name with the start of the digest
// before its extension. With no usable name the only path is named from the
// digest, with .bgcode for a file that starts with GCDE and .gcode otherwise.
func (f *Files) PrinterPaths(ctx context.Context, u sdkfiles.URL, storage, source string) []string {
	_, digest, _ := strings.Cut(u.ID.Text(), ":")
	short := digest[:stemLength]

	if printerTakes(u.Name) {
		extension := path.Ext(u.Name)

		return []string{
			printerFolder + u.Name,
			printerFolder + strings.TrimSuffix(u.Name, extension) + "-" + short + extension,
		}
	}

	stem := printerFolder + "fractal-" + short

	if bytes.HasPrefix(f.head(ctx, u.ID, storage, source), binaryMagic) {
		return []string{stem + ".bgcode"}
	}

	return []string{stem + ".gcode"}
}

// printerTakes reports whether name is valid NFC, ends in .gcode or .bgcode,
// and has no character FAT refuses. The core reads a backslash as a separator.
func printerTakes(name string) bool {
	if name == "" || sdkfiles.CheckName(name) != nil || !norm.NFC.IsNormalString(name) {
		return false
	}

	if strings.ContainsAny(name, `\:*?"<>|`) {
		return false
	}

	if strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return false
	}

	extension := strings.ToLower(path.Ext(name))

	return extension == ".gcode" || extension == ".bgcode"
}

// head returns the first bytes of the file, or nil when they cannot be read.
func (f *Files) head(ctx context.Context, id sdkfiles.Identity, storage, source string) []byte {
	if storage == deviceapi.ModuleDataStorage {
		first, err := f.cfg.Cache.head(id, len(binaryMagic))
		if err != nil {
			return nil
		}

		return first
	}

	chunk, err := f.cfg.Storage.Read(
		ctx,
		storageapi.ReadParams{Storage: storage, Path: source, Length: len(binaryMagic)},
	)
	if err != nil {
		return nil
	}

	return chunk.Data
}
