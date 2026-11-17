package arch_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// firmwareLabelPackage defines connectorapi.FirmwareLabel. A package that can
// hold one has this in its transitive dependencies, because a value cannot
// exist without the package that declares its type being reachable. That is
// what makes the scan below a closed set rather than a sample.
const firmwareLabelPackage = modulePath + "/sdk/connectorapi"

// orderingOperators are the four that impose an order. Equality is not one of
// them: FirmwareLabel is comparable on purpose, and two labels being the same
// string is a fact about the device, not a guess about which came first.
var orderingOperators = []token.Token{token.LSS, token.GTR, token.LEQ, token.GEQ}

// TestFirmwareTextIsNeverOrdered is the gate connectorapi.FirmwareLabel's
// documentation names, and the reason that documentation can say the rule is
// held by something.
//
// FirmwareLabel is a struct so that < and > do not compile against it. That is a
// speed bump and not a wall: String is exported because the profile resolver and
// telemetry both need the text, so a.Firmware.String() > b.Firmware.String()
// compiles, and it answers wrongly for exactly the versions that matter, because
// "6.4.0" > "6.10.0" is true as text and false as a version. Selecting a
// capability profile that way gives a printer capabilities it does not have,
// which is the one direction an earlier design decision forbids.
//
// The scan is every package that can hold a label, and inside those an ordering
// comparison on any String result is refused rather than only one this gate can
// prove is a label's. Syntax cannot tell one Stringer from another, and a rule
// that only catches the spelling it was written for is the rule this repository
// has already been caught shipping. Ordering the text of anything in this set
// has no use anybody has found; ordering a version has profile.parseVersion,
// which reads the components as numbers.
//
// What it does not reach, said plainly rather than left to be discovered: a sort
// is not a comparison operator, so slices.SortFunc over strings.Compare of two
// labels passes this. That was measured rather than assumed, and the reason it
// is left open is that widening to strings.Compare refuses two sorts that are
// right - error codes by code, registered types by qualified name - and the
// syntax cannot tell those from a label. The hazard this gate is for is
// selecting a profile by comparing a reported version against a range bound,
// which is an operator and not a sort.
func TestFirmwareTextIsNeverOrdered(t *testing.T) {
	t.Parallel()

	holders := packagesThatCanHoldAFirmwareLabel(t)
	require.NotEmpty(t, holders, "this gate read no packages, which means it is checking nothing")

	var violations []string

	for _, dir := range holders {
		violations = append(violations, orderedTextComparisons(t, dir)...)
	}

	require.Empty(t, violations, "a firmware version is not ordered by its text: \"6.4.0\" sorts above\n"+
		"\"6.10.0\", so a profile selected this way is the wrong profile and the printer is offered\n"+
		"capabilities it does not have. Parse the components as numbers, the way\n"+
		"internal/printer/profile does, or select on a capability instead of on a version.")
}

// TestTheFirmwareGateReadsWhatItClaimsTo is the mutation check kept in the tree.
// A gate that has never been seen rejecting is a gate nobody has evidence for,
// and this one's whole value is that it rejects a shape the compiler accepts.
func TestTheFirmwareGateReadsWhatItClaimsTo(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	probes := map[string]struct {
		code   string
		refuse bool
	}{
		"the comparison the type cannot prevent": {
			code: "package p\nfunc f(a, b I) bool { return a.Firmware.String() > b.Firmware.String() }\n", refuse: true,
		},
		"the same comparison the other way round": {
			code:   "package p\nfunc f(a, b I) bool { return a.Firmware.String() <= b.Firmware.String() }\n",
			refuse: true,
		},
		"one side a literal": {
			code: "package p\nfunc f(a I) bool { return a.Firmware.String() < \"6.5\" }\n", refuse: true,
		},
		// The boundary, pinned so it is a decision rather than a gap somebody
		// finds later. See the gate's own comment for why it is here.
		"a sort, which this gate deliberately does not reach": {
			code:   "package p\nfunc f(a, b I) int { return strings.Compare(a.Firmware.String(), b.Firmware.String()) }\n",
			refuse: false,
		},
		"equality, which the type documents as legal": {
			code:   "package p\nfunc f(a, b I) bool { return a.Firmware.String() == b.Firmware.String() }\n",
			refuse: false,
		},
		"a number compared, which is what parsing produces": {
			code: "package p\nfunc f(a, b [3]int) bool { return a[0] < b[0] }\n", refuse: false,
		},
	}

	// Three probes the gate must refuse and three it must accept. A table that
	// lost its refusing half leaves a mutation check with nothing to mutate.
	requireScanned(t, "probes against the ordering gate", len(probes), 6)

	for name, source := range probes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(fset, "probe.go", source.code, parser.SkipObjectResolution)
			require.NoError(t, err)

			found := orderedTextLines(fset, "probe.go", file)

			if source.refuse {
				require.NotEmpty(t, found, "the gate read this and saw nothing")

				return
			}

			require.Empty(t, found)
		})
	}
}

