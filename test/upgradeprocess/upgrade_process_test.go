//go:build !windows

package upgradeprocess

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

const (
	oldVersion       = "1.0.0"
	goodVersion      = "1.1.0"
	badVersion       = "1.2.0"
	buildTimeout     = 4 * time.Minute
	startTimeout     = 30 * time.Second
	operationTimeout = 90 * time.Second
)

var (
	servingLine   = regexp.MustCompile(`msg=serving api=(\S+)`)
	setupCodeLine = regexp.MustCompile(`setup_code=([0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4})`)
	builtFixtures = sync.OnceValues(buildAllFixtures)
)

type fixtures struct {
	oldBinary  string
	goodBinary string
	badBinary  string
	module     string
	signer     string
	seed       []byte
	keyID      string
	key        string
}

func buildAllFixtures() (fixtures, error) {
	root, err := os.MkdirTemp("", "pcl-upgrade-process-*")
	if err != nil {
		return fixtures{}, err
	}

	oldBinary := filepath.Join(root, "core-"+oldVersion+exe())
	goodBinary := filepath.Join(root, "core-"+goodVersion+exe())
	if err = buildCore(repoRoot(), oldBinary, oldVersion); err != nil {
		return fixtures{}, err
	}
	if err = buildCore(repoRoot(), goodBinary, goodVersion); err != nil {
		return fixtures{}, err
	}

	metadata, err := binaryMetadata(goodBinary)
	if err != nil {
		return fixtures{}, err
	}

	badMetadata := metadata
	badMetadata.Version = badVersion
	badBinary := filepath.Join(root, "core-"+badVersion+"-fails"+exe())
	if err = buildFailedCandidate(badBinary, badMetadata); err != nil {
		return fixtures{}, err
	}

	module, err := buildDatabaseModule(root)
	if err != nil {
		return fixtures{}, err
	}

	signer := filepath.Join(root, "sign-upgrade-checksums"+exe())
	if err = buildTool(signer, "./cmd/sign-upgrade-checksums"); err != nil {
		return fixtures{}, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fixtures{}, err
	}

	return fixtures{
		oldBinary: oldBinary, goodBinary: goodBinary, badBinary: badBinary,
		module: module, signer: signer, seed: private.Seed(),
		keyID: upgrade.KeyID(public), key: base64.StdEncoding.EncodeToString(public),
	}, nil
}

func buildTool(path, pkg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "build", "-o", path, pkg)
	command.Dir = repoRoot()
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s: %w: %s", pkg, err, output)
	}

	return nil
}

// signRelease writes SHA256SUMS and its signature for one binary with the
// commands the release workflow runs: a plain SHA-256 per file, then
// cmd/sign-upgrade-checksums.
func signRelease(t *testing.T, fixture fixtures, binary string) ([]byte, []byte) {
	t.Helper()

	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	line := rawSum(t, binary) + "  " + filepath.Base(binary) + "\n"
	if err := os.WriteFile(sums, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	signature := filepath.Join(dir, "SHA256SUMS.sig")
	command := exec.CommandContext(t.Context(), fixture.signer, sums, signature)
	command.Env = append(os.Environ(),
		"UPGRADE_SIGNING_KEY="+base64.StdEncoding.EncodeToString(fixture.seed),
		"UPGRADE_PUBLIC_KEYS="+fixture.key)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sign SHA256SUMS: %v: %s", err, output)
	}
	signed, err := os.ReadFile(signature)
	if err != nil {
		t.Fatal(err)
	}

	return []byte(line), signed
}

// buildCore builds the core of the source tree at dir, with the toolchain and
// module graph that tree declares.
func buildCore(dir, path, stamp string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	ldflags := "-s -w -X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=" + stamp
	command := exec.CommandContext(ctx, "go", "build", "-ldflags", ldflags, "-o", path, "./cmd/prusa-connect")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build core %s: %w: %s", stamp, err, output)
	}

	return signOnDarwin(path)
}

