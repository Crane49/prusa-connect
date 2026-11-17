package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The files this gate reads. The baseline is the surface the last release
// published; the served document is generated from the registered operations
// and already gated against the committed one by
// internal/transport/http/httpapi.
const (
	defaultBaseline = "tools/apicompat/baseline.json"
	defaultPolicy   = "tools/apicompat/policy.json"
	defaultCurrent  = "internal/transport/http/httpapi/openapi.json"
)

// The two stabilities ADR-20260825-zkq gives a major version.
const (
	// stabilityReporting names a version whose surface may still move. A
	// breaking change is reported and does not fail the gate.
	stabilityReporting = "reporting"
	// stabilityFrozen names a version whose surface is additive only. A
	// breaking change fails the gate and the answer is a new major version.
	stabilityFrozen = "frozen"
)

// Exit codes. A refusal is separated from a usage error so a runner can tell a
// broken invocation from a broken API.
const (
	exitOK      = 0
	exitRefused = 1
	exitUsage   = 2
)

// policy is what the project promises about each major version of the API.
//
// It carries no release of its own. The release is VERSION, which is the file
// scripts/release.sh writes, and a second copy here would be a number that
// drifts from the one the freeze is decided by.
type policy struct {
	Schema int                       `json:"schema"`
	Taken  string                    `json:"baseline_taken"`
	Note   string                    `json:"note"`
	Majors map[string]majorStability `json:"majors"`
}

