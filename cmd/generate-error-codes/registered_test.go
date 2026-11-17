package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// The registry is filled by package initialisation, so a code only reaches the
// artefacts if this command imports the package that registers it. Gates 1, 3
// and 4 cannot see a package nobody imports: the registry, the reference and the
// vanished-code check all agree with each other about a catalogue that is
// missing entries. This is the gate that looks outside that agreement.
//
// It runs the code rather than reading the source for calls that look like
// registrations. Every package that could register anything is linked into one
// program, and what that program's registry holds at initialisation is compared
// with what the committed artefact documents. A registration has one spelling
// at run time and many in source: an aliased import, `fault.Global().Register`,
// or a house wrapper called through, any of which a source scan misses or
// attributes to the wrong package.
//
// A test package is excluded because a code registered by a test is not a
// product code. A main package is excluded because
// nothing can import one, which is not a reason to leave it alone: that is the
// half of the gate TestNoMainPackageRegistersACode covers, by reading the source,
// because there is no other way to reach it.

const modulePath = "github.com/prusa3d/prusa-connect"

func TestEveryRegisteredCodeIsDocumented(t *testing.T) {
	t.Parallel()

	// The probe links and runs the whole tree once per shipping tag set, so
	// what it costs is a full build rather than this package's own code. Under
	// -race the parent's build cache holds instrumented objects the child
	// build cannot reuse, so every tag set compiles the tree again and the
	// test runs past the 11 minute per-package timeout on a hosted runner.
	// -short skips it, which is what -short is for. It still runs without
	// -race: `make error-codes-check` calls it, from the pre-push hook and
	// from CI.
	if testing.Short() {
		t.Skip("links the whole tree once per tag set, so it runs without -short")
	}

	root := repoRootDir(t)

	documented := map[string]bool{}

	committed, err := readRegistryFile(filepath.Join(root, defaultRegistryPath))
	if err != nil {
		t.Fatalf("reading the committed registry: %v", err)
	}

	for _, entry := range committed {
		documented[entry.Code.String()] = true
	}

	if len(documented) == 0 {
		t.Fatalf("%s documents no codes, so this gate is comparing against nothing", defaultRegistryPath)
	}

	registered := codesTheTreeRegisters(t, root)

	var undocumented []string

	for _, code := range registered {
		if !documented[code] {
			undocumented = append(undocumented, code)
		}
	}

	if len(undocumented) == 0 {
		return
	}

	t.Errorf("%d code(s) register at initialisation and are in neither artefact:\n\t%s\n\n"+
		"A client receiving one of these finds nothing in the published reference, and the\n"+
		"vanished-code gate will not notice when it disappears. Blank-import the package\n"+
		"that registers each one in cmd/generate-error-codes/main.go and regenerate.",
		len(undocumented), strings.Join(undocumented, "\n\t"))
}

// TestNoMainPackageRegistersACode is the one thing the link graph cannot reach.
//
// Every other registration is found by linking the package that holds it into a
// probe. A main package cannot be linked into anything, because nothing can
// import one, so a fault.Register in cmd/prusa-connect ships in the
// binary, answers a real client with a real code, and appears in neither
// artefact: not in the published reference the client reads, and not in the
// vanished-code gate that would notice it disappearing. Every gate stays green.
//
// So this one reads the source, which is the weaker instrument and the only one
// available. It is deliberately wider than a match on `fault.Register`: any call
// to something named Register counts, unless the receiver names an imported
// package that is not sdk/fault. That covers an alias, `fault.Global().Register`
// and a registry held in a variable, and leaves `prometheus.Register` alone.
//
// What it does not cover: a main package calling a house wrapper from a package
// that does not itself import sdk/fault. There is no way to see that without
// type information, and a main package is the one place the link graph cannot
// help.
func TestNoMainPackageRegistersACode(t *testing.T) {
	t.Parallel()

	root := repoRootDir(t)
	commands := filepath.Join(root, "cmd")

	entries, err := os.ReadDir(commands)
	if err != nil {
		t.Fatalf("reading cmd/: %v", err)
	}

	mains := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(commands, entry.Name())

		isMain, offences, scanErr := registrationsInMain(dir)
		if scanErr != nil {
			t.Fatalf("scanning %s: %v", dir, scanErr)
		}

		if !isMain {
			continue
		}

		mains++

		for _, where := range offences {
			t.Errorf("cmd/%s is a main package and registers an error code at %s.\n"+
				"Nothing can import a main package, so that code reaches a client and is in\n"+
				"neither docs/errors/error-codes.json nor the published reference, and the\n"+
				"vanished-code gate will never notice it go. Use a code from the catalogue in\n"+
				"sdk/fault, or move the registration into a library package and blank-import\n"+
				"that package in cmd/generate-error-codes/main.go.", entry.Name(), where)
		}
	}

	if mains == 0 {
		t.Fatal("no main package under cmd/, so this gate is reading nothing")
	}
}

