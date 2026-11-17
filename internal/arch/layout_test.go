package arch_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

const modulePath = "github.com/prusa3d/prusa-connect"

// The groups of ADR-20260824-kkr, in the order they appear there. A package belongs to
// the first group whose prefix matches on a path-segment boundary, so
// `internal/appearance` is a domain and not the composition root.
//
// mayImport lists the groups a group may import. It is not transitive: an edge
// is legal only if the importing group names the imported group here.
var groups = []group{
	{name: "kernel", prefix: "internal/kernel", mayImport: nil},
	{name: "arch", prefix: "internal/arch", mayImport: nil},
	{name: "connector", prefix: "internal/connector", mayImport: []string{"kernel", "domain"}},
	{name: "transport", prefix: "internal/transport", mayImport: []string{"kernel", "domain"}},
	{name: "platform", prefix: "internal/platform", mayImport: []string{"kernel", "domain"}},
	{name: "extension", prefix: "internal/extension", mayImport: []string{"kernel"}},
	{
		name:   "app",
		prefix: "internal/app",
		mayImport: []string{
			"kernel", "arch", "domain", "connector", "transport", "platform", "extension", "app",
		},
	},

	// Everything else under internal/ is a bounded context. ADR-20260824-kkr rule 4:
	// a domain is a top-level sibling, not a child of internal/domain. The
	// prefix is `internal` rather than `internal/` so a package sitting
	// directly at internal/ lands in a group instead of escaping the law.
	//
	// A domain may import kernel and sdk. It may not import a sibling context,
	// so `domain` must not appear in its own mayImport: this group is the
	// catch-all, and naming itself would make most of the tree freely
	// importable from most of the tree.
	{name: "domain", prefix: "internal", mayImport: []string{"kernel"}},
}

type group struct {
	name      string
	prefix    string
	mayImport []string
}

// placement is where a package sits: its group, and the bounded context inside
// that group. Two packages in the same context are one unit and import each
// other freely; two domains are separate contexts however alike their paths
// look.
type placement struct {
	group   group
	context string
}

// frozenLawViolations are the edges ADR-20260824-kkr forbids that the tree is still
// allowed to hold, each against the reason it stays. ADR-20260824-kkr's enforcement
// section promises this list. An empty one is what makes "the gate is green"
// and "the tree obeys the ADR" the same statement.
//
// Shrinking it is roadmap. Adding to it needs a reason in the pull request.
var frozenLawViolations = map[string]string{}

// TestInternalImportsFollowTheDependencyLaw walks every edge in the real import
// graph and fails on one the table above does not permit. It is the enforcement
// clause of ADR-20260824-kkr, which review alone cannot hold.
//
// An edge counts whether it is declared directly or laundered through a
// package outside internal/: adapters/, examples/, tools/ and test/ are all
// first-party, so each is usable as a relay between two groups that may not
// import each other.
func TestInternalImportsFollowTheDependencyLaw(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	var violations []string

	seen := map[string]bool{}

	placed, judged := 0, 0

	for _, pkg := range graph.packages {
		from, ok := placementOf(pkg)
		if !ok {
			continue
		}

		placed++

		for target, via := range effectiveTargets(graph, pkg) {
			to, isInternal := placementOf(target)
			if !isInternal || to.context == from.context {
				continue
			}

			judged++

			if slices.Contains(from.group.mayImport, to.group.name) {
				continue
			}

			key := rel(pkg) + " -> " + rel(target)
			if _, frozen := frozenLawViolations[key]; frozen {
				seen[key] = true

				continue
			}

			violations = append(violations, fmt.Sprintf(
				"%s (%s) imports %s (%s)%s: %s may import %s",
				rel(pkg), from.group.name, rel(target), to.group.name, relayNote(via),
				from.group.name, allowed(from.group)))
		}
	}

	// This gate expects zero violations on every healthy run, so its green says
	// nothing about whether it read anything. Two input sets carry a floor: the
	// packages the group table placed, and the cross-context edges it then
	// judged. Every gate in this file finds its packages by cutting modulePath
	// off an import path, and a prefix that stops matching takes all of them to
	// zero and green.
	requireScanned(t, "internal packages placed in a group", placed, 30,
		"Nothing was placed, so the group table matched no package in the graph.")
	requireScanned(t, "cross-context edges judged", judged, 40)

	report(
		t,
		violations,
		"ADR-20260824-kkr groups, docs/decisions/20260824-kkr-repository-layout-and-package-boundaries.md",
	)
	reportStale(t, frozenLawViolations, seen, "frozenLawViolations")
}

