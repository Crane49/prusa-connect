package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/parts/internal/hostfake"
)

// TestNothingAPersonWroteReachesTheLog drives the module with its standard
// error read back, because the host copies every line of it into its own log.
//
// A module's log is the one place a record body leaks without anybody asking
// for it. This module writes three fixed lines there: what it is, that it
// was asked to stop, and a failure that ended the process. Beside them it
// warns about a record it skipped or left behind, by key and id, never by
// what the record holds.
//
// It does not call t.Parallel: it replaces the process's standard error while
// it runs.
func TestNothingAPersonWroteReachesTheLog(t *testing.T) {
	secret := "the customer's drawing number, which is nobody else's business"

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe to read the log back: %v", err)
	}

	held := os.Stderr
	os.Stderr = write //nolint:reassign // the log under test is the process's own standard error.

	t.Cleanup(func() { os.Stderr = held }) //nolint:reassign // put back after the run.

	serves := configured(t, &hostfake.Records{}, &hostfake.Files{})

	// One write that lands, carrying something a person typed.
	response := call(t, serves, routesapi.Request{
		Method: http.MethodPost, Path: "/parts", Principal: "user:alice",
		Body: []byte(`{"name":"Bracket","description":"` + secret + `"}`),
	})
	if response.Status != http.StatusCreated {
		t.Fatalf("adding a part answered %d: %s", response.Status, response.Body)
	}

	// One write the rules refuse, which is the path where a module is most
	// tempted to print what it was holding.
	call(t, serves, routesapi.Request{
		Method: http.MethodPost, Path: "/parts", Principal: "user:alice",
		Body: []byte(`{"name":"Hinge","folder":42,"description":"` + secret + `"}`),
	})

	// A version naming a file the storage does not hold.
	call(t, serves, routesapi.Request{
		Method: http.MethodPost, Path: "/parts/1/versions", Principal: "user:alice",
		Body: []byte(`{"files":[{"path":"parts/1/up/` + "secret-drawing.pdf" + `"}]}`),
	})

	call(t, serves, routesapi.Request{Method: http.MethodGet, Path: "/parts", Principal: "user:alice"})

	if _, err = serves[wirerpc.MethodShutdown](context.Background(),
		wirerpc.Call{Method: wirerpc.MethodShutdown}); err != nil {
		t.Fatalf("shut the module down: %v", err)
	}

	os.Stderr = held //nolint:reassign // put back before the log is read.

	if err = write.Close(); err != nil {
		t.Fatalf("close the pipe: %v", err)
	}

	logged, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("read the log back: %v", err)
	}

	said := string(logged)

	for _, leaked := range []string{secret, "Bracket", "Hinge", "secret-drawing", "user:alice", "part/"} {
		if strings.Contains(said, leaked) {
			t.Errorf("the log carries %q: %s", leaked, said)
		}
	}
}
