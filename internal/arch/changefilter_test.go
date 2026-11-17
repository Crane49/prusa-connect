package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// No job skips because a change looks unrelated to it, unless the paths
// prove it.
//
// The owner set the first bar on 2026-08-27: "We should always run the test
// right, we don't want to find issues for 'unrelated' things to find out that
// they were related." On 2026-10-24 he narrowed it for pull requests: run only
// the checks that make sense for what the change touches, and weaken no gate.
//
// A change filter decides from the paths alone that a change cannot break a
// suite, and it is silent when it is wrong: a job that never started reports
// nothing. So ci.yml has one filter, the paths job `changes`, and it answers
// in areas. Each area holds every file its code reads, which
// ci_areas_test.go holds to the code. A job skips only on a pull request, and
// only when it is in pathGatedJobs, so CI OK knows which answer explains the
// skip.
//
// Three shapes skip work for the paths, and this gate refuses each one
// outside that design.
//
//  1. a change-detection action, whose output another job reads
//  2. a job-level `if:` reading another job's outputs, which is how the output
//     of such an action becomes a skip. Only a job in pathGatedJobs, in
//     ci.yml, may read the answer of `changes`, and only in a condition that
//     still runs it on every event that is not a pull request
//  3. `paths:` or `paths-ignore:` under `on:`, where the workflow does not start
//
// One workflow may filter by path: extensions.yml, named in
// triggerFilterAllowed. The first-party extensions under extensions/ are
// shipped and versioned apart from the core, and running their gates on every
// core change would lengthen the pipeline every core pull request waits for.
// The exception is narrow. Only rule 3 is lifted, only for that file, and only
// while the file also runs on a schedule, so a core change that breaks an
// extension through a path the filter does not list is still found.
//
// What it does not hold: whether the jobs that do start run the whole suite.
// taggedtests_test.go holds the build tags, and AUTHZ_REQUIRED_ENGINES holds a
// suite that was reached and then skipped for a missing engine.

// workflowDir is where the workflows live, relative to the repository root.
const workflowDir = ".github/workflows"

// changeDetectionActions are the actions whose whole purpose is to answer
// "which files did this change touch". Naming them is the direct form of the
// refusal; rule 2 catches the indirect one.
//
//nolint:gochecknoglobals // a closed list, read by one function.
var changeDetectionActions = []string{
	"dorny/paths-filter",
	"tj-actions/changed-files",
	"getsentry/paths-filter",
	"marceloprado/has-changed-path",
}

// triggerFilterAllowed names the workflows that may put `paths:` under `on:`,
// each with its reason. Rules 1 and 2 still hold in them.
//
//nolint:gochecknoglobals // a closed list, read by one function.
var triggerFilterAllowed = map[string]string{
	workflowDir + "/extensions.yml": "the extensions' gates stay out of the core's pipeline",
}

// everyOtherEventRuns is what the condition of a job in pathGatedJobs must
// contain, so that only a pull request can skip it for the paths.
const everyOtherEventRuns = "github.event_name != 'pull_request' ||"

// readsTheAreas is how a condition reads the answer of the paths job.
const readsTheAreas = "needs." + changesJob + ".outputs."

