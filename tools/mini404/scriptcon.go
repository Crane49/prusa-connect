package mini404

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// The script console's own budgets.
const (
	// consoleDialRetry is the wait between connection attempts. QEMU's chardev
	// server accepts from the moment the process starts, well before the guest
	// console is live, so a connection that drops is retried rather than
	// reported.
	consoleDialRetry = 250 * time.Millisecond
	// consoleNoiseLines bounds how much prompt and banner output Do will read
	// past before it gives up on finding the acknowledgement.
	consoleNoiseLines = 256

	// DisplayScriptable is the script context name of the LCD, so
	// DisplayScriptable + "::Screenshot" is the action Screenshot sends. It is
	// the QOM type name of the panel, generic-spi-display, and not the model
	// name: hw/arm/prusa/parts/spi_display.c registers the instance under
	// TYPE_SPI_DISPLAY for every panel variant.
	DisplayScriptable = "generic-spi-display"
)

// ErrNoAcknowledgement is a command the console accepted without answering.
//
// MINI404 only writes the "ACK" reply when it was compiled with coverage
// instrumentation: the write in hw/arm/prusa/utility/p404_script_console.c sits
// inside an #ifdef CONFIG_GCOV. scripts/build-mini404.sh therefore configures
// with --enable-gcov, and a console taken from a build without it should use
// Send rather than Do.
var ErrNoAcknowledgement = errors.New("the script console accepted the command without acknowledging it")

// ScriptConsole drives the MINI404 script console over its TCP chardev. One
// command at a time: the console itself refuses concurrent input, so calls are
// serialised here.
type ScriptConsole struct {
	addr string
	log  *slog.Logger

	mu   sync.Mutex
	conn net.Conn
	r    *bufio.Reader
}

func newScriptConsole(addr string, log *slog.Logger) *ScriptConsole {
	return &ScriptConsole{addr: addr, log: log}
}

// Addr is the loopback address the chardev listens on.
func (c *ScriptConsole) Addr() string { return c.addr }

// Do sends one script action and waits for the console to acknowledge it.
//
// The action is the console's own spelling, context and method:
// "generic-spi-display::Screenshot(/tmp/lcd.png)".
func (c *ScriptConsole) Do(ctx context.Context, action string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.send(ctx, action); err != nil {
		return err
	}
	return c.awaitAck(ctx, action)
}

// Send sends one script action without waiting for an acknowledgement. It is
// what a console on a build with no ACK reply has, and what a command that
// stops the guest needs.
func (c *ScriptConsole) Send(ctx context.Context, action string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.send(ctx, action)
}

// Screenshot writes the LCD contents to pngPath, which the guest's QEMU process
// resolves, so it is a host path.
func (c *ScriptConsole) Screenshot(ctx context.Context, pngPath string) error {
	return c.Do(ctx, fmt.Sprintf("%s::Screenshot(%s)", DisplayScriptable, pngPath))
}

// Close drops the connection. A later call reconnects.
func (c *ScriptConsole) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.drop()
}

func (c *ScriptConsole) send(ctx context.Context, action string) error {
	conn, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	if deadlineErr := setDeadline(ctx, conn); deadlineErr != nil {
		return deadlineErr
	}
	if _, writeErr := conn.Write([]byte(action + "\n")); writeErr != nil {
		c.drop()
		return fmt.Errorf("write %q to the script console: %w", action, writeErr)
	}
	return nil
}

func (c *ScriptConsole) awaitAck(ctx context.Context, action string) error {
	for range consoleNoiseLines {
		line, err := c.r.ReadString('\n')
		if err != nil {
			c.drop()
			return fmt.Errorf("read the reply to %q: %w", action, err)
		}
		switch text := strings.TrimSpace(line); {
		case strings.Contains(text, "NACK"), strings.Contains(text, "ERR"):
			return fmt.Errorf("the script console refused %q: %s", action, text)
		case strings.Contains(text, "ACK"):
			return nil
		default:
			// A prompt, an autocomplete banner or firmware chatter. The console
			// interleaves all three with replies.
			c.log.DebugContext(ctx, "script console noise", "line", text)
		}
	}
	return fmt.Errorf("%w: %q", ErrNoAcknowledgement, action)
}

// ensure returns a live connection, dialling until ctx runs out.
func (c *ScriptConsole) ensure(ctx context.Context) (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}

	var dialer net.Dialer
	var last error
	for {
		conn, dialErr := dialer.DialContext(ctx, "tcp4", c.addr)
		if dialErr == nil {
			c.conn = conn
			c.r = bufio.NewReader(conn)
			return conn, nil
		}
		last = dialErr

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf(
				"connect to the script console at %s: %w (last dial error: %w)", c.addr, ctx.Err(), last)
		case <-time.After(consoleDialRetry):
		}
	}
}

func (c *ScriptConsole) drop() {
	if c.conn == nil {
		return
	}
	if err := c.conn.Close(); err != nil {
		c.log.Debug("closing the script console failed", "error", err)
	}
	c.conn = nil
	c.r = nil
}

// setDeadline puts the caller's deadline on the socket, so a read blocks for no
// longer than the context allows. A context with no deadline clears it.
func setDeadline(ctx context.Context, conn net.Conn) error {
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set the socket deadline: %w", err)
	}
	return nil
}
