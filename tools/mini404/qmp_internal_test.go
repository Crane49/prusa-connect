package mini404

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// errHangUp tells the fake monitor to drop the connection, which is what QEMU
// does when it acts on a quit.
var errHangUp = errors.New("hang up")

func TestTheMonitorHandshakeRunsBeforeTheFirstCommand(t *testing.T) {
	t.Parallel()

	names := make(chan string, 4)
	q := fakeMonitor(t, names, func(_ string, enc *json.Encoder) error {
		return enc.Encode(map[string]any{"return": map[string]any{}})
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := q.Screendump(ctx, "/tmp/screen.ppm"); err != nil {
		t.Fatalf("screendump: %v", err)
	}

	got := []string{<-names, <-names}
	want := []string{"qmp_capabilities", "screendump"}
	if !slices.Equal(got, want) {
		t.Errorf("the monitor received %q and should have received %q", got, want)
	}
}

func TestAnEventBeforeTheReturnIsSkipped(t *testing.T) {
	t.Parallel()

	q := fakeMonitor(t, nil, func(_ string, enc *json.Encoder) error {
		if err := enc.Encode(map[string]any{"event": "RESUME", "timestamp": map[string]any{"seconds": 1}}); err != nil {
			return err
		}
		if err := enc.Encode(map[string]any{"event": "STOP", "timestamp": map[string]any{"seconds": 2}}); err != nil {
			return err
		}
		return enc.Encode(map[string]any{"return": map[string]any{}})
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := q.Quit(ctx); err != nil {
		t.Fatalf("quit: %v", err)
	}
}

func TestARefusedCommandCarriesTheMonitorsReason(t *testing.T) {
	t.Parallel()

	q := fakeMonitor(t, nil, func(name string, enc *json.Encoder) error {
		if name == "qmp_capabilities" {
			return enc.Encode(map[string]any{"return": map[string]any{}})
		}
		return enc.Encode(map[string]any{"error": map[string]any{
			"class": "GenericError",
			"desc":  "there is no display",
		}})
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	err := q.Screendump(ctx, "/tmp/screen.ppm")
	if err == nil {
		t.Fatal("a refusal was read as success")
	}
	if !strings.Contains(err.Error(), "there is no display") {
		t.Errorf("the error does not carry the monitor's reason: %v", err)
	}
}

func TestQuitTreatsAHangUpAsSuccess(t *testing.T) {
	t.Parallel()

	q := fakeMonitor(t, nil, func(name string, enc *json.Encoder) error {
		if name == "qmp_capabilities" {
			return enc.Encode(map[string]any{"return": map[string]any{}})
		}
		return errHangUp
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := q.Quit(ctx); err != nil {
		t.Fatalf("a monitor that hung up on the quit was reported as a failure: %v", err)
	}
}

func TestAGreetinglessSocketIsNotAMonitor(t *testing.T) {
	t.Parallel()

	path := filepath.Join(socketDir(t), "qmp.sock")
	l, listenErr := net.Listen("unix", path)
	if listenErr != nil {
		t.Fatalf("listen on %s: %v", path, listenErr)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		conn, acceptErr := l.Accept()
		if acceptErr != nil {
			return
		}
		_, _ = conn.Write([]byte("{\"hello\":true}\n"))
		_ = conn.Close()
	}()

	q := newQMP(path, slog.New(slog.DiscardHandler))
	t.Cleanup(q.Close)

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if dumpErr := q.Screendump(ctx, "/tmp/screen.ppm"); dumpErr == nil {
		t.Fatal("a socket that sent no greeting was accepted as a monitor")
	}
}

// fakeMonitor is a unix socket that greets like QMP and answers each execute
// through reply. Command names go to names when it is not nil.
func fakeMonitor(t *testing.T, names chan<- string, reply func(name string, enc *json.Encoder) error) *QMP {
	t.Helper()

	path := filepath.Join(socketDir(t), "qmp.sock")
	l, listenErr := net.Listen("unix", path)
	if listenErr != nil {
		t.Fatalf("listen on %s: %v", path, listenErr)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		conn, acceptErr := l.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		enc := json.NewEncoder(conn)
		dec := json.NewDecoder(conn)
		greeting := map[string]any{"QMP": map[string]any{
			"version":      map[string]any{"package": "test"},
			"capabilities": []string{},
		}}
		if greetErr := enc.Encode(greeting); greetErr != nil {
			return
		}

		for {
			var msg struct {
				Execute string `json:"execute"`
			}
			if decodeErr := dec.Decode(&msg); decodeErr != nil {
				return
			}
			if names != nil {
				names <- msg.Execute
			}
			if replyErr := reply(msg.Execute, enc); replyErr != nil {
				return
			}
		}
	}()

	q := newQMP(path, slog.New(slog.DiscardHandler))
	t.Cleanup(q.Close)
	return q
}
