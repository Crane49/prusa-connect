package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func raw(t *testing.T, value any) json.RawMessage {
	t.Helper()

	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestAPrinterIsListedAsTheCloudListsIt holds the mapping to the shape of a
// printer in the cloud's printer list, as PrusaSlicer reads it.
func TestAPrinterIsListedAsTheCloudListsIt(t *testing.T) {
	got := PrinterJSON(Printer{
		ID: "00000000-0000-0000-0000-000000000000", Name: "CORE1L", Model: "CoreOneL", Connected: true,
		State: map[string]json.RawMessage{
			"/state":                             raw(t, "FINISHED"),
			"/tool_count":                        raw(t, 1),
			"/tools/0/type":                      raw(t, "FFF_PRINT_HEAD"),
			"/tools/0/features/nozzle_diameter":  raw(t, 0.4),
			"/tools/0/features/nozzle_high_flow": raw(t, true),
			"/tools/0/features/nozzle_hardened":  raw(t, false),
			"/tools/0/material_package_instance/package/material/abbreviation": raw(t, "PLA"),
		},
	})

	want := map[string]any{
		"uuid": "00000000-0000-0000-0000-000000000000", "name": "CORE1L", "connect_state": "FINISHED",
		"legacy_printer_model": "COREONEL", "model": "COREONEL", "base_model": "COREONE", "tool_count": 1,
	}

	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s is %v, want %v", key, got[key], value)
		}
	}

	tool := got["tools"].(map[string]any)["0"].(map[string]any)
	if tool["class"] != "FFF" || tool["type"] != "PRINT_HEAD" {
		t.Errorf("the tool is %v", tool)
	}

	features := tool["features"].(map[string]any)
	if features["nozzle_diameter"] != 0.4 || features["nozzle_high_flow"] != true {
		t.Errorf("the features are %v", features)
	}

	material := tool["material_package_instance"].(map[string]any)["package"].(map[string]any)["material"]
	if material.(map[string]any)["type"] != "PLA" {
		t.Errorf("the material is %v", material)
	}
}

func TestAnAwayPrinterIsOffline(t *testing.T) {
	got := PrinterJSON(Printer{ID: "p", Model: "MK4S", State: map[string]json.RawMessage{"/state": raw(t, "IDLE")}})
	if got["connect_state"] != "OFFLINE" || got["model"] != "MK4S" || got["base_model"] != "MK4" {
		t.Fatalf("got %v", got)
	}
}

func TestAnINDXIsNamedByItsToolCount(t *testing.T) {
	got := PrinterJSON(Printer{ID: "p", Model: "CoreOne_INDX", Connected: true,
		State: map[string]json.RawMessage{"/state": raw(t, "IDLE"), "/tool_count": raw(t, 8)}})
	if got["legacy_printer_model"] != "COREONE_INDX8T" || got["tool_count"] != 8 {
		t.Fatalf("got %v", got)
	}
}

type fakeFleet struct {
	printed []string
	// viewers is the principals the host refuses a print, as it refuses a
	// person who may only view the printer.
	viewers map[string]bool
	// states is the connect_state of each printer, IDLE when it names none.
	states map[string]string
	// printErr is what Print answers, when it is set.
	printErr error
	// numbered is the name the printer prints, when it held another file at
	// the target.
	numbered string
}

// MayPrint answers as access.check answers for operate: a viewer may not.
func (f *fakeFleet) MayPrint(_ context.Context, principal, _ string) (bool, error) {
	return !f.viewers[principal], nil
}

func (f *fakeFleet) Printers(context.Context, string) ([]Printer, error) { return nil, nil }

func (f *fakeFleet) Printer(_ context.Context, _, id string) (Printer, error) {
	if id == "no-files" {
		return Printer{ID: id, Capabilities: []string{capabilityPrint}}, nil
	}

	state, known := f.states[id]
	if id != "printer-1" && !known {
		return Printer{}, ErrNoPrinter
	}

	if state == "" {
		state = "IDLE"
	}

	return Printer{
		ID: id, Connected: true, Capabilities: []string{capabilityUpload, capabilityPrint},
		State: map[string]json.RawMessage{"/state": json.RawMessage(`"` + state + `"`)},
	}, nil
}

