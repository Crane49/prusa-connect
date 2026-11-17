package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// statusVerb is the reference connector's own deliberately safe verb, the one
// adapters/connector/echo says a conformance run may send. No profile in this
// repository carries it, because every profile here is one xBuddy dialect's.
const statusVerb = connectorapi.Capability("core.command.get_status")

// baselineDriver is a driver with a baseline and no profiles of its own, which
// is every community dialect on the day it is written.
type baselineDriver struct {
	baseline connectorapi.Set
}

func (d baselineDriver) Baseline() connectorapi.Set { return d.baseline.Clone() }

// TestADriverIsResolvedAgainstItsOwnProfilesAndNobodyElses is the defect this
// wiring exists to stop.
//
// A resolver is scoped to one driver's profiles, because that union is the tier
// a device those profiles do not cover is held to. One resolver shared by every
// driver makes that union the xBuddy matrix for everybody, and subtracts from a
// community dialect's baseline every verb Prusa's firmware does not have,
// including the reference connector's own status verb.
func TestADriverIsResolvedAgainstItsOwnProfilesAndNobodyElses(t *testing.T) {
	t.Parallel()

	shipped, err := profile.Builtin()
	require.NoError(t, err)
	require.False(t, shipped.Coverage().Has(statusVerb),
		"a shipped profile now carries %s, so this test proves nothing", statusVerb)

	community := baselineDriver{baseline: connectorapi.Set{
		statusVerb: {Flag: true, Kind: connectorapi.KindFlag},
	}}
	identity := connectorapi.Identity{Vendor: "Echo", Model: "echo-1"}

	own, err := resolverFor(community, discard())
	require.NoError(t, err)

	kept := own.Arrive(t.Context(), community.Baseline(), identity, nil)
	require.True(t, kept.Capabilities.Has(statusVerb),
		"a driver that ships no profiles lost a verb from its own baseline")

	// The other half, which is what makes the first half mean something: the
	// shared registry does subtract it, so this is a scope and not a no-op.
	shared := capability.NewResolver(shipped, discard())

	narrowed := shared.Arrive(t.Context(), community.Baseline(), identity, nil)
	require.False(t, narrowed.Capabilities.Has(statusVerb),
		"one shared registry no longer narrows a driver it knows nothing about, so this test proves nothing")
}

// TestTheDialectThatShipsProfilesStillResolvesAgainstThem is the other
// direction: scoping the registry per driver must not stop the driver that
// owns the profile data from matching against it.
func TestTheDialectThatShipsProfilesStillResolvesAgainstThem(t *testing.T) {
	t.Parallel()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	found, err := types.Lookup("xbuddy")
	require.NoError(t, err)

	driver, err := found.Build(connectorapi.Spec{Ref: "xbuddy", Type: found.Qualified()})
	require.NoError(t, err)

	resolver, err := resolverFor(driver, discard())
	require.NoError(t, err)

	resolved := resolver.Arrive(t.Context(), driver.Baseline(), connectorapi.Identity{
		Vendor:   "Prusa",
		Model:    "MK4",
		Firmware: connectorapi.NewFirmwareLabel("6.4.0"),
	}, nil)

	require.True(t, resolved.ProfileMatched, "the dialect that ships the profile data no longer matches it")
	require.NotEmpty(t, resolved.Capabilities)
}

// echoRoutes is where the reference dialect answers when it is declared under
// the instance name below. The host chooses the prefix and strips it, so the
// driver's own /connect becomes this.
const (
	echoInstance = "lab"
	echoConnect  = connectorPrefix + echoInstance + "/connect"
	echoCommands = connectorPrefix + echoInstance + "/commands"

	// xbuddyRegister is the path shipping firmware has compiled in. It is
	// absolute, so it is the same wherever the dialect is declared.
	xbuddyRegister = "/p/register"
)

