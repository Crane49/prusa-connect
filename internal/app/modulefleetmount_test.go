package app

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The end-to-end proof for the fleet doors: the operator's file, the real
// store, the real supervision plane, a real second process, the real
// authorization graph, the real device plane and the shipping HTTP surface.
// A person reaches the module at its mount, the module calls back into the
// fleet and the stream for that person, and the graph decides.
//
// Three callers. The reader is the deployment's admin. The bystander holds
// the module and nothing on any device, so the module answers them and the
// graph hides the fleet from them. The module's own work is the reader asking
// the module to call as itself.

// bystander reaches the module's routes and holds nothing on any device.
const bystander = "bystander"

// The registration the module may decide, and the fingerprint it is for.
const (
	mountedCode    = "K7PQ2XR4MN"
	mountedMachine = "FP-MOUNTED"
)

// fleetModule is what the cases drive: the two callers, the device the plane
// holds, the registration table, and the hub.
type fleetModule struct {
	reader    *process
	bystander *process
	device    string
	table     *fakeAdmission
	hub       *stream.Hub
}

// serveFleetModule wires the deployment with the fleet and the feed handed to
// the supervision plane exactly as app.go hands them, and one device that
// refuses every command with a reason, so a command that reaches it is told
// apart from one that did not, and that reports files, so a listing that
// reaches it is told apart from one that did not.
func serveFleetModule(t *testing.T, dir string) fleetModule {
	t.Helper()

	installed := installModule(t)

	write(t, filepath.Join(dir, "config.yaml"), fleetModuleConfig(dir, installed))

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

	fleets := &moduleFleets{}

	host, err := resourcehost.New(resourcehost.Config{
		Registry: types,
		Logger:   discard(),
		State:    records.Records,
		Events:   instanceEmitter(hub, fleetMeters{}),
		Fleet:    fleets.forModule,
		Feed:     (&moduleFeeds{hub: hub}).forModule,
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

	// Both fleet doors are bound before the store starts a module, the way
	// app.go orders it. A module's fleet handlers read the holder from their
	// own start goroutine, and the holder's fields are plain, so this order is
	// the whole of what puts the writes before the reads. A bind after the
	// Reload below races that goroutine, which is what the race detector found.
	fleet := newDevices(graph, recordsOf(t, graph), hub, discard(), bootstrapOrg, staging{host: host}, fleetMeters{}, 0)
	t.Cleanup(fleet.Close)

	table := &fakeAdmission{waiting: []admission.Registration{
		{Code: mountedCode, Fingerprint: mountedMachine, Serial: "SN-MOUNTED"},
	}}

	admissions := newRegistrations([]admissionTable{{ref: connectorapi.Ref("xbuddy"), driver: table}}, fleet)

	fleets.bind(fleet)
	fleets.bindAdmission(&admissions, graph)

	require.NoError(t, store.Reload(t.Context()))

	device := arriveBrowsing(t, fleet, "SIM-MOUNTED", "NOT_NOW")

	// The bystander reaches the module and nothing else. The instance itself
	// watches the fleet and drives the one device, which is the grant half of
	// its own work; the ceiling half is in the file. The vocabulary lets a
	// sink watch an org and drive only a device it was named on.
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, ref.KindSink.String(), authz.RelOperator, "user:"+bystander)))

	module := sinkPrincipal(sinkRef(t, "planner"))

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeOrg, bootstrapOrg, authz.RelViewer, module)))
	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, device, authz.RelOperator, module)))

	surface := func(principal string) *process {
		t.Helper()

		api, buildErr := httpapi.New(httpapi.Config{
			Version:      "test",
			Logger:       discard(),
			Principals:   graphPrincipals{inner: unauthenticated{subject: principal}},
			Sessions:     noIssuer{},
			Accounts:     noAccounts{},
			Readiness:    readiness{store: store, host: host},
			Resources:    store,
			Presets:      store,
			Endpoints:    store,
			Grants:       graph,
			Devices:      fleet,
			Events:       hub,
			Stores:       storagePlane{graph: graph, stores: stores{host: host}},
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

	served := fleetModule{
		reader:    surface(config.DefaultPrincipal),
		bystander: surface(bystander),
		device:    device,
		table:     table,
		hub:       hub,
	}

	require.Eventually(t, func() bool {
		return served.reader.instanceState(t, moduleInstance) == condition.StateReady().String()
	}, settle, 20*time.Millisecond, "the declared module never reached ready")

	return served
}

// sinkRef is one module instance's ref.
func sinkRef(t *testing.T, name string) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, name)
	require.NoError(t, err)

	return instance
}

