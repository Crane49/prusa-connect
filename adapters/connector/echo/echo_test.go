package echo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/connector/echo"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
)

// testPollWindow is short so that a device's poll returns promptly when a test
// ends. A real deployment sets it near the host's read timeout, because a poll
// that returns often is a request that costs a connection often.
const testPollWindow = 20 * time.Millisecond

// TestContract is ADR-20260825-n55 rule 4: this adapter passes the same published suite
// a community author's does.
func TestContract(t *testing.T) {
	connectorapitest.RunDriverContract(t, func(_ *testing.T) connectorapitest.Subject {
		return connectorapitest.Subject{
			Driver:   echo.NewDriver(4, testPollWindow),
			Dial:     dialDevice,
			SafeVerb: echo.VerbStatus,
		}
	})
}

func TestTheDriverRegistersItsType(t *testing.T) {
	// Draining seals the package gathering list for this test binary, which is
	// what the host does once at startup. Nothing after this point may register.
	registry, err := connectorapi.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	found, err := registry.Lookup(echo.Type)
	if err != nil {
		t.Fatalf("the adapter's init did not put %q in the registry: %v", echo.Type, err)
	}

	if !found.Namespace().IsCore() {
		t.Errorf("an adapter in this repository is namespaced %q, and the host derives core for its "+
			"own module", found.Namespace())
	}

	driver, err := registry.Build(connectorapi.Spec{
		Ref:    "printer-1",
		Type:   echo.Type,
		Config: json.RawMessage(`{"queue_depth":2,"poll_window":"20ms"}`),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if _, _, reachable := connectorapi.Directions(driver); reachable != nil {
		t.Errorf("the registered driver is reachable in no direction: %v", reachable)
	}

	if !connectorapi.Sealed() {
		t.Error("the gathering list is not sealed after the host built its registry")
	}
}

func TestAnUnreadableConfigIsRefused(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		config string
	}{
		{name: "a queue nothing can be put in", config: `{"queue_depth":0}`},
		{name: "a poll window that is not a duration", config: `{"poll_window":"soon"}`},
		{name: "a poll window that never waits", config: `{"poll_window":"0s"}`},
		{name: "a block that is not an object", config: `["queue_depth"]`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := echo.New(connectorapi.Spec{Ref: "printer-1", Type: echo.Type,
				Config: json.RawMessage(testCase.config)})
			if !errors.Is(err, echo.ErrInvalidConfig) {
				t.Errorf("a config this driver cannot act on was accepted: %v", err)
			}
		})
	}
}

// A device that claims less than the host resolved gets less, and a device that
// claims more gets no more: the probe is the third tier and it narrows only.
func TestTheProbeNarrowsAndNeverWidens(t *testing.T) {
	t.Parallel()

	driver := echo.NewDriver(4, testPollWindow)
	host := connectorapitest.NewHost(driver.Baseline())
	base := mount(t, driver, host)

	dev := connect(t, base, deviceIdentity{
		Model:    "ECHO-1",
		Firmware: "1.2.3",
		Serial:   "sn-probe",
		Probe: []connectorapi.Capability{
			echo.VerbStatus,
			"core.command.fabricated_by_the_device",
		},
	})

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	caps, _ := session.Capabilities()

	if !caps.Has(echo.VerbStatus) {
		t.Error("a capability the device claimed and the baseline declares was dropped")
	}

	if caps.Has("core.command.fabricated_by_the_device") {
		t.Error("a capability the device invented reached the resolved set")
	}

	if caps.Has(echo.VerbStartPrint) {
		t.Error("a capability the device did not claim survived its probe")
	}

	if !slices.Contains(dev.capabilities, echo.VerbStatus) {
		t.Errorf("the device was told it may send %v, which does not include the verb it claimed",
			dev.capabilities)
	}
}

