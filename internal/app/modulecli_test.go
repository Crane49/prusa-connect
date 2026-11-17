package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// What `module add` has to produce, end to end: one declaration in the
// machine-owned overlay, carrying both halves of a module under one namespace,
// which the loader reads back as one instance (ADR-20260911-4p6 rule 30).

// theModulePath is the repository the namespace is derived from, and
// theNamespace is what it renders as.
const (
	theModulePath = "github.com/acme/spools"
	theNamespace  = "com.github.aux.acme.spools"
)

// installConfig writes an operator's file whose API address nothing is
// listening on, so the probe at the end of an install answers at once.
func installConfig(t *testing.T) (string, config.Core) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	body := strings.Join([]string{
		"core:",
		"  principal: operator",
		"  api:",
		"    addr: '127.0.0.1:1'",
		"  authz:",
		"    database: '" + filepath.ToSlash(filepath.Join(dir, "authz.db")) + "'",
		// The marketplace is on by default. A test reads none, so no install
		// reaches a server on the internet; one that wants a marketplace
		// starts its own.
		"  marketplace:",
		"    enabled: false",
		"",
	}, "\n")

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	core, err := config.Load(path)
	require.NoError(t, err)

	return path, core
}

// aRelease writes the release layout earlier research names: the
// manifest at the root, this platform's binary beside it when the module runs
// a process, and the built bundle under ui/.
func aRelease(t *testing.T, document string, withProcess bool) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "release")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ui"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(document), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ui", "index.js"),
		[]byte("export default () => {}\n"), 0o600))

	if withProcess {
		require.NoError(t, os.WriteFile(filepath.Join(dir, thisPlatformBinary()),
			[]byte("#!/bin/sh\nexit 0\n"), 0o700))
	}

	return dir
}

// thisPlatformBinary is the file name a release ships for the host these tests
// run on.
func thisPlatformBinary() string {
	name := "spools_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

// bothHalvesManifest is a module with a process and a browser bundle, which is
// the shape one declaration has to carry.
func bothHalvesManifest() string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"permissions:",
		"  - observe",
		"ui:",
		"  entry: ui/index.js",
		`  library: ">=0.1.0 <1.0.0"`,
		"  contributes:",
		"    pages:",
		"      - path: spools",
		"        title: Spools",
		"",
	}, "\n")
}

// bundleOnlyManifest is a module that draws and runs nothing.
func bundleOnlyManifest() string {
	return strings.Join([]string{
		"name: nozzle-watch",
		"kind: asset",
		"license: MIT",
		"version: 2.1.0",
		"ui:",
		"  entry: ui/index.js",
		`  library: ">=0.1.0 <1.0.0"`,
		"",
	}, "\n")
}

// addModuleFrom runs one `module add` line and returns what it printed.
func addModuleFrom(t *testing.T, configPath, source, from string, extra ...string) string {
	t.Helper()

	var out bytes.Buffer

	args := append([]string{addVerb, "-config", configPath, "-from", from}, extra...)
	args = append(args, source)

	require.NoError(t, Module(t.Context(), &out, args))

	return out.String()
}

// TestModuleAddWritesOneDeclarationForBothHalves is the whole of what the
// command has to do: the files land in the modules directory, the declaration
// goes into the overlay and nowhere else, and the loader reads it back as one
// instance carrying the routes and the bundle under one namespace.
func TestModuleAddWritesOneDeclarationForBothHalves(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, said, theNamespace)
	require.Contains(t, said, "enabled     yes")

	// The operator's own file is untouched.
	body, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(body), "spools")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found, "the overlay holds no declaration for the module")
	require.Equal(t, resource.SourceOverlay, declared.Source)
	require.Equal(t, theNamespace+"/spools", declared.Spec.Type)
	require.True(t, declared.Spec.Enabled)

	require.NotNil(t, declared.Spec.Service)
	require.Equal(t, 1, declared.Spec.Service.ContractMajor)
	require.Len(t, declared.Spec.Service.Permissions, 1)

	// One declaration, both halves. The bundle is inside the service block,
	// under the same namespace as the routes.
	require.NotNil(t, declared.Spec.Service.Assets)

	namespace, isModule := declared.ModuleNamespace()
	require.True(t, isModule)
	require.Equal(t, theNamespace, namespace)

	// And the files are where the declaration says they are.
	require.FileExists(t, filepath.Join(core.Modules, "spools", binaryDirName, thisPlatformBinary()))
	require.FileExists(t, filepath.Join(core.Modules, "spools", bundleDirName, manifestName))
	require.FileExists(t, filepath.Join(core.Modules, "spools", bundleDirName, "ui", "index.js"))
}

