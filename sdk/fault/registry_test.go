package fault_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

func bucketDoc() fault.Doc {
	return fault.Document(
		"The bucket did not answer",
		"Check the endpoint and retry.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"bucket",
	)
}

func TestTryRegisterAndLookup(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()

	definition, err := registry.TryRegister(
		acmeDomain(t),
		"BUCKET_UNREACHABLE",
		http.StatusServiceUnavailable,
		bucketDoc(),
	)
	if err != nil {
		t.Fatalf("TryRegister: %v", err)
	}

	if definition.Code().String() != "io.github.acme.s3/BUCKET_UNREACHABLE" {
		t.Fatalf("Code() = %q", definition.Code())
	}

	entry, ok := registry.Lookup(definition.Code())
	if !ok {
		t.Fatalf("Lookup(%q) found nothing", definition.Code())
	}

	if entry.Summary != "The bucket did not answer" || entry.Action != "Check the endpoint and retry." {
		t.Fatalf("entry documentation = %+v", entry)
	}

	if entry.Retry != fault.RetryAllowed || entry.Redaction != fault.ShowDetail {
		t.Fatalf("entry retry/redaction = %v/%v", entry.Retry, entry.Redaction)
	}

	if entry.Status != http.StatusServiceUnavailable || !slices.Equal(entry.Params, []string{"bucket"}) {
		t.Fatalf("entry status/params = %d/%v", entry.Status, entry.Params)
	}

	entry.Params[0] = "mutated"

	if again, _ := registry.Lookup(definition.Code()); again.Params[0] != "bucket" {
		t.Fatal("Lookup handed out the registry's own slice")
	}
}

func TestRegisteredAndLookupMiss(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()
	definition := registry.Register(acmeDomain(t), "BUCKET_UNREACHABLE", http.StatusServiceUnavailable, bucketDoc())

	if !registry.Registered(definition.Code()) {
		t.Fatalf("Registered(%q) = false for a registered code", definition.Code())
	}

	unknown, err := fault.NewCode(acmeDomain(t), "NEVER_REGISTERED")
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	if registry.Registered(unknown) {
		t.Fatalf("Registered(%q) = true for an unregistered code", unknown)
	}

	if _, ok := registry.Lookup(unknown); ok {
		t.Fatalf("Lookup(%q) found an unregistered code", unknown)
	}

	if registry.Registered(fault.Code{}) {
		t.Fatal("the zero code is registered")
	}
}

func TestZeroRegistryIsUsable(t *testing.T) {
	t.Parallel()

	var registry fault.Registry

	if len(registry.All()) != 0 || registry.Registered(fault.Code{}) {
		t.Fatal("the zero registry is not empty")
	}

	definition, err := registry.TryRegister(
		acmeDomain(t),
		"BUCKET_UNREACHABLE",
		http.StatusServiceUnavailable,
		bucketDoc(),
	)
	if err != nil {
		t.Fatalf("TryRegister: %v", err)
	}

	if !registry.Registered(definition.Code()) {
		t.Fatal("the zero registry did not keep the registration")
	}
}

func TestTryRegisterRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		reason  string
		status  int
		doc     fault.Doc
		wantErr error
	}{
		{
			name:   "missing summary",
			reason: "BUCKET_UNREACHABLE",
			status: http.StatusServiceUnavailable,
			doc: fault.Document(
				"",
				"Check the endpoint and retry.",
				fault.RetryAllowed,
				fault.ShowDetail,
				"bucket",
			),
			wantErr: fault.ErrUndocumented,
		},
		{
			name:   "missing action",
			reason: "BUCKET_UNREACHABLE",
			status: http.StatusServiceUnavailable,
			doc: fault.Document(
				"The bucket did not answer",
				"",
				fault.RetryAllowed,
				fault.ShowDetail,
			),
			wantErr: fault.ErrUndocumented,
		},
		{
			name:    "no documentation at all",
			reason:  "BUCKET_UNREACHABLE",
			status:  http.StatusServiceUnavailable,
			doc:     fault.Doc{},
			wantErr: fault.ErrUndocumented,
		},
		{
			name:    "retry hint out of range",
			reason:  "BUCKET_UNREACHABLE",
			status:  http.StatusServiceUnavailable,
			doc:     fault.Document("The bucket did not answer", fault.NoAction, 9, fault.ShowDetail),
			wantErr: fault.ErrUndocumented,
		},
		{
			name:    "status outside the error range",
			reason:  "BUCKET_UNREACHABLE",
			status:  http.StatusOK,
			doc:     bucketDoc(),
			wantErr: fault.ErrInvalidStatus,
		},
		{
			name:   "param name is not snake_case",
			reason: "BUCKET_UNREACHABLE",
			status: http.StatusServiceUnavailable,
			doc: fault.Document(
				"The bucket did not answer",
				fault.NoAction,
				fault.RetryNever,
				fault.ShowDetail,
				"Bucket",
			),
			wantErr: fault.ErrInvalidParamName,
		},
		{
			name:    "malformed reason",
			reason:  "bucket_unreachable",
			status:  http.StatusServiceUnavailable,
			doc:     bucketDoc(),
			wantErr: fault.ErrInvalidReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := fault.NewRegistry()

			definition, err := registry.TryRegister(acmeDomain(t), tt.reason, tt.status, tt.doc)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TryRegister error = %v, want %v", err, tt.wantErr)
			}

			if definition != nil {
				t.Fatalf("TryRegister returned %q alongside an error", definition.Code())
			}

			if len(registry.All()) != 0 {
				t.Fatal("a rejected registration reached the registry")
			}
		})
	}
}

