package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The two doors ADR-20260910-cqq task 4 adds, on the real plane and the real graph:
// device.jobs and device.files call what the HTTP routes call, so a module
// and a person are held to one rule, and the ceiling on the module's own work
// is the class each path takes on the HTTP surface.

// browsedFile is the one file the browsing session answers metadata for.
const browsedFile = "usb/part.bgcode"

// browsingSession is a refusingSession that also reports files: one storage,
// three entries under usb, and one file's own entry by its path.
type browsingSession struct {
	refusingSession
}

// browsingBaseline is what a printer that can be asked about its files
// carries: the command verb the refusing session answers, and the two verbs
// the file half is spoken through.
func browsingBaseline() connectorapi.Set {
	baseline := talkingBaseline()
	baseline[connectorapi.VerbSendInfo] = connectorapi.Value{Flag: true}
	baseline[connectorapi.VerbSendFileInfo] = connectorapi.Value{Flag: true}

	return baseline
}

func (s *browsingSession) Capabilities() (connectorapi.Set, uint64) { return browsingBaseline(), 1 }

func (*browsingSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	return []connectorapi.Storage{
		{Mountpoint: "/usb", Name: "usb", Type: "USB", FreeSpace: 4096, FreeSpaceKnown: true},
	}, nil
}

func (*browsingSession) List(_ context.Context, path string) (connectorapi.Listing, error) {
	if path == browsedFile {
		return connectorapi.Listing{
			Directory: connectorapi.File{Path: path, Name: "PART~1.BGC", DisplayName: "part.bgcode", Size: 12},
		}, nil
	}

	return connectorapi.Listing{
		Directory: connectorapi.File{Path: path, Name: path, Folder: true},
		Entries: []connectorapi.File{
			{Path: path + "/examples", Name: "examples", Folder: true},
			{Path: browsedFile, Name: "PART~1.BGC", DisplayName: "part.bgcode", Size: 12},
			{Path: path + "/tangle.bgcode", Name: "tangle.bgcode", Size: 7},
		},
	}, nil
}

// arriveBrowsing puts a printer in the plane that refuses every command with
// the reason given and reports files, and returns its id.
func arriveBrowsing(t *testing.T, plane *devices, serial, reason string) string {
	t.Helper()

	host := plane.Arrivals("test", browsingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &browsingSession{refusingSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
			patches:  make(chan connectorapi.StatePatch),
		},
		reason: reason,
	}}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}

// A jobs read takes observe, as reading the device does. The module's own
// read is inside its ceiling and its grant; a person's is theirs; and a
// stranger is told what a caller asking about an unknown device is told.
func TestAJobsReadTakesWhatReadingTheDeviceTakes(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-MODULE-JOBS")

	grantOrg(t, graph, authz.RelViewer, ceilingPrincipal)
	grantOrg(t, graph, authz.RelViewer, "user:alice")

	t.Run("the module inside its ceiling", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve)

		jobs, err := fleet.Jobs(t.Context(), deviceapi.JobsParams{ID: id})
		require.NoError(t, err)
		require.Nil(t, jobs.Current, "a machine that has never printed carries a current print")
		require.Nil(t, jobs.Last)
	})

	t.Run("the module outside its ceiling", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionOperate)

		_, err := fleet.Jobs(t.Context(), deviceapi.JobsParams{ID: id})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})

	t.Run("a person the graph lets at the device", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane)

		_, err := fleet.Jobs(t.Context(), deviceapi.JobsParams{Principal: "user:alice", ID: id})
		require.NoError(t, err)
	})

	t.Run("a person holding nothing", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane)

		_, err := fleet.Jobs(t.Context(), deviceapi.JobsParams{Principal: "user:stranger", ID: id})
		require.ErrorIs(t, err, deviceapi.ErrNoDevice)
	})
}

