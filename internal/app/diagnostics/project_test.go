package diagnostics

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The projection alone has to hold: these tests pass no secrets, so the
// bundle's sweep cannot hide a regression.
func TestConfigViewReportsSecretsAsPresenceOnly(t *testing.T) {
	t.Parallel()

	core := config.Core{}
	core.Auth.SetupCode = "PLANTED-SETUP"
	core.Persistence.Postgres.DSN = "postgres://pcl:PLANTED-DSN-PASS@10.9.8.7:5432/pcl"
	core.API.Addr = "10.9.8.7:8080"
	core.Frontend.SupportURL = "https://support.example.com/PLANTED-SUPPORT"

	view := configView(core, NewScrubber([]byte("s"), nil))

	auth, _ := view["auth"].(map[string]any)
	require.Equal(t, "set", auth["setup_code"])
	persistence, _ := view["persistence"].(map[string]any)
	require.Equal(t, "set", persistence["postgres_dsn"])
	api, _ := view["api"].(map[string]any)
	require.Equal(t, ":8080", api["addr"])
	frontend, _ := view["frontend"].(map[string]any)
	require.Equal(t, "set", frontend["support_url"])

	encoded, err := json.Marshal(view)
	require.NoError(t, err)

	for _, secret := range []string{"PLANTED-SETUP", "PLANTED-DSN-PASS", "10.9.8.7", "PLANTED-SUPPORT"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestConfigViewDoesNotReportAnOperatorChosenKey(t *testing.T) {
	t.Parallel()

	on := true
	core := config.Core{
		Connectors:   map[string]config.Connector{"PLANTED-KEY-sk_live_1234": {Type: "prusalink", Enabled: &on}},
		ModulesLocal: map[string]string{"PLANTED-MODULE-PATH": "/srv/modules"},
	}

	bundle, err := Build(t.Context(), Sources{Config: core}, Options{Salt: []byte("s")})
	require.NoError(t, err)

	got := unzip(t, bundle)["config.json"]
	require.NotContains(t, got, "PLANTED")
	require.Contains(t, got, `"prusalink"`)
}

func TestWalkCountsAMapItHasNoDocumentedKeysFor(t *testing.T) {
	t.Parallel()

	view := walk("", map[string]any{
		"api":          map[string]any{"addr": "x:1"},
		"undocumented": map[string]any{"PLANTED-KEY": "v"},
	}, NewScrubber([]byte("s"), nil))

	require.Equal(t, map[string]any{"api": map[string]any{"addr": ":1"}, "undocumented": 1}, view)
}

func TestHealthViewHashesTheConnectorInstanceAndNamesItsDialect(t *testing.T) {
	t.Parallel()

	s := NewScrubber([]byte("s"), nil)
	core := config.Core{Connectors: map[string]config.Connector{"basement": {Type: "prusalink"}}}
	rollups := []httpapi.ConnectorRollup{{
		Connector: "basement", Status: "degraded", Counts: map[string]int{"error": 1}, Total: 1,
		Degraded: []httpapi.ConnectorDevice{{ID: "d1", State: "error", Reason: "AuthRejected", Attempts: 3}},
	}}

	view := healthViewOf(httpapi.ReadinessReport{Ready: true, Declared: 1}, rollups, core, s)

	require.Equal(t, []connectorHealthView{{
		Connector: s.Hash("basement"), Type: "prusalink", Status: "degraded",
		Counts: map[string]int{"error": 1}, Total: 1,
		Degraded: []deviceHealthView{{ID: "d1", State: "error", Reason: "AuthRejected", Attempts: 3}},
	}}, view.Connectors)
	require.True(t, view.Readiness.Ready)
}

func TestConnectorTypeOfTheShippingDialectIsItsName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "xbuddy", connectorType(config.Core{}, "xbuddy"))
}
