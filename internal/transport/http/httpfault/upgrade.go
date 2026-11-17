package httpfault

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// UpgradeRetryAfterParam is the param of UPGRADE_SERVER_LIMITED that holds
// the wait in seconds. The HTTP surface copies it into Retry-After.
const UpgradeRetryAfterParam = "retry_after_seconds"

var upgradeServerLimited = fault.Register(
	fault.DomainResource(), "UPGRADE_SERVER_LIMITED", http.StatusTooManyRequests, fault.Document(
		"The update server refused the request because this server asked too often",
		"Wait for the number of seconds in Retry-After, then ask again. The server keeps the "+
			"release list for a few minutes, so reloading the page does not ask the update server again.",
		fault.RetryAllowed,
		fault.ShowDetail,
		UpgradeRetryAfterParam,
	))

var upgradeConflict = fault.Register(
	fault.DomainResource(), "UPGRADE_CONFLICT", http.StatusConflict, fault.Document(
		"The update is not in a state that allows this step",
		"Reload the Updates page to see where the update is now. Another update may be running, "+
			"or this one may already have finished or rolled back.",
		fault.RetryNever,
		fault.ShowDetail,
	))

var upgradeRefused = fault.Register(
	fault.DomainResource(), "UPGRADE_REFUSED", http.StatusUnprocessableEntity, fault.Document(
		"The server refused the update or the backup it was given",
		"The message names the check that failed, such as the signature, the checksum or the "+
			"platform. Use a release built and signed for this server.",
		fault.RetryNever,
		fault.ShowDetail,
	))

// UpgradeConflict reports a step the update's current state does not allow.
func UpgradeConflict(err error) *fault.Fault {
	return upgradeConflict.New(err.Error()).Wrap(err)
}

// UpgradeRefused reports a release, bundle or backup that failed a check.
func UpgradeRefused(err error) *fault.Fault {
	return upgradeRefused.New(err.Error()).Wrap(err)
}

// CodeUpgradeConflict names the state refusal for a caller that branches on it.
func CodeUpgradeConflict() fault.Code { return upgradeConflict.Code() }

// CodeUpgradeRefused names the check refusal for a caller that branches on it.
func CodeUpgradeRefused() fault.Code { return upgradeRefused.Code() }

// UpgradeServerLimited reports a 429 from the update server with its wait.
func UpgradeServerLimited(wait time.Duration) *fault.Fault {
	seconds := max(int64(wait.Round(time.Second)/time.Second), 1)

	return upgradeServerLimited.New("the update server asks to wait " + strconv.FormatInt(seconds, 10) + " seconds").
		With(fault.IntParam(UpgradeRetryAfterParam, seconds))
}

// CodeUpgradeServerLimited names the refusal for a caller that branches on it.
func CodeUpgradeServerLimited() fault.Code { return upgradeServerLimited.Code() }
