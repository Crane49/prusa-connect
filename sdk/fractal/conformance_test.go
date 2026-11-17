package fractal_test

import (
	"testing"

	conf "github.com/prusa3d/prusa-connect/sdk/fractal/internal/conformance"
)

// conformanceRow pairs one requirement of fractal.md with the name of the test
// that tests it. Package internal/conformance has the checks that run over the
// table, and every package under sdk/fractal uses the same checks.
type conformanceRow = conf.Row

// conformance lists the requirements of fractal.md that this package can test
// without a shadow, a join exchange or a running node. The packages shadow,
// join, schema and fractaltest test the other requirements, each with a table
// like this one.
var conformance = []conformanceRow{
	{
		Section: "Protocol Version",
		Must:    "the current version of the protocol is 1",
		Test:    "TestVersionIsOne",
	},
	{
		Section: "Protocol Version",
		Must:    "a node checks the version field of every message",
		Test:    "TestEveryMessageCarriesVersion",
	},
	{
		Section: "Identifier",
		Must:    "an identifier starts with a letter or an underscore",
		Test:    "TestIdentifierGrammar",
	},
	{
		Section: "Node API",
		Must:    "an interface instance id may start with a digit",
		Test:    "TestInstanceIDGrammar",
	},
	{
		Section: "Node ID",
		Must:    "a node id does not start with a hyphen and is at most 64 characters",
		Test:    "TestNodeIDGrammar",
	},
	{
		Section: "Node Path",
		Must:    "a node path is at most 255 characters",
		Test:    "TestNodePathGrammar",
	},
	{
		Section: "The Join Exchange",
		Must:    "the parent assigns its own node path, a slash and the requested node id",
		Test:    "TestNodePathChild",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "the node path followed by a slash is a prefix of the receiver",
		Test:    "TestNodePathIsAncestorOf",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "forward to the child whose node id is the next segment of the receiver",
		Test:    "TestNodePathNextHop",
	},
	{
		Section: "Sender Validation",
		Must:    "the sender is that child's node path, optionally followed by further segments",
		Test:    "TestNodePathCovers",
	},
	{
		Section: "Shadow Selection",
		Must:    "a node rejects a pattern with a character outside the identifier set and the wildcard",
		Test:    "TestPatternRejectsOtherCharacters",
	},
	{
		Section: "Shadow Selection",
		Must:    "a pattern matches the whole interface name and is case-sensitive",
		Test:    "TestPatternMatch",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a method with no parameters sends no params field",
		Test:    "TestRPCParamsOmittedWhenEmpty",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the expiry check comes first",
		Test:    "TestRPCExpired",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a failed call carries an error and no result",
		Test:    "TestRESResultAbsentOnFailure",
	},
	{
		Section: "Connection Loss Message",
		Must:    "the affected node path is the sender and the child joined by a slash",
		Test:    "TestLOSAffected",
	},
	{
		Section: "Error Codes",
		Must:    "the defined codes are used with the values and the ranges of the table",
		Test:    "TestDefinedCodeValues",
	},
	{
		Section: "Error Codes",
		Must:    "bits 12 to 9 are zero",
		Test:    "TestCodeReservedBits",
	},
	{
		Section: "Error Codes",
		Must:    "a code in 0x6000 to 0x61FF is not used",
		Test:    "TestCodeReservedRange",
	},
	{
		Section: "Error Codes",
		Must:    "a code matches a base code whatever its refinement",
		Test:    "TestCodeIs",
	},
	{
		Section: "Error Codes",
		Must:    "the joining node decides from the code whether to retry",
		Test:    "TestJoinRetry",
	},
	{
		Section: "Join Messages",
		Must:    "a join denial carries one of the join denial codes",
		Test:    "TestJoinDenialValidate",
	},
	{
		Section: "Join Messages",
		Must:    "a receiver ignores a field it does not recognize",
		Test:    "TestDecodeIgnoresUnknownFields",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "an AREP has the fields of a REP",
		Test:    "TestGoldenAREP",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "an ALOS carries the version and the node path of the node that lost its parent",
		Test:    "TestGoldenALOS",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "an ARBR carries only the version",
		Test:    "TestGoldenARBR",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "ARBR travels up, and AREP and ALOS travel down, each to every node on the way",
		Test:    "TestKindTable",
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
