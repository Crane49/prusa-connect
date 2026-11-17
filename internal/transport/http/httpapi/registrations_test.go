package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fakeRegistrations is a registration table a case can put anything into. It
// answers admission.ErrUnknownCode for a code it does not hold, which is the
// one refusal the transport has to render.
type fakeRegistrations struct {
	mu       sync.Mutex
	waiting  []Registration
	approved []string
	rejected []string
	fail     error

	// bound is the device id an approval answers with, empty for a fake that
	// binds none.
	bound string
}

var _ DeviceRegistrations = (*fakeRegistrations)(nil)

func (f *fakeRegistrations) Pending(context.Context) ([]Registration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return nil, f.fail
	}

	return f.waiting, nil
}

func (f *fakeRegistrations) Approve(_ context.Context, code string) (Registration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, item := range f.waiting {
		if item.Code != code {
			continue
		}

		f.approved = append(f.approved, code)
		item.Approved = true
		item.DeviceID = f.bound

		return item, nil
	}

	return Registration{}, fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

func (f *fakeRegistrations) Reject(_ context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, item := range f.waiting {
		if item.Code == code {
			f.rejected = append(f.rejected, code)

			return nil
		}
	}

	return fmt.Errorf("%w: %s", admission.ErrUnknownCode, code)
}

// waitingRegistration is one row a case decides about.
func waitingRegistration() Registration {
	return Registration{
		Code:        "K7PQ2XR4MN",
		Connector:   "xbuddy",
		Fingerprint: "b3f1c0d2e4a5",
		Serial:      "8221-1234567",
		Model:       "MK4",
		Firmware:    "6.1.3",
	}
}

// registrationServer is a server whose registration table and graph are the
// case's. Everything else is the ordinary stand-in set.
func registrationServer(t *testing.T, tables DeviceRegistrations, grants GrantStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:       "test",
		Principals:    fixedPrincipal("user:operator"),
		Resources:     seededStore(),
		Presets:       seededPresets(),
		Endpoints:     seededEndpoints(),
		Grants:        grants,
		Sessions:      newFakeSessions(),
		Accounts:      &fakeAccounts{},
		Readiness:     &fakeReadiness{report: readyReport()},
		Devices:       noPrinters{},
		Stores:        noStorages{},
		Events:        noEvents{},
		Registrations: tables,
	})
	require.NoError(t, err)

	return server
}

