package arch_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// `CI OK` is the one result a reader or a branch rule reads for a run of
// ci.yml. A job missing from its `needs` can fail and the run still reports
// green, so this gate refuses that. It also refuses a job with no timeout,
// because a hung job holds a runner until GitHub's own six hour limit ends it.
//
// A skipped job reads as a pass unless `CI OK` knows why it was skipped. So
// every job carries one of a few conditions, and the `CI OK` script decides
// from the job's name which one applies:
//
//   - a race job runs on the nightly schedule and on a push to main
//   - the paths job, `changes`, runs on every event and has no condition
//   - a job in pathGatedJobs skips on a pull request that touches none of its
//     areas. Its condition names those areas, and the script names the same
//     areas in a case of its own
//   - every other job runs on every event except the nightly one
//
// ADR-20260923-xcq rule 1 says why a job may skip for the paths.
const (
	ciOKJob  = "ci-ok"
	ciOKName = "CI OK"

	// changesJob answers which areas a pull request touches.
	// scripts/ci-areas.sh gives the answer, and every job in pathGatedJobs
	// reads it.
	changesJob = "changes"

	// ciOKCondition runs the result job whatever happened before it. A
	// skipped result job reads as a pass, so it must run on a cancelled run
	// too, and its script then fails.
	ciOKCondition = "always()"

	// cancelledCondition runs the step that fails a cancelled run. GitHub
	// allows the status functions only in an if, never in a step's env.
	cancelledCondition = "cancelled()"

	// raceJobPrefix starts the id of every race detector job. The `CI OK`
	// script matches the same prefix to know when such a job may skip.
	raceJobPrefix = "backend-race"

	// raceCondition runs a race job on the nightly schedule and on a push to
	// main.
	raceCondition = "github.event_name == 'schedule' || " +
		"(github.event_name == 'push' && github.ref == 'refs/heads/main')"

	// everyChangeCondition runs a job on every event except the nightly one.
	everyChangeCondition = "github.event_name != 'schedule'"
)

// gatedJob is what a job that skips for the paths reads.
type gatedJob struct {
	// areas are the outputs of the paths job the job reads. It runs when
	// any of them is true.
	areas []string
	// nightly is true for a job the nightly run starts as well.
	nightly bool
}

// pathGatedJobs are the jobs that skip on a pull request that touches none of
// their areas, each with those areas. An area holds every file the job's code
// reads, and ci_areas_test.go holds the area script to that. The `CI OK`
// script must name each job with the same areas, or a skip it should fail
// reads as a pass. changefilter_test.go refuses a job outside this list that
// reads the answer of the paths job.
//
//nolint:gochecknoglobals // a closed list, read by the gates below.
var pathGatedJobs = map[string]gatedJob{
	"backend-lint":           {areas: []string{"go"}},
	"backend-security":       {areas: []string{"go"}},
	"backend-verify":         {areas: []string{"go", "docs"}},
	"backend-error-codes":    {areas: []string{"go"}},
	"backend-matrix":         {areas: []string{"go"}},
	"backend-unit":           {areas: []string{"go"}},
	"backend-unit-nodb":      {areas: []string{"go"}},
	"backend-unit-sdk":       {areas: []string{"go"}},
	"backend-unit-minimal":   {areas: []string{"go"}},
	"backend-unit-printer":   {areas: []string{"go"}},
	"backend-adapters":       {areas: []string{"go"}},
	"backend-client-install": {areas: []string{"go", "web"}},
	"backend-build":          {areas: []string{"go"}},
	"docker-image":           {areas: []string{"go", "web"}},
	"web-client":             {areas: []string{"go", "web"}},
	"web-client-lint":        {areas: []string{"web"}},
	"web-client-test":        {areas: []string{"web"}},
	"web-app":                {areas: []string{"web"}},
	"web-embedded":           {areas: []string{"go", "web"}},
	"web-examples":           {areas: []string{"go", "web"}},
	"web-journey":            {areas: []string{"go", "web"}},
	"docs":                   {areas: []string{"go", "docs", "changes", "removed"}},
	"web-oidc-providers":     {areas: []string{"sign_in"}, nightly: true},
}

