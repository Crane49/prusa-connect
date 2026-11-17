package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module that reads a file type answers for the core, through a read
// handle for the one file (ADR-20261027-wvj rules 5 to 12). The module here
// is a fake of the supervision plane that reads the file the way a module
// would: through the storage wire, by the handle it was handed.

// readerNamespace is the module that reads .demo files.
const readerNamespace = "com.github.aux.acme.reader"

// demoReader stands for the supervision plane with one module serving that
// reads .demo files.
type demoReader struct {
	instance ref.Ref
	release  string
	files    moduleStorage
	calls    atomic.Int32
	// answer is what the module does with the bytes it read.
	answer func(ctx context.Context, content []byte) (fileapi.Analysis, error)
}

func (d *demoReader) AnalystFor(name string) (resourcehost.Analyst, bool) {
	if !strings.HasSuffix(name, ".demo") {
		return resourcehost.Analyst{}, false
	}

	return resourcehost.Analyst{Instance: d.instance, Namespace: readerNamespace, Release: d.release}, true
}

func (d *demoReader) Analyze(
	ctx context.Context, _ resourcehost.Analyst, params fileapi.Params,
) (fileapi.Analysis, error) {
	d.calls.Add(1)

	var out bytes.Buffer

	_, err := d.files.Fetch(ctx, storageapi.FetchParams{Handle: params.Handle}, &out)
	if err != nil {
		return fileapi.Analysis{}, err
	}

	return d.answer(ctx, out.Bytes())
}

// tinyPNG is a picture the core's own decoder accepts.
func tinyPNG(t *testing.T) []byte {
	t.Helper()

	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 4, 3))))

	return out.Bytes()
}

// analysedPlane is a storage on disk with the demo reader in front of it.
func analysedPlane(t *testing.T, reader *demoReader) (storagePlane, string) {
	t.Helper()

	root := t.TempDir()
	plane := planeOver(t, root)
	plane.previews = newPreviewCache(config.DefaultPreviewCacheBytes)

	handles := newFileHandles()
	instance, err := ref.Parse("sink:reader")
	require.NoError(t, err)

	reader.instance = instance
	if reader.release == "" {
		reader.release = "1.0.0"
	}

	reader.files = moduleStorage{
		standing: moduleStanding{principal: sinkPrincipal(instance)}, raw: plane.stores, handles: handles,
	}
	plane.analysis = newFileAnalysis(reader, handles, config.DefaultPreviewCacheBytes)

	return plane, root
}

// put writes one file straight onto the storage's disk. The storage refuses
// an upload of a type it does not print, and these files are of a type only
// the module knows.
func put(t *testing.T, root, name string, content []byte) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(root, name), content, 0o600))
}

func TestAModuleAnalysesAFileForTheCore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		picture := tinyPNG(t)
		reader := &demoReader{answer: func(_ context.Context, content []byte) (fileapi.Analysis, error) {
			return fileapi.Analysis{
				Title:      string(content),
				Thumbnails: []fileapi.Thumbnail{{MediaType: fileapi.MediaPNG, Width: 4, Height: 3, Data: picture}},
				Print:      &storageapi.PrintInfo{PrinterModel: "MK4S", NozzleMM: 0.6},
				Facts:      []fileapi.Fact{{Key: "plates", Label: "Plates", Value: "2"}},
				Objects:    []fileapi.Object{{ID: "1", Name: "Box"}},
			}, nil
		}}
		plane, root := analysedPlane(t, reader)
		who := subjectOf(config.Default().Principal)

		put(t, root, "box.demo", []byte("Storage box"))

		entry, err := plane.Stat(t.Context(), who, "bench", "box.demo")
		require.NoError(t, err)
		require.NotNil(t, entry.Analysis)
		require.Equal(t, readerNamespace, entry.Analysis.Provider)
		require.Equal(t, "Storage box", entry.Analysis.Title, "the module did not read the file through the handle")
		require.Equal(t, "Plates", entry.Analysis.Facts[0].Label)
		require.Equal(t, "Box", entry.Analysis.Objects[0].Name)
		require.NotNil(t, entry.Print)
		require.Equal(t, "MK4S", entry.Print.PrinterModel)
		require.Equal(t, 4, entry.Print.ThumbnailWidth)

		served, err := plane.Thumbnail(t.Context(), who, "bench", "box.demo")
		require.NoError(t, err)
		require.Equal(t, picture, served.Bytes)
		require.Equal(t, int32(1), reader.calls.Load(), "the picture asked the module again")

		// A type the module does not read is the core's alone.
		put(t, root, "notes.gcode", []byte("G28\n"))

		entry, err = plane.Stat(t.Context(), who, "bench", "notes.gcode")
		require.NoError(t, err)
		require.Nil(t, entry.Analysis)
		require.Equal(t, int32(1), reader.calls.Load())
	})
}

func TestTheCoreReadsAFileWhenTheModuleFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// The module's own refusal, as the wire carries it.
		reader := &demoReader{answer: func(context.Context, []byte) (fileapi.Analysis, error) {
			return fileapi.Analysis{}, &wirerpc.Error{
				Code:    wirerpc.CodeInternal,
				Message: "the module could not read it",
			}
		}}
		plane, root := analysedPlane(t, reader)
		who := subjectOf(config.Default().Principal)

		// A print file under a name the module claims: the core's own reader
		// still knows what it holds.
		put(t, root, "cube10.demo", slicerFile(t, "cube10.bgcode"))

		entry, err := plane.Stat(t.Context(), who, "bench", "cube10.demo")
		require.NoError(t, err)
		require.Nil(t, entry.Analysis, "a failed module was named as the one that answered")
		require.NotNil(t, entry.Print)
		require.Equal(t, "MK4IS", entry.Print.PrinterModel)

		// A refusal is kept, so the next caller does not ask again.
		_, err = plane.Stat(t.Context(), who, "bench", "cube10.demo")
		require.NoError(t, err)
		require.Equal(t, int32(1), reader.calls.Load())
	})
}

