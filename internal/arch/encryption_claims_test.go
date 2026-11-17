package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// claimedEndToEnd matches the promise, in every spelling a writer reaches for.
// "end-to-end" on its own is not matched: an end-to-end test is an ordinary
// phrase and the claim this refuses is about encryption.
//
//nolint:gochecknoglobals // a compiled pattern, never written to.
var claimedEndToEnd = regexp.MustCompile(
	`(?i)\b(end[ -]?to[ -]?end|e2ee)\b[^.\n]{0,40}\b(encrypt|encrypted|encryption)\b|` +
		`\b(encrypt|encrypted|encryption)\b[^.\n]{0,40}\b(end[ -]?to[ -]?end|e2ee)\b`)

// claimSurfaces are the trees a user reads as a promise: the published
// documentation site, the front page, the OpenAPI document the API serves, the
// frontend a browser renders, and every Go string this binary can print.
//
// docs/decisions holds engineering records and is not here. A record is an
// argument about what the phrase means and why we may not use it, and a gate
// that refused the argument would delete the reason for the rule.
//
//nolint:gochecknoglobals // a closed list of trees, never written to.
var claimSurfaces = []string{
	"docs/src/content/docs",
	"internal",
	"adapters",
	"sdk",
	"cmd",
	"frontend/src",
	"config",
	"README.md",
	"AGENTS.md",
}

// claimExtensions are the files inside those trees that carry text a user sees.
//
//nolint:gochecknoglobals // a closed set, never written to.
var claimExtensions = map[string]bool{
	".md": true, ".mdx": true, ".go": true, ".json": true, ".yaml": true, ".yml": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".html": true,
}

// TestNoPublishedTextClaimsEndToEndEncryption is a design requirement and earlier research
// §6, as a gate.
//
// Every media path in this product decrypts at this node, because this node is
// the thing doing the relay. End-to-end is a claim about who holds the key and
// not about which protocols are in the path, and four camera vendors have now
// been found by a regulator to have made it while a second viewing path or a
// support tool could read the frames. The phrase is therefore refused outright
// rather than reviewed case by case: a reviewer who has to decide whether this
// particular use is honest is the control that failed at Eufy.
//
// The gate is on the claim, not on the subject. Writing about encryption is
// fine, and so is writing about why this product does not make the claim.
func TestNoPublishedTextClaimsEndToEndEncryption(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	var (
		violations []string
		scanned    int
	)

	for _, surface := range claimSurfaces {
		target := filepath.Join(root, surface)

		info, err := os.Stat(target)
		if err != nil {
			continue
		}

		if !info.IsDir() {
			scanned++
			violations = append(violations, claimsIn(t, root, target)...)

			continue
		}

		err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				if skippedTree(entry.Name()) {
					return filepath.SkipDir
				}

				return nil
			}

			if !claimExtensions[strings.ToLower(filepath.Ext(path))] {
				return nil
			}

			scanned++
			violations = append(violations, claimsIn(t, root, path)...)

			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", surface, err)
		}
	}

	requireScanned(t, "published text files under the scanned surfaces", scanned, 1,
		"claimExtensions names the suffixes it reads, and the walk names the surfaces.")

	if len(violations) > 0 {
		t.Errorf("%d place(s) claim end-to-end encryption. Every media path decrypts at this node, "+
			"because this node does the relay, so the claim is false here whatever protocol is in the "+
			"path. Write what is protected and what is not: earlier research has the wording.\n\t%s",
			len(violations), strings.Join(violations, "\n\t"))
	}
}

// gateFile is this file. It carries the phrase in its own pattern and in the
// message it prints, so it is the one exemption, named rather than matched by a
// rule somebody else's file could satisfy.
const gateFile = "internal/arch/encryption_claims_test.go"

// claimsIn reports every line of one file that makes the claim.
func claimsIn(t *testing.T, root, path string) []string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}

	relative = filepath.ToSlash(relative)
	if relative == gateFile {
		return nil
	}

	var found []string

	for number, line := range strings.Split(string(content), "\n") {
		if claimedEndToEnd.MatchString(line) {
			found = append(found, fmt.Sprintf("%s:%d: %s", relative, number+1, strings.TrimSpace(line)))
		}
	}

	return found
}

// skippedTree names the directories that hold nothing a user reads: build
// output, dependencies, and the quarantined implementation.
func skippedTree(name string) bool {
	switch name {
	case "node_modules", "dist", "build", ".astro", "testdata":
		return true
	}

	return false
}
