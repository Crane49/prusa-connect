package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	pfiles "github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// fakeFiles is the file distribution extension as the bridge sees it.
type fakeFiles struct {
	mu        sync.Mutex
	reachable bool
	storage   string
	source    string
	released  int
	// failure is the error Obtain returns.
	failure error
	// block makes Obtain wait until its context ends.
	block bool
	// asked counts the Obtain calls.
	asked int
	// paths are the printer paths, usb/<name> when empty.
	paths []string
}

func (f *fakeFiles) Reachable(context.Context, sdkfiles.Identity) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reachable
}

func (f *fakeFiles) Obtain(ctx context.Context, _ sdkfiles.URL) (string, string, func(), error) {
	f.mu.Lock()
	f.asked++
	f.mu.Unlock()

	if f.block {
		<-ctx.Done()

		return "", "", nil, ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failure != nil {
		return "", "", nil, f.failure
	}

	return f.storage, f.source, func() {
		f.mu.Lock()
		f.released++
		f.mu.Unlock()
	}, nil
}

func (f *fakeFiles) PrinterPaths(_ context.Context, u sdkfiles.URL, _, _ string) []string {
	if f.paths != nil {
		return f.paths
	}

	return []string{"usb/" + u.Name}
}

func (f *fakeFiles) obtains() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.asked
}

func (f *fakeFiles) releases() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.released
}

// printingBridge builds a bridge with one printer in state and files as its
// file distribution extension. deadline is both files.start_deadline and the
// longest hold on a copy; zero keeps the defaults.
func printingBridge(
	t *testing.T, files bridge.Files, state string, deadline time.Duration,
) (*fakeFleet, *fakeNode, *bridge.Bridge, *logLines) {
	t.Helper()

	reading := telemetry()
	reading["/state"] = json.RawMessage(strconv.Quote(state))

	fleet := newFleet(printer(printerOne, reading))
	node := newNode()
	logged := &logLines{}

	made, err := bridge.New(bridge.Config{
		Fleet: fleet, Node: node, Mapping: mapping(t),
		Principal: func() string { return "node:hq" },
		Files:     files, StartDeadline: deadline, MaxHold: deadline,
		Logger: slog.New(slog.NewJSONHandler(logged, nil)),
	})
	if err != nil {
		t.Fatalf("build the bridge: %v", err)
	}

	if err = made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	return fleet, node, made, logged
}

// logLines keeps the JSON log lines of a bridge.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

// records returns every line with its time removed.
func (l *logLines) records(t *testing.T) []map[string]any {
	t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()

	var out []map[string]any

	for line := range bytes.Lines(l.buf.Bytes()) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("read a log line: %v", err)
		}

		delete(record, "time")
		out = append(out, record)
	}

	return out
}

// settledWindow is a window with the settled event of transfer-1.
func settledWindow(t *testing.T) busapi.Window {
	t.Helper()

	return settledAs(t, event.TransferSettled{TransferID: "transfer-1", State: event.TransferDone})
}

// settledAs is a window with one settled event.
func settledAs(t *testing.T, frame event.TransferSettled) busapi.Window {
	t.Helper()

	settled, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("encode the settled event: %v", err)
	}

	envelope, err := json.Marshal(event.Envelope{Type: event.TypeDeviceTransferSettled, Data: settled})
	if err != nil {
		t.Fatalf("encode the envelope: %v", err)
	}

	return busapi.Window{Events: []json.RawMessage{envelope}}
}

// awaitTrue waits until cond holds.
func awaitTrue(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("waited in vain for %s", what)
}

func cubeURL() string {
	return sdkfiles.URL{ID: sdkfiles.Sum([]byte("GCDE cube")), Name: "cube.bgcode"}.String()
}

