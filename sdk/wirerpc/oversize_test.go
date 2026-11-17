package wirerpc_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// promptly bounds a call that must fail at once. It is far shorter than any
// deadline a real caller sets, and far longer than one round trip on a pipe.
const promptly = 10 * time.Second

// logBuffer is a log destination two goroutines may write at once.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// connectLogged builds a session whose two sides log into the buffers it
// returns, so a test can read what each side wrote.
func connectLogged(t *testing.T, hostMethods, childMethods wirerpc.Methods) (session, *logBuffer, *logBuffer) {
	t.Helper()

	hostSide, childSide := pipePair(t)
	hostLog, childLog := &logBuffer{}, &logBuffer{}

	host, err := wirerpc.NewConn(hostSide, wirerpc.Options{
		Role: wirerpc.RoleHost, Methods: hostMethods,
		Logger: slog.New(slog.NewTextHandler(hostLog, nil)),
	})
	if err != nil {
		t.Fatalf("build the host session: %v", err)
	}

	child, err := wirerpc.NewConn(childSide, wirerpc.Options{
		Role: wirerpc.RoleChild, Methods: childMethods,
		Logger: slog.New(slog.NewTextHandler(childLog, nil)),
	})
	if err != nil {
		t.Fatalf("build the child session: %v", err)
	}

	var running sync.WaitGroup

	running.Go(func() { _ = host.Serve(t.Context()) })
	running.Go(func() { _ = child.Serve(t.Context()) })

	t.Cleanup(func() {
		_ = host.Close()
		_ = child.Close()
		running.Wait()
	})

	return session{host: host, child: child}, hostLog, childLog
}

// oversized is a result whose encoding is over the default frame limit.
func oversized(_ context.Context, _ wirerpc.Call) (any, error) {
	return strings.Repeat("x", wire.DefaultMaxPayloadBytes), nil
}

// small answers with a short result, so a test can see the session still works.
func small(_ context.Context, _ wirerpc.Call) (any, error) {
	return "ok", nil
}

// TestAnAnswerOverTheFrameLimitReachesTheCallerAsAnError holds both
// directions. The answering side cannot send the result, so it logs that and
// sends CodeResultTooLarge in its place. The caller gets that error at once
// rather than at its own deadline, and the session carries on.
func TestAnAnswerOverTheFrameLimitReachesTheCallerAsAnError(t *testing.T) {
	t.Parallel()

	methods := wirerpc.Methods{"blob.big": oversized, "blob.small": small}

	cases := []struct {
		name string
		// caller and answerLog pick the side that calls and the log of the
		// side that answers.
		caller    func(session) *wirerpc.Conn
		answerLog func(host, child *logBuffer) *logBuffer
	}{
		{
			name:      "the host calls and the child answers",
			caller:    func(s session) *wirerpc.Conn { return s.host },
			answerLog: func(_, child *logBuffer) *logBuffer { return child },
		},
		{
			name:      "the child calls and the host answers",
			caller:    func(s session) *wirerpc.Conn { return s.child },
			answerLog: func(host, _ *logBuffer) *logBuffer { return host },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pair, hostLog, childLog := connectLogged(t, methods, methods)
			caller := tc.caller(pair)

			ctx, cancel := context.WithTimeout(t.Context(), promptly)
			defer cancel()

			_, err := caller.Call(ctx, "blob.big", nil)

			var failure *wirerpc.Error
			if !errors.As(err, &failure) {
				t.Fatalf("an answer over the frame limit came back as %v, and it is a JSON-RPC error", err)
			}

			if failure.Code != wirerpc.CodeResultTooLarge {
				t.Errorf("the error carries code %d, and an answer too large is %d",
					failure.Code, wirerpc.CodeResultTooLarge)
			}

			if logged := tc.answerLog(hostLog, childLog).String(); !strings.Contains(logged, "over the frame limit") {
				t.Errorf("the answering side logged %q, and it says the answer was over the frame limit", logged)
			}

			answer, err := caller.Call(ctx, "blob.small", nil)
			if err != nil {
				t.Fatalf("a call after the refused answer failed: %v", err)
			}

			if string(answer) != `"ok"` {
				t.Errorf("the next call answered %s, and its handler returned \"ok\"", answer)
			}
		})
	}
}

// TestACallOverTheFrameLimitIsRefusedToItsCaller holds the request direction.
// The writer refuses the frame before a byte of it is sent, so the caller gets
// the refusal at once and the session carries on.
func TestACallOverTheFrameLimitIsRefusedToItsCaller(t *testing.T) {
	t.Parallel()

	methods := wirerpc.Methods{"blob.small": small}
	pair, _, _ := connectLogged(t, methods, methods)

	ctx, cancel := context.WithTimeout(t.Context(), promptly)
	defer cancel()

	_, err := pair.child.Call(ctx, "blob.small", strings.Repeat("x", wire.DefaultMaxPayloadBytes))
	if !errors.Is(err, wire.ErrPayloadTooLarge) {
		t.Fatalf("a call over the frame limit returned %v, and it reports %v", err, wire.ErrPayloadTooLarge)
	}

	if _, err = pair.child.Call(ctx, "blob.small", nil); err != nil {
		t.Fatalf("a call after the refused one failed: %v", err)
	}
}