// TestTheModulePathMatchesGoMod pins the const above to the module it names.
// placementOf, underPrefix, isFirstParty and rel all cut that prefix off an
// import path, so a const naming a path nothing in the tree carries makes every
// gate here skip every package and pass.
//
// Reading go.mod rather than repeating it is what removes the drift: a module
// that moves takes this const with it or fails here.
func TestTheModulePathMatchesGoMod(t *testing.T) {
	t.Parallel()

	declared, err := arch.FileValue(
		filepath.Join(repoRoot(t), "go.mod"),
		`(?m)^module\s+(\S+)`,
		"the module path",
	)
	if err != nil {
		t.Fatalf("reading the module path: %v", err)
	}

	if declared != modulePath {
		t.Errorf("go.mod declares the module %q and layout_test.go names %q.\n"+
			"Every gate in this file matches packages by that prefix, so they judge nothing\n"+
			"while the two disagree.", declared, modulePath)
	}
}

// TestKernelImportsNothingInternal is ADR-20260824-kkr rule 1, the rule the whole graph
// hangs from.
//
// It parses the source files itself rather than reusing the graph the other
// gates share, so it does not inherit their blind spots. Parsing sees every
// file the compiler could select: any build tag, any GOOS suffix, test files,
// external test packages and files under testdata alike. This gate and
// TestInternalImportsFollowTheDependencyLaw therefore fail independently.
func TestKernelImportsNothingInternal(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	kernel := filepath.Join(root, "internal", "kernel")

	var violations []string

	fset := token.NewFileSet()
	parsed := 0

	err := filepath.WalkDir(kernel, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		parsed++

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		where, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		for _, imported := range importPaths(file.Imports) {
			if !strings.HasPrefix(imported, modulePath+"/internal/") ||
				strings.HasPrefix(imported, modulePath+"/internal/kernel/") ||
				imported == modulePath+"/internal/kernel" {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s",
				filepath.ToSlash(where), rel(imported)))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/kernel: %v", err)
	}

	requireScanned(t, "Go files under internal/kernel", parsed, 10,
		"The walk reached no file, so the rule the whole graph hangs from read nothing.")

	report(t, violations, "ADR-20260824-kkr rule 1")
}

// effectiveTargets returns the internal packages an internal package depends
// on, collapsing any chain that leaves internal/ and comes back. The value is
// the relay chain, empty for a direct import.
func effectiveTargets(graph importGraph, from string) map[string][]string {
	targets := map[string][]string{}

	for _, imported := range graph.importsOf(from) {
		switch {
		case isInternal(imported):
			targets[imported] = nil
		case isRelay(imported):
			for target, via := range reachInternal(graph, imported) {
				if _, direct := targets[target]; !direct {
					targets[target] = via
				}
			}
		}
	}

	return targets
}

// reachInternal walks out from a package outside internal/ and reports the
// internal packages it reaches, each with the chain that leads there.
func reachInternal(graph importGraph, start string) map[string][]string {
	found := map[string][]string{}
	chains := map[string][]string{start: {start}}
	queue := []string{start}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, imported := range graph.productionImportsOf(current) {
			switch {
			case isInternal(imported):
				if _, ok := found[imported]; !ok {
					found[imported] = chains[current]
				}
			case isRelay(imported):
				if _, seen := chains[imported]; seen {
					continue
				}

				chains[imported] = append(append([]string{}, chains[current]...), imported)
				queue = append(queue, imported)
			}
		}
	}

	return found
}

