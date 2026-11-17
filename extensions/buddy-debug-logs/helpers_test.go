package main

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
	"github.com/prusa3d/buddy-debug-logs/internal/routes"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

// The module, driven the way the host drives it: the method set main.go
// builds, configured once with settings and grants, and then called. The
// ports are real UDP sockets on this machine, on ports the system picks.

// testMAC is the Ethernet MAC of the one printer on the fake fleet.
const testMAC = "10:9c:70:2a:bb:cd"

// rig is one configured module and the fleet it reads.
type rig struct {
	module  *module
	serves  wirerpc.Methods
	devices *hostfake.Devices
}

// newRig configures a module with a data directory and both ports.
func newRig(t *testing.T) rig {
	t.Helper()

	return configuredRig(t, wirerpc.Grants{
		Data: t.TempDir(),
		Listen: map[string]wirerpc.ListenGrant{
			listenSyslog:  {Network: "udp", Port: 0},
			listenMetrics: {Network: "udp", Port: 0},
		},
	})
}

// configuredRig configures a module with the grants given.
func configuredRig(t *testing.T, grants wirerpc.Grants) rig {
	t.Helper()

	devices := &hostfake.Devices{}
	devices.Add("mk4", "MK4 left", testMAC, "192.168.1.20", "")

	held := &module{devices: devices, now: time.Now}
	serves := held.methods()

	t.Cleanup(held.stop)

	params, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"retention_hours":24,"max_mib":128}`),
		Grants: &grants,
	})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	if _, err = serves[wirerpc.MethodConfigure](t.Context(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params}); err != nil {
		t.Fatalf("the module refused the settings the host sends: %v", err)
	}

	r := rig{module: held, serves: serves, devices: devices}

	// The module reads the fleet in the background. The first read is waited
	// for here, so a datagram sent next is matched at once.
	if book := held.current().Book; book.Sources != nil {
		if err = book.Sources.Refresh(t.Context()); err != nil {
			t.Fatalf("read the fleet: %v", err)
		}
	}

	return r
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

	return response
}

// get makes one GET as alice and decodes the answer.
func (r rig) get(t *testing.T, path string, into any) int {
	t.Helper()

	response := r.call(t, routesapi.Request{Method: "GET", Path: path, Principal: "user:alice"})

	if into != nil {
		if err := json.Unmarshal(response.Body, into); err != nil {
			t.Fatalf("GET %s answered %d %s: %v", path, response.Status, response.Body, err)
		}
	}

	return response.Status
}

// port is the port the module holds for a stream.
func (r rig) port(t *testing.T, stream store.Stream) int {
	t.Helper()

	for _, held := range routes.StatusOf(r.module.current()).Streams {
		if held.Stream == string(stream) && held.Listener.Listening {
			return held.Listener.Port
		}
	}

	t.Fatalf("the module listens on no port for %s", stream)

	return 0
}

// send writes one datagram to the stream's port from this machine.
func (r rig) send(t *testing.T, stream store.Stream, datagram string) {
	t.Helper()

	conn, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port(t, stream))))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer conn.Close()

	if _, err = conn.Write([]byte(datagram)); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// collectAll walks every page of diagnostics.collect and joins the files.
func (r rig) collectAll(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	cursor := ""

	for range 100 {
		params, err := json.Marshal(diagnosticsapi.Request{Cursor: cursor})
		if err != nil {
			t.Fatalf("render the request: %v", err)
		}

		raw, err := r.serves[diagnosticsapi.MethodCollect](t.Context(),
			wirerpc.Call{Method: diagnosticsapi.MethodCollect, Params: params})
		if err != nil {
			t.Fatalf("collect: %v", err)
		}

		page, ok := raw.(diagnosticsapi.Page)
		if !ok {
			t.Fatalf("collect answered %T", raw)
		}

		for _, file := range page.Files {
			files[file.Name] += file.Text
		}

		if page.Cursor == "" {
			return files
		}

		cursor = page.Cursor
	}

	t.Fatal("collect never answered its last page")

	return nil
}

// eventually polls check until it is true or five seconds pass. A datagram
// crosses a real socket, so its arrival is waited for.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if check() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s did not happen within five seconds", what)
}
