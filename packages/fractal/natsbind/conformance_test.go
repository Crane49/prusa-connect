package natsbind_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packageDirs maps a package name in the table to the directory with its
// tests, relative to this directory.
var packageDirs = map[string]string{
	"natsbind": ".",
	"joinwire": "./joinwire",
	"bridge":   "../bridge",
}

// suiteSource is the path of the conformance suite of the sdk, which is in the
// same repository as this module. A row with a Reason names a case of that
// suite. When the file exists, TestConformanceTableNamesRealTests checks that
// the suite has that case.
const suiteSource = "../../../sdk/fractal/fractaltest/suite.go"

// conformanceRow links one MUST sentence of fractal-nats.md to the test that
// checks it. A row for a rule that no test in this module checks has a Reason
// and the name of the conformance suite case that checks it.
type conformanceRow struct {
	// Section is the heading in fractal-nats.md. A rule of another document of
	// the specification names that document in brackets.
	Section string
	// Must is the MUST sentence, shortened to its main clause.
	Must string
	// Package is the key in packageDirs of the package with the test.
	Package string
	// Test is the name of the test function.
	Test string
	// Reason says why no test in this module checks the rule.
	Reason string
	// Suite is the name of the case in RunBindingSuite that checks the rule.
	Suite string
	// Ours marks a rule of this binding, for a choice fractal-nats.md leaves
	// open. It is not a rule of the specification, and
	// TestConformanceTableCountsWhatItCovers counts it separately.
	Ours bool
}

