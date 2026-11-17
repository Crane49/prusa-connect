package resourcehost_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
)

// operator is the identity every request in this file arrives as. There is no
// authorization graph over resource instances yet, so it exists to satisfy the
// transport rather than to be checked.
type operator struct{}

func (operator) Principal(*http.Request) (string, error) { return "user:operator", nil }

// noLogin, alwaysReady and noStream fill the seams this test does not
// exercise. They are here rather than defaulted in the constructor because a
// server that cannot say who is calling, how somebody logs in, whether it is
// ready or what it publishes events from is a server nobody wired on purpose.
type noLogin struct{}

func (noLogin) Begin(context.Context) (httpapi.Redirect, error) {
	return httpapi.Redirect{}, errors.New("no issuer in this test")
}

func (noLogin) Complete(context.Context, string, string) (httpapi.Session, error) {
	return httpapi.Session{}, errors.New("no issuer in this test")
}

func (noLogin) Revoke(context.Context, *http.Request) error { return nil }

func (noLogin) Cookie() string { return "pcl_session" }

// noAccountStore is the credential store of a deployment that holds none. It
// refuses everything rather than admitting anybody, for the reason noLogin
// gives.
type noAccountStore struct{}

var errNoAccounts = errors.New("no local accounts in this test")

func (noAccountStore) Info(context.Context) (httpapi.AuthInfo, error) {
	return httpapi.AuthInfo{}, nil
}

func (noAccountStore) Setup(context.Context, httpapi.NewAccount) (httpapi.Account, error) {
	return httpapi.Account{}, errNoAccounts
}

func (noAccountStore) Login(context.Context, string, string) (httpapi.Session, error) {
	return httpapi.Session{}, errNoAccounts
}

func (noAccountStore) List(context.Context) ([]httpapi.Account, error) {
	return []httpapi.Account{}, nil
}

func (noAccountStore) Get(context.Context, string) (httpapi.Account, error) {
	return httpapi.Account{}, errNoAccounts
}

func (noAccountStore) Create(context.Context, httpapi.NewAccount) (httpapi.Account, error) {
	return httpapi.Account{}, errNoAccounts
}

func (noAccountStore) Delete(context.Context, string) error { return errNoAccounts }

func (noAccountStore) ChangePassword(context.Context, string, string, string) (httpapi.Session, error) {
	return httpapi.Session{}, errNoAccounts
}

func (noAccountStore) ResetPassword(context.Context, string, string) error { return errNoAccounts }

type alwaysReady struct{}

func (alwaysReady) Ready(context.Context, string) httpapi.ReadinessReport {
	return httpapi.ReadinessReport{Ready: true}
}

type noStream struct{}

func (noStream) Subscribe(context.Context, stream.SubscribeRequest) (*stream.Subscription, error) {
	return nil, stream.ErrNotPermitted
}

func (noStream) Subscribers() int { return 0 }

// errNoPrinter is what a fleet nothing has dialled into answers with.
var errNoPrinter = errors.New("no printer in this test")

type noPrinters struct{}

func (noPrinters) List(context.Context, string, httpapi.DeviceQuery) (httpapi.DevicePage, error) {
	return httpapi.DevicePage{}, nil
}

func (noPrinters) Get(context.Context, string, string) (httpapi.Device, error) {
	return httpapi.Device{}, errNoPrinter
}

func (noPrinters) Capabilities(context.Context, string, string) (httpapi.DeviceCapabilitySet, error) {
	return httpapi.DeviceCapabilitySet{}, errNoPrinter
}

func (noPrinters) Jobs(context.Context, string, string) (httpapi.DeviceJobs, error) {
	return httpapi.DeviceJobs{}, errNoPrinter
}

func (noPrinters) Job(context.Context, string, string) (httpapi.DeviceJob, error) {
	return httpapi.DeviceJob{}, errNoPrinter
}