// The root of a device's file tree and a path under it take two classes, as
// they do on the HTTP surface: the storages are what the printer volunteered,
// and a path is asked of the machine. The ceiling on the module's own work is
// held to the class of the path it names.
func TestAFilesReadIsHeldToTheClassOfItsPath(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveBrowsing(t, plane, "SIM-MODULE-FILES", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, ceilingPrincipal)))

	t.Run("observe alone reads the root and not a path", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve)

		root, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id})
		require.NoError(t, err)
		require.Nil(t, root.Entry)
		require.Len(t, root.Storages, 1)
		require.Equal(t, "/usb", root.Storages[0].Mountpoint)
		require.NotNil(t, root.Storages[0].FreeSpace, "the printer reported free space and the door dropped it")
		require.Equal(t, int64(4096), *root.Storages[0].FreeSpace)
		require.Equal(t, 1, root.Total)
		require.Equal(t, deviceapi.DefaultPageLimit, root.Limit)

		_, err = fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: "usb"})
		require.ErrorIs(t, err, deviceapi.ErrDenied)
	})

	t.Run("operate reads a folder, a window of it, and a file", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

		folder, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: "usb"})
		require.NoError(t, err)
		require.NotNil(t, folder.Entry)
		require.True(t, folder.Entry.Folder)
		require.Equal(t, "usb", folder.Entry.Path)
		require.Len(t, folder.Items, 3)
		require.Equal(t, 3, folder.Total)
		require.Empty(t, folder.Storages, "a path answered the storages as well")

		// The window is cut here, and the total is what it was cut from.
		window, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: "usb", Limit: 2, Offset: 2})
		require.NoError(t, err)
		require.Len(t, window.Items, 1)
		require.Equal(t, "usb/tangle.bgcode", window.Items[0].Path)
		require.Equal(t, 3, window.Total)
		require.Equal(t, 2, window.Limit)
		require.Equal(t, 2, window.Offset)

		past, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: "usb", Offset: 10})
		require.NoError(t, err, "a page past the end is empty and not a refusal")
		require.Empty(t, past.Items)
		require.Equal(t, 3, past.Total)

		// One file's metadata, by its path: the entry and nothing inside it.
		file, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: browsedFile})
		require.NoError(t, err)
		require.NotNil(t, file.Entry)
		require.False(t, file.Entry.Folder)
		require.Equal(t, "part.bgcode", file.Entry.DisplayName)
		require.Equal(t, int64(12), file.Entry.Size)
		require.Empty(t, file.Items)
		require.Zero(t, file.Total)
	})

	t.Run("a path that leaves its storage never reaches the machine", func(t *testing.T) {
		t.Parallel()

		fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

		for _, path := range []string{"..", "usb/..", "usb/../../etc/passwd"} {
			_, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: path})
			require.ErrorIsf(t, err, deviceapi.ErrInvalidQuery, "listing %q", path)
		}
	})
}

// A person the module serves is held to the same two classes the HTTP surface
// holds them to. A viewer reads the storages and cannot make this server ask
// the machine, and somebody holding nothing is told there is no such device.
func TestAFilesReadForAPersonIsTheHTTPRule(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arriveBrowsing(t, plane, "SIM-MODULE-FILES-PERSON", "NOT_NOW")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelViewer, "user:watcher")))

	fleet, _ := moduleUnder(t, graph, plane)

	root, err := fleet.Files(t.Context(), deviceapi.FilesParams{Principal: "user:watcher", ID: id})
	require.NoError(t, err)
	require.Len(t, root.Storages, 1)

	_, err = fleet.Files(t.Context(), deviceapi.FilesParams{Principal: "user:watcher", ID: id, Path: "usb"})
	require.ErrorIs(t, err, deviceapi.ErrDenied,
		"a viewer made this server put a frame on the printer's wire")

	_, err = fleet.Files(t.Context(), deviceapi.FilesParams{Principal: "user:stranger", ID: id})
	require.ErrorIs(t, err, deviceapi.ErrNoDevice)

	_, err = fleet.Files(t.Context(), deviceapi.FilesParams{Principal: "user:stranger", ID: id, Path: "usb"})
	require.ErrorIs(t, err, deviceapi.ErrNoDevice)
}

// A device whose resolved set cannot report files answers the verb refusal,
// which is what the HTTP route answers, and not a failure of the host's.
func TestAFilesReadOnADeviceWithNoFileVocabularyIsTheVerbRefusal(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-MODULE-NO-FILES")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, ceilingPrincipal)))

	fleet, _ := moduleUnder(t, graph, plane, authz.ActionObserve, authz.ActionOperate)

	_, err := fleet.Files(t.Context(), deviceapi.FilesParams{ID: id})
	require.ErrorIs(t, err, deviceapi.ErrVerbUnknown)

	_, err = fleet.Files(t.Context(), deviceapi.FilesParams{ID: id, Path: "usb"})
	require.ErrorIs(t, err, deviceapi.ErrVerbUnknown)
}
