package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/db/sqlite"
	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// newGraph is a real authorization graph on a database of this test's own,
// bootstrapped for the principal an unauthenticated deployment serves.
func newGraph(t *testing.T) *authz.Store {
	t.Helper()

	return graphAt(t, filepath.Join(t.TempDir(), "authz.db"))
}

func graphAt(t *testing.T, path string) *authz.Store {
	t.Helper()

	graph, _ := graphAndRecordsAt(t, path)

	return graph
}

// graphAndRecordsAt opens the two schemas the way a deployment opens them now:
// the graph on the host's own handle, and the module record store as the
// adapter it became, on a file of its own (ADR-20260901-rf2 rules 8 and 17).
func graphAndRecordsAt(t *testing.T, path string) (*authz.Store, stateapi.Backend) {
	t.Helper()

	core := config.Default()
	core.Authz.Database = path

	host := openTestHost(t, core)

	settings, err := json.Marshal(map[string]any{"path": path + ".records"})
	require.NoError(t, err)

	records, err := sqlite.NewRecords(stateapi.ProviderSpec{
		ID: "records", Type: sqlite.Type, Config: settings,
	})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, records.Close()) })

	return host.graph, records
}

// TestAFirstRunCreatesTheGraphAndWritesTheFirstGrant is what a fresh install
// does: the file is not there, it is created, it starts empty, and the
// principal the server serves every request as can act. Nothing is fetched, so
// this is also the air-gapped case.
func TestAFirstRunCreatesTheGraphAndWritesTheFirstGrant(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "authz.db")

	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)

	graph := graphAt(t, path)

	_, err = os.Stat(path)
	require.NoError(t, err, "a first run leaves no database behind")

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	allowed, err := graph.Can(t.Context(), owner, authz.ActionConfigureResource, kindObject(t, ref.KindStorage))
	require.NoError(t, err)
	require.True(t, allowed,
		"the principal an unauthenticated deployment serves every request as could do nothing")
}

// TestASecondStartDoesNotWidenAGraphSomebodyNarrowed is the property that keeps
// the first grant from being a way back in. An operator who withdrew it gets it
// back only by emptying the graph, which is a reinstall.
func TestASecondStartDoesNotWidenAGraphSomebodyNarrowed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")
	graph := graphAt(t, path)

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	tenant, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	admin, err := authz.NewTuple(tenant, authz.RelAdmin, owner)
	require.NoError(t, err)

	require.NoError(t, graph.Delete(t.Context(), admin))

	allowed, err := graph.Can(t.Context(), owner, authz.ActionViewResource, kindObject(t, ref.KindStorage))
	require.NoError(t, err)
	require.False(t, allowed)

	// The handle is reopened, which is what a restart is.
	restarted := graphAt(t, path)

	allowed, err = restarted.Can(t.Context(), owner, authz.ActionViewResource, kindObject(t, ref.KindStorage))
	require.NoError(t, err)
	require.False(t, allowed, "restarting rewrote a grant the operator had withdrawn")
}

// TestASecondStartDoesNotRebootstrapAnEmptiedGraph is the case the one above
// leaves untouched, and the one an operator can now reach: the grant API
// withdraws tuples one at a time, so revoking everything is a sequence of calls
// rather than a database edit. Every tuple goes, which is the state a check
// that counted tuples would read as a fresh install.
func TestASecondStartDoesNotRebootstrapAnEmptiedGraph(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")
	graph := graphAt(t, path)

	held, err := graph.Tuples(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, held)

	require.NoError(t, graph.Delete(t.Context(), held...))

	emptied, err := graph.Tuples(t.Context())
	require.NoError(t, err)
	require.Empty(t, emptied, "the revocation left tuples behind, so this case proves nothing")

	restarted := graphAt(t, path)

	after, err := restarted.Tuples(t.Context())
	require.NoError(t, err)
	require.Empty(t, after, "restarting handed the whole graph back to the configured principal")

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	allowed, err := restarted.Can(t.Context(), owner, authz.ActionViewResource, kindObject(t, ref.KindSink))
	require.NoError(t, err)
	require.False(t, allowed, "an emptied graph made the configured principal an admin again")
}

