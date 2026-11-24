package link_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
	"github.com/prusa3d/prusaslicer2-connector/internal/routes"
)

const (
	alice   = "user:alice"
	printer = "mk4-01"
	// formPath is where PrusaSlicer 2.5 and older post the form.
	formPath = "/printers/" + printer + "/api/files/local"
)

// fakeFleet holds one printer and records each print and each file that
// only landed.
type fakeFleet struct {
	mu      sync.Mutex
	printer link.Printer
	missing bool
	printed []printed
	landed  []printed
	failed  error
}

type printed struct {
	principal, id, storage, source, target string
}

func (f *fakeFleet) Printer(_ context.Context, _, id string) (link.Printer, error) {
	if f.missing || id != f.printer.ID {
		return link.Printer{}, link.ErrNoPrinter
	}

	return f.printer, nil
}

func (f *fakeFleet) Print(_ context.Context, principal, id, storage, source, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failed != nil {
		return f.failed
	}

	f.printed = append(
		f.printed,
		printed{principal: principal, id: id, storage: storage, source: source, target: target},
	)

	return nil
}

func (f *fakeFleet) Send(_ context.Context, principal, id, storage, source, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failed != nil {
		return f.failed
	}

	f.landed = append(f.landed, printed{principal: principal, id: id, storage: storage, source: source, target: target})

	return nil
}

type fakeAccess struct {
	allowed bool
	err     error
}

func (a fakeAccess) MayPrint(context.Context, string, string) (bool, error) { return a.allowed, a.err }

// fakeFiles records each file written into a storage, by principal and
// storage path.
type fakeFiles struct {
	mu      sync.Mutex
	written map[string]string
	err     error
}

func (f *fakeFiles) Write(_ context.Context, principal, storage, file string, size int64, body io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	if int64(len(data)) != size {
		return errors.New("short body")
	}

	if f.written == nil {
		f.written = map[string]string{}
	}

	f.written[principal+" "+storage+"/"+file] = string(data)

	return nil
}

func (f *fakeFiles) Exists(_ context.Context, _, storage, file string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for key := range f.written {
		if strings.HasSuffix(key, " "+storage+"/"+file) {
			return true, nil
		}
	}

	return false, nil
}

type rig struct {
	t       *testing.T
	data    string
	fleet   *fakeFleet
	files   *fakeFiles
	handler http.Handler
	// staged is the body file the host would have written, per request.
	staged string
}

func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		t:     t,
		data:  t.TempDir(),
		fleet: &fakeFleet{printer: link.Printer{ID: printer, Name: "Workshop MK4", Model: "MK4", State: "IDLE"}},
		files: &fakeFiles{},
	}

	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{allowed: true}, Files: r.files,
		Data: r.data, Version: "0.1.0",
	})

	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })

	return r
}

// stage writes body where the host writes a staged body, and names it for
// the next request.
func (r *rig) stage(body []byte) {
	r.t.Helper()

	if err := os.MkdirAll(filepath.Join(r.data, "staged"), 0o750); err != nil {
		r.t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(r.data, "staged", "BODY"), body, 0o600); err != nil {
		r.t.Fatal(err)
	}

	r.staged = "staged/BODY"
}

func (r *rig) do(method, target string, body io.Reader, header http.Header) *httptest.ResponseRecorder {
	r.t.Helper()

	req := httptest.NewRequest(method, target, body)
	maps.Copy(req.Header, header)

	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)

	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	return out
}

func TestTheVersionIsWhatPrusaSlicerTakesForPrusaLink(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodGet, "/printers/"+printer+"/api/version", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}

	body := decode(t, rec)
	if body["api"] != "2.0.0" || !strings.HasPrefix(body["text"].(string), "PrusaLink") {
		t.Fatalf("version %v: PrusaSlicer needs api and a text that starts with PrusaLink", body)
	}

	if caps, _ := body["capabilities"].(map[string]any); caps["upload-by-put"] != true {
		t.Fatalf("capabilities %v, want upload-by-put", body["capabilities"])
	}

	if body["hostname"] != "Workshop MK4" {
		t.Fatalf("hostname %v", body["hostname"])
	}
}

