package firmware

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/tools/mini404"
)

// The lane's switch and the one printer this milestone covers.
const (
	// envLane turns the lane on. Off, every scenario skips: a scenario here is a
	// QEMU process and minutes of wall clock, so it runs when somebody asked.
	envLane = "PCL_FIRMWARE"
	// laneTarget is what a skip line names, so a reader of a skipped run knows
	// how to unskip it.
	laneTarget = "make test-firmware"
	// laneModel is matched against the pin table, which carries the machine, the
	// build preset and the firmware revision for it.
	laneModel = "MK4"
)

// The budgets. All wall-clock: a QEMU process and a real listener are outside
// any synctest bubble, so these are paid in full. They are generous on purpose,
// because this lane blocks nothing and a false failure here costs more than a
// slow pass.
const (
	// buildBudget covers building the server binary once for the package.
	buildBudget = 3 * time.Minute
	// startBudget covers the binary binding its listeners, and its shutdown.
	startBudget = 30 * time.Second
	// requestBudget bounds one call to the server's own HTTP API, which is
	// loopback and already up.
	requestBudget = 10 * time.Second

	// bootBudget is the launch to PrusaLink answering on the forwarded port. A
	// warm xflash boots in a fraction of this; the figure is sized for a loaded
	// host running at instruction-level emulation, which tools/mini404smoke
	// budgets ten minutes for.
	bootBudget = 12 * time.Minute
	// registerBudget is PrusaLink answering to the printer appearing connected
	// on the server's API. The firmware's Connect client starts after the rest
	// of the network stack and retries on a schedule of its own, so this covers
	// several of its attempts rather than one.
	registerBudget = 5 * time.Minute
	// telemetryBudget is a stimulus on the script console reaching the server's
	// API. It covers the firmware's own sampling interval, the telemetry frame
	// and the state update behind it.
	telemetryBudget = 3 * time.Minute
	// commandBudget bounds one synchronous command round trip to a printer that
	// is attached. The answer waits on the firmware's own command queue, not
	// only on the wire, so it is minutes rather than the requestBudget a plain
	// read gets.
	commandBudget = 3 * time.Minute
	// disconnectBudget is a dead guest to the API reporting the printer as not
	// connected. The QEMU process exiting closes the host side of the socket,
	// so the server's read fails at once; the budget covers a loaded host and
	// the liveness backstop behind it.
	disconnectBudget = 2 * time.Minute
	// drainBudget bounds a command queued for a printer that is away: it spans
	// the whole reboot and the registration that follows, so it is the sum of
	// the budgets those two waits get.
	drainBudget = bootBudget + registerBudget
	// bootstrapBudget is the one-off first boot on a zeroed xflash, where the
	// firmware unpacks its resources from the USB stick and formats a
	// filesystem before it serves anything.
	bootstrapBudget = 15 * time.Minute
	// listBudget bounds a read the server answers by asking the printer, which
	// is the file listing. See getWithin for why it is not requestBudget.
	listBudget = 90 * time.Second
	// transferBudget bounds one file leaving a storage and landing on the
	// printer's own stick, including the readback the driver takes afterwards.
	// The send route is synchronous, so this is paid on one HTTP call.
	transferBudget = 4 * time.Minute
	// printBudget bounds a whole travel-only print on the emulated machine: the
	// preview dialog, the homing before the first move, and the moves.
	printBudget = 10 * time.Minute
	// pollInterval is how often a scenario reads the device render while it
	// watches a print. It is the interval the live re-test used, halved.
	pollInterval = 2 * time.Second
	// closeBudget covers the QMP quit and reaping the QEMU process.
	closeBudget = 30 * time.Second
	// diagnosticBudget bounds a screendump taken while a failed test tears down.
	diagnosticBudget = 30 * time.Second
)

// The phase names. Every wait in this lane carries one, so a timeout says
// which part of the boot died rather than which line of Go was executing.
const (
	phaseArtefacts  = "artefacts"
	phaseEEPROM     = "eeprom"
	phaseLaunch     = "qemu-launch"
	phaseBoot       = "boot-to-prusalink"
	phaseRegistered = "prusalink-to-registered"
	phaseTelemetry  = "telemetry-follows-the-thermistor"
	phaseSampled    = "telemetry-nozzle-sampled"
	phaseGuestDown  = "guest-down-to-disconnected"
)

