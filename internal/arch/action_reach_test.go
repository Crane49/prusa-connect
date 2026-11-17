package arch_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ADR-20260825-bhe makes every vocabulary a gated, published resource. Publishing an
// action nobody can reach publishes a promise the server does not keep, and two
// of the eleven were exactly that: cancel_job was conferred on the operator role
// and no check ever named it, and operate_resource had no endpoint of any kind
// while the two actions on either side of it did.
//
// This gate is about the authorization vocabulary and not about feature
// coverage. It asks whether a caller can cause each published action to be
// decided. It does not ask whether the request then succeeds: a verb classified
// here that no driver in this tree can send is authorized on this path and
// refused on the next one, and that gap belongs to the driver, not to the
// vocabulary.
//
// Five things it cannot catch, written down so the next person does not infer
// them from the code:
//
//   - whether the check is the right one. An endpoint that checks
//     view_telemetry where it should check manage passes here
//   - whether the check runs before the work. Ordering is what the absence rule
//     rests on, and it is held by the tests in internal/app and internal/authz
//   - an action reached through a call this file does not name. decisionCalls
//     and derivations are tables, and TestEveryDecisionCallIsMade is the floor
//     that stops one going stale rather than silent
//   - an action a caller can only reach with a credential nothing mints. The
//     device credential is that today, which is why report_telemetry and
//     fetch_job carry the note they do
//   - a handler that names the action in a refusal and never in a decision. The
//     gate reads arguments to decision calls, so naming an action in a fault
//     parameter counts for nothing
const authorizationArtefact = "docs/vocabularies/authorization.json"

// authzVocabulary is where the action constants and the verb table are
// declared. It is read rather than imported: internal/arch imports nothing of
// the tree it polices (ADR-20260824-kkr), so this gate parses the source the way
// citation_docs_test.go does.
const authzVocabulary = "internal/authz/vocabulary.go"

// authzGeneratedVerbs is the generated half of the verb table: the printer's
// verbs and the class each carries (ADR-20260901-b2z rule 5). It left vocabulary.go
// when the table gained its device-kind dimension.
const authzGeneratedVerbs = "internal/authz/generatedverbs.go"

// enforcementDirs are the trees a request passes through on its way to a
// decision. internal/authz is here because the graph enforces some actions
// inside itself: administering a grant takes manage on the object, and the
// endpoint that reaches it passes no action of its own.
//
//nolint:gochecknoglobals // a table five tests read; there is nowhere cheaper.
var enforcementDirs = []string{"internal/app", "internal/transport/http", "internal/authz"}

// decisionCalls are the functions that take an action and answer whether a
// caller may perform it. An action handed to one of these is decided; an action
// named anywhere else is mentioned.
//
// The wrappers are here beside the graph's own methods because a handler calls
// the wrapper. reach and reachLive resolve a printer behind an action, and
// leaving them out would report send_command unreachable while the file listing
// checks it. permit and permitStorageContents are the same shape for a
// storage's contents, which two planes reach: a storage route, and putting a
// file on a printer.
//
//nolint:gochecknoglobals // read beside enforcementDirs, for the same reason.
var decisionCalls = []string{
	"Can", "CanResource", "ListResources", "WalkDevices",
	"checkable", "checkableResource", "reach", "reachLive",
	"permit", "permitStorageContents",
}

// derivations are the functions that produce an action for somebody else to
// check. An action one of them returns is decided wherever its caller decides,
// so the gate follows the derivation instead of demanding that every handler
// name every action it can reach.
//
// Each entry is a claim with three parts the gate checks: the function is
// declared in declaredIn, it is called from calledIn, and the actions it names
// are the ones this entry vouches for. A derivation nobody calls vouches for
// nothing and fails.
//
//nolint:gochecknoglobals // read beside decisionCalls, for the same reason.
var derivations = []struct {
	function   string
	declaredIn string
	calledIn   string
	reason     string
}{
	{
		function:   "ActionForVerb",
		declaredIn: authzVocabulary,
		calledIn:   "internal/app/devices.go",
		reason: "A printer command's action is its verb's. The device plane is the one place every " +
			"transport passes through, so it derives the action there rather than naming one " +
			"(ADR-20260825-z65 rule 9).",
	},
	{
		function:   "AdministerAction",
		declaredIn: "internal/authz/grant.go",
		calledIn:   "internal/authz/grant.go",
		reason: "Writing a grant takes the object type's administering action, which the caller never " +
			"names. POST /api/v1/grants/{object_type}/{object_id} is the endpoint that reaches it.",
	},
}

