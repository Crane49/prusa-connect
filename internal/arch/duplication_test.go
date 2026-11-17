package arch_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The trees this gate reads. The four are the application module's own, and
// frontend/ holds no Go.
var twinRoots = []string{"internal", "sdk", "cmd", "adapters"}

// minTwinSize is how big a function body has to be before two identical copies
// of it are worth a row here, counted in whitespace-separated pieces of the
// printed body.
//
// It is 20 because that is where the finder stops reporting the shapes Go
// writes the same way by necessity: a two-branch accessor, a `switch` over four
// constants, a `for` that appends. dupl in .golangci.yml starts at its default
// of 150 tokens and skips _test.go entirely, so everything between the two is
// what this gate is for.
const minTwinSize = 20

// twinsAllowed is every set of functions in the tree with byte-identical
// bodies, against why the copy stays. A key is the members joined by " = ",
// each written as path#receiver.name, sorted.
//
// The value is a closed set of reasons and not free prose. A reason nothing
// checks is a reason that stops being true in silence, and a row is cheaper to
// write than a collapse, so the two together are how a list like this grows.
//
// Every reason in the set carries at least one condition
// TestEveryAllowedTwinGivesAReasonTheGateCanCheck verifies. The one reached for
// most often carries a real computation: lawForbidsAHome is answered against
// the same ADR-20260824-kkr group table layout_test.go enforces, and the row is
// refused, with the group named, when a home does exist.
//
// The list may not grow. twinsAllowedCeiling is its length, and the only edit
// the gate accepts to that number is a smaller one.
var twinsAllowed = map[string]twin{
	"adapters/camera/mjpeg/mjpeg.go#durationAt = adapters/camera/rtsp/rtsp.go#durationAt": {
		why:  adapterStandsAlone,
		note: "each adapter carries its own config readers, because the author copies one whole",
	},
	"adapters/camera/mjpeg/mjpeg.go#takenAt = adapters/camera/rtsp/snapshot.go#takenAt": {
		why:  adapterStandsAlone,
		note: "as above: a shared helper is one the copier would not get with the copy",
	},
	"adapters/events/amqp/kinds.go#destinations = adapters/events/kafka/kinds.go#destinations = adapters/events/webhook/kinds.go#destinations": {
		why:  adapterStandsAlone,
		note: "three worked examples of the same contract, each complete on its own",
	},
	"adapters/events/amqp/reference_test.go#offerOneOfEach = adapters/events/kafka/reference_test.go#offerOneOfEach": {
		why:  adapterStandsAlone,
		note: "the reference gate an author copies with the adapter",
	},
	"adapters/events/amqp/reference_test.go#referenceAddresses = adapters/events/kafka/reference_test.go#referenceAddresses = adapters/events/webhook/reference_test.go#referenceAddresses": {
		why:  adapterStandsAlone,
		note: "as above. Open row: ADR-20260825-n55 rule 4 puts a conformance suite in sdk/eventsapi/eventsapitest, and these four rows are what it does not cover yet",
	},
	"adapters/events/amqp/reference_test.go#registeredDoc = adapters/events/kafka/reference_test.go#registeredDoc": {
		why:  adapterStandsAlone,
		note: "as above",
	},
	"adapters/events/kafka/reference_test.go#TestEverySummaryInTheReferenceSaysWhichKindItIs = adapters/events/webhook/reference_test.go#TestEverySummaryInTheReferenceSaysWhichKindItIs": {
		why:  adapterStandsAlone,
		note: "as above",
	},

	"cmd/generate-error-codes/registered_test.go#listWithDeps = cmd/generate-extensions/registered_test.go#listWithDeps": {
		why:  testScaffoldingOnly,
		note: "two commands' external test packages read the build graph the same way; internal/arch already exports the recipe reader and could take this too",
	},
	"cmd/generate-error-codes/registered_test.go#localName = cmd/generate-extensions/mainscan_test.go#localName": {
		why:  testScaffoldingOnly,
		note: "as above",
	},
	"cmd/generate-error-codes/registered_test.go#registryTagSets = cmd/generate-extensions/registered_test.go#registryTagSets": {
		why:  testScaffoldingOnly,
		note: "as above",
	},

	"cmd/generate-error-codes/registered_test.go#repoRootDir = cmd/generate-extensions/registered_test.go#repoRootDir = internal/arch/graph_test.go#repoRoot = internal/printer/profile/divergence_test.go#repoRoot": {
		why:  lawForbidsAHome,
		note: "internal/arch may import nothing internal, so no one package serves all four. Six of the seven walkers in the tree could share internal/kernel; this row is the set arch is in",
	},
	"internal/app/config/kubernetes_manifest_test.go#repositoryRoot = internal/transport/http/httpapi/contract_test.go#repoRoot": {
		why:  testScaffoldingOnly,
		note: "both may import kernel, so the law does not forbid it. What is missing is a package for a go.mod walk that no production code calls",
	},

	"internal/app/resourcehost/host_test.go#testDoc = internal/extension/registry/registry_test.go#testDoc": {
		why:  testScaffoldingOnly,
		note: "both may import kernel, so the law does not forbid it. An external test package exporting nothing is why it was never lifted, and a non-test package both import is the answer",
	},
	"internal/connector/xbuddy/simulator_test.go#*pipeListener.dial = internal/printer/simulator/host_test.go#*pipeListener.dial": {
		why:  testScaffoldingOnly,
		note: "connector may import a domain, so internal/printer/simulator could publish this pipe listener",
	},
	"sdk/cameraapi/cameraapitest/contract_violation_test.go#runChild = sdk/connectorapi/connectorapitest/contract_violation_test.go#runChild = sdk/eventsapi/eventsapitest/contract_violation_test.go#runChild": {
		why:  testScaffoldingOnly,
		note: "re-running the test binary as a child, in three conformance suites. sdk/ has no internal/ directory; one would hold this without touching the published surface",
	},
	"adapters/db/internal/histstore/engine_test.go#engines = adapters/db/internal/recstore/engine_test.go#engines": {
		why:  testScaffoldingOnly,
		note: "both sit under adapters/db/internal, so a test-support package beside engine/ holds this. The shared environment names are the point: one deployment has one engine, so the invocation that arms one suite arms the other",
	},

	"internal/extension/registry/registry.go#Spec.DecodeConfig = sdk/connectorapi/registry.go#Spec.DecodeConfig": {
		why:  moduleBoundary,
		note: "collapsing it means publishing it from sdk/, which is a v1 surface commitment",
	},
	"internal/kernel/ref/ref.go#echo = sdk/event/origin.go#echo": {
		why:  moduleBoundary,
		note: "as above",
	},
	"internal/printer/profile/coverage.go#widenDefault = sdk/connectorapi/set.go#meetDefault": {
		why:  moduleBoundary,
		note: "as above, and the two names disagree about what the operation is called",
	},

	"sdk/authapi/provider.go#*Registry.Lookup = sdk/cameraapi/camera.go#*Registry.Lookup = sdk/eventsapi/provider.go#*Registry.Lookup = sdk/historyapi/provider.go#*Registry.Lookup = sdk/stateapi/provider.go#*Registry.Lookup = sdk/storageapi/provider.go#*Registry.Lookup": {
		why:  collapsingOpensABoundary,
		note: "the body builds the package's own Registration, whose two fields are unexported so that Declare is the only way to a non-zero one. Lifting it publishes a constructor that pairs a built-in's descriptor with a third party's code",
	},
	"sdk/authapi/provider.go#registrationsOf = sdk/cameraapi/camera.go#registrationsOf = sdk/eventsapi/provider.go#registrationsOf = sdk/historyapi/provider.go#registrationsOf = sdk/stateapi/provider.go#registrationsOf = sdk/storageapi/provider.go#registrationsOf": {
		why:  collapsingOpensABoundary,
		note: "as above, and it is the one the fallback of collapsing this row alone runs into as well",
	},
	"sdk/connectorapi/connectorapitest/contract_self_test.go#*fakeSession.Do = sdk/connectorapi/connectorapitest/contract_violation_test.go#*baseSession.Do": {
		why:  sameOnPurpose,
		note: "one fake obeys the contract and one breaks it, and they answer a session identically so that the difference under test is the only difference",
	},
}

