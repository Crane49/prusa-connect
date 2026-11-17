package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// demoOwned are the keys of the operator's file the demo replaces with its
// own, as dotted paths. Everything else in that file reaches the core as
// written: a declared module, an allowed origin, a trusted proxy, a second
// connector.
//
// Each key here is one of two things. Either it is where the demo keeps its
// state, which goes when the demo directory goes, so a run never writes into
// the operator's graph, overlay or account store. Or it is a setting the
// simulated fleet depends on: the printers dial plain HTTP on the demo's own
// address, and the demo account lives behind the demo's own provider.
//
// A connector's own listener is dropped under whatever name the operator gave
// that connector, because a demo that bound the operator's printer socket
// would take the certificate, the port and the printers of a running
// deployment.
//
//nolint:gochecknoglobals // a fixed list, read-only.
var demoOwned = []string{
	"core.api.addr",
	"core.api.tls",
	"core.connectors." + anyName + ".config.listener",
	"core.log",
	"core.site",
	"core.principal",
	"core.overlay",
	"core.authz.database",
	"core.auth",
	"persistence",
	"resources.history.past",
	"resources.storage.local",
	"resources.storage.scratch",
}

// readOperatorFile reads the file the demo lays its settings over. A path
// that names no file is the same as no path: the demo runs on its defaults,
// which is what a fresh checkout with the configuration file deleted gets.
func readOperatorFile(path string) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	return content, true, nil
}

// demoDocument lays the demo's own document over the operator's and renders
// the file the core reads.
//
// The two are merged as parsed nodes rather than as structs, because the
// resources block has no struct here: its schema is each declared type's, and
// the resource store reads it back out of the file the core is started with.
// A mapping the operator wrote and the demo also writes merges key by key, so
// a connector the operator configured keeps its keys under the demo's
// registration bound. A key in demoOwned is dropped from the operator's side
// first, so the demo's value is the whole of it.
func demoDocument(operator []byte, own string) ([]byte, error) {
	base, err := mappingOf(operator)
	if err != nil {
		return nil, fmt.Errorf("read the configuration file: %w", err)
	}

	over, err := mappingOf([]byte(own))
	if err != nil {
		return nil, fmt.Errorf("read the demo configuration: %w", err)
	}

	for _, key := range demoOwned {
		dropKey(base, strings.Split(key, "."))
	}

	mergeMapping(base, over)

	rendered, err := yaml.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("write the demo configuration: %w", err)
	}

	return rendered, nil
}

// mappingOf parses a document down to its top-level mapping. A document with
// none, which is what an empty file and a file of comments both are, is an
// empty mapping.
func mappingOf(content []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}

	if document.Kind == yaml.DocumentNode && len(document.Content) == 1 &&
		document.Content[0].Kind == yaml.MappingNode {
		return document.Content[0], nil
	}

	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
}

// anyName stands for every key at one step of a path. It is how the demo drops
// a setting that sits under a name the operator chose: a connector's own
// listener is core.connectors.<name>.config.listener, and <name> is the
// operator's word and not this package's.
const anyName = "*"

// dropKey removes one dotted path from a mapping, and does nothing when the
// path is not there. A mapping's Content is keys and values in turn, which is
// why every walk here steps by two.
func dropKey(node *yaml.Node, path []string) {
	for i := 0; i+1 < len(node.Content); {
		if path[0] != anyName && node.Content[i].Value != path[0] {
			i += nodesPerEntry

			continue
		}

		if len(path) == 1 {
			node.Content = append(node.Content[:i], node.Content[i+nodesPerEntry:]...)

			continue
		}

		if node.Content[i+1].Kind == yaml.MappingNode {
			dropKey(node.Content[i+1], path[1:])
		}

		i += nodesPerEntry
	}
}

// mergeMapping writes every key of over into base. Two mappings under one key
// merge; anything else under a key the overlay names is replaced whole, so a
// list or a scalar is the demo's and not a splice of the two.
func mergeMapping(base, over *yaml.Node) {
	for i := 0; i+1 < len(over.Content); i += nodesPerEntry {
		key, value := over.Content[i], over.Content[i+1]

		found := valueOf(base, key.Value)

		switch {
		case found == nil:
			base.Content = append(base.Content, key, value)
		case found.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode:
			mergeMapping(found, value)
		default:
			*found = *value
		}
	}
}

// declaresRecordBackend answers whether the operator's file declares a
// resource of the db kind. A file that cannot be read declares none here;
// demoDocument reports why it cannot be read.
func declaresRecordBackend(operator []byte) bool {
	base, err := mappingOf(operator)
	if err != nil {
		return false
	}

	resources := valueOf(base, "resources")
	if resources == nil {
		return false
	}

	db := valueOf(resources, "db")

	return db != nil && db.Kind == yaml.MappingNode && len(db.Content) > 0
}

// valueOf is the node under one key of a mapping, nil when the key is absent.
func valueOf(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += nodesPerEntry {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

// nodesPerEntry is how many nodes one key of a mapping takes in a yaml.Node:
// the key and its value.
const nodesPerEntry = 2
