package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheShippedConfigurationNamesTheAPIListener reads config/config.yaml, the
// file `make generate-printer-config` passes to this command.
func TestTheShippedConfigurationNamesTheAPIListener(t *testing.T) {
	t.Parallel()

	shipped := filepath.Join("..", "..", "config", "config.yaml")

	content, err := os.ReadFile(shipped)
	require.NoError(t, err)

	found, err := resolve(shipped, content, "xbuddy")
	require.NoError(t, err)
	require.Equal(t, answer{Listener: "core.api", Port: "8080", TLS: false}, found)
}

func TestResolveFindsTheListenerAPrinterDials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		want    answer
		wantErr string
	}{
		{
			name:    "the API listener when the connector has none",
			content: "core:\n  api:\n    addr: \":8080\"\n",
			want:    answer{Listener: "core.api", Port: "8080"},
		},
		{
			name: "the API listener with a certificate uses TLS",
			content: "core:\n  api:\n    addr: \"0.0.0.0:8443\"\n    tls:\n" +
				"      certificate: certs/server/api.crt\n",
			want: answer{Listener: "core.api", Port: "8443", TLS: true},
		},
		{
			name: "the connector's own listener comes first",
			content: "core:\n  api:\n    addr: \":8080\"\n    tls:\n      certificate: api.crt\n" +
				"  connectors:\n    xbuddy:\n      type: xbuddy\n      config:\n" +
				"        listener:\n          addr: \":8081\"\n",
			want: answer{Listener: "core.connectors.xbuddy.config.listener", Port: "8081"},
		},
		{
			name: "a commented value is not read",
			content: "core:\n  api:\n    addr: \":8080\" # the default\n    tls:\n" +
				"      certificate: \"\"\n      # certificate: certs/server/api.crt\n",
			want: answer{Listener: "core.api", Port: "8080"},
		},
		{
			name:    "an IPv6 address",
			content: "core:\n  api:\n    addr: \"[::1]:9000\"\n",
			want:    answer{Listener: "core.api", Port: "9000"},
		},
		{
			name:    "no addr anywhere is refused",
			content: "core:\n  site: local\n",
			wantErr: "neither core.connectors.xbuddy.config.listener.addr nor core.api.addr is set",
		},
		{
			name:    "an addr with no port is refused",
			content: "core:\n  api:\n    addr: localhost\n",
			wantErr: "has no port",
		},
		{
			name:    "a file that is not YAML is refused",
			content: "core: [unclosed\n",
			wantErr: "parse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			found, err := resolve("config.yaml", []byte(tc.content), "xbuddy")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, found)
		})
	}
}
