package arch_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/arch"
)

// adapters/tags.json is the airlock between what an operator configures and
// what the compiler is asked to do. It only works while it is true of this
// tree, and nothing about writing a configuration file makes it true.
//
// Two ways it goes wrong, and both are silent. An adapter added to the tree
// with no entry stops being able to influence the build, which is the defect
// the airlock closes, arriving in a new place. An entry naming a
// type nothing registers passes a tag for code that is not there, and a build
// tag that compiles nothing is not an error the compiler reports.
//
// The catalogue this checks against is docs/extensions/extensions.json, which
// make extensions-check already holds to the live registries. This package may
// import nothing under internal/, so the chain is registry to catalogue to
// airlock, each link gated.
const (
	airlockPath   = "adapters/tags.json"
	cataloguePath = "docs/extensions/extensions.json"
)

// configurableKinds is the closed set of kinds an operator declares instances
// of, and the extension point each one serves. It is the table at
// internal/kernel/ref and internal/extension/registry/registry.go:85, repeated
// because ADR-20260824-kkr gives this package no imports under internal/.
//
//nolint:gochecknoglobals // a closed lookup table, never written to.
var configurableKinds = map[string]string{
	"storage": "storage",
	"auth":    "identity",
	"sink":    "event-sink",
	"driver":  "connector",
	"db":      "database",
	"camera":  "camera",
	"history": "history",
	"link":    "link",
}

// airlockFile is adapters/tags.json.
type airlockFile struct {
	Version   int `json:"version"`
	Resources []struct {
		Kind string `json:"kind"`
		Type string `json:"type"`
		Tag  string `json:"tag"`
	} `json:"resources"`
	Conditions []struct {
		Condition string `json:"condition"`
		Tag       string `json:"tag"`
	} `json:"conditions"`
}

// catalogueEntry is the part of a published extension entry this gate reads.
type catalogueEntry struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// TestTheAirlockCoversEveryRegisteredType fails when an entry names a type
// nothing registers, so a tag cannot be passed for code that is not here.
//
// It does not fail in the other direction. ADR-20260901-7t4 rule 11 holds the airlock
// to what it is for: it maps a type onto a build tag, an untagged adapter
// carries no build-relevant fact, and demanding a "tag": "" row for one would
// make adding an adapter a core edit for nothing. A composed adapter never appears here at
// all, because a composed adapter takes no build tag: a tag can only gate code
// already in the module graph, and the build configuration has already decided
// presence.
//
// What still holds the other direction is TestEveryTaggedAdapterDeclaresItsTag
// below, which reads the constraints in the tree rather than a list beside it.
func TestTheAirlockCoversEveryRegisteredType(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	airlock := readAirlock(t, root)
	catalogue := readCatalogue(t, root)

	declared := map[string]bool{}
	for _, entry := range airlock.Resources {
		declared[entry.Kind+" "+entry.Type] = true
	}

	registered := map[string]bool{}

	for _, entry := range catalogue {
		kind, serves := resourceKindOf(entry.Kind)
		if !serves {
			// routes is an extension point with no declared instances.
			continue
		}

		registered[kind+" "+entry.Namespace+"/"+entry.Name] = true
	}

	requireScanned(t, "types of a configurable kind in "+cataloguePath, len(registered), 1,
		"Run make generate-extensions.")

	var violations []string

	for name := range declared {
		if !registered[name] {
			violations = append(violations, fmt.Sprintf(
				"%s maps %s and nothing in %s registers it, so its tag would compile nothing",
				airlockPath, name, cataloguePath))
		}
	}

	refuse(t, violations)
}

