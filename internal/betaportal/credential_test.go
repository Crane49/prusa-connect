package betaportal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPairingStoreIsPrivateAtomicAndDoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.d", pairingFile)
	store, err := Open(path)
	require.NoError(t, err)
	require.Equal(t, StateUnpaired, store.Status().State)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	expires := time.Now().Add(time.Minute).UTC()
	_, err = store.start(pairingDocument{
		DeviceCode: "device-code-secret", UserCode: "ABCD-EFGH",
		VerificationURI: "https://portal.example/device", ExpiresAt: expires,
		IntervalSeconds: 5, NextPollAt: time.Now(),
	}, nil)
	require.NoError(t, err)
	status := store.Status()
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "device-code-secret")
	require.Equal(t, "ABCD-EFGH", status.UserCode)

	require.NoError(t, store.approve("device-code-secret", approval{
		token: "opaque-access-token", identity: &Identity{Subject: "safe-id", Name: "Safe name"},
	}))
	oldKey := store.doc.PrivateKey
	reopened, err := Open(path)
	require.NoError(t, err)
	token, _ := reopened.access()
	require.Equal(t, "opaque-access-token", token)
	require.Equal(t, StatePaired, reopened.Status().State)

	require.NoError(t, reopened.ClearLocal())
	require.Equal(t, StateUnpaired, reopened.Status().State)
	token, _ = reopened.access()
	require.Empty(t, token)
	require.NotEqual(t, oldKey, reopened.doc.PrivateKey)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(content), "opaque-access-token")
	temporary, err := filepath.Glob(path + ".tmp*")
	require.NoError(t, err)
	require.Empty(t, temporary)
}

func TestPairingPathIsBesideTheOverlay(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		filepath.Join("/var/lib/prusa-connect", pairingFile),
		Path("/var/lib/prusa-connect/resources.json"),
	)
}

func TestFailedPairingWriteKeepsPreviousFileAndMemoryState(t *testing.T) {
	t.Parallel()
	if os.PathSeparator == '\\' {
		t.Skip("Windows does not enforce the directory mode used to force the write failure")
	}

	root := t.TempDir()
	path := filepath.Join(root, pairingFile)
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.start(testPairing("write-failure"), nil)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0o700)) })
	err = store.approve("write-failure", approval{token: "must-not-be-published"})
	require.Error(t, err)
	require.Equal(t, StatePending, store.Status().State)

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, before, after)
	require.NotContains(t, string(after), "must-not-be-published")
}

func TestDPoPProofClaimsSignatureTokenHashAndUniqueness(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), strings.NewReader(strings.Repeat("a", 128)))
	require.NoError(t, err)
	address, err := url.Parse("HTTPS://Portal.Example:443/api/releases?secret=query#fragment")
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)

	first, err := makeProof(key, "get", address, "opaque-token", now)
	require.NoError(t, err)
	second, err := makeProof(key, "get", address, "opaque-token", now)
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	parts := strings.Split(first, ".")
	require.Len(t, parts, 3)
	var header proofHeader
	decodeJWTPart(t, parts[0], &header)
	require.Equal(t, dpopType, header.Type)
	require.Equal(t, "ES256", header.Algorithm)
	require.Equal(t, "EC", header.JWK.KeyType)
	require.Equal(t, "P-256", header.JWK.Curve)

	var claims proofClaims
	decodeJWTPart(t, parts[1], &claims)
	require.NotEmpty(t, claims.JTI)
	require.Equal(t, "GET", claims.HTTPMethod)
	require.Equal(t, "https://portal.example/api/releases", claims.HTTPURI)
	require.Equal(t, now.Unix(), claims.IssuedAt)
	digest := sha256.Sum256([]byte("opaque-token"))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), claims.TokenHash)

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, signature, 64, "JOSE ES256 uses raw R concatenated with S")
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	require.True(t, ecdsa.Verify(
		&key.PublicKey, hash[:],
		new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]),
	))
}

