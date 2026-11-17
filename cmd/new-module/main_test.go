package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/scaffold"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// What the scaffold has to produce: a directory an author can build and
// install without editing anything first.

// scaffolded writes one new module into a directory of this test's own and
// returns where it went.
func scaffolded(t *testing.T, args ...string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "shelf")

	checkout := theCheckout(t)

	var out bytes.Buffer

	line := append([]string{
		"-dir", dir, "-checkout", checkout, "-git=false", "-kind", "both", "-repo", "github.com/example/shelf",
	}, args...)
	require.NoError(t, run(t.Context(), &out, nil, here(t), line), out.String())

	return dir
}

// here is an environment whose working directory is a fresh one of this
// test's own, with no checkout above it.
func here(t *testing.T) environment {
	t.Helper()

	return environment{workDir: t.TempDir(), version: allTools}
}

// theCheckout is the checkout these tests run in, two directories up.
func theCheckout(t *testing.T) string {
	t.Helper()

	checkout, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	return checkout
}

// shelfIn is the shelf folder as a printed command writes it. The folder is
// below where the run started, or inside dirs when they are given. Git Bash
// drops a bare backslash, so on Windows the command quotes the folder.
func shelfIn(dirs ...string) string {
	if len(dirs) == 0 {
		return scaffold.ShellWord("." + string(filepath.Separator) + "shelf")
	}

	return scaffold.ShellWord(filepath.Join(append(dirs, "shelf")...))
}

// fakeCheckout is a checkout of a test's own, under t.TempDir(), holding only
// what scaffold.FindCheckout reads to recognise one: sdk/go.mod and
// packages/svelte/package.json. A test that resolves the default location
// beside a checkout uses this rather than theCheckout, so it never computes a
// path beside the real one.
func fakeCheckout(t *testing.T) string {
	t.Helper()

	checkout := filepath.Join(t.TempDir(), "checkout")

	require.NoError(t, os.MkdirAll(filepath.Join(checkout, "sdk"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "sdk", "go.mod"),
		[]byte("module github.com/prusa3d/prusa-connect/sdk\n\ngo "+publishedGo+"\n"), 0o600))

	require.NoError(t, os.MkdirAll(filepath.Join(checkout, "packages", "svelte"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "packages", "svelte", "package.json"),
		[]byte(`{"version":"0.5.0"}`), 0o600))

	return checkout
}

// report is what a script reads: exitUsage is a typo on the command line,
// exitFailure is everything else that stopped the write.
func TestTheExitCodeTellsATypoFromAFailure(t *testing.T) {
	t.Parallel()

	require.Equal(t, exitOK, report(nil))
	require.Equal(t, exitUsage, report(fmt.Errorf("%w: -kind", errUsage)))
	require.Equal(t, exitFailure, report(scaffold.ErrNotEmpty))
}

// TestAScaffoldedModuleVetsAndItsManifestIsOneTheHostAccepts is the only claim
// this command makes that matters. What it writes compiles, and the manifest
// beside it is one `module add` would install.
func TestAScaffoldedModuleVetsAndItsManifestIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool on PATH: %v", err)
	}

	dir := scaffolded(t, "-name", "shelf")

	vet := exec.CommandContext(t.Context(), goTool, "vet", "./...")
	vet.Dir = dir

	said, err := vet.CombinedOutput()
	require.NoError(t, err, "go vet in the scaffolded module: %s", said)

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err, "the host would refuse the scaffolded manifest")

	require.Equal(t, "shelf", held.Name)
	require.Equal(t, "sink", held.Kind)
	require.Equal(t, 1, held.ContractMajor)
	require.NotNil(t, held.UI, "the scaffolded module draws nothing")
	require.Equal(t, "ui/index.js", held.UI.Entry)
	require.Len(t, held.UI.Contributes.Pages, 1)
	require.Equal(t, "shelf", held.UI.Contributes.Pages[0].Path)
	require.Equal(t, "Shelf", held.UI.Contributes.Pages[0].Title)

	// The range is the library in this checkout, not a number copied from an
	// example, so a bundle built here loads in the app built here.
	_, err = manifest.ParseRange(held.UI.Library)
	require.NoError(t, err)

	// Both halves are there, and the browser one is where the manifest says.
	require.FileExists(t, filepath.Join(dir, "main.go"))
	require.FileExists(t, filepath.Join(dir, "openapi.json"))
	require.FileExists(t, filepath.Join(dir, "README.md"))
	require.FileExists(t, filepath.Join(dir, "ui", "src", "index.ts"))
	require.FileExists(t, filepath.Join(dir, "ui", "src", "page.svelte"))
}

// TestAScaffoldedModuleStartsAtTheStandardEveryModuleIsHeldTo names the files
// an author would otherwise write last or not at all: a test, the codes the
// module refuses with, a changelog, and one declared setting. An author copies
// what the skeleton gives them, so a skeleton that gives them none of this
// produces a module that has none of it.
func TestAScaffoldedModuleStartsAtTheStandardEveryModuleIsHeldTo(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf")

	require.FileExists(t, filepath.Join(dir, "main_test.go"))
	require.FileExists(t, filepath.Join(dir, "faults.go"))
	require.FileExists(t, filepath.Join(dir, "CHANGELOG.md"))

	// Every setting is declared with a type, a default and one sentence saying
	// what it controls, and the process reads them from plugin.configure.
	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err)
	require.Len(t, held.Options, 1)
	require.Equal(t, "message", held.Options[0].Key)
	require.Equal(t, extension.OptionString, held.Options[0].Type)
	require.NotEmpty(t, held.Options[0].Default)
	require.NotEmpty(t, held.Options[0].Summary)
	require.LessOrEqual(t, len(held.Options[0].Summary), maxSummaryLen)

	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(body), "wirerpc.MethodConfigure")
	require.NotContains(t, string(body), "os.Getenv",
		"a setting read from the environment is one no operator can find in the manifest")

	// The changelog's newest heading is the version the manifest declares, so
	// an operator reading either one reads the same release.
	changelog, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	require.NoError(t, err)
	require.Contains(t, string(changelog), "## "+held.Version)
}

