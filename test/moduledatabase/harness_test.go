package moduledatabase

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
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

	// The two drivers the cases read the module's database with.
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// enabledEnv switches the run on. Without it every case skips with the
// reason, because the run needs Docker and minutes.
const enabledEnv = "PCL_MODULE_DATABASE_E2E"

// postgresImage is the Postgres the CI fixture runs.
const postgresImage = "postgres:18"

// containerPrefix starts the name of every container the run starts, so a
// person finds and removes what a killed run left: docker ps -a --filter
// name=pcl-moddb-e2e-.
const containerPrefix = "pcl-moddb-e2e-"

// The budgets, in wall-clock time.
const (
	buildBudget   = 5 * time.Minute
	startBudget   = 60 * time.Second
	requestBudget = 2 * time.Minute
	settleBudget  = 60 * time.Second
	settleStep    = 100 * time.Millisecond
)

// requireEnabled skips a case unless the run was asked for.
func requireEnabled(t *testing.T) {
	t.Helper()

	if os.Getenv(enabledEnv) != "1" {
		t.Skipf("%s is not 1. Run make module-database-e2e, which needs Docker", enabledEnv)
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("%s is 1 and docker is not on PATH: %v", enabledEnv, err)
	}
}

// repoRoot is two directories up from this package.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// run is one command, with its output on failure.
func run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}

	return string(out), nil
}

// builds holds the server and the module releases, built once per run.
type builds struct {
	dir    string
	server string
	err    error
	once   sync.Once
	mu     sync.Mutex
	module map[string]string
}

//nolint:gochecknoglobals // one build directory per test binary, shared by every case.
var built = &builds{module: map[string]string{}}

// serverBinary builds the server with the Postgres driver.
func serverBinary(t *testing.T) string {
	t.Helper()

	built.once.Do(func() {
		// The builds outlive any one case, so TestMain removes the directory.
		built.dir, built.err = os.MkdirTemp("", "pcl-moddb-e2e-build-*") //nolint:usetesting // shared by every case.
		if built.err != nil {
			return
		}

		built.server = filepath.Join(built.dir, "prusa-connect"+exe())

		ctx, cancel := context.WithTimeout(context.Background(), buildBudget)
		defer cancel()

		_, built.err = run(ctx, repoRoot(), "go", "build", "-tags", "postgres", "-o", built.server,
			"./cmd/prusa-connect")
	})

	if built.err != nil {
		t.Fatalf("build the server: %v", built.err)
	}

	return built.server
}

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}

// moduleName is the example module's own name, which the manifest, the
// binary and the instance carry.
const moduleName = "example-database"

// modulePath is the module path the cases install it under.
const modulePath = "github.com/acme/notes"

// release is one release of the example module: its version, the files the
// build adds to the package, whether its manifest asks for a database, and a
// variant that tells two builds of one version apart.
type release struct {
	version string
	overlay map[string]string
	noAsk   bool
	variant string
}

// releaseDir builds the release once and lays it out as a directory an
// install reads: the manifest and this platform's binary.
func releaseDir(t *testing.T, one release) string {
	t.Helper()

	serverBinary(t)

	built.mu.Lock()
	defer built.mu.Unlock()

	key := one.version + one.variant
	if dir, done := built.module[key]; done {
		return dir
	}

	dir := filepath.Join(built.dir, "release-"+key)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("make the release directory: %v", err)
	}

	args := []string{"build", "-ldflags", "-X main.version=" + one.version}

	if len(one.overlay) > 0 {
		args = append(args, "-overlay", writeOverlay(t, dir, one.overlay))
	}

	binary := filepath.Join(dir, moduleName+"_"+runtime.GOOS+"_"+runtime.GOARCH+exe())

	ctx, cancel := context.WithTimeout(t.Context(), buildBudget)
	defer cancel()

	if _, err := run(ctx, repoRoot(), "go", append(args, "-o", binary, "./examples/database-module")...); err != nil {
		t.Fatalf("build the module %s: %v", key, err)
	}

	manifest := []string{
		"name: " + moduleName,
		"kind: sink",
		"license: MIT",
		"version: " + one.version,
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
	}
	if !one.noAsk {
		manifest = append(manifest, "needs:", "  database: true")
	}

	if err := os.WriteFile(filepath.Join(dir, "module.yaml"), []byte(strings.Join(manifest, "\n")+"\n"),
		0o600); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}

	built.module[key] = dir

	return dir
}

