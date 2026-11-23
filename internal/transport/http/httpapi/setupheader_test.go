package httpapi

import (
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The setup code runs the first setup of this server, so a module cannot ask
// for the header that carries it (ADR-20261118-nnm rule 3).
func TestTheSetupCodeHeaderIsNeverGrantedToAModule(t *testing.T) {
	t.Parallel()

	for _, input := range []any{setupConfigurationInput{}, setupOIDCInput{}} {
		field, ok := reflect.TypeOf(input).FieldByName("Code")
		if !ok {
			t.Fatalf("%T has no Code field", input)
		}

		name := field.Tag.Get("header")
		if manifest.CheckRequestHeader(name) == nil {
			t.Errorf("a module may ask for %s, which carries the setup code", name)
		}
	}
}
