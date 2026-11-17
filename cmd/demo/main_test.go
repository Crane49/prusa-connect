package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app"
)

// The three things the two guides could not get from the demo, held here
// against the demo the command starts: a module declared in the operator's
// file starts under it, the account it prints signs in and mints a token, and
// an origin the file allows is answered with the CORS header a browser needs.
//
// One run holds all three, because each one costs a core, a printer and a
// child process, and the three are independent reads of the same server.

// The module the configuration declares. The namespace is the operator's
// claim and the file name is the artefact's, which is examples/service-plugin
// built for this platform.
const (
	moduleNamespace = "com.github.aux.acme.planner"
	moduleInstance  = "sink/counter"
	modulePath      = "/api/v1/resources/" + moduleInstance
	pageOrigin      = "https://printers.example"
	demoPassword    = "a demo password"
	settle          = 90 * time.Second
	poll            = 250 * time.Millisecond
)

func TestTheDemoStartsTheOperatorsFileWithItsOwnSettingsOverIt(t *testing.T) {
	dir, checksum := buildExampleModule(t)

	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(strings.Join([]string{
		"core:",
		"  api:",
		// The operator's address is replaced by the demo's own, and a
		// certificate would be too. The origin below survives.
		`    addr: ":9"`,
		"    allowed_origins:",
		"      - " + pageOrigin,
		"resources:",
		"  sink:",
		"    counter:",
		"      type: " + moduleNamespace + "/service-plugin",
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: " + filepath.ToSlash(dir),
		"        checksum: sha256:" + checksum,
		"        contract_major: 1",
		"",
	}, "\n")), 0o600))

	addr := freeAddress(t)
	said := &printed{}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- app.Demo(ctx, app.DemoOptions{
			APIAddr:    addr,
			ConfigPath: file,
			Password:   demoPassword,
			Printers:   1,
			Flap:       0,
			Cameras:    false,
			Out:        said,
		})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done, "a demo the person stopped must stop cleanly")
	})

	base := "http://" + addr
	client := &http.Client{Timeout: 15 * time.Second}

	// The account the demo printed signs in over the same path a person uses,
	// and the session mints a token. Polled, because the login is served only
	// once the core is up.
	var cookie string

	require.Eventually(t, func() bool {
		answered := send(t, client, http.MethodPost, base+"/api/v1/auth/login",
			`{"username":"demo","password":"`+demoPassword+`"}`, nil)
		if answered.status != http.StatusOK {
			return false
		}

		cookie = strings.SplitN(answered.header.Get("Set-Cookie"), ";", 2)[0]

		return cookie != ""
	}, settle, poll, "the demo account never signed in")

	minted := send(t, client, http.MethodPost, base+"/api/v1/auth/credentials",
		`{"name":"the guide"}`, map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, minted.status, minted.body)

	var credential struct {
		Token string `json:"token"`
	}

	require.NoError(t, json.Unmarshal([]byte(minted.body), &credential))
	require.True(t, strings.HasPrefix(credential.Token, "pcl_"), minted.body)

	bearer := map[string]string{"Authorization": "Bearer " + credential.Token}

	// The declared module reaches ready under the demo, which is the whole
	// reason the demo reads the file.
	require.Eventually(t, func() bool {
		return instanceState(t, client, base+modulePath, bearer) == "ready"
	}, settle, poll, "the declared module never reached ready under the demo")

	// The page's origin is answered with the header a browser looks for
	// before it sends the request, and with the one that lets it send the
	// credential.
	preflight := send(t, client, http.MethodOptions, base+"/api/v1/devices", "", map[string]string{
		"Origin":                        pageOrigin,
		"Access-Control-Request-Method": http.MethodGet,
	})
	require.Equal(t, pageOrigin, preflight.header.Get("Access-Control-Allow-Origin"), preflight.body)

	listed := send(t, client, http.MethodGet, base+"/api/v1/devices", "", map[string]string{
		"Origin":        pageOrigin,
		"Authorization": bearer["Authorization"],
	})
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.Equal(t, "true", listed.header.Get("Access-Control-Allow-Credentials"))

	// And the run said so: the account, and the file it read.
	require.Eventually(t, func() bool {
		return strings.Contains(said.text(), "password  "+demoPassword)
	}, settle, poll, "the demo never printed the account to sign in with")
	require.Contains(t, said.text(), "username  demo")
	require.Contains(t, said.text(), "The demo read "+file)
}

