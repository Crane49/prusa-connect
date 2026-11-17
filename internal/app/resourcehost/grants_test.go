package resourcehost

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func grantedInstance(t *testing.T) ref.Ref {
	t.Helper()

	instance, err := ref.New(ref.KindSink, "buddy-debug-logs")
	if err != nil {
		t.Fatalf("ref: %v", err)
	}

	return instance
}

// A module granted nothing beyond the API is sent no grants, so a host that
// grants nothing sends what every host before ADR-20261019-3ns sent.
func TestAModuleGrantedNothingIsSentNoGrants(t *testing.T) {
	t.Parallel()

	grants, err := grantsOf(t.TempDir(), grantedInstance(t), &serviceSpec{})
	if err != nil || grants != nil {
		t.Fatalf("grants = %+v, err = %v, want neither", grants, err)
	}
}

// The ports and the directory reach the module, and the directory exists
// before the module starts (ADR-20261019-3ns rules 11 and 12).
func TestAModuleIsSentItsPortsAndADirectoryThatExists(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "module-data")
	spec := &serviceSpec{service: install.Service{
		Listen: map[string]install.ListenPort{"syslog": {Network: "udp", Port: 13514}},
		Data:   true,
	}}

	grants, err := grantsOf(root, grantedInstance(t), spec)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}

	if got := grants.Listen["syslog"]; got != (wirerpc.ListenGrant{Network: "udp", Port: 13514}) {
		t.Errorf("syslog grant = %+v", got)
	}

	want, _ := filepath.Abs(filepath.Join(root, "buddy-debug-logs"))
	if grants.Data != want {
		t.Errorf("data = %q, want %q", grants.Data, want)
	}

	info, err := os.Stat(grants.Data)
	if err != nil || !info.IsDir() {
		t.Fatalf("the data directory was not made: %v", err)
	}
}

// A directory the host cannot make stops the start with a code that names
// the path, rather than starting a module that cannot write.
func TestADataDirectoryTheHostCannotMakeStopsTheStart(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	spec := &serviceSpec{service: install.Service{Data: true}}

	for name, root := range map[string]string{"no root": "", "a file in the way": blocker} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := grantsOf(root, grantedInstance(t), spec)
			if !errors.Is(err, moduleDataUnavailable.New("")) {
				t.Fatalf("err = %v, want RESOURCE_MODULE_DATA_UNAVAILABLE", err)
			}
		})
	}
}

// The data directory outlives a removal. The module it was made for gets it
// back, and a different module installed under the same instance name is
// refused it (ADR-20261019-3ns rule 12).
func TestADataDirectoryGoesOnlyToTheModuleItWasMadeFor(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "module-data")
	first := &serviceSpec{typeName: "com.github.acme.logs.buddy-debug-logs", service: install.Service{Data: true}}

	grants, err := grantsOf(root, grantedInstance(t), first)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}

	if err = os.WriteFile(filepath.Join(grants.Data, "state.db"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err = grantsOf(root, grantedInstance(t), first); err != nil {
		t.Fatalf("the same module again: %v", err)
	}

	other := &serviceSpec{typeName: "com.github.other.logs.buddy-debug-logs", service: install.Service{Data: true}}

	_, err = grantsOf(root, grantedInstance(t), other)
	if !errors.Is(err, moduleDataOwned.New("")) {
		t.Fatalf("err = %v, want RESOURCE_MODULE_DATA_OWNED", err)
	}

	if !strings.Contains(err.Error(), "com.github.acme.logs.buddy-debug-logs") {
		t.Errorf("err = %v, want it to name the module the folder belongs to", err)
	}
}

// A folder that already exists is limited to the server's own account, as a
// new one is.
func TestAnExistingDataDirectoryIsLimitedToTheServer(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no group and other permission bits")
	}

	root := filepath.Join(t.TempDir(), "module-data")
	dir := filepath.Join(root, "buddy-debug-logs")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := grantsOf(root, grantedInstance(t), &serviceSpec{service: install.Service{Data: true}}); err != nil {
		t.Fatalf("grants: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if info.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want 0750", info.Mode().Perm())
	}
}

// A folder an operator made narrower than 0750 keeps its mode: the host only
// takes access away.
func TestANarrowerDataDirectoryKeepsItsMode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no group and other permission bits")
	}

	root := filepath.Join(t.TempDir(), "module-data")
	dir := filepath.Join(root, "buddy-debug-logs")

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := grantsOf(root, grantedInstance(t), &serviceSpec{service: install.Service{Data: true}}); err != nil {
		t.Fatalf("grants: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", info.Mode().Perm())
	}
}

