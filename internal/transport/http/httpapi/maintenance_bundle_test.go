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
	cfg.MaxUploadBytes = uploadLimit

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