// conformance links every MUST of fractal-nats.md to its test. It leaves out
// the informational sections, which have no rule about behaviour.
var conformance = []conformanceRow{
	{
		Section: "Node Deployment Model",
		Must:    "every node runs its own embedded single-node server with JetStream",
		Package: "natsbind", Test: "TestAJoinedNodeTakesThePathItWasGranted",
	},
	{
		Section: "Node Deployment Model",
		Must:    "the relation between two nodes is a leaf node connection",
		Package: "natsbind", Test: "TestAJoinedNodeTakesThePathItWasGranted",
	},
	{
		Section: "Node Deployment Model",
		Must:    "every message is forwarded hop by hop and never by subject propagation",
		Package: "natsbind", Test: "TestTheTreeCarriesTheExamplesOfTheSpecification",
	},
	{
		Section: "Node Deployment Model",
		Must:    "subject interest does not pass beyond one hop",
		Package: "natsbind", Test: "TestSiblingsSeeNothingOfEachOther",
	},
	{
		Section: "Node Deployment Model",
		Must:    "a send to a node this one holds no link to says so",
		Reason:  "the reason is the engine's, and every transport answers it the same way",
		Suite:   "a send to a node the transport does not hold says so",
		Ours:    true,
	},
	{
		Section: "JetStream Domain Naming",
		Must:    "the domain replaces every slash and doubles every underscore a node id carries",
		Package: "natsbind", Test: "TestTheDomainOfANodePath",
	},
	{
		Section: "JetStream Domain Naming",
		Must:    "the derivation is injective",
		Package: "natsbind", Test: "TestTheDerivationIsInjective",
	},
	{
		Section: "Security",
		Must:    "a listener uses tls with server authentication and asks for no client certificate",
		Package: "natsbind", Test: "TestTheListenersAreTls13AndAskForNoCertificate",
	},
	{
		Section: "Security",
		Must:    "tls is supported on every connection even where a deployment turns it off",
		Package: "natsbind", Test: "TestALinkRunsOverTls",
	},
	{
		Section: "Security",
		Must:    "the node that joins checks the one above against the anchors it was given",
		Package: "natsbind", Test: "TestALinkRunsOverTls",
	},
	{
		Section: "Security",
		Must:    "the identity is an nkey whose seed does not leave the node",
		Package: "natsbind", Test: "TestTheIdentityIsTheSameAfterARestart",
	},
	{
		Section: "Security",
		Must:    "a node that accepts others keeps its identity across a restart",
		Package: "natsbind", Test: "TestTheIdentityIsTheSameAfterARestart",
	},
	{
		Section: "Security",
		Must:    "the join exchange runs over a shared bootstrap account",
		Package: "natsbind", Test: "TestAJoinedNodeTakesThePathItWasGranted",
	},
	{
		Section: "Security",
		Must:    "each link is placed in an account of its own",
		Package: "natsbind", Test: "TestSiblingsSeeNothingOfEachOther",
	},
	{
		Section: "Security",
		Must:    "the state and the results of one node are invisible to its siblings",
		Package: "natsbind", Test: "TestSiblingsSeeNothingOfEachOther",
	},
	{
		Section: "Security",
		Must:    "a node below may publish only upward and subscribe only downward",
		Package: "natsbind", Test: "TestTheLinkPermissionsCarryTheDirection",
	},
	{
		Section: "Security",
		Must:    "the node above is held to the mirror image of those permissions",
		Package: "natsbind", Test: "TestTheLinkPermissionsCarryTheDirection",
	},
	{
		Section: "Security",
		Must:    "the jetstream grants name that link's own stream and no other",
		Package: "natsbind", Test: "TestTheJetStreamGrantsNameOneStream",
	},
	{
		Section: "Security",
		Must:    "revoking an admission takes effect on the connection already established",
		Package: "natsbind", Test: "TestRevocationCutsTheConnectionItAlreadyHolds",
	},
	{
		Section: "Security",
		Must:    "a revocation is applied again at every start, so an unexpired credential stays dead",
		Package: "natsbind", Test: "TestARevokedAdmissionComesBackRevoked",
	},
	{
		Section: "Security",
		Must:    "a revocation between the checks and the link denies the join and builds no link",
		Package: "natsbind", Test: "TestARevocationBeforeTheLinkStands",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "a node that accepts others exposes fractal.join with the two roles",
		Package: "natsbind", Test: "TestTheBootstrapRolesAreTheOnesInTheSpecification",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "a node that accepts none exposes neither the endpoint nor the account",
		Package: "natsbind", Test: "TestANodeThatTakesNothingExposesNoJoinEndpoint",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "the open mapping is restricted to standard client connections",
		Package: "natsbind", Test: "TestTheSentinelIsRestrictedToClientConnections",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "a peer presenting no credentials brings up no leaf connection",
		Package: "natsbind", Test: "TestAnUnauthenticatedLeafConnectionIsRefused",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "a request beyond the bound is denied and not dropped",
		Package: "natsbind", Test: "TestAJoinBeyondTheBoundIsDenied",
	},
	{
		Section: "Proof of Possession",
		Must:    "the request is a compact jws carrying typ fractal-join+jws, alg EdDSA and the identity nkey",
		Package: "joinwire", Test: "TestASignedJoinRequestNamesItsPurpose",
	},
	{
		Section: "Proof of Possession",
		Must:    "a request naming another typ is refused before anything else in its header",
		Package: "joinwire", Test: "TestAJoinRequestIsRefused",
	},
	{
		Section: "Proof of Possession",
		Must:    "a request naming another algorithm is refused",
		Package: "joinwire", Test: "TestAJoinRequestIsRefused",
	},
	{
		Section: "Proof of Possession",
		Must:    "no part of the header or the body is acted on until the signature verifies",
		Package: "joinwire", Test: "TestNothingOfAnUnprovenRequestIsRead",
	},
	{
		Section: "Join Messages",
		Must:    "a receiver ignores a field it does not recognize",
		Package: "joinwire", Test: "TestAFieldNobodyKnowsIsIgnored",
	},
	{
		Section: "Join Messages",
		Must:    "a denial names one of the join denial codes",
		Package: "joinwire", Test: "TestTheRefinementIsAJoinDenialCode",
	},
	{
		Section: "Join Messages",
		Must:    "a node denied under the waiting refinement asks again with backoff",
		Package: "natsbind", Test: "TestAnUnknownIdentityWaitsForAnOperator",
		Ours: true,
	},
	{
		Section: "Join Messages",
		Must:    "a node persists the granted heartbeat pacing, which no later message repeats",
		Package: "joinwire", Test: "TestAGrantSurvivesTheWire",
	},
	{
		Section: "Join Messages",
		Must:    "the grant carries the extension join data keyed by an identifier naming the extension",
		Package: "joinwire", Test: "TestAGrantSurvivesTheWire",
	},
	{
		Section: "JWT Mode",
		Must:    "a claim the parent does not recognize is ignored",
		Package: "joinwire", Test: "TestAJoinCredentialIsAccepted",
	},
	{
		Section: "JWT Mode",
		Must:    "the algorithm is RS256 over a modulus of at least 2048 bits",
		Package: "joinwire", Test: "TestAJoinCredentialIsRefused",
	},
	{
		Section: "JWT Mode",
		Must:    "another algorithm is refused before any key is resolved",
		Package: "joinwire", Test: "TestAnotherAlgorithmIsRefusedBeforeAnyKeyIsResolved",
	},
	{
		Section: "JWT Mode",
		Must:    "the node id, the parent path and the bound key equal what the request presents",
		Package: "joinwire", Test: "TestAJoinCredentialIsRefused",
	},
	{
		Section: "JWT Mode",
		Must:    "the expiry is enforced with no tolerance and the not-before with one",
		Package: "joinwire", Test: "TestAJoinCredentialIsRefused",
	},
	{
		Section: "JWT Mode",
		Must:    "every numeric date form is accepted and the value is range-checked first",
		Package: "joinwire", Test: "TestEveryNumericDateFormIsRead",
	},
	{
		Section: "JWT Mode",
		Must:    "the key source keeps serving a stale cache",
		Package: "joinwire", Test: "TestAStaleCacheKeepsServing",
	},
	{
		Section: "JWT Mode",
		Must:    "the key source looks again on an unknown kid and rate-limits that look",
		Package: "joinwire", Test: "TestAnUnknownKidLooksAgainOnce",
	},
	{
		Section: "JWT Mode",
		Must:    "a key the verifier cannot use is skipped",
		Package: "joinwire", Test: "TestAKeyThatCannotBeUsedIsSkipped",
	},
	{
		Section: "JWT Mode",
		Must:    "a document carrying private key material is refused whole",
		Package: "joinwire", Test: "TestADocumentWithPrivateMaterialIsRefusedWhole",
	},
	{
		Section: "JWT Mode",
		Must:    "a key source that cannot answer gives unavailable and never unauthorized",
		Package: "joinwire", Test: "TestAKeySourceThatCannotAnswerIsNotUnauthorized",
	},
	{
		Section: "Provisioning the Link",
		Must:    "on a grant the account of the link exists and the credentials are issued for it",
		Package: "natsbind", Test: "TestTheConnectionCredentialsAreLeafOnlyAndExpire",
	},
	{
		Section: "Provisioning the Link",
		Must:    "the credentials are bound to the presented key and expire at a finite instant",
		Package: "natsbind", Test: "TestTheConnectionCredentialsAreLeafOnlyAndExpire",
	},
	{
		Section: "Provisioning the Link",
		Must:    "on a grant the stream that holds the method invocations exists",
		Package: "natsbind", Test: "TestAMethodInvocationAndItsResultCrossTheLink",
	},
	{
		Section: "Provisioning the Link",
		Must:    "the node that joined brings up its leaf connection with the issued credentials",
		Package: "natsbind", Test: "TestAJoinedNodeTakesThePathItWasGranted",
	},
	{
		Section: "Provisioning the Link",
		Must:    "the result stream and the consumer bound across the link exist",
		Package: "natsbind", Test: "TestAMethodInvocationAndItsResultCrossTheLink",
	},
	{
		Section: "Provisioning the Link",
		Must:    "an existing stream or consumer is never created again",
		Package: "natsbind", Test: "TestANodeInTheMiddleComesBackWithWhatItHeld",
	},
	{
		Section: "Provisioning the Link",
		Must:    "building a link that is already built changes nothing and answers the same way",
		Package: "natsbind", Test: "TestAJoinRequestFromTheSameIdentityIsGrantedAgain",
	},
	{
		Section: "Provisioning the Link",
		Must:    "a node renews its credentials before they expire, even while connected",
		Package: "natsbind", Test: "TestTheCredentialsAreRenewedBeforeTheyExpire",
	},
	{
		Section: "Provisioning the Link",
		Must:    "renewed credentials are adopted by making the leaf connection again",
		Package: "natsbind", Test: "TestTheCredentialsAreRenewedBeforeTheyExpire",
	},
	{
		Section: "Provisioning the Link",
		Must:    "a node that takes a new node path builds its domain-scoped resources again",
		Package: "natsbind", Test: "TestTheStoreOfANodeCarriesItsDomain",
	},
	{
		Section: "Provisioning the Link",
		Must:    "the account material and whether a link ever connected are persisted",
		Package: "natsbind", Test: "TestANodeInTheMiddleComesBackWithWhatItHeld",
	},
	{
		Section: "Subject Namespace",
		Must:    "the subjects are the ones the table names",
		Package: "natsbind", Test: "TestTheSubjectsAreTheOnesInTheTable",
	},
	{
		Section: "Subject Namespace",
		Must:    "every downward subject carries the node id of the link and no upward one does",
		Package: "natsbind", Test: "TestAMessageTakesTheSubjectOfItsType",
	},
	{
		Section: "Subject Namespace",
		Must:    "AREP and ALOS go only down and ARBR goes only up",
		Package: "natsbind", Test: "TestAnAncestorMessageGoesOneWayOnly",
	},
	{
		Section: "Message Encoding",
		Must:    "every message is a utf-8 json object with the fields of its type",
		Package: "natsbind", Test: "TestTheTreeCarriesTheExamplesOfTheSpecification",
	},
	{
		Section: "Message Encoding",
		Must:    "the version is checked and an unsupported one is dropped and acknowledged",
		Reason:  "the engine in sdk/fractal/node reads the version and acknowledges what it drops",
		Suite:   "durable is delivered once when acknowledged",
	},
	{
		Section: "Message Encoding",
		Must:    "a persisted message carries its fractal id as the Nats-Msg-Id header",
		Reason:  "the suite proves what the header is for: one id is delivered once",
		Suite:   "durable of a repeated id is delivered once",
	},
	{
		Section: "REP",
		Must:    "a state report travels per hop over core nats with no persistence",
		Package: "natsbind", Test: "TestAStateReportReachesTheNodeAbove",
	},
	{
		Section: "DES",
		Must:    "a requested property change is dropped rather than queued while the link is down",
		Reason:  "the suite states the rule as behaviour a transport must show",
		Suite:   "best effort is dropped while the link is down",
	},
	{
		Section: "RPC",
		Must:    "the stream is a work queue on file storage with the deduplication window",
		Package: "natsbind", Test: "TestACutLinkKeepsMethodInvocationsInOrder",
	},
	{
		Section: "RPC",
		Must:    "the consumer is durable, acknowledges one at a time and takes one message at a time",
		Package: "natsbind", Test: "TestTheConsumersTakeOneMessageAtATime",
	},
	{
		Section: "RPC",
		Must:    "a message waits in the stream while the node below is away and arrives in order",
		Package: "natsbind", Test: "TestACutLinkKeepsMethodInvocationsInOrder",
	},
	{
		Section: "RES",
		Must:    "the result stream is owned by the node below and the consumer by the node above",
		Package: "natsbind", Test: "TestAMethodInvocationAndItsResultCrossTheLink",
	},
	{
		Section: "Crash-Safety and Redelivery",
		Must:    "a forwarded message is persisted before the one it came from is acknowledged",
		Reason:  "a durable send returns only after the publish acknowledgement, which the suite drives",
		Suite:   "durable is delivered again when not acknowledged",
	},
	{
		Section: "LOS",
		Must:    "a connection loss message travels per hop over core nats",
		Package: "natsbind", Test: "TestTheTreeCarriesTheExamplesOfTheSpecification",
	},
	{
		Section: "RBR",
		Must:    "a request for a full state report is published once per link on that link's subject",
		Package: "natsbind", Test: "TestTheTreeCarriesTheExamplesOfTheSpecification",
	},
	{
		Section: "RBR",
		Must:    "a node originates one toward a node below when that link comes up",
		Reason:  "the engine sends it when the transport reports the link up",
		Suite:   "a connect signal reaches the node above",
	},
	{
		Section: "AREP",
		Must:    "an ancestor state report travels per hop over core nats, once per child link, unchanged",
		Package: "natsbind", Test: "TestTheTreeCarriesTheAncestorMessages",
	},
	{
		Section: "ALOS",
		Must:    "an ancestor loss message travels per hop over core nats, once per child link, unchanged",
		Package: "natsbind", Test: "TestTheTreeCarriesTheAncestorMessages",
	},
	{
		Section: "ARBR",
		Must:    "an ancestor report request travels per hop over core nats on fractal.up.arbr, unchanged",
		Package: "natsbind", Test: "TestTheTreeCarriesTheAncestorMessages",
	},
	{
		Section: "ARBR",
		Must:    "the child that sent an ancestor report request is the link it arrived on",
		Package: "natsbind", Test: "TestTheTreeCarriesTheAncestorMessages",
	},
	{
		Section: "Connectivity Detection",
		Must:    "a node publishes an empty heartbeat upward at least as often as it was granted",
		Package: "natsbind", Test: "TestTheHeartbeatKeepsALinkAlive",
	},
	{
		Section: "Connectivity Detection",
		Must:    "the absence of a child's heartbeat for the liveness timeout is its disconnect",
		Package: "natsbind", Test: "TestOnlyAHeartbeatKeepsANodeAlive",
	},
	{
		Section: "Connectivity Detection",
		Must:    "the loss of a leaf connection is a disconnect however briefly it lasts",
		Package: "natsbind", Test: "TestBothEndsSeeEveryShortDrop",
	},
	{
		Section: "Connectivity Detection",
		Must:    "a node admitted but never connected produces no loss message",
		Reason:  "the suite states the rule as behaviour a transport must show",
		Suite:   "a child that never connected produces no loss",
	},
	{
		Section: "Connectivity Detection",
		Must:    "the liveness timeout is a multiple of the granted interval",
		Package: "natsbind", Test: "TestAConfigurationIsRefused",
	},
	{
		Section: "Connectivity Detection",
		Must:    "the leaf connection lifecycle of the embedded server is observed",
		Package: "natsbind", Test: "TestTheTreeCarriesTheExamplesOfTheSpecification",
	},
	{
		Section: "Connectivity Detection",
		Must:    "a disconnect marks the subtree stale and sends a loss message upward",
		Reason:  "the engine does both when the transport reports the link down",
		Suite:   "a loss signal reaches the node above",
	},
	{
		Section: "Connectivity Detection",
		Must:    "a node detects the loss and the return of its parent from the leaf connection it opened",
		Package: "natsbind", Test: "TestTheParentLinkIsReportedWhenItMoves",
	},
	{
		Section: "Connectivity Detection",
		Must:    "a parent link that never opened is never reported lost",
		Package: "natsbind", Test: "TestTheParentLinkIsReportedWhenItMoves",
		Ours: true,
	},
	{
		Section: "Connectivity Detection",
		Must:    "on losing its parent a node marks its ancestor shadow stale and sends ALOS down",
		Package: "natsbind", Test: "TestAnAncestorShadowFollowsTheParentLink",
	},
	{
		Section: "Connectivity Detection",
		Must:    "on a reconnect to its parent a node sends ARBR",
		Package: "natsbind", Test: "TestAnAncestorShadowFollowsTheParentLink",
	},
	{
		Section: "Subtree Autonomy",
		Must:    "the ancestor shadow below a break goes stale and the ARBR on reconnection restores it",
		Package: "natsbind", Test: "TestAnAncestorShadowFollowsTheParentLink",
	},
	{
		Section: "Provisioning the Link",
		Must:    "a renewal reopens the parent link and the ancestor shadow is repaired after it",
		Package: "natsbind", Test: "TestARenewalRepairsTheAncestorShadow",
		Ours: true,
	},
	{
		Section: "Deduplication",
		Must:    "one parameter sets the duplicates window of the streams and the life of both buckets",
		Package: "natsbind", Test: "TestAConfigurationIsRefused",
	},
	{
		Section: "Deduplication",
		Must:    "the buckets are on file storage and outlive a restart",
		Package: "natsbind", Test: "TestTheDeduplicationBucketsHoldWhatWasDone",
	},
	{
		Section: "Deduplication",
		Must:    "the executed entry holds the result that was produced and not only the fact",
		Package: "natsbind", Test: "TestTheDeduplicationBucketsHoldWhatWasDone",
	},
	{
		Section: "Versioning",
		Must:    "the version is in the payload and not in the subject",
		Package: "natsbind", Test: "TestTheSubjectsAreTheOnesInTheTable",
	},
	{
		Section: "Configuration Parameters",
		Must:    "every parameter of the table is exposed and checked",
		Package: "natsbind", Test: "TestAConfigurationIsRefused",
	},
	{
		Section: "Configuration Parameters",
		Must:    "the deduplication window is required",
		Package: "natsbind", Test: "TestAConfigurationIsRefused",
	},
	{
		Section: "Configuration Parameters",
		Must:    "one message at a time is fixed and not configurable",
		Package: "natsbind", Test: "TestTheConsumersTakeOneMessageAtATime",
	},
	{
		Section: "Error Codes (fractal.md)",
		Must:    "a revocation is not retried by a start of the node",
		Package: "natsbind", Test: "TestARevokedNodeDoesNotAskAgainWhenItStarts",
	},
	{
		Section: "Error Codes (fractal.md)",
		Must:    "a revocation is not retried by a renewal of the credentials",
		Package: "natsbind", Test: "TestARevocationLearnedElsewhereStopsTheRenewals",
	},
	{
		Section: "Error Codes (fractal.md)",
		Must:    "a denial that allows no retry is not retried by a Join, a renewal or a restart",
		Package: "natsbind", Test: "TestARefusedNodeNeverAsksAgain",
	},
	{
		Section: "Error Codes (fractal.md)",
		Must:    "ERR_JOIN_UNAUTHORIZED is retried once the node holds a new join credential",
		Package: "natsbind", Test: "TestAPermanentRefusalIsCleared",
	},
	{
		Section: "Conventions (domain-common.md)",
		Must:    "a method that takes a URL answers ERR_INVALID_PARAMS for a scheme the node does not support",
		Package: "bridge", Test: "TestFileURLs",
	},
}

