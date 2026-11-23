package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAPublishedExampleIsWhatTheHandlerWrites is ADR-20260910-he3 rule 8: every
// sample goes through the real router. It reads the served document, builds
// each operation's request from the sample the document publishes, the path
// filled from the parameter examples, the query the sample declares, the
// request example as the body, sends it through the real handler over the
// fakes in examplesfakes_test.go, and requires the status and the body to be
// what the document publishes beside the request.
//
// The comparison is exact unless shapeOnly names the operation with the state
// no fake can fix, and every operation with a sample is driven here unless
// heldElsewhere names the test that holds it instead. So a sample that differs
// from what its handler writes fails the build, and an exception is a row with
// a reason rather than an operation nobody drove.
func TestAPublishedExampleIsWhatTheHandlerWrites(t *testing.T) {
	t.Parallel()

	document := servedDocument(t, exampleServer(t))
	handler := exampleDataServer(t).Handler()

	driven := 0

	for path, item := range document.Paths {
		for method, op := range item {
			if !op.hasResponseExample() {
				continue
			}

			if reason, elsewhere := heldElsewhere[op.OperationID]; elsewhere {
				require.NotEmptyf(t, reason, "%s is held elsewhere with no reason", op.OperationID)

				continue
			}

			driven++

			t.Run(op.OperationID, func(t *testing.T) {
				t.Parallel()

				own := handler
				if prepare, needed := preparedBy[op.OperationID]; needed {
					own = prepare(t, document)
				}

				roundTrip(t, own, strings.ToUpper(method), path, op)
			})
		}
	}

	require.GreaterOrEqual(t, driven, 60, "this gate drove no samples, or almost none")

	for operation := range heldElsewhere {
		require.Containsf(t, operationExamples(), operation,
			"heldElsewhere names %s, which publishes no sample. Remove the row", operation)
	}

	for operation := range shapeOnly {
		_, elsewhere := heldElsewhere[operation]
		require.Falsef(t, elsewhere, "%s is both held elsewhere and compared by shape", operation)
		require.Containsf(t, operationExamples(), operation,
			"shapeOnly names %s, which publishes no sample. Remove the row", operation)
	}
}

// heldElsewhere are the samples another test drives through the router,
// each with the reason this one cannot.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var heldElsewhere = map[string]string{
	"open-page-session": "it needs an app pairing credential in the Authorization header, which a " +
		"sample request does not carry. TestAPageSessionStandsInForTheAppsCredential drives it",
	"stream-events": "the sample is a transcript of three frames, and producing it needs a hub holding " +
		"a stored event at offset 412 and a telemetry sample in flight. " +
		"TestTheEventStreamExampleIsAParseableStream holds its shape",
	"get-setup": "the sample is the pending answer and every other sample needs a server that is " +
		"not pending, because a pending one refuses them. " +
		"TestTheFirstRunStateIsWhatThePageReads drives it",
	"record-setup": "the decision is recorded once, and a server that has recorded it answers 409. " +
		"TestRecordingRequiredSignsTheAdministratorIn drives it",
	"get-setup-configuration": "it answers only while the first run is pending, and every other sample " +
		"needs a server that is not. TestTheWizardReadsTheConfigurationBehindTheCode drives it",
	"create-setup-certificates": "it writes files, and answers only while the first run is pending. " +
		"TestAMadeAuthoritySignsBothCertificates drives the files",
	"check-setup-storage": "it answers only while the first run is pending. " +
		"TestTheWizardChecksAFolderBehindTheCode drives it",
	"check-setup-oidc": "it answers only while the first run is pending, and it calls the provider. " +
		"TestTheWizardTriesAProviderBehindTheCode drives it",
	"list-vocabularies":            vocabularyReason,
	"get-error-code-vocabulary":    vocabularyReason,
	"get-event-type-vocabulary":    vocabularyReason,
	"get-resource-kind-vocabulary": vocabularyReason,
	"get-capability-vocabulary":    vocabularyReason,
	"get-authorization-vocabulary": vocabularyReason,
	"get-device-issue-vocabulary":  vocabularyReason,
	"get-device-model-vocabulary":  vocabularyReason,
	"get-device-state-vocabulary": "the sample shows three states of a reference dialect and the served set is " +
		"what the connectors this server mounted declare, which a test server mounts none of",
	"get-extension-vocabulary": "the sample shows one connector and the served catalogue is what this binary " +
		"registered, which under go test is empty. Nothing holds its entries yet",
}

