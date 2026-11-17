package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mutations are applied to the real committed baseline rather than to a toy
// document. A classifier tested against a fixture of its own shape passes on
// shapes the real document never has, which is the failure this whole tool
// exists to stop.
const baselineFile = "baseline.json"

// TestEveryMutationIsClassified applies one change to the served document and
// requires the tool to name it. A mutation nothing classifies is the gate
// reporting "no breaking changes" about a change nobody read.
func TestEveryMutationIsClassified(t *testing.T) {
	t.Parallel()

	for name, mutation := range map[string]struct {
		apply   func(document)
		verdict string
		path    string
	}{
		"a path is removed": {
			apply:   func(d document) { delete(paths(d), "/api/v1/devices") },
			verdict: verdictBreaking,
			path:    "paths" + separator + "/api/v1/devices",
		},
		"a path is added": {
			apply: func(d document) {
				paths(d)["/api/v1/kettles"] = map[string]any{
					"get": map[string]any{"operationId": "list-kettles", "responses": map[string]any{}},
				}
			},
			verdict: verdictAdditive,
			path:    "paths" + separator + "/api/v1/kettles",
		},
		"an operation is removed": {
			apply:   func(d document) { delete(operation(d, "/api/v1/devices/{device_id}/commands"), "post") },
			verdict: verdictBreaking,
			path:    "paths" + separator + "/api/v1/devices/{device_id}/commands" + separator + "post",
		},
		"a required query parameter is added": {
			apply: func(d document) {
				get := operation(d, "/api/v1/devices")["get"].(map[string]any)
				get["parameters"] = []any{map[string]any{
					"name": "since", "in": "query", "required": true,
					"schema": map[string]any{"type": "string"},
				}}
			},
			verdict: verdictBreaking,
			path: "paths" + separator + "/api/v1/devices" + separator + "get" + separator +
				"parameters" + separator + "query:since",
		},
		"an optional query parameter is added": {
			apply: func(d document) {
				get := operation(d, "/api/v1/devices")["get"].(map[string]any)
				get["parameters"] = []any{map[string]any{
					"name": "since", "in": "query", "required": false,
					"schema": map[string]any{"type": "string"},
				}}
			},
			verdict: verdictAdditive,
			path: "paths" + separator + "/api/v1/devices" + separator + "get" + separator +
				"parameters" + separator + "query:since",
		},
		"a query parameter is removed": {
			apply: func(d document) {
				get := operation(d, "/api/v1/resources")["get"].(map[string]any)
				get["parameters"] = []any{}
			},
			verdict: verdictBreaking,
			path: "paths" + separator + "/api/v1/resources" + separator + "get" + separator +
				"parameters" + separator + "query:kind",
		},
		"a schema property is added": {
			apply: func(d document) {
				properties(d, "Device")["nickname"] = map[string]any{"type": "string"}
			},
			verdict: verdictAdditive,
			path: "components" + separator + "schemas" + separator + "Device" + separator +
				"properties" + separator + "nickname",
		},
		"a schema property named like a keyword is added": {
			apply: func(d document) {
				properties(d, "Device")["title"] = map[string]any{"type": "string"}
			},
			verdict: verdictAdditive,
			path: "components" + separator + "schemas" + separator + "Device" + separator +
				"properties" + separator + "title",
		},
		"a schema property named tags is removed": {
			apply:   func(d document) { delete(properties(d, "Device"), "tags") },
			verdict: verdictBreaking,
			path: "components" + separator + "schemas" + separator + "Device" + separator +
				"properties" + separator + "tags",
		},
		"a schema property is removed": {
			apply:   func(d document) { delete(properties(d, "Device"), "serial") },
			verdict: verdictBreaking,
			path: "components" + separator + "schemas" + separator + "Device" + separator +
				"properties" + separator + "serial",
		},
		"a schema property becomes required": {
			apply: func(d document) {
				schema := schemas(d)["PrinterResult"].(map[string]any)
				schema["required"] = append(schema["required"].([]any), "ignored")
			},
			verdict: verdictBreaking,
			path: "components" + separator + "schemas" + separator + "PrinterResult" + separator +
				"required" + separator + "ignored",
		},
		"a schema property type changes": {
			apply: func(d document) {
				properties(d, "Device")["serial"].(map[string]any)["type"] = "integer"
			},
			verdict: verdictBreaking,
			path: "components" + separator + "schemas" + separator + "Device" + separator +
				"properties" + separator + "serial" + separator + "type",
		},
		"an enum value is added to a request parameter": {
			apply: func(d document) {
				get := operation(d, "/api/v1/resources")["get"].(map[string]any)
				parameter := get["parameters"].([]any)[0].(map[string]any)
				schema := parameter["schema"].(map[string]any)
				schema["enum"] = append(schema["enum"].([]any), "kettle")
			},
			verdict: verdictAdditive,
			path: "paths" + separator + "/api/v1/resources" + separator + "get" + separator +
				"parameters" + separator + "query:kind" + separator + "schema" + separator +
				"enum" + separator + "kettle",
		},
		"an enum value is removed from a request parameter": {
			apply: func(d document) {
				get := operation(d, "/api/v1/resources")["get"].(map[string]any)
				parameter := get["parameters"].([]any)[0].(map[string]any)
				schema := parameter["schema"].(map[string]any)
				schema["enum"] = schema["enum"].([]any)[1:]
			},
			verdict: verdictBreaking,
			path: "paths" + separator + "/api/v1/resources" + separator + "get" + separator +
				"parameters" + separator + "query:kind" + separator + "schema" + separator +
				"enum" + separator + "storage",
		},
		"a response status is removed": {
			apply: func(d document) {
				get := operation(d, "/api/v1/devices")["get"].(map[string]any)
				delete(get["responses"].(map[string]any), "200")
			},
			verdict: verdictBreaking,
			path: "paths" + separator + "/api/v1/devices" + separator + "get" + separator +
				"responses" + separator + "200",
		},
		"a response status is added": {
			apply: func(d document) {
				get := operation(d, "/api/v1/devices")["get"].(map[string]any)
				get["responses"].(map[string]any)["418"] = map[string]any{"description": "I am a teapot"}
			},
			verdict: verdictAdditive,
			path: "paths" + separator + "/api/v1/devices" + separator + "get" + separator +
				"responses" + separator + "418",
		},
		"an operation id changes": {
			apply: func(d document) {
				operation(d, "/api/v1/devices")["get"].(map[string]any)["operationId"] = "devices-list"
			},
			verdict: verdictBreaking,
			path:    "paths" + separator + "/api/v1/devices" + separator + "get" + separator + "operationId",
		},
		"a description changes": {
			apply: func(d document) {
				operation(d, "/api/v1/devices")["get"].(map[string]any)["summary"] = "Devices, listed"
			},
			verdict: verdictNeutral,
			path:    "paths" + separator + "/api/v1/devices" + separator + "get" + separator + "summary",
		},
		"a shape no rule covers appears": {
			apply: func(d document) {
				operation(d, "/api/v1/devices")["get"].(map[string]any)["x-rate-limit"] = "100/s"
			},
			verdict: verdictUnknown,
			path:    "paths" + separator + "/api/v1/devices" + separator + "get" + separator + "x-rate-limit",
		},
		"a rendered sample's extension appears": {
			apply: func(d document) {
				operation(d, "/api/v1/devices")["get"].(map[string]any)["x-sample-query"] = "page_size=5"
			},
			verdict: verdictNeutral,
			path:    "paths" + separator + "/api/v1/devices" + separator + "get" + separator + "x-sample-query",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseline := readBaseline(t)
			current := readBaseline(t)
			mutation.apply(current)

			found := classifier{baseline: flatten(baseline), current: flatten(current)}.findings()
			if len(found) == 0 {
				t.Fatalf("the mutation produced no difference at all, so it did not mutate anything")
			}

			match, ok := findingAt(found, mutation.path)
			if !ok {
				t.Fatalf("nothing was reported at %q. What was reported: %s", mutation.path, describe(found))
			}

			if match.Verdict != mutation.verdict {
				t.Errorf("%s is %s, want %s. Reason given: %s",
					mutation.path, match.Verdict, mutation.verdict, match.Reason)
			}

			t.Logf("observed: %s %s is %s, because %s", match.Kind, match.Path, match.Verdict, match.Reason)
		})
	}
}

