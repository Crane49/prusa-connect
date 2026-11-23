package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// appPairings is the app pairing surface as the HTTP layer consumes it, over
// the platform's pairing store and the credential store. It is here for the
// reason apiCredentials is: the transport may not import the platform group
// and the platform may not name the transport's types.
//
// Every step writes one audit line at info, with the message fixed and the
// app's name, the scopes and who decided as attributes, so an operator can
// read who let which app in and when.
type appPairings struct {
	store       auth.AppPairingStore
	credentials auth.Store
	verifier    *auth.Verifier
	logger      *slog.Logger
	now         func() time.Time
}

var _ httpapi.AppPairings = appPairings{}

// openAppPairings is the pairing store this process runs with: rows in the
// host's own database, so a pairing one replica opened can be approved and
// collected through another.
func openAppPairings(
	host hostStore, credentials auth.Store, verifier *auth.Verifier, logger *slog.Logger,
) (appPairings, error) {
	store, err := dbstore.NewAppPairings(host.db, host.engine)
	if err != nil {
		return appPairings{}, fmt.Errorf("open the app pairing store: %w", err)
	}

	return appPairings{store: store, credentials: credentials, verifier: verifier, logger: logger, now: time.Now}, nil
}

// Request opens a pairing.
func (a appPairings) Request(ctx context.Context, request httpapi.AppPairingRequest) (httpapi.IssuedAppPairing, error) {
	issued, err := auth.RequestAppPairing(ctx, a.store, auth.AppPairingRequest{
		AppName: request.AppName, Scopes: request.Scopes, ClientAddress: request.ClientAddress,
	}, a.now())
	if err != nil {
		if errors.Is(err, auth.ErrAppPairingBusy) {
			a.logger.WarnContext(ctx, "an app pairing was refused: too many are waiting",
				"app", request.AppName, "address", request.ClientAddress, "limit", auth.MaxPendingAppPairings)

			return httpapi.IssuedAppPairing{}, httpapi.ErrAppPairingBusy
		}

		return httpapi.IssuedAppPairing{}, err
	}

	a.logger.InfoContext(ctx, "an app asked for a credential",
		"audit", true, "app", request.AppName, "scopes", request.Scopes, "address", request.ClientAddress,
		"user_code", auth.FormatUserCode(issued.UserCode), "expires_at", issued.ExpiresAt)

	return httpapi.IssuedAppPairing{
		AppPairing: appPairingOf(issued.AppPairing),
		DeviceCode: issued.DeviceCode,
		Interval:   issued.Interval,
		Lifetime:   issued.ExpiresAt.Sub(issued.CreatedAt),
	}, nil
}

// Lookup finds a live pairing that still waits for a decision.
func (a appPairings) Lookup(ctx context.Context, userCode string) (httpapi.AppPairing, error) {
	pairing, err := auth.LiveAppPairing(ctx, a.store, userCode, a.now())
	if err != nil {
		return httpapi.AppPairing{}, appPairingRefusal(err)
	}

	if pairing.State != auth.AppPairingPending {
		return httpapi.AppPairing{}, httpapi.ErrAppPairingClosed
	}

	return appPairingOf(pairing), nil
}

// Decide records a person's answer.
func (a appPairings) Decide(ctx context.Context, decision httpapi.AppPairingDecision) error {
	pairing, err := auth.LiveAppPairing(ctx, a.store, decision.UserCode, a.now())
	if err != nil {
		return appPairingRefusal(err)
	}

	state := auth.AppPairingDenied
	if decision.Approve {
		state = auth.AppPairingApproved
	}

	err = auth.DecideAppPairing(ctx, a.store, auth.AppPairing{
		UserCode:          decision.UserCode,
		State:             state,
		DecidedBy:         decision.DecidedBy,
		DecidedScopes:     decision.Scopes,
		DecidedGeneration: decision.Fence.Subject,
		DecidedRoot:       decision.Root,
		RootGeneration:    decision.Fence.Root,
	}, a.now())
	if err != nil {
		return appPairingRefusal(err)
	}

	message := "a person denied an app a credential"
	if decision.Approve {
		message = "a person approved an app a credential"
	}

	a.logger.InfoContext(ctx, message,
		"audit", true, "app", pairing.AppName, "by", decision.DecidedBy, "scopes", decision.Scopes,
		"asked", pairing.Scopes, "address", pairing.ClientAddress)

	return nil
}

