package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The three files that have to agree about what the image runs.
const (
	dockerfilePath = "docker/server/Dockerfile"
	servicePath    = "docker/server/server.yml"
	commandPath    = "compose/compose.go"
	nonRootPath    = "scripts/check-container-nonroot.sh"
)

// coreCommandPattern reads the word the command routes to the core from the
// command's own source. The gate compares the image against that word rather
// than against a copy of it, so renaming the subcommand fails here instead of
// shipping an image whose CMD names a subcommand nothing answers.
var coreCommandPattern = regexp.MustCompile(`coreCommand\s*=\s*"([^"]+)"`)

// probeCommandPattern reads the word that runs the probe, for the same reason.
var probeCommandPattern = regexp.MustCompile(`probeCommand\s*=\s*"([^"]+)"`)

// execFormPattern matches a Dockerfile or compose instruction written in exec
// form, and captures the arguments between the brackets.
var execFormPattern = regexp.MustCompile(`\[([^\]]*)\]`)

// installPattern matches the Dockerfile line that creates the runtime
// directories, and captures the list of them.
var installPattern = regexp.MustCompile(`install -d -o prusa -g prusa -m \d+ (.*)`)

// ownedFloorPattern matches the floor the non-root gate declares over the
// directories it reads out of the Dockerfile.
var ownedFloorPattern = regexp.MustCompile(`OWNED_FLOOR=(\d+)`)

// TestTheImageRunsTheCore holds what the container is for. The entry point is
// the binary, which also answers to `serve` and reaches the retired server
// there, so the CMD line is what says which of the two this image is.
//
// The visible symptom when it is wrong is a 404 at /docs, /openapi.json and
// every /api/v1 path, on a container that reports itself healthy.
func TestTheImageRunsTheCore(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	core, _ := subcommands(t, root)

	cmd := execArguments(t, instruction(t, read(t, root, dockerfilePath), "CMD"))

	if len(cmd) != 1 || cmd[0] != core {
		t.Errorf("%s runs %q, and the core is reached by %q.\n"+
			"The entry point is the binary, which also answers to the retired server's own\n"+
			"word, and that server serves 404 at /docs, /openapi.json and every /api/v1 path.",
			dockerfilePath, strings.Join(cmd, " "), core)
	}
}

// TestTheHealthCheckProbesTheServerTheImageRuns is the other half of the same
// mistake. A health check pointed at the shipping server's /health answers 200
// from a process the core never started, and a container that reports healthy
// while serving nothing is worse than one with no probe at all.
func TestTheHealthCheckProbesTheServerTheImageRuns(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	core, probe := subcommands(t, root)
	want := []string{core, probe}

	image := execArguments(t, instruction(t, read(t, root, dockerfilePath), "HEALTHCHECK"))
	if !endsWith(image, want) {
		t.Errorf("%s probes with %q, and the core's probe is %q.\n"+
			"The shipping server's healthcheck asks for /health, which the core does not serve.",
			dockerfilePath, strings.Join(image, " "), strings.Join(want, " "))
	}

	service := execArguments(t, healthcheckTest(t, read(t, root, servicePath)))
	if !endsWith(service, want) {
		t.Errorf("%s probes with %q and %s probes with %q.\n"+
			"Compose overrides the image's own HEALTHCHECK, so the two disagreeing means the\n"+
			"stack is checked by whichever file the reader did not open.",
			servicePath, strings.Join(service, " "), dockerfilePath, strings.Join(image, " "))
	}
}

