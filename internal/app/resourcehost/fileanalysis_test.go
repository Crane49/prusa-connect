package resourcehost_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The host asks a running module about one file of a type its manifest names
// (ADR-20261027-wvj rules 1 to 4), over the real wire, to the example module
// that reads .demo files.

// demoFile is the storage a module reads through: one file, under one handle.
type demoFile struct{ content []byte }

func (d demoFile) known(handle string) error {
	if handle != "the-handle" {
		return fmt.Errorf("%w: unknown handle", storageapi.ErrNoFile)
	}

	return nil
}

func (d demoFile) Stat(_ context.Context, params storageapi.StatParams) (storageapi.FileInfo, error) {
	if err := d.known(params.Handle); err != nil {
		return storageapi.FileInfo{}, err
	}

	return storageapi.FileInfo{Name: "box.demo", Path: "box.demo", Size: int64(len(d.content))}, nil
}

func (d demoFile) Read(context.Context, storageapi.ReadParams) (storageapi.Chunk, error) {
	return storageapi.Chunk{}, errors.New("this module fetches")
}

func (d demoFile) Write(context.Context, storageapi.WriteParams, io.Reader) (storageapi.Written, error) {
	return storageapi.Written{}, errors.New("this module only reads")
}

func (d demoFile) Move(context.Context, storageapi.MoveParams) (storageapi.Written, error) {
	return storageapi.Written{}, errors.New("this module only reads")
}

func (d demoFile) List(context.Context, storageapi.ListParams) (storageapi.Listing, error) {
	return storageapi.Listing{}, storageapi.ErrNoFile
}

func (d demoFile) Fetch(
	_ context.Context, params storageapi.FetchParams, w io.Writer,
) (storageapi.Fetched, error) {
	if err := d.known(params.Handle); err != nil {
		return storageapi.Fetched{}, err
	}

	end := min(int64(len(d.content)), params.Offset+int64(params.Length))
	n, err := w.Write(d.content[min(params.Offset, end):end])

	return storageapi.Fetched{Size: int64(len(d.content)), Length: n}, err
}

func TestAModuleAnalysesAFileOverTheWire(t *testing.T) {
	t.Parallel()

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		cfg.Storage = func(resourcehost.Module) storageapi.Files {
			return demoFile{content: []byte("Storage box\nlid\nbase\n")}
		}
	})
	module := installAs(t, pluginName)

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        file_types: [.demo]
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, mountedNamespace+"/"+pluginName, filepath.ToSlash(module.dir), module.checksum))
	require.NoError(t, w.store.Reload(t.Context()))
	w.waitReady(theServiceSink(t))

	_, found := w.host.AnalystFor("notes.gcode")
	require.False(t, found, "a module was found for a type it does not read")

	analyst, found := w.host.AnalystFor("BOX.DEMO")
	require.True(t, found)
	require.Equal(t, mountedNamespace, analyst.Namespace)
	require.Equal(t, "1.0.0", analyst.Release)

	answered, err := w.host.Analyze(
		t.Context(),
		analyst,
		fileapi.Params{Handle: "the-handle", Name: "box.demo", Size: 21},
	)
	require.NoError(t, err)
	require.Equal(t, "Storage box", answered.Title)
	require.Len(t, answered.Objects, 2)
	require.Equal(t, "base", answered.Objects[1].Name)

	// The module reads through the handle and nothing else.
	_, err = w.host.Analyze(t.Context(), analyst, fileapi.Params{Handle: "another", Name: "box.demo"})
	require.Error(t, err)

	// The refusal is the module's own, which is what the core keeps.
	_, refused := errors.AsType[*wirerpc.Error](err)
	require.True(t, refused, "the module's refusal arrived as %v", err)
}

// TestTwoModulesThatReadOneTypeAreNeitherAsked is a file the operator wrote
// with two modules that read .demo. The install refuses that; a hand-written
// file can still hold it, and then the core reads the file itself.
func TestTwoModulesThatReadOneTypeAreNeitherAsked(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	first, second := installAs(t, pluginName), installAs(t, pluginName)

	w.writeFile(fmt.Sprintf(`
resources:
  sink:
    example:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        file_types: [.demo]
      service:
        path: %s
        checksum: %s
        contract_major: 1
    provider:
      type: %s
      runtime: service
      enabled: true
      module:
        version: 1.0.0
        file_types: [.demo]
      service:
        path: %s
        checksum: %s
        contract_major: 1
`, mountedNamespace+"/"+pluginName, filepath.ToSlash(first.dir), first.checksum,
		theOtherNamespace+"/"+pluginName, filepath.ToSlash(second.dir), second.checksum))
	require.NoError(t, w.store.Reload(t.Context()))
	w.waitReady(theServiceSink(t))
	provider, err := ref.New(ref.KindSink, "provider")
	require.NoError(t, err)
	w.waitReady(provider)

	_, found := w.host.AnalystFor("box.demo")
	require.False(t, found, "one of two modules that read .demo was chosen")
}
