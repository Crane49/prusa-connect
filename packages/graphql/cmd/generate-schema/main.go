// Command generate-schema writes this module's GraphQL schema from the core's
// committed OpenAPI document.
//
// It writes two files beside the module's source:
//
//	schema.graphql  the schema definition language a client reads
//	model.json      the same schema as data, which the module embeds and
//	                builds its runtime types out of
//
// One description of the surface produces both, so the document a client reads
// and the schema the server executes cannot say different things.
//
// `go generate ./...` in packages/graphql runs it. The gate in
// schema_test.go runs it again and refuses a committed copy that differs, so a
// core that moves fails the module's own tests rather than a client's call.
package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/prusa3d/graphql/internal/gen"
	"github.com/prusa3d/graphql/internal/schema"
)

func main() {
	document := flag.String("core", filepath.Join("..", "..", gen.CoreDocument),
		"the core's committed OpenAPI document")
	out := flag.String("out", ".", "the directory the two files are written into")
	flag.Parse()

	if err := run(*document, *out); err != nil {
		fmt.Fprintf(os.Stderr, "generate-schema: %v\n", err)
		os.Exit(1)
	}
}

// run reads the document, builds the model and writes both files.
func run(document, out string) error {
	raw, err := os.ReadFile(document)
	if err != nil {
		return fmt.Errorf("read the core document: %w", err)
	}

	model, err := gen.Generate(raw, gen.CoreDocument)
	if err != nil {
		return err
	}

	rendered, err := schema.Files(model)
	if err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(rendered)) {
		//nolint:gosec // G703: out is this generator's own output directory flag.
		if err = os.WriteFile(filepath.Join(out, name), rendered[name], 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}

		fmt.Fprintf(os.Stdout, "wrote %s, %d bytes\n", filepath.Join(out, name), len(rendered[name]))
	}

	return nil
}
