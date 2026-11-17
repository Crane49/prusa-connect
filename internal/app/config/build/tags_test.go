package build_test

import (
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/build"
)

// shippedCatalogue is the airlock the build recipes use, not a fixture. A gate
// that maps its own types proves nothing about what this project compiles.
func shippedCatalogue(t *testing.T) build.Catalogue {
	t.Helper()

	catalogue, err := build.LoadCatalogue(repoPath("adapters", "tags.json"))
	require.NoError(t, err)

	return catalogue
}

func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", ".."}, parts...)...)
}

// TestADeclaredAdapterReachesTheBuild is the reconciliation the old backlog asks for: an
// adapter an operator declares has to influence the build, which it can only do
// through the resources field Detect reads.
func TestADeclaredAdapterReachesTheBuild(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: serial
`), build.VariantFull, nil, fixtureCatalogue(t))
	require.NoError(t, err)
	require.Equal(t, []string{"serialdriver"}, tags)
}

// TestAServiceInstanceContributesNoTag is the point of the runtime key: an
// implementation reached as a child process is not in this binary, so nothing
// has to be compiled in for it.
func TestAServiceInstanceContributesNoTag(t *testing.T) {
	t.Parallel()

	catalogue := fixtureCatalogue(t)

	tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: serial
      runtime: service
      service:
        path: plugins/serial
        checksum: sha256:`+strings.Repeat("a", 64)+`
        contract_major: 1
`), build.VariantFull, nil, catalogue)
	require.NoError(t, err)
	require.Empty(t, tags)
}

// TestADisabledInstanceContributesNoTag keeps the switch an operator reads as
// off from linking code in.
func TestADisabledInstanceContributesNoTag(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: serial
      enabled: false
`), build.VariantFull, nil, fixtureCatalogue(t))
	require.NoError(t, err)
	require.Empty(t, tags)
}

// TestAQualifiedAndABareNameResolveToOneTag covers the two ways an operator
// writes a type the registry answers to.
func TestAQualifiedAndABareNameResolveToOneTag(t *testing.T) {
	t.Parallel()

	catalogue := fixtureCatalogue(t)

	for _, written := range []string{"serial", "core/serial"} {
		tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: `+written+`
`), build.VariantFull, nil, catalogue)
		require.NoError(t, err)
		require.Equal(t, []string{"serialdriver"}, tags, "type written as %q", written)
	}
}