// serviceOf is a Service over fleet, which also answers whether a person may
// print, with the upload route approved.
func serviceOf(fleet *fakeFleet, data string, now func() time.Time) *Service {
	return New(Options{Fleet: fleet, Access: fleet, Data: data, MaxSize: 1 << 20, Staged: true, Now: now})
}

func (f *fakeFleet) Print(_ context.Context, principal, id, source, target string) (string, error) {
	if f.viewers[principal] {
		return "", ErrDenied
	}

	if f.printErr != nil {
		return "", f.printErr
	}

	f.printed = append(f.printed, id+" "+source+" "+target)

	return f.numbered, nil
}

func register(s *Service, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.Register(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user:alice", "1")

	return recorder
}

func TestTheUploadIsRefusedAsTheCloudRefusesIt(t *testing.T) {
	s := serviceOf(&fakeFleet{states: map[string]string{"busy": "PRINTING"}}, t.TempDir(), nil)

	cases := []struct {
		body, code, reason string
		status             int
	}{
		{`{"filename":"a.bgcode","size":1,"printer_uuid":"printer-1","set_ready":true,"position":-1}`,
			"BAD_REQUEST", "Set ready allowed only for first position", 400},
		{`{"filename":"a.bgcode","size":1,"printer_uuid":"printer-1","wait_until":5}`,
			"BAD_REQUEST", "wait_until is only valid when adding to job queue", 400},
		{`{"filename":"a.stl","size":1,"printer_uuid":"printer-1","set_ready":true}`, "INVALID_EXTENSION", "", 400},
		{
			`{"filename":"a?.bgcode","size":1,"printer_uuid":"printer-1","set_ready":true}`,
			"INVALID_FILENAME_CHARS",
			"",
			400,
		},
		{
			`{"filename":"a.bgcode","size":99999999,"printer_uuid":"printer-1","set_ready":true}`,
			"PAYLOAD_TOO_LARGE",
			"",
			413,
		},
		{`{"filename":"a.bgcode","size":0,"printer_uuid":"printer-1","set_ready":true}`,
			"BAD_REQUEST", "the file is empty", 400},
		{`{"filename":"a.bgcode","size":1,"printer_uuid":"nope","set_ready":true}`, "NOT_FOUND", "", 404},
		{`{"filename":"a.bgcode","size":1,"printer_uuid":"no-files","set_ready":true}`, "PRINTER_NO_TRASFER", "", 400},
		{
			`{"filename":"a.bgcode","size":1,"printer_uuid":"busy","set_ready":true}`,
			"BAD_REQUEST",
			"the printer is PRINTING. This server has no print queue, so it prints only on a printer that is ready",
			400,
		},
	}

	for _, c := range cases {
		recorder := register(s, c.body)

		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)

		if recorder.Code != c.status || body["code"] != c.code || body["message"] == nil {
			t.Errorf("%s answered %d %v", c.body, recorder.Code, body)
		}

		if c.reason != "" && body["reason"] != c.reason {
			t.Errorf("%s gave the reason %v", c.body, body["reason"])
		}
	}
}

func TestAnUploadedFileIsPrintedThroughTheHost(t *testing.T) {
	data := t.TempDir()
	fleet := &fakeFleet{}
	s := serviceOf(fleet, data, func() time.Time { return time.Unix(1_800_000_000, 0) })

	recorder := register(
		s,
		`{"filename":"benchy.bgcode","size":5,"printer_uuid":"printer-1","set_ready":true,"team_id":1}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("register answered %d: %s", recorder.Code, recorder.Body)
	}

	var upload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &upload)

	id, isNumber := upload["id"].(float64)
	if !isNumber || id <= 0 || upload["state"] != "INITIATED" {
		t.Fatalf("PrusaSlicer needs an unsigned id: %v", upload)
	}

	if err := os.MkdirAll(filepath.Join(data, "staged"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(data, "staged", "x"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	put := httptest.NewRecorder()
	s.Receive(
		put,
		httptest.NewRequest(http.MethodPut, "/?upload_id="+jsonNumber(id), nil),
		"user:alice",
		"1",
		"staged/x",
	)

	if put.Code != http.StatusOK {
		t.Fatalf("the file answered %d: %s", put.Code, put.Body)
	}

	if len(fleet.printed) != 1 || !strings.HasSuffix(fleet.printed[0], "-benchy.bgcode /usb/benchy.bgcode") {
		t.Fatalf("printed %v", fleet.printed)
	}
}

// send registers benchy.bgcode for alice, stages its five bytes and sends
// them, and answers the reply to the file and the upload's id.
func send(t *testing.T, s *Service, data, principal string) (*httptest.ResponseRecorder, string) {
	t.Helper()

	recorder := register(s, `{"filename":"benchy.bgcode","size":5,"printer_uuid":"printer-1","set_ready":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("register answered %d: %s", recorder.Code, recorder.Body)
	}

	var upload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &upload)
	id := jsonNumber(upload["id"].(float64))

	return put(t, s, data, principal, id), id
}

