package wirerpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The two sides of one channel, built from operating system pipes rather than
// from net.Pipe. A pipe with no buffer deadlocks the moment both read loops
// write at once, and the channel a host really hands a child is a pipe pair
// with a buffer in the kernel.
type channel struct {
	reader *os.File
	writer *os.File
}

func (c channel) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c channel) Write(p []byte) (int, error) { return c.writer.Write(p) }

func (c channel) Close() error {
	return errors.Join(c.reader.Close(), c.writer.Close())
}

// pipePair returns the two ends of one duplex channel.
func pipePair(t *testing.T) (channel, channel) {
	t.Helper()

	hostReads, childWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	childReads, hostWrites, err := os.Pipe()
	if err != nil {
		t.Fatalf("open a pipe: %v", err)
	}

	return channel{reader: hostReads, writer: hostWrites},
		channel{reader: childReads, writer: childWrites}
}

// session is a pair of connections serving each other over one channel.
type session struct {
	host  *wirerpc.Conn
	child *wirerpc.Conn
}

// connect builds both sides, starts both read loops, and stops them when the
// test ends.
func connect(t *testing.T, hostMethods, childMethods wirerpc.Methods, window uint32) session {
	t.Helper()

	hostSide, childSide := pipePair(t)

	host, err := wirerpc.NewConn(hostSide, wirerpc.Options{
		Role: wirerpc.RoleHost, Methods: hostMethods, Window: window,
	})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	child, err := wirerpc.NewConn(childSide, wirerpc.Options{
		Role: wirerpc.RoleChild, Methods: childMethods, Window: window,
	})
	if err != nil {
		t.Fatalf("build the child session: %v", err)
	}

	ctx := t.Context()

	var running sync.WaitGroup

	running.Add(2)

	go func() {
		defer running.Done()

		_ = host.Serve(ctx)
	}()

	go func() {
		defer running.Done()

		_ = child.Serve(ctx)
	}()

	t.Cleanup(func() {
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return session{host: host, child: child}
}

// TestAChildAnswersACallTheHostMakes is the smallest thing a method set has to
// do: a name reaches the other side, and what the handler returned comes back.
func TestAChildAnswersACallTheHostMakes(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{
		wirerpc.MethodInfo: func(_ context.Context, _ wirerpc.Call) (any, error) {
			return wirerpc.Info{Name: "acme", Version: "1.0.0", Contract: "1.2.0"}, nil
		},
	}, 0)

	answer, err := pair.host.Call(t.Context(), wirerpc.MethodInfo, nil)
	if err != nil {
		t.Fatalf("call %s: %v", wirerpc.MethodInfo, err)
	}

	var info wirerpc.Info
	if read := json.Unmarshal(answer, &info); read != nil {
		t.Fatalf("read the answer: %v", read)
	}

	if invalid := info.Validate(); invalid != nil {
		t.Fatalf("the answer is not a usable info: %v", invalid)
	}

	if info.Name != "acme" {
		t.Errorf("the child reported the name %q, and its handler returned acme", info.Name)
	}
}

// TestAMethodNoSideServesIsRefusedWithItsCode holds the error path. A caller
// separates "this child cannot do that" from "the call failed" by the code.
func TestAMethodNoSideServesIsRefusedWithItsCode(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{}, 0)

	_, err := pair.host.Call(t.Context(), "blob.read", nil)

	var failure *wirerpc.Error
	if !errors.As(err, &failure) {
		t.Fatalf("calling an unserved method returned %v, and it reports a JSON-RPC error", err)
	}

	if failure.Code != wirerpc.CodeMethodNotFound {
		t.Errorf("the refusal carries code %d, and an unserved method is %d",
			failure.Code, wirerpc.CodeMethodNotFound)
	}
}

// TestAHandlerErrorReachesTheCallerWithTheCodeItChose holds the other half:
// an *Error a handler returns crosses unchanged.
func TestAHandlerErrorReachesTheCallerWithTheCodeItChose(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{
		"blob.read": func(_ context.Context, _ wirerpc.Call) (any, error) {
			return nil, &wirerpc.Error{Code: wirerpc.CodeInvalidParams, Message: "bytes is required"}
		},
	}, 0)

	_, err := pair.host.Call(t.Context(), "blob.read", nil)

	var failure *wirerpc.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the handler's error arrived as %v, and it reports a JSON-RPC error", err)
	}

	if failure.Code != wirerpc.CodeInvalidParams || failure.Message != "bytes is required" {
		t.Errorf("the refusal is %d %q, and the handler chose %d %q",
			failure.Code, failure.Message, wirerpc.CodeInvalidParams, "bytes is required")
	}
}