// TestABaseUrlIsClassified is ADR-20260825-zkq rules 4 and 5 on the servers member.
//
// It builds both sides rather than joining the table above, because that table
// mutates the current document only and two of these three cases need a base
// URL to be in the baseline to start with.
func TestABaseUrlIsClassified(t *testing.T) {
	t.Parallel()

	withServer := func(url string) document {
		doc := readBaseline(t)
		doc[nodeServers] = []any{map[string]any{"url": url}}

		return doc
	}

	// The served document carries a servers member, so the side that stands for
	// a document with no base URL says so rather than reading whatever the
	// baseline happens to hold.
	withoutServers := func() document {
		doc := readBaseline(t)
		delete(doc, nodeServers)

		return doc
	}

	for name, testCase := range map[string]struct {
		baseline document
		current  document
		verdict  string
		path     string
	}{
		"a base URL is added": {
			baseline: withoutServers(),
			current:  withServer("/"),
			verdict:  verdictAdditive,
			path:     nodeServers,
		},
		"a base URL is removed": {
			baseline: withServer("/"),
			current:  withoutServers(),
			verdict:  verdictBreaking,
			path:     nodeServers,
		},
		"a base URL moves": {
			baseline: withServer("/"),
			current:  withServer("https://printers.example.com"),
			verdict:  verdictBreaking,
			path:     nodeServers + separator + "0" + separator + "url",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := classifier{
				baseline: flatten(testCase.baseline),
				current:  flatten(testCase.current),
			}.findings()

			var match *finding

			for i, item := range found {
				if item.Path == testCase.path {
					match = &found[i]
				}
			}

			if match == nil {
				t.Fatalf("nothing was reported at %q. What was reported: %s",
					testCase.path, describe(found))
			}

			if match.Verdict != testCase.verdict {
				t.Errorf("%s is %s, want %s. Reason given: %s",
					testCase.path, match.Verdict, testCase.verdict, match.Reason)
			}

			t.Logf("observed: %s %s is %s, because %s",
				match.Kind, match.Path, match.Verdict, match.Reason)
		})
	}
}

