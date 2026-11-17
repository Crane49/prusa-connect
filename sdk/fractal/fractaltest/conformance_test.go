package fractaltest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packageDirs maps each package name a row may use to the directory of its
// test files, relative to the directory of this package.
var packageDirs = map[string]string{
	"fractal":     "..",
	"shadow":      "../shadow",
	"join":        "../join",
	"schema":      "../schema",
	"node":        "../node",
	"fractaltest": ".",
}

// conformanceRow connects one MUST sentence of fractal.md with the test that
// proves it. A row fills Package and Test, or it fills Reason and Suite. The
// second form is for a rule that only a transport can prove: Reason says why,
// and Suite names the case of RunBindingSuite that a transport must pass.
type conformanceRow struct {
	// Section is the heading in fractal.md that has the sentence. For a rule
	// of this implementation, it is the heading the rule belongs under.
	Section string
	// Must is the MUST sentence, shortened to its main clause.
	Must string
	// Package is the package that has the test, by its name in packageDirs.
	Package string
	// Test is the name of the test function in that package.
	Test string
	// Reason says why no test in this repository proves the rule.
	Reason string
	// Suite is the name of the RunBindingSuite case that a transport must pass
	// to prove the rule.
	Suite string
	// Ours marks a rule of this implementation. fractal.md leaves the choice
	// open there, so the rule is not part of the specification.
	// TestConformanceTableCountsWhatItCovers counts these rows separately.
	Ours bool
}

