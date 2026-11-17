package manifest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The readers below answer one key of a decoded document. An absent key is the
// zero value. A key of the wrong shape is refused by name.

// text reads a string key.
func text(tree map[string]any, where, key string) (string, error) {
	held, present := tree[key]
	if !present || held == nil {
		return "", nil
	}

	value, isText := held.(string)
	if !isText {
		return "", fmt.Errorf("%w: %s is %s, expected text", ErrInvalid, at(where, key), shapeOf(held))
	}

	return strings.TrimSpace(value), nil
}

// requiredText reads a string key that has to say something.
func requiredText(tree map[string]any, where, key string) (string, error) {
	value, err := text(tree, where, key)
	if err != nil {
		return "", err
	}

	if value == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrInvalid, at(where, key))
	}

	return value, nil
}

// truth reads a boolean key. An absent key is false.
func truth(tree map[string]any, where, key string) (bool, error) {
	held, present := tree[key]
	if !present || held == nil {
		return false, nil
	}

	value, isBool := held.(bool)
	if !isBool {
		return false, fmt.Errorf("%w: %s is %s, expected true or false", ErrInvalid, at(where, key), shapeOf(held))
	}

	return value, nil
}

// whole reads an integer key. yaml.v3 writes an int and encoding/json a
// float64. A float64 that is not whole is refused.
func whole(tree map[string]any, where, key string) (int, error) {
	held, present := tree[key]
	if !present || held == nil {
		return 0, nil
	}

	switch value := held.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		if value != float64(int(value)) {
			return 0, fmt.Errorf("%w: %s is %v, expected a whole number", ErrInvalid, at(where, key), value)
		}

		return int(value), nil
	default:
		return 0, fmt.Errorf("%w: %s is %s, expected a whole number", ErrInvalid, at(where, key), shapeOf(held))
	}
}

// textList reads a list of strings.
func textList(tree map[string]any, where, key string) ([]string, error) {
	held, present := tree[key]
	if !present || held == nil {
		return nil, nil
	}

	items, isList := held.([]any)
	if !isList {
		return nil, fmt.Errorf("%w: %s is %s, expected a list", ErrInvalid, at(where, key), shapeOf(held))
	}

	out := make([]string, 0, len(items))

	for index, item := range items {
		value, isText := item.(string)
		if !isText {
			return nil, fmt.Errorf("%w: %s[%d] is %s, expected text", ErrInvalid, at(where, key), index, shapeOf(item))
		}

		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: %s[%d] is empty", ErrInvalid, at(where, key), index)
		}

		out = append(out, value)
	}

	return out, nil
}

// textTable reads a mapping of string to string. An absent key is an empty
// table.
func textTable(tree map[string]any, where, key string) (map[string]string, error) {
	held, present := tree[key]
	if !present || held == nil {
		return map[string]string{}, nil
	}

	entries, isTable := held.(map[string]any)
	if !isTable {
		return nil, fmt.Errorf("%w: %s is %s, expected a mapping", ErrInvalid, at(where, key), shapeOf(held))
	}

	out := make(map[string]string, len(entries))

	for name, item := range entries {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%w: %s carries an entry with no name", ErrInvalid, at(where, key))
		}

		value, isText := item.(string)
		if !isText {
			return nil, fmt.Errorf("%w: %s[%q] is %s, expected text", ErrInvalid, at(where, key), name, shapeOf(item))
		}

		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: %s[%q] is empty", ErrInvalid, at(where, key), name)
		}

		out[name] = strings.TrimSpace(value)
	}

	return out, nil
}

// node reads a mapping key as a tree of its own. The second answer is whether
// the key was present.
func node(tree map[string]any, where, key string) (map[string]any, bool, error) {
	held, present := tree[key]
	if !present || held == nil {
		return nil, false, nil
	}

	value, isNode := held.(map[string]any)
	if !isNode {
		return nil, false, fmt.Errorf("%w: %s is %s, expected a mapping", ErrInvalid, at(where, key), shapeOf(held))
	}

	return value, true, nil
}

// nodeList reads a list of mappings.
func nodeList(tree map[string]any, where, key string) ([]map[string]any, error) {
	held, present := tree[key]
	if !present || held == nil {
		return nil, nil
	}

	items, isList := held.([]any)
	if !isList {
		return nil, fmt.Errorf("%w: %s is %s, expected a list", ErrInvalid, at(where, key), shapeOf(held))
	}

	out := make([]map[string]any, 0, len(items))

	for index, item := range items {
		entry, isNode := item.(map[string]any)
		if !isNode {
			return nil, fmt.Errorf("%w: %s[%d] is %s, expected a mapping",
				ErrInvalid, at(where, key), index, shapeOf(item))
		}

		out = append(out, entry)
	}

	return out, nil
}

// refuseUnknown refuses a key this version does not read. where is the path
// into the document, and empty at the top level.
func refuseUnknown(tree map[string]any, where string, known []string) error {
	var unknown []string

	for key := range tree {
		if !slices.Contains(known, key) {
			unknown = append(unknown, key)
		}
	}

	if len(unknown) == 0 {
		return nil
	}

	sort.Strings(unknown)

	place := "the manifest"
	if where != "" {
		place = where
	}

	if reason, derived := derivedKeys[unknown[0]]; derived {
		return fmt.Errorf("%w: %s carries %s, and %s", ErrInvalid, place, unknown[0], reason)
	}

	return fmt.Errorf("%w: %s carries %s, which this version does not read; the keys are %s",
		ErrInvalid, place, strings.Join(unknown, ", "), strings.Join(known, ", "))
}

// derivedKeys are the keys an author may write that the host answers itself.
// Each has its own refusal text.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var derivedKeys = map[string]string{
	"namespace": "the host derives the namespace from the module path and builds every address " +
		"from it, so a manifest names none",
	"slot": slotRefusal(),
}

// slotRefusal names every list, so the text moves with the table.
func slotRefusal() string {
	return "a contribution names its list, " + joinNames(contributesKeys()) +
		", and the slot follows from the list it is in"
}

// joinNames writes a list of names in English, with "or" before the last.
func joinNames(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}

	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// at joins a location and a key into the name a refusal quotes. The location
// is empty at the top level, where a key names itself.
func at(where, key string) string {
	if where == "" {
		return key
	}

	return where + "." + key
}

// shapeOf names what a value is, in the words of a document.
func shapeOf(held any) string {
	switch held.(type) {
	case string:
		return "text"
	case bool:
		return "true or false"
	case int, int64, float64:
		return "a number"
	case []any:
		return "a list"
	case map[string]any:
		return "a mapping"
	default:
		return fmt.Sprintf("%T", held)
	}
}
