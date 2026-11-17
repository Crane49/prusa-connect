package httpapi

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// A published set announces a removal before it makes one, and the announcement
// has to reach the client that is branching on the entry. The marker is one
// type for every vocabulary, so this case drives the capability keys and the
// rest of them carry the same field.
func TestADeprecatedEntryCarriesItsMarkerOntoTheWire(t *testing.T) {
	t.Parallel()

	key := connectorapi.VerbSendInfo
	coverage := connectorapi.Set{key: connectorapi.Value{
		Flag:       true,
		Kind:       connectorapi.KindFlag,
		Deprecated: vocabulary.Deprecate("0.2.0", "core.command.send_file_info"),
	}}

	body := capabilityBodyOf(coverage)

	for _, entry := range body.Keys {
		if entry.Key != key.String() {
			continue
		}

		if !entry.Deprecated.Deprecated() {
			t.Fatalf("%s is deprecated and the served entry carries no marker", key)
		}

		if entry.Deprecated.Since != "0.2.0" || entry.Deprecated.ReplacedBy != "core.command.send_file_info" {
			t.Fatalf("the served marker reads %+v", *entry.Deprecated)
		}

		return
	}

	t.Fatalf("%s never reached the served answer, so this case judged nothing", key)
}

// An entry nothing deprecated carries no marker, so a client reading one always
// found an announcement rather than a default.
func TestAnEntryNobodyDeprecatedCarriesNoMarker(t *testing.T) {
	t.Parallel()

	coverage := connectorapi.Set{
		connectorapi.VerbSendInfo: connectorapi.Value{Flag: true, Kind: connectorapi.KindFlag},
	}

	for _, entry := range capabilityBodyOf(coverage).Keys {
		if entry.Deprecated.Deprecated() {
			t.Errorf("%s carries a marker nothing declared", entry.Key)
		}
	}
}