// buildReleaseCore builds the core of a past release from its git tag. The
// tree is exported from git, so the build reads nothing of this checkout.
func buildReleaseCore(t *testing.T, ref, path, stamp string) {
	t.Helper()

	dir := t.TempDir()
	archive := filepath.Join(dir, "source.tar")
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{
		{"git", "-C", repoRoot(), "archive", "--output", archive, ref},
		{"tar", "-x", "-f", archive, "-C", source},
	} {
		if output, err := exec.CommandContext(t.Context(), step[0], step[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("export %s: %v: %s", ref, err, output)
		}
	}
	if err := buildCore(source, path, stamp); err != nil {
		t.Fatal(err)
	}
}

func buildFailedCandidate(path string, metadata upgrade.BinaryMetadata) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	source := `package main

import (
	"encoding/json"
	"os"
)

const metadata = ` + strconv.Quote(string(encoded)) + `

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString(metadata + "\n")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "upgrade" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"checked_at": "2026-10-27T00:00:00Z",
			"duration_ms": 1,
		})
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "core" {
		_ = os.WriteFile(os.Getenv("PCL_UPGRADE_E2E_DAMAGE"), []byte("candidate changed live data"), 0o600)
		_ = os.WriteFile(os.Getenv("PCL_UPGRADE_E2E_MARKER"), []byte("candidate reached trial"), 0o600)
		os.Exit(23)
	}
	os.Exit(2)
}
`

	sourceDir, err := os.MkdirTemp("", "pcl-failed-candidate-*")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(sourceDir, "main.go"), []byte(source), 0o600); err != nil {
		return err
	}
	moduleFile := []byte("module failedcandidate\n\ngo 1.26.11\n")
	if err = os.WriteFile(filepath.Join(sourceDir, "go.mod"), moduleFile, 0o600); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "build", "-o", path, ".")
	command.Dir = sourceDir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, buildErr := command.CombinedOutput(); buildErr != nil {
		return fmt.Errorf("build failed candidate: %w: %s", buildErr, output)
	}

	return signOnDarwin(path)
}

func buildDatabaseModule(root string) (string, error) {
	release := filepath.Join(root, "database-module")
	if err := os.MkdirAll(release, 0o750); err != nil {
		return "", err
	}

	binary := filepath.Join(release, "example-database_"+runtime.GOOS+"_"+runtime.GOARCH+exe())
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./examples/database-module")
	command.Dir = repoRoot()
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build database module: %w: %s", err, output)
	}
	if err := signOnDarwin(binary); err != nil {
		return "", err
	}

	manifest := strings.Join([]string{
		"name: example-database",
		"kind: sink",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"needs:",
		"  database: true",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(release, "module.yaml"), []byte(manifest), 0o600); err != nil {
		return "", err
	}

	return release, nil
}

func signOnDarwin(path string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	if output, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput(); err != nil {
		return fmt.Errorf("ad-hoc sign %s: %w: %s", path, err, output)
	}

	return nil
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func exe() string {
	return ""
}

type lockedLog struct {
	mu   sync.Mutex
	body bytes.Buffer
}

func (l *lockedLog) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.body.WriteString(line + "\n")
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.body.String()
}

type process struct {
	dir       string
	config    string
	stable    string
	baseURL   string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	log       *lockedLog
	addresses chan string
	stopped   chan error
	waited    bool
}

func startProcess(t *testing.T, dir, source string) *process {
	t.Helper()

	stable := filepath.Join(dir, "prusa-connect"+exe())
	copyExecutable(t, source, stable)

	config := writeConfig(t, dir)
	damage := filepath.Join(dir, "module-data", "example-database", "database.sqlite")
	marker := filepath.Join(dir, "failed-candidate-ran")

	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, stable, "core", "--config", config)
	command.Dir = dir
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = 10 * time.Second
	command.Env = append(os.Environ(),
		"PCL_UPGRADE_E2E_DAMAGE="+damage,
		"PCL_UPGRADE_E2E_MARKER="+marker,
	)

	reader, writer := io.Pipe()
	command.Stdout, command.Stderr = writer, writer
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start core: %v", err)
	}

	proc := &process{
		dir: dir, config: config, stable: stable, cmd: command, cancel: cancel,
		log: &lockedLog{}, addresses: make(chan string, 16), stopped: make(chan error, 1),
	}

	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			proc.log.add(line)
			if match := servingLine.FindStringSubmatch(line); match != nil {
				select {
				case proc.addresses <- "http://" + match[1]:
				default:
				}
			}
		}
	}()
	go func() {
		proc.stopped <- command.Wait()
		_ = writer.Close()
	}()

	select {
	case proc.baseURL = <-proc.addresses:
	case err := <-proc.stopped:
		proc.waited = true
		t.Fatalf("core exited before serving: %v\n%s", err, proc.log.String())
	case <-time.After(startTimeout):
		t.Fatalf("core did not serve within %s\n%s", startTimeout, proc.log.String())
	}

	proc.settleFirstRun(t)
	t.Cleanup(func() {
		proc.stop(t)
		if t.Failed() {
			t.Logf("core log:\n%s", proc.log.String())
		}
	})

	return proc
}