// servingLine is what internal/app logs once the API listener is bound. The
// device-facing listener is a second socket on a port this lane picks, because
// the firmware has to be told where to dial before it boots; the API port is
// read out of this line, because nothing has to know it in advance.
var servingLine = regexp.MustCompile(`msg=serving api=(\S+)`)

// laneEnabled is read once, so every scenario skips on the same value.
//
//nolint:gochecknoglobals // read-only after TestMain.
var laneEnabled bool

// binary builds the command under test once for the whole package.
//
//nolint:gochecknoglobals // a once-value over a build, read-only after init.
var binary = sync.OnceValues(buildBinary)

func TestMain(m *testing.M) {
	laneEnabled = os.Getenv(envLane) == "1"

	os.Exit(m.Run())
}

// requireLane skips unless the lane was asked for.
func requireLane(t *testing.T) {
	t.Helper()

	if !laneEnabled {
		t.Skipf("firmware in the loop is off. Run %s (%s=1) to boot real firmware under MINI404.",
			laneTarget, envLane)
	}
}

func buildBinary() (string, error) {
	// The build outlives every test in the package, so it cannot go in a
	// test's own temporary directory.
	dir, err := os.MkdirTemp("", "pcl-firmware-*")
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

// server is one running binary.
type server struct {
	baseURL string
	stopped chan error
	// storage is the root of the filesystem storage resource the configuration
	// declares. A scenario that sends a file to the printer puts the source
	// there: the store reads its objects off the disk, so a file dropped in
	// after the process started is an object the send route can name.
	storage string
}

// startServer writes a configuration whose device-facing listener is on
// printerPort, starts the binary on it, and waits for the API listener to be
// announced.
//
// The port is a parameter rather than something read back out of the log
// because the firmware learns where to dial from an EEPROM image written before
// it boots. Reading a bound port back is what test/e2e does, and it cannot work
// here.
func startServer(t *testing.T, printerPort int) *server {
	t.Helper()

	path, err := binary()
	if err != nil {
		t.Fatalf("%v", err)
	}

	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")

	if err = os.WriteFile(config, []byte(configFile(dir, printerPort)), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	// The storage root is the store's to create, and it is created here as well
	// so a scenario may put a source file in it before the process is up.
	if err = os.MkdirAll(filepath.Join(dir, "storage"), 0o700); err != nil {
		t.Fatalf("make the storage root: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, "core", "--config", config)
	cmd.Dir = dir

	recorded := &bytes.Buffer{}
	reader, writer := io.Pipe()
	cmd.Stderr = writer
	cmd.Stdout = writer

	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start the binary: %v", err)
	}

	srv := &server{stopped: make(chan error, 1), storage: filepath.Join(dir, "storage")}
	addresses := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			recorded.WriteString(line + "\n")

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

		select {
		case <-srv.stopped:
		case <-time.After(startBudget):
			t.Errorf("the binary did not stop within %s", startBudget)
		}

		if t.Failed() {
			t.Logf("the server's log:\n%s", recorded.String())
		}
	})

	select {
	case address := <-addresses:
		srv.baseURL = "http://" + address
	case err = <-srv.stopped:
		t.Fatalf("the binary exited before it served: %v\n%s", err, recorded.String())
	case <-time.After(startBudget):
		t.Fatalf("the binary did not announce a listener in %s\n%s", startBudget, recorded.String())
	}

	srv.settleFirstRun(t)

	return srv
}

// settleFirstRun records what this server does about identity, because an
// install that has recorded nothing answers SETUP_PENDING to every other
// operation. The decision is open, which serves every request as the
// configured principal and asks for no credential, which is what every lane
// here assumes. A server that read a decision back is left as it is.
func (s *server) settleFirstRun(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), startBudget)
	defer cancel()

	status, body, err := s.get(ctx, "/api/v1/setup")
	if err != nil || status != http.StatusOK {
		t.Fatalf("read the first run: %d %s %v", status, body, err)
	}

	var state struct {
		State string `json:"state"`
	}
	if err = json.Unmarshal(body, &state); err != nil {
		t.Fatalf("decode the first run: %v", err)
	}

	if state.State != "pending" {
		return
	}

	status, body, err = s.post(ctx, "/api/v1/setup", []byte(`{"mode":"open","code":"`+setupCode+`"}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("record the first run: %d %s %v", status, body, err)
	}
}

// setupCode is the first-run code this lane writes into core.auth.setup_code,
// the way an automated install does, so no step reads it out of the log.
const setupCode = "FIRMWARE-LANE-SETUP"

// configFile is the operator's file this lane runs the binary on.
//
// core.printer.addr is the socket the firmware dials, and it is loopback
// because the guest reaches the host through the slirp gateway at 10.0.2.2,
// which QEMU proxies to the host's loopback. It carries no tls key, which is a
// listener with no certificate and therefore plain HTTP: the firmware verifies
// a certificate against a CA it loads from a USB stick, and seeding that is a
// later milestone. The core block is decoded with unknown keys refused, so a
// key that is not on internal/app/config.Core stops the binary starting.
//
// The admission the seeded token relies on is mode none, which admits any token
// a printer presents, and the lane declares it: a server that has recorded no
// admission choice asks first (internal/app/connectors.go, withAdmission), and
// the first run this lane records above leaves that choice at its default. The
// operator's own key wins over both.
func configFile(dir string, printerPort int) string {
	return strings.Join([]string{
		"core:",
		"  api:",
		`    addr: "127.0.0.1:0"`,
		"  printer:",
		`    addr: "127.0.0.1:` + strconv.Itoa(printerPort) + `"`,
		"  connectors:",
		"    xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        mode: none",
		"  log:",
		"    level: info",
		"    format: text",
		"  overlay: " + yamlPath(filepath.Join(dir, "resources.json")),
		"  authz:",
		"    database: " + yamlPath(filepath.Join(dir, "authz.db")),
		"  principal: operator",
		"  site: firmware",
		"  auth:",
		"    setup_code: " + setupCode,
		// The lane talks to the firmware over loopback alone, and an mDNS
		// announcement would join the multicast group on every interface.
		"  discovery:",
		"    enabled: false",
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: " + yamlPath(filepath.Join(dir, "storage")),
		"",
	}, "\n")
}

// yamlPath quotes a path for the configuration file. A Windows path carries
// backslashes, which yaml reads as escapes inside double quotes.
func yamlPath(path string) string {
	return "'" + strings.ReplaceAll(path, `'`, `''`) + "'"
}

// printerView is one device as the API reports it, with the fields this lane
// reads. It is spelled out rather than imported: this reads the server the way
// a client outside the process does.
type printerView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Firmware string `json:"firmware"`
	Serial   string `json:"serial"`
	// UnitUID is the identity the machine keeps across reconnections, which for
	// this dialect is the fingerprint it presents. It is what makes a rebooted
	// machine the same device rather than a second one.
	UnitUID        string                     `json:"unit_uid"`
	Driver         string                     `json:"driver"`
	Connected      bool                       `json:"connected"`
	Capabilities   []string                   `json:"capabilities"`
	ProfileMatched bool                       `json:"profile_matched"`
	State          map[string]json.RawMessage `json:"state"`
}