// gatedCondition is the condition a job in pathGatedJobs must carry. It runs
// the job on every event that is not a pull request, the nightly one only
// when the job asks for it, and on a pull request when one of the areas is
// true. `!cancelled()` keeps it running on a push whose paths job failed.
func gatedCondition(job gatedJob) string {
	reads := make([]string, 0, len(job.areas))
	for _, area := range job.areas {
		reads = append(reads, "needs."+changesJob+".outputs."+area+" == 'true'")
	}

	schedule := everyChangeCondition + " && "
	if job.nightly {
		schedule = ""
	}

	return "${{ !cancelled() && " + schedule + "(github.event_name != 'pull_request' || " +
		strings.Join(reads, " || ") + ") }}"
}

// scriptFunction is the function of the `CI OK` script that decides a job in
// pathGatedJobs.
func scriptFunction(job gatedJob) string {
	if job.nightly {
		return "gated_with_nightly"
	}

	return "gated"
}

// gatedCase matches one case of the `CI OK` script that decides a job by its
// areas: `a|b) expected=$(gated go web) ;;`.
var gatedCase = regexp.MustCompile(
	`(?m)^\s*([a-z0-9|*-]+)\)\s*expected=\$\((gated|gated_with_nightly)((?: [a-z_]+)+)\)\s*;;`)

// scriptCases reads the jobs the `CI OK` script decides by their areas, each
// with the function and the areas it names.
func scriptCases(script string) map[string]string {
	cases := map[string]string{}

	for _, match := range gatedCase.FindAllStringSubmatch(script, -1) {
		for job := range strings.SplitSeq(match[1], "|") {
			cases[job] = match[2] + match[3]
		}
	}

	return cases
}

