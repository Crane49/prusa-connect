package app

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestABrokenResourceInstanceFailsAloneAndTheServerServes is earlier research
// on the real binary: a declaration the loader refuses is a failed instance and
// not a dead server.
//
// Each case declares a good instance and a broken one in the same file, because
// a fixture with one instance in it cannot tell "one failed" from "the file
// failed": both leave nothing running.
func TestABrokenResourceInstanceFailsAloneAndTheServerServes(t *testing.T) {
	t.Parallel()

	// The storage root is under the test's own directory. A relative root is
	// resolved against the working directory, which for a test is the source
	// tree it is compiled from.
	good := func(dir string) string {
		return `core:
  log:
    level: error
resources:
  storage:
    local:
      type: filesystem
      config:
        root: ` + slashed(filepath.Join(dir, "storage")) + `
`
	}

	cases := map[string]string{
		"an unknown key in a resource instance": `    scratch:
      type: filesystem
      requried: true
`,
		"a resource kind that does not exist": `  nonsense:
    other:
      type: filesystem
`,
		"an instance name that is not a label": `    Scratch:
      type: filesystem
`,
	}

	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			running := start(t, dir, good(dir)+broken)

			listed := running.get(t, "/api/v1/resources")
			require.Equal(t, http.StatusOK, listed.status, listed.body)

			var listing struct {
				Items []httpapi.ResourceInstance `json:"items"`
			}

			require.NoError(t, json.Unmarshal([]byte(listed.body), &listing))
			require.Len(t, listing.Items, 1, "the instance that loads was dropped with the one that did not")
			require.Equal(t, "local", listing.Items[0].Name)

			running.stopAndWait(t)
		})
	}
}