func writeConfig(t *testing.T, dir string) string {
	t.Helper()

	fixture, err := builtFixtures()
	if err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(dir, "config.yaml")
	address := freeAddress(t)
	body := strings.Join([]string{
		"core:",
		"  log:",
		"    level: info",
		"    format: text",
		"  overlay: " + yamlPath(filepath.Join(dir, "resources.json")),
		"  modules: " + yamlPath(filepath.Join(dir, "modules")),
		"  authz:",
		"    database: " + yamlPath(filepath.Join(dir, "host.db")),
		"  principal: operator",
		"  site: upgrade-process-e2e",
		"  discovery:",
		"    enabled: false",
		"  upgrade:",
		"    enabled: true",
		"    trusted_keys:",
		"      - id: " + fixture.keyID,
		"        key: " + strconv.Quote(fixture.key),
		"    trial_timeout: 15s",
		"  api:",
		"    addr: " + strconv.Quote(address),
		"    request_timeout: 2m",
		"    stream_timeout: 2m",
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
	if err = os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return config
}

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}

	return address
}

func yamlPath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}

func copyExecutable(t *testing.T, source, destination string) {
	t.Helper()

	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
}

func (p *process) settleFirstRun(t *testing.T) {
	t.Helper()

	// The code is printed once every port is open, after the serving line the
	// address was read from, so it is waited for.
	var match []string
	for deadline := time.Now().Add(startTimeout); match == nil && time.Now().Before(deadline); {
		if match = setupCodeLine.FindStringSubmatch(p.log.String()); match == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if match == nil {
		t.Fatalf("core printed no setup code:\n%s", p.log.String())
	}
	answer := p.call(t, http.MethodPost, "/api/v1/setup",
		map[string]any{"mode": "open", "code": match[1]})
	if answer.status != http.StatusOK {
		t.Fatalf("settle first run: %d %s", answer.status, answer.body)
	}
}

func (p *process) stop(t *testing.T) {
	t.Helper()

	if p.waited {
		return
	}
	p.cancel()
	select {
	case <-p.stopped:
		p.waited = true
	case <-time.After(startTimeout):
		t.Errorf("core did not stop within %s", startTimeout)
	}
}

type response struct {
	status int
	body   []byte
}

func (p *process) call(t *testing.T, method, path string, body any) response {
	t.Helper()

	status, payload, err := request(p.baseURL, method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	return response{status: status, body: payload}
}

func request(baseURL, method, path string, body any) (int, []byte, error) {
	var content io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		content = bytes.NewReader(encoded)
	}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, content)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	answer, err := (&http.Client{Timeout: operationTimeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer answer.Body.Close()

	payload, err := io.ReadAll(answer.Body)

	return answer.StatusCode, payload, err
}

func decode[T any](t *testing.T, payload []byte) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}

	return out
}

func binaryMetadata(path string) (upgrade.BinaryMetadata, error) {
	output, err := exec.Command(path, "--version", "--json").Output()
	if err != nil {
		return upgrade.BinaryMetadata{}, err
	}

	var metadata upgrade.BinaryMetadata
	if err = json.Unmarshal(output, &metadata); err != nil {
		return upgrade.BinaryMetadata{}, err
	}

	return metadata, nil
}