type ciWorkflowJobs struct {
	Jobs map[string]struct {
		Name           string            `yaml:"name"`
		If             string            `yaml:"if"`
		Needs          []string          `yaml:"needs"`
		Outputs        map[string]string `yaml:"outputs"`
		TimeoutMinutes int               `yaml:"timeout-minutes"`
		Steps          []struct {
			If  string `yaml:"if"`
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// ciOKStepViolations refuses a result job whose steps could skip the verdict,
// or whose script does not tell a race job apart from the others.
func ciOKStepViolations(steps []struct {
	If  string `yaml:"if"`
	Run string `yaml:"run"`
},
) []string {
	var violations []string

	matchesRace := false
	failsCancelled := false

	for _, step := range steps {
		switch strings.TrimSpace(step.If) {
		case "":
		case cancelledCondition:
			if strings.Contains(step.Run, "exit 1") {
				failsCancelled = true
			}
		default:
			violations = append(violations, fmt.Sprintf(
				"a step of %q has `if: %s`, so the verdict could be skipped", ciOKJob, step.If))
		}

		if strings.Contains(step.Run, raceJobPrefix+"*)") && strings.Contains(step.Run, "skipped") {
			matchesRace = true
		}
	}

	if !failsCancelled {
		violations = append(violations, fmt.Sprintf(
			"no step of %q runs under `if: %s` and exits 1, so a cancelled run reads as a pass",
			ciOKJob, cancelledCondition))
	}

	if !matchesRace {
		violations = append(violations, fmt.Sprintf(
			"no step of %q matches %s*) and reads a skipped result, so a skip reads as a pass",
			ciOKJob, raceJobPrefix))
	}

	return violations
}

// gatedJobViolations refuses a job in pathGatedJobs that does not wait for
// the paths job, reads an area the paths job does not give, or that the
// `CI OK` script decides with other areas.
func gatedJobViolations(id string, job gatedJob, needs []string, outputs map[string]string,
	cases map[string]string,
) []string {
	var violations []string

	if !slices.Contains(needs, changesJob) {
		violations = append(violations, fmt.Sprintf(
			"job %q skips for the paths and does not need %q, so it reads no answer", id, changesJob))
	}

	for _, area := range job.areas {
		if _, ok := outputs[area]; !ok {
			violations = append(violations, fmt.Sprintf(
				"job %q reads the area %q, which job %q does not give", id, area, changesJob))
		}
	}

	want := scriptFunction(job) + " " + strings.Join(job.areas, " ")

	got, named := cases[id]

	switch {
	case !named:
		violations = append(violations, fmt.Sprintf(
			"job %q skips for the paths, and no case of %s's script decides it with `$(%s)`, "+
				"so a skip it should fail reads as a pass", id, ciOKJob, want))
	case got != want:
		violations = append(violations, fmt.Sprintf(
			"%s's script decides job %q with `$(%s)`, and the job's condition reads `$(%s)`",
			ciOKJob, id, got, want))
	}

	return violations
}

// ciOKViolations reads one workflow and returns what it refuses, and the
// number of jobs it read. It takes the body so the case below can prove the
// rule fires without a workflow file to break.
func ciOKViolations(body []byte) (violations []string, jobs int, err error) {
	var workflow ciWorkflowJobs
	if err = yaml.Unmarshal(body, &workflow); err != nil {
		return nil, 0, fmt.Errorf("parsing the workflow: %w", err)
	}

	aggregator, ok := workflow.Jobs[ciOKJob]
	if !ok {
		return []string{fmt.Sprintf("there is no job %q. It is the one result for the run.", ciOKJob)},
			len(workflow.Jobs), nil
	}

	if aggregator.Name != ciOKName {
		violations = append(violations, fmt.Sprintf("job %q is named %q, want %q", ciOKJob, aggregator.Name, ciOKName))
	}

	// Without this condition the result job is skipped the moment a job it
	// needs fails, and a skipped job reads as a pass.
	if strings.TrimSpace(aggregator.If) != ciOKCondition {
		violations = append(violations, fmt.Sprintf("job %q has `if: %s`, want `if: %s`",
			ciOKJob, aggregator.If, ciOKCondition))
	}

	violations = append(violations, ciOKStepViolations(aggregator.Steps)...)

	runs := make([]string, 0, len(aggregator.Steps))
	for _, step := range aggregator.Steps {
		runs = append(runs, step.Run)
	}

	cases := scriptCases(strings.Join(runs, "\n"))

	needed := map[string]bool{}
	for _, need := range aggregator.Needs {
		needed[need] = true
	}

	outputs := workflow.Jobs[changesJob].Outputs

	for id, job := range workflow.Jobs {
		if job.TimeoutMinutes <= 0 {
			violations = append(violations, fmt.Sprintf("job %q has no timeout-minutes", id))
		}

		if id == ciOKJob {
			continue
		}

		want := everyChangeCondition

		switch gated, isGated := pathGatedJobs[id]; {
		case id == changesJob:
			want = ""
		case strings.HasPrefix(id, raceJobPrefix):
			want = raceCondition
		case isGated:
			want = gatedCondition(gated)
			violations = append(violations, gatedJobViolations(id, gated, job.Needs, outputs, cases)...)
		}

		if strings.TrimSpace(job.If) != want {
			violations = append(violations, fmt.Sprintf(
				"job %q has `if: %s`, want `if: %s`, the condition %s reads for it", id, job.If, want, ciOKJob))
		}

		if !needed[id] {
			violations = append(violations, fmt.Sprintf(
				"job %q is missing from %s's needs, so it can fail while the run reports green", id, ciOKJob))
		}

		delete(needed, id)
	}

	// A case for a job that is not in pathGatedJobs lets that job skip for
	// the paths, and nothing holds its condition to the same areas.
	for id := range cases {
		if _, isGated := pathGatedJobs[id]; !isGated {
			violations = append(violations, fmt.Sprintf(
				"%s's script lets job %q skip for the paths, and pathGatedJobs does not name it", ciOKJob, id))
		}
	}

	for stale := range needed {
		violations = append(violations, fmt.Sprintf("%s needs %q, which is not a job", ciOKJob, stale))
	}

	sort.Strings(violations)

	return violations, len(workflow.Jobs), nil
}

func TestCIOKNeedsEveryJobAndEveryJobHasATimeout(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), ciWorkflow))
	if err != nil {
		t.Fatalf("reading %s: %v", ciWorkflow, err)
	}

	violations, jobs, err := ciOKViolations(body)
	if err != nil {
		t.Fatalf("%s: %v", ciWorkflow, err)
	}

	requireScanned(t, "jobs in "+ciWorkflow, jobs, 10)

	if len(violations) > 0 {
		t.Errorf("%s:\n  %s", ciWorkflow, strings.Join(violations, "\n  "))
	}
}

