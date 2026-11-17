package app

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// An install has to take effect while the server runs. The operator installed a
// module from the extensions page and saw nothing until they restarted the
// process, because the install wrote the overlay and nothing read it again.
//
// These cases hold the whole path: the declaration is written, this server
// converges on it, the instance is supervised, its bundle is mounted, and the
// resources listing carries it with the ui object a page draws the sidebar
// entry from. And the same in reverse for an uninstall.

// theLiveModule is the module these cases install: the bundle-only example,
// which is one asset instance and needs no child process on the test machine.
const (
	theBundlePath      = "github.com/acme/nozzle-watch"
	theBundleNamespace = "com.github.aux.acme.nozzle-watch"
	theBundleVersion   = "2.1.0"
)

// liveDeployment is the installer with everything behind it a running
// deployment has: the real graph, the real supervision plane and the real
// store, wired the way app.go wires them.
type liveDeployment struct {
	installer *moduleInstaller
	store     *resource.Store
	host      *resourcehost.Host
	announced *announcedStates
	principal string
}

// announcedStates records every status change the plane reported. It is the
// hook the event bus hangs off in app.go, so a change recorded here is a
// resource.state.changed frame a page on the stream reads.
type announcedStates struct {
	mu   sync.Mutex
	seen []ref.Ref
}

func (a *announcedStates) observe(status supervisor.Status) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.seen = append(a.seen, status.Ref)
}

// heard reports whether the plane announced anything for one instance.
func (a *announcedStates) heard(instance ref.Ref) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Contains(a.seen, instance)
}

// aLiveDeployment builds it. The host is running before the store reloads, the
// way serveOver orders the two, so the first reconcile is a reconcile and not a
// wait for a plane nothing is driving.
func aLiveDeployment(t *testing.T) liveDeployment {
	t.Helper()

	configPath, core := installConfig(t)

	return aLiveDeploymentOver(t, configPath, core)
}

