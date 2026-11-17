package app

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/oidc"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// A failed sign-in names its reason to the sign-in page. A forged or replayed
// callback is not a sign-in that failed, and stays the one refusal.
func TestASignInFailureIsSortedByWhatFailed(t *testing.T) {
	t.Parallel()

	for cause, want := range map[error]string{
		session.ErrLoginExpired:        httpapi.SignInReasonExpired,
		oidc.ErrExchangeRefused:        httpapi.SignInReasonProvider,
		oidc.ErrUnsupportedAlgorithm:   httpapi.SignInReasonIDToken,
		oidc.ErrBadSignature:           httpapi.SignInReasonIDToken,
		oidc.ErrClaimRejected:          httpapi.SignInReasonIDToken,
		oidc.ErrSubject:                httpapi.SignInReasonSubject,
		errors.New("database is gone"): httpapi.SignInReasonFailed,
	} {
		reason, failed := signInReason(auth.Refuse(fmt.Errorf("wrapped: %w", cause)))
		require.True(t, failed, cause)
		require.Equal(t, want, reason, cause)
	}

	_, failed := signInReason(auth.Refuse(session.ErrUnknownState))
	require.False(t, failed, "a state this server did not start is not a failed sign-in")
}
