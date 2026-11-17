package routes

import (
	"fmt"
	"testing"

	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"
)

// A value Postgres cannot hold, or a row a constraint refuses, is the
// caller's to fix. SQLite accepts the same request, so a 503 here would be a
// refusal that hangs on the engine.
func TestAWriteTheEngineRefusesIsInvalid(t *testing.T) {
	t.Parallel()

	for _, sqlstate := range []pqerror.Code{"22003", "23503", "23514"} {
		err := fmt.Errorf("insert spool: %w", &pq.Error{Code: sqlstate})
		if got := codeFor(err, SpoolInvalid); got != SpoolInvalid {
			t.Errorf("SQLSTATE %s answered %s and not %s", sqlstate, got.Name, SpoolInvalid.Name)
		}
	}

	if got := codeFor(fmt.Errorf("x: %w", &pq.Error{Code: "08006"}), SpoolInvalid); got != StoreFailed {
		t.Errorf("a lost connection answered %s and not %s", got.Name, StoreFailed.Name)
	}
}
