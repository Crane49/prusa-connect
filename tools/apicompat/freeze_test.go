package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoVersion is the committed release, from the root of the tree this package
// sits two directories below.
const repoVersion = "../../VERSION"

// writeRelease puts a VERSION file at a temporary root.
func writeRelease(t *testing.T, dir, text string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(text+"\n"), 0o600); err != nil {
		t.Fatalf("write the release: %v", err)
	}
}

// writePolicy puts a one-major policy at a temporary root.
func writePolicy(t *testing.T, dir, stability string) {
	t.Helper()

	encoded, err := json.Marshal(policy{
		Schema: 1,
		Majors: map[string]majorStability{
			"v1": {Stability: stability, Since: "0.1.0", Why: "under test"},
		},
	})
	if err != nil {
		t.Fatalf("encode the policy: %v", err)
	}

	if err = os.WriteFile(filepath.Join(dir, "policy.json"), encoded, 0o600); err != nil {
		t.Fatalf("write the policy: %v", err)
	}
}

// TestTheFreezeFollowsTheRelease is ADR-20260825-zkq rule 8 as a mechanism rather than
// as a thing somebody remembers. The release decides, and the policy file only
// records the decision.
//
// The case that matters most is the last one: a release at or above the floor
// with a policy still reading reporting refuses on the policy alone, with no
// breaking change anywhere in the document. Without it this gate would pass on
// the day the flip was forgotten, which is the day it exists for.
func TestTheFreezeFollowsTheRelease(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		version   string
		stability string
		mutate    func(document)
		exit      int
	}{
		"a break below the floor is reported": {
			version:   "0.9.9",
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitOK,
		},
		"a break at the floor refuses, whatever the policy says": {
			version:   "1.0.0",
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitRefused,
		},
		"a break above the floor refuses": {
			version:   "2.4.1",
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitRefused,
		},
		"a release candidate for the floor still reports": {
			version:   "1.0.0-rc.1",
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitOK,
		},
		"build metadata does not move the release": {
			version:   "0.1.0+8628",
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitOK,
		},
		"an additive change at the floor is accepted": {
			version:   "1.0.0",
			stability: stabilityFrozen,
			mutate: func(d document) {
				properties(d, "Device")["nickname"] = map[string]any{"type": "string"}
			},
			exit: exitOK,
		},
		"a stale policy at the floor refuses with no change at all": {
			version:   "1.0.0",
			stability: stabilityReporting,
			mutate:    func(document) {},
			exit:      exitRefused,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "baseline.json"), readBaseline(t))

			current := readBaseline(t)
			testCase.mutate(current)
			write(t, filepath.Join(dir, "current.json"), current)

			writePolicy(t, dir, testCase.stability)
			writeRelease(t, dir, testCase.version)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			code := run([]string{
				"-root", dir,
				"-baseline", "baseline.json",
				"-current", "current.json",
				"-policy", "policy.json",
				"check",
			}, stdout, stderr)

			if code != testCase.exit {
				t.Errorf("release %s with a %s policy exited %d, want %d.\nstdout:\n%s\nstderr:\n%s",
					testCase.version, testCase.stability, code, testCase.exit, stdout, stderr)
			}

			t.Logf("release %s, policy %s, observed exit %d:\n%s%s",
				testCase.version, testCase.stability, code,
				firstLines(stdout.String(), 3), stderr)
		})
	}
}

// TestAReleaseThisGateCannotReadIsRefused holds the other end. The release
// decides whether a break refuses, so a gate that could not find one has no
// verdict and must not answer 0.
func TestAReleaseThisGateCannotReadIsRefused(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"an empty file":         "",
		"two numbers":           "1.0",
		"four numbers":          "1.0.0.1",
		"a word":                "latest",
		"a leading v":           "v1.0.0",
		"a negative component":  "1.-1.0",
		"a number with a space": "1. 0.0",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "baseline.json"), readBaseline(t))
			write(t, filepath.Join(dir, "current.json"), readBaseline(t))
			writePolicy(t, dir, stabilityReporting)
			writeRelease(t, dir, text)

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			code := run([]string{
				"-root", dir, "-baseline", "baseline.json",
				"-current", "current.json", "-policy", "policy.json", "check",
			}, stdout, stderr)

			if code != exitUsage {
				t.Errorf("%q exited %d, want %d.\nstdout:\n%s\nstderr:\n%s",
					text, code, exitUsage, stdout, stderr)
			}

			t.Logf("observed refusal: %s", strings.TrimSpace(stderr.String()))
		})
	}
}

// TestAMissingReleaseFileIsRefused is the same requirement for the file that is
// not there at all, which is what a runner rooted at the wrong directory has.
func TestAMissingReleaseFileIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "baseline.json"), readBaseline(t))
	write(t, filepath.Join(dir, "current.json"), readBaseline(t))
	writePolicy(t, dir, stabilityReporting)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	code := run([]string{
		"-root", dir, "-baseline", "baseline.json",
		"-current", "current.json", "-policy", "policy.json", "check",
	}, stdout, stderr)

	if code != exitUsage {
		t.Errorf("a missing VERSION exited %d, want %d.\nstdout:\n%s\nstderr:\n%s",
			code, exitUsage, stdout, stderr)
	}

	t.Logf("observed refusal: %s", strings.TrimSpace(stderr.String()))
}

// TestTheCommittedPolicyAgreesWithTheCommittedRelease runs the same judgement
// over the two real files. The table above proves the rule on constructed
// inputs; this is what fires on the day this repository cuts 1.0.0 with
// policy.json left as it is.
func TestTheCommittedPolicyAgreesWithTheCommittedRelease(t *testing.T) {
	t.Parallel()

	rules, err := readPolicy("policy.json")
	if err != nil {
		t.Fatalf("the committed policy: %v", err)
	}

	current, err := readRelease(repoVersion)
	if err != nil {
		t.Fatalf("the committed release: %v", err)
	}

	if stale := policyDisagrees(rules, current); stale != nil {
		t.Error(stale)
	}

	t.Logf("observed: VERSION %s, floor %s, frozen %t, policy %v",
		current.text, freezeFloor, current.frozenBy(mustFloor()), rules.Majors)
}