func rawSum(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

func (p *process) installDatabaseModule(t *testing.T, release string) string {
	t.Helper()

	answer := p.call(t, http.MethodPost, "/api/v1/modules", map[string]any{
		"module": "github.com/acme/notes", "version": "1.0.0", "from": release,
		"approve": true, "database": map[string]any{"mode": "sqlite"},
	})
	if answer.status != http.StatusCreated {
		t.Fatalf("install database module: %d %s", answer.status, answer.body)
	}
	installed := decode[struct {
		Namespace     string `json:"namespace"`
		State         string `json:"state"`
		SchemaVersion int    `json:"schema_version"`
	}](t, answer.body)
	if installed.State != "ready" || installed.SchemaVersion == 0 {
		t.Fatalf("database module is not ready: %s", answer.body)
	}

	return installed.Namespace
}

func (p *process) addNote(t *testing.T, namespace, text string) {
	t.Helper()

	answer := p.call(t, http.MethodPost, "/api/v1/modules/"+namespace+"/notes", map[string]string{"text": text})
	if answer.status != http.StatusCreated {
		t.Fatalf("add module note: %d %s", answer.status, answer.body)
	}
}

func (p *process) requireNote(t *testing.T, namespace, text string) {
	t.Helper()

	deadline := time.Now().Add(startTimeout)
	var last response
	for time.Now().Before(deadline) {
		last = p.call(t, http.MethodGet, "/api/v1/modules/"+namespace+"/notes", nil)
		if last.status == http.StatusOK {
			var notes struct {
				Notes []struct {
					Text string `json:"text"`
				} `json:"notes"`
			}
			if err := json.Unmarshal(last.body, &notes); err == nil &&
				len(notes.Notes) == 1 && notes.Notes[0].Text == text {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("module note %q did not return after restart: %d %s", text, last.status, last.body)
}

// stageAndPrepare stages and prepares one candidate on a server with the
// given number of installed modules, each optional and compatible.
func (p *process) stageAndPrepare(t *testing.T, candidate, candidateVersion string, modules int) {
	t.Helper()

	content, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(candidate)
	fixture, err := builtFixtures()
	if err != nil {
		t.Fatal(err)
	}
	sums, signature := signRelease(t, fixture, candidate)

	staged := p.call(t, http.MethodPost, "/api/v1/server/upgrade/staged", map[string]any{
		"version": candidateVersion, "name": name, "binary": content,
		"sha256sums": sums, "signature": signature,
	})
	if staged.status != http.StatusCreated {
		t.Fatalf("stage candidate: %d %s", staged.status, staged.body)
	}
	verified := decode[upgrade.Journal](t, staged.body)
	if verified.State != upgrade.StateVerified || verified.Verified == nil {
		t.Fatalf("stage did not verify candidate: %s", staged.body)
	}

	prepared := p.call(t, http.MethodPost, "/api/v1/server/upgrade", nil)
	if prepared.status != http.StatusOK {
		t.Fatalf("prepare candidate: %d %s", prepared.status, prepared.body)
	}
	rehearsed := decode[upgrade.Journal](t, prepared.body)
	if rehearsed.State != upgrade.StateRehearsed || rehearsed.Rehearsal == nil {
		t.Fatalf("prepare did not rehearse candidate: %s", prepared.body)
	}
	if rehearsed.Preflight == nil || len(rehearsed.Preflight.ModuleImpacts) != modules {
		t.Fatalf("preflight did not inspect the installed modules: %s", prepared.body)
	}
	for _, impact := range rehearsed.Preflight.ModuleImpacts {
		if impact.Outcome != upgrade.ImpactOK || impact.NeededToStart {
			t.Fatalf("compatible optional module impact = %+v", impact)
		}
	}
}

func (p *process) applyNow(t *testing.T) {
	t.Helper()

	answer := p.call(t, http.MethodPost, "/api/v1/server/upgrade/apply",
		map[string]any{"schedule": "now"})
	if answer.status != http.StatusAccepted {
		t.Fatalf("apply candidate: %d %s", answer.status, answer.body)
	}
}

func (p *process) awaitCommittedVersion(t *testing.T, wanted string) {
	t.Helper()

	p.awaitJournal(t, wanted, upgrade.StateCommitted)
}

// awaitJournal waits until the process serves one version and its journal
// stands in one state, at whichever address the restarted process bound.
func (p *process) awaitJournal(t *testing.T, wanted string, state upgrade.State) {
	t.Helper()

	deadline := time.Now().Add(operationTimeout)
	addresses := []string{}
	for time.Now().Before(deadline) {
		select {
		case address := <-p.addresses:
			addresses = append(addresses, address)
		default:
		}

		for _, address := range addresses {
			status, payload, err := request(address, http.MethodGet, "/api/v1/server/version", nil)
			if err != nil || status != http.StatusOK {
				continue
			}
			var reported struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(payload, &reported) != nil || reported.Version != wanted {
				continue
			}

			status, payload, err = request(address, http.MethodGet, "/api/v1/server/upgrade", nil)
			if err != nil || status != http.StatusOK {
				continue
			}
			journal := decode[upgrade.Journal](t, payload)
			if journal.State == state {
				p.baseURL = address

				return
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf(
		"version %s never served a journal in state %s; addresses=%v\n%s",
		wanted,
		state,
		addresses,
		p.log.String(),
	)
}

func (p *process) requireVersion(t *testing.T, wanted string) {
	t.Helper()

	answer := p.call(t, http.MethodGet, "/api/v1/server/version", nil)
	if answer.status != http.StatusOK {
		t.Fatalf("read server version: %d %s", answer.status, answer.body)
	}
	reported := decode[struct {
		Version string `json:"version"`
	}](t, answer.body)
	if reported.Version != wanted {
		t.Fatalf("server version = %q, want %q", reported.Version, wanted)
	}
}

func TestAutomaticUpgradeSwapsTrialsAndExecsTheCandidate(t *testing.T) {
	fixture, err := builtFixtures()
	if err != nil {
		t.Fatal(err)
	}

	proc := startProcess(t, t.TempDir(), fixture.oldBinary)
	initialPID := proc.cmd.Process.Pid
	oldDigest := rawSum(t, proc.stable)

	namespace := proc.installDatabaseModule(t, fixture.module)
	proc.addNote(t, namespace, "kept through candidate trial")
	proc.stageAndPrepare(t, fixture.goodBinary, goodVersion, 1)
	proc.applyNow(t)
	proc.awaitCommittedVersion(t, goodVersion)

	if proc.cmd.Process.Pid != initialPID {
		t.Fatalf("final exec changed pid from %d to %d", initialPID, proc.cmd.Process.Pid)
	}
	proc.requireVersion(t, goodVersion)
	proc.requireNote(t, namespace, "kept through candidate trial")
	if rawSum(t, proc.stable) == oldDigest {
		t.Fatal("the stable executable still has the old binary bytes")
	}
	metadata, err := binaryMetadata(proc.stable)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != goodVersion {
		t.Fatalf("stable executable version = %q, want %q", metadata.Version, goodVersion)
	}
}

// TestUpgradeFromThePreviousReleaseCommits upgrades the release tagged in
// PCL_UPGRADE_E2E_FROM to this checkout. The other journeys build both sides
// from this checkout, so they never cross a check that the old release makes
// of the candidate. A server on 0.0.1-alpha.7 refused 0.0.1-alpha.8 that way,
// because its wire contract had a new minor.
func TestUpgradeFromThePreviousReleaseCommits(t *testing.T) {
	// make upgrade-process-e2e always sets the variable, and leaves it empty
	// when it finds no tag. A checkout without its tags would otherwise pass
	// without this journey. A plain go test sets nothing and skips it. The
	// first release of a repository, with no tag before it, sets `first`.
	ref, set := os.LookupEnv("PCL_UPGRADE_E2E_FROM")
	if !set {
		t.Skip("PCL_UPGRADE_E2E_FROM names no release to upgrade from")
	}
	if ref == "" {
		t.Fatal("PCL_UPGRADE_E2E_FROM is empty: no release tag before HEAD was found, so fetch the tags")
	}
	if ref == "first" {
		t.Skip("the first release of this repository: no release before it to upgrade from")
	}
	fixture, err := builtFixtures()
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "core-"+oldVersion+exe())
	buildReleaseCore(t, ref, previous, oldVersion)

	// No module is installed. One built from this checkout may need a newer
	// contract than the old release serves, which no real server meets.
	proc := startProcess(t, t.TempDir(), previous)
	proc.stageAndPrepare(t, fixture.goodBinary, goodVersion, 0)
	proc.applyNow(t)
	proc.awaitCommittedVersion(t, goodVersion)
}

func TestFailedCandidateRestoresTheBinaryAndExtensionDatabase(t *testing.T) {
	fixture, err := builtFixtures()
	if err != nil {
		t.Fatal(err)
	}

	proc := startProcess(t, t.TempDir(), fixture.oldBinary)
	initialPID := proc.cmd.Process.Pid
	oldDigest := rawSum(t, proc.stable)
	namespace := proc.installDatabaseModule(t, fixture.module)
	proc.addNote(t, namespace, "restored from the protected snapshot")

	proc.stageAndPrepare(t, fixture.badBinary, badVersion, 1)
	proc.applyNow(t)

	// The rollback starts the restored executable in the same process, so the
	// server comes back on the old version instead of staying down.
	proc.awaitJournal(t, oldVersion, upgrade.StateRolledBack)
	if proc.cmd.Process.Pid != initialPID {
		t.Fatalf("the restart after rollback changed pid from %d to %d", initialPID, proc.cmd.Process.Pid)
	}

	marker := filepath.Join(proc.dir, "failed-candidate-ran")
	if content, readErr := os.ReadFile(marker); readErr != nil || string(content) != "candidate reached trial" {
		t.Fatalf("failed candidate marker = %q, %v", content, readErr)
	}
	if rawSum(t, proc.stable) != oldDigest {
		t.Fatal("rollback did not restore the original executable bytes")
	}
	metadata, err := binaryMetadata(proc.stable)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != oldVersion {
		t.Fatalf("restored executable version = %q, want %q", metadata.Version, oldVersion)
	}

	proc.requireNote(t, namespace, "restored from the protected snapshot")

	status := proc.call(t, http.MethodGet, "/api/v1/server/upgrade", nil)
	if status.status != http.StatusOK {
		t.Fatalf("read rolled-back journal: %d %s", status.status, status.body)
	}
	journal := decode[upgrade.Journal](t, status.body)
	if journal.BackupID == "" {
		t.Fatalf("rollback journal = %s", status.body)
	}
}