func TestDevicePairingPendingSlowDownApprovalAndRestart(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	var polls int
	var proofs []string
	var receivedForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		proofs = append(proofs, request.Header.Get("DPoP"))
		if request.Header.Get("Authorization") != "" {
			t.Error("device-flow request carried access-token authorization")
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse portal form: %v", err)

			return
		}
		switch request.URL.Path {
		case DeviceAuthorizationPath:
			receivedForm = request.Form
			writeJSON(response, http.StatusOK, map[string]any{
				"device_code": "never-return-this", "user_code": "ABCD-EFGH",
				"verification_uri": serverURL(request) + "/device",
				"expires_in":       60, "interval": 2,
			})
		case TokenPath:
			polls++
			switch polls {
			case 1:
				writeJSON(response, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
			case 2:
				writeJSON(response, http.StatusBadRequest, map[string]string{"error": "slow_down"})
			default:
				writeJSON(response, http.StatusOK, map[string]any{
					"access_token": "approved-token", "token_type": "DPoP",
					"identity": map[string]string{"subject": "account-1", "name": "Owner"},
				})
			}
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "config.d", pairingFile)
	store, err := Open(path)
	require.NoError(t, err)
	client, err := NewClient(server.URL, server.Client(), store, Metadata{
		InstanceID: "instance-1", Name: "Workshop", Platform: "linux_arm64",
	})
	require.NoError(t, err)
	client.now = func() time.Time { return now }

	status, err := client.Start(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePending, status.State)
	require.Equal(t, "ABCD-EFGH", status.UserCode)
	require.Equal(t, ClientID, receivedForm.Get("client_id"))
	require.Equal(t, Scope, receivedForm.Get("scope"))
	require.Equal(t, "instance-1", receivedForm.Get("instance_id"))
	require.Equal(t, "Workshop", receivedForm.Get("name"))
	require.Equal(t, "linux_arm64", receivedForm.Get("platform"))

	reopened, err := Open(path)
	require.NoError(t, err)
	restarted, err := NewClient(server.URL, server.Client(), reopened, Metadata{})
	require.NoError(t, err)
	restarted.now = func() time.Time { return now }
	_, err = restarted.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, polls)

	now = now.Add(2 * time.Second)
	_, err = restarted.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, polls)
	now = now.Add(2 * time.Second)
	_, err = restarted.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, polls)
	now = now.Add(6 * time.Second)
	_, err = restarted.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, polls, "slow_down adds five seconds to the interval")
	now = now.Add(time.Second)
	status, err = restarted.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, StatePaired, status.State)
	require.Equal(t, "account-1", status.Identity.Subject)
	require.GreaterOrEqual(t, len(proofs), 4)
	require.NotEqual(t, proofs[len(proofs)-1], proofs[len(proofs)-2])
}

func TestConcurrentStartsShareOnePendingPairing(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		<-release
		writeJSON(response, http.StatusOK, map[string]any{
			"device_code": "one-device-code", "user_code": "ABCD-EFGH",
			"verification_uri": serverURL(request) + "/device",
			"expires_in":       60, "interval": defaultInterval,
		})
	}))
	defer server.Close()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	client, err := NewClient(server.URL, server.Client(), store, Metadata{})
	require.NoError(t, err)

	const callers = 8
	var group sync.WaitGroup
	group.Add(callers)
	results := make(chan error, callers)
	for range callers {
		go func() {
			defer group.Done()
			status, startErr := client.Start(t.Context())
			if startErr == nil && status.State != StatePending {
				startErr = errors.New("start did not return the pending state")
			}
			results <- startErr
		}()
	}
	<-entered
	close(release)
	group.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	require.Equal(t, int32(1), requests.Load())
}

func TestPairingDeniedExpiredAndUnavailableDoNotLeakSecrets(t *testing.T) {
	t.Parallel()

	for _, responseError := range []string{"access_denied", "expired_token"} {
		t.Run(responseError, func(t *testing.T) {
			t.Parallel()
			server := pairingServer(t, responseError)
			defer server.Close()
			client, now := testClient(t, server)
			_, err := client.Start(t.Context())
			require.NoError(t, err)
			*now = now.Add(defaultInterval * time.Second)
			status, err := client.Refresh(t.Context())
			require.NoError(t, err)
			if responseError == "access_denied" {
				require.Equal(t, StateDenied, status.State)
			} else {
				require.Equal(t, StateExpired, status.State)
			}
		})
	}

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	client, err := NewClient("http://127.0.0.1:1", &http.Client{Timeout: 20 * time.Millisecond}, store, Metadata{})
	require.NoError(t, err)
	_, err = client.Start(t.Context())
	require.ErrorIs(t, err, ErrUpstream)
	require.NotContains(t, err.Error(), "device-code-secret")
}

func TestDisconnectRevokesWithDPoPAndClearsLocallyWhenPortalIsUnavailable(t *testing.T) {
	t.Parallel()

	var authorization, proof string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		proof = request.Header.Get("DPoP")
		response.WriteHeader(http.StatusNoContent)
	}))
	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "revoke-token")
	client, err := NewClient(server.URL, server.Client(), store, Metadata{})
	require.NoError(t, err)
	require.NoError(t, client.Disconnect(t.Context()))
	server.Close()
	require.Equal(t, "DPoP revoke-token", authorization)
	require.NotEmpty(t, proof)
	require.Equal(t, StateUnpaired, store.Status().State)

	markPaired(t, store, "unavailable-token")
	err = client.Disconnect(t.Context())
	require.ErrorIs(t, err, ErrRevocationIncomplete)
	require.Equal(t, StateUnpaired, store.Status().State)
	token, _ := store.access()
	require.Empty(t, token)
	require.NotContains(t, err.Error(), "unavailable-token")
}

