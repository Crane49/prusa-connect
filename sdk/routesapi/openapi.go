package routesapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MaxFragmentBytes bounds the OpenAPI fragment a module ships. The fragment
// is part of the plugin.info answer, which is one control frame.
const MaxFragmentBytes = 512 << 10

// ErrFragment reports an OpenAPI fragment a host will not accept. The message
// names the path and the operation.
var ErrFragment = errors.New("routesapi: the OpenAPI fragment is refused")

// fragmentVersion is the OpenAPI version a fragment must declare. The host
// publishes 3.1.
const fragmentVersion = "3.1"

// operationMethods are the path-item members that are operations.
//
//nolint:gochecknoglobals // a fixed set the walk reads; a const cannot hold a slice.
var operationMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// Fragment is the OpenAPI 3.1 document a module ships for the routes it serves.
//
// Its paths are relative to the mount: a module writes /spools. The host
// rebases them under the mount and serves the result at <mount>/openapi.json.
// Build one with ParseFragment.
type Fragment struct {
	members map[string]json.RawMessage
	paths   map[string]json.RawMessage
}

// ParseFragment reads a fragment and refuses one a host cannot publish. The
// document must parse and declare OpenAPI 3.1. Every path must be relative,
// and every operation needs an operationId, a summary and a description.
func ParseFragment(document []byte) (Fragment, error) {
	if len(document) > MaxFragmentBytes {
		return Fragment{}, fmt.Errorf("%w: the document is %d bytes and the limit is %d",
			ErrFragment, len(document), MaxFragmentBytes)
	}

	var members map[string]json.RawMessage
	if err := json.Unmarshal(document, &members); err != nil {
		return Fragment{}, fmt.Errorf("%w: it does not parse as a JSON object: %w", ErrFragment, err)
	}

	var version string
	if err := json.Unmarshal(members["openapi"], &version); err != nil || !strings.HasPrefix(version, fragmentVersion) {
		return Fragment{}, fmt.Errorf("%w: the openapi member is %s and a fragment declares %s",
			ErrFragment, string(members["openapi"]), fragmentVersion)
	}

	var paths map[string]json.RawMessage
	if err := json.Unmarshal(members["paths"], &paths); err != nil || len(paths) == 0 {
		return Fragment{}, fmt.Errorf("%w: paths is not an object naming at least one path", ErrFragment)
	}

	seen := map[string]string{}

	for _, path := range sortedKeys(paths) {
		if err := checkPath(path, paths[path], seen); err != nil {
			return Fragment{}, err
		}
	}

	return Fragment{members: members, paths: paths}, nil
}

// Paths lists the relative paths the fragment describes, sorted.
func (f Fragment) Paths() []string {
	return sortedKeys(f.paths)
}

// Rebase renders the document with every path under mount. Every other
// member is unchanged.
func (f Fragment) Rebase(mount string) ([]byte, error) {
	if f.paths == nil {
		return nil, fmt.Errorf("%w: the zero Fragment describes nothing", ErrFragment)
	}

	mount = strings.TrimSuffix(mount, "/")

	rebased := make(map[string]json.RawMessage, len(f.paths))
	for path, item := range f.paths {
		rebased[mount+path] = item
	}

	paths, err := json.Marshal(rebased)
	if err != nil {
		return nil, fmt.Errorf("routesapi: render the rebased paths: %w", err)
	}

	out := maps.Clone(f.members)
	out["paths"] = paths

	document, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("routesapi: render the rebased document: %w", err)
	}

	return document, nil
}

// Describe declares the routes a module serves on the info it answers
// plugin.info with. It names MethodServe in Serves and attaches the fragment,
// and it refuses a fragment the host would refuse. A host mounts a module
// that names MethodServe only when a fragment comes with it.
func Describe(info wirerpc.Info, fragment []byte) (wirerpc.Info, error) {
	if _, err := ParseFragment(fragment); err != nil {
		return wirerpc.Info{}, err
	}

	if !info.Answers(MethodServe) {
		info.Serves = append(slices.Clone(info.Serves), MethodServe)
	}

	info.OpenAPI = json.RawMessage(bytes.Clone(fragment))

	return info, nil
}

// checkPath checks one path and its operations.
func checkPath(path string, raw json.RawMessage, seen map[string]string) error {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "/api/") || strings.Contains(path, "//") {
		return fmt.Errorf("%w: the path %q is not relative to the mount", ErrFragment, path)
	}

	for segment := range strings.SplitSeq(path, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%w: the path %q carries a dot segment", ErrFragment, path)
		}
	}

	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("%w: the path %q is not an object: %w", ErrFragment, path, err)
	}

	operations := 0

	for _, method := range sortedKeys(item) {
		if !slices.Contains(operationMethods, method) {
			continue
		}

		operations++

		if err := checkOperation(path, method, item[method], seen); err != nil {
			return err
		}
	}

	if operations == 0 {
		return fmt.Errorf("%w: the path %q declares no operation", ErrFragment, path)
	}

	return nil
}

// operation is the part of an operation object the rule reads.
type operation struct {
	ID          string `json:"operationId"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// checkOperation checks that one operation has an operationId, a summary and
// a description.
func checkOperation(path, method string, raw json.RawMessage, seen map[string]string) error {
	where := strings.ToUpper(method) + " " + path

	var op operation
	if err := json.Unmarshal(raw, &op); err != nil {
		return fmt.Errorf("%w: %s is not an operation object: %w", ErrFragment, where, err)
	}

	switch {
	case strings.TrimSpace(op.ID) == "":
		return fmt.Errorf("%w: %s carries no operationId", ErrFragment, where)
	case strings.TrimSpace(op.Summary) == "":
		return fmt.Errorf("%w: %s carries no summary", ErrFragment, where)
	case strings.TrimSpace(op.Description) == "":
		return fmt.Errorf("%w: %s carries no description", ErrFragment, where)
	}

	if other, taken := seen[op.ID]; taken {
		return fmt.Errorf("%w: %s and %s share the operationId %q", ErrFragment, other, where, op.ID)
	}

	seen[op.ID] = where

	return nil
}

// sortedKeys is the keys of one object, sorted.
func sortedKeys(members map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(members))
}