// TestModuleAddWithoutApproveWritesNeitherCeilingNorSwitch is earlier research
// recommendation 15. The module asks and the operator decides: until they say
// yes, the instance is declared, switched off, and holds none of the
// permissions its manifest asked for.
func TestModuleAddWithoutApproveWritesNeitherCeilingNorSwitch(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release)
	require.Contains(t, said, "enabled     no")
	require.Contains(t, said, "asks for    permission observe")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.False(t, declared.Spec.Enabled)
	require.Empty(t, declared.Spec.Service.Permissions)
}

// TestModuleAddInstallsAModuleThatOnlyDraws is the other half of rule 30: a
// module with no process is one asset instance, and the same command installs
// it.
func TestModuleAddInstallsAModuleThatOnlyDraws(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bundleOnlyManifest(), false)

	addModuleFrom(t, configPath, "github.com/acme/nozzle-watch@2.1.0", release, "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindAsset, "nozzle-watch")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.Nil(t, declared.Spec.Service)
	require.NotNil(t, declared.Spec.Asset)
	require.NoDirExists(t, filepath.Join(core.Modules, "nozzle-watch", binaryDirName))
}

// TestModuleAddInstallsAModuleThatOnlyRuns is the third kind `make new-module`
// writes: a process and a manifest with no ui block. It installs as a service
// with no bundle beside it.
func TestModuleAddInstallsAModuleThatOnlyRuns(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, noPermissionManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.NotNil(t, declared.Spec.Service)
	require.Nil(t, declared.Spec.Service.Assets, "a module with no ui block was given a bundle")
	require.FileExists(t, filepath.Join(core.Modules, "spools", binaryDirName, thisPlatformBinary()))
}

// TestModuleAddReadsATarball covers the second source shape. A release an
// author published as one file installs exactly as the directory does.
func TestModuleAddReadsATarball(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	archive := anArchiveOf(t, aRelease(t, bundleOnlyManifest(), false))

	addModuleFrom(t, configPath, "github.com/acme/nozzle-watch@2.1.0", archive, "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindAsset, "nozzle-watch")
	require.NoError(t, err)

	_, found := doc.Find(instance)
	require.True(t, found)
	require.FileExists(t, filepath.Join(core.Modules, "nozzle-watch", bundleDirName, "ui", "index.js"))
}

// anArchiveOf writes one directory as the .tar.gz a release ships as.
func anArchiveOf(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "release.tar.gz")

	file, err := os.Create(path)
	require.NoError(t, err)

	zipped := gzip.NewWriter(file)
	writer := tar.NewWriter(zipped)

	require.NoError(t, filepath.WalkDir(dir, func(where string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

		body, readErr := os.ReadFile(where)
		if readErr != nil {
			return readErr
		}

		relative, relErr := filepath.Rel(dir, where)
		if relErr != nil {
			return relErr
		}

		if err = writer.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(relative), Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}

		_, err = writer.Write(body)

		return err
	}))

	require.NoError(t, writer.Close())
	require.NoError(t, zipped.Close())
	require.NoError(t, file.Close())

	return path
}

// TestModuleAddRefusesAVersionTheManifestDoesNotClaim holds the one thing a
// local release can be held to: the operator asked for a version, and the
// bytes in front of them say another.
func TestModuleAddRefusesAVersionTheManifestDoesNotClaim(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	var out bytes.Buffer

	err := Module(t.Context(), &out,
		[]string{addVerb, "-config", configPath, "-from", release, theModulePath + "@2.0.0"})
	require.ErrorContains(t, err, "1.0.0")
	require.NoFileExists(t, core.Overlay, "a refused install wrote a declaration")
}

