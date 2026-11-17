package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// filesChanged is spelled out for the reason printerSampled is.
const filesChanged = "com.prusa.connect.device.files.changed"

// slowListing is how long each printer here takes to list a folder. An MK4S
// takes 12 to 15 s for a USB root of about 300 files. This is shorter to keep
// the lane fast, and longer than answerBound, so a route that waited for the
// printer fails the bound.
const slowListing = 4 * time.Second

// answerBound is how long a route may take while a listing is on the wire:
// the server's one-second grace (ADR-20261112-wfc) and room for a loaded CI
// machine.
const answerBound = 2500 * time.Millisecond

// listingRoot is the file the simulator's default tree holds at the root of
// its USB storage.
const listingRoot = benchStorage + "/tangle.bgcode"

// listingState is the part of a listing answer these cases read.
type listingState struct {
	Reading bool `json:"reading"`
	Items   []struct {
		Path string `json:"path"`
	} `json:"items"`
}

func (l listingState) paths() []string {
	out := make([]string, 0, len(l.Items))
	for _, item := range l.Items {
		out = append(out, item.Path)
	}

	return out
}

// timedGet is one GET and how long it took.
func (s *server) timedGet(t *testing.T, path string) (response, time.Duration) {
	t.Helper()

	start := time.Now()
	answer := s.get(t, path)

	return answer, time.Since(start)
}

func decodeListing(t *testing.T, answer response) listingState {
	t.Helper()

	if answer.status != http.StatusOK {
		t.Fatalf("the listing answered %d: %s", answer.status, answer.body)
	}

	var state listingState
	if err := json.Unmarshal(answer.body, &state); err != nil {
		t.Fatalf("parse the listing %s: %v", answer.body, err)
	}

	return state
}

// TestASlowPrinterListingNeverBlocksARequest runs ADR-20261112-wfc against a
// printer that lists a folder as slowly as a real one and answers no other
// command meanwhile, as Buddy firmware does.
func TestASlowPrinterListingNeverBlocksARequest(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	// Each case runs its own printer, and a serial of its own keeps them apart:
	// two printers with one serial are one printer to the server.
	slowPrinter := func(serial string) simulator.Config {
		return simulator.Config{Model: benchModel, Serial: serial, ListingDelay: slowListing}
	}

	t.Run("a cold listing answers at once and the rows follow on the event stream", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), settleBudget)
		defer cancel()

		frames, errs := server.subscribe(ctx, t)

		_, listed := server.attachPrinterWith(t, slowPrinter("SIM-LIST-COLD"))

		answer, took := server.timedGet(t, "/api/v1/devices/"+listed.ID+"/files?path="+benchStorage)
		if took > answerBound {
			t.Fatalf("the cold listing took %s, want at most %s", took, answerBound)
		}

		if !decodeListing(t, answer).Reading {
			t.Fatalf("the cold listing does not say reading: %s", answer.body)
		}

		t.Logf("observed: the cold listing answered in %s: %s", took, answer.body)

		landed := awaitFrame(ctx, t, frames, errs, func(f frame) bool {
			if f.event != filesChanged {
				return false
			}

			var envelope struct {
				Data struct {
					DeviceID string `json:"device_id"`
					Path     string `json:"path"`
					Reading  bool   `json:"reading"`
					Failed   bool   `json:"failed"`
				} `json:"data"`
			}

			if err := json.Unmarshal([]byte(f.data), &envelope); err != nil {
				t.Fatalf("the files.changed frame is not an envelope: %v", err)
			}

			data := envelope.Data

			return data.DeviceID == listed.ID && data.Path == benchStorage && !data.Reading && !data.Failed
		})

		t.Logf("observed: %s", landed.data)

		rows := decodeListing(t, server.get(t, "/api/v1/devices/"+listed.ID+"/files?path="+benchStorage))
		if rows.Reading || !slices.Contains(rows.paths(), listingRoot) {
			t.Fatalf("after the read landed the listing is reading=%t with %v, want %s",
				rows.Reading, rows.paths(), listingRoot)
		}
	})

	t.Run("storages with refresh answers while a listing is on the wire", func(t *testing.T) {
		t.Parallel()

		printer, listed := server.attachPrinterWith(t, slowPrinter("SIM-LIST-STORAGES"))

		// The first read lands, so the printer has reported its storages.
		server.deviceListing(t, listed.ID, benchStorage)

		// A folder nobody has read yet is read at once, which puts a listing on
		// the wire. A refresh of rows already held would not: they are fresh.
		before := printer.Asked("SEND_FILE_INFO")

		cold := decodeListing(t, server.get(t, "/api/v1/devices/"+listed.ID+"/files?path="+benchStorage+"/examples"))
		if !cold.Reading {
			t.Fatalf("a folder nobody read does not say reading: %v", cold)
		}

		eventually(t, "the read reached the printer", func() bool {
			return printer.Asked("SEND_FILE_INFO") > before
		})

		answer, took := server.timedGet(t, "/api/v1/devices/"+listed.ID+"/storages?refresh=true")
		if answer.status != http.StatusOK {
			t.Fatalf("storages with refresh answered %d after %s: %s", answer.status, took, answer.body)
		}

		if took > answerBound {
			t.Fatalf("storages with refresh took %s, want at most %s", took, answerBound)
		}

		if !strings.Contains(string(answer.body), benchStorage) {
			t.Fatalf("storages with refresh does not name %s: %s", benchStorage, answer.body)
		}

		t.Logf("observed: storages with refresh answered 200 in %s: %s", took, answer.body)
	})

	t.Run("a send lands while the background read of the root is on the wire", func(t *testing.T) {
		t.Parallel()

		const source = "e2e-listing.bgcode"

		destination := benchStorage + "/" + source

		saved := server.upload(t, source, sampleFile(4<<10).data)
		if saved.status != http.StatusOK && saved.status != http.StatusCreated {
			t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
		}

		printer, listed := server.attachPrinterWith(t, slowPrinter("SIM-LIST-SEND"))

		// Nothing here has asked for a listing, so the one the printer was
		// asked is the server's own warm-up of the root.
		eventually(t, "the warm-up read reached the printer", func() bool {
			return printer.Asked("SEND_FILE_INFO") > 0
		})

		started := time.Now()

		answer, sent := server.sendFile(t, listed.ID, source, destination)
		if answer.status != http.StatusOK {
			t.Fatalf("the send answered %d: %s", answer.status, answer.body)
		}

		t.Logf("observed: the send answered %s in %s, state %s", answer.body, time.Since(started), sent.State)

		if paths := devicePaths(t, server, listed.ID, benchStorage); !slices.Contains(paths, destination) {
			t.Fatalf("the sent file is not in the listing of %s: %v", benchStorage, paths)
		}
	})
}
