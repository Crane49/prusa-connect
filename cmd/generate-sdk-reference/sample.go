package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
	"github.com/prusa3d/prusa-connect/internal/docsref"
)

// The sample under each operation: a curl line, the client call and the
// answer. Every value comes from the examples the server attached to the
// operation. Nothing here invents one. An operation with a sample excuse
// prints the excuse instead.

const (
	// sampleHost is the address in every curl line: the server as `make run`
	// starts it.
	sampleHost = "http://127.0.0.1:8080"

	// clientName is the variable name of the PrusaConnect instance in a call.
	clientName = "client"

	// sampleAnchorPrefix keeps a sample's anchor apart from its operation's.
	sampleAnchorPrefix = "sample-"

	// tokenVariable stands in for the credential in a curl line.
	tokenVariable = "$TOKEN"

	// The text media type, the file names a curl line reads a body from, and
	// the fence languages.
	mediaSDP   = "application/sdp"
	offerFile  = "offer.sdp"
	bodyFile   = "file"
	fenceShell = "sh"
	fenceTS    = "ts"
	fenceJSON  = "json"
	fenceText  = "text"
)

// The ways a sample cannot be rendered. Each is a defect in the server's
// example table, to be fixed there.
var (
	errNoSample        = errors.New("the operation carries neither a sample nor an excuse")
	errNoRequestSample = errors.New("the operation takes a body and its sample sends none")
	errNoParameter     = errors.New("a parameter carries no example, so the sample's path cannot be filled")
	errQueryMissing    = errors.New("a required query parameter is not in the sample's query")
)

// pathParameter matches one `{name}` in a route template.
var pathParameter = regexp.MustCompile(`\{([^}]+)\}`)

// sampleRequest is the request a sample sends, with every value filled.
type sampleRequest struct {
	// path is the route with each parameter replaced by its escaped example.
	path string
	// pathValues and queryValues are the values for the client call, in the
	// order the document declares the parameters.
	pathValues  []namedValue
	queryValues []namedValue
	// query is the query string for the curl line, or empty.
	query string
}

// namedValue is one parameter and its sample value as JSON text, so a number
// stays a number.
type namedValue struct {
	name  string
	value string
}

func writeSample(body *strings.Builder, op operation, taken anchors) error {
	if op.SampleExcuse != "" {
		fmt.Fprintf(body, "\nThis operation has no sample: %s.\n",
			docsref.EscapeProse(strings.TrimSuffix(strings.TrimSpace(op.SampleExcuse), ".")))

		return nil
	}

	sample, carried := op.Sample()
	if !carried {
		return fmt.Errorf("%w: %s", errNoSample, op.ID)
	}

	if op.RequestBody != nil && op.RequestBody.Required && len(sample.Request) == 0 {
		return fmt.Errorf("%w: %s", errNoRequestSample, op.ID)
	}

	request, err := fillRequest(op)
	if err != nil {
		return fmt.Errorf("%s: %w", op.ID, err)
	}

	anchor := sampleAnchorPrefix + docsref.Anchor(op.ID)
	if err = taken.claim(anchor, op.ID+" sample"); err != nil {
		return err
	}

	call, err := clientCall(op, sample, request)
	if err != nil {
		return fmt.Errorf("%s: %w", op.ID, err)
	}

	answer, err := renderedResponse(sample)
	if err != nil {
		return fmt.Errorf("%s: %w", op.ID, err)
	}

	fmt.Fprintf(body, "\n#### Sample {#%s}\n\n", anchor)
	fmt.Fprintf(body, "```%s\n%s\n```\n\n", fenceShell, curlLine(op, sample, request))
	fmt.Fprintf(body, "```%s\n%s\n```\n\n", fenceTS, call)
	fmt.Fprintf(body, "The server answers `%s`%s:\n\n", sample.Status, mediaNote(sample.ResponseMedia))
	fmt.Fprintf(body, "```%s\n%s\n```\n", fenceOf(sample.ResponseMedia), answer)

	return nil
}

// mediaNote names the answer's media type when it is not JSON.
func mediaNote(media string) string {
	if media == openapidoc.MediaJSON {
		return ""
	}

	return " as `" + media + "`"
}

func fenceOf(media string) string {
	if media == openapidoc.MediaJSON {
		return fenceJSON
	}

	return fenceText
}