// twinsAllowedCeiling is how many rows the list is allowed to hold. It exists
// because the cheapest response to this gate is a new row, and a list that
// grows every time somebody trips it records duplication rather than preventing
// it.
//
// The gate wants this number and len(twinsAllowed) to be equal, so removing a
// row makes it fail until the number comes down with it. That is the whole
// mechanism: the number only ever goes down, and adding a row costs a removal.
//
// Raising it is not a fix. It is the same move as raising minTwinSize, one
// level up.
const twinsAllowedCeiling = 22

// twinReason is why one copy is allowed to stay: a closed set, so that a row
// cannot be argued in with a sentence nobody can check.
type twinReason string

const (
	// lawForbidsAHome says the members have no ADR-20260824-kkr group they may all
	// import, so there is nowhere the shared half could go. noSharedHome
	// computes the answer rather than believing it.
	lawForbidsAHome twinReason = "ADR-20260824-kkr leaves the members no group they may all import"

	// adapterStandsAlone is ADR-20260825-n55 rules 6 and 7. A community author copies
	// one adapter directory whole, so an adapter carries its own helpers rather
	// than importing a shared one the copy would not bring.
	adapterStandsAlone twinReason = "ADR-20260825-n55: an adapter is copied whole"

	// moduleBoundary is ADR-20260824-gfr rule 1. sdk/ is a separate module, so a helper
	// the application and the sdk both need is written on both sides until the
	// sdk publishes it, which is a v1 surface commitment.
	moduleBoundary twinReason = "ADR-20260824-gfr: sdk/ is a separate module"

	// oneContractManyTypes is one contract published several times over
	// different types. The bodies match because the contract holds.
	oneContractManyTypes twinReason = "one contract, published over several types"

	// sameOnPurpose is a copy whose point is that it answers identically. Two
	// fakes that must agree everywhere except in the one thing under test.
	sameOnPurpose twinReason = "the two answer alike on purpose"

	// collapsingOpensABoundary is a copy whose shared version would have to
	// export something the copies keep unexported. Each body builds a value of
	// its own package's type, and the type is closed so that one declared path
	// is the only way to a valid one. A shared constructor is a way around that
	// path, which is the thing the closed type exists to prevent.
	//
	// Its condition is structural: inside one module, in more than one package.
	// The argument itself is in the note, and a row claiming this reason is
	// asking a reviewer to read it.
	collapsingOpensABoundary twinReason = "the shared version would have to export what the copies keep closed"

	// testScaffoldingOnly is a helper that exists only in _test.go files and
	// that no production package has volunteered to hold. It is the weakest
	// reason here and the ceiling is what keeps it from becoming the answer to
	// everything.
	testScaffoldingOnly twinReason = "test scaffolding, with no package holding it yet"
)

