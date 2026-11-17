package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/parts/internal/catalog"
	"github.com/prusa3d/parts/internal/hostfake"
	"github.com/prusa3d/parts/internal/settings"
)

// The module, driven the way the host drives it: the method set main.go builds,
// configured once and then called. Nothing here opens a socket or starts a
// process, because the module does neither.

// at is the clock a record is written against.
func at() time.Time { return time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC) }

// aModule builds the method set over a record store and a storage in memory,
// sends it the settings the host would send, and fills it with one folder,
// one part in it, one version of that part and one comment on it. Each of
// them has id 1, which is what a test filling a path template relies on.
func aModule(t *testing.T) wirerpc.Methods {
	t.Helper()

	files := &hostfake.Files{}
	files.Put("local", "parts/1/first/bracket.bgcode", 2048, "abc")

	serves := configured(t, &hostfake.Records{}, files)

	for _, step := range []routesapi.Request{
		{Method: http.MethodPost, Path: "/folders", Body: []byte(`{"name":"Brackets"}`)},
		{Method: http.MethodPost, Path: "/parts", Body: []byte(`{"name":"Shelf bracket","folder":1}`)},
		{
			Method: http.MethodPost, Path: "/parts/1/versions",
			Body: []byte(`{"note":"first","files":[{"path":"parts/1/first/bracket.bgcode"}]}`),
		},
		{Method: http.MethodPost, Path: "/parts/1/comments", Body: []byte(`{"body":"prints well in PETG"}`)},
	} {
		step.Principal = "user:alice"

		response := call(t, serves, step)
		if response.Status != http.StatusCreated {
			t.Fatalf("%s %s answered %d: %s", step.Method, step.Path, response.Status, response.Body)
		}
	}

	return serves
}

// configured builds the method set and sends it one plugin.configure call
// with a SQLite database of its own, which is what makes the module open and
// migrate the database and start answering.
func configured(t *testing.T, records *hostfake.Records, files *hostfake.Files) wirerpc.Methods {
	t.Helper()

	live := &settings.Live{}
	opened := &database{}
	t.Cleanup(opened.close)

	serves := methods(catalog.New(records, files, live.Get().Place(), at), live, opened)

	params, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"storage":"local","root":"parts"}`),
		Grants: &wirerpc.Grants{Database: &wirerpc.DatabaseGrant{
			Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(t.TempDir(), "database.sqlite"),
		}},
	})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	if _, err = serves[wirerpc.MethodConfigure](context.Background(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params}); err != nil {
		t.Fatalf("the module refused the settings the host sends: %v", err)
	}

	return serves
}

// call makes one routes.serve call against the module's own method set and
// reads the answer back.
func call(t *testing.T, serves wirerpc.Methods, request routesapi.Request) routesapi.Response {
	t.Helper()

	params, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	handler, ok := serves[routesapi.MethodServe]
	if !ok {
		t.Fatalf("the module answers no %s", routesapi.MethodServe)
	}

	raw, err := handler(context.Background(),
		wirerpc.Call{Method: routesapi.MethodServe, Params: params})
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.Path, err)
	}

	response, ok := raw.(routesapi.Response)
	if !ok {
		t.Fatalf("the module answered %T and not a routesapi.Response", raw)
	}

	if err = response.Validate(); err != nil {
		t.Fatalf("the host would refuse this answer: %v", err)
	}

	return response
}
