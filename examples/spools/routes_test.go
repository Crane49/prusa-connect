package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The two routes are driven the way the host drives them: as routes.serve
// calls, with the principal the host decided riding on the call rather than on
// a header. Nothing here opens a socket, because the module opens none.

// callRoutes makes one routes.serve call against the module's own method set
// and reads the answer back.
func callRoutes(t *testing.T, serves wirerpc.Methods, request routesapi.Request) routesapi.Response {
	t.Helper()

	params, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	handler, ok := serves[routesapi.MethodServe]
	if !ok {
		t.Fatalf("the module answers no %s", routesapi.MethodServe)
	}

	answered, err := handler(context.Background(), wirerpc.Call{Method: routesapi.MethodServe, Params: params})
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.Path, err)
	}

	response, ok := answered.(routesapi.Response)
	if !ok {
		t.Fatalf("the module answered %T and not a routesapi.Response", answered)
	}

	if err = response.Validate(); err != nil {
		t.Fatalf("the host would refuse this answer: %v", err)
	}

	return response
}

// listing is the shape GET /spools answers with.
type listing struct {
	Spools    []Spool `json:"spools"`
	Principal string  `json:"principal"`
}

// created is the shape POST /spools answers with.
type created struct {
	Spool     Spool  `json:"spool"`
	Principal string `json:"principal"`
}

// refusal is the RFC 9457 document a refused write answers with.
type refusal struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Retryable bool   `json:"retryable"`
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()

	var into T
	if err := json.Unmarshal(body, &into); err != nil {
		t.Fatalf("read the answer %q: %v", body, err)
	}

	return into
}

func TestTheReadAnswersTheShelfAndThePrincipalTheHostDecided(t *testing.T) {
	t.Parallel()

	serves := methods(&shelf{rows: starting(), size: maxSpools})

	answered := callRoutes(t, serves, routesapi.Request{
		Method:    http.MethodGet,
		Path:      "/spools",
		Principal: "user:alice",
		// A header naming somebody else, which the module must not read. The
		// host's answer is the only identity a module has.
		Headers: map[string][]string{"X-Principal": {"user:mallory"}},
	})

	if answered.Status != http.StatusOK {
		t.Fatalf("the read answered %d: %s", answered.Status, answered.Body)
	}

	shelf := decode[listing](t, answered.Body)

	if shelf.Principal != "user:alice" {
		t.Errorf("the read attributed the call to %q", shelf.Principal)
	}

	if len(shelf.Spools) != 2 {
		t.Fatalf("the shelf read as %+v", shelf.Spools)
	}

	if shelf.Spools[0].ID != "petg-black" || shelf.Spools[0].Material != "PETG" {
		t.Errorf("the first row read as %+v", shelf.Spools[0])
	}
}

func TestTheWriteAddsASpoolTheNextReadAnswersWith(t *testing.T) {
	t.Parallel()

	serves := methods(&shelf{rows: starting(), size: maxSpools})

	answered := callRoutes(t, serves, routesapi.Request{
		Method:    http.MethodPost,
		Path:      "/spools",
		Principal: "user:alice",
		Body:      []byte(`{"id":"pctg-clear","material":"PCTG","colour":"clear","device":"p3f2a1c9b","grams":1000}`),
	})

	if answered.Status != http.StatusCreated {
		t.Fatalf("the write answered %d: %s", answered.Status, answered.Body)
	}

	wrote := decode[created](t, answered.Body)

	if wrote.Spool.ID != "pctg-clear" || wrote.Spool.Grams != 1000 {
		t.Errorf("the write answered %+v", wrote.Spool)
	}

	if wrote.Principal != "user:alice" {
		t.Errorf("the write attributed the call to %q", wrote.Principal)
	}

	// The read the page makes next carries the row, which is the whole of what
	// the page needs to be true.
	read := decode[listing](t, callRoutes(t, serves, routesapi.Request{
		Method: http.MethodGet, Path: "/spools", Principal: "user:alice",
	}).Body)

	if !slices.ContainsFunc(read.Spools, func(row Spool) bool { return row.ID == "pctg-clear" }) {
		t.Errorf("the spool that was written is not on the shelf: %+v", read.Spools)
	}

	// The device query is what a device card would ask with.
	one := decode[listing](t, callRoutes(t, serves, routesapi.Request{
		Method: http.MethodGet, Path: "/spools", Query: "device=p3f2a1c9b", Principal: "user:alice",
	}).Body)

	if len(one.Spools) != 1 || one.Spools[0].ID != "pctg-clear" {
		t.Errorf("the device query read as %+v", one.Spools)
	}
}

