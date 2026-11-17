package httpapi

// The four device operations the add-a-device and device pages need: naming a
// machine, scrolling its finished prints, narrowing and ordering the fleet, and
// retiring a machine somebody took away.
//
// Each case drives the real router with the fixtures the published samples are
// built from, so what is compared is what a client would receive.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNamingADeviceWritesTheNameAndAnswersIt is the whole of what a rename
// promises a page: the value reaches the fleet, and the answer is what the
// device is called afterwards, so nothing has to be read back.
func TestNamingADeviceWritesTheNameAndAnswersIt(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodPut,
		"/api/v1/devices/"+examplePrinterID+"/name", `{"name":"Workshop CoreOne"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer struct {
		Name string `json:"name"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	require.Equal(t, "Workshop CoreOne", answer.Name)
	require.Equal(t, []string{"Workshop CoreOne"}, fleet.named,
		"the handler bound the body and did not hand it to the fleet")
}

// TestNamingADeviceThisServerDoesNotHoldIsNotFound is the refusal every device
// operation gives: a device the caller reaches nothing on and an id nothing has
// ever dialled under are the same answer.
func TestNamingADeviceThisServerDoesNotHoldIsNotFound(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, &fakeFleet{printer: examplePrinter()}).Handler()

	rec := call(t, handler, http.MethodPut, "/api/v1/devices/nobody/name", `{"name":"Bench"}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestTheJobHistoryPagesByTheCursorItAnswers holds the two halves of paging: the
// request's cursor reaches the fleet, and the page's own cursor is published for
// the caller to send back.
func TestTheJobHistoryPagesByTheCursorItAnswers(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), history: exampleDeviceJobHistory()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/devices/"+examplePrinterID+"/jobs/history?limit=50&cursor="+exampleJobCursor, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page DeviceJobPage

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.Equal(t, exampleJobCursor, page.Cursor)

	require.Equal(t, []DeviceJobQuery{{Limit: 50, Cursor: exampleJobCursor}}, fleet.jobQueries,
		"the handler dropped the page the caller asked for")
}

// TestEveryJobOnTheHistoryPageHasEnded is what separates this page from the jobs
// read. The print on the bed has no end, so it is that read's current and never
// a row here.
func TestEveryJobOnTheHistoryPageHasEnded(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), history: exampleDeviceJobHistory()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/devices/"+examplePrinterID+"/jobs/history", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var page DeviceJobPage

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.NotEmpty(t, page.Items)

	for _, job := range page.Items {
		require.NotNilf(t, job.Ended, "%s is on the history page and has not ended", job.ID)
	}
}

// TestTheListingBindsEveryFilterAndTheOrder reads the query the fleet was
// handed, which is what tells a handler that binds its parameters from one that
// publishes them and drops them.
func TestTheListingBindsEveryFilterAndTheOrder(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/devices?state=PRINTING,PAUSED&model=MK4S&search=brno"+
			"&retired=only&sort=name&order=desc&limit=25&offset=50", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, fleet.queries, 1)

	asked := fleet.queries[0]
	require.Equal(t, []string{"PRINTING", "PAUSED"}, asked.States)
	require.Equal(t, []string{"MK4S"}, asked.Models)
	require.Equal(t, "brno", asked.Search)
	require.Equal(t, "only", asked.Retired)
	require.Equal(t, "name", asked.Sort)
	require.Equal(t, "desc", asked.Order)
	require.Equal(t, 25, asked.Limit)
	require.Equal(t, 50, asked.Offset)
}

// TestTheListingRefusesASortFieldItDoesNotPublish is why the set is closed on
// the document. A caller who misspelled a field and was answered in the default
// order could not see that anything went wrong.
func TestTheListingRefusesASortFieldItDoesNotPublish(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, &fakeFleet{printer: examplePrinter()}).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/devices?sort=temperature", "")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestRetiringADeviceAnswersNoContentAndReachesTheFleet is the retirement over
// the wire. The answer carries no body, because the record is still there and
// the device's own read is where a client looks afterwards.
func TestRetiringADeviceAnswersNoContentAndReachesTheFleet(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodDelete, "/api/v1/devices/"+examplePrinterID, "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, rec.Body.String())
	require.Equal(t, []string{examplePrinterID}, fleet.retired)
}

// TestRetiringADeviceThisServerDoesNotHoldIsNotFound is the refusal, in the same
// bytes every other device operation answers an unknown id with.
func TestRetiringADeviceThisServerDoesNotHoldIsNotFound(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, &fakeFleet{printer: examplePrinter()}).Handler()

	rec := call(t, handler, http.MethodDelete, "/api/v1/devices/nobody", "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestTheModelVocabularyCarriesEveryProfiledModel is the join a page makes: it
// reads `model` off a device record and looks it up here, so a model the server
// can serve and the catalogue does not carry would leave a page with nothing to
// draw.
func TestTheModelVocabularyCarriesEveryProfiledModel(t *testing.T) {
	t.Parallel()

	handler := serverWithoutIdentity(t).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/vocabularies/device-models", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []deviceModelEntry `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Items)

	for _, entry := range body.Items {
		require.NotEmptyf(t, entry.Model, "a row carries no machine spelling")
		require.NotEmptyf(t, entry.DisplayName, "%s carries no name a person reads", entry.Model)
		require.NotEmptyf(t, entry.Icon, "%s carries no icon key", entry.Model)
		require.Equalf(t, "printer", entry.Kind, "%s is not a printer", entry.Model)
	}
}

// TestTheJobHistoryTakesARangeOfTime is the half of the page a report reads:
// from and to reach the fleet as the caller wrote them, so the narrowing
// happens in the query and not in the handler.
func TestTheJobHistoryTakesARangeOfTime(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), history: exampleDeviceJobHistory()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/devices/"+examplePrinterID+
			"/jobs/history?from=2026-09-01T00:00:00Z&to=2026-10-01T00:00:00Z", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, fleet.jobQueries, 1)
	require.Equal(t,
		time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), fleet.jobQueries[0].From.UTC())
	require.Equal(t,
		time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), fleet.jobQueries[0].To.UTC())
}

// A range that starts after it ends holds no print, so it is refused rather
// than answered with an empty page, which would read as a device that printed
// nothing.
func TestAJobHistoryRangeThatStartsAfterItEndsIsRefused(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter(), history: exampleDeviceJobHistory()}
	handler := serverWithFleet(t, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/devices/"+examplePrinterID+
			"/jobs/history?from=2026-10-01T00:00:00Z&to=2026-09-01T00:00:00Z", "")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Empty(t, fleet.jobQueries, "a range that holds no print reached the fleet")
}
