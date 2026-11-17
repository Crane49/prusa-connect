package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// separator joins the segments of a canonical path. It is not "/" because a
// path template is itself a segment and carries slashes.
const separator = " > "

// The node names the rules read. They are the OpenAPI vocabulary and not this
// tool's, so they are spelled once.
const (
	nodeParameters = "parameters"
	nodeRequired   = "required"
	nodeEnum       = "enum"
	nodeTags       = "tags"
	nodePaths      = "paths"
	nodeServers    = "servers"
	nodeResponses  = "responses"
	nodeSecurity   = "security"
	nodeDefault    = "default"
	valueTrue      = "true"
)

// document is one OpenAPI document, read as a tree.
type document map[string]any

// parse reads a document and refuses one this tool has no rules for.
func parse(data []byte) (document, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse the document: %w", err)
	}

	version, _ := doc["openapi"].(string)
	if !strings.HasPrefix(version, "3.") {
		return nil, fmt.Errorf("the document declares openapi %q, and the rules here are written for 3.x",
			version)
	}

	return doc, nil
}

// flatten renders a document as one leaf per canonical path.
//
// Two shapes are canonicalised on the way, because both are ordered in JSON and
// unordered in meaning, and comparing them by index reports a reordering as a
// change:
//
//   - a parameter list becomes an object keyed by "in:name"
//   - a required list and an enum list become objects keyed by their values
//
// Everything else keeps its own keys, so a rule can match on a path a reader of
// the document recognises.
func flatten(doc document) map[string]string {
	out := map[string]string{}

	// The conversion is load-bearing. A Go type switch matches the exact type,
	// so `case map[string]any` does not match the named type, and passing the
	// document itself makes the whole file one leaf that always compares equal
	// to itself.
	walk(nil, map[string]any(doc), out)

	return out
}

func walk(path []string, node any, out map[string]string) {
	switch value := node.(type) {
	case map[string]any:
		if len(value) == 0 {
			out[join(path)] = "{}"

			return
		}

		for _, key := range sortedKeys(value) {
			walk(append(path, key), value[key], out)
		}
	case []any:
		walkList(path, value, out)
	default:
		out[join(path)] = scalar(node)
	}
}

// walkList canonicalises the three list shapes this document uses and refuses
// nothing: a list the rules do not know keeps its index, which makes a
// reordering a change and a change the classifier calls unknown.
func walkList(path []string, list []any, out map[string]string) {
	if len(list) == 0 {
		out[join(path)] = "[]"

		return
	}

	last := ""
	if len(path) > 0 {
		last = path[len(path)-1]
	}

	switch last {
	case nodeParameters:
		for _, item := range list {
			walk(append(path, parameterKey(item)), item, out)
		}
	case nodeRequired, nodeEnum, nodeTags:
		for _, item := range list {
			out[join(append(path, scalar(item)))] = "present"
		}
	default:
		for i, item := range list {
			walk(append(path, strconv.Itoa(i)), item, out)
		}
	}
}

// parameterKey names one parameter by where it travels and what it is called,
// which is what makes it the same parameter across two documents.
func parameterKey(item any) string {
	object, ok := item.(map[string]any)
	if !ok {
		return scalar(item)
	}

	in, _ := object["in"].(string)
	name, _ := object["name"].(string)

	return in + ":" + name
}

func join(path []string) string { return strings.Join(path, separator) }

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// scalar renders a leaf so two documents compare as text. A JSON number is a
// float64 whatever it was written as, so it is formatted rather than printed
// with %v, which would render 200 as 200 in one document and 2e+02 in another.
func scalar(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		if v {
			return valueTrue
		}

		return "false"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}

		return string(encoded)
	}
}

// The three kinds of difference: a node present on one side, absent on the
// other, or carrying a different value.
const (
	kindAdded   = "added"
	kindRemoved = "removed"
	kindChanged = "changed"
)
