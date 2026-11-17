package arch_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A cut through the middle of a character, which ADR-20260829-jdq rule 3 refuses.
//
// Most of what a printer's firmware writes is not English. A bound written as
// `text[:maxSomething]` cuts bytes, so a value whose limit falls inside a
// two-byte or three-byte character loses half of it, and what reaches the JSON
// encoder is U+FFFD. `machinetext.Clip` is the one cut that pulls back to a
// rune boundary and marks what it took.
//
// This was found three times. `internal/transport/devtools/commands.go` had it,
// then `internal/extension/supervisor/instance.go`, whose value crosses the
// event bus as a condition message, then `internal/authz/store.go`, which
// quotes a device name back inside a refusal. Each was repaired on its own and
// nothing stopped the fourth.
//
// The shape is enumerable: a slice expression with no low bound whose high
// bound is an identifier named for a maximum. That is what a byte cap looks
// like in this tree, and every instance of it found by a sweep on 2026-08-30
// was either this defect or one of the four entries below.
//
// What it cannot catch: a cut whose bound is a literal, an expression, or a
// constant named something else. It is a floor under the rule and not the rule.
// A reviewer holds the rest, through ADR-20260829-jdq rule 3.

// textCutBound matches the name of a byte cap. `maxMessageLen`,
// `MaxPasswordBytes`, `maxEchoLen` and `maxDeviceNameLen` are the four spellings
// this tree has used.
var textCutBound = regexp.MustCompile(`^[Mm]ax[A-Z]`)

// textCutAllowed names a file whose constant-bounded cut is not this defect,
// against the reason. It is checked both ways: an entry naming a file with no
// such cut fails, so an excuse cannot outlive what it excuses.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var textCutAllowed = map[string]string{
	"internal/platform/auth/password.go": "clampPassword bounds the bytes that reach the key " +
		"derivation, and nothing reads them back as text. Argon2 is given a byte slice, and a " +
		"cut inside a character changes the digest of a password boundPassword refuses anyway.",
	"internal/transport/http/loginguard/loginguard.go": "clamp bounds a map key, so one entry's " +
		"cost is a number rather than whatever a caller sent. The key is never answered with, " +
		"logged or encoded.",
	"internal/kernel/ref/ref.go": "echo pulls the cut back to a rune boundary itself, with a " +
		"loop. It is the internal half of a pair sdk/event/origin.go holds, and ADR-20260824-gfr stops " +
		"the sdk half importing internal/kernel/machinetext, so collapsing the two is a row on " +
		"the old backlog",
	"internal/printer/simulator/metrics.go": "parseM334 copies what the firmware does: its " +
		"%20s reads at most twenty bytes of the host, and an M334 host is an address in ASCII, " +
		"so the simulator cuts at the same byte.",
	"sdk/event/origin.go": "the sdk half of that same pair, and the reason it cannot call " +
		"machinetext.Clip. It pulls the cut back to a rune boundary with the same loop.",
}

// textCutRoots are the trees this reads. They are the application's own.
//
//nolint:gochecknoglobals // read beside textCutAllowed, for the same reason.
var textCutRoots = []string{"internal", "sdk", "adapters", "cmd"}

// TestEveryConstantBoundedTextCutIsRuneSafe is the gate.
func TestEveryConstantBoundedTextCutIsRuneSafe(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	cuts, files := constantBoundedCuts(t, root)

	requireScanned(t, "Go files read for a constant-bounded cut", files, 400,
		"Either a tree in textCutRoots moved, or the walk stopped reaching it.")

	var violations []string

	for _, cut := range cuts {
		if _, allowed := textCutAllowed[cut.file]; allowed {
			continue
		}

		violations = append(violations, cut.where)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("%d cut(s) bound text at a byte and not a rune boundary:\n%s\n\n"+
			"ADR-20260829-jdq rule 3: cut on a rune boundary and mark it. Call\n"+
			"internal/kernel/machinetext.Clip, or add the file to textCutAllowed with the\n"+
			"reason the value is not text a person reads.",
			len(violations), strings.Join(violations, "\n"))
	}
}

// TestEveryTextCutExcuseNamesAFileThatCuts is the other direction. An entry
// naming a file that no longer cuts anything hides nothing and has to be read
// by everybody who meets the list.
func TestEveryTextCutExcuseNamesAFileThatCuts(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	cuts, _ := constantBoundedCuts(t, root)

	requireScanned(t, "textCutAllowed entries", len(textCutAllowed), 1)

	cutting := map[string]bool{}
	for _, cut := range cuts {
		cutting[cut.file] = true
	}

	for file, reason := range textCutAllowed {
		if !cutting[file] {
			t.Errorf("textCutAllowed names %s, which has no constant-bounded cut in it.\n"+
				"The excuse reads: %s\nDelete the entry.", file, reason)
		}
	}
}

// boundedCut is one slice expression and where it is written.
type boundedCut struct {
	file  string
	where string
}

// constantBoundedCuts is every `x[:maxSomething]` under textCutRoots, and how
// many Go files were read to find them.
func constantBoundedCuts(t *testing.T, root string) ([]boundedCut, int) {
	t.Helper()

	var (
		cuts []boundedCut
		read int
	)

	for _, tree := range textCutRoots {
		walkErr := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				return skipVendored(entry.Name())
			}

			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			read++

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}

			cuts = append(cuts, cutsInFile(t, path, filepath.ToSlash(rel))...)

			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", tree, walkErr)
		}
	}

	return cuts, read
}

// skipVendored keeps the walk out of directories that are somebody else's code.
func skipVendored(name string) error {
	if name == "node_modules" || name == "vendor" || name == "testdata" {
		return filepath.SkipDir
	}

	return nil
}

// cutsInFile reads one file's syntax. A slice expression with no low bound and
// an identifier high bound named for a maximum is the shape.
func cutsInFile(t *testing.T, path, rel string) []boundedCut {
	t.Helper()

	fileSet := token.NewFileSet()

	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}

	var found []boundedCut

	ast.Inspect(parsed, func(node ast.Node) bool {
		slice, isSlice := node.(*ast.SliceExpr)
		if !isSlice || slice.Low != nil || slice.Max != nil {
			return true
		}

		bound, isIdent := slice.High.(*ast.Ident)
		if !isIdent || !textCutBound.MatchString(bound.Name) {
			return true
		}

		found = append(found, boundedCut{
			file:  rel,
			where: fmt.Sprintf("    %s:%d cuts at %s", rel, fileSet.Position(slice.Pos()).Line, bound.Name),
		})

		return true
	})

	return found
}
