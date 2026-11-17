package wirehost_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The child these tests run is testdata/blobchild, a second process on the
// other side of the channel. The cases cover the process boundary: the
// handshake, the digest, the log, and the credit gate against a sender the host
// cannot see into.
//
// The channel is the standard input and output pair, which the specification
// names for Windows and which needs no socket and no port. On Linux and macOS
// the specification names a unix socket instead. The wire is the same on both,
// so what these cases cover holds there too.

// stall bounds every wait on the child. A child that stops writing while the
// host waits for it is the failure these tests catch, and this makes it report
// as a failure and not as the package timeout.
const stall = 30 * time.Second

// recorder collects what the host logged, so a test can read the child's own
// standard error back out of it.
type recorder struct {
	mu    sync.Mutex
	lines bytes.Buffer
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lines.Write(p)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lines.String()
}

// launch starts the child with the options a test varies.
func launch(t *testing.T, opts wirehost.Options) (*wirehost.Child, *recorder) {
	t.Helper()

	logged := &recorder{}

	opts.Instance = "blob-example"

	if opts.Path == "" {
		opts.Path = builtChild.file(t)
	}
	opts.Logger = slog.New(slog.NewTextHandler(logged, nil))

	child, err := wirehost.Launch(t.Context(), opts)
	if err != nil {
		t.Fatalf("launch the child: %v", err)
	}

	t.Cleanup(func() { _ = child.Close() })

	return child, logged
}

// TestAChildAnswersTheLifecycleMethodSet covers a whole launch. A second
// process starts, writes a handshake line, settles a wire minor, and answers
// the method set every child answers.
func TestAChildAnswersTheLifecycleMethodSet(t *testing.T) {
	t.Parallel()

	child, logged := launch(t, wirehost.Options{Digest: digestOf(t, builtChild.file(t))})

	if got := child.Minor(); got != 0 {
		t.Errorf("the two sides settled on minor %d, and both speak 0 to 0", got)
	}

	info := child.Info()
	if info.Name != "example-blob" {
		t.Errorf("the child reported the name %q, and blobchild reports example-blob", info.Name)
	}

	if err := info.Contract.CompatibleWith(wirerpc.Contract); err != nil {
		t.Errorf("the child's contract %s is not one this host serves: %v", info.Contract, err)
	}

	if err := child.Close(); err != nil {
		t.Errorf("stop the child: %v", err)
	}

	// Close waits for the mirror, so what the child wrote to its standard
	// error is all in the log by now.
	if got := logged.String(); !strings.Contains(got, "example-blob 1.0.0 is serving on stdio") {
		t.Errorf("the child's standard error did not reach the host's log:\n%s", got)
	} else if !strings.Contains(got, "instance=blob-example") {
		t.Errorf("the child's log lines are not bound to the instance:\n%s", got)
	}
}

// starved is what the child says when it has bytes left and no credit.
type starved struct {
	Sent int `json:"sent"`
}

// TestAChildStopsWritingWhenTheHostStopsReading covers the credit gate across a
// process boundary. The host grants one window and reads nothing, and the child
// stops after exactly what it was granted.
//
// Nothing here waits on a clock. The child says it has stopped, on a control
// frame that costs no credit, and the count it reports is the window.
func TestAChildStopsWritingWhenTheHostStopsReading(t *testing.T) {
	t.Parallel()

	const (
		window = 64
		total  = 4096
		chunk  = 16
	)

	stopped := make(chan starved, total/chunk)

	child, _ := launch(t, wirehost.Options{
		Window: window,
		Methods: wirerpc.Methods{
			"blob.starved": func(_ context.Context, call wirerpc.Call) (any, error) {
				var said starved
				if err := json.Unmarshal(call.Params, &said); err != nil {
					return nil, err
				}

				select {
				case stopped <- said:
				default:
				}

				return struct{}{}, nil
			},
		},
	})

	// Closing the session unblocks a read on it. The failure reported is the
	// session the child stopped speaking on, not the timeout.
	expired := time.AfterFunc(stall, func() { _ = child.Conn().Close() })
	defer expired.Stop()

	stream, err := child.Conn().Open()
	if err != nil {
		t.Fatalf("open a stream: %v", err)
	}

	if asked := stream.Notify("blob.read", map[string]int{"bytes": total, "chunk": chunk}); asked != nil {
		t.Fatalf("ask for the blob: %v", asked)
	}

	select {
	case first := <-stopped:
		if first.Sent != window {
			t.Errorf("the child stopped after %d bytes, and the host granted %d", first.Sent, window)
		}
	case <-child.Conn().Done():
		if !expired.Stop() {
			t.Fatalf("the child said nothing for %s", stall)
		}

		t.Fatalf("the session stopped before the child said it had: %v", child.Conn().Err())
	}

	read, err := io.ReadAll(stream)
	if err != nil {
		if !expired.Stop() {
			t.Fatalf("the child stopped writing for %s with %d of %d bytes read", stall, len(read), total)
		}

		t.Fatalf("read the blob: %v", err)
	}

	if len(read) != total {
		t.Fatalf("the blob is %d bytes, and %d were asked for", len(read), total)
	}

	for at, got := range read {
		if want := byte(at % 251); got != want {
			t.Fatalf("byte %d is %d, and the child wrote %d", at, got, want)
		}
	}
}