// vocabularyReason is why a vocabulary sample is compared by containment in
// TestAVocabularyExampleEntryIsOneTheServerSends rather than whole here.
const vocabularyReason = "the answer is the whole closed set and the sample shows a few entries of it; " +
	"TestAVocabularyExampleEntryIsOneTheServerSends holds each entry to the served answer"

// shapeOnly are the samples whose answer is compared member by member for
// shape rather than value, each with the state no fake can fix.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var shapeOnly = map[string]string{
	"request-app-pairing": "the verification page is built from the host the request reached, which the " +
		"test request names example.com, and the codes are drawn at random",
	"get-developer-device-commands": "the log records the instant a command was sent and how long it " +
		"took, from the clock, so at and elapsed_ms are never the sample's",
	"get-developer-connectors": "the connectors are read from the registry this binary was built with, " +
		"which under go test holds no dialect, so only the routes can match",
}

// preparedBy are the samples whose answer is a record of earlier requests,
// each with the requests that write the record. The record is written on a
// server of its own, so what the other samples send in parallel is not in it.
//
//nolint:gochecknoglobals // a lookup table, read-only after init.
var preparedBy = map[string]func(*testing.T, publishedDocument) http.Handler{
	// The developer log holds what this process sent, so a command is sent
	// first: the one the command sample sends.
	"get-developer-device-commands": func(t *testing.T, document publishedDocument) http.Handler {
		t.Helper()

		handler := exampleDataServer(t).Handler()

		op, method, path := document.operation(t, "send-device-command")
		roundTrip(t, handler, method, path, op)

		return handler
	},
}

// roundTrip sends one operation's sample through the handler and holds the
// answer to the response sample.
func roundTrip(t *testing.T, handler http.Handler, method, template string, op publishedOperation) {
	t.Helper()

	target := op.samplePath(t, template)

	var request *http.Request

	media, body := op.requestSample(t)
	if body == nil {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(string(body)))
		request.Header.Set("Content-Type", media)
	}

	for name, value := range op.SampleHeaders {
		request.Header.Set(name, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)

	status, responseMedia, published := op.responseSample(t)

	require.Equalf(t, status, rec.Code, "%s %s answered %d, and the sample is on %d: %s",
		method, target, rec.Code, status, rec.Body.String())

	got := rec.Body.String()

	switch {
	case responseMedia != jsonMedia:
		require.Equalf(t, string(published), got,
			"the %s example published for %s is not what the handler writes", responseMedia, op.OperationID)
	case shapeOnly[op.OperationID] != "":
		requireSameShape(t, string(published), got, op.OperationID)
	default:
		require.JSONEqf(t, string(published), got,
			"the example published for %s is not what the handler writes for the value it was built from",
			op.OperationID)
	}
}

// pathParameter matches one `{name}` in a route template.
var pathParameter = regexp.MustCompile(`\{([^}]+)\}`)

// samplePath is the route with every parameter filled from its example and
// the sample's query appended.
func (op publishedOperation) samplePath(t *testing.T, template string) string {
	t.Helper()

	examples := map[string]string{}

	for _, param := range op.Parameters {
		if param.In != "path" {
			continue
		}

		raw, carried := param.example()
		require.Truef(t, carried, "%s: the path parameter %s carries no example", op.OperationID, param.Name)

		var value string

		require.NoError(t, json.Unmarshal(raw, &value))

		examples[param.Name] = url.PathEscape(value)
	}

	filled := pathParameter.ReplaceAllStringFunc(template, func(match string) string {
		value, known := examples[strings.Trim(match, "{}")]
		require.Truef(t, known, "%s: %s is in the route and not in its parameters", op.OperationID, match)

		return value
	})

	if op.SampleQuery == "" {
		return filled
	}

	return filled + "?" + op.SampleQuery
}