// maxSummaryLen is how long the one sentence beside a setting may be. It is the
// length a settings pane prints without wrapping into a paragraph.
const maxSummaryLen = 120

// TestTheScaffoldSubstitutesTheNamesEverywhere is what separates a skeleton
// from a copied example: an author renames nothing after this runs.
func TestTheScaffoldSubstitutesTheNamesEverywhere(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-repo", "github.com/acme/shelf", "-title", "Filament shelf")

	for _, name := range []string{"main.go", "README.md", "module.yaml", "openapi.json"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)

		require.NotContains(t, string(body), "spools", "%s still names the example", name)
	}

	body, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(body), "title: Filament shelf")

	// The namespace the host will derive, written into the comments so an
	// author reads the addresses their module answers on.
	body, err = os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(body), "com.github.aux.acme.shelf")

	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(mod), "module github.com/acme/shelf")
	require.Contains(t, string(mod), "replace github.com/prusa3d/prusa-connect/sdk =>")
}

// TestTheScaffoldNeverOverwrites keeps a second run from replacing a module
// somebody has written. Half a module and half a skeleton compiles, which is
// what makes a silent overwrite worse than a refusal.
func TestTheScaffoldNeverOverwrites(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf")

	checkout := theCheckout(t)

	var out bytes.Buffer

	require.ErrorIs(t,
		run(t.Context(), &out, nil, here(t), []string{
			"-dir", dir, "-checkout", checkout, "-name", "shelf", "-kind", "both", "-repo", "github.com/example/shelf",
		}),
		scaffold.ErrNotEmpty,
		"a second run over a directory that already holds a module was accepted")
}

// TestTheScaffoldRefusesANameNothingCanCarry holds the one field that reaches
// three places: the instance, the binary and the page's route.
func TestTheScaffoldRefusesANameNothingCanCarry(t *testing.T) {
	t.Parallel()

	checkout := theCheckout(t)

	for name, given := range map[string]string{
		"a name with a slash":   "acme/shelf",
		"a name in capitals":    "Shelf",
		"a name that is a word": "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			dir := filepath.Join(t.TempDir(), "out")
			args := []string{"-dir", dir, "-checkout", checkout}

			if given != "" {
				args = append(args, "-name", given, "-kind", "both", "-repo", "github.com/example/shelf")
			}

			err := run(t.Context(), &out, nil, here(t), args)
			require.Error(t, err)

			if given != "" {
				// -kind and -repo are given, so it is the name that is refused,
				// not the missing flags.
				require.NotErrorIs(t, err, errUsage)
				require.Contains(t, err.Error(), "cannot name an instance")
			}

			require.NoDirExists(t, dir)
		})
	}
}

// TestTheScaffoldRefusesAModulePathWithNoHost is the other half of the same
// check. The namespace is the module path reversed, and a path with no host
// renders as nothing a host can address.
func TestTheScaffoldRefusesAModulePathWithNoHost(t *testing.T) {
	t.Parallel()

	checkout := theCheckout(t)

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, here(t), []string{
		"-dir", filepath.Join(t.TempDir(), "out"),
		"-checkout", checkout, "-repo", "shelf", "-name", "shelf", "-kind", "both",
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errUsage)
	require.NotContains(t, out.String(), "wrote")
}

// contributesRegex reads one `host.register` call out of a generated entry:
// which function was called, and the name it was called with.
//
//nolint:gochecknoglobals // one compiled pattern, read by the tests below.
var registration = regexp.MustCompile(`host\.register\.([a-zA-Z]+)\('([^']+)'`)

// registered is every registration the generated entry makes, as
// `<slot>:<id>`.
func registered(t *testing.T, dir string) []string {
	t.Helper()

	entry, err := os.ReadFile(filepath.Join(dir, "ui", "src", "index.ts"))
	require.NoError(t, err)

	made := []string{}

	for _, call := range registration.FindAllStringSubmatch(string(entry), -1) {
		made = append(made, call[1]+":"+call[2])
	}

	slices.Sort(made)

	return made
}

// declared is every entry the generated manifest's `contributes` block carries,
// named the way the registration that matches it would be. It walks the slot
// table rather than naming a slot, so a row the table gains needs no matching
// case here.
func declared(t *testing.T, dir string) []string {
	t.Helper()

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err, "the host would refuse the scaffolded manifest")
	require.NotNil(t, held.UI)

	said := []string{}

	for _, page := range held.UI.Contributes.Pages {
		said = append(said, manifest.SlotPage.Register()+":"+page.Path)
	}

	for _, card := range held.UI.Contributes.Cards {
		said = append(said, manifest.SlotDashboardCard.Register()+":"+card.ID)
	}

	for _, slot := range manifest.Slots() {
		for _, section := range held.UI.Contributes.Sections[slot.Name] {
			said = append(said, slot.Name.Register()+":"+section.ID)
		}

		for _, action := range held.UI.Contributes.Actions[slot.Name] {
			said = append(said, slot.Name.Register()+":"+action.ID)
		}
	}

	slices.Sort(said)

	return said
}

