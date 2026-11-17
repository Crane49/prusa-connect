package httpfault

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// BetaPortalRetryAfterParam is the param of BETA_PORTAL_RATE_LIMITED that
// says how many seconds to wait. The handler copies it into Retry-After.
const BetaPortalRetryAfterParam = "retry_after_seconds"

var (
	betaPortalUnavailable = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_UNAVAILABLE", http.StatusBadGateway, fault.Document(
			"The Beta Portal did not answer the pairing request",
			"Try again in a moment. If it keeps failing, check that core.upgrade.server is right and "+
				"that this server can reach it.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))
	betaPortalRevocationIncomplete = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_REVOCATION_INCOMPLETE", http.StatusBadGateway, fault.Document(
			"The local pairing was cleared but remote revocation was not confirmed",
			"This server no longer uses the pairing. The Beta Portal could not be reached to revoke "+
				"the old token, so remove this device in the portal when it is available.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	betaPortalAlreadyPaired = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_ALREADY_PAIRED", http.StatusConflict, fault.Document(
			"This server is already paired with the Beta Portal",
			"Disconnect the current pairing first if you want to pair this server with another account.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	betaPortalRateLimited = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_RATE_LIMITED", http.StatusTooManyRequests, fault.Document(
			"The Beta Portal asked this server to wait before pairing again",
			"Wait for the number of seconds the message names, then start pairing again. "+
				"Retry-After gives the same wait.",
			fault.RetryAllowed,
			fault.ShowDetail,
			BetaPortalRetryAfterParam,
		))
	betaPortalPairingNeeded = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_PAIRING_NEEDED", http.StatusConflict, fault.Document(
			"The update server refused this server because it is not paired with the Beta Portal",
			"Pair this server in Settings, Updates. A pairing removed or revoked in the portal has to be "+
				"made again.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	betaPortalAccessSuspended = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_ACCESS_SUSPENDED", http.StatusForbidden, fault.Document(
			"The Beta Portal suspended the tester's access to the product",
			"The pairing stays. Ask the product team to restore the access of the tester this server "+
				"is paired as; updates work again without pairing anew.",
			fault.RetryNever,
			fault.ShowDetail,
		))
	betaPortalClockSkew = fault.Register(
		fault.DomainResource(), "BETA_PORTAL_CLOCK_SKEW", http.StatusBadGateway, fault.Document(
			"The Beta Portal refused the request because this server's clock is wrong",
			"Set the date and time of the machine this server runs on, or turn on time "+
				"synchronisation (NTP), then try again.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))
)

// BetaPortalUnavailable reports a failed or invalid upstream portal response.
func BetaPortalUnavailable() *fault.Fault {
	return betaPortalUnavailable.New("the Beta Portal is unavailable")
}

// BetaPortalRevocationIncomplete reports the safe local-clear fallback.
func BetaPortalRevocationIncomplete() *fault.Fault {
	return betaPortalRevocationIncomplete.New("local authorization was cleared")
}

// CodeBetaPortalAccessSuspended is the code BetaPortalAccessSuspended raises.
func CodeBetaPortalAccessSuspended() fault.Code { return betaPortalAccessSuspended.Code() }

// BetaPortalAccessSuspended reports a portal that refused this paired server
// because the tester's access is suspended. The pairing stays.
func BetaPortalAccessSuspended() *fault.Fault {
	return betaPortalAccessSuspended.New("the tester's access to the product is suspended")
}

// CodeBetaPortalPairingNeeded is the code BetaPortalPairingNeeded raises.
func CodeBetaPortalPairingNeeded() fault.Code { return betaPortalPairingNeeded.Code() }

// BetaPortalPairingNeeded reports an update server that refused this server's
// credential, so online updates need a new pairing.
func BetaPortalPairingNeeded() *fault.Fault {
	return betaPortalPairingNeeded.New("pair this server with the Beta Portal to read online updates")
}

// BetaPortalAlreadyPaired refuses a second pairing over a working one.
func BetaPortalAlreadyPaired() *fault.Fault {
	return betaPortalAlreadyPaired.New("disconnect the current pairing before pairing again")
}

// BetaPortalRateLimited reports the portal's wait before another pairing.
func BetaPortalRateLimited(wait time.Duration) *fault.Fault {
	seconds := max(1, int64(wait.Round(time.Second)/time.Second))

	return betaPortalRateLimited.New("wait " + strconv.FormatInt(seconds, 10) + " seconds before pairing again").
		With(fault.IntParam(BetaPortalRetryAfterParam, seconds))
}

// BetaPortalClockSkew reports a proof the portal refused because of this
// server's clock. The message names the difference.
func BetaPortalClockSkew(offset time.Duration) *fault.Fault {
	return betaPortalClockSkew.New("this server's clock differs from the Beta Portal's by " +
		offset.Round(time.Second).Abs().String())
}