// TestEveryPathGatedJobIsInTheWorkflow refuses a stale entry. An entry for a
// job that is gone describes nothing, and one whose job was renamed stops
// holding that job.
func TestEveryPathGatedJobIsInTheWorkflow(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), ciWorkflow))
	if err != nil {
		t.Fatalf("reading %s: %v", ciWorkflow, err)
	}

	var workflow ciWorkflowJobs
	if err = yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("parsing %s: %v", ciWorkflow, err)
	}

	for id := range pathGatedJobs {
		if _, ok := workflow.Jobs[id]; !ok {
			t.Errorf("pathGatedJobs names %q, which is not a job in %s. Delete the entry.", id, ciWorkflow)
		}
	}
}

// okScript is the smallest `CI OK` script the gate accepts.
const okScript = "'case $job in " + raceJobPrefix + "*) ;; esac; test $result = skipped'"

// ciOKCase writes a workflow with one job and the result job. A condition is
// quoted because YAML reads a leading exclamation mark as a tag.
func ciOKCase(job, jobIf, okIf, needs, script string) string {
	lines := []string{"jobs:", "  " + job + ":", "    timeout-minutes: 1"}
	if jobIf != "" {
		lines = append(lines, "    if: "+strconv.Quote(jobIf))
	}

	lines = append(lines, "  ci-ok:", "    name: CI OK", "    timeout-minutes: 1")
	if okIf != "" {
		lines = append(lines, "    if: "+strconv.Quote(okIf))
	}

	lines = append(lines, "    needs: "+needs, "    steps:",
		"      - if: "+cancelledCondition, "        run: exit 1",
		"      - run: "+script)

	return strings.Join(lines, "\n") + "\n"
}

// gatedCaseBody writes a workflow with the paths job, one job that skips for
// the paths, and the result job.
func gatedCaseBody(job, jobIf, jobNeeds, script string) string {
	return "jobs:\n" +
		"  changes:\n    timeout-minutes: 1\n    outputs:\n      go: x\n      web: x\n      sign_in: x\n" +
		"  " + job + ":\n    timeout-minutes: 1\n    needs: " + jobNeeds + "\n    if: " + strconv.Quote(jobIf) + "\n" +
		"  ci-ok:\n    name: CI OK\n    timeout-minutes: 1\n    if: always()\n" +
		"    needs: [changes, " + job + "]\n    steps:\n" +
		"      - if: " + cancelledCondition + "\n        run: exit 1\n" +
		"      - run: " + strconv.Quote(script) + "\n"
}

