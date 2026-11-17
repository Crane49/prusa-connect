package mini404

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// qmpDialRetry is the wait between attempts to reach the monitor socket. QEMU
// creates it during startup, so the first dial can lose the race.
const qmpDialRetry = 100 * time.Millisecond

// QMP is a minimal QEMU Machine Protocol client over the monitor's unix socket.
// It carries the two commands a firmware test needs and nothing else. It
// connects on first use and performs the capabilities handshake then.
type QMP struct {
	path string
	log  *slog.Logger

	mu   sync.Mutex
	conn net.Conn
	dec  *json.Decoder
	enc  *json.Encoder
}

func newQMP(path string, log *slog.Logger) *QMP {
	return &QMP{path: path, log: log}
}

// Quit asks the guest to shut down. QEMU may close the socket instead of
// answering, which counts as success.
func (q *QMP) Quit(ctx context.Context) error {
	if err := q.execute(ctx, "quit", nil); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Screendump writes the guest display to path as PPM. QEMU resolves the path,
// so it is a host path. The script console's Screenshot is the higher-fidelity
// route for the LCD; this one needs no scripting support in the machine.
func (q *QMP) Screendump(ctx context.Context, path string) error {
	return q.execute(ctx, "screendump", map[string]any{"filename": path})
}

// Close drops the connection. A later call reconnects.
func (q *QMP) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.drop()
}

func (q *QMP) execute(ctx context.Context, command string, args map[string]any) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	conn, err := q.ensure(ctx)
	if err != nil {
		return err
	}
	if deadlineErr := setDeadline(ctx, conn); deadlineErr != nil {
		return deadlineErr
	}
	if cmdErr := q.command(command, args); cmdErr != nil {
		q.drop()
		return cmdErr
	}
	return nil
}

// command writes one execute and reads until the reply to it. Callers hold the
// mutex and have set the deadline.
func (q *QMP) command(name string, args map[string]any) error {
	msg := map[string]any{"execute": name}
	if args != nil {
		msg["arguments"] = args
	}
	if err := q.enc.Encode(msg); err != nil {
		return fmt.Errorf("send the qmp command %q: %w", name, err)
	}
	return q.awaitReturn(name)
}

// awaitReturn reads JSON lines until one carries a return or an error. QEMU
// interleaves asynchronous events with replies, and an event carries neither
// key, so presence is what separates them.
func (q *QMP) awaitReturn(name string) error {
	for {
		var reply struct {
			Return json.RawMessage `json:"return"`
			Error  *struct {
				Class string `json:"class"`
				Desc  string `json:"desc"`
			} `json:"error"`
			Event string `json:"event"`
		}
		if err := q.dec.Decode(&reply); err != nil {
			return fmt.Errorf("read the qmp reply to %q: %w", name, err)
		}
		switch {
		case reply.Error != nil:
			return fmt.Errorf("qmp refused %q: %s: %s", name, reply.Error.Class, reply.Error.Desc)
		case reply.Return != nil:
			return nil
		default:
			q.log.Debug("qmp event", "event", reply.Event)
		}
	}
}

// ensure returns a connection that has completed the handshake, dialling until
// ctx runs out.
func (q *QMP) ensure(ctx context.Context) (net.Conn, error) {
	if q.conn != nil {
		return q.conn, nil
	}

	var dialer net.Dialer
	var last error
	for {
		conn, dialErr := dialer.DialContext(ctx, "unix", q.path)
		if dialErr == nil {
			q.conn = conn
			q.dec = json.NewDecoder(conn)
			q.enc = json.NewEncoder(conn)
			if handshakeErr := q.handshake(ctx, conn); handshakeErr != nil {
				q.drop()
				return nil, handshakeErr
			}
			return conn, nil
		}
		last = dialErr

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf(
				"connect to the qmp socket at %s: %w (last dial error: %w)", q.path, ctx.Err(), last)
		case <-time.After(qmpDialRetry):
		}
	}
}

// handshake reads the greeting and leaves capabilities negotiation done, which
// is what puts the monitor into command mode.
func (q *QMP) handshake(ctx context.Context, conn net.Conn) error {
	if deadlineErr := setDeadline(ctx, conn); deadlineErr != nil {
		return deadlineErr
	}

	var greeting struct {
		QMP json.RawMessage `json:"QMP"`
	}
	if err := q.dec.Decode(&greeting); err != nil {
		return fmt.Errorf("read the qmp greeting: %w", err)
	}
	if greeting.QMP == nil {
		return errors.New("the qmp socket sent no greeting, so it is not a monitor")
	}
	return q.command("qmp_capabilities", nil)
}

func (q *QMP) drop() {
	if q.conn == nil {
		return
	}
	if err := q.conn.Close(); err != nil {
		q.log.Debug("closing the qmp socket failed", "error", err)
	}
	q.conn = nil
	q.dec = nil
	q.enc = nil
}