// fillRequest fills the route from the parameter examples and checks the
// sample's query against the declared query parameters.
func fillRequest(op operation) (sampleRequest, error) {
	request := sampleRequest{}

	byName := map[string]openapidoc.Parameter{}
	for _, p := range op.Parameters {
		byName[p.In+" "+p.Name] = p
	}

	var missing error

	request.path = pathParameter.ReplaceAllStringFunc(op.Path, func(match string) string {
		name := strings.Trim(match, "{}")

		value, ok := byName["path "+name].ExampleValue()
		if !ok {
			missing = fmt.Errorf("%w: path parameter %s", errNoParameter, name)

			return match
		}

		request.pathValues = append(request.pathValues, namedValue{name: name, value: string(value)})

		return url.PathEscape(plain(value))
	})

	if missing != nil {
		return sampleRequest{}, missing
	}

	given, err := parseQuery(op.SampleQuery)
	if err != nil {
		return sampleRequest{}, err
	}

	for _, p := range op.ParametersIn("query") {
		value, sent := given[p.Name]

		if !sent {
			if p.Required {
				return sampleRequest{}, fmt.Errorf("%w: %s", errQueryMissing, p.Name)
			}

			continue
		}

		request.queryValues = append(request.queryValues, namedValue{name: p.Name, value: queryLiteral(p, value)})
	}

	request.query = encodeQuery(op.SampleQuery)

	return request, nil
}

// parseQuery reads the sample's query as name to value. A repeated name is
// refused because a client call cannot send it twice.
func parseQuery(query string) (map[string]string, error) {
	given := map[string]string{}

	if query == "" {
		return given, nil
	}

	for pair := range strings.SplitSeq(query, "&") {
		name, value, _ := strings.Cut(pair, "=")

		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return nil, fmt.Errorf("sample query %q: %w", query, err)
		}

		if _, twice := given[name]; twice {
			return nil, fmt.Errorf("sample query %q names %s twice", query, name)
		}

		given[name] = decoded
	}

	return given, nil
}

// encodeQuery escapes each query value but leaves a slash readable. A query
// value may contain one unescaped.
func encodeQuery(query string) string {
	if query == "" {
		return ""
	}

	pairs := strings.Split(query, "&")
	for i, pair := range pairs {
		name, value, _ := strings.Cut(pair, "=")
		decoded, _ := url.QueryUnescape(value)
		pairs[i] = name + "=" + strings.ReplaceAll(url.QueryEscape(decoded), "%2F", "/")
	}

	return strings.Join(pairs, "&")
}

// queryLiteral formats one query value for the client call: bare for a
// number or boolean parameter, quoted otherwise.
func queryLiteral(p openapidoc.Parameter, value string) string {
	switch p.Schema.Name() {
	case "integer", "number", "boolean":
		if json.Valid([]byte(value)) {
			return value
		}
	}

	quoted, _ := json.Marshal(value)

	return string(quoted)
}

// plain returns a JSON scalar as text: a string without its quotes, anything
// else as written.
func plain(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	return string(raw)
}

func curlLine(op operation, sample openapidoc.Sample, request sampleRequest) string {
	target := sampleHost + request.path
	if request.query != "" {
		target += "?" + request.query
	}

	var lines []string

	if op.Method == http.MethodGet {
		lines = append(lines, fmt.Sprintf("curl %q", target))
	} else {
		lines = append(lines, fmt.Sprintf("curl -X %s %q", op.Method, target))
	}

	if !op.Anonymous() {
		lines = append(lines, fmt.Sprintf("-H \"Authorization: Bearer %s\"", tokenVariable))
	}

	for _, name := range slices.Sorted(maps.Keys(op.SampleHeaders)) {
		lines = append(lines, fmt.Sprintf("-H %q", name+": "+op.SampleHeaders[name]))
	}

	if len(sample.Request) > 0 {
		lines = append(lines, fmt.Sprintf("-H %q", "Content-Type: "+sample.RequestMedia))
		lines = append(lines, curlBody(sample, request))
	}

	return strings.Join(lines, " \\\n  ")
}

// curlBody is the body flag: the JSON example inline, or a file for any
// other media type.
func curlBody(sample openapidoc.Sample, request sampleRequest) string {
	if sample.RequestMedia == openapidoc.MediaJSON {
		var pretty bytes.Buffer

		if err := json.Indent(&pretty, sample.Request, "  ", "  "); err != nil {
			pretty.Reset()
			pretty.Write(sample.Request)
		}

		return "-d '" + strings.ReplaceAll(pretty.String(), "'", `'\''`) + "'"
	}

	return "--data-binary @" + uploadedFile(sample.RequestMedia, request)
}

// uploadedFile is the file name a curl line reads a body from. It is the
// last segment of the sample's `path` query value when there is one.
func uploadedFile(media string, request sampleRequest) string {
	if media == mediaSDP {
		return offerFile
	}

	for _, q := range request.queryValues {
		if q.name == "path" {
			path := plain(json.RawMessage(q.value))

			return path[strings.LastIndex(path, "/")+1:]
		}
	}

	return bodyFile
}

