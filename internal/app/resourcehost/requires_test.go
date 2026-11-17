package resourcehost_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// A module declares what it cannot run without, and the host does not start it
// without those things. The declaration records the list at install, because a
// module that runs a process leaves no manifest on disk.
//
// The cases below drive a real child process through a real configuration file,
// the way every other case in this package does, so what they prove is what an
// operator would see.

// theOtherNamespace is a second module namespace, for a case that declares two
// modules at once. One namespace is one module, so two instances need two.
const theOtherNamespace = "com.github.aux.other"

// declareRequiringModule writes the block `module add` writes for a module
// whose manifest named one requirement.
func (w *wired) declareRequiringModule(module *installed, key, value string) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        requires:
          - %s: %s
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, mountedNamespace+"/"+pluginName, key, value, filepath.ToSlash(module.dir), module.checksum))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// reads reports the status and the reason of the DependenciesSatisfied
// condition on an instance.
func reads(status httpapi.ResourceStatus) (string, string) {
	for _, held := range status.Conditions {
		if held.Type == condition.TypeDependenciesSatisfied.String() {
			return held.Status, held.Reason
		}
	}

	return "", ""
}

// TestAModuleRequiringACapabilityThisBuildDoesNotServeIsRefused is the refusal
// an operator reads on an instance that would answer an error to every call.
// This host serves no fleet, so the module's process is never launched.
func TestAModuleRequiringACapabilityThisBuildDoesNotServeIsRefused(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareRequiringModule(module, "capability", "device.list")

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_DEPENDENCY_UNSERVED"), conditionsOf(&status))

	held, reason := reads(status)
	require.Equal(t, condition.StatusFalse.String(), held, conditionsOf(&status))
	require.Equal(t, condition.ReasonDependencyFailed, reason, conditionsOf(&status))
	require.True(t, says(status, "device.list"), "the refusal does not name what is missing")
}

// TestAModuleRequiringACapabilityThisBuildServesStarts is the other direction:
// the same path with the host serving the method set the module named.
func TestAModuleRequiringACapabilityThisBuildServesStarts(t *testing.T) {
	t.Parallel()

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		cfg.State = func(string, int64) (stateapi.Records, error) { return servedRecords{}, nil }
	})
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareRequiringModule(module, "capability", string(stateapi.MethodGet))

	w.waitReady(instance)
}

// servedRecords stands for a record backend that serves. Nothing calls it: the
// cases that use it read only whether the host found a store.
type servedRecords struct{ stateapi.Records }

// TestAModuleRequiringTheRecordStoreIsNotReadyWithoutABackend is the server a
// first-run wizard sets up: no record backend. A module that keeps its data in
// the record store must not report ready there, because every call it served
// would fail. It waits on the retry ladder, and the refusal says why.
func TestAModuleRequiringTheRecordStoreIsNotReadyWithoutABackend(t *testing.T) {
	t.Parallel()

	var serving atomic.Bool

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		cfg.State = func(string, int64) (stateapi.Records, error) {
			if !serving.Load() {
				return nil, nil //nolint:nilnil // no backend and no failure is what the door answers.
			}

			return servedRecords{}, nil
		}
	})
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareRequiringModule(module, "capability", string(stateapi.MethodGet))

	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "RESOURCE_RECORDS_UNSERVED"), conditionsOf(&status))
	require.True(t, says(status, "no record backend is serving"), conditionsOf(&status))
	require.Contains(t, conditionsOf(&status), `[DependenciesSatisfied=False DependencyMissing "`,
		"the instance does not wait on the retry ladder")

	// A backend that starts serving brings the module up with nothing else
	// done.
	serving.Store(true)

	w.waitReady(instance)
}

// TestAModuleRequiringACoreVersionThisServerIsNotIsRefused holds the running
// build to the range the module declared. Nothing on this machine brings
// another core, so the instance latches instead of retrying.
func TestAModuleRequiringACoreVersionThisServerIsNotIsRefused(t *testing.T) {
	t.Parallel()

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) { cfg.CoreVersion = "1.0.0" })
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareRequiringModule(module, "core", `">=9.0.0"`)

	status := w.waitState(instance, condition.StateFailed())
	require.True(t, says(status, "RESOURCE_DEPENDENCY_UNSERVED"), conditionsOf(&status))
	require.True(t, says(status, "1.0.0"), "the refusal does not say which core is running")
}