func TestPrintFileRefuses(t *testing.T) {
	t.Parallel()

	type outcome struct {
		Code     fractal.Code
		Commands int
		Prints   int
	}

	for name, tc := range map[string]struct {
		files  bool
		way    bool
		state  string
		iface  fractal.InterfaceName
		method fractal.MethodName
		url    string
		want   fractal.Code
	}{
		"a printer that is not ready": {
			true, true, "PRINTING", "Printer1", "PrintFile", cubeURL(), bridge.CodeStateRefused,
		},
		"a malformed URL": {
			true, true, "READY", "Printer1", "PrintFile", "fractal:///sha256:abc", bridge.CodeURLMalformed,
		},
		"a malformed URL to a printer that is not ready": {
			true, true, "PRINTING", "Printer1", "PrintFile", "fractal:///sha256:abc", bridge.CodeURLMalformed,
		},
		"a malformed file URL to a printer that is not ready": {
			true, true, "PRINTING", "Printer1", "PrintFile", "file:///usb/../cube.bgcode", bridge.CodeURLMalformed,
		},
		"no way to the file": {
			true, false, "READY", "Printer1", "PrintFile", cubeURL(), bridge.CodeObjectUnreachable,
		},
		"another scheme": {
			true, true, "READY", "Printer1", "PrintFile", "https://hq/cube.bgcode", bridge.CodeURLNotFetched,
		},
		"no scheme": {
			true, true, "READY", "Printer1", "PrintFile", "usb/cube.bgcode", bridge.CodeURLNotFetched,
		},
		"a malformed file URL": {
			true, true, "READY", "Printer1", "PrintFile", "file:///usb/../cube.bgcode", bridge.CodeURLMalformed,
		},
		"no files extension": {
			false, true, "READY", "Printer1", "PrintFile", cubeURL(), bridge.CodeURLNotFetched,
		},
		"a firmware URL": {
			true, true, "READY", "UpgradableDevice1", "UpgradeFromFile", cubeURL(), bridge.CodeURLNotFetched,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var files bridge.Files
			if tc.files {
				files = &fakeFiles{reachable: tc.way, storage: "library", source: "prints/cube.bgcode"}
			}

			fleet, node, _, _ := printingBridge(t, files, tc.state, 0)

			_, err := node.owner(t, tc.iface, fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
				Method: tc.method, Params: raws(strconv.Quote(tc.url)), Caller: "hq",
			})

			got := outcome{Commands: len(fleet.sent()), Prints: len(fleet.prints())}
			if raised, ok := errors.AsType[*fractal.Error](err); ok {
				got.Code = raised.Code
			}

			if want := (outcome{Code: tc.want}); !reflect.DeepEqual(got, want) {
				t.Errorf("the call gave %+v, want %+v", got, want)
			}
		})
	}
}

func TestPrintFilePrints(t *testing.T) {
	t.Parallel()

	files := &fakeFiles{reachable: true, storage: "library", source: "prints/cube.bgcode"}
	fleet, node, made, _ := printingBridge(t, files, "READY", 0)

	_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
		Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
	})
	if err != nil {
		t.Fatalf("PrintFile was refused: %v", err)
	}

	// The count follows the hold on the copy, so the settled event below finds it.
	awaitTrue(t, "the print", func() bool { return made.FetchCounts()[bridge.OutcomeStarted] == 1 })

	if err = made.Window(t.Context(), settledWindow(t)); err != nil {
		t.Fatalf("Window: %v", err)
	}

	type result struct {
		Prints   []deviceapi.PrintParams
		Commands int
		Released int
	}

	got := result{Prints: fleet.prints(), Commands: len(fleet.sent()), Released: files.releases()}
	want := result{
		Prints: []deviceapi.PrintParams{
			{
				Principal: "node:hq",
				ID:        printerOne,
				Storage:   "library",
				Source:    "prints/cube.bgcode",
				Path:      "usb/cube.bgcode",
			},
		},
		Released: 1,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the print gave %+v, want %+v", got, want)
	}
}