// get is one GET against the server's API, whole.
func (s *server) get(ctx context.Context, path string) (int, []byte, error) {
	return s.getWithin(ctx, path, requestBudget)
}

// getWithin is get for a read the server answers by asking the printer.
//
// requestBudget is sized for a read the server answers out of what it already
// holds. A printer's own file listing is not that: the server asks the machine
// and waits, and an emulated machine at instruction-level speed takes tens of
// seconds over a directory a real board answers in one.
func (s *server) getWithin(ctx context.Context, path string, budget time.Duration) (int, []byte, error) {
	called, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	req, err := http.NewRequestWithContext(called, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build GET %s: %w", path, err)
	}

	resp, err := (&http.Client{Timeout: budget}).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s: %w", path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read GET %s: %w", path, err)
	}

	return resp.StatusCode, body, nil
}

// The API paths this lane reads. Printers are devices on this surface: one
// listing carries every kind the host serves, and a printer is a row whose kind
// says so. The paths are spelled out here rather than built at each call site,
// so a route that moves again moves in one place.
const (
	devicesPath       = "/api/v1/devices"
	devDevicesPath    = "/api/v1/dev/devices"
	registrationsPath = "/api/v1/registrations"
)

// printerKind is what the device listing calls a printer, so this lane reads
// past a camera or anything else the host serves on the same surface.
const printerKind = "printer"

