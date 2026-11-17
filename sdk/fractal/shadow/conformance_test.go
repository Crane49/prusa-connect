package shadow_test

import (
	"testing"

	conf "github.com/prusa3d/prusa-connect/sdk/fractal/internal/conformance"
)

// conformanceRow pairs one rule of the specification with the name of the test
// that checks it. The conformance package has the functions that check the
// table, and every package under sdk/fractal uses them.
type conformanceRow = conf.Row

// conformance lists every rule of the specification sections this package
// implements, each with its test. The sections are Shadow, Shadow Selection,
// Shadow Status, the Synchronization sections, the REP message and the LOS
// message. Must is the sentence of the specification, shortened.
var conformance = []conformanceRow{
	{
		Section: "Shadow",
		Must:    "each shadow interface has an updatedAt timestamp, null while it is EMPTY",
		Test:    "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Shadow Selection",
		Must:    "a node is not required to know its descendants in advance",
		Test:    "TestApplyREPPatternCreatesInterface",
	},
	{
		Section: "Shadow Selection",
		Must:    "a node rejects a pattern with a character outside the identifier set and the wildcard",
		Test:    "TestNewRefusesABadSelection",
	},
	{
		Section: "Shadow Selection",
		Must:    "a matching pattern selects every instance of that interface",
		Test:    "TestStoreSelects",
	},
	{
		Section: "Shadow Selection",
		Must:    "selecting nothing is permitted and means the node holds no shadow",
		Test:    "TestNewSelectsNothing",
	},
	{
		Section: "Shadow Selection",
		Must:    "an interface a pattern selects is created when the first matching REP arrives",
		Test:    "TestApplyREPPatternCreatesInterface",
	},
	{
		Section: "Shadow Selection",
		Must:    "a shadow interface is created as EMPTY, so the move into OK is always EMPTY to OK",
		Test:    "TestRemovedInterfaceIsCreatedAgainAsEmpty",
	},
	{
		Section: "Shadow Selection",
		Must:    "a REP for an interface the node does not select creates no shadow interface",
		Test:    "TestApplyREPNotSelected",
	},
	{
		Section: "Shadow Status",
		Must:    "a created shadow interface has updatedAt null",
		Test:    "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Shadow Status",
		Must:    "EMPTY or STALE moves to OK when state information is received",
		Test:    "TestStaleInterfaceTakesTheNextReport",
	},
	{
		Section: "Shadow Status",
		Must:    "OK moves to STALE when connectivity is lost",
		Test:    "TestMarkStaleCoversTheSubtree",
	},
	{
		Section: "Shadow Status",
		Must:    "updatedAt is unchanged when an interface goes STALE",
		Test:    "TestMarkStaleCoversTheSubtree",
	},
	{
		Section: "Shadow Status",
		Must:    "EMPTY stays EMPTY when connectivity is lost and updatedAt stays null",
		Test:    "TestMarkStaleLeavesEmptyInterfacesEmpty",
	},
	{
		Section: "Shadow Status",
		Must:    "every other transition is forbidden",
		Test:    "TestStatusTransitions",
	},
	{
		Section: "Shadow Status",
		Must:    "no operation reaches a forbidden transition",
		Test:    "TestForbiddenTransitionsCannotHappen",
	},
	{
		Section: "Shadow Status",
		Must:    "the shadow of a node permanently removed from the hierarchy is deleted",
		Test:    "TestRemoveNodeDeletesTheSubtree",
	},
	{
		Section: "Synchronization of the Shadow - Happy path",
		Must:    "every selected interface the node can enumerate in advance is created as EMPTY",
		Test:    "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Synchronization of the Shadow - Reaction to Connection Loss and Reconnection",
		Must:    "a full state report after a reconnection puts the shadow back into OK",
		Test:    "TestSpecExamples",
	},
	{
		Section: "Reported Properties Message",
		Must:    "an interface selected by a pattern and not present is created as EMPTY first",
		Test:    "TestApplyREPPatternCreatesInterface",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a timestamp strictly below the current updatedAt is discarded and not propagated",
		Test:    "TestApplyREPStaleGuard",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the guard does not trigger while updatedAt is null",
		Test:    "TestApplyREPEmptyInterfaceTakesAnyTimestamp",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the stored property values are replaced and no property left out is removed",
		Test:    "TestApplyREPMergesProps",
	},
	{
		Section: "Reported Properties Message",
		Must:    "updatedAt is set to the timestamp field of the message",
		Test:    "TestApplyREPStaleGuard",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the shadow interface is marked OK",
		Test:    "TestSpecExamples",
	},
	{
		Section: "Connection Loss Message",
		Must:    "every OK interface of the lost child goes STALE and every EMPTY one stays EMPTY",
		Test:    "TestMarkStaleLeavesEmptyInterfacesEmpty",
	},
	{
		Section: "Connection Loss Message",
		Must:    "every shadowed node whose path starts with the lost path and a slash goes STALE",
		Test:    "TestMarkStaleCoversTheSubtree",
	},
	{
		Section: "Connection Loss Message",
		Must:    "the affected node path is the sender and the child joined by a slash",
		Test:    "TestApplyLOS",
	},
	{
		Section: "Examples (Informational)",
		Must:    "the six worked examples reach the shadow the document prints",
		Test:    "TestSpecExamples",
	},
	{
		Section: "Application-Level Semantics (Informational)",
		Must:    "an OK value was received after the interface was last EMPTY or STALE",
		Test:    "TestSpecExamples",
	},
	{
		Section: "Application-Level Semantics (Informational)",
		Must:    "a STALE value is the last value received, because STALE is reached only from OK",
		Test:    "TestStatusTransitions",
	},
	{
		Section: "Shadow Selection",
		Must:    "an enumeration entry with no instance id selects every instance, as a pattern does",
		Test:    "TestAnEntryWithNoInstanceSelectsEveryInstance",
	},
	{
		Section: "Shadow Selection",
		Must:    "an entry with no instance id is created on the first report that matches it",
		Test:    "TestAnEntryWithNoInstanceSelectsEveryInstance",
	},
	{
		Section: "Synchronization of the Ancestor Shadow - Happy path",
		Must:    "every selected interface of a node above that the node can enumerate is created as EMPTY",
		Test:    "TestRebaseKeepsOnlyTheAncestors",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a node taking another node path discards its ancestor shadow in full",
		Test:    "TestRebaseKeepsOnlyTheAncestors",
	},
	{
		Section: "After the Join Exchange",
		Must:    "no report from a former parent creates a node that is not above the new path",
		Test:    "TestConcurrentApplyAndRebase",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a child whose admission is revoked can discard its ancestor shadow in full",
		Test:    "TestRebaseWithNoPathDropsEverything",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "every OK interface of every node above the sender goes STALE",
		Test:    "TestMarkStaleAboveSparesTheSender",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "the sender of the ALOS is not marked",
		Test:    "TestMarkStaleAboveSparesTheSender",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "an EMPTY interface above the sender stays EMPTY",
		Test:    "TestMarkStaleAboveLeavesEmptyInterfacesEmpty",
	},
}

// TestConformanceTableNamesRealTests fails when the table names a test this
// package does not have.
func TestConformanceTableNamesRealTests(t *testing.T) {
	t.Parallel()
	conf.NamesRealTests(t, conformance)
}

// TestConformanceTableIsWellFormed fails when a row has an empty field and
// when the same Must is in the table twice.
func TestConformanceTableIsWellFormed(t *testing.T) {
	t.Parallel()
	conf.IsWellFormed(t, conformance)
}