// TestAHandlerThatPanicsDoesNotTakeTheSessionWithIt holds ADR-20260824-nyw rule 3. A
// handler is code this side did not write, and one bad call answers with a
// failure rather than ending the connection.
func TestAHandlerThatPanicsDoesNotTakeTheSessionWithIt(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{
		"blob.read": func(_ context.Context, _ wirerpc.Call) (any, error) {
			panic("the handler is not ready")
		},
		wirerpc.MethodInfo: func(_ context.Context, _ wirerpc.Call) (any, error) {
			return wirerpc.Info{Name: "acme", Version: "1.0.0", Contract: "1.0.0"}, nil
		},
	}, 0)

	_, err := pair.host.Call(t.Context(), "blob.read", nil)

	var failure *wirerpc.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a panicking handler answered %v, and it reports a JSON-RPC error", err)
	}

	if !strings.Contains(failure.Message, "the handler is not ready") {
		t.Errorf("the refusal reads %q, and the panic said the handler is not ready", failure.Message)
	}

	// The session is the thing being tested: the next call still works.
	if _, again := pair.host.Call(t.Context(), wirerpc.MethodInfo, nil); again != nil {
		t.Errorf("the call after the panic failed, so the session did not survive it: %v", again)
	}
}

// blobRequest is what the streaming method takes.
type blobRequest struct {
	Bytes int `json:"bytes"`
	Chunk int `json:"chunk"`
}

// starved is what a writer with no credit left says before it waits.
type starved struct {
	Sent int `json:"sent"`
}

// TestAReaderThatStopsReadingStopsTheWriter is the credit gate. The host
// grants one window and reads nothing, and the sender stops after exactly that
// many bytes rather than filling a queue nobody bounds.
//
// Nothing here waits on a clock. The sender says it has stopped, on a control
// frame that costs no credit, and the count it reports is what the host
// granted.
func TestAReaderThatStopsReadingStopsTheWriter(t *testing.T) {
	t.Parallel()

	const (
		window = 64
		total  = 4096
		chunk  = 16
	)

	stopped := make(chan starved, total/chunk)

	pair := connect(t, wirerpc.Methods{
		"blob.starved": func(_ context.Context, call wirerpc.Call) (any, error) {
			var said starved
			if err := json.Unmarshal(call.Params, &said); err != nil {
				return nil, err
			}

			// The sender says this every time it runs out, and the first one
			// is what the gate reads. A full channel is dropped rather than
			// blocking the handler that reports it.
			select {
			case stopped <- said:
			default:
			}

			return struct{}{}, nil
		},
	}, wirerpc.Methods{
		"blob.read": writeBlob,
	}, window)

	stream, err := pair.host.Open()
	if err != nil {
		t.Fatalf("open a stream: %v", err)
	}

	if asked := stream.Notify("blob.read", blobRequest{Bytes: total, Chunk: chunk}); asked != nil {
		t.Fatalf("ask for the blob: %v", asked)
	}

	select {
	case first := <-stopped:
		if first.Sent != window {
			t.Errorf("the sender stopped after %d bytes, and the host granted %d", first.Sent, window)
		}
	case <-pair.host.Done():
		t.Fatalf("the session stopped before the sender said it had: %v", pair.host.Err())
	}

	read, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read the blob: %v", err)
	}

	if len(read) != total {
		t.Fatalf("the blob is %d bytes, and %d were asked for", len(read), total)
	}

	for at, got := range read {
		if want := byte(at % 251); got != want {
			t.Fatalf("byte %d is %d, and the sender wrote %d", at, got, want)
		}
	}
}

// writeBlob is the child's side of the credit gate. It writes what it was
// asked for, chunk by chunk, and says so before it waits for credit it does
// not hold.
func writeBlob(_ context.Context, call wirerpc.Call) (any, error) {
	var asked blobRequest
	if err := json.Unmarshal(call.Params, &asked); err != nil {
		return nil, err
	}

	blob := make([]byte, asked.Bytes)
	for at := range blob {
		blob[at] = byte(at % 251)
	}

	for sent := 0; sent < len(blob); {
		if call.Stream.Credit() == 0 {
			if err := call.Stream.Notify("blob.starved", starved{Sent: sent}); err != nil {
				return nil, err
			}
		}

		wrote, err := call.Stream.Write(blob[sent:min(sent+asked.Chunk, len(blob))])
		if err != nil {
			return nil, err
		}

		sent += wrote
	}

	return nil, call.Stream.CloseWrite()
}

