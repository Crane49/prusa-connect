package join

import (
	"testing"

	conf "github.com/prusa3d/prusa-connect/sdk/fractal/internal/conformance"
)

// conformanceRow pairs one MUST sentence of the specification with the name of
// the test that checks it. The conformance package has the functions that
// check the table, and every package under sdk/fractal uses them.
type conformanceRow = conf.Row

// conformance lists every MUST of the specification sections this package
// implements, each with its test. Must is the sentence of the specification,
// shortened.
var conformance = []conformanceRow{
	{
		Section: "Joining the Network",
		Must:    "joining requires no node other than the joining node to be reconfigured or restarted",
		Test:    "TestJoiningReconfiguresNoOtherNode",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "every node has an authentication identity whose possession it can prove",
		Test:    "TestTheParentDeniesARequestWhoseProofDoesNotVerify",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "the connection credentials are bound to the child's authentication identity",
		Test:    "TestTheGrantCarriesEverythingTheChildNeeds",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "a node with no valid credentials obtains new ones by re-running the join exchange",
		Test:    "TestTheChildRenewsBeforeTheCredentialsExpire",
	},
	{
		Section: "Authentication Identity and Connection Credentials",
		Must:    "the parent answers such a request while the child is connected and treats it as no loss",
		Test:    "TestTheParentAnswersAConnectedChild",
	},
	{
		Section: "Admission and Revocation",
		Must:    "the parent persists the node id, the identity and the revocation of every child",
		Test:    "TestTheFileStoreOutlivesTheParent",
	},
	{
		Section: "Admission and Revocation",
		Must:    "the parent restores that record before it starts answering join requests",
		Test:    "TestTheParentRestoresItsAdmissionsBeforeItAnswers",
	},
	{
		Section: "Admission and Revocation",
		Must:    "the parent denies a revoked child whatever identity it presents and issues it nothing",
		Test:    "TestTheParentDeniesARevokedChild",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a revocation that arrives while a rejoin waits for its credentials denies the rejoin",
		Test:    "TestARevocationDuringARejoinStands",
	},
	{
		Section: "Admission and Revocation",
		Must:    "a revocation saved while the child's first connection is saved stays in the record",
		Test:    "TestARevocationDuringTheFirstConnectionStands",
	},
	{
		Section: "Admission and Revocation",
		Must:    "revoking a child runs the binding's hook that ends its existing access, until the hook succeeds",
		Test:    "TestARevocationHookThatFailsIsReported",
	},
	{
		Section: "The Join Exchange",
		Must:    "the parent evaluates the request in the order given, answering the first failure",
		Test:    "TestTheParentAnswersTheFirstCheckThatFails",
	},
	{
		Section: "The Join Exchange",
		Must:    "the order of the checks is the one the specification lists",
		Test:    "TestTheChecksRunInTheOrderTheSpecificationSets",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 1: a parent that holds no node path of its own denies every request",
		Test:    "TestTheParentWithoutItsOwnNodePathDeniesEveryRequest",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 2: a join request of an unsupported version is denied rather than dropped",
		Test:    "TestTheParentDeniesAnUnsupportedProtocolVersion",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 3: the request parses, carries every field, and its proof of possession verifies",
		Test:    "TestTheParentDeniesARequestWhoseProofDoesNotVerify",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 4: the requested node id conforms to the node id definition",
		Test:    "TestTheParentDeniesANodeIDOutsideItsGrammar",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 5: the assigned node path stays within the node path length bound",
		Test:    "TestTheParentDeniesANodePathOverTheBound",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 6: the parent verifies the join credential and skips the check for the holder",
		Test:    "TestTheParentVerifiesTheJoinCredential",
	},
	{
		Section: "The Join Exchange",
		Must:    "check 7: the child is not revoked and the node id is not held by another child",
		Test:    "TestTheParentDeniesACollisionInBothDirections",
	},
	{
		Section: "The Join Exchange",
		Must:    "the parent creates no resource for the child until every check has passed",
		Test:    "TestTheParentCreatesNothingForADeniedChild",
	},
	{
		Section: "The Join Exchange",
		Must:    "on success the parent records the admission, issues credentials and answers a grant",
		Test:    "TestTheGrantCarriesEverythingTheChildNeeds",
	},
	{
		Section: "The Join Exchange",
		Must:    "the grant carries the join data of the extensions the parent offers",
		Test:    "TestTheGrantCarriesTheOfferedExtensions",
	},
	{
		Section: "The Join Exchange",
		Must:    "a child ignores the join data of an extension it does not run",
		Test:    "TestDecideKeepsOnlyTheExtensionsTheNodeRuns",
	},
	{
		Section: "The Join Exchange",
		Must:    "a node whose request goes unanswered retries it with backoff",
		Test:    "TestTheChildRetriesAnUnansweredRequestWithBackoff",
	},
	{
		Section: "Uniqueness",
		Must:    "a request contradicting an existing child is denied as a collision, in both directions",
		Test:    "TestTheParentDeniesACollisionInBothDirections",
	},
	{
		Section: "Uniqueness",
		Must:    "the identity that holds the node id gets a fresh grant and loses no state",
		Test:    "TestTheHoldingIdentityGetsAFreshGrant",
	},
	{
		Section: "Uniqueness",
		Must:    "of concurrent requests for one node id at most one is granted",
		Test:    "TestConcurrentRequestsForOneNodeIDGrantAtMostOne",
	},
	{
		Section: "Uniqueness",
		Must:    "of concurrent requests from one identity for two node ids at most one is granted",
		Test:    "TestConcurrentRequestsFromOneIdentityGrantOneNodeID",
	},
	{
		Section: "Uniqueness",
		Must:    "a reservation shared by two requests of one node holds until both end",
		Test:    "TestAFailedSharerKeepsTheReservation",
	},
	{
		Section: "Uniqueness",
		Must:    "an identity whose credentials could not be issued holds no second node id",
		Test:    "TestAnIdentityKeepsItsNodeIDWhenIssueFails",
	},
	{
		Section: "Uniqueness",
		Must:    "a node id is not released because its child has disconnected",
		Test:    "TestTheParentKeepsANodeIDWhileTheChildIsAway",
	},
	{
		Section: "After the Join Exchange",
		Must:    "a grant of an unsupported version brings up no connection and is never retried",
		Test:    "TestTheChildRefusesAGrantOfAnUnsupportedVersion",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child persists the path, the parent identity, the credentials and the connection",
		Test:    "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child keeps what the transport binding asked it to persist",
		Test:    "TestDecideKeepsWhatTheBindingPersisted",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child persists the join data of the extensions it runs",
		Test:    "TestTheJoinerKeepsTheJoinDataOfTheExtensionsItRuns",
	},
	{
		Section: "After the Join Exchange",
		Must:    "the child compares the persisted parent identity against the grant before connecting",
		Test:    "TestDecideCoversEveryBranchOfAfterTheJoinExchange",
	},
	{
		Section: "Error Codes",
		Must:    "the joining node decides from the denial code whether to retry",
		Test:    "TestTheChildActsOnTheDenialCode",
	},
	{
		Section: "Bootstrap Account and Join Endpoint",
		Must:    "a request beyond the bound is denied as unavailable rather than dropped",
		Test:    "TestTheParentBoundsJoins",
	},
	{
		Section: "Key source unavailable",
		Must:    "a check that could not run answers unavailable and never unauthorized",
		Test:    "TestTheParentVerifiesTheJoinCredential",
	},
	{
		Section: "Provisioning the Link",
		Must:    "the parent persists whether the link has ever completed a connection",
		Test:    "TestTheFileStoreOutlivesTheParent",
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
