package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stretchr/testify/require"
)

// TestUpgradesOffAnswersNotFoundAndKeepsTheVersionAndTheRestart is
// core.upgrade.enabled switched off. Every operation that prepares an upgrade,
// touches a backup or pairs with the Beta Portal answers 404, says so in its
// description, and the page is told so. The version and
// the restart, safe mode included, are not part of the upgrade engine and stay.
func TestUpgradesOffAnswersNotFoundAndKeepsTheVersionAndTheRestart(t *testing.T) {
	t.Parallel()

	restarter := &recordingRestarter{}
	cfg := exampleConfig(&fakeFleet{}, seededStore(), seededPresets())
	cfg.Maintenance = restarter
	cfg.UpgradesOff = true
	cfg.BetaPortal = nil

	server, err := New(cfg)
	require.NoError(t, err)
	handler := server.Handler()

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, upgradePath, ""},
		{http.MethodGet, upgradeHistory, ""},
		{http.MethodGet, upgradeReleases, ""},
		{http.MethodPost, upgradeStaged, `{"version":"2.0.0"}`},
		{http.MethodPost, upgradeBundle, ""},
		{http.MethodPost, upgradePath, ""},
		{http.MethodPost, upgradeApply, `{"schedule":"now"}`},
		{http.MethodDelete, upgradePath, ""},
		{http.MethodGet, backupsPath, ""},
		{http.MethodPost, backupsPath, `{}`},
		{http.MethodPost, backupsPath + "/b1/restore", ""},
		{http.MethodDelete, backupsPath + "/b1", ""},
		{http.MethodGet, betaPortalPairingPath, ""},
		{http.MethodPost, betaPortalPairingPath, ""},
		{http.MethodPost, betaPortalPairingPath + "/refresh", ""},
		{http.MethodDelete, betaPortalPairingPath, ""},
	} {
		rec := call(t, handler, route.method, route.path, route.body)
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s: %s", route.method, route.path, rec.Body.String())
	}

	// Every operation below these paths answers 404 above, and its description
	// says why, so a client tells "switched off" from "nothing is there". A new
	// operation here must join the list above and carry the note.
	noted := 0
	for path, item := range server.api.OpenAPI().Paths {
		if !strings.HasPrefix(path, upgradePath) && !strings.HasPrefix(path, backupsPath) &&
			!strings.HasPrefix(path, betaPortalPairingPath) {
			continue
		}
		for method, operation := range map[string]*huma.Operation{
			http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put,
			http.MethodPatch: item.Patch, http.MethodDelete: item.Delete,
		} {
			if operation == nil {
				continue
			}
			noted++
			require.Truef(t, strings.HasSuffix(operation.Description, upgradesOffNote),
				"%s %s does not say it answers not found while upgrades are off", method, path)
		}
	}
	require.Equal(t, 16, noted, "an operation below the upgrade, backup or pairing paths is not listed above")

	rec := call(t, handler, http.MethodGet, serverVersionPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = call(t, handler, http.MethodPost, restartPath, `{"mode":"safe"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, restarter.asked, 1)

	require.False(t, uiSettings(t, handler).Upgrades)

	cfg.UpgradesOff = false
	server, err = New(cfg)
	require.NoError(t, err)
	require.True(t, uiSettings(t, server.Handler()).Upgrades)
}