// printers is the fleet as the API lists it, narrowed to printers.
func (s *server) printers(ctx context.Context) ([]printerView, error) {
	status, body, err := s.get(ctx, devicesPath)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d: %s", devicesPath, status, body)
	}

	// The listing is a page: items and the count behind them. This lane boots
	// one machine, so a page never truncates it and no cursor is followed.
	var listing struct {
		Items []printerView `json:"items"`
		Total int           `json:"total"`
	}

	if err = json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse the device listing: %w", err)
	}

	found := make([]printerView, 0, len(listing.Items))

	for _, item := range listing.Items {
		if item.Kind == printerKind {
			found = append(found, item)
		}
	}

	return found, nil
}

// post is one POST against the server's API, whole. Unlike get it carries no
// timeout of its own: a command for a printer that is away queues until the
// printer returns, so the caller's context is the only honest bound.
func (s *server) post(ctx context.Context, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build POST %s: %w", path, err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("POST %s: %w", path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read POST %s: %w", path, err)
	}

	return resp.StatusCode, answer, nil
}

// commandResult is what the printer made of a command, with the fields this
// lane reads. Spelled out for the same reason printerView is.
type commandResult struct {
	// CommandID is the invocation the server minted. Every durable frame of the
	// command carries it, so it is what a row is read back under.
	CommandID     string `json:"command_id"`
	OK            bool   `json:"ok"`
	MachineReason string `json:"machine_reason"`
	// FaultCode is this API's own word for a refusal, where MachineReason is the
	// machine's. A printer that refused and said nothing carries only this.
	FaultCode string `json:"fault_code"`
}

// sendCommand asks one printer to carry out one verb and returns the printer's
// answer. The context is the caller's because the wait is the point: a command
// sent while the printer is away returns when the printer does.
func (s *server) sendCommand(ctx context.Context, id, verb string) (commandResult, error) {
	return s.sendCommandArgs(ctx, id, verb, nil)
}

// sendCommandArgs is sendCommand for a verb that takes arguments, such as the
// path a print starts from or the button a dialog is answered with. Absent
// arguments are left off the request rather than sent empty, because the
// capability check reads which arguments were named.
func (s *server) sendCommandArgs(ctx context.Context, id, verb string, args map[string]any) (commandResult, error) {
	request := map[string]any{"verb": verb}
	if len(args) > 0 {
		request["args"] = args
	}

	body, err := json.Marshal(request)
	if err != nil {
		return commandResult{}, fmt.Errorf("encode the command: %w", err)
	}

	status, answer, err := s.post(ctx, devicesPath+"/"+id+"/commands", body)
	if err != nil {
		return commandResult{}, err
	}

	if status != http.StatusOK {
		return commandResult{}, fmt.Errorf("POST %s to %s answered %d: %s", verb, id, status, answer)
	}

	var result commandResult
	if err = json.Unmarshal(answer, &result); err != nil {
		return commandResult{}, fmt.Errorf("parse the command result: %w", err)
	}

	return result, nil
}

