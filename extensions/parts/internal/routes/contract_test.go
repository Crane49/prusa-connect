package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The answers held to the fragment the module publishes. The page is
// generated from that fragment, so a field it calls required and the server
// leaves out, or an array it sends as null, is a page that reads a property of
// undefined. These cases drive every operation with the sparsest data each can
// answer about: an empty catalogue, a part with no tags, no versions and no
// comments, a version and a comment taken away again.

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
			return nil
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

func TestEveryAnswerHoldsToTheFragmentWithTheSparsestData(t *testing.T) {
	t.Parallel()

	doc := readFragment(t)
	h := newHarness(t)

	// An empty catalogue: no folder, no part, no tag.
	h.sendHeld(t, doc, "/overview", http.MethodGet, "/overview", "", http.StatusOK)
	h.sendHeld(t, doc, "/parts", http.MethodGet, "/parts", "", http.StatusOK)

	h.sendHeld(t, doc, "/folders", http.MethodPost, "/folders", `{"name":"Garage"}`, http.StatusCreated)
	h.sendHeld(t, doc, "/folders/{id}", http.MethodPatch, "/folders/1", `{"name":"Shed"}`, http.StatusOK)

	// A part with a name and nothing else.
	h.sendHeld(t, doc, "/parts", http.MethodPost, "/parts", `{"name":"Bare"}`, http.StatusCreated)
	h.sendHeld(t, doc, "/parts/{id}", http.MethodGet, "/parts/1", "", http.StatusOK)
	h.sendHeld(t, doc, "/parts/{id}", http.MethodPatch, "/parts/1", `{"summary":"still bare"}`, http.StatusOK)
	h.sendHeld(t, doc, "/parts", http.MethodGet, "/parts", "", http.StatusOK)
	h.sendHeld(t, doc, "/overview", http.MethodGet, "/overview", "", http.StatusOK)

	// Tags given and taken away again.
	h.sendHeld(t, doc, "/parts/{id}", http.MethodPatch, "/parts/1", `{"tags":["a"]}`, http.StatusOK)
	h.sendHeld(t, doc, "/parts/{id}", http.MethodPatch, "/parts/1", `{"tags":[]}`, http.StatusOK)

	h.files.Put("local", "parts/1/u1/bare.stl", 10, "beef")
	h.sendHeld(t, doc, "/parts/{id}/versions", http.MethodPost, "/parts/1/versions",
		`{"files":[{"path":"parts/1/u1/bare.stl"}]}`, http.StatusCreated)
	h.sendHeld(t, doc, "/parts/{id}/versions/{number}", http.MethodPatch, "/parts/1/versions/1",
		`{"note":""}`, http.StatusOK)

	h.sendHeld(t, doc, "/parts/{id}/comments", http.MethodPost, "/parts/1/comments",
		`{"body":"ok"}`, http.StatusCreated)
	h.sendHeld(t, doc, "/parts/{id}/comments/{comment}", http.MethodPatch, "/parts/1/comments/1",
		`{"body":"fine"}`, http.StatusOK)
	h.sendHeld(t, doc, "/parts/{id}/comments/{comment}", http.MethodDelete, "/parts/1/comments/1",
		"", http.StatusOK)

	// The last version taken away leaves a part with no versions again.
	h.sendHeld(t, doc, "/parts/{id}/versions/{number}", http.MethodDelete, "/parts/1/versions/1",
		"", http.StatusOK)
	h.sendHeld(t, doc, "/parts/{id}", http.MethodGet, "/parts/1", "", http.StatusOK)

	// A part with no files is deleted, and so is the folder.
	h.sendHeld(t, doc, "/parts/{id}", http.MethodDelete, "/parts/1", "", http.StatusOK)
	h.sendHeld(t, doc, "/folders/{id}", http.MethodDelete, "/folders/1", "", http.StatusOK)
	h.sendHeld(t, doc, "/overview", http.MethodGet, "/overview", "", http.StatusOK)
}