// TestAnArtefactThatDoesNotMatchItsDigestIsNotExecuted covers what the
// specification requires of a host: the recorded digest is verified before
// every launch and not only at install.
func TestAnArtefactThatDoesNotMatchItsDigestIsNotExecuted(t *testing.T) {
	t.Parallel()

	ran := filepath.Join(t.TempDir(), "ran")

	// A child that would leave a file behind if it ever started. It does not
	// run, so a missing file shows the refusal came first.
	_, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "blob-example",
		Path:     builtChild.file(t),
		Args:     []string{"-mark", ran},
		Digest:   wirehost.Digest("sha256:" + strings.Repeat("0", 64)),
	})

	if !errors.Is(err, wirehost.ErrDigestMismatch) {
		t.Fatalf("a launch over a wrong digest returned %v, and it is refused with %v",
			err, wirehost.ErrDigestMismatch)
	}

	if _, stat := os.Stat(ran); stat == nil {
		t.Error("the child ran, and a digest that does not match is checked before anything is executed")
	}
}

// TestAChildSpeakingAnotherWireMajorIsRefused covers version negotiation. Peers
// with different majors exchange no frame at all.
func TestAChildSpeakingAnotherWireMajorIsRefused(t *testing.T) {
	t.Parallel()

	_, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "blob-example",
		Path:     builtChild.file(t),
		Args:     []string{"-protocol", "2"},
	})

	if !errors.Is(err, wire.ErrProtocolMajor) {
		t.Fatalf("a child on another major was launched with %v, and it is refused with %v",
			err, wire.ErrProtocolMajor)
	}
}

// TestAChildThatWritesNoHandshakeIsRefused covers the other end of the same
// step. A child that says nothing is stopped and not waited on.
func TestAChildThatWritesNoHandshakeIsRefused(t *testing.T) {
	t.Parallel()

	_, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "blob-example",
		Path:     builtChild.file(t),
		Args:     []string{"-silent"},
	})

	if !errors.Is(err, wirehost.ErrHandshakeMissing) {
		t.Fatalf("a silent child was launched with %v, and it is refused with %v",
			err, wirehost.ErrHandshakeMissing)
	}
}

// TestAChildBuiltAgainstAnotherContractIsRefused covers the lifecycle contract.
// A child reports the version it was built against, and the host refuses a
// major it does not serve.
func TestAChildBuiltAgainstAnotherContractIsRefused(t *testing.T) {
	t.Parallel()

	child, err := wirehost.Launch(t.Context(), wirehost.Options{
		Instance: "blob-example",
		Path:     builtChild.file(t),
		Contract: extension.Version("2.0.0"),
	})
	if err == nil {
		_ = child.Close()

		t.Fatal("a child built against contract 1.0.0 was accepted by a host serving 2.0.0")
	}

	if !errors.Is(err, extension.ErrIncompatibleContract) {
		t.Fatalf("the refusal is %v, and an incompatible contract is %v", err, extension.ErrIncompatibleContract)
	}
}

