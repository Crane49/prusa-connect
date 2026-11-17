package receiver_test

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/datagram"
	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
	"github.com/prusa3d/buddy-debug-logs/internal/receiver"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

func now() time.Time { return time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC) }

func rig(t *testing.T) (*receiver.Receiver, *store.Store) {
	t.Helper()

	held, err := store.Open(t.TempDir(), now, store.Limits{
		Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 8 << 20, store.Metrics: 8 << 20},
	})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}

	t.Cleanup(func() { _ = held.Close() })

	devices := &hostfake.Devices{}
	devices.Add("mk4", "MK4", "10:9c:70:2a:bb:cd", "", "")

	table, err := sources.Open("", devices, now)
	if err != nil {
		t.Fatalf("open the sources: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	return receiver.New(held, table, now), held
}

func all(store.Record) bool { return true }

// A syslog line becomes one record, a metrics datagram one record per point,
// and a datagram in neither format is kept raw and counted.
func TestEveryDatagramIsKept(t *testing.T) {
	t.Parallel()

	r, held := rig(t)

	r.Handle(store.Syslog, "192.168.1.20",
		[]byte(`<11>1 - 10:9c:70:2a:bb:cd buddy Connect - [metrics@64164 v="5" msg="3" tm="99"] reset`))
	r.Handle(store.Metrics, "192.168.1.20",
		[]byte("<14>1 - 10:9c:70:2a:bb:cd buddy - - - msg=4,tm=1000,v=4 temp_bed v=60.5 -20\nfan_speed v=4200i 5\n"))
	r.Handle(store.Syslog, "10.0.0.9", []byte("not a printer"))

	logs, _, err := held.Newest(store.Syslog, store.Window{}, all, nil, 10)
	if err != nil || len(logs) != 2 {
		t.Fatalf("syslog holds %d records, %v", len(logs), err)
	}

	if logs[1].Record.Device != "mk4" || logs[1].Record.Text != "reset" || *logs[1].Record.Severity != 3 {
		t.Errorf("the line was kept as %+v", logs[1].Record)
	}

	if logs[0].Record.Raw != "not a printer" {
		t.Errorf("the unreadable datagram was kept as %+v", logs[0].Record)
	}

	points, _, err := held.Newest(store.Metrics, store.Window{}, all, nil, 10)
	if err != nil || len(points) != 2 || points[1].Record.Name != "temp_bed" || points[1].Record.PointUS != 980 {
		t.Fatalf("metrics = %+v, %v", points, err)
	}

	status := r.Status()
	if c := status[store.Syslog].Counts; c.Received != 2 || c.Unreadable != 1 || c.Records != 2 {
		t.Errorf("syslog counts = %+v", c)
	}

	if c := status[store.Metrics].Counts; c.Received != 1 || c.Records != 2 {
		t.Errorf("metrics counts = %+v", c)
	}
}

// A real datagram sent to the open port is kept, and the port closes with the
// context.
func TestADatagramSentToThePortIsKept(t *testing.T) {
	t.Parallel()

	r, held := rig(t)

	ctx, cancel := context.WithCancel(t.Context())

	done, err := r.Listen(ctx, store.Syslog, "udp", 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := r.Status()[store.Syslog].Listener.Port

	conn, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer conn.Close()

	if _, err = conn.Write(
		[]byte(`<12>1 - 10:9c:70:2a:bb:cd buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] hello`),
	); err != nil {
		t.Fatalf("send: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		got, _, _ := held.Newest(store.Syslog, store.Window{}, all, nil, 1)
		if len(got) == 1 {
			if got[0].Record.Text != "hello" || got[0].Record.IP != "127.0.0.1" {
				t.Fatalf("kept %+v", got[0].Record)
			}

			if !r.Status()[store.Syslog].Listener.Listening {
				t.Fatal("the status says the port is closed while it reads")
			}

			cancel()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the reading goroutine did not end with its context")
			}

			if r.Status()[store.Syslog].Listener.Listening {
				t.Error("the status says the port is open after it closed")
			}

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	t.Fatal("the datagram was not kept within five seconds")
}

// A port another program holds is reported, and nothing else stops.
func TestAPortInUseIsReported(t *testing.T) {
	t.Parallel()

	taken, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("take a port: %v", err)
	}

	defer taken.Close()

	r, _ := rig(t)
	port := taken.LocalAddr().(*net.UDPAddr).Port

	if _, err = r.Listen(t.Context(), store.Metrics, "udp", port); err == nil {
		t.Skip("this system lets two sockets share the port, so the refusal cannot be shown here")
	}

	listener := r.Status()[store.Metrics].Listener
	if listener.Listening || listener.Error == "" || !listener.Granted {
		t.Errorf("listener = %+v", listener)
	}
}

// A datagram whose handling panics is counted as unreadable, and the loop goes
// on reading the next one. A receiver with no sources table panics on every
// readable datagram, which stands in for a defect a sender could meet.
func TestADatagramThatPanicsDoesNotStopTheLoop(t *testing.T) {
	t.Parallel()

	_, held := rig(t)
	r := receiver.New(held, nil, now)

	if _, err := r.Listen(t.Context(), store.Syslog, "udp", 0); err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := r.Status()[store.Syslog].Listener.Port

	conn, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer conn.Close()

	for _, raw := range []string{
		`<12>1 - 10:9c:70:2a:bb:cd buddy GUI - [metrics@64164 v="5" msg="1" tm="1"] hello`,
		"not a printer",
	} {
		if _, err = conn.Write([]byte(raw)); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		got, _, _ := held.Newest(store.Syslog, store.Window{}, all, nil, 1)
		if len(got) == 1 {
			if got[0].Record.Raw != "not a printer" {
				t.Fatalf("kept %+v", got[0].Record)
			}

			if c := r.Status()[store.Syslog].Counts; c.Received != 2 || c.Unreadable != 2 {
				t.Errorf("counts = %+v, want 2 received and 2 unreadable", c)
			}

			if !r.Status()[store.Syslog].Listener.Listening {
				t.Error("the port closed after a datagram panicked")
			}

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("the datagram after the one that panicked was not kept within five seconds")
}

// An unreadable datagram is kept with its control characters escaped, and one
// larger than any the firmware sends is unreadable.
func TestAnUnreadableDatagramIsKeptClean(t *testing.T) {
	t.Parallel()

	r, held := rig(t)

	r.Handle(store.Syslog, "10.0.0.9", []byte("evil\x1b[2J\nline"))

	big := `<12>1 - 10:9c:70:2a:bb:cd buddy GUI - - ` + strings.Repeat("x", datagram.MaxBytes)
	r.Handle(store.Syslog, "10.0.0.9", []byte(big))

	got, _, err := held.Newest(store.Syslog, store.Window{}, all, nil, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("kept %d records, %v", len(got), err)
	}

	if got[1].Record.Raw != `evil\x1b[2J\x0aline` {
		t.Errorf("raw = %q", got[1].Record.Raw)
	}

	if got[0].Record.Raw == "" || got[0].Record.Text != "" {
		t.Errorf("the large datagram was kept as %+v", got[0].Record)
	}

	if c := r.Status()[store.Syslog].Counts; c.Unreadable != 2 {
		t.Errorf("counts = %+v", c)
	}
}