// isRelay reports whether a package is first-party and outside internal/.
// Those are the packages an internal/ package can import and route a banned
// edge through.
func isRelay(importPath string) bool {
	return isFirstParty(importPath) && !isInternal(importPath)
}

func isInternal(importPath string) bool {
	return underPrefix(importPath, "internal")
}

// underPrefix matches on a path-segment boundary. `strings.HasPrefix` alone put
// internal/appearance in the composition root's group, where it inherited
// permission to import everything, while the same file at internal/bppearance
// was correctly refused.
func underPrefix(importPath, prefix string) bool {
	suffix, ok := strings.CutPrefix(importPath, modulePath+"/")
	if !ok {
		return false
	}

	return suffix == prefix || strings.HasPrefix(suffix, prefix+"/")
}

// placementOf reports where a package sits. Only paths under internal/ have a
// place; cmd/, sdk/ and adapters/ are governed by the other tests.
func placementOf(importPath string) (placement, bool) {
	suffix, ok := strings.CutPrefix(importPath, modulePath+"/")
	if !ok {
		return placement{}, false
	}

	for _, g := range groups {
		if suffix != g.prefix && !strings.HasPrefix(suffix, g.prefix+"/") {
			continue
		}

		return placement{group: g, context: contextOf(g, suffix)}, true
	}

	return placement{}, false
}

// contextOf names the bounded context a package belongs to. For a named group
// that is the group itself. For the domain catch-all it is the directory
// directly under internal/, because ADR-20260824-kkr rule 4 makes each of those a
// separate bounded context rather than one shared "domain".
func contextOf(g group, suffix string) string {
	if g.name != "domain" {
		return g.name
	}

	rest, ok := strings.CutPrefix(suffix, "internal/")
	if !ok {
		return "internal"
	}

	first, _, _ := strings.Cut(rest, "/")

	return "internal/" + first
}

func importPaths(specs []*ast.ImportSpec) []string {
	out := make([]string, 0, len(specs))

	for _, spec := range specs {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		out = append(out, path)
	}

	return out
}

func relayNote(via []string) string {
	if len(via) == 0 {
		return ""
	}

	return " through " + strings.Join(relAll(via), " -> ")
}

func rel(importPath string) string {
	if suffix, ok := strings.CutPrefix(importPath, modulePath+"/"); ok {
		return suffix
	}

	return importPath
}

func relAll(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, rel(p))
	}

	return out
}

func allowed(g group) string {
	if len(g.mayImport) == 0 {
		return "nothing else internal"
	}

	return strings.Join(g.mayImport, ", ")
}

func report(t *testing.T, violations []string, law string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d import edge(s) break %s:\n\t%s",
		len(violations), law, strings.Join(violations, "\n\t"))
}

// reportStale fails on an allow-list entry that no longer describes anything. A
// stale exception stops being a record and starts being precedent.
func reportStale[V any](t *testing.T, allowList map[string]V, seen map[string]bool, name string) {
	t.Helper()

	for _, key := range sortedKeys(allowList) {
		if !seen[key] {
			t.Errorf("%s lists %q, which is not in the graph any more. Remove the entry.", name, key)
		}
	}
}

