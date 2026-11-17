package resourcehost_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

	"github.com/stretchr/testify/require"

	configinstall "github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// These cases drive the whole path an operator takes: a declaration in a
// configuration file, through the resource store and the supervision plane,
// into a second process that is running when they finish.
//
// The child is examples/service-plugin, built here rather than checked in,
// because the host launches <type>_<GOOS>_<GOARCH> and a committed binary would
// be one platform's. Owner questions 37 and 38 are open over the two things
// that shape says: an instance carries no interpreter, so the artefact is a
// native executable, and the file is named after the type rather than the
// instance.
const (
	// pluginType is what an instance writes under type, and therefore the name
	// of the artefact this host looks for.
	pluginType = "service-plugin"

	// pluginName is what the child answers plugin.info with.
	pluginName = "example-service"

	// startBudget is what promptPolicy gives a start, and what bounds the case
	// below that launches a child which never speaks.
	startBudget = 5 * time.Second

	// silentType is a child that writes no handshake and keeps its standard
	// output open. The host reads that line with no context of its own, so this
	// is the shape that holds a supervision loop when nothing bounds the launch.
	silentType = "silent-plugin"
)

// servingLine is the child's own banner, mirrored into the host's log off the
// child's standard error. The pid in it is the only handle a test has on the
// process, and the host publishes no other.
var servingLine = regexp.MustCompile(pluginName + ` [0-9.]+ is serving on stdio as pid (\d+)`)

// builtPlugin is the example built once for the whole package. Every case
// copies it into an install directory of its own, so a case that corrupts an
// artefact does not corrupt anybody else's.
var builtPlugin = sync.OnceValues(buildExamplePlugin)

// buildDir holds that one build, and TestMain removes it.
var buildDir string

// builtHog is testdata/hogchild built once for the package, in hogDir.
var (
	builtHog = sync.OnceValues(buildHog)
	hogDir   string
)

// testReaper is the one reaper of this test process, which every wired host
// starts its children through. Two would collect each other's children.
var testReaper = sync.OnceValue(func() *procgroup.Reaper {
	reaper := procgroup.NewReaper()

	go reaper.Run(context.Background())

	return reaper
})

func TestMain(m *testing.M) {
	code := m.Run()

	for _, dir := range []string{buildDir, hogDir} {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}

	os.Exit(code)
}

// buildExamplePlugin compiles examples/service-plugin for this platform.
func buildExamplePlugin() (string, error) {
	toolchain, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("find the go toolchain: %w", err)
	}

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve the repository root: %w", err)
	}

	buildDir, err = os.MkdirTemp("", "service-plugin-build")
	if err != nil {
		return "", fmt.Errorf("make a build directory: %w", err)
	}

	out := filepath.Join(buildDir, artefactName(pluginType))

	build := exec.Command(toolchain, "build", "-o", out, "./examples/service-plugin")
	build.Dir = root

	if said, buildErr := build.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("build the example plugin: %w: %s", buildErr, said)
	}

	return out, nil
}

// artefactName is the file name the host looks for, which is the one an
// author's release workflow produces.
func artefactName(typeName string) string {
	name := typeName + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

// installed is one extension as an operator has it on disk: a directory, the
// artefact in it, and the digest recorded for it.
type installed struct {
	dir      string
	path     string
	checksum string
}

// install copies the built example into a directory of this test's own and
// records what it hashes to.
func install(t *testing.T) *installed {
	t.Helper()

	source, err := builtPlugin()
	require.NoError(t, err)

	body, err := os.ReadFile(source)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	path := filepath.Join(dir, artefactName(pluginType))
	require.NoError(t, os.WriteFile(path, body, 0o700))

	return &installed{dir: dir, path: path, checksum: digestOf(body)}
}

// corrupt appends a byte to the artefact, leaving the recorded checksum
// covering bytes that are no longer there.
func (i *installed) corrupt(t *testing.T) {
	t.Helper()

	body, err := os.ReadFile(i.path)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(i.path, append(body, '\n'), 0o700))
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)

	return "sha256:" + hex.EncodeToString(sum[:])
}

// declare writes the configuration one service instance is declared by. The
// path is rendered with forward slashes, which the operating system accepts on
// every platform and which no YAML quoting rule then has to escape.
func (w *wired) declare(dir, checksum string, contractMajor int) {
	w.t.Helper()
	w.declareType(pluginType, dir, checksum, contractMajor)
}