// TestTheEntryRegistersExactlyWhatTheManifestDeclares is the most valuable
// thing this command does. `contributes` is the ceiling: a registration the
// manifest did not declare throws and the host drops the module whole
// (ADR-20260911-4p6 rule 21), and the two are written from one answer so that they
// cannot disagree.
func TestTheEntryRegistersExactlyWhatTheManifestDeclares(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"a page, which is the default":    {},
		"a device-card section":           {"-contributes", "device-card"},
		"a settings pane":                 {"-contributes", "settings"},
		"every slot with no process half": {"-kind", "ui", "-contributes", strings.Join(slotNames(), ",")},
	}

	// Every row of the table gets its own case, so a slot the table gains
	// starts covered here with no edit to this test.
	for _, slot := range manifest.Slots() {
		cases[string(slot.Name)] = []string{"-contributes", string(slot.Name)}
	}

	cases["every slot"] = []string{"-contributes", strings.Join(slotNames(), ",")}

	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := scaffolded(t, append([]string{"-name", "shelf"}, line...)...)

			made := registered(t, dir)
			require.NotEmpty(t, made, "the entry registers nothing at all")
			require.Equal(t, declared(t, dir), made,
				"the manifest and the entry disagree, and the host would drop this module whole")
		})
	}
}

// TestAModuleThatIsOneBrowserBundleWritesNoProcessHalf is ADR-20260911-4p6 rule 30: a
// module that runs nothing is the asset kind, and an author deletes nothing
// after the generator has run.
func TestAModuleThatIsOneBrowserBundleWritesNoProcessHalf(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-kind", "ui", "-contributes", "device-card")

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err, "the host would refuse the scaffolded manifest")

	require.Equal(t, "asset", held.Kind)
	require.Zero(t, held.ContractMajor, "a module with no process half speaks no wire protocol")
	require.NotNil(t, held.UI)
	require.Len(t, held.UI.Contributes.Sections[manifest.SlotDeviceCard], 1)
	require.Empty(t, held.UI.Contributes.Pages)

	// Nothing Go is written, so there is nothing to delete.
	for _, name := range []string{"main.go", "go.mod", "openapi.json"} {
		require.NoFileExists(t, filepath.Join(dir, name),
			"%s was written for a module that runs no process", name)
	}

	require.FileExists(t, filepath.Join(dir, "ui", "src", "device-card.svelte"))
	require.FileExists(t, filepath.Join(dir, "install.sh"))
}

// TestOnlyTheContributionsAskedForAreWritten is the other half of it: a module
// that draws one thing carries no component for the two it does not.
func TestOnlyTheContributionsAskedForAreWritten(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-contributes", "settings")
	source := filepath.Join(dir, "ui", "src")

	require.FileExists(t, filepath.Join(source, "settings.svelte"))
	require.NoFileExists(t, filepath.Join(source, "page.svelte"))
	require.NoFileExists(t, filepath.Join(source, "device-card.svelte"))

	catalogue, err := os.ReadFile(filepath.Join(source, "locales", "en.json"))
	require.NoError(t, err)

	var texts map[string]string

	require.NoError(t, json.Unmarshal(catalogue, &texts), "the catalogue is not valid JSON")
	require.Contains(t, texts, "shelf.settings.body")
	require.NotContains(t, texts, "shelf.page.title")
	require.NotContains(t, texts, "shelf.deviceCard.body")
}

// translated is each key a component of the bundle reads through `t`.
var translated = regexp.MustCompile(`\bt\('([^']+)'\)`)

// TestEveryTextAComponentReadsIsInTheCatalogue. A key the catalogue lacks
// reaches the screen as the key itself, and nothing fails on the way there.
func TestEveryTextAComponentReadsIsInTheCatalogue(t *testing.T) {
	t.Parallel()

	source := filepath.Join(
		scaffolded(t, "-name", "shelf", "-contributes", strings.Join(slotNames(), ",")), "ui", "src")

	catalogue, err := os.ReadFile(filepath.Join(source, "locales", "en.json"))
	require.NoError(t, err)

	var texts map[string]string

	require.NoError(t, json.Unmarshal(catalogue, &texts), "the catalogue is not valid JSON")

	components, err := filepath.Glob(filepath.Join(source, "*.svelte"))
	require.NoError(t, err)

	scripts, err := filepath.Glob(filepath.Join(source, "*.ts"))
	require.NoError(t, err)

	// index.ts and host.ts are the bundle's own plumbing, not a slot's
	// component; only a run function named after a FormAction slot belongs
	// beside the .svelte ones here.
	for _, script := range scripts {
		if base := filepath.Base(script); base != "index.ts" && base != "host.ts" {
			components = append(components, script)
		}
	}

	require.Len(t, components, len(manifest.Slots()))

	for _, component := range components {
		markup, failed := os.ReadFile(component)
		require.NoError(t, failed)

		for _, read := range translated.FindAllStringSubmatch(string(markup), -1) {
			require.Contains(t, texts, read[1], "%s reads a text the catalogue does not have", filepath.Base(component))
		}
	}
}

