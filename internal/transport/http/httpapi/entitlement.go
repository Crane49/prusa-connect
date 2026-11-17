package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const (
	betaPortalPairingPath = "/api/v1/server/beta-portal-pairing"
	millisecondsPerSecond = 1000
)

// BetaPortalPairing is the server-owned OAuth device pairing.
type BetaPortalPairing interface {
	Start(context.Context) (betaportal.Status, error)
	Status(context.Context) (betaportal.Status, error)
	Refresh(context.Context) (betaportal.Status, error)
	Disconnect(context.Context) error
}

type betaPortalPairingStatus struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	State betaportal.State `json:"state" enum:"unpaired,pending,paired,denied,expired,revoked" doc:"Pairing state. revoked means the portal refused the token this server held. The server pairs again to get a new one."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Step betaportal.Step `json:"step,omitempty" enum:"awaiting_confirmation,awaiting_approval" doc:"What a pending pairing waits for: the tester confirming the code on the portal, or the product team approving the device."`

	UserCode          string     `json:"user_code,omitempty"                 doc:"Pairing code."`
	VerifyURL         string     `json:"verification_uri,omitempty"          doc:"Verification address."                                                                   format:"uri"`
	VerifyURLComplete string     `json:"verification_uri_complete,omitempty" doc:"Verification address with the pairing code filled in. Absent when the portal sent none." format:"uri"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"                doc:"Code expiry."`
	PollIntervalMS    int64      `json:"interval_ms,omitempty"               doc:"Milliseconds between two refreshes of a pending pairing that can reach the portal."`

	Identity *betaportal.Identity `json:"identity,omitempty" doc:"Safe account metadata."`
	Product  *BetaPortalProduct   `json:"product,omitempty"  doc:"The portal product this server is registered under."`
	Device   *BetaPortalDevice    `json:"device,omitempty"   doc:"The portal's record of this server."`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	AccessSuspended bool `json:"access_suspended,omitempty" doc:"The portal refused this paired server because the tester's access to the product is suspended. The pairing stays and works again once the product team restores the access."`
}

// BetaPortalProduct is the portal product a paired server is registered under.
type BetaPortalProduct struct {
	Slug string `json:"slug,omitempty" doc:"The product's portal identifier."`
	Name string `json:"name,omitempty" doc:"The product's display name."`
}

// BetaPortalDevice is the portal's record of a paired server.
type BetaPortalDevice struct {
	Identifier string `json:"identifier,omitempty" doc:"The identifier the portal registered this server under."`
}

type betaPortalPairingOutput struct {
	Body betaPortalPairingStatus
}

type disconnectBetaPortalPairingOutput struct {
	Status int
}