// put stages five bytes and sends them as the file of upload id.
func put(t *testing.T, s *Service, data, principal, id string) *httptest.ResponseRecorder {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(data, "staged"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(data, "staged", "x"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	s.Receive(recorder, httptest.NewRequest(http.MethodPut, "/?upload_id="+id, nil), principal, "1", "staged/x")

	return recorder
}

// A person sends a file the printer already holds, as after slicing again.
// The printer keeps both, and the slicer is told the name that is printed.
func TestAFileSentAgainIsPrintedUnderAFreeName(t *testing.T) {
	data := t.TempDir()
	s := serviceOf(&fakeFleet{numbered: "/usb/benchy[1].bgcode"}, data, nil)

	recorder, _ := send(t, s, data, "user:alice")

	var body map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)

	if recorder.Code != http.StatusOK || body["path"] != "/usb/benchy[1].bgcode" ||
		body["name"] != "benchy[1].bgcode" {
		t.Fatalf("answered %d: %v", recorder.Code, body)
	}
}

// A failed print ends its upload and keeps no file, whatever the reason.
// PrusaSlicer registers a new upload to try again, so a person whose prints
// keep failing is never locked out by uploads that wait for nothing.
func TestAFailedPrintEndsItsUpload(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{errors.New("wirerpc: the call timed out"), http.StatusServiceUnavailable},
		{ErrNotReady, http.StatusBadRequest},
	} {
		data := t.TempDir()
		fleet := &fakeFleet{printErr: tc.err}
		s := serviceOf(fleet, data, nil)

		for range maxPendingPerPerson + 1 {
			recorder, id := send(t, s, data, "user:alice")
			if recorder.Code != tc.status {
				t.Fatalf("%v answered %d: %s", tc.err, recorder.Code, recorder.Body)
			}

			if again := put(t, s, data, "user:alice", id); again.Code != http.StatusNotFound {
				t.Fatalf("%v: the same upload again answered %d", tc.err, again.Code)
			}
		}

		if entries, _ := os.ReadDir(filepath.Join(data, "print")); len(entries) != 0 {
			t.Errorf("%v kept %d files", tc.err, len(entries))
		}
	}
}

// The file reaches only the upload that named it: another person, a wrong
// size or an unknown id is NOT_FOUND, and the upload still waits.
func TestAFileForAnotherUploadIsNotFound(t *testing.T) {
	data := t.TempDir()
	s := serviceOf(&fakeFleet{}, data, nil)

	recorder := register(s, `{"filename":"benchy.bgcode","size":5,"printer_uuid":"printer-1","set_ready":true}`)

	var upload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &upload)
	id := jsonNumber(upload["id"].(float64))

	for _, tc := range []struct{ principal, id string }{{"user:bob", id}, {"user:alice", "1"}} {
		if got := put(t, s, data, tc.principal, tc.id); got.Code != http.StatusNotFound {
			t.Errorf("%s sending to %s answered %d", tc.principal, tc.id, got.Code)
		}
	}

	if got := put(t, s, data, "user:alice", id); got.Code != http.StatusOK {
		t.Errorf("the right file then answered %d: %s", got.Code, got.Body)
	}
}