func TestPrintFileEnds(t *testing.T) {
	t.Parallel()

	notAvailable := fmt.Errorf("fetch: %w", pfiles.ErrNotAvailable)
	full := errors.New("the disk is full")
	busy := errors.New("the printer is busy")
	taken := fmt.Errorf("device.print: %w", deviceapi.ErrNameTaken)

	for name, tc := range map[string]struct {
		files    *fakeFiles
		deadline time.Duration
		refused  error
		outcome  string
		reason   string
		prints   int
		released int
	}{
		"not available": {
			files: &fakeFiles{reachable: true, failure: notAvailable}, outcome: bridge.OutcomeNotFound,
			reason: notAvailable.Error(),
		},
		"another failure": {
			files: &fakeFiles{reachable: true, failure: full}, outcome: bridge.OutcomeFailed, reason: full.Error(),
		},
		"too late": {
			files: &fakeFiles{reachable: true, block: true}, deadline: 100 * time.Millisecond,
			outcome: bridge.OutcomeDropped,
			reason:  context.DeadlineExceeded.Error(),
		},
		"a refused print": {
			files: &fakeFiles{reachable: true, storage: "library", source: "prints/cube.bgcode"}, refused: busy,
			outcome: bridge.OutcomeFailed, reason: busy.Error(), prints: 1, released: 1,
		},
		// The core starts a copy it sent itself, so a name taken at every
		// path holds a file nobody checked, and it is not printed.
		"every name taken": {
			files: &fakeFiles{
				reachable: true, storage: "library", source: "prints/cube.bgcode",
				paths: []string{"usb/cube.bgcode", "usb/cube-7FcFH9hw1lY1.bgcode"},
			},
			refused: taken, outcome: bridge.OutcomeFailed,
			reason: "the printer holds a file at usb/cube.bgcode and at usb/cube-7FcFH9hw1lY1.bgcode: " +
				taken.Error(),
			prints: 2, released: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node, made, logged := printingBridge(t, tc.files, "READY", tc.deadline)
			fleet.printRefuse = tc.refused

			_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
				Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
			})
			if err != nil {
				t.Fatalf("PrintFile was refused: %v", err)
			}

			awaitTrue(t, "the end of the print", func() bool { return len(made.FetchCounts()) == 1 })

			type result struct {
				Counts   map[string]uint64
				Log      []map[string]any
				Prints   int
				Released int
			}

			got := result{
				Counts: made.FetchCounts(), Log: logged.records(t), Prints: len(fleet.prints()),
				Released: tc.files.releases(),
			}
			want := result{
				Counts: map[string]uint64{tc.outcome: 1},
				Log: []map[string]any{{
					"level": "WARN", "msg": "a print of a file from the network did not start",
					"identity": sdkfiles.Sum([]byte("GCDE cube")).Text(), "printer": printerOne,
					"caller": "hq", "outcome": tc.outcome, "reason": tc.reason,
				}},
				Prints:   tc.prints,
				Released: tc.released,
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("the end was %+v, want %+v", got, want)
			}
		})
	}
}

func TestPrintFileStopsWithTheBridge(t *testing.T) {
	t.Parallel()

	files := &fakeFiles{reachable: true, block: true}
	_, node, made, logged := printingBridge(t, files, "READY", 0)

	running, stop := context.WithCancel(t.Context())
	ran := make(chan error, 1)

	go func() { ran <- made.Run(running) }()

	_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
		Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
	})
	if err != nil {
		t.Fatalf("PrintFile was refused: %v", err)
	}

	stop()

	if err = <-ran; err != nil {
		t.Fatalf("Run: %v", err)
	}

	type result struct {
		Counts map[string]uint64
		Lines  int
	}

	got := result{Counts: made.FetchCounts(), Lines: len(logged.records(t))}
	if want := (result{Counts: map[string]uint64{bridge.OutcomeDropped: 1}, Lines: 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("when Run returned, the print had %+v, want %+v", got, want)
	}
}

func TestPrintFileReleasesTheCopy(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		settleInPrint bool
		deadline      time.Duration
	}{
		"settled before the hold": {settleInPrint: true},
		"never settled":           {deadline: 100 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFiles{reachable: true, storage: deviceapi.ModuleDataStorage, source: "sha256-cube"}
			fleet, node, made, _ := printingBridge(t, files, "READY", tc.deadline)

			if tc.settleInPrint {
				fleet.onPrint = func() {
					if err := made.Window(context.Background(), settledWindow(t)); err != nil {
						t.Errorf("Window: %v", err)
					}
				}
			}

			_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
				Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
			})
			if err != nil {
				t.Fatalf("PrintFile was refused: %v", err)
			}

			awaitTrue(t, "the release of the copy", func() bool { return files.releases() == 1 })
		})
	}
}

// TestPrintFileTransferFails: a transfer that fails after Fleet.Print
// started it ends the print with one line and counts it failed, whether it
// settled before the print held its copy or after.
func TestPrintFileTransferFails(t *testing.T) {
	t.Parallel()

	failed := event.TransferSettled{
		TransferID: "transfer-1", State: event.TransferFailed, Reason: "the printer went away",
	}

	for name, settleInPrint := range map[string]bool{"after the hold": false, "before the hold": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFiles{reachable: true, storage: deviceapi.ModuleDataStorage, source: "sha256-cube"}
			fleet, node, made, logged := printingBridge(t, files, "READY", 0)

			if settleInPrint {
				fleet.onPrint = func() {
					if err := made.Window(context.Background(), settledAs(t, failed)); err != nil {
						t.Errorf("Window: %v", err)
					}
				}
			}

			_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
				Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
			})
			if err != nil {
				t.Fatalf("PrintFile was refused: %v", err)
			}

			// A settle before the hold counts the print started for a moment and
			// then failed, so that case waits for the failed count.
			counted := bridge.OutcomeStarted
			if settleInPrint {
				counted = bridge.OutcomeFailed
			}

			awaitTrue(t, "the print to be counted "+counted, func() bool { return made.FetchCounts()[counted] == 1 })

			if !settleInPrint {
				if err = made.Window(t.Context(), settledAs(t, failed)); err != nil {
					t.Fatalf("Window: %v", err)
				}
			}

			type result struct {
				Counts   map[string]uint64
				Log      []map[string]any
				Released int
			}

			got := result{Counts: made.FetchCounts(), Log: logged.records(t), Released: files.releases()}
			want := result{
				Counts: map[string]uint64{bridge.OutcomeFailed: 1},
				Log: []map[string]any{{
					"level": "WARN", "msg": "a print of a file from the network did not start",
					"identity": sdkfiles.Sum([]byte("GCDE cube")).Text(), "printer": printerOne,
					"caller": "hq", "outcome": bridge.OutcomeFailed,
					"reason": "the transfer failed: the printer went away",
				}},
				Released: 1,
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("a failed transfer gave %+v, want %+v", got, want)
			}
		})
	}
}

