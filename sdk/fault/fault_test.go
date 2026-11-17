// The tests are an external package on purpose: they reach the taxonomy
// through the same exported surface a third party compiles against, so a
// property that only holds from inside the package cannot pass here.
package fault_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	rejectedCode = "io.github.acme.s3/COMMAND_REJECTED"
	traceID      = "0af7651916cd43dd8448eb211c80319c"
)

// acmeDomain mints a plugin domain the way a host does from a manifest id.
func acmeDomain(t *testing.T) fault.Domain {
	t.Helper()

	domain, err := fault.NewDomain("io.github.acme.s3")
	if err != nil {
		t.Fatalf("NewDomain: %v", err)
	}

	return domain
}

// rejected returns a definition in a registry of its own, so no test shares
// registry state with another.
func rejected(t *testing.T) *fault.Definition {
	t.Helper()

	return fault.NewRegistry().Register(acmeDomain(t), "COMMAND_REJECTED", http.StatusConflict, fault.Document(
		"Printer rejected the command",
		"Wait for the printer to leave its current state, then send the command again.",
		fault.RetryNever,
		fault.ShowDetail,
		"printer_name", "state", "command",
	))
}

func unreachable(t *testing.T) *fault.Definition {
	t.Helper()

	return fault.NewRegistry().Register(acmeDomain(t), "UNREACHABLE", http.StatusServiceUnavailable, fault.Document(
		"Printer did not answer",
		"Check that the printer is powered on and on the network, then retry.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"printer_name",
	))
}

func internal(t *testing.T) *fault.Definition {
	t.Helper()

	return fault.NewRegistry().Register(acmeDomain(t), "STORE_FAILED", http.StatusInternalServerError, fault.Document(
		"The server failed to handle the request",
		fault.NoAction,
		fault.RetryNever,
		fault.RedactDetail,
	))
}

func rejectedFault(t *testing.T) *fault.Fault {
	t.Helper()

	return rejected(t).New("Printer MK4-07 is printing and cannot accept start_job.").
		With(
			fault.StringParam("printer_name", "MK4-07"),
			fault.StringParam("state", "PRINTING"),
			fault.StringParam("command", "start_job"),
		).
		WithTrace(traceID)
}

func TestDefinitionNewCarriesTheRegisteredDocumentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		def           func(*testing.T) *fault.Definition
		wantCode      string
		wantTitle     string
		wantStatus    int
		wantRetryable bool
		wantRedacted  bool
	}{
		{
			name:       "not retryable",
			def:        rejected,
			wantCode:   rejectedCode,
			wantTitle:  "Printer rejected the command",
			wantStatus: http.StatusConflict,
		},
		{
			name:          "retryable",
			def:           unreachable,
			wantCode:      "io.github.acme.s3/UNREACHABLE",
			wantTitle:     "Printer did not answer",
			wantStatus:    http.StatusServiceUnavailable,
			wantRetryable: true,
		},
		{
			name:         "redacted",
			def:          internal,
			wantCode:     "io.github.acme.s3/STORE_FAILED",
			wantTitle:    "The server failed to handle the request",
			wantStatus:   http.StatusInternalServerError,
			wantRedacted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			failure := tt.def(t).New("something happened")

			if failure.Code().String() != tt.wantCode {
				t.Fatalf("Code() = %q, want %q", failure.Code(), tt.wantCode)
			}

			if failure.Title() != tt.wantTitle {
				t.Fatalf("Title() = %q, want %q", failure.Title(), tt.wantTitle)
			}

			if failure.Status() != tt.wantStatus {
				t.Fatalf("Status() = %d, want %d", failure.Status(), tt.wantStatus)
			}

			if failure.Retryable() != tt.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", failure.Retryable(), tt.wantRetryable)
			}

			if failure.Redacted() != tt.wantRedacted {
				t.Fatalf("Redacted() = %v, want %v", failure.Redacted(), tt.wantRedacted)
			}

			if failure.Message() != "something happened" {
				t.Fatalf("Message() = %q", failure.Message())
			}
		})
	}
}