// TestABundleThatRegistersNothingIsRefused. A module with no contribution
// draws nothing, and a slot name the library does not publish fails the
// manifest gate at install; both are cheaper to say here.
func TestABundleThatRegistersNothingIsRefused(t *testing.T) {
	t.Parallel()

	checkout := theCheckout(t)

	for name, given := range map[string]string{
		"no contribution at all":  "",
		"a slot nobody publishes": "dashboard",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "out")

			var out bytes.Buffer

			require.ErrorIs(t, run(t.Context(), &out, nil, here(t), []string{
				"-dir", dir, "-checkout", checkout, "-name", "shelf", "-contributes", given,
				"-kind", "both", "-repo", "github.com/example/shelf",
			}), errUsage)
			require.NoDirExists(t, dir)
		})
	}
}

// TestAScaffoldedModuleCarriesItsOwnTailwind is ADR-20260914-244 rules 10 to 13. The
// application's `@source` lines cannot see a module, so a module that does not
// build its own Tailwind names a utility nobody emits.
func TestAScaffoldedModuleCarriesItsOwnTailwind(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf")

	sheet, err := os.ReadFile(filepath.Join(dir, "ui", "src", "styles.css"))
	require.NoError(t, err)

	for _, directive := range []string{
		"@reference 'tailwindcss';",
		"@reference '@prusa/connect-ui/tokens.css';",
		"@tailwind utilities;",
		"@source './';",
	} {
		require.Contains(t, string(sheet), directive)
	}

	// A stylesheet nothing imports emits nothing, and the build has to be able
	// to resolve Tailwind itself.
	entry, err := os.ReadFile(filepath.Join(dir, "ui", "src", "index.ts"))
	require.NoError(t, err)
	require.Contains(t, string(entry), "import './styles.css';")

	packaged, err := os.ReadFile(filepath.Join(dir, "ui", "package.json"))
	require.NoError(t, err)

	var held struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}

	require.NoError(t, json.Unmarshal(packaged, &held))
	require.Contains(t, held.DevDependencies, "tailwindcss",
		"the module's own stylesheet has no Tailwind to build it with")
}

// TestNobodyIsAskedWhenNobodyIsThere. `make new-module`, this test and
// every script run on the flags, and a question in front of one is a run that
// never ends.
func TestNobodyIsAskedWhenNobodyIsThere(t *testing.T) {
	t.Parallel()

	checkout := theCheckout(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, here(t), []string{
		"-dir", filepath.Join(t.TempDir(), "shelf"), "-checkout", checkout, "-name", "shelf", "-git=false",
		"-kind", "both", "-repo", "github.com/example/shelf",
	}))
	require.NotContains(t, out.String(), "A new module.",
		"the questions were put to a stream nobody types into")
	require.NotContains(t, out.String(), "?", "a question was asked of a stream nobody types into")
}

// TestWithNoTerminalTheMissingFlagsAreNamed. A script that leaves out an
// answer is told every flag it left out, in one run.
func TestWithNoTerminalTheMissingFlagsAreNamed(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, here(t), []string{"-name", "shelf"})
	require.ErrorIs(t, err, errUsage)
	require.Contains(t, err.Error(), "-kind -repo")
}

// TestAnExplicitlyEmptyRepoFails. -repo carries no default the way an unset
// repository address does on a later interview pass: a script that types
// -repo "" chose that, and planFor must not read it as nobody having chosen.
func TestAnExplicitlyEmptyRepoFails(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, here(t),
		[]string{"-name", "shelf", "-kind", "both", "-repo", ""})
	require.ErrorIs(t, err, errUsage)
	require.Contains(t, err.Error(), "-repo is the repository address")
}

// TestAModuleThatIsOneProcessWritesNoBundle is the third kind: a process and a
// manifest with no ui block, so the app draws nothing for it and the author
// deletes nothing.
func TestAModuleThatIsOneProcessWritesNoBundle(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-kind", "process")

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err, "the host would refuse the scaffolded manifest")
	require.Equal(t, "sink", held.Kind)
	require.Equal(t, 1, held.ContractMajor)
	require.Nil(t, held.UI, "a module that draws nothing was given a ui block")

	require.NoDirExists(t, filepath.Join(dir, "ui"))
	require.FileExists(t, filepath.Join(dir, "main.go"))

	for _, name := range []string{"README.md", "module.yaml", "install.sh", "main.go", "CHANGELOG.md"} {
		body, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, readErr)
		require.NotContains(t, string(body), "ui/", "%s names a bundle this module does not have", name)
	}
}

// TestAProcessOnlyModuleTakesNoContributions. -contributes names what a bundle
// registers, and a process-only module has no bundle.
func TestAProcessOnlyModuleTakesNoContributions(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, here(t), []string{
		"-dir", filepath.Join(t.TempDir(), "out"), "-checkout", theCheckout(t),
		"-name", "shelf", "-kind", "process", "-contributes", "page",
	})
	require.ErrorIs(t, err, errUsage)
}

// TestAUIOnlyChangelogNamesNoRoute. The first release of a module with no
// process adds what it draws, and no route it does not serve.
func TestAUIOnlyChangelogNamesNoRoute(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-kind", "ui", "-contributes", "device-card")

	changelog, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	require.NoError(t, err)
	require.NotContains(t, string(changelog), "GET /")
	require.Contains(t, string(changelog), "one line on each printer's card in the listing")
}

// TestTheEditorIsOfferedTheExtensionsForTheHalvesPresent.
func TestTheEditorIsOfferedTheExtensionsForTheHalvesPresent(t *testing.T) {
	t.Parallel()

	for kind, want := range map[string][]string{
		"ui":      {"svelte.svelte-vscode"},
		"process": {"golang.go"},
		"both":    {"svelte.svelte-vscode", "golang.go"},
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			dir := scaffolded(t, "-name", "shelf", "-kind", kind)

			body, err := os.ReadFile(filepath.Join(dir, ".vscode", "extensions.json"))
			require.NoError(t, err)

			var held struct {
				Recommendations []string `json:"recommendations"`
			}

			require.NoError(t, json.Unmarshal(body, &held), "extensions.json is not JSON: %s", body)
			require.Equal(t, want, held.Recommendations)
		})
	}
}

