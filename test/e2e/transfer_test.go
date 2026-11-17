package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The three types one transfer publishes. They are spelled out rather than
// imported for the reason printerSampled is: a subscriber outside this process
// reads them off the wire, and a constant shared with the publisher would make
// a rename invisible to exactly the readers it breaks.
const (
	transferStarted    = "com.prusa.connect.device.transfer.started"
	transferProgressed = "com.prusa.connect.device.transfer.progressed"
	transferSettled    = "com.prusa.connect.device.transfer.settled"
)

// scratchStorage is the staging storage every case here uploads into, declared
// beside the file-backed one the harness always writes.
const scratchStorage = "scratch"

// scratchResources declares that storage in a directory of the caller's, which
// the server creates and owns for the length of the run.
//
// It is not `type: memory`, the store that keeps its objects in the server's
// own heap: that store is examples/adapters/storage-memory (ADR-20260901-7t4), outside
// the core module, and a binary built from the shipped configuration does not
// know the name, so these cases name the staging store instead.
//
// It is worth being exact about what the cases prove. The bytes are not
// unreachable: they are in a directory on the machine the server runs on. The
// proof is not that the printer could not reach the bytes some other way. It
// is that this store hands out no download URL, `temporary` is disk-backed and
// implements no storageapi.Presigner, so the only way the bytes the printer
// digests can have got there is the core reading the object and relaying it
// over the printer's own connection. A store that presigned would end the
// proof; a store on disk does not.
func scratchResources(dir string) string {
	return strings.Join([]string{
		"    " + scratchStorage + ":",
		"      type: temporary",
		"      enabled: true",
		"      config:",
		"        path: " + quotedPath(dir),
	}, "\n")
}

// quotedPath renders a directory for the YAML the harness writes: forward
// slashes, which the Go file API accepts on every platform, inside single
// quotes so nothing left in the value reads as an escape.
func quotedPath(dir string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(dir), "'", "''") + "'"
}

// awaitStorage blocks until one storage instance is serving.
//
// Readiness does not cover it: this instance is declared optional, so the
// process reports itself ready while the instance is still starting, and an
// upload sent into that window is refused as not serving.
func (s *server) awaitStorage(t *testing.T) {
	t.Helper()

	name := scratchStorage

	eventually(t, "the "+name+" storage started serving", func() bool {
		listed := s.get(t, "/api/v1/storages")
		if listed.status != http.StatusOK {
			return false
		}

		var body struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}

		if err := json.Unmarshal(listed.body, &body); err != nil {
			t.Fatalf("parse the storage listing %s: %v", listed.body, err)
		}

		for _, item := range body.Items {
			if item.Name == name {
				return true
			}
		}

		return false
	})
}

// transferDestination is where a transferred file lands on the printer, in the
// storage-prefixed spelling the send route takes. The extension is one the
// built-in list accepts, because a storage refuses an upload it does not know
// the extension of.
const transferDestination = benchStorage + "/e2e-transfer.bgcode"

// sample is a body and the digest the test itself took of it.
type sample struct {
	data   []byte
	digest string
}

// sampleFile builds a deterministic body of the given length. It is
// pseudo-random from a fixed seed rather than a run of zeros, so a digest that
// matches is a digest of these bytes and not of any body of this length.
func sampleFile(size int) sample {
	source := rand.NewChaCha8([32]byte{'p', 'c', 'l', 'e', '2', 'e'})
	data := make([]byte, size)

	if _, err := io.ReadFull(source, data); err != nil {
		panic("read the sample body: " + err.Error())
	}

	sum := sha256.Sum256(data)

	return sample{data: data, digest: hex.EncodeToString(sum[:])}
}

// upload writes a body into a storage and returns the answer, which carries the
// storage's own digest of what it wrote.
func (s *server) upload(t *testing.T, path string, body []byte) response {
	t.Helper()

	storage := scratchStorage

	ctx, cancel := context.WithTimeout(t.Context(), requestBudget)
	defer cancel()

	target := s.baseURL + "/api/v1/storages/" + storage + "/content?path=" + url.QueryEscape(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build the upload: %v", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := (&http.Client{Timeout: requestBudget}).Do(req)
	if err != nil {
		t.Fatalf("upload %d bytes into %s: %v", len(body), storage, err)
	}

	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the upload's answer: %v", err)
	}

	return response{status: resp.StatusCode, body: payload, header: resp.Header}
}

