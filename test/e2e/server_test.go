package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The budgets. They are wall-clock: nothing here is in a synctest bubble,
// because a real listener is not durably blocking.
const (
	buildBudget   = 3 * time.Minute
	startBudget   = 30 * time.Second
	requestBudget = 10 * time.Second
	settleBudget  = 20 * time.Second
	settleStep    = 20 * time.Millisecond
)

// servingLine is what internal/app logs once the listener is bound. Reading the
// address out of it rather than picking a port first is what removes the window
// between choosing a port and the server taking it.
var servingLine = regexp.MustCompile(`msg=serving api=(\S+)`)

// setupCodeLine is the log line that carries the one-time setup code a fresh
// install prints, read the way an operator reads it in `docker logs`.
var setupCodeLine = regexp.MustCompile(`setup_code=([0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4})`)

// binary builds the command under test once for the whole package.
//
//nolint:gochecknoglobals // a once-value over a build, read-only after init.
var binary = sync.OnceValues(buildBinary)

func buildBinary() (string, error) {
	dir, err := os.MkdirTemp("", "pcl-e2e-*")
	if err != nil {
		return "", fmt.Errorf("make a build directory: %w", err)
	}

	name := "prusa-connect"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := filepath.Join(dir, name)

	ctx, cancel := context.WithTimeout(context.Background(), buildBudget)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./cmd/prusa-connect")
	cmd.Dir = repoRoot()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build the binary: %w: %s", err, output)
	}

	return path, nil
}

// repoRoot is two directories up from this package.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// log is the binary's own output, collected by the scanner goroutine and read
// by whichever test goroutine reports a failure.
//
// The two are concurrent and nothing orders them: a cleanup runs the moment the
// test fails, and the scanner is still draining the pipe. A plain bytes.Buffer
// there is a data race, which is invisible in a run without -race and is what
// the CI unit job reports on Linux.
type log struct {
	mu   sync.Mutex
	body bytes.Buffer
}

func (l *log) WriteString(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.body.WriteString(s)
}

func (l *log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.body.String()
}

// server is one running binary.
type server struct {
	baseURL string
	dir     string
	cmd     *exec.Cmd
	stderr  *log
	stopped chan error
	// gone is set once something has already collected the exit, so the
	// cleanup does not wait for a process that is not there.
	gone bool
}

// kill stops the binary the way a power cut does: no signal it can handle, no
// shutdown, nothing flushed. It is how a test proves that what is on disk is
// what a restart reads, rather than what a clean shutdown wrote on its way out.
func (s *server) kill(t *testing.T) {
	t.Helper()

	if s.gone {
		return
	}

	if err := s.cmd.Process.Signal(os.Kill); err != nil {
		t.Fatalf("kill the binary: %v", err)
	}

	select {
	case <-s.stopped:
	case <-time.After(startBudget):
		t.Fatalf("the binary did not die within %s", startBudget)
	}

	s.gone = true
}

// startServer writes a configuration, starts the binary on it and waits for the
// listener to be announced.
func startServer(t *testing.T) *server {
	t.Helper()

	return startServerWith(t, "")
}

// startServerWith is startServer with extra lines appended to the resources
// block, for a scenario that needs an instance the default file does not
// declare. The lines are indented as the block wants them and are this
// package's own literals.
func startServerWith(t *testing.T, resources string) *server {
	t.Helper()

	return startServerCore(t, "", resources)
}

// startServerCore is startServerWith with extra lines appended to the core
// block as well, for a scenario that needs a deployment key the default file
// does not set. The permissions lane needs one: a server that authenticates
// nobody serves every request as core.principal, so a test of what a second
// person may do has to turn the local password login on.
//
// The lines are indented as each block wants them and are this package's own
// literals.
func startServerCore(t *testing.T, core string, resources string) *server {
	t.Helper()

	return startServerIn(t, t.TempDir(), core, resources)
}

