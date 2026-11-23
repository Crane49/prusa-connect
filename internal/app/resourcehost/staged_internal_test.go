package resourcehost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// stagingTarget is a module that reads the staged file while it serves, the
// way a real one does, and may move it to keep it.
type stagingTarget struct {
	dataDir string
	keep    string
	// delay is how long the module takes to answer.
	delay time.Duration

	mu    sync.Mutex
	seen  []routesapi.Request
	bytes [][]byte
}

func (s *stagingTarget) Call(ctx context.Context, _ string, params any) (json.RawMessage, error) {
	request, ok := params.(routesapi.Request)
	if !ok {
		return nil, errors.New("not a routes.serve request")
	}

	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var held []byte

	if request.BodyFile != "" {
		file := filepath.Join(s.dataDir, filepath.FromSlash(request.BodyFile))

		read, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}

		held = read

		if s.keep != "" {
			if err = os.Rename(file, filepath.Join(s.dataDir, s.keep)); err != nil {
				return nil, err
			}
		}
	}

	s.mu.Lock()
	s.seen = append(s.seen, request)
	s.bytes = append(s.bytes, held)
	s.mu.Unlock()

	return json.Marshal(routesapi.Response{Status: http.StatusCreated})
}

func (s *stagingTarget) requests() ([]routesapi.Request, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]routesapi.Request(nil), s.seen...), append([][]byte(nil), s.bytes...)
}

// stagingModule runs a module with one staged route, PUT /files/{name} up to
// 2 MiB, and answers the handler the mount calls. data says whether the
// operator granted the data directory.
func stagingModule(t *testing.T, data bool, keep string) (http.Handler, *stagingTarget, string) {
	t.Helper()

	return stagingModuleUnder(t, data, keep, supervisor.DefaultPolicy())
}

// stagingModuleUnder is stagingModule under policy.
func stagingModuleUnder(
	t *testing.T, data bool, keep string, policy supervisor.Policy,
) (http.Handler, *stagingTarget, string) {
	t.Helper()

	declared := declaredAs(t, ref.KindSink, "uploader", "com.github.acme.uploader/uploader", install.RuntimeService)
	declared.Spec.Service = &install.Service{
		Data:         data,
		StagedRoutes: []install.StagedRoute{{Method: http.MethodPut, Path: "/files/{name}", MaxMiB: 2}},
	}
	declared.Spec.Module = &install.ModuleEntry{}

	root := t.TempDir()
	dataDir, err := dataPath(root, declared.Ref)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dataDir, 0o750))

	host := &Host{
		cfg:     Config{ModuleData: root, Logger: slog.New(slog.DiscardHandler)},
		running: map[ref.Ref]*instance{},
	}

	target := &stagingTarget{dataDir: dataDir, keep: keep}

	runner, err := supervisor.New(supervisor.Config[any]{
		Ref:    declared.Ref,
		Policy: policy,
		Start:  func(context.Context, supervisor.Handle) (any, error) { return target, nil },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = runner.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, runner.Serving, 5*time.Second, 5*time.Millisecond)

	host.running[declared.Ref] = &instance{runner: runner, declaration: declared}

	return &moduleHandler{host: host, instance: declared.Ref}, target, dataDir
}

// put sends one body to the handler. A negative length sends it with no
// Content-Length, as a chunked upload arrives.
func put(t *testing.T, handler http.Handler, path string, body []byte, length int64) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, path, io.NopCloser(bytes.NewReader(body)))
	request.ContentLength = length

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

// stagedFiles is what is left in the staged folder.
func stagedFiles(t *testing.T, dataDir string) []os.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(dataDir, stagedDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	require.NoError(t, err)

	return entries
}