// TestInstallShParsesForEveryKind. install.sh is rendered from one template
// with a branch per half, and a branch that breaks the shell's grammar fails
// only on the author's machine.
func TestInstallShParsesForEveryKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"ui", "process", "both"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			dir := scaffolded(t, "-name", "shelf", "-kind", kind)

			said, err := exec.CommandContext(t.Context(), "sh", "-n", filepath.Join(dir, "install.sh")).CombinedOutput()
			require.NoError(t, err, "%s", said)
		})
	}
}

// TestAScaffoldedModuleIsAlreadyFormatted is what lets scaffold.Render skip
// go/format.
func TestAScaffoldedModuleIsAlreadyFormatted(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"process", "both"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			dir := scaffolded(t, "-name", "shelf", "-kind", kind)

			for _, name := range []string{"main.go", "main_test.go", "faults.go"} {
				body, err := os.ReadFile(filepath.Join(dir, name))
				require.NoError(t, err)

				formatted, err := format.Source(body)
				require.NoError(t, err, "%s does not parse", name)
				require.Equal(t, string(formatted), string(body), "%s is not gofmt-formatted", name)
			}
		})
	}
}

// TestInstallShowsPnpmOnlyWhenItFails. pnpm prints pages on a good run, and on
// every run it warns about an ~/.npmrc variable that is not set. Only the end
// of a failure is shown, without that warning.
func TestInstallShowsPnpmOnlyWhenItFails(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX shell script")
	}

	const warning = `WARN  Issue while reading "/home/you/.npmrc". Failed to replace env in config: ${NPM_TOKEN}`

	for name, c := range map[string]struct {
		pnpm     string
		shown    string
		notShown string
	}{
		"a failing install shows the end of its log": {
			pnpm:     "echo '" + warning + "'\necho 'ERR_PNPM_FETCH_404 no such package' >&2\nexit 1\n",
			shown:    "ERR_PNPM_FETCH_404",
			notShown: "Failed to replace env",
		},
		"a passing install shows nothing of pnpm": {
			pnpm:     "echo 'Progress: resolved 1, reused 1'\nexit 0\n",
			shown:    "is missing module.yaml or ui/index.js",
			notShown: "Progress: resolved",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checkout := t.TempDir()
			built := filepath.Join(checkout, "packages", "svelte", "dist", "build")
			require.NoError(t, os.MkdirAll(built, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(built, "module.js"), nil, 0o600))

			dir := filepath.Join(t.TempDir(), "shelf")
			_, err := scaffold.Write(scaffold.Plan{
				Name: "shelf", Title: "Shelf", Module: "github.com/acme/shelf",
				Namespace: "com.github.aux.acme.shelf", Kind: assetKind, Version: firstVersion,
				Dir: dir, Checkout: filepath.ToSlash(checkout), GoVersion: "1.26.11",
				Library: ">=0.1.0 <1.0.0", UI: true,
				Contributions: []scaffold.Contribution{{Slot: manifest.SlotPage, ID: "shelf", Title: "Shelf"}},
			})
			require.NoError(t, err)

			tools := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(tools, "go"), []byte("#!/bin/sh\nexit 0\n"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(tools, "pnpm"), []byte("#!/bin/sh\n"+c.pnpm), 0o700))

			install := exec.CommandContext(t.Context(), "sh", "install.sh")
			install.Dir = dir
			install.Env = append(os.Environ(), "PATH="+tools+":/usr/bin:/bin")

			said, err := install.CombinedOutput()
			require.Error(t, err, "install.sh passed with no bundle built: %s", said)
			require.Contains(t, string(said), "1. shelf installs and builds into a bundle...")
			require.Contains(t, string(said), c.shown)
			require.NotContains(t, string(said), c.notShown)
		})
	}
}