// packagesThatCanHoldAFirmwareLabel returns the directory of every package in
// either module whose reachable set includes the package that declares the
// label.
//
// Test imports are folded in with one hop of their own dependencies, because
// `go list` reports Deps for what a package imports and not for what its tests
// import. A test that reaches a label through a helper package is a test that
// can order one.
func packagesThatCanHoldAFirmwareLabel(t *testing.T) []string {
	t.Helper()

	root := repoRoot(t)

	pkgs := listWithDeps(t, root)
	pkgs = append(pkgs, listWithDeps(t, filepath.Join(root, "sdk"))...)

	depsOf := make(map[string][]string, len(pkgs))
	for _, pkg := range pkgs {
		depsOf[pkg.ImportPath] = pkg.Deps
	}

	var dirs []string

	for _, pkg := range pkgs {
		if reachesTheLabel(pkg, depsOf) {
			dirs = append(dirs, pkg.Dir)
		}
	}

	slices.Sort(dirs)

	return slices.Compact(dirs)
}

func reachesTheLabel(pkg listedWithDeps, depsOf map[string][]string) bool {
	if pkg.ImportPath == firmwareLabelPackage {
		return true
	}

	if slices.Contains(pkg.Deps, firmwareLabelPackage) {
		return true
	}

	for _, imported := range slices.Concat(pkg.TestImports, pkg.XTestImports) {
		if imported == firmwareLabelPackage || slices.Contains(depsOf[imported], firmwareLabelPackage) {
			return true
		}
	}

	return false
}

// orderedTextComparisons parses every Go file of one package directory,
// including its tests, and reports every ordering comparison on a String
// result. The directory is read rather than the file list `go list` produced, so
// a file behind a build tag is read too: the gate is about what is written, and
// a tag that hides a comparison from this configuration does not stop it
// compiling in another.
func orderedTextComparisons(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	fset := token.NewFileSet()

	var found []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, parseErr)

		found = append(found, orderedTextLines(fset, path, file)...)
	}

	return found
}

// orderedTextLines reports every ordering comparison on a String result in one
// file.
func orderedTextLines(fset *token.FileSet, path string, file *ast.File) []string {
	var found []string

	ast.Inspect(file, func(n ast.Node) bool {
		node, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		if !slices.Contains(orderingOperators, node.Op) {
			return true
		}

		if !isStringCall(node.X) && !isStringCall(node.Y) {
			return true
		}

		found = append(found, position(fset, path, node.Pos())+": "+node.Op.String()+" on a String result")

		return true
	})

	return found
}

// isStringCall reports whether an expression is a no-argument call to a method
// named String.
func isStringCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}

	selector, isSelector := call.Fun.(*ast.SelectorExpr)

	return isSelector && selector.Sel.Name == "String"
}

func position(fset *token.FileSet, path string, pos token.Pos) string {
	at := fset.Position(pos)
	if at.Filename == "" {
		at.Filename = path
	}

	return filepath.ToSlash(at.Filename) + ":" + strconv.Itoa(at.Line)
}

// listedWithDeps is what this gate needs from `go list` and nothing more: where
// the package's files are, and everything it can reach.
type listedWithDeps struct {
	ImportPath   string   `json:"ImportPath"`
	Dir          string   `json:"Dir"`
	Deps         []string `json:"Deps"`
	TestImports  []string `json:"TestImports"`
	XTestImports []string `json:"XTestImports"`
}

func listWithDeps(t *testing.T, dir string) []listedWithDeps {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = dir

	out, err := cmd.Output()
	require.NoErrorf(t, err, "go list in %s%s", dir, arch.StderrOf(err))

	decoder := json.NewDecoder(bytes.NewReader(out))

	var pkgs []listedWithDeps

	for {
		var pkg listedWithDeps

		decodeErr := decoder.Decode(&pkg)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		require.NoError(t, decodeErr)

		pkgs = append(pkgs, pkg)
	}

	return pkgs
}