// TestADialectNamedInConfigurationIsMountedAndOneThatIsNotIsNot is the switch
// this key exists for, in both directions and through the real composition
// root.
//
// Before it, every registered dialect was mounted in every deployment, which is
// why the reference connector could not be linked at all: it authenticates
// nothing, so linking it opened device registration on every install.
//
// Each probe is the dialect's own answer rather than a 404. A path no dialect
// claims is refused by the origin, and so is a path no dialect was ever mounted
// on, so the status alone cannot tell a mounted dialect from an absent one.
// Only the dialect's own refusal proves it is there.
func TestADialectNamedInConfigurationIsMountedAndOneThatIsNotIsNot(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		declared []string
		echo     bool
		xbuddy   bool
	}{
		"naming the reference dialect leaves the frozen paths unmounted": {
			declared: []string{"    " + echoInstance + ":", "      type: echo"},
			echo:     true,
		},
		"naming the shipping dialect leaves the reference dialect unmounted": {
			declared: []string{"    xbuddy:", "      type: xbuddy"},
			xbuddy:   true,
		},
		"a connector switched off is declared and not mounted": {
			declared: []string{"    xbuddy:", "      type: xbuddy", "      enabled: false"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			running := start(t, t.TempDir(), lines(append(
				[]string{"core:", "  log:", "    level: error", "  connectors:"}, tc.declared...)...))

			require.Equal(t, tc.echo, joinedDevice(t, running) != "",
				"the reference dialect at %s answered against a configuration that names it %v",
				echoConnect, tc.echo)

			// The frozen path answers its own 400 for a registration carrying no
			// fingerprint, and nothing else on this origin answers that.
			refused := running.call(t, http.MethodPost, xbuddyRegister, "{}").status == http.StatusBadRequest
			require.Equal(t, tc.xbuddy, refused,
				"the shipping dialect at %s answered against a configuration that names it %v",
				xbuddyRegister, tc.xbuddy)
		})
	}
}

// TestADeclaredDialectAdmitsADeviceAndTheFleetHasIt is the other half of the
// switch: a dialect an operator named is not merely routable, it registers
// printers. The reference connector is the one this proves it on, because
// nothing links it into a build unless a configuration names it.
func TestADeclaredDialectAdmitsADeviceAndTheFleetHasIt(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines("core:", "  log:", "    level: error",
		"  connectors:", "    "+echoInstance+":", "      type: echo"))

	require.NotEmpty(t, joinedDevice(t, running), "the declared dialect admitted no device")

	listed := printers(t, running)
	require.Len(t, listed, 1, "a device that joined reached no fleet")
}

// TestADialectReadsTheOperatorsOwnSettings is the whole point of the key: the
// block under a connector reaches the driver that is running, not a copy of it.
//
// The poll window is the observable because its shipping default is 25 seconds
// and nothing else moves it. A driver built from a spec carrying no config
// holds this request open for that whole window, so the bound below is the
// assertion.
func TestADialectReadsTheOperatorsOwnSettings(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines("core:", "  log:", "    level: error",
		"  connectors:", "    "+echoInstance+":",
		"      type: echo",
		"      config:",
		"        poll_window: 20ms"))

	device := joinedDevice(t, running)
	require.NotEmpty(t, device, "the declared dialect admitted no device")

	// A quarter of the shipping default, so a driver that took the default
	// cannot answer inside it and a slow machine that took the configured one
	// still can.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+running.addr+echoCommands+"?device="+device, nil)
	require.NoError(t, err)

	client := http.Client{Timeout: 6 * time.Second}

	polled, err := client.Do(request)
	require.NoError(t, err, "the poll window an operator configured did not reach the running driver")

	defer func() { _ = polled.Body.Close() }()

	require.Equal(t, http.StatusNoContent, polled.StatusCode)
}

// TestASettingsBlockTheDialectRefusesStopsTheStart is the refusal that key
// buys. The three liveness durations are validated by the dialect itself, and
// before the key existed an operator writing them got the shipping sixty
// seconds whatever they wrote.
func TestASettingsBlockTheDialectRefusesStopsTheStart(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block []string
		names string
	}{
		"a type this build does not carry": {
			block: []string{"    printers:", "      type: bambu"},
			names: "core.connectors.printers",
		},
		"a shortened deadline with the shipping interval left above it": {
			block: []string{"    xbuddy:", "      type: xbuddy", "      config:", "        read_deadline: 12s"},
			names: "ping_interval",
		},
		"two dialects answering one frozen path": {
			block: []string{"    first:", "      type: xbuddy", "    second:", "      type: xbuddy"},
			names: xbuddyRegister,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "config.yaml"),
				lines(append([]string{"core:", "  log:", "    level: error", "  connectors:"}, tc.block...)...))

			err := runBounded(t, Options{
				ConfigPath:   filepath.Join(dir, "config.yaml"),
				APIAddr:      freeAddr(t),
				LogLevel:     "error",
				LoopbackOnly: true,
			})

			require.ErrorContains(t, err, tc.names)
		})
	}
}

// TestAFileThatNamesNoConnectorStillServesThePrintersInTheField is the
// fallback, and the reason this key did not have to be written into every
// existing deployment. Firmware that cannot be updated on this project's
// schedule dials fixed paths, so a file that says nothing keeps answering them.
func TestAFileThatNamesNoConnectorStillServesThePrintersInTheField(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines("core:", "  log:", "    level: error"))

	refused := running.call(t, http.MethodPost, xbuddyRegister, "{}")
	require.Equal(t, http.StatusBadRequest, refused.status,
		"a file that declares no connector left %s unmounted", xbuddyRegister)

	require.Empty(t, joinedDevice(t, running),
		"the reference dialect is mounted where no configuration names it")
}