// TestAGrantWrittenThroughTheGraphWidensALiveSubscription is the other half of
// E10, over the real hub and the real graph. A subscription is attached, a
// grant is written through the same call the HTTP surface makes, and the reader
// is moved onto the new origin on the pump rather than at its next reconnect.
func TestAGrantWrittenThroughTheGraphWidensALiveSubscription(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		graph := newGraph(t)
		owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
		require.NoError(t, err)

		reader, err := authz.NewSubject(humanSubjectType, "viewer")
		require.NoError(t, err)

		require.NoError(t, graph.RegisterResources(t.Context(), instanceRef(t, ref.KindStorage, "local")))

		// What registers a printer is what places it in an org: no grant can be
		// administered on a printer nobody has put anywhere, because administering
		// one takes a grant on it.
		require.NoError(t, graph.UpsertDevice(t.Context(), "p-alpha", "Alpha"))
		require.NoError(t, graph.Write(t.Context(), tuple(t,
			authz.TypeDevice, "p-alpha", authz.RelOrg, "org:"+bootstrapOrg)))

		wrote, err := graph.WriteGrant(t.Context(), owner, tuple(t,
			authz.TypeResourceKind, ref.KindStorage.String(), authz.RelViewer, reader.String()))
		require.NoError(t, err)
		require.True(t, wrote)

		hub, err := stream.New(stream.Options{
			Site:       "local",
			Interval:   stream.MinInterval,
			Authorizer: visibility{graph: graph},
		})
		require.NoError(t, err)

		defer hub.Close()

		sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: reader.String()})
		require.NoError(t, err)

		defer sub.Close()

		require.Len(t, sub.Origins(), 1, "the reader starts on the one origin it was granted")

		before := graph.Epoch()

		wrote, err = graph.WriteGrant(t.Context(), owner, tuple(t,
			authz.TypeDevice, "p-alpha", authz.RelViewer, reader.String()))
		require.NoError(t, err)
		require.True(t, wrote)
		require.Greater(t, graph.Epoch(), before, "a grant written through the API moved no generation")

		require.Eventually(t, func() bool { return len(sub.Origins()) == 2 },
			4*stream.AuthorizeInterval, 20*time.Millisecond,
			"a grant written while the stream was open did not widen it")
	})
}

// tuple builds one edge through the vocabulary's own constructors.
func tuple(t *testing.T, typ authz.Type, id string, relation authz.Relation, subject string) authz.Tuple {
	t.Helper()

	object, err := authz.NewObject(typ, id)
	require.NoError(t, err)

	who, err := authz.ParseSubject(subject)
	require.NoError(t, err)

	out, err := authz.NewTuple(object, relation, who)
	require.NoError(t, err)

	return out
}

// TestTheEpochMovesWhenAGrantIsWritten is the graph half of E10. The stream
// hub reads this on every subscriber's tick and re-authorizes when it has
// moved, so a graph whose epoch never changes is a revocation nobody notices.
func TestTheEpochMovesWhenAGrantIsWritten(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	seen := visibility{graph: graph}

	before := seen.Epoch()

	require.NoError(t, graph.UpsertDevice(t.Context(), "p-alpha", "Alpha"))

	require.Greater(t, seen.Epoch(), before,
		"a write that changed the graph left the generation where it was")
}

// TestARevokedGrantNarrowsALiveSubscriptionToNothing is E10 end to end over the
// real hub and the real graph: the reader is attached, the grant is withdrawn,
// and the routing table is emptied on the pump rather than at the reader's next
// reconnect. The reader is narrowed and not dropped, because dropping only
// sends them round a reconnect that hands back the same empty stream and costs
// them the widening if the grant comes back.
func TestARevokedGrantNarrowsALiveSubscriptionToNothing(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		graph := newGraph(t)
		local := instanceRef(t, ref.KindStorage, "local")

		require.NoError(t, graph.RegisterResources(t.Context(), local))

		hub, err := stream.New(stream.Options{
			Site:       "local",
			Interval:   stream.MinInterval,
			Authorizer: visibility{graph: graph},
		})
		require.NoError(t, err)

		defer hub.Close()

		principal, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
		require.NoError(t, err)

		sub, err := hub.Subscribe(t.Context(), stream.SubscribeRequest{Principal: principal.String()})
		require.NoError(t, err)

		defer sub.Close()

		require.NotEmpty(t, sub.Origins(), "the bootstrapped principal may observe the declared instance")

		tenant, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
		require.NoError(t, err)

		admin, err := authz.NewTuple(tenant, authz.RelAdmin, principal)
		require.NoError(t, err)

		require.NoError(t, graph.Delete(t.Context(), admin))

		// The pump re-reads the graph on its idle tick, so the wait is that tick
		// and not a reconnection.
		require.Eventually(t, func() bool {
			return len(sub.Origins()) == 0
		}, 4*stream.AuthorizeInterval, 50*time.Millisecond,
			"a withdrawn grant left the instance routed to a live subscription")
	})
}

