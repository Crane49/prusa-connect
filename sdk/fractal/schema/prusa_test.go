package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/prusa"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// specDirEnv is the environment variable that holds the path of the folder
// with the documents of the Fractal specification. A developer sets it to that
// folder in a checkout of the specification.
// TestPrusaFilesMatchTheSpecification skips when it is not set, because a CI
// machine has no checkout of the specification.
const specDirEnv = "FRACTAL_SPECS_DIR"

// vocabularyFiles are the documents of the specification that define the
// Prusa printer interfaces: the interfaces of every kind of device, and those
// of a 3D printer.
var vocabularyFiles = []string{"domain-common.md", "domain-3d-print.md"}

// prusaInterfaceCount is the number of interfaces the vocabulary defines.
const prusaInterfaceCount = 11

// prusaEnumCount is the number of enumerations the vocabulary defines.
const prusaEnumCount = 4

// loadPrusa builds a registry from the definition files embedded in package
// sdk/fractal/prusa. The Fractal module loads the same files, so these tests
// check the definitions that the module serves.
func loadPrusa(t *testing.T) *schema.Registry {
	t.Helper()
	r, err := schema.Load(prusa.Files)
	if err != nil {
		t.Fatalf("load the Prusa definitions: %v", err)
	}
	return r
}

// TestPrusaFilesLoad tests that the file format can describe a real interface
// specification: every interface and enumeration of the vocabulary loads.
// Package schema has no code that is specific to printers.
func TestPrusaFilesLoad(t *testing.T) {
	t.Parallel()
	r := loadPrusa(t)
	if r.NumDefinitions() != prusaInterfaceCount {
		t.Fatalf("loaded %d interfaces, want %d", r.NumDefinitions(), prusaInterfaceCount)
	}
	if r.NumEnums() != prusaEnumCount {
		t.Fatalf("loaded %d enumerations, want %d", r.NumEnums(), prusaEnumCount)
	}
	for _, name := range []fractal.InterfaceName{
		"Device1", "UpgradableDevice1", "Printer1", "Controller1", "FDMPrinter1",
		"SLAPrinter1", "FDMPrinterHead1", "PrintJob1", "FDMPrintJob1",
		"FilamentSlot1", "Chamber1",
	} {
		if _, ok := r.Definition(name); !ok {
			t.Fatalf("%s is not in the registry", name)
		}
	}
}

// TestPrusaDESAgainstAPrinter validates DES and REP props for the Prusa
// interfaces FDMPrinterHead1 and Printer1. Some props are valid and some are
// not.
func TestPrusaDESAgainstAPrinter(t *testing.T) {
	t.Parallel()
	r := loadPrusa(t)
	head, _ := r.Definition("FDMPrinterHead1")
	printer, _ := r.Definition("Printer1")

	if err := head.ValidateDES(props("targetNozzle", `215.5`)); err != nil {
		t.Fatalf("ValidateDES refused targetNozzle 215.5: %v", err)
	}
	if err := head.ValidateDES(props("targetNozzle", `null`)); err != nil {
		t.Fatalf("ValidateDES refused targetNozzle null, which turns the heater off: %v", err)
	}
	if err := head.ValidateDES(props("tempNozzle", `215.5`)); err == nil {
		t.Fatal("ValidateDES accepted the read-only property tempNozzle")
	}
	if err := printer.ValidateDES(props("speed", `100`)); err != nil {
		t.Fatalf("ValidateDES refused speed 100: %v", err)
	}
	if err := printer.ValidateDES(props("speed", `100.0`)); err == nil {
		t.Fatal("ValidateDES accepted 100.0 for the int property speed")
	}
	if err := printer.ValidateDES(props("state", `"READY"`)); err == nil {
		t.Fatal("ValidateDES accepted the read-only property state")
	}
	if err := printer.ValidateREP(props("state", `"READY"`)); err != nil {
		t.Fatalf("ValidateREP refused the read-only property state: %v", err)
	}
	if err := printer.ValidateREP(props("state", `"WARMING"`)); err == nil {
		t.Fatal("ValidateREP accepted the state WARMING, which PrinterState does not list")
	}
}