// changeFilterViolations reads one workflow and returns what it refuses, and
// the number of lines it read. It takes the body rather than a path so the
// table below can prove each rule fires without a workflow file to break.
func changeFilterViolations(name, body string) (violations []string, lines int) {
	inTriggers := false
	inJobs := false
	job := ""
	_, filterAllowed := triggerFilterAllowed[name]
	scheduled := false
	filtered := ""

	for number, line := range strings.Split(body, "\n") {
		lines++

		where := fmt.Sprintf("%s:%d", name, number+1)
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// The `on:` block runs from its own key to the next one at column 0.
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			inTriggers = strings.HasPrefix(line, "on:")
			inJobs = strings.HasPrefix(line, "jobs:")
		}

		// A job's own key sits at two spaces under `jobs:`.
		if inJobs && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
			strings.HasSuffix(trimmed, ":") {
			job = strings.TrimSuffix(trimmed, ":")
		}

		if inTriggers && strings.HasPrefix(trimmed, "schedule:") {
			scheduled = true
		}

		for _, action := range changeDetectionActions {
			if strings.Contains(trimmed, action) {
				violations = append(violations, fmt.Sprintf(
					"%s names %s. It answers which files a change touched, and the answer's only "+
						"use in a workflow is to skip work.", where, action))
			}
		}

		// A job's own key sits at two spaces and its keys at four. A step's
		// `if:` is deeper than that, and `if: failure()` on a step is a
		// different thing entirely.
		if strings.HasPrefix(line, "    if:") && !strings.HasPrefix(line, "     ") {
			_, gated := pathGatedJobs[job]
			if name == ciWorkflow && gated && strings.Contains(trimmed, everyOtherEventRuns) &&
				strings.Count(trimmed, "needs.") == strings.Count(trimmed, readsTheAreas) {
				continue
			}

			if strings.Contains(trimmed, "needs.") && strings.Contains(trimmed, ".outputs.") {
				violations = append(violations, fmt.Sprintf(
					"%s gates a whole job on another job's output: %s\n"+
						"    A job that never started reports nothing, and the run is green either way.",
					where, trimmed))
			}
		}

		if inTriggers && (strings.HasPrefix(trimmed, "paths:") || strings.HasPrefix(trimmed, "paths-ignore:")) {
			if filterAllowed {
				filtered = where

				continue
			}

			violations = append(violations, fmt.Sprintf(
				"%s puts %s under on:, so the workflow does not start for a change outside it.",
				where, strings.TrimSuffix(trimmed, ":")))
		}
	}

	if filtered != "" && !scheduled {
		violations = append(violations, fmt.Sprintf(
			"%s filters by path with no schedule under on:. The exception for %s holds only while "+
				"a scheduled run still finds what the filter skips.", filtered, name))
	}

	return violations, lines
}

// TestNoWorkflowSkipsAJobBecauseTheChangeLooksUnrelated is the gate, over the
// committed workflows.
func TestNoWorkflowSkipsAJobBecauseTheChangeLooksUnrelated(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, workflowDir))
	if err != nil {
		t.Fatalf("reading %s: %v", workflowDir, err)
	}

	var (
		violations []string
		read       int
		files      int
	)

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		name := filepath.ToSlash(filepath.Join(workflowDir, entry.Name()))

		body, readErr := os.ReadFile(filepath.Join(root, workflowDir, entry.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", name, readErr)
		}

		found, lines := changeFilterViolations(name, string(body))

		violations = append(violations, found...)
		read += lines
		files++
	}

	requireScanned(t, "workflow files under "+workflowDir, files, 2,
		"A workflow that moved or was renamed leaves this gate reading an empty directory.")
	requireScanned(t, "workflow lines", read, 400)

	sort.Strings(violations)

	if len(violations) > 0 {
		t.Errorf("%d change filter(s) in %s:\n  %s\n\n"+
			"A job skips for the paths only through the paths job `changes`, only when "+
			"pathGatedJobs names it, and only on a pull request. ADR-20260923-xcq rule 1 says why.",
			len(violations), workflowDir, strings.Join(violations, "\n  "))
	}
}

