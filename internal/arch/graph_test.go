package arch_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// A gate that reads one configuration polices one configuration. `go list` with
// no tags is the one configuration this project never ships, so a file behind
// `//go:build sqlite` is invisible to such a gate while compiling into the
// image. The graph is therefore read once per configuration the project builds
// under and unioned: an edge counts if any shipped or developer build sees it.
type buildConfig struct {
	name string
	goos string
	tags string
}

// linux is the only GOOS released. windows and darwin are developer platforms,
// covered because CI is ubuntu everywhere: nothing else would ever compile a
// `_windows.go` file, let alone look at its imports.
const shippedPlatform = "linux"

var developerPlatforms = []string{"windows", "darwin"}

// buildConfigs pairs every tag set the build recipes use with every platform.
// The shipped platform gets all of them; a developer platform gets the two ends
// of the tag space, which is what decides whether a file is compiled at all.
func buildConfigs(root string) ([]buildConfig, error) {
	sets, err := arch.TagSets(root)
	if err != nil {
		return nil, err
	}

	var configs []buildConfig

	for _, name := range sortedKeys(sets) {
		configs = append(configs, buildConfig{
			name: name + "/" + shippedPlatform,
			goos: shippedPlatform,
			tags: sets[name],
		})
	}

	for _, goos := range developerPlatforms {
		for _, name := range []string{"untagged", "build-ci"} {
			configs = append(configs, buildConfig{
				name: name + "/" + goos,
				goos: goos,
				tags: sets[name],
			})
		}
	}

	return configs, nil
}

// importGraph is the union of the import edges every configuration reports.
//
// Production and test edges are kept apart. Both count as an edge the package
// itself declares, which is why the gates read the union. Only production
// edges continue a chain: a test file of some package in the middle of a route
// is not something the package at the start of it links against, and counting
// one would report a dependency that does not exist.
type importGraph struct {
	production map[string]map[string]bool
	tests      map[string]map[string]bool
	configs    []buildConfig
	packages   []string
}

// importsOf is every edge a package declares, production and test alike.
func (g importGraph) importsOf(pkg string) []string {
	out := map[string]bool{}
	for imported := range g.production[pkg] {
		out[imported] = true
	}

	for imported := range g.tests[pkg] {
		out[imported] = true
	}

	return sortedKeys(out)
}

// productionImportsOf is what a package links against.
func (g importGraph) productionImportsOf(pkg string) []string { return sortedKeys(g.production[pkg]) }

// testImportsOf is what only its tests reach for.
func (g importGraph) testImportsOf(pkg string) []string { return sortedKeys(g.tests[pkg]) }

// listed is what this gate reads out of `go list -json`.
//
// All three import lists are folded together. An illegal edge is illegal
// whichever file declares it, and `package foo_test` lands in XTestImports,
// which is the package clause `layout_test.go` itself uses: reading only
// Imports and TestImports makes the convention this repository writes tests in
// the one place a banned import can hide.
type listed struct {
	ImportPath   string   `json:"ImportPath"`
	Imports      []string `json:"Imports"`
	TestImports  []string `json:"TestImports"`
	XTestImports []string `json:"XTestImports"`
}

func (l listed) testOnlyImports() []string {
	out := make([]string, 0, len(l.TestImports)+len(l.XTestImports))
	out = append(out, l.TestImports...)
	out = append(out, l.XTestImports...)

	return out
}

type graphCache struct {
	once  sync.Once
	value importGraph
	err   error
}

var (
	rootGraph graphCache
	sdkGraph  graphCache
)

// packageGraph is the root module's graph, read once for the whole test binary.
// Four gates share it; reading it per gate multiplied the cost of covering more
// than one configuration by four.
func packageGraph(t *testing.T) importGraph {
	t.Helper()

	return rootGraph.load(t, ".")
}

// sdkPackageGraph is the sdk module's own graph. The root module's graph says
// nothing about what the sdk pulls in on its own account.
func sdkPackageGraph(t *testing.T) importGraph {
	t.Helper()

	return sdkGraph.load(t, "sdk")
}