// declareType is the same, for a case that installs a child of its own.
func (w *wired) declareType(typeName, dir, checksum string, contractMajor int) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      service:
        path: %s
        checksum: %s
        contract_major: %d
`, typeName, filepath.ToSlash(dir), checksum, contractMajor))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// silentSource is a child that starts, holds its standard output open and says
// nothing. It is a whole module so that building it pulls in nothing.
const silentSource = `package main

import "time"

func main() { time.Sleep(10 * time.Minute) }
`

// installSilent builds and installs the child that never speaks.
func installSilent(t *testing.T) *installed {
	t.Helper()

	toolchain, err := exec.LookPath("go")
	require.NoError(t, err)

	source := filepath.Join(t.TempDir(), "silent")
	require.NoError(t, os.MkdirAll(source, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(source, "main.go"), []byte(silentSource), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "go.mod"),
		[]byte("module silentplugin\n\ngo 1.26.7\n"), 0o600))

	dir := filepath.Join(t.TempDir(), "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	path := filepath.Join(dir, artefactName(silentType))

	build := exec.Command(toolchain, "build", "-o", path, ".")
	build.Dir = source

	said, err := build.CombinedOutput()
	require.NoError(t, err, string(said))

	body, err := os.ReadFile(path)
	require.NoError(t, err)

	return &installed{dir: dir, path: path, checksum: digestOf(body)}
}

// theServiceSink is the instance every case in this file declares.
func theServiceSink(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "example")
	require.NoError(t, err)

	return instance
}

// waitState blocks until the instance reaches one state, woken by the status
// the plane publishes when it gets there rather than by a ticker, and returns
// the status that satisfied it.
//
// It reports what the instance's conditions said when it did not, read at the
// moment it gave up.
func (w *wired) waitState(instance ref.Ref, want condition.State) httpapi.ResourceStatus {
	w.t.Helper()

	var status httpapi.ResourceStatus

	w.await(func() bool {
		found, ok := w.host.Status(instance)
		status = found

		return ok && found.State == want.String()
	}, "the instance never reached "+want.String(),
		func() string { return w.describe(instance) })

	return status
}

// describe renders the plane's whole view of one instance, for a failure
// message that says what the instance thought was wrong.
func (w *wired) describe(instance ref.Ref) string {
	found, ok := w.host.Status(instance)
	if !ok {
		return "the host is supervising nothing under " + instance.String()
	}

	return "state=" + found.State + " " + conditionsOf(&found)
}

// conditionsOf renders every condition on one status, for a failure message
// that says what the instance thought was wrong.
func conditionsOf(status *httpapi.ResourceStatus) string {
	var said strings.Builder

	said.WriteString("result=" + status.Result)

	for _, held := range status.Conditions {
		fmt.Fprintf(&said, " [%s=%s %s %q]", held.Type, held.Status, held.Reason, held.Message)
	}

	return said.String()
}

// says reports whether any condition's message carries text.
func says(status httpapi.ResourceStatus, text string) bool {
	for _, held := range status.Conditions {
		if strings.Contains(held.Message, text) {
			return true
		}
	}

	return false
}

// twoBanners reports a log carrying a second child's announcement, which is
// what says the replacement is a different process rather than the same one.
func twoBanners(logged string) bool {
	return len(servingLine.FindAllString(logged, -1)) > 1
}

// pidOf reads the child's pid out of the banner it wrote to its standard error.
func pidOf(t *testing.T, logged string) int {
	t.Helper()

	found := servingLine.FindStringSubmatch(logged)
	require.Len(t, found, 2, "the child never announced itself:\n%s", logged)

	pid, err := strconv.Atoi(found[1])
	require.NoError(t, err)

	return pid
}

// TestAServiceInstanceLaunchesTheArtefactAnOperatorDeclared is the whole point
// of the runtime. An operator writes a directory, a checksum and a contract
// major, and a process that is not in this binary is running and supervised.
func TestAServiceInstanceLaunchesTheArtefactAnOperatorDeclared(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 1)
	w.waitReady(instance)

	// The child's own standard error is the proof a second process ran: this
	// text exists nowhere in the host.
	w.waitLog(func(logged string) bool { return strings.Contains(logged, "is serving on stdio as pid") },
		"the child's standard error never reached the host's log")

	logged := w.logs.String()
	require.Contains(t, logged, "instance=sink:example", "the child's log lines are not bound to the instance")
	require.Positive(t, pidOf(t, logged))

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.True(t, status.Serving)
}

// TestAServiceInstanceWithNoArtefactRetriesRatherThanLatching holds the one
// refusal of the three that waiting can fix. An operator who has not installed
// the extension yet gets an instance that comes up when they do.
func TestAServiceInstanceWithNoArtefactRetriesRatherThanLatching(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theServiceSink(t)

	empty := filepath.Join(t.TempDir(), "plugins")
	require.NoError(t, os.MkdirAll(empty, 0o750))

	w.declare(empty, digestOf(nil), 1)

	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "RESOURCE_PLUGIN_MISSING"), conditionsOf(&status))

	// A condition message is capped at 200 characters, so the file the host
	// wanted is at the end of the refusal the log carries whole.
	require.Contains(t, w.logs.String(), artefactName(pluginType),
		"the refusal names the directory and not the file it wanted")
}

// TestAServiceInstanceWhoseArtefactChangedIsNotExecuted is the digest gate seen
// from the operator's end. The bytes moved after the checksum was recorded, so
// nothing is launched and nothing retries.
func TestAServiceInstanceWhoseArtefactChangedIsNotExecuted(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	recorded := extension.checksum
	extension.corrupt(t)

	w.declare(extension.dir, recorded, 1)

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_PLUGIN_CHECKSUM"), conditionsOf(&status))
	require.NotContains(t, w.logs.String(), "is serving on stdio",
		"the artefact ran, and a checksum that does not match is checked before anything is executed")
}

// TestAServiceInstanceDeclaringAnotherContractMajorIsRefused is the other
// permanent refusal. The operator wrote the major, so it is answered without a
// process being started at all.
func TestAServiceInstanceDeclaringAnotherContractMajorIsRefused(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 2)

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_PLUGIN_CONTRACT"), conditionsOf(&status))
	require.NotContains(t, w.logs.String(), "is serving on stdio",
		"a contract this host does not serve was launched anyway")
}

// TestAChildThatDiesIsStartedAgain holds the supervision half. A child killed
// from outside is a runtime failure like any other, so the instance climbs the
// backoff ladder and comes back as a different process.
func TestAChildThatDiesIsStartedAgain(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 1)
	w.waitReady(instance)

	w.waitLog(servingLine.MatchString, "the child never announced itself")

	first := pidOf(t, w.logs.String())

	process, err := os.FindProcess(first)
	require.NoError(t, err)
	require.NoError(t, process.Kill())

	// The instance says why it went down before it climbs the ladder. Without
	// this the case would pass over a restart the host did for any reason.
	failed := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(failed, "the child process ended and the host did not ask it to"),
		conditionsOf(&failed))

	// The stop says how the process ended. Windows ends a killed process
	// with an exit code rather than a signal.
	if runtime.GOOS != "windows" {
		require.NotNil(t, failed.Memory, conditionsOf(&failed))
		require.NotNil(t, failed.Memory.LastStop)
		require.Equal(t, "signal", failed.Memory.LastStop.Result)
		require.Equal(t, "SIGKILL", failed.Memory.LastStop.Signal)
	}

	// Two banners in the log is a second process, and the pid says it is not
	// the one that was killed.
	w.waitLog(twoBanners, "the killed child was never replaced")

	w.waitReady(instance)

	banners := servingLine.FindAllStringSubmatch(w.logs.String(), -1)
	require.NotEqual(t, banners[0][1], banners[1][1], "the same process announced itself twice")
}

// TestAChildThatWritesNothingDoesNotHoldTheSupervisionLoop is the reason the
// launch is bounded by the start budget and the child is not.
//
// The host reads a child's handshake line with no context at all, so a process
// that starts and then says nothing would block the loop until something killed
// it. Cancelling the child's own context is that something, and the instance
// gets a failed start it retries rather than a supervision loop that never
// comes back.
func TestAChildThatWritesNothingDoesNotHoldTheSupervisionLoop(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	silent := installSilent(t)
	instance := theServiceSink(t)

	w.declareType(silentType, silent.dir, silent.checksum, 1)

	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "start budget"), conditionsOf(&status))

	// The loop is still answering, which is what the case is about.
	require.True(t, w.host.Restart(instance))
}

// TestRestartingAServiceInstanceReplacesTheChild is the operator's restart over
// a child process. The instance keeps its name and its supervisor, the old
// process goes, and a new one takes its place.
//
// A restart is not a failure. The stop the host performed must not be reported
// as the child dying, or the instance comes back carrying a runtime failure it
// does not have.
func TestRestartingAServiceInstanceReplacesTheChild(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 1)
	w.waitReady(instance)

	w.waitLog(servingLine.MatchString, "the child never announced itself")

	require.True(t, w.host.Restart(instance))

	w.waitLog(twoBanners, "the restart launched no second child")

	w.waitReady(instance)

	banners := servingLine.FindAllStringSubmatch(w.logs.String(), -1)
	require.NotEqual(t, banners[0][1], banners[1][1], "the same process announced itself twice")

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.Equal(t, condition.StateReady().String(), status.State, conditionsOf(&status))
	require.False(t, says(status, "the child process ended and the host did not ask it to"),
		"the host reported its own stop as the child dying: %s", conditionsOf(&status))
}

// TestStoppingAServiceInstanceAsksTheChildToStop is the shutdown half. The
// host asks rather than kills, and it waits, so nothing is left behind holding
// the artefact.
func TestStoppingAServiceInstanceAsksTheChildToStop(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 1)
	w.waitReady(instance)

	w.waitLog(servingLine.MatchString, "the child never announced itself")

	w.stop()

	require.Contains(t, w.logs.String(), pluginName+" was asked to stop",
		"the child was killed rather than asked")

	// Removing a running executable fails on Windows and succeeds everywhere
	// else, so this half of the proof bites on one platform. The half that
	// holds everywhere is above it: Child.Close returns only after the child's
	// standard error reached its end, which happens when the process exits.
	require.NoError(t, os.Remove(extension.path), "the artefact is still held by a process")
}

// TestStoppingAServiceInstanceLeavesNoChildProcess is the other half of the
// shutdown. What the host leaves behind is nothing: the process the instance
// launched is gone by the time the stop returns.
//
// The kill is armed before the child is asked to stop, because a child that has
// stopped reading its pipe blocks that ask inside the operating system. A cancel
// deferred behind the ask would never run, and the process would outlive the
// host that started it.
func TestStoppingAServiceInstanceLeavesNoChildProcess(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	extension := install(t)
	instance := theServiceSink(t)

	w.declare(extension.dir, extension.checksum, 1)
	w.waitReady(instance)

	w.waitLog(servingLine.MatchString, "the child never announced itself")

	pid := pidOf(t, w.logs.String())
	child := watchChild(t, pid)

	w.stop()

	require.True(t, child.gone(), "the child process %d outlived the host's stop", pid)
}

// hogType is the type the hog child is declared under, and so the name of its
// artefact and the name it answers plugin.info with.
const hogType = "hogchild"

// hogBanner is the hog child's line once it has its settings.
var hogBanner = regexp.MustCompile(hogType + ` \w+ is serving as pid (\d+) under GOMEMLIMIT=\d+`)

// buildHog compiles testdata/hogchild for this platform.
func buildHog() (string, error) {
	dir, err := os.MkdirTemp("", "hogchild-build")
	if err != nil {
		return "", fmt.Errorf("make a build directory: %w", err)
	}

	hogDir = dir
	out := filepath.Join(dir, artefactName(hogType))

	build := exec.Command("go", "build", "-o", out, "./testdata/hogchild")
	if said, buildErr := build.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("build the hog child: %w: %s", buildErr, said)
	}

	return out, nil
}

// installHog copies the built hog child into a directory of this test's own.
func installHog(t *testing.T) *installed {
	t.Helper()

	source, err := builtHog()
	require.NoError(t, err)

	body, err := os.ReadFile(source)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	path := filepath.Join(dir, artefactName(hogType))
	require.NoError(t, os.WriteFile(path, body, 0o700))

	return &installed{dir: dir, path: path, checksum: digestOf(body)}
}

// declareHog declares the hog child in one mode under a budget, beside an
// in-process sink that shows the host still serving.
func (w *wired) declareHog(hog *installed, mode string, memoryMiB int) {
	w.t.Helper()
	w.declareNotedHog(hog, mode, "", memoryMiB)
}

// declareNotedHog is declareHog with a note, a setting the child ignores, so
// a case can change the settings and nothing else about what the child does.
func (w *wired) declareNotedHog(hog *installed, mode, note string, memoryMiB int) {
	w.t.Helper()
	w.declareHogOnOver(hog, mode, note, memoryMiB, configinstall.OnOverMemoryStop)
}

// declareHogOnOver is declareNotedHog with the instance's on_over_memory.
func (w *wired) declareHogOnOver(hog *installed, mode, note string, memoryMiB int, onOver string) {
	w.t.Helper()
	w.declareHogService(hog, mode, note, memoryMiB, "on_over_memory: "+onOver)
}

// declareHogService is declareNotedHog with more lines in the service block,
// each indented by declareHogService itself.
func (w *wired) declareHogService(hog *installed, mode, note string, memoryMiB int, service ...string) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      config:
        mode: %s
        note: %q
      module:
        version: 1.0.0
        options:
          - key: mode
            type: string
            summary: What the child does with memory.
          - key: note
            type: string
            summary: Nothing the child reads.
      service:
        path: %s
        checksum: %s
        contract_major: 1
        memory_mib: %d
%s
    webhook:
      type: counted
`, hogType, mode, note, filepath.ToSlash(hog.dir), hog.checksum, memoryMiB,
		"        "+strings.Join(service, "\n        ")))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// TestAModuleOverItsBudgetIsStoppedAndSaysWhy is the watchdog seen from the