// Collect is one poll. After an approval it mints the credential, for the
// person who approved, with the scopes they approved, at the generations read
// before their decision. The credential is returned bare and in its JWT shape
// (ADR-20261112-nay).
func (a appPairings) Collect(ctx context.Context, deviceCode string) (httpapi.IssuedCredential, error) {
	pairing, err := auth.PollAppPairing(ctx, a.store, deviceCode, a.now())
	if err != nil {
		if errors.Is(err, auth.ErrAppPairingDenied) || errors.Is(err, auth.ErrAppPairingExpired) {
			a.logger.InfoContext(ctx, "an app was told its pairing ended without a credential",
				"audit", true, "reason", err.Error())
		}

		return httpapi.IssuedCredential{}, appPairingRefusal(err)
	}

	token, cred, err := auth.IssueAPI(ctx, a.credentials, auth.APISpec{
		Subject:     pairing.DecidedBy,
		Name:        pairing.AppName,
		MintedBy:    pairing.DecidedBy,
		RootCreator: pairing.DecidedRoot,
		Fence: &auth.Fence{
			Subject: pairing.DecidedGeneration,
			Minter:  pairing.DecidedGeneration,
			Root:    pairing.RootGeneration,
		},
		Scopes: pairing.DecidedScopes,
		Origin: auth.OriginAppPairing,
	}, a.now())
	if err != nil {
		return httpapi.IssuedCredential{}, fmt.Errorf("mint the credential of the app %q: %w", pairing.AppName, err)
	}

	a.logger.InfoContext(ctx, "an app collected the credential a person approved",
		"audit", true, "app", pairing.AppName, "credential", cred.PublicID, "subject", cred.Subject,
		"scopes", cred.Scopes, "expires_at", cred.ExpiresAt)

	return httpapi.IssuedCredential{
		APICredential: apiCredentialOf(cred),
		Token:         string(token),
		AccessToken:   string(auth.WrapJWT(token, cred)),
	}, nil
}

// OpenPageSession trades an app pairing credential for a page session, an
// api credential that stands in for it in an HttpOnly cookie
// (ADR-20261112-nay). The token is checked as any presented token is, and
// anything but a live app pairing credential is refused.
func (a appPairings) OpenPageSession(
	ctx context.Context, token string, admit func(parent string) bool,
) (httpapi.PageSession, error) {
	parent, err := a.verifier.Verify(ctx, auth.Token(token), auth.TransportHTTP)
	if err != nil {
		return httpapi.PageSession{}, err
	}

	now := a.now()
	if auth.OpensPageSession(parent, now) != nil {
		return httpapi.PageSession{}, httpapi.ErrNotAppCredential
	}

	if !admit(parent.PublicID) {
		return httpapi.PageSession{}, httpapi.ErrPageSessionLimited
	}

	issued, cred, err := auth.IssuePageSession(ctx, a.credentials, parent, now)
	if errors.Is(err, auth.ErrNotAppCredential) {
		return httpapi.PageSession{}, httpapi.ErrNotAppCredential
	}

	if err != nil {
		return httpapi.PageSession{}, fmt.Errorf("open a page session for %q: %w", parent.PublicID, err)
	}

	a.logger.InfoContext(ctx, "an app opened a page session",
		"audit", true, "app", parent.Name, "credential", parent.PublicID, "session", cred.PublicID,
		"subject", cred.Subject, "expires_at", cred.ExpiresAt)

	return httpapi.PageSession{Token: string(issued), ExpiresAt: cred.ExpiresAt}, nil
}

// appPairingOf renders a pairing for the person who decides it.
func appPairingOf(pairing auth.AppPairing) httpapi.AppPairing {
	return httpapi.AppPairing{
		UserCode:      auth.FormatUserCode(pairing.UserCode),
		AppName:       pairing.AppName,
		Scopes:        append([]string{}, pairing.Scopes...),
		ClientAddress: pairing.ClientAddress,
		ExpiresAt:     pairing.ExpiresAt,
	}
}

// appPairingRefusal turns the platform's answer into the surface's.
func appPairingRefusal(err error) error {
	switch {
	case errors.Is(err, auth.ErrAppPairingPending):
		return httpapi.ErrAppPairingPending
	case errors.Is(err, auth.ErrAppPairingSlowDown):
		return httpapi.ErrAppPairingSlowDown
	case errors.Is(err, auth.ErrAppPairingDenied):
		return httpapi.ErrAppPairingDenied
	case errors.Is(err, auth.ErrAppPairingExpired):
		return httpapi.ErrAppPairingExpired
	case errors.Is(err, auth.ErrAppPairingUnknown):
		return httpapi.ErrAppPairingUnknown
	case errors.Is(err, auth.ErrAppPairingDecided):
		return httpapi.ErrAppPairingClosed
	case errors.Is(err, auth.ErrAppPairingBusy):
		return httpapi.ErrAppPairingBusy
	default:
		return err
	}
}
