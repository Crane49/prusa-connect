package dbstore_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
)

// The stores here are ports over the host's own schema. A test that built the
// tables from its own CREATE statements would be a test of a schema nothing
// ships, so these migrate the graph's schema and this package's, as the host
// does, on every engine the run reaches (internal/authz/authztest).

// engine is one backend a test runs against.
type engine = authztest.Engine

// forEachEngine runs body as a subtest per available engine.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()
	authztest.ForEachEngine(t, body, dbstore.Migrations())
}
