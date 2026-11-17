package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestAKeyABuiltinDoesNotDeclareRefusesThatInstanceAlone holds a compiled-in
// type's config block to the settings it declares, the way a module's block is
// held to its manifest.
//
// The sink is the case that needs the check. Its bridge reads the block as an
// open mapping and hands it to the adapter, so a mistyped key was carried into
// the adapter and dropped there, and the instance came up reporting ready on a
// setting nobody had written. The good instance in the same file proves the
// refusal is this instance's own and not the file's.
func TestAKeyABuiltinDoesNotDeclareRefusesThatInstanceAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `core:
  log:
    level: error
resources:
  sink:
    good:
      type: webhook
      config:
        url: http://127.0.0.1:9/hook
        timeout: 5s
    typo:
      type: webhook
      config:
        url: http://127.0.0.1:9/hook
        timeut: 5s
`

	running := start(t, dir, content)

	require.Eventually(t, func() bool {
		return running.instanceState(t, "sink/typo") == "failed"
	}, settle, 20*time.Millisecond, "a block carrying a key the type does not read must fail its instance")

	require.Eventually(t, func() bool {
		return running.instanceState(t, "sink/good") == "ready"
	}, settle, 20*time.Millisecond, "the instance whose block is right must still come up")

	one := running.get(t, "/api/v1/resources/sink/typo")
	require.Equal(t, http.StatusOK, one.status, one.body)

	var instance httpapi.ResourceInstance
	require.NoError(t, json.Unmarshal([]byte(one.body), &instance))

	said := conditionText(instance)
	require.Contains(t, said, `"timeut"`, "the refusal must name the key the operator wrote")
	require.Contains(t, said, "This type reads", "the refusal must say what the type does read")

	running.stopAndWait(t)
}

// conditionText is every message on one instance's conditions, joined, for a
// case that asserts what the refusal says.
func conditionText(instance httpapi.ResourceInstance) string {
	said := make([]string, 0, len(instance.Status.Conditions))
	for _, condition := range instance.Status.Conditions {
		said = append(said, condition.Message)
	}

	return strings.Join(said, "\n")
}