// recordedCommand is one row of the developer command log, with the fields
// this lane reads.
type recordedCommand struct {
	Verb      string `json:"verb"`
	OK        bool   `json:"ok"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// recordedCommands reads the developer command log for one printer.
func (s *server) recordedCommands(ctx context.Context, id string) ([]recordedCommand, error) {
	path := devDevicesPath + "/" + id + "/commands"

	status, body, err := s.get(ctx, path)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d: %s", path, status, body)
	}

	var listing struct {
		Items []recordedCommand `json:"items"`
	}

	if err = json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse the command log: %w", err)
	}

	return listing.Items, nil
}

// printer reads one printer by the id the API minted for it.
func (s *server) printer(ctx context.Context, id string) (printerView, error) {
	status, body, err := s.get(ctx, devicesPath+"/"+id)
	if err != nil {
		return printerView{}, err
	}

	if status != http.StatusOK {
		return printerView{}, fmt.Errorf("%s/%s answered %d: %s", devicesPath, id, status, body)
	}

	var view printerView
	if err = json.Unmarshal(body, &view); err != nil {
		return printerView{}, fmt.Errorf("parse the printer: %w", err)
	}

	return view, nil
}

// registrationView is one pending registration as the API lists it, with the
// fields this lane reads.
type registrationView struct {
	Code        string `json:"code"`
	Connector   string `json:"connector"`
	Fingerprint string `json:"fingerprint"`
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Firmware    string `json:"firmware"`
	Approved    bool   `json:"approved"`
}

// registrations is what is waiting for a decision.
//
// It is a diagnostic rather than an assertion for the seeded-token path, which
// never posts to /p/register. For the tokenless path it is the difference
// between firmware that dialled and was held, and firmware that never dialled
// at all, and a timeout that cannot say which is a timeout nobody can act on.
func (s *server) registrations(ctx context.Context) ([]registrationView, error) {
	status, body, err := s.get(ctx, registrationsPath)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d: %s", registrationsPath, status, body)
	}

	var listing struct {
		Items []registrationView `json:"items"`
	}

	if err = json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse the registration listing: %w", err)
	}

	return listing.Items, nil
}

// rig is one emulated printer, booted, connected, and the server it dialled.
type rig struct {
	server  *server
	printer *mini404.Printer
	pin     mini404.Pin
	// port is the device-facing port the firmware was told to dial, which is
	// the value baked into the EEPROM image before boot.
	port int
	// usb is the host directory the guest mounts as its USB stick, empty for a
	// machine booted with no stick. It is where a transferred file lands, as
	// far as QEMU's own FAT emulation writes anything back.
	usb string
	// guest is the launch the printer came from. A scenario that power-cycles
	// the machine boots it again: the same EEPROM banks and the same external
	// flash, under a fresh runtime directory.
	guest mini404.Config
}

// seededToken is the Connect token written into the EEPROM before the guest
// boots. It is twenty characters because that is the width of the firmware's
// own field, and its value is arbitrary: admission mode none accepts any token
// a printer presents.
//
// Holding a token is what makes the boot headless. The firmware skips the
// on-screen registration and dials GET /p/ws directly, so nothing has to press
// a button on the LCD. An empty token is the /p/register flow instead, which
// startTokenlessRig boots.
const seededToken = "0123456789abcdefghij"

// startRig is the lane's printer: booted, seeded with a token, and dialling the
// running binary.
func startRig(t *testing.T) *rig {
	t.Helper()

	return startRigWith(t, seededToken, false)
}

// startRigWithStick is the lane's printer with a USB stick in it, which is the
// machine a file may be transferred to and a print may be started from.
//
// The stick is a directory of the host's, exposed through QEMU's own FAT
// emulation (tools/mini404/mini404.go, Config.USBDir). It is off for
// every other scenario, so a board that behaves differently with a mass-storage
// device attached changes only the scenarios that need one.
func startRigWithStick(t *testing.T) *rig {
	t.Helper()

	return startRigWith(t, seededToken, true)
}

// startTokenlessRig is the same printer with no Connect token, which is the
// firmware that has to register itself over /p/register before it may dial.
func startTokenlessRig(t *testing.T) *rig {
	t.Helper()

	return startRigWith(t, "", false)
}

// startRigWith brings the whole lane up, in the one order that works.
//
// The connect port is written into an EEPROM image before the guest boots, so
// it has to be chosen before the server starts and cannot be read back off a
// bound listener. Artefacts resolve first, ahead of the three-minute binary
// build, so a cache miss fails in a second with the script that fills it.
func startRigWith(t *testing.T, token string, stick bool) *rig {
	t.Helper()
	requireLane(t)

	pin, err := mini404.PinFor(laneModel)
	if err != nil {
		t.Fatalf("phase %q: %v", phaseArtefacts, err)
	}

	resolver := mini404.Resolver{}

	qemu, err := resolver.QEMUBinary()
	if err != nil {
		t.Fatalf("phase %q: %v", phaseArtefacts, err)
	}

	firmware, err := resolver.FirmwareImage(pin.Model)
	if err != nil {
		t.Fatalf("phase %q: %v", phaseArtefacts, err)
	}

	port := reservePort(t)
	srv := startServer(t, port)

	dir := t.TempDir()

	// The guest reaches the host at 10.0.2.2, the slirp gateway QEMU proxies to
	// the host's loopback, and the port is the one reserved above. Both are read
	// by the firmware's Connect client on its first pass, before anything is on
	// the screen. See seededToken for what the token does.
	bank1, bank2, err := mini404.WriteEEPROMBanks(dir, mini404.ConnectSeed{
		Host:    "10.0.2.2",
		Port:    uint16(port),
		TLS:     false,
		Token:   token,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("phase %q: %v", phaseEEPROM, err)
	}

	// The bootstrap boot behind this is minutes, and it is paid once per
	// firmware image on a machine. tools/firmwareprinter asks for the same
	// image the same way, so a developer's first run and this lane's first run
	// fill the same cache entry.
	xflash, err := mini404.XFlash(t.Context(), mini404.XFlashRequest{
		QEMUPath:     qemu,
		Pin:          pin,
		FirmwarePath: firmware,
		Dir:          dir,
		Budget:       bootstrapBudget,
		Logger:       laneLogger(t),
	})
	if err != nil {
		t.Fatalf("%v", err)
	}

	guest := mini404.Config{
		QEMUPath:     qemu,
		Machine:      pin.Machine,
		FirmwarePath: firmware,
		EEPROMBank1:  bank1,
		EEPROMBank2:  bank2,
		XFlashPath:   xflash,
		Dir:          dir,
	}

	// The stick is its own directory rather than the runtime one, because a
	// reboot takes a fresh runtime directory and the stick has to survive it
	// the way a stick left in a printer does.
	var usb string

	if stick {
		usb = t.TempDir()
		guest.USBDir = usb
	}

	printer := launchGuest(t, guest)

	return &rig{server: srv, printer: printer, pin: pin, port: port, usb: usb, guest: guest}
}

// launchGuest boots one emulator, registers its teardown and diagnostics, and
// waits for PrusaLink to answer on the forwarded port.
func launchGuest(t *testing.T, guest mini404.Config) *mini404.Printer {
	t.Helper()

	printer, err := mini404.Launch(t.Context(), guest)
	if err != nil {
		t.Fatalf("phase %q: %v", phaseLaunch, err)
	}

	// Cleanup is last in, first out, so the emulator is registered after the
	// server and comes down before it: a guest still dialling a dead port logs
	// a reconnection storm into the failure it is being read for. The
	// diagnostics are registered last of all, so they run while the guest is
	// still alive and can be asked for a screen.
	t.Cleanup(func() {
		closing, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), closeBudget)
		defer cancel()

		if closeErr := printer.Close(closing); closeErr != nil {
			t.Errorf("close the emulator: %v", closeErr)
		}
	})

	t.Cleanup(func() {
		if !t.Failed() {
			return
		}

		t.Logf("the emulator's output, last %d bytes:\n%s", len(printer.OutputTail()), printer.OutputTail())

		// The screen is the fastest read on a firmware that stopped: a wizard
		// page, a redscreen or a blank panel each say something different. It
		// goes beside the emulator's own log in this test's temporary directory,
		// and the tail above is logged inline as well, because that directory is
		// removed when the test ends and the path alone outlives nothing.
		shot := filepath.Join(guest.Dir, "screen.ppm")

		dumping, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), diagnosticBudget)
		defer cancel()

		if dumpErr := printer.QMP().Screendump(dumping, shot); dumpErr != nil {
			t.Logf("no screendump: %v", dumpErr)

			return
		}

		t.Logf("screendump: %s (and the emulator's whole log: %s)", shot, filepath.Join(guest.Dir, "qemu.log"))
	})

	awaitPhase(t, bootBudget, phaseBoot, func(ctx context.Context) error {
		return mini404.WaitPrusaLink(ctx, printer.HTTPPort())
	})

	return printer
}

// stopGuest brings the emulator down mid-test, which is the socket dying under
// the server. Close is idempotent, so the teardown registered at boot finds
// nothing left to do.
func stopGuest(t *testing.T, rg *rig) {
	t.Helper()

	closing, cancel := context.WithTimeout(t.Context(), closeBudget)
	defer cancel()

	if err := rg.printer.Close(closing); err != nil {
		t.Fatalf("stop the guest: %v", err)
	}
}

// rebootGuest boots a fresh guest from the images the stopped one used, which
// is what a power cycle leaves a real printer with: the EEPROM still carries
// the seeded host, port and token, and the external flash carries whatever the
// firmware wrote. Only the runtime directory is new, so the two runs' sockets
// and logs never collide.
func rebootGuest(t *testing.T, rg *rig) *mini404.Printer {
	t.Helper()

	guest := rg.guest
	guest.Dir = t.TempDir()

	rg.printer = launchGuest(t, guest)

	return rg.printer
}

// reservePort takes a loopback port by binding and closing it.
//
// The gap between the close and the server's own bind is unavoidable: there is
// no way to hand a listening socket to a child process, and the port has to be
// known before the EEPROM image is written. A lost race is a server that
// refuses to start, which is a clear failure rather than a silent one.
func reservePort(t *testing.T) int {
	t.Helper()

	var binder net.ListenConfig

	listener, err := binder.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a loopback port: %v", err)
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		t.Fatalf("a tcp4 listener reported a %T address", listener.Addr())
	}

	if err = listener.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}

	return addr.Port
}

// awaitPhase polls until the probe passes, and fails naming the phase. Every
// wait in this lane goes through it, so a timeout says which phase died rather
// than which line of Go was executing.
func awaitPhase(t *testing.T, budget time.Duration, phase string, probe func(context.Context) error) {
	t.Helper()

	if err := waitForPhase(t, budget, phase, probe); err != nil {
		t.Fatalf("%v", err)
	}
}

// waitForPhase is awaitPhase as a verdict rather than as a failure, for a
// scenario written as a probe: a blocked entry's probe reads its own waits as
// evidence that the entry still holds, and a t.Fatalf inside one would fail the
// gate that is only asking.
func waitForPhase(
	t *testing.T, budget time.Duration, phase string, probe func(context.Context) error,
) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()

	if err := mini404.WaitFunc(ctx, phase, probe); err != nil {
		return fmt.Errorf("%w (budget %s)", err, budget)
	}

	return nil
}

// laneLogger routes the emulator package's own progress lines into the test
// log. The external flash bootstrap is a quarter of an hour on a cold cache,
// and it says so through this rather than into a log nobody is reading.
func laneLogger(t *testing.T) *slog.Logger {
	t.Helper()

	return slog.New(slog.NewTextHandler(testWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// testWriter is one log line to t.Log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(line []byte) (int, error) {
	w.t.Logf("%s", bytes.TrimRight(line, "\n"))

	return len(line), nil
}

// transferRow is one file this server moved onto the printer, with the fields
// this lane reads. Spelled out for the reason printerView is: this reads the
// wire the way a client outside the process does.
type transferRow struct {
	TransferID string `json:"transfer_id"`
	Path       string `json:"path"`
	BytesTotal int64  `json:"bytes_total"`
	BytesSent  int64  `json:"bytes_sent"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
	// Warning is what the server could not establish about a transfer it
	// nevertheless finished. BUG-014's fix puts the unconfirmed readback here.
	Warning string `json:"warning"`
}