// grantHistorySight gives the history provider sight of one printer, so the
// frames it publishes reach the store and the projection folds them. A provider
// is a principal of its own and holds only what it was granted, and the grant
// that carries a device's frames is written per device here for the reason
// TestACommandIsReadableFromTheStoredPast records.
func (s *server) grantHistorySight(t *testing.T, printer string) {
	t.Helper()

	grant := s.do(t, http.MethodPost, "/api/v1/grants/device/"+printer,
		`{"relation":"manager","subject":"history:past"}`, nil)
	if grant.status != http.StatusOK && grant.status != http.StatusCreated {
		t.Fatalf("granting the provider answered %d: %s", grant.status, grant.body)
	}
}

// awaitHistorySight waits until the granted provider is actually being
// delivered this printer's frames.
//
// The grant alone is not enough. A subscription is authorized on the pump when
// the graph's epoch moves, so a frame published between writing the grant and
// that pass reaches the provider never: nothing replays a frame a subscriber
// was not given. The cheap verb is spent here until one of its frames is in the
// store, so the transfer that follows is not the thing that falls in the
// window.
func (s *server) awaitHistorySight(t *testing.T, printer string) {
	t.Helper()

	eventually(t, "the history provider was delivered this printer's frames", func() bool {
		answer := s.do(t, http.MethodPost, "/api/v1/devices/"+printer+"/commands",
			fmt.Sprintf(`{"verb":%q}`, sendInfo), nil)
		if answer.status != http.StatusOK {
			t.Fatalf("POST a command answered %d: %s", answer.status, answer.body)
		}

		// One flush interval, so the poll is not asking about a batch the feed
		// has not written yet.
		time.Sleep(historyFlush)

		page := s.get(t, "/api/v1/history/facts?origins=device:"+printer+"&types="+commandSettled)

		return page.status == http.StatusOK && strings.Contains(string(page.body), commandSettled)
	})
}

