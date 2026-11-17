package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingConnectors remembers who it was asked about, which is the assertion
// that matters here: the rollup is scoped to the caller, so a handler that
// resolved the principal and then asked about somebody else would hand one
// caller another caller's fleet.
type recordingConnectors struct {
	asked   []string
	rollups []ConnectorRollup
}

func (c *recordingConnectors) Connectors(_ context.Context, principal string) ([]ConnectorRollup, error) {
	c.asked = append(c.asked, principal)

	return c.rollups, nil
}

// TestTheRollupIsAskedAboutTheCallerAndNobodyElse is the transport half of the
// property the device plane enforces. A count over another caller's population
// tells this one how many printers exist, which is what the printer listing
// refuses them.
func TestTheRollupIsAskedAboutTheCallerAndNobodyElse(t *testing.T) {
	t.Parallel()

	source := &recordingConnectors{rollups: []ConnectorRollup{{
		Connector: "xbuddy",
		Status:    "degraded",
		Counts:    map[string]int{"connected": 3, "disconnected": 1},
		Total:     4,
	}}}

	config := metricsConfig(nil, fixedPrincipal("user:viewer"))
	config.Connectors = source

	server, err := New(config)
	require.NoError(t, err)

	rolled := call(t, server.Handler(), http.MethodGet, connectorHealthPath, "")
	require.Equal(t, http.StatusOK, rolled.Code, rolled.Body.String())
	require.Equal(t, []string{"user:viewer"}, source.asked)

	var body struct {
		Items []ConnectorRollup `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rolled.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, 4, body.Items[0].Total)
	require.Equal(t, 3, body.Items[0].Counts["connected"])
}

// TestADeploymentWithNoConnectorPlaneAnswersAnEmptyList keeps "there are no
// connectors" and "there is no such path" apart. A 404 would send an operator
// looking for a build problem.
func TestADeploymentWithNoConnectorPlaneAnswersAnEmptyList(t *testing.T) {
	t.Parallel()

	server, err := New(metricsConfig(nil, fixedPrincipal("user:operator")))
	require.NoError(t, err)

	rolled := call(t, server.Handler(), http.MethodGet, connectorHealthPath, "")
	require.Equal(t, http.StatusOK, rolled.Code)
	require.Contains(t, rolled.Body.String(), `"items":[]`)
}

// TestTheRollupTakesAnIdentity keeps the device identifiers it names behind the
// same door every other read is behind.
func TestTheRollupTakesAnIdentity(t *testing.T) {
	t.Parallel()

	config := metricsConfig(nil, fixedPrincipal(""))
	config.Connectors = &recordingConnectors{}

	server, err := New(config)
	require.NoError(t, err)

	requireProblem(t, call(t, server.Handler(), http.MethodGet, connectorHealthPath, ""),
		http.StatusUnauthorized)
}
