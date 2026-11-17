package manifest_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// TestTheRecordsCeilingIsTheStoresCeiling holds the number the manifest
// writes out to the one the record store enforces. The manifest keeps to the
// standard library and cannot import it, so this is where the two meet.
func TestTheRecordsCeilingIsTheStoresCeiling(t *testing.T) {
	t.Parallel()

	if got, want := manifest.MaxRecordsMiB, stateapi.MaxGrantedNamespaceBytes/stateapi.NamespaceMiB; got != want {
		t.Errorf("records_mib may ask for %d MiB, and a store may be granted %d", got, want)
	}
}
