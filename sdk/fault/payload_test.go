package fault_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	wantCore = `"code":"` + rejectedCode + `",` +
		`"title":"Printer rejected the command",` +
		`"detail":"Printer MK4-07 is printing and cannot accept start_job.",` +
		`"params":{"printer_name":"MK4-07","state":"PRINTING","command":"start_job"},` +
		`"retryable":false`

	wantTrace = `,"trace_id":"` + traceID + `"`
)

func TestRenderings(t *testing.T) {
	t.Parallel()

	failure := rejectedFault(t)

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "payload, which is also the printer command result",
			value: failure.Payload(),
			want:  `{` + wantCore + wantTrace + `}`,
		},
		{
			name:  "problem+json",
			value: failure.ProblemJSON("/api/v1/printers/mk4-07/commands"),
			want: `{` + wantCore + wantTrace +
				`,"type":"` + fault.TypeBaseURI + `#io-github-acme-s3-command-rejected"` +
				`,"status":409` +
				`,"instance":"/api/v1/printers/mk4-07/commands"}`,
		},
		{
			// The envelope carries the trace id as an attribute of its own.
			name:  "event envelope data.error",
			value: failure.EventError(),
			want:  `{` + wantCore + `}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			if string(encoded) != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", encoded, tt.want)
			}
		})
	}
}

func TestProblemJSONWithoutAnInstance(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(unreachable(t).New("").ProblemJSON(""))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if strings.Contains(string(encoded), "instance") || strings.Contains(string(encoded), "detail") {
		t.Fatalf("empty members were rendered: %s", encoded)
	}

	if !strings.Contains(string(encoded), `"params":{}`) {
		t.Fatalf("params is not an empty object: %s", encoded)
	}

	if !strings.Contains(string(encoded), `"retryable":true`) {
		t.Fatalf("retryable is missing: %s", encoded)
	}
}

func TestRedactedCodeRendersWithoutDetailOrParams(t *testing.T) {
	t.Parallel()

	failure := internal(t).
		New("insert into printers failed: duplicate key /var/lib/pcl/pcl.db").
		With(fault.StringParam("query", "insert into printers")).
		WithTrace(traceID)

	for _, surface := range renderings(failure, "/api/v1/printers") {
		t.Run(surface.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(surface.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			for _, leaked := range []string{"insert into", "duplicate key", "/var/lib/pcl", "query", "detail"} {
				if strings.Contains(string(encoded), leaked) {
					t.Fatalf("%q reached the wire: %s", leaked, encoded)
				}
			}

			if !strings.Contains(string(encoded), `"title":"The server failed to handle the request"`) {
				t.Fatalf("the English title did not ship: %s", encoded)
			}
		})
	}

	if failure.Message() == "" {
		t.Fatal("the message was dropped from the fault itself, not only from the rendering")
	}
}

func TestWrappedCauseNeverReachesARendering(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial tcp 10.0.0.7:5000: connect: connection refused")
	failure := unreachable(t).New("Printer MK4-07 did not answer.").
		With(fault.StringParam("printer_name", "MK4-07")).
		Wrap(cause)

	for _, surface := range renderings(failure, "/api/v1/printers/mk4-07") {
		t.Run(surface.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(surface.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			for _, leaked := range []string{"10.0.0.7", "connection refused"} {
				if strings.Contains(string(encoded), leaked) {
					t.Fatalf("the cause reached the wire: %s", encoded)
				}
			}
		})
	}

	if !errors.Is(failure, cause) {
		t.Fatal("the cause is not reachable in process")
	}
}

// A payload that was never built through a Fault must not marshal into
// something a client would read as a first-party problem document.
func TestHandBuiltPayloadsRefuseToMarshal(t *testing.T) {
	t.Parallel()

	values := []rendering{
		{name: "payload", value: fault.Payload{}},
		{name: "problem", value: fault.Problem{}},
		{name: "event", value: fault.EventError{}},
		{
			name: "a type URI borrowed from a core code",
			value: fault.Problem{
				Type:   fault.TypeBaseURI + "core.auth/SESSION_EXPIRED",
				Status: http.StatusUnauthorized,
			},
		},
	}

	for _, surface := range values {
		t.Run(surface.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(surface.value)
			if !errors.Is(err, fault.ErrInvalidCode) {
				t.Fatalf("Marshal = %s, %v, want %v", encoded, err, fault.ErrInvalidCode)
			}
		})
	}
}

func TestUnregisteredCodeCannotRenderAsAFirstPartyOne(t *testing.T) {
	t.Parallel()

	forged, err := fault.NewCode(acmeDomain(t), "SESSION_EXPIRED")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	encoded, err := json.Marshal(fault.New(forged, "you are signed out").ProblemJSON("/api/v1/session"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if strings.Contains(string(encoded), "SESSION_EXPIRED") || strings.Contains(string(encoded), "signed out") {
		t.Fatalf("an unregistered code rendered as itself: %s", encoded)
	}

	if !strings.Contains(string(encoded), `"code":"core.internal/UNREGISTERED_CODE"`) {
		t.Fatalf("the fault of last resort did not answer: %s", encoded)
	}
}

func TestParamScalars(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()
	def := registry.Register(acmeDomain(t), "SCALARS", http.StatusConflict, fault.Document(
		"Every scalar kind",
		fault.NoAction,
		fault.RetryNever,
		fault.ShowDetail,
		"command", "attempts", "nozzle_temp", "queued",
	))

	failure := def.New("").With(
		fault.StringParam("command", "start_job"),
		fault.IntParam("attempts", 3),
		fault.FloatParam("nozzle_temp", 215.5),
		fault.BoolParam("queued", true),
	)

	encoded, err := json.Marshal(failure.Payload())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `"params":{"command":"start_job","attempts":3,"nozzle_temp":215.5,"queued":true}`
	if !strings.Contains(string(encoded), want) {
		t.Fatalf("got\n%s\nwant it to contain\n%s", encoded, want)
	}

	if got := strings.Join(failure.Params().Keys(), ","); got != "command,attempts,nozzle_temp,queued" {
		t.Fatalf("Keys() = %q", got)
	}

	if _, ok := failure.Params().Get("missing"); ok {
		t.Fatal("Get found a param that was never set")
	}
}

func TestParamKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		param    fault.Param
		wantKind fault.ParamKind
		wantText string
	}{
		{name: "string", param: fault.StringParam("k", "v"), wantKind: fault.KindString, wantText: "v"},
		{name: "int", param: fault.IntParam("k", -7), wantKind: fault.KindInt, wantText: "-7"},
		{name: "float", param: fault.FloatParam("k", 215.5), wantKind: fault.KindFloat, wantText: "215.5"},
		{name: "bool", param: fault.BoolParam("k", false), wantKind: fault.KindBool, wantText: "false"},
		{name: "unset", param: fault.Param{Key: "k"}, wantKind: 0, wantText: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.param.Value.Kind() != tt.wantKind {
				t.Fatalf("Kind() = %d, want %d", tt.param.Value.Kind(), tt.wantKind)
			}

			if tt.param.Value.String() != tt.wantText {
				t.Fatalf("String() = %q, want %q", tt.param.Value.String(), tt.wantText)
			}
		})
	}
}

// rendering is one surface's view of a fault, named for the subtest it drives.
type rendering struct {
	name  string
	value any
}

func renderings(failure *fault.Fault, instance string) []rendering {
	return []rendering{
		{name: "payload", value: failure.Payload()},
		{name: "problem+json", value: failure.ProblemJSON(instance)},
		{name: "event envelope data.error", value: failure.EventError()},
	}
}

// TestPayloadRoundTripsThroughJSON is a design requirement applied to the error
// shape: a consumer that receives one of these has to be able to decode it into
// the published type, or the contract is one-directional.
func TestPayloadRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(rejectedFault(t).Payload())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded fault.Payload
	if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
		t.Fatalf("unmarshal: %v", decodeErr)
	}

	if decoded.Code.String() != rejectedCode {
		t.Fatalf("code = %q, want %q", decoded.Code, rejectedCode)
	}

	if decoded.TraceID != traceID || decoded.Retryable {
		t.Fatalf("payload lost its members: %+v", decoded)
	}

	if got := strings.Join(decoded.Params.Keys(), ","); got != "printer_name,state,command" {
		t.Fatalf("param order = %q, want the order it was written in", got)
	}

	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	if string(again) != string(encoded) {
		t.Fatalf("re-encoded as %s, want %s", again, encoded)
	}
}

func TestParamValuesKeepTheirKindThroughJSON(t *testing.T) {
	t.Parallel()

	written := fault.Params{
		fault.IntParam("job_id", 9007199254740993),
		fault.FloatParam("progress", 42.5),
		fault.BoolParam("retryable", true),
		fault.StringParam("printer", "MK4-07"),
	}

	encoded, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var read fault.Params
	if decodeErr := json.Unmarshal(encoded, &read); decodeErr != nil {
		t.Fatalf("unmarshal: %v", decodeErr)
	}

	for i, param := range written {
		if read[i].Key != param.Key || read[i].Value.Kind() != param.Value.Kind() {
			t.Fatalf("param %d = %+v, want %+v", i, read[i], param)
		}

		if read[i].Value.String() != param.Value.String() {
			t.Fatalf("param %q = %q, want %q", param.Key, read[i].Value.String(), param.Value.String())
		}
	}
}

func TestUnmarshalRejectsWhatIsNotThePayloadShape(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"code is not domain/REASON": `{"code":"nonsense","params":{}}`,
		"code is not text":          `{"code":42,"params":{}}`,
		"params are not an object":  `{"code":"` + rejectedCode + `","params":[1,2]}`,
		"a param is not a scalar":   `{"code":"` + rejectedCode + `","params":{"a":{"b":1}}}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var decoded fault.Payload
			if err := json.Unmarshal([]byte(body), &decoded); err == nil {
				t.Fatalf("decoded %s without complaint", body)
			}
		})
	}
}
