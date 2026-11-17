package schema_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// raws builds the params array of an RPC from raw JSON.
func raws(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, v := range values {
		out = append(out, json.RawMessage(v))
	}
	return out
}

// TestValidateDES tests the rules of the Desired Properties Message section of
// fractal.md, with props ValidateDES must accept and props it must refuse.
func TestValidateDES(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	cases := []struct {
		name     string
		props    fractal.Props
		want     error
		property fractal.PropertyName
	}{
		{"an empty message", props(), nil, ""},
		{"one writable property", props("label", `"hi"`), nil, ""},
		{"several writable properties", props("label", `"hi"`, "count", `3`, "onoff", `true`), nil, ""},
		{"a nullable property set to null", props("maybe", `null`), nil, ""},
		{"an unknown name", props("nothing", `1`), schema.ErrUnknownProperty, "nothing"},
		{"a read-only property", props("reported", `1`), schema.ErrReadOnly, "reported"},
		{"a nullable read-only property", props("sensor", `null`), schema.ErrReadOnly, "sensor"},
		{"a value of the wrong type", props("count", `"3"`), schema.ErrWrongType, "count"},
		{"null for a property that is not nullable", props("count", `null`), schema.ErrNotNullable, "count"},
		{"a value outside the enumeration", props("colour", `"PINK"`), schema.ErrUnknownEnumValue, "colour"},
		{"a value that is not JSON", props("label", `"hi`), schema.ErrMalformed, "label"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := d.ValidateDES(c.props)
			if !errors.Is(err, c.want) {
				t.Fatalf("ValidateDES = %v, want %v", err, c.want)
			}
			if c.want == nil {
				return
			}
			var pe *schema.PropertyError
			if !errors.As(err, &pe) {
				t.Fatalf("ValidateDES returned %T, want a *schema.PropertyError", err)
			}
			if pe.Property != c.property || pe.Interface != "Sample1" {
				t.Fatalf("the PropertyError names %s.%s, want Sample1.%s", pe.Interface, pe.Property, c.property)
			}
		})
	}
}

// TestValidateDESRejectsTheWholeMessage tests that ValidateDES returns an error
// for props with one invalid property, when every other property is valid.
func TestValidateDESRejectsTheWholeMessage(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	err := d.ValidateDES(props("label", `"hi"`, "count", `3`, "nothing", `1`))
	if !errors.Is(err, schema.ErrUnknownProperty) {
		t.Fatalf("ValidateDES = %v, want ErrUnknownProperty", err)
	}
}

// TestValidateREP tests that ValidateREP accepts a read-only property, and
// still checks the type and the name of each property.
func TestValidateREP(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	if err := d.ValidateREP(props("reported", `1`, "sensor", `null`)); err != nil {
		t.Fatalf("ValidateREP refused valid read-only properties: %v", err)
	}
	if err := d.ValidateREP(props("reported", `"1"`)); !errors.Is(err, schema.ErrWrongType) {
		t.Fatalf("ValidateREP = %v, want ErrWrongType", err)
	}
	if err := d.ValidateREP(props("nothing", `1`)); !errors.Is(err, schema.ErrUnknownProperty) {
		t.Fatalf("ValidateREP = %v, want ErrUnknownProperty", err)
	}
}

