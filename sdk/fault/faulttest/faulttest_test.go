package faulttest_test

import (
	"fmt"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/fault/faulttest"
)

// The assertions are tested against a registry of their own rather than the
// core catalogue: this package tests the gate, and the codes the server
// registers are the gate's subject, not its fixture.
const (
	knownCode   = "io.github.faulttest/KNOWN"
	unknownCode = "io.github.faulttest/UNKNOWN"
)

// recorder stands in for *testing.T so a gate can be seen failing. The embedded
// nil TB is deliberate: an assertion that calls anything but Helper or Errorf
// panics here rather than passing quietly.
type recorder struct {
	testing.TB

	failures []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func TestAssertBodyRegistered(t *testing.T) {
	t.Parallel()

	registry := registryWithKnownCode(t)

	tests := []struct {
		name       string
		body       string
		wantFailed bool
	}{
		{
			name:       "registered code in a problem document",
			body:       `{"type":"x","code":"` + knownCode + `","title":"t","status":507}`,
			wantFailed: false,
		},
		{
			name:       "unregistered code",
			body:       `{"code":"` + unknownCode + `","title":"t"}`,
			wantFailed: true,
		},
		{
			name:       "code nested under an event envelope",
			body:       `{"data":{"error":{"code":"` + knownCode + `"}}}`,
			wantFailed: false,
		},
		{
			name:       "unregistered code nested in a list",
			body:       `[{"error":{"code":"` + knownCode + `"}},{"error":{"code":"` + unknownCode + `"}}]`,
			wantFailed: true,
		},
		{
			name:       "no code at all",
			body:       `{"printers":[{"id":"p1","state":"IDLE"}]}`,
			wantFailed: false,
		},
		{
			name:       "code member that is not a code",
			body:       `{"printer":{"code":17}}`,
			wantFailed: false,
		},
		{
			name:       "malformed code text",
			body:       `{"code":"not a code"}`,
			wantFailed: true,
		},
		{
			name:       "truncated document",
			body:       `{"code":`,
			wantFailed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rec := &recorder{}
			faulttest.AssertBodyRegistered(rec, registry, []byte(test.body))

			if failed := len(rec.failures) > 0; failed != test.wantFailed {
				t.Fatalf("failed = %v, want %v (failures: %v)", failed, test.wantFailed, rec.failures)
			}
		})
	}
}

// A nil registry means the global one, which is what a server binary answers
// from and what a handler test passes by default.
func TestAssertRegisteredFallsBackToTheGlobalRegistry(t *testing.T) {
	t.Parallel()

	registered := fault.Global().All()
	if len(registered) == 0 {
		t.Fatal("the global registry is empty, so the core catalogue registered nothing")
	}

	rec := &recorder{}
	faulttest.AssertRegistered(rec, nil, registered[0].Code)

	if len(rec.failures) > 0 {
		t.Fatalf("a registered code failed the assertion: %v", rec.failures)
	}
}

func TestCodes(t *testing.T) {
	t.Parallel()

	body := `{"errors":[{"code":"` + knownCode + `"},{"code":"` + unknownCode + `"},` +
		`{"code":"` + knownCode + `"}],"code":"io.github.faulttest/THIRD"}`

	codes, err := faulttest.Codes([]byte(body))
	if err != nil {
		t.Fatalf("Codes() error = %v", err)
	}

	want := []string{knownCode, unknownCode, "io.github.faulttest/THIRD"}
	if len(codes) != len(want) {
		t.Fatalf("Codes() = %v, want %v", codes, want)
	}

	for i, code := range codes {
		if code.String() != want[i] {
			t.Fatalf("Codes()[%d] = %s, want %s", i, code, want[i])
		}
	}
}

func registryWithKnownCode(t *testing.T) *fault.Registry {
	t.Helper()

	domain, err := fault.NewDomain("io.github.faulttest")
	if err != nil {
		t.Fatalf("NewDomain() error = %v", err)
	}

	registry := fault.NewRegistry()
	if _, err = registry.TryRegister(domain, "KNOWN", 500, fault.Document(
		"A registered code",
		fault.NoAction,
		fault.RetryNever,
		fault.ShowDetail,
	)); err != nil {
		t.Fatalf("TryRegister() error = %v", err)
	}

	return registry
}
