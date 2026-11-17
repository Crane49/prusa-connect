package main

import (
	"sort"
	"strings"
)

// The four verdicts a difference can carry. ADR-20260825-zkq rule 3 defines the first
// two; unknown exists because a classifier that quietly passed a shape it has
// no rule for would report "no breaking changes" about a change nobody read.
const (
	verdictBreaking = "breaking"
	verdictAdditive = "additive"
	verdictNeutral  = "neutral"
	verdictUnknown  = "unknown"
)

// finding is one difference and what this tool makes of it.
type finding struct {
	// Path is the canonical path of the node that differs.
	Path string
	// Kind is added, removed or changed.
	Kind string
	// Verdict is breaking, additive, neutral or unknown.
	Verdict string
	// Reason is the rule that decided it, in a sentence a reviewer can argue
	// with.
	Reason string
	// From and To are the values, for a changed leaf.
	From string
	To   string
}

// classifier holds both documents, because deciding an addition needs to read
// the document it was added to: a new parameter is additive or breaking
// depending on whether it is required, and that is a sibling leaf.
type classifier struct {
	baseline map[string]string
	current  map[string]string
}

// findings classifies every difference between the two documents.
//
// Additions and removals are reported at the shallowest node that is present on
// one side and absent on the other, so a new operation is one finding rather
// than the forty leaves it contains.
func (c classifier) findings() []finding {
	basePrefixes := prefixesOf(c.baseline)
	currentPrefixes := prefixesOf(c.current)

	roots := map[string]string{}

	for path := range c.current {
		if _, present := c.baseline[path]; !present {
			roots[shallowestAbsent(path, basePrefixes)] = kindAdded
		}
	}

	for path := range c.baseline {
		if _, present := c.current[path]; !present {
			roots[shallowestAbsent(path, currentPrefixes)] = kindRemoved
		}
	}

	out := make([]finding, 0, len(roots))

	for path, kind := range roots {
		verdict, reason := c.decide(path, kind)
		out = append(out, finding{Path: path, Kind: kind, Verdict: verdict, Reason: reason})
	}

	for path, was := range c.baseline {
		now, present := c.current[path]
		if !present || now == was {
			continue
		}

		verdict, reason := c.decideChange(path, was, now)
		out = append(out, finding{
			Path: path, Kind: kindChanged, Verdict: verdict, Reason: reason, From: was, To: now,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Kind < out[j].Kind
		}

		return out[i].Path < out[j].Path
	})

	return out
}

// decide classifies a node that is present on one side and absent on the other.
func (c classifier) decide(path, kind string) (verdict, reason string) {
	segments := strings.Split(path, separator)
	last := segments[len(segments)-1]
	parent := ""

	if len(segments) > 1 {
		parent = segments[len(segments)-2]
	}

	if documentation(segments) {
		return verdictNeutral, "documentation, which no client branches on"
	}

	switch {
	case parent == nodeRequired:
		return c.decideRequired(kind, path)
	case parent == nodeEnum:
		return c.decideEnum(kind, path)
	case parent == "properties":
		return decideProperty(kind, last)
	case parent == nodeParameters:
		return c.decideParameter(kind, path, last)
	case parent == nodeResponses:
		return c.decideResponse(kind, path, last)
	case parent == nodePaths:
		return decideAddedOrRemoved(kind, "a path", "every client that calls it")
	case len(segments) == 3 && segments[0] == nodePaths:
		return decideAddedOrRemoved(kind, "an operation", "every client that calls it")
	case len(segments) == 3 && segments[0] == "components":
		return decideAddedOrRemoved(kind, "a component", "anything referencing it")
	case segments[0] == nodeServers:
		return decideServer(kind)
	case last == nodeSecurity || parent == nodeSecurity:
		return c.decideSecurity(kind, path)
	case last == "requestBody" || parent == "requestBody":
		return decideAddedOrRemoved(kind, "a request body", "every client that sends one")
	case last == "deprecated":
		return verdictNeutral, "a deprecation marker, which is the announcement and not the removal"
	case last == longRunningExtension:
		return c.decideLongRunning(kind, path)
	}

	return verdictUnknown, "no rule covers this shape, so it needs a human"
}

