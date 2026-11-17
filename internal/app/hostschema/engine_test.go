package hostschema

import (
	"testing"

	"github.com/prusa3d/prusa-connect/internal/authz/authztest"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// engine is one backend a test runs against, opened empty or migrated with the
// whole host schema.
type engine = authztest.Engine

// otherOwners is the host schema past the graph, which authztest migrates
// first on its own account.
func otherOwners() []sqlstore.Migrations { return Schema().Owners[1:] }

// forEachEngine runs body as a parallel subtest per engine this run reaches.
func forEachEngine(t *testing.T, body func(t *testing.T, e engine)) {
	t.Helper()
	authztest.ForEachEngine(t, body, otherOwners()...)
}

// engines is every engine this run reaches.
func engines(t *testing.T) []engine {
	t.Helper()

	return authztest.Engines(t, otherOwners()...)
}
