package httpapi

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// eventStreamMedia is the media type an operation declares when its response is
// a stream rather than a document.
const eventStreamMedia = "text/event-stream"

// TestStreamPathsNamesEveryOperationThatStreams is what keeps the list honest.
//
// StreamPaths is how the composition root tells the package that resolves
// identity which routes are streams, and the acceptance rule for a credential
// differs between a call and a stream. A stream route missing from the list is
// judged as a call, so an api credential is admitted onto it: the refusal that
// covers that case would still pass its own test, over a route nobody asks it
// about.
func TestStreamPathsNamesEveryOperationThatStreams(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	var streaming []string

	for path, item := range server.api.OpenAPI().Paths {
		for _, op := range operationsOfPath(item) {
			if op == nil {
				continue
			}

			for _, response := range op.Responses {
				if response == nil || response.Content[eventStreamMedia] == nil {
					continue
				}

				if !slices.Contains(streaming, path) {
					streaming = append(streaming, path)
				}
			}
		}
	}

	require.NotEmpty(t, streaming, "this gate read no streaming operation, so it is comparing nothing")

	declared := StreamPaths()

	slices.Sort(streaming)
	slices.Sort(declared)

	require.Equal(t, streaming, declared,
		"StreamPaths and the operations that answer with %s disagree. A route in the document and not in "+
			"the list is judged as a plain call, and every credential a stream refuses is admitted on it",
		eventStreamMedia)
}