func (noPrinters) Command(
	context.Context, string, string, httpapi.PrinterCommand,
) (httpapi.PrinterResult, error) {
	return httpapi.PrinterResult{}, errNoPrinter
}

func (noPrinters) Storages(context.Context, string, string, bool) (httpapi.DeviceStorages, error) {
	return httpapi.DeviceStorages{}, errNoPrinter
}

func (noPrinters) Files(context.Context, string, string, string, bool) (httpapi.DeviceListing, error) {
	return httpapi.DeviceListing{}, errNoPrinter
}

func (noPrinters) Send(
	context.Context, string, string, httpapi.DeviceUpload,
) (httpapi.DeviceTransfer, error) {
	return httpapi.DeviceTransfer{}, errNoPrinter
}

func (noPrinters) Transfers(context.Context, string, string) ([]httpapi.DeviceTransfer, error) {
	return nil, errNoPrinter
}

func (noPrinters) StopTransfer(context.Context, string, string, string) error { return errNoPrinter }

func (noPrinters) RunningTransfers(context.Context, string) ([]httpapi.RunningTransfer, error) {
	return nil, nil
}

func (noPrinters) CreateFolder(context.Context, string, string, string) error { return errNoPrinter }

func (noPrinters) DeleteFile(context.Context, string, string, string) error { return errNoPrinter }

func (noPrinters) DeleteFolder(context.Context, string, string, string) error { return errNoPrinter }

func (noPrinters) Thumbnail(
	context.Context, string, string, string, int64, int64,
) (httpapi.StorageThumbnail, error) {
	return httpapi.StorageThumbnail{}, errNoPrinter
}

func (noPrinters) SetTags(context.Context, string, string, []string) ([]string, error) {
	return nil, errNoPrinter
}

func (noPrinters) SetLabels(
	context.Context, string, string, map[string]string,
) (map[string]string, error) {
	return nil, errNoPrinter
}

func (noPrinters) SetName(context.Context, string, string, string) (string, error) {
	return "", errNoPrinter
}

func (noPrinters) Retire(context.Context, string, string) error { return errNoPrinter }

func (noPrinters) JobHistory(
	context.Context, string, string, httpapi.DeviceJobQuery,
) (httpapi.DeviceJobPage, error) {
	return httpapi.DeviceJobPage{}, errNoPrinter
}

func (noPrinters) ListJobs(context.Context, string, httpapi.JobQuery) (httpapi.JobPage, error) {
	return httpapi.JobPage{Items: []httpapi.DeviceJob{}}, nil
}

func (noPrinters) Print(
	context.Context, string, string, httpapi.DevicePrint,
) (httpapi.DevicePrintResult, error) {
	return httpapi.DevicePrintResult{}, errNoPrinter
}

func (noPrinters) StartPrint(
	context.Context, string, string, httpapi.DevicePrint,
) (httpapi.DevicePrintResult, error) {
	return httpapi.DevicePrintResult{}, errNoPrinter
}

