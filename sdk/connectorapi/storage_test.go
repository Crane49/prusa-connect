package connectorapi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// filesystemSession is a session that reports one storage and one directory.
type filesystemSession struct {
	connectorapi.Session
}

func (filesystemSession) Storages(context.Context) ([]connectorapi.Storage, error) {
	return []connectorapi.Storage{{Mountpoint: "/usb", Name: "usb"}}, nil
}

func (filesystemSession) List(context.Context, string) (connectorapi.Listing, error) {
	return connectorapi.Listing{}, nil
}

// plainSession is a session whose dialect cannot report files.
type plainSession struct {
	connectorapi.Session
}

func TestFilesReportsWhetherTheDialectHasAFilesystemHalf(t *testing.T) {
	t.Parallel()

	if _, ok := connectorapi.Files(filesystemSession{}); !ok {
		t.Error("a session implementing Filesystem was not recognised as one")
	}

	if _, ok := connectorapi.Files(plainSession{}); ok {
		t.Error("a session that cannot report files was offered as a filesystem")
	}

	if _, ok := connectorapi.Files(nil); ok {
		t.Error("a nil session was offered as a filesystem")
	}
}

// The one rule a path carries into a driver. It is checked on the path that
// reaches the wire, so every shape that puts the segment at an edge has to be
// refused: a lone "..", a leading one, a trailing one and one in the middle.
func TestAPathThatLeavesTheStorageItNamesIsRefused(t *testing.T) {
	t.Parallel()

	escaping := []string{
		"..",
		"../etc/passwd",
		"usb/..",
		"usb/../../etc/passwd",
		"/usb/../etc/passwd",
		"/..",
		"a/../b",
	}

	for _, path := range escaping {
		if !connectorapi.PathEscapes(path) {
			t.Errorf("%q carries a .. segment and was accepted", path)
		}
	}

	staying := []string{
		"",
		"/",
		"usb",
		"/usb",
		"usb/models/a.bgcode",
		"usb/..hidden",
		"usb/a..b",
		"usb/...",
		"/usb/dir..",
	}

	for _, path := range staying {
		if connectorapi.PathEscapes(path) {
			t.Errorf("%q stays inside the storage it names and was refused", path)
		}
	}
}

// A storage that reported no free space and a storage with none left are
// different answers, and a caller reading FreeSpace alone cannot tell them
// apart. The pair is the same shape Value.Limit and Value.HasLimit already use.
func TestAStorageThatReportedNoFreeSpaceIsNotAFullOne(t *testing.T) {
	t.Parallel()

	full := connectorapi.Storage{FreeSpace: 0, FreeSpaceKnown: true}
	silent := connectorapi.Storage{}

	if full.FreeSpace != silent.FreeSpace {
		t.Fatal("the two cases this pair exists for no longer read the same on FreeSpace alone")
	}

	if !full.FreeSpaceKnown {
		t.Error("a storage that reported zero bytes left says it reported nothing")
	}

	if silent.FreeSpaceKnown {
		t.Error("a storage that reported no free space at all says it reported a figure")
	}
}

func TestTheFilesystemVerbsAreValidCapabilityKeys(t *testing.T) {
	t.Parallel()

	for _, verb := range []connectorapi.Capability{
		connectorapi.VerbSendInfo,
		connectorapi.VerbSendFileInfo,
	} {
		if !verb.Valid() {
			t.Errorf("%s is not a valid capability key", verb)
		}

		if !verb.IsCore() {
			t.Errorf("%s is not namespaced by the host", verb)
		}
	}
}

func TestNoFilesystemIsInspectableWithErrorsIs(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(errors.New("outer"), connectorapi.ErrNoFilesystem)
	if !errors.Is(wrapped, connectorapi.ErrNoFilesystem) {
		t.Error("ErrNoFilesystem does not survive being wrapped")
	}
}
