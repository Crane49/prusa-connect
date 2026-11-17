//go:build nodashboards

package httpapi

import "github.com/danielgtaylor/huma/v2"

// DashboardStore is the store a build without the dashboards is never handed.
// It names no method, so the Config field still compiles and holds nothing.
type DashboardStore any

// dashboardsCarried says this build leaves the dashboards out. It is the build
// that runs on a printer, and its home page is the one printer it serves.
const dashboardsCarried = false

// registerDashboards registers nothing in a build without the dashboards.
func registerDashboards(huma.API, DashboardStore) {}

// dashboardExamples is empty, because no dashboard operation is registered.
func dashboardExamples() map[string]operationExample { return nil }