// twinChecks is the condition each reason has to satisfy beyond being spelled
// correctly. A reason with no condition beside it is prose again, so every
// member of the closed set above appears here.
//
//nolint:gochecknoglobals // a table read by one gate, in a test binary.
var twinChecks = map[twinReason]func(members []string) error{
	lawForbidsAHome:          noSharedHome,
	adapterStandsAlone:       allUnder("adapters/"),
	moduleBoundary:           straddlesTheSdk,
	oneContractManyTypes:     allUnder("sdk/"),
	sameOnPurpose:            deliberateTwinShape,
	collapsingOpensABoundary: boundaryShape,
	testScaffoldingOnly:      scaffoldingShape,
}

// twin is one allowed copy: why it stays, and a line saying what would end it.
type twin struct {
	why twinReason
	// note is read by the next person to open this list. It says what the
	// remedy is, or why there is none, and it is required.
	note string
}

// TestNoFunctionBodyIsWrittenTwice refuses a second copy of a function body.
//
// A helper written twice is two helpers from the day after: one of them gets
// the fix, the bound, the rune walk, and the other keeps what it had. Every
// duplicate found in this tree was found by accident, because nothing looks.
//
// It compares printed bodies rather than shapes, so it says nothing about two
// functions that merely resemble each other. Identical bytes are not a
// resemblance, and a rename is enough to get past it: this is a floor, and the
// reuse lens in .agents/rules/adversarial-review.md holds the rest.
func TestNoFunctionBodyIsWrittenTwice(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	groups, bodies := twinGroups(t, root, twinRoots)

	var violations []string

	seen := map[string]bool{}

	for _, key := range sortedKeys(groups) {
		if _, allowed := twinsAllowed[key]; allowed {
			seen[key] = true

			continue
		}

		violations = append(violations, fmt.Sprintf("%d copies of one body: %s", len(groups[key]), key))
	}

	// The floor is over the bodies read, never over the twins found. A clean
	// tree scores zero twins, and so does a walk whose roots stopped matching.
	requireScanned(t, "function bodies", bodies, 2000,
		"The walk reached no Go file, so a tree full of copies would score zero twins.")

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d set(s) of functions share one body:\n\t%s\n"+
			"Give the job one mechanism and have the other call it. A copy that has to stay is a "+
			"row in twinsAllowed, and the reason has to be one of the %d the gate can check.",
			len(violations), strings.Join(violations, "\n\t"), len(twinChecks))
	}

	reportStale(t, twinsAllowed, seen, "twinsAllowed")
}