func registerBetaPortalPairing(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-beta-portal-pairing-status",
		Method:      http.MethodGet,
		Path:        betaPortalPairingPath,
		Summary:     "Beta Portal pairing status",
		Description: "Reports the persisted safe pairing state without contacting the portal. " +
			"The device code, access token and proof key never leave the server. It takes the admin role on this server's organization." +
			upgradesOffNote,
		Tags:   []string{"system"},
		Errors: betaPortalPairingErrors,
	}, func(ctx context.Context, _ *struct{}) (*betaPortalPairingOutput, error) {
		if err := requireBetaPortalAdmin(ctx, cfg); err != nil {
			return nil, err
		}
		status, err := cfg.BetaPortal.Status(ctx)
		if err != nil {
			return nil, betaPortalError(err)
		}

		return pairingOutput(status), nil
	})

	register(api, huma.Operation{
		OperationID: "start-beta-portal-pairing",
		Method:      http.MethodPost,
		Path:        betaPortalPairingPath,
		Summary:     "Start Beta Portal pairing",
		Description: "Starts OAuth device authorization with this server's instance identity and a local P-256 proof key. " +
			"Repeating it while a code is pending returns that same safe status. " +
			"It refuses with 409 while the server is paired, so only a disconnect ends a working pairing." + upgradesOffNote,
		Tags:          []string{"system"},
		DefaultStatus: http.StatusCreated,
		Errors:        betaPortalStartErrors,
	}, func(ctx context.Context, _ *struct{}) (*betaPortalPairingOutput, error) {
		if err := requireBetaPortalAdmin(ctx, cfg); err != nil {
			return nil, err
		}
		status, err := cfg.BetaPortal.Start(ctx)
		if err != nil {
			return nil, betaPortalError(err)
		}

		return pairingOutput(status), nil
	})

	register(api, huma.Operation{
		OperationID: "refresh-beta-portal-pairing",
		Method:      http.MethodPost,
		Path:        betaPortalPairingPath + "/refresh",
		Summary:     "Refresh Beta Portal pairing",
		Description: "Polls the token endpoint when the saved interval is due and returns only the safe pairing state. " +
			"Calling it early returns the saved state without contacting the portal. " +
			"On a paired server it asks the portal at most once a minute whether the pairing still works." + upgradesOffNote,
		Tags:   []string{"system"},
		Errors: betaPortalPairingErrors,
	}, func(ctx context.Context, _ *struct{}) (*betaPortalPairingOutput, error) {
		if err := requireBetaPortalAdmin(ctx, cfg); err != nil {
			return nil, err
		}
		status, err := cfg.BetaPortal.Refresh(ctx)
		if err != nil {
			return nil, betaPortalError(err)
		}

		return pairingOutput(status), nil
	})

	register(api, huma.Operation{
		OperationID: "disconnect-beta-portal",
		Method:      http.MethodDelete,
		Path:        betaPortalPairingPath,
		Summary:     "Disconnect the Beta Portal",
		Description: "Revokes the access token at the portal and clears the local token and proof key. " +
			"If the portal is unavailable, local authorization is still cleared and the response says remote revocation was not confirmed." +
			upgradesOffNote,
		Tags:          []string{"system"},
		DefaultStatus: http.StatusNoContent,
		Errors:        betaPortalPairingErrors,
	}, func(ctx context.Context, _ *struct{}) (*disconnectBetaPortalPairingOutput, error) {
		if err := requireBetaPortalAdmin(ctx, cfg); err != nil {
			return nil, err
		}
		if err := cfg.BetaPortal.Disconnect(ctx); err != nil {
			return nil, betaPortalError(err)
		}

		return &disconnectBetaPortalPairingOutput{Status: http.StatusNoContent}, nil
	})
}

var betaPortalPairingErrors = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
	http.StatusBadGateway, http.StatusInternalServerError,
}

var betaPortalStartErrors = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
	http.StatusTooManyRequests, http.StatusBadGateway, http.StatusInternalServerError,
}

func requireBetaPortalAdmin(ctx context.Context, cfg Config) error {
	if cfg.BetaPortal == nil {
		return httpfault.ResourceNotFound(httpfault.Reference("server", "beta-portal-pairing"))
	}

	return requireOrgAdmin(ctx, cfg, "the Beta Portal pairing")
}

func pairingOutput(status betaportal.Status) *betaPortalPairingOutput {
	var expiresAt *time.Time
	if !status.ExpiresAt.IsZero() {
		expiresAt = &status.ExpiresAt
	}

	var product *BetaPortalProduct
	if status.Product != nil {
		product = &BetaPortalProduct{Slug: status.Product.Slug, Name: status.Product.Name}
	}
	var device *BetaPortalDevice
	if status.Device != nil {
		device = &BetaPortalDevice{Identifier: status.Device.Identifier}
	}

	return &betaPortalPairingOutput{Body: betaPortalPairingStatus{
		State: status.State, Step: status.Step, UserCode: status.UserCode,
		VerifyURL: status.VerificationURI, VerifyURLComplete: status.VerificationURIComplete,
		ExpiresAt: expiresAt, PollIntervalMS: int64(status.IntervalSeconds) * millisecondsPerSecond,
		Identity: status.Identity, Product: product, Device: device,
		AccessSuspended: status.AccessSuspended,
	}}
}

func betaPortalError(err error) error {
	var limited *betaportal.RateLimitError
	var skew *betaportal.ClockSkewError
	switch {
	case errors.As(err, &limited):
		wait := httpfault.BetaPortalRateLimited(limited.RetryAfter)
		seconds, _ := wait.Params().Get(httpfault.BetaPortalRetryAfterParam)

		return huma.ErrorWithHeaders(wait, http.Header{"Retry-After": []string{seconds.String()}})
	case errors.As(err, &skew):
		return httpfault.BetaPortalClockSkew(skew.Offset)
	case errors.Is(err, betaportal.ErrAlreadyPaired):
		return httpfault.BetaPortalAlreadyPaired()
	case errors.Is(err, betaportal.ErrRevocationIncomplete):
		return httpfault.BetaPortalRevocationIncomplete()
	case errors.Is(err, betaportal.ErrUpstream):
		return httpfault.BetaPortalUnavailable()
	default:
		return httpfault.Unexpected("change the Beta Portal pairing").Wrap(err)
	}
}