// startServerIn is startServerCore against a directory the caller holds, so a
// test can stop the binary and start it again over the same database, overlay
// and storage. That is what a restart is, and it is the only way to prove that
// something survives one.
func startServerIn(t *testing.T, dir string, core string, resources string) *server {
	t.Helper()

	recordOpenAdmission(t, dir)

	srv := launchServerIn(t, dir, core, resources)

	// A lane with the password login records its first run itself, because
	// that first run creates the administrator (setupFirstAdmin).
	if !strings.Contains(core, "local: true") {
		srv.settleFirstRun(t)
	}

	return srv
}

// recordOpenAdmission writes "let every printer in" into the overlay, as the
// first run records it. These lanes drive simulated printers with nobody at a
// keyboard to approve a code, and a server that has recorded no choice asks
// first. A connector block that sets its own mode still wins over it.
func recordOpenAdmission(t *testing.T, dir string) {
	t.Helper()

	overlay := filepath.Join(dir, "resources.json")
	document := map[string]any{"version": 1, "instances": []any{}}

	if existing, err := os.ReadFile(overlay); err == nil {
		if err = json.Unmarshal(existing, &document); err != nil {
			t.Fatalf("read the overlay: %v", err)
		}
	}

	document["admission"] = map[string]any{"mode": "open"}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode the overlay: %v", err)
	}

	if err = os.WriteFile(overlay, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write the overlay: %v", err)
	}
}

// launchServerIn starts the binary and waits for its listener, and records no
// first-run decision. A test of the first run itself starts here.
func launchServerIn(t *testing.T, dir string, core string, resources string) *server {
	t.Helper()

	path, err := binary()
	if err != nil {
		t.Fatalf("%v", err)
	}

	config := filepath.Join(dir, "config.yaml")

	// The operator's file, in the shape config/config.yaml documents: a core
	// block and one required storage instance, so readiness has something real
	// to report on.
	//
	// The api block is written last, directly above the caller's lines, so a
	// scenario may continue it at four spaces as well as add a sibling at two.
	// A second api block would be a duplicate key, which the parser refuses.
	body := strings.Join([]string{
		"core:",
		"  log:",
		"    level: info",
		"    format: text",
		"  overlay: " + yamlPath(filepath.Join(dir, "resources.json")),
		"  authz:",
		"    database: " + yamlPath(filepath.Join(dir, "authz.db")),
		"  principal: operator",
		"  site: e2e",
		// Every lane here talks to the binary over loopback. An mDNS
		// announcement would join the multicast group on every interface, and
		// on Windows the firewall then asks about each new build.
		"  discovery:",
		"    enabled: false",
		"  api:",
		`    addr: "127.0.0.1:0"`,
		core,
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: " + yamlPath(filepath.Join(dir, "storage")),
		resources,
		"",
	}, "\n")

	if err = os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, "core", "--config", config)
	cmd.Dir = dir

	stderr := &log{}
	reader, writer := io.Pipe()
	cmd.Stderr = writer
	cmd.Stdout = writer

	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start the binary: %v", err)
	}

	srv := &server{
		dir:     dir,
		cmd:     cmd,
		stderr:  stderr,
		stopped: make(chan error, 1),
	}

	addresses := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			stderr.WriteString(line + "\n")

			if match := servingLine.FindStringSubmatch(line); match != nil {
				select {
				case addresses <- match[1]:
				default:
				}
			}
		}
	}()

	go func() {
		waitErr := cmd.Wait()
		_ = writer.Close()
		srv.stopped <- waitErr
	}()

	t.Cleanup(func() {
		cancel()

		if !srv.gone {
			select {
			case <-srv.stopped:
			case <-time.After(startBudget):
				t.Errorf("the binary did not stop within %s", startBudget)
			}
		}

		if t.Failed() {
			t.Logf("the server's log:\n%s", stderr.String())
		}
	})

	select {
	case address := <-addresses:
		srv.baseURL = "http://" + address
	case err = <-srv.stopped:
		t.Fatalf("the binary exited before it served: %v\n%s", err, stderr.String())
	case <-time.After(startBudget):
		t.Fatalf("the binary did not announce a listener in %s\n%s", startBudget, stderr.String())
	}

	return srv
}

