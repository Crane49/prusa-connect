package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/threemf/internal/projects"
	"github.com/prusa3d/threemf/internal/storagefake"
)

// The module, driven the way the host drives it: the method set main.go
// builds, called over a storage in memory. Nothing here opens a socket or
// starts a process, because the module does neither.

// The person every request is made as, and the storage the files are in.
const (
	alice   = "user:alice"
	storage = "local"
)

// fixture reads one of the projects the reader tests itself on.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("threemf", "testdata", name))
	if err != nil {
		t.Fatalf("read the fixture %s: %v", name, err)
	}

	return raw
}

// aModule builds the method set over a storage holding the multibed project
// at projects/multibed.3mf.
func aModule(t *testing.T) (wirerpc.Methods, *storagefake.Files) {
	t.Helper()

	files := &storagefake.Files{}
	files.Put(storage, "projects/multibed.3mf", fixture(t, "prusaslicer-2.9.6-multibed.3mf"), 1_761_000_000)

	return methods(storageapi.NewClient(nobody{}), projects.New(files)), files
}

// nobody is a session that answers no call. file.analyze opens its file
// through the storage client, which these tests do not reach.
type nobody struct{}

func (nobody) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return nil, storageapi.ErrUnreadable
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

	raw, err := handler(context.Background(), wirerpc.Call{Method: routesapi.MethodServe, Params: params})
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

// get asks one route as alice.
func get(t *testing.T, serves wirerpc.Methods, path, query string) routesapi.Response {
	t.Helper()

	return call(t, serves, routesapi.Request{Method: "GET", Path: path, Query: query, Principal: alice})
}

// manifest is module.yaml as text.
func manifest(t *testing.T) string {
	t.Helper()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	return strings.ReplaceAll(string(document), "\r\n", "\n")
}

// scalar is the value of a top-level key of the manifest.
func scalar(document, key string) string {
	for line := range strings.SplitSeq(document, "\n") {
		if rest, found := strings.CutPrefix(line, key+":"); found {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}

	return ""
}
