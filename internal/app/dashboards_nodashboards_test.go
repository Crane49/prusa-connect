//go:build nodashboards

package app

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// TestABuildWithoutDashboardsServesNone is the printer's build: whatever the
// key says, there is no store and no origin, and the start says so.
func TestABuildWithoutDashboardsServesNone(t *testing.T) {
	t.Parallel()

	var said bytes.Buffer

	store, off, err := servedDashboards(t.Context(), config.Default(), hostStore{}, nil,
		slog.New(slog.NewTextHandler(&said, nil)))
	require.NoError(t, err)
	require.Nil(t, store)
	require.True(t, off)
	require.Contains(t, said.String(), "dashboards are off")

	who, err := authz.ParseSubject("user:operator")
	require.NoError(t, err)

	grants, err := dashboardGrants(t.Context(), staticGraph{admits: true}, who)
	require.NoError(t, err)
	require.Empty(t, grants)
}
