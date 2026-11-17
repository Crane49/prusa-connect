package join

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestTheFileStoreOutlivesTheParent covers fractal.md, Admission and
// Revocation: a Parent MUST save the record of every node it has admitted,
// also of a revoked node, and MUST load the records before it answers join
// requests. The second Parent in the test reads the file the first one wrote.
func TestTheFileStoreOutlivesTheParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admissions.json")
	first := newFixture(t, func(cfg *ParentConfig) { cfg.Store = NewFileAdmissionStore(path) })
	first.mustGrant(t, request("printer-1", "child-key"))
	first.mustGrant(t, request("printer-2", "second-key"))
	if err := first.parent.MarkConnected(t.Context(), "printer-2"); err != nil {
		t.Fatalf("mark connected: %v", err)
	}
	if err := first.parent.Revoke(t.Context(), "printer-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	second := newFixture(t, func(cfg *ParentConfig) {
		cfg.Store = NewFileAdmissionStore(path)
		cfg.Authorizer = refusingAuthorizer()
	})
	t.Run("the revoked node is still denied", func(t *testing.T) {
		second.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)
	})
	t.Run("an admitted node is granted with no authorization check", func(t *testing.T) {
		second.mustGrant(t, request("printer-2", "second-key"))
	})
	t.Run("EverConnected is still set", func(t *testing.T) {
		a, ok := second.parent.Admission("printer-2")
		if !ok || !a.EverConnected {
			t.Errorf("the admission is %+v, ok %v", a, ok)
		}
	})
	t.Run("a node with no admission needs a join credential", func(t *testing.T) {
		second.mustDeny(t, request("printer-3", "third-key"), fractal.CodeJoinUnauthorized)
	})
}

