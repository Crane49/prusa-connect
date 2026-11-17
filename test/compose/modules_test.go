// The two worked example modules, in one binary, doing two different jobs.
//
// gate_test.go beside this proves the mechanism against a fixture written for
// the purpose: a sink and a device kind, small enough to read in one sitting.
// This is the other half, and it is the half the claim actually rests on. A
// fixture can be shaped around whatever the composition path happens to
// support. These two were not: storage-memory is a storage provider that the
// core does not compile in, and moonraker is a printer dialect for machines
// this project does not make. Neither is in any import list in this
// repository. A device class with verbs of its own is the fixture's half, in
// gate_test.go.
//
// Each of the two is checked by a different kind of evidence, on purpose:
//
//	storage     a .bgcode uploaded into it and the digest the provider
//	            computed on the write read back out of it
//	moonraker   a simulated Klipper machine dialled, arriving on the fleet,
//	            and reporting what it said it was, and the Klipper model row
//	            and its drawing, which only the driver declares
//
// The moonraker leg runs the simulator as a program rather than as a library,
// and that is the boundary being demonstrated rather than an inconvenience: the
// simulator moved out of the core with the dialect it serves, so this package
// cannot import it and starts examples/adapters/moonraker/cmd/moonraker-sim
// instead.
//
// It is skipped under -short for the reason the gate beside it is: composing is
// a full build of the core.
package compose_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	// examplesBuildFile is the configuration that names the two modules. It
	// is the one the published page tells a reader to run, so the gate proves
	// the documented command and not a variant of it.
	examplesBuildFile = "examples/compose/build.yaml"
	// scratchStorage is the instance name the configuration gives the memory
	// provider. The type is `memory`, which is the name the provider registered
	// and which a binary composed without it does not know at all.
	scratchStorage = "scratch"
	// klipperConnector is the instance name the configuration gives the
	// moonraker dialect.
	klipperConnector = "klipper"
	// modulesAddr is where the composed server binds for this test. It is a
	// fixed port rather than an ephemeral one because the server announces the
	// address it bound and this test would then have to race the announcement
	// against its own first request.
	modulesAddr = "127.0.0.1:18098"
	// uploadBytes is how much .bgcode is pushed into the memory store. It is
	// large enough that a provider buffering it wrongly would show up and small
	// enough to hold twice in memory without thinking about it.
	uploadBytes = 512 * 1024
	// arrivalBudget bounds the wait for a dialled machine to reach the fleet
	// listing. The dial loop is not on a timer this test can shorten.
	arrivalBudget = 30 * time.Second
)

// simListening is the one line examples/adapters/moonraker/cmd/moonraker-sim
// prints, and its whole contract with this test.
var simListening = regexp.MustCompile(`moonraker-sim listening on (\S+)`)

// TestTheExampleModulesRunInOneComposedBinary is the gate for ADR-20260901-7t4's
// claim on something other than a fixture.
func TestTheExampleModulesRunInOneComposedBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("composing builds the core from a scratch module; run without -short")
	}

	root := repoRoot(t)
	binary := composedBinary(t, "server-examples")

	composeFrom(t, root, examplesBuildFile, binary)

	for _, module := range []string{
		"example.com/prusa/storage-memory", "example.com/prusa/moonraker",
	} {
		assertLinkedModule(t, binary, module)
	}

	machine := startMoonrakerSim(t, root)
	base := startComposedServer(t, binary)

	assertMemoryStorageTakesAnUpload(t, base)
	assertMoonrakerMachineArrives(t, base, machine)
	assertKlipperRowIsDeclared(t, base)

	assertCoreUntouched(t, root)
}