// faultDoc is a refusal as the API renders one: a problem document carrying the
// catalogue code and the parameters behind it.
type faultDoc struct {
	Code   string            `json:"code"`
	Detail string            `json:"detail"`
	Params map[string]string `json:"params"`
}

// storageName is the storage resource the configuration declares, which is
// where a scenario's source file is read from.
const storageName = "local"

// printerStorage is the first path segment of every file on the machine's own
// stick, which is what the send route's path is prefixed with.
const printerStorage = "usb"

// sendFile relays one file from a storage onto the printer and returns what the
// server told the caller: the transfer as it ended, or the fault it refused
// with. Both are returned rather than one of them, because which of the two
// arrives is the assertion in the collision scenario.
func (s *server) sendFile(ctx context.Context, id, source, path string) (int, transferRow, faultDoc, error) {
	body, err := json.Marshal(map[string]string{
		"storage": storageName,
		"source":  source,
		"path":    path,
	})
	if err != nil {
		return 0, transferRow{}, faultDoc{}, fmt.Errorf("encode the send: %w", err)
	}

	status, answer, err := s.post(ctx, devicesPath+"/"+id+"/files", body)
	if err != nil {
		return 0, transferRow{}, faultDoc{}, err
	}

	if status != http.StatusOK {
		var fault faultDoc
		if err = json.Unmarshal(answer, &fault); err != nil {
			return status, transferRow{}, faultDoc{}, fmt.Errorf("parse the refusal %s: %w", answer, err)
		}

		return status, transferRow{}, fault, nil
	}

	var sent struct {
		Path     string      `json:"path"`
		Transfer transferRow `json:"transfer"`
	}

	if err = json.Unmarshal(answer, &sent); err != nil {
		return status, transferRow{}, faultDoc{}, fmt.Errorf("parse the send's answer %s: %w", answer, err)
	}

	return status, sent.Transfer, faultDoc{}, nil
}

