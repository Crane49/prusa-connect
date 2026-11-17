package wirerpc

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// duplexPipe is one end of a channel built from two operating system pipes.
type duplexPipe struct {
	reader *os.File
	writer *os.File
}

func (d duplexPipe) Read(p []byte) (int, error)  { return d.reader.Read(p) }
func (d duplexPipe) Write(p []byte) (int, error) { return d.writer.Write(p) }

func (d duplexPipe) Close() error {
	return errors.Join(d.reader.Close(), d.writer.Close())
}

// TestTheStreamTableDoesNotGrowWithTheCallsAnswered is the gate on the one
// piece of state a long connection accumulates.
//
// A side that answers a call registers the stream it arrived on. Holding that
// entry after both sides have ended the stream costs one entry per call, for
// as long as the connection lives, so a child answering once a second leaks all
// day. The table is read from inside a handler, which is a moment at which the
// read loop has already dispatched every earlier frame.
func TestTheStreamTableDoesNotGrowWithTheCallsAnswered(t *testing.T) {
	t.Parallel()

	const (
		calls = 200
		// A few streams may still be in flight: a handler that has written its
		// answer releases its stream on its own goroutine, which is not ordered
		// against the read loop. What this refuses is a table that grows with
		// the calls.
		settled = 8
	)

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	held := make(chan int, 1)

	host, err := NewConn(duplexPipe{reader: hostReads, writer: hostWrites}, Options{Role: RoleHost})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	var child *Conn

	child, err = NewConn(duplexPipe{reader: childReads, writer: childWrites}, Options{
		Role: RoleChild,
		Methods: Methods{
			MethodInfo: func(_ context.Context, _ Call) (any, error) {
				child.mu.Lock()
				open := len(child.streams)
				child.mu.Unlock()

				select {
				case held <- open:
				default:
				}

				return Info{Name: "acme", Version: "1.0.0", Contract: "1.0.0"}, nil
			},
		},
	})
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

	for at := range calls {
		if _, called := host.Call(t.Context(), MethodInfo, nil); called != nil {
			t.Fatalf("call %d: %v", at, called)
		}

		<-held
	}

	// One more call, so a handler reads the table after every earlier stream
	// has been ended by both sides.
	if _, called := host.Call(t.Context(), MethodInfo, nil); called != nil {
		t.Fatalf("the last call: %v", called)
	}

	if open := <-held; open > settled {
		t.Errorf("the child holds %d streams after %d calls, and it settles at %d or fewer",
			open, calls, settled)
	}
}

// unread is a channel nobody reads from: a write on it blocks until the channel
// is closed, which is what a peer whose process has stopped reading its pipe
// looks like from this side.
//
// It blocks on a channel receive rather than on a real pipe, so the whole case
// runs inside a synctest bubble. A goroutine parked in a write syscall is not
// durably blocked, and the bubble's clock would never move.
type unread struct {
	release chan struct{}
	once    sync.Once
}

func (u *unread) Read(_ []byte) (int, error) {
	<-u.release

	return 0, io.EOF
}

func (u *unread) Write(_ []byte) (int, error) {
	<-u.release

	return 0, io.ErrClosedPipe
}

func (u *unread) Close() error {
	u.once.Do(func() { close(u.release) })

	return nil
}

// TestAPeerThatStopsReadingHasItsConnectionClosed is the gate on the one wait in
// this package that no context reaches.
//
// A write holds writeMu while it blocks in the operating system, so a peer that
// stops reading its pipe stops every writer on this side, for as long as that
// peer's process lives. The watchdog closes the channel, which is what returns
// the blocked write its error.
func TestAPeerThatStopsReadingHasItsConnectionClosed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		conn, err := NewConn(&unread{release: make(chan struct{})}, Options{Role: RoleHost})
		if err != nil {
			t.Fatalf("build the session: %v", err)
		}

		stuck := make(chan error, 1)

		go func() {
			stuck <- conn.write(wire.Control(1, []byte("{}")))
		}()

		// A second short of the bound, so the case measures the bound rather
		// than a close that happened for some other reason.
		time.Sleep(writeBound - time.Second)
		synctest.Wait()

		select {
		case <-conn.Done():
			t.Fatalf("the session stopped before %s, so the write was not what ended it", writeBound)
		default:
		}

		time.Sleep(2 * time.Second)
		synctest.Wait()

		select {
		case <-conn.Done():
		default:
			t.Fatalf("the write has been stuck for over %s and the session is still open", writeBound)
		}

		if failed := <-stuck; failed == nil {
			t.Error("the stuck write returned no error, so a caller waiting on it was never released")
		}
	})
}
