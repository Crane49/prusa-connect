package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultSetupFile is the file the first-run wizard writes, in the directory
// this server owns.
const defaultSetupFile = "setup.yaml"

// SetupFile is where the first-run wizard's configuration goes for a given
// operator file: beside the overlay, so the server never writes the operator's
// own file and a read-only mount of it does not stop the wizard.
func SetupFile(file string) string {
	return filepath.Join(filepath.Dir(file), defaultOverlayDir, defaultSetupFile)
}

// SetupBlocks are the top-level blocks the wizard's file may hold. The
// resources block is left out: what the wizard declares there is written
// through the resource store into the overlay, with every other instance a
// person declares in the browser.
//
//nolint:gochecknoglobals // the inventory of blocks the wizard's file holds, never written to.
var SetupBlocks = []string{blockCore, blockPersistence}

// checkSetup refuses a wizard file that holds anything but the blocks it may
// hold, or a key this build does not read, naming the line of that file.
func checkSetup(content []byte) error {
	root, err := parseRoot(content)
	if err != nil || root == nil {
		return err
	}

	for index := 0; index+1 < len(root.Content); index += nodesPerEntry {
		name := root.Content[index]
		if name.Value != blockCore && name.Value != blockPersistence {
			return fmt.Errorf("line %d: %s: the wizard's file holds only %s and %s",
				name.Line, name.Value, blockCore, blockPersistence)
		}
	}

	_, err = parse(content)

	return err
}

// merged is the operator's file laid over the wizard's: a key the operator
// wrote wins, a mapping is merged key by key, and a list or a single value is
// replaced whole. It is re-encoded so that parse reads one document. Each file
// is checked on its own before this runs, so no line number of the merged
// document reaches an operator.
func merged(setup, operator []byte) ([]byte, error) {
	under, err := parseRoot(setup)
	if err != nil {
		return nil, err
	}

	over, err := parseRoot(operator)
	if err != nil {
		return nil, err
	}

	switch {
	case under == nil:
		return operator, nil
	case over == nil:
		return setup, nil
	}

	out, err := yaml.Marshal(mergeMappings(under, over))
	if err != nil {
		return nil, fmt.Errorf("encode the merged configuration: %w", err)
	}

	return out, nil
}

// mergeMappings returns under with every key of over laid on it.
func mergeMappings(under, over *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: under.Tag}
	out.Content = append(out.Content, under.Content...)

	for index := 0; index+1 < len(over.Content); index += nodesPerEntry {
		key, value := over.Content[index], over.Content[index+1]

		at := indexOf(out, key.Value)
		if at < 0 {
			out.Content = append(out.Content, key, value)

			continue
		}

		held := out.Content[at+1]
		if held.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			out.Content[at+1] = mergeMappings(held, value)

			continue
		}

		// A key written with nothing under it means the defaults of that
		// block, and in this install the wizard's values are the defaults.
		if value.Tag == nullTag && held.Kind == yaml.MappingNode {
			continue
		}

		out.Content[at+1] = value
	}

	return out
}

// indexOf is the position of the key named name in a mapping, or -1.
func indexOf(mapping *yaml.Node, name string) int {
	for index := 0; index+1 < len(mapping.Content); index += nodesPerEntry {
		if mapping.Content[index].Value == name {
			return index
		}
	}

	return -1
}

// parseRoot is the top-level mapping of a document, or nil for an empty one or
// one whose top level is not a mapping.
func parseRoot(content []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil //nolint:nilnil // an empty document has no top level, which is not an error.
	}

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse the top level: %w", err)
	}

	return rootMapping(document), nil
}

// Written returns every key the operator's file at path sets, as a dotted path
// to the value written there. A key under a block written with nothing in it
// is not set. The first-run wizard shows these keys locked, because the
// operator's value wins over anything it writes.
func Written(path string) (map[string]any, error) {
	content, err := read(path)
	if err != nil {
		return nil, err
	}

	root, err := parseRoot(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}

	out := map[string]any{}
	if root != nil {
		collect("", root, out)
	}

	return out, nil
}

// collect adds every leaf under node to out, keyed by its dotted path.
func collect(prefix string, node *yaml.Node, out map[string]any) {
	for index := 0; index+1 < len(node.Content); index += nodesPerEntry {
		key, value := node.Content[index], node.Content[index+1]

		path := key.Value
		if prefix != "" {
			path = prefix + "." + key.Value
		}

		if value.Kind == yaml.MappingNode {
			collect(path, value, out)

			continue
		}

		if value.Tag == nullTag {
			continue
		}

		var decoded any
		if value.Decode(&decoded) == nil {
			out[path] = decoded
		}
	}
}

// WrittenDuration formats a duration the way a person writes it in the file.
// Go prints six hours as 6h0m0s, and this returns 6h.
func WrittenDuration(value time.Duration) string {
	written := value.String()

	if strings.HasSuffix(written, "h0m0s") {
		return strings.TrimSuffix(written, "0m0s")
	}

	if strings.HasSuffix(written, "m0s") {
		return strings.TrimSuffix(written, "0s")
	}

	return written
}