// conformance has one row for every MUST sentence of fractal.md. It leaves out
// the RFC 2119 paragraph and the non-normative sections, because they state no
// rule about behaviour.
var conformance = []conformanceRow{
	{
		Section: "Protocol Version",
		Must:    "a node checks the version of every message and drops one it does not support",
		Package: "node", Test: "TestAMalformedMessageOfEveryTypeIsDropped",
	},
	{
		Section: "Protocol Version",
		Must:    "a join request of an unsupported version is denied rather than dropped",
		Package: "join", Test: "TestTheParentDeniesAnUnsupportedProtocolVersion",
	},
	{
		Section: "Identifier",
		Must:    "an identifier starts with a letter or an underscore",
		Package: "fractal", Test: "TestIdentifierGrammar",
	},
	{
		Section: "Underlying Network",
		Must:    "the protocol deduplicates itself where the transport does not",
		Package: "node", Test: "TestTheMemoryDedupForgetsWhatThePastWindowHolds",
	},
	{
		Section: "Node API",
		Must:    "the pair of interface name and instance id is unique within a node",
		Package: "node", Test: "TestRegisterRefusesWhatItCannotUse",
	},
	{
		Section: "Interface",
		Must:    "an interface definition states its exposure: upward, downward or both",
		Package: "schema", Test: "TestLoadReadsTheExposure",
	},
	{
		Section: "Interface Instance",
		Must:    "an instance takes the exposure of its definition, and one that states another is refused",
		Package: "node", Test: "TestRegisterTakesTheExposureOfItsDefinition",
		Ours: true,
	},
	{
		Section: "Node ID",
		Must:    "a node id does not start with a hyphen",
		Package: "fractal", Test: "TestNodeIDGrammar",
	},
	{
		Section: "Node ID",
		Must:    "a node id is at most 64 characters",
		Package: "fractal", Test: "TestNodeIDGrammar",
	},
	{
		Section: "Node Path",
		Must:    "a node assumes no node path before its parent has granted one",
		Package: "node", Test: "TestACallWithNoNodePathIsRefused",
	},
	{
		Section: "Node Path",
		Must:    "a node with no node path sends and processes nothing but the join exchange",
		Package: "node", Test: "TestANodeWithNoPathProcessesNothing",
	},
	{
		Section: "Node Path",
		Must:    "a node path is at most 255 characters",
		Package: "fractal", Test: "TestNodePathGrammar",
	},
	{
		Section: "Authority and Control",
		Must:    "the value of a property is changed only by the node that owns it",
		Package: "node", Test: "TestARejectedChangeIsAnsweredWithNothing",
	},
	{
		Section: "Joining the Network",
		Must:    "joining reconfigures and restarts no node other than the one that joins",
		Package: "join", Test: "TestJoiningReconfiguresNoOtherNode",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "every node has a permanent authentication identity whose possession it can prove",
		Package: "join", Test: "TestTheParentDeniesARequestWhoseProofDoesNotVerify",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "the credentials a grant carries are bound to the identity the child presented",
		Package: "join", Test: "TestTheGrantCarriesEverythingTheChildNeeds",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "a node holding no valid credentials runs the join exchange again",
		Package: "join", Test: "TestTheChildRenewsBeforeTheCredentialsExpire",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "a parent answers such a request while the child is connected and treats it as no loss",
		Package: "join", Test: "TestTheParentAnswersAConnectedChild",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "the transport binding defines when a connection ends after its credentials expire",
		Reason:  "only the transport has the connection, so only a transport can end it",
		Suite:   "a loss signal reaches the node above",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a parent persists every admission and restores it before it answers a request",
		Package: "join", Test: "TestTheParentRestoresItsAdmissionsBeforeItAnswers",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a parent denies a revoked child whatever identity it presents",
		Package: "join", Test: "TestTheParentDeniesARevokedChild",
	},
	{
		Section: "Admission and Revocation",
		Must:    "revoking a child runs the binding's hook that ends the access its credentials give it",
		Package: "join", Test: "TestARevocationHookThatFailsIsReported",
	},
	{
		Section: "The Join Exchange",
		Must:    "the denial carries the code of the first check that fails",
		Package: "join", Test: "TestTheParentAnswersTheFirstCheckThatFails",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 1: a parent with no node path of its own denies every request",
		Package: "join", Test: "TestTheParentWithoutItsOwnNodePathDeniesEveryRequest",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 2: the version of the request is supported",
		Package: "join", Test: "TestTheParentDeniesAnUnsupportedProtocolVersion",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 3: the request parses and its proof of possession verifies",
		Package: "join", Test: "TestTheParentDeniesARequestWhoseProofDoesNotVerify",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 4: the requested node id follows the node id grammar",
		Package: "join", Test: "TestTheParentDeniesANodeIDOutsideItsGrammar",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 5: the assigned node path is within the length bound",
		Package: "join", Test: "TestTheParentDeniesANodePathOverTheBound",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 6: the join credential is verified, and skipped for the holding identity",
		Package: "join", Test: "TestTheParentVerifiesTheJoinCredential",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 7: the child is not revoked and the node id is not held by another",
		Package: "join", Test: "TestTheParentDeniesACollisionInBothDirections",
	},
	{
		Section: "The Join Exchange",
		Must:    "nothing is created for the child until every check has passed",
		Package: "join", Test: "TestTheParentCreatesNothingForADeniedChild",
	},
	{
		Section: "The Join Exchange",
		Must:    "a grant records the admission, issues credentials and carries the assigned path",
		Package: "join", Test: "TestTheGrantCarriesEverythingTheChildNeeds",
	},
	{
		Section: "Uniqueness",
		Must:    "a request contradicting an existing child is denied as a collision",
		Package: "join", Test: "TestTheParentDeniesACollisionInBothDirections",
	},
	{
		Section: "Uniqueness",
		Must:    "the identity that already holds the node id gets fresh credentials and keeps its state",
		Package: "join", Test: "TestTheHoldingIdentityGetsAFreshGrant",
	},
	{
		Section: "Uniqueness",
		Must:    "of requests for one node id from different identities at most one is granted",
		Package: "join", Test: "TestConcurrentRequestsForOneNodeIDGrantAtMostOne",
	},
	{
		Section: "Uniqueness",
		Must:    "a parent does not release a node id because its child disconnected",
		Package: "join", Test: "TestTheParentKeepsANodeIDWhileTheChildIsAway",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a grant of an unsupported version brings up no connection and is not retried",
		Package: "join", Test: "TestTheChildRefusesAGrantOfAnUnsupportedVersion",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child persists the path, the parent identity, the credentials and the endpoint",
		Package: "join", Test: "TestDecideKeepsWhatTheBindingPersisted",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child compares the parent identity before it brings up the connection",
		Package: "join", Test: "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the same parent granting a different path is refused and logged",
		Package: "join", Test: "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "After the Join Exchange",
		Must:    "another parent is adopted only over a different endpoint",
		Package: "join", Test: "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a node taking another path discards what only the former path made routable",
		Package: "node", Test: "TestADifferentNodePathDiscardsWhatItCannotRoute",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a node that holds a child does not join a different parent",
		Package: "join", Test: "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "an upward message addressed to this node is processed here",
		Package: "node", Test: "TestAnAnswerIsActedOnOnce",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "an upward message addressed to another node goes to the parent",
		Package: "node", Test: "TestAnAnswerForAnotherNodeIsPassedUp",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a root node drops an upward message it is not the recipient of",
		Package: "node", Test: "TestARootDropsAnAnswerItIsNotTheCallerOf",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a downward message addressed to this node is processed here",
		Package: "node", Test: "TestAnAcceptedChangeIsReported",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "the node path followed by a slash is a prefix of the receiver",
		Package: "fractal", Test: "TestNodePathIsAncestorOf",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a downward message goes to the child named by the next segment of the receiver",
		Package: "node", Test: "TestAChangeIsRoutedDown",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a downward message for no node below this one is dropped",
		Package: "node", Test: "TestAChangeForNoChildIsDropped",
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a node sends no message to a sibling",
		Package: "fractaltest", Test: "TestASiblingCannotBeAddressed",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "a broadcast is processed and forwarded by every node that receives it",
		Package: "node", Test: "TestAReportIsAppliedAndPassedOn",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "the sender of an upward broadcast sends it to its parent",
		Package: "node", Test: "TestAChildLinkGoingDownReportsTheLoss",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "an upward broadcast stops at a root node",
		Package: "node", Test: "TestARootStopsAnUpwardBroadcast",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "a downward broadcast is forwarded to all of a node's children",
		Package: "node", Test: "TestAFullReportRequestAnswersAndIsPassedOn",
	},
	{
		Section: "Sender Validation",
		Must:    "the sender is that child's node path, optionally followed by further segments",
		Package: "fractal", Test: "TestNodePathCovers",
	},
	{
		Section: "Sender Validation",
		Must:    "a message that fails the check is neither acted on nor forwarded",
		Package: "fractaltest", Test: "TestAForgedSenderIsDropped",
	},
	{
		Section: "Shadow Selection",
		Must:    "a node is not required to know its descendants or their interfaces in advance",
		Package: "shadow", Test: "TestStoreSelects",
	},
	{
		Section: "Shadow Selection",
		Must:    "a pattern holds only identifier characters and the wildcard",
		Package: "fractal", Test: "TestPatternRejectsOtherCharacters",
	},
	{
		Section: "Shadow Selection",
		Must:    "an interface a pattern selects is created on the first report that matches",
		Package: "shadow", Test: "TestApplyREPPatternCreatesInterface",
	},
	{
		Section: "Shadow Selection",
		Must:    "a shadow interface is created empty, before any state is applied to it",
		Package: "shadow", Test: "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Shadow Selection",
		Must:    "a report for an unselected interface creates nothing and is still propagated",
		Package: "shadow", Test: "TestApplyREPNotSelected",
	},
	{
		Section: "Shadow Status",
		Must:    "a created shadow interface has no updated time",
		Package: "shadow", Test: "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Shadow Status",
		Must:    "a report moves an empty or stale interface to ok and sets the reported time",
		Package: "shadow", Test: "TestStatusTransitions",
	},
	{
		Section: "Shadow Status",
		Must:    "a lost connection moves an ok interface to stale and leaves its time alone",
		Package: "shadow", Test: "TestMarkStaleCoversTheSubtree",
	},
	{
		Section: "Shadow Status",
		Must:    "a lost connection leaves an empty interface empty",
		Package: "shadow", Test: "TestMarkStaleLeavesEmptyInterfacesEmpty",
	},
	{
		Section: "Synchronization of the Shadow - Happy path",
		Must:    "a node runs the startup procedure once it holds a node path",
		Package: "node", Test: "TestStartupAsksEveryChildForAFullReport",
	},
	{
		Section: "Synchronization of the Shadow - Happy path",
		Must:    "every interface the node can enumerate in advance is created empty",
		Package: "shadow", Test: "TestNewCreatesEnumeratedInterfacesEmpty",
	},
	{
		Section: "Synchronization of the Shadow - Happy path",
		Must:    "the node asks every child for a full state report",
		Package: "fractaltest", Test: "TestExample1Startup",
	},
	{
		Section: "Synchronization of the Shadow - Reaction to Connection Loss and Reconnection",
		Must:    "a lost connection with a child is handled as the loss message section says",
		Package: "fractaltest", Test: "TestExample5ConnectionLoss",
	},
	{
		Section: "Synchronization of the Shadow - Reaction to Connection Loss and Reconnection",
		Must:    "a child link coming up produces a request for a full state report",
		Package: "node", Test: "TestAChildLinkComingUpAsksForAFullReport",
	},
	{
		Section: "Synchronization of the Shadow - Reaction to Connection Loss and Reconnection",
		Must:    "a request for a full report is answered with the full state of every interface",
		Package: "fractaltest", Test: "TestExample6Reconnection",
	},
	{
		Section: "Synchronization of the Shadow - Reaction to Connection Loss and Reconnection",
		Must:    "a request for a full report is passed to every child",
		Package: "node", Test: "TestAnOwnerThatCannotReadItsStateIsSkipped",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a node reports every change of its own state to its parent",
		Package: "fractaltest", Test: "TestExample2StateChange",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a pattern creates the shadow interface as empty before the report is applied",
		Package: "shadow", Test: "TestApplyREPPatternCreatesInterface",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a report older than the state the shadow holds is discarded silently",
		Package: "node", Test: "TestAnOldReportIsNotPassedOn",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the shadow interface takes the reported property values",
		Package: "shadow", Test: "TestApplyREPMergesProps",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a property the report leaves out keeps the value the shadow holds",
		Package: "shadow", Test: "TestStaleInterfaceTakesTheNextReport",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the updated time of the shadow interface takes the timestamp of the report",
		Package: "shadow", Test: "TestSpecExamples",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the shadow interface is marked ok",
		Package: "shadow", Test: "TestApplyREPEmptyInterfaceTakesAnyTimestamp",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the report is passed to the parent unchanged",
		Package: "node", Test: "TestAReportIsAppliedAndPassedOn",
	},
	{
		Section: "Reported Properties Message",
		Must:    "the sender sets the timestamp to the time the state changed",
		Package: "node", Test: "TestAnAcceptedChangeIsReported",
	},
	{
		Section: "Reported Properties Message",
		Must:    "a full state report has the time the node determined the state, not the time it last changed",
		Package: "node", Test: "TestAFullReportTakesTheClockWhenTheOwnerHasNoTime",
	},
	{
		Section: "Desired Properties Message",
		Must:    "a requested property change is delivered at most once",
		Reason:  "only a transport can duplicate a message, so only a test of a transport can show that it does not",
		Suite:   "best effort is dropped while the link is down",
	},
	{
		Section: "Desired Properties Message",
		Must:    "the recipient validates the props and rejects the whole message on the first fault",
		Package: "schema", Test: "TestValidateDESRejectsTheWholeMessage",
	},
	{
		Section: "Desired Properties Message",
		Must:    "an accepted change updates the state and is reported",
		Package: "node", Test: "TestAnAcceptedChangeIsReported",
	},
	{
		Section: "Desired Properties Message",
		Must:    "a node that is not the recipient routes the request down",
		Package: "node", Test: "TestAChangeIsRoutedDown",
	},
	{
		Section: "Desired Properties Message",
		Must:    "a property the request leaves out stays unchanged",
		Package: "fractaltest", Test: "TestExample3Desired",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the id correlates the answer and detects a request delivered again",
		Package: "node", Test: "TestARepeatedInvocationResendsTheStoredAnswer",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the recipient keeps the answer for as long as the id counts as a duplicate",
		Package: "node", Test: "TestTheMemoryDedupForgetsWhatThePastWindowHolds",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the recipient answers every request it receives",
		Package: "node", Test: "TestTheRecipientAnswersEveryInvocation",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the answer names the caller that sent the request",
		Package: "node", Test: "TestACallSendsAndWaits",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the expiry check comes before the recipient check",
		Package: "fractal", Test: "TestRPCExpired",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "an expired request is dropped and answered with the expiry code",
		Package: "fractaltest", Test: "TestAQueuedInvocationExpires",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a repeated id resends the stored answer and runs the method no second time",
		Package: "fractaltest", Test: "TestARepeatedInvocationRunsOnce",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "an instance or a method the node does not have takes the object-not-found code",
		Package: "node", Test: "TestTheRecipientAnswersEveryInvocation",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the target is resolved before the parameters are validated",
		Package: "node", Test: "TestTheRecipientAnswersEveryInvocation",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "invalid parameters are answered with the invalid-parameters code",
		Package: "schema", Test: "TestValidateParams",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a deliberate refusal runs nothing",
		Package: "node", Test: "TestAGuardRefusesBeforeTheMethodRuns",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the method runs and its result is answered, and a failure takes the internal code",
		Package: "node", Test: "TestASuccessfulInvocationCarriesItsResult",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a node that is not the recipient routes the request down",
		Package: "node", Test: "TestAnInvocationIsRoutedDown",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a request for an unreachable child waits in order and is checked for expiry on delivery",
		Reason:  "only a transport can keep a message while a link is down",
		Suite:   "durable waits while the link is down",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a recipient that cannot exist below this node takes the node-not-found code",
		Package: "node", Test: "TestARequestThatCannotBeRoutedIsAnswered",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "the parameters are in the order the method declares them",
		Package: "schema", Test: "TestValidateParams",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a method with no parameters sends no params field",
		Package: "fractal", Test: "TestRPCParamsOmittedWhenEmpty",
	},
	{
		Section: "Method Invocation Request Message",
		Must:    "a method never runs after its expires timestamp",
		Package: "fractaltest", Test: "TestAQueuedInvocationExpires",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "the answer reaches the caller exactly once and waits while the next hop is away",
		Package: "fractaltest", Test: "TestAnswerCrossesACutLinkOnce",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "the caller acts on one answer once and drops every later delivery of it",
		Package: "node", Test: "TestAnAnswerIsActedOnOnce",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a node that is not the caller passes the answer to its parent",
		Package: "node", Test: "TestAnAnswerForAnotherNodeIsPassedUp",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a successful call with a return value carries the result",
		Package: "node", Test: "TestASuccessfulInvocationCarriesItsResult",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a failed call carries no result",
		Package: "fractal", Test: "TestRESResultAbsentOnFailure",
	},
	{
		Section: "Method Invocation Result Message",
		Must:    "a failure carries an error with at least a code",
		Package: "fractal", Test: "TestGoldenRES",
	},
	{
		Section: "Connection Loss Message",
		Must:    "a node that detects a loss marks the shadow and tells its parent",
		Package: "node", Test: "TestAChildLinkGoingDownReportsTheLoss",
	},
	{
		Section: "Connection Loss Message",
		Must:    "the sender of the loss message is the node that detected it",
		Package: "fractaltest", Test: "TestExample5ConnectionLoss",
	},
	{
		Section: "Connection Loss Message",
		Must:    "the child of the loss message is the node id of the child that went away",
		Package: "fractal", Test: "TestLOSAffected",
	},
	{
		Section: "Connection Loss Message",
		Must:    "every ok interface of the affected node is marked stale",
		Package: "shadow", Test: "TestApplyLOS",
	},
	{
		Section: "Connection Loss Message",
		Must:    "every ok interface of every node below the affected one is marked stale",
		Package: "fractaltest", Test: "TestASubtreeGoesStaleAtEveryAncestor",
	},
	{
		Section: "Connection Loss Message",
		Must:    "the loss message is passed to the parent unchanged",
		Package: "node", Test: "TestALossMarksTheSubtreeAndIsPassedOn",
	},
	{
		Section: "Full State Report Request Message",
		Must:    "the node reports every interface it owns and passes the request to every child",
		Package: "node", Test: "TestAFullReportRequestAnswersAndIsPassedOn",
	},
	{
		Section: "Full State Report Request Message",
		Must:    "an RBR is answered while the work queue is full, and none is dropped",
		Package: "node", Test: "TestABusyNodeStillAnswersAFullReportRequest",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "ARBR travels up, and AREP and ALOS travel down",
		Package: "fractal", Test: "TestKindTable",
	},
	{
		Section: "Shadow Selection",
		Must:    "an enumeration entry with no instance id selects every instance of the interface",
		Package: "shadow", Test: "TestAnEntryWithNoInstanceSelectsEveryInstance",
	},
	{
		Section: "Synchronization of the Ancestor Shadow - Happy path",
		Must:    "every interface of a node above the node can enumerate is created empty",
		Package: "shadow", Test: "TestRebaseKeepsOnlyTheAncestors",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a node taking another path discards its ancestor shadow in full",
		Package: "shadow", Test: "TestRebaseKeepsOnlyTheAncestors",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a child that learns its admission is revoked discards its ancestor shadow in full",
		Package: "fractaltest", Test: "TestARevokedNodeForgetsItsAncestors",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "an ALOS marks every ok interface of every node above its sender stale",
		Package: "shadow", Test: "TestMarkStaleAboveSparesTheSender",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "the sender of an ALOS is not marked",
		Package: "shadow", Test: "TestMarkStaleAboveSparesTheSender",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "an empty interface stays empty when an ALOS arrives",
		Package: "shadow", Test: "TestMarkStaleAboveLeavesEmptyInterfacesEmpty",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a store anchored at a node path never creates a node outside it again",
		Package: "shadow", Test: "TestConcurrentApplyAndRebase",
		Ours: true,
	},
	{
		Section: "Interface Instance",
		Must:    "a node reports an instance by REP when exposed upward and by AREP when exposed downward",
		Package: "node", Test: "TestAnInstanceIsReportedInTheDirectionsItIsExposed",
	},
	{
		Section: "Interface Instance",
		Must:    "a node reports no instance in a direction its definition does not state",
		Package: "node", Test: "TestAFullReportRequestReportsOnlyUpward",
	},
	{
		Section: "Interface Instance",
		Must:    "a node invokes methods only on a node below it, never on a node above",
		Package: "node", Test: "TestACallIsRefusedBeforeItReachesTheTransport",
	},
	{
		Section: "Shadow",
		Must:    "a node keeps its descendant shadow and its ancestor shadow apart",
		Package: "node", Test: "TestTheAncestorShadowIsKeptApart",
	},
	{
		Section: "Sender Validation",
		Must:    "the sender of a message from the parent is one of the node's ancestors",
		Package: "node", Test: "TestASenderFromAboveMustBeAnAncestor",
	},
	{
		Section: "Synchronization of the Ancestor Shadow - Happy path",
		Must:    "the procedure runs once the node holds a node path and again on a different one",
		Package: "node", Test: "TestANewPathRebuildsTheAncestorShadow",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "a node sends an AREP only for an interface it exposes downward",
		Package: "node", Test: "TestAFullReportRequestReportsOnlyUpward",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "a change of a downward instance goes to every child as an AREP, also from a root",
		Package: "node", Test: "TestAnInstanceIsReportedInTheDirectionsItIsExposed",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "an AREP from the parent updates the ancestor shadow and is passed to every child unchanged",
		Package: "node", Test: "TestAnAncestorReportIsAppliedAndPassedOn",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "an AREP for an interface the node does not select creates nothing and is still passed on",
		Package: "node", Test: "TestAnAncestorReportIsPassedOnUnlessItIsOld",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "an AREP older than the ancestor shadow is discarded and not passed on",
		Package: "node", Test: "TestAnAncestorReportIsPassedOnUnlessItIsOld",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "an ALOS from the parent marks the nodes above its sender and is passed to every child unchanged",
		Package: "node", Test: "TestAnAncestorLossIsAppliedAndPassedOn",
	},
	{
		Section: "Message Routing (Directional Broadcast)",
		Must:    "a downward broadcast from the root reaches a leaf through every node",
		Package: "fractaltest", Test: "TestExample7ExposedStateChange",
	},
	{
		Section: "Synchronization of the Ancestor Shadow - Happy path",
		Must:    "a node with a parent sends an ARBR once it holds a node path, subject to the bound",
		Package: "node", Test: "TestAParentLinkComingUpAsksForTheAncestorState",
	},
	{
		Section: "Synchronization of the Ancestor Shadow - Happy path",
		Must:    "a node sends an ARBR again when it adopts a different node path",
		Package: "node", Test: "TestANewPathResetsTheBound",
	},
	{
		Section: "Synchronization of the Shadows - Reaction to Connection Loss and Reconnection",
		Must:    "a parent link coming up produces an ARBR even when nothing above is selected",
		Package: "node", Test: "TestAParentLinkComingUpAsksForTheAncestorState",
	},
	{
		Section: "Synchronization of the Shadows - Reaction to Connection Loss and Reconnection",
		Must:    "a lost parent is handled as the ALOS section says",
		Package: "node", Test: "TestALostParentMarksTheAncestorsAndTellsTheChildren",
	},
	{
		Section: "Synchronization of the Shadows - Reaction to Connection Loss and Reconnection",
		Must:    "a lost and restored parent link returns the ancestor shadow to ok",
		Package: "fractaltest", Test: "TestExample8LossOfAnAncestor",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "a node that loses its parent marks every ok interface of its ancestor shadow stale",
		Package: "node", Test: "TestALostParentMarksTheAncestorsAndTellsTheChildren",
	},
	{
		Section: "Ancestor Connection Loss Message",
		Must:    "it sends every child an ALOS whose sender is its own node path",
		Package: "node", Test: "TestALostParentMarksTheAncestorsAndTellsTheChildren",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "an ARBR from a child is answered with the full downward state to every child and an ARBR to the parent",
		Package: "node",
		Test:    "TestAnAncestorReportRequestIsAnsweredAfterTheWindow",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "a coalescing node answers after the window closes and forwards one ARBR for the whole window",
		Package: "node", Test: "TestAnAncestorReportRequestIsAnsweredAfterTheWindow",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "the answer covers an ARBR from a link established while the window was open",
		Package: "node", Test: "TestTheWindowCoversALinkThatCameUpInIt",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "a node that bounds its ARBR origination sends one once the bound allows",
		Package: "node", Test: "TestTheBoundDelaysAndNeverSwallowsAnARBR",
	},
	{
		Section: "Ancestor Reported Properties Message",
		Must:    "a full-state AREP carries the time the node determined that state",
		Package: "node", Test: "TestAnAncestorReportRequestIsAnsweredAfterTheWindow",
	},
	{
		Section: "Ancestor Full State Report Request Message",
		Must:    "an owner that does not return delays the answer to a window only until a timeout",
		Package: "node", Test: "TestASlowOwnerDoesNotStopTheAnswer",
		Ours: true,
	},
	{
		Section: "Error Codes",
		Must:    "bits 12 to 9 of an error code are zero",
		Package: "fractal", Test: "TestCodeReservedBits",
	},
	{
		Section: "Error Codes",
		Must:    "the defined codes are used with the values and the ranges of the table",
		Package: "fractal", Test: "TestDefinedCodeValues",
	},
	{
		Section: "Error Codes",
		Must:    "a joining node decides from the code whether to retry",
		Package: "fractal", Test: "TestJoinRetry",
	},
	{
		Section: "Error Codes",
		Must:    "the joiner never retries a rejection, a collision or a revocation",
		Package: "join", Test: "TestTheChildActsOnTheDenialCode",
	},
	{
		Section: "Error Codes",
		Must:    "a code in the reserved range is not used",
		Package: "fractal", Test: "TestCodeReservedRange",
	},
	{
		Section: "Error Codes",
		Must:    "an authorizer may name only a refinement of the code its own check answers",
		Package: "join", Test: "TestAnAuthorizerRefinesTheCodeOfItsOwnCheck",
		Ours: true,
	},
	{
		Section: "Error Codes",
		Must:    "a transport binding may say how a node retries a refinement it defines",
		Package: "join", Test: "TestTheChildActsOnTheDenialCode",
		Ours: true,
	},
	{
		Section: "Message Routing (Unicast)",
		Must:    "a transport says when it holds no link to the node named, and the call is answered not found",
		Package: "node", Test: "TestACallIntoALinkThatIsNotThereIsAnswered",
		Ours: true,
	},
	{
		Section: "Reported Properties Message",
		Must:    "a node never sends a report for one interface behind the last one it sent for it",
		Package: "node", Test: "TestAReportNeverGoesBackInTime",
		Ours: true,
	},
}

