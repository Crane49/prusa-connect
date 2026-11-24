package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// bundleMaintenance reads an offline bundle as the real staging does: through
// the stream, to its end, and refuses when the stream fails.
type bundleMaintenance struct {
	exampleMaintenance

	got       []byte
	principal string
}

func (m *bundleMaintenance) StageBundle(_ context.Context, body io.Reader, principal string) (upgrade.Journal, error) {
	read, err := io.ReadAll(body)
	if err != nil {
		return upgrade.Journal{}, fmt.Errorf("%w: read artifact: %w", upgrade.ErrBundle, err)
	}

	m.got, m.principal = read, principal

	return exampleUpgradeJournal(), nil
}

func bundleServer(t *testing.T, maintenance Maintenance, uploadLimit int64) *Server {
	t.Helper()

	cfg := exampleConfig(noPrinters{}, seededStore(), seededPresets())
	cfg.Maintenance = maintenance
	cfg.MaxUpgradeBytes = uploadLimit

	server, err := New(cfg)
	require.NoError(t, err)

	return server
}

func TestAnOfflineBundleIsStagedFromTheStreamedBody(t *testing.T) {
	t.Parallel()

	maintenance := &bundleMaintenance{}
	server := bundleServer(t, maintenance, 1<<20)
	bundle := bytes.Repeat([]byte("bundle bytes "), 4096)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, upgradeBundle, bytes.NewReader(bundle))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Equal(t, bundle, maintenance.got, "the body reaches the staging as it was sent")
	require.Equal(t, "user:alice", maintenance.principal)
}

func TestAnOfflineBundleOverTheUploadBoundIsRefusedAsTooLarge(t *testing.T) {
	t.Parallel()

	maintenance := &bundleMaintenance{}
	server := bundleServer(t, maintenance, 1024)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, upgradeBundle,
		strings.NewReader(strings.Repeat("x", 4096)))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
	require.Nil(t, maintenance.got, "nothing was staged")
}

// A bundle that declares a length over its bound is refused before a byte of
// it is read, with the general code: core.api.max_upload_size does not bound
// a bundle.
func TestAnOfflineBundleDeclaredOverItsBoundIsRefusedUnread(t *testing.T) {
	t.Parallel()

	maintenance := &bundleMaintenance{}
	server := bundleServer(t, maintenance, 1024)

	body := &countingReader{Reader: strings.NewReader(strings.Repeat("x", 4096))}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, upgradeBundle, body)
	request.ContentLength = 4096
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), httpfault.CodeBodyTooLarge().String())
	require.Zero(t, body.read, "the body was read before the refusal")
	require.Nil(t, maintenance.got)
}

// Lowering the storage bound leaves the bundle's own, so an operator who lowers
// core.api.max_upload_size can still upgrade.
func TestALoweredUploadBoundStillTakesABundle(t *testing.T) {
	t.Parallel()

	maintenance := &bundleMaintenance{}
	cfg := exampleConfig(noPrinters{}, seededStore(), seededPresets())
	cfg.Maintenance = maintenance
	cfg.MaxUploadBytes = 8

	server, err := New(cfg)
	require.NoError(t, err)

	bundle := bytes.Repeat([]byte("bundle bytes "), 64)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, upgradeBundle, bytes.NewReader(bundle))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Equal(t, bundle, maintenance.got)
}