// TestModuleAddTakesAVersionRange is the owner's condition on ADR-20260910-74m: an
// operator writes the version the way a module author writes one, so a caret
// takes whatever release of that line is in front of them and a range that
// does not cover it refuses.
func TestModuleAddTakesAVersionRange(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bundleOnlyManifest(), false)

	addModuleFrom(t, configPath, "github.com/acme/nozzle-watch@^2.0.0", release, "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindAsset, "nozzle-watch")
	require.NoError(t, err)

	_, found := doc.Find(instance)
	require.True(t, found, "a caret range did not take the release it covers")

	var out bytes.Buffer

	err = Module(t.Context(), &out, []string{
		addVerb, "-config", configPath, "-from", release, "github.com/acme/nozzle-watch@^3.0.0",
	})
	require.ErrorContains(t, err, "which it does not hold")
}

// TestModuleAddVerifiesTheReleasesChecksumFile is the one provenance check a
// local release carries. A file changed after the release was built is refused
// before anything is copied.
func TestModuleAddVerifiesTheReleasesChecksumFile(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bundleOnlyManifest(), false)

	require.NoError(t, os.WriteFile(filepath.Join(release, checksumsName),
		[]byte("0000000000000000000000000000000000000000000000000000000000000000  ui/index.js\n"), 0o600))

	var out bytes.Buffer

	err := Module(t.Context(), &out,
		[]string{addVerb, "-config", configPath, "-from", release, "github.com/acme/nozzle-watch@2.1.0"})
	require.ErrorContains(t, err, checksumsName)
	require.NoFileExists(t, core.Overlay)
}

// TestModuleListAndRemoveReadAndUndoWhatAddWrote closes the loop. The listing
// is one line per module, and the remove takes the declaration and the files
// away together.
func TestModuleListAndRemoveReadAndUndoWhatAddWrote(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, bothHalvesManifest(), true)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")

	var listed bytes.Buffer

	require.NoError(t, Module(t.Context(), &listed, []string{listVerb, "-config", configPath}))
	require.Contains(t, listed.String(), "sink:spools")
	require.Contains(t, listed.String(), theNamespace+"/spools")

	// One line, not two. The header is the other line.
	require.Len(t, strings.Split(strings.TrimSpace(listed.String()), "\n"), 2)

	var removed bytes.Buffer

	require.NoError(t, Module(t.Context(), &removed, []string{removeVerb, "-config", configPath, theModulePath}))
	require.Contains(t, removed.String(), "removed sink:spools")

	require.NoDirExists(t, filepath.Join(core.Modules, "spools"))

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Instances)
}

// TestModuleAddRefusesToShadowABlockTheOperatorWrote keeps the two doors
// apart. The operator's file wins at load, so an overlay entry under a name
// they declared would be written and never run.
func TestModuleAddRefusesToShadowABlockTheOperatorWrote(t *testing.T) {
	t.Parallel()

	configPath, _ := installConfig(t)
	release := aRelease(t, bundleOnlyManifest(), false)

	body, err := os.ReadFile(configPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(configPath, append(body, []byte(strings.Join([]string{
		"resources:",
		"  asset:",
		"    nozzle-watch:",
		"      type: com.github.aux.acme.nozzle-watch/nozzle-watch",
		"      asset:",
		"        path: somewhere",
		"        checksum: sha256:" + strings.Repeat("0", 64),
		"",
	}, "\n"))...), 0o600))

	var out bytes.Buffer

	err = Module(t.Context(), &out,
		[]string{addVerb, "-config", configPath, "-from", release, "github.com/acme/nozzle-watch@2.1.0"})
	require.ErrorContains(t, err, configPath)
}

// manifestWithACard is a module whose manifest carries the three keys a
// marketplace card prints.
func manifestWithACard() string {
	return strings.Join([]string{
		"name: spools",
		"kind: sink",
		"license: MIT",
		"version: 1.4.0",
		"summary: Counts what each print used and warns before a spool runs out.",
		"codeowner: acme",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"",
	}, "\n")
}

