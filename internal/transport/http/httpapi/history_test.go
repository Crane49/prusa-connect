package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// recordingHistory answers a fixed page and keeps the query it was asked, which
// is what a test asserts against to tell a handler that reads its parameters
// from one that binds them and drops them.
type recordingHistory struct {
	facts   HistoryFactPage
	samples HistorySamplePage

	factQueries   []HistoryFactQuery
	sampleQueries []HistorySampleQuery
	principals    []string
}

func (h *recordingHistory) Facts(
	_ context.Context, principal string, query HistoryFactQuery,
) (HistoryFactPage, error) {
	h.factQueries = append(h.factQueries, query)
	h.principals = append(h.principals, principal)

	return h.facts, nil
}

func (h *recordingHistory) Samples(
	_ context.Context, principal string, query HistorySampleQuery,
) (HistorySamplePage, error) {
	h.sampleQueries = append(h.sampleQueries, query)
	h.principals = append(h.principals, principal)

	return h.samples, nil
}

// serverWithHistory is the surface over one history store and one printer, so
// the per-device paths have a device to resolve.
func serverWithHistory(t *testing.T, past HistoryStore, fleet DeviceFleet) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
		History:    fixedHistory(past),
	})
	require.NoError(t, err)

	return server
}

// fixedHistory is a resolver over one store, and nil for a deployment that
// keeps none: the two answers every case below is written against.
func fixedHistory(past HistoryStore) HistoryResolver {
	if past == nil {
		return nil
	}

	return func() (HistoryStore, error) { return past, nil }
}

// TestADeploymentWithNoHistoryProviderSaysSo holds an earlier design decision on the wire. A
// server that answered an empty page would tell a caller that nothing happened,
// when what is true is that nothing is kept.
func TestADeploymentWithNoHistoryProviderSaysSo(t *testing.T) {
	t.Parallel()

	handler := serverWithHistory(t, nil, noPrinters{}).Handler()

	for name, path := range map[string]string{
		"the fleet events":       historyFactsPath,
		"the fleet measurements": historySamplesPath,
		"one device's events":    devicesPath + "/" + examplePrinterID + "/history/facts",
		"one device's samples":   devicesPath + "/" + examplePrinterID + "/history/samples",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, http.MethodGet, path, "")

			require.Equal(t, http.StatusNotImplemented, rec.Code, "body: %s", rec.Body.String())
			require.Contains(t, rec.Body.String(), httpfault.CodeHistoryNotConfigured().String())
		})
	}
}

// TestAHistorySearchCarriesEveryParameterToTheStore is the gate on a handler
// that binds its query and then queries for something else. Every narrowing a
// caller can express has to reach the store, because the store is the only
// thing that can apply one before the page is cut.
func TestAHistorySearchCarriesEveryParameterToTheStore(t *testing.T) {
	t.Parallel()

	past := &recordingHistory{}
	handler := serverWithHistory(t, past, noPrinters{}).Handler()

	rec := call(t, handler, http.MethodGet, historyFactsPath+
		"?origins=device:"+examplePrinterID+
		"&types=com.prusa.connect.device.arrived"+
		"&from=2026-08-26T09:00:00Z&to=2026-08-26T10:00:00Z&limit=25&cursor=abc", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, past.factQueries, 1)
	asked := past.factQueries[0]

	require.Equal(t, []string{"com.prusa.connect.device.arrived"}, asked.Types)
	require.Equal(t, 25, asked.Limit)
	require.Equal(t, "abc", asked.Cursor)
	require.Equal(t, time.Date(2026, time.August, 26, 9, 0, 0, 0, time.UTC), asked.From.UTC())
	require.Equal(t, time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC), asked.To.UTC())
	require.Equal(t, "user:operator", past.principals[0],
		"the store was asked without the principal whose permission decides the answer")

	require.Len(t, asked.Origins, 1)
	require.Equal(t, "device:"+examplePrinterID, asked.Origins[0].String())
}

// TestAHistorySearchNamingNoOriginNamesNone holds the other half of the
// narrowing. An empty set has to reach the store as an empty set, because that
// is what the store reads as "everything this caller may read"; a handler that
// substituted something would decide a permission here.
func TestAHistorySearchNamingNoOriginNamesNone(t *testing.T) {
	t.Parallel()

	past := &recordingHistory{}
	handler := serverWithHistory(t, past, noPrinters{}).Handler()

	rec := call(t, handler, http.MethodGet, historySamplesPath, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, past.sampleQueries, 1)
	require.Empty(t, past.sampleQueries[0].Origins)
	require.Empty(t, past.sampleQueries[0].Metrics)
}

// TestAnOriginThisServerCouldNotHaveIssuedIsRefused keeps a malformed narrowing
// from being dropped. A client that asked for two printers and was silently
// given one is a client watching a printer it does not know it is missing.
func TestAnOriginThisServerCouldNotHaveIssuedIsRefused(t *testing.T) {
	t.Parallel()

	past := &recordingHistory{}
	handler := serverWithHistory(t, past, noPrinters{}).Handler()

	rec := call(t, handler, http.MethodGet, historyFactsPath+"?origins=not-an-origin", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	require.Empty(t, past.factQueries, "a refused request still reached the store")
}

// TestOneDevicesHistoryIsRefusedTheWayTheDeviceIs is the 404 earlier research
// section 5.3 asks for. A device this server never met and one the caller may
// not read answer alike, so this path never says which devices exist.
func TestOneDevicesHistoryIsRefusedTheWayTheDeviceIs(t *testing.T) {
	t.Parallel()

	past := &recordingHistory{}
	handler := serverWithHistory(t, past, noPrinters{}).Handler()

	for name, path := range map[string]string{
		"events":       devicesPath + "/" + examplePrinterID + "/history/facts",
		"measurements": devicesPath + "/" + examplePrinterID + "/history/samples",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, http.MethodGet, path, "")

			require.Equal(t, http.StatusNotFound, rec.Code, "body: %s", rec.Body.String())
		})
	}

	require.Empty(t, past.factQueries, "a refused device still reached the store")
	require.Empty(t, past.sampleQueries, "a refused device still reached the store")
}

// TestOneDevicesHistoryIsTheFleetSearchWithTheOriginFixed is the property that
// keeps the two shapes from answering differently: the per-device path is the
// fleet-wide one with the origin the path names.
func TestOneDevicesHistoryIsTheFleetSearchWithTheOriginFixed(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{printer: examplePrinter()}
	past := &recordingHistory{}
	handler := serverWithHistory(t, past, fleet).Handler()

	rec := call(t, handler, http.MethodGet,
		devicesPath+"/"+examplePrinterID+"/history/samples?metrics=/tools/0/nozzle_temp", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, past.sampleQueries, 1)
	asked := past.sampleQueries[0]

	require.Equal(t, []string{"/tools/0/nozzle_temp"}, asked.Metrics)
	require.Len(t, asked.Origins, 1)

	want, err := event.NewOrigin(event.KindDevice, examplePrinterID)
	require.NoError(t, err)
	require.Equal(t, want, asked.Origins[0])
}

// TestAHistoryPageWithNoRowsIsAnEmptyArray keeps a client that iterates the
// member correct on every answer.
func TestAHistoryPageWithNoRowsIsAnEmptyArray(t *testing.T) {
	t.Parallel()

	handler := serverWithHistory(t, &recordingHistory{}, noPrinters{}).Handler()

	rec := call(t, handler, http.MethodGet, historyFactsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"items":[],"cursor":"","incomplete":false}`, rec.Body.String())
}
