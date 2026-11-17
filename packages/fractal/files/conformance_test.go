package files_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// conformanceRow pairs one rule of fractal-files.md or fractal-files-http.md
// that one server keeps with the test of this package that proves it.
type conformanceRow struct {
	// Section is the heading of the rule in the extension or its HTTP binding.
	Section string
	// Must is the rule, shortened to its main clause.
	Must string
	// Test is the name of a test function of this package.
	Test string
}

// conformance pairs the rules one server keeps with their tests. The rules
// both ends agree on are in sdk/fractal/files, and the files entry of the
// grant and the ancestor shadow are tested in package main.
var conformance = []conformanceRow{
	{
		Section: "Names",
		Must:    "a published name is in Normalization Form C and unique in its folder",
		Test:    "TestNamesInNFC",
	},
	{
		Section: "The Listing",
		Must:    "a published tree gives the listings of the extension's example, byte for byte",
		Test:    "TestPublishesSpecTree",
	},
	{
		Section: "Interface: ContentStore1",
		Must:    "rootHash is null until the first whole scan of the store",
		Test:    "TestStoreReportsRootHash",
	},
	{
		Section: "Interface: ContentStore1",
		Must:    "an instance is never withdrawn, and a store that is gone has a null rootHash",
		Test:    "TestStoreStaysAfterRemoval",
	},
	{
		Section: "Interface: ContentStore1",
		Must:    "List answers a listing the store publishes and ERR_OBJECT_NOT_FOUND for any other identity",
		Test:    "TestListAnswers",
	},
	{
		Section: "The Ed25519 Proof",
		Must:    "a proof carries typ fractal-proof+jws, alg EdDSA and the caller's nkey",
		Test:    "TestSignProof",
	},
	{
		Section: "The Ed25519 Proof",
		Must:    "a node refuses a proof whose typ or alg is not as specified",
		Test:    "TestVerifyProof",
	},
	{
		Section: "The Ed25519 Proof",
		Must:    "a node acts on no part of the payload until the signature verifies",
		Test:    "TestVerifyProof",
	},
	{
		Section: "The Ed25519 Proof",
		Must:    "a node refuses a payload that is not an object or has no integer iat",
		Test:    "TestVerifyProof",
	},
	{
		Section: "The Ed25519 Proof",
		Must:    "a node refuses an iat further from its clock than its tolerance",
		Test:    "TestVerifyProof",
	},
	{
		Section: "Addressing",
		Must:    "the identity is taken from the target as received, with no decoding and no dot-segment resolution",
		Test:    "TestServer",
	},
	{Section: "Addressing", Must: "a node ignores a query", Test: "TestServer"},
	{
		Section: "Proving Possession",
		Must:    "a node checks the proof and the child before any other part of the request",
		Test:    "TestServer",
	},
	{
		Section: "Serving (Ranges)",
		Must:    "a node answers bytes=a-b and bytes=a- with 206 and the whole length",
		Test:    "TestServer",
	},
	{
		Section: "Serving (Ranges)",
		Must:    "a node answers any other Range form with 400 before any lookup",
		Test:    "TestServer",
	},
	{
		Section: "Serving (Ranges)",
		Must:    "an empty object answers any Range with 200, a start at or past the end gets 416",
		Test:    "TestServer",
	},
	{Section: "Answering", Must: "a HEAD is answered as the GET without the body", Test: "TestServer"},
	{
		Section: "Failures",
		Must:    "every failure carries no-store and a 401 carries WWW-Authenticate: Fractal-Proof",
		Test:    "TestServer",
	},
	{
		Section: "Failures",
		Must:    "a published object that cannot be served now gets 503 with a Retry-After date",
		Test:    "TestServerCannotReadAPublishedFile",
	},
	{
		Section: "Failures",
		Must:    "a response that cannot be finished is cut short",
		Test:    "TestServerCannotReadAPublishedFile",
	},
	{
		Section: "Relaying and Caching",
		Must:    "a node answers only for an object reachable from a current root hash",
		Test:    "TestServerAfterUnsharing",
	},
	{
		Section: "Relaying and Caching",
		Must:    "a node asked for what it neither publishes nor holds relays it, or answers 404 with no parent",
		Test:    "TestRelay",
	},
	{
		Section: "Proving Possession",
		Must:    "a relay signs a proof of its own and never forwards the one it received",
		Test:    "TestRelay",
	},
	{
		Section: "Ranges (Relaying)",
		Must:    "a relay passes Range up and 206 with Content-Range down unchanged",
		Test:    "TestRelay",
	},
	{Section: "Caching", Must: "a relay sends the directives it received unchanged", Test: "TestRelay"},
	{Section: "Caching", Must: "no-cache skips the downward cache and is passed to the parent", Test: "TestRelay"},
	{Section: "Failures", Must: "a relay maps its parent's failure by the table", Test: "TestRelay"},
	{
		Section: "Failures",
		Must:    "a relay that cannot reach its parent, or waits too long, answers 502",
		Test:    "TestRelayCannotReachTheParent",
	},
	{Section: "Failures", Must: "a node does not follow a redirect", Test: "TestRelay"},
	{
		Section: "Failures",
		Must:    "a node answered 401 checks its clock and re-runs the join exchange before it asks again",
		Test:    "TestFetchRejoinsAfterA401",
	},
	{Section: "Proving Possession", Must: "a caller proves possession on every request", Test: "TestFetch"},
	{
		Section: "The Channel",
		Must:    "a caller checks the certificate against its anchors and the host of the base URL",
		Test:    "TestFetchChannel",
	},
	{
		Section: "The Channel",
		Must:    "a caller dials a cleartext parent only when configured to",
		Test:    "TestFetchChannel",
	},
	{
		Section: "Ranges (Assembling)",
		Must:    "a 200 to a request with a Range replaces the bytes held",
		Test:    "TestFetch",
	},
	{
		Section: "Ranges (Assembling)",
		Must:    "a 206 that does not start where asked is a retryable failure",
		Test:    "TestFetch",
	},
	{
		Section: "Ranges (Assembling)",
		Must:    "a caller bounds its resumes and then fails as retryable",
		Test:    "TestFetch",
	},
	{
		Section: "Caching",
		Must:    "an object assembled from several answers takes the most restrictive retention",
		Test:    "TestFetch",
	},
	{
		Section: "Caching",
		Must:    "a caller that discarded a mismatching copy asks again with no-cache",
		Test:    "TestFetch",
	},
	{
		Section: "Failures",
		Must:    "a caller follows no redirect",
		Test:    "TestFetch",
	},
	{
		Section: "The Downward Cache",
		Must:    "a node keeps an object no longer than its retention",
		Test:    "TestCacheKeepsOnlyUnlimited",
	},
	{
		Section: "Retention",
		Must:    "a node discards an object of retention none when its use ends",
		Test:    "TestObtainRefusesALimitedRetention",
	},
	{
		Section: "Relaying and Caching",
		Must:    "a node whose Node Path changes discards its downward cache",
		Test:    "TestCacheFollowsTheNodePath",
	},
	{
		Section: "Ranges (Assembling)",
		Must:    "a node never puts a partial answer in its downward cache",
		Test:    "TestCacheFailedFillLeavesNothing",
	},
}

// TestConformanceTableNamesRealTests fails for a row whose test this package
// does not have, so a renamed test changes the table.
func TestConformanceTableNamesRealTests(t *testing.T) {
	t.Parallel()

	tests := testFunctions(t)
	for _, row := range conformance {
		if !tests[row.Test] {
			t.Errorf("%s: %q names %s, which is not a test of this package", row.Section, row.Must, row.Test)
		}
	}
}

// TestConformanceTableIsWellFormed fails on an empty cell and on a rule
// written twice.
func TestConformanceTableIsWellFormed(t *testing.T) {
	t.Parallel()

	written := map[string]bool{}
	for _, row := range conformance {
		if row.Section == "" || row.Must == "" || row.Test == "" {
			t.Errorf("an empty cell in row %+v", row)
		}

		key := row.Section + "\x00" + row.Must
		if written[key] {
			t.Errorf("the rule %q is in the table twice", row.Must)
		}

		written[key] = true
	}
}

// testFunctions returns the names of the test functions in the test files of
// this directory.
func testFunctions(t *testing.T) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read this directory: %v", err)
	}

	set := token.NewFileSet()
	names := map[string]bool{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(set, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), parseErr)
		}

		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}

	return names
}
