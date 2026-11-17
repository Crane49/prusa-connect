package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/lane"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// warmSession is walkSession reporting its storages as xBuddy does: a list the
// test sets and a wake-up the test sends. It counts every read of a storage
// root with the file questions, holds a root's answer until roots is closed,
// and keeps the background mark each read carried.
type warmSession struct {
	*walkSession

	changed chan struct{}
	roots   chan struct{}
	// ask, when set, holds the answer to the storages question until it is
	// closed, as a printer busy with another command does.
	ask chan struct{}

	marks    sync.Map // path → *lane.Background, nil for a person's read
	storages []connectorapi.Storage
}

func newWarmSession(serial string, names ...string) *warmSession {
	roots := make(chan struct{})
	close(roots)

	s := &warmSession{walkSession: newWalkSession(serial), changed: make(chan struct{}), roots: roots}
	s.setStorages(names...)

	return s
}

func (s *warmSession) setStorages(names ...string) {
	storages := make([]connectorapi.Storage, 0, len(names))
	for _, name := range names {
		storages = append(storages, connectorapi.Storage{Mountpoint: "/" + name, Name: name})
	}

	s.mu.Lock()
	s.storages = storages
	s.mu.Unlock()
}

func (s *warmSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storages, nil
}

func (s *warmSession) AskStorages(ctx context.Context) error {
	if s.ask == nil {
		return nil
	}

	select {
	case <-s.ask:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*warmSession) StoragesReported() bool { return true }

func (s *warmSession) StoragesChanged() <-chan struct{} { return s.changed }

func (s *warmSession) List(ctx context.Context, path string) (connectorapi.Listing, error) {
	s.marks.Store(path, lane.BackgroundOf(ctx))

	if strings.Contains(path, "/") {
		return s.walkSession.List(ctx, path)
	}

	s.mu.Lock()
	s.asked[path]++
	s.mu.Unlock()

	select {
	case <-s.roots:
	case <-ctx.Done():
		return connectorapi.Listing{}, ctx.Err()
	}

	listing := connectorapi.Listing{Directory: connectorapi.File{Path: path, Folder: true}}
	switch path {
	case "usb":
		listing.Entries = walkFiles
	case "":
		s.mu.Lock()
		for _, storage := range s.storages {
			listing.Entries = append(listing.Entries, connectorapi.File{Path: storage.Name, Folder: true})
		}
		s.mu.Unlock()
	}

	return listing, nil
}

func (s *warmSession) mark(path string) *lane.Background {
	held, _ := s.marks.Load(path)
	mark, _ := held.(*lane.Background)

	return mark
}

// arriveWarming admits the session on a plane that holds listings, reports the
// printer's state, and returns the plane, the printer's id and an owner. The
// storages are not reported until the test sends on session.changed.
func arriveWarming(t *testing.T, session *warmSession, state string) (*devices, string, string) {
	t.Helper()

	plane := newPlane(t)
	plane.listings = newListingCache(1 << 20)
	plane.previews = newPreviewCache(1 << 20)
	plane.prefetchThumbnails = true

	baseline := connectorapi.Set{
		connectorapi.VerbSendInfo:     {Flag: true},
		connectorapi.VerbSendFileInfo: {Flag: true},
	}

	host := plane.Arrivals("test", baseline, capability.NewResolver(profile.Registry{}, discard()), "")

	_, err := host.Admit(t.Context(), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(t.Context(), session))
	t.Cleanup(func() { close(session.patches) })

	reportState(session, state)

	id := plane.idOf(session.identity)
	require.Eventually(t, func() bool { return plane.entries[id].idle() == (state == "IDLE") },
		time.Second, time.Millisecond)

	owner, err := authz.NewSubject(humanSubjectType, config.DefaultPrincipal)
	require.NoError(t, err)

	return plane, id, owner.String()
}

func reportState(session *warmSession, state string) {
	session.patches <- connectorapi.StatePatch{Path: printerStatePath, Value: json.RawMessage(`"` + state + `"`)}
}

func heldNow(plane *devices, id, path string) bool {
	_, held := plane.listings.get(id, path)

	return held
}

func TestAnIdlePrinterIsWarmedOnceItsStoragesAreReported(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-1", "usb", "local")
	plane, id, owner := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") && heldNow(plane, id, "local") },
		time.Second, time.Millisecond)

	asked := session.askedAbout()
	require.Equal(t, 1, asked["usb"])
	require.Equal(t, 1, asked["local"])
	require.Zero(t, asked["usb/folder"], "the warm-up read a subfolder")

	listing, err := plane.Files(t.Context(), owner, id, "usb", false)
	require.NoError(t, err)
	require.True(t, listing.Stale)
	require.Equal(t, 1, session.askedAbout()["usb"], "a held root was read again")
}

func TestAStorageThatAppearsIsWarmedAndOneThatWentIsForgotten(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-2", "usb")
	plane, id, _ := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") }, time.Second, time.Millisecond)

	session.setStorages("local")
	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return heldNow(plane, id, "local") }, time.Second, time.Millisecond)
	require.False(t, heldNow(plane, id, "usb"), "the storage that went is still held")
	require.Equal(t, 1, session.askedAbout()["local"])
	require.Equal(t, 1, session.askedAbout()["usb"], "the storage that went was read again")
}