// TestTheAirlockNamesOnlyThingsTheBuildKnows holds each half of an entry to a
// closed set: a kind an operator can declare, a type qualified the way the
// registry mints one, and a tag some build recipe actually passes.
func TestTheAirlockNamesOnlyThingsTheBuildKnows(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	airlock := readAirlock(t, root)

	if airlock.Version != 1 {
		t.Fatalf("%s is format %d and this gate reads 1", airlockPath, airlock.Version)
	}

	requireScanned(t, "resource entries in "+airlockPath, len(airlock.Resources), 1)
	requireScanned(t, "condition entries in "+airlockPath, len(airlock.Conditions), 1)

	known := knownTags(t, root)

	var violations []string

	for _, entry := range airlock.Resources {
		if _, ok := configurableKinds[entry.Kind]; !ok {
			violations = append(violations, fmt.Sprintf("kind %q is not one an operator declares instances of",
				entry.Kind))
		}

		if strings.Count(entry.Type, "/") != 1 {
			violations = append(violations, fmt.Sprintf(
				"type %q is not qualified as namespace/name, which is the only form unique by construction",
				entry.Type))
		}

		violations = append(violations, unknownTag(entry.Tag, entry.Kind+" "+entry.Type, known)...)
	}

	// A tag that takes code out, such as nodashboards, is passed by no recipe
	// literally: the configuration implies it. It is known when the tree has
	// code it leaves out, which is the only thing such a tag can do.
	removing := negatedTags(t, root)

	for _, entry := range airlock.Conditions {
		if strings.HasPrefix(entry.Tag, "no") && removing[entry.Tag] {
			continue
		}

		violations = append(violations, unknownTag(entry.Tag, "condition "+entry.Condition, known)...)
	}

	refuse(t, violations)
}

// negatedTags is every tag a //go:build line under internal/ negates, which is
// every tag whose presence leaves code out of the binary.
func negatedTags(t *testing.T, root string) map[string]bool {
	t.Helper()

	out := map[string]bool{}

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		first, _, _ := strings.Cut(string(content), "\n")

		found := buildConstraint.FindStringSubmatch(strings.TrimSpace(first))
		if found == nil {
			return nil
		}

		for _, negated := range negatedTerm.FindAllStringSubmatch(found[1], -1) {
			out[negated[1]] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("reading the build constraints under internal/: %v", err)
	}

	requireScanned(t, "tags a build constraint under internal/ negates", len(out), 1)

	return out
}

// negatedTerm is one negated tag in a build constraint.
var negatedTerm = regexp.MustCompile(`!([A-Za-z_][A-Za-z0-9_.]*)`)

// TestNoConfigurationFileNamesABuildTag is the hazard the airlock exists for. A
// configuration file is operator input and a build tag is a compiler argument,
// so the shipped configurations must not carry a key whose value is a tag.
func TestNoConfigurationFileNamesABuildTag(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	known := knownTags(t, root)

	// The tags that are also ordinary configuration values: a persistence type
	// really is called postgres, and the airlock is what turns that into a tag.
	values := map[string]bool{"postgres": true, "mqtt": true, "full": true,
		"minimal": true, "observability": true}

	files, err := filepath.Glob(filepath.Join(root, "config", "*.yaml"))
	if err != nil {
		t.Fatalf("listing the shipped configurations: %v", err)
	}

	examples, err := filepath.Glob(filepath.Join(root, "config", "examples", "*.yaml"))
	if err != nil {
		t.Fatalf("listing the example configurations: %v", err)
	}

	files = append(files, examples...)

	requireScanned(t, "shipped or example configuration files", len(files), 1)

	var violations []string

	for _, file := range files {
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("reading %s: %v", file, readErr)
		}

		for line := range strings.SplitSeq(string(content), "\n") {
			key, value, found := strings.Cut(strings.TrimSpace(hashComment(line)), ":")
			if !found {
				continue
			}

			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if !known[value] || values[value] || !strings.Contains(key, "tag") {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s names the build tag %q under %q",
				filepath.Base(file), value, key))
		}
	}

	refuse(t, violations)
}

// refuse fails with every violation at once, so one run says everything that is
// wrong rather than one thing at a time.
func refuse(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d statement(s) break the airlock, earlier research:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}

// knownTags is every tag a build recipe names literally: the Makefile's
// FULL_TAGS, which is the widest set the project compiles, and the release
// image's own list.
//
// It deliberately does not use arch.TagSets. That runs detect-build-tags, which
// reads the airlock, so a made-up tag in the airlock would appear in the answer
// and this gate would accept it because it had just produced it. A gate reading
// its own output passes everything, which a made-up tag planted in the airlock
// proves here.
func knownTags(t *testing.T, root string) map[string]bool {
	t.Helper()

	all, err := arch.FileValue(filepath.Join(root, "Makefile"), arch.FullTagsPattern, "FULL_TAGS")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	release, err := arch.ReleaseImageTags(root)
	if err != nil {
		t.Fatalf("reading the release build: %v", err)
	}

	known := map[string]bool{}

	for _, set := range []string{arch.ExpandMakeVars(all), release} {
		for tag := range strings.FieldsSeq(set) {
			known[tag] = true
		}
	}

	requireScanned(t, "build tags named literally by a recipe", len(known), 1,
		"FULL_TAGS in the Makefile and the release image's own list are what it reads.")

	return known
}

