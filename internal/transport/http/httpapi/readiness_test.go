package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeReadiness answers whatever verdict a test set on it.
type fakeReadiness struct {
	report ReadinessReport
	calls  int
}

func (r *fakeReadiness) Ready(context.Context, string) ReadinessReport {
	r.calls++

	return r.report
}

func readyReport() ReadinessReport {
	return ReadinessReport{
		Ready:    true,
		Serving:  1,
		Declared: 1,
		Instances: []ReadinessInstance{
			{Resource: "storage:local", State: "ready", Required: true, Serving: true},
		},
	}
}

func blockedReport() ReadinessReport {
	return ReadinessReport{
		Ready:    false,
		Serving:  0,
		Declared: 1,
		Blocked:  []string{"storage:local"},
		Instances: []ReadinessInstance{
			{Resource: "storage:local", State: "retrying", Required: true, Serving: false},
		},
	}
}

func TestReadinessAnswers200WhileEveryRequiredInstanceIsServing(t *testing.T) {
	t.Parallel()

	probes := &fakeReadiness{report: readyReport()}
	rec := call(t, serverWithReadiness(t, probes).Handler(), http.MethodGet, readinessPath, "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body ReadinessReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Ready)
	require.Empty(t, body.Blocked)
	require.Equal(t, readyReport().Instances, body.Instances)
}

func TestReadinessAnswers503AndNamesWhatIsBlockingIt(t *testing.T) {
	t.Parallel()

	probes := &fakeReadiness{report: blockedReport()}
	rec := call(t, serverWithReadiness(t, probes).Handler(), http.MethodGet, readinessPath, "")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body ReadinessReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Ready)
	require.Equal(t, []string{"storage:local"}, body.Blocked)
	require.Equal(t, "retrying", body.Instances[0].State)
}

// TestReadinessIsReadPerRequest keeps the answer from becoming a cached one. A
// load balancer acting on a verdict computed a poll ago is routing on a moment
// that has passed.
func TestReadinessIsReadPerRequest(t *testing.T) {
	t.Parallel()

	probes := &fakeReadiness{report: readyReport()}
	handler := serverWithReadiness(t, probes).Handler()

	call(t, handler, http.MethodGet, readinessPath, "")
	call(t, handler, http.MethodGet, readinessPath, "")

	require.Equal(t, 2, probes.calls)
}

// TestReadinessTakesNoIdentity is what makes it usable by the probe that reads
// it. A load balancer holds no credential, and a readiness endpoint it cannot
// call is a readiness endpoint nothing routes on.
func TestReadinessTakesNoIdentity(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithoutIdentity(t).Handler(), http.MethodGet, readinessPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
}

// TestReadinessIsInsideTheSeal is the whole reason it is an operation rather
// than a second mux: what this server answers is what its document publishes,
// and a probe path that is not in the document is one no client generator, and
// no gate, ever sees.
func TestReadinessIsInsideTheSeal(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	require.Contains(t, publishedOperations(server.api), http.MethodGet+" "+readinessPath)

	document := map[string]any{}
	require.NoError(t, json.Unmarshal(renderedDocument(t, server), &document))

	paths, _ := document["paths"].(map[string]any)
	item, _ := paths[readinessPath].(map[string]any)
	get, _ := item["get"].(map[string]any)
	responses, _ := get["responses"].(map[string]any)

	require.Contains(t, responses, "200")
	require.Contains(t, responses, "503", "the answer a load balancer routes on has to be in the document")
}
