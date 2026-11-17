package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestOneDeploymentServesOneIdentityProvider is the pair that cannot be
// resolved at runtime. Two providers leave one person with two accounts on one
// server and no rule for which grants belong to which, so the configuration is
// refused before a socket is opened.
func TestOneDeploymentServesOneIdentityProvider(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `core:
  auth:
    local: true
    issuer: "https://issuer.example"
    client_id: "prusa-connect"
    redirect_uri: "https://host.example/api/v1/auth/callback"
`)

	core, err := config.Load(path)
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "one deployment serves one identity provider")
}

// TestAnAccountsPathNothingReadsIsRefused is the typo that would otherwise be
// silent. An operator who set the path and not the key would keep an open
// server and a file that never appears.
func TestAnAccountsPathNothingReadsIsRefused(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `core:
  auth:
    accounts: ./data/accounts.json
`)

	core, err := config.Load(path)
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "core.auth.accounts")
}

// TestALocalDeploymentGetsAnAccountsPathBesideTheGraph is the default a
// self-hoster never has to write. The file holds password hashes, so it goes in
// the directory this server owns rather than beside the operator's own file.
func TestALocalDeploymentGetsAnAccountsPathBesideTheGraph(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "core:\n  auth:\n    local: true\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.True(t, core.Auth.Local)
	require.False(t, core.Auth.Anonymous(), "a deployment with local accounts authenticates nobody")
	require.Equal(t, config.DefaultAccounts(path), core.Auth.Accounts)
	require.Equal(t, filepath.Dir(core.Authz.Database), filepath.Dir(core.Auth.Accounts),
		"the accounts and the graph are not in the same machine-owned directory")
}

// TestADeploymentWithNoProviderIsTheAnonymousOne keeps the third wiring
// distinct. It is neither an issuer nor an account store, and it is the one
// that serves every request as core.principal.
func TestADeploymentWithNoProviderIsTheAnonymousOne(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "core:\n  site: local\n")

	core, err := config.Load(path)
	require.NoError(t, err)
	require.True(t, core.Auth.Anonymous())
	require.Empty(t, core.Auth.Accounts, "a deployment that reads no accounts was given a path to them")
}

// TestTheSessionCookieFollowsTheSchemeABrowserReachesThisServerOn holds the one
// attribute a browser silently drops a credential over.
//
// The two providers read different keys because each holds the scheme in a
// different place: a redirect login has written the origin down in its redirect
// URI, and a password login has not, so the API listener's own certificate is
// the answer.
func TestTheSessionCookieFollowsTheSchemeABrowserReachesThisServerOn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		yaml   string
		secure bool
	}{
		"an issuer sending the browser back over https": {
			yaml: `core:
  auth:
    issuer: "https://issuer.example"
    client_id: "prusa-connect"
    redirect_uri: "https://host.example/api/v1/auth/callback"
`,
			secure: true,
		},
		"an issuer sending the browser back over http": {
			yaml: `core:
  auth:
    issuer: "https://issuer.example"
    client_id: "prusa-connect"
    redirect_uri: "http://127.0.0.1:8080/api/v1/auth/callback"
`,
		},
		"local accounts on a listener with no certificate": {
			yaml:   "core:\n  auth:\n    local: true\n",
			secure: false,
		},
		"local accounts on a listener serving TLS": {
			yaml: `core:
  api:
    tls:
      certificate: "cert.pem"
      key: "key.pem"
  auth:
    local: true
`,
			secure: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))

			if strings.Contains(tc.yaml, "certificate:") {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("x"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "key.pem"), []byte("x"), 0o600))
			}

			core, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, tc.secure, core.SecureCookies())
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestAClientSecretIsReadAndNeedsAnIssuer: a confidential client's secret is
// read from the file, and a secret with no issuer is refused like a client id
// with none, because no login would ever send it.
func TestAClientSecretIsReadAndNeedsAnIssuer(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `core:
  auth:
    issuer: "https://accounts.google.com"
    client_id: "prusa-connect.apps.googleusercontent.com"
    client_secret: "GOCSPX-secret"
    redirect_uri: "https://host.example/api/v1/auth/callback"
`)

	core, err := config.Load(path)
	require.NoError(t, err)
	require.NoError(t, core.Validate())
	require.Equal(t, "GOCSPX-secret", core.Auth.ClientSecret)

	path = writeConfig(t, "core:\n  auth:\n    client_secret: \"GOCSPX-secret\"\n")

	core, err = config.Load(path)
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "client secret")
}