// aLiveDeploymentOver builds one over a configuration a case already holds,
// which is how a case starts the same files a second time.
func aLiveDeploymentOver(t *testing.T, configPath string, core config.Core) liveDeployment {
	t.Helper()

	held, closeGraph, err := openGraph(t.Context(), core, "0.0.0-test", discard())
	require.NoError(t, err)

	t.Cleanup(closeGraph)

	types, err := builtin.Registry()
	require.NoError(t, err)

	announced := &announcedStates{}

	host, err := resourcehost.New(resourcehost.Config{
		Registry: types,
		Logger:   discard(),
		OnStatus: announced.observe,

		ModuleData: moduleDataRoot(core),
		ModulesOff: !core.ModulesOn(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	store, err := resource.New(resource.Config{
		Paths:  resource.Paths{File: configPath, Overlay: core.Overlay},
		Plane:  host,
		Graph:  held.graph,
		Logger: discard(),

		ModulesOff: !core.ModulesOn(),
	})
	require.NoError(t, err)
	require.NoError(t, store.Reload(t.Context()))

	return liveDeployment{
		installer: newModuleInstaller(core, configPath, held.graph, host, store,
			held.graph, discard()),
		store:     store,
		host:      host,
		announced: announced,
		principal: "user:" + core.Principal,
	}
}

// install puts the bundle-only example in and answers what the API answered.
func (d liveDeployment) install(t *testing.T, approve bool) httpapi.ModuleInstallation {
	t.Helper()

	installed, err := d.installer.Install(t.Context(), d.principal, httpapi.ModuleRelease{
		Module:  theBundlePath,
		Version: theBundleVersion,
		From:    aRelease(t, bundleOnlyManifest(), false),
		Approve: approve,
	})
	require.NoError(t, err)

	return installed
}

// listed is one instance as the resources listing carries it, and false when
// the listing carries none under that name.
func (d liveDeployment) listed(t *testing.T, instance ref.Ref) (httpapi.ResourceInstance, bool) {
	t.Helper()

	listing, err := d.store.List(t.Context(), d.principal, instance.Kind())
	require.NoError(t, err)

	for _, item := range listing.Items {
		if item.Kind == instance.Kind().String() && item.Name == instance.ID() {
			return item, true
		}
	}

	return httpapi.ResourceInstance{}, false
}

// theBundleInstance is what the example is declared as.
func theBundleInstance(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindAsset, "nozzle-watch")
	require.NoError(t, err)

	return instance
}

// The whole of what the operator asked for: install, and the module is running
// and on the listing with its ui object, with nothing restarted.
func TestInstallingAModuleServesItWithoutARestart(t *testing.T) {
	t.Parallel()

	deployment := aLiveDeployment(t)
	instance := theBundleInstance(t)

	installed := deployment.install(t, true)

	require.Equal(t, instance.String(), installed.Instance)
	require.True(t, installed.Enabled)
	require.True(t, installed.Running, "the install did not start the module")
	require.Equal(t, "ready", installed.State)
	require.Empty(t, installed.Reason)
	require.False(t, installed.Restart, "the install asked for a restart it does not need")

	// The bundle is opened by the instance's own start goroutine, so the ui
	// object appears a moment after the declaration does.
	var surface httpapi.ResourceInstance

	require.Eventually(t, func() bool {
		item, found := deployment.listed(t, instance)
		if !found || item.UI == nil {
			return false
		}

		surface = item

		return true
	}, settle, 20*time.Millisecond, "the listing never carried the module's ui object")

	require.Equal(t, httpapi.ModuleUIPath(theBundleNamespace)+"ui/index.js", surface.UI.EntryURL)
	require.True(t, surface.Status.Serving, "the instance is on the listing and is serving nothing")

	// And the files are mounted under the namespace, which is what
	// GET /api/v1/modules/<namespace>/ui/index.js is answered from.
	mounted, served := modulePlane{host: deployment.host}.Module(theBundleNamespace)
	require.True(t, served, "the module's bundle is not mounted")
	require.Equal(t, instance, mounted.Instance)

	// The plane announced the instance, which is the frame a page watching the
	// event stream learns of the new module from.
	require.True(t, deployment.announced.heard(instance),
		"no resource state change was announced for the module that was just installed")
}

// A module installed without approve is declared and switched off. Nothing runs
// and nothing is waiting on a restart, so the answer says neither.
func TestInstallingWithoutApproveRunsNothingAndAsksForNoRestart(t *testing.T) {
	t.Parallel()

	deployment := aLiveDeployment(t)

	installed := deployment.install(t, false)

	require.False(t, installed.Enabled)
	require.False(t, installed.Running, "a module that was not switched on is running")
	require.Equal(t, "disabled", installed.State)
	require.False(t, installed.Restart, "a module that was not switched on asked for a restart")
}

// The other direction. An uninstall takes the instance out of the running set,
// off the listing and out of the readiness report in the same request.
func TestUninstallingAModuleClearsItWithoutARestart(t *testing.T) {
	t.Parallel()

	deployment := aLiveDeployment(t)
	instance := theBundleInstance(t)

	deployment.install(t, true)

	require.Eventually(t, func() bool {
		item, found := deployment.listed(t, instance)

		return found && item.UI != nil
	}, settle, 20*time.Millisecond, "the module never reached the listing")

	require.NoError(t, deployment.installer.Remove(t.Context(), deployment.principal, theBundleNamespace, false))

	_, found := deployment.listed(t, instance)
	require.False(t, found, "the listing still carries the module that was removed")

	_, supervised := deployment.host.Status(instance)
	require.False(t, supervised, "the plane is still supervising the module that was removed")

	_, mounted := modulePlane{host: deployment.host}.Module(theBundleNamespace)
	require.False(t, mounted, "the module's bundle is still mounted")

	// The readiness report reads the document the store last reconciled. A
	// removed module must be gone from it in the same request, not at the next
	// start.
	report := readiness{store: deployment.store, host: deployment.host}.
		Ready(t.Context(), deployment.principal)

	for _, held := range report.Instances {
		require.NotEqual(t, instance.String(), held.Resource,
			"the readiness report still names the module that was removed")
	}
}
