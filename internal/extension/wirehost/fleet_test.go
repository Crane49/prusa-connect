package wirehost_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The cases here run examples/service-plugin, the Go module, as a real process
// over the wire. The host calls routes.serve with a principal, the module calls
// back into the fleet or the stream for that person, and each side checks what
// it saw. The host side is a fake, so the cases cover the wire and the module:
// the principal a module received arrives on every inbound method, a refusal
// the host raises reaches the module as the sentinel it branches on, and the
// module's own work names nobody.

// The people the cases run as. The fake fleet refuses one of them by name, the
// way the real host refuses a principal it never gave the module.
const (
	served  = "user:alice"
	refused = "user:mallory"
)

// built is one program of this repository, compiled once and shared by every
// case in this package, because building the tree is most of what a case costs.
// TestMain removes the directories if it can. On Windows a child that was not
// killed keeps its binary open, and a directory left in the temporary tree is
// not worth failing a passing run over.
type built struct {
	// pkg is the package to build, relative to the repository root.
	pkg string
	// name is what the artefact is called, before the platform suffix.
	name string

	once sync.Once
	dir  string
	path string
	err  error
}

// The programs the cases launch: the example module, and the child that streams
// bulk bytes under credit.
var (
	builtModule = &built{pkg: "./examples/service-plugin", name: "service-plugin"}
	builtChild  = &built{pkg: "./internal/extension/wirehost/testdata/blobchild", name: "blobchild"}
	builtFork   = &built{pkg: "./internal/extension/wirehost/testdata/forkchild", name: "forkchild"}
)

func TestMain(m *testing.M) {
	code := m.Run()

	for _, program := range []*built{builtModule, builtChild, builtFork} {
		if program.dir != "" {
			_ = os.RemoveAll(program.dir)
		}
	}

	os.Exit(code)
}

// file builds the program on first use and returns the artefact.
func (b *built) file(t *testing.T) string {
	t.Helper()

	b.once.Do(func() {
		toolchain, err := exec.LookPath("go")
		if err != nil {
			b.err = err

			return
		}

		root, err := filepath.Abs(filepath.Join("..", "..", ".."))
		if err != nil {
			b.err = err

			return
		}

		// Shared by every case, so it cannot be one case's t.TempDir.
		// TestMain removes it.
		dir, err := os.MkdirTemp("", "prusa-connect-wirehost-") //nolint:usetesting // shared across cases
		if err != nil {
			b.err = err

			return
		}

		b.dir = dir

		file := b.name + "_" + runtime.GOOS + "_" + runtime.GOARCH
		if runtime.GOOS == "windows" {
			file += ".exe"
		}

		out := filepath.Join(dir, file)

		build := exec.Command(toolchain, "build", "-o", out, b.pkg)
		build.Dir = root

		if said, buildErr := build.CombinedOutput(); buildErr != nil {
			b.err = fmt.Errorf("build %s: %w: %s", b.pkg, buildErr, said)

			return
		}

		b.path = out
	})

	if b.err != nil {
		t.Fatalf("the program could not be built: %v", b.err)
	}

	return b.path
}

// fleetFake is the host's side of every inbound method, recording who each
// call named and refusing one person by name.
type fleetFake struct {
	mu  sync.Mutex
	saw map[wirerpc.Method][]string
	// written is the last file a write published.
	written []byte
}

func (f *fleetFake) note(method wirerpc.Method, principal string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.saw == nil {
		f.saw = map[wirerpc.Method][]string{}
	}

	f.saw[method] = append(f.saw[method], principal)

	if principal == refused {
		return fmt.Errorf("%w: %s is not a principal this host handed the module",
			deviceapi.ErrDenied, principal)
	}

	return nil
}

func (f *fleetFake) principals(method wirerpc.Method) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.saw[method]...)
}

func (f *fleetFake) List(_ context.Context, p deviceapi.ListParams) (deviceapi.Page, error) {
	if err := f.note(deviceapi.MethodList, p.Principal); err != nil {
		return deviceapi.Page{}, err
	}

	return deviceapi.Page{Items: []deviceapi.Device{{Kind: "printer", ID: "p1"}}, Total: 1, Limit: 50}, nil
}

func (f *fleetFake) Get(_ context.Context, p deviceapi.GetParams) (deviceapi.Device, error) {
	if err := f.note(deviceapi.MethodGet, p.Principal); err != nil {
		return deviceapi.Device{}, err
	}

	return deviceapi.Device{Kind: "printer", ID: p.ID}, nil
}

func (f *fleetFake) Command(_ context.Context, p deviceapi.CommandParams) (deviceapi.Result, error) {
	if err := f.note(deviceapi.MethodCommand, p.Principal); err != nil {
		return deviceapi.Result{}, err
	}

	return deviceapi.Result{OK: true}, nil
}

