//go:build !nodashboards

package dashboards_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/platform/dashboards"
)

// engine is one backend the store runs against: the host's own database,
// migrated the way the product migrates it.
type engine = authztest.Engine

// forEachEngine runs body on every engine this run reaches.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()
	authztest.ForEachEngine(t, body, dashboards.Migrations())
}
