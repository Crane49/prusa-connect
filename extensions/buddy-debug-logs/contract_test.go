package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// The answers held to the fragment the module publishes. The page is
// generated from that fragment, so a field it calls required and the module
// leaves out, or an array it sends as null, is a page that reads a property of
// undefined. Each operation is driven twice: on an empty store, and after a
// printer sent a line and a point.

// fragment is the module's OpenAPI document, read once per test.
type fragment struct {
	Paths      map[string]map[string]documented `json:"paths"`
	Components struct {
		Schemas map[string]schema `json:"schemas"`
	} `json:"components"`
}

type documented struct {
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

	raw, err := os.ReadFile("openapi.json")
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

// held makes one call and holds its answer to the fragment.
func (r rig) held(t *testing.T, doc fragment, template, method, path, body string, want int) {
	t.Helper()

	response := r.call(t, routesapi.Request{Method: method, Path: path, Principal: "user:alice", Body: []byte(body)})
	if response.Status != want {
		t.Fatalf("%s %s answered %d %s, and the case expects %d", method, path, response.Status, response.Body, want)
	}

	var answer any
	if err := json.Unmarshal(response.Body, &answer); err != nil {
		t.Fatalf("%s %s answered no JSON: %v", method, path, err)
	}

	for _, problem := range doc.check(method+" "+path, answer, doc.answerSchema(t, template, method, want)) {
		t.Error(problem)
	}
}

func TestEveryAnswerHoldsToTheFragment(t *testing.T) {
	t.Parallel()

	doc := readFragment(t)
	r := newRig(t)

	drive := func() {
		r.held(t, doc, "/status", http.MethodGet, "/status", "", http.StatusOK)
		r.held(t, doc, "/sources", http.MethodGet, "/sources", "", http.StatusOK)
		r.held(t, doc, "/logs", http.MethodGet, "/logs", "", http.StatusOK)
		r.held(t, doc, "/metrics/names", http.MethodGet, "/metrics/names", "", http.StatusOK)
		r.held(t, doc, "/metrics/series", http.MethodGet, "/metrics/series?name=temp_bed", "", http.StatusOK)
		r.held(t, doc, "/export", http.MethodGet, "/export?stream=metrics", "", http.StatusOK)
		r.held(t, doc, "/devices/{device_id}/setup", http.MethodPost, "/devices/mk4/setup",
			`{"host":"10.0.0.1","syslog":true,"metrics":true}`, http.StatusAccepted)
		// An hour back keeps what the test just sent, so the next round reads it.
		r.held(t, doc, "/data/purge", http.MethodPost, "/data/purge", `{"older_than_hours":1}`, http.StatusOK)
	}

	drive()

	r.send(t, store.Syslog, syslogLine)
	r.send(t, store.Metrics, metricsDatagram)

	eventually(t, "a line and a point arriving", func() bool {
		usage := r.module.current().Book.Store.Usage()

		return usage[store.Syslog].Bytes > 0 && usage[store.Metrics].Bytes > 0
	})

	drive()

	r.held(t, doc, "/sources/{mac}", http.MethodPut, "/sources/"+testMAC, `{"device_id":"mk4"}`, http.StatusOK)
	r.held(t, doc, "/sources/{mac}", http.MethodPut, "/sources/"+testMAC, `{"device_id":""}`, http.StatusOK)
}