// registrationsInMain parses one directory and reports whether it is a main
// package and where in it a code looks like it is being registered.
//
// Build tags are ignored on purpose. A registration behind `//go:build sqlite`
// is still a registration in the binary that tag ships in, and a main package
// has no configuration in which an unreachable code becomes reachable.
func registrationsInMain(dir string) (bool, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, nil, err
	}

	fset := token.NewFileSet()
	isMain := false
	importsFault := false
	faultNames := map[string]bool{}
	otherImports := map[string]bool{}

	var files []*ast.File

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return false, nil, parseErr
		}

		if file.Name.Name != "main" {
			continue
		}

		isMain = true

		files = append(files, file)

		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}

			local := localName(spec, path)

			if path == modulePath+"/sdk/fault" {
				importsFault = true
				faultNames[local] = true

				continue
			}

			otherImports[local] = true
		}
	}

	if !isMain {
		return false, nil, nil
	}

	var offences []string

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Register" {
				return true
			}

			root := rootIdent(sel.X)

			switch {
			case faultNames[root]:
			case otherImports[root]:
				return true
			case !importsFault:
				return true
			}

			offences = append(offences, fset.Position(call.Pos()).String())

			return true
		})
	}

	sort.Strings(offences)

	return true, offences, nil
}

// localName is the name an import is spelled with in the file that declares it.
// An alias is the cheapest way to defeat a source scan that matches `fault.`.
func localName(spec *ast.ImportSpec, path string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}

	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}

	return path
}

// rootIdent is the leftmost identifier of a selector chain, so
// `fault.Global().Register` roots at `fault` and `registry.Register` at
// `registry`.
func rootIdent(expr ast.Expr) string {
	for {
		switch node := expr.(type) {
		case *ast.Ident:
			return node.Name
		case *ast.SelectorExpr:
			expr = node.X
		case *ast.CallExpr:
			expr = node.Fun
		case *ast.IndexExpr:
			expr = node.X
		case *ast.ParenExpr:
			expr = node.X
		case *ast.StarExpr:
			expr = node.X
		default:
			return ""
		}
	}
}

// codesTheTreeRegisters links every package that could register a code and
// reports what the global registry holds once their initialisers have run.
//
// It is done once per tag set, because a registration behind a build tag is
// still a registration in the binary that tag ships in.
//
// The listings run concurrently and the probes one after another. A listing
// compiles nothing. A probe compiles the first-party tree under its tags, and
// most of that tree compiles the same under every set, so a probe that runs
// after another finds those packages in the build cache. Run concurrently,
// each `go` process compiled them again for itself, and on the two cores of a
// hosted runner this package took about 150 seconds.
func codesTheTreeRegisters(t *testing.T, root string) []string {
	t.Helper()

	sets := registryTagSets(t, root)
	candidates := make([][]string, len(sets))
	failures := make([]error, len(sets))

	var wg sync.WaitGroup

	for i, tags := range sets {
		wg.Go(func() {
			candidates[i], failures[i] = registryCandidatesUnder(root, tags)
		})
	}

	wg.Wait()

	found := map[string]bool{}

	for i, tags := range sets {
		if failures[i] != nil {
			t.Fatalf("tags %q: %v", tags, failures[i])
		}

		codes, err := runRegistryProbe(root, candidates[i], tags)
		if err != nil {
			t.Fatalf("tags %q: %v", tags, err)
		}

		for _, code := range codes {
			found[code] = true
		}
	}

	codes := make([]string, 0, len(found))
	for code := range found {
		codes = append(codes, code)
	}

	sort.Strings(codes)

	// A gate that reads nothing passes everything, and this one compares two
	// lists: an empty left-hand side agrees with any right-hand side. Every
	// other loader in this file says so, and registration happens at package
	// initialisation, which is exactly the thing that could stop happening
	// without a compile error anywhere.
	if len(codes) == 0 {
		t.Fatalf("no code registered under any of %d tag set(s), so this gate is comparing an empty\n"+
			"registry against the committed artefact and will agree with anything. Registration\n"+
			"runs at package initialisation: check that sdk/fault still registers its own catalogue\n"+
			"and that the probe still links the candidates.", len(sets))
	}

	return codes
}

func registryCandidatesUnder(root, tags string) ([]string, error) {
	candidates, err := registryCandidates(root, tags)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no package imports %s/sdk/fault, so this gate is reading nothing", modulePath)
	}

	return candidates, nil
}

// registryTagSets is the tag sets the build recipes compile under, from the same
// reader the import-graph gates use. A list written out here drifts from what
// `make build` compiles, which is the same failure that let a `//go:build` file
// hide from the import gates - and it had already happened: the three sets this
// replaces were the untagged build, ALL_TAGS and minimal, and the image is built
// with none of them. A file behind `//go:build full && !fractal` compiled into
// the shipped binary, registered a code, and reached no artefact with this gate
// green.
func registryTagSets(t *testing.T, root string) []string {
	t.Helper()

	sets, err := arch.TagSets(root)
	if err != nil {
		t.Fatalf("reading the build recipes: %v", err)
	}

	return slices.Compact(slices.Sorted(maps.Values(sets)))
}