// status. The child keeps allocating, the host kills it over its budget, and
// the stop carries both figures. The server itself goes on serving.
func TestAModuleOverItsBudgetIsStoppedAndSaysWhy(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("the figures the budget is held to are RssAnon, which only Linux reports")
	}

	w := newWired(t)
	w.declareHog(installHog(t), "alloc", 32)

	instance := theServiceSink(t)

	var status httpapi.ResourceStatus

	w.await(func() bool {
		status, _ = w.host.Status(instance)

		return status.State == condition.StateRetrying().String() && status.Result == "over-memory"
	}, "the child was never stopped for its memory", func() string { return w.describe(instance) })

	require.NotNil(t, status.Memory)
	require.NotNil(t, status.Memory.LastStop)
	stop := status.Memory.LastStop
	require.Equal(t, "over-memory", stop.Result)
	require.NotNil(t, stop.UsedBytes)
	require.NotNil(t, stop.BudgetBytes)
	require.Equal(t, int64(32<<20), *stop.BudgetBytes)
	require.Greater(t, *stop.UsedBytes, *stop.BudgetBytes)

	w.waitReady(theSink(t))
}

// TestTheThirdOverMemoryStopSwitchesTheModuleOff is the failure window over
// the watchdog: three stops inside the hour and the host stops starting it.
func TestTheThirdOverMemoryStopSwitchesTheModuleOff(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("the figures the budget is held to are RssAnon, which only Linux reports")
	}

	w := newWired(t)
	w.declareHog(installHog(t), "alloc", 32)

	instance := theServiceSink(t)

	w.await(func() bool {
		status, _ := w.host.Status(instance)

		return status.Result == "switched-off-by-host" && status.Memory != nil &&
			status.Memory.FailuresInWindow == 3
	}, "the host never switched the module off", func() string { return w.describe(instance) })
}

