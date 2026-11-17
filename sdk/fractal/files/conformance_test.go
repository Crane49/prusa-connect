package files_test

import (
	"testing"

	conf "github.com/prusa3d/prusa-connect/sdk/fractal/internal/conformance"
)

// conformance pairs the requirements of fractal-files.md and of
// fractal-files-http.md, The HTTP Join Entry, that this package can test
// with the test that proves each. The module packages/fractal tests the
// rules that need a node: one ContentStore1 per store that is never
// withdrawn, its selection in the ancestor shadow, a files entry in every
// grant, and discarding an object that does not hash to its identity.
var conformance = []conf.Row{
	{
		Section: "Object Identity",
		Must:    "an identity in a listing is the type byte followed by the digest",
		Test:    "TestIdentityEncodings",
	},
	{
		Section: "Object Identity",
		Must:    "a receiver refuses a byte string whose length its type does not require, and an undefined type",
		Test:    "TestParseBytesRefuses",
	},
	{
		Section: "Object Identity",
		Must:    "text is the name, a colon and the digest in base64url, unpadded, with unused bits zero",
		Test:    "TestIdentityEncodings",
	},
	{
		Section: "Object Identity",
		Must:    "a receiver refuses text of any other form, a short digest and an undefined name, matched with case",
		Test:    "TestParseTextRefuses",
	},
	{
		Section: "Object Identity",
		Must:    "the digest is taken over the object's bytes alone",
		Test:    "TestIdentityEncodings",
	},
	{
		Section: "The URL",
		Must:    "an object is fractal:/// and its identity text, with the scheme lowercase and no fragment",
		Test:    "TestURLString",
	},
	{
		Section: "The URL",
		Must:    "a receiver matches the scheme case-insensitively and ignores a query parameter it does not know",
		Test:    "TestParseURL",
	},
	{
		Section: "The URL",
		Must:    "a receiver refuses a URL of any other form and a path with a percent-encoded octet",
		Test:    "TestParseURLRefuses",
	},
	{
		Section: "The URL",
		Must:    "two URLs whose paths agree name one object",
		Test:    "TestURLQueryDoesNotChangeIdentity",
	},
	{
		Section: "The URL",
		Must:    "a value escapes every character outside the unreserved set of RFC 3986",
		Test:    "TestURLString",
	},
	{
		Section: "The URL",
		Must:    "a receiver ignores a name parameter that breaks the character rule of Names",
		Test:    "TestParseURL",
	},
	{
		Section: "The Listing",
		Must:    "a listing is the version and the entries, and an entry is name, kind, size and hash",
		Test:    "TestTreeListing",
	},
	{
		Section: "The Listing",
		Must:    "a directory entry's size is the length of its listing",
		Test:    "TestTreeListing",
	},
	{
		Section: "The Listing",
		Must:    "entries are sorted by the UTF-8 bytes of their names",
		Test:    "TestTreeListing",
	},
	{
		Section: "The Listing",
		Must:    "a reader refuses entries that are not sorted and unique",
		Test:    "TestDecodeListingRefuses",
	},
	{
		Section: "Encoding",
		Must:    "a listing is deterministic CBOR as RFC 8949, section 4.2.1, defines",
		Test:    "TestDecodeListingRefuses",
	},
	{
		Section: "Encoding",
		Must:    "every implementation produces the identity of the empty listing, 82 01 80",
		Test:    "TestEmptyListing",
	},
	{
		Section: "Encoding",
		Must:    "the example tree gives the listings and identities the extension shows",
		Test:    "TestTreeListing",
	},
	{
		Section: "Versioning",
		Must:    "a reader refuses a listing whose version it does not support, and reads no part of it",
		Test:    "TestDecodeListingRefuses",
	},
	{
		Section: "Names",
		Must:    "a name is valid UTF-8, not empty, without a slash or U+0000, and not . or ..",
		Test:    "TestCheckName",
	},
	{
		Section: "Names",
		Must:    "names are unique within one directory",
		Test:    "TestEncodeRefuses",
	},
	{
		Section: "Join Data",
		Must:    "a files entry carries version 1 and the join data of each binding under its name",
		Test:    "TestJoinEntryMarshal",
	},
	{
		Section: "Join Data",
		Must:    "a node refuses a files entry whose version it does not support",
		Test:    "TestParseJoinEntryRefuses",
	},
	{
		Section: "The HTTP Join Entry",
		Must:    "a node carries an http part in every files entry it issues",
		Test:    "TestJoinEntryMarshalRefuses",
	},
	{
		Section: "The HTTP Join Entry",
		Must:    "a node refuses a files entry with no http part or an http version it does not support",
		Test:    "TestParseJoinEntryRefuses",
	},
	{
		Section: "The HTTP Join Entry",
		Must:    "baseUrl is absolute, with no query and no fragment, and uses https",
		Test:    "TestCheckBaseURL",
	},
	{
		Section: "The HTTP Join Entry",
		Must:    "a node takes an http baseUrl only when it permits a cleartext parent",
		Test:    "TestParseJoinEntryAllowsCleartext",
	},
	{
		Section: "Interface: ContentStore1",
		Must:    "ContentStore1 is exposed both ways, with its five properties and List",
		Test:    "TestContentStore1Definition",
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
