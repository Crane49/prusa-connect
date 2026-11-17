package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// The sign-in button names the issuer by the name configured, else by the
// host the person is about to be sent to.
func TestTheSignInButtonNamesTheProvider(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Company login", providerName(config.Auth{
		Issuer: "https://sso.example.com/realms/home", ProviderName: "Company login",
	}))
	require.Equal(t, "sso.example.com", providerName(config.Auth{Issuer: "https://sso.example.com:8443/realms/home"}))
}