func TestBuildersCopy(t *testing.T) {
	t.Parallel()

	t.Run("with does not touch the original", func(t *testing.T) {
		t.Parallel()

		base := unreachable(t).New("Printer MK4-07 did not answer.")
		derived := base.With(fault.StringParam("printer_name", "MK4-07"))

		if len(base.Params()) != 0 {
			t.Fatalf("base gained params: %v", base.Params().Keys())
		}

		if got, ok := derived.Params().Get("printer_name"); !ok || got.String() != "MK4-07" {
			t.Fatalf("derived params = %v", derived.Params().Keys())
		}
	})

	t.Run("a repeated key replaces the value", func(t *testing.T) {
		t.Parallel()

		derived := unreachable(t).New("").
			With(fault.StringParam("printer_name", "MK4-07")).
			With(fault.StringParam("printer_name", "MK4-08"))

		if len(derived.Params()) != 1 {
			t.Fatalf("params = %v, want one entry", derived.Params().Keys())
		}

		if got, _ := derived.Params().Get("printer_name"); got.String() != "MK4-08" {
			t.Fatalf("printer_name = %q", got)
		}
	})

	t.Run("retry hint can be overridden", func(t *testing.T) {
		t.Parallel()

		base := unreachable(t).New("")

		if !base.Retryable() {
			t.Fatal("fixture is not retryable")
		}

		if base.WithRetry(fault.RetryNever).Retryable() {
			t.Fatal("WithRetry(RetryNever) stayed retryable")
		}

		if !base.Retryable() {
			t.Fatal("WithRetry mutated the original")
		}
	})

	t.Run("trace id is carried", func(t *testing.T) {
		t.Parallel()

		base := unreachable(t).New("")
		traced := base.WithTrace(traceID)

		if traced.TraceID() != traceID {
			t.Fatalf("TraceID() = %q", traced.TraceID())
		}

		if base.TraceID() != "" {
			t.Fatal("WithTrace mutated the original")
		}
	})

	t.Run("params handed out are a copy", func(t *testing.T) {
		t.Parallel()

		derived := unreachable(t).New("").With(fault.StringParam("printer_name", "MK4-07"))
		params := derived.Params()
		params[0] = fault.StringParam("printer_name", "mutated")

		if got, _ := derived.Params().Get("printer_name"); got.String() != "MK4-07" {
			t.Fatalf("Params() handed out the fault's own slice: %q", got)
		}
	})
}

func TestWithDropsParamsTheCodeDoesNotDeclare(t *testing.T) {
	t.Parallel()

	failure := unreachable(t).New("Printer MK4-07 did not answer.").With(
		fault.StringParam("printer_name", "MK4-07"),
		fault.StringParam("", "empty key"),
		fault.StringParam("Not Snake Case", "malformed key"),
		fault.StringParam("undeclared", "never documented"),
	)

	if got := strings.Join(failure.Params().Keys(), ","); got != "printer_name" {
		t.Fatalf("params = %q, want only the declared one", got)
	}

	line := failure.Error()
	for _, dropped := range []string{"Not Snake Case", "undeclared"} {
		if !strings.Contains(line, dropped) {
			t.Fatalf("Error() = %q, want it to name the dropped param %q", line, dropped)
		}
	}
}