// TestTheDockerfilesMatchWhetherACgoDriverIsLinked keeps two facts that drift
// apart in agreement: whether the production build still reaches a cgo-only
// driver, and whether the image is built with cgo.
//
// mattn/go-sqlite3 ships a stub under `//go:build !cgo` whose Open always
// returns an error, so `CGO_ENABLED=0 go build -tags sqlite` links cleanly and
// produces a binary that cannot open a single database. A green build is not
// evidence for M6 while that package is reachable, which is why the check is
// this way round rather than a plain ban.
//
// It fails in both directions: the recipe must build with cgo while something
// in the build still imports the driver, and must go back to 0 once the last
// importer is rewritten.
//
// Both recipes are read, including the one that builds the released
// cross-architecture binaries, and each may spell the setting either way round.
func TestTheDockerfilesMatchWhetherACgoDriverIsLinked(t *testing.T) {
	t.Parallel()

	const cgoOnlyDriver = "github.com/mattn/go-sqlite3"

	root := repoRoot(t)

	sets, err := arch.TagSets(root)
	if err != nil {
		t.Fatalf("reading the build recipes: %v", err)
	}

	release, err := arch.ReleaseImageTags(root)
	if err != nil {
		t.Fatalf("reading the release build's tags: %v", err)
	}

	recipes := []struct {
		path string
		tags string
	}{
		{path: "docker/server/Dockerfile", tags: sets["build"]},
		{path: ".github/dockerfiles/Dockerfile.build", tags: release},
	}

	for _, recipe := range recipes {
		t.Run(recipe.path, func(t *testing.T) {
			t.Parallel()

			linksCgoDriver, listErr := linksPackage(root, recipe.tags, cgoOnlyDriver)
			if listErr != nil {
				t.Fatalf("go list -deps: %v", listErr)
			}

			buildsWithCgo, cgoErr := arch.DockerfileBuildsWithCgo(filepath.Join(root, recipe.path))
			if cgoErr != nil {
				t.Fatalf("%s: %v", recipe.path, cgoErr)
			}

			switch {
			case linksCgoDriver && !buildsWithCgo:
				t.Errorf("the build in %s reaches %s with tags %q, but builds with CGO_ENABLED=0.\n"+
					"That driver's no-cgo build is a stub that fails on the first Open, so the\n"+
					"image would build green and be unable to reach a database.",
					recipe.path, cgoOnlyDriver, recipe.tags)
			case !linksCgoDriver && buildsWithCgo:
				t.Errorf("nothing in %s reaches %s any more, so it should build with\n"+
					"CGO_ENABLED=0. An earlier design decision wants a static binary; cgo is on here only to\n"+
					"serve that driver.", recipe.path, cgoOnlyDriver)
			}
		})
	}
}

// TestAdaptersCompileAgainstTheContractAlone is ADR-20260825-n55 rule 2, and it is the
// rule the other six rest on. An adapter that reaches past the contract is a
// contract gap: the published sdk/ contract was not sufficient to write the
// implementation, and a community author copying that adapter hits the same
// wall with no way over it. The failure is worded that way round for that
// reason.
//
// The rule is an allowlist of sdk/ and the standard library, not a ban list.
// Banning internal/ alone would leave adapters/ free to reach anything else in
// the application module.
//
// A test file may reach for a third-party assertion helper: adapters/ is part
// of the application module, so that costs a copier nothing, and it says
// nothing about whether the contract is sufficient. It may not reach another
// part of this repository, which is the same contract gap in a file that
// happens to end in _test.go.
// adapterEngineClients is the one reviewed exception, keyed "package import" so
// one entry exempts one package from one import. Each value is why the import is
// not a contract gap.
//
// A database adapter speaks a database engine, and an engine's wire is reached
// through that engine's client. sdk/historyapi cannot carry one: ADR-20260827-wgc rule 6
// refuses database/sql anywhere under sdk/, so there is no contract change that
// would make this import unnecessary, which is the test the rule above actually
// applies. The alternative is implementing the SQLite file format and the
// PostgreSQL wire protocol in this tree, which is not a smaller dependency.
//
// It is narrow on purpose: two imports, in the one package under adapters/db
// that opens a handle, and nothing about it widens to any other adapter.
//
//nolint:gochecknoglobals // a reviewed exception list, never written to.
var adapterEngineClients = map[string]string{
	"adapters/db/internal/engine modernc.org/sqlite": "the SQLite engine, pure Go, which " +
		"internal/authz already links into every build",
	"adapters/db/internal/engine github.com/lib/pq": "the PostgreSQL client, behind the postgres " +
		"build tag the adapters under adapters/db declare in adapters/tags.json",
}