func (f *fleetFake) Approve(_ context.Context, p deviceapi.ApproveParams) (deviceapi.Registration, error) {
	if err := f.note(deviceapi.MethodApprove, p.Principal); err != nil {
		return deviceapi.Registration{}, err
	}

	return deviceapi.Registration{Code: p.Code, Approved: true}, nil
}

func (f *fleetFake) Jobs(_ context.Context, p deviceapi.JobsParams) (deviceapi.Jobs, error) {
	if err := f.note(deviceapi.MethodJobs, p.Principal); err != nil {
		return deviceapi.Jobs{}, err
	}

	return deviceapi.Jobs{Last: &deviceapi.Job{ID: p.ID + ":1", State: "FINISHED"}}, nil
}

func (f *fleetFake) Files(_ context.Context, p deviceapi.FilesParams) (deviceapi.Listing, error) {
	if err := f.note(deviceapi.MethodFiles, p.Principal); err != nil {
		return deviceapi.Listing{}, err
	}

	return deviceapi.Listing{
		Entry: &deviceapi.File{Path: p.Path, Folder: true},
		Items: []deviceapi.File{{Path: p.Path + "/part.bgcode", Size: 1}},
		Total: 1,
		Limit: deviceapi.DefaultPageLimit,
	}, nil
}

func (f *fleetFake) JobHistory(
	_ context.Context, p deviceapi.JobHistoryParams,
) (deviceapi.JobPage, error) {
	if err := f.note(deviceapi.MethodJobHistory, p.Principal); err != nil {
		return deviceapi.JobPage{}, err
	}

	return deviceapi.JobPage{Limit: deviceapi.DefaultPageLimit}, nil
}

func (f *fleetFake) Print(_ context.Context, p deviceapi.PrintParams) (deviceapi.Started, error) {
	if err := f.note(deviceapi.MethodPrint, p.Principal); err != nil {
		return deviceapi.Started{}, err
	}

	return deviceapi.Started{TransferID: p.ID + ":t1"}, nil
}

func (f *fleetFake) Stat(
	_ context.Context, p storageapi.StatParams,
) (storageapi.FileInfo, error) {
	if err := f.note(storageapi.MethodStat, p.Principal); err != nil {
		return storageapi.FileInfo{}, fmt.Errorf("%w: %s is not a principal this host handed the module",
			storageapi.ErrDenied, p.Principal)
	}

	return storageapi.FileInfo{Name: "part.gcode", Path: p.Path, Size: int64(len(stagedFile))}, nil
}

// storageFake is the fake's storage door. storageapi.Files has a List of its
// own, which the fleet's List would otherwise stand in for.
type storageFake struct{ *fleetFake }

func (storageFake) List(context.Context, storageapi.ListParams) (storageapi.Listing, error) {
	return storageapi.Listing{}, nil
}

func (storageFake) Move(context.Context, storageapi.MoveParams) (storageapi.Written, error) {
	return storageapi.Written{}, storageapi.ErrDenied
}

func (f *fleetFake) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	if err := f.note(storageapi.MethodRead, p.Principal); err != nil {
		return storageapi.Chunk{}, fmt.Errorf("%w: %s is not a principal this host handed the module",
			storageapi.ErrDenied, p.Principal)
	}

	length := p.Length
	if length == 0 || length > len(stagedFile) {
		length = len(stagedFile)
	}

	return storageapi.Chunk{
		Data: []byte(stagedFile[:length]), Size: int64(len(stagedFile)),
		EOF: length == len(stagedFile),
	}, nil
}

func (f *fleetFake) Fetch(
	_ context.Context, p storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	if err := f.note(storageapi.MethodFetch, p.Principal); err != nil {
		return storageapi.Fetched{}, fmt.Errorf("%w: %s is not a principal this host handed the module",
			storageapi.ErrDenied, p.Principal)
	}

	n, err := io.WriteString(w, stagedFile)

	return storageapi.Fetched{Size: int64(len(stagedFile)), Length: n}, err
}

func (f *fleetFake) Write(
	_ context.Context, p storageapi.WriteParams, body io.Reader,
) (storageapi.Written, error) {
	if err := f.note(storageapi.MethodWrite, p.Principal); err != nil {
		return storageapi.Written{}, fmt.Errorf("%w: %s is not a principal this host handed the module",
			storageapi.ErrDenied, p.Principal)
	}

	held, err := io.ReadAll(body)
	if err != nil {
		return storageapi.Written{}, err
	}

	f.mu.Lock()
	f.written = held
	f.mu.Unlock()

	return storageapi.Written{Path: p.Path, Size: int64(len(held))}, nil
}

