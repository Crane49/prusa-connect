package storageapitest

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The suite exercises two provider shapes: full browse/write, and one that
// only presigns. Anything else must be reported as unsupported rather than
// producing a confusing cascade of subtest failures. The caps reaching these
// two functions come from storageapi.Probe, so the optional flags describe the
// interfaces the provider implements.
func TestShapeError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		caps    storageapi.Caps
		wantErr bool
	}{
		{"full", storageapi.Caps{Browse: true, Write: true, LocalFile: true}, false},
		{"presigner only", storageapi.Caps{DownloadURL: true}, false},
		{"nothing declared", storageapi.Caps{}, false},
		{"read only", storageapi.Caps{Browse: true}, true},
		{"write without browse", storageapi.Caps{Write: true}, true},
		{"local file without write", storageapi.Caps{LocalFile: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := shapeError(tc.caps)
			if tc.wantErr && err == nil {
				t.Errorf("shapeError(%+v) = nil, want an error", tc.caps)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("shapeError(%+v) = %v, want nil", tc.caps, err)
			}
		})
	}
}

// A provider that never declared where its store is must be told so, rather
// than certified on a run where every escape subtest silently skipped.
func TestStoreError(t *testing.T) {
	full := storageapi.Caps{Browse: true, Write: true}
	for _, tc := range []struct {
		name    string
		caps    storageapi.Caps
		subject Subject
		wantErr bool
	}{
		{"root declared", full, Subject{Root: "/store"}, false},
		{"no filesystem root declared", full, Subject{NoFilesystemRoot: true}, false},
		{"neither declared", full, Subject{}, true},
		{"both declared", full, Subject{Root: "/store", NoFilesystemRoot: true}, true},
		{"presigner only provider", storageapi.Caps{DownloadURL: true}, Subject{}, false},
		{
			"local file without a filesystem",
			storageapi.Caps{Browse: true, Write: true, LocalFile: true},
			Subject{NoFilesystemRoot: true},
			true,
		},
		{
			"local file with a root",
			storageapi.Caps{Browse: true, Write: true, LocalFile: true},
			Subject{Root: "/store"},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := storeError(tc.caps, tc.subject)
			if tc.wantErr && err == nil {
				t.Errorf("storeError(%+v, %+v) = nil, want an error", tc.caps, tc.subject)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("storeError(%+v, %+v) = %v, want nil", tc.caps, tc.subject, err)
			}
		})
	}
}