// digestOf records what an installed artefact hashes to.
func digestOf(t *testing.T, path string) wirehost.Digest {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	sum := sha256.Sum256(body)

	return wirehost.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// TestAChildIsHeldToItsManifest covers the check against the install record. A
// module that ships a process and a browser bundle has one module.yaml, so the
// child that answers has to be the module that file describes.
func TestAChildIsHeldToItsManifest(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		document string
		accepts  bool
	}{
		"the module the child is": {
			`{"name":"example-blob","kind":"connector","license":"MIT","version":"1.0.0","contract_major":1,
			  "ui":{"entry":"ui/index.js","library":">=0.3.0 <1.0.0"}}`,
			true,
		},
		"a manifest with no ui block": {
			`{"name":"example-blob","kind":"connector","license":"MIT","version":"1.0.0"}`,
			true,
		},
		"another module's name": {
			`{"name":"example-spools","kind":"connector","license":"MIT",
			  "ui":{"entry":"ui/index.js","library":">=0.3.0 <1.0.0"}}`,
			false,
		},
		"another release": {
			`{"name":"example-blob","kind":"connector","license":"MIT","version":"2.0.0"}`,
			false,
		},
		"another wire major": {
			`{"name":"example-blob","kind":"connector","license":"MIT","contract_major":2}`,
			false,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			declared, err := manifest.Parse("module.yaml", []byte(testCase.document), json.Unmarshal)
			if err != nil {
				t.Fatalf("the manifest was refused: %v", err)
			}

			child, err := wirehost.Launch(t.Context(), wirehost.Options{
				Instance: "blob-example",
				Path:     builtChild.file(t),
				Manifest: &declared,
			})
			if child != nil {
				t.Cleanup(func() { _ = child.Close() })
			}

			if testCase.accepts {
				if err != nil {
					t.Fatalf("the child was refused against its own manifest: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("a child that is not the module its manifest describes was accepted")
			}

			if !errors.Is(err, wirehost.ErrManifestDisagrees) {
				t.Fatalf("the refusal is %v, and a disagreeing manifest is %v", err, wirehost.ErrManifestDisagrees)
			}
		})
	}
}

// TestCloseEndsAChildWhoseGrandchildHoldsStderr covers a module that starts a
// process of its own. The child exits on shutdown while its grandchild keeps
// the standard error open. Close returns well inside Stop, because the host
// ends the group as soon as the leader is gone, and the grandchild is gone too.
func TestCloseEndsAChildWhoseGrandchildHoldsStderr(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the grandchild is sleep(1)")
	}

	child, logged := launch(t, wirehost.Options{Path: builtFork.file(t), Args: []string{"-sleeper"}, Stop: stall})
	pid := grandchildOf(t, logged)

	began := time.Now()
	_ = child.Close()

	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("Close took %v, and a child that obeys shutdown is not waited out", took)
	}

	awaitEnded(t, pid)
}

// TestAChildThatExitsBeforeItsGrandchildIsSeenToExit covers a module that
// crashes and leaves a process behind holding its standard error. The exit is
// still seen, and the leftover process is ended with it.
func TestAChildThatExitsBeforeItsGrandchildIsSeenToExit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the grandchild is sleep(1)")
	}

	child, logged := launch(t, wirehost.Options{Path: builtFork.file(t), Args: []string{"-sleeper"}})
	pid := grandchildOf(t, logged)

	go func() { _, _ = child.Conn().Call(t.Context(), "fork.exit", 3) }()

	select {
	case <-child.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("the child exited and Exited did not close")
	}

	if got, want := child.ExitStop(), (supervisor.Stop{Result: supervisor.ResultExited, Code: 3}); got != want {
		t.Errorf("ExitStop is %+v, want %+v", got, want)
	}

	awaitEnded(t, pid)
}

// TestCloseEndsAChildWhoseEscapedGrandchildHoldsStderr covers a module that
// starts a process in a session of its own. The group kill misses it, and it
// keeps the standard error open, so the host stops waiting for the pipe after
// its backstop. Close returns, and Exited is closed.
func TestCloseEndsAChildWhoseEscapedGrandchildHoldsStderr(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the grandchild is sleep(1), and a job holds it anyway")
	}

	const stop, backstop = time.Second, 5 * time.Second

	child, logged := launch(
		t,
		wirehost.Options{Path: builtFork.file(t), Args: []string{"-sleeper", "-setsid"}, Stop: stop},
	)
	pid := grandchildOf(t, logged)

	t.Cleanup(func() {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill() // outside the group, so the host does not end it.
		}
	})

	began := time.Now()
	_ = child.Close()

	if took, limit := time.Since(began), stop+backstop+2*time.Second; took > limit {
		t.Errorf("Close took %v, want at most %v", took, limit)
	}

	select {
	case <-child.Exited():
	default:
		t.Error("Close returned and Exited is not closed")
	}
}

