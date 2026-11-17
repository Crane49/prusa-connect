package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// The module these cases install. The namespace is what extension.NamespaceOf
// renders for github.com/acme/planner, so the address a caller reaches here is the
// address a real module path produces; the name after the slash is the type,
// and the artefact on disk is named after it.
const (
	moduleNamespace = "com.github.aux.acme.planner"
	moduleType      = moduleNamespace + "/service-plugin"
	moduleGroup     = "/api/v1/modules/" + moduleNamespace
	moduleInstance  = "sink/planner"
)

// outsider is a principal the graph knows nothing about: no org role, no grant
// on any kind, no membership. It is what a caller who holds nothing looks like.
const outsider = "outsider"

// TestAModuleRouteIsMountedUnderItsNamespaceAndAuthorized is the gate ADR-20260827-wmv
// rules 12 and 13 owe, on the real thing: the operator's own configuration
// file, the real resource store, the real supervision plane, a real second
// process, the real authorization graph, and the shipping HTTP surface.
//
// It is one case because the mount and the refusal are one property. A mode of the retired server
// shipped the mount without the refusal, which is the shape rule 13 names, and
// a case that proved only the address would pass against it.
func TestAModuleRouteIsMountedUnderItsNamespaceAndAuthorized(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	served := serveModule(t, dir)

	// The arming half. Without it every assertion below is about a module that
	// was never running, and the refusals would all pass over an empty plane.
	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	// Reached, by a caller who holds the kind, at the address rule 12 names.
	answered := served.reader.get(t, moduleGroup+"/spools")
	require.Equal(t, http.StatusOK, answered.status, answered.body)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(answered.body), &body))

	require.Contains(t, body, "spools", "the answer did not come from the module")
	require.Equal(t, "user:"+config.DefaultPrincipal, body["principal"],
		"the module was not told who the host authorized")

	// A write, which takes the other action.
	written := served.reader.call(t, http.MethodPost, moduleGroup+"/spools", `{"name":"petg"}`)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	// The module's own 404, which is what proves the path arrived with the
	// group taken off and the module owns everything under it.
	inner := served.reader.get(t, moduleGroup+"/nothing-here")
	require.Equal(t, http.StatusNotFound, inner.status, inner.body)
	require.Contains(t, inner.body, `"module_saw":"/nothing-here"`,
		"the module was handed the group prefix, or the answer is not the module's")

	// Refused, for a caller the graph gives nothing. It is the same module, the
	// same address and the same process.
	refused := served.outsider.get(t, moduleGroup+"/spools")
	require.Equal(t, http.StatusForbidden, refused.status, refused.body)
	require.Contains(t, refused.body, "core.auth/ACCESS_DENIED",
		"a module route refused a caller for some reason other than the graph")

	// And on the write half, which asks a different action.
	refusedWrite := served.outsider.call(t, http.MethodPost, moduleGroup+"/spools", `{"name":"petg"}`)
	require.Equal(t, http.StatusForbidden, refusedWrite.status, refusedWrite.body)
}

// TestTheNamespaceListingIsAuthorizedLikeTheModuleItDescribes is ADR-20260827-wgc rule
// 15's first endpoint, and both halves of it: what an operator reads, and what
// it does not say to a caller who may not read it.
//
// The permission is the module's own. A namespace is not an authorization
// object and ADR-20260827-rwq rule 9 says it never becomes one, so the only thing there
// is to decide against is the instance that claims it. That makes the answer
// the same one the caller would get by asking the module: a caller who may see
// what a module serves may see how much of the disk it is using.
func TestTheNamespaceListingIsAuthorizedLikeTheModuleItDescribes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	served := serveModule(t, dir)

	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	// Nothing kept yet, so the listing is empty rather than absent.
	require.Empty(t, namespacesSeenBy(t, served.reader), "a namespace holding no record was listed")

	const secret = "the grams and the colour"

	require.Equal(t, http.StatusOK,
		served.reader.put(t, moduleGroup+"/records/spools/petg", secret).status)

	listed := served.reader.get(t, namespacesPath)
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	items := namespacesOf(t, listed.body)
	require.Len(t, items, 1)
	require.Equal(t, moduleNamespace, items[0].Namespace)
	require.Equal(t, moduleInstanceRef, items[0].Instance)
	require.Equal(t, int64(1), items[0].Keys)
	require.Positive(t, items[0].Bytes)

	// No stored value is on this surface, which rule 15 states and which is the
	// difference between disk accounting and a second read door onto a module's
	// data.
	require.NotContains(t, listed.body, secret,
		"the listing published what the module stored")

	// A caller the graph gives nothing reads an empty listing. It is empty and
	// not refused, because the operation is a listing: a 403 would say that
	// there is something here to be refused, and the resource listing beside it
	// answers the same way for the same reason.
	refused := served.outsider.get(t, namespacesPath)
	require.Equal(t, http.StatusOK, refused.status, refused.body)
	require.Empty(t, namespacesOf(t, refused.body),
		"a caller who holds nothing on the module read what the module is keeping")
}

