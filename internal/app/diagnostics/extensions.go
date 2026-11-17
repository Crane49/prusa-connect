package diagnostics

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The most one module may add to one bundle.
const (
	maxExtensionBytes = 8 << 20
	maxExtensionPages = 64
	// maxExtensionFiles is room for a log and a metrics file for each printer
	// of a fleet of four hundred.
	maxExtensionFiles = 1024
	// fileCost is what a new file costs against maxExtensionBytes on top of
	// its name: the zip entry and the manifest line it adds. Without it a
	// module could send many empty files and fill memory with their names.
	fileCost = 128
)

// The time the bundle waits for modules.
const (
	maxModuleTime  = time.Minute
	maxCollectTime = 3 * time.Minute
	// requestShare divides the time a request has left, and the modules get
	// that part. The rest is for scrubbing, writing and sending the bundle.
	requestShare = 2
)

// The reasons manifest.json gives for a module whose files are short.
const (
	extensionTruncated = "truncated"
	extensionFailed    = "failed"
	extensionSkipped   = "skipped"
)

// The limit manifest.json names for a truncated module.
const (
	limitSize  = "size"
	limitPages = "pages"
	limitFiles = "files"
	limitTime  = "time"
)

// The causes a collection ends with when a module runs out of time.
var (
	errModuleTime  = errors.New("the module used its time for the bundle")
	errCollectTime = errors.New("the modules used the time for the bundle")
)

// ExtensionSource is one running module that adds files to a bundle.
type ExtensionSource struct {
	Instance ref.Ref
	Title    string
	// Collect asks the module for one page.
	Collect func(ctx context.Context, cursor string) (diagnosticsapi.Page, error)
}

// folder is where a module's files go under modules/. Neither a kind nor an
// id holds a '.', so two instances never share a folder, and the kind in
// front keeps a name such as "con" from being the whole folder name, which
// Windows refuses.
func (e ExtensionSource) folder() string {
	return e.Instance.Kind().String() + "." + e.Instance.ID()
}

// collected is what one module added to a bundle: its files in the order it
// first named them, and why they are short where they are.
type collected struct {
	files []*moduleFile
	// index finds a file by its exact name, so later text goes to it.
	index map[string]int
	// taken and folders hold every name and every folder above a name, in
	// lower case, so a name that clashes on a disk that ignores case is
	// refused.
	taken   map[string]bool
	folders map[string]bool
	size    int
	refused int
	reason  string
	limit   string
	code    string
}

type moduleFile struct {
	name string
	text strings.Builder
}

func newCollected() *collected {
	return &collected{index: map[string]int{}, taken: map[string]bool{}, folders: map[string]bool{}}
}

func (c *collected) truncate(limit string) {
	c.reason, c.limit = extensionTruncated, limit
}

// collectExtension asks one module for its files, page after page, until it
// says it has no more, and stops at the size, file, page and time limits. A
// module that fails keeps what it gave before, and the bundle is still built.
func collectExtension(
	ctx context.Context, collect func(ctx context.Context, cursor string) (diagnosticsapi.Page, error),
) *collected {
	held := newCollected()

	var cursor string

	for range maxExtensionPages {
		if ctx.Err() != nil {
			held.truncate(limitTime)

			return held
		}

		page, err := collect(ctx, cursor)
		if err == nil {
			err = page.Validate()
		}

		if err != nil {
			if ctx.Err() != nil {
				held.truncate(limitTime)

				return held
			}

			held.reason = extensionFailed
			if carried, ok := errors.AsType[*fault.Fault](err); ok {
				held.code = fmt.Sprint(carried.Code())
			}

			return held
		}

		if held.add(page.Files) || page.Cursor == "" {
			return held
		}

		cursor = page.Cursor
	}

	held.truncate(limitPages)

	return held
}

// add adds one page's text, the same name to the same file, and reports
// whether it stopped at a limit. Empty text adds no file, and a new name pays
// fileCost before its text.
func (c *collected) add(page []diagnosticsapi.File) (cut bool) {
	for _, file := range page {
		if file.Text == "" {
			continue
		}

		at, seen := c.index[file.Name]

		cost := 0
		if !seen {
			if !c.free(file.Name) {
				c.refused++

				continue
			}

			if len(c.files) == maxExtensionFiles {
				c.truncate(limitFiles)

				return true
			}

			cost = len(file.Name) + fileCost
		}

		text := file.Text
		if room := maxExtensionBytes - c.size - cost; len(text) > room {
			text = cutAtLine(text, max(room, 0))
		}

		if text == "" {
			c.truncate(limitSize)

			return true
		}

		if !seen {
			at = c.claim(file.Name)
		}

		c.files[at].text.WriteString(text)
		c.size += cost + len(text)

		if len(text) < len(file.Text) {
			c.truncate(limitSize)

			return true
		}
	}

	return false
}

// free reports whether name can be a new file: no file has the same name in
// another case, no file uses it as a folder, and no folder above it is a
// file.
func (c *collected) free(name string) bool {
	lower := strings.ToLower(name)
	if c.taken[lower] || c.folders[lower] {
		return false
	}

	for i := range len(lower) {
		if lower[i] == '/' && c.taken[lower[:i]] {
			return false
		}
	}

	return true
}

func (c *collected) claim(name string) int {
	lower := strings.ToLower(name)
	c.taken[lower] = true

	for i := range len(lower) {
		if lower[i] == '/' {
			c.folders[lower[:i]] = true
		}
	}

	c.index[name] = len(c.files)
	c.files = append(c.files, &moduleFile{name: name})

	return len(c.files) - 1
}

// cutAtLine is at most limit bytes of text, ending after a whole line where
// one fits, so a cut never leaves half of a value the scrubber would have
// recognised.
func cutAtLine(text string, limit int) string {
	text = text[:limit]

	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return text[:i+1]
	}

	return ""
}

