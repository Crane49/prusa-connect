package main_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// settle is how long the started process is given to answer. A cold start on a
// loaded machine reads the configuration, opens the graph and binds, so the
// budget is generous: what fails here is a wrong server, not a slow one.
const settle = 30 * time.Second

// TestTheBareBinaryRunsTheCore holds the default against a running process
// and not against the source. The binary is started with no subcommand at all
// and asked for a path only the core publishes.
//
// The failure it guards against is quiet. A bare binary that starts the retired
// server answers 200 on /health and 404 on every /api/v1 path, so a supervisor
// reports it up while every client written against this server sees a route
// that is not there.
func TestTheBareBinaryRunsTheCore(t *testing.T) {
	t.Parallel()

	address := freeAddress(t)
	directory := t.TempDir()
	configuration := filepath.Join(directory, "config.yaml")

	body := "core:\n  log:\n    level: error\n  api:\n    addr: \"" + address + "\"\n"
	if err := os.WriteFile(configuration, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	// No subcommand. That is the whole point of this case, so the argument list
	// carries the configuration flag and nothing else.
	start(t, built(t), "-config", configuration)

	waitForStatus(t, "http://"+address+"/api/v1/health", http.StatusOK)

	// The other half of the same claim. /health is the retired server's, and a
	// core that answers it would mean both are somehow up.
	waitForStatus(t, "http://"+address+"/health", http.StatusNotFound)

	// The same route names the build, so an operator can tell which release
	// answers. The binary was stamped the way a release is.
	if reported := healthVersion(t, "http://"+address+"/api/v1/health"); reported != testStamp {
		t.Errorf("/api/v1/health reports version %q, and the build was stamped %q", reported, testStamp)
	}
}

// TestTheVersionFlagPrintsTheStamp covers `--version`, the first thing a
// person runs on a downloaded binary. A release is stamped by the linker, and
// the flag has to print that stamp and stop without starting a server.
func TestTheVersionFlagPrintsTheStamp(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), built(t), "--version")

	said, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("`--version` failed: %v: %s", err, said)
	}

	if want := "prusa-connect " + testStamp + "\n"; string(said) != want {
		t.Errorf("`--version` printed %q, and the build was stamped %q", said, testStamp)
	}
}

func TestTheVersionFlagPrintsMachineReadableBuildFacts(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), built(t), "--version", "--json")

	said, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("`--version --json` failed: %v: %s", err, said)
	}

	var report struct {
		Version           string   `json:"version"`
		BuildProfile      string   `json:"build_profile"`
		GOOS              string   `json:"goos"`
		GOARCH            string   `json:"goarch"`
		Contract          string   `json:"contract"`
		Capabilities      []string `json:"capabilities"`
		HostSchemaVersion int      `json:"host_schema_version"`
		HostSchemaCompat  int      `json:"host_schema_compat"`
	}

	if err = json.Unmarshal(said, &report); err != nil {
		t.Fatalf("decode version report %q: %v", said, err)
	}

	if report.Version != testStamp {
		t.Errorf("version is %q, expected %q", report.Version, testStamp)
	}

	if report.BuildProfile != "normal" {
		t.Errorf("build profile is %q, expected normal", report.BuildProfile)
	}

	if report.GOOS != runtime.GOOS || report.GOARCH != runtime.GOARCH {
		t.Errorf("platform is %s/%s, expected %s/%s", report.GOOS, report.GOARCH, runtime.GOOS, runtime.GOARCH)
	}

	if report.Contract == "" || len(report.Capabilities) == 0 ||
		report.HostSchemaVersion < 1 || report.HostSchemaCompat < 1 {
		t.Errorf("version report omits contract or schema facts: %+v", report)
	}
}

// TestTheUsageNamesEveryCommand covers an operator who types a word the
// binary does not know. The answer lists the commands that exist, and the
// module commands are one of them.
func TestTheUsageNamesEveryCommand(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), built(t), "no-such-command")

	said, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("an unknown word was accepted: %s", said)
	}

	for _, want := range []string{"healthcheck", "-version", "admin", "registrations", "module add"} {
		if !strings.Contains(string(said), want) {
			t.Errorf("the usage text does not name %q: %s", want, said)
		}
	}
}

