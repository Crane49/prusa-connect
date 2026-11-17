package arch_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ciOKScript reads the script of the step that gives the verdict out of the
// `CI OK` job, so the case below runs the text CI runs.
func ciOKScript(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), ciWorkflow))
	if err != nil {
		t.Fatalf("reading %s: %v", ciWorkflow, err)
	}

	var workflow ciWorkflowJobs
	if err = yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("parsing %s: %v", ciWorkflow, err)
	}

	for _, step := range workflow.Jobs[ciOKJob].Steps {
		if strings.Contains(step.Run, raceJobPrefix+"*)") {
			return step.Run
		}
	}

	t.Fatalf("no step of %q in %s matches %s*)", ciOKJob, ciWorkflow, raceJobPrefix)

	return ""
}

// TestTheCIOKScriptFailsEveryRunThatIsNotGreen runs the verdict script with
// made-up results for the jobs it needs. A run is green only when every job
// succeeded or skipped on an event that does not run it.
func TestTheCIOKScriptFailsEveryRunThatIsNotGreen(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH, so the CI OK script cannot be run here: %v", err)
	}

	if _, err = exec.LookPath("jq"); err != nil {
		t.Skipf("no jq on PATH, so the CI OK script cannot be run here: %v", err)
	}

	script := ciOKScript(t)

	const (
		pullRequest = "pull_request"
		pullRef     = "refs/pull/1/merge"
		mainRef     = "refs/heads/main"
	)

	for name, testCase := range map[string]struct {
		needs string
		event string
		ref   string
		green bool
	}{
		"every job passed and the race job skipped on a pull request": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef, green: true,
		},
		"the nightly run skipped every job but the race job": {
			needs: `{"a":{"result":"skipped"},"backend-race":{"result":"success"}}`,
			event: "schedule", ref: mainRef, green: true,
		},
		"a job failed": {
			needs: `{"a":{"result":"failure"},"backend-race":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"a job was cancelled": {
			needs: `{"a":{"result":"cancelled"},"backend-race":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"a job skipped on a pull request": {
			needs: `{"a":{"result":"skipped"},"backend-race":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"the sign-in skipped on a pull request whose paths do not reach it": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"false"}},` +
				`"web-oidc-providers":{"result":"skipped","outputs":{}}}`,
			event: pullRequest, ref: pullRef, green: true,
		},
		"the sign-in skipped on a pull request whose paths reach it": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"true"}},` +
				`"web-oidc-providers":{"result":"skipped","outputs":{}}}`,
			event: pullRequest, ref: pullRef,
		},
		"the sign-in failed on a pull request whose paths reach it": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"true"}},` +
				`"web-oidc-providers":{"result":"failure","outputs":{}}}`,
			event: pullRequest, ref: pullRef,
		},
		"the paths job failed on a pull request": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"failure","outputs":{}},` +
				`"web-oidc-providers":{"result":"skipped","outputs":{}}}`,
			event: pullRequest, ref: pullRef,
		},
		"the nightly run skipped the sign-in": {
			needs: `{"a":{"result":"skipped"},"backend-race":{"result":"success"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"true"}},` +
				`"web-oidc-providers":{"result":"skipped","outputs":{}}}`,
			event: "schedule", ref: mainRef,
		},
		"the nightly run ran the sign-in": {
			needs: `{"a":{"result":"skipped"},"backend-race":{"result":"success"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"true"}},` +
				`"web-oidc-providers":{"result":"success","outputs":{}}}`,
			event: "schedule", ref: mainRef, green: true,
		},
		"the nightly run skipped the paths job": {
			needs: `{"a":{"result":"skipped"},"backend-race":{"result":"success"},` +
				`"changes":{"result":"skipped","outputs":{}},` +
				`"web-oidc-providers":{"result":"success","outputs":{}}}`,
			event: "schedule", ref: mainRef,
		},
		"a run a person started skipped the sign-in": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"sign_in":"true"}},` +
				`"web-oidc-providers":{"result":"skipped","outputs":{}}}`,
			event: "workflow_dispatch", ref: mainRef,
		},
		"the unit tests skipped on a pull request that touches no Go": {
			needs: `{"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"go":"false","web":"true"}},` +
				`"backend-unit":{"result":"skipped"},"web-app":{"result":"success"}}`,
			event: pullRequest, ref: pullRef, green: true,
		},
		"the unit tests skipped on a pull request that touches Go": {
			needs: `{"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"go":"true","web":"false"}},` +
				`"backend-unit":{"result":"skipped"},"web-app":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"a job of two areas skipped when one of them changed": {
			needs: `{"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"go":"false","web":"true"}},` +
				`"web-journey":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"the unit tests skipped where the paths job gave no answer for Go": {
			needs: `{"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"web":"true"}},` +
				`"backend-unit":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"the unit tests skipped on a push to main": {
			needs: `{"backend-race":{"result":"success"},` +
				`"changes":{"result":"success","outputs":{"go":"false"}},` +
				`"backend-unit":{"result":"skipped"}}`,
			event: "push", ref: mainRef,
		},
		"the commit and text gates skipped on a pull request that touches nothing": {
			needs: `{"backend-race":{"result":"skipped"},` +
				`"changes":{"result":"success","outputs":{"go":"false","web":"false","docs":"false"}},` +
				`"repo-gates":{"result":"skipped"}}`,
			event: pullRequest, ref: pullRef,
		},
		"the race job skipped on a push to main": {
			needs: `{"a":{"result":"success"},"backend-race":{"result":"skipped"}}`,
			event: "push", ref: mainRef,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"NEEDS="+testCase.needs,
				"EVENT="+testCase.event,
				"REF="+testCase.ref,
			)

			output, runErr := cmd.CombinedOutput()
			if green := runErr == nil; green != testCase.green {
				t.Errorf("green=%t, want %t. What the script printed:\n%s", green, testCase.green, output)
			}
		})
	}
}

// ciJobIDs reads the ids of the jobs `CI OK` needs.
func ciJobIDs(t *testing.T) []string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), ciWorkflow))
	if err != nil {
		t.Fatalf("reading %s: %v", ciWorkflow, err)
	}

	var workflow ciWorkflowJobs
	if err = yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("parsing %s: %v", ciWorkflow, err)
	}

	ids := slices.Clone(workflow.Jobs[ciOKJob].Needs)
	requireScanned(t, "jobs "+ciOKJob+" needs", len(ids), 10)

	return ids
}

// runCIOK runs the verdict script over one result per job, with the paths
// job's answer, and says whether the run is green.
func runCIOK(t *testing.T, bash, script, event string, results map[string]string, answer string) (bool, string) {
	t.Helper()

	needs := map[string]any{}
	for id, result := range results {
		entry := map[string]any{"result": result, "outputs": map[string]string{}}
		if id == changesJob {
			outputs := map[string]string{}
			for _, area := range []string{"all", "go", "web", "docs", "changes", "removed", "sign_in"} {
				outputs[area] = answer
			}

			entry["outputs"] = outputs
		}

		needs[id] = entry
	}

	encoded, err := json.Marshal(needs)
	if err != nil {
		t.Fatal(err)
	}

	ref := "refs/heads/main"
	if event == "pull_request" {
		ref = "refs/pull/1/merge"
	}

	cmd := exec.Command(bash, "-c", script)
	cmd.Env = append(os.Environ(), "NEEDS="+string(encoded), "EVENT="+event, "REF="+ref)

	output, runErr := cmd.CombinedOutput()

	return runErr == nil, string(output)
}

// TestTheCIOKScriptRefusesEverySkipThePathsDoNotExplain skips each job of
// ci.yml alone, on each event, and holds the verdict to what pathGatedJobs
// and the race lane allow. A skip the event and the paths job's answer do not
// explain must fail the run.
func TestTheCIOKScriptRefusesEverySkipThePathsDoNotExplain(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH, so the CI OK script cannot be run here: %v", err)
	}

	if _, err = exec.LookPath("jq"); err != nil {
		t.Skipf("no jq on PATH, so the CI OK script cannot be run here: %v", err)
	}

	script := ciOKScript(t)
	ids := ciJobIDs(t)

	isRace := func(id string) bool { return strings.HasPrefix(id, raceJobPrefix) }

	for _, scenario := range []struct {
		name   string
		event  string
		answer string
		// runs says whether a job runs in this scenario, so a skip of it
		// must fail the run.
		runs func(id string) bool
	}{
		{"a push to main", "push", "true", func(string) bool { return true }},
		{"a run a person started", "workflow_dispatch", "true", func(id string) bool { return !isRace(id) }},
		{"a pull request that touches every area", "pull_request", "true", func(id string) bool { return !isRace(id) }},
		{"a pull request that touches no area", "pull_request", "false", func(id string) bool {
			_, gated := pathGatedJobs[id]

			return !isRace(id) && !gated
		}},
		{"the nightly run", "schedule", "true", func(id string) bool {
			return isRace(id) || id == changesJob || (pathGatedJobs[id].nightly)
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			results := map[string]string{}
			for _, id := range ids {
				results[id] = "skipped"
				if scenario.runs(id) {
					results[id] = "success"
				}
			}

			if green, output := runCIOK(t, bash, script, scenario.event, results, scenario.answer); !green {
				t.Fatalf("every job that runs here passed and the run is red:\n%s", output)
			}

			refused := 0

			for _, id := range ids {
				if !scenario.runs(id) {
					continue
				}

				results[id] = "skipped"
				green, output := runCIOK(t, bash, script, scenario.event, results, scenario.answer)
				results[id] = "success"

				if green {
					t.Errorf("%s skipped and CI OK passed the run:\n%s", id, output)

					continue
				}

				refused++
			}

			requireScanned(t, "skips refused", refused, 1)
		})
	}
}
