package files_test

import (
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// member is one property or method of a definition, as its public methods
// report it.
type member struct {
	Name     string
	Type     string
	Nullable bool
	Writable bool
	Params   []string
}

// definitionShape is a definition as its public methods report it.
type definitionShape struct {
	Name       string
	Exposure   string
	Properties []member
	Methods    []member
}

// shapeOf reads d through its public methods.
func shapeOf(d *schema.Definition) definitionShape {
	shape := definitionShape{Name: string(d.Name()), Exposure: d.Exposure().String()}
	for p := range d.Properties() {
		shape.Properties = append(shape.Properties, member{
			Name: string(p.Name()), Type: p.Type().String(), Nullable: p.Nullable(), Writable: p.Writable(),
		})
	}
	for m := range d.Methods() {
		result, _ := m.Result()
		method := member{Name: string(m.Name()), Type: result.String()}
		for p := range m.Params() {
			method.Params = append(method.Params, string(p.Name())+" "+p.Type().String())
		}
		shape.Methods = append(shape.Methods, method)
	}
	return shape
}

func TestContentStore1Definition(t *testing.T) {
	t.Parallel()
	registry, err := schema.Load(files.Definitions)
	if err != nil {
		t.Fatalf("schema.Load: %v", err)
	}
	def, ok := registry.Definition(files.InterfaceName)
	if !ok || registry.NumDefinitions() != 1 {
		t.Fatalf(
			"the files define %d interfaces, and %s is there: %v",
			registry.NumDefinitions(),
			files.InterfaceName,
			ok,
		)
	}
	want := definitionShape{
		Name:     "ContentStore1",
		Exposure: "both",
		Properties: []member{
			{Name: "rootHash", Type: "string", Nullable: true},
			{Name: "mediaPresent", Type: "bool"},
			{Name: "removable", Type: "bool"},
			{Name: "capacity", Type: "int", Nullable: true},
			{Name: "free", Type: "int", Nullable: true},
		},
		Methods: []member{{Name: "List", Type: "string", Params: []string{"identity string"}}},
	}
	if got := shapeOf(def); !reflect.DeepEqual(got, want) {
		t.Fatalf("ContentStore1 is %+v, want %+v", got, want)
	}
}
