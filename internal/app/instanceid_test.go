package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestTheServerKeepsItsIDAcrossARestart is ADR-20261025-gve rules 1 to 5 over the real
// composition root. The id is read the way a client reads it, from the health
// answer with no credential, then the core is stopped and started again on the
// same directory, which is the same database, and the answer must be the same.
// A second directory is a second install and must answer a different id.
func TestTheServerKeepsItsIDAcrossARestart(t *testing.T) {
	t.Parallel()

	const quiet = "core:\n  log:\n    level: error\n"

	dir := t.TempDir()

	first := start(t, dir, quiet)
	id := instanceIDOf(t, first)

	_, err := uuid.Parse(id)
	require.NoError(t, err, "the health answer carries %q, which is not a UUID", id)

	first.stopAndWait(t)

	second := start(t, dir, quiet)
	require.Equal(t, id, instanceIDOf(t, second), "a restart changed the id of the server")

	other := start(t, t.TempDir(), quiet)
	require.NotEqual(t, id, instanceIDOf(t, other), "two installs answer to one id")
}

// instanceIDOf reads the id from GET /api/v1/health.
func instanceIDOf(t *testing.T, core *process) string {
	t.Helper()

	health := core.get(t, "/api/v1/health")
	require.Equal(t, http.StatusOK, health.status, health.body)

	var body struct {
		InstanceID string `json:"instance_id"`
	}

	require.NoError(t, json.Unmarshal([]byte(health.body), &body))
	require.NotEmpty(t, body.InstanceID)

	return body.InstanceID
}