// TestAnUnchangedDocumentHasNoFindings is the check that the normaliser does not
// invent a difference out of key order or number formatting. Without it every
// other case here could pass while the gate cried wolf on every run.
func TestAnUnchangedDocumentHasNoFindings(t *testing.T) {
	t.Parallel()

	found := classifier{
		baseline: flatten(readBaseline(t)),
		current:  flatten(readBaseline(t)),
	}.findings()

	if len(found) != 0 {
		t.Fatalf("a document compared against itself produced %d findings: %s", len(found), describe(found))
	}
}

// TestTheGateRefusesByPolicy is the enforcement clause of ADR-20260825-zkq, run through
// the command a runner runs.
func TestTheGateRefusesByPolicy(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		stability string
		mutate    func(document)
		exit      int
	}{
		"a breaking change while the version is reporting": {
			stability: stabilityReporting,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitOK,
		},
		"a breaking change while the version is frozen": {
			stability: stabilityFrozen,
			mutate:    func(d document) { delete(paths(d), "/api/v1/devices") },
			exit:      exitRefused,
		},
		"a breaking change to a shared schema while the version is frozen": {
			stability: stabilityFrozen,
			mutate:    func(d document) { delete(properties(d, "Device"), "serial") },
			exit:      exitRefused,
		},
		"an additive change while the version is frozen": {
			stability: stabilityFrozen,
			mutate: func(d document) {
				properties(d, "Device")["nickname"] = map[string]any{"type": "string"}
			},
			exit: exitOK,
		},
		"an unclassified change while the version is reporting": {
			stability: stabilityReporting,
			mutate: func(d document) {
				operation(d, "/api/v1/devices")["get"].(map[string]any)["x-rate-limit"] = "100/s"
			},
			exit: exitRefused,
		},
		"no change at all": {
			stability: stabilityFrozen,
			mutate:    func(document) {},
			exit:      exitOK,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "baseline.json"), readBaseline(t))

			current := readBaseline(t)
			testCase.mutate(current)
			write(t, filepath.Join(dir, "current.json"), current)

			rules := policy{
				Schema: 1,
				Majors: map[string]majorStability{
					"v1": {Stability: testCase.stability, Since: "0.1.0", Why: "under test"},
				},
			}

			encoded, err := json.Marshal(rules)
			if err != nil {
				t.Fatalf("encode the policy: %v", err)
			}

			if err = os.WriteFile(filepath.Join(dir, "policy.json"), encoded, 0o600); err != nil {
				t.Fatalf("write the policy: %v", err)
			}

			// Below the freeze floor, so the committed stability is what decides.
			// The release's own half is TestTheFreezeFollowsTheRelease.
			writeRelease(t, dir, "0.1.0")

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

			code := run([]string{
				"-root", dir,
				"-baseline", "baseline.json",
				"-current", "current.json",
				"-policy", "policy.json",
				"check",
			}, stdout, stderr)

			if code != testCase.exit {
				t.Errorf("the gate exited %d, want %d.\nstdout:\n%s\nstderr:\n%s",
					code, testCase.exit, stdout, stderr)
			}

			t.Logf("observed exit %d:\n%s", code, firstLines(stdout.String(), 4))
		})
	}
}