func TestAccessTokenRedirectsStayOnOriginAndReceiveFreshProofs(t *testing.T) {
	t.Parallel()

	var proofs []string
	var authorizations []string
	var portal *httptest.Server
	portal = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		proofs = append(proofs, request.Header.Get("DPoP"))
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		if request.URL.Path == RevocationPath {
			http.Redirect(response, request, portal.URL+"/revoked", http.StatusTemporaryRedirect)

			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer portal.Close()

	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "redirect-token")
	client, err := NewClient(portal.URL, portal.Client(), store, Metadata{})
	require.NoError(t, err)
	require.NoError(t, client.Disconnect(t.Context()))
	require.Equal(t, []string{"DPoP redirect-token", "DPoP redirect-token"}, authorizations)
	require.Len(t, proofs, 2)
	require.NotEqual(t, proofs[0], proofs[1])

	parts := strings.Split(proofs[1], ".")
	require.Len(t, parts, 3)
	var claims proofClaims
	decodeJWTPart(t, parts[1], &claims)
	require.Equal(t, portal.URL+"/revoked", claims.HTTPURI)

	var leakedAuthorization, leakedProof string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		leakedAuthorization = request.Header.Get("Authorization")
		leakedProof = request.Header.Get("DPoP")
	}))
	defer other.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, other.URL, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	store, err = Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	markPaired(t, store, "contained-token")
	client, err = NewClient(redirecting.URL, redirecting.Client(), store, Metadata{})
	require.NoError(t, err)
	err = client.Disconnect(t.Context())
	require.ErrorIs(t, err, ErrRevocationIncomplete)
	require.Empty(t, leakedAuthorization)
	require.Empty(t, leakedProof)
	require.Equal(t, StateUnpaired, store.Status().State)
}

func decodeJWTPart(t *testing.T, encoded string, into any) {
	t.Helper()
	content, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, into))
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}

func pairingServer(t *testing.T, tokenError string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == DeviceAuthorizationPath {
			writeJSON(response, http.StatusOK, map[string]any{
				"device_code": "device-code-secret", "user_code": "ABCD-EFGH",
				"verification_uri": serverURL(request) + "/device",
				"expires_in":       60, "interval": defaultInterval,
			})

			return
		}
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": tokenError})
	}))
}

func testClient(t *testing.T, server *httptest.Server) (*Client, *time.Time) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), pairingFile))
	require.NoError(t, err)
	client, err := NewClient(server.URL, server.Client(), store, Metadata{})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0).UTC()
	client.now = func() time.Time { return now }

	return client, &now
}

func testPairing(deviceCode string) pairingDocument {
	return pairingDocument{
		DeviceCode: deviceCode, UserCode: "ABCD-EFGH",
		VerificationURI: "https://portal.example/device",
		ExpiresAt:       time.Now().Add(time.Minute), IntervalSeconds: defaultInterval,
	}
}

func markPaired(t *testing.T, store *Store, token string) {
	t.Helper()
	_, err := store.start(testPairing("code-for-"+token), nil)
	require.NoError(t, err)
	require.NoError(t, store.approve("code-for-"+token, approval{token: token}))
}

// TestHoldsTokenReadsAPairingWithoutWritingIt is what a server with upgrades
// off asks of a pairing an earlier run left. Only a token the portal accepts
// counts, so a disconnected pairing is not named again at every start.
func TestHoldsTokenReadsAPairingWithoutWritingIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), pairingFile)
	held, err := HoldsToken(path)
	require.NoError(t, err)
	require.False(t, held)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "HoldsToken wrote a pairing")

	store, err := Open(path)
	require.NoError(t, err)
	held, err = HoldsToken(path)
	require.NoError(t, err)
	require.False(t, held, "an unpaired file")

	_, err = store.start(pairingDocument{
		DeviceCode: "device-code", UserCode: "ABCD-EFGH", VerificationURI: "https://portal.example/device",
		ExpiresAt: time.Now().Add(time.Minute).UTC(), IntervalSeconds: 5, NextPollAt: time.Now(),
	}, nil)
	require.NoError(t, err)
	held, err = HoldsToken(path)
	require.NoError(t, err)
	require.False(t, held, "a pending file")

	require.NoError(t, store.approve("device-code", approval{token: "opaque-access-token"}))
	held, err = HoldsToken(path)
	require.NoError(t, err)
	require.True(t, held, "a paired file")

	require.NoError(t, store.ClearLocal())
	held, err = HoldsToken(path)
	require.NoError(t, err)
	require.False(t, held, "a disconnected file")

	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	_, err = HoldsToken(path)
	require.Error(t, err, "a damaged file may still hold a token")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "{", string(content), "HoldsToken changed a damaged file")
}
