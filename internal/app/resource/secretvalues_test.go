//go:build diagnostics

package resource_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// TestSecretValuesNamesWhatADiagnosticsBundleMustSweep: the declared password,
// and the parts of an address Redact leaves readable that carry a credential.
func TestSecretValuesNamesWhatADiagnosticsBundleMustSweep(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) {
		cfg.Declared = declaredTypes{name: "mqtt", options: []extension.Option{
			{Key: "url", Type: extension.OptionURL, Summary: "The stream."},
			{Key: "password", Type: extension.OptionSecret, Summary: "The stream password."},
		}}
	})

	_, err := h.create(mustRef(t, ref.KindSink, "broker"),
		spec("mqtt", `{"password":"hunter2222","url":"rtsp://u:pw123456@cam/live?token=abcdef12"}`))
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"hunter2222", "pw123456", "token=abcdef12"}, h.store.SecretValues())
}

// TestSecretValuesReadsEveryStringInASecretBlock: a secret option that holds
// a block, such as a webhook's headers, has every string in it swept.
func TestSecretValuesReadsEveryStringInASecretBlock(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(cfg *resource.Config) {
		cfg.Declared = declaredTypes{name: "mqtt", options: []extension.Option{
			{Key: "headers", Type: extension.OptionSecret, Summary: "The request headers."},
		}}
	})

	_, err := h.create(mustRef(t, ref.KindSink, "broker"),
		spec("mqtt", `{"headers":{"Authorization":"Bearer abcdef1234","X-Keys":["k1-secret"]}}`))
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"Bearer abcdef1234", "k1-secret"}, h.store.SecretValues())
}

// TestSecretValuesHoldsTheModuleDatabasePassword: the connection string of a
// module's own database is a credential outside the settings block, and the
// sweep reads its password (ADR-20261021-bqc rule 10).
func TestSecretValuesHoldsTheModuleDatabasePassword(t *testing.T) {
	t.Parallel()

	major := 1
	enabled := true

	h := newHarness(t, func(cfg *resource.Config) {
		instance := mustRef(t, ref.KindSink, "notes")
		left := resource.LeftBehind(instance, "com.github.acme.notes/notes", &install.Database{
			Engine: "postgres", DSN: "postgres://module_old:leftpass678@db.lan/module_old", Provisioned: true,
		}, resource.LeftMoved, time.Now())

		_, err := resource.InstallModule(cfg.Paths.Overlay, instance, install.Entry{
			Type:    "com.github.acme.notes/notes",
			Enabled: &enabled,
			Runtime: install.RuntimeService.String(),
			Service: &install.ServiceEntry{
				Path:          "modules/notes",
				Checksum:      "sha256:" + strings.Repeat("a", 64),
				ContractMajor: &major,
				Database: &install.DatabaseEntry{
					Engine: "postgres", DSN: "postgres://module_notes:dbpass12345@db.lan/module_notes", Provisioned: true,
				},
			},
		}, resource.LeftChange{Leave: &left})
		require.NoError(t, err)
	})

	require.NoError(t, h.store.Reload(t.Context()))
	require.Contains(t, h.store.SecretValues(), "dbpass12345")
	require.Contains(t, h.store.SecretValues(), "leftpass678", "a database left behind keeps its credential")
}