// registryCandidates is every package that could hold a registration: first
// party, not a main package, and reaching sdk/fault somewhere in its own
// imports. Nothing that cannot see the fault package can call into it,
// whatever the call is spelled like, so this over-approximates in the only safe
// direction and the probe decides the rest.
func registryCandidates(root, tags string) ([]string, error) {
	var candidates []string

	for _, dir := range []string{".", "sdk"} {
		listed, err := listWithDeps(filepath.Join(root, dir), tags)
		if err != nil {
			return nil, err
		}

		for _, pkg := range listed {
			if pkg.Name == "main" || !strings.HasPrefix(pkg.ImportPath, modulePath) {
				continue
			}

			if !arch.ImportableFrom(pkg.ImportPath, modulePath+"/"+probeDir) {
				continue
			}

			if !slices.Contains(pkg.Deps, modulePath+"/sdk/fault") &&
				pkg.ImportPath != modulePath+"/sdk/fault" {
				continue
			}

			candidates = append(candidates, pkg.ImportPath)
		}
	}

	slices.Sort(candidates)

	return slices.Compact(candidates), nil
}

type listedPkg struct {
	ImportPath string   `json:"ImportPath"`
	Name       string   `json:"Name"`
	Deps       []string `json:"Deps"`
}

func listWithDeps(dir, tags string) ([]listedPkg, error) {
	// Only the fields read below, so the toolchain skips the rest.
	args := []string{"list", "-json=ImportPath,Name,Deps"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}

	args = append(args, "./...")

	cmd := exec.Command("go", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w%s", filepath.Base(dir), err, arch.StderrOf(err))
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))

	var pkgs []listedPkg

	for {
		var pkg listedPkg

		if decErr := dec.Decode(&pkg); decErr != nil {
			if errors.Is(decErr, io.EOF) {
				break
			}

			return nil, fmt.Errorf("decoding go list output: %w", decErr)
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs, nil
}

// probeDir is where the generated program appears to live. It never exists on
// disk: the file is supplied through the toolchain's overlay, so a gate that
// has to compile something inside the module writes nothing into the working
// tree and leaves nothing behind if it is killed.
const probeDir = ".error-code-registry-probe"

// runRegistryProbe compiles a program that blank-imports every candidate and
// prints the codes its registry holds.
func runRegistryProbe(root string, candidates []string, tags string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "error-code-probe-")
	if err != nil {
		return nil, fmt.Errorf("scratch directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(scratch) }()

	sourcePath := filepath.Join(scratch, "main.go")
	if writeErr := os.WriteFile(sourcePath, []byte(renderProbe(candidates)), 0o600); writeErr != nil {
		return nil, fmt.Errorf("writing the probe: %w", writeErr)
	}

	overlay, err := json.Marshal(map[string]any{
		"Replace": map[string]string{
			filepath.Join(root, probeDir, "main.go"): sourcePath,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the overlay: %w", err)
	}

	overlayPath := filepath.Join(scratch, "overlay.json")
	if writeErr := os.WriteFile(overlayPath, overlay, 0o600); writeErr != nil {
		return nil, fmt.Errorf("writing the overlay: %w", writeErr)
	}

	args := []string{"run", "-overlay", overlayPath}
	if tags != "" {
		args = append(args, "-tags", tags)
	}

	args = append(args, "./"+probeDir)

	cmd := exec.Command("go", args...)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running the registry probe: %w%s\n\n"+
			"A panic here is usually two packages registering the same code. This probe links\n"+
			"every registering package at once, so it says the code is already taken somewhere\n"+
			"in the tree, not that any one binary would die at start-up. It is still a defect:\n"+
			"a code is a public identifier and the namespace is global, so a client cannot be\n"+
			"told which of the two meanings it received. Rename one of them",
			err, arch.StderrOf(err))
	}

	var codes []string
	if decErr := json.Unmarshal(out, &codes); decErr != nil {
		return nil, fmt.Errorf("decoding the probe's output: %w\n%s", decErr, out)
	}

	return codes, nil
}

func renderProbe(candidates []string) string {
	var b strings.Builder

	b.WriteString("package main\n\nimport (\n\t\"encoding/json\"\n\t\"os\"\n\n")
	fmt.Fprintf(&b, "\t%q\n\n", modulePath+"/sdk/fault")

	for _, pkg := range candidates {
		if pkg == modulePath+"/sdk/fault" {
			continue
		}

		fmt.Fprintf(&b, "\t_ %q\n", pkg)
	}

	b.WriteString(")\n\nfunc main() {\n")
	b.WriteString("\tcodes := []string{}\n")
	b.WriteString("\tfor _, entry := range fault.Global().All() {\n")
	b.WriteString("\t\tcodes = append(codes, entry.Code.String())\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\tif err := json.NewEncoder(os.Stdout).Encode(codes); err != nil {\n")
	b.WriteString("\t\tpanic(err)\n\t}\n}\n")

	return b.String()
}

// repoRootDir walks up from the test's working directory to the directory
// holding go.mod, so the gate does not depend on where it is invoked from.
func repoRootDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}

		dir = parent
	}
}
