package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// reader is the person the fake host lets read its storages.
const reader = "user:ana"

// hostStorages is the host as far as the share routes see it: it lists the
// folders it names, as storage/path, for reader alone. A file is not a
// folder, and any other path does not exist.
type hostStorages []string

func (h hostStorages) Call(_ context.Context, _ wirerpc.Method, params any) (json.RawMessage, error) {
	listed := params.(storageapi.ListParams)

	switch place := listed.Storage + "/" + listed.Path; {
	case listed.Principal != reader:
		return nil, &wirerpc.Error{Code: storageapi.CodeDenied, Message: "no read_storage"}
	case slices.Contains(h, place):
		return json.RawMessage(`{"items":[]}`), nil
	case place == "local/jobs/benchy.gcode":
		return nil, &wirerpc.Error{Code: storageapi.CodeUnreadable, Message: "not a folder"}
	}

	return nil, &wirerpc.Error{Code: storageapi.CodeNoFile, Message: "no such folder"}
}

// sharingTree is a tree that serves, with the shared folders in a new
// directory.
func sharingTree(t *testing.T) *tree {
	t.Helper()

	shares, err := files.OpenShares(t.TempDir())
	if err != nil {
		t.Fatalf("open the shared folders: %v", err)
	}

	state := &tree{}
	folders := hostStorages{"local/jobs", "local/jobs/benchy", "usb/prints"}
	state.sharing(shares, storageapi.NewClient(folders), slog.New(slog.DiscardHandler))
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	return state
}

// answered is one answer of a route, as a value a test compares whole.
type answered struct {
	status int
	body   string
}

func answerOf(t *testing.T, handler http.Handler, method, path string) answered {
	t.Helper()

	recorded := askAs(t, handler, reader, method, path)

	return answered{status: recorded.Code, body: recorded.Body.String()}
}

// askAs sends a request the host forwarded for principal.
func askAs(t *testing.T, handler http.Handler, principal, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	request, err := routesapi.Request{Method: method, Path: path, Principal: principal}.HTTP(t.Context())
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, request)

	return recorded
}

// TestShareRoutes: a folder shared with PUT is listed, and DELETE unshares it.
func TestShareRoutes(t *testing.T) {
	t.Parallel()

	handler := routes(sharingTree(t))

	for _, step := range []struct {
		method, path string
		want         answered
	}{
		{http.MethodGet, "/files/shares", answered{http.StatusOK, `{"items":[]}` + "\n"}},
		{http.MethodPut, "/files/shares/local/jobs/benchy", answered{status: http.StatusNoContent}},
		{http.MethodPut, "/files/shares/usb/prints", answered{status: http.StatusNoContent}},
		{
			http.MethodGet, "/files/shares",
			answered{http.StatusOK,
				`{"items":[{"storage":"local","path":"jobs/benchy"},{"storage":"usb","path":"prints"}]}` + "\n"},
		},
		{http.MethodDelete, "/files/shares/usb/prints", answered{status: http.StatusNoContent}},
		{
			http.MethodGet, "/files/shares",
			answered{http.StatusOK, `{"items":[{"storage":"local","path":"jobs/benchy"}]}` + "\n"},
		},
	} {
		if got := answerOf(t, handler, step.method, step.path); got != step.want {
			t.Fatalf("%s %s answered %+v, want %+v", step.method, step.path, got, step.want)
		}
	}
}

// TestShareRouteRefusals: the root folder, a file and a missing folder are
// refused, a person who may not read the storage is refused, a server without
// a files block says so, and a server that is not serving yet says that.
func TestShareRouteRefusals(t *testing.T) {
	t.Parallel()

	off := &tree{}
	off.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	for _, row := range []struct {
		name   string
		state  *tree
		who    string
		method string
		path   string
		status int
		code   string
	}{
		{"the root folder", sharingTree(t), reader, http.MethodPut, "/files/shares/local/", statusBadRequest,
			ShareInvalid.Code().String()},
		{"no files block", off, reader, http.MethodGet, "/files/shares", statusNotFound, FilesOff.Code().String()},
		{"unknown storage", sharingTree(t), reader, http.MethodPut, "/files/shares/nowhere/jobs", statusBadRequest,
			ShareInvalid.Code().String()},
		{"missing folder", sharingTree(t), reader, http.MethodPut, "/files/shares/local/gone", statusBadRequest,
			ShareInvalid.Code().String()},
		{"a file", sharingTree(t), reader, http.MethodPut, "/files/shares/local/jobs/benchy.gcode",
			statusBadRequest, ShareInvalid.Code().String()},
		{"no read_storage", sharingTree(t), "user:bob", http.MethodPut, "/files/shares/local/jobs",
			statusForbidden, ShareDenied.Code().String()},
		{"not serving", &tree{}, reader, http.MethodDelete, "/files/shares/local/jobs", statusUnavailable,
			TransportUnavailable.Code().String()},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			recorded := askAs(t, routes(row.state), row.who, row.method, row.path)
			got := answered{status: recorded.Code, body: refusalCode(t, recorded.Body.Bytes())}

			if want := (answered{status: row.status, body: row.code}); got != want {
				t.Errorf("the route answered %+v, want %+v", got, want)
			}
		})
	}
}

// TestUnknownStorageNotRecorded: a refused storage is not kept for a content
// store to be made of.
func TestUnknownStorageNotRecorded(t *testing.T) {
	t.Parallel()

	handler := routes(sharingTree(t))
	answerOf(t, handler, http.MethodPut, "/files/shares/nowhere/jobs")

	want := answered{http.StatusOK, `{"items":[]}` + "\n"}
	if got := answerOf(t, handler, http.MethodGet, "/files/shares"); got != want {
		t.Errorf("the shared folders answered %+v, want %+v", got, want)
	}
}
