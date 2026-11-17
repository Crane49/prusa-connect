package node_test

import (
	"errors"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// siteDefinition builds the definition of an interface with no property and no
// method, exposed as the file states.
func siteDefinition(t *testing.T, exposure string) *schema.Definition {
	t.Helper()
	builder := schema.NewBuilder()
	file := `{"interface":"Site1","exposure":"` + exposure + `"}`
	if err := builder.AddJSON("site.json", []byte(file)); err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	registry, err := builder.Build()
	if err != nil {
		t.Fatalf("build the definition: %v", err)
	}
	definition, _ := registry.Definition("Site1")
	return definition
}

// TestRegisterTakesTheExposureOfItsDefinition checks the exposure Register
// gives an instance: the one it states, else the one of its Definition, else
// upward. An instance that states another exposure than its Definition is
// refused.
func TestRegisterTakesTheExposureOfItsDefinition(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, brnoPath)
		downward := siteDefinition(t, "downward")
		cases := []struct {
			name       string
			definition *schema.Definition
			exposure   schema.Exposure
			want       schema.Exposure
			err        error
		}{
			{"neither", nil, 0, schema.ExposureUpward, nil},
			{"only the instance", nil, schema.ExposureBoth, schema.ExposureBoth, nil},
			{"only the definition", downward, 0, schema.ExposureDownward, nil},
			{"both and the same", downward, schema.ExposureDownward, schema.ExposureDownward, nil},
			{"both and different", downward, schema.ExposureUpward, 0, node.ErrExposureMismatch},
			{"a value no definition can state", nil, schema.Exposure(4), 0, schema.ErrUnknownExposure},
		}
		for i, c := range cases {
			handle, err := h.node.Register(node.Instance{
				Definition: c.definition,
				Owner:      h.owner,
				Name:       "Site1",
				Instance:   fractal.InstanceID(strconv.Itoa(i + 1)),
				Exposure:   c.exposure,
			})
			var got schema.Exposure
			if err == nil {
				got = handle.Exposure()
			}
			if got != c.want || !errors.Is(err, c.err) {
				t.Errorf("%s: Register gave %s with the error %v, want %s with %v", c.name, got, err, c.want, c.err)
			}
		}
	})
}