func (f *fleetFake) Check(
	_ context.Context, p accessapi.CheckParams,
) (accessapi.Decision, error) {
	if err := f.note(accessapi.MethodCheck, p.Subject); err != nil {
		return accessapi.Decision{}, fmt.Errorf("%w: %s is not a subject this module has served",
			accessapi.ErrUnknownSubject, p.Subject)
	}

	return accessapi.Decision{Allowed: p.Action == "operate"}, nil
}

func (f *fleetFake) Subscribe(
	_ context.Context, p busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	if err := f.note(busapi.MethodSubscribe, p.Principal); err != nil {
		return fmt.Errorf("%w: %s is not a principal this host handed the module", busapi.ErrDenied, p.Principal)
	}

	return write(busapi.Window{Cursor: "origin:1", Events: []json.RawMessage{[]byte(`{"type":"x"}`)}})
}

// launchModule starts the example module with the fake behind every inbound
// method.
func launchModule(t *testing.T, fake *fleetFake) *wirehost.Child {
	t.Helper()

	path := builtModule.file(t)

	methods := wirerpc.Methods{}
	maps.Copy(methods, deviceapi.Handlers(fake))
	maps.Copy(methods, busapi.SubscribeHandler(fake))
	maps.Copy(methods, storageapi.Handlers(storageFake{fake}))
	maps.Copy(methods, accessapi.Handlers(fake))

	child, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "sink/planner",
		Path:     path,
		Digest:   digestOf(t, path),
		Methods:  methods,
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("launch the module: %v", err)
	}

	t.Cleanup(func() { _ = child.Close() })

	return child
}

// serve makes one routes.serve call into the module for one person, and
// returns the status the module answered.
//
//nolint:unparam // the principal is the parameter every case here varies next.
func serve(t *testing.T, child *wirehost.Child, principal, method, path string) (int, string) {
	t.Helper()

	return serveBody(t, child, principal, method, path, nil)
}

// serveBody is serve with a request body.
func serveBody(t *testing.T, child *wirehost.Child, principal, method, path string, body []byte) (int, string) {
	t.Helper()

	// A case writes its query into the path. The host sends the two apart,
	// and the SDK does not split them again.
	path, query, _ := strings.Cut(path, "?")

	raw, err := child.Conn().Call(t.Context(), routesapi.MethodServe, routesapi.Request{
		Method:    method,
		Path:      path,
		Query:     query,
		Principal: principal,
		Body:      body,
	})
	if err != nil {
		t.Fatalf("routes.serve %s %s: %v", method, path, err)
	}

	var response routesapi.Response
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("read the module's answer: %v", err)
	}

	status := response.Status
	if status == 0 {
		status = http.StatusOK
	}

	return status, string(response.Body)
}

// inbound is one route of the module and the method it calls back on.
type inbound struct {
	method wirerpc.Method
	verb   string
	path   string
}

// inbounds are the inbound methods, one route each.
func inbounds() []inbound {
	return []inbound{
		{deviceapi.MethodList, http.MethodGet, "/devices"},
		{deviceapi.MethodGet, http.MethodGet, "/devices/p1"},
		{deviceapi.MethodCommand, http.MethodPost, "/devices/p1/command?verb=core.command.stop_print"},
		{deviceapi.MethodApprove, http.MethodPost, "/registrations/K7PQ2XR4MN/approve"},
		{deviceapi.MethodJobs, http.MethodGet, "/devices/p1/jobs"},
		{deviceapi.MethodFiles, http.MethodGet, "/devices/p1/files?path=usb"},
		{busapi.MethodSubscribe, http.MethodGet, "/events"},
	}
}

// The principal the host handed the module on routes.serve is the one the
// module names when it calls back, on every inbound method, and the host's
// answer reaches the person as a success.
func TestAModuleServingAPersonNamesThemOnEveryInboundMethod(t *testing.T) {
	t.Parallel()

	fake := &fleetFake{}
	child := launchModule(t, fake)

	for _, route := range inbounds() {
		t.Run(string(route.method), func(t *testing.T) {
			t.Parallel()

			status, body := serve(t, child, served, route.verb, route.path)
			if status != http.StatusOK {
				t.Fatalf("the module answered %d for %s: %s", status, served, body)
			}

			if saw := fake.principals(route.method); len(saw) != 1 || saw[0] != served {
				t.Fatalf("%s reached the host as %q, want %q", route.method, saw, served)
			}
		})
	}
}

