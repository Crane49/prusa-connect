package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The compose files that define the server service. A name set in either one
// reaches the container.
var serverComposeFiles = []string{ //nolint:gochecknoglobals // the file set this gate reads.
	"docker/server/server.yml",
	"docker/server/server.local.yml",
}

// serverService is the service both files define, and the block this gate reads
// the environment out of.
const serverService = "prusa-connect"

// environmentReaderPattern captures the literal name in an os.Getenv or an
// os.LookupEnv call. A name assembled at run time is not a literal, so this gate
// cannot see it: that is why an unread key is refused here and recorded in
// unreadEnvironmentKeys rather than warned about.
var environmentReaderPattern = regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("([^"]+)"\)`)

// shellReaderPattern captures a name a shell script reads, in both spellings.
// An image entrypoint is the other thing that can consume a variable set on the
// service.
var shellReaderPattern = regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)`)

// treesWithNoSource are the directories the reader search skips. Each holds
// installed or built files rather than sources, and walking them costs seconds.
var treesWithNoSource = map[string]bool{ //nolint:gochecknoglobals // a fixed skip set.
	".git": true, "node_modules": true, "bin": true, "dist": true, "certs": true,
}

// unreadEnvironmentKeys holds an environment key the compose files set that
// nothing reads, against the reason it stays. Earlier research wants this
// empty, and an empty map is what makes a green run mean the tree obeys it.
var unreadEnvironmentKeys = map[string]string{} //nolint:gochecknoglobals // the allowance list.

// TestEveryEnvironmentKeyTheServerServiceSetsIsRead is earlier research. The
// core is configured by one file and its flags and reads no environment
// variable, which is a design requirement, so a name set on the service reaches the
// process and changes nothing there.
//
// The failure it exists for is silent on both sides: an operator sets the key,
// the behaviour does not move, and no log line says why. `LOG_LEVEL` was set by
// both files and read by nothing.
func TestEveryEnvironmentKeyTheServerServiceSetsIsRead(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	readers := environmentNamesRead(t, root)

	var unread []string

	for _, path := range serverComposeFiles {
		for _, key := range composeEnvironmentKeys(t, read(t, root, path), path) {
			if readers[key] {
				continue
			}

			if _, allowed := unreadEnvironmentKeys[key]; allowed {
				continue
			}

			unread = append(unread, path+" sets "+key)
		}
	}

	if len(unread) > 0 {
		t.Errorf("%s\n"+
			"Nothing in this repository reads those names: no os.Getenv call outside a test,\n"+
			"and no script under docker/. The core takes its configuration from\n"+
			"config/config.yaml and its flags. Delete the key, or add the reader.",
			strings.Join(unread, "\n"))
	}
}

// TestTheEnvironmentGateReadsBothHalvesOfWhatItCompares is the positive control.
// The gate above passes when the compose files set nothing, so on its own it
// cannot tell an obedient tree from a parser that returns nothing and a reader
// search that finds nothing. This runs both halves over fixtures that hold a
// known answer.
func TestTheEnvironmentGateReadsBothHalvesOfWhatItCompares(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "package fixture\n\nfunc read() string { return os.Getenv(\"PROVE_THE_SEARCH_RUNS\") }\n"

	if err := os.WriteFile(filepath.Join(dir, "reader.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("writing the reader fixture: %v", err)
	}

	if readers := environmentNamesRead(t, dir); !readers["PROVE_THE_SEARCH_RUNS"] {
		t.Error("the reader search missed an os.Getenv call with a literal name, so it would " +
			"report every key unread or every key read depending on nothing")
	}

	mapping := "services:\n  " + serverService + ":\n    environment:\n      FROM_A_MAPPING: value\n"
	list := "services:\n  " + serverService + ":\n    environment:\n      - FROM_A_LIST=value\n"

	for _, fixture := range []struct{ name, content, want string }{
		{"mapping", mapping, "FROM_A_MAPPING"},
		{"list", list, "FROM_A_LIST"},
	} {
		keys := composeEnvironmentKeys(t, fixture.content, fixture.name)
		if !contains(keys, fixture.want) {
			t.Errorf("the %s form of an environment block yielded %v, and %s is set in it",
				fixture.name, keys, fixture.want)
		}
	}
}

// composeEnvironmentKeys is the names an environment block sets on the server
// service. A file that declares no such service fails the run: the gate reading
// nothing and the tree setting nothing look the same from the outside, and only
// one of them is correct.
func composeEnvironmentKeys(t *testing.T, content, path string) []string {
	t.Helper()

	var document struct {
		Services map[string]struct {
			Environment yaml.Node `yaml:"environment"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("%s does not parse as yaml: %v", path, err)
	}

	service, declared := document.Services[serverService]
	if !declared {
		t.Fatalf("%s declares no %q service, so this gate compares nothing", path, serverService)
	}

	return environmentNames(t, service.Environment, path)
}

// environmentNames reads a compose environment block in either spelling: a
// mapping of name to value, or a list of NAME=value strings.
func environmentNames(t *testing.T, node yaml.Node, path string) []string {
	t.Helper()

	if node.Kind == 0 {
		return nil
	}

	var names []string

	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			names = append(names, node.Content[i].Value)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			name, _, _ := strings.Cut(item.Value, "=")
			names = append(names, name)
		}
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		t.Fatalf("%s: environment is neither a mapping nor a list", path)
	}

	return names
}

// environmentNamesRead is every name something under root reads: a literal in an
// os.Getenv or os.LookupEnv call outside a test, and a name a shell script under
// docker/ expands. Those are the two things that can consume a variable set on
// the service.
func environmentNamesRead(t *testing.T, root string) map[string]bool {
	t.Helper()

	found := map[string]bool{}

	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if treesWithNoSource[entry.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		pattern, reads := readerPatternFor(root, path, entry.Name())
		if !reads {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		for _, match := range pattern.FindAllStringSubmatch(string(content), -1) {
			found[match[1]] = true
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s for environment readers: %v", root, walkErr)
	}

	return found
}

// readerPatternFor is the pattern that finds a name in this file, and whether
// the file can read one at all. A _test.go file cannot: test plumbing is outside
// earlier research, and a fixture name there is not a reader.
func readerPatternFor(root, path, name string) (*regexp.Regexp, bool) {
	switch {
	case strings.HasSuffix(name, "_test.go"):
		return nil, false
	case strings.HasSuffix(name, ".go"):
		return environmentReaderPattern, true
	case strings.HasSuffix(name, ".sh") && underDocker(root, path):
		return shellReaderPattern, true
	default:
		return nil, false
	}
}

// underDocker reports whether a path sits in the docker/ tree, which is where an
// image entrypoint would live.
func underDocker(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return strings.HasPrefix(filepath.ToSlash(relative), "docker/")
}