// TestAStoppedModuleLeavesNoProcess is the process group and the reaper
// together. The child started a process of its own, and stopping the
// instance ends that one too, without a zombie left behind.
func TestAStoppedModuleLeavesNoProcess(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the child starts sleep, which Windows does not have")
	}

	w := newWired(t)
	w.declareHog(installHog(t), "fork", 64)
	w.waitReady(theServiceSink(t))

	sleeperLine := regexp.MustCompile(`sleeper (\d+)`)
	w.waitLog(sleeperLine.MatchString, "the child never started its sleeper")

	pid, err := strconv.Atoi(sleeperLine.FindStringSubmatch(w.logs.String())[1])
	require.NoError(t, err)

	sleeper := watchChild(t, pid)

	w.stop()

	// A zombie still answers the null signal, so gone is also reaped.
	require.Eventually(t, sleeper.gone, 10*time.Second, 20*time.Millisecond,
		"the sleeper %d outlived the stop, or was never reaped", pid)
}

// TestABudgetEditRestartsTheChild is the budget a module learns only at start:
// the edit replaces the child, and the new one runs under the new GOMEMLIMIT.
func TestABudgetEditRestartsTheChild(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	hog := installHog(t)
	instance := theServiceSink(t)

	w.declareHog(hog, "idle", 64)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	w.declareHog(hog, "idle", 96)
	w.waitLog(func(logged string) bool { return len(hogBanner.FindAllString(logged, -1)) > 1 },
		"the budget edit launched no second child")
	w.waitReady(instance)

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.NotNil(t, status.Memory)
	require.Equal(t, int64(96<<20), status.Memory.BudgetBytes)

	banners := hogBanner.FindAllString(w.logs.String(), -1)
	require.Contains(t, banners[0], fmt.Sprintf("GOMEMLIMIT=%d", int64(64<<20)*85/100))
	require.Contains(t, banners[1], fmt.Sprintf("GOMEMLIMIT=%d", int64(96<<20)*85/100))
}