// TestTheDemoRunsOnItsDefaultsWhenTheFileIsAbsent is the promise the README
// makes: no configuration file is needed. A path with nothing at it is not an
// error, and the run says it ran on its defaults.
func TestTheDemoRunsOnItsDefaultsWhenTheFileIsAbsent(t *testing.T) {
	addr := freeAddress(t)
	said := &printed{}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- app.Demo(ctx, app.DemoOptions{
			APIAddr:    addr,
			ConfigPath: filepath.Join(t.TempDir(), "absent.yaml"),
			Printers:   1,
			Flap:       0,
			Cameras:    false,
			Out:        said,
		})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})

	require.Eventually(t, func() bool {
		return strings.Contains(said.text(), "There is no configuration file")
	}, settle, poll, "the demo never came up on its defaults")
}

// TestAFlagTheDemoDoesNotKnowIsAUsageError holds the exit code apart from a
// failure to run, which is what lets a person tell a typo from a demo that
// could not stand up.
func TestAFlagTheDemoDoesNotKnowIsAUsageError(t *testing.T) {
	t.Parallel()

	require.Equal(t, exitUsage, run([]string{"-no-such-flag"}))
}

// buildExampleModule links examples/service-plugin for this platform into a
// directory of its own, named the way the host looks for it, and returns the
// directory and the checksum an operator would write.
//
// The directory is not t.TempDir: on Windows a child the demo did not manage
// to stop keeps its binary open, and a directory left behind is not worth
// failing a green run over.
func buildExampleModule(t *testing.T) (string, string) {
	t.Helper()

	toolchain, err := exec.LookPath("go")
	require.NoError(t, err)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	//nolint:usetesting // t.TempDir fails the run when the child still holds the file; see above.
	dir, err := os.MkdirTemp("", "prusa-connect-demo-module-")
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	name := "service-plugin_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	out := filepath.Join(dir, name)

	build := exec.Command(toolchain, "build", "-o", out, "./examples/service-plugin")
	build.Dir = root

	if said, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build examples/service-plugin: %v: %s", buildErr, said)
	}

	body, err := os.ReadFile(out)
	require.NoError(t, err)

	sum := sha256.Sum256(body)

	return dir, hex.EncodeToString(sum[:])
}

// answer is one response, read whole.
type answer struct {
	status int
	header http.Header
	body   string
}

// send makes one request with the headers given and reads the answer whole. A
// server that is not up yet answers with status zero, so a poll can go round
// again.
func send(t *testing.T, client *http.Client, method, url, body string, headers map[string]string) answer {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	require.NoError(t, err)

	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return answer{}
	}

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return answer{status: response.StatusCode, header: response.Header, body: string(read)}
}

// instanceState is the state one declared instance reports, empty until the
// instance is listed.
func instanceState(t *testing.T, client *http.Client, url string, headers map[string]string) string {
	t.Helper()

	read := send(t, client, http.MethodGet, url, "", headers)
	if read.status != http.StatusOK {
		return ""
	}

	var instance struct {
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &instance), read.body)

	return instance.Status.State
}

// freeAddress is a loopback address nothing is listening on, so two cases in
// one binary do not fight over a port.
func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() { _ = listener.Close() }()

	return listener.Addr().String()
}

// printed collects what the demo wrote, under a lock, because the demo writes
// from its own goroutine while the case reads.
type printed struct {
	mu   sync.Mutex
	body bytes.Buffer
}

func (p *printed) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.body.Write(b)
}

func (p *printed) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.body.String()
}