// TestEveryAllowedTwinGivesAReasonTheGateCanCheck is what stops the list being
// argued longer. Each row names a reason from the closed set, and each reason
// carries a condition on the members that the gate answers for itself.
//
// It is not a formality. A row whose reason has stopped holding is refused with
// the fact that contradicts it, and where the reason is lawForbidsAHome the
// refusal names the package the shared half can go in.
func TestEveryAllowedTwinGivesAReasonTheGateCanCheck(t *testing.T) {
	t.Parallel()

	for _, key := range sortedKeys(twinsAllowed) {
		row := twinsAllowed[key]

		check, known := twinChecks[row.why]
		if !known {
			t.Errorf("%s\n\tgives the reason %q, which is not one the gate knows. The reasons are: %s",
				key, row.why, strings.Join(knownTwinReasons(), "; "))

			continue
		}

		if strings.TrimSpace(row.note) == "" {
			t.Errorf("%s\n\tcarries no note. Say what would end the copy, or why nothing will.", key)
		}

		if err := check(strings.Split(key, " = ")); err != nil {
			t.Errorf("%s\n\tclaims %q, and that does not hold: %v", key, row.why, err)
		}
	}

	requireScanned(t, "allowed twins judged", len(twinsAllowed), 1,
		"The list is empty, so this gate read nothing.")
}

// TestTheAllowedTwinListDoesNotGrow holds the ceiling. The list is allowed to
// get shorter and nothing else, so a new row costs the work of removing one.
func TestTheAllowedTwinListDoesNotGrow(t *testing.T) {
	t.Parallel()

	switch held := len(twinsAllowed); {
	case held > twinsAllowedCeiling:
		t.Errorf("twinsAllowed holds %d rows against a ceiling of %d.\n"+
			"Collapse a copy instead of adding a row. Raising the ceiling is the same move as "+
			"raising minTwinSize, one level up, and it is not the fix.", held, twinsAllowedCeiling)
	case held < twinsAllowedCeiling:
		t.Errorf("twinsAllowed holds %d rows and the ceiling is still %d. "+
			"Lower twinsAllowedCeiling to %d so the room you freed cannot be spent.",
			held, twinsAllowedCeiling, held)
	}
}

// knownTwinReasons lists the closed set, for a refusal that says what was
// allowed instead of only what was not.
func knownTwinReasons() []string {
	out := make([]string, 0, len(twinChecks))
	for reason := range twinChecks {
		out = append(out, string(reason))
	}

	sort.Strings(out)

	return out
}

// allUnder requires every member to sit under one directory.
func allUnder(prefix string) func([]string) error {
	return func(members []string) error {
		for _, member := range members {
			if !strings.HasPrefix(member, prefix) {
				return fmt.Errorf("%s is not under %s", member, prefix)
			}
		}

		return nil
	}
}

// straddlesTheSdk requires the copy to actually cross the module boundary
// ADR-20260824-gfr draws, rather than name it.
func straddlesTheSdk(members []string) error {
	inSdk, outside := 0, 0

	for _, member := range members {
		if strings.HasPrefix(member, "sdk/") {
			inSdk++
		} else {
			outside++
		}
	}

	if inSdk == 0 || outside == 0 {
		return fmt.Errorf("%d of %d members are under sdk/, so both copies are inside one module "+
			"and the module boundary is not what keeps them apart", inSdk, len(members))
	}

	return nil
}