// TestTheApiDrivesTheStoreEndToEnd runs the real HTTP surface over the real
// store over the real supervisor: a create, a read, a conditional update, and
// the two refusals a client has to be able to tell apart.
func TestTheApiDrivesTheStoreEndToEnd(t *testing.T) {
	t.Parallel()

	w := newWired(t)

	server, err := httpapi.New(httpapi.Config{
		Version:    "test",
		Principals: operator{},
		Resources:  w.store,
		Presets:    w.store,
		Endpoints:  w.store,
		Grants:     w.graph,
		Sessions:   noLogin{},
		Accounts:   noAccountStore{},
		Readiness:  alwaysReady{},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noStream{},
	})
	require.NoError(t, err)

	handler := server.Handler()

	created := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook",
		http.Header{"If-None-Match": {"*"}},
		`{"type":"counted","enabled":true,"required":false,"config":{"url":"https://a.example"}}`)

	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, "/api/v1/resources/sink/webhook", created.Header().Get("Location"))

	etag := created.Header().Get("ETag")
	require.True(t, strings.HasPrefix(etag, `"`), "the version must be a quoted entity-tag: %q", etag)

	w.waitReady(theSink(t))

	read := call(t, handler, http.MethodGet, "/api/v1/resources/sink/webhook", nil, "")
	require.Equal(t, http.StatusOK, read.Code)
	require.Equal(t, etag, read.Header().Get("ETag"))

	var body httpapi.ResourceInstance

	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &body))
	require.Equal(t, "overlay", body.Source)
	require.Equal(t, "counted", body.Type)
	require.Equal(t, int64(1), body.Generation)

	w.await(func() bool {
		again := call(t, handler, http.MethodGet, "/api/v1/resources/sink/webhook", nil, "")

		var status struct {
			Status httpapi.ResourceStatus `json:"status"`
		}

		require.NoError(t, json.Unmarshal(again.Body.Bytes(), &status))

		return status.Status.State == condition.StateReady().String() && status.Status.Serving
	}, "the API never reported the instance ready", func() string { return w.describe(theSink(t)) })

	// A write quoting the version it just read is accepted.
	updated := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook",
		http.Header{"If-Match": {etag}},
		`{"type":"counted","enabled":true,"required":false,"config":{"url":"https://b.example"}}`)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())

	// The same version again is stale, and RFC 9110 mandates 412 for it.
	stale := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook",
		http.Header{"If-Match": {etag}},
		`{"type":"counted","enabled":true,"required":false,"config":{"url":"https://c.example"}}`)
	require.Equal(t, http.StatusPreconditionFailed, stale.Code, stale.Body.String())
	require.Contains(t, stale.Body.String(), "VERSION_CONFLICT")

	// A type change is a 422 naming the field, not a silent replacement.
	retyped := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook",
		http.Header{"If-Match": {updated.Header().Get("ETag")}},
		`{"type":"other","enabled":true,"required":false,"config":{}}`)
	require.Equal(t, http.StatusUnprocessableEntity, retyped.Code, retyped.Body.String())
	require.Contains(t, retyped.Body.String(), "FIELD_IMMUTABLE")
	require.Contains(t, retyped.Body.String(), "body.type")

	// A write with no precondition has nowhere to hide.
	blind := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook", nil,
		`{"type":"counted","enabled":true,"required":false,"config":{}}`)
	require.Equal(t, http.StatusPreconditionRequired, blind.Code, blind.Body.String())

	// Reconcile hands its plan to the goroutine Run owns, so the second
	// construction lands after the request that triggered it has answered.
	w.waitBuilds(w.built, 2, "one create and one settings change is two constructions")
	require.Never(t, func() bool { return w.built.buildCount.Load() > 2 }, settle, time.Millisecond,
		"and no more")

	// What a client last read is what is on disk, and the two agree about which
	// declaration the supervision plane has acted on.
	final := call(t, handler, http.MethodGet, "/api/v1/resources/sink/webhook", nil, "")
	require.NoError(t, json.Unmarshal(final.Body.Bytes(), &body))
	require.Equal(t, int64(2), body.Generation)
	require.Equal(t, body.Generation, body.Status.ObservedGeneration)
	require.JSONEq(t, `{"url":"https://b.example"}`, string(body.Config))

	// The listing carries the same token every instance under it carries,
	// because a write rewrites the overlay whole.
	listed := call(t, handler, http.MethodGet, "/api/v1/resources", nil, "")
	require.Equal(t, http.StatusOK, listed.Code)
	require.Equal(t, final.Header().Get("ETag"), listed.Header().Get("ETag"))

	overlay, readErr := os.ReadFile(w.paths.Overlay)
	require.NoError(t, readErr)
	require.Contains(t, string(overlay), `"uid": "`+body.UID+`"`)
	require.Contains(t, string(overlay), `"url": "https://b.example"`)
	require.NotContains(t, string(overlay), "https://c.example", "a refused write leaves no trace")
}

