package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// otherPasswordPrefix starts a hash at 12 MiB with 3 passes. Neither build
// defaults to that cost, so a path that drops the configured options stores
// another prefix.
const otherPasswordPrefix = "$argon2id$v=19$m=12288,t=3,p=1$"

// storedHash reads the hash of one account straight from the database.
func storedHash(t *testing.T, host hostStore, id string) string {
	t.Helper()

	rows, err := accountRows(host)
	require.NoError(t, err)

	row, err := rows.ByID(t.Context(), id)
	require.NoError(t, err)

	return row.Hash
}

// The server opens the account store with the password cost and the bound the
// configuration names.
func TestTheServerOpensTheAccountStoreAtThePasswordSettings(t *testing.T) {
	t.Parallel()

	core := localCore(t)
	core.Auth.PasswordMemoryBytes = 12 << 20
	core.Auth.PasswordPasses = 3
	core.Auth.PasswordHashesAtOnce = 2

	host := testHost(t)
	wired, _ := wiredAccountsOn(t, core, host)

	require.Equal(t, 2, wired.store.HashesAtOnce())

	_, err := wired.store.CreateFirst(t.Context(), "operator", local.NewAccount{
		Username: "owner@example.com",
		Password: []byte(testPassword),
	})
	require.NoError(t, err)

	hash := storedHash(t, host, "operator")
	require.True(t, strings.HasPrefix(hash, otherPasswordPrefix), "the server stored %s", hash)
}

// The account commands open the store with the password cost and the bound the
// configuration file names, the same as the server.
func TestTheAccountCommandsOpenTheStoreAtThePasswordSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	body := strings.Join([]string{
		"core:",
		"  principal: operator",
		"  authz:",
		"    database: '" + filepath.Join(dir, "authz.db") + "'",
		"  overlay: '" + filepath.Join(dir, "resources.json") + "'",
		"  auth:",
		"    local: true",
		"    password_memory_bytes: 12582912",
		"    password_passes: 3",
		"    password_hashes_at_once: 2",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	opened, closeHost, err := openAccountsFor(t.Context(), path, false)
	require.NoError(t, err)

	require.Equal(t, 2, opened.store.HashesAtOnce())

	_, err = opened.store.CreateFirst(t.Context(), "operator", local.NewAccount{
		Username: "owner@example.com",
		Password: []byte(testPassword),
	})
	require.NoError(t, err)

	closeHost()

	core, err := config.Load(path)
	require.NoError(t, err)

	hash := storedHash(t, testHostAt(t, core.Authz.Database), "operator")
	require.True(t, strings.HasPrefix(hash, otherPasswordPrefix), "the account commands stored %s", hash)
}
