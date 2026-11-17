//go:build nodashboards

package httpapi

import "testing"

// exampleDashboardStore is no store in a build without the dashboards, whose
// document carries no dashboard sample.
func exampleDashboardStore(*testing.T) DashboardStore { return nil }
