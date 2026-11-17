package app

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
)

// The first-run wizard and `make demo` both write a configuration that
// declares no record backend. A module that keeps its data in the record store
// then started, reported ready, and failed every request with "no record
// backend is serving". These cases run that configuration through the real
// composition root.

// noRecordBackendConfig is the shape the wizard and the demo write: a storage
// and no resources.db block. The module is the example service module, and
// requires is what its manifest asked for, copied into the declaration as
// `module add` copies it.
func noRecordBackendConfig(dir string, installed *installedModule, requires ...string) string {
	declared := lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      config:",
		"        root: "+slashed(filepath.Join(dir, "storage")),
		"  sink:",
		"    planner:",
		"      type: "+moduleType,
		"      enabled: true",
		"      runtime: service",
	)

	if len(requires) > 0 {
		declared += lines(
			"      module:",
			"        version: 1.0.0",
			"        requires:",
		)

		listed := make([]string, 0, len(requires))
		for _, capability := range requires {
			listed = append(listed, "          - capability: "+capability)
		}

		declared += lines(listed...)
	}

	return declared + lines(
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
	)
}

// TestAModuleThatNeedsTheRecordStoreIsNotReadyWithoutABackend is the report:
// the module is not ready, and its instance says why.
func TestAModuleThatNeedsTheRecordStoreIsNotReadyWithoutABackend(t *testing.T) {
	dir := t.TempDir()
	running := start(t, dir, noRecordBackendConfig(dir, installModule(t), "state.get", "state.list"))

	require.Eventually(t, func() bool {
		return running.instanceState(t, moduleInstance) == condition.StateRetrying().String()
	}, settle, 20*time.Millisecond, "the module did not wait for a record backend")

	read := running.get(t, "/api/v1/resources/"+moduleInstance)
	require.Equal(t, http.StatusOK, read.status, read.body)
	require.Contains(t, read.body, "RESOURCE_RECORDS_UNSERVED", "the instance does not say why it waits")
	require.Contains(t, read.body, "no record backend is serving")
	require.Contains(t, read.body, "resources.db", "the instance does not say what to declare")

	running.stopAndWait(t)
}

// TestAModuleThatDoesNotNeedTheRecordStoreIsToldThereIsNone is the other
// module on the same server. It starts, and a call it makes on the record
// store reads that the host serves no such method, which a module can tell
// apart from a backend that failed.
func TestAModuleThatDoesNotNeedTheRecordStoreIsToldThereIsNone(t *testing.T) {
	dir := t.TempDir()
	running := start(t, dir, noRecordBackendConfig(dir, installModule(t)))

	require.Eventually(t, func() bool {
		return running.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the module never reached ready")

	read := running.get(t, moduleGroup+"/records/spools/petg")
	require.Equal(t, http.StatusBadGateway, read.status, read.body)
	require.Contains(t, read.body, "serves no method state.get", read.body)

	running.stopAndWait(t)
}