// decideLongRunning reads the long-running mark appearing or disappearing. A
// mark written as false says nothing, so only a true mark counts.
func (c classifier) decideLongRunning(kind, path string) (verdict, reason string) {
	if kind == kindAdded {
		if c.current[path] != valueTrue {
			return verdictNeutral, "a long-running mark set to false, which a client reads as no mark"
		}

		return longRunningVerdict(true)
	}

	if c.baseline[path] != valueTrue {
		return verdictNeutral, "a long-running mark set to false, which a client reads as no mark"
	}

	return longRunningVerdict(false)
}

// longRunningVerdict classifies an operation that is now marked long-running or
// is no longer marked.
func longRunningVerdict(marked bool) (verdict, reason string) {
	if marked {
		return verdictAdditive, "a generated client drops its own deadline on this operation, " +
			"and a client that ignores the mark behaves as before"
	}

	return verdictBreaking, "a generated client puts its own deadline back on this operation, " +
		"and that deadline cuts off work the machine is still doing"
}

// decideRequired reads a name moving into or out of a schema's required list.
// Which side it is on decides it: a caller has to send a required request
// field, and a caller may rely on a required response field being there.
func (c classifier) decideRequired(kind, path string) (verdict, reason string) {
	switch side(path) {
	case sideRequest:
		if kind == kindAdded {
			return verdictBreaking, "a request field became required, so a client that omits it is now refused"
		}

		return verdictAdditive, "a request field stopped being required"
	case sideResponse:
		if kind == kindRemoved {
			return verdictBreaking, "a response field stopped being guaranteed, so a client that reads it " +
				"unconditionally can now find it absent"
		}

		return verdictAdditive, "a response field became guaranteed"
	default:
		return verdictBreaking, "a shared schema's required list changed, and a shared schema is reachable " +
			"from a request and from a response, so the strict reading is the one that holds"
	}
}

// decideEnum reads a value entering or leaving a closed set. A request accepts
// more when a value is added; a response can now carry a value a client has
// never seen.
func (c classifier) decideEnum(kind, path string) (verdict, reason string) {
	switch side(path) {
	case sideRequest:
		if kind == kindRemoved {
			return verdictBreaking, "a value this request used to accept is refused"
		}

		return verdictAdditive, "a request accepts one more value"
	case sideResponse:
		if kind == kindAdded {
			return verdictBreaking, "a response can now carry a value no released client knows, and a " +
				"client that switches exhaustively meets it as an unknown"
		}

		return verdictAdditive, "a response carries one fewer value"
	default:
		return verdictBreaking, "a shared schema's enum changed, and a shared schema is reachable from a " +
			"request and from a response, so the strict reading is the one that holds"
	}
}

// decideProperty reads a property entering or leaving a schema.
func decideProperty(kind, name string) (verdict, reason string) {
	if kind == kindAdded {
		return verdictAdditive, "a new property " + name + ", which no released client reads or sends"
	}

	return verdictBreaking, "the property " + name + " is gone, and a client that reads it finds nothing"
}

// decideParameter reads a parameter entering or leaving an operation. A new
// required parameter is a request every released client now gets wrong.
func (c classifier) decideParameter(kind, path, name string) (verdict, reason string) {
	if kind == kindRemoved {
		return verdictBreaking, "the parameter " + name + " is gone, so a client still sending it is " +
			"sending something nothing reads"
	}

	if c.current[path+separator+nodeRequired] == valueTrue {
		return verdictBreaking, "the parameter " + name + " is required, so every released client's " +
			"request is now incomplete"
	}

	return verdictAdditive, "the optional parameter " + name
}

// decideResponse reads a status code entering or leaving an operation.
//
// `default` is not a status. OpenAPI 3.1 section 4.8.16 defines it as the
// answer for every status the operation does not list, so a client reads it
// where it found no branch rather than branching on it. Generators write it as
// the fallback arm and never as a case.
//
// That makes its removal two different changes. An operation that enumerates
// its failures instead has published more than it did, and the fallback arm a
// released client still holds is simply never taken. An operation left with no
// responses at all has stopped documenting what it can answer, and that is the
// break. So the rule reads the current document: `default` leaving an operation
// that still lists a status is additive, and leaving one that lists none is
// breaking.
func (c classifier) decideResponse(kind, path, status string) (verdict, reason string) {
	if kind == kindAdded {
		if status == nodeDefault {
			return verdictAdditive, "a catch-all response, which a released client reads only where " +
				"it has no branch of its own"
		}

		return verdictAdditive, "the operation can answer " + status + ", which a client meets as an " +
			"unhandled status rather than as a wrong one"
	}

	if status == nodeDefault {
		if c.listsAnAnswer(strings.TrimSuffix(path, separator+nodeDefault)) {
			return verdictAdditive, "the catch-all response is gone and the operation lists its " +
				"answers instead, so a client reads a named status where it read the fallback"
		}

		return verdictBreaking, "the catch-all response is gone and nothing lists what the operation " +
			"answers, so a client has no description of any failure"
	}

	return verdictBreaking, "the operation no longer answers " + status + ", so a client branching on it " +
		"has a branch that is now unreachable"
}

