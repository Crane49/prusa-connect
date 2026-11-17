package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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

// The catalogue is filled by package initialisation, so an entry only reaches
// the artefacts if this command imports the package that registers it. The
// vanished-entry check and the committed-output check both compare the
// catalogue against the reference, so they agree with each other about a
// catalogue that is missing entries. This is the gate that looks outside that
// agreement.
//
// It runs the code rather than reading the source for calls that look like
// registrations. Every package that could register anything is linked into one
// program, and what that program's catalogue then holds is compared with what
// the committed artefact documents. A registration has one spelling at run time
// and many in source: an aliased import, a house wrapper called through, or a
// generic instantiation, any of which a source scan misses or attributes to the
// wrong package.
//
// A test package is excluded because an extension registered by a test is not a
// product extension. A main package is excluded because nothing can import one,
// which is the half of the gate TestNoMainPackageRegistersAnExtension covers.

const modulePath = "github.com/prusa3d/prusa-connect"

func TestEveryRegisteredExtensionIsDocumented(t *testing.T) {
	t.Parallel()

	// The probe links and runs the whole tree once per shipping tag set, so
	// what it costs is a full build rather than this package's own code. Under
	// -race the parent's build cache holds instrumented objects the child
	// build cannot reuse, so every tag set compiles the tree again and the
	// test runs past the 11 minute per-package timeout on a hosted runner.
	// -short skips it, which is what -short is for. It still runs without
	// -race: `make extensions-check` and CI's generated-code job both call it.
	if testing.Short() {
		t.Skip("links the whole tree once per tag set, so it runs without -short")
	}

	root := repoRootDir(t)

	documented := map[string]bool{}

	committed, err := readCatalogueFile(filepath.Join(root, defaultCataloguePath))
	if err != nil {
		t.Fatalf("reading the committed catalogue: %v", err)
	}

	for _, entry := range committed {
		documented[entry.String()] = true
	}

	if len(documented) == 0 {
		t.Fatalf("%s documents nothing, so this gate is comparing against an empty list", defaultCataloguePath)
	}

	registered := extensionsTheTreeRegisters(t, root)

	var undocumented []string

	for _, name := range registered {
		if !documented[name] {
			undocumented = append(undocumented, name)
		}
	}

	if len(undocumented) == 0 {
		return
	}

	t.Errorf("%d extension entr(ies) register at initialisation and are in neither artefact:\n\t%s\n\n"+
		"An operator configuring one of these finds nothing in the published reference, and the\n"+
		"vanished-entry gate will not notice when it disappears. Blank-import the package that\n"+
		"registers each one in cmd/generate-extensions/main.go and regenerate.",
		len(undocumented), strings.Join(undocumented, "\n\t"))
}

// TestNoMainPackageRegistersAnExtension is the one thing the link graph cannot
// reach.
//
// Every other registration is found by linking the package that holds it into a
// probe. A main package cannot be linked into anything, because nothing can
// import one, so a registration in cmd/prusa-connect ships in the binary,
// answers a real operator's configuration, and appears in neither artefact.
// Every gate stays green.
//
// So this one reads the source, which is the weaker instrument and the only one
// available. Any call to Register, TryRegister or RegisterFactory counts inside
// a main package, whatever it is spelled on.
func TestNoMainPackageRegistersAnExtension(t *testing.T) {
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
			t.Errorf("cmd/%s is a main package and registers an extension at %s.\n"+
				"Nothing can import a main package, so that entry reaches an operator's configuration\n"+
				"and is in neither docs/extensions/extensions.json nor the published reference, and the\n"+
				"vanished-entry gate will never notice it go. Move the registration into a library\n"+
				"package and blank-import that package in cmd/generate-extensions/main.go.",
				entry.Name(), where)
		}
	}

	if mains == 0 {
		t.Fatal("no main package under cmd/, so this gate is reading nothing")
	}
}

