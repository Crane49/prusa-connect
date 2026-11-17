package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
)

func TestMarketplaceUsesEntitlementOnlyOnTheConfiguredPortalOrigin(t *testing.T) {
	t.Parallel()

	const credential = "shared-entitlement-1234"
	var authorization string

	market := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		if authorization != "" {
			if request.Header.Get("DPoP") == "" {
				t.Error("authorized marketplace request carried no DPoP proof")
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"extensions":[],"next":null}`))
	}))
	defer market.Close()

	store := pairedPortalStore(t, credential)

	read := func(portal string) string {
		authorization = ""
		catalogue := newMarketplaceCatalogue(
			config.Marketplace{Enabled: new(true), URL: market.URL, Timeout: time.Second},
			nil,
			newPortalAuthorization(portal, store),
		)
		_, err := catalogue.list(t.Context())
		require.NoError(t, err)

		return authorization
	}

	require.Empty(t, read("http://127.0.0.1:1"),
		"a custom local marketplace must not receive the Beta Portal credential")
	require.Equal(t, "DPoP "+credential, read(market.URL))
}

func TestAuthenticatedMarketplaceRedirectStaysOnOrigin(t *testing.T) {
	t.Parallel()

	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		leaked = request.Header.Get("Authorization")
		http.Error(response, "unexpected", http.StatusInternalServerError)
	}))
	defer other.Close()

	market := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, other.URL, http.StatusFound)
	}))
	defer market.Close()

	store := pairedPortalStore(t, "do-not-leak")

	catalogue := newMarketplaceCatalogue(
		config.Marketplace{Enabled: new(true), URL: market.URL, Timeout: time.Second},
		nil,
		newPortalAuthorization(market.URL, store),
	)
	_, err := catalogue.list(t.Context())
	require.Error(t, err)
	require.Empty(t, leaked)
}

func TestMarketplaceArtifactUsesEntitlementOnlyWhenSelectedByThePortal(t *testing.T) {
	t.Parallel()

	const credential = "shared-entitlement-5678"
	content := []byte("module archive")
	digest := sha256.Sum256(content)
	var authorizations []string
	var proofs []string

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		proofs = append(proofs, request.Header.Get("DPoP"))
		_, _ = response.Write(content)
	}))
	defer server.Close()

	store := pairedPortalStore(t, credential)

	address, err := url.Parse(server.URL + "/module.tar.gz")
	require.NoError(t, err)
	installer := &moduleInstaller{
		client: server.Client(),
		log:    discard(),
		portal: newPortalAuthorization(server.URL, store),
	}

	path, err := installer.fetch(t.Context(), address, hex.EncodeToString(digest[:]), true)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	path, err = installer.fetch(t.Context(), address, hex.EncodeToString(digest[:]), false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	require.Equal(t, []string{"DPoP " + credential, ""}, authorizations)
	require.NotEmpty(t, proofs[0])
	require.Empty(t, proofs[1])
}

func pairedPortalStore(t *testing.T, token string) *betaportal.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pairing.json")
	_, err := betaportal.Open(path)
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(content, &document))
	document["access_token"] = token
	document["state"] = "paired"
	delete(document, "pairing")
	content, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	store, err := betaportal.Open(path)
	require.NoError(t, err)

	return store
}

func TestARefusedPortalTokenMarksThePairingRevoked(t *testing.T) {
	t.Parallel()

	const credential = "refused-entitlement-1234"
	calls := 0
	market := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(response, "invalid_token", http.StatusUnauthorized)
	}))
	defer market.Close()

	store := pairedPortalStore(t, credential)
	catalogue := newMarketplaceCatalogue(
		config.Marketplace{Enabled: new(true), URL: market.URL, Timeout: time.Second},
		nil,
		newPortalAuthorization(market.URL, store),
	)
	_, err := catalogue.list(t.Context())
	require.Error(t, err)

	require.Equal(t, 1, calls, "a refused token is not sent again")
	require.Equal(t, betaportal.StateRevoked, store.Status().State)
}

func TestAPortalClockAheadIsCorrectedAndTheRequestSentOnce(t *testing.T) {
	t.Parallel()

	const credential = "skewed-entitlement-1234"
	calls := 0
	market := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			response.Header().Set("Date", time.Now().Add(2*time.Hour).UTC().Format(http.TimeFormat))
			http.Error(response, "invalid_dpop_proof", http.StatusUnauthorized)

			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"extensions":[],"next":null}`))
	}))
	defer market.Close()

	store := pairedPortalStore(t, credential)
	catalogue := newMarketplaceCatalogue(
		config.Marketplace{Enabled: new(true), URL: market.URL, Timeout: time.Second},
		nil,
		newPortalAuthorization(market.URL, store),
	)
	_, err := catalogue.list(t.Context())
	require.NoError(t, err)

	require.Equal(t, 2, calls)
	require.Equal(t, betaportal.StatePaired, store.Status().State)
}

func TestAnotherMarketplaceCannotRedirectIntoASignedPortalRequest(t *testing.T) {
	t.Parallel()

	var authorization, proof string
	portal := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization, proof = request.Header.Get("Authorization"), request.Header.Get("DPoP")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"extensions":[],"next":null}`))
	}))
	defer portal.Close()
	other := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, portal.URL+request.URL.RequestURI(), http.StatusFound)
	}))
	defer other.Close()

	catalogue := newMarketplaceCatalogue(
		config.Marketplace{Enabled: new(true), URL: other.URL, Timeout: time.Second},
		nil,
		newPortalAuthorization(portal.URL, pairedPortalStore(t, "portal-entitlement-1234")),
	)
	_, err := catalogue.list(t.Context())
	require.NoError(t, err)
	require.Empty(t, authorization, "a redirect from another marketplace must not be signed")
	require.Empty(t, proof)
}