func TestAStatePatchReachesTheHost(t *testing.T) {
	t.Parallel()

	driver := echo.NewDriver(4, testPollWindow)
	host := connectorapitest.NewHost(driver.Baseline())
	base := mount(t, driver, host)

	dev := connect(t, base, deviceIdentity{Model: "ECHO-1", Serial: "sn-state"})

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	if reported := dev.report(`{"path":"/tools/0/nozzle_temp","value":215}`); reported != nil {
		t.Fatalf("reporting state: %v", reported)
	}

	select {
	case patch := <-session.State():
		if patch.Path != "/tools/0/nozzle_temp" {
			t.Errorf("the host was handed a patch for %q", patch.Path)
		}

		if string(patch.Value) != "215" {
			t.Errorf("the host was handed the value %q", patch.Value)
		}
	case <-t.Context().Done():
		t.Fatal("no patch reached the host")
	}
}

// A device that stops draining its queue must fail commands rather than absorb
// them, because a command sitting in a queue nobody empties is one the caller
// waits out its whole deadline for.
func TestAStalledDeviceFailsCommandsRatherThanQueueingThem(t *testing.T) {
	t.Parallel()

	const depth = 1

	driver := echo.NewDriver(depth, testPollWindow)
	host := connectorapitest.NewHost(driver.Baseline())
	base := mount(t, driver, host)

	// No device loop: this device dials in and then answers nothing.
	connect(t, base, deviceIdentity{Model: "ECHO-1", Serial: "sn-stalled"})

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// The queue holds exactly depth commands and the offer never waits, so of
	// the senders below exactly depth occupy it and the rest are refused at
	// once. Reading that many answers is the synchronisation: nothing here
	// waits on a clock.
	const senders = 4

	answers := make(chan error, senders)

	for range senders {
		go func() {
			_, sendErr := session.Do(ctx, connectorapi.Command{Verb: echo.VerbStatus})
			answers <- sendErr
		}()
	}

	for i := range senders - depth {
		if refused := <-answers; !errors.Is(refused, echo.ErrQueueFull) {
			t.Fatalf("refusal %d was %v, and a stalled device must refuse rather than absorb", i, refused)
		}
	}

	cancel()

	if waited := <-answers; !errors.Is(waited, context.Canceled) {
		t.Errorf("the command waiting on a stalled device did not end with its context: %v", waited)
	}
}

func TestADisconnectClosesTheSession(t *testing.T) {
	t.Parallel()

	driver := echo.NewDriver(4, testPollWindow)
	host := connectorapitest.NewHost(driver.Baseline())
	base := mount(t, driver, host)

	dev := connect(t, base, deviceIdentity{Model: "ECHO-1", Serial: "sn-bye"})

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	if gone := dev.disconnect(); gone != nil {
		t.Fatalf("disconnecting: %v", gone)
	}

	// The state channel closing is how the host learns a printer went away, so
	// a device that says goodbye must produce exactly that.
	select {
	case _, open := <-session.State():
		if open {
			t.Error("the state channel carried a patch after the device disconnected")
		}
	case <-t.Context().Done():
		t.Fatal("the state channel was still open after the device disconnected")
	}

	if _, sent := session.Do(t.Context(), connectorapi.Command{Verb: echo.VerbStatus}); sent == nil {
		t.Error("a command was accepted for a device that had disconnected")
	}
}

// A device that names a session this driver is not holding gets an answer, not
// a panic and not somebody else's printer.
func TestAnUnknownDeviceTokenIsRefused(t *testing.T) {
	t.Parallel()

	driver := echo.NewDriver(4, testPollWindow)
	base := mount(t, driver, connectorapitest.NewHost(driver.Baseline()))

	for _, path := range []string{"/commands?device=nobody", "/state?device=nobody", "/results?device="} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}

		_ = resp.Body.Close()

		if resp.StatusCode < 400 {
			t.Errorf("GET %s answered %s for a session this driver is not holding", path, resp.Status)
		}
	}
}

// mount puts the driver's routes behind a prefix, as a host does, and returns
// the base URL a device is told.
func mount(t *testing.T, driver *echo.Driver, host *connectorapitest.Host) string {
	t.Helper()

	const prefix = "/connectors/echo"

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	server := httptest.NewServer(http.StripPrefix(prefix, handler))
	t.Cleanup(server.Close)

	return server.URL + prefix
}