// TestInstallShPrintsOneLineWhenItPasses. What pnpm, go and `module add`
// print goes to the log, and a good run ends with where the module is
// installed. TARGET picks the demo's data or the real app's.
func TestInstallShPrintsOneLineWhenItPasses(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX shell script")
	}

	for name, c := range map[string]struct {
		target string
		work   string
		data   string
	}{
		"the demo":     {"", "shelf-demo", filepath.Join("shelf-demo", "demo")},
		"the real app": {"app", "shelf-app", "shelf-app"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checkout := t.TempDir()
			built := filepath.Join(checkout, "packages", "svelte", "dist", "build")
			require.NoError(t, os.MkdirAll(built, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(built, "module.js"), nil, 0o600))

			dir := filepath.Join(t.TempDir(), "shelf")
			_, err := scaffold.Write(scaffold.Plan{
				Name: "shelf", Title: "Shelf", Module: "github.com/acme/shelf",
				Namespace: "com.github.aux.acme.shelf", Kind: initKind, Version: firstVersion,
				Dir: dir, Checkout: filepath.ToSlash(checkout), GoVersion: "1.26.11",
				Library: ">=0.1.0 <1.0.0", UI: true, Process: true,
				Contributions: []scaffold.Contribution{{Slot: manifest.SlotPage, ID: "shelf", Title: "Shelf"}},
			})
			require.NoError(t, err)

			// pnpm's build writes the release the way the real one does, into
			// the directory it runs in, and both tools print what a real run
			// prints a page of.
			tools := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(tools, "go"),
				[]byte("#!/bin/sh\ncase \"$*\" in\n"+
					"env*) echo x ;;\n"+
					"*' build '*) while [ \"$1\" != -o ]; do shift; done; touch \"$2\" ;;\n"+
					"*) echo 'installed github.com/acme/shelf as asset' ;;\n"+
					"esac\n"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(tools, "pnpm"), []byte("#!/bin/sh\n"+
				"echo 'Progress: resolved 1, reused 1'\n"+
				"case \"$*\" in *'run build'*) mkdir -p dist/ui; touch dist/module.yaml dist/ui/index.js;; esac\n"),
				0o700))

			install := exec.CommandContext(t.Context(), "sh", "install.sh")
			install.Dir = dir
			install.Env = append(os.Environ(), "PATH="+tools+":/usr/bin:/bin", "TARGET="+c.target)

			said, err := install.CombinedOutput()
			require.NoError(t, err, "%s", said)

			work := filepath.Join(checkout, "bin", c.work)
			require.True(t, strings.HasSuffix(string(said), "OK    shelf is installed in "+work+"\n"), "%s", said)
			require.NotContains(t, string(said), "Progress: resolved")
			require.NotContains(t, string(said), "installed github.com/acme/shelf as asset")

			config, err := os.ReadFile(filepath.Join(work, "config.yaml"))
			require.NoError(t, err)
			require.Contains(t, string(config), "overlay: "+filepath.Join(checkout, "bin", c.data, "resources.json"))
			require.Contains(t, string(config),
				"database: "+filepath.Join(checkout, "bin", c.data, "config.d", "authz.db"))
		})
	}
}

// TestAModuleLandsBesideTheCheckout. Written from anywhere inside a checkout,
// the module goes next to the checkout and not into it, and it is printed
// from where the command ran.
func TestAModuleLandsBesideTheCheckout(t *testing.T) {
	t.Parallel()

	checkout := fakeCheckout(t)
	beside := filepath.Join(filepath.Dir(checkout), "shelf")

	for name, c := range map[string]struct {
		workDir string
		shown   string
	}{
		"from the root":          {checkout, filepath.Join("..", "shelf")},
		"from a directory in it": {filepath.Join(checkout, "internal"), filepath.Join("..", "..", "shelf")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plan, err := planFor(answers{
				name: "shelf", repo: "github.com/acme/shelf", shape: shapeBoth,
				//nolint:exhaustive // one chosen slot is the whole point of this fixture
				contributions: map[manifest.SlotName]scaffold.Contribution{manifest.SlotPage: {}}, workDir: c.workDir,
			})
			require.NoError(t, err)
			require.Equal(t, beside, plan.Dir)
			require.Equal(t, filepath.ToSlash(checkout), plan.Checkout)
			require.Equal(t, c.shown, shown(c.workDir, plan.Dir))
		})
	}
}

// TestTheFlagLineCarriesATitleThatChanged. -title is a real flag, so the flag
// line writes it when the advanced step changed the title, and leaves it out
// when the title is the one the name would give anyway.
func TestTheFlagLineCarriesATitleThatChanged(t *testing.T) {
	t.Parallel()

	request := answers{shape: shapeUI, workDir: t.TempDir()}
	plan := scaffold.Plan{
		Name:   "shelf",
		Title:  "Shelf",
		Module: "github.com/acme/shelf",
		Dir:    filepath.Join(request.workDir, "shelf"),
	}

	require.NotContains(t, flagLine(request, plan), "-title")

	plan.Title = "Filament Shelf"
	require.Contains(t, flagLine(request, plan), "-title 'Filament Shelf'")
}

// TestTheNoteNamesOnlyWhatHasNoFlag. changedLabels drives the "no flag
// carries..." note, and a changed title has its own flag: only an address or
// an id the advanced step changed is something the flag line cannot write.
func TestTheNoteNamesOnlyWhatHasNoFlag(t *testing.T) {
	t.Parallel()

	plan := scaffold.Plan{
		Name: "shelf", Title: "Filament Shelf",
		Contributions: []scaffold.Contribution{{Slot: manifest.SlotPage, ID: "shelf", Title: "Filament Shelf"}},
	}
	require.False(t, changedLabels(plan), "a changed title alone is not a changed label")

	plan.Contributions[0].ID = "rack"
	require.True(t, changedLabels(plan), "a changed id is a changed label")
}

// TestTheDefaultLocationIsPrintedRelativeToWhereItRan. `make new-module`
// passes no -dir, so the module goes beside the checkout by default. What is
// printed back must still read as a path from where the command ran, not the
// absolute one placeFor resolves it to.
func TestTheDefaultLocationIsPrintedRelativeToWhereItRan(t *testing.T) {
	t.Parallel()

	checkout := fakeCheckout(t)
	env := environment{workDir: checkout, version: allTools}

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env,
		[]string{"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-git=false"}))

	said := out.String()
	relative := filepath.Join("..", "shelf")
	absolute := filepath.Join(filepath.Dir(checkout), "shelf")

	require.Contains(t, said, "1. wrote ")
	require.Contains(t, said, relative)
	require.Contains(t, said, "-start demo -dir "+shelfIn("..")+"\n")
	require.NotContains(t, said, "-dir "+absolute,
		"the start line printed the absolute path instead of the relative one")
	require.Equal(t, 1, strings.Count(said, absolute), "only the one absolute path is printed")
}