// assertKlipperRowIsDeclared reads the row the moonraker driver declares out of
// the running server (ADR-20261016-qrp). The core carries no Klipper row, so the row
// and its drawing are served only because the driver is in the binary. The
// build refuses nothing, and a refusal is read first, because it says why the
// row would be missing.
func assertKlipperRowIsDeclared(t *testing.T, base string) {
	t.Helper()

	var served struct {
		Items []struct {
			Model     string `json:"model"`
			Connector string `json:"connector"`
			Artwork   string `json:"artwork"`
		} `json:"items"`
		Connectors []struct {
			Connector string   `json:"connector"`
			Available bool     `json:"available"`
			Models    []string `json:"models"`
		} `json:"connectors"`
		Refused []map[string]string `json:"refused"`
	}

	decode(t, get(t, base+"/api/v1/vocabularies/device-models"), &served)

	if len(served.Refused) > 0 {
		t.Fatalf("the composed server refused a declared model: %+v", served.Refused)
	}

	var artwork string

	for _, item := range served.Items {
		if item.Model == "Klipper" {
			if item.Connector != "moonraker" {
				t.Fatalf("the Klipper row is served under %q, not under the dialect that declared it", item.Connector)
			}

			artwork = item.Artwork
		}
	}

	if artwork == "" {
		t.Fatalf("the composed server serves no Klipper row with a drawing: %+v", served.Items)
	}

	available := false

	for _, connector := range served.Connectors {
		if connector.Connector == "moonraker" {
			available = connector.Available && slices.Contains(connector.Models, "Klipper")
		}
	}

	if !available {
		t.Fatalf("the moonraker dialect is not served as available with its Klipper row: %+v", served.Connectors)
	}

	drawing := request(t, http.MethodGet, base+artwork, "", nil, nil)
	if drawing.status != http.StatusOK || !bytes.Contains(drawing.body, []byte("<svg")) {
		t.Fatalf("the Klipper drawing answered %d: %s", drawing.status, drawing.body)
	}
}

// composeFrom builds one binary from one build configuration.
func composeFrom(t *testing.T, root, buildFile, binary string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), buildBudget)
	defer cancel()

	build := exec.CommandContext(ctx, "go", "run", "./cmd/compose-build",
		"--config", buildFile, "--output", binary)
	build.Dir = root

	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("composing a binary from %s failed.\n%s\n%v", buildFile, out, err)
	}
}

// assertLinkedModule reads the build graph the linker embedded. ADR-20260901-7t4 rule
// 10 is that a binary can be asked what it carries rather than remembered.
func assertLinkedModule(t *testing.T, binary, module string) {
	t.Helper()

	listed := exec.CommandContext(t.Context(), "go", "version", "-m", binary)

	out, err := listed.Output()
	if err != nil {
		t.Fatalf("reading the composed binary's module graph: %v", err)
	}

	if !strings.Contains(string(out), module) {
		t.Errorf("the composed binary's module graph does not name %s.\n%s", module, out)
	}
}

// startMoonrakerSim runs the simulator the moonraker module ships and returns
// the address it bound.
//
// It is `go run` in that module's own directory because this package cannot
// import it: the simulator is inside a module the core does not compile, which
// is the point being made. The program's contract is one line on stdout, so
// this reads one line and stops reading.
func startMoonrakerSim(t *testing.T, root string) string {
	t.Helper()

	sim := exec.CommandContext(context.WithoutCancel(t.Context()),
		"go", "run", "./cmd/moonraker-sim")
	sim.Dir = filepath.Join(root, "examples", "adapters", "moonraker")

	// `go run` builds a program and then execs it as a child, so what has to be
	// killed later is the pair. How that is arranged is per platform.
	inOwnGroup(sim)

	output, err := sim.StdoutPipe()
	if err != nil {
		t.Fatalf("reading the simulator's output: %v", err)
	}

	var complaints bytes.Buffer
	sim.Stderr = &complaints

	if err = sim.Start(); err != nil {
		t.Fatalf("starting the moonraker simulator: %v", err)
	}

	t.Cleanup(func() { killGroup(sim) })

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		if match := simListening.FindStringSubmatch(scanner.Text()); match != nil {
			return match[1]
		}
	}

	t.Fatalf("the moonraker simulator never announced a listener.\n%s", complaints.String())

	return ""
}

