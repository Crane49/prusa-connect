package arch_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// The cgo gate is a text reader, and the text it reads is written by hand in two
// files with different house styles. These are the shapes it has to get right,
// including the two a substring test cannot tell apart.
func TestDockerfileBuildsWithCgo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		recipe  string
		want    bool
		wantErr bool
	}{
		{
			name:   "inline assignment, the shape docker/server/Dockerfile uses",
			recipe: "FROM golang\nRUN CGO_ENABLED=1 go build -o /out ./cmd/x\n",
			want:   true,
		},
		{
			name:   "ENV plus a bare go build, the shape the release recipe uses",
			recipe: "FROM golang\nENV CGO_ENABLED=1\nRUN go build -v -tags \"full\" -o /out .\n",
			want:   true,
		},
		{
			name:   "cgo off",
			recipe: "FROM golang\nRUN CGO_ENABLED=0 go build -o /out ./cmd/x\n",
			want:   false,
		},
		{
			name: "the literal parked in a comment while the real build is off",
			recipe: "FROM golang\n" +
				"# Historically this was CGO_ENABLED=1 go build; kept for reference.\n" +
				"RUN CGO_ENABLED=0 go build -o /out ./cmd/x\n",
			want: false,
		},
		{
			name: "a comment inside a line continuation is still a comment",
			recipe: "FROM golang\nENV CGO_ENABLED=0\nRUN set -eu; \\\n" +
				"# CGO_ENABLED=1 go build\n" +
				"    go build -o /out ./cmd/x\n",
			want: false,
		},
		{
			name:   "an ENV before the build carries across a continuation",
			recipe: "FROM golang\nENV CGO_ENABLED=1\nRUN echo hi && \\\n    go build -o /out .\n",
			want:   true,
		},
		{
			name:   "an inline assignment overrides the ENV",
			recipe: "FROM golang\nENV CGO_ENABLED=1\nRUN CGO_ENABLED=0 go build -o /out .\n",
			want:   false,
		},
		{
			name:    "nothing says what CGO_ENABLED is",
			recipe:  "FROM golang\nRUN go build -o /out ./cmd/x\n",
			wantErr: true,
		},
		{
			name:    "no go build at all",
			recipe:  "FROM golang\nRUN echo nothing to build\n",
			wantErr: true,
		},
		{
			// Docker resets ENV at an image boundary, so the build in stage b
			// has nothing in scope. Carrying the earlier stage's value across
			// reports cgo on for a build that never mentions it.
			name: "an ENV does not cross a FROM onto a fresh image",
			recipe: "FROM golang AS a\nENV CGO_ENABLED=1\nRUN echo build a\n" +
				"FROM golang AS b\nRUN go build -o /out ./cmd/x\n",
			wantErr: true,
		},
		{
			name: "a stage built on another stage keeps what that stage set",
			recipe: "FROM golang AS a\nENV CGO_ENABLED=1\nRUN echo build a\n" +
				"FROM a AS b\nRUN go build -o /out ./cmd/x\n",
			want: true,
		},
		{
			name: "one stage building with cgo counts even when another turns it off",
			recipe: "FROM golang AS a\nENV CGO_ENABLED=1\nRUN go build -o /a ./cmd/x\n" +
				"FROM golang AS b\nENV CGO_ENABLED=0\nRUN go build -o /b ./cmd/x\n",
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "Dockerfile")
			if err := os.WriteFile(path, []byte(test.recipe), 0o600); err != nil {
				t.Fatalf("writing the recipe: %v", err)
			}

			got, err := arch.DockerfileBuildsWithCgo(path)

			if gotErr := err != nil; gotErr != test.wantErr {
				t.Fatalf("DockerfileBuildsWithCgo() error = %v, want error %v", err, test.wantErr)
			}

			if !test.wantErr && got != test.want {
				t.Errorf("DockerfileBuildsWithCgo() = %v, want %v", got, test.want)
			}
		})
	}
}

// A tag list this gate cannot find is a configuration it stops covering. It has
// to say so rather than fall back to a default, because a silent fallback leaves
// the untagged graph as the only graph anybody checks.
func TestFileValueFailsWhenTheRecipeMoved(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(path, []byte("FEATURE_TAGS=\"sqlite mqtt\"\n"), 0o600); err != nil {
		t.Fatalf("writing the recipe: %v", err)
	}

	if _, err := arch.FileValue(path, `ALL_TAGS="([^"]+)"`, "ALL_TAGS"); err == nil {
		t.Fatal("FileValue() error = nil, want a failure naming the pattern that stopped matching")
	}

	got, err := arch.FileValue(path, `FEATURE_TAGS="([^"]+)"`, "FEATURE_TAGS")
	if err != nil {
		t.Fatalf("FileValue() error = %v", err)
	}

	if got != "sqlite mqtt" {
		t.Errorf("FileValue() = %q, want %q", got, "sqlite mqtt")
	}
}

