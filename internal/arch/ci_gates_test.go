package arch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A gate that runs in no CI job is a gate that does not exist.
//
// .githooks/pre-push states the principle: a clone that never ran `make
// install-hooks` pushes with no hook at all, which is why CI runs the same
// target. On 2026-08-28 that principle held for five of the gate scripts and
// not for four. `grep -rn check-docs .github/` returned nothing, and
// scripts/check-container-probes.sh had a header sentence naming a CI job that
// never called it. The docs job had grown a second implementation of `make
// docs-check` instead: two `node` steps that skipped the stale-dist deletion
// and the two missing-toolchain refusals the shell gates carry.
//
// Nothing noticed, because nothing was asking. This is the thing that asks.
//
// It reads the workflows and the Makefile rather than a list of jobs, so a
// script wired through a new target is covered the day it appears, and a target
// CI stops calling fails the day it stops.
//
// The input set is every gate the tree can enumerate, and a gate is enumerable
// when its name says so. Two languages write one here: `scripts/check-*.sh` and
// `docs/scripts/check-*.mjs`. The second was outside this test until a reading
// on 2026-08-28 found it, and docs/scripts/check-links.mjs is the case that
// matters, because no workflow and no recipe names it. Only
// scripts/check-docs-links.sh does. So the closure follows a reached gate into
// the gates it calls, and a third .mjs gate wired to nothing fails here.
const (
	workflowsDir = ".github/workflows"
	makefileName = "Makefile"
)

// gateGlobs is the input set, one glob per place a gate is written, each with
// the floor that says the glob still matches something.
var gateGlobs = []struct {
	pattern string
	floor   int
}{
	{"scripts/check-*.sh", 9},
	{"docs/scripts/check-*.mjs", 2},
}

// gateScriptsCIExempt names a gate script that deliberately runs in no CI job,
// against the reason. A reason is required, because the whole failure mode here
// is a gate whose absence nobody had to argue for.
//
// It is checked both ways. An entry naming a script that no longer exists
// fails, and so does an entry naming a script CI does in fact run: a stale
// exception is how a list like this stops describing anything.
var gateScriptsCIExempt = map[string]string{
	"scripts/check-hooks.sh": "It asserts that git's hooks are installed and this repository's. " +
		"CI has no hooks, so asserting there that hooks work would assert a state CI " +
		"itself had to arrange. .githooks/pre-push runs it first, before every gate " +
		"whose running depends on the hook having run.",
	"scripts/check-docs-commit-scope.sh": "It reads the staged diff, and a runner stages " +
		"nothing: the index CI checks out is the commit, so `git diff --cached` there is " +
		"empty and the gate would report a tick over nothing. The commit-msg hook is the " +
		"only moment the staged set exists. internal/arch/docs_commit_scope_test.go runs " +
		"the script over five fixture diffs, and holds the hook against its name.",
	"scripts/check-release-changelog.sh": "It gates a tag, and CI cuts none. Its first check " +
		"refuses a fragment left in docs/changes/, which is the normal state of every push, " +
		"so a job running it would fail every push it judged. scripts/release.sh is its " +
		"caller, and internal/arch/release_changelog_test.go runs it over seven fixtures.",
}

// TestEveryGateScriptRunsInACIJob is the gate.
func TestEveryGateScriptRunsInACIJob(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	scripts := gateScripts(t, root)
	reached := scriptsCIReaches(t, root)

	for _, script := range scripts {
		reason, exempt := gateScriptsCIExempt[script]

		switch {
		case reached[script] && exempt:
			t.Errorf("%s is listed in gateScriptsCIExempt and CI runs it anyway.\n"+
				"The exception reads: %s\n"+
				"Delete the entry. An exception that describes nothing is one more thing to keep true.",
				script, reason)
		case !reached[script] && !exempt:
			t.Errorf("%s runs in no CI job.\n"+
				"No workflow under %s names it, and no `make` target a workflow calls reaches it.\n"+
				"A clone that never ran `make install-hooks` pushes with no hook, so a gate that\n"+
				"lives only in one is a gate a pull request can walk past. Wire it into a job, or\n"+
				"add it to gateScriptsCIExempt with the reason it is different.",
				script, workflowsDir)
		}
	}

	for script := range gateScriptsCIExempt {
		if _, err := os.Stat(filepath.Join(root, script)); err != nil {
			t.Errorf("gateScriptsCIExempt names %s, which is not in the tree: %v.\n"+
				"Delete the entry.", script, err)
		}
	}
}