// TestTheWordTheRetiredServerAnsweredReachesTheCore is what a deletion owes an
// operator. `serve` named the retired server for three releases. That server is
// gone, so this binary refuses the word rather than quietly starting something
// under it.
//
// The core's own flag parser is what answers. The failure this guards against
// is a build that reads an unknown first word as a file name, or ignores it and
// serves.
func TestTheWordTheRetiredServerAnsweredReachesTheCore(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), built(t), "serve", "admin")

	said, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("`serve admin` was accepted, and no server answers that word: %s", said)
	}

	if strings.Contains(string(said), "--password-stdin") {
		t.Errorf("`serve admin` answered with the retired server's own usage: %s", said)
	}
}

// TestTheAccountCommandIsTheCurrentServersOwn covers the operator who types
// `admin create`. It was a subcommand of the retired server, then it was
// nothing, and it is now the current server's own bootstrap command: the one
// way to create the first administrator without the server running (finding
// CL-05).
//
// The line below carries no --name and no --password, so what it proves is that
// the word reaches this build's own command and is answered as a usage error
// naming the flags, rather than reaching the retired server or falling through
// to the core's flag parser.
func TestTheAccountCommandIsTheCurrentServersOwn(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), built(t), "admin", "create")

	said, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("`admin create` with no name and no password was accepted: %s", said)
	}

	for _, want := range []string{"usage", "--name", "--password"} {
		if !strings.Contains(string(said), want) {
			t.Errorf("`admin create` was refused with %q, which does not carry %q", string(said), want)
		}
	}
}

// stampVariable is the variable a release build writes its version into, and
// testStamp is the version these cases stamp.
const (
	stampVariable = "github.com/prusa3d/prusa-connect/internal/kernel/version.stamped"
	testStamp     = "9.8.7+test"
)

// healthVersion reads the version field of the health route.
func healthVersion(t *testing.T, url string) string {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building a request for %s: %v", url, err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("asking %s: %v", url, err)
	}

	defer func() { _ = response.Body.Close() }()

	var body struct {
		Version string `json:"version"`
	}

	if decodeErr := json.NewDecoder(response.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("reading the body of %s: %v", url, decodeErr)
	}

	return body.Version
}

// theCommand builds the shipping command once for the whole test binary. Three
// cases run it and a build costs tens of seconds, so the second and third ask
// for the answer the first produced.
//
// The build is untagged: what these cases read is which composition root main()
// reaches, and no build tag moves that.
//
//nolint:gochecknoglobals // a once-value over one build, read-only after the first case asks.
var theCommand = sync.OnceValues(func() (string, error) {
	directory, err := os.MkdirTemp("", "pcl-default-*")
	if err != nil {
		return "", fmt.Errorf("make a build directory: %w", err)
	}

	name := "prusa-connect"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := filepath.Join(directory, name)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", fmt.Errorf("locate the module root: %w", err)
	}

	build := exec.Command("go", "build", "-ldflags", "-X "+stampVariable+"="+testStamp,
		"-o", path, "./cmd/prusa-connect")
	build.Dir = root

	if said, buildErr := build.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("build the command: %w: %s", buildErr, said)
	}

	return path, nil
})

// built is the path to that build, and it fails the case rather than returning
// an error nobody reads.
func built(t *testing.T) string {
	t.Helper()

	path, err := theCommand()
	if err != nil {
		t.Fatalf("%v", err)
	}

	return path
}

// start runs the binary and kills it when the test ends, whatever else happened.
func start(t *testing.T, binary string, arguments ...string) {
	t.Helper()

	command := exec.Command(binary, arguments...)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		t.Fatalf("starting %s: %v", binary, err)
	}

	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
}

// waitForStatus polls one URL until it answers with the status asked for, and
// fails with whatever it answered with last.
func waitForStatus(t *testing.T, url string, want int) {
	t.Helper()

	deadline := time.Now().Add(settle)
	last := "nothing answered"

	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("building a request for %s: %v", url, err)
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			last = err.Error()
			time.Sleep(50 * time.Millisecond)

			continue
		}

		_ = response.Body.Close()

		if response.StatusCode == want {
			return
		}

		last = fmt.Sprintf("status %d", response.StatusCode)
	}

	t.Fatalf("%s never answered %d, and the last answer was %s", url, want, last)
}

// freeAddress is a loopback address nothing holds. The listener is closed
// before the binary is told about it, so a port taken in between is a flake
// rather than a failure of what this file is about.
func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("taking a port: %v", err)
	}

	address := listener.Addr().String()

	if closeErr := listener.Close(); closeErr != nil {
		t.Fatalf("releasing the port: %v", closeErr)
	}

	return address
}
