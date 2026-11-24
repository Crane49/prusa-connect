package main

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

type noFrames struct{}

func (noFrames) Accept(context.Context, string, string, []byte) error { return nil }

// A port still held by the process this one replaces is tried again, and the
// status says why nothing listens until it opens.
func TestATakenPortIsTriedAgain(t *testing.T) {
	listenRetry = 50 * time.Millisecond
	listenHost = "127.0.0.1"

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := held.Addr().(*net.TCPAddr).Port
	m := &module{frames: noFrames{}, log: slog.New(slog.DiscardHandler)}
	t.Cleanup(m.stop)

	err = m.configure(t.Context(), Settings{Address: "127.0.0.1"}, wirerpc.Grants{
		Data:   t.TempDir(),
		Listen: map[string]wirerpc.ListenGrant{listenCamera: {Network: "tcp", Port: port}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return strings.Contains(m.current().Problem, "tries again") })

	if m.current().Listening {
		t.Fatal("the module says it listens on a port another process holds")
	}

	_ = held.Close()

	waitFor(t, func() bool { return m.current().Listening && m.current().Problem == "" })
}

// The routes fragment is one the host accepts.
func TestTheOpenAPIFragmentIsAccepted(t *testing.T) {
	t.Parallel()

	if _, err := routesapi.Describe(
		wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract},
		openAPI,
	); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("the condition never held")
		}

		time.Sleep(10 * time.Millisecond)
	}
}