// One person's uploads that never send their file hold no more than their
// share, so another person still registers.
func TestOnePersonCannotHoldEveryUpload(t *testing.T) {
	s := serviceOf(&fakeFleet{}, t.TempDir(), nil)

	body := `{"filename":"a.bgcode","size":1,"printer_uuid":"printer-1","set_ready":true}`
	for range maxPendingPerPerson {
		if recorder := register(s, body); recorder.Code != http.StatusOK {
			t.Fatalf("answered %d: %s", recorder.Code, recorder.Body)
		}
	}

	if recorder := register(s, body); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("one upload past the share answered %d", recorder.Code)
	}

	bob := httptest.NewRecorder()
	s.Register(bob, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user:bob", "1")

	if bob.Code != http.StatusOK {
		t.Fatalf("another person answered %d: %s", bob.Code, bob.Body)
	}
}

func jsonNumber(f float64) string {
	out, _ := json.Marshal(int64(f))

	return string(out)
}

// TestAToolIsListedWithTheCloudsKeysAndFeeder holds the tools to the shape
// PrusaSlicer reads. It uses the state paths the demo printers of this server
// report: a feeder with zero slots on every print head, and an MMU on the
// MK4S. PrusaSlicer refuses a feeder without a model, so a print head without
// an MMU carries no feeder, and the MMU is an MMU3 with its slot count.
func TestAToolIsListedWithTheCloudsKeysAndFeeder(t *testing.T) {
	plain := PrinterJSON(Printer{ID: "xl", Model: "XL", Connected: true, State: map[string]json.RawMessage{
		"/state": raw(t, "IDLE"), "/tool_count": raw(t, 2),
		"/tools/0/type": raw(t, "FFF_PRINT_HEAD"), "/tools/0/feeder/slot_count": raw(t, 0),
		"/tools/0/nozzle_temp": raw(t, 25), "/tools/0/features/nozzle_diameter": raw(t, 0.4),
		"/tools/1/type": raw(t, "FFF_PRINT_HEAD"), "/tools/1/feeder/slot_count": raw(t, 0),
	}})

	for key, tool := range plain["tools"].(map[string]any) {
		fields := tool.(map[string]any)
		if _, has := fields["feeder"]; has {
			t.Errorf("tool %s of a printer with no MMU carries a feeder: %v", key, fields)
		}

		if _, has := fields["nozzle_temp"]; has {
			t.Errorf("tool %s carries a key the cloud leaves out: %v", key, fields)
		}
	}

	if plain["legacy_printer_model"] != "XL2IS" {
		t.Errorf("the XL with two heads is %v", plain["legacy_printer_model"])
	}

	mmu := PrinterJSON(Printer{ID: "mk4s", Model: "MK4S", Connected: true, State: map[string]json.RawMessage{
		"/state": raw(t, "IDLE"), "/tool_count": raw(t, 1),
		"/tools/0/type": raw(t, "FFF_PRINT_HEAD"), "/tools/0/feeder/type": raw(t, "MMU"),
		"/tools/0/feeder/slot_count": raw(t, 5), "/tools/0/feeder/mmu_progress_code": raw(t, 0),
		"/tools/0.0/type": raw(t, "FFF_FEEDER_SLOT"),
		"/tools/0.0/material_package_instance/package/material/abbreviation": raw(t, "PLA"),
		"/tools/0.4/type": raw(t, "FFF_FEEDER_SLOT"),
		"/tools/0.4/material_package_instance/package/material/abbreviation": raw(t, ""),
	}})

	tools := mmu["tools"].(map[string]any)

	feeder, _ := tools["0"].(map[string]any)["feeder"].(map[string]any)
	if feeder["type"] != "MMU" || feeder["model"] != "MMU3" || feeder["base_model"] != "MMU3" ||
		feeder["num_slots"] != 5 {
		t.Errorf("the MMU is %v", feeder)
	}

	if mmu["legacy_printer_model"] != "MK4SMMU3" || mmu["model"] != "MK4S" || mmu["base_model"] != "MK4" {
		t.Errorf("the MK4S with an MMU is %v", mmu)
	}

	if slot := tools["0.4"].(map[string]any); slot["material_package_instance"] != nil {
		t.Errorf("an empty slot carries %v, and the cloud answers null", slot["material_package_instance"])
	}
}

