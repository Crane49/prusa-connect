package resource

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func TestAModulePublishesWhatTheInstallGrantedAndItsTitle(t *testing.T) {
	t.Parallel()

	granted := moduleFactsOf(Spec{Module: &install.ModuleEntry{
		Title: "Spool tracker",
		Asked: &install.ModuleAsks{Permissions: []string{"view_device"}, RecordsMiB: 64},
	}})
	require.Equal(t, "Spool tracker", granted.Title)
	require.NotNil(t, granted.Asked)
	require.Equal(t, []string{"view_device"}, granted.Asked.Permissions)
	require.Equal(t, 64, granted.Asked.RecordsMiB)

	require.Nil(t, moduleFactsOf(Spec{Module: &install.ModuleEntry{}}).Asked)
}

// The listing says which ports the instance holds now, at the number the
// operator moved one to, with the manifest's reason (ADR-20261019-3ns rule 8).
func TestAModulePublishesThePortsItHolds(t *testing.T) {
	t.Parallel()

	facts := moduleFactsOf(Spec{
		Module: &install.ModuleEntry{Asked: &install.ModuleAsks{Listen: []install.ListenAsk{
			{Name: "syslog", Network: "udp", Port: 13514, Why: "printer log lines"},
			{Name: "metrics", Network: "udp", Port: 8514, Why: "printer metrics"},
		}}},
		Service: &install.Service{Listen: map[string]install.ListenPort{
			"syslog": {Network: "udp", Port: 5514},
		}},
	})

	require.Equal(t, []httpapi.ModuleListen{
		{Name: "syslog", Network: "udp", Port: 5514, Why: "printer log lines"},
	}, facts.Listening)

	none := moduleFactsOf(Spec{Module: &install.ModuleEntry{}, Service: &install.Service{}})
	require.Empty(t, none.Listening)
}

// The listing says where the module's own database is and never carries the
// credential it logs in with, and an overlay rewrite keeps the grant
// (ADR-20261021-bqc rules 9 and 10).
func TestAModulePublishesItsDatabaseWithoutItsPassword(t *testing.T) {
	t.Parallel()

	held := &install.Database{
		Engine:      wirerpc.DatabasePostgres,
		DSN:         "postgres://module_notes:s3cret@db.lan:5432/module_notes",
		Provisioned: true,
	}

	facts := moduleFactsOf(Spec{Module: &install.ModuleEntry{}, Service: &install.Service{Database: held}})
	require.Equal(t, &httpapi.ResourceDatabase{
		Engine: "postgres", Provisioned: true, Server: "db.lan:5432/module_notes",
	}, facts.Database)

	encoded, err := json.Marshal(facts)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "s3cret")

	require.Equal(t, &install.DatabaseEntry{Engine: "postgres", DSN: held.DSN, Provisioned: true},
		serviceEntryOf(&install.Service{Database: held}, "").Database)

	require.Nil(t, moduleFactsOf(Spec{Module: &install.ModuleEntry{}, Service: &install.Service{}}).Database)
}

// A loaded service holds each event as a whole type, and the overlay keeps the
// name the operator wrote after the namespace. Written back whole, the next
// load put the namespace in front again, and the instance stopped.
func TestAServicesEventsAreWrittenBackAsTheOperatorWroteThem(t *testing.T) {
	t.Parallel()

	const namespace = "com.github.aux.acme.materials"

	service := &install.Service{Events: map[string]string{
		"tool": install.EventTypeOf(namespace, "materials.tool"),
	}}

	require.Equal(t, map[string]string{"tool": "materials.tool"},
		serviceEntryOf(service, namespace+extension.QualifiedSeparator+"materials").Events)
}