func TestTheWriteRefusesWhatTheShelfCannotShow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   string
		status int
		code   string
		says   string
	}{
		{
			name:   "no id",
			body:   `{"material":"PLA","colour":"red","grams":1000}`,
			status: http.StatusBadRequest,
			code:   "spools/INVALID_SPOOL",
			says:   "id says which spool this is",
		},
		{
			name:   "not a spool",
			body:   `["pla-red"]`,
			status: http.StatusBadRequest,
			code:   "spools/INVALID_SPOOL",
			says:   "the body is not a spool",
		},
		{
			name:   "a weight below zero",
			body:   `{"id":"pla-red","material":"PLA","colour":"red","grams":-1}`,
			status: http.StatusBadRequest,
			code:   "spools/INVALID_SPOOL",
			says:   "grams is a weight",
		},
		{
			name:   "an id the shelf holds",
			body:   `{"id":"petg-black","material":"PETG","colour":"black","grams":10}`,
			status: http.StatusConflict,
			code:   "spools/SPOOL_TAKEN",
			says:   "already holds a spool",
		},
		{
			name:   "a field longer than the module keeps",
			body:   `{"id":"` + strings.Repeat("x", maxFieldLen+1) + `","material":"PLA","colour":"red","grams":1}`,
			status: http.StatusBadRequest,
			code:   "spools/INVALID_SPOOL",
			says:   "longer than the module keeps",
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			serves := methods(&shelf{rows: starting(), size: maxSpools})

			answered := callRoutes(t, serves, routesapi.Request{
				Method: http.MethodPost, Path: "/spools", Principal: "user:alice", Body: []byte(one.body),
			})

			if answered.Status != one.status {
				t.Fatalf("the write answered %d and not %d: %s", answered.Status, one.status, answered.Body)
			}

			// The client SDK reads a refusal as RFC 9457 names it: the code
			// reaches ApiError.code and the detail reaches ApiError.message,
			// so a page can show the sentence and branch on the code.
			said := decode[refusal](t, answered.Body)

			if said.Code != one.code {
				t.Errorf("the refusal carries the code %q and not %q", said.Code, one.code)
			}

			if said.Status != one.status {
				t.Errorf("the refusal says status %d and the answer says %d", said.Status, answered.Status)
			}

			if said.Title == "" {
				t.Error("the refusal carries no title")
			}

			if !strings.Contains(said.Detail, one.says) {
				t.Errorf("the refusal reads %q, which does not say %q", said.Detail, one.says)
			}
		})
	}
}

func TestTheShelfStopsAtItsLimit(t *testing.T) {
	t.Parallel()

	held := &shelf{size: maxSpools}
	for i := range maxSpools {
		if err := held.add(Spool{ID: string(rune('a'+i%26)) + string(rune('a'+i/26))}); err != nil {
			t.Fatalf("fill the shelf: %v", err)
		}
	}

	answered := callRoutes(t, methods(held), routesapi.Request{
		Method: http.MethodPost, Path: "/spools", Principal: "user:alice",
		Body: []byte(`{"id":"one-too-many","material":"PLA","colour":"red","grams":1}`),
	})

	if answered.Status != http.StatusInsufficientStorage {
		t.Fatalf("the write over the limit answered %d: %s", answered.Status, answered.Body)
	}
}

// TestAPathTheModuleDoesNotServeReportsWhatItSaw proves the host takes the
// group prefix off before the module is called.
func TestAPathTheModuleDoesNotServeReportsWhatItSaw(t *testing.T) {
	t.Parallel()

	answered := callRoutes(t, methods(&shelf{size: maxSpools}), routesapi.Request{
		Method: http.MethodGet, Path: "/reels", Principal: "user:alice",
	})

	if answered.Status != http.StatusNotFound {
		t.Fatalf("an unserved path answered %d", answered.Status)
	}

	if saw := decode[map[string]string](t, answered.Body)["module_saw"]; saw != "/reels" {
		t.Errorf("the module saw %q, so the mount reached it", saw)
	}
}

// TestTheFragmentIsOneTheHostAccepts is what a module author runs before the
// host does: the embedded document passes the rule the host holds it to, and
// it rebases under a mount without losing a path.
func TestTheFragmentIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	info, err := routesapi.Describe(
		wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
	if err != nil {
		t.Fatalf("the host would refuse this module's fragment: %v", err)
	}

	fragment, err := routesapi.ParseFragment(info.OpenAPI)
	if err != nil {
		t.Fatalf("parse what plugin.info carries: %v", err)
	}

	if !slices.Contains(fragment.Paths(), "/spools") {
		t.Fatalf("the fragment describes %v, and the module serves /spools", fragment.Paths())
	}

	rebased, err := fragment.Rebase("/api/v1/modules/com.github.aux.acme.spools")
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}

	if !strings.Contains(string(rebased), `"/api/v1/modules/com.github.aux.acme.spools/spools"`) {
		t.Errorf("the rebased document does not carry the spools route under the mount")
	}
}