// TestAcceptMovesTheBaseline is the deliberate half of the gate.
func TestAcceptMovesTheBaseline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	current := readBaseline(t)
	delete(paths(current), "/api/v1/devices")

	write(t, filepath.Join(dir, "baseline.json"), readBaseline(t))
	write(t, filepath.Join(dir, "current.json"), current)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

	code := run([]string{
		"-root", dir, "-baseline", "baseline.json", "-current", "current.json", "accept",
	}, stdout, stderr)
	if code != exitOK {
		t.Fatalf("accept exited %d: %s", code, stderr)
	}

	moved, err := os.ReadFile(filepath.Join(dir, "baseline.json"))
	if err != nil {
		t.Fatalf("read the moved baseline: %v", err)
	}

	if strings.Contains(string(moved), `"/api/v1/devices"`) {
		t.Fatal("accept left the removed path in the baseline")
	}

	t.Logf("observed: %s", strings.TrimSpace(stdout.String()))
}

// TestAPolicyThatPromisesNothingIsRefused holds the file that decides the gate
// to being readable. A policy with an unknown stability would otherwise read as
// "not frozen" and quietly turn the gate off.
func TestAPolicyThatPromisesNothingIsRefused(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"a stability nothing defines":     `{"schema":1,"majors":{"v1":{"stability":"maybe"}}}`,
		"no major version at all":         `{"schema":1,"majors":{}}`,
		"a schema this build cannot read": `{"schema":2,"majors":{"v1":{"stability":"frozen"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write the policy: %v", err)
			}

			if _, err := readPolicy(path); err == nil {
				t.Fatal("the policy was accepted")
			} else {
				t.Logf("observed refusal: %v", err)
			}
		})
	}
}

// TestTheCommittedPolicyIsReadable is what stops the shipped policy rotting
// under the tests above, which all write their own.
func TestTheCommittedPolicyIsReadable(t *testing.T) {
	t.Parallel()

	rules, err := readPolicy("policy.json")
	if err != nil {
		t.Fatalf("the committed policy: %v", err)
	}

	if _, named := rules.Majors["v1"]; !named {
		t.Error("the committed policy names no promise for v1, and v1 is what the routes serve")
	}

	for name, major := range rules.Majors {
		if major.Why == "" {
			t.Errorf("%s declares a stability with no reason, so the value is a mood", name)
		}
	}

	t.Logf("observed: %v", rules.Majors)
}

func readBaseline(t *testing.T) document {
	t.Helper()

	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("read %s: %v", baselineFile, err)
	}

	doc, err := parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", baselineFile, err)
	}

	return doc
}

func write(t *testing.T, path string, doc document) {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func paths(d document) map[string]any { return d["paths"].(map[string]any) }

func schemas(d document) map[string]any {
	return d["components"].(map[string]any)["schemas"].(map[string]any)
}

// properties is one schema's property map, by name. The name is a parameter
// rather than a constant because a mutation that names a second schema is one
// line, and the point of the table is that adding a case costs nothing.
//
//nolint:unparam // see above.
func properties(d document, schema string) map[string]any {
	return schemas(d)[schema].(map[string]any)["properties"].(map[string]any)
}

func operation(d document, path string) map[string]any {
	return paths(d)[path].(map[string]any)
}

func findingAt(found []finding, path string) (finding, bool) {
	for _, item := range found {
		if item.Path == path {
			return item, true
		}
	}

	return finding{}, false
}

func describe(found []finding) string {
	var out []string
	for _, item := range found {
		out = append(out, item.Kind+" "+item.Path+" ["+item.Verdict+"]")
	}

	return strings.Join(out, "; ")
}

func firstLines(text string, n int) string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}

	return strings.Join(lines, "\n")
}

// TestTheCatchAllResponseIsClassified is ADR-20260825-zkq rules 4 and 5 on `default`,
// which is not a status code.
//
// It builds both sides rather than joining the table above. Two of the three
// cases need the current document to be the one that changed shape, and the
// third needs an operation whose only response is the catch-all, which the real
// document does not have.
func TestTheCatchAllResponseIsClassified(t *testing.T) {
	t.Parallel()

	const healthResponses = "paths" + separator + "/api/v1/health" + separator + "get" +
		separator + "responses"

	responsesOf := func(d document) map[string]any {
		return operation(d, "/api/v1/health")["get"].(map[string]any)["responses"].(map[string]any)
	}

	// Every operation in the served document lists a status per failure, so none
	// of them carries a catch-all. The cases below need a document that has one,
	// and this is it: the health operation with `default` beside the statuses it
	// already lists.
	withCatchAll := func() document {
		doc := readBaseline(t)
		responsesOf(doc)["default"] = map[string]any{"description": "Error"}

		return doc
	}

	// reason is asserted as well as the verdict, because two of these cases are
	// additive for different reasons and a rule that lost one of them would
	// still be additive. It is the sentence a reviewer reads to decide whether
	// to accept the diff.
	for name, one := range map[string]struct {
		build   func() (document, document)
		verdict string
		path    string
		reason  string
	}{
		"the catch-all is replaced by the statuses it stood for": {
			build: func() (document, document) {
				// The current side is the served document, which lists 500
				// where the baseline side lists the catch-all.
				return withCatchAll(), readBaseline(t)
			},
			verdict: verdictAdditive,
			path:    healthResponses + separator + "default",
		},
		// The baseline keeps its 200 so that a rule reading the baseline
		// instead of the current document answers this case wrongly.
		"the catch-all goes and nothing is left": {
			build: func() (document, document) {
				baseline, current := withCatchAll(), withCatchAll()
				for status := range responsesOf(current) {
					delete(responsesOf(current), status)
				}

				return baseline, current
			},
			verdict: verdictBreaking,
			path:    healthResponses + separator + "default",
			reason:  "nothing lists what the operation answers",
		},
		"a catch-all appears where there was none": {
			build: func() (document, document) {
				return readBaseline(t), withCatchAll()
			},
			verdict: verdictAdditive,
			path:    healthResponses + separator + "default",
			reason:  "reads only where it has no branch of its own",
		},
		"a real status still goes when the catch-all stays": {
			build: func() (document, document) {
				baseline, current := withCatchAll(), withCatchAll()
				delete(responsesOf(current), "200")

				return baseline, current
			},
			verdict: verdictBreaking,
			path:    healthResponses + separator + "200",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseline, current := one.build()

			found := classifier{baseline: flatten(baseline), current: flatten(current)}.findings()
			if len(found) == 0 {
				t.Fatalf("the two documents do not differ, so this case reads nothing")
			}

			match, ok := findingAt(found, one.path)
			if !ok {
				t.Fatalf("nothing was reported at %q. What was reported: %s", one.path, describe(found))
			}

			if match.Verdict != one.verdict {
				t.Errorf("%s is %s, want %s. Reason given: %s",
					one.path, match.Verdict, one.verdict, match.Reason)
			}

			if one.reason != "" && !strings.Contains(match.Reason, one.reason) {
				t.Errorf("%s is %s for the wrong reason. Want %q in: %s",
					one.path, match.Verdict, one.reason, match.Reason)
			}

			t.Logf("observed: %s %s is %s, because %s", match.Kind, match.Path, match.Verdict, match.Reason)
		})
	}
}

// TestASecurityRequirementIsClassified is ADR-20260825-zkq rules 4 and 5a on the
// security list, which OpenAPI 3.1 reads as alternatives.
//
// It builds both sides because the interesting case is an entry added beside
// one that was already there, and the real baseline has to be the side that
// carries the first entry.
func TestASecurityRequirementIsClassified(t *testing.T) {
	t.Parallel()

	requirements := func(entries ...string) []any {
		out := make([]any, 0, len(entries))
		for _, name := range entries {
			out = append(out, map[string]any{name: []any{}})
		}

		return out
	}

	withSecurity := func(entries ...string) document {
		doc := readBaseline(t)
		if len(entries) == 0 {
			delete(doc, nodeSecurity)

			return doc
		}

		doc[nodeSecurity] = requirements(entries...)

		return doc
	}

	emptySecurity := func() document {
		doc := readBaseline(t)
		doc[nodeSecurity] = []any{}

		return doc
	}

	for name, one := range map[string]struct {
		baseline document
		current  document
		verdict  string
		path     string
	}{
		"a second credential is accepted beside the first": {
			baseline: withSecurity("bearer"),
			current:  withSecurity("bearer", "session"),
			verdict:  verdictAdditive,
			path:     nodeSecurity + separator + "1",
		},
		"a credential is demanded where none was": {
			baseline: withSecurity(),
			current:  withSecurity("bearer"),
			verdict:  verdictBreaking,
			path:     nodeSecurity,
		},
		// The empty list is OpenAPI's way of saying no credential is required,
		// so the first entry to arrive in it demands one. It is the case that
		// separates reading the baseline from reading the current document:
		// only the baseline says whether anything was accepted before.
		"the first credential arrives in an empty list": {
			baseline: emptySecurity(),
			current:  withSecurity("bearer"),
			verdict:  verdictBreaking,
			path:     nodeSecurity + separator + "0",
		},
		"an accepted credential stops being accepted": {
			baseline: withSecurity("bearer", "session"),
			current:  withSecurity("bearer"),
			verdict:  verdictAdditive,
			path:     nodeSecurity + separator + "1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := classifier{
				baseline: flatten(one.baseline), current: flatten(one.current),
			}.findings()
			if len(found) == 0 {
				t.Fatalf("the two documents do not differ, so this case reads nothing")
			}

			match, ok := findingAt(found, one.path)
			if !ok {
				t.Fatalf("nothing was reported at %q. What was reported: %s", one.path, describe(found))
			}

			if match.Verdict != one.verdict {
				t.Errorf("%s is %s, want %s. Reason given: %s",
					one.path, match.Verdict, one.verdict, match.Reason)
			}

			t.Logf("observed: %s %s is %s, because %s", match.Kind, match.Path, match.Verdict, match.Reason)
		})
	}
}

// TestALongRunningMarkIsClassified covers the extension that tells a generated
// client to leave its own deadline off an operation. Gaining the mark only
// relaxes a client, and losing it puts a deadline back on work still running.
func TestALongRunningMarkIsClassified(t *testing.T) {
	t.Parallel()

	const route = "/api/v1/devices/{device_id}/commands"

	marked := func(value any) document {
		doc := readBaseline(t)
		post := operation(doc, route)["post"].(map[string]any)
		post[longRunningExtension] = value

		return doc
	}

	path := "paths" + separator + route + separator + "post" + separator + longRunningExtension

	for name, testCase := range map[string]struct {
		baseline document
		current  document
		verdict  string
	}{
		"an operation gains the mark": {
			baseline: readBaseline(t),
			current:  marked(true),
			verdict:  verdictAdditive,
		},
		"an operation loses the mark": {
			baseline: marked(true),
			current:  readBaseline(t),
			verdict:  verdictBreaking,
		},
		"the mark turns off": {
			baseline: marked(true),
			current:  marked(false),
			verdict:  verdictBreaking,
		},
		"the mark turns on": {
			baseline: marked(false),
			current:  marked(true),
			verdict:  verdictAdditive,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := classifier{
				baseline: flatten(testCase.baseline),
				current:  flatten(testCase.current),
			}.findings()

			match, ok := findingAt(found, path)
			if !ok {
				t.Fatalf("nothing was reported at %q. What was reported: %s", path, describe(found))
			}

			if match.Verdict != testCase.verdict {
				t.Errorf("%s is %s, want %s. Reason given: %s", path, match.Verdict, testCase.verdict, match.Reason)
			}
		})
	}
}