// TestConformanceTableNamesRealTests fails on a row that names a test function
// or a suite case that does not exist. It reads the test files of every
// package in packageDirs and the source of the suite. A reader of fractal.md
// uses the table to find the test for a rule, so a test that was renamed
// without its row is a defect.
func TestConformanceTableNamesRealTests(t *testing.T) {
	t.Parallel()
	found := make(map[string]map[string]bool, len(packageDirs))
	for name, dir := range packageDirs {
		found[name] = testFunctions(t, dir)
	}
	suiteCases := suiteCaseNames(t)
	for _, row := range conformance {
		if row.Reason != "" {
			if !suiteCases[row.Suite] {
				t.Errorf("%s: the row %q names the suite case %q, and RunBindingSuite has no case with that name",
					row.Section, row.Must, row.Suite)
			}
			continue
		}
		if !found[row.Package][row.Test] {
			t.Errorf("%s: the row %q names the test %s, and the package %s has no test function with that name",
				row.Section, row.Must, row.Test, row.Package)
		}
	}
}

// TestConformanceTableIsWellFormed checks that every row has a Section and a
// Must, that it names either a test or a reason with a suite case, and that no
// rule is in the table twice.
func TestConformanceTableIsWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, row := range conformance {
		if row.Section == "" || row.Must == "" {
			t.Errorf("the row %+v has an empty Section or an empty Must", row)
		}
		proved := row.Package != "" && row.Test != ""
		deferred := row.Reason != "" && row.Suite != ""
		if proved == deferred {
			t.Errorf("the row %q must fill Package and Test, or Reason and Suite, and never both pairs", row.Must)
		}
		if proved {
			if _, ok := packageDirs[row.Package]; !ok {
				t.Errorf("the row %q names the package %q, which is not in packageDirs", row.Must, row.Package)
			}
			if !strings.HasPrefix(row.Test, "Test") {
				t.Errorf("the row %q names %s, and a test function name starts with Test", row.Must, row.Test)
			}
		}
		if seen[row.Section+row.Must] {
			t.Errorf("the rule %q is in the table twice", row.Must)
		}
		seen[row.Section+row.Must] = true
	}
}