func TestAnAddressOfNoPrinterFailsTheConnectionTest(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodGet, "/printers/nope/api/version", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}

	if msg, _ := decode(t, rec)["message"].(string); !strings.Contains(msg, "address") {
		t.Fatalf("message %q should tell the person to check the address", msg)
	}
}

func TestTheStorageListStartsWithStorageList(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodGet, "/printers/"+printer+"/api/v1/storage", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	// PrusaSlicer reads the first key of the object and wants storage_list.
	if !strings.HasPrefix(rec.Body.String(), `{"storage_list":[`) {
		t.Fatalf("body %s", rec.Body)
	}

	list := decode(t, rec)["storage_list"].([]any)
	first := list[0].(map[string]any)

	if first["path"] != "/local" || first["read_only"] != false || first["available"] != true {
		t.Fatalf("storage %v", first)
	}
}

func TestAPutWithPrintAfterUploadStartsThePrint(t *testing.T) {
	r := newRig(t)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/benchy.bgcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}

	if len(r.fleet.printed) != 1 {
		t.Fatalf("printed %v", r.fleet.printed)
	}

	got := r.fleet.printed[0]
	if got.principal != alice || got.id != printer || got.target != "/usb/benchy.bgcode" {
		t.Fatalf("print %+v", got)
	}

	kept, err := os.ReadFile(filepath.Join(r.data, "print", filepath.FromSlash(got.source)))
	if err != nil || string(kept) != "G28\n" {
		t.Fatalf("the print folder holds %q, %v", kept, err)
	}

	if len(r.fleet.landed) != 0 {
		t.Fatalf("a print also landed the file without starting it: %v", r.fleet.landed)
	}
}

// Upload lands the file on the printer's USB drive and starts nothing.
func TestAPutWithoutPrintAfterUploadLandsTheFileAndPrintsNothing(t *testing.T) {
	r := newRig(t)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/benchy.gcode", nil, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}

	if len(r.fleet.printed) != 0 || len(r.fleet.landed) != 1 {
		t.Fatalf("printed %v, landed %v", r.fleet.printed, r.fleet.landed)
	}

	got := r.fleet.landed[0]
	if got.principal != alice || got.id != printer || got.target != "/usb/benchy.gcode" {
		t.Fatalf("landed %+v", got)
	}

	kept, err := os.ReadFile(filepath.Join(r.data, "print", filepath.FromSlash(got.source)))
	if err != nil || string(kept) != "G28\n" {
		t.Fatalf("the print folder holds %q, %v", kept, err)
	}
}

// Upload goes to a busy printer too, as with PrusaLink: only a print needs
// the printer ready.
func TestAnUploadGoesToABusyPrinter(t *testing.T) {
	r := newRig(t)
	r.fleet.printer.State = "PRINTING"
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil, nil)
	if rec.Code != http.StatusCreated || len(r.fleet.landed) != 1 {
		t.Fatalf("status %d, landed %v, body %s", rec.Code, r.fleet.landed, rec.Body)
	}
}

// An Upload for a person who may not operate the printer is refused before
// the printer is asked.
func TestAnUploadOfAPersonWhoMayNotOperateIsRefused(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{allowed: false}, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil, nil)
	if rec.Code != http.StatusForbidden || len(r.fleet.landed) != 0 {
		t.Fatalf("status %d, landed %v", rec.Code, r.fleet.landed)
	}
}

func TestAFalsePrintAfterUploadDoesNotPrint(t *testing.T) {
	r := newRig(t)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?0"}})
	if rec.Code != http.StatusCreated || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

func TestABodyOnTheRequestIsTakenWhenTheRouteIsNotStaged(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", strings.NewReader("G1\n"),
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusCreated || len(r.fleet.printed) != 1 {
		t.Fatalf("status %d, printed %v, body %s", rec.Code, r.fleet.printed, rec.Body)
	}

	if entries, _ := os.ReadDir(filepath.Join(r.data, "incoming")); len(entries) != 0 {
		t.Fatalf("incoming/ keeps %d files", len(entries))
	}
}

// A print the host refused before the printer could read the file leaves
// nothing in the print folder. Any other failure leaves the file for the
// sweep, as the printer may be reading it.
func TestOnlyARefusedPrintLeavesThePrintFolderAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kept int
	}{
		{"taken name", link.ErrNameTaken, 0},
		{"not ready", link.ErrNotReady, 0},
		{"slicer gone", context.Canceled, 1},
		{"wire failed", errors.New("wire: the session ended"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.fleet.failed = tc.err
			r.stage([]byte("G28\n"))

			rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
				http.Header{"Print-After-Upload": {"?1"}})
			if rec.Code < http.StatusBadRequest {
				t.Fatalf("status %d", rec.Code)
			}

			if entries, _ := os.ReadDir(filepath.Join(r.data, "print")); len(entries) != tc.kept {
				t.Fatalf("the print folder keeps %d entries, want %d", len(entries), tc.kept)
			}
		})
	}
}