// TestAnOperatorReadsApprovesAndRejectsARegistration is the whole surface for a
// caller who holds the permission: what is waiting, letting one in, and turning
// one away.
func TestAnOperatorReadsApprovesAndRejectsARegistration(t *testing.T) {
	t.Parallel()

	tables := &fakeRegistrations{waiting: []Registration{waitingRegistration()}}
	graph := &grantingStore{allow: true}
	handler := registrationServer(t, tables, graph).Handler()

	rec := call(t, handler, http.MethodGet, registrationsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var listing struct {
		Items []Registration `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Equal(t, []Registration{waitingRegistration()}, listing.Items)

	require.Equal(t, "resource_kind:driver", graph.asked[0].String(),
		"the permission was decided on the wrong object")

	rec = call(t, handler, http.MethodPost, "/api/v1/registrations/K7PQ2XR4MN/approve", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var approved Registration

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &approved))
	require.True(t, approved.Approved, "the answer to an approval says the device is still waiting")
	require.Equal(t, []string{"K7PQ2XR4MN"}, tables.approved)

	rec = call(t, handler, http.MethodDelete, "/api/v1/registrations/K7PQ2XR4MN", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, []string{"K7PQ2XR4MN"}, tables.rejected)
}

// TestNoRegistrationSurfaceReachesACallerWhoMayNotConfigureTheDrivers is the
// refusal half. Admitting a device decides who gets onto this server, so a
// caller who may not configure the connectors reaches none of the three
// operations and the table is never asked.
func TestNoRegistrationSurfaceReachesACallerWhoMayNotConfigureTheDrivers(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		method string
		path   string
	}{
		"the listing": {method: http.MethodGet, path: registrationsPath},
		"approving":   {method: http.MethodPost, path: "/api/v1/registrations/K7PQ2XR4MN/approve"},
		"rejecting":   {method: http.MethodDelete, path: "/api/v1/registrations/K7PQ2XR4MN"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tables := &fakeRegistrations{waiting: []Registration{waitingRegistration()}}
			handler := registrationServer(t, tables, &grantingStore{allow: false}).Handler()

			rec := call(t, handler, tc.method, tc.path, "")
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

			require.Empty(t, tables.approved, "a caller who was refused still approved a device")
			require.Empty(t, tables.rejected, "a caller who was refused still rejected a device")
			require.Contains(t, rec.Body.String(), "resource_kind:driver",
				"the refusal does not say which grant to write")
		})
	}
}

// TestACodeNoConnectorHoldsIsTheSameRefusalAsOneAlreadyRejected keeps the
// surface from becoming a way to find out which codes this server has seen.
func TestACodeNoConnectorHoldsIsTheSameRefusalAsOneAlreadyRejected(t *testing.T) {
	t.Parallel()

	handler := registrationServer(t, &fakeRegistrations{}, &grantingStore{allow: true}).Handler()

	approve := call(t, handler, http.MethodPost, "/api/v1/registrations/NOSUCHCODE/approve", "")
	require.Equal(t, http.StatusNotFound, approve.Code, approve.Body.String())

	reject := call(t, handler, http.MethodDelete, "/api/v1/registrations/NOSUCHCODE", "")
	require.Equal(t, http.StatusNotFound, reject.Code, reject.Body.String())

	var problem struct {
		Code string `json:"code"`
	}

	require.NoError(t, json.Unmarshal(approve.Body.Bytes(), &problem))
	require.Equal(t, refusalCodeOf(unknownRegistration("X")), problem.Code)
}

// TestADeploymentWithNoRegistrationTableAnswersAnEmptyListing is the wiring
// case. The document is a contract, so a build whose connectors admit on
// arrival publishes the same three paths.
func TestADeploymentWithNoRegistrationTableAnswersAnEmptyListing(t *testing.T) {
	t.Parallel()

	handler := registrationServer(t, nil, &grantingStore{allow: true}).Handler()

	rec := call(t, handler, http.MethodGet, registrationsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"items":[]}`, rec.Body.String(),
		"a listing with nothing on it answered null rather than an empty array")

	rec = call(t, handler, http.MethodPost, "/api/v1/registrations/K7PQ2XR4MN/approve", "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestARegistrationRowCarriesNoCredential is what stops this surface being a
// second way to obtain a device's token. It reads the published schema rather
// than one answer, so a member added later is caught whatever a handler puts in
// it.
func TestARegistrationRowCarriesNoCredential(t *testing.T) {
	t.Parallel()

	server := registrationServer(t, nil, &grantingStore{allow: true})

	registry := server.api.OpenAPI().Components.Schemas
	registry.Schema(reflect.TypeFor[Registration](), true, "Registration")

	schema := registry.Map()["Registration"]
	require.NotNil(t, schema, "the document publishes no Registration schema")
	require.NotEmpty(t, schema.Properties, "the published registration has no members at all")

	for _, forbidden := range []string{"token", "secret", "password", "credential"} {
		_, carried := schema.Properties[forbidden]
		require.False(t, carried, "the published registration carries a %s member", forbidden)
	}
}

// TestAConnectorFailureIsNotReportedAsTheCallersMistake holds the other branch
// of the fault mapping. A table that could not answer is this server's problem,
// and reporting it as a 404 would send an operator looking for a code that is
// there.
func TestAConnectorFailureIsNotReportedAsTheCallersMistake(t *testing.T) {
	t.Parallel()

	tables := &fakeRegistrations{fail: errors.New("the connector is not answering")}
	handler := registrationServer(t, tables, &grantingStore{allow: true}).Handler()

	rec := call(t, handler, http.MethodGet, registrationsPath, "")
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// refusalCodeOf is the code one fault carries, read off the value the surface
// itself produces, so a rename of the registered code moves this case with it.
func refusalCodeOf(err error) string {
	if refusal, ok := errors.AsType[*fault.Fault](err); ok {
		return refusal.Code().String()
	}

	return ""
}