// TestTheChangeFilterGateRefusesEachShape proves the three rules fire, and
// that the exceptions stay as narrow as their comments say. A gate
// whose only evidence is a green run over a tree that has none of what it
// refuses has never been seen to work.
func TestTheChangeFilterGateRefusesEachShape(t *testing.T) {
	t.Parallel()

	extensions := workflowDir + "/extensions.yml"

	for name, testCase := range map[string]struct {
		file    string
		body    string
		refused bool
	}{
		"paths in the extensions workflow, which also runs on a schedule": {
			file:    extensions,
			body:    "on:\n  pull_request:\n    paths:\n      - 'extensions/**'\n  schedule:\n    - cron: '0 3 * * *'\njobs:\n  parts: {}\n",
			refused: false,
		},
		"paths in the extensions workflow with no schedule": {
			file:    extensions,
			body:    "on:\n  pull_request:\n    paths:\n      - 'extensions/**'\njobs:\n  parts: {}\n",
			refused: true,
		},
		"a change-detection action in the extensions workflow": {
			file:    extensions,
			body:    "on:\n  schedule:\n    - cron: '0 3 * * *'\njobs:\n  changes:\n    steps:\n    - uses: dorny/paths-filter@v4\n",
			refused: true,
		},
		"a change-detection action": {
			body:    "jobs:\n  changes:\n    steps:\n    - uses: dorny/paths-filter@v4\n",
			refused: true,
		},
		"a job gated on another job's output": {
			body:    "jobs:\n  unit:\n    runs-on: ubuntu-latest\n    if: needs.changes.outputs.backend == 'true'\n",
			refused: true,
		},
		"a registered job gated on the paths job, on a pull request only": {
			file: ciWorkflow,
			body: "jobs:\n  web-app:\n    needs: [ changes ]\n" +
				"    if: ${{ !cancelled() && github.event_name != 'schedule' && " +
				"(github.event_name != 'pull_request' || needs.changes.outputs.web == 'true') }}\n",
			refused: false,
		},
		"the sign-in gated on the paths job, on a pull request only": {
			file: ciWorkflow,
			body: "jobs:\n  web-oidc-providers:\n    needs: [ changes ]\n" +
				"    if: ${{ !cancelled() && (github.event_name != 'pull_request' || needs.changes.outputs.sign_in == 'true') }}\n",
			refused: false,
		},
		"a registered job gated on the paths job on every event": {
			file: ciWorkflow,
			body: "jobs:\n  web-app:\n    needs: [ changes ]\n" +
				"    if: needs.changes.outputs.web == 'true'\n",
			refused: true,
		},
		"a job pathGatedJobs does not name, gated on the paths job": {
			file: ciWorkflow,
			body: "jobs:\n  repo-gates:\n    needs: [ changes ]\n" +
				"    if: ${{ !cancelled() && (github.event_name != 'pull_request' || needs.changes.outputs.go == 'true') }}\n",
			refused: true,
		},
		"a registered job gated on another job's output": {
			file: ciWorkflow,
			body: "jobs:\n  web-app:\n    needs: [ other ]\n" +
				"    if: ${{ !cancelled() && (github.event_name != 'pull_request' || needs.other.outputs.web == 'true') }}\n",
			refused: true,
		},
		"a registered job gated on the paths job in another workflow": {
			file: workflowDir + "/nightly.yml",
			body: "jobs:\n  web-app:\n    needs: [ changes ]\n" +
				"    if: ${{ !cancelled() && (github.event_name != 'pull_request' || needs.changes.outputs.web == 'true') }}\n",
			refused: true,
		},
		"paths under a trigger": {
			body:    "on:\n  push:\n    paths:\n      - '**/*.go'\njobs:\n  unit:\n    runs-on: ubuntu-latest\n",
			refused: true,
		},
		"paths-ignore under a trigger": {
			body:    "on:\n  pull_request:\n    paths-ignore:\n      - 'docs/**'\njobs:\n  unit: {}\n",
			refused: true,
		},
		"a step that runs only on failure": {
			body:    "jobs:\n  unit:\n    steps:\n    - name: Dump logs\n      if: failure()\n      run: cat log\n",
			refused: false,
		},
		"a job gated on the ref it was triggered by": {
			body:    "jobs:\n  release:\n    if: startsWith(github.ref, 'refs/tags/')\n    needs: [build]\n",
			refused: false,
		},
		"a paths key that is not under a trigger": {
			body:    "on:\n  push:\n    branches: [main]\njobs:\n  unit:\n    steps:\n    - with:\n        paths: ./bin\n",
			refused: false,
		},
		"the action's name inside a comment": {
			body:    "jobs:\n  unit:\n    # dorny/paths-filter is refused here\n    runs-on: ubuntu-latest\n",
			refused: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file := testCase.file
			if file == "" {
				file = "under-test.yml"
			}

			found, lines := changeFilterViolations(file, testCase.body)

			if lines < 3 {
				t.Fatalf("the case reads %d lines, so it proves nothing", lines)
			}

			if refused := len(found) > 0; refused != testCase.refused {
				t.Errorf("refused=%t, want %t. What it said: %v", refused, testCase.refused, found)
			}

			t.Logf("observed %d violation(s) over %d lines: %v", len(found), lines, found)
		})
	}
}

// isWorkflowFile reports whether a directory entry is a workflow.
func isWorkflowFile(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}
