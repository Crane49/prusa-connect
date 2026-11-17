package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// planted are values that must not appear in any byte of the bundle.
var planted = []string{
	"PLANTED-DSN-PASS", "PLANTED-SETUP", "PLANTED-MODULE-SECRET", "PLANTED-WEBHOOK-PATH",
	"PLANTED-SERIAL", "PLANTED-PRINTER-NAME", "planted@example.cz", "10.9.8.7",
	"/home/planted/file.gcode", "PLANTED-STDERR-TOKEN", "PLANTED-CONNECTOR",
	"PLANTED-PARAM", "PLANTED-FOLDER", "PLANTED-API-KEY", "planted-broker.home.lan", "plantedname",
	"Tr0ub4dor", "3xyz", "PLANTED-LIST-FIELD",
}

func leakySources() Sources {
	core := config.Core{}
	core.Auth.SetupCode = "PLANTED-SETUP"
	core.Persistence.Postgres.DSN = "postgres://pcl:PLANTED-DSN-PASS@10.9.8.7:5432/pcl"
	core.Overlay = "/home/planted/file.gcode"
	core.Connectors = map[string]config.Connector{"PLANTED-CONNECTOR": {Type: "prusalink"}}

	return Sources{
		Version: "0.0.1",
		Started: time.Unix(0, 0),
		Config:  core,
		Logs: []observability.LogEntry{
			{
				Message: "post failed",
				Attrs: []slog.Attr{
					slog.String("error", "post https://hooks.example.com/PLANTED-WEBHOOK-PATH?x=1: EOF"),
				},
			},
			{Message: "mail planted@example.cz", Attrs: []slog.Attr{slog.String("username", "jan")}},
			{Message: "dial failed", Attrs: []slog.Attr{slog.String("connector", "PLANTED-CONNECTOR")}},
			{Message: `auth failed for user "plantedname" with password=Tr0ub4dor&3xyz`},
			{Message: "PLANTED-STDERR-TOKEN", Attrs: []slog.Attr{slog.Bool(observability.ModuleStderrKey, true)}},
		},
		Crash: []byte("panic: open /home/planted/file.gcode\n"),
		Devices: []httpapi.Device{{
			ID: "d1", Name: "PLANTED-PRINTER-NAME", Serial: "PLANTED-SERIAL", Model: "MK4", Driver: "PLANTED-CONNECTOR",
		}},
		Connectors: []httpapi.ConnectorRollup{{
			Connector: "PLANTED-CONNECTOR", Status: "degraded", Counts: map[string]int{"error": 1}, Total: 1,
			Degraded: []httpapi.ConnectorDevice{{ID: "d1", State: "error", Reason: "AuthRejected"}},
		}},
		Resources: []httpapi.ResourceInstance{
			{
				Kind: "camera",
				Name: "front",
				ResourceSpec: httpapi.ResourceSpec{
					Type: "rtsp",
					Config: httpapi.ResourceConfig(`{"url":"rtsp://u:x@10.9.8.7/stream","password":"xxxxx",` +
						`"api_key":"PLANTED-API-KEY","broker":"planted-broker.home.lan"}`),
				},
				Status: httpapi.ResourceStatus{
					Conditions: []httpapi.ResourceCondition{{Message: "dial 10.9.8.7:554: refused"}},
				},
			},
		},
		// PLANTED-STDERR-TOKEN stands for a module printing its own configured
		// token: the sweep knows it because it is a module setting.
		Secrets: []string{
			"Tr0ub4dor&3xyz",
			"PLANTED-MODULE-SECRET",
			"PLANTED-DSN-PASS",
			"PLANTED-SETUP",
			"PLANTED-STDERR-TOKEN",
		},
	}
}

func TestNoPlantedValueReachesTheBundle(t *testing.T) {
	t.Parallel()

	for _, modules := range []bool{false, true} {
		frontend := []byte(
			`[{"time":"t","level":"error","scope":"api","message":"PLANTED-MODULE-SECRET at planted@example.cz",` +
				`"route":"/storage/usb/folder/PLANTED-FOLDER","fields":{"params":{"printer_name":"PLANTED-PARAM"},` +
				`"names":["PLANTED-LIST-FIELD"]}}]`,
		)
		bundle, err := Build(
			t.Context(), leakySources(),
			Options{Modules: modules, Frontend: frontend, Now: time.Unix(0, 0), Salt: []byte("s")},
		)
		require.NoError(t, err)

		for name, content := range unzip(t, bundle) {
			for _, secret := range planted {
				require.NotContains(t, content, secret, "%s leaks %q (modules=%v)", name, secret, modules)
			}
		}
	}
}

func TestBundleNamesItsPartsInTheManifest(t *testing.T) {
	t.Parallel()

	bundle, err := Build(
		t.Context(),
		leakySources(),
		Options{Now: time.Unix(0, 0), Salt: []byte("s"), Frontend: []byte("nope")},
	)
	require.NoError(t, err)

	files := unzip(t, bundle)
	for _, name := range []string{"manifest.json", "config.json", "logs.jsonl", "crash.log", "modules.json", "printers.json", "health.json", "accounts.json"} {
		require.Contains(t, files, name)
	}

	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(files["manifest.json"]), &manifest))
	require.Equal(t, "unreadable", manifest["frontend"])
	require.Equal(t, false, manifest["modules"])
	require.NotContains(t, manifest, "commit")
}

func TestManifestCarriesTheBuildCommit(t *testing.T) {
	t.Parallel()

	src := leakySources()
	src.Commit = "0123456789abcdef0123456789abcdef01234567"

	bundle, err := Build(t.Context(), src, Options{Now: time.Unix(0, 0), Salt: []byte("s")})
	require.NoError(t, err)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(unzip(t, bundle)["manifest.json"]), &manifest))
	require.Equal(t, src.Commit, manifest["commit"])
}

func TestBundleFilesAreDatedWhenTheBundleWasBuilt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	bundle, err := Build(t.Context(), leakySources(), Options{Now: now, Salt: []byte("s")})
	require.NoError(t, err)

	r, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	require.NoError(t, err)

	for _, f := range r.File {
		require.True(t, f.Modified.Equal(now), "%s is dated %s", f.Name, f.Modified)
	}
}

func TestConfigSecretsReadsEveryFormADSNHidesAPasswordIn(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		dsn  string
		want []string
	}{
		"url userinfo": {"postgres://pcl:userinfo-pass@db/pcl", []string{"userinfo-pass"}},
		"url query":    {"postgres://db/pcl?password=query-pass&sslpassword=key-pass", []string{"query-pass", "key-pass"}},
		"key=value":    {"host=db password=bare-pass user=pcl", []string{"bare-pass"}},
		"quoted":       {"host=db password='quoted pass' user=pcl", []string{"quoted pass"}},
		"none":         {"host=db user=pcl", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core := config.Core{}
			core.Auth.SetupCode = "setup-code"
			core.Persistence.Postgres.DSN = tc.dsn

			require.ElementsMatch(t, append([]string{"setup-code"}, tc.want...), ConfigSecrets(core))
		})
	}
}

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)

	out := map[string]string{}
	for _, f := range r.File {
		rc, openErr := f.Open()
		require.NoError(t, openErr)
		content, readErr := io.ReadAll(rc)
		require.NoError(t, readErr)
		_ = rc.Close()
		out[f.Name] = strings.TrimSpace(string(content))
	}

	return out
}