// settleFirstRun records what this server does about identity, because an
// install that has recorded nothing answers SETUP_PENDING to every other
// operation. It is the call an operator makes on the first-run page, and every
// scenario below starts after it the way a deployment does.
//
// The decision is open, which serves every request as core.principal and asks
// for no credential. That is what the lanes here assume. A lane that turns the
// password login on is decided before it starts, so this reads the state first
// and records nothing when the decision is already there: recording a second
// one is refused, and so is recording over the decision a restart read back.
func (s *server) settleFirstRun(t *testing.T) {
	t.Helper()

	state := s.get(t, "/api/v1/setup")
	if state.status != http.StatusOK {
		t.Fatalf("read the first run: %d %s", state.status, state.body)
	}

	if state.decode(t)["state"] != "pending" {
		return
	}

	recorded := s.do(t, http.MethodPost, "/api/v1/setup",
		`{"mode":"open","code":"`+s.setupCode(t)+`"}`, nil)
	if recorded.status != http.StatusOK {
		t.Fatalf("record the first run: %d %s\n%s", recorded.status, recorded.body, s.stderr.String())
	}
}

// setupCode is the one-time code this binary printed in its log. The code is
// printed once every port is open, which is after the listener is announced,
// so the line is waited for.
func (s *server) setupCode(t *testing.T) string {
	t.Helper()

	log, ok := s.awaitLog(setupCodeLine.MatchString)
	if !ok {
		t.Fatalf("the binary printed no setup code:\n%s", log)
	}

	return setupCodeLine.FindStringSubmatch(log)[1]
}

// awaitLog waits until the log holds what shows says it should, and returns
// the log as it then stood. What the start prints once its ports are open
// follows the serving line the address was read from, so it is waited for.
func (s *server) awaitLog(shows func(log string) bool) (string, bool) {
	deadline := time.Now().Add(settleBudget)

	for {
		log := s.stderr.String()
		if shows(log) {
			return log, true
		}

		if time.Now().After(deadline) {
			return log, false
		}

		time.Sleep(settleStep)
	}
}

// yamlPath quotes a path for the configuration file. A Windows path carries
// backslashes, which yaml reads as escapes inside double quotes.
func yamlPath(path string) string {
	return "'" + strings.ReplaceAll(path, `'`, `''`) + "'"
}

// response is one answer, read whole.
type response struct {
	status int
	body   []byte
	header http.Header
}

func (s *server) do(t *testing.T, method, path, body string, headers map[string]string) response {
	t.Helper()

	return s.doWithin(t, requestBudget, method, path, body, headers)
}

// doWithin is one request with a budget of its own. A command offered to a
// printer that is away waits for it, so the ordinary budget is not the one that
// call needs.
func (s *server) doWithin(
	t *testing.T, budget time.Duration, method, path, body string, headers map[string]string,
) response {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := (&http.Client{Timeout: budget}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}

	return response{status: resp.StatusCode, body: payload, header: resp.Header}
}

func (s *server) get(t *testing.T, path string) response {
	t.Helper()

	return s.do(t, http.MethodGet, path, "", nil)
}