// TestEveryDirectoryTheImageCreatesIsProvedWritable keeps the two halves of the
// read-only root filesystem together. The image creates a directory owned by
// the runtime user; the gate proves that user owns and can write it. A
// directory added to one and not the other is a container that starts in CI and
// stops on a deployment with read_only set.
//
// The two lists this compared are one list now. The gate reads the `install -d`
// line out of the Dockerfile, so a directory added to the image is probed the
// day it appears. What is left to hold is the reader itself: the gate names the
// file it reads, and the floor it refuses under is the number that file
// creates. A floor below that number is how a derivation stops noticing a loss.
func TestEveryDirectoryTheImageCreatesIsProvedWritable(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	created := captureFields(t, installPattern, read(t, root, dockerfilePath), dockerfilePath)
	requireScanned(t, "directories "+dockerfilePath+" creates", len(created), 1,
		"Either the `install -d` line moved, or installPattern stopped matching it.")

	gate := read(t, root, nonRootPath)

	if !strings.Contains(gate, dockerfilePath) {
		t.Errorf("%s does not name %s, so it cannot be reading the set from it.\n"+
			"A list typed into the gate drifts from the image it claims to follow.",
			nonRootPath, dockerfilePath)
	}

	floor := capture(t, ownedFloorPattern, gate, nonRootPath)

	if floor != strconv.Itoa(len(created)) {
		t.Errorf("%s creates %d directories and %s refuses under a floor of %s.\n"+
			"The floor is what makes a directory dropped from the image a failure rather\n"+
			"than a shorter list, so the two are the same number.",
			dockerfilePath, len(created), nonRootPath, floor)
	}
}

// subcommands is the word that reaches the core and the word that runs its
// probe, read from the command that routes them.
func subcommands(t *testing.T, root string) (string, string) {
	t.Helper()

	source := read(t, root, commandPath)

	return capture(t, coreCommandPattern, source, commandPath),
		capture(t, probeCommandPattern, source, commandPath)
}

// instruction is the body of a Dockerfile instruction, with any continuation
// lines joined. It returns the last one, which is the one that takes effect.
func instruction(t *testing.T, content, keyword string) string {
	t.Helper()

	found := ""

	for _, line := range logicalLines(content) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == keyword {
			found = strings.TrimSpace(strings.TrimPrefix(line, keyword))
		}
	}

	if found == "" {
		t.Fatalf("%s declares no %s instruction", dockerfilePath, keyword)
	}

	return found
}

// logicalLines joins backslash continuations and drops comments, so an
// instruction written over several lines reads as one.
func logicalLines(content string) []string {
	var (
		lines   []string
		current strings.Builder
	)

	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if after, ok := strings.CutSuffix(line, "\\"); ok {
			current.WriteString(strings.TrimSpace(after) + " ")

			continue
		}

		current.WriteString(line)
		lines = append(lines, current.String())
		current.Reset()
	}

	return lines
}

// healthcheckTest is the compose service's healthcheck command.
func healthcheckTest(t *testing.T, content string) string {
	t.Helper()

	for line := range strings.SplitSeq(content, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "test:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "test:"))
		}
	}

	t.Fatalf("%s declares no healthcheck test", servicePath)

	return ""
}

// execArguments is the arguments of an exec-form command, unquoted. A shell-form
// command has no brackets and returns nothing, which fails the comparison the
// caller makes: a probe that goes through a shell is one whose exit code the
// shell decides.
func execArguments(t *testing.T, body string) []string {
	t.Helper()

	match := execFormPattern.FindStringSubmatch(body)
	if match == nil {
		return nil
	}

	var args []string

	for field := range strings.SplitSeq(match[1], ",") {
		trimmed := strings.Trim(strings.TrimSpace(field), `"`)
		if trimmed != "" {
			args = append(args, trimmed)
		}
	}

	return args
}

// endsWith reports whether args finishes with want. The binary and compose's
// own CMD word come first and are not what this gate is about.
func endsWith(args, want []string) bool {
	if len(args) < len(want) {
		return false
	}

	tail := args[len(args)-len(want):]
	for i := range want {
		if tail[i] != want[i] {
			return false
		}
	}

	return true
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

func capture(t *testing.T, pattern *regexp.Regexp, content, path string) string {
	t.Helper()

	match := pattern.FindStringSubmatch(content)
	if match == nil {
		t.Fatalf("%s no longer declares %s, so this gate has nothing to compare against", path, pattern)
	}

	return match[1]
}

func captureFields(t *testing.T, pattern *regexp.Regexp, content, path string) []string {
	t.Helper()

	return strings.Fields(capture(t, pattern, content, path))
}

func read(t *testing.T, root, path string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}
