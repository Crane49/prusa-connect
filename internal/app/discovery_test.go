package app

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/discovery"
	"github.com/prusa3d/prusa-connect/internal/discovery/networktest"
)

// TestTheAnnouncementStartsAfterTheBindAndStopsBeforeTheDrain is ADR-20260916-b3x rule
// 8. The record names a port, so it may not go out before that port is open,
// and it may not outlive it either: a client reading a withdrawn record dials a
// socket nothing answers on.
//
// The order is read out of the log, because it is not observable from outside
// the process. By the time a client sees the listener close, every step has
// happened.
//
// It does not call t.Parallel: it replaces os.Stderr for the length of the run,
// and it opens the one mDNS socket this machine has.
func TestTheAnnouncementStartsAfterTheBindAndStopsBeforeTheDrain(t *testing.T) {
	const title = "Prusa Connect"

	dir := t.TempDir()
	logged := captureStderr(t)

	running := startOnNetwork(t, dir, lines(
		"core:",
		"  log:", "    level: info", "    format: text",
		"  frontend:", "    title: "+title,
		"  discovery:",
		"    enabled: true",
		"    services:",
		"      - _pcl-core._tcp"))

	require.Equal(t, discovery.StateAnnouncing,
		readinessOf(t, running.get(t, "/api/v1/readiness")).Discovery,
		"the readiness report does not say the announcer is running")

	running.stopAndWait(t)

	output := logged()

	serving := strings.Index(output, "serving")
	announced := strings.Index(output, "announcing "+title+" on ")
	withdrawn := strings.Index(output, "withdrew the local network announcement")
	drained := strings.Index(output, "listeners drained")

	require.Positive(t, serving, "the API listener never reported that it was bound:\n%s", output)
	require.Positive(t, announced, "nothing announced this server on the local network:\n%s", output)
	require.Positive(t, withdrawn, "the announcement was never withdrawn:\n%s", output)
	require.Positive(t, drained, "the listeners never reported that they drained:\n%s", output)

	require.Less(t, serving, announced,
		"the record went out before the port it names was open:\n%s", output)
	require.Less(t, withdrawn, drained,
		"the record outlived the listener it points at:\n%s", output)

	require.Contains(t, output, "as _pcl-core._tcp",
		"the startup line does not name the service the file configured:\n%s", output)
}

// TestASystemHostIsWhatTheStartupLineSays carries core.discovery.host_records
// from the file to the announcer (ADR-20261103-qag). The startup line is
// what an operator reads to learn who answers for the host name.
//
// It does not call t.Parallel: it replaces os.Stderr for the length of the
// run, and it opens the one mDNS socket this machine has.
func TestASystemHostIsWhatTheStartupLineSays(t *testing.T) {
	dir := t.TempDir()
	logged := captureStderr(t)

	running := startOnNetwork(t, dir, lines(
		"core:",
		"  log:", "    level: info", "    format: text",
		"  discovery:",
		"    enabled: true",
		"    host_records: system",
		"    services:",
		"      - _pcl-core._tcp"))

	running.stopAndWait(t)

	output := logged()
	require.Contains(t, output, "host_records=system",
		"the startup line does not say the host name is left to the system:\n%s", output)
}

// TestABlockSwitchedOffAnnouncesNothingAndSaysSo is ADR-20260916-b3x rule 7 in a
// running core. The readiness report is where an operator checks it, because a
// server announcing nothing and one whose machine refused the socket look the
// same from the network.
func TestABlockSwitchedOffAnnouncesNothingAndSaysSo(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), lines(
		"core:", "  log:", "    level: error",
		"  discovery:", "    enabled: false"))

	require.Equal(t, discovery.StateOff,
		readinessOf(t, running.get(t, "/api/v1/readiness")).Discovery)
}

// TestAnUnidentifiedCallerIsNotToldWhetherTheServerHides keeps the hidden
// setting from a probe anywhere on the network. The readiness answer to a
// caller with no identity carries no discovery field.
func TestAnUnidentifiedCallerIsNotToldWhetherTheServerHides(t *testing.T) {
	running := startPending(t, t.TempDir(), hiddenConfig("true"))

	answered := running.get(t, "/api/v1/readiness")
	require.NotContains(t, answered.body, `"discovery"`)
}

// hiddenConfig is a core whose file hides it. The service name is one no real
// client asks for, so a run beside a real instance announces nothing it reads.
func hiddenConfig(hidden string) string {
	return lines(
		"core:",
		"  log:", "    level: error",
		"  discovery:",
		"    enabled: true",
		"    hidden: "+hidden,
		"    services:",
		"      - _pcl-core._tcp")
}

// TestTheFileKeyIsWhereAPrinterStarts is core.discovery.hidden as the starting
// value: nobody has used the switch, so the file decides.
//
// It does not call t.Parallel, because it opens the one mDNS socket this
// machine has.
func TestTheFileKeyIsWhereAPrinterStarts(t *testing.T) {
	running := startOnNetwork(t, t.TempDir(), hiddenConfig("true"))

	answered := running.get(t, "/api/v1/discovery/visibility")
	require.Equal(t, http.StatusOK, answered.status, answered.body)
	requireSetting(t, answered.body, true, "hidden")

	require.Equal(t, discovery.StateHidden, readinessOf(t, running.get(t, "/api/v1/readiness")).Discovery)
}