// TestTheApiRefusesToWriteTheOperatorsInstance is earlier research as a
// client sees it: 409, and the body names the file to edit instead.
func TestTheApiRefusesToWriteTheOperatorsInstance(t *testing.T) {
	t.Parallel()

	w := newWired(t)
	w.writeFile("resources:\n  sink:\n    webhook:\n      type: counted\n")
	require.NoError(t, w.store.Reload(t.Context()))

	server, err := httpapi.New(httpapi.Config{
		Version:    "test",
		Principals: operator{},
		Resources:  w.store,
		Presets:    w.store,
		Endpoints:  w.store,
		Grants:     w.graph,
		Sessions:   noLogin{},
		Accounts:   noAccountStore{},
		Readiness:  alwaysReady{},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noStream{},
	})
	require.NoError(t, err)

	handler := server.Handler()

	read := call(t, handler, http.MethodGet, "/api/v1/resources/sink/webhook", nil, "")
	require.Equal(t, http.StatusOK, read.Code)
	require.Contains(t, read.Body.String(), `"source":"file"`)

	refused := call(t, handler, http.MethodPut, "/api/v1/resources/sink/webhook",
		http.Header{"If-Match": {read.Header().Get("ETag")}},
		`{"type":"counted","enabled":false,"required":false,"config":{}}`)
	require.Equal(t, http.StatusConflict, refused.Code, refused.Body.String())
	require.Contains(t, refused.Body.String(), "RESOURCE_NOT_WRITABLE")
	require.Contains(t, refused.Body.String(), "config.yaml")
}

func call(
	t *testing.T, handler http.Handler, method, path string, header http.Header, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	var req *http.Request
	if reader == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
	}

	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	return recorder
}

// noStorages answers the storage routes, which are published whatever a
// deployment declared. This suite is about the resource plane and declares none.
type noStorages struct{}

func (noStorages) List(context.Context, string) ([]httpapi.Storage, error) { return nil, nil }

func (noStorages) Files(context.Context, string, string, string) ([]httpapi.StorageEntry, error) {
	return nil, errNoPrinter
}

func (noStorages) Open(context.Context, string, string, string) (httpapi.Object, error) {
	return httpapi.Object{}, errNoPrinter
}

func (noStorages) Stat(context.Context, string, string, string) (httpapi.StorageEntry, error) {
	return httpapi.StorageEntry{}, errNoPrinter
}

func (noStorages) Thumbnail(context.Context, string, string, string) (httpapi.StorageThumbnail, error) {
	return httpapi.StorageThumbnail{}, errNoPrinter
}

func (noStorages) FileVersion(context.Context, string, string, string) (httpapi.FileVersion, error) {
	return httpapi.FileVersion{}, errNoPrinter
}

func (noStorages) FileExtent(context.Context, string, string, string) (httpapi.FileExtent, error) {
	return httpapi.FileExtent{}, errNoPrinter
}

func (noStorages) Save(context.Context, string, string, string, io.Reader) (httpapi.SaveResult, error) {
	return httpapi.SaveResult{}, errNoPrinter
}

func (noStorages) SaveTemporary(
	context.Context, string, string, string, io.Reader,
) (httpapi.SaveResult, error) {
	return httpapi.SaveResult{}, errNoPrinter
}

func (noStorages) Delete(context.Context, string, string, string) error { return errNoPrinter }

func (noStorages) DeleteMany(context.Context, string, string, []string) error { return errNoPrinter }

func (noStorages) DownloadURL(context.Context, string, string, string) (string, bool, error) {
	return "", false, errNoPrinter
}

func (noStorages) CreateFolder(context.Context, string, string, string) error { return errNoPrinter }

func (noStorages) Move(context.Context, string, string, string, string) error { return errNoPrinter }

// Caller is Principal with no scope.
func (x operator) Caller(r *http.Request) (httpapi.Caller, error) {
	return httpapi.Unscoped(x.Principal(r))
}