func TestAPrinterThatIsNotReadyRefusesWithConflict(t *testing.T) {
	r := newRig(t)
	r.fleet.printer.State = "PRINTING"
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusConflict || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

func TestAPersonWhoMayNotOperateIsRefused(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{allowed: false}, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusForbidden || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

// A host that does not let the module ask leaves the print itself as the
// check, so the print goes ahead.
func TestAPrintGoesAheadWhenTheModuleMayNotAsk(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{err: link.ErrCannotAsk}, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusCreated || len(r.fleet.printed) != 1 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

func TestAnEmptyFileIsNotPrinted(t *testing.T) {
	r := newRig(t)
	r.stage(nil)

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusBadRequest || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

func TestAPrintRefusedByThePrinterLeavesNoFileInThePrintFolder(t *testing.T) {
	r := newRig(t)
	r.fleet.failed = link.ErrPrintRefused
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}

	if entries, _ := os.ReadDir(filepath.Join(r.data, "print")); len(entries) != 0 {
		t.Fatalf("the print folder keeps %d entries", len(entries))
	}
}

func TestANameAPrinterDoesNotPrintIsRefused(t *testing.T) {
	cases := map[string]int{
		"model.stl":   http.StatusUnsupportedMediaType,
		"a:b.gcode":   http.StatusBadRequest,
		"a%01b.gcode": http.StatusBadRequest,
		"a%1Fb.gcode": http.StatusBadRequest,
		"a%7Fb.gcode": http.StatusBadRequest,
		// U+202E turns the rest of the name around on screen, and U+200B
		// shows nothing.
		"a%E2%80%AEedocg.exe.gcode": http.StatusBadRequest,
		"a%E2%80%8Bb.gcode":         http.StatusBadRequest,
		"a%FFb.gcode":               http.StatusBadRequest,
		"kosti%C4%8Dka.gcode":       http.StatusCreated,
		"a.gcode.":                  http.StatusUnsupportedMediaType,
		"a.gcode%20":                http.StatusUnsupportedMediaType,
		"model.3mf":                 http.StatusUnsupportedMediaType,
		"Bench.GCODE":               http.StatusCreated,
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.stage([]byte("G28\n"))

			rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/"+name, nil, nil)
			if rec.Code != want {
				t.Fatalf("status %d, want %d, body %s", rec.Code, want, rec.Body)
			}
		})
	}
}

func TestAStorageOtherThanLocalIsNotFound(t *testing.T) {
	r := newRig(t)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/usb/a.gcode", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

// formBody is the form PrusaSlicer 2.5 sends.
func formBody(t *testing.T, startPrint, folder, name, content string) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("print", startPrint)
	_ = writer.WriteField("path", folder)

	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = part.Write([]byte(content))
	_ = writer.Close()

	return buf.Bytes(), writer.FormDataContentType()
}

func TestAFormWithPrintTrueStartsThePrint(t *testing.T) {
	r := newRig(t)
	body, contentType := formBody(t, "true", "", "benchy.gcode", "G28\n")
	r.stage(body)

	rec := r.do(http.MethodPost, formPath, nil, http.Header{"Content-Type": {contentType}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}

	if len(r.fleet.printed) != 1 || r.fleet.printed[0].target != "/usb/benchy.gcode" {
		t.Fatalf("printed %v", r.fleet.printed)
	}

	kept, _ := os.ReadFile(filepath.Join(r.data, "print", filepath.FromSlash(r.fleet.printed[0].source)))
	if string(kept) != "G28\n" {
		t.Fatalf("the print folder holds %q", kept)
	}

	if entries, _ := os.ReadDir(filepath.Join(r.data, "incoming")); len(entries) != 0 {
		t.Fatalf("incoming/ keeps %d files", len(entries))
	}
}

func TestAFormWithPrintFalseLandsTheFile(t *testing.T) {
	r := newRig(t)
	body, contentType := formBody(t, "false", "", "benchy.gcode", "G28\n")
	r.stage(body)

	rec := r.do(http.MethodPost, formPath, nil, http.Header{"Content-Type": {contentType}})
	if rec.Code != http.StatusCreated || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}

	if len(r.fleet.landed) != 1 || r.fleet.landed[0].target != "/usb/benchy.gcode" {
		t.Fatalf("landed %v", r.fleet.landed)
	}
}

func TestAFormWithAFolderIsRefused(t *testing.T) {
	r := newRig(t)
	body, contentType := formBody(t, "true", "parts", "benchy.gcode", "G28\n")
	r.stage(body)

	rec := r.do(http.MethodPost, formPath, nil, http.Header{"Content-Type": {contentType}})
	if rec.Code != http.StatusBadRequest || len(r.fleet.printed) != 0 {
		t.Fatalf("status %d, printed %v", rec.Code, r.fleet.printed)
	}
}

func TestABodyThatIsNoFormIsRefused(t *testing.T) {
	r := newRig(t)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPost, formPath, nil, http.Header{"Content-Type": {"text/plain"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNoPersonIsUnauthorized(t *testing.T) {
	service := link.New(link.Options{Fleet: &fakeFleet{}, Data: t.TempDir()})
	handler := routes.New(routes.Host{
		Principal: func(*http.Request) string { return "" },
		BodyFile:  func(*http.Request) string { return "" },
	}, func() *link.Service { return service })

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/printers/x/api/version", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAModuleThatIsStartingAnswersUnavailable(t *testing.T) {
	handler := routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return "" },
	}, func() *link.Service { return nil })

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/printers/x/api/version", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestTheSweepRemovesOldPrintsAndLeftUploads(t *testing.T) {
	data := t.TempDir()
	now := time.Now()
	service := link.New(link.Options{Data: data, Now: func() time.Time { return now }})

	old := now.Add(-25 * time.Hour)

	for _, dir := range []string{"print/1-old", "print/2-new", "incoming"} {
		if err := os.MkdirAll(filepath.Join(data, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]time.Time{
		"print/1-old/a.gcode": old, "print/2-new/b.gcode": now,
		"incoming/upload-1": old, "incoming/upload-2": now,
	}
	for name, mod := range files {
		full := filepath.Join(data, name)
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		_ = os.Chtimes(full, mod, mod)
		_ = os.Chtimes(filepath.Dir(full), mod, mod)
	}

	service.Sweep()

	for name, want := range map[string]bool{
		"print/1-old": false, "print/2-new": true, "incoming/upload-1": false, "incoming/upload-2": true,
	} {
		if _, err := os.Stat(filepath.Join(data, name)); (err == nil) != want {
			t.Errorf("%s: present %v, want %v", name, err == nil, want)
		}
	}
}

// However much the print folder holds, the sweep removes no print younger
// than a day, so a printer still reading a large file over a slow network
// never loses it.
func TestTheSweepKeepsEveryPrintYoungerThanADay(t *testing.T) {
	data := t.TempDir()
	now := time.Now()
	service := link.New(link.Options{Data: data, Now: func() time.Time { return now }})

	// Sparse files: each counts a gigabyte and takes no disk.
	for name, age := range map[string]time.Duration{"1": 23 * time.Hour, "2": 2 * time.Hour, "3": time.Minute} {
		dir := filepath.Join(data, "print", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}

		full := filepath.Join(dir, "a.gcode")
		if err := os.WriteFile(full, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.Truncate(full, 1<<30); err != nil {
			t.Fatal(err)
		}

		mod := now.Add(-age)
		_ = os.Chtimes(full, mod, mod)
		_ = os.Chtimes(dir, mod, mod)
	}

	service.Sweep()

	entries, err := os.ReadDir(filepath.Join(data, "print"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("the print folder holds %d entries, %v, want 3", len(entries), err)
	}
}
