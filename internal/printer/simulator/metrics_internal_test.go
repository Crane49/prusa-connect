package simulator

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// listenUDP opens a socket on a free loopback port and answers it with a
// reader of the next datagram.
func listenUDP(t *testing.T) (int, func() string) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return conn.LocalAddr().(*net.UDPAddr).Port, func() string {
		buf := make([]byte, 2048)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		n, _, readErr := conn.ReadFrom(buf)
		if readErr != nil {
			t.Fatalf("read a datagram: %v", readErr)
		}

		return string(buf[:n])
	}
}

// M334 waits for Yes on the display, then the printer sends its log lines and
// metrics in the firmware's formats, named by its lower-case MAC.
func TestM334SendsLogsAndMetricsAfterAPersonSaysYes(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	metricsPort, readMetrics := listenUDP(t)
	syslogPort, readSyslog := listenUDP(t)

	line := "M334 127.0.0.1 " + strconv.Itoa(metricsPort) + " " + strconv.Itoa(syslogPort)
	if !printer.noteMetricsConfig([]byte(line)) {
		t.Fatal("M334 was not read")
	}

	dialog := printer.Dialog()
	if dialog == nil || dialog.Title != "G-CODE METRICS CONFIG CHANGE" {
		t.Fatalf("dialog = %+v, want the metrics warning", dialog)
	}

	if _, _, _, on := printer.MetricsTarget(); on {
		t.Fatal("the change was applied before anybody answered")
	}

	if reason, ok := printer.answerDialog(map[string]any{"dialog_id": float64(dialog.ID), "button": "Yes"}); !ok {
		t.Fatalf("answer: %s", reason)
	}

	if got := readSyslog(); !strings.Contains(got, " "+printer.MAC()+" buddy Metrics - [metrics@64164 v=\"5\"") {
		t.Errorf("syslog datagram = %q", got)
	}

	printer.sendMetrics()

	got := readMetrics()
	if !strings.HasPrefix(got, "<14>1 - "+printer.MAC()+" buddy - - - msg=") ||
		!strings.Contains(got, "\ntemp_bed v=") {
		t.Errorf("metrics datagram = %q", got)
	}
}

// No on the display leaves Metrics & Log as it was, and port 0 sends nothing
// on that stream.
func TestM334ChangesNothingWithoutYes(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	printer.noteMetricsConfig([]byte("M334 10.0.0.2 8514 0"))
	dialog := printer.Dialog()

	if _, ok := printer.answerDialog(map[string]any{"dialog_id": float64(dialog.ID), "button": "No"}); !ok {
		t.Fatal("No was not taken")
	}

	if _, _, _, on := printer.MetricsTarget(); on {
		t.Error("No switched Metrics & Log on")
	}
}