// TestConformanceTableNamesRealTests parses the test files of every package in
// packageDirs and fails for a row whose test function does not exist. A reader
// of fractal-nats.md uses the table to find the test of a rule, so the table
// has to change when a test is renamed.
func TestConformanceTableNamesRealTests(t *testing.T) {
	t.Parallel()
	found := make(map[string]map[string]bool, len(packageDirs))
	for name, dir := range packageDirs {
		found[name] = testFunctions(t, dir)
	}
	cases := suiteCaseNames(t)
	for _, row := range conformance {
		if row.Reason != "" {
			if cases != nil && !cases[row.Suite] {
				t.Errorf("%s: %q names the suite case %q, which the suite does not run",
					row.Section, row.Must, row.Suite)
			}
			continue
		}
		if !found[row.Package][row.Test] {
			t.Errorf("%s: %q names %s in %s, which is not a test there",
				row.Section, row.Must, row.Test, row.Package)
		}
	}
}

// TestConformanceTableIsWellFormed checks that every row has either a Package
// and a Test, or a Reason and a Suite, and that no rule is in the table twice.
func TestConformanceTableIsWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, row := range conformance {
		if row.Section == "" || row.Must == "" {
			t.Errorf("an empty cell in row %+v", row)
		}
		proved := row.Package != "" && row.Test != ""
		deferred := row.Reason != "" && row.Suite != ""
		if proved == deferred {
			t.Errorf("%q names neither a test nor a reason, or both", row.Must)
		}
		if proved {
			if _, ok := packageDirs[row.Package]; !ok {
				t.Errorf("%q names the package %q, which this table does not know", row.Must, row.Package)
			}
			if !strings.HasPrefix(row.Test, "Test") {
				t.Errorf("%q names %s, which is not a test name", row.Must, row.Test)
			}
		}
		if seen[row.Section+row.Must] {
			t.Errorf("the rule %q is in the table twice", row.Must)
		}
		seen[row.Section+row.Must] = true
	}
}

