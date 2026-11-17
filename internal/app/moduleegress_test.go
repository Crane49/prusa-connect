package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The install prints every connection the module makes outside Prusa Connect,
// approved or not, so the operator reads it before approving.
func TestModuleAddPrintsWhereTheModuleConnects(t *testing.T) {
	t.Parallel()

	egress := strings.Replace(bothHalvesManifest(), "permissions:\n", `egress:
  - host: materials.example
    why: Downloads the catalogue
    sends: The catalogue version it has
permissions:
`, 1)

	for _, flags := range [][]string{nil, {"-approve"}} {
		configPath, _ := installConfig(t)
		said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, egress, true), flags...)

		require.Contains(t, said,
			"connects to materials.example port 443 from the process: Downloads the catalogue. "+
				"Sends: The catalogue version it has")
	}
}