// gateScripts is the input set: every gate, by its repository-relative
// slash-separated path.
func gateScripts(t *testing.T, root string) []string {
	t.Helper()

	var scripts []string

	for _, glob := range gateGlobs {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(glob.pattern)))
		if err != nil {
			t.Fatalf("globbing %s: %v", glob.pattern, err)
		}

		requireScanned(t, "gates matching "+glob.pattern, len(matches), glob.floor,
			"Either the directory moved, or the glob "+glob.pattern+" stopped matching.")

		for _, match := range matches {
			rel, relErr := filepath.Rel(root, match)
			if relErr != nil {
				t.Fatalf("relativising %s: %v", match, relErr)
			}

			scripts = append(scripts, filepath.ToSlash(rel))
		}
	}

	sort.Strings(scripts)

	return scripts
}

// The two ways a target gets called, and they are deliberately not the same
// pattern.
//
// A workflow step calls `make <target>`. A recipe calling another recipe must
// use $(MAKE), which is GNU make's own rule and what every recursive call in
// this Makefile does. Reading a bare `make` inside a recipe read the help text
// of `install-hooks`, which lists what the pre-push hook runs, and that one
// @echo made all three documentation gates look reachable from a job that never
// called them. The first version of this test passed for that reason.
var (
	workflowMakeInvocation = regexp.MustCompile(`\bmake\s+([A-Za-z0-9_][A-Za-z0-9_.-]*)`)
	recipeMakeInvocation   = regexp.MustCompile(`\$\(MAKE\)"?\s+([A-Za-z0-9_][A-Za-z0-9_.-]*)`)
)

// scriptsCIReaches is every gate a workflow runs, directly, through the `make`
// targets it calls, or through another gate that is itself reached.
func scriptsCIReaches(t *testing.T, root string) map[string]bool {
	t.Helper()

	scripts, _ := ciReach(t, root)

	return scripts
}

// targetsCIReaches is every `make` target a workflow runs, directly or as a
// prerequisite or a $(MAKE) call of one it runs.
func targetsCIReaches(t *testing.T, root string) map[string]bool {
	t.Helper()

	_, targets := ciReach(t, root)

	return targets
}

// ciReach walks the workflows out through the Makefile and the gates, and
// reports both halves of what it reached. The two questions share one walk
// because they share every edge.
func ciReach(t *testing.T, root string) (map[string]bool, map[string]bool) {
	t.Helper()

	workflows := readWorkflows(t, root)
	targets := makeTargets(t, root)

	// The targets CI names, then everything those reach. A target's recipe may
	// call another with $(MAKE), and its prerequisites run before it, so both
	// are edges.
	pending := namedTargets(workflowMakeInvocation, workflows)
	requireScanned(t, "make targets named by a workflow", len(pending), 5,
		"Either the workflows moved, or the `make <target>` pattern stopped matching them.")

	text := []string{workflows}
	seen := map[string]bool{}

	for len(pending) > 0 {
		target := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		if seen[target] {
			continue
		}

		seen[target] = true

		recipe, ok := targets[target]
		if !ok {
			continue
		}

		text = append(text, recipe.body)
		pending = append(pending, recipe.prerequisites...)
		pending = append(pending, namedTargets(recipeMakeInvocation, recipe.body)...)
	}

	// A gate may call a gate. scripts/check-docs-links.sh is the one that does,
	// and docs/scripts/check-links.mjs is named by nothing else in the tree. So
	// the text grows until it stops growing, and a gate reached only by a gate
	// that is itself unreachable stays unreached.
	gates := gateScripts(t, root)
	reached := map[string]bool{}

	for {
		joined := strings.Join(text, "\n")
		grew := false

		for _, gate := range gates {
			if reached[gate] || !strings.Contains(joined, gate) {
				continue
			}

			reached[gate] = true
			text = append(text, withoutComments(normaliseNewlines(readGate(t, root, gate))))
			grew = true
		}

		if !grew {
			return reached, seen
		}
	}
}