// TestConformanceTableCountsWhatItCovers logs how many rules the table has,
// how many a test in this module checks, how many the conformance suite
// checks, and how many are rules of this binding and not of fractal-nats.md.
// It fails when the table has fewer rules of fractal-nats.md than floor.
func TestConformanceTableCountsWhatItCovers(t *testing.T) {
	t.Parallel()
	const floor = 60
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
		t.Errorf("the table has %d rules of fractal-nats.md, want at least %d", specified, floor)
	}
	t.Logf("rules: %d, proved here: %d, covered by the suite: %d, ours and not the specification's: %d",
		len(conformance), len(conformance)-deferred, deferred, ours)
	for name := range packageDirs {
		t.Logf("  %s: %d", name, byPackage[name])
	}
}

// testFunctions parses the test files in dir and returns the names of the
// test functions.
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
		t.Fatalf("found no test function in %s, so the table cannot be checked", dir)
	}
	return names
}

// suiteCaseNames returns every string literal in the source file of the
// conformance suite, which includes the names of its cases. It returns nil
// when the file does not exist, as in a checkout of this module alone.
func suiteCaseNames(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile(suiteSource)
	if err != nil {
		t.Logf("the file %s does not exist, so the suite case names are not checked", suiteSource)
		return nil
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, suiteSource, source, 0)
	if err != nil {
		t.Fatalf("read the suite: %v", err)
	}
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			names[strings.Trim(lit.Value, `"`)] = true
		}
		return true
	})
	return names
}