// TestPrintFileSettledTwiceCountsOnce: the core may report the end of one
// transfer in more than one window at a time, and the print is still counted
// failed once.
func TestPrintFileSettledTwiceCountsOnce(t *testing.T) {
	t.Parallel()

	failed := event.TransferSettled{
		TransferID: "transfer-1", State: event.TransferFailed, Reason: "the printer went away",
	}

	for range 20 {
		files := &fakeFiles{reachable: true, storage: deviceapi.ModuleDataStorage, source: "sha256-cube"}
		_, node, made, _ := printingBridge(t, files, "READY", 0)

		_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
			Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
		})
		if err != nil {
			t.Fatalf("PrintFile was refused: %v", err)
		}

		awaitTrue(t, "the print to be counted started", func() bool {
			return made.FetchCounts()[bridge.OutcomeStarted] == 1
		})

		var (
			together sync.WaitGroup
			start    = make(chan struct{})
		)

		for range 8 {
			together.Go(func() {
				<-start

				if windowErr := made.Window(t.Context(), settledAs(t, failed)); windowErr != nil {
					t.Errorf("Window: %v", windowErr)
				}
			})
		}

		close(start)
		together.Wait()

		if got, want := made.FetchCounts(), map[string]uint64{bridge.OutcomeFailed: 1}; !reflect.DeepEqual(got, want) {
			t.Fatalf("a transfer settled twice gave %v, want %v", got, want)
		}
	}
}

func TestPrintFileAfterRunDropsAtOnce(t *testing.T) {
	t.Parallel()

	files := &fakeFiles{reachable: true, storage: "library", source: "prints/cube.bgcode"}
	fleet, node, made, logged := printingBridge(t, files, "READY", 0)

	stopped, stop := context.WithCancel(t.Context())
	stop()

	if err := made.Run(stopped); err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
		Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
	})
	if err != nil {
		t.Fatalf("PrintFile was refused: %v", err)
	}

	type result struct {
		Counts  map[string]uint64
		Lines   int
		Obtains int
		Prints  int
	}

	got := result{
		Counts:  made.FetchCounts(),
		Lines:   len(logged.records(t)),
		Obtains: files.obtains(),
		Prints:  len(fleet.prints()),
	}
	if want := (result{Counts: map[string]uint64{bridge.OutcomeDropped: 1}, Lines: 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("a PrintFile after Run gave %+v, want %+v", got, want)
	}
}

func TestPrintFileWhileRunReturns(t *testing.T) {
	t.Parallel()

	const calls = 20

	files := &fakeFiles{reachable: true, block: true}
	_, node, made, _ := printingBridge(t, files, "READY", 0)
	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	running, stop := context.WithCancel(t.Context())
	ran := make(chan error, 1)

	go func() { ran <- made.Run(running) }()

	var callers sync.WaitGroup
	for range calls {
		callers.Go(func() {
			_, _ = owner.Invoke(t.Context(), fractalnode.Call{
				Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
			})
		})
	}

	stop()

	if err := <-ran; err != nil {
		t.Fatalf("Run: %v", err)
	}

	callers.Wait()

	if got, want := made.FetchCounts(), map[string]uint64{bridge.OutcomeDropped: calls}; !reflect.DeepEqual(got, want) {
		t.Errorf("the prints ended %v, want %v", got, want)
	}
}

// fakeAccess is the host's access.check. It answers allowed for every action
// in allow, and err for every question when err is set. It records the
// questions.
type fakeAccess struct {
	mu    sync.Mutex
	allow map[string]bool
	err   error
	asked []accessapi.CheckParams
}