func readGate(t *testing.T, root, gate string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(gate)))
	if err != nil {
		t.Fatalf("reading %s: %v", gate, err)
	}

	return string(body)
}

func namedTargets(pattern *regexp.Regexp, text string) []string {
	var named []string

	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		named = append(named, match[1])
	}

	return named
}

// withoutComments drops the lines that describe a run rather than being one.
// A YAML comment naming a script, or a recipe's @echo naming one, is prose. The
// question this gate asks is what CI executes.
//
// `//` is here for the .mjs gates, and it earns its place: the header of
// scripts/check-docs-links.sh names the checker it runs in a comment as well as
// in its exec line, so a reader that counted prose would credit a gate for
// being mentioned.
func withoutComments(text string) string {
	var kept []string

	for line := range strings.SplitSeq(text, "\n") {
		bare := strings.TrimLeft(line, " \t@-")
		if strings.HasPrefix(bare, "#") || strings.HasPrefix(bare, "//") || strings.HasPrefix(bare, "echo ") {
			continue
		}

		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

// readWorkflows returns every workflow as one string. Line endings are
// normalised, because a fresh clone materialises these files with CRLF on
// Windows and a carriage return read as data is how a reader stops matching
// what it reads.
func readWorkflows(t *testing.T, root string) string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, workflowsDir))
	if err != nil {
		t.Fatalf("reading %s: %v", workflowsDir, err)
	}

	var (
		bodies []string
		read   int
	)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(root, workflowsDir, name))
		if readErr != nil {
			t.Fatalf("reading %s/%s: %v", workflowsDir, name, readErr)
		}

		read++

		bodies = append(bodies, withoutComments(normaliseNewlines(string(body))))
	}

	requireScanned(t, "workflow files", read, 2,
		"Either "+workflowsDir+" moved, or the .yml suffix did.")

	return strings.Join(bodies, "\n")
}

type makeRecipe struct {
	prerequisites []string
	body          string
}