// majorStability is one major version's promise.
type majorStability struct {
	// Stability is reporting or frozen.
	Stability string `json:"stability"`
	// Since is the release the version first shipped in.
	Since string `json:"since"`
	// Why states what the stability rests on, so the value is not a mood.
	Why string `json:"why"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("apicompat", flag.ContinueOnError)
	flags.SetOutput(stderr)

	root := flags.String("root", ".", "the repository root the other paths are relative to")
	baselinePath := flags.String("baseline", defaultBaseline, "the surface the last release published")
	currentPath := flags.String("current", defaultCurrent, "the document this build serves")
	policyPath := flags.String("policy", defaultPolicy, "what this project promises about each major version")
	versionPath := flags.String("version-file", defaultVersion, "the release this build is, which decides the freeze")

	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	command := "check"
	if rest := flags.Args(); len(rest) > 0 {
		command = rest[0]
	}

	switch command {
	case "check":
		return check(stdout, stderr, filepath.Join(*root, *baselinePath),
			filepath.Join(*root, *currentPath), filepath.Join(*root, *policyPath),
			filepath.Join(*root, *versionPath))
	case "accept":
		return accept(stdout, stderr, filepath.Join(*root, *baselinePath), filepath.Join(*root, *currentPath))
	default:
		fmt.Fprintf(stderr, "apicompat: %q is not a command. Use check or accept.\n", command)

		return exitUsage
	}
}

// check classifies every difference and decides by policy.
//
// The release is read before anything else, because it decides the policy the
// verdict is taken under. ADR-20260825-zkq rule 8 freezes a major version at the first
// 1.0.0, and `freeze` applies that from VERSION rather than from anybody
// remembering to edit a file on the day.
func check(stdout, stderr io.Writer, baselinePath, currentPath, policyPath, versionPath string) int {
	committed, err := readPolicy(policyPath)
	if err != nil {
		fmt.Fprintf(stderr, "apicompat: %v\n", err)

		return exitUsage
	}

	current, err := readRelease(versionPath)
	if err != nil {
		fmt.Fprintf(stderr, "apicompat: %v\n", err)

		return exitUsage
	}

	rules := freeze(committed, current)

	baseline, served, err := readDocuments(baselinePath, currentPath)
	if err != nil {
		fmt.Fprintf(stderr, "apicompat: %v\n", err)

		return exitUsage
	}

	found := classifier{baseline: flatten(baseline), current: flatten(served)}.findings()

	report(stdout, found, rules, current)

	verdict := verdictOf(stdout, found, rules)

	if stale := policyDisagrees(committed, current); stale != nil {
		fmt.Fprintf(stderr, "apicompat: %v\n", stale)

		return exitRefused
	}

	return verdict
}

// verdictOf applies ADR-20260825-zkq's enforcement clause: an unknown difference always
// refuses, and a breaking one refuses only while the major version it touches is
// frozen.
func verdictOf(out io.Writer, found []finding, rules policy) int {
	refused := false

	for _, item := range found {
		if item.Verdict == verdictUnknown {
			refused = true
		}

		if item.Verdict != verdictBreaking {
			continue
		}

		if stabilityFor(rules, majorOf(item.Path)) == stabilityFrozen {
			refused = true
		}
	}

	if refused {
		fmt.Fprintln(out, "\nrefused. An unknown difference always refuses, and a breaking one refuses "+
			"while its major version is frozen. See docs/decisions/20260825-zkq-api-compatibility.md.")

		return exitRefused
	}

	fmt.Fprintln(out, "\naccepted under the current policy. Run `make api-accept` to move the baseline "+
		"forward once the change is in the changelog.")

	return exitOK
}

// report prints one line per finding, grouped by verdict, with the breaking
// ones first because they are what a reader is looking for.
func report(out io.Writer, found []finding, rules policy, current release) {
	counts := map[string]int{}
	for _, item := range found {
		counts[item.Verdict]++
	}

	fmt.Fprintf(out, "apicompat: %d differences (%d breaking, %d additive, %d neutral, %d unknown)\n",
		len(found), counts[verdictBreaking], counts[verdictAdditive],
		counts[verdictNeutral], counts[verdictUnknown])

	fmt.Fprintf(out, "  release %s, and the freeze floor is %s\n", current.text, freezeFloor)

	for _, major := range sortedMajors(rules) {
		fmt.Fprintf(out, "  %s: %s, since %s. %s\n",
			major, rules.Majors[major].Stability, rules.Majors[major].Since, rules.Majors[major].Why)
	}

	for _, verdict := range []string{verdictUnknown, verdictBreaking, verdictAdditive, verdictNeutral} {
		for _, item := range found {
			if item.Verdict != verdict {
				continue
			}

			fmt.Fprintf(out, "\n[%s] %s %s\n  %s\n", item.Verdict, item.Kind, item.Path, item.Reason)

			if item.Kind == kindChanged {
				fmt.Fprintf(out, "  was %q, now %q\n", item.From, item.To)
			}
		}
	}
}

// accept rewrites the baseline from the current document. It is the deliberate
// half of the gate: a change the policy refuses is taken by moving the baseline
// in a commit a reviewer reads, never by a flag on the check.
func accept(stdout, stderr io.Writer, baselinePath, currentPath string) int {
	data, err := os.ReadFile(currentPath)
	if err != nil {
		fmt.Fprintf(stderr, "apicompat: read %s: %v\n", currentPath, err)

		return exitUsage
	}

	if _, err = parse(data); err != nil {
		fmt.Fprintf(stderr, "apicompat: %s: %v\n", currentPath, err)

		return exitUsage
	}

	//nolint:gosec // G703: baselinePath is a path this tool was asked to write, not input from a request.
	if err = os.WriteFile(baselinePath, data, 0o600); err != nil {
		fmt.Fprintf(stderr, "apicompat: write %s: %v\n", baselinePath, err)

		return exitUsage
	}

	fmt.Fprintf(stdout, "apicompat: %s now carries the surface %s serves\n", baselinePath, currentPath)

	return exitOK
}

func readDocuments(baselinePath, currentPath string) (baseline, current document, err error) {
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read the baseline %s: %w", baselinePath, err)
	}

	currentData, err := os.ReadFile(currentPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read the current document %s: %w", currentPath, err)
	}

	baseline, err = parse(baselineData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", baselinePath, err)
	}

	current, err = parse(currentData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", currentPath, err)
	}

	return baseline, current, nil
}

func readPolicy(path string) (policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return policy{}, fmt.Errorf("read the policy %s: %w", path, err)
	}

	var rules policy
	if err = json.Unmarshal(data, &rules); err != nil {
		return policy{}, fmt.Errorf("parse the policy %s: %w", path, err)
	}

	if rules.Schema != 1 {
		return policy{}, fmt.Errorf("%s declares schema %d, and this build reads schema 1", path, rules.Schema)
	}

	if len(rules.Majors) == 0 {
		return policy{}, fmt.Errorf("%s names no major version, so nothing here has a promise", path)
	}

	for name, major := range rules.Majors {
		if major.Stability != stabilityReporting && major.Stability != stabilityFrozen {
			return policy{}, fmt.Errorf("%s: %s is %q, and the stabilities are %q and %q",
				path, name, major.Stability, stabilityReporting, stabilityFrozen)
		}
	}

	return rules, nil
}

// majorOf reads the major version a canonical path belongs to, which is the
// segment after /api/ in the route. A difference outside a route belongs to
// every version, and the strictest promise is the one that holds.
func majorOf(path string) string {
	segments := strings.Split(path, separator)
	if len(segments) < 2 || segments[0] != nodePaths {
		return ""
	}

	parts := strings.Split(strings.TrimPrefix(segments[1], "/"), "/")
	if len(parts) < 2 || parts[0] != "api" {
		return ""
	}

	return parts[1]
}

// stabilityFor is the promise that governs one difference.
//
// A difference outside a route, such as a shared component schema, belongs to
// every major version that references it. Which ones those are is not something
// this tool resolves, so the strictest promise any version makes is the one that
// holds: a frozen v1 refuses a breaking change to a schema v1 might be reading.
func stabilityFor(rules policy, major string) string {
	if major != "" {
		if named, ok := rules.Majors[major]; ok {
			return named.Stability
		}
	}

	for _, candidate := range rules.Majors {
		if candidate.Stability == stabilityFrozen {
			return stabilityFrozen
		}
	}

	return stabilityReporting
}

func sortedMajors(rules policy) []string {
	out := make([]string, 0, len(rules.Majors))
	for name := range rules.Majors {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}
