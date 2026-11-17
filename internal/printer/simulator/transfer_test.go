package simulator_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The connection drop a Buddy board performs for having accepted a transfer, and
// the file arriving whole across it.
//
// The drop is the measured behaviour and not an edge: an MK4S at 6.5.7+12836
// reset its connection within about 100 ms of accepting, every time, and asked
// again on the next connection under a file id it had not used before. A server
// that bound the first id and refused the rest serves a transfer that can never
// finish.
//
// The session count is asserted at both ends: two, because the drop is owed once
// per transfer, and a version that dropped on every chunk would finish the file
// as well and take eight connections to do it.
func TestAnAcceptedTransferDropsTheConnectionAndResumesUnderANewFileID(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		host := newHost(t)
		host.onMessage = transferMessages(messages)
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		printer, err := simulator.New(simulator.Config{
			Model:      "MK4",
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: time.Hour,
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		<-sessions

		// The chunk is the largest this board takes, and the file is several
		// chunks long so that the drop lands with bytes still to come.
		const chunk = 1024

		file := sampleFile(8 * chunk)

		if _, sendErr := host.send(printer.Fingerprint(), "START_INLINE_DOWNLOAD", map[string]any{
			"path": "/usb/landing.bgcode", "hash": "0f0f", "team_id": 1, "orig_size": len(file),
		}); sendErr != nil {
			t.Fatalf("send START_INLINE_DOWNLOAD: %v", sendErr)
		}

		awaitEvent(t, messages, "TRANSFER_INFO")

		first := awaitBlockRequest(t, messages)
		if got := numberOf(first, "start"); got != 0 {
			t.Fatalf("the first block request starts at %d, want 0", got)
		}

		if chunkErr := host.sendChunk(printer.Fingerprint(),
			uint32(numberOf(first, "file_id")), file[:chunk]); chunkErr != nil {
			t.Fatalf("serve the first chunk: %v", chunkErr)
		}

		// Nobody pulled a cable: this is the printer ending the session itself.
		awaitSession(t, sessions, printer.Fingerprint())

		second := awaitBlockRequest(t, messages)

		if numberOf(second, "file_id") == numberOf(first, "file_id") {
			t.Errorf("the resumed transfer asks under file_id %d again, and the firmware mints a new one",
				numberOf(second, "file_id"))
		}

		if got := numberOf(second, "start"); got != chunk {
			t.Errorf("the resumed transfer asks from byte %d, and %d bytes had arrived", got, chunk)
		}

		for at := chunk; at < len(file); at += chunk {
			if chunkErr := host.sendChunk(printer.Fingerprint(),
				uint32(numberOf(second, "file_id")), file[at:at+chunk]); chunkErr != nil {
				t.Fatalf("serve the chunk at %d: %v", at, chunkErr)
			}
		}

		awaitEvent(t, messages, "TRANSFER_FINISHED")

		landed, fetched := printer.LastTransfer()
		if !fetched || !landed.Finished {
			t.Fatalf("the printer did not finish the transfer: %+v", landed)
		}

		t.Logf("observed: %d bytes arrived across the printer's own reset, digest %s",
			landed.Received, landed.Digest)

		if landed.Received != int64(len(file)) {
			t.Errorf("the printer received %d bytes, and the file is %d", landed.Received, len(file))
		}

		if landed.Digest != digestOf(file) {
			t.Errorf("the file that survived the reset is not the one sent: %s, want %s",
				landed.Digest, digestOf(file))
		}

		if landed.Path != "/usb/landing.bgcode" {
			t.Errorf("the file landed at %q, want /usb/landing.bgcode", landed.Path)
		}

		if got, want := printer.Sessions(), uint64(2); got != want {
			t.Errorf("the printer established %d sessions, and one drop per transfer is %d", got, want)
		}

		cancel()
		<-done
		host.stop()
	})
}

// A file that fits in one chunk still reports TRANSFER_FINISHED, on the
// connection after the drop.
//
// It is the ordering the drop can destroy: the last byte and the drop arrive in
// the same frame, and a printer that ended the transfer before dropping would
// send its one completion event into a socket nobody can read. Nothing on the
// wire replaces it, so the server would wait for a transfer that is over.
func TestAFileThatFitsInOneChunkStillReportsItFinished(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		host := newHost(t)
		host.onMessage = transferMessages(messages)
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		printer, err := simulator.New(simulator.Config{
			Model:      "MK4",
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: time.Hour,
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		<-sessions

		file := sampleFile(600)

		if _, sendErr := host.send(printer.Fingerprint(), "START_INLINE_DOWNLOAD", map[string]any{
			"path": "/usb/small.bgcode", "hash": "0f0f", "team_id": 1, "orig_size": len(file),
		}); sendErr != nil {
			t.Fatalf("send START_INLINE_DOWNLOAD: %v", sendErr)
		}

		awaitEvent(t, messages, "TRANSFER_INFO")

		first := awaitBlockRequest(t, messages)

		if chunkErr := host.sendChunk(printer.Fingerprint(),
			uint32(numberOf(first, "file_id")), file); chunkErr != nil {
			t.Fatalf("serve the whole file in one chunk: %v", chunkErr)
		}

		awaitSession(t, sessions, printer.Fingerprint())

		awaitEvent(t, messages, "TRANSFER_FINISHED")

		landed, fetched := printer.LastTransfer()
		if !fetched || !landed.Finished || landed.Digest != digestOf(file) {
			t.Fatalf("the file did not land whole: %+v", landed)
		}

		t.Logf("observed: %d bytes and the drop in one frame, and the transfer still ended",
			landed.Received)

		cancel()
		<-done
		host.stop()
	})
}