// requestSample is the body the sample sends and its media type, or nil for
// an operation that sends none. A text body is published as a JSON string and
// sent as the text.
func (op publishedOperation) requestSample(t *testing.T) (string, []byte) {
	t.Helper()

	if op.RequestBody == nil {
		return "", nil
	}

	for media, content := range op.RequestBody.Content {
		if len(content.Example) == 0 {
			continue
		}

		if media == jsonMedia {
			return media, content.Example
		}

		var text string

		require.NoErrorf(t, json.Unmarshal(content.Example, &text),
			"%s: the %s request example is not a string", op.OperationID, media)

		return media, []byte(text)
	}

	return "", nil
}

// responseSample is the status the sample answers, its media type and the
// body: JSON as published, a text answer decoded from its JSON string.
func (op publishedOperation) responseSample(t *testing.T) (int, string, []byte) {
	t.Helper()

	statuses := make([]string, 0, len(op.Responses))
	for status := range op.Responses {
		statuses = append(statuses, status)
	}

	sort.Strings(statuses)

	for _, status := range statuses {
		for media, content := range op.Responses[status].Content {
			if len(content.Example) == 0 {
				continue
			}

			code, err := strconv.Atoi(status)
			require.NoError(t, err)

			if media == jsonMedia {
				return code, media, content.Example
			}

			var text string

			require.NoErrorf(t, json.Unmarshal(content.Example, &text),
				"%s: the %s response example is not a string", op.OperationID, media)

			return code, media, []byte(text)
		}
	}

	t.Fatalf("%s publishes no response example", op.OperationID)

	return 0, "", nil
}

// operation finds one operation in the served document with its method and
// route.
func (d publishedDocument) operation(t *testing.T, operationID string) (publishedOperation, string, string) {
	t.Helper()

	for path, item := range d.Paths {
		for method, op := range item {
			if op.OperationID == operationID {
				return op, strings.ToUpper(method), path
			}
		}
	}

	t.Fatalf("the document publishes no operation called %s", operationID)

	return publishedOperation{}, "", ""
}

// requireSameShape holds two JSON documents to one shape: the same members
// at every level, each of the same kind, and a list's elements of the shape
// the sample's first element has.
func requireSameShape(t *testing.T, published, got, operation string) {
	t.Helper()

	var want, have any

	require.NoError(t, json.Unmarshal([]byte(published), &want))
	require.NoErrorf(t, json.Unmarshal([]byte(got), &have),
		"%s answered something that is not JSON: %s", operation, got)

	if problem := shapeDifference(want, have, "$"); problem != "" {
		t.Fatalf("the example published for %s has a different shape from what the handler writes: %s\n"+
			"published: %s\nanswered:  %s", operation, problem, published, got)
	}
}

// shapeDifference is the first place two decoded documents differ in shape,
// or empty when they do not.
func shapeDifference(want, have any, at string) string {
	switch expected := want.(type) {
	case map[string]any:
		actual, ok := have.(map[string]any)
		if !ok {
			return at + " is an object in the sample and not in the answer"
		}

		for key := range expected {
			if _, present := actual[key]; !present {
				return at + "." + key + " is in the sample and not in the answer"
			}
		}

		for key := range actual {
			if _, present := expected[key]; !present {
				return at + "." + key + " is in the answer and not in the sample"
			}
		}

		for key, value := range expected {
			if problem := shapeDifference(value, actual[key], at+"."+key); problem != "" {
				return problem
			}
		}
	case []any:
		actual, ok := have.([]any)
		if !ok {
			return at + " is a list in the sample and not in the answer"
		}

		if len(expected) > 0 && len(actual) > 0 {
			return shapeDifference(expected[0], actual[0], at+"[0]")
		}
	default:
		if kindOf(want) != kindOf(have) {
			return at + " is a " + kindOf(want) + " in the sample and a " + kindOf(have) + " in the answer"
		}
	}

	return ""
}

func kindOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	default:
		return "value"
	}
}
