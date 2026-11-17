// Package build derives the optional Go build tags a configuration implies.
//
// # The airlock
//
// A configuration file never names a Go build tag. Configuration is operator
// input and a build tag is a compiler argument, so an edited file must not
// choose what the compiler is asked to do. adapters/tags.json is the one place
// a declared type or a config condition is mapped onto a tag. It is in the
// repository and under review, and it only carries tags for adapters that
// exist in this tree.
//
// # Where the tags come from
//
// There are two inputs and one map. The top-level blocks the shipping server
// reads yield a condition each, and the resources block yields the type of
// every instance an operator declared as running and compiled in. An instance
// reached as a service contributes no tag, because its implementation is not
// in this binary.
package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// ErrCatalogue reports an airlock this package will not read. Callers inspect
// it with errors.Is.
var ErrCatalogue = errors.New("build: invalid tag catalogue")

// DefaultCataloguePath is where the airlock lives, relative to the repository
// root. Every recipe that builds this project runs from there.
const DefaultCataloguePath = "adapters/tags.json"

// catalogueFormat is the airlock's schema version. A file carrying another
// version is refused.
const catalogueFormat = 1

// qualifiedSeparator divides a type's namespace from its name.
const qualifiedSeparator = "/"

// Catalogue is the airlock as read. The zero value maps nothing, and Detect
// refuses it: an empty map and a missing file would give the same output, and
// the build is configured from that output.
type Catalogue struct {
	byType      map[typeKey]string
	byBare      map[typeKey][]string
	byCondition map[string]condition
}

// typeKey is a resource kind and a type name.
type typeKey struct {
	kind string
	name string
}

// condition is one config condition and the tag it implies.
type condition struct {
	tag     string
	minimal bool
}

// catalogueFile is the airlock's on-disk shape.
type catalogueFile struct {
	Comment    string               `json:"$comment"`
	Version    int                  `json:"version"`
	Resources  []catalogueResource  `json:"resources"`
	Conditions []catalogueCondition `json:"conditions"`
}

// catalogueResource maps one registered type onto the tag that compiles it in.
type catalogueResource struct {
	Kind string `json:"kind"`
	Type string `json:"type"`
	Tag  string `json:"tag"`
	Note string `json:"note,omitempty"`
}

// catalogueCondition maps one config condition onto the tag it implies, and
// says whether a minimal build keeps it.
type catalogueCondition struct {
	Condition string `json:"condition"`
	Tag       string `json:"tag"`
	Minimal   bool   `json:"minimal"`
	Note      string `json:"note,omitempty"`
}

// LoadCatalogue reads the airlock. A missing file is an error and not an empty
// catalogue: the caller would otherwise build with no optional tags and no way
// to tell that from a configuration that asked for none.
func LoadCatalogue(path string) (Catalogue, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Catalogue{}, fmt.Errorf("%w: read %s: %w", ErrCatalogue, path, err)
	}

	return ParseCatalogue(path, content)
}

// ParseCatalogue reads an airlock already in memory.
func ParseCatalogue(name string, content []byte) (Catalogue, error) {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()

	var file catalogueFile
	if err := decoder.Decode(&file); err != nil {
		return Catalogue{}, fmt.Errorf("%w: %s: %w", ErrCatalogue, name, err)
	}

	if file.Version != catalogueFormat {
		return Catalogue{}, fmt.Errorf("%w: %s is format %d, this build reads %d",
			ErrCatalogue, name, file.Version, catalogueFormat)
	}

	if len(file.Resources) == 0 && len(file.Conditions) == 0 {
		return Catalogue{}, fmt.Errorf("%w: %s maps nothing", ErrCatalogue, name)
	}

	cat := Catalogue{
		byType:      make(map[typeKey]string, len(file.Resources)),
		byBare:      make(map[typeKey][]string, len(file.Resources)),
		byCondition: make(map[string]condition, len(file.Conditions)),
	}

	for _, entry := range file.Resources {
		key := typeKey{kind: entry.Kind, name: entry.Type}
		if _, held := cat.byType[key]; held {
			return Catalogue{}, fmt.Errorf("%w: %s maps %s %s twice", ErrCatalogue, name, entry.Kind, entry.Type)
		}

		cat.byType[key] = entry.Tag

		bare := typeKey{kind: entry.Kind, name: bareName(entry.Type)}
		cat.byBare[bare] = append(cat.byBare[bare], entry.Type)
	}

	for _, entry := range file.Conditions {
		if _, held := cat.byCondition[entry.Condition]; held {
			return Catalogue{}, fmt.Errorf("%w: %s maps condition %s twice", ErrCatalogue, name, entry.Condition)
		}

		cat.byCondition[entry.Condition] = condition{tag: entry.Tag, minimal: entry.Minimal}
	}

	return cat, nil
}

// Knows reports whether a tag is one this tree compiles against. That is every
// tag the airlock names, and the tag of the full variant.
func (c Catalogue) Knows(tag string) bool {
	return slices.Contains(c.Tags(), tag)
}

// Tags is every tag Knows accepts, sorted.
func (c Catalogue) Tags() []string {
	known := map[string]struct{}{VariantFull: {}, VariantMinimal: {}}

	for _, tag := range c.byType {
		if tag != "" {
			known[tag] = struct{}{}
		}
	}

	for _, found := range c.byCondition {
		if found.tag != "" {
			known[found.tag] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(known))
}

// IsZero reports whether the catalogue was never loaded.
func (c Catalogue) IsZero() bool { return c.byCondition == nil && c.byType == nil }

// TagForType returns the tag that compiles one type in, and false when the
// airlock does not carry the type. A type it does not carry is not in this
// tree, as a plugin reached as a service is not, so the absence is not an
// error.
//
// A qualified name matches exactly. A bare name matches when one entry of that
// kind answers to it, and matches nothing when two do. A guess between two
// authors could hand the compiler the wrong tag.
func (c Catalogue) TagForType(kind, name string) (string, bool) {
	if tag, held := c.byType[typeKey{kind: kind, name: name}]; held {
		return tag, true
	}

	candidates := c.byBare[typeKey{kind: kind, name: name}]
	if len(candidates) != 1 {
		return "", false
	}

	return c.byType[typeKey{kind: kind, name: candidates[0]}], true
}

// TagForCondition returns the tag one config condition implies. A condition the
// airlock does not carry is an error: the conditions are this package's own,
// so an absent one means the airlock and the detector disagree about what this
// project builds.
func (c Catalogue) TagForCondition(name string, minimal bool) (string, error) {
	found, held := c.byCondition[name]
	if !held {
		return "", fmt.Errorf("%w: no condition %q, so %s cannot say which tag it implies",
			ErrCatalogue, name, DefaultCataloguePath)
	}

	if minimal && !found.minimal {
		return "", nil
	}

	return found.tag, nil
}

// bareName strips a namespace from a qualified type name.
func bareName(name string) string {
	if _, after, found := strings.Cut(name, qualifiedSeparator); found {
		return after
	}

	return name
}