// fleetModuleConfig is moduleConfig with the ceiling on the module's own work:
// it may read and drive the fleet as itself, and may not admit a device.
func fleetModuleConfig(dir string, installed *installedModule) string {
	return moduleConfig(dir, installed) + lines(
		"        permissions:",
		"          - observe",
		"          - operate",
	)
}

// decoded reads a JSON answer.
func decoded(t *testing.T, body string) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)

	return out
}

// ids is the device ids a listing carries.
func ids(t *testing.T, body string) []string {
	t.Helper()

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &page), body)

	out := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, item.ID)
	}

	return out
}

// TestAModuleReadsAndDrivesTheFleetForThePersonItServes is the six device
// methods on the real thing. Each has its permit, for the admin the module is
// serving, and its refuse: the person the graph hides the device from, or a
// principal the host never handed the module.
func TestAModuleReadsAndDrivesTheFleetForThePersonItServes(t *testing.T) {
	t.Parallel()

	served := serveFleetModule(t, t.TempDir())
	commandPath := moduleGroup + "/devices/" + served.device + "/command?verb=" + nozzleVerb
	jobsPath := moduleGroup + "/devices/" + served.device + "/jobs"
	filesPath := moduleGroup + "/devices/" + served.device + "/files?path=usb"

	t.Run("device.list", func(t *testing.T) {
		t.Parallel()

		listed := served.reader.get(t, moduleGroup+"/devices")
		require.Equal(t, http.StatusOK, listed.status, listed.body)
		require.Equal(t, []string{served.device}, ids(t, listed.body))

		// A principal the host never handed the module. The module is serving
		// the admin and names somebody else.
		forged := served.reader.get(t, moduleGroup+"/devices?as=user:mallory")
		require.Equal(t, http.StatusForbidden, forged.status, forged.body)

		// The bystander reaches the module, and the graph shows them nothing.
		hidden := served.bystander.get(t, moduleGroup+"/devices")
		require.Equal(t, http.StatusOK, hidden.status, hidden.body)
		require.Empty(t, ids(t, hidden.body))
	})

	t.Run("device.get", func(t *testing.T) {
		t.Parallel()

		got := served.reader.get(t, moduleGroup+"/devices/"+served.device)
		require.Equal(t, http.StatusOK, got.status, got.body)
		require.Equal(t, served.device, decoded(t, got.body)["id"])

		hidden := served.bystander.get(t, moduleGroup+"/devices/"+served.device)
		require.Equal(t, http.StatusNotFound, hidden.status, hidden.body)
	})

	t.Run("device.command", func(t *testing.T) {
		t.Parallel()

		sent := served.reader.call(t, http.MethodPost, commandPath, "")
		require.Equal(t, http.StatusOK, sent.status, sent.body)
		require.Equal(t, "NOT_NOW", decoded(t, sent.body)["machine_reason"],
			"the command did not reach the machine")

		hidden := served.bystander.call(t, http.MethodPost, commandPath, "")
		require.Equal(t, http.StatusNotFound, hidden.status, hidden.body)
	})

	t.Run("device.jobs", func(t *testing.T) {
		t.Parallel()

		read := served.reader.get(t, jobsPath)
		require.Equal(t, http.StatusOK, read.status, read.body)
		require.Empty(t, decoded(t, read.body), "a machine that has never printed carries a print")

		hidden := served.bystander.get(t, jobsPath)
		require.Equal(t, http.StatusNotFound, hidden.status, hidden.body)
	})

	t.Run("device.files", func(t *testing.T) {
		t.Parallel()

		listed := served.reader.get(t, filesPath)
		require.Equal(t, http.StatusOK, listed.status, listed.body)
		require.Len(t, decoded(t, listed.body)["items"], 3, "the listing did not reach the machine")

		hidden := served.bystander.get(t, filesPath)
		require.Equal(t, http.StatusNotFound, hidden.status, hidden.body)
	})

	t.Run("device.approve", func(t *testing.T) {
		t.Parallel()

		refused := served.bystander.call(t, http.MethodPost,
			moduleGroup+"/registrations/"+mountedCode+"/approve", "")
		require.Equal(t, http.StatusForbidden, refused.status, refused.body)
		require.Empty(t, served.table.approved)

		approved := served.reader.call(t, http.MethodPost,
			moduleGroup+"/registrations/"+mountedCode+"/approve", "")
		require.Equal(t, http.StatusOK, approved.status, approved.body)
		require.Equal(t, true, decoded(t, approved.body)["approved"])
		require.Equal(t, "user:"+config.DefaultPrincipal, served.table.admittedBy,
			"the registration does not record the person the module was serving")
	})
}

