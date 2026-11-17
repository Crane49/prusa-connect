package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// blockBytes is the span one block request covers. It is the simulator's own
// figure, written here because a test that asks for the next block has to know
// where the last one ended.
const blockBytes = 1 << 18

// pacedTransfer is one transfer between a test host and a printer, past the
// drop the printer owes for accepting it, with the second block still to come.
type pacedTransfer struct {
	host     *host
	printer  *simulator.Printer
	messages chan observed
	file     []byte
	fileID   uint32
	stop     func()
}

// startPacedTransfer offers the printer a file two blocks long and serves the
// whole first block, the drop and the resume included. hold, when set, runs
// once the resumed block request has arrived and before the block is served.
func startPacedTransfer(t *testing.T, cfg simulator.Config, hold func(*simulator.Printer)) *pacedTransfer {
	t.Helper()

	messages := make(chan observed, 64)
	sessions := make(chan string, 8)

	host := newHost(t)
	host.onMessage = transferMessages(messages)
	host.onSession = func(fingerprint string) { sessions <- fingerprint }

	cfg.Model = "MK4"
	cfg.BaseURL = host.baseURL()
	cfg.Dial = host.dial
	cfg.IdlePeriod = time.Hour

	printer, err := simulator.New(cfg)
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

	file := sampleFile(blockBytes + 2*chunk)

	if _, sendErr := host.send(printer.Fingerprint(), "START_INLINE_DOWNLOAD", map[string]any{
		"path": "/usb/paced.bgcode", "hash": "0f0f", "team_id": 1, "orig_size": len(file),
	}); sendErr != nil {
		t.Fatalf("send START_INLINE_DOWNLOAD: %v", sendErr)
	}

	awaitEvent(t, messages, "TRANSFER_INFO")

	first := awaitBlockRequest(t, messages)
	if chunkErr := host.sendChunk(printer.Fingerprint(),
		uint32(numberOf(first, "file_id")), file[:chunk]); chunkErr != nil {
		t.Fatalf("serve the first chunk: %v", chunkErr)
	}

	awaitSession(t, sessions, printer.Fingerprint())

	resumed := awaitBlockRequest(t, messages)
	fileID := uint32(numberOf(resumed, "file_id"))

	if hold != nil {
		hold(printer)
	}

	for at := int(numberOf(resumed, "start")); at <= int(numberOf(resumed, "end")); at += chunk {
		if chunkErr := host.sendChunk(printer.Fingerprint(), fileID, file[at:at+chunk]); chunkErr != nil {
			t.Fatalf("serve the chunk at %d: %v", at, chunkErr)
		}
	}

	return &pacedTransfer{
		host: host, printer: printer, messages: messages, file: file, fileID: fileID,
		stop: func() {
			cancel()
			<-done
			host.stop()
		},
	}
}

// finish serves the block the printer asked for last and waits for the file
// to land whole.
func (p *pacedTransfer) finish(t *testing.T, request map[string]any) {
	t.Helper()

	from, to := int(numberOf(request, "start")), int(numberOf(request, "end"))
	if chunkErr := p.host.sendChunk(p.printer.Fingerprint(),
		uint32(numberOf(request, "file_id")), p.file[from:to+1]); chunkErr != nil {
		t.Fatalf("serve the last block: %v", chunkErr)
	}

	awaitEvent(t, p.messages, "TRANSFER_FINISHED")

	landed, fetched := p.printer.LastTransfer()
	if !fetched || !landed.Finished || !landed.Landed || landed.Digest != digestOf(p.file) {
		t.Fatalf("the file did not land whole: %+v", landed)
	}
}

// A held transfer asks for nothing more once its block is in, and asks for the
// next block as soon as it is released. A test ends the server's side of a
// transfer part way with this, at the point it chose.
func TestAHeldTransferAsksForNoBlockUntilReleased(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		transfer := startPacedTransfer(t, simulator.Config{}, (*simulator.Printer).HoldTransfer)
		defer transfer.stop()

		synctest.Wait()

		if !transfer.printer.TransferWaiting() {
			t.Fatal("the held transfer is not waiting at the end of its block")
		}

		select {
		case message := <-transfer.messages:
			t.Fatalf("a held transfer said %v", message.payload)
		case <-time.After(time.Hour):
		}

		if err := transfer.printer.ReleaseTransfer(); err != nil {
			t.Fatalf("release: %v", err)
		}

		next := awaitBlockRequest(t, transfer.messages)
		// The resume asked from the byte after the first chunk, so the block
		// after it starts one block further on.
		if got, want := numberOf(next, "start"), int64(1024+blockBytes); got != want {
			t.Fatalf("the released transfer asks from byte %d, want %d", got, want)
		}

		if transfer.printer.TransferWaiting() {
			t.Fatal("the released transfer still says it is waiting")
		}

		transfer.finish(t, next)
	})
}

// A printer given a rate asks for the next block once the last one would have
// taken that long to arrive, and not before.
func TestATransferRatePacesTheBlockRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const rate = blockBytes / 4 // four seconds a block

		transfer := startPacedTransfer(t, simulator.Config{TransferRate: rate}, nil)
		defer transfer.stop()

		served := time.Now()
		next := awaitBlockRequest(t, transfer.messages)
		waited := time.Since(served)

		t.Logf("observed: the next block was asked for %s after the last one arrived", waited)

		if waited < 4*time.Second || waited >= 5*time.Second {
			t.Fatalf("the next block was asked for after %s, and the rate gives four seconds a block", waited)
		}

		transfer.finish(t, next)
	})
}
