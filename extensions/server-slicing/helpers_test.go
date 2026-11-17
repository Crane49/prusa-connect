package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/server-slicing/internal/fixture"
	"github.com/prusa3d/server-slicing/internal/hostfake"
)

// The module, driven the way the host drives it: the method set main.go
// builds, configured once with settings and grants, and then called. The
// slicer is the stand-in under testdata, named as a local binary.

// The people of the tests. alice may read and write the storage, bob may
// only read it, and root may also configure modules.
const (
	alice = "user:alice"
	bob   = "user:bob"
	root  = "user:root"
)

// rig is one configured module and the host it calls.
type rig struct {
	module *module
	serves wirerpc.Methods
	host   *hostfake.Host
}

// stand is the path of the stand-in slicer.
func stand(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in slicer is a shell script")
	}

	path, err := filepath.Abs("testdata/fake-slicer.sh")
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// newRig configures a module with a data directory and the stand-in slicer,
// and waits until the slicer is listed.
func newRig(t *testing.T) rig {
	t.Helper()

	config, err := json.Marshal(map[string]any{"local_binaries": []string{stand(t)}, "github_releases": false})
	if err != nil {
		t.Fatal(err)
	}

	r := configuredRig(t, config, wirerpc.Grants{Data: t.TempDir()})

	eventually(t, "the stand-in slicer is listed", func() bool {
		return len(r.module.current().Slicers.Installed()) == 1
	})

	return r
}

// configuredRig configures a module with the settings and the grants given.
func configuredRig(t *testing.T, config json.RawMessage, grants wirerpc.Grants) rig {
	t.Helper()

	host := hostfake.New("shelf")
	host.Grant(alice, "read_storage", "write_storage")
	host.Grant(bob, "read_storage")
	host.Grant(root, "read_storage", "write_storage", "configure_resource")
	host.Grant("", "read_storage", "write_storage")

	held := &module{files: host, access: host, log: slog.New(slog.DiscardHandler)}
	serves := held.methods()

	t.Cleanup(held.stop)

	params, err := json.Marshal(wirerpc.Settings{Config: config, Grants: &grants})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	if _, err = serves[wirerpc.MethodConfigure](t.Context(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params}); err != nil {
		t.Fatalf("the module refused the settings the host sends: %v", err)
	}

	return rig{module: held, serves: serves, host: host}
}

// call makes one routes.serve call and reads the answer back.
func (r rig) call(t *testing.T, request routesapi.Request) routesapi.Response {
	t.Helper()

	params, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("render the request: %v", err)
	}

	raw, err := r.serves[routesapi.MethodServe](context.Background(),
		wirerpc.Call{Method: routesapi.MethodServe, Params: params})
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.Path, err)
	}

	response, ok := raw.(routesapi.Response)
	if !ok {
		t.Fatalf("the module answered %T and not a routesapi.Response", raw)
	}

	if err = response.Validate(); err != nil {
		t.Fatalf("the host would refuse this answer: %v", err)
	}

	if response.Status == 0 {
		response.Status = 200
	}

	return response
}

// as makes one call as a person and decodes the answer into into.
func (r rig) as(t *testing.T, who, method, path string, body []byte, into any) int {
	t.Helper()

	response := r.call(t, routesapi.Request{Method: method, Path: path, Principal: who, Body: body})

	if into != nil && len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, into); err != nil {
			t.Fatalf("%s %s answered %d %s: %v", method, path, response.Status, response.Body, err)
		}
	}

	return response.Status
}

// eventually waits for a condition the module reaches in the background.
func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if done() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// threeMF is a project of two cubes on bed 0 whose stored entry tells the
// stand-in slicer what to do.
func threeMF(t *testing.T, mode string) []byte {
	t.Helper()

	data, err := fixture.Project{At: [][2]float64{{100, 100}, {130, 100}}, Settings: fixture.Bed200, Mode: mode}.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	return data
}
