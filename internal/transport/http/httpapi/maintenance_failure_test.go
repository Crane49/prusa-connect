package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A 429 from the update server is its own retryable refusal with the wait in
// Retry-After, never the generic BETA_PORTAL_UNAVAILABLE.
func TestTheUpdateServerLimitBecomesARetryableRefusalWithItsWait(t *testing.T) {
	t.Parallel()

	limited := &upgrade.LimitedError{RetryAfter: 90 * time.Second}
	cause := fmt.Errorf("%w: catalog: %w", upgrade.ErrMachineRelease, limited)
	err := maintenanceFailure("list releases", cause)

	headers, ok := errors.AsType[huma.HeadersError](err)
	if !ok || headers.GetHeaders().Get("Retry-After") != "90" {
		t.Fatalf("error %v carries no Retry-After of 90", err)
	}
	raised, ok := errors.AsType[*fault.Fault](err)
	if !ok || !fault.HasCode(raised, httpfault.CodeUpgradeServerLimited()) {
		t.Fatalf("error is %v", err)
	}
	if raised.Status() != http.StatusTooManyRequests {
		t.Fatalf("status is %d", raised.Status())
	}
}

// An update server that refuses the credential asks for a new pairing; the
// portal answered, so it is not BETA_PORTAL_UNAVAILABLE and not retryable.
func TestARefusedPairingAsksForANewOne(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: %w: catalog answered 401 Unauthorized",
		upgrade.ErrMachineRelease, upgrade.ErrMachineRefused)
	raised, ok := errors.AsType[*fault.Fault](maintenanceFailure("list releases", cause))
	if !ok || !fault.HasCode(raised, httpfault.CodeBetaPortalPairingNeeded()) {
		t.Fatalf("error is %v", raised)
	}
	if raised.Status() != http.StatusConflict {
		t.Fatalf("status is %d", raised.Status())
	}
}

// A suspended tester keeps the pairing, so the answer names the suspension
// rather than asking for a new pairing.
func TestASuspendedAccessIsNotAskedToPairAgain(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: read catalog: %w", upgrade.ErrMachineRelease, betaportal.ErrAccessSuspended)
	raised, ok := errors.AsType[*fault.Fault](maintenanceFailure("list releases", cause))
	if !ok || !fault.HasCode(raised, httpfault.CodeBetaPortalAccessSuspended()) {
		t.Fatalf("error is %v", raised)
	}
	if raised.Status() != http.StatusForbidden {
		t.Fatalf("status is %d", raised.Status())
	}
}

// A step the update's state refuses, such as a second Apply on a committed
// update, names that state rather than a request that failed the schema.
func TestAStepTheUpdateStateRefusesIsAConflict(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: apply authorization needs rehearsed, found committed", upgrade.ErrJournal)
	raised, ok := errors.AsType[*fault.Fault](maintenanceFailure("apply the upgrade", cause))
	if !ok || !fault.HasCode(raised, httpfault.CodeUpgradeConflict()) {
		t.Fatalf("error is %v", raised)
	}
	if raised.Status() != http.StatusConflict {
		t.Fatalf("status is %d", raised.Status())
	}
}

// A bundle that fails a check names the check rather than the request schema.
func TestABundleThatFailsACheckIsRefused(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: no trusted key signed the bundle", upgrade.ErrSignature)
	raised, ok := errors.AsType[*fault.Fault](maintenanceFailure("inspect the bundle", cause))
	if !ok || !fault.HasCode(raised, httpfault.CodeUpgradeRefused()) {
		t.Fatalf("error is %v", raised)
	}
	if raised.Status() != http.StatusUnprocessableEntity {
		t.Fatalf("status is %d", raised.Status())
	}
}
