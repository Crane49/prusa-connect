package session

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The headers a request signs in with never reach a module, so a module
// cannot ask for one (ADR-20261118-nnm rule 3).
func TestACredentialHeaderIsNeverGrantedToAModule(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Authorization", APIKeyHeader, "Cookie"} {
		if manifest.CheckRequestHeader(name) == nil {
			t.Errorf("a module may ask for %s, which carries a credential", name)
		}
	}
}
