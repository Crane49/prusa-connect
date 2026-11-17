package app

import (
	"bytes"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// listeningManifest is the two-halves module asking for two UDP ports and a
// data directory (ADR-20261019-3ns).
func listeningManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", `needs:
  listen:
    - name: syslog
      udp: 13514
      why: printer log lines
    - name: metrics
      udp: 8514
      why: printer metrics
  data: true
permissions:
`, 1)
}

// listenOfDeclared reads back the service block of the installed module.
func listenOfDeclared(t *testing.T, configPath string, core config.Core) *install.Service {
	t.Helper()

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)

	return declared.Spec.Service
}

// approve grants the directory and leaves the ports open, because a port is
// approved apart from the other requests (ADR-20261019-3ns rules 5 and 6).
func TestApproveAloneGrantsNoPort(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, listeningManifest(), true), "-approve")

	service := listenOfDeclared(t, configPath, core)
	require.Empty(t, service.Listen)
	require.True(t, service.Data)
	require.Contains(
		t,
		said,
		"not granted listen udp 13514 as syslog (printer log lines). Run the same line with -approve -approve-listen",
	)
	require.Contains(t, said, "not granted listen udp 8514 as metrics (printer metrics)")
}

// approve with approve-listen grants every port the manifest asks for, at the
// port it asks for.
func TestApproveListenGrantsThePorts(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, listeningManifest(), true),
		"-approve", "-approve-listen")

	service := listenOfDeclared(t, configPath, core)
	require.Equal(t, map[string]install.ListenPort{
		"syslog":  {Network: "udp", Port: 13514},
		"metrics": {Network: "udp", Port: 8514},
	}, service.Listen)
	require.Contains(t, said, "listens on  udp/13514 as syslog (printer log lines)")
	require.NotContains(t, said, "not granted listen")
}

// The operator moves a port by its name, and an update approved without the
// port approval keeps the port they chose (ADR-20261019-3ns rules 7 and 10).
func TestTheOperatorMovesAPortAndAnUpdateKeepsIt(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, listeningManifest(), true)

	addModuleFrom(
		t,
		configPath,
		theModulePath+"@1.0.0",
		release,
		"-approve",
		"-approve-listen",
		"-listen",
		"syslog=5514",
	)
	require.Equal(
		t,
		install.ListenPort{Network: "udp", Port: 5514},
		listenOfDeclared(t, configPath, core).Listen["syslog"],
	)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	service := listenOfDeclared(t, configPath, core)
	require.Equal(t, install.ListenPort{Network: "udp", Port: 5514}, service.Listen["syslog"])
	require.Equal(t, install.ListenPort{Network: "udp", Port: 8514}, service.Listen["metrics"])
}

// A port approval without approve, a moved port without the approval, a name
// the manifest does not list and a port out of range all stop the install
// before anything is written.
func TestAPortTheInstallCannotGrantIsRefused(t *testing.T) {
	t.Parallel()

	for name, extra := range map[string][]string{
		"approve-listen without approve": {"-approve-listen"},
		"a moved port without approval":  {"-approve", "-listen", "syslog=5514"},
		"a name the manifest lacks":      {"-approve", "-approve-listen", "-listen", "web=80"},
		"a port out of range":            {"-approve", "-approve-listen", "-listen", "syslog=70000"},
		"a pair with no port":            {"-approve", "-approve-listen", "-listen", "syslog"},
		"a name given twice":             {"-approve", "-approve-listen", "-listen", "syslog=5514,syslog=5515"},
		"a name moved onto another":      {"-approve", "-approve-listen", "-listen", "syslog=8514"},
		"both names moved onto one port": {"-approve", "-approve-listen", "-listen", "syslog=9000,metrics=9000"},
		"a port below 1024":              {"-approve", "-approve-listen", "-listen", "syslog=514"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath, core := installConfig(t)
			release := aRelease(t, listeningManifest(), true)

			var out bytes.Buffer

			args := append([]string{addVerb, "-config", configPath, "-from", release}, extra...)
			err := Module(t.Context(), &out, append(args, theModulePath+"@1.0.0"))
			require.ErrorIs(t, err, ErrUsage)

			doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
			require.NoError(t, err)
			require.Empty(t, doc.Instances, "a refused install wrote a declaration")
		})
	}
}

// Over HTTP the ports are requests until approve_listen, and then they are
// among what was given, at the port the body names.
func TestAnInstallOverHTTPGrantsPortsOnlyWithTheirApproval(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	held, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, listeningManifest(), true), Approve: true,
	})
	require.NoError(t, err)
	require.Contains(t, held.Requests, "listen udp 13514 as syslog (printer log lines)")
	require.Contains(t, held.Approved, "data directory")
	require.NotContains(t, held.Approved, "listen udp 13514 as syslog (printer log lines)")

	installer, _ = theInstaller(t, true)

	held, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, listeningManifest(), true),
		Approve: true, ApproveListen: true, Listen: map[string]httpapi.ModulePort{"metrics": 9514},
	})
	require.NoError(t, err)
	require.Empty(t, held.Requests)
	require.Contains(t, held.Approved, "listen udp 13514 as syslog (printer log lines)")
	require.Contains(t, held.Approved, "listen udp 9514 as metrics (printer metrics)")
}

// A page that showed no ports cannot approve a release that asks for them
// (ADR-20261019-3ns rule 9).
func TestAPortThePageDidNotShowRefusesTheInstall(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, listeningManifest(), true),
		Approve: true, ApproveListen: true,
		Expect: &httpapi.ModuleRequests{Permissions: []string{"observe"}, Data: true},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "MODULE_REQUESTS_CHANGED")
	require.Contains(t, err.Error(), "listen udp 13514 as syslog (printer log lines)")
	require.NotContains(t, err.Error(), "permission observe")
}

// The data directories sit beside the modules directory, which an update
// and a removal leave alone.
func TestModuleDataSitsBesideTheModulesDirectory(t *testing.T) {
	t.Parallel()

	root := filepath.Join("srv", "config.d")

	require.Equal(t, filepath.Join(root, "module-data"),
		moduleDataRoot(config.Core{Modules: filepath.Join(root, "modules")}))
	require.Empty(t, moduleDataRoot(config.Core{}))
}

// An update that would put two names on one port is refused before the
// installed module is stopped or its files replaced, so the module keeps
// running on the ports it holds.
func TestAnUpdateMovingTwoNamesOntoOnePortKeepsTheInstalledModule(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, listeningManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve", "-approve-listen")

	var out bytes.Buffer

	err := Module(t.Context(), &out, []string{
		addVerb, "-config", configPath, "-from", aRelease(t, upgradedListening("1.1.0"), true),
		"-approve", "-approve-listen", "-listen", "syslog=8514", theModulePath + "@1.1.0",
	})
	require.ErrorIs(t, err, ErrUsage)
	require.Contains(t, err.Error(), "udp/8514 is granted to both")

	service := listenOfDeclared(t, configPath, core)
	require.Equal(t, install.ListenPort{Network: "udp", Port: 13514}, service.Listen["syslog"])
	require.Equal(t, install.ListenPort{Network: "udp", Port: 8514}, service.Listen["metrics"])
}

// Over HTTP the same refusal is a 4xx naming the request, and the instance is
// left as it was.
func TestAnInstallOverHTTPMovingTwoNamesOntoOnePortIsRefused(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: theModulePath, Version: "1.0.0", From: aRelease(t, listeningManifest(), true),
		Approve: true, ApproveListen: true, Listen: map[string]httpapi.ModulePort{"syslog": 9000, "metrics": 9000},
	})
	require.Error(t, err)

	refused, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok, "err = %v, want a fault", err)
	require.Equal(t, http.StatusUnprocessableEntity, refused.Status())
	require.Contains(t, err.Error(), "udp/9000 is granted to both")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
}

// A port another instance holds, or one the server listens on, is refused.
func TestAPortInUseIsNotGrantedAgain(t *testing.T) {
	t.Parallel()

	other, err := ref.New(ref.KindSink, "collector")
	require.NoError(t, err)

	this, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	doc := resource.Document{Instances: []resource.Declaration{
		{Ref: other, Spec: resource.Spec{Service: &install.Service{
			Listen: map[string]install.ListenPort{"syslog": {Network: "udp", Port: 13514}},
		}}},
		{Ref: this, Spec: resource.Spec{Service: &install.Service{
			Listen: map[string]install.ListenPort{"metrics": {Network: "udp", Port: 8514}},
		}}},
	}}
	inUse := portsInUse(config.Core{API: config.Listener{Addr: "0.0.0.0:8080"}}, doc, this)

	for name, tc := range map[string]struct {
		granted install.ListenAsk
		refused string
	}{
		"another instance's port": {install.ListenAsk{Name: "syslog", Network: "udp", Port: 13514}, "sink:collector"},
		"the API port":            {install.ListenAsk{Name: "web", Network: "tcp", Port: 8080}, "core.api.addr"},
		"the API port over udp":   {install.ListenAsk{Name: "web", Network: "udp", Port: 8080}, ""},
		"its own port":            {install.ListenAsk{Name: "metrics", Network: "udp", Port: 8514}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			refused := portsFree([]install.ListenAsk{tc.granted}, inUse)
			if tc.refused == "" {
				require.NoError(t, refused)

				return
			}

			require.ErrorIs(t, refused, ErrUsage)
			require.Contains(t, refused.Error(), tc.refused)
		})
	}
}

// A port is kept on an update only for the reason the operator approved. A
// changed reason is a new request: it needs approve-listen again, and is
// listed as new when it gets it (ADR-20261019-3ns rule 10).
func TestAChangedReasonForAPortIsANewRequest(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, listeningManifest(), true),
		"-approve", "-approve-listen")

	changed := strings.Replace(upgradedListening("1.1.0"),
		"why: printer log lines", "why: printer log lines and alerts", 1)

	said := addModuleFrom(t, configPath, theModulePath+"@1.1.0", aRelease(t, changed, true), "-approve")

	service := listenOfDeclared(t, configPath, core)
	require.NotContains(t, service.Listen, "syslog")
	require.Equal(t, install.ListenPort{Network: "udp", Port: 8514}, service.Listen["metrics"])
	require.Contains(t, said, "not granted listen udp 13514 as syslog (printer log lines and alerts)")

	said = addModuleFrom(t, configPath, theModulePath+"@1.1.0", aRelease(t, changed, true),
		"-approve", "-approve-listen")
	require.Contains(t, said, "new         listen udp 13514 as syslog (printer log lines and alerts)")
	require.NotContains(t, said, "new         listen udp 8514 as metrics")
}

// A port the operator first grants on a later install is new, although the
// release before already asked for it.
func TestAPortFirstGrantedOnALaterInstallIsNew(t *testing.T) {
	t.Parallel()

	configPath, _ := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, listeningManifest(), true), "-approve")

	said := addModuleFrom(t, configPath, theModulePath+"@1.1.0", aRelease(t, upgradedListening("1.1.0"), true),
		"-approve", "-approve-listen")
	require.Contains(t, said, "new         listen udp 13514 as syslog (printer log lines)")
	require.Contains(t, said, "new         listen udp 8514 as metrics (printer metrics)")
	require.NotContains(t, said, "new         data directory")
}

// upgradedListening is the listening module at another version.
func upgradedListening(version string) string {
	return strings.Replace(listeningManifest(), "version: 1.0.0", "version: "+version, 1)
}