// TestConformanceTableCountsWhatItCovers logs the number of rows, the number
// with a test in this repository, the number that only a transport can prove,
// and the number that are rules of this implementation. It fails when the
// table has fewer rules of fractal.md than floor, which catches a table that
// lost rows.
func TestConformanceTableCountsWhatItCovers(t *testing.T) {
	t.Parallel()
	const floor = 100
	byPackage := map[string]int{}
	deferred, ours := 0, 0
	for _, row := range conformance {
		if row.Ours {
			ours++
		}
		if row.Reason != "" {
			deferred++
			continue
		}
		byPackage[row.Package]++
	}
	if specified := len(conformance) - ours; specified < floor {
		t.Errorf("the table has %d rules of fractal.md, want at least %d: rows are missing", specified, floor)
	}
	t.Logf("rules: %d, with a test here: %d, proved only by a transport: %d, rules of this implementation: %d",
		len(conformance), len(conformance)-deferred, deferred, ours)
	for name := range packageDirs {
		t.Logf("  %s: %d", name, byPackage[name])
	}
}

// testFunctions parses the _test.go files in dir and returns the names of the
// top-level functions that start with Test.
func testFunctions(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	set := token.NewFileSet()
	names := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(set, filepath.Join(dir, entry.Name()), nil, 0)
		if parseErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), parseErr)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("found no test function in %s, so no row that names this package can be checked", dir)
	}
	return names
}

// suiteCaseNames returns every string literal in suite.go. The names of the
// RunBindingSuite cases are among them, so a row cannot name a case that was
// removed or renamed.
func suiteCaseNames(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("suite.go")
	if err != nil {
		t.Fatalf("read the suite: %v", err)
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "suite.go", source, 0)
	if err != nil {
		t.Fatalf("read the suite: %v", err)
	}
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING {
			names[strings.Trim(lit.Value, `"`)] = true
		}
		return true
	})
	return names
}
