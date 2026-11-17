package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A request with no credential is refused with the one fault, and marked so
// the transport logs it at debug. A malformed credential is not marked.
func TestARequestWithNoCredentialIsMarkedAsSuch(t *testing.T) {
	t.Parallel()

	principals := credentialPrincipals{
		inner: session.NewPrincipals(auth.NewVerifier(auth.NewMemoryStore(), nil), nil),
	}

	_, err := principals.Principal(httptest.NewRequest(http.MethodGet, "/api/v1/principal", nil))
	require.ErrorIs(t, err, httpapi.ErrNoCredential)
	require.True(t, fault.HasCode(err, fault.AuthTokenInvalid.Code()), "the caller gets the one refusal")

	malformed := httptest.NewRequest(http.MethodGet, "/api/v1/principal", nil)
	malformed.Header.Set("Authorization", "Bearer pcl_not-a-token")

	_, err = principals.Principal(malformed)
	require.Error(t, err)
	require.NotErrorIs(t, err, httpapi.ErrNoCredential)
}
