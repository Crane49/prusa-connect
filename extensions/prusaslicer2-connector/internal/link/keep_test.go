package link_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
	"github.com/prusa3d/prusaslicer2-connector/internal/routes"
)

const keepPath = "/printers/" + printer + "/keep"

// keep sets where the printer's uploads are saved, as the printer's page does.
func (r *rig) keep(body string) {
	r.t.Helper()

	rec := r.do(http.MethodPut, keepPath, strings.NewReader(body), nil)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("set keep: status %d %s", rec.Code, rec.Body)
	}
}

// Save to storage is held per printer and read back as it was set, with the
// folder's slashes trimmed.
func TestAPrintersSaveToStorageReadsBackAsSet(t *testing.T) {
	r := newRig(t)

	if got := decode(t, r.do(http.MethodGet, keepPath, nil, nil)); got["storage"] != "" || got["may_change"] != true {
		t.Fatalf("before any set: %v", got)
	}

	r.keep(`{"storage": "local", "folder": "/prints/mk4/"}`)

	got := decode(t, r.do(http.MethodGet, keepPath, nil, nil))
	if got["storage"] != "local" || got["folder"] != "prints/mk4" {
		t.Fatalf("got %v", got)
	}

	r.keep(`{"storage": ""}`)

	if got = decode(t, r.do(http.MethodGet, keepPath, nil, nil)); got["storage"] != "" || got["folder"] != "" {
		t.Fatalf("after turning it off: %v", got)
	}
}

// With Save to storage on, an Upload is written into the storage as the
// person who sent it and lands on the printer from there. Nothing stays in
// the module's folders.
func TestAKeptUploadIsSavedAndSentFromTheStorage(t *testing.T) {
	r := newRig(t)
	r.keep(`{"storage": "local", "folder": "prints"}`)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/benchy.gcode", nil,
		http.Header{"Print-After-Upload": {"?0"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}

	if r.files.written[alice+" local/prints/benchy.gcode"] != "G28\n" {
		t.Fatalf("written %v", r.files.written)
	}

	want := []printed{
		{principal: alice, id: printer, storage: "local", source: "prints/benchy.gcode", target: "/usb/benchy.gcode"},
	}
	if len(r.fleet.landed) != 1 || r.fleet.landed[0] != want[0] || len(r.fleet.printed) != 0 {
		t.Fatalf("landed %v, printed %v", r.fleet.landed, r.fleet.printed)
	}

	if _, err := os.Stat(filepath.Join(r.data, "staged", "BODY")); !os.IsNotExist(err) {
		t.Fatalf("the staged body is still there: %v", err)
	}
}

// Upload and Print saves the file too, and starts it.
func TestAKeptPrintIsSavedAndStarted(t *testing.T) {
	r := newRig(t)
	r.keep(`{"storage": "local"}`)
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/benchy.gcode", nil,
		http.Header{"Print-After-Upload": {"?1"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}

	if r.files.written[alice+" local/benchy.gcode"] != "G28\n" ||
		len(r.fleet.printed) != 1 || r.fleet.printed[0].storage != "local" {
		t.Fatalf("written %v, printed %v", r.files.written, r.fleet.printed)
	}
}

// A person who may not write into the storage is told so, and the printer is
// not asked.
func TestAKeepThePersonMayNotWriteSaysSo(t *testing.T) {
	r := newRig(t)
	r.keep(`{"storage": "local"}`)
	r.files.err = link.ErrCannotKeep
	r.stage([]byte("G28\n"))

	rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/a.gcode", nil,
		http.Header{"Print-After-Upload": {"?0"}})
	if rec.Code != http.StatusForbidden || len(r.fleet.landed) != 0 {
		t.Fatalf("status %d, landed %v", rec.Code, r.fleet.landed)
	}

	if msg, _ := decode(t, rec)["message"].(string); !strings.Contains(msg, "Save to storage") {
		t.Fatalf("message %q", msg)
	}
}

// A folder that leaves the storage's root, or a storage name with a slash,
// is refused and nothing is saved.
func TestASaveToStorageOutsideTheStorageIsRefused(t *testing.T) {
	r := newRig(t)

	for _, body := range []string{
		`{"storage": "local", "folder": "../etc"}`,
		`{"storage": "local", "folder": "a/../../b"}`,
		`{"storage": "a/b"}`,
		`{"storage": "local", "extra": true}`,
		`not json`,
	} {
		if rec := r.do(http.MethodPut, keepPath, strings.NewReader(body), nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", body, rec.Code)
		}
	}

	if got := decode(t, r.do(http.MethodGet, keepPath, nil, nil)); got["storage"] != "" {
		t.Fatalf("saved %v", got)
	}
}

// Only a person who may operate the printer changes where its uploads go.
func TestAPersonWhoMayNotOperateCannotSetSaveToStorage(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{allowed: false}, Files: r.files, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })

	rec := r.do(http.MethodPut, keepPath, strings.NewReader(`{"storage": "local"}`), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}

	rec = r.do(http.MethodGet, keepPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: status %d", rec.Code)
	}

	if got := decode(t, rec); got["may_change"] != false || got["refused"] != "operate" {
		t.Fatalf("read %v, want may_change false for operate", got)
	}
}