func TestAModuleThatDidNotAnswerIsAskedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// A module that stopped, as the supervision plane reports it: no
		// answer of the module's own.
		reader := &demoReader{answer: func(context.Context, []byte) (fileapi.Analysis, error) {
			return fileapi.Analysis{}, errors.New("the module is not serving")
		}}
		plane, root := analysedPlane(t, reader)
		who := subjectOf(config.Default().Principal)

		put(t, root, "a.demo", []byte("abc"))

		for range 2 {
			entry, err := plane.Stat(t.Context(), who, "bench", "a.demo")
			require.NoError(t, err)
			require.Nil(t, entry.Analysis)
		}

		require.Equal(t, int32(2), reader.calls.Load(), "a failure that was not the module's own was kept")
	})
}

func TestAModulesOwnStatNeverWaitsForAModule(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := make(chan struct{})
		reader := &demoReader{answer: func(ctx context.Context, content []byte) (fileapi.Analysis, error) {
			select {
			case <-held:
			case <-ctx.Done():
			}

			return fileapi.Analysis{Title: string(content)}, nil
		}}
		plane, root := analysedPlane(t, reader)
		who := subjectOf(config.Default().Principal)

		put(t, root, "a.demo", []byte("abc"))

		// What a module's storage.stat runs under: it takes the core's reader
		// at once, and asks no module.
		entry, err := plane.Stat(withoutAnalysisWait(t.Context()), who, "bench", "a.demo")
		require.NoError(t, err)
		require.Nil(t, entry.Analysis)
		require.Equal(t, int32(0), reader.calls.Load())

		close(held)
	})
}

func TestAnAnalysisIsKeptPerFileVersion(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := make(chan struct{})
		reader := &demoReader{answer: func(ctx context.Context, content []byte) (fileapi.Analysis, error) {
			if string(content) == "slow" {
				select {
				case <-held:
				case <-ctx.Done():
					return fileapi.Analysis{}, ctx.Err()
				}
			}

			return fileapi.Analysis{Title: string(content)}, nil
		}}
		plane, root := analysedPlane(t, reader)
		who := subjectOf(config.Default().Principal)

		put(t, root, "a.demo", []byte("first"))

		for range 3 {
			entry, statErr := plane.Stat(t.Context(), who, "bench", "a.demo")
			require.NoError(t, statErr)
			require.Equal(t, "first", entry.Analysis.Title)
		}

		require.Equal(t, int32(1), reader.calls.Load(), "one file version was asked more than once")

		// A new release of the module is a new key.
		reader.release = "1.1.0"
		_, err := plane.Stat(t.Context(), who, "bench", "a.demo")
		require.NoError(t, err)
		require.Equal(t, int32(2), reader.calls.Load())

		// A listing waits two seconds and then takes the core's reader. The
		// call goes on, and its answer is there for the next caller.
		put(t, root, "b.demo", []byte("slow"))

		_, err = plane.Thumbnail(t.Context(), who, "bench", "b.demo")
		require.Error(t, err, "the listing waited for a module that had not answered")

		close(held)
		synctest.Wait()

		entry, err := plane.Stat(t.Context(), who, "bench", "b.demo")
		require.NoError(t, err)
		require.Equal(t, "slow", entry.Analysis.Title)
		require.Equal(t, int32(3), reader.calls.Load())
	})
}

func TestAHandleReadsOneFileForOneInstance(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		plane := planeOver(t, root)

		put(t, root, "a.demo", []byte("abc"))

		info, err := plane.stores.Stat(t.Context(), "bench", "a.demo")
		require.NoError(t, err)

		handles := newFileHandles()
		mine := moduleStorage{standing: moduleStanding{principal: "sink:reader"}, raw: plane.stores, handles: handles}
		other := moduleStorage{standing: moduleStanding{principal: "sink:other"}, raw: plane.stores, handles: handles}

		handle, end := handles.open("sink:reader", "bench", info)

		// The instance it was made for reads the file, with no grant at all.
		chunk, err := mine.Read(t.Context(), storageapi.ReadParams{Handle: handle})
		require.NoError(t, err)
		require.Equal(t, "abc", string(chunk.Data))

		// Another instance does not.
		_, err = other.Read(t.Context(), storageapi.ReadParams{Handle: handle})
		require.ErrorIs(t, err, storageapi.ErrNoFile)

		// A file that changed since is not the file the handle names.
		put(t, root, "a.demo", []byte("abcdef"))

		_, err = mine.Fetch(t.Context(), storageapi.FetchParams{Handle: handle}, io.Discard)
		require.ErrorIs(t, err, storageapi.ErrNoFile)

		// A handle that ended reads nothing.
		info, err = plane.stores.Stat(t.Context(), "bench", "a.demo")
		require.NoError(t, err)

		fresh, endFresh := handles.open("sink:reader", "bench", info)
		end()
		endFresh()

		_, err = mine.Stat(t.Context(), storageapi.StatParams{Handle: fresh})
		require.ErrorIs(t, err, storageapi.ErrNoFile)
	})
}