// TestAStagedBodyReachesTheModuleAsAFile is the gate ADR-20261120-xys rules 4
// to 6 owe. A body over the wire's bound reaches the module as a file in
// staged/ of its data directory, with no body on the wire, and the file is
// gone once the module answered. A body over the route's bound is refused
// with BODY_TOO_LARGE before the module is called and leaves no file, whether
// it stated its length or not.
func TestAStagedBodyReachesTheModuleAsAFile(t *testing.T) {
	t.Parallel()

	handler, target, dataDir := stagingModule(t, true, "")
	body := bytes.Repeat([]byte("G1 X1\n"), (routesapi.MaxModuleBody*3/2)/6)

	answered := put(t, handler, "/files/benchy.bgcode", body, int64(len(body)))
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	seen, held := target.requests()
	require.Len(t, seen, 1)
	require.Empty(t, seen[0].Body, "a staged body was also sent on the wire")
	require.Regexp(t, `^staged/[A-Z2-7]{26}$`, seen[0].BodyFile)
	require.Equal(t, body, held[0], "the module read another body than the one sent")
	require.Empty(t, stagedFiles(t, dataDir), "the host left the staged file behind")

	over := bytes.Repeat([]byte{'x'}, 2<<20+1)

	for name, length := range map[string]int64{"stated": int64(len(over)), "not stated": -1} {
		refused := put(t, handler, "/files/big.bgcode", over, length)
		require.Equal(t, http.StatusRequestEntityTooLarge, refused.Code, name)
		require.Contains(t, refused.Body.String(), "BODY_TOO_LARGE", name)
		require.Contains(t, refused.Body.String(), "2097152", "the refusal names another limit than the route's")
		require.Empty(t, stagedFiles(t, dataDir), "a refused body left a file (%s)", name)
	}

	// A length that understates the body is held to the bytes that arrive.
	refused := put(t, handler, "/files/big.bgcode", over, 10)
	require.NotEqual(t, http.StatusCreated, refused.Code)
	require.Empty(t, stagedFiles(t, dataDir))

	// Another route is held to the wire's bound, as before.
	plain := put(t, handler, "/other", body, int64(len(body)))
	require.Equal(t, http.StatusRequestEntityTooLarge, plain.Code, plain.Body.String())

	seen, _ = target.requests()
	require.Len(t, seen, 1, "the module was called for a refused body")
}

// A module may read a staged file for longer than the call budget of an
// ordinary request. The call takes the budget of its route, and a request on
// any other route keeps the policy's.
func TestAStagedRequestTakesTheBudgetOfItsRoute(t *testing.T) {
	t.Parallel()

	policy := supervisor.DefaultPolicy()
	policy.CallTimeout = 50 * time.Millisecond

	handler, target, _ := stagingModuleUnder(t, true, "", policy)
	target.delay = 200 * time.Millisecond

	answered := put(t, handler, "/files/a", []byte("G28\n"), 4)
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	plain := put(t, handler, "/other", []byte("G28\n"), 4)
	require.NotEqual(t, http.StatusCreated, plain.Code, "an ordinary request outlived the policy's budget")
}

// A module that keeps the body moves the file before it answers, and the
// host leaves the moved file alone (ADR-20261120-xys rule 6).
func TestAStagedFileTheModuleMovedStays(t *testing.T) {
	t.Parallel()

	handler, _, dataDir := stagingModule(t, true, "kept.bgcode")

	answered := put(t, handler, "/files/a", []byte("G28\n"), 4)
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	kept, err := os.ReadFile(filepath.Join(dataDir, "kept.bgcode"))
	require.NoError(t, err)
	require.Equal(t, "G28\n", string(kept))
	require.Empty(t, stagedFiles(t, dataDir))
}

// A module the operator granted no data directory gets the body on the wire,
// held to the wire's bound.
func TestAStagedRouteWithNoDataDirectorySendsTheBody(t *testing.T) {
	t.Parallel()

	handler, target, _ := stagingModule(t, false, "")

	answered := put(t, handler, "/files/a", []byte("G28\n"), 4)
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	seen, _ := target.requests()
	require.Len(t, seen, 1)
	require.Empty(t, seen[0].BodyFile)
	require.Equal(t, []byte("G28\n"), seen[0].Body)
}

// The host never follows a link the module put where the staged folder
// should be.
func TestAStagedFolderThatIsALinkIsNotFollowed(t *testing.T) {
	t.Parallel()

	handler, target, dataDir := stagingModule(t, true, "")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dataDir, stagedDir)); err != nil {
		t.Skipf("this platform makes no link here: %v", err)
	}

	answered := put(t, handler, "/files/a", []byte("G28\n"), 4)
	require.Equal(t, http.StatusInternalServerError, answered.Code, answered.Body.String())

	written, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, written, "the host wrote through the module's link")

	seen, _ := target.requests()
	require.Empty(t, seen)
}