// actionsOffHTTP are the published actions no HTTP handler checks, on purpose.
// The list is a claim and not an exemption: an action here that a handler does
// check fails TestAnActionListedOffHTTPIsReallyOffHTTP, and every field is
// required, because a blank reason is how a list like this becomes a place to
// put an action nobody wants to think about.
//
// site names a file that decides the action, and the gate reads it: an entry
// whose site does not exist, or does not name the action, fails.
//
//nolint:gochecknoglobals // read beside the two tables above.
var actionsOffHTTP = []struct {
	action string
	reason string
	site   string
}{
	{
		action: "report_telemetry",
		site:   "internal/authz/grant.go",
		reason: "Conferred by printer#agent and by no role, so the only subject that reaches it is a " +
			"device credential on its own printer. Every HTTP principal is a user: the composition " +
			"root mints one type and internal/app/auth.go is the only thing that mints any. An " +
			"endpoint for it would be a way to forge a printer's readings.",
	},
	{
		action: "fetch_job",
		site:   "internal/authz/grant.go",
		reason: "The other half of the device credential, conferred by printer#agent alone. It is what " +
			"keeps a printer credential from becoming an operator credential, which an HTTP surface " +
			"for it would undo.",
	},
	{
		action: "view_audit",
		site:   "internal/authz/vocabulary.go",
		reason: "Audit sight over a device: who authorized which command on it. It is minted by " +
			"ADR-20260901-b2z rule 3 and consumed by ADR-20260901-atx rule 12, which gates the authorized frame of a " +
			"durable command record on it. The record surface is a separate branch, so today the " +
			"action exists, is conferred at manager and by nothing lower, and is checked by no " +
			"handler. This entry comes off the list when that filter lands: an action nobody checks " +
			"is a grant that means nothing, and it stays visible here until it means something.",
	},
}

// TestEveryPublishedActionIsCheckedSomewhere is the gate. Every action the
// published vocabulary carries is decided somewhere a caller can reach, or is
// named in actionsOffHTTP with the argument for why not.
func TestEveryPublishedActionIsCheckedSomewhere(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	checked := checkedActions(t, root)

	var violations []string

	for _, action := range publishedActions(t, root) {
		if _, ok := checked[action]; ok {
			continue
		}

		if listedOffHTTP(action) {
			continue
		}

		violations = append(violations, fmt.Sprintf(
			"%s is published in %s and no decision call names it. Either check it in a handler, or "+
				"put it in actionsOffHTTP with the argument for why nobody can",
			action, authorizationArtefact))
	}

	reportActions(t, violations)
}

// TestAnActionListedOffHTTPIsReallyOffHTTP is the other half of the floor. The
// list is checked rather than trusted: an action that gained a surface moves out
// of it, and the gate says so rather than leaving a claim that has quietly
// stopped being true.
func TestAnActionListedOffHTTPIsReallyOffHTTP(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	checked := checkedActions(t, root)

	var violations []string

	for _, entry := range actionsOffHTTP {
		sites, ok := checked[entry.action]
		if !ok {
			continue
		}

		violations = append(violations, fmt.Sprintf(
			"%s is listed as reachable through no handler and is decided at %s. Remove it from "+
				"actionsOffHTTP", entry.action, strings.Join(sites, ", ")))
	}

	reportActions(t, violations)
}

// TestEveryActionListedOffHTTPCarriesItsArgument refuses a blank. An entry with
// an empty reason is an action somebody skipped, and an entry whose site does
// not name the action is a claim about a file that decides something else.
func TestEveryActionListedOffHTTPCarriesItsArgument(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	published := publishedActions(t, root)

	var violations []string

	for _, entry := range actionsOffHTTP {
		switch {
		case entry.action == "":
			violations = append(violations, "actionsOffHTTP carries an entry naming no action")

			continue
		case entry.reason == "":
			violations = append(violations, entry.action+" is listed off HTTP with no reason")
		case entry.site == "":
			violations = append(violations, entry.action+" is listed off HTTP with no site")
		}

		if !contains(published, entry.action) {
			violations = append(violations, fmt.Sprintf(
				"%s is listed off HTTP and %s does not publish it", entry.action, authorizationArtefact))
		}

		if entry.site == "" {
			continue
		}

		source, err := os.ReadFile(filepath.Join(root, entry.site))
		if err != nil {
			violations = append(violations, fmt.Sprintf(
				"%s names %s as the site that decides it, and that file cannot be read: %v",
				entry.action, entry.site, err))

			continue
		}

		if !strings.Contains(string(source), entry.action) {
			violations = append(violations, fmt.Sprintf(
				"%s names %s as the site that decides it, and that file does not mention it",
				entry.action, entry.site))
		}
	}

	reportActions(t, violations)
}