// A data directory or an owner file that is a link is refused, so a module
// never writes where the folder name does not say.
func TestALinkedDataDirectoryOrOwnerFileIsRefused(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("making a link needs a privilege on Windows")
	}

	spec := &serviceSpec{typeName: "com.github.acme.logs.buddy-debug-logs", service: install.Service{Data: true}}

	t.Run("the directory", func(t *testing.T) {
		t.Parallel()

		root := filepath.Join(t.TempDir(), "module-data")
		elsewhere := t.TempDir()

		if err := os.MkdirAll(root, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.Symlink(elsewhere, filepath.Join(root, "buddy-debug-logs")); err != nil {
			t.Fatalf("link: %v", err)
		}

		if _, err := grantsOf(root, grantedInstance(t), spec); !errors.Is(err, moduleDataUnavailable.New("")) {
			t.Fatalf("err = %v, want RESOURCE_MODULE_DATA_UNAVAILABLE", err)
		}
	})

	t.Run("the owner file", func(t *testing.T) {
		t.Parallel()

		root := filepath.Join(t.TempDir(), "module-data")
		dir := filepath.Join(root, "buddy-debug-logs")
		target := filepath.Join(t.TempDir(), "written-here")

		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.Symlink(target, filepath.Join(dir, ownerFile)); err != nil {
			t.Fatalf("link: %v", err)
		}

		if _, err := grantsOf(root, grantedInstance(t), spec); err == nil {
			t.Fatal("a linked owner file was followed")
		}

		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the link's target was written: %v", err)
		}
	})
}

// A SQLite database is a file in the module's data directory, which the host
// makes even for a module that asked for no directory, and the module is not
// told the directory it did not ask for (ADR-20261021-bqc rule 16).
func TestAModuleIsSentTheFileOfItsSQLiteDatabase(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "module-data")
	spec := &serviceSpec{service: install.Service{Database: &install.Database{Engine: wirerpc.DatabaseSQLite}}}

	grants, err := grantsOf(root, grantedInstance(t), spec)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}

	dir, _ := filepath.Abs(filepath.Join(root, "buddy-debug-logs"))
	want := wirerpc.DatabaseGrant{Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(dir, "database.sqlite")}

	if grants.Database == nil || *grants.Database != want {
		t.Fatalf("database = %+v, want %+v", grants.Database, want)
	}

	if grants.Data != "" {
		t.Errorf("data = %q, and the module asked for no directory", grants.Data)
	}

	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		t.Fatalf("the directory for the database was not made: %v", statErr)
	}
}

// A Postgres database reaches the module as the connection string the
// declaration holds, and the host makes no directory for it.
func TestAModuleIsSentTheConnectionStringOfItsPostgresDatabase(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "module-data")
	dsn := "postgres://module_notes:s3cret@db.lan:5432/module_notes"
	spec := &serviceSpec{service: install.Service{
		Database: &install.Database{Engine: wirerpc.DatabasePostgres, DSN: dsn, Provisioned: true},
	}}

	grants, err := grantsOf(root, grantedInstance(t), spec)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}

	if grants.Database == nil ||
		*grants.Database != (wirerpc.DatabaseGrant{Engine: wirerpc.DatabasePostgres, DSN: dsn}) {
		t.Fatalf("database = %+v", grants.Database)
	}

	if _, statErr := os.Stat(root); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a directory was made for a Postgres database: %v", statErr)
	}
}

// The doors a module calls are told where its data directory is, the same
// path the module is sent, and an instance granted none is told nothing.
func TestTheDoorsKnowTheDataDirectoryAModuleWasGranted(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "module-data")

	for name, granted := range map[string]bool{"granted": true, "not granted": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			declared := declaredAs(t, ref.KindSink, "queue", "com.github.acme.queue/queue",
				install.RuntimeService)
			declared.Spec.Service = &install.Service{ContractMajor: 1, Data: granted}

			var handed Module

			host := supervisingDeclarations(declared)
			host.cfg.ModuleData = root
			host.cfg.Fleet = func(module Module) deviceapi.Fleet {
				handed = module

				return nil
			}

			if _, err := host.servesTo(declared); err != nil {
				t.Fatalf("serves: %v", err)
			}

			want := Module{Instance: declared.Ref}
			if granted {
				want.Data, _ = filepath.Abs(filepath.Join(root, "queue"))
			}

			if !reflect.DeepEqual(handed, want) {
				t.Fatalf("the fleet was handed %+v, want %+v", handed, want)
			}
		})
	}
}