// TestAModuleRequiringACoreVersionThisServerIsStarts is the same declaration
// against a build inside the range.
func TestAModuleRequiringACoreVersionThisServerIsStarts(t *testing.T) {
	t.Parallel()

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) { cfg.CoreVersion = "9.1.0" })
	module := installAs(t, pluginName)
	instance := theServiceSink(t)

	w.declareRequiringModule(module, "core", `">=9.0.0"`)

	w.waitReady(instance)
}

// theDependency is the namespace of the module the cases below require.
const theDependency = theOtherNamespace

// dependencyModules writes two modules: example requires the module under
// theDependency at the range, and dependency is that module, switched on as
// enabled says.
func (w *wired) dependencyModules(first, second *installed, wanted string, enabled bool) {
	w.t.Helper()

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        requires:
          - module: %s
            version: "%s"
      service:
        path: %s
        checksum: %s
        contract_major: 1
    dependency:
      type: %s
      runtime: service
      enabled: %t
      module:
        version: 1.0.0
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, mountedNamespace+"/"+pluginName, theDependency, wanted, filepath.ToSlash(first.dir), first.checksum,
		theDependency+"/"+pluginName, enabled, filepath.ToSlash(second.dir), second.checksum))

	require.NoError(w.t, w.store.Reload(w.t.Context()))
}

// theDependencyInstance is the instance of the required module.
func theDependencyInstance(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "dependency")
	require.NoError(t, err)

	return instance
}

// TestAModuleWhoseDependencyIsOutOfRangeWaitsForIt is the requirement an
// operator can still meet. The instance climbs the retry ladder instead of
// latching, so updating the module it requires brings it up.
func TestAModuleWhoseDependencyIsOutOfRangeWaitsForIt(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theServiceSink(t)

	w.dependencyModules(installAs(t, pluginName), installAs(t, pluginName), "^2.0", true)

	status := w.waitState(instance, condition.StateRetrying())
	require.True(t, says(status, "RESOURCE_DEPENDENCY_MISSING"), conditionsOf(&status))
	require.True(t, says(status, theDependency+" ^2.0"), "the refusal does not name the module and its range")

	held, reason := reads(status)
	require.Equal(t, condition.StatusFalse.String(), held, conditionsOf(&status))
	require.Equal(t, "DependencyMissing", reason, conditionsOf(&status))
}

// TestADependantStartsAfterItsDependency declares a module and its dependant
// in one file. The dependant's start waits instead of being refused, so it
// reaches ready with no DependenciesSatisfied condition in its history
// (ADR-20261027-e6k rule 22).
func TestADependantStartsAfterItsDependency(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theServiceSink(t)

	w.dependencyModules(installAs(t, pluginName), installAs(t, pluginName), "^1.0", true)

	w.waitReady(theDependencyInstance(t))
	w.waitReady(instance)

	status, _ := w.host.Status(instance)
	held, _ := reads(status)
	require.Empty(t, held, "the dependant was refused before its dependency served: %s", conditionsOf(&status))
}

// TestADependencyThatStopsTakesItsDependantsDown switches the required module
// off and on again. The dependant goes back to waiting, and comes up again
// once the module serves (ADR-20261027-e6k rule 23).
func TestADependencyThatStopsTakesItsDependantsDown(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	instance := theServiceSink(t)
	first, second := installAs(t, pluginName), installAs(t, pluginName)

	w.dependencyModules(first, second, "^1.0", true)
	w.waitReady(instance)

	w.dependencyModules(first, second, "^1.0", false)

	status := w.waitState(instance, condition.StateRetrying())
	held, reason := reads(status)
	require.Equal(t, condition.StatusFalse.String(), held, conditionsOf(&status))
	require.Equal(t, "DependencyMissing", reason, conditionsOf(&status))

	w.dependencyModules(first, second, "^1.0", true)
	w.waitReady(instance)
}