func TestAdaptersCompileAgainstTheContractAlone(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)
	std := standardPackages(t)

	var violations []string

	adapters := 0

	for _, pkg := range graph.packages {
		if !underPrefix(pkg, "adapters") {
			continue
		}

		adapters++

		for _, imported := range graph.productionImportsOf(pkg) {
			if std[imported] || underPrefix(imported, "sdk") || underPrefix(imported, "adapters") {
				continue
			}

			if _, reviewed := adapterEngineClients[rel(pkg)+" "+imported]; reviewed {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), rel(imported)))
		}

		for _, imported := range graph.testImportsOf(pkg) {
			if !isFirstParty(imported) || underPrefix(imported, "sdk") || underPrefix(imported, "adapters") {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s from a test file", rel(pkg), rel(imported)))
		}
	}

	requireScanned(t, "packages under adapters/", adapters, 3,
		"No adapter was read, so nothing was asked whether the contract is sufficient.")

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d adapter import(s) reach past sdk/ and the standard library, which means the\n"+
		"contract they implement is not sufficient to write them. Grow the contract rather\n"+
		"than the import (ADR-20260825-n55 rule 2):\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}

// TestOnlyTheCompositionRootWiresAdapters is ADR-20260825-n55 rule 5. Removing an
// adapter has to be a change to internal/app and to nothing else, and an
// internal/ package that imports one is also the first half of a route from
// internal/ to anywhere the adapter can reach.
func TestOnlyTheCompositionRootWiresAdapters(t *testing.T) {
	t.Parallel()

	graph := packageGraph(t)

	var violations []string

	outsideTheRoot := 0

	for _, pkg := range graph.packages {
		if !isInternal(pkg) || underPrefix(pkg, "internal/app") {
			continue
		}

		outsideTheRoot++

		for _, imported := range graph.importsOf(pkg) {
			if !underPrefix(imported, "adapters") {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s", rel(pkg), rel(imported)))
		}
	}

	requireScanned(t, "internal packages outside the composition root", outsideTheRoot, 20,
		"Nothing matched internal/, so no package was asked whether it wires an adapter.")

	report(
		t,
		violations,
		"ADR-20260825-n55 rule 5, docs/decisions/20260825-n55-adapters-are-reference-implementations.md",
	)
}

// skipWholeTree names the directories with nothing to walk.
func skipWholeTree(name string) bool {
	return name == ".git" || name == "node_modules"
}

// underTestdata reports whether a path sits under a testdata directory. It
// splits on the separator because a substring test needing a slash on the left
// misses a testdata directory at the repository root.
func underTestdata(where string) bool {
	return slices.Contains(strings.Split(path2Dir(where), "/"), "testdata")
}

func path2Dir(where string) string {
	if i := strings.LastIndex(where, "/"); i >= 0 {
		return where[:i]
	}

	return "."
}

// sdkThirdPartyExceptions is the allow-list for TestTheSdkImportsOnlyTheStandardLibrary.
// ADR-20260824-gfr rule 2 exists so an extension author inherits the standard library
// and nothing else. sdk/go.mod records one violation in prose; prose does not
// stop a second one, and a second one is how "stdlib only" quietly becomes
// untrue. It already had: testify has been a direct requirement of sdk/go.mod,
// reached through test files that the gate was not reading.
//
// It is empty, and staying empty is the point. The one entry it held was
// sdk/routesapi exposing gin through RouteDeps.Router; that contract is deleted
// and sdk/go.mod now requires nothing at all. A new entry here is a new
// violation, not a note about an old one.
var sdkThirdPartyExceptions = map[string]string{}

// sdkRefusedStdlibImports names standard-library packages no package under sdk/
// may import, even though ADR-20260824-gfr rule 2 hands an extension author the whole
// standard library. Each value is the reason the door is shut.
//
// The refusals are the ones a contract can leak. A package the sdk imports is a
// type an sdk signature may name, so refusing the import is what makes the
// prohibition mechanical rather than a thing a reviewer has to notice.
var sdkRefusedStdlibImports = map[string]string{
	"database/sql": "ADR-20260827-wgc rule 6. A contract that exposes *sql.DB makes every extension a " +
		"client of the core's schema, reaches authz_tuple, writes tables no migration ledger " +
		"records and pins its author to one engine. An extension reaches persistence through " +
		"sdk/stateapi instead",
}

// sdkRefusedImportExceptions is the allow-list for sdkRefusedStdlibImports,
// keyed "package import" so one entry exempts one package from one refused
// import. sdkThirdPartyExceptions is keyed by package alone and cannot be
// reused here: an entry there would exempt its package from every refused
// import as well, which is how a reviewed exception widens into a hole.
//
// An entry needs the same review the third-party table's entries get, and
// holds one package to one import.
var sdkRefusedImportExceptions = map[string]string{
	"sdk/moduledb database/sql": "ADR-20261021-bqc rule 20. The handle is the module's own database, which " +
		"the operator granted it, and never the core's. Rule 6 of ADR-20260827-wgc guards the core's schema, " +
		"and no contract passes a handle on it",
}

// TestTheSdkImportsOnlyTheStandardLibrary reads the sdk module's own package
// list, because the root module's graph says nothing about what the sdk pulls
// in on its own.
//
// Test imports count. ADR-20260824-gfr rule 2 is not qualified, and the cost it names -
// the module graph an implementer inherits - is paid by a test dependency
// exactly as it is by a production one, because both land in sdk/go.mod.
func TestTheSdkImportsOnlyTheStandardLibrary(t *testing.T) {
	t.Parallel()

	graph := sdkPackageGraph(t)
	std := standardPackages(t)

	offenders := map[string]map[string]bool{}
	refused := map[string]map[string]bool{}
	seen := map[string]bool{}
	seenRefused := map[string]bool{}

	for _, pkg := range graph.packages {
		where := rel(pkg)

		for _, imported := range graph.importsOf(pkg) {
			// Checked before the standard-library branch, because every refused
			// import is a standard-library one and would otherwise be waved
			// through as exactly the thing ADR-20260824-gfr rule 2 allows.
			if _, shut := sdkRefusedStdlibImports[imported]; shut {
				exception := where + " " + imported

				if _, ok := sdkRefusedImportExceptions[exception]; ok {
					seenRefused[exception] = true

					continue
				}

				if refused[where] == nil {
					refused[where] = map[string]bool{}
				}

				refused[where][imported] = true

				continue
			}

			if std[imported] || isFirstParty(imported) {
				continue
			}

			if _, ok := sdkThirdPartyExceptions[where]; ok {
				seen[where] = true

				continue
			}

			if offenders[where] == nil {
				offenders[where] = map[string]bool{}
			}

			offenders[where][imported] = true
		}
	}

	for _, where := range sortedKeys(refused) {
		for _, imported := range sortedKeys(refused[where]) {
			t.Errorf("%s imports %s, which sdk/ refuses: %s.",
				where, imported, sdkRefusedStdlibImports[imported])
		}
	}

	for _, where := range sortedKeys(offenders) {
		t.Errorf("%s imports %s. ADR-20260824-gfr gives an extension author the standard library and\n"+
			"nothing else, so a third-party type in an sdk signature pins every implementor to\n"+
			"our choice of it, and a third-party test dependency pins their module graph.\n"+
			"Express the contract, and its tests, in stdlib terms.",
			where, strings.Join(sortedKeys(offenders[where]), ", "))
	}

	reportStale(t, sdkThirdPartyExceptions, seen, "sdkThirdPartyExceptions")
	reportStale(t, sdkRefusedImportExceptions, seenRefused, "sdkRefusedImportExceptions")
}
