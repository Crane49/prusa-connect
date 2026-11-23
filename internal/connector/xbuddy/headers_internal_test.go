package xbuddy

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// A printer's credentials never reach a module, so a module cannot ask for
// the header a printer registers or signs in with (ADR-20261118-nnm rule 3).
func TestAPrintersCredentialHeaderIsNeverGrantedToAModule(t *testing.T) {
	t.Parallel()

	for _, name := range []string{codeHeader, tokenHeader, fingerprintHeader} {
		if manifest.CheckRequestHeader(name) == nil {
			t.Errorf("a module may ask for %s, which carries a printer's credential", name)
		}
	}
}