// M334 reads its arguments the way the firmware's sscanf does, over what the
// printer has stored. A bare M334 switches the feature off and keeps where it
// sent, as Prusa-Firmware-Buddy's M330.cpp does.
func TestM334ReadsItsArgumentsOverTheStoredSetting(t *testing.T) {
	t.Parallel()

	stored := metricsTarget{on: true, host: "10.0.0.1", metricsPort: 8514, syslogPort: 13514}

	cases := []struct {
		name string
		args []string
		want metricsTarget
	}{
		{name: "no argument", args: nil, want: metricsTarget{host: "10.0.0.1", metricsPort: 8514, syslogPort: 13514}},
		{
			name: "a host and both ports", args: []string{"10.0.0.2", "9000", "0"},
			want: metricsTarget{on: true, host: "10.0.0.2", metricsPort: 9000},
		},
		{
			name: "a host alone keeps the ports", args: []string{"10.0.0.2"},
			want: metricsTarget{on: true, host: "10.0.0.2", metricsPort: 8514, syslogPort: 13514},
		},
		{
			name: "a port that does not read ends the scan", args: []string{"10.0.0.2", "x", "9001"},
			want: metricsTarget{on: true, host: "10.0.0.2", metricsPort: 8514, syslogPort: 13514},
		},
		{
			name: "a host longer than the firmware stores is cut", args: []string{"host-name-longer-than-twenty", "1"},
			want: metricsTarget{on: true, host: "host-name-longer-tha", metricsPort: 8514, syslogPort: 13514},
		},
		{
			name: "digits after the twentieth character are the metrics port",
			args: []string{"abcdefghijklmnopqrst9001", "9002"},
			want: metricsTarget{on: true, host: "abcdefghijklmnopqrst", metricsPort: 9001, syslogPort: 9002},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseM334(tc.args, stored); got != tc.want {
				t.Errorf("parseM334(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

// A bare M334 on a printer that sends asks first and then stops sending, and
// a later M334 naming only the host sends to the ports it had. On a printer
// that sends nothing it changes nothing and asks nothing.
func TestABareM334SwitchesMetricsOffAndKeepsWhereTheyWent(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	printer.noteMetricsConfig([]byte("M334"))

	if dialog := printer.Dialog(); dialog != nil {
		t.Fatalf("a bare M334 on a printer that sends nothing raised %+v", dialog)
	}

	answerYes := func() {
		t.Helper()

		dialog := printer.Dialog()
		if dialog == nil {
			t.Fatal("M334 raised no dialog")
		}

		if reason, ok := printer.answerDialog(map[string]any{"dialog_id": float64(dialog.ID), "button": "Yes"}); !ok {
			t.Fatalf("answer: %s", reason)
		}
	}

	printer.noteMetricsConfig([]byte("M334 10.0.0.2 9000 9001"))
	answerYes()

	printer.noteMetricsConfig([]byte("M334"))
	answerYes()

	if _, _, _, on := printer.MetricsTarget(); on {
		t.Fatal("a bare M334 left Metrics & Log on")
	}

	printer.noteMetricsConfig([]byte("M334 10.0.0.3"))
	answerYes()

	host, metricsPort, syslogPort, on := printer.MetricsTarget()
	if !on || host != "10.0.0.3" || metricsPort != 9000 || syslogPort != 9001 {
		t.Errorf("target = %s %d %d %v, want 10.0.0.3 9000 9001 on", host, metricsPort, syslogPort, on)
	}
}

// Answering a dialog returns the printer to the state it was raised in, so a
// print the dialog stopped goes on printing.
func TestAnsweringADialogReturnsToTheStateBeforeIt(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if !printer.startJob(1, "/usb/part.bgcode") {
		t.Fatal("the print did not start")
	}

	printer.RaiseDialog(Dialog{ID: 1, Buttons: []string{"Ok"}})
	printer.RaiseDialog(Dialog{ID: 2, Buttons: []string{"Ok"}})

	if reason, ok := printer.answerDialog(map[string]any{"dialog_id": float64(2), "button": "Ok"}); !ok {
		t.Fatalf("answer: %s", reason)
	}

	printer.mu.Lock()
	state := printer.state
	printer.mu.Unlock()

	if state != statePrinting {
		t.Errorf("state after the dialog = %s, want %s", state, statePrinting)
	}
}

// What waits on an M334 answer belongs to the M334 dialog. A dialog raised at
// the same moment never runs it when answered.
func TestADialogRaisedAlongsideM334NeverTakesItsAnswer(t *testing.T) {
	t.Parallel()

	for range 200 {
		printer, err := New(Config{Model: "MK4"})
		if err != nil {
			t.Fatalf("new: %v", err)
		}

		var ran atomic.Bool

		var wg sync.WaitGroup

		wg.Go(func() {
			printer.raiseAnswered(Dialog{ID: 1, Buttons: []string{"Yes"}}, func(string) { ran.Store(true) })
		})
		wg.Go(func() { printer.RaiseDialog(Dialog{ID: 2, Buttons: []string{"Yes"}}) })
		wg.Wait()

		shown := printer.Dialog()
		if _, ok := printer.answerDialog(map[string]any{"dialog_id": float64(shown.ID), "button": "Yes"}); !ok {
			t.Fatal("the dialog on the display took no answer")
		}

		if ran.Load() != (shown.ID == 1) {
			t.Fatalf("answering dialog %d ran the M334 answer: %v", shown.ID, ran.Load())
		}
	}
}

// One emitter sends at a time. A Run after an earlier one ended starts the
// emitter again, and two Runs at once never send twice per tick.
func TestOneEmitterSendsAndALaterRunStartsItAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer, err := New(Config{Model: "MK4"})
		if err != nil {
			t.Fatalf("new: %v", err)
		}

		sent := func() uint32 {
			printer.mu.Lock()
			defer printer.mu.Unlock()

			return printer.metricsSeq
		}

		first, stopFirst := context.WithCancel(t.Context())
		second, stopSecond := context.WithCancel(t.Context())

		defer stopSecond()

		// The first takes the token before the second is started, so the
		// second is the one that has to wait.
		go printer.emit(first)
		synctest.Wait()

		go printer.emit(second)

		time.Sleep(10*metricsTick + metricsTick/2)
		synctest.Wait()

		if got := sent(); got != 10 {
			t.Fatalf("ten ticks sent %d datagrams, want 10 from one emitter", got)
		}

		stopFirst()
		synctest.Wait()

		time.Sleep(10*metricsTick + metricsTick/2)
		synctest.Wait()

		if got := sent(); got != 20 {
			t.Errorf("after the first emitter ended, ten more ticks reached %d datagrams, want 20", got)
		}
	})
}