// deviceListing is one directory of a printer's own tree once the printer has
// read it. The route answers at once, with reading set while the printer is
// still at it, so this asks again until the read lands, and fails on the fault
// a read that did not land is answered with.
func (s *server) deviceListing(t *testing.T, printer, directory string) response {
	t.Helper()

	// listingBudget is the server's own bound on one read of a folder.
	const listingBudget = 2 * time.Minute

	path := "/api/v1/devices/" + printer + "/files?path=" + url.QueryEscape(directory)
	deadline := time.Now().Add(listingBudget)

	for {
		listing := s.get(t, path)
		if listing.status != http.StatusOK {
			t.Fatalf("listing %s answered %d: %s", directory, listing.status, listing.body)
		}

		var state struct {
			Reading bool            `json:"reading"`
			Fault   json.RawMessage `json:"fault"`
		}

		if err := json.Unmarshal(listing.body, &state); err != nil {
			t.Fatalf("parse the listing of %s: %v", directory, err)
		}

		if state.Fault != nil {
			t.Fatalf("the printer did not list %s: %s", directory, listing.body)
		}

		if !state.Reading {
			return listing
		}

		if time.Now().After(deadline) {
			t.Fatalf("the printer was still reading %s after %s", directory, listingBudget)
		}

		time.Sleep(settleStep)
	}
}

// decode reads a JSON answer into a map, so a test can name one field.
func (r response) decode(t *testing.T) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}

	return out
}

// eventually retries until the condition holds or the budget runs out. It is
// for the supervision plane settling, which is asynchronous by design.
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(settleBudget)

	for time.Now().Before(deadline) {
		if condition() {
			return
		}

		time.Sleep(settleStep)
	}

	t.Fatalf("%s did not happen within %s", what, settleBudget)
}

// earlyAnswerWindow is how long a queued command is watched for an answer it
// must not give. It is wall-clock, so it is a compromise: long enough that a
// refusal would have arrived, short enough not to dominate the lane.
const earlyAnswerWindow = 500 * time.Millisecond

// awaitReady blocks until the process reports itself ready.
func (s *server) awaitReady(t *testing.T) {
	t.Helper()

	eventually(t, "the process reported itself ready", func() bool {
		return s.get(t, "/api/v1/readiness").status == http.StatusOK
	})
}

// publishes reports whether the served OpenAPI document carries a route. It is
// the document rather than a guessed path, because a route absent from it is a
// route this project's own contract test refuses to serve.
func (s *server) publishes(t *testing.T, route string) bool {
	t.Helper()

	document := s.get(t, "/openapi.json")
	if document.status != http.StatusOK {
		t.Fatalf("/openapi.json answered %d", document.status)
	}

	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(document.body, &spec); err != nil {
		t.Fatalf("parse the served document: %v", err)
	}

	_, published := spec.Paths[route]

	return published
}

// printerView is one printer as the API reports it.
type printerView struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Vendor          string                     `json:"vendor"`
	Model           string                     `json:"model"`
	Firmware        string                     `json:"firmware"`
	Serial          string                     `json:"serial"`
	Driver          string                     `json:"driver"`
	Connected       bool                       `json:"connected"`
	Capabilities    []string                   `json:"capabilities"`
	CapabilityEpoch uint64                     `json:"capability_epoch"`
	ProfileMatched  bool                       `json:"profile_matched"`
	State           map[string]json.RawMessage `json:"state"`
}

// attachPrinter registers a simulated printer of the named model against the
// running binary over a real socket, runs it, and waits for it to appear on the
// API. The printer is stopped when the test ends.
func (s *server) attachPrinter(t *testing.T, model string) (*simulator.Printer, printerView) {
	t.Helper()

	return s.attachPrinterWith(t, simulator.Config{Model: model})
}

// attachPrinterWith is attachPrinter for a printer the caller configures. The
// base URL is always the running binary's.
func (s *server) attachPrinterWith(t *testing.T, cfg simulator.Config) (*simulator.Printer, printerView) {
	t.Helper()

	cfg.BaseURL = s.baseURL
	model := cfg.Model

	printer, err := simulator.New(cfg)
	if err != nil {
		t.Fatalf("new %s: %v", model, err)
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))

	if err = printer.Register(ctx); err != nil {
		cancel()
		t.Fatalf("register a %s against the running binary: %v", model, err)
	}

	if printer.Token() == "" {
		cancel()
		t.Fatal("the handshake finished and the printer holds no token")
	}

	running := make(chan error, 1)
	go func() { running <- printer.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-running
	})

	var view printerView

	eventually(t, "the printer appeared on /api/v1/devices", func() bool {
		listed, ok := s.findPrinter(t, printer.Serial())
		if !ok {
			return false
		}

		view = listed

		return view.Connected
	})

	return printer, view
}