// joinedDevice is one device arriving on the reference dialect, as a device
// does it: a plain POST carrying what it is. It returns the connection the
// dialect named, and the empty string when nothing there speaks that dialect.
//
// It does not go through the process helper's call, which adds the precondition
// header the resource API's writes need and a device wire has no use for.
func joinedDevice(t *testing.T, running *process) string {
	t.Helper()

	body := `{"vendor":"Echo","model":"echo-1","firmware":"1.0.0","serial":"SIM-ECHO",` +
		`"unit_uid":"sim-echo"}`

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+running.addr+echoConnect, strings.NewReader(body))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	if response.StatusCode != http.StatusOK {
		return ""
	}

	var admitted struct {
		Device string `json:"device"`
	}

	if err = json.Unmarshal(read, &admitted); err != nil {
		return ""
	}

	return admitted.Device
}

// outboundOnly is a dialect the host reaches rather than one that reaches the
// host. It answers no route, so mounting it is the whole of what this case
// reads; dialling it is dial_test.go, over a real socket.
type outboundOnly struct{ baselineDriver }

// Connect is never called here. This case reads what mountOne does with the
// driver and stops before anything opens a connection.
func (outboundOnly) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return nil, fmt.Errorf("%w: this case mounts and does not dial, and %d capabilities were offered",
		errNotDialled, len(profile))
}

// errNotDialled is what the driver above answers, so a host that started
// dialling reports something a reader can find rather than a nil session.
var errNotDialled = errors.New("outboundOnly: not dialled")

// TestAnOutboundDialectMountsNoRouteAndIsHandedToTheDialLoop is the seam the
// dial path hangs on.
//
// An outbound driver mounts nothing, because a route is what a device dials and
// this driver is dialled instead. What it produces is the dialling half the
// endpoint list feeds, and a host that dropped it on the floor would be the
// silence this case was first written against: a declared connector that
// mounted nothing, said nothing, and never reached the printer at its address.
//
// Watched red twice. With the dialled value not returned from mountOne the run
// reports a nil dialling half; with the inbound branch taken unconditionally it
// reaches inbound.Handler on a nil interface and panics.
func TestAnOutboundDialectMountsNoRouteAndIsHandedToTheDialLoop(t *testing.T) {
	t.Parallel()

	registered, err := connectorapi.Declare("dialled-out", outboundDoc(), func(
		_ connectorapi.Spec,
	) (connectorapi.Driver, error) {
		return outboundOnly{baselineDriver{baseline: connectorapi.Set{
			statusVerb: {Flag: true, Kind: connectorapi.KindFlag},
		}}}, nil
	})
	require.NoError(t, err)

	types, err := connectorapi.NewRegistryFrom(registered)
	require.NoError(t, err)

	built, err := mountOne(t.Context(), types, discard(), newPlane(t), config.NamedConnector{
		Name:      "far",
		Connector: config.Connector{Type: "dialled-out"},
	}, nil, nil, "", "")
	require.NoError(t, err, "a declared outbound dialect is not a configuration error")
	require.Empty(t, built.mounts, "an outbound dialect mounted a route, and a route is what a device dials")

	require.Nil(t, built.admission,
		"a dialect with no registration table reached the operator surface, which has nothing to decide")

	require.NotNil(t, built.dialled, "an outbound dialect reached no dial loop, so no printer reaches it")
	require.Equal(t, connectorapi.Ref("far"), built.dialled.ref)
	require.NotNil(t, built.dialled.arrivals, "the dial loop was handed no way to admit what it reaches")
	require.True(t, built.dialled.baseline.Has(statusVerb), "the dial loop was handed the wrong baseline")
}

