package busapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// principalFeed is a host feed that records who each subscription was opened
// for, and refuses one principal by name.
type principalFeed struct {
	mu      sync.Mutex
	saw     []string
	refused string
}

func (f *principalFeed) Subscribe(
	_ context.Context, params busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	f.mu.Lock()
	f.saw = append(f.saw, params.Principal)
	f.mu.Unlock()

	if params.Principal == f.refused {
		return fmt.Errorf("%w: %s was never handed to this module", busapi.ErrDenied, params.Principal)
	}

	return write(busapi.Window{Cursor: "origin:1", Events: []json.RawMessage{[]byte(`{}`)}})
}

func (f *principalFeed) principals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.saw...)
}

// pipeEnd is one side of a duplex channel built from operating system pipes,
// which is the channel a host really hands a child.
type pipeEnd struct {
	reader *os.File
	writer *os.File
}

func (p pipeEnd) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p pipeEnd) Write(b []byte) (int, error) { return p.writer.Write(b) }
func (p pipeEnd) Close() error                { return errors.Join(p.reader.Close(), p.writer.Close()) }

// session serves a host method set to a child over one channel and answers the
// child's side, which is what a module subscribes through.
func session(t *testing.T, hostMethods wirerpc.Methods) *wirerpc.Conn {
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
		wirerpc.Options{Role: wirerpc.RoleHost, Methods: hostMethods})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	child, err := wirerpc.NewConn(pipeEnd{reader: childReads, writer: childWrites},
		wirerpc.Options{Role: wirerpc.RoleChild})
	if err != nil {
		t.Fatalf("build the child session: %v", err)
	}

	var running sync.WaitGroup

	running.Add(2)

	go func() {
		defer running.Done()

		_ = host.Serve(t.Context())
	}()

	go func() {
		defer running.Done()

		_ = child.Serve(t.Context())
	}()

	t.Cleanup(func() {
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return child
}

// The principal a subscription names reaches the host as it was written, and a
// subscription that names none reaches it as none. What each means is the
// host's decision; the wire carries the difference.
func TestThePrincipalReachesTheHostOnASubscription(t *testing.T) {
	t.Parallel()

	for _, principal := range []string{"user:alice", ""} {
		t.Run(principal, func(t *testing.T) {
			t.Parallel()

			feed := &principalFeed{refused: "user:mallory"}
			child := session(t, busapi.SubscribeHandler(feed))

			var windows int

			err := busapi.Subscribe(t.Context(), child, busapi.SubscribeParams{Principal: principal},
				func(busapi.Window) error {
					windows++

					return nil
				})
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}

			if windows != 1 {
				t.Fatalf("%d windows arrived, want the one the host wrote", windows)
			}

			if saw := feed.principals(); len(saw) != 1 || saw[0] != principal {
				t.Fatalf("the host saw %q, want %q", saw, principal)
			}
		})
	}
}

// A principal the host will not open a subscription as is refused with the
// sentinel a module branches on, and no window arrives.
func TestASubscriptionAsAPrincipalTheHostRefusesIsDenied(t *testing.T) {
	t.Parallel()

	feed := &principalFeed{refused: "user:mallory"}
	child := session(t, busapi.SubscribeHandler(feed))

	err := busapi.Subscribe(t.Context(), child, busapi.SubscribeParams{Principal: "user:mallory"},
		func(busapi.Window) error {
			t.Error("a window arrived on a subscription the host refused")

			return nil
		})

	if !errors.Is(err, busapi.ErrDenied) {
		t.Fatalf("subscribe = %v, want ErrDenied", err)
	}
}