// TestPrusaRPCAgainstAPrinter validates RPC params for methods of the Prusa
// interfaces FDMPrinter1 and FDMPrinterHead1. MoveRel has an optional
// parameter.
func TestPrusaRPCAgainstAPrinter(t *testing.T) {
	t.Parallel()
	r := loadPrusa(t)
	fdm, _ := r.Definition("FDMPrinter1")
	head, _ := r.Definition("FDMPrinterHead1")

	if err := fdm.ValidateParams("Home", raws(`true`, `true`, `false`)); err != nil {
		t.Fatalf("ValidateParams refused Home with three bool parameters: %v", err)
	}
	if err := fdm.ValidateParams("Home", raws(`true`, `true`)); err == nil {
		t.Fatal("ValidateParams accepted Home with two of its three required parameters")
	}
	if err := fdm.ValidateParams("MoveRel", raws(`1`, `2`, `3`)); err != nil {
		t.Fatalf("ValidateParams refused MoveRel without the optional feedrate: %v", err)
	}
	if err := fdm.ValidateParams("MoveRel", raws(`1`, `2`, `3`, `600`)); err != nil {
		t.Fatalf("ValidateParams refused MoveRel with a feedrate: %v", err)
	}
	if err := fdm.ValidateParams("LevelBed", nil); err != nil {
		t.Fatalf("ValidateParams refused LevelBed, which has no parameters, with nil params: %v", err)
	}
	if err := head.ValidateParams("LoadFilament", raws(`"PETG"`)); err != nil {
		t.Fatalf("ValidateParams refused LoadFilament with the material PETG: %v", err)
	}
	if err := head.ValidateParams("LoadFilament", raws(`"WOOD"`)); err == nil {
		t.Fatal("ValidateParams accepted the material WOOD, which FDMMaterial does not list")
	}
	if err := head.ValidateParams("SetNozzle", raws(`"HARDENED"`, `0.6`, `true`)); err != nil {
		t.Fatalf("ValidateParams refused valid SetNozzle parameters: %v", err)
	}
	if err := head.ValidateParams("SetNozzle", raws(`"HARDENED"`, `0.6`, `1`)); err == nil {
		t.Fatal("ValidateParams accepted the number 1 for the bool parameter of SetNozzle")
	}
}

// TestPrusaFilesMatchTheSpecification reads the vocabulary of the specification
// and fails when an exposure, a property, method, parameter, type, access mode,
// nullable flag or enumeration value differs from the definition files. It
// fails after a change to the vocabulary until the definition files have the
// same change.
func TestPrusaFilesMatchTheSpecification(t *testing.T) {
	t.Parallel()
	dir := os.Getenv(specDirEnv)
	if dir == "" {
		t.Skipf("set %s to the folder of the specification documents to run this comparison", specDirEnv)
	}
	var text strings.Builder
	for _, name := range vocabularyFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s in the folder %s names: %v", name, specDirEnv, err)
		}
		text.Write(data)
		text.WriteByte('\n')
	}
	doc := parseVocabulary(text.String())
	r := loadPrusa(t)
	compareEnums(t, r, doc)
	compareInterfaces(t, r, doc)
}

// compareEnums fails the test when the loaded enumerations differ from the
// enumerations of the vocabulary.
func compareEnums(t *testing.T, r *schema.Registry, doc specDoc) {
	t.Helper()
	if len(doc.EnumOrder) != r.NumEnums() {
		t.Fatalf("the document has %d enumerations, the files have %d", len(doc.EnumOrder), r.NumEnums())
	}
	for _, name := range doc.EnumOrder {
		e, ok := r.Enum(fractal.Identifier(name))
		if !ok {
			t.Errorf("enumeration %s is in the document and not in the files", name)
			continue
		}
		want := doc.Enums[name]
		var got []string
		for v := range e.Values() {
			got = append(got, v)
		}
		if len(got) != len(want) {
			t.Errorf("%s has %d values in the files and %d in the document", name, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s value %d is %q in the files and %q in the document", name, i, got[i], want[i])
			}
		}
	}
}