// listsAnAnswer reports whether the current document gives this operation's
// responses object any entry at all.
//
// It reads `current` and never `baseline`, because the question is what the
// operation publishes now. Every entry it can find is a real status: this is
// asked only where `default` left the document, so `default` is not among them.
//
// The document is held as a flat map of leaf paths, so membership is a scan for
// a deeper path under the same responses node.
func (c classifier) listsAnAnswer(responses string) bool {
	prefix := responses + separator

	for path := range c.current {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// decideServer reads a base URL entering or leaving the document. A released
// client resolved its paths against the server it was generated with, and
// nothing in the document changes what that client already compiled, so an
// added base URL is one more choice a new client may take. A removed one is the
// address a client was generated against, gone.
//
// A document with no servers at all and one carrying `/` say the same thing:
// OpenAPI 3.1 section 4.8.5 makes `/` the value a tool assumes when the member
// is absent.
func decideServer(kind string) (verdict, reason string) {
	if kind == kindAdded {
		return verdictAdditive, "a base URL a released client does not resolve against, because it " +
			"resolves against the one it was generated with"
	}

	return verdictBreaking, "a base URL is gone, and a client generated against it is addressing " +
		"nothing"
}

// decideSecurity reads a security requirement entering or leaving a document or
// an operation.
//
// The list is OR and each entry is AND. OpenAPI 3.1 section 4.8.30: a request
// satisfying any one entry is authorized, and it has to satisfy every scheme
// inside the entry it satisfies. So an entry added beside an existing one
// widens what is accepted, and a released client keeps passing on the entry it
// already satisfied.
//
// An entry added where the list carried none is the narrowing case, and it is
// the one ADR-20260825-zkq rule 4 names: a credential is demanded where none was.
func (c classifier) decideSecurity(kind, path string) (verdict, reason string) {
	if kind != kindAdded {
		return verdictAdditive, "a credential is no longer demanded"
	}

	if c.baselineAcceptedACredential(path) {
		return verdictAdditive, "one more credential is accepted where one already was, and the " +
			"list of requirements is read as alternatives"
	}

	return verdictBreaking, "a credential is now demanded where none was, so an unauthenticated " +
		"client is refused"
}

// baselineAcceptedACredential reports whether the security list this addition
// landed in already carried an entry in the baseline.
//
// path names the added entry, such as `security > 1`, so the list is the path
// without its last segment. A path with no separator at all is the whole
// security member arriving, and the baseline accepted nothing.
func (c classifier) baselineAcceptedACredential(path string) bool {
	cut := strings.LastIndex(path, separator)
	if cut < 0 {
		return false
	}

	prefix := path[:cut] + separator

	for other := range c.baseline {
		if strings.HasPrefix(other, prefix) {
			return true
		}
	}

	return false
}

func decideAddedOrRemoved(kind, what, who string) (verdict, reason string) {
	if kind == kindAdded {
		return verdictAdditive, what + " that no released client uses"
	}

	return verdictBreaking, what + " is gone, and it is what " + who + " reaches"
}

// decideChange classifies a leaf whose value moved.
func (c classifier) decideChange(path, was, now string) (verdict, reason string) {
	segments := strings.Split(path, separator)
	last := segments[len(segments)-1]

	if documentation(segments) {
		return verdictNeutral, "documentation, which no client branches on"
	}

	switch last {
	case "openapi":
		return verdictNeutral, "the document's own format version"
	case "operationId":
		return verdictBreaking, "a generated client's method is named from this, so the name a caller " +
			"compiled against is gone"
	case "type", "format", "$ref", "items", "additionalProperties":
		return verdictBreaking, "the shape of a value changed from " + was + " to " + now
	case nodeRequired:
		if now == valueTrue {
			return verdictBreaking, "a request field became required"
		}

		return verdictAdditive, "a request field stopped being required"
	case "maximum", "maxLength", "maxItems", "minimum", "minLength", "minItems", "pattern", "default":
		return verdictBreaking, "a constraint moved from " + was + " to " + now + ", and a value that " +
			"used to be accepted may not be"
	case "deprecated":
		return verdictNeutral, "a deprecation marker, which is the announcement and not the removal"
	case longRunningExtension:
		return longRunningVerdict(now == valueTrue)
	}

	if segments[0] == "info" {
		return verdictNeutral, "the document's own identity"
	}

	if segments[0] == nodeServers {
		return verdictBreaking, "the base URL a client resolves every path against moved from " +
			was + " to " + now
	}

	return verdictUnknown, "no rule covers this shape, so it needs a human"
}

// The two halves of an operation a rule reads differently, and the third case
// where a schema is reachable from both.
const (
	sideRequest  = "request"
	sideResponse = "response"
	sideShared   = "shared"
)

// side says which half of an operation a path belongs to.
func side(path string) string {
	switch {
	case strings.Contains(path, separator+"responses"+separator):
		return sideResponse
	case strings.Contains(path, separator+"requestBody"+separator),
		strings.Contains(path, separator+nodeParameters+separator):
		return sideRequest
	default:
		return sideShared
	}
}

// sampleExtensionPrefix is the vendor extension family the reference generator
// reads, x-sample-excuse, x-sample-query and x-sample-headers. Each one feeds
// the rendered sample on an operation's page and nothing else, so it is prose
// under ADR-20260825-zkq rule 6. The long-running mark has a rule of its own below. Any
// other x- key stays unknown, because a client could branch on it.
const sampleExtensionPrefix = "x-sample-"

// longRunningExtension marks an operation whose answer waits on a machine. A
// generated client leaves its own deadline off such an operation.
const longRunningExtension = "x-long-running"

// documentation reports whether a path names prose rather than contract. A
// description is read by a person and branched on by nobody.
//
// A segment under properties, required or enum is a name the API chose, and
// never an OpenAPI keyword. A property called tags or title is contract, so it
// is skipped here and classified like any other property.
func documentation(segments []string) bool {
	for index, segment := range segments {
		if index > 0 && namesAMember(segments[index-1]) {
			continue
		}

		switch segment {
		case "description", "summary", "title", "example", "examples", "externalDocs", nodeTags, "doc":
			return true
		}

		if strings.HasPrefix(segment, sampleExtensionPrefix) {
			return true
		}
	}

	return false
}

// namesAMember reports whether the segment after this one is a name the API
// chose rather than an OpenAPI keyword.
func namesAMember(segment string) bool {
	return segment == "properties" || segment == nodeRequired || segment == nodeEnum
}

// depthEstimate is how many ancestors a leaf has on average here. It sizes the
// map so it does not rehash on the way up, and nothing depends on it.
const depthEstimate = 4

// prefixesOf is every path in the document plus every ancestor of one, which is
// what "present on one side" means for a node that is not a leaf.
func prefixesOf(leaves map[string]string) map[string]bool {
	out := make(map[string]bool, len(leaves)*depthEstimate)

	for path := range leaves {
		segments := strings.Split(path, separator)
		for i := range segments {
			out[strings.Join(segments[:i+1], separator)] = true
		}
	}

	return out
}

// containers are the nodes that hold things rather than being things. A
// difference reported at one of them says nothing: "parameters appeared" is not
// a finding, and "the required parameter since appeared" is.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var containers = map[string]bool{
	nodePaths:         true,
	"components":      true,
	"schemas":         true,
	"securitySchemes": true,
	nodeParameters:    true,
	"properties":      true,
	"responses":       true,
	nodeRequired:      true,
	nodeEnum:          true,
	"content":         true,
	"requestBody":     true,
	"headers":         true,
}

// shallowestAbsent walks up a leaf's ancestors and returns the highest one the
// other document does not have, descending past a container. It is what turns
// forty added leaves into one finding that says "this operation is new".
func shallowestAbsent(path string, other map[string]bool) string {
	segments := strings.Split(path, separator)

	for i := range segments {
		candidate := strings.Join(segments[:i+1], separator)
		if other[candidate] || containers[segments[i]] {
			continue
		}

		return candidate
	}

	return path
}
