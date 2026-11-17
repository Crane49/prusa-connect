package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deadlineWriter records what a handler did to the connection's deadlines.
//
// It is the writer the handler is given rather than a fake of the controller,
// because http.NewResponseController reaches the connection through exactly
// these two methods: a handler that moved a deadline some other way would not be
// moving the one the server armed.
type deadlineWriter struct {
	http.ResponseWriter

	reads  []time.Time
	writes []time.Time
	fail   error
}

func (w *deadlineWriter) SetReadDeadline(at time.Time) error {
	w.reads = append(w.reads, at)

	return nil
}

func (w *deadlineWriter) SetWriteDeadline(at time.Time) error {
	w.writes = append(w.writes, at)

	return nil
}

func (w *deadlineWriter) Write(p []byte) (int, error) {
	if w.fail != nil {
		return 0, w.fail
	}

	return w.ResponseWriter.Write(p) //nolint:wrapcheck // the recorder's own error, unaltered.
}

// serveRecordingDeadlines runs one request and returns what the handler did to
// the deadlines beside the response it wrote.
func serveRecordingDeadlines(
	handler http.Handler, req *http.Request,
) (*deadlineWriter, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	writer := &deadlineWriter{ResponseWriter: rec}

	handler.ServeHTTP(writer, req)

	return writer, rec
}

// cleared reports whether the zero time, which is no deadline at all, is among
// the values that were set.
func cleared(set []time.Time) bool {
	for _, at := range set {
		if at.IsZero() {
			return true
		}
	}

	return false
}

// armed reports whether a real deadline is among the values that were set.
func armed(set []time.Time) bool {
	for _, at := range set {
		if !at.IsZero() {
			return true
		}
	}

	return false
}

// An upload lifts both connection deadlines and re-arms both as the bytes
// arrive.
//
// The write deadline is the half that was missing: the server arms WriteTimeout
// when the request arrives, so an upload that took longer than it landed on
// disk and then failed to answer, which the client sees as a reset rather than
// as the success it was.
func TestAnUploadMovesBothConnectionDeadlines(t *testing.T) {
	t.Parallel()

	stores := &fakeStorages{}
	handler := serverWithStores(t, stores).Handler()

	req := httptest.NewRequest(http.MethodPut,
		storagesPathPrefix+"/bench/content?path=part.gcode", strings.NewReader("G28\nG1 X10\n"))

	writer, rec := serveRecordingDeadlines(handler, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "G28\nG1 X10\n", string(stores.saved))

	require.True(t, cleared(writer.reads), "the read deadline was never lifted")
	require.True(t, armed(writer.reads), "the read deadline was never re-armed per chunk")
	require.True(t, cleared(writer.writes), "the write deadline was never lifted")
	require.True(t, armed(writer.writes), "the write deadline was never re-armed per chunk")

	t.Logf("observed: %d read deadlines and %d write deadlines moved over one upload",
		len(writer.reads), len(writer.writes))
}

// Sending a file to a printer lifts the write deadline before the transfer
// starts.
//
// There is nothing to re-arm: the bytes leave over the printer's own
// connection, so this handler makes no progress an idle window could measure
// and the response is written only when the printer says the file arrived.
func TestSendingAFileToAPrinterLiftsTheWriteDeadline(t *testing.T) {
	t.Parallel()

	var atSend []time.Time

	fleet := &fakeFleet{printer: Device{ID: "pknown"}}
	writer := &deadlineWriter{}

	hooked := sendingFleet{fakeFleet: fleet, onSend: func() { atSend = writer.writes }}
	handler := serverWithFleet(t, hooked).Handler()

	req := httptest.NewRequest(http.MethodPost, devicesPath+"/pknown/files",
		strings.NewReader(`{"storage":"bench","source":"part.gcode","path":"usb/part.gcode"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	writer.ResponseWriter = rec
	handler.ServeHTTP(writer, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fleet.sent, 1)
	require.True(t, cleared(atSend), "the transfer started with the server's write deadline still armed")

	t.Logf("observed: write deadlines %v were in place when the transfer began", atSend)
}

// sendingFleet is a fleet that says when the transfer started, so the deadline
// can be read at that moment rather than after the response.
type sendingFleet struct {
	*fakeFleet

	onSend func()
}

func (f sendingFleet) Send(
	ctx context.Context, principal string, id string, upload DeviceUpload,
) (DeviceTransfer, error) {
	f.onSend()

	return f.fakeFleet.Send(ctx, principal, id, upload)
}

// A HEAD is answered with the headers a GET would carry and without reading
// the object: the router sees a GET, so serveDownload has to restore the method
// or ServeContent copies the whole file into a body net/http then discards.
func TestAHeadDownloadAnswersTheHeadersWithoutReadingTheObject(t *testing.T) {
	t.Parallel()

	body := "0123456789"

	served := &fakeStorages{object: body}
	handler := serverWithStores(t, served).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead,
		storagesPathPrefix+"/bench/content?path=part.gcode", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, strconv.Itoa(len(body)), rec.Header().Get("Content-Length"))
	require.Empty(t, rec.Body.String())
	require.Zero(t, served.reads, "a HEAD read the object")
	require.Equal(t, 1, served.closes, "the object was not closed after a HEAD")
}

// A download answers a Range request from the standard library and closes the
// object either way: when the client read all of it, and when the client went
// away part of the way through.
func TestADownloadAnswersARangeAndClosesTheObject(t *testing.T) {
	t.Parallel()

	body := "0123456789"

	served := &fakeStorages{object: body}
	handler := serverWithStores(t, served).Handler()

	req := httptest.NewRequest(http.MethodGet, storagesPathPrefix+"/bench/content?path=part.gcode", nil)
	req.Header.Set("Range", "bytes=2-5")

	_, rec := serveRecordingDeadlines(handler, req)

	require.Equal(t, http.StatusPartialContent, rec.Code)
	require.Equal(t, "2345", rec.Body.String())
	require.Equal(t, "bytes 2-5/10", rec.Header().Get("Content-Range"))
	require.Equal(t, 1, served.closes, "the object was not closed after a range was served")

	// The client that goes away mid-body. The write fails, ServeContent stops,
	// and the handle still has to be given back: a download that leaked one
	// would hold a file open for as long as the process lives.
	aborted := &fakeStorages{object: body}
	handler = serverWithStores(t, aborted).Handler()

	rec = httptest.NewRecorder()
	gone := &deadlineWriter{ResponseWriter: rec, fail: errors.New("the client went away")}

	handler.ServeHTTP(gone, httptest.NewRequest(http.MethodGet,
		storagesPathPrefix+"/bench/content?path=part.gcode", nil))

	require.Equal(t, 1, aborted.closes, "the object was not closed after the client went away")

	t.Logf("observed: closed on both paths, %d bytes served as %d", served.closes, rec.Code)
}