// deviceIdentity is what a device says about itself in the handshake.
type deviceIdentity struct {
	Vendor   string                    `json:"vendor"`
	Model    string                    `json:"model"`
	Firmware string                    `json:"firmware"`
	Serial   string                    `json:"serial"`
	UnitUID  string                    `json:"unit_uid"`
	Probe    []connectorapi.Capability `json:"probe,omitempty"`
}

// device is the printer half of the dialect, which lives here rather than in
// the package because it is a fake printer and not part of the adapter.
type device struct {
	base         string
	token        string
	capabilities []connectorapi.Capability
}

// dialDevice is the connectorapitest.DialDevice for this dialect: one device
// dials in and then answers whatever it is asked, which is what makes it an
// echo.
func dialDevice(t *testing.T, baseURL string) error {
	t.Helper()

	dev, err := dialOnce(baseURL, deviceIdentity{
		Vendor:   "prusa",
		Model:    "ECHO-1",
		Firmware: "1.2.3",
		Serial:   "sn-contract",
		UnitUID:  "uid-contract",
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	go dev.answerCommands(ctx, stopped)

	return nil
}

// connect dials in and leaves the device answering commands until the test ends.
func connect(t *testing.T, baseURL string, identity deviceIdentity) *device {
	t.Helper()

	dev, err := dialOnce(baseURL, identity)
	if err != nil {
		t.Fatalf("dialling in: %v", err)
	}

	return dev
}

func dialOnce(baseURL string, identity deviceIdentity) (*device, error) {
	body, err := json.Marshal(identity)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(baseURL+"/connect", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the driver answered %s to the handshake", resp.Status)
	}

	var accepted struct {
		Device       string                    `json:"device"`
		Capabilities []connectorapi.Capability `json:"capabilities"`
		Digest       string                    `json:"digest"`
	}

	if decoded := json.NewDecoder(resp.Body).Decode(&accepted); decoded != nil {
		return nil, decoded
	}

	if accepted.Device == "" {
		return nil, errors.New("the driver named no device")
	}

	return &device{base: baseURL, token: accepted.Device, capabilities: accepted.Capabilities}, nil
}

// answerCommands is the device's own loop: poll, do what was asked, say so.
func (d *device) answerCommands(ctx context.Context, stopped chan struct{}) {
	defer close(stopped)

	client := &http.Client{}

	for ctx.Err() == nil {
		command, ok, err := d.poll(ctx, client)
		if err != nil {
			return
		}

		if !ok {
			continue
		}

		if answered := d.answer(ctx, client, command.ID); answered != nil {
			return
		}
	}
}

type wireCommand struct {
	ID   uint64                  `json:"id"`
	Verb connectorapi.Capability `json:"verb"`
	Args map[string]string       `json:"args,omitempty"`
}

func (d *device) poll(ctx context.Context, client *http.Client) (wireCommand, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/commands?device="+d.token, nil)
	if err != nil {
		return wireCommand{}, false, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return wireCommand{}, false, err
	}

	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		_, _ = io.Copy(io.Discard, resp.Body)

		return wireCommand{}, false, nil
	case http.StatusOK:
	default:
		return wireCommand{}, false, fmt.Errorf("polling answered %s", resp.Status)
	}

	var command wireCommand
	if decoded := json.NewDecoder(resp.Body).Decode(&command); decoded != nil {
		return wireCommand{}, false, decoded
	}

	return command, true, nil
}

func (d *device) answer(ctx context.Context, client *http.Client, id uint64) error {
	body := fmt.Sprintf(`{"id":%d,"ok":true}`, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.base+"/results?device="+d.token, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("reporting a result answered %s", resp.Status)
	}

	return nil
}

func (d *device) report(patch string) error {
	resp, err := http.Post(d.base+"/state?device="+d.token, "application/json", bytes.NewReader([]byte(patch)))
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("reporting state answered %s", resp.Status)
	}

	return nil
}

func (d *device) disconnect() error {
	resp, err := http.Post(d.base+"/disconnect?device="+d.token, "application/json", nil)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("disconnecting answered %s", resp.Status)
	}

	return nil
}