// testFilesOnly requires every member to live in a _test.go file. A helper
// production code calls is not scaffolding and is not a fake.
func testFilesOnly(members []string) error {
	for _, member := range members {
		where, _, _ := strings.Cut(member, "#")
		if !strings.HasSuffix(where, "_test.go") {
			return fmt.Errorf("%s is not a test file, so the copy is in code that ships", member)
		}
	}

	return nil
}

// scaffoldingShape is testScaffoldingOnly's condition: test files, spread over
// more than one bounded context.
//
// It is the weakest reason in the closed set and it carries the most rows, so
// it is the one most worth constraining. Two copies inside one context are two
// copies with no import rule between them, and that is a rename rather than a
// reason.
func scaffoldingShape(members []string) error {
	if err := testFilesOnly(members); err != nil {
		return err
	}

	if contexts := twinContexts(members); len(contexts) == 1 {
		return fmt.Errorf("every member sits in %s, so one of them can call the other", contexts[0])
	}

	return nil
}

// boundaryShape is collapsingOpensABoundary's condition: inside one module, in
// more than one package. Two copies in one package already share whatever the
// package keeps closed, so there is nothing for a shared version to open.
func boundaryShape(members []string) error {
	if err := allUnder("sdk/")(members); err != nil {
		return err
	}

	if contexts := twinContexts(members); len(contexts) == 1 {
		return fmt.Errorf("every member sits in %s, which already shares whatever that package "+
			"keeps unexported", contexts[0])
	}

	return nil
}

// deliberateTwinShape is sameOnPurpose's condition, and it is the mirror of the
// one above: two fakes that must answer alike are siblings in one suite, read
// side by side. Two of them in different packages are not one pair of fakes,
// they are a copy that has travelled.
func deliberateTwinShape(members []string) error {
	if err := testFilesOnly(members); err != nil {
		return err
	}

	if contexts := twinContexts(members); len(contexts) > 1 {
		return fmt.Errorf("the members are spread over %s, so they are not two fakes in one suite",
			strings.Join(contexts, " and "))
	}

	return nil
}

// noSharedHome answers whether the members really have nowhere to put the
// shared half, out of the ADR-20260824-kkr group table that layout_test.go enforces.
//
// The claim fails in one particular way often enough to be worth naming: two
// packages that may not import each other can still both import
// internal/kernel, so the law offers a home the row says it forbids.
//
// It models internal/ and cmd/ only. A command is not placed by the group
// table, so its imports are not judged and every group is open to it, which is
// why five rows claiming cmd/ had no shared package do not survive here. A
// member under adapters/ or sdk/ is governed by ADR-20260825-n55 rule 2 or ADR-20260824-gfr
// rule 2 instead, and those rows carry the reason that names one of those.
//
// It is conservative on purpose: a group counts only when every member may
// import it, so one internal/arch member takes the answer to none, which is the
// true answer for the six repository-root walkers.
func noSharedHome(members []string) error {
	if contexts := twinContexts(members); len(contexts) == 1 {
		return fmt.Errorf("every member sits in the same bounded context (%s), so the shared half "+
			"goes in one of those packages and no import rule is in the way", contexts[0])
	}

	hosts, modelled := sharedHostGroups(members)
	if !modelled {
		return errors.New("a member sits outside internal/ and cmd/, which this reason does not " +
			"model. Use the reason that names the boundary actually keeping the two apart")
	}

	if len(hosts) > 0 {
		return fmt.Errorf("every member may import the %s group, so ADR-20260824-kkr offers a home there",
			strings.Join(hosts, " and the "))
	}

	return nil
}

// sharedHostGroups returns the ADR-20260824-kkr groups every member may import, and
// whether the members are ones the group table decides at all.
func sharedHostGroups(members []string) ([]string, bool) {
	var common map[string]bool

	for _, member := range members {
		hosts, modelled := hostGroupsFor(member)
		if !modelled {
			return nil, false
		}

		if common == nil {
			common = hosts

			continue
		}

		for name := range common {
			if !hosts[name] {
				delete(common, name)
			}
		}
	}

	out := make([]string, 0, len(common))
	for name := range common {
		out = append(out, name)
	}

	sort.Strings(out)

	return out, true
}