// TestAnInstallRecordsWhatTheManifestSaidAboutItself is the old backlog. The
// release directory is staged and thrown away and a module that runs a process
// keeps no manifest on disk, so what a card prints is copied into the
// declaration while the manifest is still readable.
func TestAnInstallRecordsWhatTheManifestSaidAboutItself(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	addModuleFrom(t, configPath, theModulePath+"@1.4.0", aRelease(t, manifestWithACard(), true), "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.NotNil(t, declared.Spec.Module, "the declaration carries nothing a card could print")
	require.Equal(t, "1.4.0", declared.Spec.Module.Version)
	require.Equal(t, "Counts what each print used and warns before a spool runs out.",
		declared.Spec.Module.Summary)
	require.Equal(t, "acme", declared.Spec.Module.Author)
}

// TestAManifestWithNoCardKeysInstallsAnyway keeps the three optional. A module
// that says only what it is called and what release it is stays installable,
// and its card prints what there is.
func TestAManifestWithNoCardKeysInstallsAnyway(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)

	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, bothHalvesManifest(), true), "-approve")

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)
	require.NotNil(t, declared.Spec.Module)
	require.Equal(t, "1.0.0", declared.Spec.Module.Version)
	require.Empty(t, declared.Spec.Module.Summary)
	require.Empty(t, declared.Spec.Module.Author)
}

// linkModuleManifest is a module declared under the link kind: a process, no
// browser half, and the permission a link needs to see what it reports on.
func linkModuleManifest() string {
	return strings.Join([]string{
		"name: spools",
		"kind: link",
		"license: MIT",
		"version: 1.0.0",
		"contract_major: 1",
		"platforms:",
		"  - " + runtime.GOOS + "_" + runtime.GOARCH,
		"permissions:",
		"  - observe",
		"",
	}, "\n")
}

// TestModuleAddWritesALinkDeclaration is the install half of a link that runs
// in a process of its own: the manifest names the kind, the command writes the
// declaration under it, and the loader reads it back as a service instance.
func TestModuleAddWritesALinkDeclaration(t *testing.T) {
	t.Parallel()

	configPath, core := installConfig(t)
	release := aRelease(t, linkModuleManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, "-approve")
	require.Contains(t, said, theNamespace)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindLink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found, "the overlay holds no link declaration for the module")
	require.Equal(t, theNamespace+"/spools", declared.Spec.Type)
	require.True(t, declared.Spec.Enabled)

	require.NotNil(t, declared.Spec.Service)
	require.Equal(t, 1, declared.Spec.Service.ContractMajor)
	require.FileExists(t,
		filepath.Join(core.Modules, "spools", binaryDirName, thisPlatformBinary()))
}

// TestModuleRemoveTakesKindAndName is the spelling the refusal of an ambiguous
// name lists: one name in two kinds is ambiguous, and kind:name resolves it.
func TestModuleRemoveTakesKindAndName(t *testing.T) {
	t.Parallel()

	sink, err := ref.New(ref.KindSink, "stats")
	require.NoError(t, err)
	asset, err := ref.New(ref.KindAsset, "stats")
	require.NoError(t, err)

	rows := []resource.Declaration{
		{Ref: sink, Spec: resource.Spec{Type: "com.github.aux.acme.stats/stats"}},
		{Ref: asset, Spec: resource.Spec{Type: "com.github.aux.acme.statsui/stats"}},
	}

	_, err = moduleNamed(rows, "stats")
	require.ErrorContains(t, err, "sink:stats")

	named, err := moduleNamed(rows, "sink:stats")
	require.NoError(t, err)
	require.Equal(t, sink, named.Ref)
}

// TestModuleRemoveNamesTheRemovalCall is the notice a removal prints while a
// core answers: the call that removes at once, not the one that installs.
func TestModuleRemoveNamesTheRemovalCall(t *testing.T) {
	t.Parallel()

	configPath, _ := installConfig(t)
	addModuleFrom(t, configPath, theModulePath+"@1.0.0", aRelease(t, bothHalvesManifest(), true), "-approve")

	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(answering.Close)

	written, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte(strings.Replace(string(written),
		"127.0.0.1:1", strings.TrimPrefix(answering.URL, "http://"), 1)), 0o600))

	var removed bytes.Buffer

	require.NoError(t, Module(t.Context(), &removed, []string{removeVerb, "-config", configPath, "sink:spools"}))
	require.Contains(t, removed.String(), "removed sink:spools")
	require.Contains(t, removed.String(), "DELETE /api/v1/modules/"+theNamespace+",")
	require.NotContains(t, removed.String(), "POST /api/v1/modules")
}