// TestTheSwitchWinsOverTheFileKeyAndTakesEffectAtOnce is the customer's choice
// stored in the machine-owned file. It applies with no restart, and after a
// restart it still wins over the key that says the printer starts visible.
func TestTheSwitchWinsOverTheFileKeyAndTakesEffectAtOnce(t *testing.T) {
	dir := t.TempDir()
	running := startOnNetwork(t, dir, hiddenConfig("false"))

	require.Equal(t, discovery.StateAnnouncing, readinessOf(t, running.get(t, "/api/v1/readiness")).Discovery)

	answered := running.put(t, "/api/v1/discovery/visibility", `{"hidden": true}`)
	require.Equal(t, http.StatusOK, answered.status, answered.body)
	requireSetting(t, answered.body, true, "hidden")

	require.Equal(t, discovery.StateHidden, readinessOf(t, running.get(t, "/api/v1/readiness")).Discovery,
		"hiding the server needed a restart")

	running.stopAndWait(t)

	again := launchOnNetwork(t, dir)
	waitUntilReady(t, again)

	answered = again.get(t, "/api/v1/discovery/visibility")
	requireSetting(t, answered.body, true, "hidden", "the stored choice lost to core.discovery.hidden after a restart")

	answered = again.put(t, "/api/v1/discovery/visibility", `{"hidden": false}`)
	requireSetting(t, answered.body, false, "announcing")
}

// TestAHiddenServerStillListsThePrintersItHears keeps the list working on a
// hidden printer (ADR-20260925-jne rule 52). Another server announces itself on this
// machine's network, and the hidden core lists it. Hiding stops the printer
// being found, and nothing else.
//
// It does not call t.Parallel, because it opens the mDNS socket twice.
func TestAHiddenServerStillListsThePrintersItHears(t *testing.T) {
	running := startOnNetwork(t, t.TempDir(), hiddenConfig("true"))

	instance := "PrusaConnect-hidden-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	other := discovery.New(discovery.Config{
		Enabled:  true,
		Services: []string{config.ServicePrusaConnect},
		Instance: instance,
		Version:  "test",
	}, discard(), discovery.NewLink(discard(), nil))

	other.Start(t.Context(), 18099)
	t.Cleanup(func() { other.Stop(context.WithoutCancel(t.Context())) })

	require.Eventually(t, func() bool {
		answered := running.get(t, "/api/v1/discovery/nearby")

		return answered.status == http.StatusOK && strings.Contains(answered.body, instance)
	}, 15*time.Second, 200*time.Millisecond, "a hidden server did not list a server it can hear")
}

// TestAHiddenServerListensWithoutAsking is the other half of rule 52: the
// hidden server's listener asks nothing, from the start and after every
// change of the setting, because a question carries its address.
func TestAHiddenServerListensWithoutAsking(t *testing.T) {
	t.Parallel()

	link := discovery.NewLink(discard(), nil)
	announcer := discovery.New(discovery.Config{}, discard(), link)
	listener := discovery.NewListener(discovery.ListenerConfig{}, discard(), link)
	store := &storedHidden{hidden: true, recorded: true}

	network, err := newLocalNetwork(t.Context(), config.Core{}, announcer, listener, store)
	require.NoError(t, err)
	require.True(t, listener.Quiet(), "a server hidden at start asks for the printers nearby")

	_, err = network.SetHidden(t.Context(), false)
	require.NoError(t, err)
	require.False(t, listener.Quiet(), "a server shown again still asks nothing")

	_, err = network.SetHidden(t.Context(), true)
	require.NoError(t, err)
	require.True(t, listener.Quiet(), "a server hidden again still asks")
}

// startOnNetwork is start for a test about the local network. The core
// announces itself and listens for others as its file says, so the test is
// skipped unless PCL_NETWORK_TESTS is 1.
func startOnNetwork(t *testing.T, dir, content string) *process {
	t.Helper()
	networktest.Require(t)

	write(t, filepath.Join(dir, "config.yaml"), content)
	recordOpenSetup(t, dir)

	running := launchOnNetwork(t, dir)
	waitUntilReady(t, running)

	return running
}

// launchOnNetwork is launch for a test about the local network.
func launchOnNetwork(t *testing.T, dir string) *process {
	t.Helper()
	networktest.Require(t)

	return launchOptions(t, Options{
		ConfigPath:   filepath.Join(dir, "config.yaml"),
		APIAddr:      freeAddr(t),
		Version:      "test",
		LoopbackOnly: false,
	})
}

// storedHidden is the machine-owned file's hidden setting, in memory.
type storedHidden struct {
	hidden, recorded bool
}

func (s *storedHidden) DiscoveryHidden() (hidden, recorded bool, err error) {
	return s.hidden, s.recorded, nil
}

func (s *storedHidden) RecordDiscoveryHidden(hidden bool) error {
	s.hidden, s.recorded = hidden, true

	return nil
}

// requireSetting reads the hidden setting and the announcer's state out of a
// visibility answer. The rest of the answer names this machine, which differs
// on every runner.
func requireSetting(t *testing.T, body string, hidden bool, announcement string, why ...any) {
	t.Helper()

	var got struct {
		Hidden       bool   `json:"hidden"`
		Announcement string `json:"announcement"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &got), body)
	require.Equal(t, hidden, got.Hidden, why...)
	require.Equal(t, announcement, got.Announcement, why...)
}
