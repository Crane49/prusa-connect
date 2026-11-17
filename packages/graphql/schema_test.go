package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/prusa3d/graphql/internal/gen"
	"github.com/prusa3d/graphql/internal/schema"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The gate ADR-20260910-cqq rule 3 owes: the committed schema is what the generator
// produces from the core document that is in the tree now.
//
// It fails two ways, and both are the same failure seen from different ends. A
// core that moved and a module that was not regenerated look identical here,
// which is the point: neither may be released.

// TestTheCommittedSchemaIsWhatTheDocumentProduces refuses a schema that has
// fallen behind the core.
func TestTheCommittedSchemaIsWhatTheDocumentProduces(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", gen.CoreDocument))
	if err != nil {
		t.Fatalf("read the core document: %v", err)
	}

	model, err := gen.Generate(raw, gen.CoreDocument)
	if err != nil {
		t.Fatalf("the core document and this module's tables disagree: %v", err)
	}

	wanted, err := schema.Files(model)
	if err != nil {
		t.Fatalf("render the files: %v", err)
	}

	for _, name := range slices.Sorted(maps.Keys(wanted)) {
		committed, read := os.ReadFile(name)
		if read != nil {
			t.Fatalf("read the committed %s: %v", name, read)
		}

		if !bytes.Equal(committed, wanted[name]) {
			t.Errorf("%s is not what the core document produces. "+
				"Run `go generate ./...` in packages/graphql and commit what it writes.", name)
		}
	}
}

// TestTheEmbeddedModelBuildsAndEveryFieldResolves holds that what the module
// embeds is a schema it can actually run: every field has a resolver, every
// type it names is declared, and nothing falls back to answering null.
func TestTheEmbeddedModelBuildsAndEveryFieldResolves(t *testing.T) {
	t.Parallel()

	if _, err := buildSchema(deviceapi.NewClient(nil)); err != nil {
		t.Fatalf("the embedded schema does not build: %v", err)
	}
}

// TestEveryFieldTheModelDeclaresIsOneTheResolversBind is the same rule from
// the other side, and it is what a reviewer checks when a field is added: the
// generator writes the field, and a resolver has to be written for it before
// the module will start.
func TestEveryFieldTheModelDeclaresIsOneTheResolversBind(t *testing.T) {
	t.Parallel()

	var model schema.Model
	if err := json.Unmarshal(generated, &model); err != nil {
		t.Fatalf("read the embedded model: %v", err)
	}

	bound := resolversFor(deviceapi.NewClient(nil))

	for root, declared := range map[string][]schema.Field{
		"Query": model.Queries, "Mutation": model.Mutations,
	} {
		for _, one := range declared {
			if _, carried := bound[root+"."+one.Name]; !carried {
				t.Errorf("%s.%s is in the schema and nothing resolves it", root, one.Name)
			}
		}
	}

	if len(bound) != len(model.Queries)+len(model.Mutations) {
		t.Errorf("the module binds %d resolvers and the schema declares %d fields",
			len(bound), len(model.Queries)+len(model.Mutations))
	}
}
