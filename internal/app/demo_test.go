package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// demoSettle bounds the whole run: the core binding, three printers registering
// and opening a session, and the first telemetry frame after the print starts.
const demoSettle = 60 * time.Second

// demoFleetSize is every machine the demo stands up on its defaults: the Prusa
// printers, and the one machine that is not a Prusa.
//
// A case that reads the fleet has to wait for all of them, because the demo
// reads its own listing only once the whole fleet has arrived. A case that read
// it earlier can miss a machine the demo acts on. The camera case would then
// hold three machines beside four cameras. The print case would miss the first
// printer by serial, which the demo starts a print on, whenever that printer
// registers last. In both cases the test fails whatever the server did.
const demoFleetSize = demoPrinters + 1

// TestTheDemoStandsUpAFleetACallerCanList is the gate under the one command a
// person runs to see this server do something.
//
// It reads what that person reads: the listing at /api/v1/devices, the printer
// telemetry behind it, and the words the command puts on their screen. The
// failure it guards against is quiet, because a demo that comes up and lists
// nothing still holds the port and still answers.
func TestTheDemoStandsUpAFleetACallerCanList(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	said := &demoSaid{}

	// No directory, no file and no printer count. The claim is that the
	// command alone is enough, so the only settings here are the address this
	// case has to know, the fault schedule, which would take a printer off the
	// wire in the middle of the reading below, and the password, which the
	// reads below sign in with the way a person does.
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- Demo(ctx, DemoOptions{APIAddr: addr, Flap: 0, Password: demoTestPassword, Out: said})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done, "a demo the person stopped must stop cleanly")
	})

	base := "http://" + addr
	client := &http.Client{Timeout: demoRequest}

	// The demo serves a login now, so a listing takes a credential. It is
	// minted through the demo's own sign-in, which is the two API calls a
	// person makes, against the account the demo wrote before it started.
	reader := &demoFleet{client: client}

	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token

		return err == nil
	}, demoSettle, demoPoll, "the demo account never signed in")

	var fleet demoListing

	require.Eventually(t, func() bool {
		fleet = demoRead[demoListing](t, reader, base+"/api/v1/devices")

		return connectedIn(fleet) >= demoFleetSize
	}, demoSettle, demoPoll, "the demo never listed %d connected printers", demoFleetSize)

	// The command the demo sends is a real one over the real path, so the
	// printer that took it reports a print. A fleet that only connects is the
	// filled table this gate refuses.
	require.Eventually(t, func() bool {
		return demoPrinting(t, reader, base, fleet) >= 1
	}, demoSettle, demoPoll, "no listed printer reports PRINTING, so the start command reached nobody")

	// The account is on the screen, once, beside the addresses. The line is
	// printed once the fleet has dialled in, which a loaded two-core runner
	// reaches after the print above is already reported, so it is waited for
	// rather than read once.
	require.Eventually(t, func() bool {
		text := said.text()

		return strings.Contains(text, "username  "+demoUsername) &&
			strings.Contains(text, "password  "+demoTestPassword)
	}, demoSettle, demoPoll, "the demo never printed its account: %q", said.text())

	// A design requirement of the row this closes: the run says where to look, with the
	// address, because a page named without its server running is a page nobody
	// can open.
	for _, address := range []string{base + "/dev", base + "/api/v1/devices", base + "/docs"} {
		require.Contains(t, said.text(), address, "the demo never printed the address to open")
	}
}

// demoTestPassword is the password these cases hand the demo, so they can
// sign in without reading it back off the screen.
const demoTestPassword = "a demo password"

// demoPrinting counts the listed printers whose telemetry reports a print.
func demoPrinting(t *testing.T, reader *demoFleet, base string, fleet demoListing) int {
	t.Helper()

	var printing int

	for _, item := range fleet.Items {
		read := demoRead[struct {
			State map[string]any `json:"state"`
		}](t, reader, base+"/api/v1/devices/"+item.ID)

		if word, ok := read.State["/state"].(string); ok && word == "PRINTING" {
			printing++
		}
	}

	return printing
}

// demoRead reads one answer whole and decodes it, carrying the token the
// reader signed in with. An answer that cannot be read is the zero value,
// because every caller here is inside a poll and a printer that has not
// arrived yet is not a failure.
func demoRead[T any](t *testing.T, reader *demoFleet, url string) T {
	t.Helper()

	var out T

	_, _ = reader.call(t.Context(), http.MethodGet, url, "", &out)

	return out
}

