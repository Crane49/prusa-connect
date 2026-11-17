package httpapi

import (
	"io/fs"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func printerAuthorityServer(t *testing.T, read func() ([]byte, error), allow bool) http.Handler {
	t.Helper()

	server, err := New(Config{
		Version:          "test",
		Principals:       fixedPrincipal("user:operator"),
		Resources:        seededStore(),
		Presets:          seededPresets(),
		Endpoints:        seededEndpoints(),
		Grants:           &grantingStore{allow: allow},
		Sessions:         newFakeSessions(),
		Accounts:         &fakeAccounts{},
		Readiness:        &fakeReadiness{report: readyReport()},
		Devices:          noPrinters{},
		Stores:           noStorages{},
		Events:           noEvents{},
		PrinterAuthority: read,
	})
	require.NoError(t, err)

	return server.Handler()
}

func TestThePrinterAuthorityIsServedAsAFileToDownload(t *testing.T) {
	t.Parallel()

	der := []byte{0x30, 0x82, 0x01, 0x0a, 0x00, 0xff}
	handler := printerAuthorityServer(t, func() ([]byte, error) { return der, nil }, true)

	rec := call(t, handler, http.MethodGet, printerAuthorityPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, der, rec.Body.Bytes())
	require.Equal(t, "application/pkix-cert", rec.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="connect.der"`, rec.Header().Get("Content-Disposition"))
}

func TestThePrinterAuthorityIsNotFoundWhenThereIsNoFile(t *testing.T) {
	t.Parallel()

	handler := printerAuthorityServer(t, func() ([]byte, error) { return nil, fs.ErrNotExist }, true)

	rec := call(t, handler, http.MethodGet, printerAuthorityPath, "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestThePrinterAuthorityRefusesACallerWhoMayNotAdmitDevices(t *testing.T) {
	t.Parallel()

	read := func() ([]byte, error) { return []byte{1}, nil }
	handler := printerAuthorityServer(t, read, false)

	rec := call(t, handler, http.MethodGet, printerAuthorityPath, "")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "\x01")
}
