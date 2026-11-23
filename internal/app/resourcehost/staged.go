package resourcehost

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// A staged route takes a body larger than a wire call carries. The host writes
// it into the module's data directory and the module reads the file
// (ADR-20261120-xys rules 4 to 6).

// stagedDir is the folder of the data directory the bodies are written to.
const stagedDir = "staged"

// mib is one mebibyte in bytes.
const mib = 1 << 20

// The modes of the staged folder and of a file in it: the module reads and
// moves the file, and nobody outside the server's group reads either.
const (
	stagedDirMode  = 0o750
	stagedFileMode = 0o640
)

// stagedRoute is where one request's body goes, and how much of it the route
// takes.
type stagedRoute struct {
	dataDir string
	limit   int64
}

// stagedFloor is the slowest upload a staged route waits for, in bytes a
// second, and stagedSlack the time it adds for the headers and the first
// bytes. A route of 1024 MiB waits about 17 minutes. The module then has as
// long again to answer, so it can read the file at that speed, and
// stagedAnswer is how long the host has to write the answer after that.
const (
	stagedFloor  = mib
	stagedSlack  = 30 * time.Second
	stagedAnswer = 2 * time.Minute
)

// readBudget is how long the host reads a body on the route before it gives
// up.
func (s stagedRoute) readBudget() time.Duration {
	return stagedSlack + time.Duration(s.limit/stagedFloor)*time.Second
}

// stagedFor is the staged route a request is on, and false for a request on
// any other route or for an instance the operator granted no data directory.
// The path is relative to the mount, as the module's routes are written.
func (h *Host) stagedFor(instance ref.Ref, method, routePath string) (stagedRoute, bool) {
	h.mu.Lock()
	running, ok := h.running[instance]
	h.mu.Unlock()

	if !ok {
		return stagedRoute{}, false
	}

	dir, routes := h.stagedOf(running.declaration)

	for _, route := range routes {
		if route.Matches(method, routePath) {
			return stagedRoute{dataDir: dir, limit: int64(route.MaxMiB) * mib}, true
		}
	}

	return stagedRoute{}, false
}

// stagedOf is the data directory a declaration's staged bodies are written to
// and each staged route the operator approved, with its limit. It is no route
// for an instance granted no data directory, whose bodies go on the wire. The
// mount's surface and the host read this one answer, so the server and the
// host hold a body to one limit (ADR-20261120-xys rule 6).
func (h *Host) stagedOf(declared resource.Declaration) (string, []manifest.StagedRoute) {
	spec := serviceOf(declared)
	if spec == nil || len(spec.service.StagedRoutes) == 0 {
		return "", nil
	}

	dir := h.dataGranted(declared)
	if dir == "" {
		return "", nil
	}

	// A route the declaration also names public is not staged, so a hand
	// edit cannot give a caller nobody resolved the disk (ADR-20261120-xys
	// rule 4). Its body goes on the wire, held to the document bound.
	return dir, stagedApart(spec.service.PublicRoutes, spec.service.StagedRoutes)
}

// stagedApart is each staged route that no public route could also reach. A
// placeholder matches any segment, so a public /up/{id} reaches a staged
// /up/x, and such a staged route is dropped.
func stagedApart(public []manifest.Route, staged []install.StagedRoute) []manifest.StagedRoute {
	routes := make([]manifest.StagedRoute, 0, len(staged))
	for _, route := range staged {
		if slices.ContainsFunc(public, route.Route().Overlaps) {
			continue
		}

		routes = append(routes, manifest.StagedRoute{Route: route.Route(), MaxMiB: route.MaxMiB})
	}

	return routes
}

// sweepStaged empties staged/ of the instance's data directory the first time
// this process starts the instance. Every body this process stages is removed
// once the module answered, so a body found there was left by a server that
// was stopped mid-upload. A later start in the same process leaves the folder
// alone, because a body may be on its way in while the module restarts.
func (h *Host) sweepStaged(ctx context.Context, declared resource.Declaration) {
	dir := h.dataGranted(declared)
	if dir == "" {
		return
	}

	h.mu.Lock()
	_, swept := h.swept[declared.Ref]
	if h.swept == nil {
		h.swept = map[ref.Ref]struct{}{}
	}
	h.swept[declared.Ref] = struct{}{}
	h.mu.Unlock()

	if swept {
		return
	}

	// Through os.Root, as stage writes it: the module owns the directory and
	// may have put a link where the folder should be.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return // no data directory yet, so nothing was staged in it
	}
	defer func() { _ = root.Close() }()

	if err = root.RemoveAll(stagedDir); err != nil {
		h.cfg.Logger.WarnContext(ctx, "removing the staged bodies a previous run left",
			"resource", declared.Ref.String(), "error", err.Error())
	}
}

// The bounds of staging. A staged route takes up to 4096 MiB, so a few
// callers at once could fill the disk past what one limit says. Each instance
// holds at most stagedSlots bodies at once, and a body is refused before it
// is read when the disk has no room for it and stagedMargin beside it.
const (
	stagedSlots      = 4
	stagedMargin     = 64 * mib
	stagedRetryAfter = 10 * time.Second
)

// errStagingBusy reports a staged body refused because the instance holds as
// many as it may. The caller retries after stagedRetryAfter.
var errStagingBusy = errors.New("resourcehost: the module is receiving as many bodies as it may at once")

// errStagingFull reports a staged body refused because the disk of the data
// directory has no room for it.
var errStagingFull = errors.New("resourcehost: the data directory has no room for the body")