func kindObject(t *testing.T, kind ref.Kind) authz.Object {
	t.Helper()

	object, err := authz.NewObject(authz.TypeResourceKind, kind.String())
	require.NoError(t, err)

	return object
}

// grantsConfig is one storage instance and nothing else: the smallest
// deployment that has an origin to observe and a kind to hold grants on.
func grantsConfig(dir string) string {
	return `
core:
  log:
    level: error
resources:
  storage:
    local:
      type: filesystem
      enabled: true
      config:
        root: ` + slashed(filepath.Join(dir, "storage")) + `
`
}

// TestGrantsAreReadWrittenAndWithdrawnOverHTTP drives grant administration
// through the shipping binary's own surface. Without these routes, the first
// grant is the only grant an operator gets without editing the database.
func TestGrantsAreReadWrittenAndWithdrawnOverHTTP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, grantsConfig(dir))

	const (
		storage = "/api/v1/grants/resource_kind/storage"
		granted = `{"relation":"viewer","subject":"user:viewer"}`
	)

	held := running.get(t, storage)
	require.Equal(t, http.StatusOK, held.status, held.body)
	require.Contains(t, held.body, `"object":"resource_kind:storage","relation":"org","subject":"org:local"`,
		"the bootstrap grant is not readable through the surface that writes them")

	written := running.call(t, http.MethodPost, storage, granted)
	require.Equal(t, http.StatusCreated, written.status, written.body)

	repeated := running.call(t, http.MethodPost, storage, granted)
	require.Equal(t, http.StatusOK, repeated.status, "a grant the graph already held answered as created")

	held = running.get(t, storage)
	require.Contains(t, held.body, `"relation":"viewer","subject":"user:viewer"`)

	withdrawn := running.call(t, http.MethodDelete, storage+"?relation=viewer&subject=user:viewer", "")
	require.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)

	held = running.get(t, storage)
	require.NotContains(t, held.body, `"subject":"user:viewer"`)
}

// TestTheGrantSurfaceRefusesWhatNobodyHolds is the refusal half over HTTP. The
// deployment registers no printer, so the caller holds nothing on one and is
// told so in the same words whether the printer exists or not; an org the
// caller does not administer reads the same way, and the one org grant this
// surface refuses outright is the admin role.
//
// What it does not hold is what its name suggests. Nothing registers a printer
// in this deployment, so p-alpha and p-nowhere are both absent and the two
// refusals compared here are two absent cases rather than a present one and an
// absent one. The property is held one layer down, over a directory that has
// both, by authz.TestGrantRefusalsHideWhetherTheObjectExists. It can be held
// here when something registers a printer, which is an open task in
// the old backlog.
func TestTheGrantSurfaceRefusesWhatNobodyHolds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, grantsConfig(dir))

	refused := running.get(t, "/api/v1/grants/device/p-alpha")
	require.Equal(t, http.StatusForbidden, refused.status, refused.body)
	require.Contains(t, refused.body, "core.auth/ACCESS_DENIED")

	absent := running.get(t, "/api/v1/grants/device/p-nowhere")
	require.Equal(t,
		anonymised(strings.ReplaceAll(refused.body, "p-alpha", "<id>")),
		anonymised(strings.ReplaceAll(absent.body, "p-nowhere", "<id>")),
		"a caller learned which printers exist from the refusal")

	// The caller is this org's administrator, because Bootstrap wrote them as
	// one, and the admin role is still the grant they cannot confer.
	elevated := running.call(t, http.MethodPost, "/api/v1/grants/org/"+bootstrapOrg,
		`{"relation":"admin","subject":"user:viewer"}`)
	require.Equal(t, http.StatusForbidden, elevated.status, elevated.body)
	require.Contains(t, elevated.body, "core.auth/ACCESS_DENIED")

	// An org they do not administer is refused in the words of an org that is
	// not there, which is the same answer because there is no org directory.
	elsewhere := running.get(t, "/api/v1/grants/org/elsewhere")
	require.Equal(t, http.StatusForbidden, elsewhere.status, elsewhere.body)
	require.Contains(t, elsewhere.body, "core.auth/ACCESS_DENIED")
}

