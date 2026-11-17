package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

type fakeBetaPortal struct {
	status        betaportal.Status
	starts        int
	refreshes     int
	disconnects   int
	statusErr     error
	startErr      error
	disconnectErr error
}

func (f *fakeBetaPortal) Start(context.Context) (betaportal.Status, error) {
	f.starts++
	if f.startErr != nil {
		return betaportal.Status{}, f.startErr
	}

	return f.status, nil
}

func (f *fakeBetaPortal) Status(context.Context) (betaportal.Status, error) {
	return f.status, f.statusErr
}

func (f *fakeBetaPortal) Refresh(context.Context) (betaportal.Status, error) {
	f.refreshes++

	return f.status, f.statusErr
}

func (f *fakeBetaPortal) Disconnect(context.Context) error {
	f.disconnects++

	return f.disconnectErr
}

func TestBetaPortalPairingAPIOnlyReturnsSafeStatus(t *testing.T) {
	t.Parallel()

	expires := time.Now().Add(time.Minute).UTC()
	portal := &fakeBetaPortal{status: betaportal.Status{
		State: betaportal.StatePending, UserCode: "ABCD-EFGH",
		VerificationURI: "https://portal.example/device", ExpiresAt: expires,
	}}
	server := betaPortalServer(t, portal, &fakeGrantStore{})

	response := call(t, server.Handler(), http.MethodPost, betaPortalPairingPath, "")
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())

	var status betaPortalPairingStatus
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	require.Equal(t, betaportal.StatePending, status.State)
	require.Equal(t, "ABCD-EFGH", status.UserCode)
	require.NotContains(t, response.Body.String(), "device_code")
	require.NotContains(t, response.Body.String(), "access_token")
	require.NotContains(t, response.Body.String(), "private_key")
	require.Equal(t, 1, portal.starts)

	response = call(t, server.Handler(), http.MethodGet, betaPortalPairingPath, "")
	require.Equal(t, http.StatusOK, response.Code)

	response = call(t, server.Handler(), http.MethodPost, betaPortalPairingPath+"/refresh", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, portal.refreshes)

	response = call(t, server.Handler(), http.MethodDelete, betaPortalPairingPath, "")
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, response.Body.String())
	require.Equal(t, 1, portal.disconnects)
}

func TestBetaPortalPairingStatusCarriesStepAddressAndPairedDevice(t *testing.T) {
	t.Parallel()

	expires := time.Now().Add(time.Minute).UTC()
	portal := &fakeBetaPortal{status: betaportal.Status{
		State: betaportal.StatePending, Step: betaportal.StepApproval, UserCode: "WDJB-MJHT",
		VerificationURI:         "https://portal.example/device",
		VerificationURIComplete: "https://portal.example/device?user_code=WDJB-MJHT",
		ExpiresAt:               expires, IntervalSeconds: 5,
	}}
	server := betaPortalServer(t, portal, &fakeGrantStore{})
	response := call(t, server.Handler(), http.MethodGet, betaPortalPairingPath, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "awaiting_approval", body["step"])
	require.Equal(t, "https://portal.example/device?user_code=WDJB-MJHT", body["verification_uri_complete"])
	require.InDelta(t, 5000, body["interval_ms"], 0)

	portal.status = betaportal.Status{
		State:   betaportal.StatePaired,
		Product: &betaportal.Product{Slug: "connect-local", Name: "Prusa Connect"},
		Device:  &betaportal.Device{Identifier: "instance-1"},
	}
	response = call(t, server.Handler(), http.MethodGet, betaPortalPairingPath, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	body = map[string]any{}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, map[string]any{"slug": "connect-local", "name": "Prusa Connect"}, body["product"])
	require.Equal(t, map[string]any{"identifier": "instance-1"}, body["device"])
	require.NotContains(t, body, "step")
}

func TestBetaPortalStartRefusalsCarryTheirCodes(t *testing.T) {
	t.Parallel()

	portal := &fakeBetaPortal{startErr: betaportal.ErrAlreadyPaired}
	server := betaPortalServer(t, portal, &fakeGrantStore{})
	response := call(t, server.Handler(), http.MethodPost, betaPortalPairingPath, "")
	problem := requireProblem(t, response, http.StatusConflict)
	require.Equal(t, "core.resource/BETA_PORTAL_ALREADY_PAIRED", problem.Code.String())

	portal.startErr = &betaportal.RateLimitError{RetryAfter: 90 * time.Second}
	response = call(t, server.Handler(), http.MethodPost, betaPortalPairingPath, "")
	problem = requireProblem(t, response, http.StatusTooManyRequests)
	require.Equal(t, "core.resource/BETA_PORTAL_RATE_LIMITED", problem.Code.String())
	require.Equal(t, "90", response.Header().Get("Retry-After"))

	portal.startErr = &betaportal.ClockSkewError{Offset: 2 * time.Hour}
	response = call(t, server.Handler(), http.MethodPost, betaPortalPairingPath, "")
	problem = requireProblem(t, response, http.StatusBadGateway)
	require.Equal(t, "core.resource/BETA_PORTAL_CLOCK_SKEW", problem.Code.String())
	require.Contains(t, response.Body.String(), "2h0m0s")
}

func TestBetaPortalPairingNeedsOrgAdmin(t *testing.T) {
	t.Parallel()

	grants := &fakeGrantStore{
		refuse: fault.New(authz.CodeAccessDenied(), "the subject holds no admin role on this org"),
	}
	server := betaPortalServer(t, &fakeBetaPortal{}, grants)

	response := call(t, server.Handler(), http.MethodPost, betaPortalPairingPath, "")
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestBetaPortalUpstreamAndLocalClearFaultsAreActionable(t *testing.T) {
	t.Parallel()

	portal := &fakeBetaPortal{statusErr: betaportal.ErrUpstream}
	server := betaPortalServer(t, portal, &fakeGrantStore{})
	response := call(t, server.Handler(), http.MethodGet, betaPortalPairingPath, "")
	problem := requireProblem(t, response, http.StatusBadGateway)
	require.Equal(t, "core.resource/BETA_PORTAL_UNAVAILABLE", problem.Code.String())

	portal.statusErr = nil
	portal.disconnectErr = errors.Join(betaportal.ErrRevocationIncomplete, betaportal.ErrUpstream)
	response = call(t, server.Handler(), http.MethodDelete, betaPortalPairingPath, "")
	problem = requireProblem(t, response, http.StatusBadGateway)
	require.Equal(t, "core.resource/BETA_PORTAL_REVOCATION_INCOMPLETE", problem.Code.String())
}

func TestProvisionalBetaPortalCredentialAPIIsRemoved(t *testing.T) {
	t.Parallel()

	server := betaPortalServer(t, &fakeBetaPortal{}, &fakeGrantStore{})
	response := call(t, server.Handler(), http.MethodPut, "/api/v1/server/beta-portal-credential",
		`{"credential":"must-not-be-accepted"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
}

func betaPortalServer(
	t *testing.T, portal BetaPortalPairing, grants GrantStore,
) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		BetaPortal: portal,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}
