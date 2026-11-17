package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The cases below send a temporary copy from a real core to a simulated
// printer, and then read the storage's disk to see whether the copy is still
// there. A file a person kept is checked the same way.

// uploadTemporary puts one file into the storage as a temporary copy and
// returns the path the server chose.
func (b *relayBench) uploadTemporary(t *testing.T, name string, body []byte) string {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		"http://"+b.addr+"/api/v1/storages/"+relayStorage+"/content?temporary=true&path="+url.QueryEscape(name),
		bytes.NewReader(body))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/octet-stream")

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	read, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode, string(read))

	var saved struct {
		Path      string `json:"path"`
		Temporary bool   `json:"temporary"`
	}

	require.NoError(t, json.Unmarshal(read, &saved))
	require.True(t, saved.Temporary, string(read))
	require.True(t, strings.HasPrefix(saved.Path, incomingDir+"/"), saved.Path)

	return saved.Path
}

// stored reports whether the storage's disk holds a path.
func (b *relayBench) stored(name string) bool {
	_, err := os.Stat(filepath.Join(b.root, filepath.FromSlash(name)))

	return err == nil
}

// requireGone waits for a temporary copy to leave the storage's disk.
func (b *relayBench) requireGone(t *testing.T, source string) {
	t.Helper()

	require.Eventually(t, func() bool {
		return !b.stored(copyFolder(source))
	}, settle, 25*time.Millisecond, "the temporary copy %s is still in the storage", source)
}

// A temporary copy whose file landed on the printer is deleted.
func TestATemporaryCopyIsDeletedOnceTheFileLands(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-TEMP-DONE")
	body := relayFile(4000, "done")
	source := bench.uploadTemporary(t, "done.gcode", body)

	got := <-bench.send(t.Context(), source, "usb/done.gcode", false)
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, got.body)

	bench.requireWhole(t, "usb/done.gcode", body)
	bench.requireGone(t, source)
}

// A temporary copy whose transfer a person stopped is deleted.
func TestATemporaryCopyIsDeletedWhenItsTransferIsStopped(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-TEMP-STOP")
	body := relayFile(40000, "stop")
	source := bench.uploadTemporary(t, "stop.gcode", body)

	bench.printer.HoldTransfer()

	answer := bench.send(t.Context(), source, "usb/stop.gcode", false)

	bench.held(t)
	require.True(t, bench.stored(source), "the copy went before its relay ended")

	running := bench.transfers(t)
	require.NotEmpty(t, running)

	stop := <-bench.post(t.Context(),
		"/api/v1/devices/"+bench.id+"/transfers/"+running[0].TransferID+"/stop", map[string]any{})
	require.NoError(t, stop.err)
	require.Less(t, stop.status, 300, stop.body)

	got := <-answer
	require.NoError(t, got.err)
	require.NotEqual(t, http.StatusOK, got.status, got.body)

	require.NoError(t, bench.printer.ReleaseTransfer())

	bench.requireGone(t, source)
	bench.requireNone(t, "usb/stop.gcode")
}

// A temporary copy refused because the printer holds the name stays while
// the page asks the person, and the replacing send then lands it and deletes
// it.
func TestATemporaryCopyWaitsForTheAnswerToATakenName(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-TEMP-TAKEN")

	older := relayFile(3000, "older")
	bench.upload(t, "older.gcode", older)

	got := <-bench.send(t.Context(), "older.gcode", "usb/part.gcode", false)
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, got.body)
	bench.requireWhole(t, "usb/part.gcode", older)

	newer := relayFile(5000, "newer")
	source := bench.uploadTemporary(t, "part.gcode", newer)

	refused := <-bench.send(t.Context(), source, "usb/part.gcode", false)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusConflict, refused.status, refused.body)
	require.True(t, bench.stored(source), "the copy went while the person was still being asked")

	replaced := <-bench.send(t.Context(), source, "usb/part.gcode", true)
	require.NoError(t, replaced.err)
	require.Equal(t, http.StatusOK, replaced.status, replaced.body)

	bench.requireWhole(t, "usb/part.gcode", newer)
	bench.requireGone(t, source)

	require.True(t, bench.stored("older.gcode"), "a file uploaded to the storage was deleted after its send")
}

// A file the person chose to keep stays in the folder they chose after it is
// sent.
func TestAKeptFileStaysInItsFolderAfterTheSend(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-TEMP-KEPT")
	body := relayFile(4000, "kept")
	bench.upload(t, "models/kept.gcode", body)

	got := <-bench.send(t.Context(), "models/kept.gcode", "usb/kept.gcode", false)
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, got.body)

	bench.requireWhole(t, "usb/kept.gcode", body)

	require.Never(t, func() bool {
		return !bench.stored("models/kept.gcode")
	}, time.Second, 50*time.Millisecond, "the kept file left its folder after the send")
}

// A temporary copy left behind when the server stops is deleted once it has
// started again.
func TestACopyLeftBeforeARestartIsDeletedAfterIt(t *testing.T) {
	t.Parallel()

	bench := newRelayBench(t, "SIM-TEMP-RESTART")
	source := bench.uploadTemporary(t, "left.gcode", relayFile(100, "left"))
	bench.upload(t, "kept.gcode", relayFile(100, "kept"))

	require.NoError(t, bench.running.wait())
	require.True(t, bench.stored(source), "the copy went before the restart")

	bench.running = bench.launch(t)

	bench.requireGone(t, source)
	require.True(t, bench.stored("kept.gcode"), "a kept file went with the leftovers")

	listed := bench.running.get(t, "/api/v1/storages/"+relayStorage+"/files")
	require.Equal(t, http.StatusOK, listed.status, listed.body)
	require.NotContains(t, listed.body, incomingDir, "the storage listing shows the temporary area")
}