// traceID matches the one member of a problem document that is per request. Two
// refusals that differ only there are the same refusal.
var traceID = regexp.MustCompile(`"trace_id":"[0-9a-f]*"`)

func anonymised(problem string) string {
	return traceID.ReplaceAllString(problem, `"trace_id":"<trace>"`)
}

// TestAGrantWithdrawnOverHTTPNarrowsALiveSubscription is E10 end to end over
// the shipping HTTP surface: the reader is attached, a grant is withdrawn over
// HTTP, and what reaches the open stream afterwards is nothing.
//
// The narrowing is proved by a printer that stays dialled in after the
// withdrawal rather than by the connection ending. A stream that is still open
// and carrying nothing is the answer a principal who may observe nothing gets,
// and the withdrawal has to arrive at the same place a fresh subscription would.
//
// The withdrawal is made by the successor and not by the reader, and the order
// is the one the lockout refusal names: each object is handed to the successor
// first, then the successor takes the reader's edge away. A caller cannot narrow
// their own subscription at all, and twoAdministrators carries the reason.
func TestAGrantWithdrawnOverHTTPNarrowsALiveSubscription(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	core := serveTwoAdministrators(t, dir)

	frames := watch(t, core.reader)

	_ = runSimulator(t, core.reader, simulator.Config{Model: "MK4", Serial: "SIM-BEFORE"}, "")

	require.Eventually(t, func() bool {
		return frames.saw(event.TypeDeviceArrived)
	}, settle, 25*time.Millisecond, "the stream carried nothing to begin with, so a narrowing proves nothing")

	listed := printers(t, core.reader)
	require.Len(t, listed, 1)

	// Both objects this principal observes anything through: the kind the
	// storage instance hangs off, and the printer the device plane registered.
	objects := []string{
		"/api/v1/grants/resource_kind/storage",
		"/api/v1/grants/device/" + listed[0].ID,
	}

	for _, object := range objects {
		handed := core.reader.call(t, http.MethodPost, object,
			`{"relation":"manager","subject":"user:`+custodian+`"}`)
		require.Equal(t, http.StatusCreated, handed.status, handed.body)
	}

	for _, object := range objects {
		withdrawn := core.custodian.call(t, http.MethodDelete,
			object+"?relation=org&subject=org:"+bootstrapOrg, "")
		require.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)
	}

	require.Empty(t, printers(t, core.reader),
		"the reader still lists a printer, so the withdrawal narrowed nothing to observe")

	// The pump re-reads the graph on its idle tick, so the wait is that tick and
	// not a reconnection. Two ticks is one whole one however the withdrawal
	// fell against it.
	time.Sleep(2 * stream.AuthorizeInterval)

	require.True(t, frames.still(),
		"a withdrawal dropped the reader, who would reconnect straight back onto the empty stream "+
			"this one became")

	settled := frames.total()

	// The printer reports four times a second, so this is eight readings that
	// would have reached the stream if the withdrawal had not.
	time.Sleep(2 * stream.AuthorizeInterval)

	require.Equal(t, settled, frames.total(),
		"a printer still reached a stream whose principal had every grant withdrawn")
}

// custodian is the successor's principal: the second administrator a handover
// needs, and who withdraws the departing administrator's edge.
const custodian = "custodian"

// twoAdministrators is one core with two HTTP surfaces over one graph, one hub
// and one device plane, each serving every request as its own principal. That
// is the production wiring for a deployment with no issuer, built twice.
//
// Two principals are not a convenience here. Every relation that confers an
// object's manager action confers the viewer mask under it, so a withdrawal
// that leaves the caller administering the object leaves them observing it, and
// one that does not is the lockout mayStandDown refuses: nobody can narrow
// their own subscription through the grant API. Proving a withdrawal reaches a
// live stream therefore takes a reader and a successor.
//
// They share one process because a subscription re-authorizes on the epoch its
// own authz.Store caches, and a second server against the same database file
// moves the row without moving that cache: measured, a write through one handle
// took its own epoch from 2 to 3 and left the other handle answering 2, so the
// reader's pump would never look again. Two ports, one graph.
type twoAdministrators struct {
	// reader is the departing administrator: it watches the stream, the printer
	// dials it, and it is the principal the withdrawal narrows.
	reader *process
	// custodian is the successor, who holds a grant of its own on each object
	// before it takes the reader's away.
	custodian *process
}