// TestAModuleOutsideACheckoutTakesThePublishedPackages. With no checkout above
// the working directory, the module goes into it and asks the registries for
// the versions this tree releases.
func TestAModuleOutsideACheckoutTakesThePublishedPackages(t *testing.T) {
	t.Parallel()

	env := here(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env,
		[]string{"-name", "shelf", "-kind", "both", "-repo", "github.com/acme/shelf", "-git=false"}))

	dir := filepath.Join(env.workDir, "shelf")
	require.NoDirExists(t, filepath.Join(dir, ".git"))

	packaged, err := os.ReadFile(filepath.Join(dir, "ui", "package.json"))
	require.NoError(t, err)

	var held struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	require.NoError(t, json.Unmarshal(packaged, &held))
	require.Equal(t, map[string]string{
		"@prusa/connect":    publishedPackages,
		"@prusa/connect-ui": publishedPackages,
	}, held.Dependencies)
	require.Equal(t, publishedPackages, held.DevDependencies["@prusa/frontend-standards"])

	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(mod), "require github.com/prusa3d/prusa-connect/sdk "+publishedSDK)
	require.NotContains(t, string(mod), "replace")

	tsconfig, err := os.ReadFile(filepath.Join(dir, "ui", "tsconfig.json"))
	require.NoError(t, err)
	require.True(t, json.Valid(tsconfig), "tsconfig.json is not JSON: %s", tsconfig)
	require.NotContains(t, string(tsconfig), "paths")

	// install.sh runs the checkout's server and demo, and there is none.
	require.NoFileExists(t, filepath.Join(dir, "install.sh"))

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)

	manifested, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err)
	require.Equal(t, ">="+publishedPackages+" <1.0.0", manifested.UI.Library)
}

// TestAModuleOutsideACheckoutInstallsFromTheLocalRegistry. Nothing is on npm
// yet, so the scope resolves from the registry `make registry-up` starts.
func TestAModuleOutsideACheckoutInstallsFromTheLocalRegistry(t *testing.T) {
	t.Parallel()

	env := here(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env,
		[]string{"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-git=false"}))

	dir := filepath.Join(env.workDir, "shelf")

	npmrc, err := os.ReadFile(filepath.Join(dir, "ui", ".npmrc"))
	require.NoError(t, err)
	require.Contains(t, string(npmrc), "@prusa:registry=http://127.0.0.1:54874/\n")

	workspace, err := os.ReadFile(filepath.Join(dir, "ui", "pnpm-workspace.yaml"))
	require.NoError(t, err)
	require.NotContains(t, string(workspace), "overrides:")

	// pnpm 11's default minimum-release-age window refuses a version
	// published seconds ago unless the three names are excluded from it.
	require.Contains(t, string(workspace), "minimumReleaseAgeExclude:\n")
	require.Contains(t, string(workspace), "'@prusa/connect'")
	require.Contains(t, string(workspace), "'@prusa/connect-ui'")
	require.Contains(t, string(workspace), "'@prusa/frontend-standards'")

	// install.sh installs with CI=true and -start runs the dev server
	// without it, so the setting whose default CI flips is pinned.
	require.Contains(t, string(workspace), "\nenableGlobalVirtualStore: false\n")
}

// TestAFolderThatIsNotEmptyIsRefused. A skeleton written among files of
// someone else's is half a module nobody can tell apart from the rest, so
// nothing is written into a folder that holds anything.
func TestAFolderThatIsNotEmptyIsRefused(t *testing.T) {
	t.Parallel()

	for name, prepare := range map[string]func(t *testing.T, dir string){
		"a folder with a file in it": func(t *testing.T, dir string) {
			t.Helper()
			require.NoError(t, os.MkdirAll(dir, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine\n"), 0o600))
		},
		"a file": func(t *testing.T, dir string) {
			t.Helper()
			require.NoError(t, os.WriteFile(dir, []byte("mine\n"), 0o600))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "shelf")
			prepare(t, dir)

			var out bytes.Buffer

			err := run(t.Context(), &out, nil, here(t), []string{
				"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-dir", dir,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), dir)
			require.NoFileExists(t, filepath.Join(dir, "module.yaml"))
		})
	}

	t.Run("an empty folder is taken", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		var out bytes.Buffer

		require.NoError(t, run(t.Context(), &out, nil, here(t), []string{
			"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-dir", dir, "-git=false",
		}))
		require.FileExists(t, filepath.Join(dir, "module.yaml"))
	})
}

// TestAMissingToolStopsTheRunBeforeAnythingIsWritten.
func TestAMissingToolStopsTheRunBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	env := environment{workDir: t.TempDir(), version: versions(map[string]string{"node": "v24.19.0"})}

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, env,
		[]string{"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf"})
	require.ErrorIs(t, err, errTools)
	require.Contains(t, err.Error(), "corepack enable pnpm")
	require.NoDirExists(t, filepath.Join(env.workDir, "shelf"))
}

