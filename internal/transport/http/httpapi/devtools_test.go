//go:build !nodevtools

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// devFleet is a fleet that answers for one printer. The contract is embedded
// rather than implemented method by method, so a method added to DeviceFleet
// does not turn this file into a compile error about something it never
// exercises; a test that calls one anyway fails loudly on the nil interface.
type devFleet struct {
	DeviceFleet

	printer  Device
	result   PrinterResult
	refuse   error
	received []PrinterCommand
}

func (f *devFleet) Get(_ context.Context, _ string, id string) (Device, error) {
	if id != f.printer.ID {
		return Device{}, notFoundPrinter(id)
	}

	return f.printer, nil
}

func (f *devFleet) Command(
	_ context.Context, _ string, id string, cmd PrinterCommand,
) (PrinterResult, error) {
	if id != f.printer.ID {
		return PrinterResult{}, notFoundPrinter(id)
	}

	f.received = append(f.received, cmd)

	if f.refuse != nil {
		return PrinterResult{}, f.refuse
	}

	return f.result, nil
}

// developerGrants is a caller who holds develop_device on the driver resource
// kind. It records what it was asked, so a test can hold the surface to the one
// action rather than to "the graph said yes about something".
type developerGrants struct {
	fakeGrantStore

	mu      sync.Mutex
	asked   []authz.Action
	objects []authz.Object
}

func (g *developerGrants) Can(
	_ context.Context, _ authz.Subject, action authz.Action, object authz.Object,
) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.asked = append(g.asked, action)
	g.objects = append(g.objects, object)

	return action == authz.ActionDevelopDevice, nil
}

// viewerGrants holds nothing on the developer surface. It is the person a
// signed-in fleet viewer is: they reach the device listing and they do not
// reach the bench.
type viewerGrants struct{ fakeGrantStore }

func (g *viewerGrants) Can(
	context.Context, authz.Subject, authz.Action, authz.Object,
) (bool, error) {
	return false, nil
}

func devServer(t *testing.T, fleet DeviceFleet) *Server {
	t.Helper()

	return devServerWith(t, fleet, &developerGrants{})
}

// devServerWith is the production server over a fleet and a grant store the
// case chooses. It is gcodeServer under another name: both files want the same
// server with a different answer from the graph, and one builder is one place
// the wiring lives.
func devServerWith(t *testing.T, fleet DeviceFleet, grants GrantStore) *Server {
	t.Helper()

	return gcodeServer(t, fleet, grants)
}

// TestTheDeveloperPageTakesTheSameIdentityEveryOtherOperationTakes is the
// authorization decision, in both directions. The page is a route like any
// other rather than an exception beside the router, so a caller this server
// cannot identify is refused here exactly as they are on /api/v1/principal.
func TestTheDeveloperPageTakesTheSameIdentityEveryOtherOperationTakes(t *testing.T) {
	t.Parallel()

	t.Run("an identified caller", func(t *testing.T) {
		t.Parallel()

		rec := call(t, devServer(t, &devFleet{printer: devPrinter()}).Handler(),
			http.MethodGet, devPagePath, "")

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		require.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'none'")
		require.Contains(t, rec.Body.String(), "developer console")
	})

	t.Run("a caller this server cannot identify", func(t *testing.T) {
		t.Parallel()

		rec := call(t, serverWithoutIdentity(t).Handler(), http.MethodGet, devPagePath, "")

		body := requireProblem(t, rec, http.StatusUnauthorized)
		require.Equal(t, fault.AuthTokenInvalid.Code(), body.Code)
	})
}

// TestTheDeveloperPageRevalidates keeps a reload during a debugging session at
// one conditional request, and keeps the tag tied to the bytes: a page cached
// past a rebuild is a tool that reports the wrong build.
func TestTheDeveloperPageRevalidates(t *testing.T) {
	t.Parallel()

	handler := devServer(t, &devFleet{printer: devPrinter()}).Handler()

	first := call(t, handler, http.MethodGet, devPagePath, "")
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, devtools.Rendered().ETag, etag)

	req := httptest.NewRequest(http.MethodGet, devPagePath, nil)
	req.Header.Set("If-None-Match", etag)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotModified, rec.Code)
	require.Empty(t, rec.Body.String())
}