// grandchildOf reads the pid forkchild -sleeper names on its standard error.
func grandchildOf(t *testing.T, logged *recorder) int {
	t.Helper()

	named := regexp.MustCompile(`grandchild (\d+)`)

	for deadline := time.Now().Add(stall); ; time.Sleep(10 * time.Millisecond) {
		if found := named.FindStringSubmatch(logged.String()); found != nil {
			pid, _ := strconv.Atoi(found[1])

			return pid
		}

		if time.Now().After(deadline) {
			t.Fatalf("the child never named its grandchild:\n%s", logged)
		}
	}
}

// awaitEnded fails the test unless pid ends within two seconds.
func awaitEnded(t *testing.T, pid int) {
	t.Helper()

	for deadline := time.Now().Add(2 * time.Second); !ended(pid); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the grandchild %d outlived its module", pid)
		}
	}
}

// TestAChildThatExitsReportsItsCode covers a child that ends on its own.
func TestAChildThatExitsReportsItsCode(t *testing.T) {
	t.Parallel()

	child, _ := launch(t, wirehost.Options{Path: builtFork.file(t)})

	// The child exits instead of answering, so the call has no result to read.
	go func() { _, _ = child.Conn().Call(t.Context(), "fork.exit", 3) }()

	select {
	case <-child.Exited():
	case <-time.After(stall):
		t.Fatal("the child was told to exit and Exited never closed")
	}

	if got, want := child.ExitStop(), (supervisor.Stop{Result: supervisor.ResultExited, Code: 3}); got != want {
		t.Errorf("ExitStop is %+v, want %+v", got, want)
	}
}

// TestAKilledChildReportsTheSignal covers a child ended by a signal.
func TestAKilledChildReportsTheSignal(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a Windows process does not end by a signal")
	}

	child, _ := launch(t, wirehost.Options{Path: builtFork.file(t)})

	if err := child.Kill(); err != nil {
		t.Fatalf("kill the child: %v", err)
	}

	select {
	case <-child.Exited():
	case <-time.After(stall):
		t.Fatal("the group was killed and Exited never closed")
	}

	if got, want := child.ExitStop(), (supervisor.Stop{Result: supervisor.ResultSignal, Signal: "SIGKILL"}); got != want {
		t.Errorf("ExitStop is %+v, want %+v", got, want)
	}
}

// ended reports whether pid is gone or a zombie. Whoever adopted the orphan
// reaps it, and in a container whose first process reaps nothing that never
// happens.
func ended(pid int) bool {
	if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		return strings.Contains(string(stat), ") Z ")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}

	return errors.Is(process.Signal(syscall.Signal(0)), os.ErrProcessDone)
}

// TestAReapedChildIsNotKilledAgain covers a kill that arrives after the child
// has been reaped. Its group or job is gone by then, so Kill sends nothing and
// reports nothing, and the recorded exit stands.
func TestAReapedChildIsNotKilledAgain(t *testing.T) {
	t.Parallel()

	child, _ := launch(t, wirehost.Options{Path: builtFork.file(t)})

	if got := child.ExitStop(); got != (supervisor.Stop{}) {
		t.Errorf("ExitStop of a running child is %+v, want the zero Stop", got)
	}

	go func() { _, _ = child.Conn().Call(t.Context(), "fork.exit", 3) }()

	select {
	case <-child.Exited():
	case <-time.After(stall):
		t.Fatal("the child was told to exit and Exited never closed")
	}

	if err := child.Kill(); err != nil {
		t.Errorf("Kill after the child was reaped: %v", err)
	}

	if got, want := child.ExitStop(), (supervisor.Stop{Result: supervisor.ResultExited, Code: 3}); got != want {
		t.Errorf("ExitStop is %+v, want %+v", got, want)
	}
}
