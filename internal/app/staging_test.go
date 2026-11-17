package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// stagingConfig declares one filesystem storage over root.
func stagingConfig(root string) string {
	return "core:\n  api:\n    addr: \"127.0.0.1:0\"\nresources:\n  storage:\n    bench:\n" +
		"      type: filesystem\n      enabled: true\n      required: false\n" +
		"      config:\n        root: " + slashed(root) + "\n"
}

// stageOne stands up a supervised filesystem storage over a directory and
// returns the staging that reads from it.
//
// It is the real supervisor and the real built-in rather than a fake, because
// what this is checking is whether a file reaches a printer through the
// supervised call path at all, and a fake of that path would answer for
// itself.
func stageOne(t *testing.T, root string) staging {
	t.Helper()

	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), stagingConfig(root))

	types, err := registry.New()
	require.NoError(t, err)

	host, err := resourcehost.New(resourcehost.Config{Registry: types})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	stopped := make(chan error, 1)

	go func() { stopped <- host.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-stopped)
	})

	store, err := resource.New(resource.Config{Paths: resource.Paths{
		File:    filepath.Join(dir, "config.yaml"),
		Overlay: filepath.Join(dir, "config.d", "resources.json"),
	}, Plane: host, Graph: newGraph(t)})
	require.NoError(t, err)
	require.NoError(t, store.Reload(t.Context()))

	waitServing(t, host, "bench")

	return staging{host: host}
}

// waitServing blocks until the instance is serving.
//
// Reconcile returns when the plan has been handed over and not when the
// instances in it have started, so a test that read a file straight afterwards
// would be racing the supervisor rather than testing anything.
func waitServing(t *testing.T, host *resourcehost.Host, name string) {
	t.Helper()

	target, err := ref.New(ref.KindStorage, name)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		running, found := host.Instance(target)

		return found && running.State().Serving()
	}, 5*time.Second, 5*time.Millisecond, "storage:%s never started serving", name)
}

// A file in a declared storage is readable through the supervised call path,
// at its whole length and from an offset, which is what a transfer does.
func TestAStagedFileIsReadableThroughItsStorageResource(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		body := []byte("G28\nG1 X10 Y10 F3000\nM104 S0\n")
		require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), body, 0o600))

		files := stageOne(t, root)

		content, err := files.content(t.Context(), "bench", "part.gcode")
		require.NoError(t, err)
		require.Equal(t, int64(len(body)), content.Size())

		whole, err := content.Open(t.Context(), 0)
		require.NoError(t, err)

		read, err := io.ReadAll(whole)
		require.NoError(t, err)
		require.NoError(t, whole.Close())
		require.Equal(t, body, read)

		// A transfer opens the same file again for the next range it is asked for,
		// so an offset has to land where it says.
		rest, err := content.Open(t.Context(), 4)
		require.NoError(t, err)

		tail, err := io.ReadAll(rest)
		require.NoError(t, err)
		require.NoError(t, rest.Close())
		require.Equal(t, body[4:], tail)

		// A device that asks for the byte after the last one has asked a question
		// with an answer.
		past, err := content.Open(t.Context(), int64(len(body)))
		require.NoError(t, err)

		empty, err := io.ReadAll(past)
		require.NoError(t, err)
		require.NoError(t, past.Close())
		require.Empty(t, empty)

		t.Logf("observed: %d bytes readable through storage:bench, offsets 0 and 4 and past the end",
			content.Size())
	})
}