func TestErrorsIs(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("dial tcp 10.0.0.7:80: connection refused")
	def := rejected(t)
	other := unreachable(t)

	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{name: "same code", err: def.New("rejected"), target: def.New("any other message"), want: true},
		{name: "different code", err: def.New("rejected"), target: other.New("unreachable")},
		{
			name:   "through fmt.Errorf",
			err:    fmt.Errorf("send command: %w", def.New("rejected")),
			target: def.New(""),
			want:   true,
		},
		{name: "reaches the wrapped cause", err: other.New("").Wrap(errBoom), target: errBoom, want: true},
		{name: "a plain error is not a fault", err: errBoom, target: def.New("")},
		{name: "a nil fault target matches nothing", err: def.New(""), target: (*fault.Fault)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Fatalf("errors.Is = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorsAsReachesTheCause(t *testing.T) {
	t.Parallel()

	cause := &netError{host: "10.0.0.7"}
	failure := unreachable(t).New("Printer MK4-07 did not answer.").Wrap(cause)

	var target *netError
	if !errors.As(failure, &target) {
		t.Fatal("errors.As did not reach the wrapped cause")
	}

	if target.host != "10.0.0.7" {
		t.Fatalf("wrapped cause = %+v", target)
	}

	var asFault *fault.Fault
	if !errors.As(fmt.Errorf("send command: %w", failure), &asFault) {
		t.Fatal("errors.As did not reach the fault through fmt.Errorf")
	}

	if asFault.Code() != failure.Code() {
		t.Fatalf("Code() = %q", asFault.Code())
	}
}

func TestHasCode(t *testing.T) {
	t.Parallel()

	def := rejected(t)
	wrapped := fmt.Errorf("send command: %w", def.New("rejected"))

	tests := []struct {
		name string
		err  error
		code fault.Code
		want bool
	}{
		{name: "through a wrap", err: wrapped, code: def.Code(), want: true},
		{name: "wrong code", err: wrapped, code: unreachable(t).Code()},
		{name: "plain error", err: errors.New("plain"), code: def.Code()},
		{name: "nil error", err: nil, code: def.Code()},
		{name: "zero code matches nothing", err: wrapped, code: fault.Code{}},
		{name: "typed nil fault", err: (*fault.Fault)(nil), code: def.Code()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fault.HasCode(tt.err, tt.code); got != tt.want {
				t.Fatalf("HasCode = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorStringIsForLogs(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial tcp 10.0.0.7:80: connection refused")
	def := unreachable(t)
	failure := def.New("Printer MK4-07 did not answer.").Wrap(cause)

	want := def.Code().String() + ": Printer MK4-07 did not answer.: " + cause.Error()
	if failure.Error() != want {
		t.Fatalf("Error() = %q, want %q", failure.Error(), want)
	}

	if got := def.New("").Error(); got != def.Code().String() {
		t.Fatalf("Error() without a message = %q", got)
	}

	if !errors.Is(failure.Unwrap(), cause) {
		t.Fatalf("Unwrap() = %v", failure.Unwrap())
	}
}

// A nil or zero value reaches these methods on the error path, in code the host
// cannot patch. None of them may panic.
func TestNilAndZeroValuesDoNotPanic(t *testing.T) {
	t.Parallel()

	var nilFault *fault.Fault

	var nilDefinition *fault.Definition

	zero := &fault.Fault{}

	faults := []struct {
		name    string
		failure *fault.Fault
	}{
		{name: "nil fault", failure: nilFault},
		{name: "zero fault", failure: zero},
		{name: "from a nil definition", failure: nilDefinition.New("no definition")},
	}

	for _, tt := range faults {
		failure := tt.failure

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := failure.Code().String(); got != "core.internal/UNREGISTERED_CODE" {
				t.Fatalf("Code() = %q", got)
			}

			if failure.Title() == "" || failure.Status() != http.StatusInternalServerError {
				t.Fatalf("Title()/Status() = %q/%d", failure.Title(), failure.Status())
			}

			if !failure.Redacted() || failure.Retryable() {
				t.Fatalf("Redacted()/Retryable() = %v/%v", failure.Redacted(), failure.Retryable())
			}

			if failure.Error() == "" || failure.Params() != nil || failure.TraceID() != "" {
				t.Fatalf("Error()/Params()/TraceID() = %q/%v/%q", failure.Error(), failure.Params(), failure.TraceID())
			}

			if failure.Unwrap() != nil {
				t.Fatalf("Unwrap() = %v", failure.Unwrap())
			}

			derived := failure.With(fault.StringParam("printer_name", "MK4-07")).
				Wrap(errors.New("cause")).
				WithTrace(traceID).
				WithRetry(fault.RetryAllowed)

			if derived.TraceID() != traceID || !derived.Retryable() {
				t.Fatalf("builders on a valueless fault = %q/%v", derived.TraceID(), derived.Retryable())
			}

			if len(failure.Payload().Params) != 0 || failure.Payload().Detail != "" {
				t.Fatalf("payload = %+v", failure.Payload())
			}
		})
	}

	if nilFault.Error() == "" {
		t.Fatal("a nil fault has no error text")
	}

	if fault.HasCode(nilFault, nilDefinition.Code()) {
		t.Fatal("HasCode matched a zero code")
	}
}

// registered counts the codes this case has taken. See freshCode.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var registered atomic.Uint64

// freshCode is a code nothing has registered yet.
//
// The global registry is the process's own and nothing takes a code out of it
// again, so a fixed code is registered once and every run after that is
// refused. Under -count=2 the registration below failed, which is the trap
// somebody chasing a flake with -count falls into first.
func freshCode(stem string) string {
	return stem + "_" + strconv.FormatUint(registered.Add(1), 10)
}

func TestNewResolvesThroughTheGlobalRegistry(t *testing.T) {
	domain, err := fault.NewDomain("io.github.prusa3d.faulttest")
	if err != nil {
		t.Fatalf("NewDomain: %v", err)
	}

	def, err := fault.TryRegister(domain, freshCode("GLOBAL_PATH"), http.StatusConflict, fault.Document(
		"Registered in the global registry",
		fault.NoAction,
		fault.RetryNever,
		fault.ShowDetail,
		"printer_name",
	))
	if err != nil {
		t.Fatalf("TryRegister: %v", err)
	}

	resolved := fault.New(def.Code(), "resolved through the global registry")

	if resolved.Code() != def.Code() || resolved.Title() != "Registered in the global registry" {
		t.Fatalf("New = %q/%q", resolved.Code(), resolved.Title())
	}

	if resolved.Payload().Detail != "resolved through the global registry" {
		t.Fatalf("detail = %q", resolved.Payload().Detail)
	}

	if !fault.Global().Registered(def.Code()) {
		t.Fatalf("%q is missing from the global registry", def.Code())
	}
}

func TestNewWithAnUnregisteredCodeIsTheFaultOfLastResort(t *testing.T) {
	t.Parallel()

	unknown, err := fault.NewCode(acmeDomain(t), "NEVER_REGISTERED")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	failure := fault.New(unknown, "the printer said no")

	if got := failure.Code().String(); got != "core.internal/UNREGISTERED_CODE" {
		t.Fatalf("Code() = %q", got)
	}

	if !failure.Redacted() || failure.Status() != http.StatusInternalServerError {
		t.Fatalf("Redacted()/Status() = %v/%d", failure.Redacted(), failure.Status())
	}

	payload := failure.Payload()
	if payload.Detail != "" || len(payload.Params) != 0 {
		t.Fatalf("payload carried detail: %+v", payload)
	}

	line := failure.Error()
	if !strings.Contains(line, unknown.String()) || !strings.Contains(line, "the printer said no") {
		t.Fatalf("Error() = %q, want the requested code and message in the log line", line)
	}
}

type netError struct {
	host string
}

func (e *netError) Error() string { return "no route to " + e.host }