// A commented-out build line matches every pattern the real one does, and
// FileValue takes the first match. Parking one above the real line narrows the
// release and build-ci sets to whatever the comment says, with err = nil, so the
// promise that a pattern which stops matching fails loudly never engages: the
// pattern still matches. Every edge behind a tag outside the parked list then
// goes unchecked, green. It is the cgo gate's defect one file along.
func TestFileValueReadsPastACommentedOutRecipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name    string
		file    string
		content string
		pattern string
		want    string
	}{
		{
			name: "a parked release build line",
			file: "Dockerfile.build",
			content: "FROM golang\n" +
				"# historic: RUN go build -v -tags \"full sqlite\" -o /out .\n" +
				"RUN go build -v -tags \"full mqtt observability sqlite fractal\" -o /out .\n",
			pattern: `go build[^\n]*-tags "([^"]+)"`,
			want:    "full mqtt observability sqlite fractal",
		},
		{
			name: "a parked ALL_TAGS example",
			file: "Makefile",
			content: "# example: ALL_TAGS=\"sqlite\"\n" +
				"\t@ALL_TAGS=\"dragonflycache mqtt mqttbroker sqlite\"; \\\n",
			pattern: `ALL_TAGS="([^"]+)"`,
			want:    "dragonflycache mqtt mqttbroker sqlite",
		},
		{
			name: "an indented comment is still a comment",
			file: "Indented",
			content: "  # ALL_TAGS=\"sqlite\"\n" +
				"ALL_TAGS=\"postgres\"\n",
			pattern: `ALL_TAGS="([^"]+)"`,
			want:    "postgres",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(dir, test.file)
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("writing the recipe: %v", err)
			}

			got, err := arch.FileValue(path, test.pattern, "the tag list")
			if err != nil {
				t.Fatalf("FileValue() error = %v", err)
			}

			if got != test.want {
				t.Errorf("FileValue() = %q, want %q: the comment decided what this gate covers", got, test.want)
			}
		})
	}
}

// A recipe whose only match is inside a comment is a recipe the gate cannot
// read, not a recipe that says what the comment says.
func TestFileValueFailsWhenOnlyACommentMatches(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(path, []byte("# ALL_TAGS=\"sqlite\"\n"), 0o600); err != nil {
		t.Fatalf("writing the recipe: %v", err)
	}

	if _, err := arch.FileValue(path, `ALL_TAGS="([^"]+)"`, "ALL_TAGS"); err == nil {
		t.Fatal("FileValue() error = nil, want a failure: the only match is commented out")
	}
}

func TestExpandMakeVars(t *testing.T) {
	t.Parallel()

	got := arch.ExpandMakeVars("dragonflycache mqtt $(EXTRA_BUILD_TAGS) sqlite")
	if want := "dragonflycache mqtt sqlite"; got != want {
		t.Errorf("ExpandMakeVars() = %q, want %q", got, want)
	}
}

// isFirstParty decides whether an import is ours. Without the trailing slash a
// different repository whose path merely starts with ours reads as first-party,
// and so escapes the sdk's stdlib-only rule entirely.
func TestIsFirstParty(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		modulePath:                                        true,
		modulePath + "/internal/kernel":                   true,
		modulePath + "/sdk/fault":                         true,
		"github.com/prusa3d/prusa-connect-extras/toolkit": false,
		"github.com/prusa3d/prusa-connect2":               false,
		"github.com/stretchr/testify/require":             false,
		"strings":                                         false,
	}

	for importPath, want := range tests {
		if got := isFirstParty(importPath); got != want {
			t.Errorf("isFirstParty(%q) = %v, want %v", importPath, got, want)
		}
	}
}