// unknownTag reports a tag no recipe passes. An empty tag is the common case:
// most adapters here compile unconditionally.
func unknownTag(tag, what string, known map[string]bool) []string {
	if tag == "" || known[tag] {
		return nil
	}

	return []string{fmt.Sprintf("%s maps onto the tag %q, which no build recipe passes", what, tag)}
}

// resourceKindOf reverses the extension point back onto the kind an operator
// writes in configuration.
func resourceKindOf(point string) (string, bool) {
	kinds := slices.Sorted(func(yield func(string) bool) {
		for kind := range configurableKinds {
			if !yield(kind) {
				return
			}
		}
	})

	for _, kind := range kinds {
		if configurableKinds[kind] == point {
			return kind, true
		}
	}

	return "", false
}

func readAirlock(t *testing.T, root string) airlockFile {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(airlockPath)))
	if err != nil {
		t.Fatalf("reading %s: %v", airlockPath, err)
	}

	var file airlockFile
	if err = json.Unmarshal(content, &file); err != nil {
		t.Fatalf("parsing %s: %v", airlockPath, err)
	}

	return file
}

func readCatalogue(t *testing.T, root string) []catalogueEntry {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cataloguePath)))
	if err != nil {
		t.Fatalf("reading %s: %v", cataloguePath, err)
	}

	var file struct {
		Extensions []catalogueEntry `json:"extensions"`
	}

	if err = json.Unmarshal(content, &file); err != nil {
		t.Fatalf("parsing %s: %v", cataloguePath, err)
	}

	sort.Slice(file.Extensions, func(i, j int) bool {
		return file.Extensions[i].Name < file.Extensions[j].Name
	})

	return file.Extensions
}

// hashComment strips a trailing comment, so a tag parked in one is not read as
// a value.
func hashComment(line string) string {
	before, _, _ := strings.Cut(line, "#")

	return before
}

// buildConstraint matches a Go build constraint line and captures what follows.
var buildConstraint = regexp.MustCompile(`^//go:build\s+(.*)$`)

// tagWord matches an identifier inside a build constraint, which is every tag
// name in it plus the operators' operands. The operators themselves are
// punctuation, so they do not match.
var tagWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*`)

// TestEveryTaggedAdapterDeclaresItsTag is the direction ADR-20260901-7t4 rule 11 keeps.
//
// An adapter behind a build tag is code an operator's configuration has to be
// able to reach, and the airlock is the only thing that turns a configured type
// into a compiler argument. A tag in this tree that the airlock never names is
// an adapter that silently cannot be built, which is the defect the airlock
// exists to close. An untagged adapter needs no row and no longer gets one
// demanded of it, so this reads the constraints rather than counting rows.
//
// It reads the tree and not the catalogue on purpose: a tag is a property of
// the source, and the catalogue is built from a binary that was compiled with
// some particular set of them.
func TestEveryTaggedAdapterDeclaresItsTag(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	airlock := readAirlock(t, root)

	declared := map[string]bool{}

	for _, entry := range airlock.Resources {
		if entry.Tag != "" {
			declared[entry.Tag] = true
		}
	}

	for _, entry := range airlock.Conditions {
		if entry.Tag != "" {
			declared[entry.Tag] = true
		}
	}

	var (
		scanned    int
		violations []string
	)

	err := filepath.WalkDir(filepath.Join(root, "adapters"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		scanned++

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		relative, _ := filepath.Rel(root, path)

		for line := range strings.SplitSeq(string(content), "\n") {
			found := buildConstraint.FindStringSubmatch(strings.TrimSpace(line))
			if found == nil {
				continue
			}

			for _, tag := range tagWord.FindAllString(found[1], -1) {
				if !declared[tag] {
					violations = append(violations, fmt.Sprintf(
						"%s is behind the build tag %q and %s names no entry carrying it, "+
							"so no configuration can ask for that code",
						filepath.ToSlash(relative), tag, airlockPath))
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking adapters/: %v", err)
	}

	requireScanned(t, "Go files under adapters/", scanned, 10)

	refuse(t, violations)
}
