package schema_test

import (
	"testing"

	conf "github.com/prusa3d/prusa-connect/sdk/fractal/internal/conformance"
)

// conformanceRow pairs one requirement of fractal.md with the name of the test
// that tests it. Package internal/conformance has the checks that run over the
// table, and every package under sdk/fractal uses the same checks.
type conformanceRow = conf.Row

// conformance lists the requirements of fractal.md about the validation of
// messages. Other packages under sdk/fractal test routing, delivery and the
// shadow.
var conformance = []conformanceRow{
	{
		Section: "Desired Properties Message",
		Must:    "the node validates the requested props against its interface definition",
		Test:    "TestValidateDES",
	},
	{
		Section: "Desired Properties Message",
		Must:    "an unknown property name rejects the DES",
		Test:    "TestValidateDESRejectsTheWholeMessage",
	},
	{
		Section: "Desired Properties Message",
		Must:    "a value whose type does not match the definition rejects the DES",
		Test:    "TestCheckByType",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "invalid parameters are answered with ERR_INVALID_PARAMS",
		Test:    "TestValidateParams",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the order of parameters matches the order in the method signature",
		Test:    "TestLoadKeepsTheOrderOfTheFile",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a method with no parameters takes no params field",
		Test:    "TestValidateParams",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a method with no return value sends no result field",
		Test:    "TestValidateResult",
	},
	{
		Section: "Property",
		Must:    "a property name follows the identifier grammar",
		Test:    "TestLoadRefusesABadFile",
	},
	{
		Section: "Method",
		Must:    "a method has a fixed set of parameters with types and optionally a return type",
		Test:    "TestLoadAcceptsAWellFormedFile",
	},
	{
		Section: "Interface",
		Must:    "a property is identified by a name unique within its interface",
		Test:    "TestBuildRefusesAnUnresolvableFile",
	},
	{
		Section: "Conventions (domain-common.md)",
		Must:    "callers must not request changes to a read-only property",
		Test:    "TestPrusaDESAgainstAPrinter",
	},
	{
		Section: "Conventions (domain-common.md)",
		Must:    "a nullable property may be null and another may not",
		Test:    "TestIsNull",
	},
}

// TestConformanceTableNamesRealTests fails when the table names a test this
// package does not have.
func TestConformanceTableNamesRealTests(t *testing.T) {
	t.Parallel()
	conf.NamesRealTests(t, conformance)
}

// TestConformanceTableIsWellFormed fails on an empty cell and on a rule
// written twice.
func TestConformanceTableIsWellFormed(t *testing.T) {
	t.Parallel()
	conf.IsWellFormed(t, conformance)
}
