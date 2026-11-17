package upgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestMachineClientUsesBearerForCatalogAndSameOriginArtifacts(t *testing.T) {
	t.Parallel()

	const credential = "machine-entitlement-1234"

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "DPoP "+credential ||
			request.Header.Get("DPoP") == "" {
			http.Error(response, "missing credential", http.StatusUnauthorized)

			return
		}

		switch request.URL.Path {
		case MachineCatalogPath:
			if request.URL.Query().Get("current") != "1.0.0" ||
				request.URL.Query().Get("channel") != "beta" ||
				request.URL.Query().Get("platform") != Platform() {
				http.Error(response, "wrong query", http.StatusBadRequest)

				return
			}

			_ = json.NewEncoder(response).Encode(map[string]any{
				"schema": 1,
				"releases": []any{map[string]any{
					"version": "1.1.0",
					"name":    "One",
					"binary": map[string]any{
						"name": ReleaseBinaryName("normal", "full"), "url": "/artifacts/core", "size": 4,
					},
					"sums":      map[string]any{"name": "SHA256SUMS", "url": "/artifacts/sums", "size": 4},
					"signature": map[string]any{"name": "SHA256SUMS.sig", "url": "/artifacts/signature", "size": 4},
				}},
			})
		case "/artifacts/core":
			_, _ = response.Write([]byte("core"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := MachineClient{
		BaseURL: server.URL, Client: server.Client(), Authorize: func(request *http.Request) error {
			request.Header.Set("Authorization", "DPoP "+credential)
			request.Header.Set("DPoP", "proof-for-"+request.URL.Path)

			return nil
		},
	}
	releases, err := client.List(t.Context(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases are %#v", releases)
	}
	if releases[0].Binary.URL != server.URL+"/artifacts/core" {
		t.Fatalf("artifact URL is %q", releases[0].Binary.URL)
	}

	var artifact bytes.Buffer
	if err = client.Download(t.Context(), releases[0].Binary, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.String() != "core" {
		t.Fatalf("artifact is %q", artifact.String())
	}
}

func TestMachineClientRejectsCrossOriginArtifacts(t *testing.T) {
	t.Parallel()

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("cross-origin artifact was requested")
	}))
	defer other.Close()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"schema": 1,
			"releases": []any{map[string]any{
				"version": "1.1.0",
				"binary": map[string]any{
					"name": ReleaseBinaryName("normal", "full"), "url": other.URL + "/core", "size": 4,
				},
				"sums":      map[string]any{"name": "SHA256SUMS", "url": "/sums", "size": 4},
				"signature": map[string]any{"name": "SHA256SUMS.sig", "url": "/signature", "size": 4},
			}},
		})
	}))
	defer server.Close()

	releases, err := (MachineClient{BaseURL: server.URL, Client: server.Client()}).
		List(t.Context(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 0 {
		t.Fatalf("cross-origin release was accepted: %#v", releases)
	}
}

func TestMachineClientDoesNotFollowCredentialAcrossRedirect(t *testing.T) {
	t.Parallel()

	var received string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		received = request.Header.Get("Authorization")
	}))
	defer other.Close()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, other.URL+"/catalog", http.StatusFound)
	}))
	defer server.Close()

	_, err := (MachineClient{
		BaseURL: server.URL, Client: server.Client(), Authorize: func(request *http.Request) error {
			request.Header.Set("Authorization", "DPoP do-not-leak")
			request.Header.Set("DPoP", "proof")

			return nil
		},
	}).List(t.Context(), "1.0.0")
	if err == nil {
		t.Fatal("cross-origin redirect was accepted")
	}
	if received != "" {
		t.Fatalf("credential reached redirected origin: %q", received)
	}
}

func TestMachineClientResignsSameOriginRedirect(t *testing.T) {
	t.Parallel()

	var proofs []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		proofs = append(proofs, request.Header.Get("DPoP"))
		if request.URL.Path == MachineCatalogPath {
			http.Redirect(response, request, "/redirected-catalog", http.StatusTemporaryRedirect)

			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"schema": 1, "releases": []any{}})
	}))
	defer server.Close()

	sequence := 0
	_, err := (MachineClient{
		BaseURL: server.URL,
		Client:  server.Client(),
		Authorize: func(request *http.Request) error {
			sequence++
			request.Header.Set("Authorization", "DPoP token")
			request.Header.Set("DPoP", "proof-"+request.URL.Path+"-"+strconv.Itoa(sequence))

			return nil
		},
	}).List(t.Context(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 2 || proofs[0] == proofs[1] {
		t.Fatalf("redirect proofs are %#v", proofs)
	}
}

func TestMachineDownloadIsBounded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("too large"))
	}))
	defer server.Close()

	var destination bytes.Buffer
	err := (MachineClient{BaseURL: server.URL, Client: server.Client(), MaxBytes: 3}).Download(
		t.Context(), Asset{Name: "core", URL: server.URL, Size: 0}, &destination,
	)
	if err == nil {
		t.Fatal("oversized machine artifact was accepted")
	}
}

func TestMachineClientSendsOnceMoreOnlyWhenRejectedAsks(t *testing.T) {
	t.Parallel()

	for _, retry := range []bool{false, true} {
		calls, rejected := 0, 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				http.Error(response, "refused", http.StatusUnauthorized)

				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"schema": 1, "releases": []any{}})
		}))

		client := MachineClient{
			BaseURL: server.URL,
			Client:  server.Client(),
			Authorize: func(request *http.Request) error {
				request.Header.Set("Authorization", "DPoP token")

				return nil
			},
			Rejected: func(response *http.Response) (bool, error) {
				rejected++
				if response.StatusCode != http.StatusUnauthorized {
					t.Errorf("Rejected saw %s", response.Status)
				}

				return retry, nil
			},
		}
		_, err := client.List(t.Context(), "1.0.0")
		server.Close()

		if rejected != 1 {
			t.Fatalf("retry %v: Rejected ran %d times", retry, rejected)
		}
		if retry && (err != nil || calls != 2) {
			t.Fatalf("retry: err %v after %d calls", err, calls)
		}
		if !retry && (!errors.Is(err, ErrMachineRelease) || calls != 1) {
			t.Fatalf("no retry: err %v after %d calls", err, calls)
		}
	}
}

func TestMachineClientNamesARefusedPairing(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "refused", status)
		}))
		_, err := MachineClient{BaseURL: server.URL, Client: server.Client()}.List(t.Context(), "1.0.0")
		server.Close()
		if !errors.Is(err, ErrMachineRefused) || !errors.Is(err, ErrMachineRelease) {
			t.Fatalf("status %d: err is %v", status, err)
		}
	}
}