// findPrinter looks one printer up by the serial it reported.
func (s *server) findPrinter(t *testing.T, serial string) (printerView, bool) {
	t.Helper()

	listed := s.get(t, "/api/v1/devices")
	if listed.status != http.StatusOK {
		t.Fatalf("/api/v1/devices answered %d: %s", listed.status, listed.body)
	}

	var body struct {
		Items []printerView `json:"items"`
	}

	if err := json.Unmarshal(listed.body, &body); err != nil {
		t.Fatalf("parse the printer listing: %v", err)
	}

	for _, item := range body.Items {
		if item.Serial == serial {
			return item, true
		}
	}

	return printerView{}, false
}

// printer reads one printer by id.
func (s *server) printer(t *testing.T, id string) printerView {
	t.Helper()

	answer := s.get(t, "/api/v1/devices/"+id)
	if answer.status != http.StatusOK {
		t.Fatalf("/api/v1/devices/%s answered %d: %s", id, answer.status, answer.body)
	}

	var view printerView
	if err := json.Unmarshal(answer.body, &view); err != nil {
		t.Fatalf("parse the printer: %v", err)
	}

	return view
}

// awaitFrame reads the stream until a frame matches, and fails when the budget
// runs out rather than when the stream ends.
func awaitFrame(
	ctx context.Context, t *testing.T, frames <-chan frame, errs <-chan error, matches func(frame) bool,
) frame {
	t.Helper()

	var seen []string

	for {
		select {
		case f := <-frames:
			if matches(f) {
				return f
			}

			seen = append(seen, f.event)
		case err := <-errs:
			t.Fatalf("the event stream ended after %v: %v", seen, err)
		case <-ctx.Done():
			t.Fatalf("no matching frame arrived. %d frames did: %v", len(seen), seen)
		}
	}
}

// frame is one server-sent event.
type frame struct {
	id    string
	event string
	data  string
}

// subscribe opens GET /api/v1/events and reads frames until the context ends.
//
// It always subscribes from the head. Resuming from a stored id is a case no
// scenario here drives, so the header is set from a variable rather than from a
// parameter every caller passes empty.
func (s *server) subscribe(ctx context.Context, t *testing.T) (<-chan frame, <-chan error) {
	t.Helper()

	return s.subscribeWith(ctx, t, nil)
}

// subscribeWith is subscribe with the headers the request carries, which is how
// a client holding a bearer credential opens the stream.
func (s *server) subscribeWith(
	ctx context.Context, t *testing.T, headers map[string]string,
) (<-chan frame, <-chan error) {
	lastEventID := ""

	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("build the subscribe request: %v", err)
	}

	req.Header.Set("Accept", "text/event-stream")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	// A stream has no end, so it cannot go through a client with a timeout.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("subscribe answered %d", resp.StatusCode)
	}

	frames := make(chan frame, 256)
	errs := make(chan error, 1)

	go func() {
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

		var current frame

		for scanner.Scan() {
			line := scanner.Text()

			switch {
			case line == "":
				if current != (frame{}) {
					select {
					case frames <- current:
					default:
					}
				}

				current = frame{}
			case strings.HasPrefix(line, "id:"):
				current.id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			case strings.HasPrefix(line, "event:"):
				current.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				current.data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			errs <- scanErr

			return
		}

		errs <- fmt.Errorf("the stream closed: %w", ctx.Err())
	}()

	return frames, errs
}