// putExtensions asks each module for its files and writes them under
// modules/<kind>.<id>/ before it asks the next, so the bundle holds one
// module's files at a time. Each line is scrubbed and the whole file swept.
// It returns what manifest.json says about the modules.
func putExtensions(
	ctx context.Context, w *zip.Writer, s *Scrubber, now time.Time, sources []ExtensionSource,
) ([]map[string]any, error) {
	all, stopAll := budget(ctx, collectTime(ctx), errCollectTime)
	defer stopAll()

	sources = slices.SortedFunc(slices.Values(sources), func(a, b ExtensionSource) int {
		return strings.Compare(a.Instance.String(), b.Instance.String())
	})

	views := make([]map[string]any, 0, len(sources))

	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("collect the modules' files: %w", err)
		}

		view := map[string]any{"instance": source.Instance.String(), "title": source.Title}
		views = append(views, view)

		if all.Err() != nil {
			view["reason"] = extensionSkipped

			continue
		}

		one, stopOne := budget(all, maxModuleTime, errModuleTime)
		held := collectExtension(one, source.Collect)

		stopOne()

		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("collect the modules' files: %w", err)
		}

		sizes, err := held.put(ctx, w, s, now, "modules/"+source.folder()+"/")
		if err != nil {
			return nil, err
		}

		view["files"] = sizes
		describe(view, held)
	}

	return views, nil
}

// put writes the module's files under prefix and returns each written name
// with its size.
func (c *collected) put(
	ctx context.Context, w *zip.Writer, s *Scrubber, now time.Time, prefix string,
) (map[string]int, error) {
	sizes := make(map[string]int, len(c.files))

	for _, file := range c.files {
		text, err := scrubLines(ctx, s, file.text.String())
		if err != nil {
			return nil, err
		}

		name := scrubName(s, file.name)
		written := s.Sweep([]byte(text))

		if err = put(w, prefix+name, now, written); err != nil {
			return nil, err
		}

		// The size of what the zip holds, after the scrubbing.
		sizes[name] = len(written)
	}

	return sizes, nil
}

func describe(view map[string]any, held *collected) {
	if held.reason != "" {
		view["reason"] = held.reason
	}

	if held.limit != "" {
		view["limit"] = held.limit
	}

	if held.code != "" {
		view["code"] = held.code
	}

	if held.refused > 0 {
		view["refused"] = held.refused
	}
}

// scrubLinesCheck is how many lines scrubLines scrubs between two looks at
// the context.
const scrubLinesCheck = 1024

// scrubLines runs the text patterns over each line on its own, clipped to the
// length of a log line after the sweep, so one long line cannot make the
// patterns run long and a cut cannot leave the head of a known secret behind.
func scrubLines(ctx context.Context, s *Scrubber, text string) (string, error) {
	var out strings.Builder

	out.Grow(len(text))

	n := 0

	for line := range strings.Lines(text) {
		if n++; n%scrubLinesCheck == 0 {
			if err := ctx.Err(); err != nil {
				return "", fmt.Errorf("scrub the modules' files: %w", err)
			}
		}

		trimmed, newline := strings.CutSuffix(line, "\n")

		out.WriteString(s.Text(s.clip(trimmed)))

		if newline {
			out.WriteByte('\n')
		}
	}

	return out.String(), nil
}

// maxKeptExtension is the longest file extension a hashed name keeps, the
// dot included, as in ".jsonl".
const maxKeptExtension = 6

// scrubName replaces each part of a module's file name that the scrubber
// would change, such as a MAC or an address, with a hash of it. Within one
// bundle the same part gets the same hash, so manifest.json and the zip
// agree. A short extension stays, so the file still opens in the right
// program. A known secret can hold a '/', so no part of it matches alone:
// a name that still holds one after the parts are scrubbed is hashed whole.
func scrubName(s *Scrubber, name string) string {
	scrubbed := scrubSegments(s, name)
	if string(s.Sweep([]byte(scrubbed))) != scrubbed {
		return "h-" + strings.TrimPrefix(s.Hash(name), "h:")
	}

	return scrubbed
}

func scrubSegments(s *Scrubber, name string) string {
	segments := strings.Split(name, "/")

	for i, segment := range segments {
		if s.Text(segment) == segment {
			continue
		}

		token := "h-" + strings.TrimPrefix(s.Hash(segment), "h:")

		if ext := path.Ext(segment); ext != segment && len(ext) <= maxKeptExtension &&
			alphaPattern.MatchString(ext) && s.Text(ext) == ext {
			token += ext
		}

		segments[i] = token
	}

	return strings.Join(segments, "/")
}

// collectTime is how long the bundle waits for all modules: maxCollectTime,
// or half the time the request has left, so the bundle is still built and
// sent before the request's own deadline.
func collectTime(ctx context.Context) time.Duration {
	limit := maxCollectTime

	if deadline, ok := ctx.Deadline(); ok {
		limit = min(limit, time.Until(deadline)/requestShare)
	}

	return limit
}

// budget is a context that ends after d, or when parent ends, with a cancel
// and never a deadline. A supervised call counts a deadline it did not set as
// the module's failure, against the module's breaker. A cancel is the caller
// going away, which counts against nothing. The bundle running out of time is
// the bundle's choice and not the module's fault. context.Cause tells which
// ended it.
func budget(parent context.Context, d time.Duration, cause error) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	stopParent := context.AfterFunc(parent, func() { cancel(context.Cause(parent)) })
	timer := time.AfterFunc(d, func() { cancel(cause) })

	return ctx, func() {
		timer.Stop()
		stopParent()
		cancel(context.Canceled)
	}
}
