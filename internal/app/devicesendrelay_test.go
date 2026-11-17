package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The cases below send a real file from a storage on a real core to a
// simulated printer over the real wire, and end the caller's side part way.
// Each one ends by reading what the printer holds: the whole file, compared
// byte for byte through its digest, or no file at all. A part of a file is
// never an acceptable end.

// relayStorage is the storage every case uploads into.
const relayStorage = "local"

// relayBench is one core with a storage and one simulated printer on it.
type relayBench struct {
	dir     string
	root    string
	addr    string
	running *process
	printer *simulator.Printer
	id      string
}

// relayConfig declares the one storage the cases upload into, and a history
// that keeps how each transfer ended once it is no longer running.
func relayConfig(dir, root string) string {
	return lines(
		"core:",
		"  log:",
		"    level: error",
		"resources:",
		"  storage:",
		"    "+relayStorage+":",
		"      type: filesystem",
		"      enabled: true",
		"      required: true",
		"      config:",
		"        root: "+slashed(root),
		"  history:",
		"    past:",
		"      type: sqlite",
		"      config:",
		"        path: "+slashed(historyFile(dir)),
		"        retention:",
		`          raw: "0"`,
	)
}

func newRelayBench(t *testing.T, serial string) *relayBench {
	t.Helper()

	dir := t.TempDir()
	bench := &relayBench{dir: dir, root: filepath.Join(dir, "storage"), addr: freeAddr(t)}

	write(t, filepath.Join(dir, "config.yaml"), relayConfig(dir, bench.root))
	recordOpenSetup(t, dir)

	bench.running = bench.launch(t)

	// The history stores what it sees from the moment it is attached, so the
	// printer dials in after it is serving.
	require.Eventually(t, func() bool {
		return bench.running.instanceState(t, "storage/"+relayStorage) == "ready" &&
			bench.running.instanceState(t, "history/past") == "ready"
	}, settle, 20*time.Millisecond, "the storage or the history never became ready")

	bench.printer = dialInPrinter(t, bench.running, serial)
	bench.id = printers(t, bench.running)[0].ID

	return bench
}

// launch runs a core over the bench's directory, on the bench's own address,
// so a printer that dialled the core before a restart finds it again after.
func (b *relayBench) launch(t *testing.T) *process {
	t.Helper()

	running := launchOptions(t, Options{
		ConfigPath:   filepath.Join(b.dir, "config.yaml"),
		APIAddr:      b.addr,
		Version:      "test",
		Stderr:       io.Discard,
		LoopbackOnly: true,
	})
	waitUntilReady(t, running)

	return running
}

// upload puts one file into the storage through the API.
func (b *relayBench) upload(t *testing.T, name string, body []byte) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		"http://"+b.addr+"/api/v1/storages/"+relayStorage+"/content?path="+url.QueryEscape(name),
		bytes.NewReader(body))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/octet-stream")

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode, string(read))
}

// sent is what one send request answered, or the error a caller that left got.
type sent struct {
	status int
	body   string
	err    error
}

// post sends a JSON body under ctx on a goroutine of its own, so a case can
// leave while the answer is still on its way.
func (b *relayBench) post(ctx context.Context, path string, body any) <-chan sent {
	out := make(chan sent, 1)

	go func() {
		encoded, err := json.Marshal(body)
		if err != nil {
			out <- sent{err: err}

			return
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+b.addr+path,
			bytes.NewReader(encoded))
		if err != nil {
			out <- sent{err: err}

			return
		}

		request.Header.Set("Content-Type", "application/json")

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			out <- sent{err: err}

			return
		}

		read, err := io.ReadAll(response.Body)
		_ = response.Body.Close()

		out <- sent{status: response.StatusCode, body: string(read), err: err}
	}()

	return out
}

// send asks the core to put a stored file on the printer.
func (b *relayBench) send(ctx context.Context, source, path string, replace bool) <-chan sent {
	return b.post(ctx, "/api/v1/devices/"+b.id+"/files", map[string]any{
		"storage": relayStorage, "source": source, "path": path, "replace": replace,
	})
}