// A refusal the host raises for a person crosses the wire as the sentinel the
// module branches on, on every inbound method, and the module answers that
// person with a refusal and not with data.
func TestAPrincipalTheHostRefusesIsRefusedToThePersonOnEveryInboundMethod(t *testing.T) {
	t.Parallel()

	fake := &fleetFake{}
	child := launchModule(t, fake)

	for _, route := range inbounds() {
		t.Run(string(route.method), func(t *testing.T) {
			t.Parallel()

			status, body := serve(t, child, served, route.verb, route.path+asOf(route.path, refused))
			if status != http.StatusForbidden {
				t.Fatalf("the module answered %d for a refused principal, want 403: %s", status, body)
			}

			if saw := fake.principals(route.method); len(saw) != 1 || saw[0] != refused {
				t.Fatalf("%s reached the host as %q, want %q", route.method, saw, refused)
			}
		})
	}
}

// The module's own work names nobody on every inbound method, and the host
// answers it as the instance.
func TestAModulesOwnWorkNamesNobodyOnEveryInboundMethod(t *testing.T) {
	t.Parallel()

	fake := &fleetFake{}
	child := launchModule(t, fake)

	for _, route := range inbounds() {
		t.Run(string(route.method), func(t *testing.T) {
			t.Parallel()

			status, body := serve(t, child, served, route.verb, route.path+asOf(route.path, "self"))
			if status != http.StatusOK {
				t.Fatalf("the module answered %d for its own work: %s", status, body)
			}

			if saw := fake.principals(route.method); len(saw) != 1 || saw[0] != "" {
				t.Fatalf("%s reached the host as %q, want nobody", route.method, saw)
			}
		})
	}
}

// asOf appends the module's as parameter to a path that may already carry a
// query.
func asOf(path, as string) string {
	for _, r := range path {
		if r == '?' {
			return "&as=" + as
		}
	}

	return "?as=" + as
}

// stagedFile is what the fake storage holds: the head of a sliced file, where
// the settings a module reads are written.
const stagedFile = "; filament_type = PETG\nG28\n"

// A module writes a file into a storage over a real process, for the person
// the host is serving, and the bytes reach the host whole. The file is longer
// than one window of credit, so the write waits for the host to read.
func TestAModuleWritesAFileOverTheWire(t *testing.T) {
	t.Parallel()

	fake := &fleetFake{}
	child := launchModule(t, fake)

	body := []byte(strings.Repeat("G1 X1 Y1 E0.1\n", 40_000))

	status, answered := serveBody(t, child, served, http.MethodPut, "/files/bench/out/part.gcode", body)
	if status != http.StatusOK {
		t.Fatalf("write a file: %d %s", status, answered)
	}

	fake.mu.Lock()
	held := fake.written
	fake.mu.Unlock()

	if !bytes.Equal(held, body) {
		t.Fatalf("the host kept %d bytes, want the %d the module sent", len(held), len(body))
	}

	if principals := fake.principals(storageapi.MethodWrite); len(principals) != 1 || principals[0] != served {
		t.Fatalf("storage.write named %v, want %s", principals, served)
	}
}

// A module reads a file a storage holds and asks whether somebody may operate
// a printer, over a real process, for the person the host is serving. Both
// calls carry that person out to the host, which is the whole of what the two
// contracts promise a module author.
func TestAModuleReadsAFileAndAsksAboutAPersonOverTheWire(t *testing.T) {
	t.Parallel()

	fake := &fleetFake{}
	child := launchModule(t, fake)

	status, body := serve(t, child, served, http.MethodGet, "/bytes/bench/part.gcode?length=8")
	if status != http.StatusOK {
		t.Fatalf("read a file: %d %s", status, body)
	}

	if !strings.Contains(body, stagedFile[:8]) || !strings.Contains(body, `"size":27`) {
		t.Fatalf("the module answered %s, want the head of the staged file and its length", body)
	}

	if principals := fake.principals(storageapi.MethodRead); len(principals) != 1 ||
		principals[0] != served {
		t.Fatalf("storage.read named %v, want %s", principals, served)
	}

	status, body = serve(t, child, served, http.MethodGet, "/files/bench/part.gcode")
	if status != http.StatusOK {
		t.Fatalf("describe a file: %d %s", status, body)
	}

	status, body = serve(t, child, served, http.MethodGet, "/may/"+served+"/operate/device/p1")
	if status != http.StatusOK || !strings.Contains(body, "true") {
		t.Fatalf("ask about a person: %d %s", status, body)
	}

	// The refusal. A subject the host has not served reaches the module as the
	// sentinel it branches on, and the module answers 403.
	status, body = serve(t, child, served, http.MethodGet, "/may/"+refused+"/operate/device/p1")
	if status != http.StatusForbidden {
		t.Fatalf("a subject the host refuses answered %d %s, want 403", status, body)
	}
}