// The mount's surface and the module's grants carry the limit the host
// stages a body to, and none of the three stages anything without a data
// directory (ADR-20261120-xys rules 5 and 6).
func TestTheSurfaceAndTheGrantsCarryTheHostsStagedLimit(t *testing.T) {
	t.Parallel()

	for _, data := range []bool{true, false} {
		handler, _, _ := stagingModule(t, data, "")
		module, ok := handler.(*moduleHandler)
		require.True(t, ok)

		running := module.host.running[module.instance]
		_, surfaced := module.host.stagedOf(running.declaration)
		route, staged := module.host.stagedFor(module.instance, http.MethodPut, "/files/a")

		grants, err := grantsOf(module.host.cfg.ModuleData, module.instance, serviceOf(running.declaration))
		require.NoError(t, err)

		if !data {
			require.Empty(t, surfaced)
			require.False(t, staged)
			require.Nil(t, grants)

			continue
		}

		require.Equal(t, []manifest.StagedRoute{{
			Route: manifest.Route{Method: http.MethodPut, Path: "/files/{name}"}, MaxMiB: 2,
		}}, surfaced)
		require.True(t, staged)
		require.Equal(t, int64(surfaced[0].MaxMiB)<<20, route.limit)
		require.Equal(t, []wirerpc.StagedGrant{{Method: http.MethodPut, Path: "/files/{name}", MaxMiB: 2}},
			grants.Staged)
	}
}

// A route the declaration names both public and staged is not staged, so a
// caller nobody resolved never writes to the disk. Its body goes on the wire.
func TestAPublicRouteIsNeverStaged(t *testing.T) {
	t.Parallel()

	handler, _, _ := stagingModule(t, true, "")
	module, ok := handler.(*moduleHandler)
	require.True(t, ok)

	running := module.host.running[module.instance]
	running.declaration.Spec.Service.PublicRoutes = []manifest.Route{{Method: http.MethodPut, Path: "/files/{name}"}}

	_, surfaced := module.host.stagedOf(running.declaration)
	require.Empty(t, surfaced)

	_, staged := module.host.stagedFor(module.instance, http.MethodPut, "/files/a")
	require.False(t, staged, "a public route was staged")
}

// A public route with a placeholder reaches a staged route written with a
// segment of its own, so the host stages neither that route nor tells the
// module it does. Compared as text, the two would differ and an anonymous
// caller could fill the disk.
func TestAPublicPlaceholderKeepsItsRequestsOffTheDisk(t *testing.T) {
	t.Parallel()

	handler, _, _ := stagingModule(t, true, "")
	module, ok := handler.(*moduleHandler)
	require.True(t, ok)

	running := module.host.running[module.instance]
	running.declaration.Spec.Service.PublicRoutes = []manifest.Route{{Method: http.MethodPut, Path: "/{dir}/a"}}

	_, surfaced := module.host.stagedOf(running.declaration)
	require.Empty(t, surfaced)

	_, staged := module.host.stagedFor(module.instance, http.MethodPut, "/files/a")
	require.False(t, staged, "a request a public route reaches was staged")

	require.Empty(t, stagedGrantsOf(stagedApart(running.declaration.Spec.Service.PublicRoutes,
		running.declaration.Spec.Service.StagedRoutes)), "the module was told of a route the host does not stage")
}

// An instance holds at most stagedSlots staged bodies at once. One more is
// refused with 503 and Retry-After before a byte is read, and a place a body
// frees is taken again.
func TestAModuleHoldsAFewStagedBodiesAtOnce(t *testing.T) {
	t.Parallel()

	handler, _, _ := stagingModule(t, true, "")
	module, ok := handler.(*moduleHandler)
	require.True(t, ok)

	var held []func()

	for range stagedSlots {
		release, free := module.host.staging.take(module.instance)
		require.True(t, free)

		held = append(held, release)
	}

	refused := put(t, handler, "/files/a", []byte("body"), 4)
	require.Equal(t, http.StatusServiceUnavailable, refused.Code, refused.Body.String())
	require.Equal(t, "10", refused.Header().Get("Retry-After"))
	require.Contains(t, refused.Body.String(), httpfault.CodeModuleUploadsBusy().String())

	held[0]()
	held[0]()

	answered := put(t, handler, "/files/a", []byte("body"), 4)
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	// One place is free: the second release did nothing, and the answered
	// body gave its place back.
	_, free := module.host.staging.take(module.instance)
	require.True(t, free, "the answered body kept its place")

	_, free = module.host.staging.take(module.instance)
	require.False(t, free, "a released place was counted twice")
}