// TestATypeThisTreeDoesNotCarryContributesNoTag is the hazard the airlock
// exists for: it only carries tags for adapters that exist here, so a
// third-party name cannot reach the compiler.
func TestATypeThisTreeDoesNotCarryContributesNoTag(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: acme/bambu
`), build.VariantFull, nil, fixtureCatalogue(t))
	require.NoError(t, err)
	require.Empty(t, tags)
}

// TestOneBadInstanceStillYieldsTheOthersTags is the per-instance rule carried
// into the build command. A build that refuses to name its tags because one
// block of eight is wrong leaves an operator with no binary at all.
func TestOneBadInstanceStillYieldsTheOthersTags(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", []byte(`
resources:
  driver:
    press:
      type: serial
    broken:
      type: serial
      runtime: nonesuch
`), build.VariantFull, nil, fixtureCatalogue(t))
	require.NoError(t, err)
	require.Equal(t, []string{"serialdriver"}, tags)
}

// TestTheShippedConfigurationYieldsTheShippedTags is the case every gate in
// internal/arch rests on: what `make build` computes from CONFIG_FILE.
//
// No tag, because the shipped file names only the blocks this build reads and
// the one condition it raises, persistence.sqlite, implies none. The
// observability tag comes from a key under `core:`, and the shipped file
// leaves it off.
func TestTheShippedConfigurationYieldsTheShippedTags(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(repoPath("config", "config.yaml"))
	require.NoError(t, err)

	catalogue := shippedCatalogue(t)

	full, err := build.Detect("config/config.yaml", content, build.VariantFull, nil, catalogue)
	require.NoError(t, err)
	require.Equal(t, []string{"diagnostics"}, full)

	minimal, err := build.Detect("config/config.yaml", content, build.VariantMinimal, nil, catalogue)
	require.NoError(t, err)
	require.Empty(t, minimal)
}

// TestEveryConditionTheDetectorEvaluatesIsInTheAirlock is the gate that keeps
// the two halves of the airlock honest. A condition the detector raises and the
// file does not answer is a tag that silently stops being passed.
func TestEveryConditionTheDetectorEvaluatesIsInTheAirlock(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)

	// Every condition true at once, so each one has to be answered. The two
	// engine conditions cannot both be true any more, because one key selects
	// the engine, so the other engine is a case of its own below.
	everything := []byte(`
core:
  observability:
    enabled: true
persistence:
  type: postgres
`)

	tags, err := build.Detect("config.yaml", everything, build.VariantFull, nil, catalogue)
	require.NoError(t, err)
	require.Equal(t,
		[]string{"diagnostics", "observability", "postgres"},
		tags)

	// Minimal drops every optional feature, so nothing the airlock marks as
	// not surviving it is passed. What this case proves is that a condition the
	// detector raises is answered rather than refused.
	minimal, err := build.Detect("config.yaml", everything, build.VariantMinimal, nil, catalogue)
	require.NoError(t, err)
	require.Empty(t, minimal)

	// The airlock answers persistence.sqlite with no tag, which is a different
	// answer from a condition it does not carry at all: that one fails loudly,
	// two cases below.
	onSQLite, err := build.Detect("config.yaml", []byte("persistence:\n  type: sqlite\n"),
		build.VariantFull, nil, catalogue)
	require.NoError(t, err)
	require.Equal(t, []string{"diagnostics"}, onSQLite)
}

// TestOneSelectorDecidesTheEngine is the fold. The graph, the records and the
// queue share one database, so `queue.type` decides nothing and a file that
// still carries it cannot put the queue on a second engine (note 42,
// recommendation 3).
func TestOneSelectorDecidesTheEngine(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", []byte(`
persistence:
  type: postgres
queue:
  type: sqlite
`), build.VariantFull, nil, shippedCatalogue(t))
	require.NoError(t, err)
	require.Equal(t, []string{"diagnostics", "postgres"}, tags)
}

// TestAMissingConditionFailsLoudly is why the conditions live in the airlock
// rather than in string literals here: an entry deleted from the file is a
// failure and not a tag that quietly stops being passed.
func TestAMissingConditionFailsLoudly(t *testing.T) {
	t.Parallel()

	catalogue, err := build.ParseCatalogue("fixture", []byte(`{
	  "version": 1,
	  "resources": [],
	  "conditions": [
	    {"condition": "persistence.postgres", "tag": "postgres", "minimal": false},
	    {"condition": "core.diagnostics.enabled", "tag": "", "minimal": false}
	  ]
	}`))
	require.NoError(t, err)

	_, err = build.Detect("config.yaml", []byte("core:\n  observability:\n    enabled: true\n"),
		build.VariantFull, nil, catalogue)
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "core.observability.enabled")
}

// TestTheOptionalPartsAreSwitchedUnderCore is the owner's ruling on an earlier
// study: the key that adds the tag is the key the server reads, so a build and a
// run cannot disagree about which optional part is in the binary.
func TestTheOptionalPartsAreSwitchedUnderCore(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)

	cases := map[string]struct {
		file string
		want []string
	}{
		"the metrics client": {
			file: "core:\n  observability:\n    enabled: true\n",
			want: []string{"diagnostics", "observability"},
		},
		"the support bundle switched off": {
			file: "core:\n  diagnostics:\n    enabled: false\n",
			want: []string{},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tags, err := build.Detect("config.yaml", []byte(testCase.file),
				build.VariantFull, nil, catalogue)
			require.NoError(t, err)
			require.Equal(t, testCase.want, tags)
		})
	}
}

// TestARetiredBlockStopsTheBuildAndNamesItsReplacement is the upgrade path. The
// retired server held these three at the top level, and a build that ignored
// one would hand the compiler no tag for a part the operator asked for.
func TestARetiredBlockStopsTheBuildAndNamesItsReplacement(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		file  string
		names string
	}{
		"the transport block": {
			file:  "mqtt:\n  enabled: true\n  mode: internal\n",
			names: "resources.sink",
		},
		"the metrics block": {
			file:  "observability:\n  enabled: true\n",
			names: "core.observability.enabled",
		},
		"the link block, which is a resource instance now": {
			file:  "fractal:\n  enabled: true\n",
			names: "resources.link.<name>.enabled",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := build.Detect("config.yaml", []byte(testCase.file),
				build.VariantFull, nil, shippedCatalogue(t))
			require.ErrorIs(t, err, build.ErrRetiredKey)
			require.Contains(t, err.Error(), testCase.names)
		})
	}
}

// TestAnUnloadedCatalogueIsRefused keeps a missing airlock from reading as a
// configuration that asked for no optional tags.
func TestAnUnloadedCatalogueIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.Detect("config.yaml", nil, build.VariantFull, nil, build.Catalogue{})
	require.ErrorIs(t, err, build.ErrCatalogue)
}

// TestExtraTagsAndVariantsAreUnchanged holds the two flags `make build` passes.
func TestExtraTagsAndVariantsAreUnchanged(t *testing.T) {
	t.Parallel()

	tags, err := build.Detect("config.yaml", nil, build.VariantFull,
		build.SplitExtraTags(" postgres  observability "), shippedCatalogue(t))
	require.NoError(t, err)
	require.Equal(t, []string{"diagnostics", "observability", "postgres"}, tags)

	_, err = build.Detect("config.yaml", nil, "nonesuch", nil, shippedCatalogue(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonesuch")
}

// TestAnExtraTagThisTreeDoesNotKnowIsRefused is the rule both the Makefile and
// the image build rely on. go build accepts any word as a tag, so a misspelt
// extra tag would build a binary without the part it names.
func TestAnExtraTagThisTreeDoesNotKnowIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.Detect("config.yaml", nil, build.VariantFull,
		build.SplitExtraTags("observabilty"), shippedCatalogue(t))
	require.ErrorIs(t, err, build.ErrUnknownTag)
	require.Contains(t, err.Error(), `"observabilty"`)
	require.Contains(t, err.Error(), "observability")

	tags, err := build.Detect("config.yaml", nil, build.VariantFull,
		build.SplitExtraTags(build.VariantFull), shippedCatalogue(t))
	require.NoError(t, err, "the full variant's own tag is a tag this tree knows")
	require.Equal(t, []string{"diagnostics", build.VariantFull}, tags)
}

// TestEveryTagTheTreeDeclaresIsAKnownTag keeps the known set honest. A file
// behind a build constraint names a tag this tree compiles against, so an extra
// tag naming it has to be accepted. Operating system and architecture names
// are set by the toolchain and are left out.
func TestEveryTagTheTreeDeclaresIsAKnownTag(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)
	declared := map[string]bool{}

	// The tree is walked rather than listed by git, because the race image
	// carries the sources without their history.
	err := filepath.WalkDir(repoPath(), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return skipDir(entry.Name())
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		for line := range strings.SplitSeq(string(body), "\n") {
			line = strings.TrimSpace(line)
			if !constraint.IsGoBuild(line) {
				continue
			}

			expr, parseErr := constraint.Parse(line)
			if parseErr != nil {
				return fmt.Errorf("%s: %s: %w", path, line, parseErr)
			}

			expr.Eval(func(tag string) bool {
				if !toolchainTag(tag) {
					declared[tag] = true
				}

				return true
			})
		}

		return nil
	})
	require.NoError(t, err)

	require.NotEmpty(t, declared, "no file in the tree declared a build tag, so nothing was checked")

	printerOnly := printerOnlyTags(t)

	for tag := range declared {
		if printerOnly[tag] {
			require.False(t, catalogue.Knows(tag),
				"the printer build's tag %q is accepted as an extra tag, so make build could pass it", tag)

			continue
		}

		require.True(t, catalogue.Knows(tag),
			"a file declares the tag %q and the build refuses it as an extra tag", tag)
	}
}

// printerOnlyTags are the tags only `make build-printer` passes: the printer
// tag and every tag in PRINTER_TAGS. Every other build keeps every part and
// the normal defaults, so an extra tag naming one of them is refused.
func printerOnlyTags(t *testing.T) map[string]bool {
	t.Helper()

	body, err := os.ReadFile(repoPath("Makefile"))
	require.NoError(t, err)

	tags := map[string]bool{"printer": true}

	for line := range strings.SplitSeq(string(body), "\n") {
		value, ok := strings.CutPrefix(line, "PRINTER_TAGS +=")
		if !ok {
			continue
		}

		value, _, _ = strings.Cut(value, "#")
		for tag := range strings.FieldsSeq(value) {
			tags[tag] = true
		}
	}

	return tags
}

// skipDir leaves out the folders that hold no source of this tree: installed
// JavaScript packages and hidden folders such as .git.
func skipDir(name string) error {
	if name == "node_modules" || (len(name) > 1 && strings.HasPrefix(name, ".") && name != "..") {
		return filepath.SkipDir
	}

	return nil
}

// toolchainTag reports a tag the go command sets itself: an operating system,
// an architecture, a release such as go1.21, or one of the toolchain's own.
func toolchainTag(tag string) bool {
	switch tag {
	case "windows", "linux", "darwin", "freebsd", "netbsd", "openbsd", "plan9", "js", "wasip1",
		"unix", "amd64", "arm64", "arm", "386", "wasm", "cgo", "gc", "gccgo", "race", "ignore":
		return true
	}

	return strings.HasPrefix(tag, "go1.")
}

// fixtureCatalogue maps one type onto one tag, for the cases that need a tagged
// adapter and this tree has none. Its conditions answer every question the
// detector asks and imply nothing, so a case reads as the resources block alone.
func fixtureCatalogue(t *testing.T) build.Catalogue {
	t.Helper()

	catalogue, err := build.ParseCatalogue("fixture", []byte(`{
	  "version": 1,
	  "resources": [{"kind": "driver", "type": "core/serial", "tag": "serialdriver"}],
	  "conditions": [
	    {"condition": "core.observability.enabled", "tag": "", "minimal": false},
	    {"condition": "core.diagnostics.enabled", "tag": "", "minimal": false},
	    {"condition": "persistence.sqlite", "tag": "", "minimal": false},
	    {"condition": "persistence.postgres", "tag": "", "minimal": false}
	  ]
	}`))
	require.NoError(t, err)

	return catalogue
}

// TestDashboardsSwitchedOffAreLeftOutOfEveryVariant is the one condition whose
// tag takes code out. It holds in the minimal variant too: a file that
// switched dashboards off must not get them back from a smaller build. A file
// that says nothing about them, or switches them on, builds them in.
func TestDashboardsSwitchedOffAreLeftOutOfEveryVariant(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)
	off := []byte("core:\n  dashboards:\n    enabled: false\n")

	for _, variant := range []string{build.VariantFull, build.VariantStandard, build.VariantMinimal} {
		tags, err := build.Detect("config.yaml", off, variant, nil, catalogue)
		require.NoError(t, err)
		require.Containsf(t, tags, "nodashboards", "variant %s", variant)

		for _, on := range []string{"", "core:\n  dashboards: {}\n", "core:\n  dashboards:\n    enabled: true\n"} {
			tags, err = build.Detect("config.yaml", []byte(on), variant, nil, catalogue)
			require.NoError(t, err)
			require.NotContainsf(t, tags, "nodashboards", "variant %s, file %q", variant, on)
		}
	}

	printer, err := os.ReadFile(repoPath("config", "examples", "config.printer.yaml"))
	require.NoError(t, err)

	tags, err := build.Detect("config.printer.yaml", printer, build.VariantFull, nil, catalogue)
	require.NoError(t, err)
	require.Contains(t, tags, "nodashboards", "the printer build carries the dashboards")
}

// TestTheSupportBundleIsLeftOutOfTheMinimalBuild is the opt-in side of
// core.diagnostics.enabled. A file that says nothing builds the bundle into
// the full variants, the minimal variant leaves it out whatever the file says,
// and a file that switches it off leaves it out of every variant.
func TestTheSupportBundleIsLeftOutOfTheMinimalBuild(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)
	on := []byte("core:\n  diagnostics:\n    enabled: true\n")
	off := []byte("core:\n  diagnostics:\n    enabled: false\n")

	for _, file := range [][]byte{nil, on} {
		for _, variant := range []string{build.VariantFull, build.VariantStandard} {
			tags, err := build.Detect("config.yaml", file, variant, nil, catalogue)
			require.NoError(t, err)
			require.Containsf(t, tags, "diagnostics", "variant %s, file %q", variant, file)
		}

		tags, err := build.Detect("config.yaml", file, build.VariantMinimal, nil, catalogue)
		require.NoError(t, err)
		require.NotContainsf(t, tags, "diagnostics", "file %q", file)
	}

	for _, variant := range []string{build.VariantFull, build.VariantStandard, build.VariantMinimal} {
		tags, err := build.Detect("config.yaml", off, variant, nil, catalogue)
		require.NoError(t, err)
		require.NotContainsf(t, tags, "diagnostics", "variant %s", variant)
	}
}