// TestValidateParams tests the rules of the Method Invocation Request Message
// section of fractal.md, with params ValidateParams must accept and params it
// must refuse.
func TestValidateParams(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	cases := []struct {
		name   string
		method fractal.MethodName
		params []json.RawMessage
		want   error
		index  int
	}{
		{"no parameters and none sent", "Ping", nil, nil, 0},
		{"no parameters and an empty array", "Ping", raws(), nil, 0},
		{"no parameters and one sent", "Ping", raws(`1`), schema.ErrParamCount, schema.NoParamIndex},
		{"every parameter sent", "Move", raws(`1`, `2`, `3`), nil, 0},
		{"the optional one left off", "Move", raws(`1`, `2`), nil, 0},
		{"the optional one sent as null", "Move", raws(`1`, `2`, `null`), nil, 0},
		{"too few parameters", "Move", raws(`1`), schema.ErrParamCount, schema.NoParamIndex},
		{"too many parameters", "Move", raws(`1`, `2`, `3`, `4`), schema.ErrParamCount, schema.NoParamIndex},
		{"a parameter of the wrong type", "Move", raws(`1`, `"2"`), schema.ErrWrongType, 1},
		{"a required parameter sent as null", "Move", raws(`1`, `null`), schema.ErrNotNullable, 1},
		{"an enumeration parameter", "Paint", raws(`"RED"`), nil, 0},
		{"a value outside the enumeration", "Paint", raws(`"PINK"`), schema.ErrUnknownEnumValue, 0},
		{"an unknown method", "Fly", nil, schema.ErrUnknownMethod, schema.NoParamIndex},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := d.ValidateParams(c.method, c.params)
			if !errors.Is(err, c.want) {
				t.Fatalf("ValidateParams = %v, want %v", err, c.want)
			}
			if c.want == nil {
				return
			}
			var pe *schema.ParamError
			if !errors.As(err, &pe) {
				t.Fatalf("ValidateParams returned %T, want a *schema.ParamError", err)
			}
			if pe.Index != c.index {
				t.Fatalf("ParamError.Index = %d, want %d", pe.Index, c.index)
			}
			if pe.Code() != fractal.CodeInvalidParams {
				t.Fatalf("ParamError.Code = %s, want ERR_INVALID_PARAMS", pe.Code())
			}
		})
	}
}

// TestValidateResult tests ValidateResult with methods that return nothing, a
// bool and an int, and with results that match the return type and results
// that do not.
func TestValidateResult(t *testing.T) {
	t.Parallel()
	d := sampleDefinition(t)
	cases := []struct {
		name   string
		method fractal.MethodName
		result string
		want   error
	}{
		{"the method returns nothing and the result is absent", "Ping", ``, nil},
		{"the method returns nothing and the result is present", "Ping", `1`, schema.ErrUnexpectedResult},
		{"the method returns a bool and the result is a bool", "Paint", `true`, nil},
		{"the method returns a bool and the result is a number", "Paint", `1`, schema.ErrWrongType},
		{"the method returns a bool and the result is absent", "Paint", ``, schema.ErrMissingResult},
		{"the method returns an int and the result is null", "Count", `null`, schema.ErrNotNullable},
		{"the method returns an int and the result is an int", "Count", `7`, nil},
		{"an unknown method", "Fly", `1`, schema.ErrUnknownMethod},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := d.ValidateResult(c.method, []byte(c.result))
			if !errors.Is(err, c.want) {
				t.Fatalf("ValidateResult = %v, want %v", err, c.want)
			}
		})
	}
}

