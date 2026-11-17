package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	// The record store is pure database/sql; reading its rows back needs a
	// driver, and modernc is the one the shipping binary links.
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// No case in this file calls t.Parallel, which departs from the house default.
// Each one starts a core and two child processes through the real composition
// root, and six of them at once is eighteen processes on one machine. Run in
// parallel they starve internal/app/resourcehost, whose own cases wait ten
// seconds for a child to report ready and were failing on that budget rather
// than on anything they test. The cost is about twenty seconds on this package.
//
// The second module these cases install. It is the same artefact under a second
// namespace, which is what an operator running two modules has: two
// declarations, two processes, and one file on disk only because this example
// happens to be both of them.
const (
	otherModuleNamespace = "com.github.aux.acme.other"
	otherModuleType      = otherModuleNamespace + "/service-plugin"
	otherModuleGroup     = "/api/v1/modules/" + otherModuleNamespace
	otherModuleInstance  = "sink/other"
)

// TestAModuleKeepsAValueAndReadsItBack is ADR-20260827-rwq rule 2 on the real thing:
// the operator's own configuration file, the real database, the real
// supervision plane, a real second process, and the shipping HTTP surface.
//
// It is written as a module author would use the door, through the module's own
// routes, because a store proved against an in-process fake proves the fake.
func TestAModuleKeepsAValueAndReadsItBack(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	// A key that is not there is not an empty value.
	require.Equal(t, http.StatusNotFound, running.get(t, moduleGroup+"/records/spools/petg").status)

	// Kept.
	written := running.put(t, moduleGroup+"/records/spools/petg", `{"grams":840}`)
	require.Equal(t, http.StatusOK, written.status, written.body)
	require.Equal(t, uint64(1), versionIn(t, written.body))

	// And read back, through a second request, out of the host's own database.
	read := running.get(t, moduleGroup+"/records/spools/petg")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var held map[string]any
	require.NoError(t, json.Unmarshal([]byte(read.body), &held))
	require.Equal(t, `{"grams":840}`, held["value"])
	require.InDelta(t, 1.0, held["version"], 0)

	// A second write is a new version of one record.
	again := running.put(t, moduleGroup+"/records/spools/petg", `{"grams":700}`)
	require.Equal(t, http.StatusOK, again.status, again.body)
	require.Equal(t, uint64(2), versionIn(t, again.body))

	// Listed, sorted, and with no value in the answer.
	for _, key := range []string{"spools/pla", "spools/abs", "jobs/17"} {
		require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/"+key, "x").status)
	}

	listed := running.get(t, moduleGroup+"/records?prefix=spools/")
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.JSONEq(t, `{"keys":["spools/abs","spools/petg","spools/pla"]}`, listed.body)

	// Removed.
	require.Equal(t, http.StatusOK,
		running.call(t, http.MethodDelete, moduleGroup+"/records/spools/petg", "").status)
	require.Equal(t, http.StatusNotFound, running.get(t, moduleGroup+"/records/spools/petg").status)

	// Restarting the module leaves the records where they were. It is a
	// different process reading them, which is the whole reason they are on a
	// disk and not in the module's memory.
	restarted := running.call(t, http.MethodPost, "/api/v1/resources/"+moduleInstance+"/restart", "")
	require.Equal(t, http.StatusNoContent, restarted.status, restarted.body)

	require.Eventually(t, func() bool {
		return running.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the module never came back")

	survived := running.get(t, moduleGroup+"/records/spools/pla")
	require.Equal(t, http.StatusOK, survived.status, survived.body)
	require.Contains(t, survived.body, `"value":"x"`)

	running.stopAndWait(t)
}

// TestOneModuleReachesNoOtherModulesRecords is the containment on the real
// surface, with two modules running in two processes against one database.
//
// The attack is every shape a key could take that a reader might expect to
// escape a namespace: the other module's namespace spelled into the key, a
// relative path out of it, and the key the other module actually used. None of
// them is a permission the host refuses. Each one lands in the caller's own
// namespace, because the namespace is not in the key and there is no argument
// on the wire that carries one.
func TestOneModuleReachesNoOtherModulesRecords(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	require.Eventually(t, func() bool {
		return running.instanceState(t, otherModuleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the second module never reached ready")

	// The neighbour keeps something.
	require.Equal(t, http.StatusOK,
		running.put(t, otherModuleGroup+"/records/secret", "the other module's").status)

	// The two escapes that reach the module. Each one is a key, stored under the
	// caller's own namespace, and invisible to the neighbour.
	for _, escape := range []string{"secret", otherModuleNamespace + "/secret"} {
		read := running.get(t, moduleGroup+"/records/"+escape)
		require.Equal(t, http.StatusNotFound, read.status,
			"%q read something out of the other module's namespace: %s", escape, read.body)

		// Writing it puts a record in the caller's own namespace, where the
		// neighbour cannot see it.
		require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/"+escape, "mine").status)

		theirs := running.get(t, otherModuleGroup+"/records/"+escape)
		if escape == "secret" {
			require.Contains(t, theirs.body, "the other module's",
				"the neighbour's own record was overwritten from another namespace")

			continue
		}

		require.Equal(t, http.StatusNotFound, theirs.status,
			"%q crossed into the other module's namespace: %s", escape, theirs.body)
	}

	// The third escape never gets that far. A path climbing out of the module's
	// own group is refused by the router before any module is called, which is
	// ADR-20260827-wmv rule 12's containment doing the work first.
	climbed := running.put(t, moduleGroup+"/records/../../"+otherModuleNamespace+"/records/secret", "mine")
	require.Equal(t, http.StatusNotFound, climbed.status, climbed.body)
	require.Contains(t, running.get(t, otherModuleGroup+"/records/secret").body, "the other module's")

	// Each module lists its own keys and no others. Both namespaces hold a key
	// spelled "secret" now, and each one reads its own: the same name in two
	// namespaces is two records, which is the whole shape of the containment.
	mine := running.get(t, moduleGroup+"/records")
	require.Equal(t, http.StatusOK, mine.status, mine.body)
	require.JSONEq(t, `{"keys":["`+otherModuleNamespace+`/secret","secret"]}`, mine.body)

	require.Contains(t, running.get(t, moduleGroup+"/records/secret").body, `"value":"mine"`,
		"the caller read the neighbour's record under its own key")

	theirs := running.get(t, otherModuleGroup+"/records")
	require.Equal(t, http.StatusOK, theirs.status, theirs.body)
	require.JSONEq(t, `{"keys":["secret"]}`, theirs.body)

	running.stopAndWait(t)
}

// TestTwoWritersOnOneRecordAreToldWhoLost is what CompareAndPut is for, driven
// through the module's own surface: every writer states the version it read,
// and one of them is told it lost rather than silently overwriting the other.
func TestTwoWritersOnOneRecordAreToldWhoLost(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/leader", "first").status)

	const writers = 6

	var (
		ready   sync.WaitGroup
		start   = make(chan struct{})
		results = make(chan int, writers)
	)

	ready.Add(writers)

	// The goroutines assert nothing. They race the writes and hand back a
	// status, and the counting happens on the test's own goroutine, because a
	// failed assertion off it stops the wrong stack.
	for i := range writers {
		go func() {
			ready.Done()
			<-start

			// Every writer states version 1, which is what each of them read.
			results <- raced(fmt.Sprintf("http://%s%s/records/leader?version=1", running.addr, moduleGroup),
				fmt.Sprintf("writer-%d", i))
		}()
	}

	ready.Wait()
	close(start)

	var won, lost int

	for range writers {
		switch status := <-results; status {
		case http.StatusOK:
			won++
		case http.StatusConflict:
			lost++
		default:
			t.Errorf("a racing write answered %d", status)
		}
	}

	require.Equal(t, 1, won, "a race on one record did not settle on one winner")
	require.Equal(t, writers-1, lost, "a loser was not told it lost")

	// The record moved by exactly the one write that won.
	final := running.get(t, moduleGroup+"/records/leader")
	require.Equal(t, http.StatusOK, final.status, final.body)
	require.Contains(t, final.body, `"version":2`)

	// A stale expectation stays refused. It is never retried into a win.
	require.Equal(t, http.StatusConflict,
		running.put(t, moduleGroup+"/records/leader?version=1", "late").status)

	running.stopAndWait(t)
}

// TestAModuleCannotFillTheHostsDisk is ADR-20260827-wgc rule 12's byte limit reached
// through the whole stack: the shipping router, the wire, a real second
// process, and the real database.
//
// A module that could fill the disk is the availability defect the required
// rule already taught this codebase about, so the refusal has to be the store's
// and not the loop's, and the deployment has to still be serving after it.
func TestAModuleCannotFillTheHostsDisk(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	value := strings.Repeat("a", stateapi.MaxValueBytes)

	var (
		written int
		refused int
	)

	// One more attempt than the quota can hold, so a loop that ran out is not
	// mistaken for a store that refused.
	for i := range stateapi.DefaultNamespaceBytes/stateapi.MaxValueBytes + 2 {
		got := running.put(t, fmt.Sprintf("%s/records/bulk/%04d", moduleGroup, i), value)

		if got.status == http.StatusInsufficientStorage {
			refused++
			require.Contains(t, got.body, strconv.Itoa(stateapi.DefaultNamespaceBytes),
				"the refusal does not name the limit, so an author cannot read it")

			break
		}

		require.Equal(t, http.StatusOK, got.status, got.body)
		written++
	}

	require.Positive(t, written)
	require.Equal(t, 1, refused, "the namespace never refused a write")

	// The deployment is still serving, and so is the module.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, running.get(t, moduleGroup+"/spools").status)

	// One namespace's quota is its own. The neighbour writes.
	require.Eventually(t, func() bool {
		return running.instanceState(t, otherModuleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the second module never reached ready")

	neighbour := running.put(t, otherModuleGroup+"/records/first", value)
	require.Equal(t, http.StatusOK, neighbour.status,
		"one namespace's quota refused another namespace's first write: %s", neighbour.body)

	// And a delete makes room again, so a full namespace is not a module that
	// has to be reinstalled.
	require.Equal(t, http.StatusOK,
		running.call(t, http.MethodDelete, moduleGroup+"/records/bulk/0000", "").status)

	room := running.put(t, fmt.Sprintf("%s/records/bulk/%04d", moduleGroup, written), value)
	require.Equal(t, http.StatusOK, room.status, "deleting a record did not make room: %s", room.body)

	running.stopAndWait(t)
}

// TestAModuleHoldsTheRoomTheOperatorGranted is ADR-20261007-br4 through the whole
// stack: a declaration granting ten mebibytes reaches the record store through
// a real process, so the module writes past the default and is still refused
// at the limit it was granted, which the refusal names.
func TestAModuleHoldsTheRoomTheOperatorGranted(t *testing.T) {
	dir := t.TempDir()
	installed := installModule(t)

	const grantedMiB = 10

	running := start(t, dir, moduleConfig(dir, installed)+lines(
		"    other:",
		"      type: "+otherModuleType,
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
		"        records_mib: "+strconv.Itoa(grantedMiB),
	))

	require.Eventually(t, func() bool {
		return running.instanceState(t, otherModuleInstance) == condition.StateReady().String() &&
			running.instanceState(t, "db/records") == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the granted module or the record backend never reached ready")

	value := strings.Repeat("a", stateapi.MaxValueBytes)
	limit := grantedMiB * stateapi.NamespaceMiB

	var written int

	for i := range limit/stateapi.MaxValueBytes + 2 {
		got := running.put(t, fmt.Sprintf("%s/records/bulk/%04d", otherModuleGroup, i), value)

		if got.status == http.StatusInsufficientStorage {
			require.Contains(t, got.body, strconv.Itoa(limit),
				"the refusal names a limit other than the one granted")

			break
		}

		require.Equal(t, http.StatusOK, got.status, got.body)
		written++
	}

	require.Greater(t, written*stateapi.MaxValueBytes, stateapi.DefaultNamespaceBytes,
		"the granted module was held to the default")
	require.Less(t, written, limit/stateapi.MaxValueBytes+2, "the namespace never refused a write")

	// The operator reads the grant beside what the module holds.
	accounting := running.get(t, "/api/v1/state/namespaces")
	require.Equal(t, http.StatusOK, accounting.status, accounting.body)
	require.Contains(t, accounting.body, `"limit":`+strconv.Itoa(limit))

	running.stopAndWait(t)
}

// TestAKeyTheDoorRefusesNeverReachesTheStore holds the key grammar on the real
// surface. A module that put a newline in a key would forge a line in the
// operator's own listing, and the length bound is what stops one enormous name
// standing in for a namespace's worth of them.
func TestAKeyTheDoorRefusesNeverReachesTheStore(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	// A control character is not among these. The host's own router refuses a
	// request path carrying one before any module is called, so a case over it
	// would be a case about the router. The grammar itself is held against the
	// door, in sdk/stateapi and internal/extension/state.
	for name, key := range map[string]string{
		"over the length":  strings.Repeat("k", stateapi.MaxKeyBytes+1),
		"a trailing slash": "spools/",
	} {
		got := running.put(t, moduleGroup+"/records/"+key, "x")
		require.Equal(t, http.StatusBadRequest, got.status, "%s: %s", name, got.body)
	}

	// The store holds nothing, so none of them reached it.
	listed := running.get(t, moduleGroup+"/records")
	require.JSONEq(t, `{"keys":[]}`, listed.body)

	running.stopAndWait(t)
}

// TestAModuleCallingTheStoreBeforeItIsReadyIsToldToRetry is ADR-20260827-rwq rule 5 on
// the path a record travels, which is the opposite direction to a route.
//
// The window is real and a module reaches it without trying. The host asks a
// child plugin.info during the launch, so the child's own answer to that call
// is written while the instance is still starting, and a warm-up read is the
// natural thing for an author to put there. The example module makes exactly
// that call and keeps what it was told.
//
// The assertion is on the sentinel and not on the fact of an error. A host that
// let the call through and failed it for some other reason would produce an
// error too, and that host is the one this rule exists to refuse: a module
// whose own migration is still running must not be reading the store.
func TestAModuleCallingTheStoreBeforeItIsReadyIsToldToRetry(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	said := running.get(t, moduleGroup+"/startup")
	require.Equal(t, http.StatusOK, said.status, said.body)

	var answered struct {
		Early string `json:"early_state_call"`
	}

	require.NoError(t, json.Unmarshal([]byte(said.body), &answered), said.body)

	require.Contains(t, answered.Early, "has not marked this module ready",
		"a call made before the module was ready was answered out of the store")
	require.Contains(t, answered.Early, moduleInstanceRef,
		"the refusal does not say which instance is not ready")
	require.NotContains(t, answered.Early, stateapi.HostFailed,
		"the module was told the host broke, which is not something it can act on")

	// And it is a refusal the module retries out of, not a door that stays
	// shut. The same key is readable now, and writable, once the instance is
	// serving.
	require.Equal(t, http.StatusNotFound, running.get(t, moduleGroup+"/records/startup/probe").status)
	require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/startup/probe", "warm").status)

	running.stopAndWait(t)
}

// TestABurstOfStateCallsFromOneModuleIsAccountedFor is a burst of state calls
// well over ADR-20260827-wgc rule 12's concurrency limit, made from inside a
// live module.
//
// The burst is made inside the module, because that is the only place it can be
// made. One HTTP request into a module is one call for the routes bulkhead
// however many state calls the module makes behind it, so a burst driven from
// this test would be bounded by the wrong limit and would prove nothing about
// this one.
//
// The bound itself is not reachable from here and this case does not claim it.
// The calls arrive on one wire. On a runner with one CPU the host finishes each
// call before it reads the next, so it may never hold two at once.
// TestAModuleAtItsStateLimitIsRefusedAndNotQueued in internal/app/resourcehost
// is where the refusal is proved, and TestARefusalKeepsItsMeaningAcrossTheWire
// in sdk/stateapi is where the module reads it as stateapi.ErrBusy. What this
// asserts is that every call was accounted for and none of them failed.
func TestABurstOfStateCallsFromOneModuleIsAccountedFor(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	const calls = supervisor.DefaultConcurrency * 8

	got := running.call(t, http.MethodPost,
		moduleGroup+"/state-burst?n="+strconv.Itoa(calls), "")
	require.Equal(t, http.StatusOK, got.status, got.body)

	var counted struct {
		Kept    int    `json:"kept"`
		Busy    int    `json:"busy"`
		Failed  int    `json:"failed"`
		Refusal string `json:"refusal"`
	}

	require.NoError(t, json.Unmarshal([]byte(got.body), &counted), got.body)

	require.Zero(t, counted.Failed, "a call failed for a reason that is not the bound: %s", counted.Refusal)
	require.Equal(t, calls, counted.Kept+counted.Busy, "the module lost track of a call")
	require.Positive(t, counted.Kept, "the bound refused everything, which is a closed door and not a limit")

	// A refused call wrote nothing. Without this the case would pass against a
	// host that counted a refusal and then ran the statement anyway.
	listed := running.get(t, moduleGroup+"/records?prefix=burst/")
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	var keys struct {
		Keys []string `json:"keys"`
	}

	require.NoError(t, json.Unmarshal([]byte(listed.body), &keys), listed.body)
	require.Len(t, keys.Keys, counted.Kept,
		"the store holds a different number of records than the module was told it kept")

	// The deployment is still serving and the module can still write, which is
	// the whole reason the excess is refused rather than queued.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/after-the-burst", "x").status)

	running.stopAndWait(t)
}

// TestAModulesRoutesAndItsRecordsAreBoundedSeparately is the reason the state
// bound is a semaphore of its own rather than the supervisor's.
//
// A module's route handler is the ordinary place a state call is made from, and
// that handler is already running inside a supervisor.Call holding one of the
// instance's bulkhead slots. One semaphore across both paths would leave every
// slot held by a handler that is about to ask for a slot, and the deployment
// would move again only when the call budget expired on each of them.
//
// The route these requests use does its own work first and reads a record
// after, which is the ordering a module route has and the ordering that makes
// the failure certain rather than likely. There are three times as many
// requests at once as the routes bulkhead is wide, so the slots are all held
// before the first record is written.
func TestAModulesRoutesAndItsRecordsAreBoundedSeparately(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	const requests = supervisor.DefaultConcurrency * 3

	var (
		ready   sync.WaitGroup
		start   = make(chan struct{})
		results = make(chan int, requests)
	)

	ready.Add(requests)

	for i := range requests {
		go func() {
			ready.Done()
			<-start

			results <- raced(fmt.Sprintf("http://%s%s/state-late/shared/%02d",
				running.addr, moduleGroup, i), "held")
		}()
	}

	ready.Wait()
	close(start)

	for range requests {
		// A shared semaphore does not fail these quickly. It holds each one for
		// the whole call budget and then answers 503, so a red here is slow
		// before it is red.
		require.Equal(t, http.StatusOK, <-results,
			"a module request that made a state call did not complete")
	}

	listed := running.get(t, moduleGroup+"/records?prefix=shared/")
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	var keys struct {
		Keys []string `json:"keys"`
	}

	require.NoError(t, json.Unmarshal([]byte(listed.body), &keys), listed.body)
	require.Len(t, keys.Keys, requests)

	running.stopAndWait(t)
}

// raced makes one write with no assertion in it, for a goroutine that is not
// the test's own. A transport failure comes back as zero, which no branch above
// counts, so it fails the count rather than passing quietly.
func raced(url, body string) int {
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return 0
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0
	}

	defer func() { _ = response.Body.Close() }()

	_, _ = io.Copy(io.Discard, response.Body)

	return response.StatusCode
}

// startModules runs the real composition root over a configuration declaring
// two modules, and waits for the first one to be serving.
//
// It is app.Run and not a harness: the database is opened the way a deployment
// opens it, the record store is migrated where a deployment migrates it, and
// both modules are processes this test did not start itself.
func startModules(t *testing.T, dir string) *process {
	t.Helper()

	installed := installModule(t)

	running := start(t, dir, moduleConfig(dir, installed)+lines(
		"    other:",
		"      type: "+otherModuleType,
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
	))

	require.Eventually(t, func() bool {
		return running.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	// And the record backend, which is a supervised instance of its own now and
	// starts on its own scatter. A module whose backend is not serving yet is
	// told so and retries, which is what
	// TestAModuleCallingTheStoreBeforeItIsReadyIsToldToRetry is about; every
	// other case here is about what the door does once the deployment is up.
	require.Eventually(t, func() bool {
		return running.instanceState(t, "db/records") == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared record backend never reached ready")

	return running
}

// versionIn reads the version out of one of the module's answers.
func versionIn(t *testing.T, body string) uint64 {
	t.Helper()

	var answered struct {
		Version uint64 `json:"version"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &answered), body)

	return answered.Version
}

// TestTheRecordIsInTheHostsDatabaseUnderTheModulesNamespace reads the row
// itself, out of the file the operator's configuration named, after the process
// that wrote it has stopped.
//
// It is here because every other case in this file asks the module what it
// stored, and a module that answered out of its own memory would pass all of
// them. This one asks the disk, with a statement of its own, and reads the
// namespace column the module never named.
func TestTheRecordIsInTheHostsDatabaseUnderTheModulesNamespace(t *testing.T) {
	dir := t.TempDir()
	running := startModules(t, dir)

	require.Equal(t, http.StatusOK, running.put(t, moduleGroup+"/records/kept", "value").status)

	running.stopAndWait(t)

	// Where the deployment actually put it: the file the declared db instance
	// names, which is the instance's own and never the graph's (ADR-20260901-rf2 rule
	// 8). The module never learns which file it is, or that there is one.
	path := filepath.ToSlash(filepath.Join(dir, "records.db"))

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	var (
		namespace string
		key       string
		value     []byte
		version   uint64
	)

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT namespace, record_key, record_value, version FROM module_state`).
		Scan(&namespace, &key, &value, &version))

	require.Equal(t, moduleNamespace, namespace,
		"the record is not under the namespace the operator's declaration produced")
	require.Equal(t, "kept", key)
	require.Equal(t, "value", string(value))
	require.Equal(t, uint64(1), version)

	// The adapter ran the migration inside its own supervised start, and it
	// recorded that it did. A module ships none and could not have written this
	// row (ADR-20260827-rwq rule 5, as ADR-20260901-rf2 rule 9 narrows rule 4).
	var applied int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM module_state_migration`).Scan(&applied))
	require.Positive(t, applied)
}