// clientCall is the same request as a TypeScript client call. The request
// object has only the members the sample fills.
func clientCall(op operation, sample openapidoc.Sample, request sampleRequest) (string, error) {
	var members []string

	if len(request.pathValues) > 0 {
		members = append(members, "  path: "+inlineObject(request.pathValues)+",")
	}

	if len(request.queryValues) > 0 {
		members = append(members, "  query: "+inlineObject(request.queryValues)+",")
	}

	if len(op.SampleHeaders) > 0 {
		members = append(members, "  header: "+inlineObject(headerValues(op.SampleHeaders))+",")
	}

	if len(sample.Request) > 0 {
		literal, err := bodyLiteral(sample, "  ")
		if err != nil {
			return "", err
		}

		members = append(members, "  body: "+literal+",")
	}

	if len(members) == 0 {
		return fmt.Sprintf("await %s.%s();", clientName, op.Name), nil
	}

	return fmt.Sprintf("await %s.%s({\n%s\n});", clientName, op.Name, strings.Join(members, "\n")), nil
}

// headerValues returns the sample's headers as quoted strings, in name order.
func headerValues(headers map[string]string) []namedValue {
	values := make([]namedValue, 0, len(headers))

	for _, name := range slices.Sorted(maps.Keys(headers)) {
		quoted, _ := json.Marshal(headers[name])
		values = append(values, namedValue{name: name, value: string(quoted)})
	}

	return values
}

// inlineObject renders `{ name: value, other: value }`.
func inlineObject(values []namedValue) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, tsKey(v.name)+": "+v.value)
	}

	return "{ " + strings.Join(parts, ", ") + " }"
}

// bodyLiteral renders the request example as a TypeScript value: an object
// literal for JSON, a template literal for text.
func bodyLiteral(sample openapidoc.Sample, indent string) (string, error) {
	if sample.RequestMedia == openapidoc.MediaJSON {
		return tsLiteral(sample.Request, indent)
	}

	text := plain(sample.Request)
	text = strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${").Replace(text)

	return "`" + text + "`", nil
}

func renderedResponse(sample openapidoc.Sample) (string, error) {
	if sample.ResponseMedia != openapidoc.MediaJSON {
		return strings.TrimRight(plain(sample.Response), "\n"), nil
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, sample.Response, "", "  "); err != nil {
		return "", fmt.Errorf("the response example is not JSON: %w", err)
	}

	return pretty.String(), nil
}

// tsLiteral renders JSON as a TypeScript literal. Identifier keys are bare
// and members keep the order of the document.
func tsLiteral(raw json.RawMessage, indent string) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var out strings.Builder

	if err := writeLiteral(&out, decoder, indent); err != nil {
		return "", fmt.Errorf("the request example is not JSON: %w", err)
	}

	return out.String(), nil
}

func writeLiteral(out *strings.Builder, decoder *json.Decoder, indent string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	switch value := token.(type) {
	case json.Delim:
		return writeCompound(out, decoder, value, indent)
	case string:
		quoted, _ := json.Marshal(value)
		out.Write(quoted)
	case json.Number:
		out.WriteString(value.String())
	case bool:
		fmt.Fprintf(out, "%t", value)
	case nil:
		out.WriteString("null")
	}

	return nil
}

// writeCompound writes an object or an array, one member per line, or `{}`
// or `[]` when it is empty.
func writeCompound(out *strings.Builder, decoder *json.Decoder, open json.Delim, indent string) error {
	isObject := open == '{'
	inner := indent + "  "
	members := 0

	for decoder.More() {
		if members == 0 {
			out.WriteString(string(open) + "\n")
		}

		members++

		out.WriteString(inner)

		if isObject {
			token, err := decoder.Token()
			if err != nil {
				return err
			}

			key, _ := token.(string)
			out.WriteString(tsKey(key) + ": ")
		}

		if err := writeLiteral(out, decoder, inner); err != nil {
			return err
		}

		out.WriteString(",\n")
	}

	// The closing delimiter.
	if _, err := decoder.Token(); err != nil {
		return err
	}

	if members == 0 {
		if isObject {
			out.WriteString("{}")
		} else {
			out.WriteString("[]")
		}

		return nil
	}

	if isObject {
		out.WriteString(indent + "}")
	} else {
		out.WriteString(indent + "]")
	}

	return nil
}

// tsKey returns a member name bare when it is an identifier and quoted
// otherwise, as for a state path like `/tools/0/nozzle_temp`.
func tsKey(name string) string {
	if isIdentifier(name) {
		return name
	}

	quoted, _ := json.Marshal(name)

	return string(quoted)
}

func isIdentifier(name string) bool {
	if name == "" {
		return false
	}

	for i, r := range name {
		letter := r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'

		if !letter && (!digit || i == 0) {
			return false
		}
	}

	return true
}