// TestEveryDecisionCallIsMade is the floor under the two tables. A function
// renamed out of the tree leaves an entry that matches nothing, and every action
// that reached a decision only through it would then read as unreachable, which
// is a red gate a reader has to diagnose. This says which entry went stale.
func TestEveryDecisionCallIsMade(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	seen := callsMade(t, root)

	var violations []string

	for _, call := range decisionCalls {
		if _, ok := seen.decisions[call]; !ok {
			violations = append(violations, fmt.Sprintf(
				"decisionCalls names %s and nothing under %s calls it",
				call, strings.Join(enforcementDirs, ", ")))
		}
	}

	for _, entry := range derivations {
		if _, ok := seen.derivations[entry.function]; !ok {
			violations = append(violations, fmt.Sprintf(
				"derivations names %s and %s does not call it", entry.function, entry.calledIn))
		}

		if entry.reason == "" {
			violations = append(violations, entry.function+" is a derivation with no reason")
		}

		if len(actionsIn(t, root, entry.declaredIn, entry.function)) == 0 {
			violations = append(violations, fmt.Sprintf(
				"%s is declared in %s and names no action, so it vouches for nothing",
				entry.function, entry.declaredIn))
		}
	}

	reportActions(t, violations)
}

// checkedActions is every published action a caller can cause to be decided,
// with the sites that decide it. Two routes reach an action: it is an argument
// to a decision call, or it is one a derivation produces for such a call.
func checkedActions(t *testing.T, root string) map[string][]string {
	t.Helper()

	names := actionConstants(t, root)
	out := map[string][]string{}

	seen := callsMade(t, root)

	for constant, sites := range seen.constants {
		action, ok := names[constant]
		if !ok {
			continue
		}

		out[action] = append(out[action], sites...)
	}

	for _, entry := range derivations {
		if _, called := seen.derivations[entry.function]; !called {
			continue
		}

		for _, constant := range actionsIn(t, root, entry.declaredIn, entry.function) {
			if action, ok := names[constant]; ok {
				out[action] = append(out[action], entry.declaredIn+" via "+entry.function)
			}
		}
	}

	for action := range out {
		sort.Strings(out[action])
	}

	return out
}

// sightings is what one walk of the enforcement trees found: which action
// constant reached which decision call in which file, which decision calls were
// made at all, and which derivations were called.
type sightings struct {
	constants   map[string][]string
	decisions   map[string]struct{}
	derivations map[string]struct{}
}

// callsMade walks the enforcement trees once and reports all three.
func callsMade(t *testing.T, root string) sightings {
	t.Helper()

	found := sightings{
		constants:   map[string][]string{},
		decisions:   map[string]struct{}{},
		derivations: map[string]struct{}{},
	}

	wanted := map[string]struct{}{}
	for _, call := range decisionCalls {
		wanted[call] = struct{}{}
	}

	derivable := map[string]struct{}{}
	for _, entry := range derivations {
		derivable[entry.function] = struct{}{}
	}

	for _, dir := range enforcementDirs {
		for _, path := range goFilesUnder(t, dir) {
			file := parseGo(t, filepath.Join(root, path))

			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				name := calleeName(call.Fun)

				if _, isDerivation := derivable[name]; isDerivation {
					found.derivations[name] = struct{}{}
				}

				if _, isDecision := wanted[name]; !isDecision {
					return true
				}

				found.decisions[name] = struct{}{}

				for _, argument := range call.Args {
					if constant := actionArgument(argument); constant != "" {
						found.constants[constant] = append(found.constants[constant], filepath.ToSlash(path))
					}
				}

				return true
			})
		}
	}

	return found
}

// actionArgument reads the constant an argument names: Action... inside the
// graph's own package, or authz.Action... outside it. Anything else is a value
// this gate cannot follow, and it is counted as nothing.
func actionArgument(argument ast.Expr) string {
	switch named := argument.(type) {
	case *ast.Ident:
		if strings.HasPrefix(named.Name, "Action") {
			return named.Name
		}
	case *ast.SelectorExpr:
		pkg, ok := named.X.(*ast.Ident)
		if ok && pkg.Name == "authz" && strings.HasPrefix(named.Sel.Name, "Action") {
			return named.Sel.Name
		}
	}

	return ""
}