// A staged body the disk has no room for is refused with 507 before it is
// read, and one that states no length is held to the route's limit.
func TestAStagedBodyTheDiskHasNoRoomForIsRefused(t *testing.T) {
	t.Parallel()

	handler, target, dataDir := stagingModule(t, true, "")
	module, ok := handler.(*moduleHandler)
	require.True(t, ok)

	free := int64(stagedMargin + 100)
	module.host.cfg.FreeSpace = func(string) (int64, error) { return free, nil }

	refused := put(t, handler, "/files/a", make([]byte, 200), 200)
	require.Equal(t, http.StatusInsufficientStorage, refused.Code, refused.Body.String())
	require.Contains(t, refused.Body.String(), httpfault.CodeModuleStorageFull().String())

	unstated := put(t, handler, "/files/a", []byte("body"), -1)
	require.Equal(t, http.StatusInsufficientStorage, unstated.Code, "a body of no stated length skipped the check")

	answered := put(t, handler, "/files/a", []byte("body"), 4)
	require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())

	entries, err := os.ReadDir(filepath.Join(dataDir, stagedDir))
	require.NoError(t, err)
	require.Empty(t, entries, "a refused body left a file")
	seen, _ := target.requests()
	require.Len(t, seen, 1, "a refused body reached the module")
}

// The disk's free space does not show the bodies still on their way in, so
// the room each one was let in for is held until it is gone. Two bodies that
// fit only one at a time are not both let in.
func TestTheRoomOfABodyInFlightIsHeldForIt(t *testing.T) {
	t.Parallel()

	handler, _, dataDir := stagingModule(t, true, "")
	module, ok := handler.(*moduleHandler)
	require.True(t, ok)

	module.host.cfg.FreeSpace = func(string) (int64, error) { return stagedMargin + 150, nil }
	route := stagedRoute{dataDir: dataDir, limit: 1 << 20}

	first := httptest.NewRequest(http.MethodPut, "/files/a", nil)
	first.ContentLength = 100
	release, err := module.host.room(first, route)
	require.NoError(t, err)

	second := httptest.NewRequest(http.MethodPut, "/files/b", nil)
	second.ContentLength = 100
	_, err = module.host.room(second, route)
	require.ErrorIs(t, err, errStagingFull, "a second body was let into room the first one holds")

	release()
	release()

	again, err := module.host.room(second, route)
	require.NoError(t, err, "the room a finished body held was not given back")
	again()
}

// A chunked body over a staged route's limit is cut by the server's body
// limit, which wraps every request in http.MaxBytesReader at the route's
// limit before the module host reads it. The host answers that as the body
// over the limit, 413 naming the limit, as it answers its own count.
func TestAChunkedBodyOverTheServerLimitIsTooLarge(t *testing.T) {
	t.Parallel()

	handler, target, dataDir := stagingModule(t, true, "")

	// What the server's body limit middleware does to a module request.
	const limit = 2 << 20

	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		handler.ServeHTTP(w, r)
	})

	answered := put(t, limited, "/files/big.bgcode", bytes.Repeat([]byte("G"), limit+1), -1)

	require.Equal(t, http.StatusRequestEntityTooLarge, answered.Code, answered.Body.String())
	require.Contains(t, answered.Body.String(), "BODY_TOO_LARGE")
	require.Contains(t, answered.Body.String(), strconv.Itoa(limit))

	requests, _ := target.requests()
	require.Empty(t, requests, "the module ran on a refused body")
	require.Empty(t, stagedFiles(t, dataDir), "a refused body was left on the disk")
}

// A body a stopped server left in staged/ is removed when this process first
// starts the instance, and a later start leaves the folder alone, because a
// body may be on its way in while the module restarts.
func TestTheFirstStartRemovesStagedBodiesARunLeft(t *testing.T) {
	t.Parallel()

	declared := declaredAs(t, ref.KindSink, "uploader", "com.github.acme.uploader/uploader", install.RuntimeService)
	declared.Spec.Service = &install.Service{Data: true}

	root := t.TempDir()
	dataDir, err := dataPath(root, declared.Ref)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, stagedDir), 0o750))

	left := filepath.Join(dataDir, stagedDir, "LEFT")
	require.NoError(t, os.WriteFile(left, []byte("G28\n"), 0o600))

	kept := filepath.Join(dataDir, "kept.bgcode")
	require.NoError(t, os.WriteFile(kept, []byte("G28\n"), 0o600))

	host := &Host{cfg: Config{ModuleData: root, Logger: slog.New(slog.DiscardHandler)}}

	host.sweepStaged(t.Context(), declared)
	require.NoFileExists(t, left, "a body the last run left is still there")
	require.FileExists(t, kept, "the sweep reached outside staged/")

	inFlight := filepath.Join(dataDir, stagedDir, "IN_FLIGHT")
	require.NoError(t, os.MkdirAll(filepath.Dir(inFlight), 0o750))
	require.NoError(t, os.WriteFile(inFlight, []byte("G28\n"), 0o600))

	host.sweepStaged(t.Context(), declared)
	require.FileExists(t, inFlight, "a restart removed a body that may be on its way in")
}