// extensionsTheTreeRegisters links every package that could register and reports
// what the catalogue holds once their initialisers have run.
//
// It is done once per tag set, because a registration behind a build tag is
// still a registration in the binary that tag ships in.
//
// The listings run concurrently and the probes one after another. A listing
// compiles nothing. A probe compiles the first-party tree under its tags, and
// most of that tree compiles the same under every set, so a probe that runs
// after another finds those packages in the build cache. Run concurrently,
// each `go` process compiled them again for itself, and on two cores with a
// cache holding only the standard library and the dependencies, which is what
// CI starts from, this package took about 200 seconds rather than 40.
func extensionsTheTreeRegisters(t *testing.T, root string) []string {
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

		names, err := runRegistryProbe(root, candidates[i], tags)
		if err != nil {
			t.Fatalf("tags %q: %v", tags, err)
		}

		for _, name := range names {
			found[name] = true
		}
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}

	sort.Strings(names)

	// A gate that reads nothing passes everything, and this one compares two
	// lists: an empty left-hand side agrees with any right-hand side.
	// Registration runs at package initialisation, which is exactly the thing
	// that could stop happening without a compile error anywhere.
	if len(names) == 0 {
		t.Fatalf("nothing registered under any of %d tag set(s), so this gate is comparing an empty\n"+
			"catalogue against the committed artefact and will agree with anything. Registration runs\n"+
			"at package initialisation: check that the probe still links the candidates.", len(sets))
	}

	return names
}

func registryCandidatesUnder(root, tags string) ([]string, error) {
	candidates, err := registryCandidates(root, tags)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no package imports %s/sdk/extension, so this gate is reading nothing", modulePath)
	}

	return candidates, nil
}

// registryTagSets is the tag sets the build recipes compile under, from the same
// reader the import-graph gates use. A list written out here drifts from what
// `make build` compiles, which is the failure that let a `//go:build` file hide
// from the import gates.
func registryTagSets(t *testing.T, root string) []string {
	t.Helper()

	sets, err := arch.TagSets(root)
	if err != nil {
		t.Fatalf("reading the build recipes: %v", err)
	}

	return slices.Compact(slices.Sorted(maps.Values(sets)))
}

// registryCandidates is every package that could hold a registration: first
// party, not a main package, and reaching sdk/extension somewhere in its own
// imports. Nothing that cannot see the extension package can reach a
// registry, whatever the call is spelled like, so this over-approximates in the
// only safe direction and the probe decides the rest.
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

			if !slices.Contains(pkg.Deps, modulePath+"/sdk/extension") &&
				pkg.ImportPath != modulePath+"/sdk/extension" {
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
const probeDir = ".extension-registry-probe"

// runRegistryProbe compiles a program that blank-imports every candidate and
// prints what the catalogue then holds.
func runRegistryProbe(root string, candidates []string, tags string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "extension-probe-")
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
			"A panic here is usually two packages registering one qualified name. This probe links\n"+
			"every registering package at once, so it says the name is already taken somewhere in the\n"+
			"tree, not that any one binary would die at start-up. It is still a defect: an operator\n"+
			"writing that name in configuration cannot be told which of the two they got. Rename one",
			err, arch.StderrOf(err))
	}

	var names []string
	if decErr := json.Unmarshal(out, &names); decErr != nil {
		return nil, fmt.Errorf("decoding the probe's output: %w\n%s", decErr, out)
	}

	return names, nil
}

func renderProbe(candidates []string) string {
	var b strings.Builder

	b.WriteString("package main\n\nimport (\n\t\"encoding/json\"\n\t\"os\"\n\n")
	fmt.Fprintf(&b, "\t%q\n\n", modulePath+"/sdk/extension")

	for _, pkg := range candidates {
		if pkg == modulePath+"/sdk/extension" {
			continue
		}

		fmt.Fprintf(&b, "\t_ %q\n", pkg)
	}

	b.WriteString(")\n\nfunc main() {\n")
	b.WriteString("\tnames := []string{}\n")
	b.WriteString("\tfor _, entry := range extension.Global().All() {\n")
	b.WriteString("\t\tnames = append(names, entry.String())\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\tif err := json.NewEncoder(os.Stdout).Encode(names); err != nil {\n")
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