// A person who may only view the printer signs in to the slicer and sends a
// print. The module asks the host before the slicer sends the file, and the
// slicer reads the cloud's 403 body at the upload request. Nothing is kept.
func TestAViewersPrintIsRefusedAsTheCloudRefusesIt(t *testing.T) {
	fleet := &fakeFleet{viewers: map[string]bool{"user:alice": true}}
	s := serviceOf(fleet, t.TempDir(), func() time.Time { return time.Unix(1_800_000_000, 0) })

	recorder := register(s,
		`{"filename":"benchy.bgcode","size":5,"printer_uuid":"printer-1","set_ready":true,"team_id":1}`)

	var body map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)

	if recorder.Code != http.StatusForbidden || body["code"] != "FORBIDDEN" {
		t.Fatalf("a viewer's upload answered %d: %s", recorder.Code, recorder.Body)
	}

	if len(s.pending) != 0 || len(fleet.printed) != 0 {
		t.Fatalf("a refused upload left %d waiting and printed %v", len(s.pending), fleet.printed)
	}
}

// An install whose operator declined the upload route takes no print: the
// file would never reach the module as a file. The slicer is told at the
// upload request, with the reason, and not after it sent the file.
func TestAnUploadWithoutTheStagedRouteIsRefusedAtOnce(t *testing.T) {
	s := New(Options{Fleet: &fakeFleet{}, Data: t.TempDir(), MaxSize: 1 << 20})

	recorder := register(s, `{"filename":"a.bgcode","size":1,"printer_uuid":"printer-1","set_ready":true}`)

	var body map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)

	reason, _ := body["reason"].(string)
	if recorder.Code != http.StatusBadRequest || body["code"] != "BAD_REQUEST" ||
		!strings.Contains(reason, "did not approve uploads") {
		t.Fatalf("answered %d: %s", recorder.Code, recorder.Body)
	}
}

// The refusals of the host at the print reach the slicer with a reason, and
// a host that did not answer is 503 rather than a printer without file
// transfer.
func TestAPrintTheHostRefusesSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{ErrNotReady, http.StatusBadRequest, "BAD_REQUEST"},
		{ErrPrintRefused, http.StatusBadRequest, "BAD_REQUEST"},
		{errors.New("wirerpc: the call timed out"), http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
	} {
		refusal := fleetRefusal(tc.err)
		if refusal.Status != tc.status || refusal.Code != tc.code {
			t.Errorf("%v became %d %s", tc.err, refusal.Status, refusal.Code)
		}
	}
}

// A print the host's check cannot be asked about is left to the print: an
// install whose operator did not approve the check still prints.
func TestAPrintIsCheckedByThePrintWhenTheModuleMayNotAsk(t *testing.T) {
	s := New(Options{
		Fleet: &fakeFleet{}, Access: cannotAsk{}, Data: t.TempDir(), MaxSize: 1 << 20, Staged: true,
	})

	if recorder := register(
		s,
		`{"filename":"a.bgcode","size":1,"printer_uuid":"printer-1","set_ready":true}`,
	); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", recorder.Code, recorder.Body)
	}
}

type cannotAsk struct{}

func (cannotAsk) MayPrint(context.Context, string, string) (bool, error) { return false, ErrCannotAsk }

// A file whose send failed stays a day, and a sweep removes it after that.
func TestAPrintedFileIsRemovedAfterADay(t *testing.T) {
	data := t.TempDir()
	clock := time.Unix(1_800_000_000, 0)
	s := serviceOf(&fakeFleet{}, data, func() time.Time { return clock })

	old := filepath.Join(data, "print", "1")
	recent := filepath.Join(data, "print", "2")

	for _, dir := range []string{old, recent} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, "a.bgcode"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Chtimes(old, clock, clock.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := os.Chtimes(recent, clock, clock.Add(-23*time.Hour)); err != nil {
		t.Fatal(err)
	}

	s.Sweep()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("a print older than a day is still there: %v", err)
	}

	if _, err := os.Stat(recent); err != nil {
		t.Errorf("a print of today was removed: %v", err)
	}
}