// The standard library is asked for by name rather than guessed at. A heuristic
// reading any first path segment without a dot as stdlib exempts a module path
// shaped that way from the sdk's stdlib-only rule, silently.
func TestStandardPackagesIsTheToolchainsList(t *testing.T) {
	t.Parallel()

	std := standardPackages(t)

	for _, pkg := range []string{"strings", "net/http", "encoding/json"} {
		if !std[pkg] {
			t.Errorf("standardPackages() is missing %q", pkg)
		}
	}

	for _, pkg := range []string{"github.com/gin-gonic/gin", "gopkg.in/yaml.v3", "nodots/pkg"} {
		if std[pkg] {
			t.Errorf("standardPackages() wrongly contains %q", pkg)
		}
	}
}

// The graph has to be read under more than one configuration, and the set has to
// come from the build recipes rather than from a list somebody typed here.
func TestBuildConfigsCoverTheShippedAndDeveloperBuilds(t *testing.T) {
	t.Parallel()

	configs, err := buildConfigs(repoRoot(t))
	if err != nil {
		t.Fatalf("buildConfigs() error = %v", err)
	}

	platforms := map[string]bool{}
	tagged := 0
	untagged := 0

	for _, config := range configs {
		platforms[config.goos] = true

		if config.tags == "" {
			untagged++
		} else {
			tagged++
		}
	}

	if untagged == 0 {
		t.Error("no untagged configuration, so plain code would go unchecked")
	}

	if tagged == 0 {
		t.Error("no tagged configuration, so a //go:build sqlite file would go unchecked")
	}

	for _, goos := range append([]string{shippedPlatform}, developerPlatforms...) {
		if !platforms[goos] {
			t.Errorf("no configuration for GOOS=%s, so a _%s.go file would go unchecked", goos, goos)
		}
	}

	// The tag set the image is built with has to be in there, whatever it is.
	// Naming the tags would be the drift this gate exists to stop, so the
	// assertion is that the detector's answer is covered, not what it says.
	sets, err := arch.TagSets(repoRoot(t))
	if err != nil {
		t.Fatalf("TagSets() error = %v", err)
	}

	for name, tags := range sets {
		if !hasConfig(configs, tags) {
			t.Errorf("the %q tag set (%q) is not in any configuration", name, tags)
		}
	}
}

func hasConfig(configs []buildConfig, tags string) bool {
	for _, config := range configs {
		if config.tags == tags {
			return true
		}
	}

	return false
}

// A gate that reads nothing passes everything. Each loader says so instead.
func TestGraphIsNotEmpty(t *testing.T) {
	t.Parallel()

	for name, graph := range map[string]importGraph{
		"root": packageGraph(t),
		"sdk":  sdkPackageGraph(t),
	} {
		if len(graph.packages) == 0 {
			t.Errorf("the %s graph is empty", name)
		}

		if len(graph.configs) < 2 {
			t.Errorf("the %s graph was read under %d configuration(s)", name, len(graph.configs))
		}
	}
}

func TestUnderTestdataFindsARepositoryRootTestdata(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"testdata/x.go":                   true,
		"internal/p/testdata/x.go":        true,
		"internal/p/testdata/sub/x.go":    true,
		"internal/testdatabase/x.go":      false,
		"internal/p/x.go":                 false,
		"internal/p/testdata_helper/x.go": false,
	}

	for where, want := range tests {
		if got := underTestdata(where); got != want {
			t.Errorf("underTestdata(%q) = %v, want %v", where, got, want)
		}
	}
}

func TestErrorsAreNotSwallowed(t *testing.T) {
	t.Parallel()

	if got := arch.StderrOf(errors.New("plain")); got != "" {
		t.Errorf("arch.StderrOf() on a plain error = %q, want empty", got)
	}
}

func TestPlacementOfAnchorsOnSegments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		group   string
		context string
	}{
		modulePath + "/internal/app":              {group: "app", context: "app"},
		modulePath + "/internal/app/config":       {group: "app", context: "app"},
		modulePath + "/internal/appearance":       {group: "domain", context: "internal/appearance"},
		modulePath + "/internal/kernel":           {group: "kernel", context: "kernel"},
		modulePath + "/internal/kernelish":        {group: "domain", context: "internal/kernelish"},
		modulePath + "/internal":                  {group: "domain", context: "internal"},
		modulePath + "/internal/printer/profile":  {group: "domain", context: "internal/printer"},
		modulePath + "/internal/authz":            {group: "domain", context: "internal/authz"},
		modulePath + "/internal/transport/stream": {group: "transport", context: "transport"},
	}

	for importPath, want := range tests {
		got, ok := placementOf(importPath)
		if !ok {
			t.Errorf("placementOf(%q) found no group", importPath)

			continue
		}

		if got.group.name != want.group || got.context != want.context {
			t.Errorf("placementOf(%q) = %s/%s, want %s/%s",
				importPath, got.group.name, got.context, want.group, want.context)
		}
	}

	for _, importPath := range []string{
		modulePath + "/adapters/events/webhook",
		modulePath + "/sdk/fault",
		"github.com/stretchr/testify/require",
	} {
		if _, ok := placementOf(importPath); ok {
			t.Errorf("placementOf(%q) put a package outside internal/ in a group", importPath)
		}
	}
}