// startComposedServer writes a configuration declaring an instance of each
// module and starts the binary on it.
func startComposedServer(t *testing.T, binary string) string {
	t.Helper()

	work := t.TempDir()
	config := filepath.Join(work, "config.yaml")

	write(t, config, strings.Join([]string{
		"core:",
		"  api:",
		`    addr: "` + modulesAddr + `"`,
		"  log:",
		"    level: info",
		"    format: text",
		"  overlay: " + quoted(filepath.Join(work, "resources.json")),
		"  authz:",
		"    database: " + quoted(filepath.Join(work, "authz.db")),
		"  principal: operator",
		"  site: compose",
		"  auth:",
		"    setup_code: " + setupCode,
		// The test reaches the binary over loopback alone, so it announces
		// nothing on the local network.
		"  discovery:",
		"    enabled: false",
		"  connectors:",
		"    " + klipperConnector + ":",
		"      type: moonraker",
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: " + quoted(filepath.Join(work, "storage")),
		"    " + scratchStorage + ":",
		"      type: memory",
		"      enabled: true",
		"",
	}, "\n"))

	server := exec.CommandContext(context.WithoutCancel(t.Context()),
		binary, "core", "--config", config)
	server.Dir = work

	var transcript bytes.Buffer

	// One writer and not two: os/exec gives a non-file Stdout and a non-file
	// Stderr a copying goroutine each, and two goroutines appending to one
	// bytes.Buffer is a data race. Assigning the same value makes it one pipe.
	server.Stdout = &transcript
	server.Stderr = server.Stdout

	if err := server.Start(); err != nil {
		t.Fatalf("starting the composed server: %v", err)
	}

	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()

		if t.Failed() {
			t.Logf("the composed server said:\n%s", transcript.String())
		}
	})

	base := "http://" + modulesAddr

	// What this server does about identity, recorded before anything is read
	// from it. A fresh install answers SETUP_PENDING to every other route, so
	// the storage probe below would never see a listing.
	settleFirstRun(t, base)

	// The wait is for the storage instance and not for the listener. A resource
	// is opened after the listener binds, so a test that uploaded on the first
	// connection it could make would meet an instance that is still pending,
	// and would report the module missing when it is merely not open yet.
	deadline := time.Now().Add(startupBudget)

	for {
		if listed := probeStorages(base); listed {
			return base
		}

		if time.Now().After(deadline) {
			t.Fatalf("the composed server never served the %s storage within %s.\n%s",
				scratchStorage, startupBudget, transcript.String())
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// probeStorages reports whether the running server lists the memory store yet.
// Every failure is the same answer, because a connection refused, a 503 and a
// listing without the instance are all "not yet" until the deadline says
// otherwise.
func probeStorages(base string) bool {
	response, err := http.Get(base + "/api/v1/storages")
	if err != nil {
		return false
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		return false
	}

	return bytes.Contains(body, []byte(`"`+scratchStorage+`"`))
}

// assertMemoryStorageTakesAnUpload puts a .bgcode into the out-of-tree provider
// and reads it back.
//
// The digest is the assertion. The provider computes it as it writes, the host
// never sees the bytes twice, and this test computes its own over what it sent:
// three independent counts of the same object, and a provider that dropped or
// reordered a chunk would disagree with all of them.
func assertMemoryStorageTakesAnUpload(t *testing.T, base string) {
	t.Helper()

	content := make([]byte, uploadBytes)
	for i := range content {
		content[i] = byte(i*31 + i/251)
	}

	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	listed := struct {
		Items []struct {
			Name   string `json:"name"`
			Write  bool   `json:"write"`
			Ranges bool   `json:"ranges"`
		} `json:"items"`
	}{}
	decode(t, get(t, base+"/api/v1/storages"), &listed)

	found := false

	for _, item := range listed.Items {
		if item.Name != scratchStorage {
			continue
		}

		found = true

		if !item.Write || !item.Ranges {
			t.Errorf("the memory store is listed write=%v ranges=%v, and it serves both",
				item.Write, item.Ranges)
		}
	}

	if !found {
		t.Fatalf("the composed server serves no storage named %s, so the out-of-tree provider "+
			"never reached the resource plane", scratchStorage)
	}

	uploaded := struct {
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	}{}
	decode(t, put(t, base+"/api/v1/storages/"+scratchStorage+"/content?path=part.bgcode",
		"application/octet-stream", content), &uploaded)

	if uploaded.Bytes != int64(len(content)) || uploaded.SHA256 != digest {
		t.Fatalf("the memory store took %d bytes with digest %s, and this test sent %d bytes with %s",
			uploaded.Bytes, uploaded.SHA256, len(content), digest)
	}

	stated := struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}{}
	decode(t, get(t, base+"/api/v1/storages/"+scratchStorage+"/stat?path=part.bgcode"), &stated)

	if stated.SHA256 != digest {
		t.Errorf("the memory store reads the object back with digest %s and wrote it with %s",
			stated.SHA256, digest)
	}
}

// assertMoonrakerMachineArrives points the composed server at the simulator and
// waits for the machine on the fleet.
//
// The endpoint is written over the API rather than into the configuration file,
// because that is how a machine is added to a running server, and because it
// proves the dialect is reachable through the ordinary surface rather than only
// at start-up.
func assertMoonrakerMachineArrives(t *testing.T, base, machine string) {
	t.Helper()

	body := fmt.Sprintf(
		`{"connector":%q,"address":%q,"enabled":true,`+
			`"options":{"vendor":"Rat Rig","model":"V-Core 4","serial":"RR-0007"}}`,
		klipperConnector, machine)

	// If-None-Match: * is what makes the write a create. Without it the surface
	// answers 428, because a write with no precondition to a resource another
	// writer holds is a write that could silently replace one.
	created := request(t, http.MethodPut, base+"/api/v1/connector-endpoints/bench",
		"application/json", []byte(body), map[string]string{"If-None-Match": "*"})

	if created.status != http.StatusCreated {
		t.Fatalf("declaring the Klipper machine answered %d: %s", created.status, created.body)
	}

	deadline := time.Now().Add(arrivalBudget)

	for {
		fleet := struct {
			Items []struct {
				ID        string `json:"id"`
				Driver    string `json:"driver"`
				Vendor    string `json:"vendor"`
				Model     string `json:"model"`
				Serial    string `json:"serial"`
				Connected bool   `json:"connected"`
			} `json:"items"`
		}{}
		decode(t, get(t, base+"/api/v1/devices"), &fleet)

		if len(fleet.Items) == 1 && fleet.Items[0].Connected {
			found := fleet.Items[0]

			if found.Driver != klipperConnector {
				t.Errorf("the machine arrived under driver %q and the instance is %q",
					found.Driver, klipperConnector)
			}

			if found.Vendor != "Rat Rig" || found.Model != "V-Core 4" || found.Serial != "RR-0007" {
				t.Errorf("the machine arrived as %q %q %q, and the endpoint said Rat Rig V-Core 4 RR-0007",
					found.Vendor, found.Model, found.Serial)
			}

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("a Klipper machine at %s never arrived on the fleet within %s",
				machine, arrivalBudget)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// answer is one HTTP response, read whole.
type answer struct {
	status int
	body   []byte
}

// get, put and request are this file's HTTP client. They are three functions
// and not a dependency because a test that composes a binary and starts it has
// already paid for everything interesting, and what is left is four lines.
func get(t *testing.T, url string) answer {
	t.Helper()

	return request(t, http.MethodGet, url, "", nil, nil)
}

func put(t *testing.T, url, contentType string, body []byte) answer {
	t.Helper()

	return request(t, http.MethodPut, url, contentType, body, nil)
}

func request(t *testing.T, method, url, contentType string, body []byte, headers map[string]string) answer {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, url, err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, url, err)
	}

	return answer{status: response.StatusCode, body: read}
}

// decode reads one JSON answer, failing on a status that is not 2xx so that a
// refusal is reported as itself rather than as a decode error.
func decode(t *testing.T, got answer, into any) {
	t.Helper()

	if got.status < http.StatusOK || got.status >= http.StatusMultipleChoices {
		t.Fatalf("the composed server answered %d: %s", got.status, got.body)
	}

	if err := json.Unmarshal(got.body, into); err != nil {
		t.Fatalf("reading the answer: %v\n%s", err, got.body)
	}
}

// quoted renders a path for the configuration file. A Windows path carries
// backslashes, which yaml reads as escapes inside double quotes.
func quoted(path string) string {
	return "'" + strings.ReplaceAll(path, `'`, `''`) + "'"
}
