package main

import (
	"go/format"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/codegen"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The files this command writes, from the package directory.
const (
	committedWire       = "../../internal/transport/http/httpapi/contributes_gen.go"
	committedMapping    = "../../internal/app/resourcehost/contributes_gen.go"
	committedTypeScript = "../../packages/svelte/src/slots/generated.ts"
	committedReference  = "../../docs/src/content/docs/reference/module-slots.md"
	committedGuide      = "../../docs/src/content/docs/writing-a-module.md"
)

// checkingCommitted is the option set the drift gate runs under.
func checkingCommitted() options {
	return options{
		wirePath: committedWire, mappingPath: committedMapping,
		typescriptPath: committedTypeScript,
		referencePath:  committedReference, guidePath: committedGuide,
		check: true,
	}
}

// optionsIn builds an options whose every path is under dir. The guide is
// spliced into an existing page, so its committed copy is written into dir
// first.
func optionsIn(dir string) options {
	committed, err := os.ReadFile(committedGuide)
	if err != nil {
		panic(err)
	}

	guidePath := filepath.Join(dir, "writing-a-module.md")
	if err = os.WriteFile(guidePath, committed, codegen.FilePerms); err != nil {
		panic(err)
	}

	return options{
		wirePath:       filepath.Join(dir, "contributes_gen.go"),
		mappingPath:    filepath.Join(dir, "mapping_gen.go"),
		typescriptPath: filepath.Join(dir, "generated.ts"),
		referencePath:  filepath.Join(dir, "module-slots.md"),
		guidePath:      guidePath,
	}
}

// TestTheWireStructHasOneFieldPerSlot pins the generated struct to the table.
func TestTheWireStructHasOneFieldPerSlot(t *testing.T) {
	t.Parallel()

	source, err := renderWire(manifest.Slots())
	require.NoError(t, err)

	_, err = format.Source(source)
	require.NoError(t, err)

	for _, slot := range manifest.Slots() {
		require.Contains(t, string(source), slot.Name.Field()+" ")
		require.Contains(t, string(source), `json:"`+slot.Name.Key()+`,omitempty"`)
	}
}

// TestTheMappingReadsEverySlot proves no slot is declared and then dropped on
// its way to the wire.
func TestTheMappingReadsEverySlot(t *testing.T) {
	t.Parallel()

	source, err := renderMapping(manifest.Slots())
	require.NoError(t, err)

	for _, slot := range manifest.Slots() {
		require.Contains(t, string(source), slot.Name.Field()+":")
	}
}

// TestAStaleFileFailsTheCheck is the gate: a committed file that differs from
// what the table renders is refused, with the command that fixes it.
func TestAStaleFileFailsTheCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := optionsIn(dir)
	require.NoError(t, run(manifest.Slots(), opts))

	require.NoError(t, os.WriteFile(opts.wirePath, []byte("package httpapi\n"), codegen.FilePerms))

	opts.check = true

	err := run(manifest.Slots(), opts)
	require.ErrorIs(t, err, errStale)
	require.Contains(t, err.Error(), "make generate-slots")
}

// TestWritingThenCheckingIsGreen proves the generator's own output satisfies
// its own gate.
func TestWritingThenCheckingIsGreen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := optionsIn(dir)

	require.NoError(t, run(manifest.Slots(), opts))

	opts.check = true
	require.NoError(t, run(manifest.Slots(), opts))
}

// TestTheCommittedFilesMatchTheTable is the gate that keeps the published
// files from drifting. It is a test rather than only a make target so that it
// runs under `go test ./...` whether or not anybody remembers the target
// (ADR-20260825-bhe rule 9).
func TestTheCommittedFilesMatchTheTable(t *testing.T) {
	t.Parallel()

	if err := run(manifest.Slots(), checkingCommitted()); err != nil {
		t.Errorf("%v", err)
	}
}
