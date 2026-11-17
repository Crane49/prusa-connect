package upgrade

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseClientSelectsSignedPlatformAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/acme/core/releases" {
			http.NotFound(response, request)

			return
		}

		fmt.Fprintf(response, `[{
			"tag_name":"v1.1.0","name":"One","body":"notes","draft":false,"prerelease":false,
			"published_at":"2026-10-26T12:00:00Z",
			"assets":[
				{"name":%q,"browser_download_url":"%s/binary","size":8},
				{"name":"SHA256SUMS","browser_download_url":"%s/sums","size":80},
				{"name":"SHA256SUMS.sig","browser_download_url":"%s/signature","size":120}
			]
		}]`, releaseBinaryName(), serverURL(request), serverURL(request), serverURL(request))
	}))
	defer server.Close()

	client := ReleaseClient{Repository: "acme/core", BaseURL: server.URL, Client: server.Client()}
	releases, err := client.List(t.Context(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Version != "1.1.0" {
		t.Fatalf("releases are %#v", releases)
	}
}

func TestReleaseDownloadIsBounded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("too large"))
	}))
	defer server.Close()

	var destination bytes.Buffer
	err := (ReleaseClient{Client: server.Client(), MaxBytes: 3}).Download(
		t.Context(), Asset{Name: "core", URL: server.URL}, &destination,
	)
	if err == nil {
		t.Fatal("oversized release was accepted")
	}
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}