// TestTheConnectorInventoryReportsTheOriginsOwnRoutes is the panel that answers
// "can a printer reach this server at all". The routes come from what the host
// declared rather than from anything this package guesses, so a mount that moved
// shows up here rather than being reported as a connector that is not there.
func TestTheConnectorInventoryReportsTheOriginsOwnRoutes(t *testing.T) {
	t.Parallel()

	rec := call(t, devServer(t, &devFleet{printer: devPrinter()}).Handler(),
		http.MethodGet, devConnectorsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body devtools.Inventory
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Len(t, body.Routes, 1)
	require.Equal(t, "/p/ws", body.Routes[0].Pattern)
	require.Contains(t, body.Routes[0].Reason, "prusa-xbuddy")
}

func TestTheConnectorInventoryRefusesACallerWithNoIdentity(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithoutIdentity(t).Handler(), http.MethodGet, devConnectorsPath, "")

	body := requireProblem(t, rec, http.StatusUnauthorized)
	require.Equal(t, fault.AuthTokenInvalid.Code(), body.Code)
}

// TestACommandThroughThisServerLandsInTheLog is the one thing about a printer
// session no other endpoint can reconstruct afterwards: what was sent, and what
// came back.
func TestACommandThroughThisServerLandsInTheLog(t *testing.T) {
	t.Parallel()

	fleet := &devFleet{
		printer: devPrinter(),
		result:  PrinterResult{OK: true, Ignored: []string{"speed"}},
	}

	handler := devServer(t, fleet).Handler()
	path := "/api/v1/devices/" + fleet.printer.ID

	sent := call(t, handler, http.MethodPost, path+"/commands",
		`{"verb":"SET_NOZZLE_TEMPERATURE","args":{"nozzle_temperature":"215"}}`)
	require.Equal(t, http.StatusOK, sent.Code, "body: %s", sent.Body.String())
	require.Len(t, fleet.received, 1, "the decorator must pass the command through unchanged")

	rec := call(t, handler, http.MethodGet, "/api/v1/dev/devices/"+fleet.printer.ID+"/commands", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items   []devtools.Command `json:"items"`
		Dropped uint64             `json:"dropped"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, "SET_NOZZLE_TEMPERATURE", body.Items[0].Verb)
	require.Equal(t, "215", body.Items[0].Args["nozzle_temperature"])
	require.True(t, body.Items[0].OK)
	require.Equal(t, []string{"speed"}, body.Items[0].Ignored)
	// The key and not the figure. A command answered by a fake in the same
	// process takes under a millisecond, so the honest reading is zero.
	require.Contains(t, rec.Body.String(), `"elapsed_ms":`)
	require.GreaterOrEqual(t, body.Items[0].ElapsedMS, int64(0))
	require.Zero(t, body.Dropped)
}

// TestARefusedCommandIsRecordedWithTheCodeTheCallerWasGiven keeps the log
// honest about the interesting half. A command that was refused is what a person
// at a bench is looking for, and a log that only kept the accepted ones would
// answer with silence exactly when it is asked.
func TestARefusedCommandIsRecordedWithTheCodeTheCallerWasGiven(t *testing.T) {
	t.Parallel()

	refusal := fault.PrinterCommandRejected.New("the printer's resolved set does not carry that verb")
	fleet := &devFleet{printer: devPrinter(), refuse: refusal}

	handler := devServer(t, fleet).Handler()
	path := "/api/v1/devices/" + fleet.printer.ID

	sent := call(t, handler, http.MethodPost, path+"/commands", `{"verb":"CANCEL_PRINT"}`)
	require.NotEqual(t, http.StatusOK, sent.Code)

	rec := call(t, handler, http.MethodGet, "/api/v1/dev/devices/"+fleet.printer.ID+"/commands", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []devtools.Command `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.False(t, body.Items[0].OK)
	require.Equal(t, fault.PrinterCommandRejected.Code().String(), body.Items[0].FaultCode)
}

// TestTheLogKeepsThePrintersOwnReasonForARefusal is the wiring the page's own
// gates cannot reach.
//
// The developer view is executed against a stubbed command log, so a page that
// renders the field proves the page and nothing about what fills it. This drives
// the decorator that records a real result, which is the half where the reason
// was being dropped.
func TestTheLogKeepsThePrintersOwnReasonForARefusal(t *testing.T) {
	t.Parallel()

	fleet := &devFleet{
		printer: devPrinter(),
		result:  PrinterResult{OK: false, MachineReason: "MEDIA_NOT_INSERTED"},
	}

	handler := devServer(t, fleet).Handler()
	path := "/api/v1/devices/" + fleet.printer.ID

	sent := call(t, handler, http.MethodPost, path+"/commands", `{"verb":"START_PRINT"}`)
	require.Equal(t, http.StatusOK, sent.Code, "body: %s", sent.Body.String())

	rec := call(t, handler, http.MethodGet, "/api/v1/dev/devices/"+fleet.printer.ID+"/commands", "")
	require.Equal(t, http.StatusOK, rec.Code)

	t.Logf("observed: the command log answered %s", rec.Body.String())

	var body struct {
		Items []devtools.Command `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.False(t, body.Items[0].OK)
	require.Equal(t, "MEDIA_NOT_INSERTED", body.Items[0].MachineReason,
		"the printer's reason reached the caller and not the log a person reads at the bench")
	require.Empty(t, body.Items[0].FaultCode,
		"the printer's own word was recorded as one of this API's codes")
}

// TestACommandThatReachedNoPrinterIsNotRecorded closes the way a caller could
// write into the history of a printer they cannot see: offer a command to a
// guessed id, be refused, and leave a row behind for whoever may read that
// printer.
func TestACommandThatReachedNoPrinterIsNotRecorded(t *testing.T) {
	t.Parallel()

	log := devtools.NewCommandLog(0)
	fleet := recordingFleet{DeviceFleet: &devFleet{printer: devPrinter()}, log: log}

	_, err := fleet.Command(t.Context(), "user:operator", "pguessed",
		PrinterCommand{Verb: "CANCEL_PRINT"})
	require.Error(t, err)

	entries, dropped := log.Recent("pguessed")
	require.Empty(t, entries)
	require.Zero(t, dropped, "a refused guess must not consume the log either")

	// The decorator still records the printer that is there, so the guard above
	// is narrow rather than a log that quietly records nothing.
	_, err = fleet.Command(t.Context(), "user:operator", devPrinter().ID,
		PrinterCommand{Verb: "CANCEL_PRINT"})
	require.NoError(t, err)

	kept, _ := log.Recent(devPrinter().ID)
	require.Len(t, kept, 1)
}

// TestTheCommandLogIsReachedThroughThePrinterRefusal is the refusal half of the
// pair above. A caller who may not act on a printer is told exactly what a
// caller asking about one that is not there is told, so no refusal on this
// surface confirms that a printer exists.
func TestTheCommandLogIsReachedThroughThePrinterRefusal(t *testing.T) {
	t.Parallel()

	handler := devServer(t, &devFleet{printer: devPrinter()}).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/dev/devices/psomeoneelses/commands", "")

	body := requireProblem(t, rec, http.StatusNotFound)
	require.Equal(t, fault.PrinterNotFound.Code(), body.Code)
	require.NotContains(t, strings.ToLower(body.Detail), "permission")
}

// TestTheDeveloperSurfaceTakesItsOwnPermission is task 18 of the old backlog, in both
// directions and on all three operations.
//
// The panels are moving into the app, where every signed-in person reaches
// them, so an identity is no longer the whole check. A viewer of the fleet is
// refused with 403 and not 404: they already see the devices, so hiding the
// surface would withhold nothing and would read as a page that had gone away.
func TestTheDeveloperSurfaceTakesItsOwnPermission(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"the page":         devPagePath,
		"the connectors":   devConnectorsPath,
		"one device's log": "/api/v1/dev/devices/" + devPrinter().ID + "/commands",
	}

	for name, path := range paths {
		t.Run(name+", held by the caller", func(t *testing.T) {
			t.Parallel()

			grants := &developerGrants{}
			handler := devServerWith(t, &devFleet{printer: devPrinter()}, grants).Handler()

			rec := call(t, handler, http.MethodGet, path, "")
			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

			grants.mu.Lock()
			defer grants.mu.Unlock()

			require.Contains(t, grants.asked, authz.ActionDevelopDevice)
			require.Contains(t, grants.objects,
				mustObject(t, authz.TypeResourceKind, ref.KindDriver.String()),
				"the surface is decided on the driver resource kind")
		})

		t.Run(name+", refused to a viewer", func(t *testing.T) {
			t.Parallel()

			handler := devServerWith(t, &devFleet{printer: devPrinter()}, &viewerGrants{}).Handler()

			rec := call(t, handler, http.MethodGet, path, "")

			body := requireProblem(t, rec, http.StatusForbidden)
			require.Equal(t, authz.CodeAccessDenied(), body.Code)
			require.Contains(t, body.Detail, "developer surface")
		})
	}
}

func mustObject(t *testing.T, typ authz.Type, id string) authz.Object {
	t.Helper()

	object, err := authz.NewObject(typ, id)
	require.NoError(t, err)

	return object
}
