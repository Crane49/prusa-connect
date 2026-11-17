package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The inputs and the package, from the package directory.
const (
	committedDocument   = "../../internal/transport/http/httpapi/openapi.json"
	committedErrorCodes = "../../docs/errors/error-codes.json"
	committedEventTypes = "../../docs/vocabularies/event-types.json"
	committedAuthz      = "../../docs/vocabularies/authorization.json"
	committedVersion    = "../../VERSION"
	committedPackage    = "../../packages/typescript"
)

func committed(packageDir string, check bool) options {
	return options{
		documentPath:   committedDocument,
		errorCodesPath: committedErrorCodes,
		eventTypesPath: committedEventTypes,
		authzPath:      committedAuthz,
		versionPath:    committedVersion,
		packageDir:     packageDir,
		check:          check,
	}
}

// TestTheCommittedPackageMatchesTheContracts is the gate that keeps the
// generated half of the package from drifting behind the document and the
// registries. It is a test rather than only a make target so that it runs
// under `go test ./...` whether or not anybody remembers the target
// (ADR-20260909-c7x rule 5).
func TestTheCommittedPackageMatchesTheContracts(t *testing.T) {
	t.Parallel()

	if err := run(committed(committedPackage, true)); err != nil {
		t.Errorf("%v", err)
	}
}

func TestEveryOperationInTheDocumentIsEmitted(t *testing.T) {
	t.Parallel()

	in, err := readInputs(committed(committedPackage, false))
	if err != nil {
		t.Fatalf("read the inputs: %v", err)
	}

	if len(in.operations) < 80 {
		t.Fatalf("the document declares %d operations, so this gate is reading too little", len(in.operations))
	}

	rendered := string(renderOperations(in.operations))

	for _, op := range in.operations {
		if !strings.Contains(rendered, "  "+tsString(op.ID)+": { id: ") {
			t.Errorf("%s is not in the operation table", op.ID)
		}

		if !strings.Contains(rendered, "  "+op.Name+"(\n") {
			t.Errorf("%s has no %s method on the Operations interface", op.ID, op.Name)
		}

		if !strings.Contains(rendered, "    "+op.Name+": (request") {
			t.Errorf("%s is not bound by bindOperations", op.ID)
		}
	}
}

func TestEveryErrorCodeAndEventTypeIsEmitted(t *testing.T) {
	t.Parallel()

	in, err := readInputs(committed(committedPackage, false))
	if err != nil {
		t.Fatalf("read the inputs: %v", err)
	}

	codes := string(renderErrors(in.codes))

	for _, code := range in.codes {
		if !strings.Contains(codes, "  | "+tsString(code.Code)) {
			t.Errorf("%s is not in the ErrorCode union", code.Code)
		}

		if !strings.Contains(codes, "  "+tsString(code.Code)+": { status: ") {
			t.Errorf("%s is not in the errorCodes registry", code.Code)
		}
	}

	events := string(renderEvents(in.events))

	for _, entry := range in.events.Entries {
		if !strings.Contains(events, "  | "+tsString(entry.Type)) {
			t.Errorf("%s is not in the EventType union", entry.Type)
		}

		if !strings.Contains(events, "  "+tsString(entry.Type)+": "+tsString(entry.Class)+",\n") {
			t.Errorf("%s carries no class in eventClasses", entry.Type)
		}
	}

	if !strings.Contains(events, "no payload schema") {
		t.Error("events.ts does not say that payloads are unknown")
	}

	vocabulary := string(renderVocabulary(in.authz))

	for _, action := range in.authz.Actions {
		if !strings.Contains(vocabulary, tsString(action.Action)) {
			t.Errorf("%s is not in vocabulary.ts", action.Action)
		}
	}
}

// TestADriftedFileIsRefused is the committed-output gate on a copy of the
// package whose operations file was edited by hand.
func TestADriftedFileIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generated := filepath.Join(dir, generatedDir)

	if err := os.MkdirAll(generated, 0o750); err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(filepath.Join(committedPackage, manifestName))
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(filepath.Join(dir, manifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	if err = run(committed(dir, false)); err != nil {
		t.Fatalf("writing a fresh package refused: %v", err)
	}

	if err = run(committed(dir, true)); err != nil {
		t.Fatalf("a freshly written package reported drift: %v", err)
	}

	edited := filepath.Join(generated, "operations.ts")
	if err = os.WriteFile(edited, []byte("// edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = run(committed(dir, true))
	if !errors.Is(err, errStale) {
		t.Errorf("an edited file reported %v, want errStale", err)
	}

	if err != nil && !strings.Contains(err.Error(), "operations.ts") {
		t.Errorf("the refusal does not name the file: %v", err)
	}
}

func TestTheVersionIsStampedFromVERSION(t *testing.T) {
	t.Parallel()

	manifest := "{\n  \"name\": \"x\",\n  \"version\": \"0.0.0\",\n  \"type\": \"module\"\n}\n"

	stamped, err := stampVersion([]byte(manifest), "1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	want := "{\n  \"name\": \"x\",\n  \"version\": \"1.2.3\",\n  \"type\": \"module\"\n}\n"
	if string(stamped) != want {
		t.Errorf("stamped %q, want %q", stamped, want)
	}

	if _, err = stampVersion([]byte("{}"), "1.2.3"); !errors.Is(err, errNoVersionField) {
		t.Errorf("a manifest with no version reported %v, want errNoVersionField", err)
	}
}

func TestOperationShapes(t *testing.T) {
	t.Parallel()

	in, err := readInputs(committed(committedPackage, false))
	if err != nil {
		t.Fatalf("read the inputs: %v", err)
	}

	byID := map[string]operation{}
	for _, op := range in.operations {
		byID[op.ID] = op
	}

	cases := []struct {
		id        string
		name      string
		body      string
		response  responseKind
		anonymous bool
		optional  bool
	}{
		{id: "list-devices", name: "listDevices", response: responseJSON, optional: true},
		{id: "get-device", name: "getDevice", response: responseJSON},
		{id: "send-device-command", name: "sendDeviceCommand", body: mediaJSON, response: responseJSON},
		{id: "logout", name: "logout", response: responseNone, optional: true},
		{id: "get-health", name: "getHealth", response: responseJSON, anonymous: true, optional: true},
		{id: "stream-events", name: "streamEvents", response: responseRaw, optional: true},
		{id: "get-metrics", name: "getMetrics", response: responseText, optional: true},
		{id: "watch-camera", name: "watchCamera", body: "application/sdp", response: responseText},
		{
			id: "upload-storage-object", name: "uploadStorageObject",
			body: "application/octet-stream", response: responseJSON,
		},
		{id: "delete-device-file", name: "deleteDeviceFile", response: responseNone},
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()

			op, ok := byID[c.id]
			if !ok {
				t.Fatalf("%s is not in the document", c.id)
			}

			if op.Name != c.name || op.Body != c.body || op.Response != c.response ||
				op.Anonymous != c.anonymous || op.RequestOptional() != c.optional {
				t.Errorf("%s read as name=%s body=%q response=%s anonymous=%t optional=%t; want %+v",
					c.id, op.Name, op.Body, op.Response, op.Anonymous, op.RequestOptional(), c)
			}
		})
	}
}
