package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// bothEnds connects a host connection and a module connection through two
// pipes. The host connection serves until the test ends. The returned function
// closes the host connection.
func bothEnds(t *testing.T, methods wirerpc.Methods) (child *wirerpc.Conn, closeHost func()) {
	t.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	host, err := wirerpc.NewConn(pipeEnd{reader: hostReads, writer: hostWrites},
		wirerpc.Options{Role: wirerpc.RoleHost})
	if err != nil {
		t.Fatalf("build the host connection: %v", err)
	}

	child, err = wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild, Methods: methods})
	if err != nil {
		t.Fatalf("build the module connection: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Go(func() { _ = host.Serve(ctx) })

	t.Cleanup(func() {
		cancel()
		_ = host.Close()
		running.Wait()
	})

	return child, func() { _ = host.Close() }
}

// TestTheModuleServesUntilTheHostClosesTheChannel: together runs the node and
// the connection to the host, and returns when the host closes the connection.
func TestTheModuleServesUntilTheHostClosesTheChannel(t *testing.T) {
	t.Parallel()

	state := newLink()
	settings := make(chan Config, 1)
	child, closeHost := bothEnds(t, methods(state, &tree{}, settings))

	done := make(chan error, 1)

	go func() {
		done <- together(t.Context(), child, state, &tree{}, slog.New(slog.DiscardHandler), settings)
	}()

	// This host sends no plugin.configure, so the module still waits for its
	// settings.
	held, err := state.Status(t.Context())
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}

	if held.NodePath != "" {
		t.Fatalf("the status has a Node Path before the settings arrived: %+v", held)
	}

	closeHost()

	select {
	case <-done:
	case <-time.After(crossWait):
		t.Fatal("together did not return after the host closed the connection")
	}
}

// TestAModuleWhoseHostServesNoEventStreamSaysSo: when the host refuses the
// subscription, watch returns the error and does not subscribe again in a
// loop.
func TestAModuleWhoseHostServesNoEventStreamSaysSo(t *testing.T) {
	t.Parallel()

	child, _ := bothEnds(t, wirerpc.Methods{})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = child.Serve(ctx) }()

	if err := watch(ctx, child, nil); err == nil {
		t.Fatal("watch returned nil, and the host serves no event subscription")
	}
}

// TestARefusalThisContractDoesNotNumberCarriesACode: refusal gives every error
// of the transport a registered code and keeps the transport's error as the
// cause, so this module's log still names what went wrong.
func TestARefusalThisContractDoesNotNumberCarriesACode(t *testing.T) {
	t.Parallel()

	own := errors.New("the transport could not write the record")

	held := refusal("bench", own)
	if !errors.Is(held, own) {
		t.Fatalf("refusal returned %v, want the error of the transport as its cause", held)
	}

	payload, carried := linkapi.FaultOf(held)
	if !carried {
		t.Fatalf("refusal returned %v, and it carries no registered fault", held)
	}

	if payload.Code != DecisionRefused.Code() {
		t.Errorf("the code is %s, want %s", payload.Code, DecisionRefused.Code())
	}
}

// TestANodeTheLinkDoesNotHoldIsItsOwnCode: refusal gives an id the transport
// has no record of a code of its own, and keeps linkapi.ErrNoNode as the cause
// so the host still answers the request as a node that is not there.
func TestANodeTheLinkDoesNotHoldIsItsOwnCode(t *testing.T) {
	t.Parallel()

	held := refusal("bench", natsbind.ErrUnknownNode)

	if !errors.Is(held, linkapi.ErrNoNode) {
		t.Fatalf("refusal returned %v, want linkapi.ErrNoNode as its cause", held)
	}

	payload, carried := linkapi.FaultOf(held)
	if !carried || payload.Code != NodeUnknown.Code() {
		t.Fatalf("refusal returned %v, want a fault with %s", held, NodeUnknown.Code())
	}
}

// TestADecisionThatFailsForAnotherReasonReachesTheHost: when the transport
// fails a Revoke, link.Revoke returns that error to the host.
func TestADecisionThatFailsForAnotherReasonReachesTheHost(t *testing.T) {
	t.Parallel()

	own := errors.New("the transport could not write the record")
	state := newLink()
	state.serve(&decisions{refuse: own}, newCounters())

	if err := state.Revoke(t.Context(), linkapi.NodeParams{ID: "bench"}); !errors.Is(err, own) {
		t.Fatalf("Revoke returned %v, want the error of the transport", err)
	}
}

// TestAShadowSelectionOutsideItsGrammarIsRefused: ParseConfig refuses a shadow
// interface with an empty node path.
func TestAShadowSelectionOutsideItsGrammarIsRefused(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig([]byte(`{
		"node_id":"hq","data_dir":"/d","dedup_window":"1m",
		"shadow":{"interfaces":[{"node":"","interface":"Printer1","instance":"1"}]}
	}`))
	if err == nil {
		t.Fatal("ParseConfig accepted a shadow interface with an empty node path")
	}
}

// TestTheReadingOfThisProcessIsWhatTheManifestSays: the name and the version
// this process returns from plugin.info are the ones in module.yaml. The host
// compares them.
func TestTheReadingOfThisProcessIsWhatTheManifestSays(t *testing.T) {
	t.Parallel()

	if name != "fractal" {
		t.Errorf("the name is %q, want fractal as in module.yaml", name)
	}

	if release != "1.0.0" {
		t.Errorf("the version is %q, want 1.0.0 as in module.yaml", release)
	}
}

// TestALengthOfTimeOnlyTheBridgeReadsStopsTheNode: ParseConfig parses
// flush_interval and resync_interval too, although only the bridge reads them.
// It returns ErrBadDuration for a value that is not a duration, so the module
// stops at the start and does not run with a default the operator did not
// write.
func TestALengthOfTimeOnlyTheBridgeReadsStopsTheNode(t *testing.T) {
	t.Parallel()

	for name, field := range map[string]string{
		"the flush interval":  "flush_interval",
		"the resync interval": "resync_interval",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseConfig(fmt.Appendf(nil,
				`{"node_id":"hq","data_dir":%q,"dedup_window":"1m",%q:"now and then"}`,
				t.TempDir(), field))
			if !errors.Is(err, ErrBadDuration) {
				t.Fatalf("ParseConfig returned %v, want ErrBadDuration", err)
			}
		})
	}
}

// TestAnOperatorCanMakeEveryCallRunAsThisModule: with command_principal "self",
// principalOf returns the empty principal. The host runs such a command as
// this module, inside the permissions the operator approved.
func TestAnOperatorCanMakeEveryCallRunAsThisModule(t *testing.T) {
	t.Parallel()

	if held := principalOf(Config{CommandPrincipal: PrincipalSelf}, nil)(); held != "" {
		t.Fatalf("the principal is %q, want the empty principal", held)
	}
}