// compareInterfaces fails the test when the loaded interfaces differ from the
// interfaces of the vocabulary.
func compareInterfaces(t *testing.T, r *schema.Registry, doc specDoc) {
	t.Helper()
	if len(doc.Interfaces) != r.NumDefinitions() {
		t.Fatalf(
			"the document defines %d interfaces, the files have %d",
			len(doc.Interfaces), r.NumDefinitions(),
		)
	}
	for _, want := range doc.Interfaces {
		d, ok := r.Definition(fractal.InterfaceName(want.Name))
		if !ok {
			t.Errorf("interface %s is in the document and not in the files", want.Name)
			continue
		}
		if got := d.Exposure().String(); got != want.Exposure {
			t.Errorf("%s is exposed %s in the files and %s in the document", want.Name, got, want.Exposure)
		}
		compareProps(t, d, want)
		compareMethods(t, d, want)
	}
}

// compareProps fails the test when the properties of one loaded interface
// differ from its property table in the vocabulary.
func compareProps(t *testing.T, d *schema.Definition, want specInterface) {
	t.Helper()
	var got []*schema.Property
	for p := range d.Properties() {
		got = append(got, p)
	}
	if len(got) != len(want.Props) {
		t.Errorf("%s has %d properties in the files and %d in the document",
			want.Name, len(got), len(want.Props))
		return
	}
	for i, w := range want.Props {
		p := got[i]
		if string(p.Name()) != w.Name {
			t.Errorf("%s property %d is %q in the files and %q in the document", want.Name, i, p.Name(), w.Name)
			continue
		}
		if p.Type().String() != w.Type {
			t.Errorf("%s.%s is %s in the files and %s in the document", want.Name, w.Name, p.Type(), w.Type)
		}
		if p.Access().String() != w.Access {
			t.Errorf("%s.%s is %s in the files and %s in the document",
				want.Name, w.Name, p.Access(), w.Access)
		}
		if p.Nullable() != w.Nullable {
			t.Errorf("%s.%s nullable is %v in the files and %v in the document",
				want.Name, w.Name, p.Nullable(), w.Nullable)
		}
	}
}

// compareMethods fails the test when the methods of one loaded interface differ
// from its method table in the vocabulary.
func compareMethods(t *testing.T, d *schema.Definition, want specInterface) {
	t.Helper()
	var got []*schema.Method
	for m := range d.Methods() {
		got = append(got, m)
	}
	if len(got) != len(want.Methods) {
		t.Errorf("%s has %d methods in the files and %d in the document",
			want.Name, len(got), len(want.Methods))
		return
	}
	for i, w := range want.Methods {
		m := got[i]
		if string(m.Name()) != w.Name {
			t.Errorf("%s method %d is %q in the files and %q in the document", want.Name, i, m.Name(), w.Name)
			continue
		}
		compareParams(t, want.Name, m, w)
		result, returns := m.Result()
		switch {
		case w.Returns == "" && returns:
			t.Errorf("%s.%s returns %s in the files and nothing in the document", want.Name, w.Name, result)
		case w.Returns != "" && !returns:
			t.Errorf("%s.%s returns nothing in the files and %s in the document", want.Name, w.Name, w.Returns)
		case w.Returns != "" && result.String() != w.Returns:
			t.Errorf("%s.%s returns %s in the files and %s in the document", want.Name, w.Name, result, w.Returns)
		}
	}
}

// compareParams fails the test when the parameters of one loaded method differ
// from the parameter cell of that method in the vocabulary.
func compareParams(t *testing.T, iface string, m *schema.Method, w specMethod) {
	t.Helper()
	if m.NumParams() != len(w.Params) {
		t.Errorf("%s.%s takes %d parameters in the files and %d in the document",
			iface, w.Name, m.NumParams(), len(w.Params))
		return
	}
	for i, wp := range w.Params {
		p := m.Param(i)
		if string(p.Name()) != wp.Name {
			t.Errorf("%s.%s parameter %d is %q in the files and %q in the document",
				iface, w.Name, i, p.Name(), wp.Name)
			continue
		}
		if p.Type().String() != wp.Type {
			t.Errorf("%s.%s parameter %s is %s in the files and %s in the document",
				iface, w.Name, wp.Name, p.Type(), wp.Type)
		}
		if p.Optional() != wp.Optional {
			t.Errorf("%s.%s parameter %s optional is %v in the files and %v in the document",
				iface, w.Name, wp.Name, p.Optional(), wp.Optional)
		}
	}
}