func TestTryRegisterRejectsAnUnmintedDomain(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()

	_, err := registry.TryRegister(fault.Domain{}, "BUCKET_UNREACHABLE", http.StatusServiceUnavailable, bucketDoc())
	if !errors.Is(err, fault.ErrInvalidDomain) {
		t.Fatalf("TryRegister error = %v, want %v", err, fault.ErrInvalidDomain)
	}
}

func TestTryRegisterReportsADuplicate(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()
	domain := acmeDomain(t)

	registry.Register(domain, "BUCKET_UNREACHABLE", http.StatusServiceUnavailable, bucketDoc())

	_, err := registry.TryRegister(domain, "BUCKET_UNREACHABLE", http.StatusBadGateway, bucketDoc())
	if !errors.Is(err, fault.ErrDuplicateCode) {
		t.Fatalf("TryRegister error = %v, want %v", err, fault.ErrDuplicateCode)
	}

	if got := len(registry.All()); got != 1 {
		t.Fatalf("registry holds %d entries after a refused duplicate", got)
	}
}

// Register is the init-time path: a duplicate there is a programming error that
// must not reach a running server.
func TestRegisterPanicsOnADuplicate(t *testing.T) {
	t.Parallel()

	registry := fault.NewRegistry()
	domain := acmeDomain(t)

	registry.Register(domain, "BUCKET_UNREACHABLE", http.StatusServiceUnavailable, bucketDoc())

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Error("Register did not panic on a duplicate")

			return
		}

		message, ok := recovered.(string)
		if !ok || !strings.Contains(message, "already registered") {
			t.Errorf("panic value = %v", recovered)
		}

		if got := len(registry.All()); got != 1 {
			t.Errorf("registry holds %d entries after a refused duplicate", got)
		}
	}()

	registry.Register(domain, "BUCKET_UNREACHABLE", http.StatusBadGateway, bucketDoc())
}

func TestEntryJSONRejectsAMalformedCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "no separator", input: `{"code":"core.printer"}`},
		{name: "three parts", input: `{"code":"a/b/c"}`},
		{name: "blank halves", input: `{"code":"  /  "}`},
		{name: "empty", input: `{"code":""}`},
		{name: "empty reason", input: `{"code":"core.printer/"}`},
		{name: "empty domain", input: `{"code":"/COMMAND_REJECTED"}`},
		{name: "lowercase reason", input: `{"code":"core.printer/command_rejected"}`},
		{name: "not a string", input: `{"code":42}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var entry fault.Entry
			if err := json.Unmarshal([]byte(tt.input), &entry); err == nil {
				t.Fatalf("Unmarshal(%s) accepted it as %q", tt.input, entry.Code)
			}

			if !entry.Code.IsZero() {
				t.Fatalf("Unmarshal(%s) left %q behind", tt.input, entry.Code)
			}
		})
	}
}

func TestAllIsSortedAndDeterministic(t *testing.T) {
	t.Parallel()

	reasons := []string{"ZONE_LOST", "BUCKET_UNREACHABLE", "QUOTA_EXCEEDED", "ACCESS_DENIED"}

	registry := fault.NewRegistry()
	for _, reason := range reasons {
		registry.Register(acmeDomain(t), reason, http.StatusServiceUnavailable, bucketDoc())
	}

	first := registry.All()

	sorted := slices.IsSortedFunc(first, func(a, b fault.Entry) int {
		return strings.Compare(a.Code.String(), b.Code.String())
	})
	if !sorted {
		t.Fatalf("All() is not sorted by code: %v", codesOf(first))
	}

	// Map iteration order is randomised per range, so repeating the call is what
	// shows the order comes from the sort.
	for range 50 {
		if !slices.Equal(codesOf(registry.All()), codesOf(first)) {
			t.Fatalf("All() order changed between calls: %v", codesOf(registry.All()))
		}
	}

	reversed := fault.NewRegistry()
	for _, reason := range slices.Backward(reasons) {
		reversed.Register(acmeDomain(t), reason, http.StatusServiceUnavailable, bucketDoc())
	}

	if !slices.Equal(codesOf(reversed.All()), codesOf(first)) {
		t.Fatalf("registration order changed All(): %v", codesOf(reversed.All()))
	}
}

func TestSnapshotIsByteStable(t *testing.T) {
	t.Parallel()

	want := `[
  {
    "code": "io.github.acme.s3/ACCESS_DENIED",
    "status": 403,
    "summary": "The credentials do not allow this bucket",
    "action": "` + fault.NoAction + `",
    "retryable": false,
    "redacted": true,
    "params": []
  },
  {
    "code": "io.github.acme.s3/BUCKET_UNREACHABLE",
    "status": 503,
    "summary": "The bucket did not answer",
    "action": "Check the endpoint and retry.",
    "retryable": true,
    "redacted": false,
    "params": [
      "bucket"
    ]
  }
]
`

	got, err := snapshotFixture(t, false).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if string(got) != want {
		t.Fatalf("Snapshot() =\n%s\nwant\n%s", got, want)
	}

	for range 50 {
		again, againErr := snapshotFixture(t, false).Snapshot()
		if againErr != nil {
			t.Fatalf("Snapshot: %v", againErr)
		}

		if !bytes.Equal(again, got) {
			t.Fatalf("Snapshot() differs between calls:\n%s", again)
		}
	}

	other, err := snapshotFixture(t, true).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if !bytes.Equal(other, got) {
		t.Fatalf("registration order changed the snapshot:\n%s", other)
	}
}

func TestSnapshotRoundTrips(t *testing.T) {
	t.Parallel()

	registry := snapshotFixture(t, false)

	encoded, err := registry.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var decoded []fault.Entry
	if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
		t.Fatalf("Unmarshal: %v", decodeErr)
	}

	if !reflect.DeepEqual(decoded, registry.All()) {
		t.Fatalf("round trip =\n%+v\nwant\n%+v", decoded, registry.All())
	}
}

func TestConcurrentRegisterAndAll(t *testing.T) {
	t.Parallel()

	const (
		distinct  = 16
		duplicate = 8
	)

	registry := fault.NewRegistry()
	domain := acmeDomain(t)

	var (
		wg         sync.WaitGroup
		duplicates atomicCounter
	)

	for i := range distinct {
		wg.Go(func() {
			if _, err := registry.TryRegister(
				domain,
				"REASON_"+strconv.Itoa(i),
				http.StatusServiceUnavailable,
				bucketDoc(),
			); err != nil {
				t.Errorf("TryRegister: %v", err)
			}
		})

		wg.Go(func() { registry.All() })
	}

	for range duplicate {
		wg.Go(func() {
			_, err := registry.TryRegister(domain, "CONTENDED", http.StatusServiceUnavailable, bucketDoc())
			if err == nil {
				return
			}

			if !errors.Is(err, fault.ErrDuplicateCode) {
				t.Errorf("TryRegister: %v", err)
			}

			duplicates.add()
		})
	}

	wg.Wait()

	if got := len(registry.All()); got != distinct+1 {
		t.Fatalf("registry holds %d entries, want %d", got, distinct+1)
	}

	if got := duplicates.value(); got != duplicate-1 {
		t.Fatalf("%d contended registrations were refused, want %d", got, duplicate-1)
	}
}

func snapshotFixture(t *testing.T, reverse bool) *fault.Registry {
	t.Helper()

	registry := fault.NewRegistry()

	register := []func(){
		func() {
			registry.Register(acmeDomain(t), "BUCKET_UNREACHABLE", http.StatusServiceUnavailable, bucketDoc())
		},
		func() {
			registry.Register(acmeDomain(t), "ACCESS_DENIED", http.StatusForbidden, fault.Document(
				"The credentials do not allow this bucket",
				fault.NoAction,
				fault.RetryNever,
				fault.RedactDetail,
			))
		},
	}

	if reverse {
		slices.Reverse(register)
	}

	for _, step := range register {
		step()
	}

	return registry
}

func codesOf(entries []fault.Entry) []string {
	codes := make([]string, 0, len(entries))
	for _, entry := range entries {
		codes = append(codes, entry.Code.String())
	}

	return codes
}

type atomicCounter struct {
	mu    sync.Mutex
	count int
}

func (c *atomicCounter) add() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.count++
}

func (c *atomicCounter) value() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.count
}