// TestAnOnOverMemoryEditKeepsTheChild is on_over_memory as host policy: the
// running child is not replaced for it.
func TestAnOnOverMemoryEditKeepsTheChild(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	hog := installHog(t)
	instance := theServiceSink(t)

	w.declareHog(hog, "idle", 64)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	w.declareHogOnOver(hog, "idle", "", 64, configinstall.OnOverMemoryWarn)

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.Equal(t, condition.StateReady().String(), status.State, conditionsOf(&status))
	require.Len(t, hogBanner.FindAllString(w.logs.String(), -1), 1, "the on_over_memory edit started a second child")
}

// TestASwitchOffEditReachesTheRunningSupervisor is switch_off as host policy:
// the edit reaches the supervisor of the running child, which is not replaced.
func TestASwitchOffEditReachesTheRunningSupervisor(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	hog := installHog(t)
	instance := theServiceSink(t)

	w.declareHog(hog, "idle", 64)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	w.declareHogService(hog, "idle", "", 64, "switch_off:", "  failures: 7", "  window: 15m")

	policy, ok := resourcehost.PolicyOf(w.host, instance)
	require.True(t, ok)
	require.Equal(t, 7, policy.FailureLimit)
	require.Equal(t, 15*time.Minute, policy.FailureWindow)
	require.Len(t, hogBanner.FindAllString(w.logs.String(), -1), 1, "the switch_off edit started a second child")
}