// outboundDoc is the descriptor the declaration above carries. sdk/extension
// refuses a registration without one, so the case needs a real descriptor
// rather than a zero value.
func outboundDoc() extension.Doc {
	return extension.Doc{
		Summary:     "A dialect the host reaches, declared here so the host's answer to one can be read.",
		Description: "It is a test fixture. Connect is never called: this case reads the mount and stops.",
		Version:     "1.0.0",
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

// registerOverTheWire sends one printer's registration at the routes a set of
// mounts answers, and returns the status it got.
func registerOverTheWire(t *testing.T, mounts []httpapi.Mount, fingerprint string) int {
	t.Helper()

	const path = "/p/register"

	request := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(`{"fingerprint":"`+fingerprint+`"}`))
	recorder := httptest.NewRecorder()

	// The mounts are matched by hand rather than through a router, because
	// only the sealed transport package may name one
	// (TestOnlyTheSealedPackageImportsTheRouter).
	for _, mount := range mounts {
		if !strings.HasPrefix(path, strings.TrimSuffix(mount.Pattern, "*")) {
			continue
		}

		mount.Handler.ServeHTTP(recorder, request)

		return recorder.Code
	}

	t.Fatalf("no mount answers %s", path)

	return 0
}

// TestTheRecordedAdmissionChoiceIsWhatTheConnectorAdmitsOn is the other half of
// the first run's admission row. The choice is written into the machine-owned
// overlay, and the only thing that makes it true is the connector reading it
// when it is built.
//
// The observable is the registration table. Ask me first leaves a printer
// waiting for an operator; let every printer in approves it on arrival, so
// there is nothing waiting.
//
// An install that has recorded nothing asks first too.
func TestTheRecordedAdmissionChoiceIsWhatTheConnectorAdmitsOn(t *testing.T) {
	t.Parallel()

	for name, one := range map[string]struct {
		choice  string
		waiting int
	}{
		"ask me first":         {choice: resource.AdmissionAsk, waiting: 1},
		"let every printer in": {choice: resource.AdmissionOpen, waiting: 0},
		"nothing recorded yet": {choice: "", waiting: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			built, err := mountConnectors(t.Context(), config.Default(), "", discard(),
				newPlane(t), nil, nil, one.choice)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK,
				registerOverTheWire(t, built.mounts, "fingerprint-of-one-printer"))

			pending, err := newRegistrations(built.admissions, nil).Pending(t.Context())
			require.NoError(t, err)
			require.Len(t, pending, one.waiting)
		})
	}
}

// TestTheOperatorsOwnModeWinsOverTheRecordedChoice keeps the operator's file
// theirs. Somebody who wrote the key down meant it, and the first run fills in
// only where nothing was written.
func TestTheOperatorsOwnModeWinsOverTheRecordedChoice(t *testing.T) {
	t.Parallel()

	settings, err := withAdmission(t.Context(), discard(), "core.connectors.xbuddy",
		shippedAdmission(t), json.RawMessage(`{"mode":"token"}`),
		resource.AdmissionOpen)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"token"}`, string(settings))
}

// TestADialectThatDeclaredNoAdmissionWordsIsLeftAlone is why the translation
// comes from the driver. ask and open are the core's words, and the core cannot
// guess what another wire calls waiting for an operator.
func TestADialectThatDeclaredNoAdmissionWordsIsLeftAlone(t *testing.T) {
	t.Parallel()

	settings, err := withAdmission(t.Context(), discard(), "core.connectors.far",
		connectorapi.Admission{}, json.RawMessage(`{"url":"http://printer"}`),
		resource.AdmissionOpen)
	require.NoError(t, err)
	require.JSONEq(t, `{"url":"http://printer"}`, string(settings))
}

// TestTheShippingDialectDeclaresItsAdmissionWords is the other half: the words
// the first run writes are the driver's own, and the core reads them off the
// registration rather than knowing the dialect by name.
func TestTheShippingDialectDeclaresItsAdmissionWords(t *testing.T) {
	t.Parallel()

	words := shippedAdmission(t)
	require.Equal(t, connectorapi.Admission{Key: "mode", Open: "none", Ask: "token"}, words)

	settings, err := withAdmission(t.Context(), discard(), "core.connectors.xbuddy",
		words, nil, resource.AdmissionAsk)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"token"}`, string(settings))
}

// shippedAdmission is what the shipping dialect declared, read from the registry
// this binary was built with. Reading it rather than writing the words here
// keeps the test on the declaration the server itself uses.
func shippedAdmission(t *testing.T) connectorapi.Admission {
	t.Helper()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registration, err := types.Lookup(builtin.ShippingConnector())
	require.NoError(t, err)

	return registration.Dialect().Admission
}

// TestAnInstallThatRecordedNothingAsksFirst holds what a server does before
// its first run has recorded how a new printer joins. The dialect's own
// default admits on arrival, and the host writes the asking word over it, so
// nothing is admitted until a person decides. An operator who wrote the key
// themselves still wins, as they do over a recorded choice.
func TestAnInstallThatRecordedNothingAsksFirst(t *testing.T) {
	t.Parallel()

	settings, err := withAdmission(t.Context(), discard(), "core.connectors.xbuddy",
		shippedAdmission(t), nil, "")
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"token"}`, string(settings))

	settings, err = withAdmission(t.Context(), discard(), "core.connectors.xbuddy",
		shippedAdmission(t), json.RawMessage(`{"mode":"none"}`), "")
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"none"}`, string(settings))
}