func (a *fakeAccess) Check(_ context.Context, params accessapi.CheckParams) (accessapi.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.asked = append(a.asked, params)

	return accessapi.Decision{Allowed: a.allow[params.Action]}, a.err
}

// TestPrintFileAsksTheHostFirst: a caller who may not operate the printer or
// read the storages is refused with CodeCallNotAllowed before the file is
// fetched, as a PrintFile with a path is. A host that cannot answer the
// question leaves the check to device.print.
func TestPrintFileAsksTheHostFirst(t *testing.T) {
	t.Parallel()

	type outcome struct {
		Code   fractal.Code
		Asked  int
		Checks []accessapi.CheckParams
	}

	operate := accessapi.CheckParams{Subject: "node:hq", Action: "operate", ObjectType: "device", ObjectID: printerOne}
	read := accessapi.CheckParams{
		Subject: "node:hq", Action: "read_storage", ObjectType: "resource_kind", ObjectID: "storage",
	}

	for name, tc := range map[string]struct {
		access *fakeAccess
		want   outcome
	}{
		"both allowed": {
			&fakeAccess{allow: map[string]bool{"operate": true, "read_storage": true}},
			outcome{Asked: 1, Checks: []accessapi.CheckParams{operate, read}},
		},
		"no operate": {
			&fakeAccess{allow: map[string]bool{"read_storage": true}},
			outcome{Code: bridge.CodeCallNotAllowed, Checks: []accessapi.CheckParams{operate}},
		},
		"no read_storage": {
			&fakeAccess{allow: map[string]bool{"operate": true}},
			outcome{Code: bridge.CodeCallNotAllowed, Checks: []accessapi.CheckParams{operate, read}},
		},
		"a host that cannot answer": {
			&fakeAccess{err: accessapi.ErrUnknownSubject},
			outcome{Asked: 1, Checks: []accessapi.CheckParams{operate}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFiles{reachable: true, storage: "library", source: "prints/cube.bgcode"}
			reading := telemetry()
			reading["/state"] = json.RawMessage(`"READY"`)
			node := newNode()

			made, err := bridge.New(bridge.Config{
				Fleet: newFleet(printer(printerOne, reading)), Node: node, Mapping: mapping(t),
				Principal: func() string { return "node:hq" }, Files: files, Access: tc.access,
				Logger: slog.New(slog.DiscardHandler),
			})
			if err != nil {
				t.Fatalf("build the bridge: %v", err)
			}

			if err = made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			_, err = node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(),
				fractalnode.Call{Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq"})

			var got outcome
			if raised, ok := errors.AsType[*fractal.Error](err); ok {
				got.Code = raised.Code
			}

			if tc.want.Asked > 0 {
				awaitTrue(t, "the fetch", func() bool { return files.obtains() == tc.want.Asked })
			}

			got.Asked = files.obtains()
			tc.access.mu.Lock()
			got.Checks = tc.access.asked
			tc.access.mu.Unlock()

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the call gave %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestAnOfflinePrinterPrintsNoFractalURL: an offline printer reports BLOCKED,
// so a print of a fractal URL is refused before anything is fetched.
func TestAnOfflinePrinterPrintsNoFractalURL(t *testing.T) {
	t.Parallel()

	files := &fakeFiles{reachable: true, storage: "library", source: "prints/cube.bgcode"}
	fleet, node, made, _ := printingBridge(t, files, "READY", 0)

	away := fleet.devices[printerOne]
	away.Connected = false
	fleet.devices[printerOne] = away
	made.Touch(t.Context(), printerOne)

	_, err := node.owner(t, "Printer1", fractal.InstanceID(printerOne)).Invoke(t.Context(), fractalnode.Call{
		Method: "PrintFile", Params: raws(strconv.Quote(cubeURL())), Caller: "hq",
	})

	raised, _ := errors.AsType[*fractal.Error](err)
	want := fetchOutcome{raised: &fractal.Error{
		Code: bridge.CodeStateRefused, Message: textOf(bridge.CodeStateRefused),
	}}
	if got := (fetchOutcome{raised: raised, obtains: files.obtains()}); !reflect.DeepEqual(got, want) {
		t.Errorf("the call gave %+v", got)
	}
}

// fetchOutcome is what a print of a fractal URL did: the error it was
// answered with and how many fetches it started.
type fetchOutcome struct {
	raised  *fractal.Error
	obtains int
}

// textOf returns the sentence the bridge sends with a code.
func textOf(code fractal.Code) string {
	text, _ := bridge.TextOf(code)

	return text
}