// TestABudgetEditedWithTheSettingsReachesTheWatch is the edit that restarts the
// child: the settings change with the budget, so the new child starts under
// the new GOMEMLIMIT and the watch holds it to the new budget.
func TestABudgetEditedWithTheSettingsReachesTheWatch(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	hog := installHog(t)
	instance := theServiceSink(t)

	w.declareNotedHog(hog, "idle", "first", 64)
	w.waitReady(instance)
	w.waitLog(hogBanner.MatchString, "the child never announced itself")

	w.declareNotedHog(hog, "idle", "second", 96)
	w.waitLog(func(logged string) bool { return len(hogBanner.FindAllString(logged, -1)) > 1 },
		"the settings edit launched no second child")
	w.waitReady(instance)

	status, ok := w.host.Status(instance)
	require.True(t, ok)
	require.NotNil(t, status.Memory)
	require.Equal(t, int64(96<<20), status.Memory.BudgetBytes, "the watch holds the new child to the old budget")

	banners := hogBanner.FindAllString(w.logs.String(), -1)
	require.Contains(t, banners[1], fmt.Sprintf("GOMEMLIMIT=%d", int64(96<<20)*85/100))
}

// TestTheFirstKillKeepsItsReason is two watches killing one child: the stop
// reports the reason of the first.
func TestTheFirstKillKeepsItsReason(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.declareHog(installHog(t), "idle", 64)

	instance := theServiceSink(t)
	w.waitReady(instance)

	require.NoError(t, resourcehost.KillChild(t.Context(), w.host, instance,
		supervisor.ResultNotResponding, supervisor.ResultOverMemory))

	w.await(func() bool {
		status, _ := w.host.Status(instance)

		return status.Memory != nil && status.Memory.LastStop != nil
	}, "the killed child's stop was never reported", func() string { return w.describe(instance) })

	status, _ := w.host.Status(instance)
	require.Equal(t, "not-responding", status.Memory.LastStop.Result)
}