// A module the operator did not let ask whether a person may operate the
// printer refuses every change, since no later call checks the person.
func TestSaveToStorageCannotChangeWhenTheModuleMayNotAsk(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{err: link.ErrCannotAsk}, Files: r.files, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })

	rec := r.do(http.MethodPut, keepPath, strings.NewReader(`{"storage": "local"}`), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}

	if msg, _ := decode(t, rec)["message"].(string); !strings.Contains(msg, "operate check") {
		t.Fatalf("message %q", msg)
	}

	if got := decode(t, r.do(http.MethodGet, keepPath, nil, nil)); got["storage"] != "" {
		t.Fatalf("saved %v", got)
	}
}

// A second upload of the same name into the folder keeps the first, as the
// printer keeps both, and lands on the printer from the numbered copy.
func TestAKeptUploadNeverReplacesAnother(t *testing.T) {
	r := newRig(t)
	r.keep(`{"storage": "local", "folder": "prints"}`)

	for _, body := range []string{"first\n", "second\n", "third\n"} {
		r.stage([]byte(body))

		rec := r.do(http.MethodPut, "/printers/"+printer+"/api/v1/files/local/benchy.bgcode", nil,
			http.Header{"Print-After-Upload": {"?0"}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
	}

	want := map[string]string{
		alice + " local/prints/benchy.bgcode":    "first\n",
		alice + " local/prints/benchy[1].bgcode": "second\n",
		alice + " local/prints/benchy[2].bgcode": "third\n",
	}
	for key, body := range want {
		if r.files.written[key] != body {
			t.Fatalf("written %v, want %v", r.files.written, want)
		}
	}

	if got := r.fleet.landed[2].source; got != "prints/benchy[2].bgcode" {
		t.Fatalf("sent %q", got)
	}
}

// A person who may not change Save to storage learns whether it is on, and
// not which storage and folder the uploads go to.
func TestAPersonWhoMayNotChangeSaveToStorageSeesOnlyWhetherItIsOn(t *testing.T) {
	r := newRig(t)
	r.keep(`{"storage": "secret", "folder": "lab"}`)

	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{err: link.ErrCannotAsk}, Files: r.files, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })

	got := decode(t, r.do(http.MethodGet, keepPath, nil, nil))
	if got["on"] != true || got["storage"] != "" || got["folder"] != "" || got["refused"] != "check" {
		t.Fatalf("read %v", got)
	}
}

// A check that fails says nothing about the person, so the card gets an
// error and not a read-only setting that blames them, and the card's own
// wording rather than PrusaSlicer's.
func TestAFailedCheckIsNoRefusalOfThePerson(t *testing.T) {
	r := newRig(t)
	service := link.New(link.Options{
		Fleet: r.fleet, Access: fakeAccess{err: errors.New("the host went away")}, Files: r.files, Data: r.data,
	})
	r.handler = routes.New(routes.Host{
		Principal: func(*http.Request) string { return alice },
		BodyFile:  func(*http.Request) string { return r.staged },
	}, func() *link.Service { return service })

	rec := r.do(http.MethodGet, keepPath, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}

	if msg, _ := decode(t, rec)["message"].(string); strings.Contains(msg, "upload") {
		t.Fatalf("message %q speaks of an upload", msg)
	}
}
