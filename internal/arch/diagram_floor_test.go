package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ADR-20260826-shb rule 10 asks that a document explaining a topology, a state
// machine, a message flow or a pipeline carries a diagram of it. The record's
// enforcement table says there is no honest gate for that rule and its open
// items name the fallback this file builds: a floor per document, on the model
// of `onboardingPages`.
//
// The rule itself is not gated here and cannot be. Whether a page explains a
// pipeline is a judgement over its prose, and the keyword proxy over headings
// was measured and rejected: 42 candidate files from a list a person chose,
// with unknown recall, needing the opt-out ADR-20260826-gtb refused.
//
// What is gated is the ratchet under it. A document that carries a diagram
// today keeps it, so a rewrite cannot quietly drop the picture and leave the
// prose. A document that never had one is not accused, which is the half a
// keyword list got wrong.

// mermaidFence matches the opening fence of a diagram. Obsidian and the docs
// site both draw this one, which is why ADR-20260826-shb rule 10 names it.
var mermaidFence = regexp.MustCompile("(?m)^\\s*```mermaid")

// diagrammedDocuments are the documents carrying a `mermaid` fence, and the
// number each carried when this floor was written.
//
// A file that legitimately sheds a diagram has its number edited in the same
// change, and one that drops to zero leaves the list. Both are a decision
// somebody writes down rather than a count that quietly falls.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var diagrammedDocuments = []struct {
	file     string
	diagrams int
}{
	{file: "docs/decisions/20260826-shb-a-document-is-read-at-speed.md", diagrams: 1},
	{file: "docs/decisions/20260826-c77-one-name-for-one-thing-on-the-wire.md", diagrams: 1},
	{file: "docs/decisions/20260826-5yj-a-device-is-the-object-and-a-printer-is-the-type.md", diagrams: 1},
	{
		file:     "docs/decisions/20260831-c7b-an-adapter-declares-the-states-and-the-permissions-and-the-host-places-them.md",
		diagrams: 1,
	},
	{file: "docs/src/content/docs/internals/authorization.md", diagrams: 2},
	{file: "docs/src/content/docs/internals/build-gates-and-testing.md", diagrams: 1},
	{file: "docs/src/content/docs/internals/printers-and-drivers.md", diagrams: 2},
	{file: "docs/src/content/docs/internals/the-application-core.md", diagrams: 2},
	{file: "docs/src/content/docs/internals/the-extension-host.md", diagrams: 2},
	{file: "docs/src/content/docs/internals/the-sdk-and-extensions.md", diagrams: 2},
	{file: "docs/src/content/docs/internals/transport-and-the-stream.md", diagrams: 2},
	{file: "docs/src/content/docs/plugin-architecture.md", diagrams: 6},
	{file: "docs/src/content/docs/prusa-xbuddy-websocket-protocol.md", diagrams: 3},
}

// TestEveryDiagrammedDocumentKeepsItsDiagrams is the floor. A rewrite that
// drops a diagram from a listed document fails here.
func TestEveryDiagrammedDocumentKeepsItsDiagrams(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	requireScanned(t, "documents listed as carrying a diagram",
		len(diagrammedDocuments), 13,
		"The table emptied, so no document's diagrams would be counted.")

	var violations []string

	for _, doc := range diagrammedDocuments {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(doc.file)))
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s is listed as carrying %d diagram(s) and cannot be read: %v",
				doc.file, doc.diagrams, err))

			continue
		}

		if found := len(mermaidFence.FindAllString(string(body), -1)); found < doc.diagrams {
			violations = append(violations, fmt.Sprintf(
				"%s carries %d `mermaid` fence(s), and this floor expects at least %d. "+
					"Put the diagram back, or edit the number in this change",
				doc.file, found, doc.diagrams))
		}
	}

	reportDiagrams(t, violations)
}

// TestEveryDocumentWithADiagramIsListed keeps the table current. A document
// that gains a diagram and is not listed keeps it by luck, and the floor is
// worth nothing over a list that stopped growing with the tree.
//
// It asks for a line and never for a diagram, so a document with no picture is
// not accused of owing one.
func TestEveryDocumentWithADiagramIsListed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	listed := map[string]struct{}{}
	for _, doc := range diagrammedDocuments {
		listed[doc.file] = struct{}{}
	}

	var violations []string

	read := 0

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}

			return nil
		}

		switch filepath.Ext(entry.Name()) {
		case ".md", ".mdx":
		default:
			return nil
		}

		read++

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		if !mermaidFence.Match(body) {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		name := filepath.ToSlash(rel)
		if _, ok := listed[name]; !ok {
			violations = append(violations, fmt.Sprintf(
				"%s carries a `mermaid` fence and is not in diagrammedDocuments, so nothing holds it there",
				name))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository for markdown: %v", err)
	}

	requireScanned(t, "markdown files outside node_modules", read, 150,
		"The walk stopped finding markdown, so no unlisted diagram would be found.")

	reportDiagrams(t, violations)
}

// reportDiagrams fails with every violation listed, because one document at a
// time across thirty is thirty runs of the suite.
func reportDiagrams(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d document(s) disagree with the diagram floor:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}
