package mini404

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// consoleTestBudget is generous because these cases are on a loopback socket,
// not in virtual time.
const consoleTestBudget = 10 * time.Second

// contextWithBudget is a deadline the client puts on its socket, so a broken
// case fails instead of hanging the package.
func contextWithBudget(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	return context.WithTimeout(t.Context(), consoleTestBudget)
}

func TestACommandIsAcknowledgedThroughLeadingNoise(t *testing.T) {
	t.Parallel()

	got := make(chan string, 1)
	console := fakeConsole(t, func(line string, w *bufio.Writer) {
		got <- line
		// The console writes a prompt and an autocomplete banner before it
		// answers, which is what the reply loop has to read past.
		_, _ = w.WriteString("QEMU 9.0 monitor\n(qemu) \nACK\r\n")
		_ = w.Flush()
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := console.Do(ctx, "generic-spi-display::Screenshot(/tmp/a.png)"); err != nil {
		t.Fatalf("the command was not acknowledged: %v", err)
	}
	if line := <-got; line != "generic-spi-display::Screenshot(/tmp/a.png)" {
		t.Errorf("the console received %q", line)
	}
}

func TestAScreenshotSendsTheDisplayScriptable(t *testing.T) {
	t.Parallel()

	got := make(chan string, 1)
	console := fakeConsole(t, func(line string, w *bufio.Writer) {
		got <- line
		_, _ = w.WriteString("ACK\r\n")
		_ = w.Flush()
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := console.Screenshot(ctx, "/tmp/lcd.png"); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if want := DisplayScriptable + "::Screenshot(/tmp/lcd.png)"; <-got != want {
		t.Errorf("the console should have received %q", want)
	}
}

func TestARefusedCommandIsAnError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		reply string
	}{
		{name: "NACK", reply: "NACK\r\n"},
		{name: "ERR", reply: "ERR: no such method\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			console := fakeConsole(t, func(_ string, w *bufio.Writer) {
				_, _ = w.WriteString(tc.reply)
				_ = w.Flush()
			})

			ctx, cancel := contextWithBudget(t)
			defer cancel()
			if err := console.Do(ctx, "thing::Method()"); err == nil {
				t.Fatalf("a %s reply was read as success", tc.name)
			}
		})
	}
}

func TestAConsoleThatNeverAnswersIsReportedAsSuch(t *testing.T) {
	t.Parallel()

	console := fakeConsole(t, func(_ string, w *bufio.Writer) {
		for range consoleNoiseLines + 1 {
			_, _ = w.WriteString("(qemu) \n")
		}
		_ = w.Flush()
	})

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	err := console.Do(ctx, "thing::Method()")
	if !errors.Is(err, ErrNoAcknowledgement) {
		t.Fatalf("the error is %v and should be an ErrNoAcknowledgement", err)
	}
}

func TestSendDoesNotWaitForAnAcknowledgement(t *testing.T) {
	t.Parallel()

	got := make(chan string, 1)
	console := fakeConsole(t, func(line string, _ *bufio.Writer) { got <- line })

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := console.Send(ctx, "thing::Method()"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if <-got != "thing::Method()" {
		t.Error("the console did not receive the command")
	}
}

// fakeConsole is a loopback listener that answers one line at a time the way
// the MINI404 chardev does. It is closed with the test.
func fakeConsole(t *testing.T, answer func(line string, w *bufio.Writer)) *ScriptConsole {
	t.Helper()

	l, listenErr := net.Listen("tcp4", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			conn, acceptErr := l.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()

				r := bufio.NewReader(conn)
				w := bufio.NewWriter(conn)
				for {
					line, readErr := r.ReadString('\n')
					if readErr != nil {
						return
					}
					answer(strings.TrimSpace(line), w)
				}
			}()
		}
	}()

	console := newScriptConsole(l.Addr().String(), slog.New(slog.DiscardHandler))
	t.Cleanup(console.Close)
	return console
}