// TestTheFileStoreRefusesAFileItCannotRead checks that FileAdmissionStore
// returns ErrCorruptStore, and no records, for a file it cannot use. With an
// empty result a Parent would admit a revoked node again.
func TestTheFileStoreRefusesAFileItCannotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"not json":       "{ this is not json",
		"another format": `{"format":99,"admissions":[]}`,
		"a bad node id":  `{"format":1,"admissions":[{"nodeId":"-bad","identity":"k"}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "admissions.json")
			if err := os.WriteFile(path, []byte(content), storeMode); err != nil {
				t.Fatalf("write the file: %v", err)
			}
			records, err := NewFileAdmissionStore(path).LoadAdmissions(t.Context())
			if !errors.Is(err, ErrCorruptStore) {
				t.Fatalf("the error is %v, wanted %v", err, ErrCorruptStore)
			}
			if records != nil {
				t.Errorf("the store returned %d records together with the error", len(records))
			}
		})
	}

	t.Run("NewParent fails on such a file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "admissions.json")
		if err := os.WriteFile(path, []byte("{"), storeMode); err != nil {
			t.Fatalf("write the file: %v", err)
		}
		_, err := NewParent(t.Context(), ParentConfig{
			Identity: "parent-key", Path: parentPath, Store: NewFileAdmissionStore(path),
			Verifier: jsonVerifier{}, Authorizer: TOFU{}, Issuer: &countingIssuer{},
		})
		if !errors.Is(err, ErrCorruptStore) {
			t.Fatalf("NewParent returned %v, wanted %v", err, ErrCorruptStore)
		}
	})

	t.Run("a file that does not exist is an empty store", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "nothing-here.json")
		records, err := NewFileAdmissionStore(path).LoadAdmissions(t.Context())
		if err != nil || len(records) != 0 {
			t.Errorf("the store returned %d records and %v", len(records), err)
		}
	})
}

// TestTheMembershipFileRoundTrips covers fractal.md, After the Join Exchange:
// the joining node saves the granted Node Path, the identity of the node that
// granted it, the credentials and the endpoint it joined through. A new
// FileMembershipStore on the same file, as after a restart, loads them again.
func TestTheMembershipFileRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "membership.json")
	want := Membership{
		NodePath:       "hq/brno/printer-1",
		ParentIdentity: "parent-key",
		Credentials:    "leaf-jwt",
		ExpiresAt:      fractal.TimestampOf(time.Unix(1700003600, 0)),
		Endpoint:       "tls://brno:4222",
		Binding:        json.RawMessage(`{"maxHeartbeatInterval":5000}`),
		Extensions:     map[fractal.Identifier]json.RawMessage{"files": json.RawMessage(`{"version":1}`)},
	}
	store := NewFileMembershipStore(path)

	t.Run("LoadMembership returns ok false before the first save", func(t *testing.T) {
		_, ok, err := store.LoadMembership(t.Context())
		if err != nil || ok {
			t.Errorf("the store returned ok %v and %v", ok, err)
		}
	})
	t.Run("a second store loads the saved membership", func(t *testing.T) {
		if err := store.SaveMembership(t.Context(), want); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok, err := NewFileMembershipStore(path).LoadMembership(t.Context())
		if err != nil || !ok {
			t.Fatalf("read: ok %v, %v", ok, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the loaded membership is %+v, wanted %+v", got, want)
		}
	})
	t.Run("a file with a node path that is not valid is an error", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "membership.json")
		content := []byte(`{"format":1,"membership":{"nodePath":"hq//brno"}}`)
		if err := os.WriteFile(bad, content, storeMode); err != nil {
			t.Fatalf("write the file: %v", err)
		}
		_, ok, err := NewFileMembershipStore(bad).LoadMembership(t.Context())
		if !errors.Is(err, ErrCorruptStore) || ok {
			t.Errorf("the store returned ok %v and %v", ok, err)
		}
	})
}

// TestTheWriteIsAllOrNothing covers writeWholeFile, which both file stores
// use. It writes a temporary file in the same directory, syncs it and renames
// it. When the rename fails, the old content is still at the path and the
// temporary file is removed.
func TestTheWriteIsAllOrNothing(t *testing.T) {
	t.Parallel()

	t.Run("a successful write leaves only the store file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "admissions.json")
		store := NewFileAdmissionStore(path)
		for _, id := range []fractal.NodeID{"printer-1", "printer-2"} {
			if err := store.SaveAdmission(t.Context(), Admission{NodeID: id, Identity: "k"}); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read the directory: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "admissions.json" {
			t.Errorf("the directory has %d entries, wanted only admissions.json", len(entries))
		}
	})

	t.Run("a rename that fails leaves the old content", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// The path is a directory that is not empty, so the rename fails after
		// the temporary file is written.
		path := filepath.Join(dir, "admissions.json")
		if err := os.MkdirAll(filepath.Join(path, "inside"), 0o700); err != nil {
			t.Fatalf("make the directory: %v", err)
		}
		if err := writeWholeFile(path, []byte("new")); err == nil {
			t.Fatal("writeWholeFile returned no error")
		}
		if _, err := os.Stat(filepath.Join(path, "inside")); err != nil {
			t.Errorf("the directory that was at the path is gone: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read the directory: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("the directory has %d entries, so a temporary file was left", len(entries))
		}
	})

	t.Run("only the owner may read the store file", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("os.Stat on windows does not report unix permission bits")
		}
		path := filepath.Join(t.TempDir(), "admissions.json")
		if err := writeWholeFile(path, []byte("{}")); err != nil {
			t.Fatalf("write: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("read the permission: %v", err)
		}
		if info.Mode().Perm() != storeMode {
			t.Errorf("the permission is %v, wanted %v", info.Mode().Perm(), os.FileMode(storeMode))
		}
	})
}

// TestTheFileStoreKeepsMemoryAndDiskTogether checks FileAdmissionStore after a
// write that failed. The record of that write is not in the file, so
// LoadAdmissions must not return it from memory.
func TestTheFileStoreKeepsMemoryAndDiskTogether(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "admissions.json")
	store := NewFileAdmissionStore(path)
	if err := store.SaveAdmission(t.Context(), Admission{NodeID: "printer-1", Identity: "k"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The test puts a directory that is not empty at the path, so the rename
	// of the next write fails.
	if err := os.Remove(path); err != nil {
		t.Fatalf("clear the path: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	if err := store.SaveAdmission(t.Context(), Admission{NodeID: "printer-2", Identity: "k"}); err == nil {
		t.Fatal("SaveAdmission returned no error")
	}
	records, err := store.LoadAdmissions(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 1 || records[0].NodeID != "printer-1" {
		t.Errorf("the store returned %+v after a write that failed, wanted only printer-1", records)
	}
}