// calleeName is the function a call names, with any receiver dropped. The
// receiver is deliberately ignored: the gate is about which decision was asked
// for, not about which value was asked.
func calleeName(fun ast.Expr) string {
	switch named := fun.(type) {
	case *ast.Ident:
		return named.Name
	case *ast.SelectorExpr:
		return named.Sel.Name
	default:
		return ""
	}
}

// actionsIn returns the action constants one function's body names. It is how a
// derivation vouches for the actions it produces.
func actionsIn(t *testing.T, root, path, function string) []string {
	t.Helper()

	file := parseGo(t, filepath.Join(root, path))

	var out []string

	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function || fn.Body == nil {
			continue
		}

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if named, isIdent := node.(*ast.Ident); isIdent && strings.HasPrefix(named.Name, "Action") {
				out = append(out, named.Name)
			}

			return true
		})
	}

	// ActionForVerb answers from a table rather than from its own body, so the
	// table is where its actions are. Reading the body alone would report that
	// it vouches for nothing.
	if function == "ActionForVerb" {
		out = append(out, verbTableActions(t, root)...)
	}

	sort.Strings(out)

	return out
}

// verbTableActions is every class the generated half of the verb table names.
func verbTableActions(t *testing.T, root string) []string {
	t.Helper()

	file := parseGo(t, filepath.Join(root, authzGeneratedVerbs))

	var out []string

	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "generatedVerbs" || len(spec.Values) != 1 {
			return true
		}

		table, isComposite := spec.Values[0].(*ast.CompositeLit)
		if !isComposite {
			return true
		}

		for _, element := range table.Elts {
			entry, isEntry := element.(*ast.CompositeLit)
			if !isEntry {
				continue
			}

			for _, field := range entry.Elts {
				pair, isPair := field.(*ast.KeyValueExpr)
				if !isPair {
					continue
				}

				key, isKey := pair.Key.(*ast.Ident)
				if !isKey || key.Name != "class" {
					continue
				}

				if named, isIdent := pair.Value.(*ast.Ident); isIdent {
					out = append(out, named.Name)
				}
			}
		}

		return true
	})

	requireScanned(t, "generatedVerbs entries naming a class in "+authzGeneratedVerbs, len(out), 1,
		"Either the table moved out of that file, or the parse no longer reaches it.")

	return out
}

// actionConstants maps each Action constant to the string it carries, which is
// the spelling the published artefact and every log line use.
func actionConstants(t *testing.T, root string) map[string]string {
	t.Helper()

	file := parseGo(t, filepath.Join(root, authzVocabulary))
	out := map[string]string{}

	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return true
		}

		typed, isTyped := spec.Type.(*ast.Ident)
		if !isTyped || typed.Name != "Action" {
			return true
		}

		literal, isLiteral := spec.Values[0].(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}

		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}

		out[spec.Names[0].Name] = value

		return true
	})

	if len(out) == 0 {
		t.Fatalf("%s declares no Action constant, so this gate maps nothing", authzVocabulary)
	}

	return out
}

// publishedActions is the action half of the committed authorization artefact,
// which is what a client, an extension author and a federated peer read.
func publishedActions(t *testing.T, root string) []string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, authorizationArtefact))
	if err != nil {
		t.Fatalf("read %s: %v", authorizationArtefact, err)
	}

	var artefact struct {
		Actions []struct {
			Action string `json:"action"`
		} `json:"actions"`
	}

	if err = json.Unmarshal(content, &artefact); err != nil {
		t.Fatalf("decode %s: %v", authorizationArtefact, err)
	}

	out := make([]string, 0, len(artefact.Actions))
	for _, action := range artefact.Actions {
		out = append(out, action.Action)
	}

	if len(out) == 0 {
		t.Fatalf("%s publishes no action, so this gate checks nothing", authorizationArtefact)
	}

	sort.Strings(out)

	return out
}

func parseGo(t *testing.T, path string) *ast.File {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	return file
}

func listedOffHTTP(action string) bool {
	for _, entry := range actionsOffHTTP {
		if entry.action == action {
			return true
		}
	}

	return false
}

func reportActions(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Errorf("%d published action(s) are not reachable as claimed:\n\t%s",
		len(violations), strings.Join(violations, "\n\t"))
}
