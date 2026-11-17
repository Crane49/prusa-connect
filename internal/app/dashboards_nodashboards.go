//go:build nodashboards

package app

import (
	"context"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
)

// dashboardsBuilt says whether this build leaves out the dashboards, so the
// first-run wizard offers core.dashboards.enabled only where it does
// something.
const dashboardsBuilt = false

// servedDashboards serves none in a build that leaves the dashboards out, and
// the start says so in one line, whatever core.dashboards.enabled says.
func servedDashboards(
	ctx context.Context, _ config.Core, _ hostStore, _ *stream.Hub, logger *slog.Logger,
) (httpapi.DashboardStore, bool, error) {
	logger.InfoContext(ctx, "dashboards are off", "reason", "this build leaves them out")

	return nil, true, nil
}

// dashboardGrants grants no dashboard origin, because nothing publishes one.
func dashboardGrants(context.Context, graph, authz.Subject) ([]stream.Grant, error) {
	return nil, nil
}