// stagingSlots counts the staged bodies on disk by instance, and the bytes
// the bodies on their way in may still take.
type stagingSlots struct {
	mu   sync.Mutex
	held map[ref.Ref]int
	// reserved is the bytes every body in flight was let in for. The disk's
	// free space does not show them yet, so the next body is checked against
	// what is left after them.
	reserved int64
}

// take holds one place of the instance, and reports false when every place
// is held. The caller runs the release it returns once the body is gone.
func (s *stagingSlots) take(instance ref.Ref) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.held[instance] >= stagedSlots {
		return nil, false
	}

	if s.held == nil {
		s.held = map[ref.Ref]int{}
	}

	s.held[instance]++

	var once sync.Once

	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()

			if s.held[instance]--; s.held[instance] <= 0 {
				delete(s.held, instance)
			}
		})
	}, true
}

// room refuses a body the data directory's disk has no room for, counting
// the bytes the bodies already on their way in were let in for. A body that
// states no length is held to the route's limit. A disk the host cannot ask
// is let through, and the write reports a full disk where it happens. The
// caller runs the release it returns once the body is gone.
func (h *Host) room(r *http.Request, route stagedRoute) (func(), error) {
	if h.cfg.FreeSpace == nil {
		return func() {}, nil
	}

	free, err := h.cfg.FreeSpace(route.dataDir)
	if err != nil {
		// The write below reports a full disk.
		return func() {}, nil
	}

	need := r.ContentLength
	if need < 0 {
		need = route.limit
	}

	s := &h.staging

	s.mu.Lock()
	defer s.mu.Unlock()

	if free-s.reserved < need+stagedMargin {
		return nil, fmt.Errorf("%w: %d bytes free, %d promised to bodies in flight, %d needed",
			errStagingFull, free, s.reserved, need+stagedMargin)
	}

	s.reserved += need

	var once sync.Once

	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()

			s.reserved -= need
		})
	}, nil
}

// errStageWrite reports a body the host read and could not write to disk,
// such as on a full disk. It is the host's failure and not the caller's.
var errStageWrite = errors.New("resourcehost: the request body could not be written to the data directory")

// stagedBody is one body written to disk, and how to take it away.
type stagedBody struct {
	// file is the path relative to the data directory, with forward slashes.
	file string
	root *os.Root
	// release frees the instance's place once the file is gone.
	release func()
	// budget is how long the module has to answer, which grows with the
	// route's limit, because a module may read the whole file before it does.
	budget time.Duration
}

// stage writes the request body into the staged folder of the data
// directory under a random name, and refuses a body over the route's limit.
// Content-Length is checked before anything is read, and the bytes are
// counted as they arrive, so a body that states no length or the wrong one
// is held to the same limit. A refused or failed body leaves no file.
//
// The folder is opened through os.Root, because the module owns the data
// directory and may have put a link where the folder should be. The host
// never follows one out of the directory.
func stage(r *http.Request, route stagedRoute) (stagedBody, error) {
	if r.ContentLength > route.limit {
		return stagedBody{}, fmt.Errorf("%w: the body states %d bytes", errBodyTooLarge, r.ContentLength)
	}

	root, err := os.OpenRoot(route.dataDir)
	if err != nil {
		return stagedBody{}, fmt.Errorf("%w: open the data directory: %w", errStageWrite, err)
	}

	if err = root.Mkdir(stagedDir, stagedDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		_ = root.Close() // the open error is the one reported

		return stagedBody{}, fmt.Errorf("%w: make %s: %w", errStageWrite, stagedDir, err)
	}

	held := stagedBody{file: path.Join(stagedDir, rand.Text()), root: root}

	if err = held.write(r.Body, route.limit); err != nil {
		held.remove(nil)

		return stagedBody{}, err
	}

	return held, nil
}

// write copies the body into the file, up to limit bytes.
func (s stagedBody) write(body io.Reader, limit int64) error {
	file, err := s.root.OpenFile(s.file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, stagedFileMode)
	if err != nil {
		return fmt.Errorf("%w: create the file: %w", errStageWrite, err)
	}

	var written int64

	if body != nil {
		source := &readMemo{from: io.LimitReader(body, limit+1)}

		written, err = io.Copy(file, source)
		if err != nil {
			_ = file.Close() // the copy error is the one reported

			if source.err != nil {
				return fmt.Errorf("read the request body: %w", source.err)
			}

			return fmt.Errorf("%w: %w", errStageWrite, err)
		}
	}

	// A full disk on FAT and USB is reported here and nowhere else.
	if err = file.Close(); err != nil {
		return fmt.Errorf("%w: close the file: %w", errStageWrite, err)
	}

	if written > limit {
		return fmt.Errorf("%w: the body is over %d bytes", errBodyTooLarge, limit)
	}

	return nil
}

// remove deletes the file when it is still there, which it is not when the
// module moved it to keep it, closes the folder and frees the place.
func (s stagedBody) remove(log *slog.Logger) {
	if s.release != nil {
		defer s.release()
	}

	if err := s.root.Remove(s.file); err != nil && !errors.Is(err, fs.ErrNotExist) && log != nil {
		log.Warn("removing a staged request body", "file", s.file, "error", err.Error())
	}

	if err := s.root.Close(); err != nil && log != nil {
		log.Warn("closing the data directory", "error", err.Error())
	}
}

// readMemo remembers the error the reader returned, so a copy that failed can
// tell the caller's body from the host's disk.
type readMemo struct {
	from io.Reader
	err  error
}

func (m *readMemo) Read(p []byte) (int, error) {
	n, err := m.from.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		m.err = err
	}

	return n, err //nolint:wrapcheck // io.Copy reads io.EOF by identity.
}
