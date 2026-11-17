package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/prusa"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// A node validates every DES and every RPC before the code that implements the
// interface receives it, and one server may do this for hundreds of printers.
// The benchmarks below measure validation and report its allocations. The goal
// is no allocation for a valid message.

// benchRegistry loads the Prusa definitions for a benchmark.
func benchRegistry(b *testing.B) *schema.Registry {
	b.Helper()
	r, err := schema.Load(prusa.Files)
	if err != nil {
		b.Fatalf("load the Prusa definitions: %v", err)
	}
	return r
}

func BenchmarkValidateDES(b *testing.B) {
	d := benchDefinition(b)
	p := fractal.Props{
		"label":  json.RawMessage(`"hall 2"`),
		"count":  json.RawMessage(`100`),
		"ratio":  json.RawMessage(`45.5`),
		"onoff":  json.RawMessage(`true`),
		"colour": json.RawMessage(`"BLUE"`),
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := d.ValidateDES(p); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateDESPrusa measures ValidateDES with Chamber1, a definition
// loaded from the Prusa definition files.
func BenchmarkValidateDESPrusa(b *testing.B) {
	r := benchRegistry(b)
	d, ok := r.Definition("Chamber1")
	if !ok {
		b.Fatal("Chamber1 is not in the registry")
	}
	p := fractal.Props{
		"targetChamber":   json.RawMessage(`45.5`),
		"targetFan1Speed": json.RawMessage(`60`),
		"targetFan2Speed": json.RawMessage(`null`),
		"ledIntensity":    json.RawMessage(`100`),
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := d.ValidateDES(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateRPCParams(b *testing.B) {
	r := benchRegistry(b)
	d, ok := r.Definition("FDMPrinterHead1")
	if !ok {
		b.Fatal("FDMPrinterHead1 is not in the registry")
	}
	params := []json.RawMessage{
		json.RawMessage(`"HARDENED"`),
		json.RawMessage(`0.6`),
		json.RawMessage(`true`),
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := d.ValidateParams("SetNozzle", params); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckFloat(b *testing.B) {
	d := benchDefinition(b)
	p, _ := d.Property("ratio")
	raw := json.RawMessage(`215.75`)
	b.ReportAllocs()
	for b.Loop() {
		if err := p.Check(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckEnum(b *testing.B) {
	d := benchDefinition(b)
	p, _ := d.Property("colour")
	raw := json.RawMessage(`"BLUE"`)
	b.ReportAllocs()
	for b.Loop() {
		if err := p.Check(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckInt(b *testing.B) {
	d := benchDefinition(b)
	p, _ := d.Property("count")
	raw := json.RawMessage(`9223372036854775807`)
	b.ReportAllocs()
	for b.Loop() {
		if err := p.Check(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// benchDefinition builds the sample definition for a benchmark.
func benchDefinition(b *testing.B) *schema.Definition {
	b.Helper()
	builder := schema.NewBuilder()
	if err := builder.AddJSON("sample.json", []byte(sampleFile)); err != nil {
		b.Fatal(err)
	}
	r, err := builder.Build()
	if err != nil {
		b.Fatal(err)
	}
	d, ok := r.Definition("Sample1")
	if !ok {
		b.Fatal("Sample1 is not in the registry")
	}
	return d
}
