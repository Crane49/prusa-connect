package routes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"

	"github.com/prusa3d/materials/internal/routes"
	"github.com/prusa3d/materials/internal/stock"
)

// The answers held to the fragment the module publishes. The page is
// generated from that fragment, so a field it calls required and the server
// leaves out, or an array it sends as null, is a page that reads a property of
// undefined. These cases drive every operation with the sparsest data each can
// answer about: no catalogue, a custom material with a name and a class only,
// a catalogue material with no optional field, a spool with none either.

// fragment is the module's OpenAPI document, read once per test.
type fragment struct {
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]schema `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	Responses map[string]struct {
		Ref     string `json:"$ref"`
		Content map[string]struct {
			Schema schema `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

type schema struct {
	Ref        string            `json:"$ref"`
	Type       string            `json:"type"`
	Required   []string          `json:"required"`
	Properties map[string]schema `json:"properties"`
	Items      *schema           `json:"items"`
	AllOf      []schema          `json:"allOf"`
}

func readFragment(t *testing.T) fragment {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "openapi.json"))
	if err != nil {
		t.Fatalf("read the fragment: %v", err)
	}

	var doc fragment
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the fragment: %v", err)
	}

	return doc
}

func (f fragment) resolve(s schema) schema {
	for s.Ref != "" {
		s = f.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
	}

	return s
}

// check walks one value against its schema and names every place it breaks:
// a required member missing, or an array or an object sent as null or as
// something else.
func (f fragment) check(at string, value any, s schema) []string {
	s = f.resolve(s)

	var problems []string

	for _, part := range s.AllOf {
		problems = append(problems, f.check(at, value, part)...)
	}

	switch s.Type {
	case "array":
		items, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s is %v, and the fragment says an array", at, value)}
		}

		if s.Items != nil {
			for i, item := range items {
				problems = append(problems, f.check(at+"["+strconv.Itoa(i)+"]", item, *s.Items)...)
			}
		}
	case "object", "":
		if len(s.Properties) == 0 && len(s.Required) == 0 {
			return problems
		}

		members, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s is %v, and the fragment says an object", at, value)}
		}

		for _, name := range s.Required {
			if _, found := members[name]; !found {
				problems = append(problems, fmt.Sprintf("%s has no %q, which the fragment requires", at, name))
			}
		}

		for name, member := range members {
			if property, declared := s.Properties[name]; declared {
				problems = append(problems, f.check(at+"."+name, member, property)...)
			}
		}
	}

	return problems
}

// answerSchema is what the fragment says one operation answers with status.
func (f fragment) answerSchema(t *testing.T, path, method string, status int) schema {
	t.Helper()

	response, found := f.Paths[path][strings.ToLower(method)].Responses[strconv.Itoa(status)]
	if !found {
		t.Fatalf("the fragment declares no %d for %s %s", status, method, path)
	}

	if status == http.StatusNoContent {
		return schema{}
	}

	return response.Content["application/json"].Schema
}

// sendHeld makes one call and holds its answer to the fragment.
func (h harness) sendHeld(
	t *testing.T, doc fragment, template, method, path, body string, want int,
) map[string]any {
	t.Helper()

	status, answer := h.send(t, "user:alice", method, path, body)
	if status != want {
		t.Fatalf("%s %s answered %d %v, and the case expects %d", method, path, status, answer, want)
	}

	for _, problem := range doc.check(method+" "+path, answer, doc.answerSchema(t, template, method, status)) {
		t.Error(problem)
	}

	return answer
}