// TestTwoModulesThatRequireEachOtherAreBothRefused is the ring. Neither module
// is wrong on its own and neither can ever start, so both are named and both
// are refused rather than left climbing a ladder forever.
//
// The ring is settled over the whole document, before the first instance is
// built, so neither artefact is launched and neither is asked what it is.
func TestTwoModulesThatRequireEachOtherAreBothRefused(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	first := installAs(t, pluginName)
	second := installAs(t, pluginType)

	firstType := mountedNamespace + "/" + pluginName
	secondType := theOtherNamespace + "/" + pluginType

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    first:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        requires:
          - module: %s
            version: "^1"
      service:
        path: %s
        checksum: %s
        contract_major: 1
    second:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        requires:
          - module: %s
            version: "^1"
      service:
        path: %s
        checksum: %s
        contract_major: 1
`,
		firstType, theOtherNamespace, filepath.ToSlash(first.dir), first.checksum,
		secondType, mountedNamespace, filepath.ToSlash(second.dir), second.checksum))

	require.NoError(t, w.store.Reload(t.Context()))

	for _, name := range []string{"first", "second"} {
		instance, err := ref.New(ref.KindSink, name)
		require.NoError(t, err)

		status := w.waitState(instance, condition.StateFailed())
		require.True(t, says(status, "RESOURCE_DEPENDENCY_CYCLE"), conditionsOf(&status))

		held, reason := reads(status)
		require.Equal(t, condition.StatusFalse.String(), held, conditionsOf(&status))
		require.Equal(t, condition.ReasonDependencyCycle, reason, conditionsOf(&status))
	}
}

// Every method of the storage wire is a capability a module can require, so
// the install accepts a module that lists a folder (ADR-20261023-sns rule 5).
func TestTheStorageMethodsAreCapabilities(t *testing.T) {
	t.Parallel()

	require.Subset(t, resourcehost.Capabilities(),
		[]string{"storage.list", "storage.read", "storage.stat"})
}

// The camera wire is a capability a module can require, so the install
// accepts a module that hands frames to a push camera.
func TestTheCameraMethodIsACapability(t *testing.T) {
	t.Parallel()

	require.Contains(t, resourcehost.Capabilities(), "camera.accept")
}

// TestTwoModulesCallEachOther is ADR-20261027-e6k rule 27 on the real plane.
// Each module suggests the other and names it under calls, and a call made
// through one module's route reaches the other as the calling module.
func TestTwoModulesCallEachOther(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	first, second := installAs(t, pluginName), installAs(t, pluginName)

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        suggests: [{module: %s, reason: calls it}]
      service:
        path: %s
        checksum: %s
        contract_major: 1
        calls: [%s, com.github.aux.unnamed]
    dependency:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        suggests: [{module: %s, reason: calls it}]
      service:
        path: %s
        checksum: %s
        contract_major: 1
        calls: [%s]
`, mountedNamespace+"/"+pluginName, theDependency, filepath.ToSlash(first.dir), first.checksum, theDependency,
		theDependency+"/"+pluginName, mountedNamespace, filepath.ToSlash(second.dir), second.checksum,
		mountedNamespace))

	require.NoError(t, w.store.Reload(t.Context()))

	w.waitReady(theServiceSink(t))
	w.waitReady(theDependencyInstance(t))

	for _, leg := range []struct{ from, to, principal string }{
		{mountedNamespace, theDependency, "sink:example"},
		{theDependency, mountedNamespace, "sink:dependency"},
	} {
		surface, claimed := w.host.Module(leg.from)
		require.True(t, claimed)

		answered := fetchThrough(t, surface.Handler, "/call?module="+leg.to+"&path=/spools")
		body, err := io.ReadAll(answered.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, answered.StatusCode, string(body))

		var relayed struct {
			Body struct {
				Principal string `json:"principal"`
			} `json:"body"`
		}

		require.NoError(t, json.Unmarshal(body, &relayed))
		require.Equal(t, leg.principal, relayed.Body.Principal, "the call did not run as the calling module")
	}

	// A module named under calls and not under requires or suggests is
	// refused before anything is relayed.
	surface, _ := w.host.Module(mountedNamespace)
	refused := fetchThrough(t, surface.Handler, "/call?module=com.github.aux.unnamed&path=/spools")
	require.Equal(t, http.StatusForbidden, refused.StatusCode)
}