// A storage nobody declared and a file that is not in one are told apart, and
// neither is a failure of the server's.
func TestStagingRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		files := stageOne(t, root)

		_, err := files.content(t.Context(), "nowhere", "part.gcode")
		require.ErrorIs(t, err, ErrNoSuchStorage)
		t.Logf("observed: %v", err)

		_, err = files.content(t.Context(), "bench", "not-here.gcode")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNoSuchStorage)
		t.Logf("observed: %v", err)

		// A name that climbs out of the storage is refused rather than resolved, in
		// either spelling of a separator. It is the source of a printer send, and
		// nothing refused it: the provider rewrote it to a path inside the store and
		// sent whatever was there.
		for _, name := range []string{
			"../escape.gcode", "models/../../out.gcode", "..",
			`..\escape.gcode`, `models\..\..\out.gcode`,
		} {
			_, err = files.content(t.Context(), "bench", name)
			require.Error(t, err, "staging read %q", name)

			var carried *fault.Fault
			require.ErrorAs(t, err, &carried)
			require.Equal(t, fault.StorageInvalidPath.Code(), carried.Code(),
				"%q was refused for some other reason than climbing", name)
		}
	})
}

// A directory is not a file, and a transfer told to send one would offer the
// printer a length it can never read.
func TestStagingRefusesADirectory(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "models"), 0o700))

		files := stageOne(t, root)

		_, err := files.content(t.Context(), "bench", "models")
		require.ErrorIs(t, err, ErrStorageCannotRead)

		var carried *fault.Fault
		require.ErrorAs(t, err, &carried)
		require.Equal(t, fault.StorageNotReadable.Code(), carried.Code())

		t.Logf("observed: %v", err)
	})
}

// A write that ran out of room says so.
//
// It is the code with a remedy in it: NOT_READABLE and UNAVAILABLE both tell
// the caller to change something that was never wrong, and UNAVAILABLE is
// retryable, which is the opposite of what a full storage needs. On FAT and USB
// the condition arrives from Close rather than from a write, which is why the
// mapping is here and not where the copy runs.
func TestAFullStorageIsReportedAsOutOfSpace(t *testing.T) {
	t.Parallel()

	err := stagingFault("bench", "part.gcode", "save",
		fmt.Errorf("close the file: %w", syscall.ENOSPC))

	var carried *fault.Fault
	require.ErrorAs(t, err, &carried)
	require.Equal(t, fault.StorageNoSpace.Code(), carried.Code())
	require.ErrorIs(t, err, syscall.ENOSPC)

	t.Logf("observed: %v", err)
}

// A name that only resolves by leaving the store is answered by what the call
// was for.
//
// A store raises both errors for such a name: the provider contract publishes
// the invalid-path one, and the filesystem underneath raises the absent one.
// A caller reading a file is owed "there is no such file" and a 404. A caller
// writing or deleting is handing over a name the store will never take, so it
// is owed "that name is wrong" and a 400; telling it the file is not there
// sends it looking for a file it was about to create.
func TestANameThatLeavesTheStoreIsAnsweredByWhatTheCallWasFor(t *testing.T) {
	t.Parallel()

	escaped := fmt.Errorf("resolve %q: %w: %w", "../etc/passwd",
		storageapi.ErrInvalidPath, os.ErrNotExist)

	for op, want := range map[string]fault.Code{
		"stat":   fault.StorageFileNotFound.Code(),
		"open":   fault.StorageFileNotFound.Code(),
		"save":   fault.StorageInvalidPath.Code(),
		"delete": fault.StorageInvalidPath.Code(),
		"rename": fault.StorageInvalidPath.Code(),
		"mkdir":  fault.StorageInvalidPath.Code(),
	} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()

			var carried *fault.Fault
			require.ErrorAs(t, stagingFault("bench", "../etc/passwd", op, escaped), &carried)
			require.Equal(t, want, carried.Code())
		})
	}
}

// A name the store simply holds nothing under is the absent file on every
// call, because nothing about it says the name is wrong.
func TestAMissingFileIsNotFoundOnEveryCall(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("open the file: %w", os.ErrNotExist)

	for _, op := range []string{"stat", "open", "save", "delete", "rename", "mkdir"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()

			var carried *fault.Fault
			require.ErrorAs(t, stagingFault("bench", "part.gcode", op, missing), &carried)
			require.Equal(t, fault.StorageFileNotFound.Code(), carried.Code())
		})
	}
}
