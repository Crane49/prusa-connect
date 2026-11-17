package arch

import "strings"

// ImportableFrom reports whether importer may import importPath under Go's
// internal rule: a path holding an "internal" element is visible only to code
// in the tree rooted at that element's parent.
//
// It is exported because the registry probes in cmd/generate-extensions and
// cmd/generate-error-codes both build a main package at the root of this
// module and blank-import every candidate at once. A candidate under
// sdk/internal/ is rooted at sdk/ and is not visible there, so listing it
// fails the compile rather than the gate, and the reader is told about an
// import rule instead of about a missing registration.
//
// Skipping such a package costs the probe nothing it can otherwise reach. An
// sdk-internal package runs its initialisers whenever a published contract
// package imports it, and one no contract package imports is linked into no
// binary either.
func ImportableFrom(importPath, importer string) bool {
	parent, restricted := internalRoot(importPath)
	if !restricted {
		return true
	}

	return importer == parent || strings.HasPrefix(importer, parent+"/")
}

// internalRoot is the directory an internal element's visibility is rooted at.
// It takes the last element, so the innermost internal in a nested path is the
// one that governs.
func internalRoot(importPath string) (string, bool) {
	if at := strings.LastIndex(importPath, "/internal/"); at >= 0 {
		return importPath[:at], true
	}

	return strings.CutSuffix(importPath, "/internal")
}