// writeOverlay writes the files a release adds and the file go build -overlay
// reads.
func writeOverlay(t *testing.T, dir string, files map[string]string) string {
	t.Helper()

	replace := map[string]string{}
	pkg := filepath.Join(repoRoot(), "examples", "database-module")

	for name, content := range files {
		source := filepath.Join(dir, "overlay", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
			t.Fatalf("make the overlay directory: %v", err)
		}

		if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}

		replace[filepath.Join(pkg, filepath.FromSlash(name))] = source
	}

	encoded, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatalf("encode the overlay: %v", err)
	}

	path := filepath.Join(dir, "overlay.json")
	if err = os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write the overlay: %v", err)
	}

	return path
}

// postgresServer is one Postgres container.
type postgresServer struct {
	name     string
	host     string
	password string
}

// startPostgres starts a container and answers it once it takes a
// connection. The caller removes it with remove.
func startPostgres(role string) (postgresServer, error) {
	server := postgresServer{
		name:     containerPrefix + strings.ToLower(rand.Text())[:6] + "-" + role,
		password: strings.ToLower(rand.Text()),
	}

	ctx := context.Background()

	if _, err := run(ctx, "", "docker", "run", "-d", "--name", server.name,
		"-e", "POSTGRES_PASSWORD="+server.password, "-p", "127.0.0.1::5432", postgresImage); err != nil {
		return server, err
	}

	deadline := time.Now().Add(startBudget)

	for {
		out, err := run(ctx, "", "docker", "port", server.name, "5432/tcp")
		if err == nil && strings.TrimSpace(out) != "" {
			server.host = strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
		}

		if server.host != "" && server.ping() == nil {
			return server, nil
		}

		if time.Now().After(deadline) {
			return server, fmt.Errorf("%s did not take a connection in %s", server.name, startBudget)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// remove removes the container and its volume.
func (p postgresServer) remove() {
	_, _ = run(context.Background(), "", "docker", "rm", "-f", "-v", p.name)
}

// ping asks the server for one row as the superuser.
func (p postgresServer) ping() error {
	db, err := sql.Open("postgres", p.admin())
	if err != nil {
		return err
	}

	defer func() { _ = db.Close() }()

	return db.PingContext(context.Background())
}

// The two Postgres servers every case shares. The two-server layouts put the
// core on the first and the module on the second, and the one-server layouts
// put both on the first.
//
//nolint:gochecknoglobals // started once by TestMain, read by every case.
var firstServer, secondServer postgresServer

// TestMain starts the two Postgres servers for a run that was asked for, and
// removes them, with their volumes, when it ends.
func TestMain(m *testing.M) {
	if os.Getenv(enabledEnv) != "1" {
		os.Exit(m.Run())
	}

	var err error

	firstServer, err = startPostgres("first")
	if err == nil {
		secondServer, err = startPostgres("second")
	}

	code := 1
	if err == nil {
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, "start the Postgres servers:", err)
	}

	firstServer.remove()
	secondServer.remove()

	if built.dir != "" {
		_ = os.RemoveAll(built.dir)
	}

	os.Exit(code)
}

// dsn is a connection string on this server.
func (p postgresServer) dsn(user, password, database string) string {
	address := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: p.host, Path: "/" + database}
	address.RawQuery = "sslmode=disable"

	return address.String()
}

// admin is the superuser's connection, which only the case uses.
func (p postgresServer) admin() string { return p.dsn("postgres", p.password, "postgres") }

// exec runs statements as the superuser.
func (p postgresServer) exec(statements ...string) error {
	db, err := sql.Open("postgres", p.admin())
	if err != nil {
		return err
	}

	defer func() { _ = db.Close() }()

	for _, statement := range statements {
		if _, err = db.ExecContext(context.Background(), statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}

	return nil
}

// mustExec runs statements as the superuser and fails the case on an error.
func (p postgresServer) mustExec(t *testing.T, statements ...string) {
	t.Helper()

	if err := p.exec(statements...); err != nil {
		t.Fatalf("on %s: %v", p.name, err)
	}
}

// coreDatabase makes a role and a database for a core: the role may make
// roles and databases and is no superuser. It answers the core's connection.
func (p postgresServer) coreDatabase(t *testing.T, name string) string {
	t.Helper()

	password := strings.ToLower(rand.Text())
	p.mustExec(t,
		"CREATE ROLE "+name+" LOGIN CREATEDB CREATEROLE NOSUPERUSER PASSWORD '"+password+"'",
		"CREATE DATABASE "+name+" OWNER "+name,
		"REVOKE ALL ON DATABASE "+name+" FROM PUBLIC")

	return p.dsn(name, password, name)
}

// moduleAdmin makes a role that may make roles and databases and log in to
// the postgres database, which module_admin_dsn names.
func (p postgresServer) moduleAdmin(t *testing.T, name string) string {
	t.Helper()

	password := strings.ToLower(rand.Text())
	p.mustExec(t, "CREATE ROLE "+name+" LOGIN CREATEDB CREATEROLE NOSUPERUSER PASSWORD '"+password+"'",
		"GRANT CONNECT ON DATABASE postgres TO "+name)

	return p.dsn(name, password, "postgres")
}

// operatorDatabase makes a database and a role that may only log in, as an
// operator makes one for the mode postgres, and answers its connection.
func (p postgresServer) operatorDatabase(t *testing.T, name string) string {
	t.Helper()

	password := strings.ToLower(rand.Text())
	p.mustExec(t,
		"CREATE ROLE "+name+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '"+password+"'",
		"CREATE DATABASE "+name+" OWNER "+name,
		"REVOKE ALL ON DATABASE "+name+" FROM PUBLIC")

	return p.dsn(name, password, name)
}

// exists asks whether a role or a database of a name is on the server.
func (p postgresServer) exists(t *testing.T, catalogue, column, name string) bool {
	t.Helper()

	db, err := sql.Open("postgres", p.admin())
	if err != nil {
		t.Fatalf("open %s: %v", p.name, err)
	}

	defer func() { _ = db.Close() }()

	var count int
	if err = db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+catalogue+" WHERE "+column+" = $1", name).Scan(&count); err != nil {
		t.Fatalf("ask %s: %v", p.name, err)
	}

	return count == 1
}

// server is one running binary over one directory.
type server struct {
	dir     string
	config  string
	baseURL string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	log     *lockedLog
	stopped chan error
}

// lockedLog is the binary's output, written by the reader and read by the
// case.
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

var (
	servingLine   = regexp.MustCompile(`msg=serving api=(\S+)`)
	setupCodeLine = regexp.MustCompile(`setup_code=([0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4})`)
)

// deployment is one server's directory and the persistence block it starts
// with.
type deployment struct {
	dir         string
	persistence string
}

// newDeployment is a directory for one server. persistence is the
// persistence block, and empty runs the core on SQLite.
func newDeployment(t *testing.T, persistence string) deployment {
	t.Helper()

	return deployment{dir: t.TempDir(), persistence: persistence}
}

// postgresCore is the persistence block of a core on Postgres.
func postgresCore(dsn, moduleAdmin string) string {
	lines := []string{"persistence:", "  type: postgres", "  postgres:", "    dsn: '" + dsn + "'"}
	if moduleAdmin != "" {
		lines = append(lines, "    module_admin_dsn: '"+moduleAdmin+"'")
	}

	return strings.Join(lines, "\n")
}

// start writes the configuration, starts the binary and records the first
// run as open, so every request is served as the principal operator.
func (d deployment) start(t *testing.T) *server {
	t.Helper()

	config := filepath.Join(d.dir, "config.yaml")
	body := strings.Join([]string{
		"core:",
		"  log:",
		"    level: info",
		"    format: text",
		"  overlay: '" + filepath.Join(d.dir, "resources.json") + "'",
		"  modules: '" + filepath.Join(d.dir, "modules") + "'",
		"  principal: operator",
		"  site: moddb-e2e",
		"  discovery:",
		"    enabled: false",
		"  marketplace:",
		"    enabled: false",
		"  api:",
		`    addr: "127.0.0.1:0"`,
		d.persistence,
		"",
	}, "\n")

	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, serverBinary(t), "core", "--config", config)
	cmd.Dir = d.dir
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 20 * time.Second

	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start the server: %v", err)
	}

	srv := &server{dir: d.dir, config: config, cmd: cmd, cancel: cancel, log: &lockedLog{},
		stopped: make(chan error, 1)}
	addresses := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)

		for scanner.Scan() {
			srv.log.add(scanner.Text())

			if match := servingLine.FindStringSubmatch(scanner.Text()); match != nil {
				select {
				case addresses <- match[1]:
				default:
				}
			}
		}
	}()

	go func() {
		srv.stopped <- cmd.Wait()
		_ = writer.Close()
	}()

	t.Cleanup(func() {
		srv.stop(t)

		if t.Failed() {
			t.Logf("the server's log:\n%s", srv.log.String())
		}
	})

	select {
	case address := <-addresses:
		srv.baseURL = "http://" + address
	case err := <-srv.stopped:
		t.Fatalf("the server exited before it served: %v\n%s", err, srv.log.String())
	case <-time.After(startBudget):
		t.Fatalf("the server did not serve in %s\n%s", startBudget, srv.log.String())
	}

	srv.settleFirstRun(t)

	return srv
}

