package timeserver_test

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/timeserver"
)

const ntpEpochOffset = 2208988800

func TestAnAnswerCarriesTheTimeAndTheClientsStamp(t *testing.T) {
	t.Parallel()

	request := make([]byte, 48)
	request[0] = 4<<3 | 3
	request[2] = 6
	copy(request[40:48], "STAMP123")

	now := time.Date(2026, 11, 12, 9, 30, 0, 500_000_000, time.UTC)
	got := timeserver.Answer(request, now)

	if len(got) != 48 || got[0] != 4<<3|4 || got[1] != 10 || got[2] != 6 {
		t.Fatalf("header % x, want version 4, mode 4, stratum 10, poll 6", got[:4])
	}

	if string(got[24:32]) != "STAMP123" {
		t.Fatalf("originate %q, want the client's transmit time", got[24:32])
	}

	if secs := binary.BigEndian.Uint32(got[40:44]); int64(secs) != now.Unix()+ntpEpochOffset {
		t.Fatalf("transmit seconds %d, want %d", secs, now.Unix()+ntpEpochOffset)
	}

	if frac := binary.BigEndian.Uint32(got[44:48]); frac != 1<<31 {
		t.Fatalf("transmit fraction %d, want half a second", frac)
	}
}

func TestAVersionZeroRequestIsAnsweredAsVersionFour(t *testing.T) {
	t.Parallel()

	request := make([]byte, 48)
	request[0] = 3

	if got := timeserver.Answer(request, time.Now())[0]; got != 4<<3|4 {
		t.Fatalf("first byte %#x, want %#x", got, 4<<3|4)
	}
}

func TestAServerAnswersWhileOnAndNotOnceOff(t *testing.T) {
	t.Parallel()

	server := timeserver.New("127.0.0.1:0", slog.New(slog.DiscardHandler))

	if state, _, _ := server.State(); state != timeserver.StateOff {
		t.Fatalf("before start: %s, want off", state)
	}

	if err := server.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	addr := server.LocalAddr().String()
	if state, _, _ := server.State(); state != timeserver.StateServing {
		t.Fatalf("after start: %s, want serving", state)
	}

	if !answers(t, addr, []byte{4<<3 | 3}) {
		t.Fatal("a client request got no answer")
	}

	if answers(t, addr, []byte{4<<3 | 4}) {
		t.Fatal("a server-mode packet was answered")
	}

	if answersSized(t, addr, []byte{4<<3 | 3}, 47) {
		t.Fatal("a request shorter than an answer was answered")
	}

	server.Stop()

	if state, _, _ := server.State(); state != timeserver.StateOff {
		t.Fatalf("after stop: %s, want off", state)
	}

	if answers(t, addr, []byte{4<<3 | 3}) {
		t.Fatal("a stopped server answered")
	}
}

func TestAPortInUseIsReportedAsFailedAndStopClearsIt(t *testing.T) {
	t.Parallel()

	taken, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()

	server := timeserver.New(taken.LocalAddr().String(), slog.New(slog.DiscardHandler))
	if err = server.Start(t.Context()); err == nil {
		t.Fatal("start on a taken port succeeded")
	}

	if state, reason, cause := server.State(); state != timeserver.StateFailed || reason == "" ||
		cause != timeserver.CauseInUse {
		t.Fatalf("state %s %q %q, want failed with a reason and the cause in_use", state, reason, cause)
	}

	server.Stop()

	if state, _, _ := server.State(); state != timeserver.StateOff {
		t.Fatalf("after stop: %s, want off", state)
	}
}

func TestACancelledContextEndsServing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	server := timeserver.New("127.0.0.1:0", slog.New(slog.DiscardHandler))

	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for state, _, _ := server.State(); state == timeserver.StateServing; state, _, _ = server.State() {
		if time.Now().After(deadline) {
			t.Fatal("still serving after the context ended")
		}

		time.Sleep(10 * time.Millisecond)
	}

	server.Stop()
}

// answers sends one 48-byte packet whose first byte is head and reports
// whether anything came back within a short wait.
func answers(t *testing.T, addr string, head []byte) bool {
	t.Helper()

	return answersSized(t, addr, head, 48)
}

// answersSized is answers with a packet of size bytes.
func answersSized(t *testing.T, addr string, head []byte, size int) bool {
	t.Helper()

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	request := make([]byte, size)
	copy(request, head)

	if _, err = conn.Write(request); err != nil {
		t.Fatal(err)
	}

	if err = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	reply := make([]byte, 64)
	n, err := conn.Read(reply)

	return err == nil && n == 48
}

func TestAServerWhoseContextEndedStartsAgain(t *testing.T) {
	t.Parallel()

	server := timeserver.New("127.0.0.1:0", slog.New(slog.DiscardHandler))

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := server.Start(cancelled); err == nil {
		t.Fatal("start on an ended context succeeded")
	}

	first, end := context.WithCancel(t.Context())
	if err := server.Start(first); err != nil {
		t.Fatal(err)
	}

	end()

	deadline := time.Now().Add(2 * time.Second)
	for state, _, _ := server.State(); state == timeserver.StateServing; state, _, _ = server.State() {
		if time.Now().After(deadline) {
			t.Fatal("still serving after the context ended")
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err := server.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	if !answers(t, server.LocalAddr().String(), []byte{4<<3 | 3}) {
		t.Fatal("a server started again after its context ended does not answer")
	}
}