// deviceFile is one entry of the printer's own file tree, with the fields this
// lane reads.
type deviceFile struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Folder      bool   `json:"folder"`
	Size        int64  `json:"size"`
	ReadOnly    bool   `json:"read_only"`
}

// deviceFiles is the root of the printer's own storage, as the machine reports
// it. It is the readback a caller can take, and in the collision scenario it is
// the evidence the assertion rests on.
//
// The directory is printerStorage and is not a parameter: it is the only
// storage a machine under this harness has.
func (s *server) deviceFiles(ctx context.Context, id string) ([]deviceFile, error) {
	path := devicesPath + "/" + id + "/files?path=" + url.QueryEscape(printerStorage)

	// The route answers at once, with reading set while the printer is still
	// listing, so the read is waited out within the budget one listing had.
	deadline := time.Now().Add(listBudget)

	for {
		status, body, err := s.getWithin(ctx, path, listBudget)
		if err != nil {
			return nil, err
		}

		if status != http.StatusOK {
			return nil, fmt.Errorf("listing %s answered %d: %s", printerStorage, status, body)
		}

		var listing struct {
			Items   []deviceFile    `json:"items"`
			Reading bool            `json:"reading"`
			Fault   json.RawMessage `json:"fault"`
		}

		if err = json.Unmarshal(body, &listing); err != nil {
			return nil, fmt.Errorf("parse the listing of %s: %w", printerStorage, err)
		}

		if listing.Fault != nil {
			return nil, fmt.Errorf("the printer did not list %s: %s", printerStorage, body)
		}

		if !listing.Reading {
			return listing.Items, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the printer was still reading %s after %s", printerStorage, listBudget)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for the listing of %s: %w", printerStorage, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// reading is one string out of the state tree, absent for a path the machine
// has said nothing about. Every path this lane watches over a print carries a
// JSON string, and a number is read through numericReading instead.
func reading(view printerView, path string) (string, bool) {
	raw, present := view.State[path]
	if !present {
		return "", false
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}

	return value, true
}

// numericReading is one number out of the state tree, and zero for a reading
// the tree does not carry or does not hold a number in. Absence and zero are
// the same answer to the only callers there are, which sum readings.
func numericReading(view printerView, path string) float64 {
	raw, present := view.State[path]
	if !present {
		return 0
	}

	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0
	}

	return value
}

// stringsReading is one list of strings out of the state tree, which is what
// the dialog's buttons arrive as.
func stringsReading(view printerView, path string) ([]string, bool) {
	raw, present := view.State[path]
	if !present {
		return nil, false
	}

	var value []string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}

	return value, true
}

// deleteFile asks the printer to remove one entry of its own storage, and
// returns what it answered. It is a diagnostic in the collision scenario: the
// live run found the surviving file undeletable ("File is busy"), and whether a
// real FatFs under the emulator does the same is the evidence the bug wants.
func (s *server) deleteFile(ctx context.Context, id, path string) (int, faultDoc, error) {
	target := s.baseURL + devicesPath + "/" + id + "/files?path=" + url.QueryEscape(path)

	called, cancel := context.WithTimeout(ctx, commandBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(called, http.MethodDelete, target, nil)
	if err != nil {
		return 0, faultDoc{}, fmt.Errorf("build DELETE %s: %w", path, err)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, faultDoc{}, fmt.Errorf("DELETE %s: %w", path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, faultDoc{}, fmt.Errorf("read DELETE %s: %w", path, err)
	}

	var fault faultDoc
	if resp.StatusCode >= http.StatusBadRequest {
		if err = json.Unmarshal(body, &fault); err != nil {
			return resp.StatusCode, faultDoc{}, fmt.Errorf("parse the refusal %s: %w", body, err)
		}
	}

	return resp.StatusCode, fault, nil
}