func TestAPrintingPrinterIsWarmedWhenItIsIdleAgain(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-3", "usb", "local")
	plane, id, _ := arriveWarming(t, session, "PRINTING")

	session.changed <- struct{}{}

	require.Never(t, func() bool { return len(session.askedAbout()) > 0 }, 100*time.Millisecond, time.Millisecond,
		"a printing printer was read")

	reportState(session, "IDLE")

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") && heldNow(plane, id, "local") },
		time.Second, time.Millisecond)
}

func TestARetiredPrinterIsNotWarmed(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-4", "usb")
	plane, id, _ := arriveWarming(t, session, "PRINTING")

	session.changed <- struct{}{}
	// The pump handles the storages before it takes the state patch below.
	reportState(session, "PRINTING")

	entry, live := plane.entry(id)
	require.True(t, live)
	plane.retirePrinter(entry)

	reportState(session, "IDLE")

	require.Never(t, func() bool { return len(session.askedAbout()) > 0 }, 100*time.Millisecond, time.Millisecond,
		"a retired printer was read")
}

func TestAWarmUpReadIsBackground(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-5", "usb")
	plane, id, _ := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") }, time.Second, time.Millisecond)
	require.NotNil(t, session.mark("usb"), "the warm-up read went on a person's lane")
}

func TestAPageOpeningAFolderJoinsTheWarmUp(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-6", "usb")
	session.roots = make(chan struct{})
	plane, id, owner := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return session.askedAbout()["usb"] == 1 }, time.Second, time.Millisecond)

	listing, err := plane.Files(t.Context(), owner, id, "usb", false)
	require.NoError(t, err)
	require.True(t, listing.Reading)
	require.Equal(t, 1, session.askedAbout()["usb"], "the page started a read of its own")

	select {
	case <-session.mark("usb").Promoted():
	default:
		t.Fatal("the warm-up a person waits on was not promoted")
	}

	close(session.roots)
}

func TestTheThumbnailWalkIsBackground(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-7", "usb")
	plane, id, owner := arriveWarming(t, session, "IDLE")

	_, err := plane.Files(t.Context(), owner, id, "usb", true)
	require.NoError(t, err)
	walked(t, plane, id)

	require.Nil(t, session.mark("usb"), "a person's read went on the background lane")
	require.NotNil(t, session.mark("usb/a.bgcode"), "the walk asked on a person's lane")
}

// A refreshed storages read answers what is held once the grace is over, as a
// folder does, rather than failing while the printer is busy.
func TestARefreshOfABusyPrintersStoragesAnswersWhatIsHeld(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-STORAGES-BUSY", "usb")
	session.ask = make(chan struct{})
	defer close(session.ask)

	plane, id, owner := arriveWarming(t, session, "PRINTING")

	began := time.Now()
	storages, err := plane.Storages(t.Context(), owner, id, true)
	require.NoError(t, err)
	require.Less(t, time.Since(began), 1500*time.Millisecond, "the route waited on the printer")
	require.Len(t, storages.Items, 1)
}

// A warm-up reads a root nobody opened, so it walks none of its pictures.
func TestAWarmUpWalksNoThumbnails(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-REVIEW-1", "usb")
	plane, id, _ := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") }, time.Second, time.Millisecond)

	require.Zero(t, session.askedAbout()["usb/a.bgcode"],
		"the warm-up of a root nobody opened asked the printer for a thumbnail")
}

// A warm-up owed while the printer prints outlives a send, and runs once the
// printer is idle.
func TestAWarmUpOwedDuringAPrintSurvivesASend(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-REVIEW-2", "usb")
	plane, id, _ := arriveWarming(t, session, "PRINTING")

	session.changed <- struct{}{}
	// The pump handles the storages before it takes the state patch below.
	reportState(session, "PRINTING")

	entry, live := plane.entry(id)
	require.True(t, live)
	require.True(t, entry.warm.Load(), "no warm-up is owed")

	// What relay does before the bytes leave.
	entry.stopPrefetch()

	reportState(session, "IDLE")

	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") }, time.Second, time.Millisecond,
		"the warm-up owed during the print was dropped by a send")
}

// A report that only moves the free space, as each delete does, keeps the
// held root listing.
func TestAFreeSpaceChangeKeepsTheRoot(t *testing.T) {
	t.Parallel()

	session := newWarmSession("SIM-WARM-REVIEW-4", "usb")
	plane, id, owner := arriveWarming(t, session, "IDLE")

	session.changed <- struct{}{}
	require.Eventually(t, func() bool { return heldNow(plane, id, "usb") }, time.Second, time.Millisecond)

	_, err := plane.Files(t.Context(), owner, id, "", false)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return heldNow(plane, id, "") }, time.Second, time.Millisecond)

	// Only the free space moves, as after a delete on the stick.
	session.mu.Lock()
	session.storages[0].FreeSpace, session.storages[0].FreeSpaceKnown = 42, true
	session.mu.Unlock()
	session.changed <- struct{}{}
	// The pump handles the storages before it takes the state patch below.
	reportState(session, "IDLE")

	require.True(t, heldNow(plane, id, ""), "a free-space change forgot the root listing")
}