// namespacesPath is the operator's view of what the modules are keeping.
const namespacesPath = "/api/v1/state/namespaces"

// moduleInstanceRef is the module's instance as the listing names it, which is
// ref.Ref's own rendering and not the API path's.
const moduleInstanceRef = "sink:planner"

// namespacesSeenBy reads the listing one caller gets.
func namespacesSeenBy(t *testing.T, who *process) []httpapi.StateNamespace {
	t.Helper()

	listed := who.get(t, namespacesPath)
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	return namespacesOf(t, listed.body)
}

// namespacesOf reads the items out of one answer.
func namespacesOf(t *testing.T, body string) []httpapi.StateNamespace {
	t.Helper()

	var answered struct {
		Items []httpapi.StateNamespace `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &answered), body)

	return answered.Items
}

// TestAModuleReachesNothingOutsideItsOwnGroupOnTheRealSurface is the
// containment half against the shipping router: a module claiming a namespace
// nothing derived, a path escaping the group, and the core paths those
// escapes name still answering as themselves.
func TestAModuleReachesNothingOutsideItsOwnGroupOnTheRealSurface(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	served := serveModule(t, dir)

	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	unrouted := []string{
		"/api/v1/modules/com.github.aux.acme.other/spools",
		"/api/v1/modules/core/spools",
		"/api/v1/modules/acme/spools",
		moduleGroup + "/../../health",
		moduleGroup + "/../../../openapi.json",
	}

	for _, path := range unrouted {
		answered := served.reader.get(t, path)
		require.Equal(t, http.StatusNotFound, answered.status, "%s answered %s", path, answered.body)
	}

	// The paths those escapes named are still this server's own, and the
	// module has not shadowed one.
	require.Equal(t, http.StatusOK, served.reader.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, served.reader.get(t, "/openapi.json").status)

	// A module that panics while serving costs its own request and nothing
	// else: the child recovers on the wire, the instance stays up, and the
	// server answers the next request through the same mount.
	panicked := served.reader.get(t, moduleGroup+"/panic")
	require.GreaterOrEqual(t, panicked.status, http.StatusInternalServerError, panicked.body)

	require.Equal(t, http.StatusOK, served.reader.get(t, "/api/v1/health").status,
		"a panicking module took the server with it")

	require.Eventually(t, func() bool {
		return served.reader.get(t, moduleGroup+"/spools").status == http.StatusOK
	}, settle, 20*time.Millisecond, "the module never served again after one handler panicked")
}

// TestASecondModuleClaimingOneNamespaceFailsAloneAndTheServerServes is rule
// 12's second sentence: one mount, one claim, and the refusal is the second
// instance's own.
//
// Two modules answering under one prefix would make the address a caller
// reaches depend on which instance started first. The refusal has to leave the
// first module serving and the process up, which is the shape the required rule
// already established.
func TestASecondModuleClaimingOneNamespaceFailsAloneAndTheServerServes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	installed := installModule(t)

	running := start(t, dir, moduleConfig(dir, installed)+lines(
		"    twin:",
		"      type: "+moduleType,
		"      enabled: true",
		"      runtime: service",
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
	))

	require.Eventually(t, func() bool {
		return running.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the first module never reached ready")

	// The second instance is declared and never running. It is on the plane as
	// a declaration, and it is not serving.
	require.NotEqual(t, condition.StateReady().String(), running.instanceState(t, "sink/twin"),
		"a second module claiming one namespace was started, so the mount is first past the post")

	// The process is serving, and the module that got there first is the one
	// answering under the namespace.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, running.get(t, moduleGroup+"/spools").status)

	running.stopAndWait(t)
}

// TestAModuleMountedAtARootPrefixIsReachedThereOnTheRealSurface is ADR-20260827-wmv
// rule 12's second sentence against the shipping router: the operator writes
// service.mount, the module answers under that prefix with the caller the host
// authorized, its document is served rebased at both addresses, the namespace
// path stops answering routes, and a caller who holds nothing is refused at the
// new address the way they were at the old one.
func TestAModuleMountedAtARootPrefixIsReachedThereOnTheRealSurface(t *testing.T) {
	t.Parallel()

	const mount = "/graphql"

	dir := t.TempDir()
	served := serveModuleAt(t, dir, mount)

	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	// Reached at the prefix, with the prefix taken off and the principal on.
	answered := served.reader.get(t, mount+"/spools")
	require.Equal(t, http.StatusOK, answered.status, answered.body)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(answered.body), &body))
	require.Contains(t, body, "spools", "the answer did not come from the module")
	require.Equal(t, "user:"+config.DefaultPrincipal, body["principal"],
		"the module was not told who the host authorized")

	inner := served.reader.get(t, mount+"/nothing-here")
	require.Equal(t, http.StatusNotFound, inner.status, inner.body)
	require.Contains(t, inner.body, `"module_saw":"/nothing-here"`,
		"the module was handed the prefix, or the answer is not the module's")

	// One address. The namespace path no longer routes to the module.
	moved := served.reader.get(t, moduleGroup+"/spools")
	require.Equal(t, http.StatusNotFound, moved.status, moved.body)
	require.Contains(t, moved.body, "ROUTE_NOT_FOUND")

	// The document, rebased under the prefix, at both addresses.
	for _, address := range []string{mount + "/openapi.json", moduleGroup + "/openapi.json"} {
		document := served.reader.get(t, address)
		require.Equal(t, http.StatusOK, document.status, "%s: %s", address, document.body)
		require.Contains(t, document.body, `"`+mount+`/spools"`, "%s is not rebased under the prefix", address)
		require.NotContains(t, document.body, `"`+moduleGroup+`/spools"`, "%s is rebased under the namespace", address)
	}

	// The listing says where the module went.
	listed := served.reader.get(t, "/api/v1/resources/"+moduleInstance)
	require.Equal(t, http.StatusOK, listed.status, listed.body)

	var instance httpapi.ResourceInstance
	require.NoError(t, json.Unmarshal([]byte(listed.body), &instance))
	require.Equal(t, mount, instance.Mount)
	require.Equal(t, moduleNamespace, instance.Namespace)
	require.Equal(t, moduleGroup+"/openapi.json", instance.OpenAPIURL)

	// The core's own paths are still the core's.
	require.Equal(t, http.StatusOK, served.reader.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, served.reader.get(t, "/openapi.json").status)

	// Refused, for a caller the graph gives nothing, on every address.
	for _, address := range []string{mount + "/spools", mount, mount + "/openapi.json"} {
		refused := served.outsider.get(t, address)
		require.Equal(t, http.StatusForbidden, refused.status, "%s: %s", address, refused.body)
		require.Contains(t, refused.body, "core.auth/ACCESS_DENIED")
	}
}

// servedModule is one deployment with two HTTP surfaces over one graph, one
// store and one supervision plane, each serving every request as its own
// principal. It is the shape twoAdministrators uses and for the same reason: a
// caller who holds nothing cannot be produced by taking a grant away from the
// only caller there is.
type servedModule struct {
	reader   *process
	outsider *process
}

// serveModule wires the deployment the cases above drive: the real store, the
// real plane, the real graph and the shipping HTTP surface, with a module
// declared in the operator's own file.
func serveModule(t *testing.T, dir string) servedModule {
	t.Helper()

	return serveModuleAt(t, dir, "")
}

// serveModuleAt is serveModule with the module mounted at a root prefix the
// operator named, and under its namespace when mount is empty.
func serveModuleAt(t *testing.T, dir, mount string) servedModule {
	t.Helper()

	installed := installModule(t)

	write(t, filepath.Join(dir, "config.yaml"), moduleConfigAt(dir, installed, mount))

	graph, records := graphAndRecordsAt(t, filepath.Join(dir, "authz.db"))

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Interval:   stream.MinInterval,
		Authorizer: visibility{graph: graph},
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	types, err := registry.New()
	require.NoError(t, err)

	host, err := resourcehost.New(resourcehost.Config{
		Registry: types,
		Logger:   discard(),
		State:    records.Records,
		// Wired exactly as app.go wires it. Without it a module that declares
		// an endpoint would fail to start, and every case here would be about a
		// module that was never running.
		Events: instanceEmitter(hub, fleetMeters{}),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	store, err := resource.New(resource.Config{Paths: resource.Paths{
		File:    filepath.Join(dir, "config.yaml"),
		Overlay: filepath.Join(dir, "config.d", "resources.json"),
	}, Plane: host, Graph: graph})
	require.NoError(t, err)

	require.NoError(t, store.Reload(t.Context()))

	fleet := newDevices(graph, recordsOf(t, graph), hub, discard(), bootstrapOrg, staging{host: host}, fleetMeters{}, 0)
	t.Cleanup(fleet.Close)

	surface := func(principal string) *process {
		t.Helper()

		api, buildErr := httpapi.New(httpapi.Config{
			Version:    "test",
			Logger:     discard(),
			Principals: graphPrincipals{inner: unauthenticated{subject: principal}},
			Sessions:   noIssuer{},
			Accounts:   noAccounts{},
			Readiness:  readiness{store: store, host: host},
			Resources:  store,
			Presets:    store,
			Endpoints:  store,
			Grants:     graph,
			Devices:    fleet,
			Events:     hub,
			Stores:     storagePlane{graph: graph, stores: stores{host: host}},
			// The three fields under test, wired exactly as app.go wires them.
			Modules:      host,
			ModuleAccess: graph,
			State:        func() (httpapi.StateStore, error) { return recordAccounting{records}, nil },
		})
		require.NoError(t, buildErr)

		served := httptest.NewServer(api.Handler())
		t.Cleanup(served.Close)

		done := make(chan error, 1)
		done <- nil

		return &process{addr: served.Listener.Addr().String(), cancel: func() {}, done: done}
	}

	// The stranger holds nothing, which is asserted rather than assumed: a
	// bootstrap that happened to grant every principal would make the refusal
	// below prove nothing.
	who, err := authz.NewSubject(authz.TypeUser, outsider)
	require.NoError(t, err)

	kind, err := authz.NewObject(authz.TypeResourceKind, "sink")
	require.NoError(t, err)

	held, err := graph.ObjectActions(t.Context(), who, kind)
	require.NoError(t, err)
	require.Empty(t, held, "the outsider already holds actions on the module's kind")

	return servedModule{reader: surface(config.DefaultPrincipal), outsider: surface(outsider)}
}

// moduleConfig is the operator's file: a module declared the only way ADR-20260827-wmv
// rule 5 permits one, out of process and under a namespace the core does not
// hold.
func moduleConfig(dir string, installed *installedModule) string {
	return moduleConfigAt(dir, installed, "")
}

// moduleConfigAt is moduleConfig with the module's routes at a root prefix,
// which is the mount key on its service block.
func moduleConfigAt(dir string, installed *installedModule, mount string) string {
	declared := lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		// The record backend a module keeps its namespace in. It is a declared
		// instance of kind db now (ADR-20260901-rf2 rule 1), on a file of its own, and
		// a deployment that declares none serves its modules no state.
		"  db:",
		"    records:",
		"      type: sqlite",
		"      enabled: true",
		"      config:",
		"        path: "+slashed(filepath.Join(dir, "records.db")),
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
		"      service:",
		"        path: "+slashed(installed.dir),
		"        checksum: sha256:"+installed.checksum,
		"        contract_major: 1",
		// What this instance may put on the host's own event stream. The
		// operator writes the name; the host writes the namespace in front of
		// it. Neither half is the module's.
		"        events:",
		"          changed: spool.changed",
	)

	if mount != "" {
		declared += lines("        mount: " + mount)
	}

	return declared
}

// installedModule is the artefact as an operator has it on disk.
type installedModule struct {
	dir      string
	checksum string
}

// servicePlugin is examples/service-plugin, built once for the whole package.
var servicePlugin = &artefact{pkg: "./examples/service-plugin", name: "service-plugin"}

// installModule is examples/service-plugin as the declaration names it: the
// directory the host launches it from and the checksum it verifies first.
func installModule(t *testing.T) *installedModule {
	t.Helper()

	dir, checksum := servicePlugin.install(t)

	return &installedModule{dir: dir, checksum: checksum}
}

// recordAccounting is the projection recordDoor makes in production, over a
// backend a case built for itself rather than over the supervision plane.
type recordAccounting struct{ backend stateapi.Backend }

func (r recordAccounting) Namespaces(ctx context.Context) ([]httpapi.StateNamespace, error) {
	held, err := r.backend.Namespaces(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]httpapi.StateNamespace, 0, len(held))
	for _, one := range held {
		out = append(out, httpapi.StateNamespace{
			Namespace: one.Namespace, Keys: one.Keys, Bytes: one.Bytes,
		})
	}

	return out, nil
}