// stop ends the server and waits for it. Calling it twice is harmless.
func (s *server) stop(t *testing.T) {
	t.Helper()

	if s.cancel == nil {
		return
	}

	s.cancel()
	s.cancel = nil

	select {
	case <-s.stopped:
	case <-time.After(startBudget):
		t.Errorf("the server did not stop in %s", startBudget)
	}
}

// settleFirstRun records the open first run with the code from the log.
func (s *server) settleFirstRun(t *testing.T) {
	t.Helper()

	state := s.call(t, http.MethodGet, "/api/v1/setup", "")
	if state.json(t)["state"] != "pending" {
		return
	}

	var match []string

	for deadline := time.Now().Add(startBudget); match == nil; {
		match = setupCodeLine.FindStringSubmatch(s.log.String())
		if match == nil && time.Now().After(deadline) {
			t.Fatalf("the server printed no setup code:\n%s", s.log.String())
		}

		time.Sleep(settleStep)
	}

	recorded := s.call(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+match[1]+`"}`)
	recorded.want(t, http.StatusOK)
}

// answer is one response, read whole.
type answer struct {
	status int
	body   []byte
}

// call makes one request.
func (s *server) call(t *testing.T, method, path, body string) answer {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), requestBudget)
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}

	return answer{status: resp.StatusCode, body: payload}
}

// want fails the case unless the answer has the status.
func (a answer) want(t *testing.T, status int) answer {
	t.Helper()

	if a.status != status {
		t.Fatalf("answered %d, want %d: %s", a.status, status, a.body)
	}

	return a
}

// wantCode fails the case unless the answer is a refusal with the status and
// the code.
func (a answer) wantCode(t *testing.T, status int, code string) answer {
	t.Helper()

	a.want(t, status)

	if !strings.Contains(string(a.body), code) {
		t.Fatalf("the refusal is not %s: %s", code, a.body)
	}

	return a
}

// json decodes the answer as an object.
func (a answer) json(t *testing.T) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(a.body, &out); err != nil {
		t.Fatalf("decode %s: %v", a.body, err)
	}

	return out
}

// eventually retries until the condition holds.
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()

	for deadline := time.Now().Add(settleBudget); !condition(); time.Sleep(settleStep) {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in %s", what, settleBudget)
		}
	}
}
