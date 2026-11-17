//go:build !nodashboards

package httpapi

import (
	"context"
	"testing"
)

// exampleDashboardStore is the store the dashboard samples are driven through,
// holding exactly the values they were built from.
func exampleDashboardStore(t *testing.T) DashboardStore {
	t.Helper()

	return exampleDashboards{
		listing: sampled[DashboardListing](t, "list-dashboards", ""),
		board:   sampled[Dashboard](t, "get-dashboard", ""),
		created: sampled[Dashboard](t, "create-dashboard", ""),
	}
}

// exampleDashboards is the dashboards the samples publish.
type exampleDashboards struct {
	listing DashboardListing
	board   Dashboard
	created Dashboard
}

func (e exampleDashboards) ListDashboards(context.Context, string) (DashboardListing, error) {
	return e.listing, nil
}

func (e exampleDashboards) GetDashboard(context.Context, string, int64) (Dashboard, error) {
	return e.board, nil
}

func (e exampleDashboards) CreateDashboard(context.Context, string, DashboardDraft) (Dashboard, error) {
	return e.created, nil
}

func (e exampleDashboards) ReplaceDashboard(context.Context, string, int64, DashboardDraft) (Dashboard, error) {
	return e.board, nil
}

func (exampleDashboards) DeleteDashboard(context.Context, string, int64) error { return nil }

func (exampleDashboards) SetDefaultDashboard(_ context.Context, _ string, id *int64) (*int64, error) {
	return id, nil
}