func TestEveryRouteAnswersWhatOpenAPISays(t *testing.T) {
	t.Parallel()

	doc := readFragment(t)
	h := newHarness(t, nil)

	// No catalogue, no custom material, no spool.
	h.sendHeld(t, doc, "/catalogue", http.MethodGet, "/catalogue", "", http.StatusOK)
	h.sendHeld(t, doc, "/catalogue/brands", http.MethodGet, "/catalogue/brands", "", http.StatusOK)
	h.sendHeld(t, doc, "/materials", http.MethodGet, "/materials", "", http.StatusOK)

	// A custom material with a name and a class, changed, starred and listed.
	custom := h.sendHeld(t, doc, "/custom-materials", http.MethodPost, "/custom-materials",
		`{"name":"Bare","class":"FFF"}`, http.StatusCreated)
	id := custom["uuid"].(string)

	h.sendHeld(t, doc, "/custom-materials/{uuid}", http.MethodPatch, "/custom-materials/"+id,
		`{"name":"Still bare","class":"SLA"}`, http.StatusOK)
	h.sendHeld(t, doc, "/favourites/{uuid}", http.MethodPut, "/favourites/"+id, "", http.StatusNoContent)
	h.sendHeld(t, doc, "/materials/{uuid}", http.MethodGet, "/materials/"+id, "", http.StatusOK)
	h.sendHeld(t, doc, "/materials", http.MethodGet, "/materials?favourites=true", "", http.StatusOK)
	h.sendHeld(t, doc, "/favourites/{uuid}", http.MethodDelete, "/favourites/"+id, "", http.StatusNoContent)

	// A catalogue material with nothing but its name, and a bare spool of it.
	h.seed(t, 1)
	h.sendHeld(t, doc, "/catalogue", http.MethodGet, "/catalogue", "", http.StatusOK)
	h.sendHeld(t, doc, "/catalogue/brands", http.MethodGet, "/catalogue/brands", "", http.StatusOK)
	h.sendHeld(t, doc, "/types", http.MethodGet, "/types", "", http.StatusOK)
	h.sendHeld(t, doc, "/locations", http.MethodPost, "/locations", `{"name":"Shelf"}`, http.StatusOK)
	h.sendHeld(t, doc, "/locations/{id}", http.MethodPatch, "/locations/1", `{"name":"Top shelf"}`, http.StatusOK)
	h.sendHeld(t, doc, "/locations", http.MethodGet, "/locations", "", http.StatusOK)
	h.sendHeld(t, doc, "/locations/{id}", http.MethodDelete, "/locations/1", "", http.StatusNoContent)
	h.sendHeld(t, doc, "/list-filters", http.MethodPut, "/list-filters", `{"in_stock":true}`, http.StatusNoContent)
	h.sendHeld(t, doc, "/list-filters", http.MethodGet, "/list-filters", "", http.StatusOK)
	h.sendHeld(t, doc, "/materials/{uuid}", http.MethodGet, "/materials/m-1", "", http.StatusOK)
	h.sendHeld(t, doc, "/materials/{uuid}/spools", http.MethodGet, "/materials/m-1/spools", "", http.StatusOK)
	h.sendHeld(t, doc, "/spools", http.MethodPost, "/spools",
		`{"material_uuid":"m-1","remaining_weight_g":0,"quantity":2}`, http.StatusCreated)
	h.sendHeld(t, doc, "/spools/{id}", http.MethodPatch, "/spools/1", `{"notes":"dry"}`, http.StatusOK)
	h.sendHeld(t, doc, "/materials/{uuid}/spools", http.MethodGet, "/materials/m-1/spools", "", http.StatusOK)

	paged := h.sendHeld(t, doc, "/materials/{uuid}/spools", http.MethodGet, "/materials/m-1/spools?offset=0&limit=1",
		"", http.StatusOK)
	if paged["total"] != 2.0 || paged["next"] != 1.0 {
		t.Errorf("a page of one of two spools answered %v", paged)
	}

	h.sendHeld(t, doc, "/materials", http.MethodGet, "/materials?in_stock=true&limit=1", "", http.StatusOK)
	h.sendHeld(t, doc, "/spools", http.MethodGet, "/spools?material=m-1&limit=1", "", http.StatusOK)
	h.sendHeld(t, doc, "/printers/tools", http.MethodGet, "/printers/tools?limit=1", "", http.StatusOK)

	// A printer that reports nothing, with the spool put on its tool and taken
	// off, and one that reports a bare abbreviation on a tool it assigned.
	h.fleet["d2"] = printer("d2", "", new("PLA"))
	if _, err := h.store.Report(
		context.Background(),
		stock.Assignment{DeviceID: "d2", ToolID: "0", Abbreviation: "PLA"},
		nil,
	); err != nil {
		t.Fatal(err)
	}

	h.sendHeld(t, doc, "/printers", http.MethodGet, "/printers", "", http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools", http.MethodGet, "/printers/d1/tools", "", http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools", http.MethodGet, "/printers/d2/tools", "", http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools/{tool}", http.MethodPut, "/printers/d1/tools/0", `{"spool_id":2}`,
		http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools", http.MethodGet, "/printers/d1/tools", "", http.StatusOK)
	h.sendHeld(t, doc, "/materials/{uuid}/spools", http.MethodGet, "/materials/m-1/spools", "", http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools/{tool}", http.MethodDelete, "/printers/d1/tools/0", "",
		http.StatusNoContent)

	// A spool the printer read off a tag that names only itself and its
	// material, merged with one added by hand.
	tag := printer("d3", "IDLE", new("PLA"))
	tag.Tools.Tools["0"].Reported = &connectorapi.ReportedMaterial{InstanceUUID: "tag-1", MaterialUUID: "m-1"}
	h.fleet["d3"] = tag

	read := h.sendHeld(t, doc, "/printers/{device}/tools/{tool}/spool", http.MethodPost, "/printers/d3/tools/0/spool",
		"", http.StatusOK)
	h.sendHeld(t, doc, "/printers/{device}/tools", http.MethodGet, "/printers/d3/tools", "", http.StatusOK)

	added := h.sendHeld(
		t,
		doc,
		"/spools",
		http.MethodPost,
		"/spools",
		fmt.Sprintf(
			`{"material_uuid":%q,"remaining_weight_g":0,"quantity":1}`,
			read["material_uuid"],
		),
		http.StatusCreated,
	)
	h.sendHeld(t, doc, "/spools/{id}/merge", http.MethodPost, fmt.Sprintf("/spools/%v/merge", added["ids"].([]any)[0]),
		fmt.Sprintf(`{"into":%v}`, read["id"]), http.StatusOK)
	h.sendHeld(t, doc, "/spools/{id}", http.MethodDelete, "/spools/1", "", http.StatusNoContent)

	h.sendHeld(t, doc, "/custom-materials/{uuid}", http.MethodDelete, "/custom-materials/"+id, "", http.StatusNoContent)
	h.sendHeld(t, doc, "/catalogue/download", http.MethodPost, "/catalogue/download", "", http.StatusAccepted)
}

// TestEveryCodeThisModuleAnswersIsInTheDocument holds the code table to the
// enum a client branches on. A code added to codes.go without a line in
// openapi.json is a code no client knows.
func TestEveryCodeThisModuleAnswersIsInTheDocument(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Components struct {
			Schemas struct {
				Refusal struct {
					Properties struct {
						Code struct {
							Enum []string `json:"enum"`
						} `json:"code"`
					} `json:"properties"`
				} `json:"Refusal"`
			} `json:"schemas"`
		} `json:"components"`
	}

	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("read openapi.json: %v", err)
	}

	listed := document.Components.Schemas.Refusal.Properties.Code.Enum
	if len(listed) == 0 {
		t.Fatal("openapi.json lists no refusal code, so this test proved nothing")
	}

	var answered []string

	for _, code := range routes.Codes() {
		answered = append(answered, code.Name)

		if !slices.Contains(listed, code.Name) {
			t.Errorf("the module answers %s and openapi.json does not list it", code.Name)
		}

		if code.Title == "" || !strings.HasPrefix(code.Name, "materials/") {
			t.Errorf("%s carries no English title or not the module's name", code.Name)
		}
	}

	for _, code := range listed {
		if !slices.Contains(answered, code) {
			t.Errorf("openapi.json lists %s and the module never answers it", code)
		}
	}
}