// TestTheStepsAreNumberedFromOneAndThePathsAreRelative. What the command
// prints is read by the person who typed it, from where they typed it.
func TestTheStepsAreNumberedFromOneAndThePathsAreRelative(t *testing.T) {
	t.Parallel()

	env := here(t)

	var out bytes.Buffer

	require.NoError(t, run(t.Context(), &out, nil, env, []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf",
		"-dir", "shelf", "-checkout", theCheckout(t), "-git=false",
	}))

	said := out.String()
	folder := "." + string(filepath.Separator) + "shelf"

	require.True(t, strings.HasPrefix(said, "1. wrote "), said)
	// Built by hand: filepath.Join would clean the ./ away.
	require.Contains(t, said, "     "+folder+string(filepath.Separator)+"module.yaml\n")
	require.Equal(t, 1, strings.Count(said, env.workDir), "only the one absolute path is printed: %s", said)
	require.True(t, strings.HasSuffix(said, "  start it later  go run ./cmd/new-module -start demo -dir "+shelfIn()+
		" -checkout "+scaffold.ShellWord(theCheckout(t))+"\n"), said)
}

// TestGitHubAndARemoteAreOneChoiceOrTheOther. Both flags name where the first
// commit goes, and a module has one origin.
func TestGitHubAndARemoteAreOneChoiceOrTheOther(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, here(t), []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-github", "-remote", "x",
	})
	require.ErrorIs(t, err, errUsage)
}

// TestGitHubWithoutGhStopsBeforeAnythingIsWritten.
func TestGitHubWithoutGhStopsBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	env := here(t)

	var out bytes.Buffer

	err := run(t.Context(), &out, nil, env, []string{
		"-name", "shelf", "-kind", "ui", "-repo", "github.com/acme/shelf", "-github",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gh auth login")
	require.NoDirExists(t, filepath.Join(env.workDir, "shelf"))
}

// TestTheUIPackagesAreNamedByVersion. A module's package.json reads the way
// an author's reads once the packages are published, and the checkout is
// reached through overrides in pnpm-workspace.yaml, by a relative path.
func TestTheUIPackagesAreNamedByVersion(t *testing.T) {
	t.Parallel()

	dir := scaffolded(t, "-name", "shelf", "-kind", "ui")

	manifest, err := os.ReadFile(filepath.Join(dir, "ui", "package.json"))
	require.NoError(t, err)
	require.NotContains(t, string(manifest), "link:")
	require.NotContains(t, string(manifest), "file:")

	for _, name := range []string{"@prusa/connect", "@prusa/connect-ui", "@prusa/frontend-standards"} {
		require.Contains(t, string(manifest), `"`+name+`": "`+publishedPackages+`"`)
	}

	workspace, err := os.ReadFile(filepath.Join(dir, "ui", "pnpm-workspace.yaml"))
	require.NoError(t, err)

	require.Contains(t, string(workspace), "overrides:\n")

	// filepath.Rel here would be lexical; t.TempDir() sits under /var, a
	// symlink to /private/var on macOS, so the value the scaffold wrote (which
	// resolves through that symlink to match pnpm's own physical "../" walk)
	// is read back rather than recomputed the naive way. A relative path that
	// climbs to root and back down can legitimately repeat the checkout's own
	// path as a substring, so the real check is IsAbs, not NotContains.
	matches := regexp.MustCompile(`'@prusa/connect': 'link:(\S+)/packages/typescript'`).
		FindStringSubmatch(string(workspace))
	require.Len(t, matches, 2, "no overrides line for @prusa/connect: %s", workspace)

	rel := matches[1]
	require.False(t, filepath.IsAbs(rel), "the override is an absolute path: %s", rel)

	require.Contains(t, string(workspace), `'@prusa/connect-ui': 'link:`+rel+`/packages/svelte'`)
	require.Contains(t, string(workspace), `'@prusa/frontend-standards': 'file:`+rel+`/packages/standards'`)

	// Built by string concatenation, not filepath.Join, which would clean the
	// "../" lexically: this proves the override also resolves physically, the
	// way pnpm itself walks it.
	_, err = os.Stat(dir + "/ui/" + rel + "/packages/standards/package.json")
	require.NoError(t, err, "the override does not physically resolve to the checkout's standards package")
}

// TestMemoryFlagSkipsTheQuestion. -memory-mib is the answer, so nothing is
// asked, and the manifest holds the figure.
func TestMemoryFlagSkipsTheQuestion(t *testing.T) {
	t.Parallel()

	given := map[string]bool{
		"name": true, "repo": true, "kind": true, "dir": true, "git": true, "memory-mib": true,
	}

	var out bytes.Buffer

	held := interview(
		t.Context(),
		answering(&out, "n"),
		given,
		answers{
			name:      "shelf",
			repo:      "github.com/acme/shelf",
			shape:     shapeProcess,
			memoryMiB: 256,
			workDir:   t.TempDir(),
		},
	)
	require.Equal(t, 256, held.memoryMiB)
	require.NotContains(t, out.String(), questions["memory"].text)

	dir := scaffolded(t, "-name", "shelf", "-kind", "process", "-memory-mib", "256")

	document, err := os.ReadFile(filepath.Join(dir, "module.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(document), "memory_mib: 256")

	parsed, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	require.NoError(t, err)
	require.Equal(t, 256, parsed.Needs.MemoryMiB)
}

// TestMemoryFlagIsHeldToTheManifestBounds. 0 recommends nothing, and any other
// figure the manifest would refuse is a usage error.
func TestMemoryFlagIsHeldToTheManifestBounds(t *testing.T) {
	t.Parallel()

	for figure, ok := range map[string]bool{"0": true, "16": true, "8192": true, "15": false, "8193": false, "-1": false} {
		t.Run(figure, func(t *testing.T) {
			t.Parallel()

			_, _, err := parse(&bytes.Buffer{}, here(t), []string{"-memory-mib", figure})
			if ok {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, errUsage)
			require.Contains(t, err.Error(), "-memory-mib")
		})
	}
}