// The group table is data read next to ADR-20260824-kkr, and rule 10 says a change to
// that table needs the ADR to carry it. Nothing but this test compares them, so
// without it the table grows groups the ADR never lists, and one of them names a
// directory that does not exist.
//
// Both columns are compared, not just the names. The names are the label; the
// "may import" cell is the rule, and the cell is where the damage is: a domain
// granted permission to import a domain where the ADR grants kernel and sdk. A
// gate that reads only the first column watches the label and leaves the rule
// unguarded, so the table could hand a bounded context the whole tree with the
// ADR untouched and this test green.
func TestTheGroupTableMatchesTheADR(t *testing.T) {
	t.Parallel()

	adr, err := os.ReadFile(filepath.Join(repoRoot(t),
		"docs", "decisions", "20260824-kkr-repository-layout-and-package-boundaries.md"))
	if err != nil {
		t.Fatalf("reading ADR-20260824-kkr: %v", err)
	}

	declared := adrGroupRows(t, string(adr))
	inTable := map[string]bool{}

	for _, g := range groups {
		inTable[g.name] = true

		mayImport, ok := declared[g.name]
		if !ok {
			t.Errorf("the group table has %q, which ADR-20260824-kkr does not list. Rule 10: a change to\n"+
				"that table needs the ADR to carry it.", g.name)

			continue
		}

		want := adrMayImport(mayImport, sortedKeys(declared))
		if got := slices.Sorted(slices.Values(g.mayImport)); !slices.Equal(got, want) {
			t.Errorf("the table lets %q import %s. ADR-20260824-kkr says %s (%q). Rule 10: a change to\n"+
				"that table needs the ADR to carry it, and this column is the rule.",
				g.name, namesOrNothing(got), namesOrNothing(want), mayImport)
		}
	}

	for name := range declared {
		if !inTable[name] {
			t.Errorf("ADR-20260824-kkr lists a %q group that the table does not enforce, so that row of\n"+
				"the ADR is not a rule anything checks.", name)
		}
	}
}

// adrGroupRows reads the ADR's group table: the group name, and the "may import"
// cell verbatim.
func adrGroupRows(t *testing.T, adr string) map[string]string {
	t.Helper()

	rows := map[string]string{}
	for _, row := range adrGroupRow.FindAllStringSubmatch(adr, -1) {
		rows[row[1]] = strings.TrimSpace(row[2])
	}

	requireScanned(t, "group rows in ADR-20260824-kkr", len(rows), 1,
		"adrGroupRow is the pattern, and the table's shape is what it describes.")

	return rows
}

// adrGroupRow matches one row of the ADR's group table: `<name>/` in the first
// cell, anything in the second, the permission list in the third.
var adrGroupRow = regexp.MustCompile("(?m)^[|][ \t]*`<?([a-z]+)>?/`[ \t]*[|][^|]*[|]([^|]*)[|]")

// adrMayImport turns one "may import" cell into the group names the table has to
// name. Two cells are phrases rather than lists, and both are load-bearing:
// "nothing internal" is the kernel rule and "everything" is rule 3.
//
// `sdk` is dropped. It is a separate module rather than a group under internal/,
// it is governed by ADR-20260824-gfr, and the table's mayImport only ever decides edges
// between groups.
func adrMayImport(cell string, allGroups []string) []string {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "nothing internal":
		return nil
	case "everything":
		return slices.Sorted(slices.Values(allGroups))
	}

	var want []string

	for name := range strings.SplitSeq(cell, ",") {
		name = strings.ToLower(strings.Trim(strings.TrimSpace(name), "`"))
		if name == "" || name == "sdk" {
			continue
		}

		want = append(want, name)
	}

	slices.Sort(want)

	return slices.Compact(want)
}

func namesOrNothing(names []string) string {
	if len(names) == 0 {
		return "nothing internal"
	}

	return strings.Join(names, ", ")
}