// A transfer whose block request is answered with a chunk it did not ask for
// ends the connection, and the file id it did not ask under is the one the drop
// left behind.
//
// The reset mints a new file id, so the old one is a live way to reach
// takeTransferChunk with a frame for nothing. A simulator that kept answering to
// it would let a server serve a stale id for the length of a transfer.
func TestAChunkUnderTheFileIDTheResetRetiredEndsTheConnection(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		host := newHost(t)
		host.onMessage = transferMessages(messages)
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		printer, err := simulator.New(simulator.Config{
			Model:      "MK4",
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: time.Hour,
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		<-sessions

		const chunk = 1024

		file := sampleFile(4 * chunk)

		if _, sendErr := host.send(printer.Fingerprint(), "START_INLINE_DOWNLOAD", map[string]any{
			"path": "/usb/stale.bgcode", "hash": "0f0f", "team_id": 1, "orig_size": len(file),
		}); sendErr != nil {
			t.Fatalf("send START_INLINE_DOWNLOAD: %v", sendErr)
		}

		awaitEvent(t, messages, "TRANSFER_INFO")

		first := awaitBlockRequest(t, messages)
		retired := uint32(numberOf(first, "file_id"))

		if chunkErr := host.sendChunk(printer.Fingerprint(), retired, file[:chunk]); chunkErr != nil {
			t.Fatalf("serve the first chunk: %v", chunkErr)
		}

		awaitSession(t, sessions, printer.Fingerprint())

		second := awaitBlockRequest(t, messages)
		before := printer.Sessions()

		if chunkErr := host.sendChunk(printer.Fingerprint(), retired, file[chunk:2*chunk]); chunkErr != nil {
			t.Fatalf("serve a chunk under the retired file id: %v", chunkErr)
		}

		awaitSession(t, sessions, printer.Fingerprint())

		t.Logf("observed: a chunk under the retired file id %d ended session %d",
			retired, before)

		third := awaitBlockRequest(t, messages)
		if numberOf(third, "start") != chunk {
			t.Errorf("the transfer resumed from byte %d, and only %d bytes were ever accepted",
				numberOf(third, "start"), chunk)
		}

		if numberOf(third, "file_id") == numberOf(second, "file_id") {
			t.Errorf("the third connection asks under file_id %d again", numberOf(third, "file_id"))
		}

		cancel()
		<-done
		host.stop()
	})
}

// transferMessages collects the events and block requests of a transfer and
// drops the telemetry.
//
// Telemetry is not part of this story, and a printer sends it for as long as a
// test waits: a test that waits for a connection while telemetry fills the
// channel jams the host's reader, then the printer's writer, and the bubble
// deadlocks with nothing wrong with the printer.
func transferMessages(messages chan<- observed) func(string, map[string]any) {
	return func(fingerprint string, payload map[string]any) {
		_, isEvent := payload["event"]
		_, asking := payload["transfer"]

		if !isEvent && !asking {
			return
		}

		messages <- observed{fingerprint: fingerprint, payload: payload}
	}
}

// awaitSession waits for the printer's next connection.
//
// The deadline is virtual time, so it costs nothing and it never fires while the
// printer still has work to do. What it turns into a sentence is a printer that
// never drops the connection, which without it is a bubble deadlock and forty
// lines of stack.
func awaitSession(t *testing.T, sessions <-chan string, fingerprint string) {
	t.Helper()

	select {
	case got := <-sessions:
		if got != fingerprint {
			t.Fatalf("connected as %q, want %q", got, fingerprint)
		}
	case <-time.After(time.Minute):
		t.Fatal("the printer did not open another connection")
	}
}

// awaitBlockRequest waits for the printer's next block request. It is the one
// printer-to-server message that is neither an event nor telemetry, and the
// server classifies it by exactly the key matched here.
func awaitBlockRequest(t *testing.T, messages <-chan observed) map[string]any {
	t.Helper()

	for message := range messages {
		if _, asking := message.payload["transfer"]; asking {
			return message.payload
		}

		if event, ok := message.payload["event"]; ok {
			t.Fatalf("waiting for a block request, got %v", event)
		}
	}

	t.Fatal("the printer closed without asking for a block")

	return nil
}

// numberOf reads one number out of a printer-to-server message. JSON gives every
// number as a float64.
func numberOf(payload map[string]any, key string) int64 {
	value, _ := payload[key].(float64)

	return int64(value)
}

// sampleFile is a file whose every byte depends on where it is, so a transfer
// that repeated or reordered a block produces a different digest. A file of one
// repeated byte would not.
func sampleFile(size int) []byte {
	data := make([]byte, size)
	for at := range data {
		data[at] = byte(at*31 + at>>8)
	}

	return data
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