// TestAModuleSubscribesForThePersonItServes is bus.subscribe on the real thing:
// the module opens the stream for the admin and a command's frames reach it,
// and a principal the host never handed the module is refused.
func TestAModuleSubscribesForThePersonItServes(t *testing.T) {
	t.Parallel()

	served := serveFleetModule(t, t.TempDir())

	forged := served.reader.get(t, moduleGroup+"/events?as=user:mallory")
	require.Equal(t, http.StatusForbidden, forged.status, forged.body)

	before := served.hub.Subscribers()

	// The subscription is opened beside the case, without the helpers that
	// assert, because a failed assertion has to come from the case's own
	// goroutine.
	type reply struct {
		status int
		body   string
		err    error
	}

	windows := make(chan reply, 1)

	go func() {
		response, err := http.Get("http://" + served.reader.addr + moduleGroup + "/events?windows=1")
		if err != nil {
			windows <- reply{err: err}

			return
		}

		defer response.Body.Close()

		body, err := io.ReadAll(response.Body)
		windows <- reply{status: response.StatusCode, body: string(body), err: err}
	}()

	require.Eventually(t, func() bool { return served.hub.Subscribers() > before },
		settle, 20*time.Millisecond, "the module's subscription never reached the hub")

	// Something to observe: a command's frames go on the device's origin, and
	// the admin observes every device.
	sent := served.reader.call(t, http.MethodPost,
		moduleGroup+"/devices/"+served.device+"/command?verb="+nozzleVerb, "")
	require.Equal(t, http.StatusOK, sent.status, sent.body)

	arrived := <-windows
	require.NoError(t, arrived.err)
	require.Equal(t, http.StatusOK, arrived.status, arrived.body)
	require.NotEmpty(t, decoded(t, arrived.body)["windows"], "no window reached the module")
}

// TestAModulesOwnWorkRunsInsideItsCeilingOnTheRealThing is the module calling
// as itself. It reads the fleet and drives the device, because the operator
// listed observe and operate and granted the instance viewer on the org and
// operator on the device, and it cannot admit a device, because admit_device is
// not in its list.
func TestAModulesOwnWorkRunsInsideItsCeilingOnTheRealThing(t *testing.T) {
	t.Parallel()

	served := serveFleetModule(t, t.TempDir())

	listed := served.reader.get(t, moduleGroup+"/devices?as=self")
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.Equal(t, []string{served.device}, ids(t, listed.body))

	sent := served.reader.call(t, http.MethodPost,
		moduleGroup+"/devices/"+served.device+"/command?as=self&verb="+nozzleVerb, "")
	require.Equal(t, http.StatusOK, sent.status, sent.body)
	require.Equal(t, "NOT_NOW", decoded(t, sent.body)["machine_reason"])

	read := served.reader.get(t, moduleGroup+"/devices/"+served.device+"/jobs?as=self")
	require.Equal(t, http.StatusOK, read.status, read.body)

	browsed := served.reader.get(t, moduleGroup+"/devices/"+served.device+"/files?as=self&path=usb")
	require.Equal(t, http.StatusOK, browsed.status, browsed.body)
	require.Len(t, decoded(t, browsed.body)["items"], 3)

	refused := served.reader.call(t, http.MethodPost,
		moduleGroup+"/registrations/"+mountedCode+"/approve?as=self", "")
	require.Equal(t, http.StatusForbidden, refused.status, refused.body)
	require.Empty(t, served.table.approved, "an approval outside the ceiling reached the table")
}