// demoSaid collects what the run printed. The demo writes it from a goroutine
// of its own while this case reads, and a bare buffer there is a data race that
// only a -race run reports.
type demoSaid struct {
	mu   sync.Mutex
	body bytes.Buffer
}

func (d *demoSaid) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.body.Write(p)
}

func (d *demoSaid) text() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.body.String()
}

// TestTheDemoRefusesAFleetOfNone holds the one input the command cannot serve.
// The fleet is the whole point, so an empty one is refused rather than served
// as a core with nothing connected to it.
func TestTheDemoRefusesAFleetOfNone(t *testing.T) {
	t.Parallel()

	err := Demo(t.Context(), DemoOptions{APIAddr: freeAddr(t), Printers: -1})

	require.ErrorIs(t, err, errNoPrinters)
}

// TestTheDemoReadsTheMarketplaceItIsGiven holds the -marketplace flag to the
// key it writes, and refuses an address the core would refuse anyway before a
// fleet is started for nothing.
func TestTheDemoReadsTheMarketplaceItIsGiven(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := DemoOptions{APIAddr: "127.0.0.1:8080", Marketplace: "http://localhost:4455"}.withDefaults()

	merged, err := demoDocument([]byte("# nothing here\n"), demoConfig(opts, dir, nil, true))
	require.NoError(t, err)

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, merged, 0o600))

	core, err := config.Load(path)
	require.NoError(t, err, string(merged))
	require.NoError(t, core.Validate(), string(merged))
	require.Equal(t, "http://localhost:4455", core.Marketplace.URL)

	err = Demo(t.Context(), DemoOptions{APIAddr: freeAddr(t), Marketplace: "http://plugins.example"})
	require.ErrorContains(t, err, "marketplace")
}