// transferRow is one row of the transfer listing and of the send route's
// answer. It is read off the wire, so it is declared here rather than imported.
type transferRow struct {
	TransferID string `json:"transfer_id"`
	Storage    string `json:"storage"`
	Source     string `json:"source"`
	Path       string `json:"path"`
	BytesTotal int64  `json:"bytes_total"`
	BytesSent  int64  `json:"bytes_sent"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
}

// sendFile posts one send and returns the transfer the server answered with.
// The request returns when the transfer does, so it is given settleBudget: a
// transfer takes as long as the file is long, and every file here is short.
//
// The storage is scratchStorage and is not a parameter: it is the one staging
// storage the fixture declares, so every case here uploads into it.
func (s *server) sendFile(t *testing.T, printer, source, path string) (response, transferRow) {
	t.Helper()

	answer := s.doWithin(t, settleBudget, http.MethodPost, "/api/v1/devices/"+printer+"/files",
		fmt.Sprintf(`{"storage":%q,"source":%q,"path":%q}`, scratchStorage, source, path), nil)

	if answer.status != http.StatusOK {
		return answer, transferRow{}
	}

	var body struct {
		Path     string      `json:"path"`
		Transfer transferRow `json:"transfer"`
	}

	if err := json.Unmarshal(answer.body, &body); err != nil {
		t.Fatalf("parse the send's answer %s: %v", answer.body, err)
	}

	return answer, body.Transfer
}

// transferListing reads the transfer listing of one printer.
func (s *server) transferListing(t *testing.T, printer string) []transferRow {
	t.Helper()

	listed := s.get(t, "/api/v1/devices/"+printer+"/transfers")
	if listed.status != http.StatusOK {
		t.Fatalf("the transfer listing answered %d: %s", listed.status, listed.body)
	}

	var body struct {
		Items []transferRow `json:"items"`
	}

	if err := json.Unmarshal(listed.body, &body); err != nil {
		t.Fatalf("parse the transfer listing %s: %v", listed.body, err)
	}

	return body.Items
}

// TestAFileUploadedToScratchStorageReachesAPrinterThroughTheCore is the whole
// claim in one case: a file this deployment holds arrives on a printer byte for
// byte.
//
// It proves the bytes went through the core because of what the storage holding
// them will not do. The scratch store implements no storageapi.Presigner, so
// there is no URL a printer could be handed, and a driver is never given a
// storage provider handle, so there is no path it could open for itself. The
// only route from the upload to the printer's digest is the core reading the
// object and relaying it over the printer's own connection. Two independent
// readings are compared at each end:
// the storage's digest against the test's, and the printer's digest over what
// arrived against the same. The proof stops holding the day a driver is handed
// a URL, or a storage provider handle reaches a driver, because then the bytes
// could reach the printer without this server ever seeing them.
func TestAFileUploadedToScratchStorageReachesAPrinterThroughTheCore(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	const uploaded = 200 << 10

	file := sampleFile(uploaded)
	source := "e2e-transfer.bgcode"

	saved := server.upload(t, source, file.data)
	if saved.status != http.StatusOK && saved.status != http.StatusCreated {
		t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
	}

	stored := saved.decode(t)
	if stored["sha256"] != file.digest {
		t.Fatalf("the storage read the upload as %v, and the test read it as %s: %s",
			stored["sha256"], file.digest, saved.body)
	}

	t.Logf("observed: the upload answered %s", saved.body)

	entry := server.get(t, "/api/v1/storages/"+scratchStorage+"/stat?path="+url.QueryEscape(source))
	if entry.status != http.StatusOK {
		t.Fatalf("the stat answered %d: %s", entry.status, entry.body)
	}

	if described := entry.decode(t); described["sha256"] != file.digest {
		t.Fatalf("the stored entry reports digest %v, want %s: %s",
			described["sha256"], file.digest, entry.body)
	}

	printer, listed := server.attachPrinter(t, benchModel)

	answer, sent := server.sendFile(t, listed.ID, source, transferDestination)
	if answer.status != http.StatusOK {
		t.Fatalf("sending the file answered %d: %s", answer.status, answer.body)
	}

	// What the printer made of the bytes. The simulator drops its connection
	// once for having accepted a transfer (simulator.ErrTransferReset) and the
	// resume finishes the file, so the count and the digest here span two
	// connections.
	landed, fetched := printer.LastTransfer()
	if !fetched {
		t.Fatalf("the send returned and the printer fetched nothing: %s", answer.body)
	}

	if !landed.Finished {
		t.Fatalf("the send returned before the printer reported the transfer finished: %+v", landed)
	}

	if landed.Received != uploaded {
		t.Errorf("the printer received %d bytes, and the file is %d", landed.Received, uploaded)
	}

	if landed.Digest != file.digest {
		t.Errorf("the printer received a different file: digest %s, want %s", landed.Digest, file.digest)
	}

	// The core's own count of what it relayed, taken independently of the
	// printer's.
	//
	// BytesSent is what this server put on the wire, so it is the file's length
	// plus whatever was in flight when the printer dropped the connection it
	// owes for having accepted the transfer: the resume asks from the byte the
	// printer had, and the ranges after it go out twice. What must hold is that
	// nothing short of the file was written and that the resume did not send it
	// all again.
	if sent.BytesTotal != uploaded {
		t.Errorf("the core measured the file as %d bytes, and it is %d", sent.BytesTotal, uploaded)
	}

	if sent.BytesSent < uploaded || sent.BytesSent >= 2*uploaded {
		t.Errorf("the core relayed %d bytes for a file of %d", sent.BytesSent, uploaded)
	}

	if sent.State != "done" {
		t.Errorf("the transfer ended in state %q, want done: %s", sent.State, answer.body)
	}

	t.Logf("observed: %d bytes reached %s with digest %s, and the core relayed %d of %d",
		landed.Received, landed.Path, landed.Digest, sent.BytesSent, sent.BytesTotal)
}

// TestATransferIsOnTheListingAndOnTheEventStream is the other half of what a
// person watching a transfer has: the frames while it moves, and the row after
// it ended.
func TestATransferIsOnTheListingAndOnTheEventStream(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir())+"\n"+historyResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	const uploaded = 200 << 10

	file := sampleFile(uploaded)
	source := "e2e-listed.bgcode"

	saved := server.upload(t, source, file.data)
	if saved.status != http.StatusOK && saved.status != http.StatusCreated {
		t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
	}

	ctx, cancel := context.WithTimeout(t.Context(), settleBudget)
	defer cancel()

	// The subscription is opened before the transfer, because nothing replays a
	// frame to a subscriber that was not there for it.
	frames, errs := server.subscribe(ctx, t)

	_, listed := server.attachPrinter(t, benchModel)
	server.grantHistorySight(t, listed.ID)
	server.awaitHistorySight(t, listed.ID)

	answer, sent := server.sendFile(t, listed.ID, source, transferDestination)
	if answer.status != http.StatusOK {
		t.Fatalf("sending the file answered %d: %s", answer.status, answer.body)
	}

	// Started and settled are the two durable frames, so both must arrive. The
	// progressed frame is throttled at 250 ms and a file this size can finish
	// inside one interval, so it is reported and never required.
	for _, want := range []string{transferStarted, transferSettled} {
		got := awaitFrame(ctx, t, frames, errs, func(f frame) bool {
			return f.event == want && strings.Contains(f.data, sent.TransferID)
		})

		t.Logf("observed frame: %s", got.data)
	}

	select {
	case f := <-frames:
		if f.event == transferProgressed {
			t.Logf("observed: the transfer also published %s", transferProgressed)
		}
	default:
	}

	// The listing answers from the register while a transfer runs and from the
	// history projection once it has settled, and the projection is written on
	// the feed's own flush cadence, so the settled row is waited for.
	var row transferRow

	eventually(t, "the settled transfer reached the listing", func() bool {
		for _, item := range server.transferListing(t, listed.ID) {
			if item.TransferID == sent.TransferID && item.State == "done" {
				row = item

				return true
			}
		}

		return false
	})

	// The bytes are bounded rather than equal for the reason the acceptance
	// case records: the ranges in flight when the printer dropped its
	// connection are written twice.
	if row.BytesTotal != uploaded {
		t.Errorf("the listed transfer measures the file as %d bytes, and it is %d",
			row.BytesTotal, uploaded)
	}

	if row.BytesSent < uploaded || row.BytesSent >= 2*uploaded {
		t.Errorf("the listed transfer relayed %d bytes for a file of %d", row.BytesSent, uploaded)
	}

	if row.Path != transferDestination {
		t.Errorf("the listed transfer landed at %q, want %q", row.Path, transferDestination)
	}

	t.Logf("observed: the listing carries %s as %s, %d of %d bytes",
		row.TransferID, row.State, row.BytesSent, row.BytesTotal)
}

// TestATransferThatIsStoppedIsRecordedCancelled is the stop route against a
// transfer that is still moving: the request that began it fails, and the
// record says a person ended it rather than that the server broke.
func TestATransferThatIsStoppedIsRecordedCancelled(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir())+"\n"+historyResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	// Large enough that the transfer is still running when the stop arrives.
	// The host writes defaultChunkWriteCeiling bytes a frame, so this is
	// thousands of frames and the window is not a sleep.
	const uploaded = 8 << 20

	file := sampleFile(uploaded)
	source := "e2e-stopped.bgcode"

	saved := server.upload(t, source, file.data)
	if saved.status != http.StatusOK && saved.status != http.StatusCreated {
		t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
	}

	_, listed := server.attachPrinter(t, benchModel)
	server.grantHistorySight(t, listed.ID)
	server.awaitHistorySight(t, listed.ID)

	// The send blocks for as long as the transfer runs, so it is on a goroutine
	// of its own and the stop is issued from the test's.
	type outcome struct {
		status int
		body   []byte
	}

	sends := make(chan outcome, 1)

	go func() {
		answer := server.doWithin(t, 2*time.Minute, http.MethodPost,
			"/api/v1/devices/"+listed.ID+"/files",
			fmt.Sprintf(`{"storage":%q,"source":%q,"path":%q}`,
				scratchStorage, source, benchStorage+"/e2e-stopped.bgcode"), nil)

		sends <- outcome{status: answer.status, body: answer.body}
	}()

	var running transferRow

	eventually(t, "the transfer reported itself sending", func() bool {
		for _, item := range server.transferListing(t, listed.ID) {
			if item.State == "sending" {
				running = item

				return true
			}
		}

		return false
	})

	stopped := server.do(t, http.MethodPost,
		"/api/v1/devices/"+listed.ID+"/transfers/"+running.TransferID+"/stop", "", nil)
	if stopped.status != http.StatusNoContent {
		t.Fatalf("stopping the transfer answered %d: %s", stopped.status, stopped.body)
	}

	select {
	case result := <-sends:
		if result.status == http.StatusOK {
			t.Fatalf("the stopped send answered %d: %s", result.status, result.body)
		}

		t.Logf("observed: the stopped send answered %d: %s", result.status, result.body)
	case <-time.After(settleBudget):
		t.Fatalf("the send did not return within %s of the stop", settleBudget)
	}

	var final transferRow

	eventually(t, "the stopped transfer was recorded", func() bool {
		for _, item := range server.transferListing(t, listed.ID) {
			if item.TransferID == running.TransferID && item.State != "sending" {
				final = item

				return true
			}
		}

		return false
	})

	if final.State != "cancelled" {
		t.Fatalf("the stopped transfer was recorded %q, want cancelled: %+v", final.State, final)
	}

	t.Logf("observed: %s ended %s after %d of %d bytes, reason %q",
		final.TransferID, final.State, final.BytesSent, final.BytesTotal, final.Reason)
}

// TestAPrintStartsOnTheFileThatWasTransferred is the reason a file is sent at
// all: the path the transfer landed on is a path the printer will print.
func TestAPrintStartsOnTheFileThatWasTransferred(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	const uploaded = 64 << 10

	file := sampleFile(uploaded)
	source := "e2e-printed.bgcode"

	saved := server.upload(t, source, file.data)
	if saved.status != http.StatusOK && saved.status != http.StatusCreated {
		t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
	}

	printer, listed := server.attachPrinter(t, benchModel)

	destination := benchStorage + "/e2e-printed.bgcode"

	answer, sent := server.sendFile(t, listed.ID, source, destination)
	if answer.status != http.StatusOK {
		t.Fatalf("sending the file answered %d: %s", answer.status, answer.body)
	}

	if landed, fetched := printer.LastTransfer(); !fetched || !landed.Finished {
		t.Fatalf("the print has nothing to run on: %+v", landed)
	}

	started := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
		fmt.Sprintf(`{"verb":%q,"args":{"path":%q}}`, startPrintVerb, destination), nil)
	if started.status != http.StatusOK {
		t.Fatalf("starting a print on the transferred file answered %d: %s", started.status, started.body)
	}

	if started.decode(t)["ok"] != true {
		t.Fatalf("the printer refused to print the file it was just sent: %s", started.body)
	}

	// The state and not the 200 is what says the print began: a 200 alone is
	// the queue accepting the command.
	eventually(t, "the printer reported that it is printing the transferred file", func() bool {
		return stateOf(t, server.printer(t, listed.ID)) == "PRINTING"
	})

	t.Logf("observed: transfer %s landed at %s and the printer is PRINTING it",
		sent.TransferID, destination)
}

// TestAJobRecordTracksTheTransferredPrint is the record of a print that this
// tree does not yet keep. When the job entity exists, this case asserts that
// starting a print on a transferred file opens a job row naming the transfer's
// destination path, and that the row follows the print to completion.
func TestAJobRecordTracksTheTransferredPrint(t *testing.T) {
	t.Skip("TODO(session-02): the job record does not exist yet. When session 02 lands the job " +
		"entity, this asserts that starting a print on a transferred file opens a job row that " +
		"names the transfer's destination path and follows the print to completion.")
}

// The two destinations BUG-014 was found with. Both long names shorten to the
// FAT 8.3 spelling BUG009~1.GCO, so a stick that holds the first has nowhere
// to put the second.
const (
	collidingFirst  = benchStorage + "/bug009-probe.gcode"
	collidingSecond = benchStorage + "/bug009-control.gcode"
)

// TestATransferWhoseFileNeverLandedIsNotAnsweredDone is BUG-014 at the API
// boundary, which is where it was reported.
//
// On a CORE One L at 6.8.1+16182 both sends answered `state: done`,
// `bytes_sent == bytes_total`, and the printer's own listing held only the
// first of the two names, in three readings 3 to 20 seconds apart. So a caller
// was told twice that a file it can print is on the stick, and one of the two
// files was nowhere.
func TestATransferWhoseFileNeverLandedIsNotAnsweredDone(t *testing.T) {
	t.Parallel()

	server := startServerWith(t, scratchResources(t.TempDir()))
	server.awaitReady(t)
	server.awaitStorage(t)

	file := sampleFile(8490)
	source := "e2e-colliding.bgcode"

	saved := server.upload(t, source, file.data)
	if saved.status != http.StatusOK && saved.status != http.StatusCreated {
		t.Fatalf("the upload answered %d: %s", saved.status, saved.body)
	}

	_, listed := server.attachPrinter(t, benchModel)

	first, sent := server.sendFile(t, listed.ID, source, collidingFirst)
	if first.status != http.StatusOK {
		t.Fatalf("sending the first file answered %d: %s", first.status, first.body)
	}

	if sent.State != "done" {
		t.Fatalf("the first transfer answered %q, and its file is on the stick", sent.State)
	}

	second, _ := server.sendFile(t, listed.ID, source, collidingSecond)
	if second.status != http.StatusConflict {
		t.Fatalf("sending a file that lands nowhere answered %d: %s", second.status, second.body)
	}

	var problem struct {
		Code   string            `json:"code"`
		Params map[string]string `json:"params"`
	}

	if err := json.Unmarshal(second.body, &problem); err != nil {
		t.Fatalf("parse the refusal %s: %v", second.body, err)
	}

	if problem.Code != "core.printer/TRANSFER_NOT_ON_STORAGE" {
		t.Errorf("the refusal carries %q, want core.printer/TRANSFER_NOT_ON_STORAGE", problem.Code)
	}

	t.Logf("observed: the second send answered %d %s with %v",
		second.status, problem.Code, problem.Params)

	for name, want := range map[string]string{
		"path":          collidingSecond,
		"expected_size": "8490",
		"found":         "absent",
	} {
		if problem.Params[name] != want {
			t.Errorf("the refusal reports %s = %q, want %q", name, problem.Params[name], want)
		}
	}

	// And the printer's own listing is what the answer now agrees with.
	paths := devicePaths(t, server, listed.ID, benchStorage)

	if !slices.Contains(paths, collidingFirst) {
		t.Errorf("the first file is not in the printer's listing of %s: %v", benchStorage, paths)
	}

	if slices.Contains(paths, collidingSecond) {
		t.Errorf("the second file is in the listing, so this case proves nothing: %v", paths)
	}
}

// devicePaths is one directory of a printer's own tree, as paths.
func devicePaths(t *testing.T, s *server, printer, directory string) []string {
	t.Helper()

	listing := s.deviceListing(t, printer, directory)

	var body struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
	}

	if err := json.Unmarshal(listing.body, &body); err != nil {
		t.Fatalf("parse the listing of %s: %v", directory, err)
	}

	out := make([]string, 0, len(body.Items))
	for _, entry := range body.Items {
		out = append(out, entry.Path)
	}

	return out
}