// transfers is the printer's transfer list, newest first.
func (b *relayBench) transfers(t *testing.T) []httpapi.DeviceTransfer {
	t.Helper()

	read := b.running.get(t, "/api/v1/devices/"+b.id+"/transfers")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page struct {
		Items []httpapi.DeviceTransfer `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page.Items
}

// settledTransfer waits for the newest transfer to end and returns it.
func (b *relayBench) settledTransfer(t *testing.T) httpapi.DeviceTransfer {
	t.Helper()

	var last httpapi.DeviceTransfer

	require.Eventually(t, func() bool {
		listed := b.transfers(t)
		if len(listed) == 0 {
			return false
		}

		last = listed[0]

		return last.State != "queued" && last.State != "sending"
	}, settle, 25*time.Millisecond, "the transfer never ended")

	return last
}

// held waits until the printer has stopped at the end of a block, which is a
// transfer that has begun and has bytes still to come.
func (b *relayBench) held(t *testing.T) {
	t.Helper()

	require.Eventually(t, b.printer.TransferWaiting, settle, 10*time.Millisecond,
		"the printer never began fetching the file")
}

// onPrinter is the entry the printer lists at path, and false when there is
// none. The listing is read fresh, so it is the printer's word and not a copy:
// the route answers a refresh at once, and the read it starts is waited out.
func (b *relayBench) onPrinter(t *testing.T, path string) (httpapi.DeviceFile, bool) {
	t.Helper()

	folder := path[:strings.LastIndex(path, "/")]
	query := "refresh=true&path=" + url.QueryEscape(folder)

	var listing httpapi.DeviceListing

	require.Eventually(t, func() bool {
		read := b.running.get(t, "/api/v1/devices/"+b.id+"/files?"+query)
		require.Equal(t, http.StatusOK, read.status, read.body)

		// Reset, because reading is omitted once false and would keep the
		// previous answer's value.
		listing = httpapi.DeviceListing{}
		require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

		query = "path=" + url.QueryEscape(folder)

		return !listing.Reading
	}, settle, 10*time.Millisecond, "the printer never finished listing %s", folder)

	for _, entry := range listing.Items {
		if strings.EqualFold(entry.Path, path) {
			return entry, true
		}
	}

	return httpapi.DeviceFile{}, false
}

// requireWhole proves the printer holds exactly body at path: every byte the
// printer took, in order, matches, and the file is on its storage.
func (b *relayBench) requireWhole(t *testing.T, path string, body []byte) {
	t.Helper()

	require.Eventually(t, func() bool {
		landed, fetched := b.printer.LastTransfer()

		return fetched && landed.Finished
	}, settle, 10*time.Millisecond, "the printer never finished the file")

	landed, _ := b.printer.LastTransfer()
	t.Logf("observed on the printer: %d of %d bytes, digest %s, landed %t",
		landed.Received, landed.Size, landed.Digest, landed.Landed)

	require.True(t, landed.Landed, "the printer reported the file finished and it is not on its storage")
	require.Equal(t, "/"+path, landed.Path)
	require.Equal(t, int64(len(body)), landed.Received)
	require.Equal(t, digestOfBytes(body), landed.Digest, "the printer holds different bytes from the ones sent")

	entry, found := b.onPrinter(t, path)
	require.True(t, found, "the printer's own listing does not hold %s", path)
	require.Equal(t, int64(len(body)), entry.Size)
}

// requireNone proves the printer holds nothing at path and never finished a
// file there.
func (b *relayBench) requireNone(t *testing.T, path string) {
	t.Helper()

	landed, _ := b.printer.LastTransfer()
	t.Logf("observed on the printer: %d of %d bytes, finished %t, landed %t",
		landed.Received, landed.Size, landed.Finished, landed.Landed)

	require.False(t, landed.Finished, "the printer finished a file that was stopped")
	require.False(t, landed.Landed)

	_, found := b.onPrinter(t, path)
	require.False(t, found, "the printer lists a part of a file at %s", path)
}

// relayFile is a print file several blocks long whose every line differs, so
// a block served twice or out of order changes the digest.
func relayFile(lines int, label string) []byte {
	var out bytes.Buffer

	fmt.Fprintf(&out, "; %s\n", label)

	for at := range lines {
		fmt.Fprintf(&out, "G1 X%d.%03d Y%d.%03d E0.0%d ; %s\n", at%250, at%997, (at*7)%210, at%991, at%10, label)
	}

	return out.Bytes()
}

func digestOfBytes(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

// The page that sent a file is closed while the file is on its way. Before the
// fix the request's context ended the relay there, and the printer was told to
// stop. The file now arrives whole.
func TestAFileStillArrivesWholeWhenItsCallerLeavesMidRelay(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-LEAVE")
	body := relayFile(40000, "leave")
	bench.upload(t, "leave.gcode", body)

	bench.printer.HoldTransfer()

	caller, leave := context.WithCancel(t.Context())
	answer := bench.send(caller, "leave.gcode", "usb/leave.gcode", false)

	bench.held(t)
	leave()

	got := <-answer
	require.ErrorIs(t, got.err, context.Canceled, "the caller did not leave before the answer")

	running := bench.transfers(t)
	require.NotEmpty(t, running)
	require.Equal(t, "sending", running[0].State, "the transfer ended with the caller that started it")

	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireWhole(t, "usb/leave.gcode", body)

	last := bench.settledTransfer(t)
	require.Equal(t, "done", last.State, last.Reason)
	require.Equal(t, int64(len(body)), last.BytesTotal)
}

// A person who presses stop still ends a transfer, and the printer holds no
// part of the file afterwards.
func TestAStoppedRelayLeavesNoFileOnThePrinter(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-STOP")
	body := relayFile(40000, "stop")
	bench.upload(t, "stop.gcode", body)

	bench.printer.HoldTransfer()

	answer := bench.send(t.Context(), "stop.gcode", "usb/stop.gcode", false)

	bench.held(t)

	running := bench.transfers(t)
	require.NotEmpty(t, running)

	stopped := bench.post(t.Context(),
		"/api/v1/devices/"+bench.id+"/transfers/"+running[0].TransferID+"/stop", map[string]any{})
	stop := <-stopped
	require.NoError(t, stop.err)
	require.Less(t, stop.status, 300, stop.body)

	got := <-answer
	require.NoError(t, got.err)
	require.NotEqual(t, http.StatusOK, got.status, "a stopped transfer was answered as sent: %s", got.body)

	last := bench.settledTransfer(t)
	require.Equal(t, "cancelled", last.State, last.Reason)

	// The printer asks for its next block once released, and is told nothing,
	// because the transfer it asks about is over on both sides.
	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireNone(t, "usb/stop.gcode")
}

// The printer's connection drops while a file is on its way and the caller has
// already left. The printer takes the transfer up again on its next connection
// and the file arrives whole.
func TestARelayResumesWholeAcrossAPrinterThatDropsItsConnection(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-DROP")
	body := relayFile(40000, "drop")
	bench.upload(t, "drop.gcode", body)

	bench.printer.HoldTransfer()

	caller, leave := context.WithCancel(t.Context())
	answer := bench.send(caller, "drop.gcode", "usb/drop.gcode", false)

	bench.held(t)
	leave()
	<-answer

	sessions := bench.printer.Sessions()

	bench.printer.GoOffline()
	bench.printer.GoOnline()

	require.Eventually(t, func() bool {
		return bench.printer.Sessions() > sessions && bench.printer.TransferWaiting()
	}, settle, 10*time.Millisecond, "the printer never took the transfer up again")

	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireWhole(t, "usb/drop.gcode", body)
	require.Equal(t, "done", bench.settledTransfer(t).State)
}

// The server stops while a file is on its way. The transfer is recorded as
// interrupted when it starts again, and the printer, which asks again for a
// transfer the new process does not know, holds no part of the file.
func TestARelayInterruptedByARestartLeavesNoFileOnThePrinter(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-RESTART")
	body := relayFile(40000, "restart")
	bench.upload(t, "restart.gcode", body)

	bench.printer.HoldTransfer()

	caller, leave := context.WithCancel(t.Context())
	answer := bench.send(caller, "restart.gcode", "usb/restart.gcode", false)

	bench.held(t)
	leave()
	<-answer

	sessions := bench.printer.Sessions()

	// The process stops the way a signal stops it, and stops cleanly: the
	// request the caller left is over, so the listener drains at once, and the
	// relay ends when the printer's session is closed.
	require.NoError(t, bench.running.wait())

	bench.running = bench.launch(t)

	require.Eventually(t, func() bool {
		return bench.printer.Sessions() > sessions && bench.printer.TransferWaiting()
	}, settle, 10*time.Millisecond, "the printer never came back to the restarted server")

	// A stop by signal closes the printer's session first, so the relay ends
	// as failed with the reason that says so. A process that dies instead
	// leaves it to the next start, which records it as interrupted.
	last := bench.settledTransfer(t)
	require.Contains(t, []string{"failed", "interrupted"}, last.State, last.Reason)
	require.Less(t, last.BytesSent, last.BytesTotal)

	require.NoError(t, bench.printer.ReleaseTransfer())

	// The printer's request names a transfer the new process never offered, and
	// the answer to that is silence. Nothing may land in the time a block takes.
	require.Never(t, func() bool {
		landed, _ := bench.printer.LastTransfer()

		return landed.Finished || landed.Landed
	}, time.Second, 50*time.Millisecond, "a file the restarted server never served landed on the printer")

	bench.requireNone(t, "usb/restart.gcode")
}

// A second send to a printer already fetching a file is refused, and the first
// one still arrives whole.
func TestASecondSendWhileOneIsOnItsWayIsRefused(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-TWICE")
	body := relayFile(40000, "twice")
	bench.upload(t, "twice.gcode", body)

	bench.printer.HoldTransfer()

	first := bench.send(t.Context(), "twice.gcode", "usb/twice.gcode", false)

	bench.held(t)

	second := <-bench.send(t.Context(), "twice.gcode", "usb/twice.gcode", false)
	require.NoError(t, second.err)
	t.Logf("observed: the second send answered %d %s", second.status, second.body)
	require.Equal(t, http.StatusConflict, second.status, second.body)

	require.NoError(t, bench.printer.ReleaseTransfer())

	got := <-first
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, got.body)

	bench.requireWhole(t, "usb/twice.gcode", body)
}

// A send that replaces a file the printer holds, whose caller then leaves,
// still leaves the new file whole on the printer and not the old one, and not
// a part of either.
func TestAReplacingSendWhoseCallerLeavesLandsTheNewFileWhole(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-REPLACE")

	older := relayFile(3000, "older")
	bench.upload(t, "older.gcode", older)

	got := <-bench.send(t.Context(), "older.gcode", "usb/part.gcode", false)
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, got.body)
	bench.requireWhole(t, "usb/part.gcode", older)

	newer := relayFile(40000, "newer")
	bench.upload(t, "newer.gcode", newer)

	refused := <-bench.send(t.Context(), "newer.gcode", "usb/part.gcode", false)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusConflict, refused.status, refused.body)
	require.Contains(t, refused.body, "FILE_NAME_TAKEN")

	bench.printer.HoldTransfer()

	caller, leave := context.WithCancel(t.Context())
	answer := bench.send(caller, "newer.gcode", "usb/part.gcode", true)

	bench.held(t)
	leave()
	<-answer

	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireWhole(t, "usb/part.gcode", newer)
}

// A print asked for by a page that closes while the file moves still starts,
// on the whole file.
func TestAPrintStartsOnTheWholeFileAfterItsCallerLeft(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-PRINT")
	body := relayFile(40000, "print")
	bench.upload(t, "print.gcode", body)

	bench.printer.HoldTransfer()

	caller, leave := context.WithCancel(t.Context())
	answer := bench.post(caller, "/api/v1/devices/"+bench.id+"/print", map[string]any{
		"storage": relayStorage, "source": "print.gcode", "path": "usb/print.gcode",
	})

	bench.held(t)
	leave()
	<-answer

	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireWhole(t, "usb/print.gcode", body)

	require.Eventually(t, func() bool {
		return bench.printer.State() == "PRINTING"
	}, settle, 25*time.Millisecond, "the print never started on the file that arrived")
}

// A client that goes away part way through an upload leaves nothing in the
// storage: no file under the name, and no part of one beside it.
func TestAnUploadCutOffPartWayLeavesNothingInTheStorage(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-RELAY-UPLOAD")

	reader, writer := io.Pipe()
	caller, leave := context.WithCancel(t.Context())

	request, err := http.NewRequestWithContext(caller, http.MethodPut,
		"http://"+bench.addr+"/api/v1/storages/"+relayStorage+"/content?path=cut.gcode", reader)
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/octet-stream")
	request.ContentLength = 1 << 20

	done := make(chan error, 1)

	go func() {
		response, doErr := http.DefaultClient.Do(request)
		if doErr == nil {
			_ = response.Body.Close()
		}

		done <- doErr
	}()

	_, err = writer.Write(relayFile(2000, "cut"))
	require.NoError(t, err)

	leave()
	_ = writer.CloseWithError(context.Canceled)

	require.Error(t, <-done)

	// The server learns of the drop when its read fails, so the name is checked
	// until the abort has had its chance, and must never appear.
	require.Never(t, func() bool {
		read := bench.running.try(t, http.MethodGet, "/api/v1/storages/"+relayStorage+"/files?path=cut.gcode", "")

		return read != nil && read.status == http.StatusOK
	}, time.Second, 50*time.Millisecond, "the storage published a part of an upload")

	entries, err := os.ReadDir(bench.root)
	require.NoError(t, err)

	for _, entry := range entries {
		t.Errorf("the storage holds %s after an upload that was cut off", entry.Name())
	}
}