// TestASenderThatSpendsCreditItWasNotGrantedIsRefused holds the receiving half
// of the same rule. A peer that ignores the balance is stopped, because the
// alternative is a receiver that buffers whatever arrives.
func TestASenderThatSpendsCreditItWasNotGrantedIsRefused(t *testing.T) {
	t.Parallel()

	const window = 64

	hostSide, peerSide := pipePair(t)

	host, err := wirerpc.NewConn(hostSide, wirerpc.Options{
		Role:   wirerpc.RoleHost,
		Window: window,
		Methods: wirerpc.Methods{
			"blob.read": func(_ context.Context, _ wirerpc.Call) (any, error) { return struct{}{}, nil },
		},
	})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	stopped := make(chan error, 1)

	go func() { stopped <- host.Serve(t.Context()) }()

	t.Cleanup(func() { _ = host.Close() })

	// A peer that frames its own bytes, so that it can break the rule the
	// session's own writer refuses to break.
	writer := wire.NewWriter(peerSide, 0)

	opening, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "blob.read"})
	if err != nil {
		t.Fatalf("encode the opening call: %v", err)
	}

	if opened := writer.Write(wire.Control(2, opening)); opened != nil {
		t.Fatalf("open the stream: %v", opened)
	}

	if spent := writer.Write(wire.Data(2, make([]byte, window*2))); spent != nil {
		t.Fatalf("write past the grant: %v", spent)
	}

	if ended := <-stopped; !errors.Is(ended, wirerpc.ErrCreditOverspent) {
		t.Errorf("the session stopped with %v, and a sender past its balance is %v",
			ended, wirerpc.ErrCreditOverspent)
	}
}

// TestAStreamThePeerOpensWithoutACallIsRefused holds the session's own rule: a
// stream carries a method call, so the frame that opens one names the method.
func TestAStreamThePeerOpensWithoutACallIsRefused(t *testing.T) {
	t.Parallel()

	hostSide, peerSide := pipePair(t)

	host, err := wirerpc.NewConn(hostSide, wirerpc.Options{Role: wirerpc.RoleHost})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	stopped := make(chan error, 1)

	go func() { stopped <- host.Serve(t.Context()) }()

	t.Cleanup(func() { _ = host.Close() })

	if opened := wire.NewWriter(peerSide, 0).Write(wire.Data(2, []byte("hello"))); opened != nil {
		t.Fatalf("open a stream with data: %v", opened)
	}

	if ended := <-stopped; !errors.Is(ended, wirerpc.ErrStreamOpening) {
		t.Errorf("the session stopped with %v, and a stream opened with data is %v",
			ended, wirerpc.ErrStreamOpening)
	}
}

// TestAConnectionWithNoRoleIsRefused holds the one mandatory option. Two sides
// that allocate from the same half collide on their first stream.
func TestAConnectionWithNoRoleIsRefused(t *testing.T) {
	t.Parallel()

	side, _ := pipePair(t)

	if _, err := wirerpc.NewConn(side, wirerpc.Options{}); !errors.Is(err, wirerpc.ErrRole) {
		t.Errorf("a session with no role was built with %v, and it is refused with %v", err, wirerpc.ErrRole)
	}
}

// TestEveryConnAnswersPing holds that a child with no methods at all still
// answers the liveness probe, with JSON null.
func TestEveryConnAnswersPing(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{}, 0)

	answer, err := pair.host.Call(t.Context(), wirerpc.MethodPing, nil)
	if err != nil {
		t.Fatalf("call %s: %v", wirerpc.MethodPing, err)
	}

	if string(answer) != "null" {
		t.Errorf("%s answered %s, and it answers null", wirerpc.MethodPing, answer)
	}
}

// TestPingIsAnsweredWhileAHandlerBlocks holds that a stuck handler does not
// stop the connection answering a ping: each request has its own goroutine.
func TestPingIsAnsweredWhileAHandlerBlocks(t *testing.T) {
	t.Parallel()

	never := make(chan struct{})

	pair := connect(t, nil, wirerpc.Methods{
		"block": func(ctx context.Context, _ wirerpc.Call) (any, error) {
			select {
			case <-never:
			case <-ctx.Done():
			}

			return nil, ctx.Err()
		},
	}, 0)

	go func() { _, _ = pair.host.Call(t.Context(), "block", nil) }()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if _, err := pair.host.Call(ctx, wirerpc.MethodPing, nil); err != nil {
		t.Fatalf("ping while a handler blocks: %v", err)
	}
}

// TestAMethodsEntryCannotReplacePing holds that a module cannot take the
// liveness probe over.
func TestAMethodsEntryCannotReplacePing(t *testing.T) {
	t.Parallel()

	pair := connect(t, nil, wirerpc.Methods{
		wirerpc.MethodPing: func(_ context.Context, _ wirerpc.Call) (any, error) {
			return nil, errors.New("a module's own ping")
		},
	}, 0)

	if _, err := pair.host.Call(t.Context(), wirerpc.MethodPing, nil); err != nil {
		t.Fatalf("ping with a replacing handler: %v", err)
	}
}
