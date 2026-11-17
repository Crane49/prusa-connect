package routes_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/parts/internal/catalog"
	"github.com/prusa3d/parts/internal/dbtest"
	"github.com/prusa3d/parts/internal/hostfake"
	"github.com/prusa3d/parts/internal/routes"
)

// The handlers driven as plain HTTP, with the principal the host would hand
// over on the request's context. routesapi.Serve does the same before it calls
// the handler, so what is proved here is what the host's call reaches.

type harness struct {
	handler http.Handler
	files   *hostfake.Files
}

func newHarness(t *testing.T) harness {
	t.Helper()

	files := &hostfake.Files{}
	held := catalog.New(&hostfake.Records{}, files, catalog.Place{Storage: "local", Root: "parts"},
		func() time.Time { return time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC) })

	db, grant := dbtest.SQLite(t)
	held.Attach(db, grant.Engine)

	return harness{handler: routes.New(held), files: files}
}

// send makes one request as who, and answers the status and the body.
func (h harness) send(t *testing.T, who, method, path, body string) (int, map[string]any) {
	t.Helper()

	target, query, _ := strings.Cut(path, "?")

	request, err := routesapi.Request{
		Method: method, Path: target, Query: query, Principal: who, Body: []byte(body),
	}.HTTP(context.Background())
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}

	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)

	raw, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	answer := map[string]any{}
	if err = json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("%s %s answered a body that is not JSON: %s", method, path, raw)
	}

	return recorder.Code, answer
}

func TestARequestNobodyMadeIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	status, answer := h.send(t, "", http.MethodGet, "/overview", "")
	if status != http.StatusForbidden || answer["code"] != routes.NoPrincipal.Name {
		t.Errorf("a request with no principal answered %d %v", status, answer)
	}
}

func TestEachRefusalCarriesItsOwnCode(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, step := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{http.MethodGet, "/parts/abc", "", http.StatusBadRequest, routes.InvalidQuery.Name},
		{http.MethodGet, "/parts/7", "", http.StatusNotFound, routes.PartNotFound.Name},
		{http.MethodGet, "/parts?order=size", "", http.StatusBadRequest, routes.InvalidQuery.Name},
		{http.MethodPost, "/parts", `{"name":""}`, http.StatusBadRequest, routes.Invalid.Name},
		{http.MethodPost, "/parts", `{"name":"x","colour":"red"}`, http.StatusBadRequest, routes.Invalid.Name},
		{http.MethodPost, "/folders", `{"name":"Jigs","parent":9}`, http.StatusNotFound, routes.FolderNotFound.Name},
		{http.MethodPost, "/folders", `{"name":"Jigs"}`, http.StatusCreated, ""},
		{http.MethodPost, "/folders", `{"name":"JIGS"}`, http.StatusConflict, routes.NameTaken.Name},
		{http.MethodPost, "/parts", `{"name":"Clamp","folder":1}`, http.StatusCreated, ""},
		{http.MethodDelete, "/folders/1", "", http.StatusConflict, routes.FolderNotEmpty.Name},
		{http.MethodPost, "/parts/1/versions", `{"files":[{"path":"parts/1/up/clamp.stl"}]}`,
			http.StatusUnprocessableEntity, routes.FileMissing.Name},
		{http.MethodDelete, "/parts/1/versions/3", "", http.StatusNotFound, routes.VersionNotFound.Name},
		{http.MethodPatch, "/parts/1/comments/4", `{"body":"x"}`, http.StatusNotFound, routes.CommentNotFound.Name},
		{http.MethodPatch, "/parts/1", `{"name":"Clamp","revision":99}`, http.StatusConflict, routes.Changed.Name},
		{http.MethodGet, "/nowhere", "", http.StatusNotFound, ""},
	} {
		status, answer := h.send(t, "user:alice", step.method, step.path, step.body)
		if status != step.status {
			t.Errorf("%s %s answered %d and not %d: %v", step.method, step.path, status, step.status, answer)

			continue
		}

		if step.code != "" && answer["code"] != step.code {
			t.Errorf("%s %s answered the code %v and not %s", step.method, step.path, answer["code"], step.code)
		}
	}
}

func TestAPartGoesFromUploadToDetailToDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if status, answer := h.send(t, "user:alice", http.MethodPost, "/parts",
		`{"name":"Clamp","summary":"for the 20 mm rail","tags":["Rail"]}`); status != http.StatusCreated {
		t.Fatalf("add a part answered %d %v", status, answer)
	}

	h.files.Put("local", "parts/1/u1/clamp.bgcode", 4096, "f00d")

	status, answer := h.send(t, "user:alice", http.MethodPost, "/parts/1/versions",
		`{"note":"first print","files":[{"path":"parts/1/u1/clamp.bgcode"}]}`)
	if status != http.StatusCreated {
		t.Fatalf("add a version answered %d %v", status, answer)
	}

	part, _ := answer["part"].(map[string]any)
	versions, _ := part["versions"].([]any)

	if len(versions) != 1 || part["dir"] != "parts/1" || part["storage"] != "local" {
		t.Fatalf("the part came back as %v", part)
	}

	status, listing := h.send(t, "user:bob", http.MethodGet, "/parts?tag=rail", "")
	if status != http.StatusOK || listing["matched"] != float64(1) {
		t.Fatalf("listing by tag answered %d %v", status, listing)
	}

	rows, _ := listing["parts"].([]any)
	row, _ := rows[0].(map[string]any)

	if picture, _ := row["picture"].(map[string]any); picture["kind"] != catalog.PictureThumbnail {
		t.Errorf("the row draws %v, and a print file carries a thumbnail", row["picture"])
	}

	status, deleted := h.send(t, "user:alice", http.MethodDelete, "/parts/1", "")
	if status != http.StatusOK {
		t.Fatalf("delete the part answered %d %v", status, deleted)
	}

	removed, _ := deleted["removed"].(map[string]any)
	if removed["dir"] != "parts/1" || removed["storage"] != "local" {
		t.Errorf("deleting the part answered %v, and the page deletes parts/1 from local", removed)
	}
}

func TestTheOverviewFramesTheScreen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.send(t, "user:alice", http.MethodPost, "/folders", `{"name":"Garage"}`)
	h.send(t, "user:alice", http.MethodPost, "/parts", `{"name":"Hook","folder":1,"tags":["bike"]}`)
	h.send(t, "user:alice", http.MethodPost, "/parts", `{"name":"Knob"}`)

	status, answer := h.send(t, "user:alice", http.MethodGet, "/overview", "")
	if status != http.StatusOK {
		t.Fatalf("the overview answered %d %v", status, answer)
	}

	folders, _ := answer["folders"].([]any)
	first, _ := folders[0].(map[string]any)

	if len(folders) != 1 || first["parts"] != float64(1) || answer["root_parts"] != float64(1) {
		t.Errorf("the overview answered %v", answer)
	}

	if usage, _ := answer["usage"].(map[string]any); usage["bytes"] == float64(0) {
		t.Errorf("the usage reads %v after three writes", usage)
	}

	if answer["principal"] != "user:alice" {
		t.Errorf("the overview names %v as the caller", answer["principal"])
	}
}

// JSON may spell every character as \uXXXX. A description at its limit sent
// that way is six times as long on the wire, and it is still accepted.
func TestADescriptionAtItsLimitIsAcceptedHoweverItIsSpelt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// č is two bytes, so this is the limit in bytes and twelve times as many
	// on the wire.
	escaped := strings.Repeat(`\u010d`, catalog.MaxDescriptionBytes/2)

	status, answer := h.send(t, "user:alice", http.MethodPost, "/parts",
		`{"name":"Bracket","description":"`+escaped+`"}`)
	if status != http.StatusCreated {
		t.Errorf("a description of %d bytes spelt as escapes answered %d %v",
			catalog.MaxDescriptionBytes, status, answer["code"])
	}
}