func (c *graphCache) load(t *testing.T, subdir string) importGraph {
	t.Helper()

	root := repoRoot(t)

	c.once.Do(func() {
		configs, err := buildConfigs(root)
		if err != nil {
			c.err = err

			return
		}

		c.value, c.err = loadGraph(filepath.Join(root, subdir), configs)
	})

	if c.err != nil {
		t.Fatalf("reading the import graph: %v", c.err)
	}

	return c.value
}

type configResult struct {
	config buildConfig
	pkgs   []listed
	err    error
}

// loadGraph runs one `go list` per configuration, concurrently: they are
// independent processes sharing a build cache, so the wall clock is the slowest
// of them rather than their sum.
func loadGraph(dir string, configs []buildConfig) (importGraph, error) {
	results := make([]configResult, len(configs))

	var wg sync.WaitGroup

	for i, config := range configs {
		wg.Go(func() {
			pkgs, err := listPackages(dir, config)
			results[i] = configResult{config: config, pkgs: pkgs, err: err}
		})
	}

	wg.Wait()

	graph := importGraph{
		production: map[string]map[string]bool{},
		tests:      map[string]map[string]bool{},
		configs:    configs,
	}

	for _, result := range results {
		if result.err != nil {
			return importGraph{}, result.err
		}

		for _, pkg := range result.pkgs {
			graph.add(graph.production, pkg.ImportPath, pkg.Imports)
			graph.add(graph.tests, pkg.ImportPath, pkg.testOnlyImports())
		}
	}

	if len(graph.production) == 0 {
		return importGraph{}, fmt.Errorf(
			"go list returned no packages for any of %d configurations, which means this gate is "+
				"reading nothing", len(configs))
	}

	graph.packages = sortedKeys(graph.production)

	return graph, nil
}

func (g importGraph) add(into map[string]map[string]bool, pkg string, imports []string) {
	if into[pkg] == nil {
		into[pkg] = map[string]bool{}
	}

	for _, imported := range imports {
		into[pkg][imported] = true
	}
}

func listPackages(dir string, config buildConfig) ([]listed, error) {
	args := []string{"list", "-json"}
	if config.tags != "" {
		args = append(args, "-tags", config.tags)
	}

	args = append(args, "./...")

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS="+config.goos)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list for %s in %s: %w%s", config.name, filepath.Base(dir), err, arch.StderrOf(err))
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))

	var pkgs []listed

	for {
		var pkg listed

		if decErr := dec.Decode(&pkg); decErr != nil {
			if errors.Is(decErr, io.EOF) {
				break
			}

			return nil, fmt.Errorf("decoding go list output for %s: %w", config.name, decErr)
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs, nil
}

// linksPackage reports whether a build of the server under these tags reaches
// a package. It asks the linker's own view rather than reading imports, so an
// indirect dependency counts.
func linksPackage(root, tags, want string) (bool, error) {
	args := []string{"list", "-deps"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}

	args = append(args, "./cmd/prusa-connect")

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+shippedPlatform, "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("go list -deps -tags %q: %w%s", tags, err, arch.StderrOf(err))
	}

	return slices.Contains(strings.Fields(string(out)), want), nil
}

// standardPackages is the standard library as the toolchain lists it. The
// heuristic "a stdlib path has no dot in its first segment" is wrong for any
// import path whose first segment happens to have no dot.
var (
	stdOnce sync.Once
	stdSet  map[string]bool
	errStd  error
)

func standardPackages(t *testing.T) map[string]bool {
	t.Helper()

	stdOnce.Do(func() {
		out, err := exec.Command("go", "list", "std").Output()
		if err != nil {
			errStd = fmt.Errorf("go list std: %w%s", err, arch.StderrOf(err))

			return
		}

		stdSet = map[string]bool{}
		for line := range strings.FieldsSeq(string(out)) {
			stdSet[line] = true
		}

		if len(stdSet) == 0 {
			errStd = errors.New("go list std returned nothing")
		}
	})

	if errStd != nil {
		t.Fatalf("listing the standard library: %v", errStd)
	}

	return stdSet
}

// isFirstParty reports whether an import path is this repository's own. The
// trailing slash is load-bearing: without it every module whose path merely
// starts with ours, such as prusa-connect-extras, reads as first-party.
func isFirstParty(importPath string) bool {
	return importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")
}

func repoRoot(t *testing.T) string {
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

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}