// TestTheDemoConfigurationCarriesEveryKeyTheCoreRefusesToStartWithout is the
// cheap half of the case above: the file the demo writes is read by
// config.Load, and a key missing from it stops the core before any of this is
// reached.
func TestTheDemoConfigurationCarriesEveryKeyTheCoreRefusesToStartWithout(t *testing.T) {
	t.Parallel()

	written := demoConfig(DemoOptions{APIAddr: "127.0.0.1:8080"}.withDefaults(), t.TempDir(), nil, true)

	for _, key := range []string{"principal:", "site:", "overlay:", "addr:", "type: filesystem", "local: true"} {
		require.Contains(t, written, key, "the demo configuration would not start the core")
	}

	require.NotContains(t, written, `\`, "a Windows separator reaches YAML unescaped")
	require.True(t, strings.HasSuffix(written, "\n"), "the configuration must end in a newline")
}

// TestTheDemoLaysItsOwnSettingsOverTheOperatorsFile holds the split between
// the two documents. What the demo owns is the demo's whatever the file says:
// its listener, its identity provider, its engine and its state. What the
// file declares beyond that reaches the core as written: the module, the
// origin, and a connector's own keys under the demo's registration bound.
func TestTheDemoLaysItsOwnSettingsOverTheOperatorsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := DemoOptions{APIAddr: "127.0.0.1:8080"}.withDefaults()

	operator := strings.Join([]string{
		"persistence:",
		"  type: postgres",
		"  postgres:",
		"    dsn: postgres://printers",
		"core:",
		"  api:",
		`    addr: ":9"`,
		"    tls:",
		"      certificate: printers.crt",
		"      key: printers.key",
		"    allowed_origins:",
		"      - https://printers.example",
		"  auth:",
		"    issuer: https://issuer.example",
		"    client_id: printers",
		"  modules: config/config.d/modules",
		"  authz:",
		"    database: /somewhere/authz.db",
		"    org_admin_handover: true",
		"  connectors:",
		"    prusa-xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        max_registrations: 1",
		"        approval: manual",
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      config:",
		"        root: /elsewhere",
		"  sink:",
		"    counter:",
		"      type: com.github.aux.acme.counter/counter",
		"      runtime: service",
		"",
	}, "\n")

	merged, err := demoDocument([]byte(operator), demoConfig(opts, dir, nil, true))
	require.NoError(t, err)

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, merged, 0o600))

	core, err := config.Load(path)
	require.NoError(t, err, string(merged))
	require.NoError(t, core.Validate(), string(merged))

	// The demo's.
	require.Equal(t, opts.APIAddr, core.API.Addr)
	require.Empty(t, core.API.TLS.Certificate)
	require.True(t, core.Auth.Local)
	require.Empty(t, core.Auth.Issuer)
	require.Equal(t, config.EngineSQLite, core.Persistence.Type)
	require.Equal(t, filepath.Join(dir, "resources.json"), filepath.Clean(core.Overlay))
	require.Equal(t, config.DefaultModules(path), filepath.Clean(core.Modules))
	require.NotEqual(t, "/somewhere/authz.db", core.Authz.Database)

	// The operator's.
	require.Equal(t, []string{"https://printers.example"}, core.API.AllowedOrigins)
	require.True(t, core.Authz.OrgAdminHandover)

	text := string(merged)
	require.Contains(t, text, "counter:", "the declared module was lost")
	require.Contains(t, text, "approval: manual", "the connector's own key was lost")
	require.Contains(t, text, "max_registrations: "+strconv.Itoa(opts.Printers*demoRegistrationHeadroom))
	require.NotContains(t, text, "/elsewhere", "the demo's storage was not the demo's")

	// And a file of nothing but comments is the demo's own document.
	alone, err := demoDocument([]byte("# nothing here\n"), demoConfig(opts, dir, nil, true))
	require.NoError(t, err)
	require.Contains(t, string(alone), "local: true")
}

// TestADemoOnALoopbackAddressStaysOffTheNetwork is the rule that keeps
// `make demo` and the check scripts that start it from announcing themselves.
func TestADemoOnALoopbackAddressStaysOffTheNetwork(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		addr string
		want bool
	}{
		"the default address":          {"", true},
		"a loopback port":              {"127.0.0.1:18096", true},
		"localhost":                    {"localhost:8080", true},
		"IPv6 loopback":                {"[::1]:8080", true},
		"every interface":              {":8080", false},
		"every interface, written out": {"0.0.0.0:8080", false},
		"one interface on the network": {"192.168.1.5:8080", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, DemoOptions{APIAddr: tc.addr}.withDefaults().onLoopback())
		})
	}
}

// A print stopped over the API reaches history as a cancelled job.
func TestTheDemoRecordsAStoppedPrintInHistory(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	said := &demoSaid{}
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() {
		done <- Demo(ctx, DemoOptions{APIAddr: addr, Flap: 0, Password: demoTestPassword, Out: said})
	}()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done, "the demo did not stop cleanly") })

	base := "http://" + addr
	reader := &demoFleet{client: &http.Client{Timeout: demoRequest}}
	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token
		return err == nil
	}, demoSettle, demoPoll, "the demo account never signed in")

	// The history feed subscribes at the live head once its provider serves,
	// so a print that settles earlier is never stored. Until then this read
	// answers 501.
	require.Eventually(t, func() bool {
		status, err := reader.call(t.Context(), http.MethodGet, base+"/api/v1/history/facts?limit=1", "", nil)
		return err == nil && status == http.StatusOK
	}, demoSettle, demoPoll, "history never served")

	var fleet demoListing
	require.Eventually(t, func() bool {
		fleet = demoRead[demoListing](t, reader, base+"/api/v1/devices")
		return demoPrinting(t, reader, base, fleet) >= 1
	}, demoSettle, demoPoll, "no printer started printing")

	printing := ""
	for _, device := range fleet.Items {
		row := demoRead[struct {
			State map[string]any `json:"state"`
		}](t, reader, base+"/api/v1/devices/"+device.ID)
		if row.State["/state"] == "PRINTING" {
			printing = device.ID
			break
		}
	}
	require.NotEmpty(t, printing)

	sent := time.Now()
	status, err := reader.write(t.Context(), http.MethodPost, base+"/api/v1/devices/"+printing+"/commands",
		`{"verb":"core.command.stop_print","args":{}}`, nil)
	require.NoError(t, err)
	require.Less(t, status, 300, "stop was refused")

	// Older cancelled facts may already be in history, so only a print that
	// ended after the stop counts.
	require.Eventually(t, func() bool {
		page := demoRead[struct {
			Items []struct {
				Payload json.RawMessage `json:"payload"`
			} `json:"items"`
		}](t, reader, base+"/api/v1/history/facts?types=com.prusa.connect.device.job.settled&origins=device:"+printing+"&limit=500&from="+sent.Add(-time.Second).UTC().Format(time.RFC3339))
		for _, item := range page.Items {
			var settled struct {
				Outcome string    `json:"outcome"`
				Ended   time.Time `json:"ended"`
			}
			if json.Unmarshal(item.Payload, &settled) != nil {
				continue
			}
			if settled.Outcome == "cancelled" && settled.Ended.After(sent.Add(-time.Second)) {
				return true
			}
		}
		return false
	}, demoSettle, demoPoll, "the stopped print never reached history")
}

// With every print set to fail, a failed job reaches history through the real
// driver, which is the path the module's failure table reads.
func TestTheDemoRecordsAFailedPrint(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() {
		done <- Demo(ctx, DemoOptions{APIAddr: addr, Flap: 0, Password: demoTestPassword,
			Out: &demoSaid{}, PrintGap: time.Second, FailShare: 1})
	}()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

	base := "http://" + addr
	reader := &demoFleet{client: &http.Client{Timeout: demoRequest}}
	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token
		return err == nil
	}, demoSettle, demoPoll)
	started := time.Now()

	require.Eventually(t, func() bool {
		page := demoRead[struct {
			Items []struct {
				Payload json.RawMessage `json:"payload"`
			} `json:"items"`
		}](t, reader, base+"/api/v1/history/facts?types=com.prusa.connect.device.job.settled&limit=500&from="+started.UTC().Format(time.RFC3339))
		for _, item := range page.Items {
			var settled struct {
				Outcome string    `json:"outcome"`
				Ended   time.Time `json:"ended"`
			}
			if json.Unmarshal(item.Payload, &settled) == nil &&
				settled.Outcome == "failed" && settled.Ended.After(started) {
				return true
			}
		}
		return false
	}, demoSettle, demoPoll, "a failed print never reached history as failed")
}

func TestTheDemoStartsWithThreeWeeksOfHistory(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	said := &demoSaid{}
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- Demo(ctx, DemoOptions{APIAddr: addr, Flap: 0, Password: demoTestPassword, Out: said})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})

	base := "http://" + addr
	reader := &demoFleet{client: &http.Client{Timeout: demoRequest}}

	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token

		return err == nil
	}, demoSettle, demoPoll)

	from := time.Now().AddDate(0, 0, -demoBackfillDays).UTC().Format(time.RFC3339)

	require.Eventually(t, func() bool {
		page := demoRead[struct {
			Items []json.RawMessage `json:"items"`
		}](t, reader, base+"/api/v1/history/facts?types=com.prusa.connect.device.job.settled&limit=100&from="+from)

		return len(page.Items) >= 100
	}, demoSettle, demoPoll, "the demo started without its past: %q", said.text())
}

// A fast demo makes a print a module starts on an idle fleet as short as its
// own, so a check that sends a print sees it end in seconds.
func TestAFastDemoMakesAModulesPrintTakeSeconds(t *testing.T) {
	t.Parallel()

	require.Equal(t, demoJobFrames, demoJobFramesOf(DemoOptions{}))
	require.Equal(t, demoFastBase, demoJobFramesOf(DemoOptions{PrintGap: time.Second, Idle: true}))
}

// The demo declares a record backend of its own only when the operator's file
// declares none. A deployment with two refuses every module state call, which
// is what the spools gate's file, declaring its own, would otherwise meet.
func TestTheDemoDeclaresARecordBackendOnlyWhenTheOperatorDeclaresNone(t *testing.T) {
	t.Parallel()

	opts := DemoOptions{APIAddr: "127.0.0.1:8080"}.withDefaults()
	dir := t.TempDir()

	none := []byte("# nothing here\n")
	require.False(t, declaresRecordBackend(none))

	alone, err := demoDocument(none, demoConfig(opts, dir, nil, !declaresRecordBackend(none)))
	require.NoError(t, err)
	require.Contains(t, string(alone), "records.db")

	operator := []byte(strings.Join([]string{
		"resources:",
		"  db:",
		"    main:",
		"      type: sqlite",
		"      config:",
		"        path: /somewhere/state.db",
		"",
	}, "\n"))
	require.True(t, declaresRecordBackend(operator))

	merged, err := demoDocument(operator, demoConfig(opts, dir, nil, !declaresRecordBackend(operator)))
	require.NoError(t, err)
	require.Contains(t, string(merged), "/somewhere/state.db")
	require.NotContains(t, string(merged), "records.db")
}