// TestRegistryValidatesWholeMessages tests the Validate methods of Registry.
// Each takes a whole message and finds the interface by the name in the
// message, and each returns ErrUnknownInterface for a name the registry does
// not have.
func TestRegistryValidatesWholeMessages(t *testing.T) {
	t.Parallel()
	r := loadFiles(t, sampleFile)

	des := fractal.DES{
		Version:   fractal.Version,
		Receiver:  "hq/brno/printer-1",
		Interface: "Sample1",
		Instance:  "1",
		Props:     props("label", `"hi"`),
	}
	if err := r.ValidateDES(des); err != nil {
		t.Fatalf("ValidateDES: %v", err)
	}
	des.Interface = "Other1"
	if err := r.ValidateDES(des); !errors.Is(err, schema.ErrUnknownInterface) {
		t.Fatalf("ValidateDES = %v, want ErrUnknownInterface", err)
	}

	rep := fractal.REP{
		Version:   fractal.Version,
		Sender:    "hq/brno/printer-1",
		Interface: "Sample1",
		Instance:  "1",
		Props:     props("reported", `1`),
	}
	if err := r.ValidateREP(rep); err != nil {
		t.Fatalf("ValidateREP: %v", err)
	}
	rep.Interface = "Other1"
	if err := r.ValidateREP(rep); !errors.Is(err, schema.ErrUnknownInterface) {
		t.Fatalf("ValidateREP = %v, want ErrUnknownInterface", err)
	}

	rpc := fractal.RPC{
		Version:   fractal.Version,
		ID:        "abc",
		Sender:    "hq",
		Receiver:  "hq/brno/printer-1",
		Interface: "Sample1",
		Instance:  "1",
		Method:    "Move",
		Params:    raws(`1`, `2`),
	}
	if err := r.ValidateRPC(rpc); err != nil {
		t.Fatalf("ValidateRPC: %v", err)
	}
	rpc.Interface = "Other1"
	rpcErr := r.ValidateRPC(rpc)
	if !errors.Is(rpcErr, schema.ErrUnknownInterface) {
		t.Fatalf("ValidateRPC = %v, want ErrUnknownInterface", rpcErr)
	}
	var pe *schema.ParamError
	if !errors.As(rpcErr, &pe) || pe.Code() != fractal.CodeInvalidParams {
		t.Fatalf("ValidateRPC of an unknown interface = %v, want a *ParamError with ERR_INVALID_PARAMS", rpcErr)
	}

	res := fractal.RES{Version: fractal.Version, ID: "abc", Receiver: "hq", Result: []byte(`true`)}
	if resErr := r.ValidateRES("Sample1", "Paint", res); resErr != nil {
		t.Fatalf("ValidateRES: %v", resErr)
	}
	if resErr := r.ValidateRES("Other1", "Paint", res); !errors.Is(resErr, schema.ErrUnknownInterface) {
		t.Fatalf("ValidateRES = %v, want ErrUnknownInterface", resErr)
	}
	failed := fractal.RES{
		Version:  fractal.Version,
		ID:       "abc",
		Receiver: "hq",
		Error:    &fractal.Error{Code: fractal.CodeInternal},
	}
	if resErr := r.ValidateRES("Sample1", "Paint", failed); resErr != nil {
		t.Fatalf("ValidateRES refused a RES with an error and no result: %v", resErr)
	}
}

// TestErrorTextNamesTheFault tests the text of each error type. The text must
// name the interface and the invalid property, parameter, method, result or
// file.
func TestErrorTextNamesTheFault(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			"a property",
			&schema.PropertyError{Interface: "Sample1", Property: "count", Err: schema.ErrWrongType},
			"Sample1.count: value does not have the declared type",
		},
		{
			"an interface",
			&schema.PropertyError{Interface: "Sample1", Err: schema.ErrUnknownInterface},
			"Sample1: interface is not in the registry",
		},
		{
			"a parameter",
			&schema.ParamError{
				Interface: "Sample1", Method: "Move", Param: "y", Index: 1, Err: schema.ErrNotNullable,
			},
			"Sample1.Move param 1 y: value is null and the definition forbids it",
		},
		{
			"a method",
			&schema.ParamError{
				Interface: "Sample1", Method: "Fly", Index: schema.NoParamIndex, Err: schema.ErrUnknownMethod,
			},
			"Sample1.Fly: method is not in the interface",
		},
		{
			"a result",
			&schema.ResultError{Interface: "Sample1", Method: "Ping", Err: schema.ErrUnexpectedResult},
			"Sample1.Ping result: method returns nothing and a result is present",
		},
		{
			"a file",
			&schema.LoadError{Source: "a.json", Where: "properties[0].type", Err: schema.ErrUnknownType},
			"a.json properties[0].type: type is not a built-in type or a declared enumeration",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.err.Error(); got != c.want {
				t.Fatalf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}