// targetLine matches a Makefile rule's head. A recipe line begins with a tab,
// so a rule head never does.
var targetLine = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*:{1,2}(?:[^=]|$)`)

// makeTargets reads the Makefile into target, prerequisites and recipe. It is
// the Makefile's own text and not a list, for the reason internal/arch/recipes.go
// gives: a list typed into a test drifts from `make build` in silence.
func makeTargets(t *testing.T, root string) map[string]makeRecipe {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, makefileName))
	if err != nil {
		t.Fatalf("reading %s: %v", makefileName, err)
	}

	targets := map[string]makeRecipe{}

	var current string

	for line := range strings.SplitSeq(normaliseNewlines(string(body)), "\n") {
		if strings.HasPrefix(line, "\t") {
			if current != "" {
				recipe := targets[current]
				recipe.body += withoutComments(line) + "\n"
				targets[current] = recipe
			}

			continue
		}

		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		current = ""

		match := targetLine.FindStringSubmatch(line)
		if match == nil || match[1] == ".PHONY" {
			continue
		}

		// Everything after the colon is prerequisites, minus the ## help text
		// ADR-20260826-gtv asks every public target to carry.
		_, after, _ := strings.Cut(line, ":")
		after, _, _ = strings.Cut(after, "##")

		current = match[1]
		targets[current] = makeRecipe{
			prerequisites: strings.Fields(after),
			body:          targets[current].body,
		}
	}

	requireScanned(t, "Makefile targets", len(targets), 50,
		"Either the Makefile moved, or targetLine stopped matching a rule head.")

	return targets
}

func normaliseNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// A gate target that runs in no CI job, which is the same failure one level up.
//
// TestEveryGateScriptRunsInACIJob above reads `scripts/check-*.sh`. A gate
// written as a Makefile recipe rather than a script is outside it, and
// `extensions-check` was: CI ran `go test ./cmd/generate-extensions/`, which is
// step 2 of four, and nothing ran `-check` or diffed the committed catalogue
// against a regeneration. Its twin `error-codes-check` runs all four. So a
// committed extension artefact could go stale in a push, and the job that
// exists to ask whether it had reported green.
//
// A gate target is enumerable the way a gate script is: its name says so. Every
// target in this Makefile whose name ends in `-check` is one, which is the
// convention ADR-20260826-gtv rule 8 sets and 30 targets follow.
const gateTargetSuffix = "-check"

// gateTargetsCIExempt names a gate target that deliberately runs in no CI job,
// against the reason. It is checked both ways, on gateScriptsCIExempt's model.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var gateTargetsCIExempt = map[string]string{
	"hooks-check": "It runs scripts/check-hooks.sh, which gateScriptsCIExempt already " +
		"excuses: CI checks out with no hooks, so a job asserting hooks work would be " +
		"asserting its own setup.",
	"go-version-check": "The `Backend / Verify generated code` job runs " +
		"scripts/check-go-version.sh --refresh, which is this target's script plus the half " +
		"that needs the network. Offline the target answers from scripts/go-releases.tsv; " +
		"the job asks go.dev, so a release that appeared since the last commit still fails it.",
	"vuln-check": "It needs the vulnerability database over the network. CI runs " +
		"golang/govulncheck-action in the `Backend / Security scan` job, which is the same " +
		"question asked through the runner's own cache.",
	"docker-check": "It builds the image and drives a compose stack. The `Docker` job runs " +
		"the same three scripts as separate steps against one build, so the image is built " +
		"once for the job rather than once for this target.",
	"mockup-check": "It builds one image, starts four servers from it, and stops and starts " +
		"them again to prove a connection comes back. The whole run takes minutes of a " +
		"runner's time and proves what the Fractal module's own tests already prove message " +
		"by message. A developer runs it after changing the module or the mockup.",
	"client-ui-check": "It runs the four component library gates one after the other for a " +
		"developer. CI runs each of the four as its own target, client-ui-lint, " +
		"client-ui-svelte-check, client-ui-test and client-ui-storybook, in three jobs side by " +
		"side, because one after the other they took seven minutes of a two-core runner.",
}

// TestEveryGateTargetRunsInACIJob is the gate.
func TestEveryGateTargetRunsInACIJob(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	targets := makeTargets(t, root)
	reached := targetsCIReaches(t, root)

	var gates []string

	for name := range targets {
		if strings.HasSuffix(name, gateTargetSuffix) {
			gates = append(gates, name)
		}
	}

	sort.Strings(gates)
	requireScanned(t, "Makefile targets named `*"+gateTargetSuffix+"`", len(gates), 25,
		"Either the Makefile moved, or this project stopped naming a gate target for what it is.")

	for _, gate := range gates {
		reason, exempt := gateTargetsCIExempt[gate]

		switch {
		case reached[gate] && exempt:
			t.Errorf("`make %s` is listed in gateTargetsCIExempt and CI runs it anyway.\n"+
				"The exception reads: %s\n"+
				"Delete the entry. An exception that describes nothing is one more thing to keep true.",
				gate, reason)
		case !reached[gate] && !exempt:
			t.Errorf("`make %s` runs in no CI job.\n"+
				"No workflow under %s calls it, and no target a workflow calls names it as a\n"+
				"prerequisite or reaches it with $(MAKE). A gate that lives only in a local hook\n"+
				"is one a pull request can walk past. Wire it into a job, or add it to\n"+
				"gateTargetsCIExempt with the reason it is different.",
				gate, workflowsDir)
		}
	}

	for gate := range gateTargetsCIExempt {
		if _, declared := targets[gate]; !declared {
			t.Errorf("gateTargetsCIExempt names `%s`, which the Makefile does not declare.\n"+
				"Delete the entry.", gate)
		}
	}
}