// hostGroupsFor is the set of groups one member may import.
func hostGroupsFor(member string) (map[string]bool, bool) {
	dir := twinDir(member)

	if strings.HasPrefix(dir, "cmd/") {
		// A command is not placed by the group table, so nothing judges its
		// imports and every group is a candidate home.
		out := make(map[string]bool, len(groups))
		for _, g := range groups {
			out[g.name] = true
		}

		return out, true
	}

	placed, ok := placementOf(modulePath + "/" + dir)
	if !ok {
		return nil, false
	}

	out := make(map[string]bool, len(placed.group.mayImport))
	for _, name := range placed.group.mayImport {
		out[name] = true
	}

	return out, true
}

// twinContexts is the bounded contexts the members sit in, sorted and deduped.
// Two members in one context import each other freely, so a copy across them
// has no import rule behind it at all.
func twinContexts(members []string) []string {
	held := map[string]bool{}

	for _, member := range members {
		dir := twinDir(member)

		if placed, ok := placementOf(modulePath + "/" + dir); ok {
			held[placed.context] = true

			continue
		}

		held[dir] = true
	}

	out := make([]string, 0, len(held))
	for name := range held {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// twinDir is the directory a member sits in, cut out of the path#name form the
// keys are written in.
func twinDir(member string) string {
	where, _, _ := strings.Cut(member, "#")

	return path.Dir(where)
}

// TestTheTwinFinderSeesACopy is the mutation check for the finder itself. The
// gate above expects zero violations on a healthy tree, so its green cannot
// tell a finder that reports nothing from a tree that holds nothing.
func TestTheTwinFinderSeesACopy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()

		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	// Four functions, two identical pairs. The long pair is over minTwinSize and
	// has to be reported. The one-line pair is under it and must not be, which is
	// the half a finder that reports everything would fail.
	const long = `{
	out := make([]string, 0, len(in))
	for _, each := range in {
		if each == "" {
			continue
		}
		out = append(out, strings.ToUpper(each))
	}
	sort.Strings(out)
	return out
}`

	write("a.go", "package a\n\nfunc First(in []string) []string "+long+"\n")
	write("b.go", "package a\n\nfunc Second(in []string) []string "+long+"\n")
	write("c.go", "package a\n\nfunc Third(in []string) []string {\n\treturn append(in, \"x\")\n}\n")
	write("d.go", "package a\n\nfunc Fourth(in []string) []string {\n\treturn append(in, \"x\")\n}\n")

	groups, bodies := twinGroups(t, dir, []string{"."})

	if bodies != 4 {
		t.Fatalf("read %d bodies, want 4", bodies)
	}

	keys := sortedKeys(groups)
	if len(keys) != 1 {
		t.Fatalf("found %d twin groups, want the one pair: %v", len(keys), keys)
	}

	if want := "a.go#First = b.go#Second"; keys[0] != want {
		t.Errorf("the pair is reported as %q, want %q", keys[0], want)
	}
}

// twinGroups returns the sets of functions sharing one printed body, keyed by
// their members, and how many bodies were read.
func twinGroups(t *testing.T, root string, subdirs []string) (map[string][]string, int) {
	t.Helper()

	byBody := map[string][]string{}
	bodies := 0

	for _, subdir := range subdirs {
		fset := token.NewFileSet()

		err := filepath.WalkDir(filepath.Join(root, subdir), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			where, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}

			where = filepath.ToSlash(where)

			if d.IsDir() {
				if skipWholeTree(d.Name()) || d.Name() == "testdata" {
					return fs.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return fmt.Errorf("parsing %s: %w", where, parseErr)
			}

			for _, decl := range file.Decls {
				fn, isFunc := decl.(*ast.FuncDecl)
				if !isFunc || fn.Body == nil {
					continue
				}

				bodies++

				body, size := printedBody(fset, fn.Body)
				if size < minTwinSize {
					continue
				}

				byBody[body] = append(byBody[body], where+"#"+twinName(fn))
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", subdir, err)
		}
	}

	groups := map[string][]string{}

	for _, members := range byBody {
		if len(members) < 2 {
			continue
		}

		sort.Strings(members)
		groups[strings.Join(members, " = ")] = members
	}

	return groups, bodies
}

// printedBody renders a body with its whitespace flattened, and its size. Two
// bodies differing only in layout or in a comment are the same body: gofmt
// decides the first, and a copy carrying a fresh comment is still a copy.
func printedBody(fset *token.FileSet, body *ast.BlockStmt) (string, int) {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, body); err != nil {
		return "", 0
	}

	pieces := strings.Fields(buf.String())
	sum := sha256.Sum256([]byte(strings.Join(pieces, " ")))

	return hex.EncodeToString(sum[:]), len(pieces)
}

// twinName is how a function is named in a key: the bare name, or the receiver
// type and the name, so two methods of one file are told apart.
func twinName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	return receiverType(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverType(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + receiverType(typed.X)
	case *ast.IndexExpr:
		return receiverType(typed.X)
	case *ast.IndexListExpr:
		return receiverType(typed.X)
	}

	return "?"
}

// TestTheReasonCheckAnswersTheLawRatherThanBelievingTheRow is the mutation
// check for noSharedHome, which is the one reason in the closed set that is
// computed rather than asserted. The gate above expects every row to pass, so
// its green cannot tell a check that reads the group table from one that
// returns nil.
//
// The three cases are the three the old list got wrong or right: a domain and a
// platform package that both reach kernel, two members of one context, and a
// set holding internal/arch, which may import nothing internal.
func TestTheReasonCheckAnswersTheLawRatherThanBelievingTheRow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		members []string
		refused string
	}{
		{
			name: "a domain and a platform package both reach kernel",
			members: []string{
				"internal/authz/sqlite.go#enableWAL",
				"internal/platform/history/sqlite.go#enableWAL",
			},
			refused: "may import the kernel group",
		},
		{
			name: "a command may import every group",
			members: []string{
				"cmd/generate-authz-vocabulary/compare.go#writeFile",
				"cmd/generate-device-issues/compare.go#writeFile",
			},
			refused: "may import the",
		},
		{
			name: "two members of one package have no import rule between them",
			members: []string{
				"internal/app/devicelinkevent_test.go#*eventLog.ofType",
				"internal/app/scriptemit_test.go#*eventLog.framesOfType",
			},
			refused: "same bounded context (app)",
		},
		{
			name: "a member under sdk/ is not this reason's business",
			members: []string{
				"internal/kernel/ref/ref.go#echo",
				"sdk/event/origin.go#echo",
			},
			refused: "outside internal/ and cmd/",
		},
		{
			name: "internal/arch may import nothing internal, so the claim holds",
			members: []string{
				"internal/arch/graph_test.go#repoRoot",
				"internal/docsref/published_test.go#repoRoot",
			},
			refused: "",
		},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			err := noSharedHome(each.members)

			if each.refused == "" {
				if err != nil {
					t.Fatalf("the claim holds here and the check refused it: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("the claim does not hold here and the check accepted it")
			}

			if !strings.Contains(err.Error(), each.refused) {
				t.Errorf("the refusal is %q, which does not name %q", err, each.refused)
			}
		})
	}
}

// TestEveryReasonCarriesACondition is the half that keeps the closed set from
// becoming prose again. A reason with no entry in twinChecks is a label a row
// can wear without answering to anything.
func TestEveryReasonCarriesACondition(t *testing.T) {
	t.Parallel()

	declared := []twinReason{
		lawForbidsAHome, adapterStandsAlone, moduleBoundary, oneContractManyTypes,
		sameOnPurpose, collapsingOpensABoundary, testScaffoldingOnly,
	}

	for _, reason := range declared {
		if _, checked := twinChecks[reason]; !checked {
			t.Errorf("the reason %q has no condition beside it in twinChecks, so a row claiming "+
				"it answers to nothing", reason)
		}
	}

	if len(twinChecks) != len(declared) {
		t.Errorf("twinChecks holds %d conditions and %d reasons are declared. A condition with no "+
			"reason is dead, and a reason with no condition is prose.", len(twinChecks), len(declared))
	}
}