func serveTwoAdministrators(t *testing.T, dir string) twoAdministrators {
	t.Helper()

	write(t, filepath.Join(dir, "config.yaml"), grantsConfig(dir))

	graph := graphAt(t, filepath.Join(dir, "authz.db"))

	// The successor inherits from the same org the reader does. A successor
	// holding only a grant written on the object does not outrank a victim whose
	// access arrives from above it, which is what mayRemove bounds, and org is
	// not an administrable type, so this edge is written rather than granted.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeOrg, bootstrapOrg, authz.RelAdmin, "user:"+custodian)))

	hub, err := stream.New(stream.Options{
		Site:       "local",
		Interval:   stream.MinInterval,
		Authorizer: visibility{graph: graph},
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	types, err := registry.New()
	require.NoError(t, err)

	host, err := resourcehost.New(resourcehost.Config{Registry: types, HostKeys: builtin.HostKeys})
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

	// The printer is let in on arrival. This case is about the stream and not
	// the door, and nobody is here to approve a code.
	built, err := mountConnectors(t.Context(), config.Default(), "", discard(), fleet, nil, nil,
		resource.AdmissionOpen)
	require.NoError(t, err)

	mounts := built.mounts

	surface := func(principal string, beside []httpapi.Mount) *process {
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
			Mounts:     beside,
		})
		require.NoError(t, buildErr)

		served := httptest.NewServer(api.Handler())
		t.Cleanup(served.Close)

		// The listener is the test server's, so there is no Run to cancel and
		// nothing to wait for; the shape is what the request helpers take.
		done := make(chan error, 1)
		done <- nil

		return &process{addr: served.Listener.Addr().String(), cancel: func() {}, done: done}
	}

	// The device wire is mounted on the reader alone, so a printer dials the
	// surface whose stream this test watches. One driver serves both.
	return twoAdministrators{
		reader:    surface(config.DefaultPrincipal, mounts),
		custodian: surface(custodian, nil),
	}
}

// TestASecondStartOfADeploymentWithNoInstancesWarnsAboutNothing is the warning
// reading the wrong table. It fired before the first Reload, over the instance
// directory, which is empty on every start of a deployment that declares no
// instance. The operator was told the graph had lost their principal, on a graph
// that had not.
func TestASecondStartOfADeploymentWithNoInstancesWarnsAboutNothing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")

	graphAt(t, path)

	require.NotContains(t, startupLog(t, path, config.DefaultPrincipal), "holds no grant",
		"a deployment declaring no instance was told its principal had lost the graph")
}

// TestASecondStartWarnsWhenTheConfiguredPrincipalHoldsNothing is the half that
// holds. An operator who withdrew every grant keeps a graph that reaches
// nothing, and every request afterwards is refused: the warning is the only
// thing that says so.
func TestASecondStartWarnsWhenTheConfiguredPrincipalHoldsNothing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")

	graph := graphAt(t, path)

	held, err := graph.Tuples(t.Context())
	require.NoError(t, err)
	require.NoError(t, graph.Delete(t.Context(), held...))

	require.Contains(t, startupLog(t, path, config.DefaultPrincipal), "holds no grant",
		"a principal the graph reaches nothing for was started without a word")
}

// TestASecondStartWithAChangedPrincipalHandsTheOrgOver is the recovery a
// changed core.principal promises, and what the setup wizard relies on when it
// names the administrator at an issuer after a first start.
func TestASecondStartWithAChangedPrincipalHandsTheOrgOver(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "authz.db")

	graph := graphAt(t, path)

	require.NotContains(t, startupLog(t, path, "somebody-else"), "holds no grant",
		"the changed principal was not made the administrator")

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	successor, err := authz.NewSubject(humanSubjectType, "somebody-else")
	require.NoError(t, err)

	_, err = graph.Grants(t.Context(), successor, org)
	require.NoError(t, err, "the changed principal does not administer the org")
}

// startupLog opens an existing graph as this principal and returns what the
// start wrote. It is the real openGraph, so the warning under test is reached
// the way a restart reaches it.
func startupLog(t *testing.T, path, principal string) string {
	t.Helper()

	core := config.Default()
	core.Authz.Database = path
	core.Principal = principal

	var written strings.Builder

	logger := slog.New(slog.NewTextHandler(&written, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, closeGraph, err := openGraph(t.Context(), core, "0.0.0-test", logger)
	require.NoError(t, err)

	t.Cleanup(closeGraph)

	return written.String()
}
