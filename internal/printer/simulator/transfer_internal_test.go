package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
)

// Every field of a transfer is guarded by Printer.mu (transfer.go), and this is
// what holds that rule.
//
// The read loop counts chunks and looks the transfer up while the session's
// resume claims ranges and mints file ids, which is every way one transfer is
// reached, run at once. An unguarded count loses increments under this load, and
// the race detector names the field:
// `make test-race RACE_PACKAGES=./internal/printer/simulator/`.
func TestNoChunkIsLostWhenChunksAndBlockRequestsRunAtOnce(t *testing.T) {
	t.Parallel()

	const (
		counters = 8
		chunks   = 200
		payload  = 64
	)

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	total := int64(counters * chunks * payload)
	current := &transfer{
		fileID:   7919,
		path:     "/usb/contended.bgcode",
		size:     total,
		blockEnd: total - 1,
		digest:   sha256.New(),
	}

	printer.transfer = current
	printer.lastTransfer = Transfer{Path: current.path, Size: total}

	chunk := make([]byte, payload)

	var group sync.WaitGroup

	for range counters {
		group.Go(func() {
			for range chunks {
				printer.accountChunk(current, chunk)
			}
		})
	}

	group.Go(func() {
		for range counters * chunks {
			printer.claimBlock(current)
		}
	})

	// The resume mints a file id on every reconnection, which is the write the
	// lookup below races against. Zero is a file id this printer never mints, so
	// the lookup has an answer that holds whatever the resume has just written.
	group.Go(func() {
		for range counters * chunks {
			printer.mu.Lock()
			printer.sessions++
			printer.mu.Unlock()

			printer.beginResume()
		}
	})

	group.Go(func() {
		for range counters * chunks {
			if _, fetching := printer.transferFor(0); fetching {
				t.Error("a file id this printer never minted matched its transfer")

				return
			}
		}
	})

	group.Wait()

	if got := current.received; got != total {
		t.Errorf("the transfer counted %d bytes of the %d that arrived", got, total)
	}

	landed, fetched := printer.LastTransfer()
	if !fetched || landed.Received != total {
		t.Errorf("the printer reports %d bytes received, want %d", landed.Received, total)
	}

	// The digest is over the bytes and not over their order here, because every
	// chunk is the same chunk: what it catches is a write that went missing.
	expected := sha256.New()
	for range counters * chunks {
		expected.Write(chunk)
	}

	if got, want := hex.EncodeToString(current.digest.Sum(nil)),
		hex.EncodeToString(expected.Sum(nil)); got != want {
		t.Errorf("the digest is %s, and the bytes that arrived hash to %s", got, want)
	}
}

// A transfer that ended is not advanced by whoever was still holding it.
//
// Holding the pointer is not owning the transfer: a STOP_TRANSFER on the read
// loop can end one between a resume claiming its next range and the answer
// arriving. Every step is given a transfer the printer is no longer running here,
// and a step that acted on one would ask the server for a range of a stopped
// transfer, or plant the file and report it finished after the printer had
// already said it stopped. The nil connection is the assertion: a step that
// answers this one writes to the wire and panics.
func TestATransferThatEndedIsNotAdvancedByItsHolder(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	stopped := &transfer{fileID: 7919, path: "/usb/stopped.bgcode", size: 4096, digest: sha256.New()}

	if _, state := printer.claimBlock(stopped); state != blockAbandoned {
		t.Errorf("claimBlock answered %d for a transfer nobody is running, want blockAbandoned", state)
	}

	if finishErr := printer.finishTransfer(nil, stopped); finishErr != nil {
		t.Errorf("finishTransfer: %v", finishErr)
	}

	if abortErr := printer.abortTransfer(nil, stopped); abortErr != nil {
		t.Errorf("abortTransfer: %v", abortErr)
	}

	if _, fetched := printer.LastTransfer(); fetched {
		t.Error("a transfer nobody is running was reported as what the printer last fetched")
	}

	if children := printer.fileChildren("/usb"); listingHolds(children, stopped.path) {
		t.Errorf("the file of a stopped transfer was planted: %+v", children)
	}
}

// The connection that accepted a transfer does not resume it, and the one after
// it does.
//
// A session starts its resume and its read loop together, so the resume can
// reach a transfer the read loop has only just accepted. Renaming that one would
// retire the file id the block request had already gone out under, and would pay
// the drop of ErrTransferReset with a drop that never happened.
func TestOnlyTheConnectionAfterTheOneThatAcceptedATransferResumesIt(t *testing.T) {
	t.Parallel()

	printer, err := New(Config{Model: "MK4"})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	const accepted = 7919

	printer.sessions = 1
	printer.nextFileID = accepted
	printer.transfer = &transfer{
		fileID: accepted, session: 1, size: 4096, digest: sha256.New(), resetOwed: true,
	}

	if _, _, fetching := printer.beginResume(); fetching {
		t.Error("the connection that accepted the transfer resumed it as well")
	}

	if got := printer.transfer.fileID; got != accepted {
		t.Errorf("the transfer was renamed to %d under the block request already sent as %d",
			got, accepted)
	}

	if !printer.transfer.resetOwed {
		t.Error("the drop was spent on the connection that accepted the transfer, so none follows")
	}

	printer.sessions = 2

	resumed, _, fetching := printer.beginResume()
	if !fetching {
		t.Fatal("the connection after the drop did not resume the transfer")
	}

	if resumed.fileID == accepted {
		t.Errorf("the resumed transfer asks under file_id %d again", resumed.fileID)
	}

	if resumed.resetOwed {
		t.Error("the transfer still owes a drop after one was performed")
	}
}

func listingHolds(entries []File, path string) bool {
	for _, entry := range entries {
		if entry.Path == path {
			return true
		}
	}

	return false
}
