package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestTheReferenceURLResolvesToASectionOfThePage is the old backlog: the type URI
// a refusal carries has to open the section that explains the code. It holds the
// URI the server writes against the page the generator writes, so a change to
// either one alone fails here rather than on a help link a person clicked.
func TestTheReferenceURLResolvesToASectionOfThePage(t *testing.T) {
	t.Parallel()

	entries := fault.Global().All()
	if len(entries) == 0 {
		t.Fatal("no codes registered, so this test proves nothing")
	}

	page, err := renderReference(entries)
	if err != nil {
		t.Fatalf("renderReference: %v", err)
	}

	rendered := string(page)

	for _, entry := range entries {
		uri := entry.Code.TypeURI()

		base, anchor, cut := strings.Cut(uri, "#")
		if !cut {
			t.Errorf("%s carries the reference URL %q, which names no section", entry.Code, uri)

			continue
		}

		if base != fault.TypeBaseURI {
			t.Errorf("%s points at %q and the reference is published at %q", entry.Code, base, fault.TypeBaseURI)
		}

		if !strings.Contains(rendered, "{#"+anchor+"}\n") {
			t.Errorf("%s points at #%s, and the page carries no section anchored there", entry.Code, anchor)
		}
	}
}

// TestEveryCodeIsAnAnchoredSection is ADR-20260826-swm rules 1 and 2 held against the
// real registry: every registered code reaches the page as its own heading,
// carrying the code verbatim and the anchor a reader can construct.
func TestEveryCodeIsAnAnchoredSection(t *testing.T) {
	t.Parallel()

	entries := fault.Global().All()
	if len(entries) == 0 {
		t.Fatal("no codes registered, so this test proves nothing")
	}

	page, err := renderReference(entries)
	if err != nil {
		t.Fatalf("renderReference: %v", err)
	}

	rendered := string(page)

	for _, entry := range entries {
		want := "### `" + entry.Code.String() + "` {#" + entry.Code.Anchor() + "}\n"
		if !strings.Contains(rendered, want) {
			t.Errorf("code %s has no anchored heading. Wanted the line %q", entry.Code, strings.TrimSpace(want))
		}
	}
}

// TestThePageCarriesNoTable is ADR-20260826-swm rule 7 against the page that carried
// the defect. Prose in a table cell is the defect being fixed, and a
// generator is exactly the thing that can be stopped from reintroducing it.
func TestThePageCarriesNoTable(t *testing.T) {
	t.Parallel()

	page, err := renderReference(fault.Global().All())
	if err != nil {
		t.Fatalf("renderReference: %v", err)
	}

	for number, line := range strings.Split(string(page), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			t.Errorf("line %d is a table row, which rule 7 refuses on this page: %q", number+1, line)
		}
	}
}

// TestTwoCodesSharingAnAnchorStopsTheRun proves the collision gate fires rather
// than publishing two sections that answer to one link.
func TestTwoCodesSharingAnAnchorStopsTheRun(t *testing.T) {
	t.Parallel()

	// Two codes the grammar accepts as distinct that reduce to one anchor:
	// the underscore in a reason and the hyphen in a domain label both become
	// the same separator. That is the whole failure mode.
	first, err := fault.ParseCode("core.auth/A_B_C")
	if err != nil {
		t.Fatalf("parse the first code: %v", err)
	}

	second, err := fault.ParseCode("core.auth-a/B_C")
	if err != nil {
		t.Fatalf("parse the second code: %v", err)
	}

	if first.Anchor() != second.Anchor() {
		t.Fatalf("the fixture no longer collides: #%s and #%s", first.Anchor(), second.Anchor())
	}

	entries := []fault.Entry{
		{Code: first, Status: 400, Summary: "one", Action: "act"},
		{Code: second, Status: 400, Summary: "two", Action: "act"},
	}

	_, err = renderReference(entries)
	if !errors.Is(err, errDuplicateAnchor) {
		t.Fatalf("renderReference accepted two codes at one anchor, err = %v", err)
	}
}

// TestThePreambleExampleIsARegisteredCode stops the preamble pointing a reader
// at an anchor the page does not contain.
func TestThePreambleExampleIsARegisteredCode(t *testing.T) {
	t.Parallel()

	parsed, err := fault.ParseCode(exampleCode)
	if err != nil {
		t.Fatalf("parse the preamble example: %v", err)
	}

	if got := parsed.Anchor(); got != exampleAnchor {
		t.Fatalf("the preamble says %s is at #%s, and the code anchors at #%s", exampleCode, exampleAnchor, got)
	}

	for _, entry := range fault.Global().All() {
		if entry.Code.String() == exampleCode {
			return
		}
	}

	t.Fatalf("the preamble's example %s is not a registered code", exampleCode)
}