// TestTheCIOKGateRefusesAJobItDoesNotNeed proves each rule fires.
func TestTheCIOKGateRefusesAJobItDoesNotNeed(t *testing.T) {
	t.Parallel()

	lint := pathGatedJobs["backend-lint"]
	sign := pathGatedJobs["web-oidc-providers"]
	noCase := "case $job in " + raceJobPrefix + "*) ;; esac; test $result = skipped"
	lintScript := "case $job in " + raceJobPrefix + "*) ;;\n backend-lint) expected=$(gated go) ;;\n" +
		"esac; test $result = skipped"
	signScript := "case $job in " + raceJobPrefix + "*) ;;\n web-oidc-providers) expected=$(gated_with_nightly sign_in) ;;\n" +
		"esac; test $result = skipped"

	for name, testCase := range map[string]struct {
		body    string
		refused bool
	}{
		"every job needed": {
			body:    ciOKCase("a", everyChangeCondition, ciOKCondition, "[a]", okScript),
			refused: false,
		},
		"a job left out of needs": {
			body: ciOKCase("a", everyChangeCondition, ciOKCondition, "[a]", okScript) +
				"  b:\n    timeout-minutes: 1\n    if: " + strconv.Quote(everyChangeCondition) + "\n",
			refused: true,
		},
		"!cancelled() instead of always()": {
			body:    ciOKCase("a", everyChangeCondition, "${{ !cancelled() }}", "[a]", okScript),
			refused: true,
		},
		"no condition on the result job": {
			body:    ciOKCase("a", everyChangeCondition, "", "[a]", okScript),
			refused: true,
		},
		"a job with no condition": {
			body:    ciOKCase("a", "", ciOKCondition, "[a]", okScript),
			refused: true,
		},
		"a job that skips on a pull request": {
			body:    ciOKCase("a", "github.event_name == 'push'", ciOKCondition, "[a]", okScript),
			refused: true,
		},
		"a race job with the condition of every change": {
			body:    ciOKCase(raceJobPrefix, everyChangeCondition, ciOKCondition, "["+raceJobPrefix+"]", okScript),
			refused: true,
		},
		"a race job with its own condition": {
			body:    ciOKCase(raceJobPrefix, raceCondition, ciOKCondition, "["+raceJobPrefix+"]", okScript),
			refused: false,
		},
		"a script that reads only failures": {
			body:    ciOKCase("a", everyChangeCondition, ciOKCondition, "[a]", "exit 0"),
			refused: true,
		},
		"no step for a cancelled run": {
			body: strings.Replace(ciOKCase("a", everyChangeCondition, ciOKCondition, "[a]", okScript),
				"      - if: "+cancelledCondition+"\n        run: exit 1\n", "", 1),
			refused: true,
		},
		"a job with no timeout": {
			body: strings.Replace(ciOKCase("a", everyChangeCondition, ciOKCondition, "[a]", okScript),
				"    timeout-minutes: 1\n", "", 1),
			refused: true,
		},
		"a path-gated job with its condition, the paths job and a case in the script": {
			body:    gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]", lintScript),
			refused: false,
		},
		"the sign-in, which the nightly run starts, with its own case": {
			body:    gatedCaseBody("web-oidc-providers", gatedCondition(sign), "[changes]", signScript),
			refused: false,
		},
		"a path-gated job the script does not name": {
			body:    gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]", noCase),
			refused: true,
		},
		"a path-gated job the script decides with other areas": {
			body: gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]",
				strings.Replace(lintScript, "gated go", "gated web", 1)),
			refused: true,
		},
		"a path-gated job the script decides as one the nightly run starts": {
			body: gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]",
				strings.Replace(lintScript, "gated go", "gated_with_nightly go", 1)),
			refused: true,
		},
		"a path-gated job that does not need the paths job": {
			body:    gatedCaseBody("backend-lint", gatedCondition(lint), "[other]", lintScript),
			refused: true,
		},
		"a path-gated job with the condition of every change": {
			body:    gatedCaseBody("backend-lint", everyChangeCondition, "[changes]", lintScript),
			refused: true,
		},
		"a path-gated job whose condition reads another area": {
			body: gatedCaseBody("backend-lint", gatedCondition(gatedJob{areas: []string{"web"}}),
				"[changes]", lintScript),
			refused: true,
		},
		"a path-gated job that reads an area the paths job does not give": {
			body: strings.Replace(gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]", lintScript),
				"      go: x\n", "", 1),
			refused: true,
		},
		"a script that lets a job outside the list skip for the paths": {
			body: gatedCaseBody("backend-lint", gatedCondition(lint), "[changes]",
				lintScript+"\ncase $job in\n a) expected=$(gated go) ;;\nesac"),
			refused: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found, _, err := ciOKViolations([]byte(testCase.body))
			if err != nil {
				t.Fatal(err)
			}

			if refused := len(found) > 0; refused != testCase.refused {
				t.Errorf("refused=%t, want %t. What it said: %v", refused, testCase.refused, found)
			}
		})
	}
}
